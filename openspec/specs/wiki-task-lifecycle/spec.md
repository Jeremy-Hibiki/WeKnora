# wiki-task-lifecycle Specification

## Purpose
TBD - created by archiving change fix-wiki-zombie-task-desync. Update Purpose after archive.
## Requirements
### Requirement: Housekeeping sweep scrubs wiki ops for failed knowledge

When the housekeeping sweep marks a knowledge row as `failed`, it MUST remove that knowledge's pending `WikiOpIngest` entries from `task_pending_ops`, matching the behavior already enforced by cancel, delete, and reparse paths. Retract ops MUST be preserved so any delete-cleanup can still unlink pages.

#### Scenario: Knowledge with pending wiki op is marked failed by housekeeping

- **WHEN** the housekeeping sweep marks knowledge K as `parse_status=failed`
- **AND** K has a pending `WikiOpIngest` op in `task_pending_ops`
- **THEN** the `WikiOpIngest` op for K is removed from `task_pending_ops`
- **AND** any `WikiOpRetract` ops for K are preserved

#### Scenario: Scrub failure is logged but does not block the sweep

- **WHEN** the scrub call for knowledge K fails (DB error)
- **THEN** the failure is logged as a warning
- **AND** the sweep continues processing remaining stuck rows

---

### Requirement: Wiki worker skips knowledge in failed parse status

The wiki ingest worker MUST treat `ParseStatusFailed` as a terminal state and skip LLM extraction for that knowledge, identical to how it already handles `ParseStatusDeleting` and `ParseStatusCancelled`.

#### Scenario: Wiki ingest op claimed for a failed knowledge

- **WHEN** the wiki worker claims a `WikiOpIngest` op for knowledge K
- **AND** K's `parse_status` is `failed`
- **THEN** the worker skips LLM extraction for K
- **AND** the op is treated as a terminal skip (no docResult, no failedOp)
- **AND** the knowledge's finalizing subtask slot is released

#### Scenario: Failed knowledge is re-parsed

- **WHEN** a user re-parses knowledge K whose `parse_status` is `failed`
- **THEN** `prepareWikiForReparse` scrubs K's stale wiki ingest ops
- **AND** when re-parse reaches post-process, a fresh wiki ingest op is enqueued
- **AND** the worker processes the fresh op normally (K's status is now `processing`, not `failed`)

---

### Requirement: Wiki batch failure-path DB writes survive context cancellation

When the wiki batch handler's context is cancelled (asynq timeout, graceful shutdown), failure-path DB writes — `trimPendingList` (delete successful ops) and `requeueFailedOps` (increment fail count, dead-letter) — MUST execute on a detached context so the retry budget is consumed and ops eventually reach dead-letter status.

#### Scenario: Asynq timeout during wiki batch processing

- **WHEN** the batch context is cancelled by asynq timeout (60m) after the map phase
- **AND** one or more ops are in `failedOps`
- **THEN** `requeueFailedOps` calls `IncrFailCount` on a detached context
- **AND** the fail count for each failed op increments successfully
- **AND** ops that exceed `wikiMaxFailRetries` are archived to `task_dead_letters`

#### Scenario: Successful ops are trimmed even on cancelled context

- **WHEN** the batch context is cancelled after map+reduce completed for some ops
- **AND** those ops are not in `failedOps`
- **THEN** `trimPendingList` calls `DeleteByIDs` on a detached context
- **AND** the successful ops are removed from `task_pending_ops`

#### Scenario: Detached context is bounded by a timeout

- **WHEN** a detached-context DB write is used in the failure path
- **THEN** each write uses a bounded timeout (matching `finalizeSubtaskDetachedTimeout`)
- **AND** a wedged DB connection cannot pin the goroutine indefinitely

