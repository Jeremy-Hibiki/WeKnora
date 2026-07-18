## ADDED Requirements

<!-- This spec documents the parent-chunk recall behavior being ported from dev to main's worker-pool architecture.
     No product behavior changes — these requirements describe what the ported feature must preserve. -->

### Requirement: Parent chunk summary generation via enrichment pool

Parent chunks (`ChunkTypeParentText`) discovered during post-process must trigger asynchronous LLM summary generation. Summaries are written directly to the vector index (not DB) as `ChunkTypeParentSummary` with `SourceID` = parent chunk ID. LLM failure falls back to the parent content's first 500 characters.

#### Scenario: Parent chunks with summary model configured
- **WHEN** post-process runs for a knowledge with `kb.SummaryModelID != ""` and parent chunks exist
- **THEN** one `TypeParentSummaryGeneration` task is enqueued per batch of 20 parent chunks to `QueueParentSummary` (enrichment pool)

#### Scenario: Parent chunks without summary model
- **WHEN** post-process runs with `kb.SummaryModelID == ""`
- **THEN** no parent-summary tasks are enqueued

#### Scenario: LLM failure during summary generation
- **WHEN** the LLM call fails for a parent chunk
- **THEN** the handler falls back to the parent chunk content's first 500 characters as the summary

### Requirement: Counter reconciliation includes parent-summary batches

Parent-summary batch tasks must be counted in the post-process counter reconciliation so a failed enqueue releases its seeded slot instead of stranding the knowledge row in "finalizing".

#### Scenario: All parent-summary batches enqueued
- **WHEN** `parentSummaryBatchCount` batches are planned and all enqueue successfully
- **THEN** `actualOwned == plannedOwned` and no shortfall release occurs

#### Scenario: Some parent-summary batches fail to enqueue
- **WHEN** `parentSummaryBatchCount` batches are planned but some fail to enqueue
- **THEN** the shortfall (`plannedOwned - actualOwned`) slots are released via `FinalizeSubtask`, promoting the row to "completed" when the counter reaches zero

### Requirement: Parent chunks are searchable

Both `ChunkTypeParentText` (parent original text) and `ChunkTypeParentSummary` (generated summary) must be included in the `isSearchableChunk` whitelist so vector retrieval can surface them.

#### Scenario: Vector search hits parent chunk
- **WHEN** a query semantically matches a parent chunk's content or summary
- **THEN** the parent chunk appears in search results

#### Scenario: Parent-child overlap deduplication
- **WHEN** both a parent chunk and its child chunk are hit in the same search
- **THEN** `removePartialOverlaps` detects >85% content overlap and removes the child subset, keeping the parent (which provides fuller context)

### Requirement: Merge pipeline resolves parent chunks correctly

The `resolveParentChunks` stage in `chat_pipeline/merge.go` must handle four correctness invariants:

#### Scenario: Child chunk retains own content (Bug1 fix)
- **WHEN** a parent chunk is resolved for a child result
- **THEN** the child chunk keeps its own `content` field; the parent is not a full-text replacement

#### Scenario: Image chunk skips grandparent chain (Bug2 fix)
- **WHEN** an image chunk (OCR/caption) traces to a parent
- **THEN** resolution stops at the immediate parent; no grandparent chain traversal

#### Scenario: Parent-summary case uses correct lookup key (Bug3 fix)
- **WHEN** resolving a `ChunkTypeParentSummary` result
- **THEN** the parentMap lookup uses `r.ID` (the result's own ID), not the parent chunk ID

#### Scenario: Continue guard is case-scoped (Bug4 fix)
- **WHEN** a `ChunkTypeParentSummary` result is processed
- **THEN** the continue guard executes inside the case block, not before the switch, so parent-summary results are never accidentally skipped
