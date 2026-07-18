# folder-scoped-search Specification

## Purpose
TBD - created by archiving change knowledge-folder-hierarchy. Update Purpose after archive.
## Requirements
### Requirement: Folder-scoped vector recall

The retrieval pipeline SHALL accept an optional `folder_scope` parameter that restricts vector recall to chunks whose vector-store `folder_id` metadata matches the specified folder scope. When `include_subfolders` is false, the `folder_id` set is passed directly as a metadata filter to the vector engine's base query — no SQL round-trip to resolve folder IDs to knowledge IDs is required. When `folder_scope` is null or empty, the search covers the entire KB with no folder filtering applied.

#### Scenario: Exact-folder search uses vector metadata filter
- **WHEN** a user searches "how to configure auth" scoped to folder `F` with `include_subfolders = false`
- **THEN** the vector engine receives `folder_id IN ("F")` as a metadata pre-filter
- **AND** no SQL query to resolve folder IDs to knowledge IDs is executed

#### Scenario: Search scoped to root excludes folder contents
- **WHEN** a user searches with no folder scope
- **THEN** results include knowledge from all folders and root, with no folder filtering applied

#### Scenario: Fallback during backfill transition
- **WHEN** the `folder_id` metadata backfill has not been completed for a KB
- **AND** exact-folder search is performed
- **THEN** the system SHALL fall back to SQL-resolved `knowledge_id IN (...)` filtering to ensure correctness until backfill completes

#### Scenario: LLM-initiated folder-scoped search
- **WHEN** the LLM calls `knowledge_search` with `folders: ["Go"]`
- **THEN** the backend resolves "Go" → folder UUID(s) within the KB
- **AND** passes them as `SearchParams.FolderIDs` to the retrieval pipeline
- **AND** the vector engine applies `folder_id IN (...)` metadata filtering

#### Scenario: LLM-initiated search with no folder scope
- **WHEN** the LLM calls `knowledge_search` without the `folders` parameter
- **THEN** no folder filtering is applied and all chunks in the KB scope are searchable

### Requirement: Folder ID enrichment on chunks

Each vector-store point/document SHALL carry a `folder_id` metadata field derived from its parent knowledge entry at indexing time. The field is populated via `IndexInfo.FolderID`, which is set from `Knowledge.FolderID` (empty string for root-level entries) at every `IndexInfo` construction site. This field enables vector-store-level metadata filtering during recall without requiring a runtime join.

#### Scenario: Indexing a document inside a folder
- **WHEN** a knowledge entry with `folder_id = F` is processed into 10 chunks and indexed
- **THEN** all 10 vector-store points have `folder_id = "F"` in their payload/metadata

#### Scenario: Indexing a document at root level
- **WHEN** a knowledge entry with `folder_id = NULL` is processed into chunks and indexed
- **THEN** all vector-store points have `folder_id = ""` (empty string) in their payload/metadata

#### Scenario: Folder ID flows through all IndexInfo construction sites
- **WHEN** chunks are created via any pipeline path (document processing, FAQ indexing, summary generation, multimodal indexing, data-table extraction, question generation)
- **THEN** the resulting `IndexInfo` carries the parent knowledge entry's `folder_id`

### Requirement: Folder descendant expansion for scoped search

When a folder scope is specified with `include_subfolders = true`, the system SHALL expand it to include all descendant folder IDs using the materialized path: `SELECT id FROM knowledge_folders WHERE path LIKE '<scope_path>%' OR id = '<scope_id>'`. The resulting folder ID set is passed directly as a `folder_id IN (...)` metadata filter to the vector recall query — it is NOT converted to a `knowledge_id IN (...)` list.

#### Scenario: Scoping to a folder with 3 levels of children
- **WHEN** folder "Projects" has children "Projects/A", "Projects/B", "Projects/A/x"
- **AND** search is scoped to "Projects" with `include_subfolders = true`
- **THEN** the vector engine receives `folder_id IN ("Projects", "A", "B", "A/x")` as a metadata filter

#### Scenario: Exact-folder search skips descendant expansion
- **WHEN** search is scoped to "Projects" with `include_subfolders = false`
- **THEN** no SQL descendant expansion is executed
- **AND** the vector engine receives `folder_id IN ("Projects")` as a metadata filter

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

### Requirement: Vector metadata update on folder move

When a knowledge entry is moved to a different folder via `MoveToFolder` or `BatchMoveToFolder`, the system SHALL update the `folder_id` metadata of all vector-store points belonging to that knowledge entry via a `BatchUpdateFolderID` call — without re-embedding. The update SHALL broadcast across all dimension-sharded collections in every registered vector engine.

#### Scenario: Moving a single knowledge entry
- **WHEN** a knowledge entry with 50 indexed chunks is moved from folder F1 to folder F2
- **THEN** all 50 vector-store points have their `folder_id` metadata updated from "F1" to "F2"
- **AND** no re-embedding occurs

#### Scenario: Moving a knowledge entry to root
- **WHEN** a knowledge entry is moved from folder F1 to root (folder_id = NULL)
- **THEN** all vector-store points have their `folder_id` metadata set to "" (empty string)

#### Scenario: Batch move of multiple entries
- **WHEN** 10 knowledge entries are batch-moved to folder F3
- **THEN** `BatchUpdateFolderID` is called once per engine with all 10 knowledge IDs mapped to "F3"

#### Scenario: Vector update failure does not block the move
- **WHEN** the `BatchUpdateFolderID` call fails after the SQL UPDATE succeeds
- **THEN** the move operation SHALL log the error and return success
- **AND** the SQL fallback path ensures search correctness until reconciliation

### Requirement: Backfill folder metadata for existing chunks

The system SHALL provide an admin endpoint `POST /api/v1/admin/vector-stores/backfill-folder-metadata` that populates `folder_id` metadata for all existing vector-store points. The endpoint accepts an optional `kb_id` parameter to scope the backfill to a single knowledge base. The backfill is idempotent and can be safely retried.

#### Scenario: Backfill all knowledge bases
- **WHEN** the admin endpoint is called without a `kb_id` parameter
- **THEN** the system iterates all knowledge bases across all tenants
- **AND** for each knowledge entry, calls `BatchUpdateFolderID` with its current `folder_id`

#### Scenario: Backfill a single knowledge base
- **WHEN** the admin endpoint is called with `kb_id = "<kb-uuid>"`
- **THEN** only knowledge entries in that KB are processed

#### Scenario: Idempotent retry
- **WHEN** the backfill endpoint is called multiple times
- **THEN** each call produces the same result without duplicating or corrupting data
