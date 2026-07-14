## 1. Backend Name Resolution Helpers

- [x] 1.1 Add `ResolveFolderNames(ctx, tenantID, kbID, names []string) ([]string, error)` to the knowledge folder service — case-insensitive leaf-name match, returns folder IDs, supports multiple matches per name
- [x] 1.2 Add `ResolveTagNames(ctx, tenantID, kbID, names []string) ([]string, error)` to the knowledge tag service — case-insensitive match, returns tag IDs
- [x] 1.3 Add both methods to the `KnowledgeFolderService` and `KnowledgeTagService` interfaces in `internal/types/interfaces/`
- [x] 1.4 Unit test: name resolution with exact match, case-insensitive match, no match, multiple matches

## 2. Agent Tool — knowledge_search Enhancement

- [x] 2.1 Add `Folders []string`, `Tags []string`, `IncludeSubfolders *bool` to `KnowledgeSearchInput` in `internal/agent/tools/knowledge_search.go`
- [x] 2.2 Update the tool's JSON schema to include `folders`, `tags`, `include_subfolders` as optional properties with descriptive descriptions
- [x] 2.3 In the tool's `Run` method, resolve `Folders` → folder IDs and `Tags` → tag IDs using the name-resolution helpers before building `SearchParams`
- [x] 2.4 Set `SearchParams.FolderIDs`, `SearchParams.TagIDs`, and `SearchParams.IncludeSubfolders` from the resolved IDs
- [x] 2.5 Ensure resolved scope intersects with the agent's pre-computed `searchTargets` KB scope (resolve only within agent KBs)
- [x] 2.6 Enrich search results to include `folder_path` and `tags` (tag names) per result — resolve tag IDs to names if needed

## 3. Agent Tool — list_folders

- [x] 3.1 Create `internal/agent/tools/list_folders.go` implementing the `list_folders` tool
- [x] 3.2 Tool accepts optional `knowledge_base_ids` (defaults to agent's KB scope); returns JSON array of `{name, path, document_count, children_count}`
- [x] 3.3 Uses existing folder service `ListFolders` or `GetFolderTree` method; falls back to listing all folders across agent KBs
- [x] 3.4 Define tool schema (input + output format) and register in the tool registry

## 4. Agent Tool — list_tags

- [x] 4.1 Create `internal/agent/tools/list_tags.go` implementing the `list_tags` tool
- [x] 4.2 Tool accepts optional `knowledge_base_ids` (defaults to agent's KB scope); returns JSON array of `{name, document_count}`
- [x] 4.3 Uses existing tag service `ListTags` or `ListKnowledgeTags` method
- [x] 4.4 Define tool schema and register in the tool registry

## 5. Agent Engine Registration

- [x] 5.1 Register `list_folders` and `list_tags` tools in the agent engine setup (where `knowledge_search` is registered — likely `internal/agent/tools/registry.go` or engine factory)
- [x] 5.2 Ensure the tools have access to the agent's KB scope (inject `searchTargets` or KB list at construction, same pattern as `knowledge_search`)
- [x] 5.3 Verify the tools appear in the LLM's available tool list when the agent has KB scope

## 6. MCP Server — Tool Schemas and Handlers

- [x] 6.1 Add `list_kb_folders` tool definition to `mcp-server/weknora_mcp_server.py` — accepts `kb_id`, returns folder tree via REST API `GET /knowledge-bases/{id}/folders`
- [x] 6.2 Add `list_kb_tags` tool definition — accepts `kb_id`, returns tags via REST API `GET /knowledge-bases/{id}/knowledge-tags`
- [x] 6.3 Add `folder_names`, `tag_names`, `include_subfolders` optional parameters to the `hybrid_search` tool schema
- [x] 6.4 Add the same parameters to the `chat` and `agent_chat` tool schemas
- [x] 6.5 Implement name-to-ID resolution in the MCP Python handler: call `list_kb_folders`/`list_kb_tags` internally, match names case-insensitively, then forward resolved `folder_ids`/`tag_ids` to the REST API
- [x] 6.6 Handle the case where a name doesn't match (log warning, omit from filter, don't error)

## 7. Frontend (Optional Polish)

- [x] 7.1 Verify the frontend `knowledge_search` display shows folder_path in results (likely already works via `SearchResult.FolderPath`)
- [x] 7.2 No frontend changes required for the tool itself — this is LLM-facing only

## 8. Tests

- [x] 8.1 Unit test: `ResolveFolderNames` — exact match, case-insensitive, no match, multiple matches, empty input
- [x] 8.2 Unit test: `ResolveTagNames` — same pattern
- [x] 8.3 Unit test: `knowledge_search` tool with `folders` parameter — verify `SearchParams.FolderIDs` is populated from resolved names
- [x] 8.4 Unit test: `knowledge_search` tool with `tags` parameter — verify `SearchParams.TagIDs` is populated
- [x] 8.5 Unit test: `knowledge_search` tool without `folders`/`tags` — verify behavior unchanged (backward compat)
- [x] 8.6 Unit test: `list_folders` tool returns correct folder tree
- [x] 8.7 Unit test: `list_tags` tool returns correct tag list
- [x] 8.8 Run `go test -count=1 ./internal/agent/tools/... ./internal/application/service/...` — all pass
- [x] 8.9 Run `cd mcp-server && pytest` — all pass
