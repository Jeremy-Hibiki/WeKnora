package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var listFoldersTool = BaseTool{
	name: ToolListFolders,
	description: `List all folders in the knowledge base scope.

Use this to discover folder names you can pass to knowledge_search's "folders" parameter.

## Parameters
- knowledge_base_ids (optional): KB IDs to list folders for. Defaults to all configured KBs.

## Output
Returns a JSON array of folders with names, paths, and document counts.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "knowledge_base_ids": {
      "type": "array",
      "description": "Optional: KB IDs to list folders for. Defaults to all configured KBs.",
      "items": {"type": "string"},
      "maxItems": 5
    }
  }
}`),
}

// ListFoldersInput defines the input for the list_folders tool.
type ListFoldersInput struct {
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty"`
}

// ListFoldersTool lists folders in the agent's KB scope.
type ListFoldersTool struct {
	BaseTool
	knowledgeService interfaces.KnowledgeService
	searchTargets    types.SearchTargets
}

// NewListFoldersTool creates a new list_folders tool.
func NewListFoldersTool(ks interfaces.KnowledgeService, targets types.SearchTargets) *ListFoldersTool {
	return &ListFoldersTool{
		BaseTool:         listFoldersTool,
		knowledgeService: ks,
		searchTargets:    targets,
	}
}

func (t *ListFoldersTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input ListFoldersInput
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
	allFolders := make([]types.FolderSummary, 0)
	for _, kbID := range kbIDs {
		folders, err := t.knowledgeService.ListFoldersByKB(ctx, tenantID, kbID)
		if err != nil {
			logger.Warnf(ctx, "[Tool][ListFolders] Failed to list folders for KB %s: %v", kbID, err)
			continue
		}
		allFolders = append(allFolders, folders...)
	}

	data, err := json.MarshalIndent(allFolders, "", "  ")
	if err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("Failed to marshal result: %v", err)}, err
	}

	return &types.ToolResult{Success: true, Output: string(data)}, nil
}
