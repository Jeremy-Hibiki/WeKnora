## Context

Post-rebase audit found 4 HIGH, 7 MEDIUM, and several LOW issues across the folder feature. The rebase merged dev's component refactoring (DocumentCardView, DocumentActionMenu) with the feature branch's folder logic, creating asymmetries between grid and list views, plus backend data-integrity gaps.

## Goals / Non-Goals

**Goals:**
- Fix all 4 HIGH issues: grid move-to-folder, folder rename, MoveFolder depth check, BatchMove IDOR
- Fix all 7 MEDIUM issues: allSelected, manual folderId, UpdateFolder ownership, CreateFolder atomicity, doc fix, upload size, zip bomb
- Clean up LOW issues: dead code, hardcoded strings

**Non-Goals:**
- Redesigning folder UI or navigation
- Changing the materialized-path folder model
- Adding new folder features

## Decisions

### D1: Grid move-to-folder via prop passthrough
Mirror the list view's working pattern: add `folderTreePresent` prop and `@move-folder` emit to `DocumentCardView`, which passes them to `DocumentActionMenu`. Add `move-folder` to `handleCardAction` in `KnowledgeBase.vue`. This is a 3-file prop-chain fix, no architectural change.

### D2: Folder rename via existing dialog
`FolderManageDialog` already supports edit mode. Add "Rename" item to folder card ⋯ menu and list-view folder actions. Set `folderDialogMode='edit'` + `currentEditFolder=folder` then open the dialog. No new component needed.

### D3: MoveFolder descendant depth via max-relative-depth
Before move, query the subtree's max depth relative to the moved folder (`maxDescendantDepth - folder.Depth`). If `newDepth + maxRelativeDepth > MaxFolderDepth`, reject. Single query: `SELECT MAX(depth) - ? FROM knowledge_folders WHERE path LIKE ? || '%'`.

### D4: BatchMove tenant scoping in repository
Change `BatchUpdateKnowledgeFolderID` to accept `tenantID` and scope the UPDATE: `WHERE tenant_id = ? AND id IN ?`. Handler validates all knowledge IDs belong to the caller's KB before calling.

### D5: UpdateFolder ownership-first
Swap the order in handler: `GetFolder` → check `folder.KnowledgeBaseID == kbID` → then `UpdateFolder`. Same pattern as Delete/Move handlers.

### D6: CreateFolder pre-generate ID
Generate UUID before Create, compute path from parent path + new ID, single `Create` call. Remove the second `Update`. Matches `EnsureFolderPath` pattern.

### D7: Zip bomb guard
Cap total decompressed bytes at a constant (e.g. 1GB). Track cumulative `io.ReadAll` size; abort if exceeded.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| D3 extra query on every move | Move is rare; single MAX query is O(subtree), bounded by depth limit |
| D4 changes BatchUpdateKnowledgeFolderID signature | Only one caller; interface update is mechanical |
| D6 changes CreateFolder path generation | EnsureFolderPath already uses this pattern; tested |
| D7 zip cap may reject legitimate large archives | 1GB is generous; configurable via constant |
