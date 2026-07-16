## Context

WeKnora has two orthogonal document-organization axes:

- **Folders** (`knowledge_folders`, materialized path): live membership, denormalized onto chunks as `folder_id` for vector-store metadata filtering. Moving a doc out of a folder instantly changes retrieval scope.
- **Tags** (`knowledge_tag_relations`, M:N join table): sticky on the document, resolved to knowledge IDs at query time via `ListKnowledgeIDsByTagIDs` SQL join. Never reach the vector engine for document KBs.

Both primitives have full CRUD. The gap: no operation connects them. To tag 200 documents in a folder today, a user must select all 200 manually and run `UpdateKnowledgeTagBatch` (which loops per-document, calling `SetKnowledgeTags` — full replace — on each).

Existing infrastructure to compose from:
- `ListKnowledgeIDsByFolderIDs(tenantID, kbID, folderIDs, recursive)` — folder→doc expansion, already supports recursive via materialized path
- `knowledge_tag_relations` — `(knowledge_id, tag_id)` composite PK, supports `INSERT ... ON CONFLICT DO NOTHING` for idempotent add
- `KnowledgeFolderHandler` — handler with folder-scoped routes under `/knowledge-bases/:id/folders/`
- `TagEditDialog.vue` — reusable tag picker with search/create, already used in batch mode

## Goals / Non-Goals

**Goals:**
- Add/remove document tags recursively across a folder subtree in a single operation
- Use the folder purely as a selector — write tags to `knowledge_tag_relations`, no new entities
- Show affected document count before confirming (operation reshapes in-flight session retrieval scope)
- Achieve this with zero vector-store writes (document tags are purely relational)

**Non-Goals:**
- Tag inheritance — new documents added to the folder after the operation do NOT get the tag
- FAQ tag operations — FAQ tags (`chunks.tag_id`) are a different system; not in scope
- Syncing tags with datasource auto-tag (`datasource_service.go:699`) — datasource-synced files arriving post-operation carry only the datasource tag
- Upload-confirm changes — that flow already passes `tag_ids` through `UploadConfirmDialog`

## Decisions

### D1: Single SQL operation per add/remove, not per-document loop

**Decision**: Implement two new repository methods that operate in bulk:
- `AddTagToKnowledgeBatch(knowledgeIDs, tagIDs)` → `INSERT INTO knowledge_tag_relations (knowledge_id, tag_id) SELECT ... ON CONFLICT DO NOTHING`
- `RemoveTagFromKnowledgeBatch(knowledgeIDs, tagIDs)` → `DELETE FROM knowledge_tag_relations WHERE tag_id IN (...) AND knowledge_id IN (...)`

**Why not reuse `SetKnowledgeTags`**: It does full replace (delete all, insert all) per document — O(docs × tags) writes, and would wipe unrelated tags. Add/remove need set arithmetic (union/subtract), not replace.

**Why not loop `SetKnowledgeTags`**: The existing `UpdateKnowledgeTagBatch` loops it. For 200 docs that's 200 round-trips with full-delete-then-insert each. A single SQL is cheaper and atomic.

**Alternative considered**: Resolve knowledge IDs in-app then loop `SetKnowledgeTags`. Rejected — defeats the purpose of a bulk operation.

### D2: One new endpoint, POST on KB folder scope

**Decision**: `POST /api/v1/knowledge-bases/:id/folders/tag-bulk`

Request body:
```json
{
  "folder_ids": ["uuid1", "uuid2"],
  "tag_ids": ["uuidA", "uuidB"],
  "action": "add",       // or "remove"
  "recursive": true       // default true
}
```

Response:
```json
{
  "success": true,
  "data": { "affected_count": 247 }
}
```

**Why this path**: Fits the existing folder route group (`router_knowledge_folder.go`) which already has `/knowledge-bases/:id/folders/*`. Uses `OwnedKBOrAdmin()` + `KBAccessWrite("id")` guards — same as folder CRUD.

**Why POST not PATCH**: It's an action, not a resource update. Matches the `/move` action pattern on folders.

**RBAC guard**: `OwnedKBOrAdmin()` + `KBAccessWrite("id")` — same guard chain as folder CRUD (`router_knowledge_folder.go:25`). Remove mode is batch-destructive; the guard MUST NOT be weaker than existing tag endpoints. The tag-ownership validation (D7) provides an additional server-side guard independent of RBAC.

**Alternative considered**: `PUT /knowledge-bases/:id/folders/:folder_id/tags`. Rejected — the operation should accept multiple folders (cross-folder union use case), so it can't be scoped to a single `:folder_id` param.

### D3: Service method composes existing folder expansion + new bulk tag ops

**Decision**: New `TagByFolder(ctx, kbID, folderIDs, tagIDs, action, recursive)` on `knowledgeService`.

Flow:
0. **KB type guard**: reject with `400` if KB type is not `KnowledgeBaseTypeDocument` (see D4). FAQ KBs use a separate tag system (`chunks.tag_id`); writing `knowledge_tag_relations` rows on FAQ entries is silently invisible at retrieval time.
1. Validate all tag IDs belong to the KB (`validateKnowledgeTagIDs` — already exists, see D5)
2. Expand folder IDs → knowledge IDs (`ListKnowledgeIDsByFolderIDs` — already exists, recursive)
3. Call `AddTagToKnowledgeBatch` or `RemoveTagFromKnowledgeBatch` (new repo methods)
4. Return `len(knowledgeIDs)` as `affected_count`

**Semantics of `affected_count`**: It is the count of knowledge entries in the expanded folder scope (step 2 output), NOT the number of rows actually inserted/deleted. For `add`, some entries may already carry the tag (`ON CONFLICT DO NOTHING` skips them); for `remove`, some entries may not have the tag (zero rows deleted). The preview count shown to the user before confirming uses the same step-2 expansion, so the number the user sees and the number returned are always consistent — both represent the *scope size*, not the *mutation row count*.

No vector store call. No re-embedding. No async task.

### D4: KB type guard — document KBs only

**Decision**: The endpoint SHALL return `400 Bad Request` if the target KB's type is not a document KB (`kb.Type != KnowledgeBaseTypeDocument`).

**Why**: FAQ tags live on `chunks.tag_id` (singular, `int64` seq_id, stored in vector store). Document tags live on `knowledge_tag_relations` (M:N, UUID string, purely relational). If the endpoint writes `knowledge_tag_relations` rows for FAQ knowledge entries, those tags are invisible at retrieval time because FAQ search resolves `RetrieveParams.TagIDs` via the vector store's `tag_id` metadata, not the join table. The user would see "tag applied" in the UI but it would have zero retrieval effect — a silent functional gap.

**Enforcement**: Check in the handler (or service) before any folder expansion or tag write. Precedent: `knowledge_faq.go:866` (`validateFAQKnowledgeBase`) does the inverse guard for FAQ-only operations.

### D5: Tag ownership validation — all tag IDs must belong to the KB

**Decision**: Before any write, validate that every `tag_id` in the request belongs to the target KB (`:id` path param). Reuse `validateKnowledgeTagIDs` (already exists, `knowledge.go:690`). Applies to both add and remove actions.

**Why**: Tags are KB-local (`KnowledgeTag.KnowledgeBaseID`). Without validation, a caller could specify tag IDs from KB A while targeting KB B's folders — cross-KB tag contamination. This is the same risk that makes the API key scope guard (`tenant_api_key.go:348`) reject `tag_ids` for KB-restricted keys. Precedent: `UpdateKnowledgeTag` (`knowledge.go:773`) calls `validateKnowledgeTagIDs` before persisting.

### D6: Frontend — context-menu action on folder nodes

**Decision**: Add a "Tag folder" action to the folder context menu / `⋯` dropdown in `KnowledgeBase.vue`. Opens a lightweight dialog (reuse `TagEditDialog` in a variant or a new small component) that:
1. Pre-fetches and shows the affected document count (`ListKnowledgeIDsByFolderIDs` or a count endpoint)
2. Shows the point-in-time warning: "Documents added to this folder later will NOT automatically receive these tags"
3. Offers add/remove mode toggle
4. Tag picker with search/create (reuse existing `TagEditDialog` patterns)
5. On confirm, calls the new API endpoint

**Why context menu not a toolbar button**: The action is inherently folder-scoped. A toolbar button would require folder selection state, which doesn't exist — the user browses *into* a folder.

### D7: Affected count preview before execution

**Decision**: The frontend fetches the count before showing the confirm button. Options:
- **Option A**: Reuse `ListKnowledgeIDsByFolderIDs` (returns IDs, take `len()`) — simplest, no new endpoint, but loads IDs into memory for large folders.
- **Option B**: New lightweight `CountKnowledgeByFolderIDs` repo method — one `SELECT COUNT(*)`, no row transfer.

**Recommendation**: Option B for correctness on large subtrees (thousands of docs). A root folder with 10 levels could contain thousands of entries; returning all IDs just to count them is wasteful.

## Risks / Trade-offs

- **[Risk] In-flight session scope change** → The confirmation dialog surfaces affected count. Add widens scope, remove narrows it. Documented in point-in-time warning text. No mitigation needed beyond transparency — this is the intended behavior.

- **[Risk] Datasource sync gap** → Documents synced into a folder after the operation won't have the tag. Documented in the confirmation dialog warning text. This is the accepted point-in-time contract, not a bug.

- **[Risk] Large folder subtree** → A root-level folder could contain thousands of docs. The single SQL operation is still fast (set arithmetic on an indexed join table), but the count preview and the SQL itself should be profiled. If needed, consider adding a `LIMIT` safety cap with a "too many documents, refine your selection" message.

- **[Trade-off] No inheritance** → Simplest model, but users must re-run the operation when adding documents to the folder. Accepted by design decision — matches `datasource_service.go` precedent where auto-tagging is also point-in-time.

- **[Trade-off] Remove is non-reversible by the operation itself** → Unlike add (idempotent via ON CONFLICT), remove silently deletes rows. The confirmation dialog with affected count is the safeguard. No undo mechanism planned.

## Open Questions

1. **Should `recursive` default to `true`?** The existing folder-scoped search uses `include_subfolders` as an explicit parameter. For tag-by-folder, recursive feels like the natural default (you're tagging "everything in this folder"), but it should be toggleable in the UI. → **Decision: default `true`, toggleable.**

2. **Should the count preview be a separate endpoint or piggyback on `ListKnowledgeIDsByFolderIDs`?** See D5 — lean toward a dedicated count method for large subtrees.
