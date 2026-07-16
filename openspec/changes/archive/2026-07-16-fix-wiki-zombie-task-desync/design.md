## Context

WeKnora's wiki ingest pipeline uses a durable DB-backed queue (`task_pending_ops`) drained by a debounced KB-scoped asynq trigger task (`wiki:ingest`). The knowledge row's `parse_status` column is the authoritative state for the document lifecycle. Three independent gaps allow these two systems to desync when the housekeeping sweep marks a row as `failed`:

1. The sweep (`HousekeepingService.runSweep`) updates `parse_status=failed` but never calls `scrubWikiPendingIngest`, leaving wiki ops in the queue.
2. The wiki worker's skip check (`isKnowledgeGone`) only recognizes `deleting`/`cancelled` — not `failed`.
3. On asynq timeout, `trimPendingList` and `requeueFailedOps` use the already-cancelled batch context, so `fail_count` never increments and the op never reaches the dead-letter budget.

The `finalizeWikiSubtask` success path already uses `context.WithoutCancel(ctx)` — this design extends the same pattern to the failure path.

## Goals / Non-Goals

**Goals:**
- A knowledge row marked `failed` by housekeeping must have its pending wiki ingest ops removed from `task_pending_ops`.
- The wiki worker must skip any knowledge whose `parse_status` is `failed` (in addition to `deleting`/`cancelled`).
- Wiki batch failure-path DB writes (`trimPendingList`, `requeueFailedOps`) must succeed even when the batch context is cancelled by asynq timeout, so `fail_count` increments and the retry budget is consumed.
- "Failed → reparse" workflow must not be blocked (reparse writes fresh ops after scrubbing stale ones, so this is safe).

**Non-Goals:**
- Redesigning the wiki queue architecture (DB-backed `task_pending_ops` stays).
- Making `filterOutQueued` aware of `task_pending_ops` (the asynq inspector stays asynq-only; the scrub-at-fail approach is simpler and more direct).
- Changing the `finalizeWikiSubtask` detached-context pattern (already correct).
- Addressing the "KNOWN GAP (TODO)" in `knowledge_post_process.go` about fire-and-forget wiki enqueue failure (separate issue, lower severity).

## Decisions

### D1: Scrub wiki ops in housekeeping sweep (Gap ①)

**Decision:** After the sweep's bulk `UPDATE ... SET parse_status=failed`, iterate over the affected knowledge IDs and call `scrubWikiPendingIngest` for each.

**Why not a DB-level cascade or trigger?** `scrubWikiPendingIngest` calls `pendingRepo.DeleteByDedupKey` which is a targeted delete filtered by `op=WikiOpIngest` (preserving retract ops). A raw cascade would also delete retracts, breaking the delete-cleanup path. The Go-level call is already battle-tested across three call sites.

**Why not teach `filterOutQueued` about `task_pending_ops`?** That would require injecting `TaskPendingOpsRepository` into `HousekeepingService` (currently it only has `db` + `inspector`), and the logic to distinguish "KB-scoped wiki trigger in flight" from "this specific knowledge's op is backed up" is non-trivial. Scrubbing at the point of failure is simpler and matches the cancel/delete/reparse pattern exactly.

**Implementation note:** The sweep already collects `stuckIDs` before the UPDATE. The scrub loop runs after the UPDATE succeeds, matching the ordering in `CancelKnowledgeParse` (status flip first, then scrub).

### D2: Add `ParseStatusFailed` to `isKnowledgeGone` (Gap ②)

**Decision:** Add `types.ParseStatusFailed` to the existing `switch` in `isKnowledgeGone`.

**Alternative considered:** Rename `isKnowledgeGone` to `isKnowledgeTerminal` for clarity. **Rejected** — the name is used in 4 call sites and referenced in comments; renaming adds diff noise without behavioral benefit. The function's docstring already says "returns true if the given knowledge has been deleted or is in the middle of being deleted" — we'll update the docstring to cover failed.

**Reparse safety:** When a user re-parses a `failed` document, `prepareWikiForReparse` calls `scrubWikiPendingIngest` *before* `KnowledgePostProcess` enqueues the new wiki op. The new op lands in the queue only after reparse reaches post-process, by which time `parse_status` has transitioned back to `processing`. So `isKnowledgeGone` checking `failed` cannot block reparse.

### D3: Detached context for failure-path DB writes (Gap ③)

**Decision:** In `trimPendingList` and `requeueFailedOps`, replace `ctx` with `context.WithoutCancel(ctx)` for all `pendingRepo` / `deadLetterRepo` calls.

**Why not move the calls before the errgroup?** The trim and requeue logic depends on `failedOps` and `peekedIDs`, which are only known after the map+reduce phases complete. They must stay at the end.

**Timeout budget:** Each detached call should use `context.WithTimeout(context.WithoutCancel(ctx), finalizeSubtaskDetachedTimeout)` to bound a wedged connection, matching the existing pattern in `KnowledgePostProcessService.Handle`'s shortfall-release loop.

**Asymmetry resolution:** This makes the failure path consistent with the success path. `finalizeWikiSubtask` already uses `finalizeSubtaskDetached` for the same reason — "the wiki batch worker may be mid-shutdown or have a cancelled ctx when this runs."

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Scrub in housekeeping adds DB calls to the sweep | The sweep already runs every 5 min and the `stuck` set is typically 0–2 rows. `DeleteByDedupKey` is indexed. Negligible overhead. |
| `isKnowledgeGone` returning true for `failed` could skip a wiki op that was enqueued for a valid reason | A `failed` document is terminal — its chunks are incomplete or missing. Running wiki extraction on it produces garbage. The only way out is reparse, which re-enqueues. |
| Detached ctx extends wall-clock of a timed-out batch by a few ms for DB writes | Bounded by `finalizeSubtaskDetachedTimeout`. The batch already ran for 60 min; a few extra ms is noise. |
| If scrub fails (DB error), the zombie persists | `scrubWikiPendingIngest` logs the error. Combined with D2 (worker skip), the zombie won't execute LLM calls — it just sits in the queue until an operator clears it. Defense-in-depth: the worst case degrades from "infinite LLM loop" to "harmless stale row." |
