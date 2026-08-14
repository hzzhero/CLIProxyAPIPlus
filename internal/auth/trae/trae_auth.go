// Package trae provides authentication and token management for ByteDance's Trae IDE.
// It handles the OAuth2 authorization code flow with PKCE for secure authentication.
package trae

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
)

// Trae OAuth2 configuration constants.
const (
	// traeClientID is the public client ID for Trae OAuth.
	traeClientID = "en1oxy7wnw8j9n"

	// traeOAuthHost is the OAuth server endpoint.
	traeOAuthHost = "https://account.trae.ai"

	// traeTokenURL is the endpoint for exchanging tokens.
	traeTokenURL = traeOAuthHost + "/oauth/token"

	// traeAuthorizeURL is the authorization endpoint.
	traeAuthorizeURL = traeOAuthHost + "/oauth/authorize"

	// traeUserInfoURL is the user info endpoint.
	traeUserInfoURL = traeOAuthHost + "/api/userinfo"

	// traeAPIBaseURL is the base URL for Trae API requests.
	traeAPIBaseURL = "https://api.trae.ai"

	// defaultCallbackPort is the local port for OAuth callback.
	DefaultCallbackPort = 18789

	// defaultCallbackPath is the callback path.
	defaultCallbackPath = "/oauth/callback"

	// traeScope is the OAuth scope for Trae.
	traeScope = "openid profile email"
)

// TraeAuth handles Trae authentication flow.
type TraeAuth struct {
	httpClient *http.Client
	cfg        *config.Config
}

// NewTraeAuth creates a new TraeAuth service instance.
func NewTraeAuth(cfg *config.Config) *TraeAuth {
	client := &http.Client{Timeout: 30 * time.Second}
	return &TraeAuth{
		httpClient: util.SetProxy(&cfg.SDKConfig, client),
		cfg:        cfg,
	}
}

// AuthorizationURL builds the OAuth2 authorization URL with PKCE.
func (ta *TraeAuth) AuthorizationURL(state string, port int) (authURL, redirectURI, codeVerifier string, err error) {
	redirectURI = fmt.Sprintf("http://localhost:%d%s", port, defaultCallbackPath)

	// Generate PKCE code verifier and challenge
	codeVerifier = generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", traeClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", traeScope)
	values.Set("state", state)
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", "S256")

	authURL = fmt.Sprintf("%s?%s", traeAuthorizeURL, values.Encode())
	return authURL, redirectURI, codeVerifier, nil
}

// ExchangeCodeForTokens exchanges an authorization code for access and refresh tokens.
func (ta *TraeAuth) ExchangeCodeForTokens(ctx context.Context, code, redirectURI, codeVerifier string) (*TokenData, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", traeClientID)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, traeTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("trae token: create request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := ta.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae token: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae token: read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("trae token request failed: status=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("trae token: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err = json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("trae token: decode response failed: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("trae token: missing access token in response")
	}

	// Fetch user info
	userInfo, err := ta.FetchUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		log.Warnf("trae: failed to fetch user info: %v", err)
		userInfo = &UserInfo{}
	}

	data := &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
		Email:        userInfo.Email,
		UserID:       userInfo.UserID,
		Type:         "trae",
	}

	if tokenResp.ExpiresIn > 0 {
		data.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}

	return data, nil
}

// RefreshTokens exchanges a refresh token for a new access token.
func (ta *TraeAuth) RefreshTokens(ctx context.Context, refreshToken string) (*TokenData, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", traeClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, traeTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("trae token: create refresh request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := ta.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae token: refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae token: read refresh response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("trae token refresh failed: status=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("trae token refresh: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err = json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("trae token: decode refresh response failed: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("trae token: missing access token in refresh response")
	}

	return &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
		Type:         "trae",
	}, nil
}

// UserInfo represents the user information from Trae.
type UserInfo struct {
	UserID  string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Picture string `json:"picture"`
}

// FetchUserInfo retrieves user information using the access token.
func (ta *TraeAuth) FetchUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traeUserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("trae userinfo: create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := ta.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae userinfo: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae userinfo: read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trae userinfo: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var userInfo UserInfo
	if err = json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("trae userinfo: decode response failed: %w", err)
	}

	return &userInfo, nil
}

// CreateTokenStorage converts token data into persistence storage.
func (ta *TraeAuth) CreateTokenStorage(data *TokenData) *TokenStorage {
	if data == nil {
		return nil
	}
	return &TokenStorage{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		TokenType:    data.TokenType,
		Scope:        data.Scope,
		ExpiresAt:    data.ExpiresAt,
		Email:        data.Email,
		UserID:       data.UserID,
		Type:         "trae",
	}
}

// LoadAndValidateToken loads a token from storage and validates it.
func (ta *TraeAuth) LoadAndValidateToken(ctx context.Context, storage *TokenStorage) (bool, error) {
	if storage == nil || storage.AccessToken == "" {
		return false, fmt.Errorf("no token available")
	}

	// Check if we should refresh
	if storage.NeedsRefresh() && storage.RefreshToken != "" {
		tokenData, err := ta.RefreshTokens(ctx, storage.RefreshToken)
		if err != nil {
			return false, fmt.Errorf("failed to refresh token: %w", err)
		}
		*storage = *ta.CreateTokenStorage(tokenData)
		return true, nil
	}

	if !storage.IsExpired() {
		return true, nil
	}

	if storage.RefreshToken == "" {
		return false, fmt.Errorf("token expired and no refresh token available")
	}

	return false, fmt.Errorf("token expired")
}

// GetAPIEndpoint returns the Trae API base endpoint.
func (ta *TraeAuth) GetAPIEndpoint() string {
	return traeAPIBaseURL
}

// APIBaseURL is a helper to get the Trae API base URL.
func APIBaseURL() string {
	return traeAPIBaseURL
}

// generateCodeVerifier generates a random PKCE code verifier.
func generateCodeVerifier() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i*17 + int(time.Now().UnixNano())%256)
	}
	// Encode and sanitize per PKCE spec (base64url, no padding)
	verifier := base64.RawURLEncoding.EncodeToString(b)
	return verifier
}

// generateCodeChallenge generates a PKCE code challenge from the verifier using SHA-256.
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	// Remove padding as per PKCE spec
	challenge = strings.TrimRight(challenge, "=")
	return challenge
}

// generateState generates a random state parameter for CSRF protection.
func generateState() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(i*13 + int(time.Now().UnixNano())%256)
	}
	return fmt.Sprintf("%x", b)
}
