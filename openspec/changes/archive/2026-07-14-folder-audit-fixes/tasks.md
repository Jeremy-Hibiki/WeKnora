## 1. HIGH: Grid view move-to-folder (frontend)

- [x] 1.1 Add `folderTreePresent` prop and `@move-folder` emit to `DocumentCardView.vue`
- [x] 1.2 Pass `:folder-tree-present` and `@move-folder` from `DocumentCardView` to `DocumentActionMenu`
- [x] 1.3 Add `:folder-tree-present` and `@move-folder` to `<DocumentCardView>` in `KnowledgeBase.vue` grid template
- [x] 1.4 Add `move-folder` branch to `handleCardAction` in `KnowledgeBase.vue`
- [x] 1.5 Add `'move-folder'` to `handleCardAction` action type union

## 2. HIGH: Folder rename (frontend)

- [x] 2.1 Add "Rename" item to folder card ⋯ menu in `KnowledgeBase.vue` grid template
- [x] 2.2 Add rename action to list-view folder actions in `DocumentListView.vue`
- [x] 2.3 Create `handleRenameFolder(folder)` handler: set `folderDialogMode='edit'`, `currentEditFolder=folder`, open dialog
- [x] 2.4 Verify `FolderManageDialog` edit mode works with `currentEditFolder`

## 3. HIGH: MoveFolder descendant depth check (backend)

- [x] 3.1 In `MoveFolder` service, before move: query `SELECT MAX(depth) FROM knowledge_folders WHERE path LIKE oldPath || '%'`
- [x] 3.2 Compute `maxRelativeDepth = maxDescendantDepth - folder.Depth`
- [x] 3.3 If `newDepth + maxRelativeDepth > MaxFolderDepth`, return `ErrMaxDepthExceeded`
- [x] 3.4 Add test: move deep subtree to deeper parent → reject

## 4. HIGH: BatchMove IDOR fix (backend)

- [x] 4.1 In `BatchMoveKnowledgeToFolder` handler: validate every knowledge ID belongs to caller's KB (batch GetKnowledge + ownership check)
- [x] 4.2 Change `BatchUpdateKnowledgeFolderID` repo signature to accept `tenantID`
- [x] 4.3 Scope the UPDATE: `WHERE tenant_id = ? AND id IN ?`
- [x] 4.4 Update interface in `types/interfaces/knowledge.go`
- [x] 4.5 Add test: batch move with cross-tenant ID → reject

## 5. MEDIUM: allSelected indeterminate (frontend)

- [x] 5.1 In `DocumentListView.vue`: filter `items.filter(i => !i.isFolder)` in `allSelected` and `someSelected` computeds

## 6. MEDIUM: Manual knowledge folderId (frontend + backend)

- [x] 6.1 In `KnowledgeBase.vue` `handleManualCreate`: pass `folderId: currentFolderId.value` to `openManualEditor`
- [x] 6.2 In `stores/ui.ts`: add `manualEditorFolderId`, `folderId` option, and reset on close
- [x] 6.3 In `manual-knowledge-editor.vue`: read `uiStore.manualEditorFolderId` and include `folder_id` in payload
- [x] 6.4 In `api/knowledge-base/index.ts` `createManualKnowledge`: add optional `folder_id` param
- [x] 6.5 In `types/knowledge.go`: add `FolderID` to `ManualKnowledgePayload`
- [x] 6.6 In `service/knowledge_create.go`: set `FolderID: payload.FolderID` on the new `Knowledge`
- [x] 6.7 In `handler/knowledge.go`: validate folder ownership in `CreateManualKnowledge`

## 7. MEDIUM: UpdateFolder ownership-first (backend)

- [x] 7.1 In `UpdateFolder` handler: call `GetFolder` first, check KB ownership, then `UpdateFolder`

## 8. MEDIUM: CreateFolder atomicity (backend)

- [x] 8.1 In `CreateFolder` service: pre-generate UUID, compute full path, single Create call (remove second Update)

## 9. MEDIUM: ForceDeleteSubtree doc fix (backend)

- [x] 9.1 Update Swagger/route doc comments to reflect re-parent-to-root behavior

## 10. MEDIUM: Folder upload size limit (backend)

- [x] 10.1 In `uploadFolder` handler: check `file.Size > maxSize` per file before processing

## 11. MEDIUM: Zip bomb guard (backend)

- [x] 11.1 In `extractZipToEntries`: track cumulative decompressed bytes, abort if > 1GB
- [x] 11.2 Pre-flight each entry with `UncompressedSize64` to OOM before reading

## 12. LOW: Dead code + i18n (frontend)

- [x] 12.1 Remove unreachable `handleMoveFolderConfirm` branches (Case 2, Case 3)
- [x] 12.2 Replace hardcoded Chinese strings with i18n keys
- [x] 12.3 Add Korean translations for `moveToFolderSuccess`/`moveToFolderFailed` in `ko-KR.ts`

## 13. Verification

- [x] 13.1 `go build ./...` passes
- [x] 13.2 `go vet ./internal/...` passes (after fixing pre-existing unrelated `custom_agent_api_key_scope_test.go` signature mismatch)
- [x] 13.3 `pnpm exec vue-tsc --noEmit` passes
- [x] 13.4 `pnpm exec vite build` passes
- [x] 13.5 Run existing folder tests: `go test ./internal/application/service/ -run TestFolder` — PASS
- [x] 13.6 Run existing folder repo tests: `go test ./internal/application/repository/ -run TestKnowledge` — PASS
