package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// traeRefreshLead is the duration before token expiry when refresh should occur.
var traeRefreshLead = 5 * time.Minute

// TraeAuthenticator implements the OAuth2 authorization code flow login for Trae IDE.
type TraeAuthenticator struct{}

// NewTraeAuthenticator constructs a new Trae authenticator.
func NewTraeAuthenticator() Authenticator {
	return &TraeAuthenticator{}
}

// Provider returns the provider key for trae.
func (TraeAuthenticator) Provider() string {
	return "trae"
}

// RefreshLead returns the duration before token expiry when refresh should occur.
func (TraeAuthenticator) RefreshLead() *time.Duration {
	return &traeRefreshLead
}

// Login initiates the Trae OAuth2 authorization code flow authentication.
func (a TraeAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	authSvc := trae.NewTraeAuth(cfg)

	state := trae.GenerateState()
	port := opts.CallbackPort
	if port <= 0 {
		port = trae.DefaultCallbackPort
	}

	authURL, redirectURI, codeVerifier, err := authSvc.AuthorizationURL(state, port)
	if err != nil {
		return nil, fmt.Errorf("trae: failed to generate authorization URL: %w", err)
	}

	fmt.Printf("\nTo authenticate with Trae, please visit:\n%s\n\n", authURL)
	fmt.Printf("Callback URL: %s\n", redirectURI)

	// Try to open the browser automatically
	if !opts.NoBrowser {
		if browser.IsAvailable() {
			if errOpen := browser.OpenURL(authURL); errOpen != nil {
				log.Warnf("Failed to open browser automatically: %v", errOpen)
			} else {
				fmt.Println("Browser opened automatically.")
			}
		}
	}

	fmt.Println("Waiting for authorization callback...")
	fmt.Println("(If the browser does not open automatically, please open the URL manually)")

	// Wait for user authorization via callback
	tokenData, err := waitForCallback(ctx, redirectURI, codeVerifier, authSvc, opts.Prompt)
	if err != nil {
		return nil, fmt.Errorf("trae: %w", err)
	}

	fmt.Printf("Authentication successful for %s\n", tokenData.Email)

	fileName := trae.CredentialFileName(tokenData.Email)
	metadata := map[string]any{
		"type":          "trae",
		"access_token":  tokenData.AccessToken,
		"refresh_token": tokenData.RefreshToken,
		"token_type":    tokenData.TokenType,
		"scope":         tokenData.Scope,
		"email":         tokenData.Email,
		"user_id":       tokenData.UserID,
		"expires_at":    tokenData.ExpiresAt,
	}

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Label:    tokenData.Email,
		Storage:  authSvc.CreateTokenStorage(tokenData),
		Metadata: metadata,
	}, nil
}

// waitForCallback starts a local HTTP server to receive the OAuth callback and waits for it.
func waitForCallback(ctx context.Context, redirectURI, codeVerifier string, authSvc *trae.TraeAuth, promptFn func(string) (string, error)) (*trae.TokenData, error) {
	callbackCh := make(chan *trae.TokenData, 1)
	errCh := make(chan error, 1)

	// Extract port from redirect URI
	portStr := strings.Split(redirectURI, ":")[2]
	portStr = strings.Split(portStr, "/")[0]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	mux := http.NewServeMux()
	server := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			desc := r.URL.Query().Get("error_description")
			errCh <- fmt.Errorf("trae: authorization denied: %s - %s", errParam, desc)
			http.ServeHTTP(w, r, http.Error(w, fmt.Sprintf("Authorization failed: %s", desc), http.StatusBadRequest))
			return
		}

		if code == "" {
			errCh <- fmt.Errorf("trae: no authorization code in callback")
			http.ServeHTTP(w, r, http.Error(w, "No authorization code", http.StatusBadRequest))
			return
		}

		// Exchange code for tokens
		tokenData, err := authSvc.ExchangeCodeForTokens(r.Context(), code, redirectURI, codeVerifier)
		if err != nil {
			errCh <- fmt.Errorf("trae: failed to exchange code: %w", err)
			http.ServeHTTP(w, r, http.Error(w, fmt.Sprintf("Failed to exchange code: %v", err), http.StatusInternalServerError))
			return
		}

		callbackCh <- tokenData

		// Show success page
		html := `<!DOCTYPE html>
<html>
<head><title>Trae Authentication Successful</title></head>
<body>
<h1>Authentication Successful!</h1>
<p>You can close this window and return to the terminal.</p>
<script>window.close();</script>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server failed: %w", err)
		}
	}()

	// Wait for callback or timeout (15 minutes)
	timeout := time.After(15 * time.Minute)

	select {
	case tokenData := <-callbackCh:
		_ = server.Close()
		return tokenData, nil
	case err := <-errCh:
		_ = server.Close()
		return nil, err
	case <-timeout:
		_ = server.Close()
		return nil, fmt.Errorf("trae: authentication timed out after 15 minutes")
	case <-ctx.Done():
		_ = server.Close()
		return nil, fmt.Errorf("trae: authentication cancelled")
	}
}
