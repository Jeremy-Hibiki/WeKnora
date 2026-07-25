package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture layout (seedKnowledgeFolderFixture):
//
//	A/ (rootFolderA)   → kA "Knowledge in A"
//	  B/ (childFolderB) → kB "Knowledge in B"
//	C/ (folderC)       → kC "Knowledge in C"
//	root               → kRoot "Knowledge in Root"

func TestListMatchedFolderIDs_MatchesWithinScopeSubtree(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, childFolderB, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	// Keyword only matches kB, so only FolderB (inside A's subtree) is reported.
	matched, err := repo.ListMatchedFolderIDs(ctx, tenantID, kbID, types.KnowledgeListFilter{
		Keyword:       "Knowledge in B",
		FolderScopeID: rootFolderA,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{childFolderB}, matched)
}

func TestListMatchedFolderIDs_IncludesScopeFolderItself(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, childFolderB, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	// Keyword matches kA + kB (+ kC/kRoot, but those live outside A's subtree).
	matched, err := repo.ListMatchedFolderIDs(ctx, tenantID, kbID, types.KnowledgeListFilter{
		Keyword:       "Knowledge",
		FolderScopeID: rootFolderA,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{rootFolderA, childFolderB}, matched,
		"folders outside the scope subtree must be excluded even when their documents match")
}

func TestListMatchedFolderIDs_NoMatchInsideScope(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, rootFolderA, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	// kC matches the keyword but lives in FolderC, outside A's subtree.
	matched, err := repo.ListMatchedFolderIDs(ctx, tenantID, kbID, types.KnowledgeListFilter{
		Keyword:       "Knowledge in C",
		FolderScopeID: rootFolderA,
	})
	require.NoError(t, err)
	assert.Empty(t, matched)
}

func TestListMatchedFolderIDs_UnknownScopeFolder(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	matched, err := repo.ListMatchedFolderIDs(ctx, tenantID, kbID, types.KnowledgeListFilter{
		Keyword:       "Knowledge",
		FolderScopeID: uuid.New().String(),
	})
	require.NoError(t, err)
	assert.Empty(t, matched, "unknown scope folder must yield no matches, not the whole KB")
}

func TestListMatchedFolderIDs_NoScope(t *testing.T) {
	db := setupKnowledgeFolderTestDB(t)
	tenantID, kbID, _, _, _ := seedKnowledgeFolderFixture(t, db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()

	matched, err := repo.ListMatchedFolderIDs(ctx, tenantID, kbID, types.KnowledgeListFilter{
		Keyword: "Knowledge",
	})
	require.NoError(t, err)
	assert.Empty(t, matched, "without a folder scope there is no subtree to search")
}
