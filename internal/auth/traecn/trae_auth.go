package traecn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// NewDeviceFingerprint returns a fingerprint with freshly generated device and
// machine IDs plus static values matching a current Trae CN Windows client.
func NewDeviceFingerprint() DeviceFingerprint {
	return DeviceFingerprint{
		DeviceID:       uuid.NewString(),
		MachineID:      uuid.NewString(),
		DeviceBrand:    "Microsoft",
		DeviceCPU:      "x86_64",
		DeviceType:     "Windows",
		OSVersion:      "10.0.19045",
		IDEVersion:     "2.0.0",
		IDEVersionCode: "20000",
		IDEVersionType: "stable",
	}
}

// BuildAuthorizeURL assembles the browser authorization URL the user must
// open to start the Trae CN login flow.
func BuildAuthorizeURL(fp DeviceFingerprint, callbackPort int) string {
	q := url.Values{}
	q.Set("client_id", ClientID)
	q.Set("x_device_id", fp.DeviceID)
	q.Set("x_machine_id", fp.MachineID)
	q.Set("x_device_brand", fp.DeviceBrand)
	q.Set("x_device_type", fp.DeviceType)
	q.Set("x_os_version", fp.OSVersion)
	q.Set("redirect_uri", fmt.Sprintf("http://127.0.0.1:%d/authorize", callbackPort))
	return AuthorizeURL + "?" + q.Encode()
}

// TokenData carries the credentials returned by the token exchange endpoint.
type TokenData struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	UserID       string
}

// TraeCNAuth manages authentication and token handling for Trae CN.
type TraeCNAuth struct {
	httpClient *http.Client
}

// NewTraeCNAuth creates a TraeCNAuth. When cfg is nil it falls back to
// http.DefaultClient (handy for tests); otherwise it builds a proxy-aware
// client from the SDK config, mirroring the qoder auth package.
func NewTraeCNAuth(cfg *config.Config) *TraeCNAuth {
	if cfg == nil {
		return &TraeCNAuth{httpClient: http.DefaultClient}
	}
	return &TraeCNAuth{httpClient: util.SetProxy(&cfg.SDKConfig, &http.Client{})}
}

// exchangeTokenResponse mirrors the ExchangeToken endpoint payload. The
// upstream nests credentials under "Result" with PascalCase keys.
type exchangeTokenResponse struct {
	Result struct {
		Token        string `json:"Token"`
		RefreshToken string `json:"RefreshToken"`
		ExpiresIn    int64  `json:"ExpiresIn"`
		UserID       string `json:"UserID"`
	} `json:"Result"`
}

// ExchangeToken trades a refresh token for a short-lived access token via
// POST ExchangeTokenURL.
func (a *TraeCNAuth) ExchangeToken(ctx context.Context, clientID, refreshToken string) (*TokenData, error) {
	reqBody := map[string]string{
		"ClientID":     clientID,
		"RefreshToken": refreshToken,
		"ClientSecret": "-",
		"UserID":       "",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: failed to marshal exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ExchangeTokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("trae-cn: failed to create exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: failed to read exchange response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trae-cn: exchange failed: %d %s. Response: %s", resp.StatusCode, resp.Status, truncateBody(body, 200))
	}

	var response exchangeTokenResponse
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("trae-cn: failed to parse exchange response: %w", err)
	}
	if strings.TrimSpace(response.Result.Token) == "" {
		return nil, fmt.Errorf("trae-cn: exchange returned empty token (endpoint may have changed, check endpoints.go)")
	}

	return &TokenData{
		AccessToken:  response.Result.Token,
		RefreshToken: response.Result.RefreshToken,
		ExpiresIn:    response.Result.ExpiresIn,
		UserID:       response.Result.UserID,
	}, nil
}

// truncateBody caps a response body at n bytes for safe inclusion in error
// messages, mirroring the qoder auth helper. Keeps tokens out of logs when
// the upstream echoes the request or returns a large error payload.
func truncateBody(body []byte, n int) string {
	if len(body) <= n {
		return string(body)
	}
	return string(body[:n]) + "..."
}

// ParseCallbackURL extracts login parameters from a Trae CN callback URL.
// Parameters may arrive either as regular query parameters or embedded in the
// fragment (e.g. https://host/login-success#token=...). All parameters from
// both sources are merged; an error is returned when the URL is invalid or
// carries no parameters at all.
func ParseCallbackURL(raw string) (map[string]string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: invalid callback URL: %w", err)
	}

	params := make(map[string]string)
	for key, values := range u.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	if frag := u.Fragment; frag != "" {
		if fragValues, errFrag := url.ParseQuery(frag); errFrag == nil {
			for key, values := range fragValues {
				if len(values) > 0 {
					params[key] = values[0]
				}
			}
		}
	}

	if len(params) == 0 {
		return nil, fmt.Errorf("trae-cn: callback URL contains no parameters")
	}
	return params, nil
}

// CreateTokenStorage assembles a TraeCNTokenStorage from exchanged token
// data, the device fingerprint used during login, and the account email.
func CreateTokenStorage(td *TokenData, fp DeviceFingerprint, email string, expireMs int64) *TraeCNTokenStorage {
	storage := &TraeCNTokenStorage{
		Token:        td.AccessToken,
		RefreshToken: td.RefreshToken,
		Email:        email,
		UserID:       td.UserID,
		ExpireTime:   expireMs,
		LastRefresh:  time.Now().Format(time.RFC3339),

		DeviceID:       fp.DeviceID,
		MachineID:      fp.MachineID,
		DeviceBrand:    fp.DeviceBrand,
		DeviceCPU:      fp.DeviceCPU,
		DeviceType:     fp.DeviceType,
		OSVersion:      fp.OSVersion,
		IDEVersion:     fp.IDEVersion,
		IDEVersionCode: fp.IDEVersionCode,
		IDEVersionType: fp.IDEVersionType,
	}
	storage.SetMetadata()
	return storage
}
