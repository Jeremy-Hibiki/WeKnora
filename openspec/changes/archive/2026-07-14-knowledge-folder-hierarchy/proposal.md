## Why

Knowledge bases today store all documents in a flat list. As KBs grow past a few hundred entries, finding, organizing, and managing documents becomes painful — users cannot group related files, browse by topic, or scope a search to a subset. Competing products (Notion, Google Drive, S3) all offer hierarchical folders. WeKnora needs the same: a **net-disk-like folder hierarchy** that supports whole-folder uploads preserving internal structure, and crucially **leverages that hierarchy to improve retrieval precision** (search within a folder, folder-scoped RAG, folder-aware result grouping).

## What Changes

- **Hierarchical folder model**: Introduce a `knowledge_folders` table with parent-child relationships (adjacency list + materialized path), integrated into the existing `knowledges` table via a nullable `folder_id` FK.
- **Folder CRUD & navigation**: Create/rename/move/delete folders; render as a tree with infinite nesting depth; breadcrumb path display; drag-and-drop reorganization.
- **Structure-preserving folder upload**: Upload an entire local folder (via `webkitRelativePath`) or a `.zip` archive; the backend reconstructs the directory tree as folder records and assigns each file to the correct folder automatically.
- **Folder-scoped retrieval**: **BREAKING** (additive) — extend the search/RAG pipeline to accept a `folder_scope` parameter (specific folder + descendants, or root). Documents outside the scope are filtered out before vector recall, reducing noise and improving precision.
- **Folder metadata in search results**: Each search result carries its folder path, enabling users to understand context and optionally refine by folder.
- **UI overhaul**: KnowledgeBase view transitions from flat list to a dual-mode (grid + list) file manager with folder navigation, breadcrumb, and a folder-scoped search toggle.

## Capabilities

### New Capabilities
- `knowledge-folder-management`: Folder CRUD, hierarchy navigation (tree, breadcrumb), move/reorganize, and tenant-scoped ownership validation. Covers the organizational model that replaces flat lists.
- `folder-upload`: Multi-file folder upload (with relative path preservation) and `.zip` archive upload; server-side directory tree reconstruction; atomic per-folder creation within a transaction.
- `folder-scoped-search`: Extend the retrieval pipeline (vector recall + reranking) to support folder-scoped filtering; surface folder metadata in search results; enable "search within this folder" in the UI.

### Modified Capabilities
<!-- No existing specs in openspec/specs/ yet; all three are new. -->

## Impact

- **Database**: New migration `000065_knowledge_folders` adds `knowledge_folders` table and `knowledges.folder_id` column. Future migration needed to add folder metadata to the vector/chunk layer for scoped search.
- **Backend (Go)**: New handler/service/repository for folders; existing `knowledge.go` handlers gain `folder_id` parameters; search pipeline (`chat_pipeline/`, `knowledgebase_search*.go`) gains folder-scope filtering.
- **API**: 12 new HTTP endpoints (folder CRUD + folder/zip upload + move operations); 2 existing upload endpoints gain optional `folder_id`.
- **Frontend (Vue 3 + TDesign)**: New components (FolderTree, FolderBreadcrumb, FolderSelector, FolderManageDialog, KnowledgeFolderView); KnowledgeBase.vue refactored to dual-mode file-manager layout; upload confirm dialog gains folder destination.
- **Retrieval**: Vector store filtering or post-recall filtering by folder_id; potential chunk-level metadata enrichment for folder paths.
