## ADDED Requirements

### Requirement: SVN connector type registration

The system SHALL register `"svn"` as a valid connector type in `ConnectorMetadataRegistry` with capabilities `["incremental", "deletion_sync"]` and auth type `"custom"`. The system SHALL register the SVN connector instance in `initConnectorRegistry()`.

#### Scenario: SVN connector listed in available connectors

- **WHEN** the frontend requests available connector types via `ListAvailableConnectors()`
- **THEN** the response SHALL include an entry with `type: "svn"`, `name: "SVN Repository"`, and capabilities `["incremental", "deletion_sync"]`

#### Scenario: SVN connector resolved by registry

- **WHEN** `ProcessSync` looks up a data source with `type: "svn"` in the `ConnectorRegistry`
- **THEN** the registry SHALL return the registered SVN connector instance without error

### Requirement: SVN connector configuration

The SVN connector SHALL accept the following configuration fields:
- `repo_url` (required): SVN repository URL, supporting `svn://`, `http://`, `https://`, `svn+ssh://` schemes
- `username` (optional): SVN username for authentication
- `password` (optional): SVN password, stored AES-256-GCM encrypted in Credentials
- `file_extensions` (optional, Settings): whitelist of file extensions to sync (e.g., `[".md", ".txt", ".pdf"]`). Empty = sync all
- `exclude_paths` (optional, Settings): glob patterns to exclude (e.g., `["draft/*", "*/temp/*"]`)
- `max_file_size` (optional, Settings): maximum file size in bytes, default 50MB (52428800)

#### Scenario: Configuration with credentials

- **WHEN** a user creates a data source with `repo_url: "svn://svn.example.com/docs"`, `username: "alice"`, `password: "secret"`
- **THEN** the system SHALL store `username` and `password` in the encrypted Credentials map, and store `repo_url` in Settings

#### Scenario: Missing repo_url rejected

- **WHEN** a user creates a data source without `repo_url`
- **THEN** `Validate` SHALL return `ErrInvalidConfig` with message indicating `repo_url` is required

### Requirement: Connectivity validation

`Validate` SHALL execute `svn info --non-interactive <repo_url>` to test connectivity. On success, it SHALL parse the XML response to confirm the repository is accessible. On failure, it SHALL return a descriptive error.

#### Scenario: Successful validation

- **WHEN** `Validate` is called with a valid `repo_url` and correct credentials
- **THEN** the system SHALL execute `svn info --non-interactive <repo_url>` and return nil (no error)

#### Scenario: Invalid credentials

- **WHEN** `Validate` is called with incorrect credentials
- **THEN** the system SHALL return an error indicating authentication failure

#### Scenario: svn CLI not installed

- **WHEN** `Validate` is called and the `svn` executable is not found in PATH
- **THEN** the system SHALL return an error indicating `svn` CLI is not installed

### Requirement: Directory tree resource listing

`ListResources` SHALL return directory listings from the SVN repository via `svn list --xml`. When `parentID` is empty, it SHALL list the repository root. When `parentID` is a path, it SHALL list that subdirectory. Each directory entry SHALL have `HasChildren: true`; each file entry SHALL have `HasChildren: false`.

#### Scenario: List root directory

- **WHEN** `ListResources` is called with `parentID: ""`
- **THEN** the system SHALL execute `svn list --xml <repo_url>` and return a list of `Resource` items for each root-level entry

#### Scenario: List subdirectory lazily

- **WHEN** `ListResources` is called with `parentID: "/docs/architecture"`
- **THEN** the system SHALL execute `svn list --xml <repo_url>/docs/architecture` and return only direct children of that path

### Requirement: Full sync via FetchAll

`FetchAll` SHALL discover all files in the selected resource paths via `svn list -R`, then fetch each file's content via `svn cat -r HEAD`. Files SHALL be filtered by `file_extensions`, `exclude_paths`, and `max_file_size` before fetching content.

#### Scenario: Full sync of selected directory

- **WHEN** `FetchAll` is called with `resourceIDs: ["/docs"]`
- **THEN** the system SHALL recursively list all files under `/docs`, filter by extension/size/exclude rules, and return a `FetchedItem` with content bytes for each matching file

#### Scenario: File extension filtering

- **WHEN** `file_extensions: [".md", ".txt"]` is configured and a directory contains `readme.md`, `image.png`, `data.xlsx`
- **THEN** `FetchAll` SHALL only return `FetchedItem` items for `readme.md` and `data.xlsx` (if xlsx is in extensions), skipping `image.png`

#### Scenario: Max file size enforcement

- **WHEN** `max_file_size: 1048576` (1MB) is configured and a file is 5MB
- **THEN** `FetchAll` SHALL skip that file and not fetch its content via `svn cat`

### Requirement: Incremental sync via FetchIncremental

`FetchIncremental` SHALL use `svn diff --summarize -r <last_revision>:HEAD --xml` to detect exact Added/Modified/Deleted changes. For A and M items, it SHALL fetch content via `svn cat -r HEAD`. For D items, it SHALL emit `FetchedItem` with `IsDeleted: true`. The returned cursor SHALL contain the new HEAD revision and repository UUID.

#### Scenario: Incremental sync detects new file

- **WHEN** `FetchIncremental` is called with `cursor.LastRevision: 1000` and a file was added at r1005
- **THEN** the system SHALL execute `svn diff --summarize -r 1000:HEAD` and return a `FetchedItem` with content for the new file

#### Scenario: Incremental sync detects deleted file

- **WHEN** `FetchIncremental` is called and a file was deleted between `LastRevision` and HEAD
- **THEN** the system SHALL return a `FetchedItem` with `IsDeleted: true` and `ExternalID` matching the deleted file path

#### Scenario: Incremental sync detects modified file

- **WHEN** `FetchIncremental` is called and a file was modified between `LastRevision` and HEAD
- **THEN** the system SHALL return a `FetchedItem` with updated content bytes fetched via `svn cat -r HEAD`

#### Scenario: Repository UUID change triggers fallback

- **WHEN** `FetchIncremental` is called and the repository UUID differs from the cursor's stored UUID
- **THEN** the system SHALL fall back to `FetchAll` behavior (full re-sync)

### Requirement: Cursor persistence

The SVN cursor SHALL contain exactly `last_revision` (int64), `repo_uuid` (string), and `last_sync_time`. The cursor SHALL be persisted to `DataSource.LastSyncCursor` after each successful sync by the existing `ProcessSync` infrastructure.

#### Scenario: Cursor updated after successful sync

- **WHEN** a sync completes successfully with HEAD at r1005
- **THEN** `DataSource.LastSyncCursor` SHALL contain `{"last_sync_time": "...", "connector_cursor": {"last_revision": 1005, "repo_uuid": "..."}}`

### Requirement: ExternalID as file path

Each `FetchedItem.ExternalID` SHALL be the repository-relative file path (e.g., `/docs/readme.md`). This path is stable across revisions and serves as the dedup key for `ingestItem`'s update detection (matching via `metadata.external_id`).

#### Scenario: ExternalID used for update detection

- **WHEN** a file `/docs/guide.md` is modified and re-synced
- **THEN** `ingestItem` SHALL find the existing knowledge item by `metadata.external_id = "/docs/guide.md"`, delete it, and create a new one with updated content

### Requirement: Command security

All SVN CLI invocations SHALL use `exec.Command("svn", args...)` with arguments as separate string values. User-supplied input (URL, paths, credentials) SHALL NEVER be interpolated into a shell string. All calls SHALL include `--non-interactive` to prevent interactive prompts.

#### Scenario: No shell injection

- **WHEN** a malicious user sets `repo_url` to `svn://host/repo; rm -rf /`
- **THEN** the entire string SHALL be passed as a single argument to `svn info`, resulting in a connection error, NOT shell command execution

### Requirement: SSRF protection

`Validate` SHALL verify the `repo_url` against private IP ranges (loopback, link-local, RFC1918, metadata endpoints) before attempting connection, using the existing `datasource.ValidateConnectorBaseURL` infrastructure or equivalent validation.

#### Scenario: Private IP rejected

- **WHEN** `repo_url` is set to `svn://127.0.0.1/repo` or `svn://169.254.169.254/repo`
- **THEN** `Validate` SHALL reject the URL with an SSRF protection error

### Requirement: Partial fetch resilience

When some files fail to fetch (e.g., `svn cat` errors on one file out of 100), the connector SHALL continue processing remaining files and return a `PartialFetchError` with details of failed paths, rather than failing the entire sync.

#### Scenario: Some files fail during full sync

- **WHEN** `FetchAll` encounters an error on `svn cat` for one file but others succeed
- **THEN** the connector SHALL return successfully fetched items plus a `PartialFetchError` containing the failed file paths and error messages
