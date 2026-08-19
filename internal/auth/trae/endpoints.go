// Package trae provides authentication and token management functionality
// for Trae / Trae CN AI services. It handles OAuth authorization code flow
// with PKCE, local callback server, and the Trae-specific HTTP APIs
// (GetLoginGuidance, ExchangeToken, GetUserInfo).
package trae

import "strings"

// Endpoints groups the region-specific URLs for a Trae deployment.
// The international (trae.ai) and China (trae.cn) editions share the
// same API path layout; only the host names differ.
type Endpoints struct {
	// LoginGuidanceURLs is the list of GetLoginGuidance endpoints to try,
	// in priority order. CN uses api.trae.cn / api.trae.com.cn / www.trae.cn.
	LoginGuidanceURLs []string

	// DefaultLoginHost is the fallback origin used when GetLoginGuidance
	// fails entirely. For CN this is https://www.trae.cn.
	DefaultLoginHost string

	// AuthorizationPath is the browser-facing login page path.
	// Always "/authorization".
	AuthorizationPath string

	// CallbackPath is the local HTTP server callback path.
	// Always "/authorize".
	CallbackPath string

	// ExchangeTokenPaths lists the ExchangeToken endpoint paths, in
	// priority order. /trae/api/v3/oauth/ExchangeToken is the official
	// auth-code endpoint; /cloudide/api/v3/trae/oauth/ExchangeToken is
	// the legacy path still accepted by the server.
	ExchangeTokenPaths []string

	// GetUserInfoPath is the path for fetching the authenticated user's
	// profile. Always "/cloudide/api/v3/trae/GetUserInfo".
	GetUserInfoPath string

	// ClientIDStandard is the OAuth client_id for standard (non-Solo) Trae.
	ClientIDStandard string

	// ClientIDSolo is the OAuth client_id for TRAE SOLO.
	ClientIDSolo string

	// ClientSecret is a fixed dummy value ("-") used by Trae's
	// ExchangeToken endpoint.
	ClientSecret string

	// ExchangeClientSecret is the client_secret value passed in ExchangeToken.
	// Alias of ClientSecret for parity with cockpit-tools naming.
	ExchangeClientSecret string

	// DefaultAppVersion is the minimum auth_app_version advertised.
	DefaultAppVersion string

	// DefaultPluginVersion is the plugin_version advertised when the
	// real product.json cannot be read.
	DefaultPluginVersion string

	// DefaultAppType is the app_type (quality) advertised.
	DefaultAppType string

	// DefaultDeviceID is the device_id fallback.
	DefaultDeviceID string
}

// GlobalEndpoints holds the international (trae.ai) URLs.
var GlobalEndpoints = Endpoints{
	LoginGuidanceURLs: []string{
		"https://api.marscode.com/cloudide/api/v3/trae/GetLoginGuidance",
		"https://api.trae.ai/cloudide/api/v3/trae/GetLoginGuidance",
		"https://www.trae.ai/cloudide/api/v3/trae/GetLoginGuidance",
	},
	DefaultLoginHost:     "https://www.trae.ai",
	AuthorizationPath:    "/authorization",
	CallbackPath:         "/authorize",
	ExchangeTokenPaths:   []string{"/trae/api/v3/oauth/ExchangeToken", "/cloudide/api/v3/trae/oauth/ExchangeToken"},
	GetUserInfoPath:      "/cloudide/api/v3/trae/GetUserInfo",
	ClientIDStandard:     "ono9krqynydwx5",
	ClientIDSolo:         "en1oxy7wnw8j9n",
	ClientSecret:         "-",
	ExchangeClientSecret: "-",
	DefaultAppVersion:    "3.5.54",
	DefaultPluginVersion: "local",
	DefaultAppType:       "stable",
	DefaultDeviceID:      "0",
}

// CN endpoint hosts. The China edition routes everything through
// trae.cn / trae.com.cn; there are no SG/US/USTTP regional variants.
var cnAccountAPICandidates = []string{
	"https://api.trae.cn",
	"https://api.trae.com.cn",
	"https://www.trae.cn",
}

// CNEndpoints holds the China (trae.cn) URLs.
var CNEndpoints = Endpoints{
	LoginGuidanceURLs: []string{
		"https://api.trae.cn/cloudide/api/v3/trae/GetLoginGuidance",
		"https://api.trae.com.cn/cloudide/api/v3/trae/GetLoginGuidance",
		"https://www.trae.cn/cloudide/api/v3/trae/GetLoginGuidance",
	},
	DefaultLoginHost:     "https://www.trae.cn",
	AuthorizationPath:    "/authorization",
	CallbackPath:         "/authorize",
	ExchangeTokenPaths:   []string{"/trae/api/v3/oauth/ExchangeToken", "/cloudide/api/v3/trae/oauth/ExchangeToken"},
	GetUserInfoPath:      "/cloudide/api/v3/trae/GetUserInfo",
	ClientIDStandard:     "ono9krqynydwx5",
	ClientIDSolo:         "en1oxy7wnw8j9n",
	ClientSecret:         "-",
	ExchangeClientSecret: "-",
	DefaultAppVersion:    "3.5.54",
	DefaultPluginVersion: "local",
	DefaultAppType:       "stable",
	DefaultDeviceID:      "0",
}

// EndpointsForProvider returns the endpoint set for the given provider key.
// "trae-cn" (case-insensitive) selects CNEndpoints; anything else selects
// GlobalEndpoints so unknown / empty values fall back to the international
// edition rather than breaking.
func EndpointsForProvider(provider string) Endpoints {
	if strings.EqualFold(strings.TrimSpace(provider), "trae-cn") {
		return CNEndpoints
	}
	return GlobalEndpoints
}

// CandidateAPICandidates returns the ordered list of API origins to try
// for ExchangeToken / GetUserInfo calls. For CN the list is fixed; for
// international it would normally include the SG/US/USTTP variants but
// for MVP simplicity we return the standard candidate list including the
// login_host-derived origins via BuildCandidateOrigins.
func (e Endpoints) AccountAPICandidatesCN() []string {
	// slice copy so callers cannot mutate the package-level slice
	out := make([]string, len(cnAccountAPICandidates))
	copy(out, cnAccountAPICandidates)
	return out
}

// IsCN reports whether this endpoint set represents the CN deployment.
// True exactly when the DefaultLoginHost points at trae.cn.
func (e Endpoints) IsCN() bool {
	return strings.Contains(strings.ToLower(e.DefaultLoginHost), ".cn")
}
