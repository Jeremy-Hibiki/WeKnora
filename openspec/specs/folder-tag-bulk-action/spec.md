# folder-tag-bulk-action Specification

## Purpose

Bulk document-tag operations scoped by folder subtree. Lets users apply or remove one or more KB-local document tags across every knowledge entry in a folder (optionally recursive) in a single atomic call, with an upfront affected-count preview. The folder is a point-in-time selector, not a persistent binding — documents added later do not inherit tags.

## Requirements

### Requirement: Tag-by-folder bulk add

The system SHALL provide a bulk operation that adds one or more document tags to every knowledge entry in a specified folder subtree. The operation SHALL accept multiple folder IDs, one or more tag IDs, and a `recursive` flag (default `true`). When `recursive` is true, the folder IDs SHALL be expanded via the materialized path to include all descendant folders before resolving to knowledge IDs.

The add operation SHALL be idempotent: entries that already carry the tag SHALL be silently skipped (`ON CONFLICT DO NOTHING`), not error.

#### Scenario: Add tag to a single folder with subfolders
- **WHEN** a user adds tag `T` to folder `F` with `recursive: true`
- **AND** folder `F` contains 3 direct knowledge entries
- **AND** `F` has a child folder `F/C` containing 2 more entries
- **THEN** all 5 knowledge entries receive tag `T` in `knowledge_tag_relations`
- **AND** the response returns `affected_count: 5`

#### Scenario: Add tag to a folder where some entries already have it
- **WHEN** a user adds tag `T` to folder `F` containing 3 entries
- **AND** one entry already has tag `T`
- **THEN** the other 2 entries receive tag `T`
- **AND** the existing association is unchanged
- **AND** the response returns `affected_count: 3` (scope size, not mutation count)

#### Scenario: Non-recursive add touches only direct folder entries
- **WHEN** a user adds tag `T` to folder `F` with `recursive: false`
- **AND** folder `F` contains 2 direct knowledge entries
- **AND** `F` has a child folder `F/C` containing 3 entries
- **THEN** only the 2 direct entries receive tag `T`
- **AND** the 3 entries in `F/C` are unchanged
- **AND** the response returns `affected_count: 2`

#### Scenario: Overlapping folder IDs do not double-count
- **WHEN** a user adds tag `T` to folders `[F, F/C]` with `recursive: true`
- **AND** `F` has 2 direct entries and `F/C` has 3 entries
- **THEN** both expansions resolve `F/C`'s entries, but the knowledge query (`folder_id IN (...)`) matches each knowledge entry once because each entry has exactly one `folder_id`
- **AND** the response returns `affected_count: 5`, not 8

#### Scenario: Add tags across multiple folders (cross-folder union)
- **WHEN** a user adds tag `T` to folders `F1` (10 entries) and `F2` (20 entries) with `recursive: true`
- **THEN** all 30 knowledge entries across both subtrees receive tag `T`
- **AND** the response returns `affected_count: 30`

### Requirement: Tag-by-folder bulk remove

The system SHALL provide a bulk operation that removes one or more document tags from every knowledge entry in a specified folder subtree. The remove operation SHALL be a set subtraction: it deletes only associations matching both the tag ID and a knowledge entry within the expanded folder scope. Entries that do not have the tag are silently unaffected.

#### Scenario: Remove tag from a folder subtree
- **WHEN** a user removes tag `T` from folder `F` with `recursive: true`
- **AND** folder `F` and its children contain 5 entries
- **AND** 3 of those entries currently have tag `T`
- **THEN** 3 rows are deleted from `knowledge_tag_relations`
- **AND** the other 2 entries are unchanged
- **AND** the response returns `affected_count: 5` (scope size, not mutation count)

#### Scenario: Remove a tag that no entry in the scope has
- **WHEN** a user removes tag `T` from folder `F` (5 entries)
- **AND** none of the entries have tag `T`
- **THEN** zero rows are deleted
- **AND** the response returns `affected_count: 5`
- **AND** no error is raised

### Requirement: affected_count semantics

The `affected_count` field in the response SHALL represent the number of knowledge entries in the expanded folder scope (after folder expansion, before the tag write), deduplicated by knowledge ID. It SHALL NOT represent the number of database rows inserted or deleted. Because each knowledge entry has exactly one `folder_id`, overlapping folder IDs in the request (e.g. an ancestor and its descendant with `recursive: true`) do not produce duplicate knowledge IDs — the SQL `folder_id IN (...)` clause matches each row once. The count shown to the user in the confirmation preview SHALL use the same expansion logic, so the preview count and the returned count are always consistent.

#### Scenario: Preview count matches returned count
- **WHEN** a user previews the operation for folder `F` (recursive) and sees "47 documents"
- **AND** confirms the operation
- **THEN** the response returns `affected_count: 47`
- **AND** the number 47 is the scope size regardless of how many rows were actually mutated

### Requirement: Document KB type enforcement

The tag-by-folder operation SHALL reject requests targeting a non-document knowledge base with `400 Bad Request`. Document tags (`knowledge_tag_relations`, UUID `string` IDs) and FAQ tags (`chunks.tag_id`, `int64` seq_id) are separate systems: document tags resolve to knowledge IDs via SQL join at query time, while FAQ tags are metadata in the vector store. Writing `knowledge_tag_relations` rows on FAQ entries would be invisible at retrieval time because FAQ search reads tag scope from vector-store `tag_id` metadata, not the join table.

#### Scenario: Request on a document KB succeeds
- **WHEN** a user calls tag-by-folder on a KB of type `document`
- **THEN** the operation proceeds normally

#### Scenario: Request on an FAQ KB is rejected
- **WHEN** a user calls tag-by-folder on a KB of type `faq`
- **THEN** the system returns `400 Bad Request`
- **AND** no folder expansion or tag write occurs

### Requirement: Tag ownership validation

The tag-by-folder operation SHALL validate that every `tag_id` in the request belongs to the knowledge base identified by the `:id` path parameter. Tags are KB-local (`KnowledgeTag.KnowledgeBaseID`); without validation, a caller could write tag associations from one KB onto another KB's documents. This validation SHALL apply to both add and remove actions.

#### Scenario: All tag IDs belong to the target KB
- **WHEN** a user requests tag-by-folder on KB `K` with tag IDs `[T1, T2]`
- **AND** both `T1` and `T2` belong to KB `K`
- **THEN** the operation proceeds normally

#### Scenario: A tag ID belongs to a different KB
- **WHEN** a user requests tag-by-folder on KB `K` with tag IDs `[T1, T2]`
- **AND** `T2` belongs to a different KB
- **THEN** the system returns `400 Bad Request`
- **AND** no folder expansion or tag write occurs

### Requirement: No vector store involvement

The tag-by-folder operation SHALL NOT touch the vector store in any way. Document tags are resolved to knowledge IDs at query time via `ListKnowledgeIDsByTagIDs`; the vector engine never sees `tag_id` metadata for document KBs. This makes the operation cheaper than folder moves, which must call `BatchUpdateFolderID` across all engines.

#### Scenario: No BatchUpdateChunkTagID call
- **WHEN** a tag-by-folder add operation completes successfully on 200 documents
- **THEN** zero calls are made to any vector engine's `BatchUpdateChunkTagID` or `BatchUpdateFolderID`

### Requirement: Point-in-time semantics (no inheritance)

The tag-by-folder operation SHALL be point-in-time: only knowledge entries present in the folder subtree at the moment of execution receive the tag. Knowledge entries added to the folder afterward (e.g., via datasource sync, manual upload, or folder move) SHALL NOT inherit the tag. This is the accepted design contract — the folder is a selector, not a persistent binding.

#### Scenario: Document added after operation does not inherit tag
- **WHEN** tag `T` is added to folder `F` (contains 5 entries)
- **AND** a new knowledge entry is later created inside `F`
- **THEN** the new entry does NOT have tag `T`
- **AND** retrieving by tag `T` does NOT return the new entry

#### Scenario: Confirmation dialog warns about point-in-time semantics
- **WHEN** the user opens the tag-by-folder dialog
- **THEN** the dialog SHALL display a warning stating that documents added to the folder later will not automatically receive the selected tags

### Requirement: Affected count preview

The system SHALL provide a way for the frontend to preview the number of knowledge entries in a folder subtree before the user confirms the operation. This preview count SHALL match the `affected_count` returned by the operation. The preview is necessary because the operation reshapes retrieval scope for in-flight sessions and agents that have pinned the affected tags.

#### Scenario: User previews count before confirming
- **WHEN** a user opens the tag-by-folder dialog for folder `F` (recursive)
- **THEN** the dialog fetches and displays the document count in the subtree
- **AND** the count is fetched before the confirm button becomes actionable

#### Scenario: In-flight session scope change is surfaced
- **WHEN** the preview shows "47 documents will be affected"
- **THEN** the dialog context makes clear that this operation immediately affects retrieval scope for sessions using these tags
