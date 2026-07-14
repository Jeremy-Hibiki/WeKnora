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

// fakeKSForList embeds interfaces.KnowledgeService and overrides only the
// list methods needed by ListFoldersTool / ListTagsTool.
type fakeKSForList struct {
	interfaces.KnowledgeService
	foldersByKB map[string][]types.FolderSummary
	tagsByKB    map[string][]types.TagSummary
}

func (f *fakeKSForList) ListFoldersByKB(_ context.Context, _ uint64, kbID string) ([]types.FolderSummary, error) {
	return f.foldersByKB[kbID], nil
}

func (f *fakeKSForList) ListTagsByKB(_ context.Context, _ uint64, kbID string) ([]types.TagSummary, error) {
	return f.tagsByKB[kbID], nil
}

func TestListFoldersTool_ReturnsFolders(t *testing.T) {
	kb1 := "kb-1"
	kb2 := "kb-2"
	ks := &fakeKSForList{
		foldersByKB: map[string][]types.FolderSummary{
			kb1: {
				{Name: "Go", Path: "/f1/", DocumentCount: 3},
				{Name: "API", Path: "/f2/", DocumentCount: 5},
			},
			kb2: {
				{Name: "Docs", Path: "/f3/", DocumentCount: 1},
			},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb2, TenantID: 1},
	}
	tool := NewListFoldersTool(ks, targets)

	result, err := tool.Execute(ctxWithTenant(1), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.Success)

	var folders []types.FolderSummary
	require.NoError(t, json.Unmarshal([]byte(result.Output), &folders))
	assert.Len(t, folders, 3, "should return folders from both KBs")
}

func TestListFoldersTool_FilteredByKB(t *testing.T) {
	kb1 := "kb-1"
	kb2 := "kb-2"
	ks := &fakeKSForList{
		foldersByKB: map[string][]types.FolderSummary{
			kb1: {{Name: "Go", Path: "/f1/", DocumentCount: 2}},
			kb2: {{Name: "Rust", Path: "/f2/", DocumentCount: 1}},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb2, TenantID: 1},
	}
	tool := NewListFoldersTool(ks, targets)

	// Request only kb-1.
	input, _ := json.Marshal(ListFoldersInput{KnowledgeBaseIDs: []string{kb1}})
	result, err := tool.Execute(ctxWithTenant(1), input)
	require.NoError(t, err)
	assert.True(t, result.Success)

	var folders []types.FolderSummary
	require.NoError(t, json.Unmarshal([]byte(result.Output), &folders))
	assert.Len(t, folders, 1)
	assert.Equal(t, "Go", folders[0].Name)
}

func TestListFoldersTool_EmptyKBScope(t *testing.T) {
	ks := &fakeKSForList{}
	tool := NewListFoldersTool(ks, types.SearchTargets{})

	result, err := tool.Execute(ctxWithTenant(1), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "[]", result.Output)
}

func TestListFoldersTool_FilteredToNonExistentKB(t *testing.T) {
	kb1 := "kb-1"
	ks := &fakeKSForList{
		foldersByKB: map[string][]types.FolderSummary{
			kb1: {{Name: "Go", Path: "/f1/"}},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
	}
	tool := NewListFoldersTool(ks, targets)

	// Request a KB that is NOT in the search targets — should return empty.
	input, _ := json.Marshal(ListFoldersInput{KnowledgeBaseIDs: []string{"kb-nonexistent"}})
	result, err := tool.Execute(ctxWithTenant(1), input)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "[]", result.Output)
}
