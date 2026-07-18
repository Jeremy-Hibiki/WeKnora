## Why

The `dev` branch carries a parent-child chunk recall feature (commit `22b1fd2f`) that adds `QueueParentSummary` — a task queue constant and handler for asynchronously generating summaries of parent chunks. Upstream `main` has since refactored the entire task-queue layer to a worker-pool model (`queueDefinitions[]` registry, 6 independent asynq.Servers). The rebase of `dev` onto latest `main` is blocked at this commit because `QueueParentSummary` and its siblings (`QueueCritical`, `QueueLow`, etc.) no longer exist in main's `internal/types/task.go`. Without migrating this feature, the rebase cannot complete and `dev`'s parent-chunk recall capability is stranded on the old architecture.

## What Changes

- Migrate `QueueParentSummary` from the old flat-queue model to main's worker-pool architecture by registering it in `queueDefinitions[]` under the `enrichment` pool (same family as `QueueSummary`, `QueueMultimodal`, `QueueGraph`, `QueueQuestion`).
- Port `TypeParentSummaryGeneration` task type and `ParentSummaryGenerationPayload` into main's `internal/types/task.go`.
- Port the `ProcessParentSummaryGeneration` handler and `enqueueParentSummaryGenerationTasks` enqueue logic into main's refactored `knowledge_post_process.go` / `knowledge_process.go`, adapting to main's counter-reconciliation and span-tracking framework.
- Port the 4 merge-pipeline bug fixes in `chat_pipeline/merge.go` (child-chunk preservation, image-chunk simplification, parentMap lookup fix, continue-guard fix).
- Port supporting changes: `ChunkTypeParentText`/`ChunkTypeParentSummary` searchability whitelist, `ListAllContentChunksByKnowledgeID` interface, and `isSearchableChunk` updates.
- Register the handler in `internal/router/task.go` (`mux.HandleFunc`).

No breaking changes — this is a pure port of existing functionality onto a new internal architecture.

## Capabilities

### New Capabilities

_None — all capabilities already exist in `dev`; this change ports them, it does not introduce new product behavior._

### Modified Capabilities

_None at the spec/requirement level. The parent-chunk recall behavior is unchanged; only its task-queue integration layer changes from flat-queue to worker-pool._

## Impact

- **`internal/types/task.go`**: Add `QueueParentSummary` constant, `TypeParentSummaryGeneration` task type, `ParentSummaryGenerationPayload` struct, and a `queueDefinitions[]` entry (`Pool: WorkerPoolEnrichment`).
- **`internal/router/task.go`**: Add `mux.HandleFunc(types.TypeParentSummaryGeneration, ...)`.
- **`internal/application/service/knowledge_post_process.go`**: Integrate parent-chunk collection and `enqueueParentSummaryGenerationTasks` into main's counter-reconciliation flow (`plannedOwned`/`actualOwned`).
- **`internal/application/service/knowledge_process.go`**: Port `ProcessParentSummaryGeneration` handler.
- **`internal/application/service/chat_pipeline/merge.go`**: Port 4 bug fixes in `resolveParentChunks`.
- **`internal/application/repository/chunk.go`**, **`internal/types/interfaces/chunk.go`**: Port `ListAllContentChunksByKnowledgeID`.
- **`internal/types/chunk.go`**: Port `ChunkTypeParentText`/`ChunkTypeParentSummary` constants.
- **`internal/application/service/knowledgebase_search_results.go`**: Port `isSearchableChunk` whitelist update.
- **Runtime observability**: `QueueParentSummary` will automatically appear in the RuntimeQueues dashboard (enrichment pool section) via the registry — no additional wiring needed.
