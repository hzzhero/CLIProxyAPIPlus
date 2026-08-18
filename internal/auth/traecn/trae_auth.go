package traecn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

// DeviceFingerprint holds the x-* header values Trae expects on every call.
type DeviceFingerprint struct {
	DeviceID       string
	MachineID      string
	DeviceBrand    string
	DeviceCPU      string
	DeviceType     string
	OSVersion      string
	IDEVersion     string
	IDEVersionCode string
	IDEVersionType string
}

// NewDeviceFingerprint creates a new device fingerprint with random IDs.
func NewDeviceFingerprint() DeviceFingerprint {
	return DeviceFingerprint{
		DeviceID:       uuid.New().String(),
		MachineID:      uuid.New().String(),
		DeviceBrand:    "Microsoft",
		DeviceCPU:      "x86_64",
		DeviceType:     "Windows",
		OSVersion:      "10.0.19045",
		IDEVersion:     "2.0.0",
		IDEVersionCode: "20000",
		IDEVersionType: "stable",
	}
}

// BuildAuthorizeURL constructs the Trae authorization page URL.
func BuildAuthorizeURL(fp DeviceFingerprint, callbackPort int) string {
	base := AuthorizeURL + "/authorization"
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("x_device_id", fp.DeviceID)
	params.Set("x_machine_id", fp.MachineID)
	if fp.DeviceBrand != "" {
		params.Set("x_device_brand", fp.DeviceBrand)
	}
	if fp.DeviceType != "" {
		params.Set("x_device_type", fp.DeviceType)
	}
	if fp.OSVersion != "" {
		params.Set("x_os_version", fp.OSVersion)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/authorize", callbackPort)
	params.Set("redirect_uri", redirectURI)
	return base + "?" + params.Encode()
}

// TokenData holds the result of a successful token exchange.
type TokenData struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // seconds from now (for non-ms expiry sources)
	UserID       string
}

// TraeCNAuth manages authentication and token handling for Trae CN.
type TraeCNAuth struct {
	httpClient *http.Client
}

// NewTraeCNAuth creates a new TraeCNAuth instance.
func NewTraeCNAuth(cfg *config.Config) *TraeCNAuth {
	return &TraeCNAuth{
		httpClient: util.SetProxy(&cfg.SDKConfig, &http.Client{Timeout: 15 * time.Second}),
	}
}

// GetRefreshToken requests a refresh token after browser authorization.
func (a *TraeCNAuth) GetRefreshToken(ctx context.Context, clientID string) (string, error) {
	reqBody := map[string]string{"clientID": clientID}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal GetRefreshToken request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GetRefreshTokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to create GetRefreshToken request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GetRefreshToken request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read GetRefreshToken response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GetRefreshToken failed: %d %s. Response: %s", resp.StatusCode, resp.Status, truncateBody(body, 500))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse GetRefreshToken response: %w", err)
	}

	// Result may be nested under a "data" key or flat.
	var refreshToken string
	if data, ok := result["data"].(map[string]any); ok {
		if rt, ok := data["refresh_token"].(string); ok {
			refreshToken = rt
		}
	} else if rt, ok := result["refresh_token"].(string); ok {
		refreshToken = rt
	}
	if rt, ok := result["Result"].(map[string]any); ok {
		if inner, ok := rt["RefreshToken"].(string); ok {
			refreshToken = inner
		}
	}

	if refreshToken == "" {
		return "", fmt.Errorf("GetRefreshToken returned empty refresh token; raw response: %s", string(body))
	}
	return refreshToken, nil
}

// ExchangeToken exchanges a refresh token for an IDE access token.
func (a *TraeCNAuth) ExchangeToken(ctx context.Context, clientID, refreshToken string) (*TokenData, error) {
	reqBody := map[string]string{
		"ClientID":     clientID,
		"RefreshToken": refreshToken,
		"ClientSecret": "-",
		"UserID":       "",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ExchangeToken request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ExchangeTokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create ExchangeToken request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ExchangeToken request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ExchangeToken response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ExchangeToken failed: %d %s. Response: %s", resp.StatusCode, resp.Status, truncateBody(body, 500))
	}

	// The response wraps fields under "Result" based on reverse-engineering notes.
	var rawResult map[string]any
	if err := json.Unmarshal(body, &rawResult); err != nil {
		return nil, fmt.Errorf("failed to parse ExchangeToken response: %w", err)
	}

	var result map[string]any
	if r, ok := rawResult["Result"].(map[string]any); ok {
		result = r
	} else {
		result = rawResult
	}

	accessToken, _ := result["Token"].(string)
	refreshTokenOut, _ := result["RefreshToken"].(string)
	userID, _ := result["UserID"].(string)

	var expiresIn int64
	if e, ok := result["ExpiresIn"].(float64); ok {
		expiresIn = int64(e)
	} else if e, ok := result["expires_in"].(float64); ok {
		expiresIn = int64(e)
	}

	if accessToken == "" {
		return nil, fmt.Errorf("trae-cn: ExchangeToken returned empty token (endpoint may have changed, check endpoints.go)")
	}

	return &TokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenOut,
		ExpiresIn:    expiresIn,
		UserID:       userID,
	}, nil
}

// ParseCallbackURL extracts query parameters from a callback URL,
// supporting both query string and fragment formats.
func ParseCallbackURL(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty callback URL")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse callback URL: %w", err)
	}

	params := make(map[string]string)

	// Merge query parameters.
	for k, v := range u.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	// Merge fragment parameters (some OAuth flows put params there).
	if u.Fragment != "" {
		fragParams, err := url.ParseQuery(u.Fragment)
		if err == nil {
			for k, v := range fragParams {
				if len(v) > 0 {
					params[k] = v[0]
				}
			}
		}
	}

	if len(params) == 0 {
		return nil, fmt.Errorf("no parameters found in callback URL: %s", raw)
	}
	return params, nil
}

// CreateTokenStorage assembles a TraeCNTokenStorage from the token data.
func CreateTokenStorage(td *TokenData, fp DeviceFingerprint, email string, expireMs int64) *TraeCNTokenStorage {
	storage := &TraeCNTokenStorage{
		Token:        td.AccessToken,
		RefreshToken: td.RefreshToken,
		Email:        email,
		UserID:       td.UserID,
		ExpireTime:   expireMs,
		LastRefresh:  time.Now().Format(time.RFC3339),
		DeviceID:     fp.DeviceID,
		MachineID:    fp.MachineID,
		DeviceBrand:  fp.DeviceBrand,
		DeviceCPU:    fp.DeviceCPU,
		DeviceType:   fp.DeviceType,
		OSVersion:    fp.OSVersion,
		IDEVersion:   fp.IDEVersion,
		IDEVersionCode: fp.IDEVersionCode,
		IDEVersionType: fp.IDEVersionType,
		Type:         "trae-cn",
	}
	storage.SetMetadata()
	return storage
}

// FirstNonEmptyParam returns the first non-empty value from params matching any of the given keys.
func FirstNonEmptyParam(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := params[key]; v != "" {
			return v
		}
	}
	return ""
}

// ParamKeys returns sorted parameter keys for error reporting.
func ParamKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncateBody(body []byte, n int) string {
	if len(body) <= n {
		return string(body)
	}
	return string(body[:n]) + "..."
}
