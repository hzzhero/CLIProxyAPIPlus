package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	// agnesDefaultBaseURL is the China edition API base URL.
	// The international edition uses https://apihub.agnes-ai.com/v1 — same keys,
	// same paths, only the host differs. Users can override via AGNES_BASE_URL
	// or Metadata["base_url"].
	agnesDefaultBaseURL = "https://apihub.agnes-ai.cn/v1"

	// agnesAPIKeyMetadataKey is the LoginOptions.Metadata key carrying the API key.
	agnesAPIKeyMetadataKey = "api_key"

	// agnesBaseURLMetadataKey is the LoginOptions.Metadata key carrying the base URL.
	agnesBaseURLMetadataKey = "base_url"
)

// agnesAPIKeyEnvKeys are the environment variables consulted for the Agnes API
// key, in priority order.
var agnesAPIKeyEnvKeys = []string{"AGNES_API_KEY", "AGNES_PERSONAL_ACCESS_TOKEN"}

// agnesBaseURLEnvKeys are the environment variables consulted for the base URL.
var agnesBaseURLEnvKeys = []string{"AGNES_BASE_URL"}

// AgnesAuthenticator implements API-key-based login for Agnes AI.
//
// Agnes uses a static sk-... Bearer token against an OpenAI-compatible API.
// There is no OAuth device flow, no token exchange, and no refresh — the API
// key is created manually in the dashboard at https://platform.agnes-ai.cn/
// and persists indefinitely until revoked. Because the API is fully
// OpenAI-compatible, the executor is the shared OpenAICompatExecutor; this
// authenticator only handles credential acquisition and persistence.
type AgnesAuthenticator struct{}

// NewAgnesAuthenticator constructs an Agnes authenticator.
func NewAgnesAuthenticator() Authenticator {
	return &AgnesAuthenticator{}
}

// Provider returns the provider key for Agnes.
func (AgnesAuthenticator) Provider() string {
	return "agnes"
}

// RefreshLead returns nil to disable scheduled refresh — Agnes API keys are
// static and never expire.
func (AgnesAuthenticator) RefreshLead() *time.Duration {
	return nil
}

// resolveAPIKey resolves the API key from (in order): opts.Metadata, environment
// variables, then opts.Prompt. Returns an error when no key can be obtained.
func (AgnesAuthenticator) resolveAPIKey(opts *LoginOptions) (string, error) {
	if opts != nil && opts.Metadata != nil {
		if v := strings.TrimSpace(opts.Metadata[agnesAPIKeyMetadataKey]); v != "" {
			return v, nil
		}
	}
	for _, envKey := range agnesAPIKeyEnvKeys {
		if raw, ok := os.LookupEnv(envKey); ok {
			if v := strings.TrimSpace(raw); v != "" {
				return v, nil
			}
		}
	}
	if opts != nil && opts.Prompt != nil {
		fmt.Println()
		fmt.Println("Agnes AI uses an API key (sk-...) for login.")
		fmt.Println("Generate one at https://platform.agnes-ai.cn/")
		value, err := opts.Prompt("Enter Agnes API key (sk-...): ")
		if err != nil {
			return "", fmt.Errorf("agnes auth: failed to read API key: %w", err)
		}
		if v := strings.TrimSpace(value); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("agnes auth: API key is required (pass via Prompt, Metadata[%q], or %s)",
		agnesAPIKeyMetadataKey, agnesAPIKeyEnvKeys[0])
}

// resolveBaseURL resolves the base URL from opts.Metadata, environment variables,
// or the default CN endpoint.
func (AgnesAuthenticator) resolveBaseURL(opts *LoginOptions) string {
	if opts != nil && opts.Metadata != nil {
		if v := strings.TrimSpace(opts.Metadata[agnesBaseURLMetadataKey]); v != "" {
			return v
		}
	}
	for _, envKey := range agnesBaseURLEnvKeys {
		if raw, ok := os.LookupEnv(envKey); ok {
			if v := strings.TrimSpace(raw); v != "" {
				return v
			}
		}
	}
	return agnesDefaultBaseURL
}

// validateAPIKey performs a best-effort GET /models request to validate the
// API key and optionally extract a label (email or account name). On failure
// it returns empty strings and logs a warning — login still succeeds because
// the key might work for chat even if /models is unavailable.
func (AgnesAuthenticator) validateAPIKey(ctx context.Context, cfg *config.Config, baseURL, apiKey string) (label string) {
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Warnf("agnes: build validation request: %v", err)
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	httpClient := util.SetProxy(&cfg.SDKConfig, &http.Client{Timeout: 15 * time.Second})
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("agnes: API key validation failed (login continues): %v", err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Warnf("agnes: API key validation returned %d (login continues)", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("agnes: read validation response: %v", err)
		return ""
	}

	// The /models endpoint returns an OpenAI-compatible {"data": [...]} object.
	// We don't extract a user label from it (Agnes has no user-info endpoint),
	// but a successful 200 confirms the key is valid.
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &result); err == nil && len(result.Data) > 0 {
		log.Infof("agnes: API key validated, %d models available", len(result.Data))
	}
	return ""
}

// Login resolves the Agnes API key and persists it as an auth record.
func (a AgnesAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	apiKey, err := a.resolveAPIKey(opts)
	if err != nil {
		return nil, err
	}

	baseURL := a.resolveBaseURL(opts)

	// Best-effort validation — confirms the key works before persisting.
	label := a.validateAPIKey(ctx, cfg, baseURL, apiKey)
	if label == "" {
		if opts.Metadata != nil {
			if v := strings.TrimSpace(opts.Metadata["email"]); v != "" {
				label = v
			} else if v := strings.TrimSpace(opts.Metadata["alias"]); v != "" {
				label = v
			}
		}
	}
	if label == "" {
		// Derive a short label from the API key prefix (sk-xxxx...).
		if len(apiKey) > 8 {
			label = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
		} else {
			label = fmt.Sprintf("user-%d", time.Now().UnixMilli())
		}
	}

	fileName := fmt.Sprintf("agnes-%s.json", sanitizeAgnesLabel(label))

	metadata := map[string]any{
		"type":         "agnes",
		"api_key":      apiKey,
		"base_url":     baseURL,
		"compat_name":  "agnes",
		"provider_key": "agnes",
		"auth_kind":    "api_key",
	}

	// Attributes are populated at login time so OpenAICompatExecutor can
	// resolve credentials immediately. The filestore/synthesizer also copies
	// these from metadata to Attributes on reload.
	attributes := map[string]string{
		"api_key":      apiKey,
		"base_url":     baseURL,
		"compat_name":  "agnes",
		"provider_key": "agnes",
	}

	fmt.Println("Agnes authentication successful")
	fmt.Printf("Base URL: %s\n", baseURL)

	return &coreauth.Auth{
		ID:         fileName,
		Provider:   a.Provider(),
		FileName:   fileName,
		Label:      label,
		Storage:    nil,
		Metadata:   metadata,
		Attributes: attributes,
	}, nil
}

// sanitizeAgnesLabel produces a filesystem-safe label for the auth file name.
func sanitizeAgnesLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "user"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "user"
	}
	return result
}
