package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddTagToKnowledgeBatch_AddsNewRelations(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	_, k1, tagA, tagB := seedKnowledgeTagFixture(t, db)
	k2 := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, parse_status)
		VALUES (?, 1, 'kb-1', 'file', 'd2', 'completed')
	`, k2).Error)

	err := repo.AddTagToKnowledgeBatch(ctx, []string{k1, k2}, []string{tagA, tagB})
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Table("knowledge_tag_relations").
		Where("knowledge_id IN ?", []string{k1, k2}).Count(&count).Error)
	assert.Equal(t, int64(4), count, "both docs should have both tags")
}

func TestAddTagToKnowledgeBatch_Idempotent(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	_, k1, tagA, _ := seedKnowledgeTagFixture(t, db)

	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, []string{k1}, []string{tagA}))
	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, []string{k1}, []string{tagA}))

	var count int64
	require.NoError(t, db.Table("knowledge_tag_relations").
		Where("knowledge_id = ? AND tag_id = ?", k1, tagA).Count(&count).Error)
	assert.Equal(t, int64(1), count, "duplicate add should not create extra rows")
}

func TestAddTagToKnowledgeBatch_DedupesInput(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	_, k1, tagA, _ := seedKnowledgeTagFixture(t, db)

	// Pass duplicate knowledge + tag IDs — should dedup internally
	err := repo.AddTagToKnowledgeBatch(ctx, []string{k1, k1}, []string{tagA, tagA})
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Table("knowledge_tag_relations").
		Where("knowledge_id = ? AND tag_id = ?", k1, tagA).Count(&count).Error)
	assert.Equal(t, int64(1), count, "deduped input should create exactly 1 row")
}

func TestAddTagToKnowledgeBatch_EmptyInputs(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, nil, []string{"tag-1"}))
	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, []string{"k-1"}, nil))
}

func TestRemoveTagFromKnowledgeBatch_RemovesSpecificTags(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	_, k1, tagA, tagB := seedKnowledgeTagFixture(t, db)
	k2 := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, parse_status)
		VALUES (?, 1, 'kb-1', 'file', 'd2', 'completed')
	`, k2).Error)

	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, []string{k1, k2}, []string{tagA, tagB}))
	require.NoError(t, repo.RemoveTagFromKnowledgeBatch(ctx, []string{k1, k2}, []string{tagA}))

	var countTagA int64
	require.NoError(t, db.Table("knowledge_tag_relations").Where("tag_id = ?", tagA).Count(&countTagA).Error)
	assert.Equal(t, int64(0), countTagA)

	var countTagB int64
	require.NoError(t, db.Table("knowledge_tag_relations").Where("tag_id = ?", tagB).Count(&countTagB).Error)
	assert.Equal(t, int64(2), countTagB)
}

func TestRemoveTagFromKnowledgeBatch_NoOpWhenNoMatch(t *testing.T) {
	db := setupKnowledgeTagTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()

	_, k1, tagA, _ := seedKnowledgeTagFixture(t, db)
	require.NoError(t, repo.AddTagToKnowledgeBatch(ctx, []string{k1}, []string{tagA}))

	tagB := uuid.New().String()
	require.NoError(t, repo.RemoveTagFromKnowledgeBatch(ctx, []string{k1}, []string{tagB}))

	var count int64
	require.NoError(t, db.Table("knowledge_tag_relations").Where("knowledge_id = ?", k1).Count(&count).Error)
	assert.Equal(t, int64(1), count, "tagA should still be there")
}

func TestCountKnowledgeByFolderIDs_Recursive(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{rootFolderA}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "recursive: FolderA + B = kA + kB")

	count, err = repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{rootFolderA, folderC}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "recursive: A+B+C = kA + kB + kC")
}

func TestCountKnowledgeByFolderIDs_NonRecursive(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{rootFolderA}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "non-recursive: only kA")

	count, err = repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{rootFolderA, folderC}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "non-recursive: kA + kC")
}

func TestCountKnowledgeByFolderIDs_RootFolder(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{"__root__"}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "root: kRoot")

	count, err = repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{"__root__", rootFolderA}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "root + A(recursive): kRoot + kA + kB")
}

func TestCountKnowledgeByFolderIDs_EmptyInput(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestCountKnowledgeByFolderIDs_UnknownFolderID(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{uuid.New().String()}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "unknown folder ID must return 0, not entire KB")
}

func TestCountKnowledgeByFolderIDs_OverlappingFolders(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, childFolderB, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, []string{rootFolderA, childFolderB}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "overlapping ancestor+descendant must not double-count")
}

func TestListAndCountByFolderIDs_Agree(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	folderIDs := []string{rootFolderA, folderC, "__root__"}

	ids, err := repo.ListKnowledgeIDsByFolderIDs(ctx, tenantID, kbID, folderIDs, true)
	require.NoError(t, err)

	count, err := repo.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, folderIDs, true)
	require.NoError(t, err)

	assert.Equal(t, int64(len(ids)), count, "count must match list length: resolveFolderScope guarantees identical scope")
}
