## Why

Folder-scoped vector retrieval currently works by resolving `folder_ids → knowledge_ids` via a SQL round-trip on every search, then passing the expanded list as a `knowledge_id IN (...)` filter to the vector engine. This adds latency, produces potentially huge IN-lists for broad folder scopes, and diverges from the original `folder-scoped-search` spec which states that chunks "SHALL carry a `folder_id` metadata field… enabling vector-store-level metadata filtering during recall without requiring a runtime join." Storing `folder_id` directly in vector store metadata eliminates the SQL round-trip for exact-folder queries and aligns the implementation with its specification.

## What Changes

- Add `FolderID` (nullable string) to `IndexInfo` so it flows through the indexing write path into every vector engine's point/document metadata.
- Add `folder_id` field/payload key to all 10 supported engine storage structs and their `to*VectorEmbedding` conversion functions.
- Add a `BatchUpdateFolderID(ctx, knowledgeFolderMap map[string]string)` method to `RetrieveEngineRepository` and `RetrieveEngineService` interfaces (mirrors the existing `BatchUpdateChunkTagID` pattern) for metadata-only updates without re-embedding.
- Implement `BatchUpdateFolderID` in all engine packages: qdrant, postgres, milvus, elasticsearch v7/v8, opensearch, weaviate, tencentvectordb, doris, sqlite.
- Modify `buildRetrievalParams` to prefer vector-level `folder_id` metadata filtering when no `include_subfolders` expansion is needed; fall back to SQL resolution for descendant expansion.
- Wire `MoveToFolder` / `BatchMoveToFolder` to call `BatchUpdateFolderID` after the SQL UPDATE so vector metadata stays consistent.
- Add a background migration / admin endpoint to backfill `folder_id` metadata for existing indexed chunks.

## Capabilities

### New Capabilities

_(none — this is an enhancement to folder-scoped-search)_

### Modified Capabilities

- `folder-scoped-search`: The retrieval mechanism changes from SQL-resolved `knowledge_id IN (...)` filtering to direct `folder_id` metadata filtering at the vector store level for exact-folder scopes. Move operations must now update vector metadata in addition to the relational `folder_id` column.

## Impact

- **Vector engine repositories** (10 packages under `internal/application/repository/retriever/`): each needs a new field on its point struct, a new payload/column in its schema, and a `BatchUpdateFolderID` implementation.
- **`IndexInfo`** (`internal/types/embedding.go`): gains a `FolderID` field; every construction site (~12 across `knowledge_process.go`, `knowledge_faq.go`, `extract.go`, `image_multimodal.go`, etc.) must populate it.
- **Retriever interfaces** (`internal/types/interfaces/retriever.go`): new `BatchUpdateFolderID` method on `RetrieveEngineRepository` and `RetrieveEngineService`.
- **Knowledge move operations** (`internal/application/service/knowledge.go`, `knowledge_folder.go`): `MoveToFolder`, `BatchMoveToFolder`, and `MoveSubtree` must trigger `BatchUpdateFolderID` for affected knowledge entries.
- **Search path** (`internal/application/service/knowledgebase_search.go`): `buildRetrievalParams` gains a new branch for vector-level folder filtering.
- **Migrations**: for SQL-backed engines (postgres, sqlite, doris), a new migration adds the `folder_id` column to the embeddings table. For non-SQL engines, no migration is needed (schemaless payload).
- **Backfill**: a one-time background job or admin API endpoint to populate `folder_id` metadata for existing indexed chunks across all KBs.
