package service

import (
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessSyncCancelsWhenKnowledgeBaseDeleted(t *testing.T) {
	ds := &types.DataSource{
		ID:              "ds-1",
		TenantID:        1,
		KnowledgeBaseID: "kb-deleted",
		Type:            types.ConnectorTypeRSS,
		Status:          types.DataSourceStatusActive,
	}
	dsRepo := newKBDeleteDSRepo("kb-deleted", ds)
	syncLog := &types.SyncLog{
		ID:           "log-1",
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	syncLogRepo := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{syncLog.ID: syncLog}}

	svc := &DataSourceService{
		dsRepo:      dsRepo,
		syncLogRepo: syncLogRepo,
		kbService:   &processSyncKBService{getErr: apprepo.ErrKnowledgeBaseNotFound},
	}

	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		SyncLogID:    syncLog.ID,
	})
	require.NoError(t, err)

	err = svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))
	require.NoError(t, err)

	updated := syncLogRepo.logs[syncLog.ID]
	require.NotNil(t, updated)
	assert.Equal(t, types.SyncLogStatusCanceled, updated.Status)
	assert.Equal(t, "knowledge base has been deleted", updated.ErrorMessage)
	require.NotNil(t, updated.FinishedAt)
}

type processSyncKBService struct {
	getErr error
}

func (s *processSyncKBService) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, s.getErr
}
func (s *processSyncKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, s.getErr
}
func (s *processSyncKBService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}
func (s *processSyncKBService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) UpdateKnowledgeBase(
	context.Context, string, string, string, *types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) DeleteKnowledgeBase(context.Context, string) error { return nil }
func (s *processSyncKBService) TogglePinKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return nil, nil
}
func (s *processSyncKBService) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, nil
}
func (s *processSyncKBService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return nil
}
func (s *processSyncKBService) CopyKnowledgeBase(context.Context, string, string) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	return nil, nil, nil
}
func (s *processSyncKBService) DuplicateKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) GetRepository() interfaces.KnowledgeBaseRepository { return nil }
func (s *processSyncKBService) ProcessKBDelete(context.Context, *asynq.Task) error {
	return nil
}

var _ interfaces.KnowledgeBaseService = (*processSyncKBService)(nil)

type processSyncSyncLogRepo struct {
	logs map[string]*types.SyncLog
}

func (r *processSyncSyncLogRepo) Create(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}
func (r *processSyncSyncLogRepo) FindByID(_ context.Context, id string) (*types.SyncLog, error) {
	log, ok := r.logs[id]
	if !ok {
		return nil, errors.New("sync log not found")
	}
	return log, nil
}
func (r *processSyncSyncLogRepo) FindByDataSource(context.Context, string, int, int) ([]*types.SyncLog, error) {
	return nil, nil
}
func (r *processSyncSyncLogRepo) FindLatest(context.Context, string) (*types.SyncLog, error) {
	return nil, nil
}
func (r *processSyncSyncLogRepo) HasRunningSync(context.Context, string) (bool, error) {
	return false, nil
}
func (r *processSyncSyncLogRepo) Update(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}
func (r *processSyncSyncLogRepo) UpdateResult(_ context.Context, log *types.SyncLog) error {
	return r.Update(context.Background(), log)
}
func (r *processSyncSyncLogRepo) CancelPendingByDataSource(context.Context, string) error {
	return nil
}
func (r *processSyncSyncLogRepo) CleanupOldLogs(context.Context, int) error { return nil }

func TestAllFetchedItemsFailedError(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  2,
		Failed: 2,
		Errors: []types.SyncItemError{{Message: "doc one: export failed"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all fetched items failed during sync (2/2)")
	assert.Contains(t, err.Error(), "doc one: export failed")
}

func TestAllFetchedItemsFailedErrorIgnoresPartialFailure(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Created: 1,
		Failed:  2,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorIgnoresSkippedItems(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Skipped: 3,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorTruncatesLongDetail(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  1,
		Failed: 1,
		Errors: []types.SyncItemError{{Message: strings.Repeat("x", 600)}},
	})
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 560)
	assert.Contains(t, err.Error(), "...")
}

// --- ingestItem folder-resolution tests ---
//
// The fakes below embed the large service interfaces (KnowledgeService has
// 100+ methods) so the struct satisfies each interface by promotion; only the
// handful of methods that ingestItem actually calls are overridden. Calling any
// other method would panic, but the tests never exercise them.

type ingestFakeKGService struct {
	interfaces.KnowledgeService
	repo            interfaces.KnowledgeRepository
	fileFolderID    *string
	urlFolderID     *string
	fileCallCount   int
	urlCallCount    int
	deleteCallCount int
}

func (f *ingestFakeKGService) GetRepository() interfaces.KnowledgeRepository {
	return f.repo
}

func (f *ingestFakeKGService) CreateKnowledgeFromFile(
	_ context.Context, _ string, _ *multipart.FileHeader, _ map[string]string,
	_ *bool, _ string, _ []string, _ string, _ *types.KnowledgeProcessOverrides, folderID *string,
) (*types.Knowledge, error) {
	f.fileCallCount++
	f.fileFolderID = folderID
	return &types.Knowledge{ID: "kg-new"}, nil
}

func (f *ingestFakeKGService) CreateKnowledgeFromURL(
	_ context.Context, _ string, _ string, _ string, _ string, _ *bool, _ string,
	_ []string, _ string, _ *types.KnowledgeProcessOverrides, folderID *string,
) (*types.Knowledge, error) {
	f.urlCallCount++
	f.urlFolderID = folderID
	return &types.Knowledge{ID: "kg-new"}, nil
}

func (f *ingestFakeKGService) DeleteKnowledge(_ context.Context, _ string) error {
	f.deleteCallCount++
	return nil
}

type ingestFakeKGRepo struct {
	interfaces.KnowledgeRepository
	existing *types.Knowledge
	findErr  error
}

func (r *ingestFakeKGRepo) FindByMetadataKey(
	_ context.Context, _ uint64, _ string, _ string, _ string,
) (*types.Knowledge, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.existing, nil
}

type ingestFakeFolderService struct {
	interfaces.KnowledgeFolderService
	ensureParent *string
	ensurePath   string
	ensureCalls  int
	leaf         *types.KnowledgeFolder
	ensureErr    error
}

func (f *ingestFakeFolderService) EnsureFolderPath(
	_ context.Context, _ string, parent *string, rel string,
) (*types.KnowledgeFolder, error) {
	f.ensureCalls++
	f.ensurePath = rel
	f.ensureParent = parent
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	return f.leaf, nil
}

func newIngestItemService(kg *ingestFakeKGService, folder *ingestFakeFolderService) *DataSourceService {
	return &DataSourceService{
		knowledgeService: kg,
		folderService:    folder,
	}
}

func TestIngestItem_ResolvesFolderPathToFileID(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeSVN}

	leafFolder := &types.KnowledgeFolder{ID: "folder-leaf"}
	folderSvc := &ingestFakeFolderService{leaf: leafFolder}
	kgSvc := &ingestFakeKGService{repo: &ingestFakeKGRepo{}}
	svc := newIngestItemService(kgSvc, folderSvc)

	item := &types.FetchedItem{
		ExternalID: "/docs/api/readme.md",
		Title:      "readme.md",
		FileName:   "readme.md",
		Content:    []byte("# hello"),
		FolderPath: "docs/api",
	}

	_, err := svc.ingestItem(ctx, ds, item, nil)
	require.NoError(t, err)

	// Folder tree resolved from the relative path, rooted at the KB root.
	assert.Equal(t, 1, folderSvc.ensureCalls)
	assert.Equal(t, "docs/api", folderSvc.ensurePath)
	assert.Nil(t, folderSvc.ensureParent)
	// The leaf folder ID is threaded through to the create call.
	assert.Equal(t, 1, kgSvc.fileCallCount)
	require.NotNil(t, kgSvc.fileFolderID)
	assert.Equal(t, "folder-leaf", *kgSvc.fileFolderID)
	assert.Equal(t, 0, kgSvc.urlCallCount)
}

func TestIngestItem_EmptyFolderPathGoesToRoot(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeSVN}

	folderSvc := &ingestFakeFolderService{}
	kgSvc := &ingestFakeKGService{repo: &ingestFakeKGRepo{}}
	svc := newIngestItemService(kgSvc, folderSvc)

	item := &types.FetchedItem{
		ExternalID: "/readme.md",
		Title:      "readme.md",
		FileName:   "readme.md",
		Content:    []byte("# hello"),
	}

	_, err := svc.ingestItem(ctx, ds, item, nil)
	require.NoError(t, err)

	// No folder resolution performed; create receives a nil folderID (KB root).
	assert.Equal(t, 0, folderSvc.ensureCalls)
	assert.Equal(t, 1, kgSvc.fileCallCount)
	assert.Nil(t, kgSvc.fileFolderID)
}

func TestIngestItem_FolderPathErrorFallsBackToRoot(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeSVN}

	folderSvc := &ingestFakeFolderService{ensureErr: errors.New("max folder depth exceeded")}
	kgSvc := &ingestFakeKGService{repo: &ingestFakeKGRepo{}}
	svc := newIngestItemService(kgSvc, folderSvc)

	item := &types.FetchedItem{
		ExternalID: "/a/b/c/d/e/f/g/h/i/j/k.md",
		Title:      "k.md",
		FileName:   "k.md",
		Content:    []byte("# hello"),
		FolderPath: "a/b/c/d/e/f/g/h/i/j/k",
	}

	// Folder creation failure degrades to the KB root rather than failing the file.
	_, err := svc.ingestItem(ctx, ds, item, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, folderSvc.ensureCalls)
	assert.Equal(t, 1, kgSvc.fileCallCount)
	assert.Nil(t, kgSvc.fileFolderID)
}

func TestIngestItem_URLItemThreadsFolderID(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeSVN}

	leafFolder := &types.KnowledgeFolder{ID: "folder-leaf"}
	folderSvc := &ingestFakeFolderService{leaf: leafFolder}
	kgSvc := &ingestFakeKGService{repo: &ingestFakeKGRepo{}}
	svc := newIngestItemService(kgSvc, folderSvc)

	// URL-only item (no Content bytes) takes the CreateKnowledgeFromURL branch.
	item := &types.FetchedItem{
		ExternalID: "/docs/api/guide.md",
		Title:      "guide.md",
		FileName:   "guide.md",
		URL:        "https://svn.example.com/repo/docs/api/guide.md",
		FolderPath: "docs/api",
	}

	_, err := svc.ingestItem(ctx, ds, item, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, folderSvc.ensureCalls)
	assert.Equal(t, "docs/api", folderSvc.ensurePath)
	assert.Equal(t, 0, kgSvc.fileCallCount)
	assert.Equal(t, 1, kgSvc.urlCallCount)
	require.NotNil(t, kgSvc.urlFolderID)
	assert.Equal(t, "folder-leaf", *kgSvc.urlFolderID)
}
