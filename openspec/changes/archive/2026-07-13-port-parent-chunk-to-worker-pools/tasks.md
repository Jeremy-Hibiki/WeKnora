## 1. Resume rebase and resolve task-queue architecture conflict

- [x] 1.1 In `.worktrees/dev`, stash local changes (`config/config.yaml`, `config/builtin_models.yaml`), then `git rebase main`
- [x] 1.2 At the `22b1fd2f` conflict on `internal/types/task.go`: take main's worker-pool architecture as base (ours), then add dev's additions: `QueueParentSummary` constant, `TypeParentSummaryGeneration` task type, `ParentSummaryGenerationPayload` struct, and a `queueDefinitions[]` entry `{Name: QueueParentSummary, Pool: WorkerPoolEnrichment, Weight: 1, SharedWeight: 1, TaskTypes: []string{TypeParentSummaryGeneration}}`
- [x] 1.3 Continue rebase past any remaining conflicts (merge commits will flatten; earlier conflicts admin/menu/package already resolved on prior attempt)

## 2. Port post-process parent-chunk integration

- [x] 2.1 In `internal/application/service/knowledge_post_process.go`: take main's version as base. Insert dev's parent-chunk collection block (filter `ChunkTypeParentText` from `textChunks`), `parentSummaryBatchCount` calculation, and `willSpawnParentSummary` flag. Use `ListAllContentChunksByKnowledgeID` instead of `ListChunksByKnowledgeID`.
- [x] 2.2 Add `parentSummaryBatchCount` to `expectedSubtasks`, `plannedOwned`, and `enqueuedParentSummaryCount` to `actualOwned` in the counter-reconciliation block.
- [x] 2.3 Insert the `enqueueParentSummaryGenerationTasks` call block (step 4b in dev's diff) between question generation and graph RAG task spawning.
- [x] 2.4 Add `enqueued_parent_summary` / `enqueued_parent_summary_count` to the `postOutput` span metadata.
- [x] 2.5 Remove all `[PP-debug]` verbose log lines; keep only the final summary log line.
- [x] 2.6 Port the `enqueueParentSummaryGenerationTasks` method and `parentSummaryGenBatchSize` constant.

## 3. Port parent-summary handler and supporting code

- [x] 3.1 Port `ProcessParentSummaryGeneration` handler into `internal/application/service/knowledge_process.go`. Verify it compiles against main's current interfaces (`BatchIndex`, LLM service, chunk repo). Adapt signatures if main changed them.
- [x] 3.2 Add handler registration in `internal/router/task.go`: `mux.HandleFunc(types.TypeParentSummaryGeneration, params.KnowledgeService.ProcessParentSummaryGeneration)`
- [x] 3.3 Port `ListAllContentChunksByKnowledgeID` into `internal/application/repository/chunk.go` and `internal/types/interfaces/chunk.go`.
- [x] 3.4 Port `ChunkTypeParentText` / `ChunkTypeParentSummary` constants into `internal/types/chunk.go` if not already present.
- [x] 3.5 Port `isSearchableChunk` whitelist update in `internal/application/service/knowledgebase_search_results.go`.

## 4. Port merge-pipeline bug fixes

- [x] 4.1 Diff main vs dev on `internal/application/service/chat_pipeline/merge.go`. Identify whether main changed `resolveParentChunks` independently.
- [x] 4.2 Apply the 4 bug fixes from dev: (1) child retains own content, (2) image chunk skips grandparent chain, (3) parentMap uses `r.ID` for parent-summary case, (4) continue guard inside case block.
- [x] 4.3 Port the diagnostic logging additions (`logCandidateSummary`, `logResultTypes`, `per_chunk_decisions`) if they don't conflict with main's logging.

## 5. Verify build and complete rebase

- [x] 5.1 Run `go build ./...` in the worktree — fix any compile errors from interface mismatches
- [x] 5.2 `git rebase --continue` through any remaining commits until rebase completes cleanly
- [x] 5.3 Pop the stashed local changes (`config/config.yaml`, `config/builtin_models.yaml`)
- [x] 5.4 Verify `git log --oneline main..dev` shows all dev commits replayed on top of main
- [x] 5.5 Confirm `QueueParentSummary` appears in the RuntimeQueues enrichment-pool section by checking `QueueForTaskType(TypeParentSummaryGeneration)` returns `parent_summary`
