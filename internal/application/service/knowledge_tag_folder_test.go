package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes ---

type tagByFolderFakeRepo struct {
	interfaces.KnowledgeRepository
	listFn          func(ctx context.Context, tenantID uint64, kbID string, folderIDs []string, recursive bool) ([]string, error)
	addTagFn        func(ctx context.Context, knowledgeIDs []string, tagIDs []string) error
	removeTagFn     func(ctx context.Context, knowledgeIDs []string, tagIDs []string) error
	addTagCalled    bool
	removeTagCalled bool
}

func (r *tagByFolderFakeRepo) ListKnowledgeIDsByFolderIDs(ctx context.Context, tenantID uint64, kbID string, folderIDs []string, recursive bool) ([]string, error) {
	return r.listFn(ctx, tenantID, kbID, folderIDs, recursive)
}

func (r *tagByFolderFakeRepo) AddTagToKnowledgeBatch(ctx context.Context, knowledgeIDs []string, tagIDs []string) error {
	r.addTagCalled = true
	if r.addTagFn != nil {
		return r.addTagFn(ctx, knowledgeIDs, tagIDs)
	}
	return nil
}

func (r *tagByFolderFakeRepo) RemoveTagFromKnowledgeBatch(ctx context.Context, knowledgeIDs []string, tagIDs []string) error {
	r.removeTagCalled = true
	if r.removeTagFn != nil {
		return r.removeTagFn(ctx, knowledgeIDs, tagIDs)
	}
	return nil
}

func (r *tagByFolderFakeRepo) CountKnowledgeByFolderIDs(_ context.Context, _ uint64, _ string, _ []string, _ bool) (int64, error) {
	return 42, nil
}

type tagByFolderFakeKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *tagByFolderFakeKBService) GetKnowledgeBaseByID(_ context.Context, _ string) (*types.KnowledgeBase, error) {
	if s.kb == nil {
		return nil, errors.New("KB not found")
	}
	return s.kb, nil
}

type tagByFolderFakeTagRepo struct {
	interfaces.KnowledgeTagRepository
	validateErr error
}

func (r *tagByFolderFakeTagRepo) GetByIDs(_ context.Context, _ uint64, tagIDs []string) ([]*types.KnowledgeTag, error) {
	if r.validateErr != nil {
		return nil, r.validateErr
	}
	tags := make([]*types.KnowledgeTag, 0, len(tagIDs))
	for _, id := range tagIDs {
		tags = append(tags, &types.KnowledgeTag{ID: id, KnowledgeBaseID: "kb-1"})
	}
	return tags, nil
}

// --- Tests ---

func TestTagByFolder_AddSuccess(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{
		listFn: func(_ context.Context, _ uint64, _ string, _ []string, _ bool) ([]string, error) {
			return []string{"k1", "k2", "k3"}, nil
		},
	}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	affected, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "add", true)
	require.NoError(t, err)
	assert.Equal(t, int64(3), affected, "affected_count = scope size")
	assert.True(t, fakeRepo.addTagCalled)
	assert.False(t, fakeRepo.removeTagCalled)
}

func TestTagByFolder_RemoveSuccess(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{
		listFn: func(_ context.Context, _ uint64, _ string, _ []string, _ bool) ([]string, error) {
			return []string{"k1", "k2"}, nil
		},
	}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	affected, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "remove", true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected)
	assert.True(t, fakeRepo.removeTagCalled)
	assert.False(t, fakeRepo.addTagCalled)
}

func TestTagByFolder_RejectsFAQKB(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeFAQ}
	fakeRepo := &tagByFolderFakeRepo{}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "add", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "document knowledge bases")
	assert.False(t, fakeRepo.addTagCalled, "no tag write when KB type guard fails")
}

func TestTagByFolder_RejectsInvalidAction(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{
		listFn: func(_ context.Context, _ uint64, _ string, _ []string, _ bool) ([]string, error) {
			return []string{"k1"}, nil
		},
	}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "invalid", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be")
}

func TestTagByFolder_EmptyScope_ReturnsZero(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{
		listFn: func(_ context.Context, _ uint64, _ string, _ []string, _ bool) ([]string, error) {
			return nil, nil
		},
	}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	affected, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "add", true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
	assert.False(t, fakeRepo.addTagCalled, "no tag write when scope is empty")
}

func TestTagByFolder_AffectedCountIsScopeSize(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{
		listFn: func(_ context.Context, _ uint64, _ string, _ []string, _ bool) ([]string, error) {
			// 5 docs in scope; some may already have the tag (ON CONFLICT skips them)
			return []string{"k1", "k2", "k3", "k4", "k5"}, nil
		},
	}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	affected, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "add", true)
	require.NoError(t, err)
	assert.Equal(t, int64(5), affected, "affected_count is scope size, not mutation count")
}

func TestTagByFolder_TagValidationFails(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", Type: types.KnowledgeBaseTypeDocument}
	fakeRepo := &tagByFolderFakeRepo{}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: kb},
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{validateErr: errors.New("tag not found in KB")},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"bad-tag"}, "add", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag not found")
	assert.False(t, fakeRepo.addTagCalled, "no tag write when validation fails")
}

func TestTagByFolder_KBNotFound(t *testing.T) {
	fakeRepo := &tagByFolderFakeRepo{}
	svc := &knowledgeService{
		kbService: &tagByFolderFakeKBService{kb: nil}, // KB not found
		repo:      fakeRepo,
		tagRepo:   &tagByFolderFakeTagRepo{},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, err := svc.TagByFolder(ctx, "kb-1", []string{"f1"}, []string{"t1"}, "add", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KB not found")
	assert.False(t, fakeRepo.addTagCalled)
}

func TestCountKnowledgeByFolderIDs_DelegatesToRepo(t *testing.T) {
	fakeRepo := &tagByFolderFakeRepo{}
	svc := &knowledgeService{repo: fakeRepo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	count, err := svc.CountKnowledgeByFolderIDs(ctx, 1, "kb-1", []string{"f1"}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}
