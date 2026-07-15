package datasource

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
)

// ValidateConnectorBaseURL checks a connector API base URL against the SSRF policy.
// Empty rawURL is allowed; callers apply their own default before issuing requests.
func ValidateConnectorBaseURL(rawURL string) error {
	url := strings.TrimSpace(rawURL)
	if url == "" {
		return nil
	}
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	if err := utils.ValidateURLForSSRF(url); err != nil {
		return fmt.Errorf("base_url SSRF validation failed: %w", err)
	}
	return nil
}

// ValidateConnectorBaseURLWithSchemes is like ValidateConnectorBaseURL but accepts a
// caller-specified list of allowed URL schemes. This is for connectors that need
// non-HTTP protocols (e.g. SVN's svn://, svn+ssh://). Hostname and IP SSRF checks
// are still applied by probing an equivalent http:// URL through the existing
// validator.
func ValidateConnectorBaseURLWithSchemes(rawURL string, allowedSchemes []string) error {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return nil
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := neturl.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	allowed := false
	for _, s := range allowedSchemes {
		if scheme == strings.ToLower(s) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("invalid scheme: %s (allowed: %s)", scheme, strings.Join(allowedSchemes, ", "))
	}
	// Validate hostname/IP safety via the centralised SSRF checker. We probe with
	// an http:// equivalent because the validator only accepts http/https schemes;
	// the hostname and IP-level checks are scheme-independent.
	probeURL := "http://" + parsed.Host + parsed.Path
	if err := utils.ValidateURLForSSRF(probeURL); err != nil {
		return fmt.Errorf("base_url SSRF validation failed: %w", err)
	}
	return nil
}

// NewConnectorHTTPClient returns an HTTP client with redirect and dial-time SSRF guards.
func NewConnectorHTTPClient(timeout time.Duration) *http.Client {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	return utils.NewSSRFSafeHTTPClient(cfg)
}
