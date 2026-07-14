# folder-scoped-search Specification

## Purpose
TBD - created by archiving change knowledge-folder-hierarchy. Update Purpose after archive.
## Requirements
### Requirement: Folder-scoped vector recall

The retrieval pipeline SHALL accept an optional `folder_scope` parameter that restricts vector recall to chunks belonging to a specific folder and all its descendants. When `folder_scope` is null or "root", the search covers the entire KB (current behavior).

#### Scenario: Search scoped to a folder
- **WHEN** a user searches "how to configure auth" with `folder_scope = "<guides-folder-id>"`
- **AND** the "Guides" folder contains relevant auth documentation
- **THEN** only chunks from knowledge entries inside "Guides" or its descendant folders are returned in the results

#### Scenario: Search scoped to root excludes folder contents
- **WHEN** a user searches with no folder scope
- **THEN** results include knowledge from all folders and root, with no folder filtering applied

### Requirement: Folder ID enrichment on chunks

Each chunk record SHALL carry a `folder_id` metadata field derived from its parent knowledge entry at chunk creation time. This field enables vector-store-level metadata filtering during recall without requiring a runtime join.

#### Scenario: Chunking a document inside a folder
- **WHEN** a knowledge entry with `folder_id = F` is processed into 10 chunks
- **THEN** all 10 chunk records have `folder_id = F` in their metadata

#### Scenario: Knowledge entry moved to a different folder
- **WHEN** a knowledge entry's `folder_id` is updated from F1 to F2 via move operation
- **THEN** all existing chunks for that entry have their `folder_id` metadata updated to F2

### Requirement: Folder descendant expansion for scoped search

When a folder scope is specified, the system SHALL expand it to include all descendant folder IDs using the materialized path: `SELECT id FROM knowledge_folders WHERE path LIKE '<scope_path>/%' OR id = '<scope_id>'`. This set is passed as a metadata filter to the vector recall query.

#### Scenario: Scoping to a folder with 3 levels of children
- **WHEN** folder "Projects" (path `/Projects`) has children "Projects/A", "Projects/B", "Projects/A/x"
- **AND** search is scoped to "Projects"
- **THEN** the folder ID set includes all 4 folder IDs (Projects, A, B, A/x)

### Requirement: Folder metadata in search results

Each search result SHALL include the folder path (e.g. `/Guides/Go/API`) of the source knowledge entry. Results with `folder_id = NULL` show an empty or "Root" folder path.

#### Scenario: Result from a nested folder
- **WHEN** a search result originates from a knowledge entry in folder `/Guides/Go/API`
- **THEN** the result object includes `folder_path: "/Guides/Go/API"` and `folder_id: "<uuid>"`

#### Scenario: Result from root
- **WHEN** a search result originates from a root-level knowledge entry
- **THEN** the result object includes `folder_path: ""` (or null) and `folder_id: null`

### Requirement: UI folder-scoped search toggle

The knowledge base UI SHALL provide a toggle or contextual action to restrict the search to the currently browsed folder and its descendants. When toggled on, the search query includes the current folder as `folder_scope`.

#### Scenario: Searching within a folder
- **WHEN** a user navigates to folder "Guides" and enables "search in this folder"
- **THEN** subsequent searches include `folder_scope = "<guides-folder-id>"` in the request
- **AND** results are limited to documents in "Guides" or its subfolders

#### Scenario: Clearing folder scope
- **WHEN** the user disables "search in this folder"
- **THEN** subsequent searches omit `folder_scope` and search the entire KB

