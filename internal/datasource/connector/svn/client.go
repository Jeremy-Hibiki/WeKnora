package svn

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrFileTooLarge is returned by Cat when the remote file exceeds
// max_file_size. Callers treat it as a soft skip (consistent with the
// FetchAll size pre-check), never as a sync failure.
var ErrFileTooLarge = errors.New("file exceeds max_file_size")

const (
	// commandTimeout bounds info/list/diff — small, metadata-only commands.
	commandTimeout = 5 * time.Minute
	// listRecursiveTimeout bounds `svn list -R`, which streams the whole
	// tree XML for large repos and legitimately exceeds commandTimeout.
	listRecursiveTimeout = 15 * time.Minute
	// catTimeoutFloor/Ceil bound the per-file cat budget derived from
	// maxFileSize at a conservative 100 KB/s worst-case throughput.
	catTimeoutFloor = commandTimeout
	catTimeoutCeil  = 30 * time.Minute
	// waitDelay lets pipes drain after process exit before forcible close.
	waitDelay = 10 * time.Second
)

// cliImpl implements svnCLI by shelling out to the `svn` executable.
type cliImpl struct {
	username    string
	password    string
	maxFileSize int64
}

// newCLI creates a CLI wrapper with the given credentials.
func newCLI(cfg *Config) *cliImpl {
	return &cliImpl{
		username:    cfg.Username,
		password:    cfg.Password,
		maxFileSize: cfg.GetMaxFileSize(),
	}
}

// commonArgs returns the flags prepended to every svn command.
// --no-auth-cache prevents svn from persisting credentials (realm, username,
// and on permissive configs the plaintext password) to ~/.subversion/auth on
// the server filesystem.
func (c *cliImpl) commonArgs() []string {
	args := []string{"--non-interactive", "--no-auth-cache"}
	if c.username != "" {
		args = append(args, "--username", c.username)
	}
	if c.password != "" {
		args = append(args, "--password", c.password)
	}
	return args
}

// subcommand extracts the svn subcommand (info/list/cat/diff) from the full
// arg vector. It is the first arg after the common auth flags. Error messages
// use ONLY this — never the raw args, which may carry credential values.
func (c *cliImpl) subcommand(args []string) string {
	n := len(c.commonArgs())
	if len(args) > n {
		return args[n]
	}
	return "svn"
}

// catTimeout derives the per-file cat budget from maxFileSize at a
// conservative 100 KB/s worst-case throughput, clamped to [floor, ceil].
func (c *cliImpl) catTimeout() time.Duration {
	t := time.Duration(c.maxFileSize/(100*1024)) * time.Second
	if t < catTimeoutFloor {
		return catTimeoutFloor
	}
	if t > catTimeoutCeil {
		return catTimeoutCeil
	}
	return t
}

// run executes an svn command with the default timeout and unbounded output.
func (c *cliImpl) run(ctx context.Context, args ...string) ([]byte, error) {
	return c.runCmd(ctx, commandTimeout, 0, args...)
}

// runCmd executes an svn command and returns stdout. Guarantees:
//   - No shell: args are passed individually to exec.Command (no injection).
//   - Timeout: ctx is wrapped with the given timeout; on expiry the WHOLE
//     process group is killed (covers the ssh grandchild of svn+ssh://).
//   - Bounded output: when maxOut > 0, stdout is capped at maxOut bytes; on
//     overflow the process is killed early (no full download into RAM) and a
//     size error is returned.
//   - Credential hygiene: error messages name only the svn subcommand, never
//     the raw arg vector that may carry --password values.
func (c *cliImpl) runCmd(ctx context.Context, timeout time.Duration, maxOut int64, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sub := c.subcommand(args)

	cmd := exec.CommandContext(ctx, "svn", args...)
	setProcAttr(cmd)
	// On ctx timeout/cancel, kill the WHOLE process group (not just the
	// direct svn child that CommandContext kills by default) — covers the
	// ssh grandchild spawned for svn+ssh:// URLs. Go 1.20+ Cancel replaces
	// the default kill; no watchdog goroutine needed.
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = waitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("svn %s: stdout pipe: %w", sub, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("svn %s: start: %w", sub, err)
	}

	var stdout bytes.Buffer
	var copyErr error
	if maxOut > 0 {
		_, copyErr = io.Copy(&stdout, io.LimitReader(stdoutPipe, maxOut+1))
		if int64(stdout.Len()) > maxOut {
			killProcessGroup(cmd)
			_ = cmd.Wait()
			return nil, fmt.Errorf("svn %s: %w: output exceeds %d bytes", sub, ErrFileTooLarge, maxOut)
		}
	} else {
		_, copyErr = io.Copy(&stdout, stdoutPipe)
	}
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		return nil, fmt.Errorf("svn %s: %w", sub, ctx.Err())
	}
	if copyErr != nil {
		return nil, fmt.Errorf("svn %s: read stdout: %w", sub, copyErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("svn %s: %w (stderr: %s)", sub, waitErr, strings.TrimSpace(stderr.String()))
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
		Revision:   rev,
		UUID:       entry.UUID,
		Root:       entry.Root,
		URL:        entry.URL,
		CommitDate: parseSVNDate(entry.Commit.Date),
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
	out, err := c.runCmd(ctx, listRecursiveTimeout, 0, args...)
	if err != nil {
		return nil, err
	}
	return parseListXML(out)
}

func parseListXML(out []byte) ([]listEntry, error) {
	var listing listXML
	if err := xml.Unmarshal(out, &listing); err != nil {
		return nil, fmt.Errorf("parse svn list xml: %w", err)
	}
	return listing.Entries, nil
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
// Output is capped at maxFileSize+1 bytes: `svn diff --summarize` (used by
// incremental sync) reports no file sizes, so this is the only size guard on
// the incremental path — without it a large binary matching an allowed
// extension would be buffered fully into RAM.
func (c *cliImpl) Cat(ctx context.Context, repoURL, path string, revision int64) ([]byte, error) {
	target := joinURLPath(repoURL, path)
	revStr := "HEAD"
	if revision > 0 {
		revStr = strconv.FormatInt(revision, 10)
	}
	args := append(c.commonArgs(), "cat", "-r", revStr, "--", target)
	out, err := c.runCmd(ctx, c.catTimeout(), c.maxFileSize, args...)
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
