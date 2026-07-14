package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var listTagsTool = BaseTool{
	name: ToolListTags,
	description: `List all tags in the knowledge base scope.

Use this to discover tag names you can pass to knowledge_search's "tags" parameter.

## Parameters
- knowledge_base_ids (optional): KB IDs to list tags for. Defaults to all configured KBs.

## Output
Returns a JSON array of tags with names and document counts.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "knowledge_base_ids": {
      "type": "array",
      "description": "Optional: KB IDs to list tags for. Defaults to all configured KBs.",
      "items": {"type": "string"},
      "maxItems": 5
    }
  }
}`),
}

// ListTagsInput defines the input for the list_tags tool.
type ListTagsInput struct {
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty"`
}

// ListTagsTool lists tags in the agent's KB scope.
type ListTagsTool struct {
	BaseTool
	knowledgeService interfaces.KnowledgeService
	searchTargets    types.SearchTargets
}

// NewListTagsTool creates a new list_tags tool.
func NewListTagsTool(ks interfaces.KnowledgeService, targets types.SearchTargets) *ListTagsTool {
	return &ListTagsTool{
		BaseTool:         listTagsTool,
		knowledgeService: ks,
		searchTargets:    targets,
	}
}

func (t *ListTagsTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input ListTagsInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("Failed to parse args: %v", err)}, err
		}
	}

	kbIDs := t.searchTargets.GetAllKnowledgeBaseIDs()
	if len(input.KnowledgeBaseIDs) > 0 {
		userSet := make(map[string]bool)
		for _, id := range input.KnowledgeBaseIDs {
			userSet[id] = true
		}
		var filtered []string
		for _, id := range kbIDs {
			if userSet[id] {
				filtered = append(filtered, id)
			}
		}
		kbIDs = filtered
	}

	if len(kbIDs) == 0 {
		return &types.ToolResult{Success: true, Output: "[]"}, nil
	}

	tenantID := types.MustTenantIDFromContext(ctx)
	allTags := make([]types.TagSummary, 0)
	for _, kbID := range kbIDs {
		tags, err := t.knowledgeService.ListTagsByKB(ctx, tenantID, kbID)
		if err != nil {
			logger.Warnf(ctx, "[Tool][ListTags] Failed to list tags for KB %s: %v", kbID, err)
			continue
		}
		allTags = append(allTags, tags...)
	}

	data, err := json.MarshalIndent(allTags, "", "  ")
	if err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("Failed to marshal result: %v", err)}, err
	}

	return &types.ToolResult{Success: true, Output: string(data)}, nil
}
