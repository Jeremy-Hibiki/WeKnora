## Context

The `folder-scoped-search` capability currently resolves folder scopes via SQL at query time: `folder_ids → knowledge_ids` (via `ListKnowledgeIDsByFolderIDs`), then passes the expanded list as a `knowledge_id IN (...)` metadata filter to the vector engine. The original spec intended chunks to carry `folder_id` metadata natively in the vector store ("without requiring a runtime join"), but the implementation chose the indirection approach for simplicity and to avoid re-indexing on folder moves.

The system supports 10 vector engine implementations, all sharing a uniform interface (`RetrieveEngineRepository`). Two metadata-only update methods already exist as templates: `BatchUpdateChunkEnabledStatus` and `BatchUpdateChunkTagID`. The `IndexInfo` struct is the single carrier for write-side data through the indexing pipeline. The `Knowledge.FolderID` field is available at the primary IndexInfo construction site (`processChunks`) but is not propagated further.

## Goals / Non-Goals

**Goals:**
- Store `folder_id` as metadata in every vector engine so exact-folder retrieval can filter at the vector level without a SQL round-trip.
- Support metadata-only updates when knowledge entries move between folders (no re-embedding).
- Backfill existing indexed chunks with `folder_id` metadata.
- Maintain backward compatibility: the SQL resolution path remains as a fallback for `include_subfolders` descendant expansion.

**Non-Goals:**
- Storing human-readable folder paths (e.g. `/Guides/Go/API`) in vector metadata for prefix matching. The materialized path uses UUID segments, not names; path prefix filtering is a future enhancement.
- Replacing the SQL resolution entirely. Descendant expansion (`include_subfolders`) still needs SQL to resolve the folder tree to a folder_id set.
- Changing the `Chunk` GORM model. Folder membership remains on `Knowledge`, not on individual chunks.

## Decisions

### D1: Store `folder_id` only (not folder_path)

**Decision**: Store the nullable `folder_id` string (UUID or empty for root) in vector metadata.

**Alternatives considered**:
- *Store `folder_path` (materialized path) too*: would enable prefix-based subfolder queries at the vector level, eliminating the SQL round-trip for `include_subfolders`. Rejected for now because (a) the materialized path uses UUID segments (`/<uuid1>/<uuid2>/`), not human-readable names, so it's not user-facing; (b) `MoveSubtree` would require batch-updating the path metadata of every chunk in every knowledge entry under the moved subtree — potentially expensive; (c) prefix matching support varies across engines (Qdrant/ES/OS/PG support it; TencentVectorDB does not). Can be added incrementally later.

### D2: Follow the `BatchUpdateChunkTagID` template for metadata-only updates

**Decision**: Add `BatchUpdateFolderID(ctx context.Context, knowledgeFolderMap map[string]string)` where the map key is `knowledge_id` and the value is `folder_id` (empty string for root). The implementation resolves knowledge_id → chunk_ids internally (or filters by knowledge_id directly where the engine supports it).

**Rationale**: `BatchUpdateChunkTagID` already solves the same problem (metadata-only update of a string field keyed by chunk). Every engine has a working implementation we can copy. The map key is `knowledge_id` (not `chunk_id`) because `MoveToFolder` operates at the knowledge level, and resolving to chunk IDs would require a DB query we can delegate to the engine (which already iterates by knowledge_id in its `DeleteByKnowledgeIDList` path).

**Alternatives considered**:
- *Re-index on move (delete + re-insert)*: would require re-embedding, which is expensive and slow for large documents. Rejected.
- *Store folder_id on the Chunk model and resolve chunk_ids at move time*: adds a DB query to `MoveToFolder` and changes the Chunk schema unnecessarily.

### D3: Dual-path search — vector metadata filter first, SQL fallback for descendants

**Decision**: In `buildRetrievalParams`:
- If `FolderIDs` is non-empty AND `IncludeSubfolders == false`: pass `FolderIDs` as a `folder_id IN (...)` filter to `RetrieveParams` (new field), handled by each engine's base filter builder. No SQL round-trip.
- If `FolderIDs` is non-empty AND `IncludeSubfolders == true`: resolve descendants via SQL to get the full folder_id set, then pass that set as the vector metadata filter. Still one SQL round-trip, but the filter is now `folder_id IN (...)` (typically a small set of folder UUIDs) instead of `knowledge_id IN (...)` (potentially hundreds of IDs).

**Rationale**: The `folder_id` set is always bounded by the folder tree size (typically < 100), while the `knowledge_id` set can be unbounded. Even with `include_subfolders`, the vector filter is more selective.

### D4: New `RetrieveParams.FolderIDs` field

**Decision**: Add `FolderIDs []string` to `RetrieveParams` (`internal/types/retriever.go`). Each engine's base filter builder (`getBaseFilter` / `getBaseConds` / `getBaseFilterForQuery`) adds a `folder_id` condition when this field is non-empty, using the same pattern as the existing `KnowledgeIDs` filter.

**Alternatives considered**:
- *Reuse `AdditionalParams`*: less type-safe, harder to discover. Rejected.
- *Keep resolving to KnowledgeIDs in buildRetrievalParams*: defeats the purpose — we'd still have the IN-list explosion.

### D5: `FolderID` on `IndexInfo` uses empty string for root

**Decision**: `IndexInfo.FolderID` is `string` (not `*string`). Empty string means root. This matches the existing convention for `TagID` (also a plain string, empty when unset) and simplifies engine-level serialization (no nullable handling in payload/document construction).

### D6: Backfill via admin endpoint, not blocking migration

**Decision**: Provide a `POST /api/v1/admin/vector-stores/backfill-folder-metadata` endpoint that iterates KBs and calls `BatchUpdateFolderID` for each knowledge entry. The endpoint is idempotent and can be called per-KB. Auto-trigger on startup is configurable via `AUTO_BACKFILL_FOLDER_METADATA` env var (default: false).

**Rationale**: A blocking migration on startup would delay boot for large deployments. An admin endpoint gives operators control over timing. The SQL fallback path (existing `ListKnowledgeIDsByFolderIDs`) still works for any chunks that haven't been backfilled, so there's no correctness risk during the transition.

### D7: SQL-backed engine migrations add nullable `folder_id` column

**Decision**: For postgres (`embeddings` table), sqlite (`lite_embeddings` table), and doris: add `folder_id VARCHAR(36) DEFAULT ''` via a versioned migration. For non-SQL engines (Qdrant, Milvus, ES, OS, Weaviate, TencentVectorDB): no migration needed — payload/document fields are schemaless.

## Risks / Trade-offs

- **[Move consistency]** `MoveToFolder` now touches both the relational DB and the vector store. If the vector update fails after the SQL UPDATE succeeds, there's a temporary inconsistency. → *Mitigation*: log the error and continue (the SQL fallback path still produces correct results for stale metadata). Add a periodic reconciliation job as a future enhancement.

- **[Milvus re-upsert on metadata update]** Milvus lacks native partial field updates — `BatchUpdateFolderID` must read existing points, mutate the field, and re-upsert (same pattern as the existing `BatchUpdateChunkEnabledStatus` implementation). This is slower than other engines. → *Mitigation*: acceptable because folder moves are infrequent operations. The existing tag/status update path already accepts this cost.

- **[Backfill window]** Between deployment and backfill completion, some chunks lack `folder_id` metadata. Exact-folder searches using the vector metadata filter will miss these chunks. → *Mitigation*: during the transition period, `buildRetrievalParams` can check a feature flag and fall back to SQL resolution when `folder_id` metadata is not yet populated. Once backfill completes, switch to the vector-native path.

- **[Engine schema changes for Milvus]** Milvus uses typed schema columns. Adding `folder_id` as a new VARCHAR field requires recreating the collection or using Milvus's dynamic field support. → *Mitigation*: use Milvus dynamic fields (`enable_dynamic_field=true`) if available in the current client version; otherwise, add the field to the schema declaration in `NewSchema()` and handle existing collections via the backfill endpoint (which can create new collections with the updated schema during re-index).

## Open Questions

- Should the `BatchUpdateFolderID` interface method accept `knowledge_id → folder_id` mapping (batch per knowledge) or `chunk_id → folder_id` mapping (batch per chunk)? The former is more ergonomic for `MoveToFolder`; the latter is more granular. Current proposal: `knowledge_id` keyed, matching the granularity of move operations.
- For engines that shard by dimension (all collections matching `<base>_*`), the `BatchUpdateFolderID` must iterate all dimension-sharded collections. Is there a way to know which dimension a knowledge entry uses to narrow the update? (Currently no — same limitation as `BatchUpdateChunkTagID`.)
