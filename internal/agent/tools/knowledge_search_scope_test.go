package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKSForScope embeds interfaces.KnowledgeService so only the methods
// needed by resolveFolderTagScope need to be overridden.
type fakeKSForScope struct {
	interfaces.KnowledgeService
	resolveFolderNamesFn func(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error)
	resolveTagNamesFn    func(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error)
}

func (f *fakeKSForScope) ResolveFolderNames(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error) {
	return f.resolveFolderNamesFn(ctx, tenantID, kbID, names)
}

func (f *fakeKSForScope) ResolveTagNames(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error) {
	return f.resolveTagNamesFn(ctx, tenantID, kbID, names)
}

func ctxWithTenant(tenantID uint64) context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
}

// --- KnowledgeSearchInput deserialization ---

func TestKnowledgeSearchInput_DeserializeFoldersTagsSubfolders(t *testing.T) {
	raw := `{
		"queries": ["What is RAG?"],
		"folders": ["Go", "API"],
		"tags": ["important"],
		"include_subfolders": false
	}`

	var input KnowledgeSearchInput
	err := json.Unmarshal([]byte(raw), &input)
	require.NoError(t, err)

	assert.Equal(t, []string{"What is RAG?"}, input.Queries)
	assert.Equal(t, []string{"Go", "API"}, input.Folders)
	assert.Equal(t, []string{"important"}, input.Tags)
	require.NotNil(t, input.IncludeSubfolders)
	assert.False(t, *input.IncludeSubfolders)
}

func TestKnowledgeSearchInput_DeserializeOmitsOptionalFields(t *testing.T) {
	raw := `{"queries": ["test query"]}`

	var input KnowledgeSearchInput
	err := json.Unmarshal([]byte(raw), &input)
	require.NoError(t, err)

	assert.Equal(t, []string{"test query"}, input.Queries)
	assert.Nil(t, input.Folders, "folders should be nil when not provided")
	assert.Nil(t, input.Tags, "tags should be nil when not provided")
	assert.Nil(t, input.IncludeSubfolders, "include_subfolders should be nil when not provided")
}

func TestKnowledgeSearchInput_DeserializeIncludeSubfoldersDefault(t *testing.T) {
	raw := `{"queries": ["q"], "folders": ["X"], "include_subfolders": true}`

	var input KnowledgeSearchInput
	err := json.Unmarshal([]byte(raw), &input)
	require.NoError(t, err)

	require.NotNil(t, input.IncludeSubfolders)
	assert.True(t, *input.IncludeSubfolders)
}

// --- resolveFolderTagScope tests ---

func TestResolveFolderTagScope_FolderResolutionSetsFolderIDs(t *testing.T) {
	kbID := "kb-1"
	folderIDA := "folder-aaa"
	folderIDB := "folder-bbb"

	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{
			resolveFolderNamesFn: func(_ context.Context, _ uint64, _ string, names []string) ([]string, error) {
				return []string{folderIDA, folderIDB}, nil
			},
			resolveTagNamesFn: func(_ context.Context, _ uint64, _ string, _ []string) ([]string, error) {
				return nil, nil
			},
		},
	}

	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: 1},
	}
	includeSubfolders := false
	input := KnowledgeSearchInput{
		Folders:           []string{"Go", "API"},
		IncludeSubfolders: &includeSubfolders,
	}

	tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kbID}, targets)

	require.Len(t, targets, 1)
	assert.ElementsMatch(t, []string{folderIDA, folderIDB}, targets[0].FolderIDs)
	assert.False(t, targets[0].IncludeSubfolders, "IncludeSubfolders must reflect the input value")
}

func TestResolveFolderTagScope_TagResolutionSetsTagIDs(t *testing.T) {
	kbID := "kb-1"
	tagID := "tag-xyz"

	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{
			resolveFolderNamesFn: func(_ context.Context, _ uint64, _ string, _ []string) ([]string, error) {
				return nil, nil
			},
			resolveTagNamesFn: func(_ context.Context, _ uint64, _ string, _ []string) ([]string, error) {
				return []string{tagID}, nil
			},
		},
	}

	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: 1},
	}
	input := KnowledgeSearchInput{
		Tags: []string{"important"},
	}

	tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kbID}, targets)

	require.Len(t, targets, 1)
	assert.Contains(t, targets[0].TagIDs, tagID)
}

func TestResolveFolderTagScope_NoFoldersOrTagsIsNoOp(t *testing.T) {
	kbID := "kb-1"
	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{},
	}

	originalFolderIDs := []string{"pre-existing"}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: 1, FolderIDs: originalFolderIDs},
	}
	input := KnowledgeSearchInput{
		Queries: []string{"q"},
	}

	tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kbID}, targets)

	// Search targets must remain unchanged when no folders/tags are provided.
	assert.Equal(t, originalFolderIDs, targets[0].FolderIDs, "backward compat: pre-existing FolderIDs must not be cleared")
	assert.False(t, targets[0].IncludeSubfolders)
	assert.Nil(t, targets[0].TagIDs)
}

func TestResolveFolderTagScope_IncludeSubfoldersDefaultsTrue(t *testing.T) {
	kbID := "kb-1"
	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{
			resolveFolderNamesFn: func(_ context.Context, _ uint64, _ string, _ []string) ([]string, error) {
				return []string{"f1"}, nil
			},
		},
	}

	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: 1},
	}
	// IncludeSubfolders is nil → should default to true.
	input := KnowledgeSearchInput{Folders: []string{"docs"}}

	tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kbID}, targets)

	assert.True(t, targets[0].IncludeSubfolders, "nil IncludeSubfolders must default to true")
}

func TestResolveFolderTagScope_MultipleKBsAggregatesIDs(t *testing.T) {
	kb1 := "kb-1"
	kb2 := "kb-2"

	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{
			resolveFolderNamesFn: func(_ context.Context, _ uint64, kbID string, _ []string) ([]string, error) {
				switch kbID {
				case kb1:
					return []string{"f-kb1"}, nil
				case kb2:
					return []string{"f-kb2"}, nil
				}
				return nil, nil
			},
		},
	}

	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb2, TenantID: 1},
	}
	input := KnowledgeSearchInput{Folders: []string{"docs"}}

	tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kb1, kb2}, targets)

	// Both targets should get the union of resolved IDs.
	assert.ElementsMatch(t, []string{"f-kb1", "f-kb2"}, targets[0].FolderIDs)
	assert.ElementsMatch(t, []string{"f-kb1", "f-kb2"}, targets[1].FolderIDs)
}

func TestResolveFolderTagScope_ResolutionErrorDoesNotAbort(t *testing.T) {
	kbID := "kb-1"
	tool := &KnowledgeSearchTool{
		knowledgeService: &fakeKSForScope{
			resolveFolderNamesFn: func(_ context.Context, _ uint64, _ string, _ []string) ([]string, error) {
				return nil, assert.AnError
			},
		},
	}

	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: 1},
	}
	input := KnowledgeSearchInput{Folders: []string{"docs"}}

	// Should not panic; search targets remain unmodified because resolution failed.
	assert.NotPanics(t, func() {
		tool.resolveFolderTagScope(ctxWithTenant(1), input, []string{kbID}, targets)
	})
	assert.Nil(t, targets[0].FolderIDs, "failed resolution must not set FolderIDs")
}
