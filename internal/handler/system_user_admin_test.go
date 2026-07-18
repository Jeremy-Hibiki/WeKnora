package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// stubUserAdminService satisfies only the UserService methods the user-
// management handlers reach. Embedding the interface keeps any other
// call a nil-deref panic, surfacing contract drift loudly — the same
// pattern used by stubMemberUserService.
type stubUserAdminService struct {
	interfaces.UserService

	listAll   func(ctx context.Context, search string, offset, limit int) ([]*types.User, int64, error)
	create    func(ctx context.Context, req *types.RegisterRequest) (*types.User, error)
	setActive func(ctx context.Context, userID string, active bool) (*types.User, error)
	getByID   func(ctx context.Context, id string) (*types.User, error)
	resetPW   func(ctx context.Context, userID, newPassword string) error
}

func (s *stubUserAdminService) ListAllUsers(ctx context.Context, search string, offset, limit int) ([]*types.User, int64, error) {
	return s.listAll(ctx, search, offset, limit)
}

func (s *stubUserAdminService) AdminCreateUser(ctx context.Context, req *types.RegisterRequest) (*types.User, error) {
	return s.create(ctx, req)
}

func (s *stubUserAdminService) SetUserActive(ctx context.Context, userID string, active bool) (*types.User, error) {
	return s.setActive(ctx, userID, active)
}

func (s *stubUserAdminService) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	return s.getByID(ctx, id)
}

func (s *stubUserAdminService) AdminResetPassword(ctx context.Context, userID, newPassword string) error {
	return s.resetPW(ctx, userID, newPassword)
}

func newSystemUserHandler(svc interfaces.UserService) *SystemHandler {
	return &SystemHandler{userSvc: svc}
}

func newSystemUserTestRouter(h *SystemHandler, callerID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/system/admin/users", func(c *gin.Context) {
		c.Request = withActorRequest(c.Request, callerID)
		h.ListUsers(c)
	})
	r.POST("/system/admin/users", func(c *gin.Context) {
		c.Request = withActorRequest(c.Request, callerID)
		h.AdminCreateUser(c)
	})
	r.PUT("/system/admin/users/:id/status", func(c *gin.Context) {
		c.Request = withActorRequest(c.Request, callerID)
		h.UpdateUserStatus(c)
	})
	r.POST("/system/admin/users/:id/reset-password", func(c *gin.Context) {
		c.Request = withActorRequest(c.Request, callerID)
		h.AdminResetPassword(c)
	})
	return r
}

// withActorRequest attaches a user id to the request context under the
// canonical key so types.UserIDFromContext resolves the caller — the
// same wiring the gin auth middleware performs in production.
func withActorRequest(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), types.UserIDContextKey, userID)
	return req.WithContext(ctx)
}

func doUserAdminJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- ListUsers -------------------------------------------------------------

func TestListUsers_DefaultsAndSearch(t *testing.T) {
	called := struct{ search string }{}
	svc := &stubUserAdminService{
		listAll: func(_ context.Context, search string, offset, limit int) ([]*types.User, int64, error) {
			called.search = search
			if offset != 0 {
				t.Fatalf("default offset = %d, want 0", offset)
			}
			if limit != 50 {
				t.Fatalf("default limit = %d, want 50", limit)
			}
			return []*types.User{{ID: "u1", Username: "alice", Email: "alice@x.com"}}, 1, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")

	w := doUserAdminJSON(t, r, http.MethodGet, "/system/admin/users?search=ali", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if called.search != "ali" {
		t.Fatalf("search forwarded = %q, want %q", called.search, "ali")
	}
	var resp ListUsersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Users) != 1 {
		t.Fatalf("expected total=1 users=1, got total=%d users=%d", resp.Total, len(resp.Users))
	}
	if resp.Users[0].ID != "u1" {
		t.Fatalf("expected user u1, got %s", resp.Users[0].ID)
	}
}

func TestListUsers_LimitCappedAt200(t *testing.T) {
	var seenLimit int
	svc := &stubUserAdminService{
		listAll: func(_ context.Context, _ string, _ int, limit int) ([]*types.User, int64, error) {
			seenLimit = limit
			return nil, 0, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")

	doUserAdminJSON(t, r, http.MethodGet, "/system/admin/users?limit=9999", nil)
	if seenLimit != 200 {
		t.Fatalf("limit not capped, got %d want 200", seenLimit)
	}
}

func TestListUsers_ServiceError(t *testing.T) {
	svc := &stubUserAdminService{
		listAll: func(context.Context, string, int, int) ([]*types.User, int64, error) {
			return nil, 0, errors.New("db down")
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	w := doUserAdminJSON(t, r, http.MethodGet, "/system/admin/users", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// --- AdminCreateUser -------------------------------------------------------

func TestAdminCreateUser_Success(t *testing.T) {
	svc := &stubUserAdminService{
		create: func(_ context.Context, req *types.RegisterRequest) (*types.User, error) {
			if req.Email != "bob@x.com" || req.Password != "secret1" {
				t.Fatalf("unexpected req: %+v", req)
			}
			return &types.User{ID: "u2", Username: req.Username, Email: req.Email, IsActive: true}, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"username": "bob", "email": "bob@x.com", "password": "secret1"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var ui types.UserInfo
	if err := json.Unmarshal(w.Body.Bytes(), &ui); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ui.ID != "u2" || !ui.IsActive {
		t.Fatalf("unexpected user info: %+v", ui)
	}
}

func TestAdminCreateUser_ValidationRejectsShortPassword(t *testing.T) {
	svc := &stubUserAdminService{
		create: func(context.Context, *types.RegisterRequest) (*types.User, error) {
			t.Fatal("create must not be called on validation failure")
			return nil, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"username": "bob", "email": "bob@x.com", "password": "short"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", w.Code)
	}
}

func TestAdminCreateUser_DuplicateSurfacesAs400(t *testing.T) {
	svc := &stubUserAdminService{
		create: func(context.Context, *types.RegisterRequest) (*types.User, error) {
			return nil, errors.New("user with this email already exists")
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"username": "bob", "email": "dup@x.com", "password": "secret1"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate, got %d", w.Code)
	}
}

// --- UpdateUserStatus ------------------------------------------------------

func TestUpdateUserStatus_DisableOtherUser(t *testing.T) {
	var seenActive bool
	svc := &stubUserAdminService{
		setActive: func(_ context.Context, userID string, active bool) (*types.User, error) {
			if userID != "u-target" {
				t.Fatalf("userID = %q, want u-target", userID)
			}
			seenActive = active
			return &types.User{ID: userID, IsActive: active, Email: "t@x.com"}, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"is_active": false}
	w := doUserAdminJSON(t, r, http.MethodPut, "/system/admin/users/u-target/status", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if seenActive {
		t.Fatal("expected active=false forwarded to service")
	}
}

func TestUpdateUserStatus_CannotDisableSelf(t *testing.T) {
	svc := &stubUserAdminService{
		setActive: func(context.Context, string, bool) (*types.User, error) {
			t.Fatal("setActive must not be called when disabling self")
			return nil, nil
		},
	}
	// caller is the same user being targeted
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-self")
	body := map[string]any{"is_active": false}
	w := doUserAdminJSON(t, r, http.MethodPut, "/system/admin/users/admin-self/status", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-disable, got %d", w.Code)
	}
}

func TestUpdateUserStatus_CanEnableSelf(t *testing.T) {
	// Re-enabling yourself is harmless and allowed — the self-disable
	// guard only fires on is_active=false.
	svc := &stubUserAdminService{
		setActive: func(_ context.Context, _ string, active bool) (*types.User, error) {
			if !active {
				t.Fatal("enable-self must forward active=true")
			}
			return &types.User{ID: "admin-self", IsActive: true}, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-self")
	body := map[string]any{"is_active": true}
	w := doUserAdminJSON(t, r, http.MethodPut, "/system/admin/users/admin-self/status", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateUserStatus_NotFound(t *testing.T) {
	svc := &stubUserAdminService{
		setActive: func(context.Context, string, bool) (*types.User, error) {
			return nil, apprepo.ErrUserNotFound
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"is_active": false}
	w := doUserAdminJSON(t, r, http.MethodPut, "/system/admin/users/ghost/status", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- AdminResetPassword ----------------------------------------------------

func TestAdminResetPassword_Success(t *testing.T) {
	svc := &stubUserAdminService{
		getByID: func(_ context.Context, id string) (*types.User, error) {
			return &types.User{ID: id, Email: "t@x.com", Username: "t"}, nil
		},
		resetPW: func(_ context.Context, userID, pw string) error {
			if userID != "u-target" || pw != "brand-new-pw9" {
				t.Fatalf("resetPW args = (%q, %q)", userID, pw)
			}
			return nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"new_password": "brand-new-pw9"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users/u-target/reset-password", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminResetPassword_NotFound(t *testing.T) {
	svc := &stubUserAdminService{
		getByID: func(context.Context, string) (*types.User, error) {
			return nil, apprepo.ErrUserNotFound
		},
		resetPW: func(context.Context, string, string) error {
			t.Fatal("resetPW must not run when user is missing")
			return nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"new_password": "brand-new-pw9"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users/ghost/reset-password", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAdminResetPassword_ValidationRejectsShort(t *testing.T) {
	svc := &stubUserAdminService{
		getByID: func(context.Context, string) (*types.User, error) {
			t.Fatal("getByID must not be called on validation failure")
			return nil, nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"new_password": "short"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users/u-target/reset-password", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdminResetPassword_RejectsSelfReset(t *testing.T) {
	svc := &stubUserAdminService{
		getByID: func(_ context.Context, id string) (*types.User, error) {
			return &types.User{ID: id, Email: "admin@x.com", Username: "admin"}, nil
		},
		resetPW: func(context.Context, string, string) error {
			t.Fatal("resetPW must not run on self reset")
			return nil
		},
	}
	r := newSystemUserTestRouter(newSystemUserHandler(svc), "admin-1")
	body := map[string]any{"new_password": "brand-new-pw9"}
	w := doUserAdminJSON(t, r, http.MethodPost, "/system/admin/users/admin-1/reset-password", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
