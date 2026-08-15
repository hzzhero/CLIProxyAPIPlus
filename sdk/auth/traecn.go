package auth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// TraeCNAuthenticator implements the OAuth login flow for Trae CN
// (ByteDance AI IDE, China edition) accounts.
type TraeCNAuthenticator struct{}

// NewTraeCNAuthenticator constructs a Trae CN authenticator.
func NewTraeCNAuthenticator() *TraeCNAuthenticator {
	return &TraeCNAuthenticator{}
}

// Provider returns the provider key for the authenticator.
func (a *TraeCNAuthenticator) Provider() string {
	return "trae-cn"
}

// RefreshLead indicates how soon before expiry a refresh should be attempted.
// Trae CN IDE access tokens are short-lived (~1h); refresh 30 minutes early.
// Returning a non-nil lead keeps the conductor refresh loop scheduled.
func (a *TraeCNAuthenticator) RefreshLead() *time.Duration {
	d := 30 * time.Minute
	return &d
}

// Login performs the Trae CN OAuth flow using a local callback server with a
// manual paste fallback (dual-channel wait, mirroring claude.go).
func (a *TraeCNAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	fp := traecn.NewDeviceFingerprint()

	callbackPort := traecn.DefaultCallbackPort
	if opts.CallbackPort > 0 {
		callbackPort = opts.CallbackPort
	}

	authURL := traecn.BuildAuthorizeURL(fp, callbackPort)

	// Start the local callback server. When the port is unavailable the flow
	// degrades to manual paste only: the user copies the final redirect URL
	// out of the browser address bar after login.
	serverStarted := false
	oauthServer := traecn.NewOAuthServer(callbackPort)
	if errStart := oauthServer.Start(); errStart != nil {
		log.Warnf("trae-cn oauth server unavailable (%v); falling back to manual URL paste", errStart)
		oauthServer = nil
	} else {
		serverStarted = true
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if stopErr := oauthServer.Stop(stopCtx); stopErr != nil {
				log.Warnf("trae-cn oauth server stop error: %v", stopErr)
			}
		}()
	}

	if !opts.NoBrowser {
		fmt.Println("Opening browser for Trae CN authentication")
		if !browser.IsAvailable() {
			log.Warn("No browser available; please open the URL manually")
			if serverStarted {
				util.PrintSSHTunnelInstructions(callbackPort)
			}
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
		} else if errOpen := browser.OpenURL(authURL); errOpen != nil {
			log.Warnf("Failed to open browser automatically: %v", errOpen)
			if serverStarted {
				util.PrintSSHTunnelInstructions(callbackPort)
			}
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
		}
	} else {
		if serverStarted {
			util.PrintSSHTunnelInstructions(callbackPort)
		}
		fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
	}

	fmt.Println("Waiting for Trae CN authentication callback...")

	// Cancellable context so the WaitForCallback goroutine below exits as soon
	// as Login returns (manual paste or error) instead of blocking until the
	// 5-minute timeout fires.
	callbackCtx, cancelCallback := context.WithCancel(ctx)
	defer cancelCallback()

	callbackCh := make(chan *traecn.OAuthResult, 1)
	callbackErrCh := make(chan error, 1)

	if serverStarted {
		go func() {
			result, errWait := oauthServer.WaitForCallback(callbackCtx, 5*time.Minute)
			if errWait != nil {
				// Context cancellation is the normal shutdown path once Login
				// has its result (e.g. manual paste); don't surface it.
				if callbackCtx.Err() != nil {
					return
				}
				callbackErrCh <- errWait
				return
			}
			callbackCh <- result
		}()
	}

	var result *traecn.OAuthResult
	var err error
	var manualPromptTimer *time.Timer
	var manualPromptC <-chan time.Time
	if opts.Prompt != nil {
		manualPromptTimer = time.NewTimer(15 * time.Second)
		manualPromptC = manualPromptTimer.C
		defer manualPromptTimer.Stop()
	}

	var manualInputCh <-chan string
	var manualInputErrCh <-chan error

waitForCallback:
	for {
		select {
		case result = <-callbackCh:
			break waitForCallback
		case err = <-callbackErrCh:
			return nil, fmt.Errorf("trae-cn auth: callback wait failed: %w", err)
		case <-ctx.Done():
			return nil, fmt.Errorf("trae-cn auth: login canceled: %w", ctx.Err())
		case <-manualPromptC:
			manualPromptC = nil
			if manualPromptTimer != nil {
				manualPromptTimer.Stop()
			}
			select {
			case result = <-callbackCh:
				break waitForCallback
			case err = <-callbackErrCh:
				return nil, fmt.Errorf("trae-cn auth: callback wait failed: %w", err)
			default:
			}
			manualInputCh, manualInputErrCh = misc.AsyncPrompt(opts.Prompt, "Paste the login success URL here: ")
			continue
		case input := <-manualInputCh:
			manualInputCh = nil
			manualInputErrCh = nil
			params, errParse := traecn.ParseCallbackURL(input)
			if errParse != nil {
				return nil, errParse
			}
			result = &traecn.OAuthResult{Params: params}
			break waitForCallback
		case errManual := <-manualInputErrCh:
			return nil, errManual
		}
	}

	if result == nil {
		return nil, fmt.Errorf("trae-cn auth: no callback result received")
	}
	if result.Error != nil {
		return nil, fmt.Errorf("trae-cn auth: provider returned error: %w", result.Error)
	}

	params := result.Params

	// Resolve the credential chain from the callback parameters: a direct
	// IDE/access token wins; otherwise exchange the refresh token.
	authSvc := traecn.NewTraeCNAuth(cfg)
	td := &traecn.TokenData{}
	if direct := firstNonEmptyParam(params, "token", "access_token", "ide_token"); direct != "" {
		td.AccessToken = direct
		td.RefreshToken = strings.TrimSpace(params["refresh_token"])
		td.UserID = strings.TrimSpace(params["user_id"])
	} else if refreshToken := strings.TrimSpace(params["refresh_token"]); refreshToken != "" {
		var errExchange error
		td, errExchange = authSvc.ExchangeToken(ctx, traecn.ClientID, refreshToken)
		if errExchange != nil {
			return nil, fmt.Errorf("trae-cn authentication failed: %w", errExchange)
		}
	} else {
		return nil, fmt.Errorf("trae-cn auth: callback carried neither access token nor refresh token (params: %s)", strings.Join(paramKeys(params), ", "))
	}

	// Compute the expiry timestamp (ms epoch) from ExpiresIn when known.
	var expireMs int64
	if td.ExpiresIn > 0 {
		expireMs = time.Now().UnixMilli() + td.ExpiresIn*1000
	}

	// Resolve email/label preference order: opts.Metadata["email"] ->
	// opts.Metadata["alias"] -> td.UserID -> timestamp fallback.
	email := ""
	if opts.Metadata != nil {
		email = strings.TrimSpace(opts.Metadata["email"])
		if email == "" {
			email = strings.TrimSpace(opts.Metadata["alias"])
		}
	}
	if email == "" {
		email = strings.TrimSpace(td.UserID)
	}
	if email == "" {
		email = fmt.Sprintf("user-%d", time.Now().UnixMilli())
	}
	label := email

	storage := traecn.CreateTokenStorage(td, fp, email, expireMs)

	fileName := fmt.Sprintf("trae-cn-%s.json", label)
	// expires_at must be RFC3339 so Auth.ExpirationTime() (which reads the
	// metadata map, not the storage struct) can schedule the next refresh.
	metadata := map[string]any{
		"email":   email,
		"user_id": td.UserID,
	}
	if expireMs > 0 {
		metadata["expires_at"] = time.UnixMilli(expireMs).UTC().Format(time.RFC3339)
	}

	fmt.Println("Trae CN authentication successful")

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		Label:    label,
		FileName: fileName,
		Storage:  storage,
		Metadata: metadata,
	}, nil
}

// firstNonEmptyParam returns the first non-empty trimmed value among the
// given keys, or "" when none is set.
func firstNonEmptyParam(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

// paramKeys returns the sorted key list of the callback parameter map. It is
// used in error messages to help diagnose the real callback format without
// leaking parameter values (which may contain tokens).
func paramKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
