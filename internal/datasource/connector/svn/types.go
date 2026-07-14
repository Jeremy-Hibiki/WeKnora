// Package svn implements the SVN data source connector for WeKnora.
//
// It syncs documents from SVN repositories (svn://, http(s)://, svn+ssh://)
// by shelling out to the `svn` CLI — no local checkout is performed.
// Incremental sync uses `svn diff --summarize -r OLD:HEAD` for exact A/M/D
// change detection, keyed off a simple revision-number cursor.
//
// SVN CLI reference:
//   - info:    svn info <url> --xml
//   - list:    svn list <url>/<path> --xml  (single level)
//   - list -R: svn list -R <url>/<path> --xml (recursive)
//   - cat:     svn cat -r <rev> <url>/<path>
//   - diff:    svn diff --summarize -r <old>:<new> <url>/<path> --xml
package svn

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

// defaultMaxFileSize is the default file size limit (50 MB).
const defaultMaxFileSize int64 = 50 * 1024 * 1024

// Config holds SVN-specific configuration.
type Config struct {
	// RepoURL is the SVN repository root URL.
	// Supports svn://, http://, https://, svn+ssh:// schemes.
	RepoURL string `json:"repo_url"`

	// Username for SVN authentication (optional).
	Username string `json:"username"`

	// Password for SVN authentication (optional).
	Password string `json:"password"`

	// FileExtensions is a whitelist of file extensions to sync (e.g., [".md", ".txt"]).
	// Empty = sync all file types.
	FileExtensions []string `json:"file_extensions,omitempty"`

	// ExcludePaths is a list of glob patterns to exclude (e.g., ["draft/*", "*/temp/*"]).
	ExcludePaths []string `json:"exclude_paths,omitempty"`

	// MaxFileSize is the maximum file size in bytes. 0 = use defaultMaxFileSize.
	MaxFileSize int64 `json:"max_file_size,omitempty"`
}

// GetMaxFileSize returns the effective max file size.
func (c *Config) GetMaxFileSize() int64 {
	if c.MaxFileSize <= 0 {
		return defaultMaxFileSize
	}
	return c.MaxFileSize
}

// parseSvnConfig extracts and validates SVN-specific configuration from a DataSourceConfig.
// Credentials (repo_url, username, password) come from config.Credentials;
// settings (file_extensions, exclude_paths, max_file_size) come from config.Settings.
func parseSvnConfig(config *types.DataSourceConfig) (*Config, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: config is nil", datasource.ErrInvalidConfig)
	}

	cfg := &Config{}

	// Parse credentials (repo_url, username, password)
	if len(config.Credentials) > 0 {
		credBytes, err := json.Marshal(config.Credentials)
		if err != nil {
			return nil, fmt.Errorf("marshal credentials: %w", err)
		}
		// Unmarshal into a temp struct to avoid overwriting Settings fields
		var creds struct {
			RepoURL  string `json:"repo_url"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(credBytes, &creds); err != nil {
			return nil, fmt.Errorf("parse svn credentials: %w", err)
		}
		cfg.RepoURL = creds.RepoURL
		cfg.Username = creds.Username
		cfg.Password = creds.Password
	}

	// Parse settings (file_extensions, exclude_paths, max_file_size)
	if len(config.Settings) > 0 {
		settingsBytes, err := json.Marshal(config.Settings)
		if err != nil {
			return nil, fmt.Errorf("marshal settings: %w", err)
		}
		if err := json.Unmarshal(settingsBytes, cfg); err != nil {
			return nil, fmt.Errorf("parse svn settings: %w", err)
		}
	}

	if strings.TrimSpace(cfg.RepoURL) == "" {
		return nil, fmt.Errorf("%w: repo_url is required", datasource.ErrInvalidCredentials)
	}

	cfg.RepoURL = strings.TrimRight(strings.TrimSpace(cfg.RepoURL), "/")

	return cfg, nil
}

// svnCursor stores incremental sync state — just a revision number + repo UUID.
type svnCursor struct {
	LastSyncTime interface{} `json:"last_sync_time,omitempty"`
	LastRevision int64       `json:"last_revision"`
	RepoUUID     string      `json:"repo_uuid"`
}

// --- SVN CLI XML response types ---

// infoXML is the parsed response of `svn info --xml`.
type infoXML struct {
	Entries []infoEntry `xml:"entry"`
}

type infoEntry struct {
	Revision string     `xml:"revision,attr"`
	Path     string     `xml:"path,attr"`
	URL      string     `xml:"url"`
	Root     string     `xml:"repository>root"`
	UUID     string     `xml:"repository>uuid"`
	Commit   commitInfo `xml:"commit"`
}

type commitInfo struct {
	Revision string `xml:"revision,attr"`
	Author   string `xml:"author"`
	Date     string `xml:"date"`
}

// repoInfo is the extracted result from `svn info`.
type repoInfo struct {
	Revision int64
	UUID     string
	Root     string
	URL      string
}

// listXML is the parsed response of `svn list --xml` and `svn list -R --xml`.
type listXML struct {
	Entries []listEntry `xml:"entry"`
}

// listEntry represents a single entry in an SVN directory listing.
type listEntry struct {
	Name   string     `xml:"name"`
	Kind   string     `xml:"kind,attr"` // "file" or "dir"
	Size   int64      `xml:"size"`
	Commit commitInfo `xml:"commit"`
}

// diffSummarizeXML is the parsed response of `svn diff --summarize --xml`.
type diffSummarizeXML struct {
	Paths []diffPath `xml:"path"`
}

// diffPath represents a single changed path in a diff summary.
type diffPath struct {
	Path  string `xml:",chardata"`
	Props string `xml:"props,attr"` // "none", "modified"
	Kind  string `xml:"kind,attr"`  // "file", "dir"
	Item  string `xml:"item,attr"`  // "added", "modified", "deleted", "none"
}

// diffEntry is the extracted result from `svn diff --summarize`.
type diffEntry struct {
	Path string
	Type string // "A" (added), "M" (modified), "D" (deleted)
}

// --- SVN CLI interface for testability ---

// svnCLI is the interface for SVN CLI operations, allowing mock implementations in tests.
type svnCLI interface {
	Info(ctx context.Context, repoURL string) (*repoInfo, error)
	List(ctx context.Context, repoURL, path string) ([]listEntry, error)
	ListRecursive(ctx context.Context, repoURL, path string) ([]listEntry, error)
	Cat(ctx context.Context, repoURL, path string, revision int64) ([]byte, error)
	DiffSummarize(ctx context.Context, repoURL, path string, oldRev, newRev int64) ([]diffEntry, error)
}

// svnInstalled checks whether the `svn` executable is in PATH.
func svnInstalled() bool {
	_, err := exec.LookPath("svn")
	return err == nil
}
