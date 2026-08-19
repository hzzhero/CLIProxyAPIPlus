package trae

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// TraeAuth is the service object that orchestrates the HTTP calls of a
// Trae login flow. It is intentionally stateless between calls so the
// Authenticator (in sdk/auth/trae_cn.go) drives sequencing and owns
// the local callback server.
type TraeAuth struct {
	endpoints Endpoints
	cfg       *config.Config
	client    *http.Client
}

// NewTraeAuth constructs a TraeAuth service bound to the given
// endpoint set (typically CNEndpoints for --trae-cn-login).
func NewTraeAuth(cfg *config.Config, endpoints Endpoints) *TraeAuth {
	return &TraeAuth{
		endpoints: endpoints,
		cfg:       cfg,
		client: &http.Client{
			// Credential-acquisition timeout only. Per AGENTS.md rule we
			// do not apply timeouts after upstream connections are
			// established — these short timeouts are only for the
			// GetLoginGuidance / ExchangeToken / GetUserInfo RPCs.
			Timeout: 15 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// Device context defaults (headless / CLI friendly)
// ---------------------------------------------------------------------------

// DefaultDeviceContext returns a DeviceContext pre-populated with
// values that the Trae server accepts for non-IDE clients.
// client_id is chosen by the caller (standard vs. Solo).
func DefaultDeviceContext(clientID string, endpoints Endpoints) DeviceContext {
	return DeviceContext{
		ClientID:      clientID,
		PluginVersion: endpoints.DefaultPluginVersion,
		MachineID:     uuid.NewString(),
		// Trae's ExchangeToken validator now requires an 8..24-digit
		// numeric device id. The literal "0" placeholder that earlier
		// versions used is rejected. We fall back to a fresh 19-digit
		// crypto-random id which matches the format cockpit-tools sees
		// from real IDE installations.
		DeviceID:     DefaultNumericDeviceID(),
		XDeviceBrand: DefaultXDeviceBrand(),
		XDeviceType:  DefaultXDeviceType(),
		XOSVersion:   DefaultXOSVersion(),
		XEnv:         "", // left empty, same as cockpit-tools fallback
		XAppVersion:  endpoints.DefaultAppVersion,
		XAppType:     endpoints.DefaultAppType,
	}
}

// RandomLoginTraceID generates a random login_trace_id. cockpit-tools
// uses a UUID string; we match that format via a 16-byte random hex
// string (equivalent entropy, no extra dependency on a specific
// serialization style).
func RandomLoginTraceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failure on a functioning platform is not
		// recoverable; fall back to a time-based identifier with
		// entropy so downstream still sees unique values.
		return fmt.Sprintf("fallback-%d-%s", time.Now().UnixNano(), uuid.NewString()[:8])
	}
	return hex.EncodeToString(buf)
}

// ---------------------------------------------------------------------------
// GetLoginGuidance
// ---------------------------------------------------------------------------

// GetLoginGuidance returns the real loginHost origin to use for the
// authorization page. For CN deployments it silently falls back to
// endpoints.DefaultLoginHost when every endpoint fails (matches
// cockpit-tools behavior).
func (t *TraeAuth) GetLoginGuidance(ctx context.Context, loginTraceID string) (string, error) {
	body := map[string]string{
		"loginTraceID":   loginTraceID,
		"login_trace_id": loginTraceID,
	}
	bodyJSON, _ := json.Marshal(body)

	var errs []string
	for _, endpoint := range t.endpoints.LoginGuidanceURLs {
		loginHost, err := t.doLoginGuidance(ctx, endpoint, bodyJSON)
		if err == nil && loginHost != "" {
			return loginHost, nil
		}
		errs = append(errs, fmt.Sprintf("%s => %v", endpoint, err))
	}

	// CN fallback: default login host instead of error
	if t.endpoints.IsCN() {
		log.Warnf("trae auth: GetLoginGuidance all endpoints failed (%s); fallback to %s",
			strings.Join(errs, " | "), t.endpoints.DefaultLoginHost)
		return t.endpoints.DefaultLoginHost, nil
	}

	return "", fmt.Errorf("trae auth: GetLoginGuidance failed: %s", strings.Join(errs, " | "))
}

func (t *TraeAuth) doLoginGuidance(ctx context.Context, endpoint string, bodyJSON []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI/1.0 trae-authenticator")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d (body=%s)", resp.StatusCode, truncateBody(raw))
	}
	// Flexible field paths match cockpit-tools extract_login_guidance_host
	host := pickAnyString(raw,
		"Result.LoginHost", "Result.loginHost", "Result.LoginURL", "Result.loginUrl",
		"result.LoginHost", "result.loginHost", "result.loginUrl",
		"data.Result.LoginHost", "data.result.loginHost", "data.loginHost", "data.loginUrl",
		"LoginHost", "loginHost", "loginUrl",
	)
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("response missing LoginHost field: %s", truncateBody(raw))
	}
	return host, nil
}

// ---------------------------------------------------------------------------
// BuildAuthURL
// ---------------------------------------------------------------------------

// BuildAuthURL constructs the full `/authorization` URL the user's
// browser should visit. It mirrors cockpit-tools'
// build_verification_uri function. auth_callback_url is appended
// without additional URL-encoding because it is itself an already
// encoded absolute URL.
func (t *TraeAuth) BuildAuthURL(loginHost, loginTraceID, callbackURL, codeChallenge string, dc DeviceContext, isSolo bool) (string, error) {
	u, err := ensureHTTPS(loginHost)
	if err != nil {
		return "", fmt.Errorf("trae auth: invalid loginHost %q: %w", loginHost, err)
	}
	u.Path = t.endpoints.AuthorizationPath
	u.RawQuery = ""

	values := url.Values{}
	set := func(k, v string) {
		if v != "" {
			values.Set(k, v)
		}
	}
	set("login_version", "1")
	if isSolo {
		set("auth_from", "solo")
	} else {
		set("auth_from", "trae")
	}
	set("login_channel", "native_ide")
	set("plugin_version", dc.PluginVersion)
	set("auth_type", "local")
	set("client_id", dc.ClientID)
	set("redirect", "0")
	set("login_trace_id", loginTraceID)
	set("machine_id", dc.MachineID)
	set("device_id", dc.DeviceID)
	set("x_device_id", dc.DeviceID)
	set("x_machine_id", dc.MachineID)
	set("x_device_brand", dc.XDeviceBrand)
	set("x_device_type", dc.XDeviceType)
	set("x_os_version", dc.XOSVersion)
	set("x_env", dc.XEnv)
	set("x_app_version", dc.XAppVersion)
	set("x_app_type", dc.XAppType)
	set("code_challenge", codeChallenge)
	set("code_challenge_method", "S256")
	if isSolo {
		set("hide_saas_login", "true")
	}

	encoded := values.Encode()
	// Append auth_callback_url *without* double-encoding. Trae's
	// server expects it verbatim (matches cockpit-tools append_pair
	// with should_encode=false).
	if callbackURL != "" {
		sep := "&"
		if encoded == "" {
			sep = ""
		}
		encoded = fmt.Sprintf("%s%sauth_callback_url=%s", encoded, sep, callbackURL)
	}
	u.RawQuery = encoded
	return u.String(), nil
}

// ---------------------------------------------------------------------------
// Candidate API origins + ExchangeToken
// ---------------------------------------------------------------------------

// candidateOrigins returns the ordered list of API origins to try for
// ExchangeToken / GetUserInfo. It includes the login host itself and
// "api."-prefixed variants, followed by the deployment defaults.
// For CN deployments the SG/US/USTTP variants are omitted.
func (t *TraeAuth) candidateOrigins(loginHost string) []string {
	seen := make(map[string]struct{})
	var out []string
	appendOrigin := func(o string) {
		o = strings.TrimRight(o, "/")
		if o == "" {
			return
		}
		if _, dup := seen[o]; dup {
			return
		}
		seen[o] = struct{}{}
		out = append(out, o)
	}

	// 1. loginHost derived (exact match + api. prefix)
	if u, err := ensureHTTPS(loginHost); err == nil {
		origin := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		appendOrigin(origin)
		if host := u.Hostname(); strings.HasPrefix(host, "www.") {
			appendOrigin(fmt.Sprintf("%s://api.%s", u.Scheme, strings.TrimPrefix(host, "www.")))
		}
	}
	// 2. Deployment defaults
	if t.endpoints.IsCN() {
		for _, o := range t.endpoints.AccountAPICandidatesCN() {
			appendOrigin(o)
		}
	} else {
		appendOrigin("https://api.marscode.com")
		appendOrigin("https://api.trae.ai")
		appendOrigin("https://www.trae.ai")
		appendOrigin("https://www.marscode.com")
	}
	return out
}

// ExchangeResult bundles the exchange response with the origin that
// actually served it, so GetUserInfo can reuse the same host. It also
// carries the DeviceInfo/DeviceKeyPair generated during the auth-code
// exchange so the scheduler can later refresh the session using a
// signed DeviceProof.
type ExchangeResult struct {
	Resp          *ExchangeResponse
	UsedOrigin    string
	DeviceInfo    map[string]any
	DeviceKeyPair *DeviceKeyPair
}

// ExchangeToken exchanges either an auth_code or a refresh_token for a
// fresh access_token bundle. It iterates candidate origins and both
// exchange paths, returning the first successful result.
//
// For the auth_code path, it mirrors cockpit-tools' official payload
// shape: ClientID + AuthCode + CodeVerifier + DeviceInfo (containing a
// freshly generated P-256 DevicePublicKey bound to the current
// DeviceContext) + IDEVersion. The resulting ExchangeResult records
// the DeviceInfo and DeviceKeyPair so later refresh operations can
// re-sign the request with a valid DeviceProof.
func (t *TraeAuth) ExchangeToken(ctx context.Context, loginHost string, dc DeviceContext, authCode, refreshToken, codeVerifier, redirectURI string) (*ExchangeResult, error) {
	if authCode == "" && refreshToken == "" {
		return nil, fmt.Errorf("trae auth: ExchangeToken requires authCode or refreshToken")
	}

	origins := t.candidateOrigins(loginHost)
	var errs []string

	// Materialize one key-pair + device-info per ExchangeToken call so
	// the server sees a consistent device identity across origins.
	// (Refresh path reuses existing stored material, not a fresh one.)
	var deviceInfo map[string]any
	var keyPair *DeviceKeyPair
	if authCode != "" {
		kp, err := GenerateDeviceKeyPair()
		if err != nil {
			return nil, fmt.Errorf("trae auth: generate device key pair failed: %w", err)
		}
		keyPair = kp
		deviceInfo = BuildOfficialDeviceInfo(dc, false /* isSolo — determined by caller via ClientID if needed, but payload field layout is identical */, kp.PublicKeyPEM)
	}

	for _, origin := range origins {
		for _, path := range t.endpoints.ExchangeTokenPaths {
			fullURL := strings.TrimRight(origin, "/") + path
			result, err := t.doExchangeToken(ctx, fullURL, dc, authCode, refreshToken, codeVerifier, redirectURI, deviceInfo, dc.XAppVersion)
			if err == nil && result != nil && result.Resp != nil {
				result.UsedOrigin = origin
				result.DeviceInfo = deviceInfo
				result.DeviceKeyPair = keyPair
				return result, nil
			}
			errs = append(errs, fmt.Sprintf("%s => %v", fullURL, err))
		}
	}
	return nil, fmt.Errorf("trae auth: ExchangeToken failed: %s", strings.Join(errs, " | "))
}

// doExchangeToken performs a single POST to one ExchangeToken URL.
//
// Payload shape differs slightly depending on grant type, matching
// cockpit-tools exactly:
//
//	auth_code flow: ClientID / AuthCode / CodeVerifier / DeviceInfo /
//	                IDEVersion
//	refresh flow:   ClientID / ClientSecret / RefreshToken / UserID=""
//
// In both cases we also include the snake_case aliases (client_id,
// refresh_token, redirect_uri) so older / OAuth2-style endpoints can
// still read the request; these aliases are additive only and are not
// present on the reference call paths.
func (t *TraeAuth) doExchangeToken(
	ctx context.Context,
	fullURL string,
	dc DeviceContext,
	authCode, refreshToken, codeVerifier, redirectURI string,
	deviceInfo map[string]any,
	ideVersion string,
) (*ExchangeResult, error) {
	payload := make(map[string]any, 12)

	if authCode != "" {
		// Official auth-code exchange payload shape.
		payload["ClientID"] = dc.ClientID
		payload["AuthCode"] = authCode
		payload["CodeVerifier"] = codeVerifier
		if deviceInfo != nil {
			payload["DeviceInfo"] = deviceInfo
		}
		if ideVersion != "" {
			payload["IDEVersion"] = ideVersion
		}
		// Additive aliases for compatibility with stricter parsers.
		payload["client_id"] = dc.ClientID
		if redirectURI != "" {
			payload["RedirectUri"] = redirectURI
			payload["redirect_uri"] = redirectURI
		}
	} else {
		// Refresh flow.
		payload["ClientID"] = dc.ClientID
		payload["ClientSecret"] = t.endpoints.ExchangeClientSecret
		payload["RefreshToken"] = refreshToken
		payload["UserID"] = ""
		// Additive OAuth2 alias.
		payload["refresh_token"] = refreshToken
		payload["grant_type"] = "refresh_token"
		payload["client_id"] = dc.ClientID
		payload["client_secret"] = t.endpoints.ExchangeClientSecret
	}

	bodyJSON, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI/1.0 trae-authenticator")
	// cockpit-tools sends an empty x-cloudide-token header on the
	// auth-code exchange. When the shortcut CloudIDEToken path is used
	// we override this via Set below.
	cloudideToken := ""
	if cc, ok := payload["CloudIDEToken"]; ok {
		if s, ok := cc.(string); ok {
			cloudideToken = s
		}
	}
	req.Header.Set("x-cloudide-token", cloudideToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(raw))
	}

	accessToken := pickAnyString(raw,
		"Result.AccessToken", "Result.accessToken", "Result.Token", "Result.token",
		"result.accessToken", "result.access_token", "result.Token", "result.token",
		"data.accessToken", "data.access_token", "data.Token", "data.token",
		"Token", "accessToken", "access_token", "token",
	)
	if accessToken == "" {
		return nil, fmt.Errorf("response missing access token: %s", truncateBody(raw))
	}
	refreshOut := pickAnyString(raw,
		"Result.RefreshToken", "Result.refreshToken",
		"result.refreshToken", "result.refresh_token",
		"data.refreshToken", "data.refresh_token",
		"refreshToken", "refresh_token",
	)
	if refreshOut == "" && refreshToken != "" {
		refreshOut = refreshToken // preserve input refresh if server omits
	}
	tokenType := pickAnyString(raw, "Result.TokenType", "result.tokenType", "token_type", "TokenType", "tokenType")
	if tokenType == "" {
		tokenType = "Bearer"
	}
	expiresAt := pickAnyInt64(raw,
		"Result.ExpiresAt", "result.expiresAt", "result.expire_time", "expiresAt", "expires_at", "expire_time",
		"Result.ExpiresIn", "result.expiresIn", "expires_in",
	)
	// If only expires_in (seconds) was provided, convert to absolute timestamp
	if expiresAt > 0 && expiresAt < 1e9 {
		expiresAt = time.Now().Add(time.Duration(expiresAt) * time.Second).Unix()
	}

	return &ExchangeResult{
		Resp: &ExchangeResponse{
			AccessToken:     accessToken,
			RefreshToken:    refreshOut,
			TokenType:       tokenType,
			ExpiresAt:       expiresAt,
			RawResponseBody: raw,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// GetUserInfo
// ---------------------------------------------------------------------------

// GetUserInfo fetches the profile of the authenticated user. It tries
// each origin from the provided list (usually a single-element list
// consisting of the successful exchange origin, preceded by the
// loginHost candidates as a fallback).
func (t *TraeAuth) GetUserInfo(ctx context.Context, origins []string, accessToken string) (*UserInfoResponse, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("trae auth: GetUserInfo requires accessToken")
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("trae auth: GetUserInfo requires at least one origin")
	}

	var errs []string
	for _, origin := range origins {
		fullURL := strings.TrimRight(origin, "/") + t.endpoints.GetUserInfoPath
		info, err := t.doGetUserInfo(ctx, fullURL, accessToken)
		if err == nil && info != nil && (info.Email != "" || info.UserID != "") {
			return info, nil
		}
		errs = append(errs, fmt.Sprintf("%s => %v", fullURL, err))
	}
	// Soft-fallback: return a partial UserInfo rather than error so
	// callers can still write a deterministic auth filename using the
	// access token hash or similar.
	log.Debugf("trae auth: GetUserInfo unsuccessful (%s); returning empty profile", strings.Join(errs, " | "))
	return &UserInfoResponse{}, nil
}

func (t *TraeAuth) doGetUserInfo(ctx context.Context, fullURL, accessToken string) (*UserInfoResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI/1.0 trae-authenticator")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	// Trae also accepts x-cloudide-token on some endpoints; keep
	// Authorization as the primary form and pass a duplicate token
	// header for compatibility.
	req.Header.Set("x-cloudide-token", accessToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(raw))
	}
	email := pickAnyString(raw,
		"Result.Email", "Result.email", "result.Email", "result.email",
		"data.Email", "data.email", "Email", "email",
	)
	userID := pickAnyString(raw,
		"Result.UserID", "Result.userId", "Result.user_id", "Result.id",
		"result.UserID", "result.userId", "result.user_id", "result.id",
		"data.UserID", "data.userId", "data.user_id", "data.id",
		"UserID", "userId", "user_id", "id",
	)
	nickname := pickAnyString(raw,
		"Result.Nickname", "Result.nickname", "Result.Name", "Result.name",
		"result.Nickname", "result.nickname", "result.name",
		"data.Nickname", "data.nickname",
		"Nickname", "nickname", "Name", "name",
	)
	return &UserInfoResponse{Email: email, UserID: userID, Nickname: nickname}, nil
}

// ---------------------------------------------------------------------------
// Small helper helpers
// ---------------------------------------------------------------------------

// ensureHTTPS parses a raw host/URL and ensures it has the https://
// scheme, returning a *url.URL. It accepts "https://host", "host",
// "/path" is rejected.
func ensureHTTPS(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty URL")
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + strings.TrimLeft(trimmed, "/")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// pickAnyString returns the first non-empty gjson result across the
// provided dotted paths.
func pickAnyString(rawJSON []byte, paths ...string) string {
	for _, p := range paths {
		r := gjson.GetBytes(rawJSON, p)
		if !r.Exists() {
			continue
		}
		switch r.Type {
		case gjson.String:
			if s := strings.TrimSpace(r.String()); s != "" {
				return s
			}
		case gjson.Number:
			return r.String()
		default:
			s := strings.TrimSpace(r.String())
			if s != "" {
				return s
			}
		}
	}
	return ""
}

// pickAnyInt64 returns the first numeric value from the JSON, as int64
// (number of seconds, or string-encoded numeric).
func pickAnyInt64(rawJSON []byte, paths ...string) int64 {
	for _, p := range paths {
		r := gjson.GetBytes(rawJSON, p)
		if !r.Exists() {
			continue
		}
		switch r.Type {
		case gjson.Number:
			return r.Int()
		case gjson.String:
			s := strings.TrimSpace(r.String())
			if s == "" {
				continue
			}
			// Try parsing as int64 first, then float
			var n int64
			if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

// truncateBody keeps error messages short and avoids leaking long
// JWTs into logs.
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}
