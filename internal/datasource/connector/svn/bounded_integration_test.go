//go:build integration

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

// TestIntegration_CatBounded verifies that a file exceeding max_file_size is
// rejected by the bounded Cat on the incremental path — the only size guard
// there, since `svn diff --summarize` reports no sizes.
func TestIntegration_CatBounded(t *testing.T) {
	tr := setupTestRepo(t)

	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": tr.serveURL,
		},
		ResourceIDs: []string{"/docs"},
		Settings: map[string]interface{}{
			// 1 KB cap — the 2 existing files are ~30 bytes, well under
			"max_file_size": float64(1024),
		},
	}

	c := NewConnector()

	// First sync: cursor established, small files fetched fine
	items1, cursor1, err := c.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, cursor1)
	assert.GreaterOrEqual(t, len(items1), 2)

	// Commit a 10 KB file — exceeds the 1 KB cap
	big := strings.Repeat("x", 10*1024)
	tr.commitNewFile("docs/big.md", big)

	// Incremental sync: big.md must be softly skipped — oversized is a
	// filtering decision, not a sync failure (consistent with FetchAll's
	// size pre-check). No error, no item.
	items2, _, err := c.FetchIncremental(context.Background(), cfg, cursor1)
	require.NoError(t, err, "oversized file must be a soft skip, not a sync error")
	for _, item := range items2 {
		if strings.Contains(item.ExternalID, "big.md") {
			t.Fatalf("oversized file was fetched with %d bytes content; bounded Cat should have rejected it", len(item.Content))
		}
	}
}

// TestIntegration_SSHGrandchildKilled verifies the watchdog kills the whole
// process group on timeout. We simulate a hanging svn+ssh-like spawn by
// replacing the svn binary with a script that forks a sleeper child.
func TestIntegration_ProcessGroupKilled(t *testing.T) {
	tmpDir := t.TempDir()

	// Fake `svn` that spawns a grandchild sleeper, then sleeps itself.
	fakeSvn := filepath.Join(tmpDir, "svn")
	sleeperMark := filepath.Join(tmpDir, "sleeper.pid")
	script := fmt.Sprintf(`#!/bin/sh
(sleep 300 & echo $! > %s) &
sleep 300
`, sleeperMark)
	require.NoError(t, os.WriteFile(fakeSvn, []byte(script), 0755))

	// Prepend fake dir to PATH
	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	cli := &cliImpl{maxFileSize: 50 * 1024 * 1024}

	// Use a very short timeout via context
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := cli.run(ctx, "--non-interactive", "info", "--xml", "svn://whatever/repo")
	elapsed := time.Since(start)

	require.Error(t, err)

	// The whole call must return promptly after timeout, not wait 300s
	assert.Less(t, elapsed.Seconds(), 5.0, "run did not return promptly after ctx timeout")

	// The grandchild sleeper must also be dead (process group kill).
	// Poll briefly: after group kill the orphan is reparented and reaped by init.
	if data, rerr := os.ReadFile(sleeperMark); rerr == nil {
		pid := strings.TrimSpace(string(data))
		alive := false
		for i := 0; i < 20; i++ {
			if exec.Command("kill", "-0", pid).Run() != nil {
				alive = false
				break
			}
			alive = true
			time.Sleep(50 * time.Millisecond)
		}
		assert.False(t, alive, "grandchild process %s still alive after group kill", pid)
	}
}
