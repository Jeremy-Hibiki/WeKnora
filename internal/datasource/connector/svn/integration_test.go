//go:build integration

// Integration tests for the SVN connector. These require svnadmin, svnserve,
// and svn CLI to be installed. Run with:
//
//	go test -tags=integration -v ./internal/datasource/connector/svn/...

package svn

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
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

	// Allow anonymous read+write (import needs write; no auth server in tests)
	authDir := filepath.Join(repoPath, "conf")
	confContent := "[general]\nanon-access = write\nauth-access = write\n"
	_ = os.WriteFile(filepath.Join(authDir, "svnserve.conf"), []byte(confContent), 0644)

	// Whitelist localhost so the connector's Validate() passes its SSRF check
	utils.SetSSRFWhitelistFromRaw("localhost")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	// Find an available port to avoid conflicts between parallel test repos
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	serveURL := fmt.Sprintf("svn://localhost:%d/", port)

	cmd := exec.Command("svnserve", "-d", "--foreground", "--listen-port", strconv.Itoa(port),
		"-r", filepath.Dir(repoPath))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start svnserve: %v", err)
	}
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
	require.NoError(t, os.MkdirAll(workDir, 0755))
	docsDir := filepath.Join(workDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# README\n\nInitial content."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Guide\n\nHow to use."), 0644))

	importOut, importErr := exec.Command("svn", "import", "--non-interactive", "-m", "initial import",
		workDir, serveURL+"repo").CombinedOutput()
	require.NoError(t, importErr, "svn import failed: %s", importOut)

	tr.serveURL = serveURL + "repo"

	// Verify import actually succeeded
	if out, err := exec.Command("svn", "info", "--non-interactive", "--xml",
		tr.serveURL).CombinedOutput(); err != nil {
		t.Fatalf("svn import verification failed: %v\n%s", err, out)
	}

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
	require.NoError(tr.t, os.MkdirAll(filepath.Dir(fullPath), 0755))
	require.NoError(tr.t, os.WriteFile(fullPath, []byte(content), 0644))
	out, err := exec.Command("svn", "import", "--non-interactive", "-m", "add "+path,
		fullPath, tr.serveURL+"/"+path).CombinedOutput()
	require.NoError(tr.t, err, "svn import %s: %s", path, out)
}

func (tr *testRepo) deleteFile(path string) {
	tr.t.Helper()
	out, err := exec.Command("svn", "--non-interactive", "delete", "-m", "delete "+path,
		tr.serveURL+"/"+path).CombinedOutput()
	require.NoError(tr.t, err, "svn delete %s: %s", path, out)
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
