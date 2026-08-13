package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

const (
	// DefaultOAuthHost is the CN OAuth API host.
	DefaultOAuthHost = "https://api-cn.trae.com.cn"
	// DefaultAPIBase is the CN SOLO agent API base URL.
	DefaultAPIBase = "https://core-normal.trae.com.cn/api/remote/v1"
	// DefaultAuthPage is the CN authorization page URL.
	DefaultAuthPage = "https://www.trae.com.cn/authorization"
	// DefaultClientID is the default OAuth client ID (same as international, may need CN-specific value).
	DefaultClientID = "en1oxy7wnw8j9n"

	oauthGetRefreshTokenPath = "/cloudide/api/v3/trae/oauth/GetRefreshToken"
	oauthExchangeTokenPath   = "/cloudide/api/v3/trae/oauth/ExchangeToken"

	pollInterval   = 5 * time.Second
	maxPollDuration = 5 * time.Minute

	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
)

// TraeAuth handles OAuth authentication for the Trae CN SOLO agent API.
type TraeAuth struct {
	httpClient  *http.Client
	cfg         *config.Config
	oauthHost   string
	apiBase     string
	authPageURL string
	clientID    string
}

// NewTraeAuth creates a new TraeAuth with CN defaults and env var overrides.
func NewTraeAuth(cfg *config.Config) *TraeAuth {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		httpClient = util.SetProxy(&cfg.SDKConfig, httpClient)
	}

	oauthHost := DefaultOAuthHost
	apiBase := DefaultAPIBase
	authPage := DefaultAuthPage
	clientID := DefaultClientID

	if v := os.Getenv("TRAE_CN_OAUTH_HOST"); v != "" {
		oauthHost = v
	}
	if v := os.Getenv("TRAE_CN_API_BASE"); v != "" {
		apiBase = v
	}
	if v := os.Getenv("TRAE_CN_AUTH_PAGE"); v != "" {
		authPage = v
	}
	if v := os.Getenv("TRAE_CN_CLIENT_ID"); v != "" {
		clientID = v
	}

	return &TraeAuth{
		httpClient:  httpClient,
		cfg:         cfg,
		oauthHost:   oauthHost,
		apiBase:     apiBase,
		authPageURL: authPage,
		clientID:    clientID,
	}
}

// AuthState holds the authorization URL and client ID for the login flow.
type AuthState struct {
	ClientID string
	AuthURL  string
}

// FetchAuthState constructs the authorization URL for the user to log in.
func (a *TraeAuth) FetchAuthState(ctx context.Context) (*AuthState, error) {
	clientID := a.clientID
	authURL := fmt.Sprintf("%s?client_id=%s", a.authPageURL, clientID)
	return &AuthState{
		ClientID: clientID,
		AuthURL:  authURL,
	}, nil
}

// refreshTokenResponse represents the response from GetRefreshToken.
type refreshTokenResponse struct {
	Result struct {
		RefreshToken string `json:"RefreshToken"`
	} `json:"Result"`
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
}

// PollForRefreshToken polls the GetRefreshToken endpoint until the user completes
// browser authorization and returns the refresh token.
func (a *TraeAuth) PollForRefreshToken(ctx context.Context, clientID string) (string, error) {
	deadline := time.Now().Add(maxPollDuration)
	url := fmt.Sprintf("%s%s", a.oauthHost, oauthGetRefreshTokenPath)
	body, _ := json.Marshal(map[string]string{"clientID": clientID})

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			log.Debugf("trae: failed to create poll request: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)

		resp, err := a.httpClient.Do(req)
		if err != nil {
			log.Debugf("trae: poll request error: %v", err)
			continue
		}
		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			log.Debugf("trae: failed to read poll response: %v", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Debugf("trae: poll returned status %d", resp.StatusCode)
			continue
		}

		var result refreshTokenResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			log.Debugf("trae: failed to parse poll response: %v", err)
			continue
		}

		// Check for errors - if the refresh token is not yet available, the API
		// may return an error or an empty RefreshToken.
		if result.ResponseMetadata.Error != nil {
			errCode := result.ResponseMetadata.Error.Code
			if strings.Contains(errCode, "Pending") || strings.Contains(errCode, "NotFound") || strings.Contains(errCode, "Wait") {
				continue
			}
			return "", fmt.Errorf("%w: %s: %s", ErrTokenFetchFailed, errCode, result.ResponseMetadata.Error.Message)
		}

		if result.Result.RefreshToken != "" {
			return result.Result.RefreshToken, nil
		}
	}
	return "", ErrPollingTimeout
}

// exchangeTokenResponse represents the response from ExchangeToken.
type exchangeTokenResponse struct {
	Result struct {
		Token               string    `json:"Token"`
		RefreshToken        string    `json:"RefreshToken"`
		TokenExpireAt       time.Time `json:"TokenExpireAt"`
		RefreshExpireAt     time.Time `json:"RefreshExpireAt"`
		TokenExpireDuration int64     `json:"TokenExpireDuration"`
		UserID              string    `json:"UserID"`
		TenantID            string    `json:"TenantID"`
	} `json:"Result"`
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
}

// ExchangeToken exchanges a refresh token for a JWT and returns the token storage.
func (a *TraeAuth) ExchangeToken(ctx context.Context, clientID, refreshToken string) (*TraeTokenStorage, error) {
	url := fmt.Sprintf("%s%s", a.oauthHost, oauthExchangeTokenPath)
	body, _ := json.Marshal(map[string]string{
		"ClientID":     clientID,
		"RefreshToken": refreshToken,
		"ClientSecret": "-",
		"UserID":       "",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("trae: failed to create exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae: exchange request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("trae exchange: close body error: %v", errClose)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trae: failed to read exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trae: exchange failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result exchangeTokenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("trae: failed to parse exchange response: %w", err)
	}

	if result.ResponseMetadata.Error != nil {
		errCode := result.ResponseMetadata.Error.Code
		if errCode == "RefreshTokenInvalid" {
			return nil, fmt.Errorf("%w: %s", ErrRefreshTokenInvalid, result.ResponseMetadata.Error.Message)
		}
		return nil, fmt.Errorf("trae: exchange failed: %s: %s", errCode, result.ResponseMetadata.Error.Message)
	}

	if result.Result.Token == "" {
		return nil, fmt.Errorf("%w: empty token in response", ErrTokenFetchFailed)
	}

	// If expiry times are zero, estimate from duration.
	tokenExpireAt := result.Result.TokenExpireAt
	if tokenExpireAt.IsZero() && result.Result.TokenExpireDuration > 0 {
		tokenExpireAt = time.Now().Add(time.Duration(result.Result.TokenExpireDuration) * time.Second)
	} else if tokenExpireAt.IsZero() {
		tokenExpireAt = time.Now().Add(14 * 24 * time.Hour) // default 14 days
	}

	storage := &TraeTokenStorage{
		Token:            result.Result.Token,
		RefreshToken:     result.Result.RefreshToken,
		UserID:           result.Result.UserID,
		TenantID:         result.Result.TenantID,
		ClientID:         clientID,
		TokenExpireAt:    tokenExpireAt,
		RefreshExpireAt:  result.Result.RefreshExpireAt,
		Type:             "trae-cn",
		Scope:            "marscode-cn",
		Tenant:           "marscode",
		Region:           "CN",
		AIRegion:         "CN",
		UserIdentity:     "Free",
	}

	// Generate a web_id if not provided by the API.
	if storage.WebID == "" {
		storage.WebID = uuid.NewString()
	}

	return storage, nil
}

// RefreshToken re-exchanges the stored refresh token for a new JWT.
func (a *TraeAuth) RefreshToken(ctx context.Context, clientID, refreshToken string) (*TraeTokenStorage, error) {
	return a.ExchangeToken(ctx, clientID, refreshToken)
}
