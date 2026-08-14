// Package executor provides runtime execution capabilities for various AI providers.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// TraeExecutor handles requests to the Trae API.
type TraeExecutor struct {
	cfg        *config.Config
	httpClient *http.Client
}

// NewTraeExecutor creates a new executor for Trae API requests.
func NewTraeExecutor(cfg *config.Config) *TraeExecutor {
	return &TraeExecutor{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   5 * time.Minute,
		},
	}
}

// Identifier returns the executor identifier.
func (te *TraeExecutor) Identifier() string { return "trae" }

// GetAccessToken extracts access token from auth record.
func GetTraeAccessToken(a *cliproxyauth.Auth) string {
	if a == nil {
		return ""
	}

	// Try attributes first
	if attrKey := strings.TrimSpace(a.Attributes["access_token"]); attrKey != "" {
		return attrKey
	}

	// Try metadata
	if a.Metadata != nil {
		if token, ok := a.Metadata["access_token"].(string); ok {
			return token
		}
	}

	return ""
}

// GetRefreshToken extracts refresh token from auth record.
func GetTraeRefreshToken(a *cliproxyauth.Auth) string {
	if a == nil {
		return ""
	}

	if attrKey := strings.TrimSpace(a.Attributes["refresh_token"]); attrKey != "" {
		return attrKey
	}

	if a.Metadata != nil {
		if token, ok := a.Metadata["refresh_token"].(string); ok {
			return token
		}
	}

	return ""
}

// Refresh refreshes the Trae token using the refresh token.
func (te *TraeExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("trae executor: refresh called")

	if auth == nil {
		return nil, fmt.Errorf("trae executor: auth is nil")
	}

	var refreshToken string
	if auth.Metadata != nil {
		if v, ok := auth.Metadata["refresh_token"].(string); ok && strings.TrimSpace(v) != "" {
			refreshToken = v
		}
	}
	if strings.TrimSpace(refreshToken) == "" {
		return auth, nil
	}

	authSvc := trae.NewTraeAuth(te.cfg)
	tokenData, err := authSvc.RefreshTokens(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = tokenData.AccessToken
	if tokenData.RefreshToken != "" {
		auth.Metadata["refresh_token"] = tokenData.RefreshToken
	}
	if tokenData.ExpiresAt != "" {
		auth.Metadata["expires_at"] = tokenData.ExpiresAt
	}
	auth.Metadata["type"] = "trae"

	return auth, nil
}

// Execute sends a chat completion request to the Trae API.
func (te *TraeExecutor) Execute(ctx context.Context, req *http.Request, auth *cliproxyauth.Auth) (*http.Response, error) {
	if te == nil || req == nil {
		return nil, fmt.Errorf("trae executor: invalid input")
	}

	token := GetTraeAccessToken(auth)
	if token == "" {
		return nil, fmt.Errorf("trae: missing access token")
	}

	// Build upstream request URL
	baseURL := trae.APIBaseURL()
	upstreamPath := "/v1/chat/completions"
	if strings.HasPrefix(req.URL.Path, "/v1/responses") {
		upstreamPath = "/v1/responses"
	}

	upstreamURL := baseURL + upstreamPath

	// Read request body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("trae: failed to read request body: %w", err)
	}

	// Create upstream request
	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("trae: failed to create upstream request: %w", err)
	}

	// Copy headers
	for key, values := range req.Header {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	// Set Trae-specific headers
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("User-Agent", "CLIProxyAPI/v7")
	upstreamReq.Header.Set("Accept", "application/json")

	// Execute upstream request
	resp, err := te.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("trae: upstream request failed: %w", err)
	}

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("trae: failed to read response body: %w", err)
	}

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("trae: upstream returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Return response
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, nil
}

// FetchModels fetches available models from the Trae API.
func FetchTraeModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) ([]*ModelInfo, error) {
	if auth == nil || cfg == nil {
		return nil, nil
	}

	token := GetTraeAccessToken(auth)
	if token == "" {
		return nil, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trae.APIBaseURL()+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("trae: failed to create models request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Debugf("trae: failed to fetch models: %v", err)
		return nil, nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae: failed to read models response: %w", err)
	}

	var modelsResp struct {
		Data []struct {
			ID          string `json:"id"`
			Object      string `json:"object"`
			OwnedBy     string `json:"owned_by"`
			Description string `json:"description,omitempty"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		log.Debugf("trae: failed to parse models response: %v", err)
		return nil, nil
	}

	now := time.Now().Unix()
	models := make([]*ModelInfo, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, &ModelInfo{
			ID:          "trae-" + m.ID,
			Object:      "model",
			Created:     now,
			OwnedBy:     m.OwnedBy,
			Type:        "trae",
			DisplayName: m.ID,
			Description: m.Description,
		})
	}

	return models, nil
}
