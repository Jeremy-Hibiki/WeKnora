## Why

When the housekeeping sweep marks a knowledge row as `failed` (stale heartbeat + no matching asynq task), it does not clean up that knowledge's pending wiki ingest ops in `task_pending_ops`. The wiki worker's skip check (`isKnowledgeGone`) only handles `deleting`/`cancelled` — not `failed` — so subsequent triggers re-execute full LLM extraction on an already-failed document. On asynq timeout (60m), the failure-path DB writes (`trimPendingList`, `requeueFailedOps`) use the already-cancelled context, silently failing to increment `fail_count`, so the op never reaches the dead-letter budget and re-loops forever. This was reported in v0.6.3 and is still present on HEAD — no commit fixed this bug class.

## What Changes

- Add `ParseStatusFailed` to `isKnowledgeGone`'s terminal status set so the wiki worker skips failed documents (same treatment already given to `deleting`/`cancelled`).
- Call `scrubWikiPendingIngest` in the housekeeping sweep when marking knowledge rows as `failed`, aligning with the cancel/delete/reparse paths that already do this.
- Switch `trimPendingList` and `requeueFailedOps` to use `context.WithoutCancel(ctx)` for their DB writes, matching the existing `finalizeWikiSubtask` detached-context pattern, so timeout-path failures still consume the retry budget and eventually dead-letter.

## Capabilities

### New Capabilities

- `wiki-task-lifecycle`: Governs the synchronization between knowledge parse status and wiki task queue ops — ensuring that terminal knowledge states (failed, cancelled, deleting) reliably remove or skip pending wiki ingest ops, and that wiki batch failure paths always consume the retry budget regardless of context cancellation.

### Modified Capabilities

None — no existing spec covers wiki task lifecycle behavior.

## Impact

- **`internal/application/service/wiki_ingest.go`** — `isKnowledgeGone` (add `ParseStatusFailed` case), `trimPendingList` (detached ctx), `requeueFailedOps` (detached ctx).
- **`internal/application/service/knowledge_housekeeping.go`** — `runSweep` (scrub wiki ops for each knowledge marked failed).
- **`internal/application/service/knowledge_housekeeping_test.go`** — new test cases for wiki op scrubbing on housekeeping failure.
- **`internal/application/service/wiki_ingest_batch.go`** — no behavioral change, but existing tests may need adjustment for `isKnowledgeGone` returning true on `ParseStatusFailed`.
- No API changes, no schema changes, no breaking changes.
