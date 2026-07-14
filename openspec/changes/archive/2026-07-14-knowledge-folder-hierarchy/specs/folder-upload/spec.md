## ADDED Requirements

### Requirement: Multi-file folder upload with structure preservation

The system SHALL accept uploading an entire local folder via `POST /api/v1/knowledge-bases/:id/knowledge/folder`. The request contains multiple files and a parallel `paths[]` array of relative paths (from `webkitRelativePath`). The backend reconstructs the directory structure as folder records and assigns each file to the correct folder.

#### Scenario: Uploading a nested folder
- **WHEN** a user uploads 3 files: `project/readme.md`, `project/src/main.go`, `project/docs/guide.md`
- **THEN** folders "project", "project/src", "project/docs" are created in the KB
- **AND** each file is assigned to its corresponding folder via `folder_id`

#### Scenario: Upload to a subfolder destination
- **WHEN** the upload includes `root_folder_id` pointing to an existing folder "Imports"
- **AND** the relative paths are `api/v1.go`, `api/v2.go`
- **THEN** folders "Imports/api" are created, and files are placed inside

### Requirement: Atomic folder tree reconstruction

Folder tree reconstruction during upload SHALL be atomic: all folder records for the upload are created in a single database transaction. If any folder creation fails, the entire batch is rolled back — no partial folder trees are persisted.

#### Scenario: Upload fails mid-creation
- **WHEN** folder tree reconstruction encounters a unique-name conflict on the 3rd of 5 folders
- **THEN** all 5 folder insertions are rolled back and the upload returns a conflict error

### Requirement: ZIP archive upload with server-side extraction

The system SHALL accept a `.zip` archive via `POST /api/v1/knowledge-bases/:id/knowledge/zip`. The backend extracts the archive server-side, reconstructs the directory tree as folder records, and processes each contained file as a knowledge entry.

#### Scenario: Uploading a ZIP with nested directories
- **WHEN** a user uploads `archive.zip` containing `docs/readme.txt`, `src/app.py`, `config.json`
- **THEN** folders "docs" and "src" are created; `readme.txt` is placed in "docs", `app.py` in "src", `config.json` at root

#### Scenario: ZIP with empty directories
- **WHEN** a ZIP contains an empty directory `empty-dir/`
- **THEN** the empty folder is created but contains no knowledge entries

### Requirement: Folder upload ownership validation

Each folder created during upload SHALL be validated for tenant and KB ownership. The `root_folder_id`, if provided, MUST belong to the same tenant and KB as the upload target.

#### Scenario: Upload to a folder from a different KB
- **WHEN** `root_folder_id` belongs to KB "A" but the upload targets KB "B"
- **THEN** the upload is rejected with 403 Forbidden

### Requirement: Existing upload endpoints gain optional folder targeting

The existing `POST /knowledge/file` and `POST /knowledge/url` endpoints SHALL accept an optional `folder_id` parameter. When provided, the uploaded/imported knowledge entry is assigned to that folder. When omitted, the entry is placed at KB root (`folder_id = NULL`).

#### Scenario: File upload to a specific folder
- **WHEN** a user uploads a file via `POST /knowledge/file` with form field `folder_id = "<folder-uuid>"`
- **THEN** the created knowledge entry has `folder_id` set to the specified folder

#### Scenario: File upload without folder_id
- **WHEN** a user uploads a file without `folder_id`
- **THEN** the created knowledge entry has `folder_id = NULL` (root level)

### Requirement: Upload preserves tag selection

When tags are selected during upload confirmation, the `tag_ids` SHALL be applied to every knowledge entry created in the batch, including those created from folder and ZIP uploads.

#### Scenario: Folder upload with tags
- **WHEN** a user selects tags "important" and "reference" and uploads a folder containing 5 files
- **THEN** all 5 created knowledge entries have both tags applied
