package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
)

// CallbackParams captures every parameter Trae can send back on the
// local `/authorize` callback. The server may return either an
// authCode (which we then exchange via ExchangeToken) or a fully
// minted refreshToken (shortcut path). Some flows also populate
// cloudide_token / userTag.
type CallbackParams struct {
	// AuthCode is the authorization code (authCode / authCodeInfo / code).
	AuthCode string
	// RefreshToken is returned directly by certain login shortcuts,
	// skipping the code-exchange step.
	RefreshToken string
	// LoginHost is the actual authorization origin (matches or derives
	// from GetLoginGuidance result). Required on the callback.
	LoginHost string
	// LoginRegion is "cn", "sg", "us", or empty. Derived from callback
	// query (loginRegion / region / userRegion / aiRegion) or inferred
	// from the login host.
	LoginRegion string
	// LoginTraceID correlates the callback with the original request.
	LoginTraceID string
	// CloudIDEToken is a shortcut JWT-style bearer token returned in
	// x-cloudide-token / accessToken / userJwt.
	CloudIDEToken string
	// UserTag signals the user's routing tier (e.g. "usttp").
	UserTag string
	// RawQuery preserves the decoded query map so the authenticator can
	// log unusual fields at DEBUG level if exchange fails.
	RawQuery map[string]string

	// Error / ErrorDescription are non-empty when the OAuth provider
	// reports a rejection (user-cancelled, invalid scope, etc.).
	Error            string
	ErrorDescription string
}

// ExchangeResponse is the normalized result of a successful
// ExchangeToken call. Raw fields are intentionally kept flat to
// simplify downstream use.
type ExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	// ExpiresAt is a Unix timestamp (seconds) at which the access_token
	// expires. Zero/negative values mean "unknown" — the caller should
	// treat such tokens as valid but schedule refresh based on the
	// nominal RefreshLead.
	ExpiresAt int64 `json:"expires_at"`

	// RawResponseBody preserves the raw JSON payload so callers can
	// stash it in auth metadata for diagnostics.
	RawResponseBody []byte `json:"-"`
}

// UserInfoResponse is the normalized result of GetUserInfo. Any of
// Email / UserID / Nickname may be empty depending on the server
// response shape; the authenticator falls back to a stable label
// when all are missing.
type UserInfoResponse struct {
	Email    string `json:"email"`
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
}

// TraeTokenStorage is persisted alongside the coreauth.Auth record as
// Storage. It mirrors the information the scheduler needs to renew an
// access token (refresh_token + login context). Keeping this struct
// flat and JSON-friendly avoids any decoding surprises when the
// file-token-store rehydrates it.
type TraeTokenStorage struct {
	Type           string `json:"type"`
	Provider       string `json:"provider"`
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	TokenType      string `json:"token_type,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`
	ClientID       string `json:"client_id"`
	LoginHost      string `json:"login_host,omitempty"`
	LoginRegion    string `json:"login_region,omitempty"`
	LoginTraceID   string `json:"login_trace_id,omitempty"`
	UserTag        string `json:"user_tag,omitempty"`
	Email          string `json:"email,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	Nickname       string `json:"nickname,omitempty"`
	AccountAPIHost string `json:"account_api_host,omitempty"`
	// DeviceID / MachineID are preserved so the refresh DeviceProof
	// flow re-uses the same device identity that was registered at
	// auth-code exchange time.
	DeviceID  string `json:"device_id,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	// XAppVersion is the IDE version reported during exchange and
	// needed when sending DeviceProof (signatures are bound to it in
	// some server implementations via the DeviceInfo.ClientVersion
	// field).
	XAppVersion string `json:"x_app_version,omitempty"`
	// DeviceKeyPair (P-256) is required to sign the official refresh
	// DeviceProof message. Storing it on disk is equivalent to what
	// cockpit-tools does under storage key prefix
	// TRAE_STORAGE_DEVICE_KEY_PREFIX/<device_id>.
	DevicePrivateKeyPEM string `json:"device_private_key_pem,omitempty"`
	DevicePublicKeyPEM  string `json:"device_public_key_pem,omitempty"`
	// DeviceInfo stores the raw DeviceInfo object from auth-code
	// exchange so a future refresh path can enrich it in-place without
	// rebuilding from scratch.
	DeviceInfo map[string]any `json:"device_info,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SetMetadata injects metadata into the storage object before a
// SaveTokenToFile call so downstream consumers can inspect it.
func (ts *TraeTokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

// SaveTokenToFile serializes the Trae token storage to a JSON file.
func (ts *TraeTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "trae-cn"
	if ts.Provider == "" {
		ts.Provider = "trae-cn"
	}
	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("trae token save: create dir failed: %w", err)
	}
	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("trae token save: create file failed: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := misc.MergeMetadata(ts, ts.Metadata)
	if err != nil {
		return fmt.Errorf("trae token save: merge metadata failed: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("trae token save: write JSON failed: %w", err)
	}
	return nil
}

// DeviceContext is the collection of device / client headers sent
// along with the authorization URL and ExchangeToken request.
// Values are best-effort defaults for CLIProxyAPI's headless
// environment; the Trae server tolerates any non-empty strings
// as long as client_id matches.
type DeviceContext struct {
	ClientID      string
	PluginVersion string
	MachineID     string
	DeviceID      string
	XDeviceBrand  string
	XDeviceType   string
	XOSVersion    string
	XEnv          string
	XAppVersion   string
	XAppType      string
}
