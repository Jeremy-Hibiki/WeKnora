## Why

Folders and tags are two orthogonal organization axes in WeKnora, but today the only way to tag many documents is to manually select them one by one. A user who has organized 200 documents into a `Reports/` subtree must click-select all 200 to apply a tag — then re-do it if the tag changes. There is no "tag everything in this folder" action, even though all the primitives exist: `ListKnowledgeIDsByFolderIDs` (recursive folder→doc expansion) and `knowledge_tag_relations` (M:N doc tags).

The value of such an action is twofold, grounded in how tags differ from folders at retrieval time:

1. **Temporal stability (time axis)**: folder scope is *live membership* — `folder_id` is denormalized onto chunks, so moving a doc out of a folder instantly removes it from that folder's retrieval scope. Tag scope is a *snapshot* — a tag sticks to the document through folder moves until explicitly removed. A user who wants a retrieval scope that survives reorganization needs tags, not folder selection.

2. **Named cross-folder union (space axis)**: the same tag can be stamped onto multiple folders (Q1/ and Q2/), producing a named, @mention-completable union of 89 documents. Folder multi-select can also be pinned (`session.go:208` carries `FolderIDs`), but it is a bag of UUIDs with no name, no flat namespace, and no autocomplete entry. Tags are the only first-class named scope.

## What Changes

- **New bulk action: tag-by-folder add**. Given a folder (or a set of folders) and one or more tags, add those tags to every knowledge entry in the folder subtree (recursive by default). Implemented as a single `INSERT ... ON CONFLICT DO NOTHING` against `knowledge_tag_relations` after expanding folder IDs to knowledge IDs — no per-document loop, no re-embedding, no vector store update.
- **New bulk action: tag-by-folder remove**. Given a folder subtree and one or more tags, remove those tag associations from all knowledge entries in the subtree. Implemented as a single `DELETE ... WHERE knowledge_id IN (subquery)`. This is a new primitive — today only `SetKnowledgeTags` exists (full replace), which would wipe unrelated tags.
- **UI affordance on folder nodes**: a context-menu action ("Add tags to folder" / "Remove tags from folder") opening a tag-picker dialog that shows the **affected document count** before confirming. The count exists because the operation reshapes retrieval scope for in-flight sessions that have pinned these tags.
- **No new entities, no inheritance, no new tables.** The folder is used purely as a selector; tags are written directly to `knowledge_tag_relations`. Documents added to the folder after the operation do NOT inherit the tag (point-in-time semantics, matching the user's explicit choice).

> **Out of scope / verified existing**: Upload-confirm batch tagging (folder upload, zip archive upload, multi-file upload) already works end-to-end — `UploadConfirmDialog` (mode `'file'`) has a tag section, and both `uploadKnowledgeFolder` / `uploadKnowledgeZip` forward `tag_ids`. This change does not touch that flow.

## Capabilities

### New Capabilities

- `folder-tag-bulk-action`: Bulk add/remove document tags scoped to a folder subtree. Covers the recursive folder→knowledge expansion, the join-table add/remove operations, the affected-document-count confirmation surface, and the UI context-menu entry on folder nodes.

### Modified Capabilities

<!-- None. This change adds a new operation on top of existing folder and tag
     infrastructure without altering any existing requirement. -->

## Impact

- **Backend (Go)**: New service method (e.g. `TagByFolder`) on `knowledgeService` that composes `ListKnowledgeIDsByFolderIDs` (already exists, recursive) with new repository-level bulk-add and bulk-remove against `knowledge_tag_relations`. New repository methods needed: `AddTagToKnowledgeBatch` (INSERT ON CONFLICT) and `RemoveTagFromKnowledgeBatch` (DELETE WHERE IN). No changes to retriever/vector-store layer — document tags resolve to knowledge IDs at query time and never reach the engine.
- **API**: One new endpoint (e.g. `POST /api/v1/knowledge-bases/{kbId}/folders/tag-bulk`) accepting `{ folder_ids: [], tag_ids: [], action: "add"|"remove", recursive: true }`. Returns `{ affected_count: N }`.
- **Frontend (Vue 3)**: Context-menu action on folder nodes in `KnowledgeBase.vue`; reuse existing `TagEditDialog` (or a lightweight variant) in a mode that shows affected count + the point-in-time warning. i18n strings for the action label, warning, and confirmation.
- **Externalities**: In-flight sessions/agents that have pinned the affected tags (`session.go:207`, `message.go:249`, agent `SearchTarget.TagIDs`) will see their retrieval scope change immediately — add widens, remove narrows. The confirmation dialog surfaces the affected document count for this reason.
- **Not affected**: FAQ tags (`chunks.tag_id`, `[]int64`), vector store metadata, embedding, retrieval engine. This change is scoped to document KBs only.
- **Edge case to document**: Datasource-synced documents arriving after a folder-tag operation will carry only the datasource auto-tag (`datasource_service.go:699`), not the folder tag. This is the point-in-time contract in action — the confirmation text should mention it.
