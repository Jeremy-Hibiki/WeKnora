## 1. Backend Repository Layer

- [ ] 1.1 Add `AddTagToKnowledgeBatch(ctx, knowledgeIDs, tagIDs)` to `KnowledgeRepository` interface (`internal/types/interfaces/knowledge.go`) and implementation (`internal/application/repository/knowledge.go`). SQL: `INSERT INTO knowledge_tag_relations (knowledge_id, tag_id) SELECT ... ON CONFLICT DO NOTHING`.
- [ ] 1.2 Add `RemoveTagFromKnowledgeBatch(ctx, knowledgeIDs, tagIDs)` to the same interface and implementation. SQL: `DELETE FROM knowledge_tag_relations WHERE tag_id IN (...) AND knowledge_id IN (...)`.
- [ ] 1.3 Add `CountKnowledgeByFolderIDs(ctx, tenantID, kbID, folderIDs, recursive)` to interface and implementation. SQL: `SELECT COUNT(*) FROM knowledge WHERE folder_id IN (...) AND deleted_at IS NULL`. Reuse the folder-descendant expansion logic from `ListKnowledgeIDsByFolderIDs`.
- [ ] 1.4 Write repository tests: add (new + idempotent), remove (existing + no-op), count (recursive/non-recursive), overlapping folder IDs (ancestor+descendant with recursive=true → no double-count).

## 2. Backend Service Layer

- [ ] 2.1 Add `TagByFolder(ctx, kbID, folderIDs, tagIDs, action string, recursive bool) (affectedCount int64, err error)` to `KnowledgeService` interface (`internal/types/interfaces/knowledge.go`).
- [ ] 2.2 Implement `TagByFolder` on `knowledgeService` (`internal/application/service/knowledge.go`): (1) KB type guard → 400 if not `KnowledgeBaseTypeDocument`, (2) `validateKnowledgeTagIDs` (tag ownership), (3) `ListKnowledgeIDsByFolderIDs` (recursive expansion), (4) add or remove per action, (5) return `len(knowledgeIDs)` as affected count.
- [ ] 2.3 Write service tests: document KB happy path (add + remove), FAQ KB rejection, cross-KB tag rejection, affected_count = scope size not mutation count, empty folder → affected_count 0.

## 3. Backend Handler + Routing

- [ ] 3.1 Add `TagByFolder` handler method on `KnowledgeFolderHandler` (`internal/handler/knowledge_folder.go`): parse request DTO `{ folder_ids, tag_ids, action, recursive }`, call service, return `{ affected_count }` envelope.
- [ ] 3.2 Add request/response DTO types (in handler or `handler/dto/`).
- [ ] 3.3 Register route `POST /knowledge-bases/:id/folders/tag-bulk` in `router_knowledge_folder.go` with `OwnedKBOrAdmin()` + `KBAccessWrite("id")` guards.
- [ ] 3.4 Add Swagger annotations (`@Summary`, `@Router`, etc.) matching the existing folder handler style.

## 4. Frontend

- [ ] 4.1 Add API function `tagByFolder(kbId, { folder_ids, tag_ids, action, recursive })` in `frontend/src/api/knowledge-folder/index.ts`.
- [ ] 4.2 Add affected-count preview API call (reuse `CountKnowledgeByFolderIDs` or `ListKnowledgeIDsByFolderIDs`).
- [ ] 4.3 Add "Tag folder" action to folder context menu / `⋯` dropdown in `KnowledgeBase.vue`.
- [ ] 4.4 Create or adapt a tag-by-folder dialog component: affected count preview, add/remove mode toggle, recursive toggle (default on — without it the `recursive: false` spec scenario is unreachable in the UI), tag picker (reuse `TagEditDialog` patterns), point-in-time warning text.
- [ ] 4.5 Wire confirm → `tagByFolder` API call → refresh knowledge list.
- [ ] 4.6 Add i18n strings (zh-CN, en-US, ru-RU, ko-KR) for: action label, dialog title, warning text, affected-count display, add/remove toggle labels.

## 5. Verification

- [ ] 5.1 Backend: `go test ./internal/application/service/... ./internal/application/repository/... ./internal/handler/...` — all new and existing tests pass.
- [ ] 5.2 Frontend: `cd frontend && pnpm run type-check` — no new type errors.
- [ ] 5.3 Manual smoke test: tag a folder subtree (recursive), verify tags appear on all docs, verify retrieval by tag returns them, then remove and verify they disappear.
- [ ] 5.4 Edge case: tag folder, then add a new doc to it, verify the new doc does NOT have the tag (point-in-time contract).
