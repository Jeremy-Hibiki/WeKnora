## ADDED Requirements

### Requirement: Folder hierarchy model

The system SHALL store knowledge folders in a hierarchical structure using an adjacency list (`parent_folder_id`) with a denormalized materialized `path` and `depth` column. Each folder belongs to exactly one knowledge base and one tenant. The `path` column stores the full slash-delimited path from root (e.g. `/Guides/Go/API`). Root-level folders have `parent_folder_id = NULL` and `depth = 0`.

#### Scenario: Folder with nested children
- **WHEN** a folder "API" is created inside "Go" inside "Guides" inside KB "docs"
- **THEN** the "API" folder has `depth = 2`, `path = /Guides/Go/API`, and `parent_folder_id` pointing to the "Go" folder

#### Scenario: Folder names are unique per parent
- **WHEN** a folder "Reports" already exists under parent "Finance"
- **AND** a new folder "Reports" is created under the same parent
- **THEN** the creation is rejected with a conflict error

### Requirement: Folder depth limit

The system SHALL enforce a maximum nesting depth of 10 levels. Folder creation or move that would result in depth > 10 SHALL be rejected with a validation error.

#### Scenario: Exceeding depth limit
- **WHEN** a folder is created at depth 10
- **AND** an attempt is made to create a child folder under it
- **THEN** the creation is rejected with error "maximum folder depth (10) exceeded"

### Requirement: Folder name sanitization

Folder names SHALL be limited to 255 characters and MUST NOT contain forward slashes (`/`), backslashes (`\`), or null bytes. Names are sanitized before insertion.

#### Scenario: Invalid characters in folder name
- **WHEN** a folder is created with name "Q1/Reports"
- **THEN** the creation is rejected with a validation error

### Requirement: Folder CRUD operations

The system SHALL provide endpoints to create, read (single + tree), update, delete, and move folders. All operations require the caller to be at least a Viewer (read) or KB Owner/Admin (write) for the target knowledge base.

#### Scenario: Creating a root folder
- **WHEN** a KB Owner sends `POST /api/v1/knowledge-bases/:id/folders` with body `{ "name": "Guides" }`
- **THEN** a folder is created with `parent_folder_id = NULL`, `depth = 0`, `path = /Guides`

#### Scenario: Creating a nested folder
- **WHEN** a KB Owner sends `POST /api/v1/knowledge-bases/:id/folders` with body `{ "name": "API", "parent_folder_id": "<guides-folder-id>" }`
- **THEN** the folder is created under the specified parent with `depth = parent.depth + 1` and `path = parent.path + "/" + name`

#### Scenario: Reading the folder tree
- **WHEN** a Viewer sends `GET /api/v1/knowledge-bases/:id/folders/tree`
- **THEN** the response contains the complete folder hierarchy as a nested tree structure, including all non-deleted folders

#### Scenario: Deleting a non-empty folder
- **WHEN** a KB Owner sends `DELETE /api/v1/knowledge-bases/:id/folders/:folder_id` on a folder containing subfolders or knowledge entries
- **AND** `force` is not set to true
- **THEN** the deletion is rejected with error "folder is not empty; use force=true to delete"

#### Scenario: Force-deleting a folder with contents
- **WHEN** a KB Owner sends `DELETE /api/v1/knowledge-bases/:id/folders/:folder_id?force=true` on a folder containing subfolders
- **THEN** all descendant folders are cascade-deleted, and knowledge entries have their `folder_id` set to NULL (moved to root)

### Requirement: Folder move with cascade path rewrite

The system SHALL support moving a folder to a new parent. The `path` and `depth` of the moved folder and all its descendants SHALL be updated atomically in a single transaction using materialized path prefix replacement.

#### Scenario: Moving a folder with children
- **WHEN** folder "API" (path `/Guides/Go/API`) with child "v2" (path `/Guides/Go/API/v2`) is moved to parent "Docs" (path `/Docs`)
- **THEN** "API" path becomes `/Docs/API` and "v2" path becomes `/Docs/API/v2`

#### Scenario: Preventing move into own descendant
- **WHEN** a folder is moved into one of its own descendants
- **THEN** the move is rejected with error "cannot move folder into its own subtree"

### Requirement: Tenant and KB ownership validation

Every folder operation SHALL validate that the folder belongs to the same tenant and knowledge base as the caller. A folder ID from a different tenant or KB SHALL be rejected with a 403 error.

#### Scenario: Cross-tenant folder access
- **WHEN** tenant A attempts to read a folder belonging to tenant B
- **THEN** the request returns 403 Forbidden

### Requirement: Breadcrumb path retrieval

The system SHALL provide an endpoint to retrieve the breadcrumb path (root → ... → current folder) for a given folder, returned as an ordered list of `{ id, name }`.

#### Scenario: Breadcrumb for nested folder
- **WHEN** `GET /api/v1/knowledge-bases/:id/folders/:folder_id/breadcrumb` is called for folder "API" at path `/Guides/Go/API`
- **THEN** the response contains `[{ "id": "...", "name": "Guides" }, { "id": "...", "name": "Go" }, { "id": "...", "name": "API" }]`

### Requirement: Folder-scoped knowledge listing

The system SHALL support listing knowledge entries filtered by folder. When `folder_id` is specified, only direct children of that folder are returned. When `folder_id` is not specified (root), only root-level entries (`folder_id IS NULL`) are returned.

#### Scenario: Listing root-level documents
- **WHEN** `GET /api/v1/knowledge-bases/:id/knowledge` is called without `folder_id`
- **THEN** only knowledge entries with `folder_id IS NULL` are returned

#### Scenario: Recursive folder listing
- **WHEN** `ListPagedKnowledgeByFolderID` is called with `recursive = true`
- **THEN** knowledge entries from the folder and all descendant subfolders are returned
