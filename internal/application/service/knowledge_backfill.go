package service

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// BackfillFolderMetadata backfills folder_id metadata in vector stores for
// existing chunks. If kbID is empty, every knowledge base across all tenants
// is processed; otherwise only the named KB is processed.
//
// Per-KB failures (engine resolution, batch update) are collected into the
// result's Errors slice rather than aborting the sweep — the caller sees how
// much succeeded and which KBs need attention.
func (s *knowledgeService) BackfillFolderMetadata(ctx context.Context, kbID string) (*types.BackfillResult, error) {
	result := &types.BackfillResult{}

	var kbs []*types.KnowledgeBase
	if kbID != "" {
		kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
		if err != nil {
			return nil, fmt.Errorf("get knowledge base %s: %w", kbID, err)
		}
		if kb == nil {
			return result, nil
		}
		kbs = []*types.KnowledgeBase{kb}
	} else {
		var err error
		kbs, err = s.kbService.GetRepository().ListKnowledgeBases(ctx)
		if err != nil {
			return nil, fmt.Errorf("list knowledge bases: %w", err)
		}
	}

	result.TotalKBs = len(kbs)

	for _, kb := range kbs {
		s.backfillKB(ctx, kb, result)
	}

	return result, nil
}

// backfillKB processes a single KB: lists its knowledge entries, builds the
// knowledgeID→folderID map, resolves the retrieve engine, and calls
// BatchUpdateFolderID. Errors are appended to the result.
func (s *knowledgeService) backfillKB(ctx context.Context, kb *types.KnowledgeBase, result *types.BackfillResult) {
	knowledgeList, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, kb.TenantID, kb.ID)
	if err != nil {
		result.Errors = append(result.Errors, types.BackfillKBError{
			KBID:  kb.ID,
			Error: fmt.Sprintf("list knowledge: %v", err),
		})
		return
	}

	knowledgeFolderMap := make(map[string]string, len(knowledgeList))
	for _, k := range knowledgeList {
		knowledgeFolderMap[k.ID] = k.GetFolderID()
	}

	if len(knowledgeFolderMap) == 0 {
		result.ProcessedKBs++
		return
	}

	engine, err := retriever.CreateRetrieveEngineForKB(
		ctx, s.retrieveEngine, s.ownership, kb.TenantID, kb.VectorStoreID,
	)
	if err != nil {
		result.Errors = append(result.Errors, types.BackfillKBError{
			KBID:  kb.ID,
			Error: fmt.Sprintf("create retrieve engine: %v", err),
		})
		return
	}

	if err := engine.BatchUpdateFolderID(ctx, knowledgeFolderMap); err != nil {
		result.Errors = append(result.Errors, types.BackfillKBError{
			KBID:  kb.ID,
			Error: fmt.Sprintf("batch update folder metadata: %v", err),
		})
		return
	}

	result.ProcessedKBs++
	result.TotalKnowledgeUpdated += len(knowledgeFolderMap)
	logger.Infof(ctx,
		"[backfill] KB %s: updated folder_id metadata for %d knowledge entries",
		kb.ID, len(knowledgeFolderMap))
}
