package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

// TraeCNAuthenticator implements the OAuth login flow for Trae CN accounts.
type TraeCNAuthenticator struct {
	CallbackPort int
}

// NewTraeCNAuthenticator constructs a Trae CN authenticator with default settings.
func NewTraeCNAuthenticator() *TraeCNAuthenticator {
	return &TraeCNAuthenticator{CallbackPort: traecn.DefaultCallbackPort}
}

func (a *TraeCNAuthenticator) Provider() string {
	return "trae-cn"
}

func (a *TraeCNAuthenticator) RefreshLead() *time.Duration {
	d := 30 * time.Minute
	return &d
}

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

	callbackPort := a.CallbackPort
	if opts.CallbackPort > 0 {
		callbackPort = opts.CallbackPort
	}

	fp := traecn.NewDeviceFingerprint()
	authSvc := traecn.NewTraeCNAuth(cfg)

	authURL := traecn.BuildAuthorizeURL(fp, callbackPort)

	oauthServer := traecn.NewOAuthServer(callbackPort)
	if err := oauthServer.Start(); err != nil {
		if strings.Contains(err.Error(), "already in use") {
			log.Warnf("Trae CN OAuth callback port %d already in use; falling back to manual URL paste", callbackPort)
		} else {
			log.Warnf("Trae CN OAuth server start failed: %v; falling back to manual URL paste", err)
		}
	} else {
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if stopErr := oauthServer.Stop(stopCtx); stopErr != nil {
				log.Warnf("trae-cn oauth server stop error: %v", stopErr)
			}
		}()
	}

	fmt.Printf("Visit the following URL to authenticate with Trae CN:\n%s\n", authURL)

	if !opts.NoBrowser && browser.IsAvailable() {
		if err := browser.OpenURL(authURL); err != nil {
			log.Warnf("Failed to open browser automatically: %v", err)
		}
	}

	fmt.Println("Waiting for Trae CN authentication callback...")

	var params map[string]string
	var loginErr error

	if oauthServer != nil {
		callbackCh := make(chan *traecn.OAuthResult, 1)
		callbackErrCh := make(chan error, 1)

		go func() {
			result, errWait := oauthServer.WaitForCallback(5 * time.Minute)
			if errWait != nil {
				callbackErrCh <- errWait
				return
			}
			callbackCh <- result
		}()

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
			case res := <-callbackCh:
				params = res.Params
				break waitForCallback
			case err := <-callbackErrCh:
				if strings.Contains(err.Error(), "timeout") {
					loginErr = fmt.Errorf("trae-cn: OAuth callback timed out after 5 minutes")
					return nil, loginErr
				}
				loginErr = err
				return nil, loginErr
			case <-manualPromptC:
				manualPromptC = nil
				if manualPromptTimer != nil {
					manualPromptTimer.Stop()
				}
				select {
				case res := <-callbackCh:
					params = res.Params
					break waitForCallback
				case err := <-callbackErrCh:
					if strings.Contains(err.Error(), "timeout") {
						loginErr = fmt.Errorf("trae-cn: OAuth callback timed out after 5 minutes")
						return nil, loginErr
					}
					loginErr = err
					return nil, loginErr
				default:
				}
				manualInputCh, manualInputErrCh = misc.AsyncPrompt(opts.Prompt, "Paste the login success URL here: ")
				continue
			case input := <-manualInputCh:
				manualInputCh = nil
				manualInputErrCh = nil
				parsed, errParse := traecn.ParseCallbackURL(input)
				if errParse != nil {
					return nil, errParse
				}
				params = parsed
				break waitForCallback
			case errManual := <-manualInputErrCh:
				return nil, errManual
			}
		}
	} else {
		// No oauth server available; prompt user directly
		var input string
		if opts.Prompt != nil {
			var err error
			input, err = opts.Prompt("Paste the login success URL here: ")
			if err != nil {
				return nil, err
			}
		} else {
			fmt.Print("Paste the login success URL here: ")
			if _, err := fmt.Scanln(&input); err != nil {
				return nil, fmt.Errorf("failed to read input: %w", err)
			}
		}
		parsed, errParse := traecn.ParseCallbackURL(input)
		if errParse != nil {
			return nil, errParse
		}
		params = parsed
	}

	// Extract token from callback parameters.
	accessToken := traecn.FirstNonEmptyParam(params, "token", "access_token", "ide_token")
	refreshToken := traecn.FirstNonEmptyParam(params, "refresh_token")

	var tokenData *traecn.TokenData
	if accessToken != "" {
		tokenData = &traecn.TokenData{AccessToken: accessToken, RefreshToken: refreshToken}
	} else if refreshToken != "" {
		// Need to exchange refresh token for access token
		rt, err := authSvc.GetRefreshToken(ctx, traecn.ClientID)
		if err != nil {
			return nil, fmt.Errorf("trae-cn: GetRefreshToken failed: %w", err)
		}
		tokenData, err = authSvc.ExchangeToken(ctx, traecn.ClientID, rt)
		if err != nil {
			return nil, fmt.Errorf("trae-cn: ExchangeToken failed: %w", err)
		}
	} else {
		availableKeys := traecn.ParamKeys(params)
		return nil, fmt.Errorf("trae-cn: no token found in callback; available keys: %v", availableKeys)
	}

	// Calculate expiry in ms. ExpiresIn may be seconds or ms depending on endpoint.
	expiresMs := time.Now().UnixMilli() + tokenData.ExpiresIn*1000
	if tokenData.ExpiresIn <= 0 {
		expiresMs = time.Now().Add(720 * time.Hour).UnixMilli() // fallback: 30 days
	}

	// Determine email/label
	email := opts.Metadata["email"]
	if email == "" {
		email = opts.Metadata["alias"]
	}
	if email == "" && tokenData.UserID != "" {
		email = tokenData.UserID
	}
	if email == "" {
		email = fmt.Sprintf("trae-cn-%d", time.Now().Unix())
	}

	label := strings.TrimSpace(email)
	if label == "" {
		label = fmt.Sprintf("user-%d", time.Now().Unix())
	}

	storage := traecn.CreateTokenStorage(tokenData, fp, label, expiresMs)
	fileName := fmt.Sprintf("trae-cn-%s.json", label)
	metadata := map[string]any{
		"email":     label,
		"user_id":   tokenData.UserID,
		"expires_at": time.UnixMilli(expiresMs).UTC().Format(time.RFC3339),
	}

	fmt.Println("Trae CN authentication successful")

	// Save the token file
	authDir := cfg.AuthDir
	if authDir == "" {
		authDir = "~/.cli-proxy-api"
	}
	authDir = os.ExpandEnv(authDir)
	filePath := authDir + "/" + fileName

	if err := storage.SaveTokenToFile(filePath); err != nil {
		log.Warnf("Failed to save token file: %v", err)
	}

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Storage:  storage,
		Metadata: metadata,
	}, nil
}
