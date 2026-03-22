package tools

import (
	"os"
	"strings"
)

// ProxyURLForAPI returns the CORS proxy URL for a given API, falling back to direct API URL if proxy is not configured.
// This allows both hosted (with CORS proxy) and local development (direct API) to work.
//
// Parameters:
//   - apiName: name of the API (e.g., "slack", "discord")
//   - directURL: the direct API endpoint (e.g., "https://slack.com/api")
//
// Returns the appropriate URL to use for requests.
func ProxyURLForAPI(apiName, directURL string) string {
	// Check for specific proxy env var first (e.g., SLACK_API_PROXY)
	proxyEnvKey := strings.ToUpper(apiName) + "_PROXY"
	if proxyURL := os.Getenv(proxyEnvKey); proxyURL != "" {
		return proxyURL
	}

	// Fall back to generic CORS_PROXY_URL if set
	if corsProxyURL := os.Getenv("CORS_PROXY_URL"); corsProxyURL != "" {
		// If we have a generic proxy URL, construct the path
		// This is a fallback - specific proxies are preferred
		return corsProxyURL
	}

	// No proxy configured - use direct API URL (for local development)
	return directURL
}
