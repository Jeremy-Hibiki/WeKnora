## ADDED Requirements

### Requirement: LLM folder-scoped search via name parameters

The `knowledge_search` agent tool SHALL accept an optional `folders` parameter containing human-readable folder names. When provided, the tool resolves each name to folder IDs within the agent's KB scope (case-insensitive leaf-name match) and restricts retrieval to chunks in those folders. An optional `include_subfolders` parameter (default `true`) controls whether descendant folders are included. When `folders` is omitted, no folder filtering is applied (current behavior).

#### Scenario: LLM scopes search to a folder by name
- **WHEN** the LLM calls `knowledge_search` with `queries: ["auth config"]` and `folders: ["Go"]`
- **AND** the KB contains a folder named "Go" with auth documentation
- **THEN** only chunks from the "Go" folder (and its descendants, if `include_subfolders` is true) are returned

#### Scenario: LLM scopes to multiple folders
- **WHEN** the LLM calls `knowledge_search` with `folders: ["Go", "Python"]`
- **THEN** chunks from both "Go" and "Python" folders are returned (union)

#### Scenario: Folder name not found
- **WHEN** the LLM calls `knowledge_search` with `folders: ["NonExistent"]`
- **THEN** the search returns zero results (no error raised)

#### Scenario: Multiple folders share the same name
- **WHEN** the KB has two folders named "docs" at different paths (`/ProjectA/docs` and `/ProjectB/docs`)
- **AND** the LLM calls `knowledge_search` with `folders: ["docs"]`
- **THEN** chunks from both "docs" folders are included in the results

#### Scenario: Folder scope intersects with agent KB scope
- **WHEN** the agent is configured for KB-A only
- **AND** the LLM calls `knowledge_search` with `folders: ["Go"]`
- **THEN** only "Go" folders within KB-A are matched (KB-B folders named "Go" are ignored)

#### Scenario: include_subfolders defaults to true
- **WHEN** the LLM calls `knowledge_search` with `folders: ["Guides"]` and omits `include_subfolders`
- **THEN** descendant folders of "Guides" (e.g. "Guides/Go", "Guides/Go/API") are included

### Requirement: LLM tag-scoped search via name parameters

The `knowledge_search` agent tool SHALL accept an optional `tags` parameter containing human-readable tag names. When provided, the tool resolves each name to tag IDs within the agent's KB scope (case-insensitive match) and applies them as retrieval filters. When omitted, no tag filtering is applied.

#### Scenario: LLM filters by tag name
- **WHEN** the LLM calls `knowledge_search` with `queries: ["deployment"]` and `tags: ["important"]`
- **AND** a tag named "important" exists in the KB
- **THEN** only chunks tagged "important" are returned

#### Scenario: Multiple tag names produce union
- **WHEN** the LLM calls `knowledge_search` with `tags: ["important", "reference"]`
- **THEN** chunks carrying either tag are returned

#### Scenario: Tag name not found
- **WHEN** the LLM calls `knowledge_search` with `tags: ["NonExistent"]`
- **THEN** the search returns zero results (no error raised)

### Requirement: Folder discovery tool

The system SHALL provide a `list_folders` agent tool that returns the folder tree of the agent's knowledge base scope. Each entry includes the folder name, full path, and document count. The tool accepts an optional `knowledge_base_ids` parameter to narrow the listing to specific KBs.

#### Scenario: LLM lists all folders in scope
- **WHEN** the LLM calls `list_folders` with no arguments
- **THEN** a JSON array is returned with entries like `{"name": "Go", "path": "/Guides/Go", "document_count": 8}`
- **AND** every folder in the agent's KB scope is included

#### Scenario: LLM lists folders for a specific KB
- **WHEN** the LLM calls `list_folders` with `knowledge_base_ids: ["kb-1"]`
- **THEN** only folders in KB "kb-1" are returned

### Requirement: Tag discovery tool

The system SHALL provide a `list_tags` agent tool that returns all tags in the agent's knowledge base scope. Each entry includes the tag name and document count.

#### Scenario: LLM lists all tags in scope
- **WHEN** the LLM calls `list_tags` with no arguments
- **THEN** a JSON array is returned with entries like `{"name": "important", "document_count": 15}`

### Requirement: Search results include folder path and tags

Each search result returned by the `knowledge_search` agent tool SHALL include the `folder_path` (e.g. `/Guides/Go/API`) and `tags` (list of tag names) of the source knowledge entry. Results from root-level entries show an empty folder_path.

#### Scenario: Result from a nested folder
- **WHEN** a search result originates from `/Guides/Go/API`
- **THEN** the result object includes `folder_path: "/Guides/Go/API"` and `tags: ["important"]`

#### Scenario: Result from root
- **WHEN** a search result originates from a root-level knowledge entry with no folder and no tags
- **THEN** the result object includes `folder_path: ""` and `tags: []`

### Requirement: MCP server folder/tag scope support

The MCP server SHALL accept optional `folder_names`, `tag_names`, and `include_subfolders` parameters on the `hybrid_search`, `chat`, and `agent_chat` tools. It SHALL provide `list_kb_folders` and `list_kb_tags` tools. The MCP handler resolves names to IDs before forwarding to the REST API.

#### Scenario: MCP client scopes hybrid search by folder
- **WHEN** an MCP client calls `hybrid_search` with `folder_names: ["Go"]`
- **THEN** the server resolves "Go" to folder IDs in the target KB and forwards `folder_ids` to the REST API

#### Scenario: MCP client lists folders
- **WHEN** an MCP client calls `list_kb_folders` with `kb_id: "kb-1"`
- **THEN** the server returns the folder tree of KB "kb-1" with names, paths, and counts

## MODIFIED Requirements

### Requirement: Folder-scoped vector recall

The retrieval pipeline SHALL accept folder scope from LLM-callable tools (agent `knowledge_search` and MCP server tools) in addition to the existing HTTP/UI entry points. LLM tool parameters use human-readable folder names; the backend resolves them to folder IDs before entering the existing `SearchParams.FolderIDs` pipeline. The resolution is case-insensitive and scoped to the caller's KB scope.

#### Scenario: LLM-initiated folder-scoped search
- **WHEN** the LLM calls `knowledge_search` with `folders: ["Go"]`
- **THEN** the backend resolves "Go" → folder UUID(s) within the KB
- **AND** passes them as `SearchParams.FolderIDs` to the retrieval pipeline
- **AND** the vector engine applies `folder_id IN (...)` metadata filtering

#### Scenario: LLM-initiated search with no folder scope
- **WHEN** the LLM calls `knowledge_search` without the `folders` parameter
- **THEN** no folder filtering is applied and all chunks in the KB scope are searchable
