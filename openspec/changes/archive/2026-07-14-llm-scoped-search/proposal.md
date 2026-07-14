## Why

The `knowledge_search` agent tool and MCP server tools currently accept only `queries` and `knowledge_base_ids`. An LLM calling these tools cannot scope retrieval by folder or tag — it has no way to express "search only in the Go docs" or "filter by the API reference tag." Meanwhile the HTTP API already supports `folder_ids` and `tag_ids`, and the vector engine now has native `folder_id` metadata filtering (via the `vector-folder-metadata` change). The filtering capability exists end-to-end — it is simply invisible to LLM callers.

## What Changes

- Add optional `folders` and `tags` parameters (human-readable names, not UUIDs) to the `knowledge_search` agent tool schema, so the LLM can scope retrieval by folder/tag name without knowing internal IDs.
- Add optional `include_subfolders` parameter to the `knowledge_search` tool.
- Add a new `list_folders` agent tool that returns the folder tree of a knowledge base (id, name, path, document count) so the LLM can discover available scopes before searching.
- Add a new `list_tags` agent tool that returns all tags in a knowledge base (id, name, document count).
- Add corresponding tools (`list_kb_folders`, `list_kb_tags`) and optional `folder_names`/`tag_names` parameters to the MCP server (`mcp-server/weknora_mcp_server.py`).
- Add a backend name-resolution helper that converts folder/tag names to IDs within a KB scope, used by both the agent tool and MCP handler before forwarding to the existing search pipeline.
- Enrich `knowledge_search` results to always include `folder_path` and `tags` per result so the LLM can reason about scope for follow-up queries.

## Capabilities

### New Capabilities

- `llm-scoped-search`: Exposes folder/tag scoping and folder/tag discovery to LLM callers (agent tool + MCP), using human-readable names resolved server-side to internal IDs.

### Modified Capabilities

- `folder-scoped-search`: The folder-scoped retrieval that currently works only for HTTP/UI callers is extended to LLM-callable tools. The underlying `SearchParams.FolderIDs` / `RetrieveParams.FolderIDs` plumbing is unchanged — this change adds a new entry point (name-based) that feeds into the same pipeline.

## Impact

- **Agent tool schema** (`internal/agent/tools/knowledge_search.go`): tool input struct gains `Folders`, `Tags`, `IncludeSubfolders` fields; tool JSON schema gains corresponding optional properties; tool execution resolves names → IDs before building `SearchParams`.
- **New agent tools** (`internal/agent/tools/`): `list_folders.go` and `list_tags.go` — thin wrappers around existing KB folder/tag service methods.
- **Agent tool registry** (`internal/agent/tools/registry.go` or engine setup): register the two new tools so they are available to the ReAct engine.
- **MCP server** (`mcp-server/weknora_mcp_server.py`): add `list_kb_folders`, `list_kb_tags` tool definitions and handlers; add `folder_names`, `tag_names`, `include_subfolders` to the existing `hybrid_search` / `chat` / `agent_chat` tool schemas.
- **Backend name resolution**: a new helper (e.g. `resolveFolderNames(kbID, names) → folderIDs` and `resolveTagNames(kbID, names) → tagIDs`) in the knowledge service or a new utility. Uses existing folder/tag repository queries.
- **Search result enrichment**: ensure `folder_path` is always populated in agent tool search results (it may already be via the `SearchResult.FolderPath` field added in the folder feature).
