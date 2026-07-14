package svn

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Compile-time proof that *Connector satisfies the datasource.Connector interface.
var _ datasource.Connector = (*Connector)(nil)

// Connector implements datasource.Connector for SVN repositories.
type Connector struct {
	// newCLIFunc is overrideable for test injection. In production it returns
	// a *cliImpl that shells out to the real svn executable.
	newCLIFunc func(cfg *Config) svnCLI
}

// NewConnector creates a new SVN connector.
func NewConnector() *Connector {
	return &Connector{
		newCLIFunc: func(cfg *Config) svnCLI { return newCLI(cfg) },
	}
}

// Type returns the connector type identifier.
func (c *Connector) Type() string { return types.ConnectorTypeSVN }

// Validate verifies that svn is installed, the URL passes SSRF checks, and
// the repository is reachable with the provided credentials.
func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	if !svnInstalled() {
		return fmt.Errorf("svn CLI is not installed or not in PATH")
	}

	cfg, err := parseSvnConfig(config)
	if err != nil {
		return err
	}

	if err := datasource.ValidateConnectorBaseURL(cfg.RepoURL); err != nil {
		return fmt.Errorf("repo_url SSRF validation failed: %w", err)
	}

	cli := c.newCLIFunc(cfg)
	info, err := cli.Info(ctx, cfg.RepoURL)
	if err != nil {
		return fmt.Errorf("svn connection failed: %w", err)
	}

	logger.Infof(ctx, "[SVN] validated repo: url=%s revision=%d uuid=%s", cfg.RepoURL, info.Revision, info.UUID)
	return nil
}

// ListResources returns a single-level directory listing for the resource picker.
// parentID == "" lists the repository root; otherwise lists the given subdirectory.
func (c *Connector) ListResources(
	ctx context.Context, config *types.DataSourceConfig, parentID string,
) ([]types.Resource, error) {
	cfg, err := parseSvnConfig(config)
	if err != nil {
		return nil, err
	}

	cli := c.newCLIFunc(cfg)
	entries, err := cli.List(ctx, cfg.RepoURL, parentID)
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", parentID, err)
	}

	resources := make([]types.Resource, 0, len(entries))
	for _, e := range entries {
		entryPath := e.Name
		if parentID != "" {
			entryPath = parentID + "/" + e.Name
		}
		entryPath = "/" + strings.TrimPrefix(entryPath, "/")

		isDir := e.Kind == "dir"
		resources = append(resources, types.Resource{
			ExternalID:  entryPath,
			Name:        e.Name,
			Type:        e.Kind,
			URL:         joinURLPath(cfg.RepoURL, entryPath),
			ModifiedAt:  parseSVNDate(e.Commit.Date),
			HasChildren: isDir,
		})
	}

	return resources, nil
}

// ResolveResourceAncestors returns all ancestor directory IDs for the given
// resource paths, enabling the lazy-loading picker to expand to pre-existing
// selections.
func (c *Connector) ResolveResourceAncestors(
	_ context.Context, _ *types.DataSourceConfig, resourceIDs []string,
) ([]string, error) {
	ancestors := make(map[string]bool)
	for _, resID := range resourceIDs {
		resID = strings.TrimPrefix(resID, "/")
		parts := strings.Split(resID, "/")
		for i := 1; i < len(parts); i++ {
			ancestor := "/" + strings.Join(parts[:i], "/")
			ancestors[ancestor] = true
		}
	}
	result := make([]string, 0, len(ancestors))
	for a := range ancestors {
		result = append(result, a)
	}
	return result, nil
}

// FetchAll discovers all files under the selected paths and returns their content.
func (c *Connector) FetchAll(
	ctx context.Context, config *types.DataSourceConfig, resourceIDs []string,
) ([]types.FetchedItem, error) {
	cfg, err := parseSvnConfig(config)
	if err != nil {
		return nil, err
	}
	cli := c.newCLIFunc(cfg)

	info, err := cli.Info(ctx, cfg.RepoURL)
	if err != nil {
		return nil, fmt.Errorf("get repo info: %w", err)
	}

	return c.fetchAllWithInfo(ctx, cfg, cli, resourceIDs, info)
}

// fetchAllWithInfo is the shared full-sync implementation used by both FetchAll
// and the UUID-change fallback in FetchIncremental.
func (c *Connector) fetchAllWithInfo(
	ctx context.Context, cfg *Config, cli svnCLI, resourceIDs []string, info *repoInfo,
) ([]types.FetchedItem, error) {
	var items []types.FetchedItem
	var partialDetails []string

	for _, resID := range resourceIDs {
		entries, err := cli.ListRecursive(ctx, cfg.RepoURL, resID)
		if err != nil {
			partialDetails = append(partialDetails, fmt.Sprintf("list %s: %v", resID, err))
			continue
		}

		for _, e := range entries {
			if e.Kind != "file" {
				continue
			}
			filePath := buildFilePath(resID, e.Name)
			if !shouldInclude(filePath, e.Size, cfg) {
				continue
			}

			content, err := cli.Cat(ctx, cfg.RepoURL, filePath, info.Revision)
			if err != nil {
				partialDetails = append(partialDetails, fmt.Sprintf("cat %s: %v", filePath, err))
				continue
			}

			items = append(items, buildFetchedItem(filePath, content, e, resID))
		}
	}

	if len(partialDetails) > 0 && len(items) == 0 {
		return nil, fmt.Errorf("fetch all failed: %s", strings.Join(partialDetails, "; "))
	}
	if len(partialDetails) > 0 {
		return items, &datasource.PartialFetchError{Details: partialDetails}
	}
	return items, nil
}

// FetchIncremental uses `svn diff --summarize` to detect exact A/M/D changes
// since the cursor's last revision.
func (c *Connector) FetchIncremental(
	ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	cfg, err := parseSvnConfig(config)
	if err != nil {
		return nil, nil, err
	}
	cli := c.newCLIFunc(cfg)

	info, err := cli.Info(ctx, cfg.RepoURL)
	if err != nil {
		return nil, nil, fmt.Errorf("get repo info: %w", err)
	}

	// Parse prior cursor
	var prev svnCursor
	if cursor != nil && cursor.ConnectorCursor != nil {
		b, _ := json.Marshal(cursor.ConnectorCursor)
		_ = json.Unmarshal(b, &prev)
	}

	// UUID change → repo was migrated; fall back to full sync
	if prev.RepoUUID != "" && prev.RepoUUID != info.UUID {
		logger.Infof(ctx, "[SVN] repo UUID changed (%s → %s), falling back to full sync", prev.RepoUUID, info.UUID)
		items, ferr := c.fetchAllWithInfo(ctx, cfg, cli, config.ResourceIDs, info)
		if ferr != nil {
			return items, nil, ferr
		}
		return items, buildCursor(info), nil
	}

	// First sync (no cursor) → full sync
	if prev.LastRevision == 0 {
		items, ferr := c.fetchAllWithInfo(ctx, cfg, cli, config.ResourceIDs, info)
		if ferr != nil {
			return items, nil, ferr
		}
		return items, buildCursor(info), nil
	}

	// No changes since last sync
	if info.Revision <= prev.LastRevision {
		return []types.FetchedItem{}, buildCursor(info), nil
	}

	// Incremental: diff --summarize
	var items []types.FetchedItem
	var partialDetails []string

	for _, resID := range config.ResourceIDs {
		diffs, err := cli.DiffSummarize(ctx, cfg.RepoURL, resID, prev.LastRevision, info.Revision)
		if err != nil {
			partialDetails = append(partialDetails, fmt.Sprintf("diff %s: %v", resID, err))
			continue
		}

		for _, d := range diffs {
			filePath := normalizeDiffPath(d.Path, info.Root, cfg.RepoURL)

			if d.Type == "D" {
				items = append(items, types.FetchedItem{
					ExternalID:       filePath,
					IsDeleted:        true,
					SourceResourceID: resID,
					Metadata: map[string]string{
						"channel": types.ChannelSVN,
					},
				})
				continue
			}

			// A or M — need file size for filtering
			// svn diff --summarize doesn't report size; we proceed with fetch
			// and rely on extension/path filters only for incremental.
			if !shouldIncludeByPath(filePath, cfg) {
				continue
			}

			content, err := cli.Cat(ctx, cfg.RepoURL, filePath, info.Revision)
			if err != nil {
				partialDetails = append(partialDetails, fmt.Sprintf("cat %s: %v", filePath, err))
				continue
			}

			items = append(items, types.FetchedItem{
				ExternalID:       filePath,
				Title:            filepath.Base(filePath),
				Content:          content,
				ContentType:      guessContentType(filePath),
				FileName:         filepath.Base(filePath),
				URL:              joinURLPath(cfg.RepoURL, filePath),
				UpdatedAt:        info.RevisionToTime(),
				SourceResourceID: resID,
				Metadata: map[string]string{
					"channel":   types.ChannelSVN,
					"revision":  strconv.FormatInt(info.Revision, 10),
					"file_path": filePath,
					"change":    d.Type,
				},
			})
		}
	}

	if len(partialDetails) > 0 {
		return items, buildCursor(info), &datasource.PartialFetchError{Details: partialDetails}
	}
	return items, buildCursor(info), nil
}

// buildCursor creates a SyncCursor from the repo info.
func buildCursor(info *repoInfo) *types.SyncCursor {
	return &types.SyncCursor{
		LastSyncTime: time.Now().UTC(),
		ConnectorCursor: map[string]interface{}{
			"last_revision": info.Revision,
			"repo_uuid":     info.UUID,
		},
	}
}

// shouldInclude checks extension whitelist, exclude paths, and file size.
func shouldInclude(filePath string, size int64, cfg *Config) bool {
	if !shouldIncludeByPath(filePath, cfg) {
		return false
	}
	if size > 0 && size > cfg.GetMaxFileSize() {
		return false
	}
	return true
}

// shouldIncludeByPath checks extension whitelist and exclude path patterns only.
func shouldIncludeByPath(filePath string, cfg *Config) bool {
	// Extension whitelist
	if len(cfg.FileExtensions) > 0 {
		ext := strings.ToLower(filepath.Ext(filePath))
		matched := false
		for _, allowed := range cfg.FileExtensions {
			if strings.ToLower(allowed) == ext {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Exclude paths (glob matching)
	for _, pattern := range cfg.ExcludePaths {
		matched, _ := filepath.Match(pattern, filepath.Base(filePath))
		if matched {
			return false
		}
		// Also try matching against full relative path
		trimmedPath := strings.TrimPrefix(filePath, "/")
		matched, _ = filepath.Match(pattern, trimmedPath)
		if matched {
			return false
		}
		// Prefix match for directory patterns like "draft/*"
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if strings.HasPrefix(trimmedPath, prefix+"/") {
				return false
			}
		}
	}

	return true
}

// buildFilePath constructs the full file path from a resource ID and entry name.
// In recursive listing, the entry name already contains the relative path.
func buildFilePath(resID, entryName string) string {
	resID = strings.TrimPrefix(resID, "/")
	if resID == "" {
		return "/" + entryName
	}
	return "/" + resID + "/" + entryName
}

// buildFetchedItem creates a FetchedItem from a file entry.
func buildFetchedItem(filePath string, content []byte, entry listEntry, resID string) types.FetchedItem {
	return types.FetchedItem{
		ExternalID:       filePath,
		Title:            filepath.Base(filePath),
		Content:          content,
		ContentType:      guessContentType(filePath),
		FileName:         filepath.Base(filePath),
		URL:              "", // Not available without repo root; can be built if needed
		UpdatedAt:        parseSVNDate(entry.Commit.Date),
		SourceResourceID: resID,
		Metadata: map[string]string{
			"channel":   types.ChannelSVN,
			"file_path": filePath,
			"size":      strconv.FormatInt(entry.Size, 10),
		},
	}
}

// normalizeDiffPath converts a path from svn diff --summarize output (which may
// be relative to the repo root or an absolute URL) to a clean repo-relative path.
func normalizeDiffPath(diffPath, repoRoot, repoURL string) string {
	p := strings.TrimSpace(diffPath)
	// svn diff --summarize on a URL returns paths relative to that URL
	// strip leading slashes for consistency
	p = strings.TrimPrefix(p, "/")
	return "/" + p
}

// guessContentType returns a MIME type based on file extension.
func guessContentType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls", ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt", ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".epub":
		return "application/epub+zip"
	default:
		return "application/octet-stream"
	}
}

// parseSVNDate parses an SVN ISO 8601 timestamp.
func parseSVNDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// SVN dates look like 2024-01-15T10:30:00.000000Z
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// RevisionToTime is a placeholder — the revision number itself doesn't map to
// a timestamp. We use it only when we don't have a per-file commit date.
func (info *repoInfo) RevisionToTime() time.Time {
	return time.Now().UTC()
}
