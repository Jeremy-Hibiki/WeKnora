# folder-audit-fixes Specification

## Purpose
TBD - created by archiving change folder-audit-fixes. Update Purpose after archive.
## Requirements
### Requirement: Grid view supports move-to-folder for documents

The grid view (default) SHALL support moving documents to folders via the card action menu, matching the list view's behavior. The ⋯ menu on document cards SHALL show "Move to Folder" when the KB has folders.

#### Scenario: Grid view document card menu shows move-to-folder
- **WHEN** the user opens a document card's ⋯ menu in grid view
- **AND** the KB has at least one folder
- **THEN** the menu includes a "Move to Folder" item

#### Scenario: Grid view move-to-folder triggers folder selector
- **WHEN** the user clicks "Move to Folder" from a grid-view document card menu
- **THEN** the FolderSelector dialog opens
- **AND** selecting a folder moves the document there

### Requirement: Folder rename is accessible

The user SHALL be able to rename a folder from both grid and list views. The folder card ⋯ menu and list-view folder actions SHALL include a "Rename" option that opens FolderManageDialog in edit mode.

#### Scenario: Rename from grid folder card
- **WHEN** the user clicks ⋯ on a folder card in grid view
- **THEN** the menu includes "Rename"
- **AND** clicking it opens FolderManageDialog pre-filled with the current name

#### Scenario: Rename from list view
- **WHEN** the user clicks the rename action on a folder row in list view
- **THEN** FolderManageDialog opens in edit mode

### Requirement: MoveFolder validates descendant depth

When moving a folder, the system SHALL reject the operation if any descendant would exceed MaxFolderDepth in the new location.

#### Scenario: Moving a deep subtree
- **WHEN** folder A (depth 3) has a descendant at depth 8
- **AND** A is moved under a parent at depth 5
- **THEN** the descendant would land at depth 10 (OK)
- **AND** moving under a parent at depth 6 → descendant at depth 11 → rejected

### Requirement: BatchMove validates all knowledge IDs

BatchMoveKnowledgeToFolder SHALL validate that every knowledge ID in the request belongs to the caller's knowledge base before performing the update. The SQL UPDATE SHALL be scoped by tenant_id.

#### Scenario: Cross-tenant ID in batch
- **WHEN** a Contributor includes a knowledge ID from a different tenant in a batch move
- **THEN** the request is rejected with 403

### Requirement: UpdateFolder checks ownership before persisting

The UpdateFolder handler SHALL verify KB ownership before saving changes, matching the pattern used by Delete and Move handlers.

#### Scenario: Cross-KB folder update
- **WHEN** a user attempts to update a folder belonging to a different KB
- **THEN** the request returns 403 without persisting any changes

### Requirement: CreateFolder is atomic

CreateFolder SHALL generate the folder ID and compute the full path before the single database insert, eliminating the two-step Create+Update that can leave incomplete paths on failure.

#### Scenario: CreateFolder path integrity
- **WHEN** a folder is created
- **THEN** the path is computed as `parentPath + newID + "/"` before insert
- **AND** no second UPDATE is needed

### Requirement: Folder upload enforces per-file size limit

The folder/zip upload handler SHALL enforce the same per-file size limit as the single-file upload handler.

#### Scenario: Oversized file in folder upload
- **WHEN** a folder upload contains a file larger than MAX_FILE_SIZE_MB
- **THEN** that file is skipped
- **AND** the response indicates the file exceeded the size limit

### Requirement: Zip extraction has decompressed-size guard

The zip extraction SHALL cap total decompressed bytes to prevent zip-bomb OOM. Extraction SHALL abort if the cumulative size exceeds the limit.

#### Scenario: Zip bomb rejected
- **WHEN** a zip archive would decompress to more than the configured cap
- **THEN** extraction aborts with an error
- **AND** no memory exhaustion occurs

### Requirement: Select-all excludes folders

The "select all" checkbox computeds SHALL filter out folder items so the header checkbox correctly reflects document-only selection state.

#### Scenario: Folders present in list view
- **WHEN** the list view contains both documents and folders
- **AND** all documents are selected
- **THEN** the header checkbox shows fully checked
- **AND** folders are not counted as selected

### Requirement: Manual knowledge respects current folder

Creating manual knowledge while browsing inside a folder SHALL assign the knowledge to that folder, not the KB root.

#### Scenario: Manual knowledge inside folder
- **WHEN** the user is browsing inside a folder
- **AND** they create manual knowledge
- **THEN** the new knowledge is assigned to the current folder
- **AND** it appears inside that folder after refresh

