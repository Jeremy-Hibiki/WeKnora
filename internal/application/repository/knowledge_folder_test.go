package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const knowledgeFolderTestDDL = `
CREATE TABLE IF NOT EXISTS knowledge_folders (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_folder_id VARCHAR(36),
    path TEXT NOT NULL,
    depth INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER DEFAULT 0,
    color VARCHAR(32),
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);
`

func setupKnowledgeFolderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupKnowledgeTestDB(t)
	require.NoError(t, db.Exec(knowledgeFolderTestDDL).Error)
	return db
}

func seedKnowledgeFolderFixture(t *testing.T, db *gorm.DB) (tenantID uint64, kbID, rootFolderA, childFolderB, folderC string) {
	t.Helper()
	tenantID = 1
	kbID = uuid.New().String()
	rootFolderA = uuid.New().String()
	childFolderB = uuid.New().String()
	folderC = uuid.New().String()

	// Create folder structure:
	//   A/ (rootFolderA, path=/rootFolderA/)
	//     B/ (childFolderB, path=/rootFolderA/childFolderB/)
	//   C/ (folderC, path=/folderC/)
	require.NoError(t, db.Exec(`
		INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth)
		VALUES (?, ?, ?, 'FolderA', NULL, ?, 1)
	`, rootFolderA, tenantID, kbID, "/"+rootFolderA+"/").Error)

	require.NoError(t, db.Exec(`
		INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth)
		VALUES (?, ?, ?, 'FolderB', ?, ?, 2)
	`, childFolderB, tenantID, kbID, rootFolderA, "/"+rootFolderA+"/"+childFolderB+"/").Error)

	require.NoError(t, db.Exec(`
		INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth)
		VALUES (?, ?, ?, 'FolderC', NULL, ?, 1)
	`, folderC, tenantID, kbID, "/"+folderC+"/").Error)

	// Create knowledge entries
	// kA: in FolderA
	// kB: in FolderB (child of A)
	// kC: in FolderC
	// kRoot: in root (folder_id IS NULL)
	kA := uuid.New().String()
	kB := uuid.New().String()
	kC := uuid.New().String()
	kRoot := uuid.New().String()

	for _, entry := range []struct {
		id       string
		folderID string
		title    string
	}{
		{kA, rootFolderA, "Knowledge in A"},
		{kB, childFolderB, "Knowledge in B"},
		{kC, folderC, "Knowledge in C"},
		{kRoot, "", "Knowledge in Root"},
	} {
		query := `INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, source, parse_status, folder_id)
			VALUES (?, ?, ?, 'document', ?, 'manual', 'completed', ?)`
		folderID := interface{}(nil)
		if entry.folderID != "" {
			folderID = entry.folderID
		}
		require.NoError(t, db.Exec(query, entry.id, tenantID, kbID, entry.title, folderID).Error)
	}

	return
}

func TestListKnowledgeIDsByFolderIDs_NonRecursive(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// Non-recursive: should return knowledge directly in FolderA (kA) + FolderC (kC), not in subfolder B
	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{rootFolderA, folderC}, false)
	require.NoError(t, err)
	assert.Len(t, ids, 2, "non-recursive should return only direct children of FolderA and FolderC")
}

func TestListKnowledgeIDsByFolderIDs_Recursive(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// Recursive: should return kA + kB (FolderA and its descendant FolderB) + kC (FolderC) = 3 total
	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{rootFolderA, folderC}, true)
	require.NoError(t, err)
	assert.Len(t, ids, 3, "recursive should return knowledge from FolderA, all its descendants, and FolderC")
}

func TestListKnowledgeIDsByFolderIDs_RootFolder(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// "__root__" should return knowledge with folder_id IS NULL
	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{"__root__"}, false)
	require.NoError(t, err)
	assert.Len(t, ids, 1, "root folder should return knowledge with folder_id IS NULL")
}

func TestListKnowledgeIDsByFolderIDs_MultipleFolders(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// Multiple folders: should return kA + kC
	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{rootFolderA, folderC}, false)
	require.NoError(t, err)
	assert.Len(t, ids, 2, "should return knowledge from both FolderA and FolderC")
}

func TestListKnowledgeIDsByFolderIDs_RootPlusFolder(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// "__root__" + FolderA: should return root knowledge + kA
	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{"__root__", rootFolderA}, false)
	require.NoError(t, err)
	assert.Len(t, ids, 2, "should return root knowledge + FolderA knowledge")
}

func TestListKnowledgeIDsByFolderIDs_EmptyInput(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{}, false)
	require.NoError(t, err)
	assert.Nil(t, ids, "empty folder IDs should return nil")
}

func TestListKnowledgeIDsByFolderIDs_UnknownFolder(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ListKnowledgeIDsByFolderIDs(context.Background(), tenantID, kbID, []string{uuid.New().String()}, false)
	require.NoError(t, err)
	assert.Len(t, ids, 0, "unknown folder ID should return empty list")
}

// --- New tests for tx-safe folder operations (feature/multi-folder-upload) ---

// TestMoveSubtree_RepDescendantsRealDB verifies the SUBSTR/OVERLAY prefix
// rewrite actually works on a real SQLite DB (the fake repo in service tests
// bypasses the SQL entirely, so this is the only test that catches a malformed
// path expression).
func TestMoveSubtree_RepDescendantsRealDB(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID := uint64(1)
	kbID := uuid.New().String()
	repo := NewKnowledgeFolderRepository(db).(*knowledgeFolderRepository)

	src := uuid.New().String()
	child := uuid.New().String()
	grand := uuid.New().String()
	dest := uuid.New().String()
	for _, f := range []struct {
		id, name, parentID, path string
		depth                    int
	}{
		{src, "src", "", "/" + src + "/", 1},
		{child, "child", src, "/" + src + "/" + child + "/", 2},
		{grand, "grand", child, "/" + src + "/" + child + "/" + grand + "/", 3},
		{dest, "dest", "", "/" + dest + "/", 1},
	} {
		pid := interface{}(nil)
		if f.parentID != "" {
			pid = f.parentID
		}
		require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.id, tenantID, kbID, f.name, pid, f.path, f.depth).Error)
	}

	newPath := "/" + dest + "/" + src + "/"
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.MoveSubtree(context.Background(), tx, tenantID, src, &dest, newPath, 2, "/"+src+"/", 1)
	})
	require.NoError(t, err)

	var got map[string]struct {
		Path  string
		Depth int
	}
	rows, err := db.Raw(`SELECT id, path, depth FROM knowledge_folders WHERE id IN (?, ?, ?)`, src, child, grand).Rows()
	require.NoError(t, err)
	defer rows.Close()
	got = make(map[string]struct {
		Path  string
		Depth int
	})
	for rows.Next() {
		var id, p string
		var d int
		require.NoError(t, rows.Scan(&id, &p, &d))
		got[id] = struct {
			Path  string
			Depth int
		}{p, d}
	}
	assert.Equal(t, "/"+dest+"/"+src+"/", got[src].Path)
	assert.Equal(t, 2, got[src].Depth)
	assert.Equal(t, "/"+dest+"/"+src+"/"+child+"/", got[child].Path)
	assert.Equal(t, 3, got[child].Depth)
	assert.Equal(t, "/"+dest+"/"+src+"/"+child+"/"+grand+"/", got[grand].Path)
	assert.Equal(t, 4, got[grand].Depth)
}

// TestGetDescendants_TenantScoped verifies the tenant_id + knowledge_base_id
// scope on the path LIKE query: a folder with an identical path prefix in a
// different tenant/kb must NOT appear in the descendants list.
func TestGetDescendants_TenantScoped(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	kbID := uuid.New().String()
	repo := NewKnowledgeFolderRepository(db)

	// Tenant 1, kb-1 tree: root → child.
	root := uuid.New().String()
	child := uuid.New().String()
	require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, 1, ?, 'root', NULL, ?, 1)`,
		root, kbID, "/"+root+"/").Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, 1, ?, 'child', ?, ?, 2)`,
		child, kbID, root, "/"+root+"/"+child+"/").Error)

	// Tenant 2, SAME kbID, a folder whose path collides with tenant-1's root
	// prefix — must be excluded by the tenant scope.
	intruder := uuid.New().String()
	require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, 2, ?, 'intruder', NULL, ?, 1)`,
		intruder, kbID, "/"+root+"/"+intruder+"/").Error)

	desc, err := repo.GetDescendants(context.Background(), 1, root)
	require.NoError(t, err)
	ids := make([]string, 0, len(desc))
	for _, d := range desc {
		ids = append(ids, d.ID)
	}
	assert.Contains(t, ids, child)
	assert.NotContains(t, ids, intruder, "descendants must be scoped to tenant_id")
}

// TestForceDeleteSubtree_Cascades verifies that force-deleting a folder removes
// all descendants and unlinks the knowledge entries that lived under them.
func TestForceDeleteSubtree_Cascades(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID := uint64(1)
	kbID := uuid.New().String()
	repo := NewKnowledgeFolderRepository(db)

	root := uuid.New().String()
	sub := uuid.New().String()
	require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, ?, ?, 'root', NULL, ?, 1)`,
		root, tenantID, kbID, "/"+root+"/").Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth) VALUES (?, ?, ?, 'sub', ?, ?, 2)`,
		sub, tenantID, kbID, root, "/"+root+"/"+sub+"/").Error)

	// Knowledge in both folders.
	kRoot := uuid.New().String()
	kSub := uuid.New().String()
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, source, parse_status, folder_id) VALUES (?, ?, ?, 'document', 'kr', 'manual', 'completed', ?)`,
		kRoot, tenantID, kbID, root).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, source, parse_status, folder_id) VALUES (?, ?, ?, 'document', 'ks', 'manual', 'completed', ?)`,
		kSub, tenantID, kbID, sub).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.ForceDeleteSubtree(context.Background(), tx, tenantID, root)
	})
	require.NoError(t, err)

	// Both folders soft-deleted (gorm soft-delete sets deleted_at).
	var folderCount int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM knowledge_folders WHERE id IN (?, ?) AND deleted_at IS NULL`, root, sub).Scan(&folderCount).Error)
	assert.Equal(t, int64(0), folderCount, "both folders must be soft-deleted")

	// Knowledge entries still exist but folder_id unlinked (SET NULL behavior).
	rows, err := db.Raw(`SELECT folder_id FROM knowledges WHERE id IN (?, ?)`, kRoot, kSub).Rows()
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var fid interface{}
		require.NoError(t, rows.Scan(&fid))
		assert.Nil(t, fid, "knowledge folder_id must be unlinked after subtree delete")
	}
}

// TestEnsureFolderPath_RealDBAtomic verifies the idempotent folder-tree creation
// against a real SQLite DB (catches issues the fake repo cannot, e.g. transaction
// visibility or SQL errors in CreateInTx / GetChildByNameInTx).
func TestEnsureFolderPath_RealDBAtomic(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	// EnsureFolderPath lives on the service; we test the repo primitives it
	// composes: GetChildByNameInTx + CreateInTx inside a transaction.
	repo := NewKnowledgeFolderRepository(db)
	ctx := context.Background()
	tenantID := uint64(1)
	kbID := uuid.New().String()

	var leafID string
	err := db.Transaction(func(tx *gorm.DB) error {
		// First call: "docs" does not exist → create it.
		existing, err := repo.GetChildByNameInTx(ctx, tx, tenantID, kbID, nil, "docs")
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if existing != nil {
			return nil
		}
		f := newFolder(tenantID, kbID, "docs", nil)
		if err := repo.CreateInTx(ctx, tx, f); err != nil {
			return err
		}
		leafID = f.ID
		return nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, leafID)

	// Second transaction: "docs" now exists → reuse, do not create a duplicate.
	err = db.Transaction(func(tx *gorm.DB) error {
		existing, err := repo.GetChildByNameInTx(ctx, tx, tenantID, kbID, nil, "docs")
		if err != nil {
			return err
		}
		assert.Equal(t, leafID, existing.ID, "must reuse the existing folder")
		return nil
	})
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM knowledge_folders WHERE name = 'docs'`).Scan(&count).Error)
	assert.Equal(t, int64(1), count, "exactly one 'docs' folder must exist")
}

// --- ListFolderIDsWithDescendants tests ---

func TestListFolderIDsWithDescendants_NoDescendants(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// folderC has no children — should return just [folderC].
	ids, err := repo.ListFolderIDsWithDescendants(context.Background(), tenantID, kbID, []string{folderC})
	require.NoError(t, err)
	assert.Equal(t, []string{folderC}, ids)
}

func TestListFolderIDsWithDescendants_IncludesDescendants(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, childFolderB, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// rootFolderA has child childFolderB — should return [rootFolderA, childFolderB].
	ids, err := repo.ListFolderIDsWithDescendants(context.Background(), tenantID, kbID, []string{rootFolderA})
	require.NoError(t, err)
	assert.Contains(t, ids, rootFolderA)
	assert.Contains(t, ids, childFolderB)
	assert.Len(t, ids, 2)
}

func TestListFolderIDsWithDescendants_RootToken(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// "__root__" is a sentinel, not a real folder row — must pass through as-is.
	ids, err := repo.ListFolderIDsWithDescendants(context.Background(), tenantID, kbID, []string{"__root__"})
	require.NoError(t, err)
	assert.Equal(t, []string{"__root__"}, ids)
}

func TestListFolderIDsWithDescendants_NonExistentSkipped(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// Non-existent folder ID should be silently skipped; valid folderC is kept.
	ids, err := repo.ListFolderIDsWithDescendants(context.Background(), tenantID, kbID, []string{uuid.New().String(), folderC})
	require.NoError(t, err)
	assert.Equal(t, []string{folderC}, ids, "non-existent ID must be skipped, valid ID must remain")
}

func TestListFolderIDsWithDescendants_EmptyInput(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ListFolderIDsWithDescendants(context.Background(), tenantID, kbID, []string{})
	require.NoError(t, err)
	assert.Nil(t, ids, "empty input should return nil")
}

// helper to build a minimal KnowledgeFolder for repo tests.
func newFolder(tenantID uint64, kbID, name string, parentID *string) *types.KnowledgeFolder {
	f := &types.KnowledgeFolder{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		Name:            name,
		ParentFolderID:  parentID,
		Depth:           1,
	}
	f.ID = uuid.New().String()
	if parentID != nil {
		f.Path = "/" + *parentID + "/" + f.ID + "/"
		f.Depth = 2
	} else {
		f.Path = "/" + f.ID + "/"
	}
	return f
}

// --- ResolveFolderNames tests ---

func TestResolveFolderNames_ExactMatch(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, []string{"FolderA"})
	require.NoError(t, err)
	assert.Contains(t, ids, rootFolderA)
}

func TestResolveFolderNames_CaseInsensitive(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	tests := []struct {
		name      string
		input     []string
		expectIDs []string
	}{
		{"lowercase", []string{"foldera"}, []string{rootFolderA}},
		{"uppercase", []string{"FOLDERC"}, []string{folderC}},
		{"mixed case", []string{"fOlDeRa"}, []string{rootFolderA}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expectIDs, ids)
		})
	}
}

func TestResolveFolderNames_NoMatch(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, []string{"NonExistent"})
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestResolveFolderNames_MultipleFoldersSameName(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// Insert a second folder named "FolderA" in the same KB (different parent).
	dupA := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO knowledge_folders (id, tenant_id, knowledge_base_id, name, parent_folder_id, path, depth)
		VALUES (?, ?, ?, 'FolderA', ?, ?, 2)
	`, dupA, tenantID, kbID, rootFolderA, "/"+rootFolderA+"/"+dupA+"/").Error)

	ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, []string{"FolderA"})
	require.NoError(t, err)
	assert.Len(t, ids, 2, "both folders named FolderA must be returned")
	assert.Contains(t, ids, rootFolderA)
	assert.Contains(t, ids, dupA)
}

func TestResolveFolderNames_MultipleNames(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, folderC := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, []string{"FolderA", "FolderC"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{rootFolderA, folderC}, ids)
}

func TestResolveFolderNames_EmptyInput(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	ids, err := repo.ResolveFolderNames(context.Background(), tenantID, kbID, []string{})
	require.NoError(t, err)
	assert.Nil(t, ids)
}

func TestResolveFolderNames_TenantScoped(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	_, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)

	// A different tenant should not see tenant-1's folders.
	ids, err := repo.ResolveFolderNames(context.Background(), 999, kbID, []string{"FolderA"})
	require.NoError(t, err)
	assert.Empty(t, ids, "folder resolution must be scoped to tenant")

	// Sanity: tenant 1 does see it.
	ids, err = repo.ResolveFolderNames(context.Background(), 1, kbID, []string{"FolderA"})
	require.NoError(t, err)
	assert.Contains(t, ids, rootFolderA)
}
