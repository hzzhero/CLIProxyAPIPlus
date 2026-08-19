package auth

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
)

// TestTraeCNBuildAuthRecord_SetsCompatBaseURL ensures the trae-cn auth
// record exposes the OpenAI-compatible gateway base URL and bearer token
// via Attributes. Without base_url the OpenAICompatExecutor returns
// "missing provider baseURL" on every chat request.
func TestTraeCNBuildAuthRecord_SetsCompatBaseURL(t *testing.T) {
	a := &TraeCNAuthenticator{}
	dc := trae.DefaultDeviceContext("client-id", trae.CNEndpoints)
	ex := &trae.ExchangeResult{
		Resp: &trae.ExchangeResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			TokenType:    "Bearer",
		},
		UsedOrigin: "https://api.trae.com.cn",
	}
	callback := &trae.CallbackParams{
		LoginHost:   "https://www.trae.cn",
		LoginRegion: "cn",
	}
	info := &trae.UserInfoResponse{
		Email:    "user@example.com",
		UserID:   "uid-1",
		Nickname: "tester",
	}

	auth, err := a.buildAuthRecord(dc, trae.CNEndpoints, callback, ex, info)
	if err != nil {
		t.Fatalf("buildAuthRecord() error = %v", err)
	}
	if auth == nil {
		t.Fatal("buildAuthRecord() returned nil auth")
	}
	if auth.Provider != "trae-cn" {
		t.Errorf("auth.Provider = %q, want trae-cn", auth.Provider)
	}

	baseURL := strings.TrimSpace(auth.Attributes["base_url"])
	if baseURL != "https://api.trae.com.cn/v1" {
		t.Errorf("Attributes[base_url] = %q, want https://api.trae.com.cn/v1", baseURL)
	}
	apiKey := strings.TrimSpace(auth.Attributes["api_key"])
	if apiKey != "test-access-token" {
		t.Errorf("Attributes[api_key] = %q, want test-access-token", apiKey)
	}
	cloudHeader := strings.TrimSpace(auth.Attributes["header:x-cloudide-token"])
	if cloudHeader != "test-access-token" {
		t.Errorf("Attributes[header:x-cloudide-token] = %q, want test-access-token", cloudHeader)
	}
}

// TestTraeCNBuildAuthRecord_BaseURLFromUsedOrigin verifies the gateway
// base URL is derived from the exchange UsedOrigin even when it carries a
// trailing slash.
func TestTraeCNBuildAuthRecord_BaseURLFromUsedOrigin(t *testing.T) {
	a := &TraeCNAuthenticator{}
	dc := trae.DefaultDeviceContext("client-id", trae.CNEndpoints)
	ex := &trae.ExchangeResult{
		Resp:       &trae.ExchangeResponse{AccessToken: "tok"},
		UsedOrigin: "https://api.trae.cn/",
	}
	auth, err := a.buildAuthRecord(dc, trae.CNEndpoints, &trae.CallbackParams{}, ex, &trae.UserInfoResponse{})
	if err != nil {
		t.Fatalf("buildAuthRecord() error = %v", err)
	}
	if got := auth.Attributes["base_url"]; got != "https://api.trae.cn/v1" {
		t.Errorf("Attributes[base_url] = %q, want https://api.trae.cn/v1", got)
	}
}
