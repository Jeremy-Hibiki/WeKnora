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
	tests := []struct {
		name     string
		patterns []string
		path     string
		exclude  bool
	}{
		// Top-level directory pattern
		{"top-level draft/*", []string{"draft/*"}, "/draft/wip.md", true},
		{"top-level draft/* keep", []string{"draft/*"}, "/docs/final.md", false},

		// Nested directory — was silently missed before fix
		{"nested draft/*", []string{"draft/*"}, "/docs/draft/secret.md", true},
		{"deeply nested draft/*", []string{"draft/*"}, "/a/b/draft/c/d.md", true},

		// */temp/* pattern — requires a segment before temp
		{"temp one level", []string{"*/temp/*"}, "/docs/temp/cache.md", true},
		{"temp at root no match", []string{"*/temp/*"}, "/temp/cache.md", false},
		{"temp deeper", []string{"*/temp/*"}, "/a/temp/b/c.md", true},

		// Suffix pattern
		{"suffix *.tmp", []string{"*.tmp"}, "/docs/readme.tmp", true},
		{"suffix *.tmp nested", []string{"*.tmp"}, "/a/b/c.tmp", true},
		{"suffix keep", []string{"*.tmp"}, "/docs/readme.md", false},

		// ** (double-star) patterns
		{"double-star draft/**", []string{"draft/**"}, "/docs/draft/deep/secret.md", true},
		{"double-star draft/** top", []string{"draft/**"}, "/draft/x.md", true},
		{"double-star **/temp/**", []string{"**/temp/**"}, "/a/temp/b/c.md", true},

		// Non-matching
		{"no match", []string{"draft/*"}, "/docs/final.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{ExcludePaths: tt.patterns}
			got := shouldInclude(tt.path, 100, cfg)
			if tt.exclude {
				assert.False(t, got, "%q should be excluded by %v", tt.path, tt.patterns)
			} else {
				assert.True(t, got, "%q should NOT be excluded by %v", tt.path, tt.patterns)
			}
		})
	}
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

func TestGlobToRegexp(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		// Literal
		{"docs/readme.md", "docs/readme.md", true},
		{"docs/readme.md", "docs/other.md", false},
		// Single * crosses / in our implementation
		{"draft/*", "draft/secret.md", true},
		{"*.tmp", "a/b/c.tmp", true},
		// ** equivalent to *
		{"draft/**", "draft/deep/secret.md", true},
		// ? matches any single char
		{"file?.md", "file1.md", true},
		{"file?.md", "file12.md", false},
		// Special regex chars are escaped
		{"file.md", "fileXmd", false}, // dot is literal
		{"a+b", "a+b", true},          // plus is literal
		// Trailing * matches zero or more
		{"docs/*", "docs/", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.path, func(t *testing.T) {
			re, err := globToRegexp(tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.match, re.MatchString(tt.path))
		})
	}
}

func TestGlobToRegexp_SpecialChars(t *testing.T) {
	// Bracket chars are escaped to literals in our glob syntax (no char classes)
	re, err := globToRegexp("[")
	require.NoError(t, err)
	assert.True(t, re.MatchString("["))
	assert.False(t, re.MatchString("a"))
}
