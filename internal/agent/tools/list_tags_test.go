package tools

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTagsTool_ReturnsTags(t *testing.T) {
	kb1 := "kb-1"
	kb2 := "kb-2"
	ks := &fakeKSForList{
		tagsByKB: map[string][]types.TagSummary{
			kb1: {
				{Name: "important", DocumentCount: 4},
				{Name: "draft", DocumentCount: 2},
			},
			kb2: {
				{Name: "review", DocumentCount: 1},
			},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb2, TenantID: 1},
	}
	tool := NewListTagsTool(ks, targets)

	result, err := tool.Execute(ctxWithTenant(1), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.Success)

	var tags []types.TagSummary
	require.NoError(t, json.Unmarshal([]byte(result.Output), &tags))
	assert.Len(t, tags, 3, "should return tags from both KBs")
}

func TestListTagsTool_FilteredByKB(t *testing.T) {
	kb1 := "kb-1"
	kb2 := "kb-2"
	ks := &fakeKSForList{
		tagsByKB: map[string][]types.TagSummary{
			kb1: {{Name: "important", DocumentCount: 4}},
			kb2: {{Name: "archived", DocumentCount: 1}},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb2, TenantID: 1},
	}
	tool := NewListTagsTool(ks, targets)

	input, _ := json.Marshal(ListTagsInput{KnowledgeBaseIDs: []string{kb2}})
	result, err := tool.Execute(ctxWithTenant(1), input)
	require.NoError(t, err)
	assert.True(t, result.Success)

	var tags []types.TagSummary
	require.NoError(t, json.Unmarshal([]byte(result.Output), &tags))
	require.Len(t, tags, 1)
	assert.Equal(t, "archived", tags[0].Name)
}

func TestListTagsTool_EmptyKBScope(t *testing.T) {
	ks := &fakeKSForList{}
	tool := NewListTagsTool(ks, types.SearchTargets{})

	result, err := tool.Execute(ctxWithTenant(1), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "[]", result.Output)
}

func TestListTagsTool_FilteredToNonExistentKB(t *testing.T) {
	kb1 := "kb-1"
	ks := &fakeKSForList{
		tagsByKB: map[string][]types.TagSummary{
			kb1: {{Name: "important", DocumentCount: 1}},
		},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kb1, TenantID: 1},
	}
	tool := NewListTagsTool(ks, targets)

	input, _ := json.Marshal(ListTagsInput{KnowledgeBaseIDs: []string{"kb-nonexistent"}})
	result, err := tool.Execute(ctxWithTenant(1), input)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "[]", result.Output)
}
