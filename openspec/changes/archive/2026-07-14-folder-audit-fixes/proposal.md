## Why

Post-rebase audit of the knowledge folder feature found 4 HIGH-severity bugs (two frontend regressions, two backend data-integrity/security issues), 7 MEDIUM issues, and several LOW issues. These span folder management, upload, search, and UI — all introduced or exposed by the rebase onto dev. Fixing them now ensures the folder feature is production-ready.

## What Changes

- **Grid view document→folder move**: Wire `folderTreePresent` prop and `@move-folder` emit through `DocumentCardView` → `DocumentActionMenu` → `KnowledgeBase.handleCardAction`, matching the list view's working implementation.
- **Folder rename**: Add "Rename" to the folder card ⋯ menu and list-view folder actions; wire `folderDialogMode='edit'` + `currentEditFolder` to `FolderManageDialog`.
- **MoveFolder descendant depth check**: Compute max descendant depth before move; reject if `newDepth + maxRelativeDepth > MaxFolderDepth`.
- **BatchMoveKnowledgeToFolder IDOR**: Validate every knowledge ID belongs to the caller's KB before batch update; scope the UPDATE by tenant_id.
- **allSelected indeterminate fix**: Filter folders from select-all computeds.
- **Manual knowledge folderId**: Pass `folder_id` to `createManualKnowledge`.
- **UpdateFolder ownership-first**: Check KB ownership before persisting changes.
- **CreateFolder atomicity**: Pre-generate ID + path in a single Create, matching `EnsureFolderPath` pattern.
- **ForceDeleteSubtree doc fix**: Update API docs to reflect re-parent-to-root behavior.
- **Folder upload size limit**: Enforce per-file size check in folder/zip upload loop.
- **Zip bomb guard**: Cap total decompressed bytes during zip extraction.
- **Dead code + i18n**: Remove unreachable `handleMoveFolderConfirm` branches; replace hardcoded Chinese strings with i18n.

## Capabilities

### Modified Capabilities
- `knowledge-folder-management`: Folder rename UI, move depth validation, UpdateFolder ownership-first, CreateFolder atomicity
- `folder-upload`: Per-file size limit, zip bomb guard
- `folder-scoped-search`: BatchMove IDOR fix, allSelected indeterminate fix, manual knowledge folderId

## Impact

- **Frontend**: `KnowledgeBase.vue`, `DocumentCardView.vue`, `DocumentActionMenu.vue`, `DocumentListView.vue`, `FolderManageDialog.vue`, `api/knowledge-base/index.ts`
- **Backend**: `knowledge_folder.go` (service), `knowledge_folder_ops.go` (handler), `knowledge.go` (repository + service), `knowledge_folder_upload.go`, `knowledge_folder_upload_helpers.go`
- **Security**: BatchMove IDOR fix prevents cross-tenant folder_id tampering
