## 1. Core Type Changes

- [x] 1.1 Add `FolderID string` field to `IndexInfo` in `internal/types/embedding.go`
- [x] 1.2 Add `FolderIDs []string` field to `RetrieveParams` in `internal/types/retriever.go`
- [x] 1.3 Add `BatchUpdateFolderID(ctx context.Context, knowledgeFolderMap map[string]string) error` to `RetrieveEngineRepository` interface in `internal/types/interfaces/retriever.go`
- [x] 1.4 Add `BatchUpdateFolderID(ctx context.Context, knowledgeFolderMap map[string]string) error` to `RetrieveEngineService` interface in `internal/types/interfaces/retriever.go`

## 2. IndexInfo Construction Sites

- [x] 2.1 Populate `FolderID` from `knowledge.FolderID` in `processChunks` (`knowledge_process.go:535`)
- [x] 2.2 Populate `FolderID` in `ProcessSummaryGeneration` (`knowledge_process.go:1166`)
- [x] 2.3 Populate `FolderID` in `ProcessParentSummaryGeneration` (`knowledge_process.go:1352`) — add FolderID to task payload or re-fetch Knowledge
- [x] 2.4 Populate `FolderID` in `processQuestionGenerationForChunks` (`knowledge_process.go:1751`) — add FolderID to task payload or re-fetch Knowledge
- [x] 2.5 Populate `FolderID` in `updateChunkVector` (`knowledge_process.go:2590`)
- [x] 2.6 Populate `FolderID` in `buildFAQIndexInfo` (`knowledge_faq.go:1865`)
- [x] 2.7 Populate `FolderID` in `incrementalIndexFAQEntry` (`knowledge_faq_import.go:1270`)
- [x] 2.8 Populate `FolderID` in multimodal image indexing (`image_multimodal.go:431`) — re-fetch Knowledge for FolderID
- [x] 2.9 Populate `FolderID` in data-table indexing (`extract.go:707`) — re-fetch Knowledge for FolderID

## 3. Engine Storage Structs & Schema

- [x] 3.1 Qdrant: add `folder_id` payload key to `QdrantVectorEmbedding`, `toQdrantVectorEmbedding`, `createPayload`, and field constant
- [x] 3.2 Postgres: add `FolderID` column to `pgVector` struct and `toDBVectorEmbedding`; write migration `000068_vector_folder_id.up/down.sql` adding `folder_id VARCHAR(36) DEFAULT ''` to `embeddings` table
- [x] 3.3 Milvus: add `folder_id` field to `MilvusVectorEmbedding`, `toMilvusVectorEmbedding`, and schema declaration in `NewSchema()` (use dynamic field if collection schema is fixed)
- [x] 3.4 Elasticsearch (shared): add `FolderID` to `VectorEmbedding` struct and `ToDBVectorEmbedding` in `elasticsearch/structs.go`
- [x] 3.5 OpenSearch: add `folder_id` to `toDoc` map in `opensearch/crud.go`
- [x] 3.6 Weaviate: add `folder_id` property to schema and `toWeaviateVectorEmbedding`; add field constant
- [x] 3.7 TencentVectorDB: add `folder_id` field constant and populate in `toVectorEmbedding`
- [x] 3.8 Doris: add `FolderID` to `DorisVectorEmbedding` struct and `toDorisVectorEmbedding`; add column to table DDL
- [x] 3.9 SQLite (Lite): add `FolderID` to `sqliteEmbedding` struct and `toSQLiteEmbedding`; auto-migrate via GORM

## 4. BatchUpdateFolderID — Per Engine

- [x] 4.1 Qdrant: implement using `client.SetPayload` with `folder_id` payload (template: `BatchUpdateChunkTagID` at `repository.go:438`)
- [x] 4.2 Postgres: implement using GORM `.Update("folder_id", ...)` filtered by `knowledge_id IN (...)` (template: `BatchUpdateChunkTagID` at `repository.go:670`)
- [x] 4.3 Milvus: implement read-filter-mutate-upsert (template: `BatchUpdateChunkTagID` at `repository.go:517`)
- [x] 4.4 Elasticsearch v8: implement using `_update_by_query` with Painless script (template: `repository.go:771`)
- [x] 4.5 Elasticsearch v7: implement using `_update_by_query` with Painless script (template: `repository.go:1394`)
- [x] 4.6 OpenSearch: implement using `_update_by_query` with Painless script (template: `bulk_update.go:47`)
- [x] 4.7 Weaviate: implement using `Updater().WithProperties()` loop (template: `repository.go:431`)
- [x] 4.8 TencentVectorDB: implement using `Collection.Update(UpdateFields)` (template: `repository.go:222`)
- [x] 4.9 Doris: implement using Stream Load partial update or read-rewrite (template: `streamload.go:234`)
- [x] 4.10 SQLite: implement using GORM `.Update("folder_id", ...)` on `lite_embeddings` (template: `repository.go:257`)

## 5. BatchUpdateFolderID — Service Layer

- [x] 5.1 Implement `BatchUpdateFolderID` in `KeywordsVectorHybridRetrieveEngineService` (delegate to repository)
- [x] 5.2 Implement `BatchUpdateFolderID` in `CompositeRetrieveEngine` (fan-out to all registered engines, template: `composite.go` `BatchUpdateChunkTagID`)

## 6. Search Path Changes

- [x] 6.1 Add `folder_id IN (...)` filter condition in Qdrant `getBaseFilter` (`repository.go:489`)
- [x] 6.2 Add `folder_id IN (...)` filter in Postgres `KeywordsRetrieve` and vector retrieve (`repository.go:181`, `repository.go:303`)
- [x] 6.3 Add `folder_id` filter expression in Milvus `getBaseFilterForQuery` (`repository.go:578`)
- [x] 6.4 Add `folder_id` terms query in Elasticsearch v8 `getBaseConds` (`repository.go:290`)
- [x] 6.5 Add `folder_id` terms query in Elasticsearch v7 (equivalent path)
- [x] 6.6 Add `folder_id` terms filter in OpenSearch `buildQuery` (`query.go:61`)
- [x] 6.7 Add `folder_id` filter in Weaviate `getFilter` (`repository.go:490`)
- [x] 6.8 Add `folder_id` In condition in TencentVectorDB retrieve (`repository.go:514`)
- [x] 6.9 Add `folder_id` IN clause in Doris query builder (`query.go:131`)
- [x] 6.10 Add `folder_id` IN clause in SQLite retrieve (`repository.go:561`)

## 7. Retrieval Orchestration

- [x] 7.1 Modify `buildRetrievalParams` (`knowledgebase_search.go:327-354`): when `FolderIDs` non-empty and `IncludeSubfolders == false`, set `RetrieveParams.FolderIDs` directly instead of resolving to KnowledgeIDs
- [x] 7.2 When `IncludeSubfolders == true`, resolve descendant folder IDs via SQL and pass the folder ID set as `RetrieveParams.FolderIDs` (instead of resolving all the way to knowledge IDs)
- [x] 7.3 Add feature flag / check for backfill status: if vector metadata not yet backfilled, fall back to SQL-resolved `knowledge_id IN (...)` path
- [x] 7.4 Remove the `mergeKnowledgeIDs` + `ListKnowledgeIDsByFolderIDs` path for the vector-native branch (keep as fallback)

## 8. Move Operation Integration

- [x] 8.1 Modify `MoveToFolder` (`knowledge.go:934`) to call `BatchUpdateFolderID` after `UpdateKnowledgeFolderID` SQL succeeds
- [x] 8.2 Modify `BatchMoveToFolder` to batch-call `BatchUpdateFolderID` with all moved knowledge IDs
- [x] 8.3 Handle `MoveSubtree` (`knowledge_folder.go`): collect all knowledge IDs under the moved subtree and call `BatchUpdateFolderID` if any folder_id assignments changed
- [x] 8.4 Log and swallow vector update errors in move operations (do not block the relational update)

## 9. Backfill Endpoint & Migration

- [x] 9.1 Add `POST /api/v1/admin/vector-stores/backfill-folder-metadata` handler in `internal/handler/` (admin-only)
- [x] 9.2 Implement backfill service: iterate KBs → iterate knowledge entries → call `BatchUpdateFolderID` per KB batch
- [x] 9.3 Register route in `internal/router/` under admin guard
- [x] 9.4 Add `AUTO_BACKFILL_FOLDER_METADATA` config flag in `config.yaml` and config struct
- [x] 9.5 Write migration `migrations/versioned/000068_vector_folder_id.{up,down}.sql` for Postgres (add column to `embeddings`)
- [x] 9.6 Add SQLite equivalent migration if needed (or rely on GORM auto-migrate)

## 10. Tests

- [x] 10.1 Unit test: `IndexInfo.FolderID` populated at every construction site
- [x] 10.2 Unit test: each engine's `to*VectorEmbedding` includes `folder_id` in the output
- [x] 10.3 Unit test: `BatchUpdateFolderID` for at least Qdrant, Postgres, SQLite (using existing test patterns)
- [x] 10.4 Unit test: `buildRetrievalParams` produces `RetrieveParams.FolderIDs` for exact-folder scope
- [x] 10.5 Unit test: `buildRetrievalParams` resolves descendants for `include_subfolders` and passes folder IDs (not knowledge IDs)
- [x] 10.6 Unit test: `MoveToFolder` calls `BatchUpdateFolderID` with correct mapping
- [x] 10.7 Integration test: search scoped to a folder returns only matching chunks after metadata is populated
- [x] 10.8 Run `go test -count=1 ./...` and `go vet ./...` — all pass
