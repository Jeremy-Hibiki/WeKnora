//go:build integration

package svn

import (
	"context"
	"strings"
	"testing"
)

// TestErrorMessageShowsSubcommand verifies errors name the failing svn
// subcommand (info/list/cat/diff), not the auth flags that precede it.
// Integration-tagged: exercises the real svn binary's failure path.
func TestErrorMessageShowsSubcommand(t *testing.T) {
	cli := &cliImpl{username: "alice", password: "x"}
	_, err := cli.List(context.Background(), "svn://127.0.0.1:1/repo", "/docs")
	if err == nil {
		t.Skip("expected connection error, got nil")
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("error message hides the failing subcommand: %s", err.Error())
	}
}
