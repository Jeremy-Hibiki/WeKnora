## Context

The WeKnora retrieval pipeline supports folder and tag scoping end-to-end: `SearchParams.FolderIDs` / `SearchParams.TagIDs` → `RetrieveParams.FolderIDs` / `RetrieveParams.TagIDs` → vector engine metadata filters. The HTTP API (`POST /knowledge-bases/{id}/hybrid-search`) and UI (`POST /sessions/{id}/knowledge-qa`) expose these parameters. However, the LLM-facing entry points — the agent `knowledge_search` tool and the MCP server tools — do not. An LLM using the ReAct engine or an MCP client cannot scope by folder or tag.

The `knowledge_search` tool schema currently exposes only `queries` and `knowledge_base_ids`. Tags and folders are "baked in" to pre-computed `searchTargets` at agent construction time; the LLM has no runtime control. Similarly, the MCP server's `hybrid_search`, `chat`, and `agent_chat` tools accept no scope parameters.

LLMs are poor at handling raw UUIDs — they hallucinate them. The design therefore uses **human-readable names** (`"Go"`, `"API Reference"`) as tool parameters, with server-side resolution to internal IDs.

## Goals / Non-Goals

**Goals:**
- Let the LLM scope `knowledge_search` by folder name(s) and tag name(s).
- Let the LLM discover available folders and tags via dedicated listing tools.
- Let MCP clients pass folder/tag scope to search and chat tools.
- Keep name resolution server-side — the LLM never touches UUIDs.
- Maintain full backward compatibility: existing tool calls without the new parameters behave identically.

**Non-Goals:**
- Changing the agent's pre-computed `searchTargets` system. The new parameters **intersect** with the agent's KB scope (they can narrow, never widen).
- Exposing `knowledge_ids` (specific file selection) to the LLM — too error-prone and the LLM has no way to discover file IDs.
- Auto-detecting folder/tag intent from the query text (semantic routing). The LLM decides when to scope; we just provide the mechanism.
- Modifying the `POST /sessions/search` non-LLM endpoint (it already has its own scope model).

## Decisions

### D1: Use human-readable names, not UUIDs, as LLM tool parameters

**Decision**: The `knowledge_search` tool accepts `folders: ["Go", "API"]` and `tags: ["important"]` — names, not IDs.

**Rationale**: LLMs hallucinate UUIDs. They can reliably use short human-readable names like "Go" or "API Reference". The backend resolves names to IDs via a simple case-insensitive lookup within the KB scope.

**Alternatives considered**:
- *Expose raw `folder_ids`/`tag_ids`*: rejected because UUIDs are meaningless to the LLM and it cannot discover or remember them across turns.
- *Use folder paths like `/Guides/Go`*: more precise but longer and harder for the LLM to get right. Rejected for the tool parameter; paths ARE shown in results and listing tools for context.

### D2: Name resolution is case-insensitive, matches on leaf name

**Decision**: `folders: ["Go"]` matches any folder whose **name** (not path) is "Go" within the KB. If multiple folders share the same name (e.g. two "docs" folders in different branches), all are matched — the search scope is the union.

**Rationale**: Folder names are unique-by-convention within a parent, but not globally within a KB. Matching by leaf name is the most intuitive for an LLM. If the user needs disambiguation, the listing tool shows full paths, and the LLM can use a more specific name.

### D3: Tag names resolved within KB scope, case-insensitive

**Decision**: `tags: ["important"]` resolves to all tags named "important" in the search-target KBs. Multiple matches are unioned.

### D4: Intersection with agent's pre-computed KB scope

**Decision**: The LLM's folder/tag parameters filter **within** the agent's existing KB scope. If the agent is configured for KB-A only, `folders: ["Go"]` resolves "Go" folders in KB-A. The LLM cannot widen the KB scope.

**Rationale**: Security and predictability. The agent's KB scope is an admin/user configuration; the LLM's folder/tag scope is a runtime refinement.

### D5: `list_folders` and `list_tags` as standalone agent tools

**Decision**: Provide two new tools: `list_folders(knowledge_base_ids?)` and `list_tags(knowledge_base_ids?)`. They return JSON arrays with `{name, path, document_count}` (folders) or `{name, document_count}` (tags).

**Rationale**: The LLM needs to discover what folders/tags exist before it can use them as filters. Without listing tools, the LLM would have to guess names from the user's query — unreliable. The listing tools follow the MCP "discover + act" pattern.

**Tool schema**:
```json
// list_folders
{
  "type": "object",
  "properties": {
    "knowledge_base_ids": {
      "type": "array", "items": {"type": "string"}, "maxItems": 5
    }
  }
}
// Returns: [{name, path, document_count, children_count}]

// list_tags
{
  "type": "object",
  "properties": {
    "knowledge_base_ids": {
      "type": "array", "items": {"type": "string"}, "maxItems": 5
    }
  }
}
// Returns: [{name, document_count}]
```

### D6: Folder/tag scope in `knowledge_search` is optional and additive

**Decision**: The updated `knowledge_search` schema adds three optional properties: `folders`, `tags`, `include_subfolders`. When omitted, behavior is unchanged.

```json
{
  "type": "object",
  "properties": {
    "queries": {"type": "array", ...},
    "knowledge_base_ids": {"type": "array", ...},
    "folders": {
      "type": "array", "items": {"type": "string"}, "maxItems": 10,
      "description": "Folder names to scope the search (e.g. [\"Go\", \"API\"]). Use list_folders to discover names."
    },
    "tags": {
      "type": "array", "items": {"type": "string"}, "maxItems: 10,
      "description": "Tag names to filter by (e.g. [\"important\"]). Use list_tags to discover names."
    },
    "include_subfolders": {
      "type": "boolean", "default": true,
      "description": "When true, folder scope includes all descendant subfolders."
    }
  },
  "required": ["queries"]
}
```

### D7: MCP server mirrors the agent tool parameters

**Decision**: The MCP `hybrid_search`, `chat`, and `agent_chat` tools gain optional `folder_names`, `tag_names`, and `include_subfolders` parameters. Two new MCP tools (`list_kb_folders`, `list_kb_tags`) are added. The MCP Python handler resolves names to IDs via the REST API (calling existing folder/tag list endpoints) before forwarding to the search/chat endpoints.

### D8: Search results always include folder_path and tags

**Decision**: Agent tool search results include `folder_path` (e.g. `/Guides/Go/API`) and `tags` (list of tag names) for each result. This lets the LLM reason: "The user asked about Go API auth; results are from `/Guides/Go/API` — I can narrow further with `folders: ['API']` if needed."

**Implementation note**: `SearchResult.FolderPath` already exists (added in the folder feature). Tag names may need to be resolved from tag IDs in the result enrichment step.

## Risks / Trade-offs

- **[Name ambiguity]** Multiple folders with the same name produce a broader-than-expected scope. → *Mitigation*: the `list_folders` tool shows full paths; the LLM can use a more specific name or path segment. The union behavior is documented in the tool description.

- **[LLM over-reliance on scoping]** The LLM might always call `list_folders` before every search, adding latency. → *Mitigation*: the tool description says "call this only when the user mentions a specific section or category." The system prompt can also discourage unnecessary calls.

- **[MCP round-trip overhead]** The MCP Python handler must resolve names → IDs before calling the REST API, adding one HTTP round-trip for the folder/tag list. → *Mitigation*: acceptable; folder/tag lists are small and cacheable. A future optimization can add a dedicated name-resolution endpoint.

- **[Name resolution failure]** If the LLM passes a folder name that doesn't exist, the search returns zero results (not an error). → *Mitigation*: the tool description says "use list_folders to discover valid names." The result can include a warning when a name didn't match.
