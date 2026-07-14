package svn

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	commandTimeout = 5 * time.Minute
)

// cliImpl implements svnCLI by shelling out to the `svn` executable.
type cliImpl struct {
	username string
	password string
}

// newCLI creates a CLI wrapper with the given credentials.
func newCLI(cfg *Config) *cliImpl {
	return &cliImpl{
		username: cfg.Username,
		password: cfg.Password,
	}
}

// commonArgs returns the authentication-related args prepended to every svn command.
func (c *cliImpl) commonArgs() []string {
	args := []string{"--non-interactive"}
	if c.username != "" {
		args = append(args, "--username", c.username)
	}
	if c.password != "" {
		args = append(args, "--password", c.password)
	}
	return args
}

// run executes an svn command and returns stdout. It enforces a context-based
// timeout to prevent network hangs. No shell is used — all args are passed as
// individual exec.Command arguments, preventing injection.
func (c *cliImpl) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "svn", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("svn %s: %w (stderr: %s)",
			strings.Join(args[:min(3, len(args))], " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Info executes `svn info --xml <url>` and returns repository metadata.
func (c *cliImpl) Info(ctx context.Context, repoURL string) (*repoInfo, error) {
	args := append(c.commonArgs(), "info", "--xml", "--", repoURL)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("svn info: %w", err)
	}

	var info infoXML
	if err := xml.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse svn info xml: %w", err)
	}
	if len(info.Entries) == 0 {
		return nil, fmt.Errorf("svn info returned no entries")
	}

	entry := info.Entries[0]
	rev, err := strconv.ParseInt(entry.Revision, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse revision %q: %w", entry.Revision, err)
	}

	return &repoInfo{
		Revision: rev,
		UUID:     entry.UUID,
		Root:     entry.Root,
		URL:      entry.URL,
	}, nil
}

// List executes `svn list --xml <url>/<path>` for a single-level directory listing.
func (c *cliImpl) List(ctx context.Context, repoURL, path string) ([]listEntry, error) {
	target := joinURLPath(repoURL, path)
	args := append(c.commonArgs(), "list", "--xml", "--", target)
	return c.runList(ctx, args)
}

// ListRecursive executes `svn list -R --xml <url>/<path>` for a recursive file listing.
func (c *cliImpl) ListRecursive(ctx context.Context, repoURL, path string) ([]listEntry, error) {
	target := joinURLPath(repoURL, path)
	args := append(c.commonArgs(), "list", "-R", "--xml", "--", target)
	return c.runList(ctx, args)
}

func (c *cliImpl) runList(ctx context.Context, args []string) ([]listEntry, error) {
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var listing listXML
	if err := xml.Unmarshal(out, &listing); err != nil {
		return nil, fmt.Errorf("parse svn list xml: %w", err)
	}
	return listing.Entries, nil
}

// Cat executes `svn cat -r <revision> <url>/<path>` and returns raw file bytes.
func (c *cliImpl) Cat(ctx context.Context, repoURL, path string, revision int64) ([]byte, error) {
	target := joinURLPath(repoURL, path)
	revStr := "HEAD"
	if revision > 0 {
		revStr = strconv.FormatInt(revision, 10)
	}
	args := append(c.commonArgs(), "cat", "-r", revStr, "--", target)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("svn cat %s: %w", path, err)
	}
	return out, nil
}

// DiffSummarize executes `svn diff --summarize -r <old>:<new> --xml <url>/<path>`
// and returns the list of changed paths with their change types (A/M/D).
func (c *cliImpl) DiffSummarize(ctx context.Context, repoURL, path string, oldRev, newRev int64) ([]diffEntry, error) {
	target := joinURLPath(repoURL, path)
	revRange := fmt.Sprintf("%d:HEAD", oldRev)
	if newRev > 0 {
		revRange = fmt.Sprintf("%d:%d", oldRev, newRev)
	}
	args := append(c.commonArgs(), "diff", "--summarize", "-r", revRange, "--xml", "--", target)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("svn diff --summarize: %w", err)
	}

	var summary diffSummarizeXML
	if err := xml.Unmarshal(out, &summary); err != nil {
		return nil, fmt.Errorf("parse svn diff xml: %w", err)
	}

	entries := make([]diffEntry, 0, len(summary.Paths))
	for _, p := range summary.Paths {
		changeType := itemToChangeType(p.Item)
		if changeType == "" {
			continue
		}
		entries = append(entries, diffEntry{
			Path: strings.TrimSpace(p.Path),
			Type: changeType,
		})
	}
	return entries, nil
}

// itemToChangeType converts SVN's item attribute to our internal A/M/D type.
func itemToChangeType(item string) string {
	switch strings.ToLower(item) {
	case "added":
		return "A"
	case "modified":
		return "M"
	case "deleted":
		return "D"
	default:
		return ""
	}
}

// joinURLPath safely joins a repo URL with a relative path.
func joinURLPath(repoURL, path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return repoURL
	}
	return repoURL + "/" + path
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
