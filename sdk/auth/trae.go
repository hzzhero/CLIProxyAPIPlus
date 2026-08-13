package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// TraeCNAuthenticator implements the browser OAuth polling flow for Trae CN.
type TraeCNAuthenticator struct{}

// NewTraeCNAuthenticator constructs a new Trae CN authenticator.
func NewTraeCNAuthenticator() Authenticator {
	return &TraeCNAuthenticator{}
}

// Provider returns the provider key for trae-cn.
func (TraeCNAuthenticator) Provider() string {
	return "trae-cn"
}

// traeCNRefreshLead is the duration before token expiry when a refresh should be attempted.
// Trae CN JWT tokens have ~14 day lifetime; refresh 24 hours before expiry.
var traeCNRefreshLead = 24 * time.Hour

// RefreshLead returns how soon before expiry a refresh should be attempted.
func (TraeCNAuthenticator) RefreshLead() *time.Duration {
	return &traeCNRefreshLead
}

// Login initiates the browser OAuth flow for Trae CN.
// It constructs the authorization URL, opens it in the browser, polls for
// the refresh token, then exchanges it for a JWT.
func (a TraeCNAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("trae-cn: configuration is required")
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	authSvc := trae.NewTraeAuth(cfg)

	authState, err := authSvc.FetchAuthState(ctx)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: failed to fetch auth state: %w", err)
	}

	fmt.Printf("\nPlease open the following URL in your browser to login:\n\n  %s\n\n", authState.AuthURL)
	fmt.Println("Waiting for authorization...")

	if !opts.NoBrowser {
		if browser.IsAvailable() {
			if errOpen := browser.OpenURL(authState.AuthURL); errOpen != nil {
				log.Debugf("trae-cn: failed to open browser: %v", errOpen)
			}
		}
	}

	refreshToken, err := authSvc.PollForRefreshToken(ctx, authState.ClientID)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: %s: %w", trae.GetUserFriendlyMessage(err), err)
	}

	storage, err := authSvc.ExchangeToken(ctx, authState.ClientID, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: %s: %w", trae.GetUserFriendlyMessage(err), err)
	}

	fmt.Printf("\nSuccessfully logged in! (User ID: %s)\n", storage.UserID)

	authID := fmt.Sprintf("trae-cn-%s.json", storage.UserID)

	label := storage.UserID
	if label == "" {
		label = "trae-cn-user"
	}

	return &coreauth.Auth{
		ID:       authID,
		Provider: a.Provider(),
		FileName: authID,
		Label:    label,
		Storage:  storage,
		Metadata: map[string]any{
			"token":         storage.Token,
			"refresh_token": storage.RefreshToken,
			"user_id":       storage.UserID,
			"client_id":     storage.ClientID,
			"web_id":        storage.WebID,
			"expires_at":    storage.TokenExpireAt.UTC().Format(time.RFC3339),
		},
	}, nil
}
