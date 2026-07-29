package svn

import (
	"context"
	"strings"
	"testing"
)

// TestParseDiffSummarizeEntriesDecodesPercentEncodedPaths verifies that
// parseDiffSummarizeEntries correctly URL-decodes paths returned by
// `svn diff --summarize --xml`, which percent-encodes non-ASCII characters.
func TestParseDiffSummarizeEntriesDecodesPercentEncodedPaths(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		// Chinese characters percent-encoded (the bug report scenario).
		{
			name:     "chinese_directory",
			input:    "%E9%85%8D%E7%BD%AE%E9%94%99%E8%AF%AF",
			expected: "配置错误",
		},
		{
			name:     "chinese_file",
			input:    "%E9%85%8D%E7%BD%AE%E9%94%99%E8%AF%AF/config.md",
			expected: "配置错误/config.md",
		},
		// Japanese characters.
		{
			name:     "japanese",
			input:    "%E3%83%86%E3%82%B9%E3%83%88",
			expected: "テスト",
		},
		// UTF-8 strings with non-ASCII that don't need percent-encoding.
		{
			name:     "utf8_already_decoded",
			input:    "docs/配置错误.md",
			expected: "docs/配置错误.md",
		},
		// Empty percent-encoding edge case (valid UTF-8 should round-trip).
		{
			name:     "ascii_always",
			input:    "docs/guide.md",
			expected: "docs/guide.md",
		},
		// Mixed ASCII and percent-encoded.
		{
			name:     "mixed_trunk",
			input:    "trunk/%E6%96%87%E6%A1%A3/readme.md",
			expected: "trunk/文档/readme.md",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []diffPath{{Path: tc.input, Item: "added"}}
			got := parseDiffSummarizeEntries(input)
			requireLen(t, got, 1)
			if got[0].Path != tc.expected {
				t.Errorf("path = %q, want %q", got[0].Path, tc.expected)
			}
			if got[0].Type != "A" {
				t.Errorf("type = %q, want %q", got[0].Type, "A")
			}
		})
	}
}

// TestParseDiffSummarizeEntriesSkipsNoChange verifies that entries with
// item="" (no change) are dropped.
func TestParseDiffSummarizeEntriesSkipsNoChange(t *testing.T) {
	input := []diffPath{
		{Path: "added_file.md", Item: "added"},
		{Path: "unchanged_file.md", Item: ""},
		{Path: "modified_file.md", Item: "modified"},
	}
	got := parseDiffSummarizeEntries(input)
	requireLen(t, got, 2)
	if got[0].Path != "added_file.md" || got[0].Type != "A" {
		t.Errorf("got[0] = %+v, want Path=added_file.md, Type=A", got[0])
	}
	if got[1].Path != "modified_file.md" || got[1].Type != "M" {
		t.Errorf("got[1] = %+v, want Path=modified_file.md, Type=M", got[1])
	}
}

// TestParseDiffSummarizeEntriesHandlesWhitespace trims whitespace around paths.
func TestParseDiffSummarizeEntriesHandlesWhitespace(t *testing.T) {
	input := []diffPath{{Path: "  space-padded.md  ", Item: "added"}}
	got := parseDiffSummarizeEntries(input)
	requireLen(t, got, 1)
	if got[0].Path != "space-padded.md" {
		t.Errorf("path trimmed = %q, want %q", got[0].Path, "space-padded.md")
	}
}

// requireLen is a tiny helper to avoid adding testify for a single assertion.
func requireLen(t *testing.T, got []diffEntry, want int) {
	t.Helper()
	if len(got) != want {
		t.Errorf("got %d entries, want %d", len(got), want)
	}
}

// TestCommonArgsNoAuthCache pins the --no-auth-cache hardening so svn never
// persists credentials to ~/.subversion/auth on the server filesystem.
// Pure unit test — no svn binary required, runs in the default test suite.
func TestCommonArgsNoAuthCache(t *testing.T) {
	cli := &cliImpl{username: "u", password: "p"}
	args := cli.commonArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--no-auth-cache") {
		t.Errorf("commonArgs missing --no-auth-cache: %v", args)
	}
}

// TestNoCredentialLeakInErrors is a regression test: error messages from the
// CLI wrapper must NEVER contain credential values. They previously embedded
// args[:3], which leaked `--password <value>` (password-only config) into
// logs. Holds with or without the svn binary installed: both the start-error
// path and the connection-error path must be credential-free.
func TestNoCredentialLeakInErrors(t *testing.T) {
	cases := []struct {
		name string
		cli  *cliImpl
	}{
		{"password-only", &cliImpl{username: "", password: "SUPERSECRET123"}},
		{"user+password", &cliImpl{username: "alice_secret", password: "SUPERSECRET123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Unreachable host forces an error through run().
			_, err := tc.cli.Info(context.Background(), "svn://127.0.0.1:1/repo")
			if err == nil {
				t.Skip("expected connection error, got nil")
			}
			msg := err.Error()
			if strings.Contains(msg, "SUPERSECRET123") {
				t.Errorf("password leaked in error: %s", msg)
			}
			if strings.Contains(msg, "alice_secret") {
				t.Errorf("username leaked in error: %s", msg)
			}
		})
	}
}
