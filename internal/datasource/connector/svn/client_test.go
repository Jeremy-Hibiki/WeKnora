package svn

import (
	"context"
	"strings"
	"testing"
)

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
