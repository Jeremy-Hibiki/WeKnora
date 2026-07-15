package datasource

import (
	"testing"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestValidateConnectorBaseURLBlocksLoopback(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	err := ValidateConnectorBaseURL("http://127.0.0.1:8000")
	if err == nil {
		t.Fatal("expected loopback base_url to be rejected")
	}
}

func TestValidateConnectorBaseURLAllowsPublicHTTPS(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	if err := ValidateConnectorBaseURL("https://open.feishu.cn"); err != nil {
		t.Fatalf("expected public base_url to pass: %v", err)
	}
}

func TestValidateConnectorBaseURLWithSchemes(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	tests := []struct {
		name    string
		url     string
		allow   []string
		wantErr bool
	}{
		// Allowed schemes
		{"svn scheme allowed", "svn://svn.apache.org/repos", []string{"svn", "http", "https"}, false},
		{"svn+ssh allowed", "svn+ssh://svn.apache.org/repo", []string{"svn+ssh", "svn", "https"}, false},
		{"https still works", "https://svn.apache.org", []string{"svn", "http", "https"}, false},
		{"http allowed", "http://svn.apache.org", []string{"svn", "http", "https"}, false},

		// Blocked schemes
		{"file scheme rejected", "file:///etc/passwd", []string{"svn", "http", "https"}, true},
		{"ftp scheme rejected", "ftp://host/file", []string{"svn", "http", "https"}, true},
		{"gopher rejected", "gopher://host", []string{"svn", "http", "https"}, true},

		// SSRF checks still apply
		{"loopback blocked", "svn://127.0.0.1:3690/repo", []string{"svn"}, true},
		{"localhost blocked", "svn://localhost/repo", []string{"svn"}, true},

		// Edge cases
		{"empty allowed", "", []string{"svn"}, false},
		{"no scheme defaults https", "svn.apache.org", []string{"https"}, false},

		// Case insensitivity
		{"uppercase SVN", "SVN://svn.apache.org/repo", []string{"svn"}, false},
		{"mixed case HTTPS", "HTTPS://example.com", []string{"https"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConnectorBaseURLWithSchemes(tt.url, tt.allow)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tt.url, err)
			}
		})
	}
}
