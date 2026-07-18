## Context

The `dev` branch's parent-child chunk recall feature (commit `22b1fd2f`) was built against the old flat-queue task model: a single `asynq.Server` with hardcoded queue weights (`QueueCritical`, `QueueDefault`, `QueueLow`, etc.) in `internal/router/task.go`. Upstream `main` (HEAD `50b8c0a7`) replaced this with a worker-pool model:

- 6 independent `asynq.Server` instances (`core`, `postprocess`, `enrichment`, `maintenance`, `shared`, `wiki`)
- A `queueDefinitions[]` registry in `internal/types/task.go` is the single source of truth mapping each queue to a pool, weight, and task-type set
- Server construction, runtime inspection (`RuntimeQueues` dashboard), and `QueueForTaskType()` all consume this registry

The rebase of `dev` onto `main` is blocked at commit `22b1fd2f` because `QueueParentSummary` and the old queue constants no longer exist in main's `task.go`.

Meanwhile, main's `knowledge_post_process.go` was heavily refactored: a counter-reconciliation system (`plannedOwned`/`actualOwned`/shortfall release) tracks each spawned enrichment subtask so a failed enqueue doesn't strand a knowledge row in "finalizing". Critically, dev's parent-summary code already integrated with this counter — it adds `parentSummaryBatchCount` to both `plannedOwned` and `actualOwned`.

## Goals / Non-Goals

**Goals:**
- Complete the rebase of `dev` onto `main` by resolving the task-queue architecture conflict at commit `22b1fd2f`.
- Preserve all parent-child chunk recall behavior: parent-summary async generation, parent_text/parent_summary searchability, and the 4 merge-pipeline bug fixes.
- Integrate `QueueParentSummary` into the enrichment pool so it shares capacity with sibling LLM-backed queues.

**Non-Goals:**
- Redesigning the parent-chunk recall algorithm.
- Changing batch sizes, retry counts, or LLM prompt logic.
- Touching the worker-pool architecture itself.
- Resolving other `dev` commits (admin UI, batch tagging, etc. — those are mechanical and already resolved).

## Decisions

### D1: `QueueParentSummary` → `WorkerPoolEnrichment`

**Decision:** Register `QueueParentSummary` in `queueDefinitions[]` with `Pool: WorkerPoolEnrichment`, `Weight: 1`, `SharedWeight: 1`.

**Rationale:** Parent-summary generation is LLM-backed, high-fanout (one task per 20-chunk batch), and slow — identical profile to `QueueSummary`, `QueueQuestion`, `QueueGraph`, `QueueMultimodal`, which are all in the enrichment pool. It should share that pool's dedicated capacity (default 12 workers) and be eligible for elastic shared-pool borrowing.

**Alternatives considered:**
- *New dedicated pool:* Rejected — adds operational complexity for a workload profile that already matches enrichment. No capacity-isolation rationale distinct from the existing enrichment queues.
- *Core pool:* Rejected — parent-summary is not latency-sensitive user-facing parsing.

### D2: Counter reconciliation — `parentSummaryBatchCount` in `plannedOwned`/`actualOwned`

**Decision:** Keep dev's integration unchanged: `parentSummaryBatchCount` is added to `expectedSubtasks`, `plannedOwned`, and `actualOwned`. Shortfall release applies if batches fail to enqueue.

**Rationale:** Main's counter-reconciliation framework is the authority for preventing "finalizing" strand. Dev already wired parent-summary into it correctly. No change needed — the code ports as-is.

### D3: `ListAllContentChunksByKnowledgeID` replaces `ListChunksByKnowledgeID`

**Decision:** Port dev's new `ListAllContentChunksByKnowledgeID` interface method, which includes `parent_text` chunks (the original `ListChunksByKnowledgeID` excluded them). Post-process uses this to discover parent chunks for summary generation.

**Rationale:** Without this, parent chunks are invisible to post-process and no summaries are generated. The repository-level implementation is a one-liner filter change.

### D4: Merge commit handling during rebase

**Decision:** Rebase dev onto main using default behavior (flatten merge commits). The 5 merge commits in dev's history (`9036a0e5`, `4a5bb4c8`, `bf21a5ef`, `d32aceb1`, `8c7c144b`) become no-ops if all their constituent commits replay cleanly.

**Rationale:** Git's default rebase flattens merge commits, replaying only non-merge commits. Since the merge commits in dev are all "merge feature branch into dev" with no independent changes, flattening is safe.

**Alternative:** `--rebase-merges` preserves merge topology. Rejected — adds complexity with no benefit; dev's merges are simple feature integrations.

## Risks / Trade-offs

- **Post-process structural drift:** Main's `knowledge_post_process.go` has ~450 lines vs dev's base of ~300. The diff is not just parent-chunk additions — main added span tracking, wiki ingest integration, cancellation guards, and detached-context shortfall release. The port must carefully merge dev's parent-chunk blocks into main's structure, not vice versa. **Mitigation:** Take main's file as base, surgically insert dev's parent-chunk blocks (collection, enqueue, counter integration).

- **Merge-pipeline bug fixes:** Dev's `merge.go` has 4 bug fixes in `resolveParentChunks`. If main's `merge.go` also changed `resolveParentChunks`, these fixes need manual re-application. **Mitigation:** Diff main vs dev on `merge.go` before applying; port only the 4 specific fix blocks.

- **`ProcessParentSummaryGeneration` handler dependencies:** The handler (in `knowledge_process.go`) calls `BatchIndex`, LLM summary, and chunk repo. If main changed these interfaces, the handler needs adaptation. **Mitigation:** Check signature compatibility during port.

- **PP-debug logging:** Dev's code has `[PP-debug]` log lines. These are verbose debug logs that should be cleaned up or downgraded to trace level before merge. **Mitigation:** Remove `[PP-debug]` lines, keep only the final summary log line.
