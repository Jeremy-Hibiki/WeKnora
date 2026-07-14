## Context

The current `feature/multi-folder-upload` branch has implemented ~80% of the folder feature: DB schema (`000065`), folder CRUD, structure-preserving upload (multi-file + zip), move operations, and a Vue file-manager UI. What is **completely missing** is integration with the retrieval pipeline — folder_id is not referenced anywhere in `chat_pipeline/` or `knowledgebase_search*.go`. Search treats all documents as flat, ignoring folder context.

This design covers the full architecture (acknowledging existing code where it already aligns) and focuses on the **retrieval integration** gap, plus hardening decisions for the organizational model.

**Current state:**
- `knowledge_folders` table: adjacency list (`parent_folder_id`) + materialized `path` + `depth`. Unique-name-per-parent constraint. Soft-delete via `deleted_at`.
- `knowledges.folder_id` FK, nullable (null = root).
- Folder CRUD, tree fetch, breadcrumb, move — all implemented with tenant + KB ownership validation.
- Upload: `POST /knowledge/folder` (multi-file with `paths[]`), `POST /knowledge/zip` (server-side extraction), both reconstruct the directory tree atomically.
- Existing `POST /knowledge/file` and `POST /knowledge/url` accept optional `folder_id`.

**Tech stack:** Go (Gin, GORM), PostgreSQL, Vue 3 + TDesign, pgvector for embeddings.

## Goals / Non-Goals

**Goals:**
- Formalize the folder hierarchy model (adjacency list + materialized path) as the canonical organizational primitive.
- Define the folder-upload reconstruction algorithm with transactional guarantees.
- **Close the retrieval gap**: enable folder-scoped search (vector recall filtered to a folder subtree) and surface folder metadata in search results.
- Define tenant/security boundaries for all folder operations.
- Specify depth and naming constraints to prevent path-explosion abuse.

**Non-Goals:**
- Folder-level permissions/sharing (all folders inherit KB-level RBAC).
- Cross-KB folder references or folder symlinks.
- Folder versioning or history tracking.
- Full-text search index changes (folder scoping works on metadata, not a new index).
- Wiki folders (`wiki/folders`) — separate domain, not unified with knowledge folders in this change.

## Decisions

### D1: Adjacency list + materialized path (hybrid)
**Choice:** Keep `parent_folder_id` (adjacency) as the source of truth for hierarchy, with a denormalized `path` column (`/Guides/Go/API`) and `depth` integer for query efficiency.

**Rationale:** Pure adjacency lists require recursive CTEs for subtree queries (expensive at depth). Pure materialized paths make moves painful (rewrite all descendant paths). The hybrid gives us O(1) subtree filtering via `WHERE path LIKE ? || '/%'` and O(subtree) path rewrites on move — acceptable since moves are rare and bounded by depth limit.

**Alternative considered:** Closure table — stores all ancestor-descendant pairs. Excellent for arbitrary-depth subtree queries but doubles write complexity on every insert/move and adds a join table. Overkill for a hierarchy that rarely exceeds 5 levels.

### D2: Folder-scoped retrieval via pre-recall metadata filter
**Choice:** Extend the vector search pipeline to accept an optional `folder_scope` (a set of folder_ids, expanded to include all descendants via the materialized path). Pass these IDs as a **metadata pre-filter** to the vector store query so irrelevant chunks are never recalled.

**How it works:**
1. User scopes search to folder F (or "root").
2. Resolve F's `path`, query `WHERE path LIKE 'F_path/%' OR id = F` to get all descendant folder_ids (single indexed LIKE query).
3. If scope = root (no folder), the set is null → no filter → search everything.
4. Pass `folder_ids` as metadata filter to pgvector recall (`WHERE folder_id = ANY(?)`).
5. Post-recall reranking and citation assembly proceed unchanged.

**Fallback:** If the vector store's metadata filtering is insufficient, apply post-recall filtering (recall top-K×2, filter by folder_id, re-rank top-K). Less efficient but correct.

**Alternative considered:** Store folder path segments as separate vector metadata dimensions (one filter per level) — rejected as over-engineered; a flat `folder_id IN (...)` set is simpler and sufficient.

### D3: Chunk-level folder_id enrichment
**Choice:** Add `folder_id` to chunk metadata at chunk creation time (when knowledge is processed). This is required for D2 to work — the vector store needs folder_id on each chunk record.

**Migration:** New migration adds `folder_id` to the chunks table (or embedding metadata JSON, depending on current schema). Existing chunks backfilled via `JOIN knowledges ON knowledge_id`.

**Alternative considered:** Look up folder_id at query time by joining `chunks → knowledges` — rejected because pgvector recall needs the filter as metadata, not a join, to stay within the ANN index scan.

### D4: Transactional folder-tree reconstruction on upload
**Choice:** Folder upload receives files + relative paths. The backend:
1. Parses unique directory segments from all `paths[]`.
2. Inserts all folder rows in a single DB transaction (sorted by depth to satisfy FK constraints).
3. On any failure, rolls back the entire batch — no partial trees.
4. Returns a `path → folder_id` map so subsequent file uploads can assign `folder_id`.

**Rationale:** Atomic creation prevents orphaned folders if the upload is interrupted. The existing implementation already follows this pattern; this decision formalizes it.

### D5: Depth limit = 10, name constraints
**Choice:** Enforce a maximum nesting depth of 10 levels. Folder names limited to 255 chars, sanitized (no `/`, `\`, null bytes). Unique name per parent (already enforced by DB unique index).

**Rationale:** Prevents pathological folder structures from degrading path-LIKE query performance and path storage. 10 levels covers any realistic document taxonomy.

### D6: Move-folder cascade path rewrite
**Choice:** When a folder is moved to a new parent, rewrite `path` and recompute `depth` for the moved folder **and all descendants**. Done in a single transaction using the materialized path prefix-replacement pattern.

**Formula:** `UPDATE knowledge_folders SET path = REPLACE(path, old_prefix, new_prefix) WHERE path LIKE old_prefix || '/%' OR id = moved_id`.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| **Path LIKE queries degrade at scale** (100k+ folders) | `idx_folders_path` GIN/text index; depth limit caps path length; LIKE prefix queries use B-tree index efficiently. |
| **Chunk backfill is expensive** on large KBs | Run backfill as a background task with batch size 1000; idempotent (WHERE folder_id IS NULL). |
| **Post-recall fallback (D2) doubles recall cost** | Only used if vector metadata filtering is unavailable; pgvector supports metadata WHERE clauses natively, so fallback is unlikely. |
| **Move cascade rewrite is O(subtree)** | Bounded by depth limit (10) and typical subtree size (< 1000). Rare operation. Done in a transaction. |
| **Concurrent folder creation races** | DB unique index (`idx_folders_unique_name`) prevents duplicate names; GORM returns error, handler retries or returns conflict. |
| **Folder delete with contents** | `ON DELETE SET NULL` on `knowledges.folder_id` preserves documents (moved to root). `ON DELETE CASCADE` on folder hierarchy removes subfolders only. Handler offers `force=true` to delete subfolders, otherwise blocks if non-empty. |
