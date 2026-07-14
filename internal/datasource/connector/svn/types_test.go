package svn

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSvnConfig_Valid(t *testing.T) {
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": "https://svn.example.com/repo",
			"username": "alice",
			"password": "secret",
		},
		Settings: map[string]interface{}{
			"file_extensions": []interface{}{".md", ".txt"},
			"exclude_paths":   []interface{}{"draft/*"},
			"max_file_size":   float64(1024 * 1024),
		},
	}

	cfg, err := parseSvnConfig(config)
	require.NoError(t, err)
	assert.Equal(t, "https://svn.example.com/repo", cfg.RepoURL)
	assert.Equal(t, "alice", cfg.Username)
	assert.Equal(t, "secret", cfg.Password)
	assert.Equal(t, []string{".md", ".txt"}, cfg.FileExtensions)
	assert.Equal(t, []string{"draft/*"}, cfg.ExcludePaths)
	assert.Equal(t, int64(1024*1024), cfg.MaxFileSize)
}

func TestParseSvnConfig_MissingRepoURL(t *testing.T) {
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"username": "alice",
		},
	}

	_, err := parseSvnConfig(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo_url is required")
}

func TestParseSvnConfig_NilConfig(t *testing.T) {
	_, err := parseSvnConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestParseSvnConfig_EmptyCredentials(t *testing.T) {
	config := &types.DataSourceConfig{}

	_, err := parseSvnConfig(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo_url is required")
}

func TestParseSvnConfig_TrailingSlashTrimmed(t *testing.T) {
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": "svn://svn.example.com/repo/",
		},
	}

	cfg, err := parseSvnConfig(config)
	require.NoError(t, err)
	assert.Equal(t, "svn://svn.example.com/repo", cfg.RepoURL)
}

func TestConfig_GetMaxFileSize_Default(t *testing.T) {
	cfg := &Config{}
	assert.Equal(t, defaultMaxFileSize, cfg.GetMaxFileSize())
}

func TestConfig_GetMaxFileSize_Custom(t *testing.T) {
	cfg := &Config{MaxFileSize: 100}
	assert.Equal(t, int64(100), cfg.GetMaxFileSize())
}

func TestShouldInclude_ExtensionWhitelist(t *testing.T) {
	cfg := &Config{FileExtensions: []string{".md"}}

	assert.True(t, shouldInclude("/docs/readme.md", 100, cfg))
	assert.False(t, shouldInclude("/docs/image.png", 100, cfg))
}

func TestShouldInclude_NoExtensionFilter(t *testing.T) {
	cfg := &Config{}
	assert.True(t, shouldInclude("/docs/anything.xyz", 100, cfg))
}

func TestShouldInclude_MaxFileSize(t *testing.T) {
	cfg := &Config{MaxFileSize: 1000}
	assert.True(t, shouldInclude("/docs/small.md", 500, cfg))
	assert.False(t, shouldInclude("/docs/big.md", 2000, cfg))
}

func TestShouldInclude_ExcludePaths(t *testing.T) {
	cfg := &Config{
		ExcludePaths: []string{"draft/*", "*/temp/*"},
	}

	assert.False(t, shouldInclude("/draft/wip.md", 100, cfg))
	assert.False(t, shouldInclude("/docs/temp/cache.md", 100, cfg))
	assert.True(t, shouldInclude("/docs/final.md", 100, cfg))
}

func TestGuessContentType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"readme.md", "text/markdown"},
		{"notes.txt", "text/plain"},
		{"page.html", "text/html"},
		{"doc.pdf", "application/pdf"},
		{"data.json", "application/json"},
		{"unknown.xyz", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, guessContentType(tt.path))
		})
	}
}

func TestJoinURLPath(t *testing.T) {
	assert.Equal(t, "svn://host/repo", joinURLPath("svn://host/repo", ""))
	assert.Equal(t, "svn://host/repo/docs", joinURLPath("svn://host/repo", "/docs"))
	assert.Equal(t, "svn://host/repo/docs/readme.md", joinURLPath("svn://host/repo", "/docs/readme.md"))
}

func TestResolveResourceAncestors(t *testing.T) {
	c := NewConnector()
	ancestors, err := c.ResolveResourceAncestors(nil, nil, []string{"/docs/architecture/overview"})
	require.NoError(t, err)

	assert.Contains(t, ancestors, "/docs")
	assert.Contains(t, ancestors, "/docs/architecture")
	assert.NotContains(t, ancestors, "/docs/architecture/overview")
}

func TestItemToChangeType(t *testing.T) {
	assert.Equal(t, "A", itemToChangeType("added"))
	assert.Equal(t, "M", itemToChangeType("modified"))
	assert.Equal(t, "D", itemToChangeType("deleted"))
	assert.Equal(t, "", itemToChangeType("none"))
	assert.Equal(t, "", itemToChangeType(""))
}
