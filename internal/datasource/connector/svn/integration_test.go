//go:build integration

// Integration tests for the SVN connector. These require svnadmin, svnserve,
// and svn CLI to be installed. Run with:
//
//	go test -tags=integration -v ./internal/datasource/connector/svn/...

package svn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRepo holds the paths and URL for a local test SVN repository.
type testRepo struct {
	t        *testing.T
	repoPath string
	serveURL string
	serveCmd *exec.Cmd
	workDir  string
}

func setupTestRepo(t *testing.T) *testRepo {
	t.Helper()
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	workDir := filepath.Join(tmpDir, "work")

	// Create repository
	if err := exec.Command("svnadmin", "create", repoPath).Run(); err != nil {
		t.Skipf("svnadmin not available: %v", err)
	}

	// Allow anonymous access (for test simplicity)
	authDir := filepath.Join(repoPath, "conf")
	for _, line := range []string{"[general]\n", "anon-access = read\n", "auth-access = write\n"} {
		_ = os.WriteFile(filepath.Join(authDir, "svnserve.conf"), []byte(line), 0644)
	}

	// Find an available port and start svnserve
	port := "3690"
	serveURL := fmt.Sprintf("svn://localhost:%s/", port)

	cmd := exec.Command("svnserve", "-d", "--foreground", "--listen-port", port,
		"-r", filepath.Dir(repoPath))
	cmd.Start()
	tr := &testRepo{
		t:        t,
		repoPath: repoPath,
		serveURL: serveURL,
		serveCmd: cmd,
		workDir:  workDir,
	}

	// Wait for svnserve to start
	time.Sleep(500 * time.Millisecond)

	// Import initial content
	_ = os.MkdirAll(workDir, 0755)
	docsDir := filepath.Join(workDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	_ = os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# README\n\nInitial content."), 0644)
	_ = os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Guide\n\nHow to use."), 0644)

	exec.Command("svn", "import", "--non-interactive", "-m", "initial import",
		workDir, serveURL+"repo").Run()

	tr.serveURL = serveURL + "repo"

	t.Cleanup(func() {
		if tr.serveCmd != nil && tr.serveCmd.Process != nil {
			tr.serveCmd.Process.Kill()
		}
	})

	return tr
}

func (tr *testRepo) commitNewFile(path, content string) {
	tr.t.Helper()
	fullPath := filepath.Join(tr.workDir, path)
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	os.WriteFile(fullPath, []byte(content), 0644)
	exec.Command("svn", "import", "--non-interactive", "-m", "add "+path,
		fullPath, tr.serveURL+"/"+path).Run()
}

func (tr *testRepo) deleteFile(path string) {
	tr.t.Helper()
	exec.Command("svn", "--non-interactive", "delete", "-m", "delete "+path,
		tr.serveURL+"/"+path).Run()
}

func TestIntegration_ValidateAndList(t *testing.T) {
	tr := setupTestRepo(t)

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
	}

	c := NewConnector()
	err := c.Validate(context.Background(), cfg)
	require.NoError(t, err)

	resources, err := c.ListResources(context.Background(), cfg, "")
	require.NoError(t, err)
	assert.NotEmpty(t, resources)

	// Should have a "docs" directory
	var found bool
	for _, r := range resources {
		if r.Name == "docs" && r.HasChildren {
			found = true
		}
	}
	assert.True(t, found, "expected to find 'docs' directory")
}

func TestIntegration_FetchAll(t *testing.T) {
	tr := setupTestRepo(t)

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
		ResourceIDs: []string{"/docs"},
	}

	c := NewConnector()
	items, err := c.FetchAll(context.Background(), cfg, cfg.ResourceIDs)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(items), 2)

	// Verify content
	for _, item := range items {
		assert.NotEmpty(t, item.Content)
		assert.Equal(t, types.ChannelSVN, item.Metadata["channel"])
	}
}

func TestIntegration_FetchIncremental_NewFile(t *testing.T) {
	tr := setupTestRepo(t)

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
		ResourceIDs: []string{"/docs"},
	}

	c := NewConnector()

	// First sync (full)
	items1, cursor1, err := c.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(items1), 2)
	require.NotNil(t, cursor1)

	// Add a new file
	tr.commitNewFile("docs/newpage.md", "# New Page\n\nFresh content.")

	// Incremental sync should detect the new file
	items2, cursor2, err := c.FetchIncremental(context.Background(), cfg, cursor1)
	require.NoError(t, err)
	require.NotNil(t, cursor2)

	var hasNew bool
	for _, item := range items2 {
		if strings.Contains(item.ExternalID, "newpage.md") {
			hasNew = true
		}
	}
	assert.True(t, hasNew, "incremental sync should detect the new file")
}

func TestIntegration_FetchIncremental_Deletion(t *testing.T) {
	tr := setupTestRepo(t)

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
		ResourceIDs: []string{"/docs"},
	}

	c := NewConnector()

	// First sync
	_, cursor1, err := c.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)

	// Delete a file
	tr.deleteFile("docs/guide.md")

	// Incremental sync should detect deletion
	items2, _, err := c.FetchIncremental(context.Background(), cfg, cursor1)
	require.NoError(t, err)

	var hasDeleted bool
	for _, item := range items2 {
		if item.IsDeleted && strings.Contains(item.ExternalID, "guide.md") {
			hasDeleted = true
		}
	}
	assert.True(t, hasDeleted, "incremental sync should detect the deleted file")
}

func TestIntegration_FileExtensionFilter(t *testing.T) {
	tr := setupTestRepo(t)

	// Add a non-md file
	tr.commitNewFile("docs/image.dat", "binary data")

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
		ResourceIDs: []string{"/docs"},
		Settings: map[string]interface{}{
			"file_extensions": []interface{}{".md"},
		},
	}

	c := NewConnector()
	items, err := c.FetchAll(context.Background(), cfg, cfg.ResourceIDs)
	require.NoError(t, err)

	for _, item := range items {
		assert.True(t, strings.HasSuffix(item.FileName, ".md"),
			"non-md file should be filtered out: %s", item.FileName)
	}
}
