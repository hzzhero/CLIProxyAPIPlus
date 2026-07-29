package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qoder"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// QoderAuthenticator implements the device flow login for Qoder accounts.
type QoderAuthenticator struct{}

// NewQoderAuthenticator constructs a Qoder authenticator.
func NewQoderAuthenticator() *QoderAuthenticator {
	return &QoderAuthenticator{}
}

func (a *QoderAuthenticator) Provider() string {
	return "qoder"
}

func (a *QoderAuthenticator) RefreshLead() *time.Duration {
	// Qoder device tokens are long-lived (~30 days), and we don't have
	// a working refresh path (see QoderExecutor.Refresh comment). Use a
	// short non-zero lead so the auto-refresh loop still revisits the
	// auth periodically — but never within the same minute it just ran.
	// Returning nil disables scheduled refresh entirely; we keep a
	// nominal 24h lead so admins can observe through the management API.
	d := 24 * time.Hour
	return &d
}

func (a *QoderAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	authSvc := qoder.NewQoderAuth(cfg)

	// Initiate device flow
	deviceFlow, err := authSvc.InitiateDeviceFlow(ctx)
	if err != nil {
		return nil, fmt.Errorf("qoder device flow initiation failed: %w", err)
	}

	authURL := deviceFlow.VerificationURIComplete

	// Open browser or display URL
	if !opts.NoBrowser {
		fmt.Println("Opening browser for Qoder authentication")
		if !browser.IsAvailable() {
			log.Warn("No browser available; please open the URL manually")
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
		} else if err = browser.OpenURL(authURL); err != nil {
			log.Warnf("Failed to open browser automatically: %v", err)
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
		}
	} else {
		fmt.Printf("Visit the following URL to continue authentication:\n%s\n", authURL)
	}

	fmt.Println("Waiting for Qoder authentication...")

	// Poll for token
	tokenData, err := authSvc.PollForToken(ctx, deviceFlow)
	if err != nil {
		return nil, fmt.Errorf("qoder authentication failed: %w", err)
	}

	// Resolve user info (best effort). FetchUserInfo only needs the access
	// token, so we always attempt it — UserID is informational here.
	tokenStorage := authSvc.CreateTokenStorage(tokenData, deviceFlow.MachineID)
	name, email := authSvc.SaveUserInfo(ctx, tokenData.AccessToken, tokenData.UserID, "", "")

	// Resolve a label for the auth file name. Preference order:
	//   1. email returned by /userinfo
	//   2. opts.Metadata[email|alias] supplied by the caller
	//   3. tokenData.UserID — stable per account, deterministic file name
	//   4. timestamp — last-resort unique fallback so non-interactive
	//      flows (Docker, management API, scripts) never block on a prompt
	//
	// We never prompt: prompting would deadlock callers that have no TTY,
	// and we already have enough information to write a unique file.
	label := strings.TrimSpace(email)
	if label == "" && opts.Metadata != nil {
		label = strings.TrimSpace(opts.Metadata["email"])
		if label == "" {
			label = strings.TrimSpace(opts.Metadata["alias"])
		}
	}
	if label == "" {
		label = strings.TrimSpace(tokenData.UserID)
	}
	if label == "" {
		label = fmt.Sprintf("user-%d", time.Now().UnixMilli())
	}

	tokenStorage.Email = label
	tokenStorage.Name = name

	// Generate file name
	fileName := fmt.Sprintf("qoder-%s.json", label)
	metadata := map[string]any{
		"email":   label,
		"name":    name,
		"user_id": tokenData.UserID,
	}

	fmt.Println("Qoder authentication successful")
	if name != "" {
		fmt.Printf("Logged in as %s <%s>\n", name, label)
	}

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Storage:  tokenStorage,
		Metadata: metadata,
	}, nil
}

// qoderCNPATMetadataKey is the LoginOptions.Metadata key carrying the CN PAT.
const qoderCNPATMetadataKey = "personal_token"

// qoderCNPATEnvKeys are the environment variables consulted for the CN PAT,
// in priority order. Mirrors the pi-provider-qoder convention.
var qoderCNPATEnvKeys = []string{"QODER_CN_PERSONAL_ACCESS_TOKEN", "QODERCN_PERSONAL_ACCESS_TOKEN", "QODER_CN_PAT", "QODERCN_PAT"}

// QoderCNAuthenticator implements PAT-based login for Qoder China accounts.
//
// CN does not support the browser device flow; users obtain a Personal Access
// Token (pt-...) from https://qoder.com.cn/account/integrations and the
// authenticator exchanges it for a short-lived job token (jt-..., ~24h) via
// POST {openapi}/api/v1/jobToken/exchange. The PAT is persisted alongside the
// job token so the scheduler can re-exchange it before expiry
// (see QoderExecutor.Refresh).
type QoderCNAuthenticator struct{}

// NewQoderCNAuthenticator constructs a Qoder CN authenticator.
func NewQoderCNAuthenticator() *QoderCNAuthenticator {
	return &QoderCNAuthenticator{}
}

func (a *QoderCNAuthenticator) Provider() string {
	return "qoder-cn"
}

func (a *QoderCNAuthenticator) RefreshLead() *time.Duration {
	// CN job tokens are short-lived (~24h). Re-exchange 1h before expiry so
	// the conductor-driven QoderExecutor.Refresh has a window to obtain a
	// fresh job token using the persisted PAT.
	d := 1 * time.Hour
	return &d
}

// resolveCNPAT resolves the PAT from (in order): opts.Metadata, environment
// variables, then opts.Prompt. Returns an error when no PAT can be obtained.
func (a *QoderCNAuthenticator) resolveCNPAT(opts *LoginOptions) (string, error) {
	if opts != nil && opts.Metadata != nil {
		if v := strings.TrimSpace(opts.Metadata[qoderCNPATMetadataKey]); v != "" {
			return v, nil
		}
	}
	for _, envKey := range qoderCNPATEnvKeys {
		if raw, ok := os.LookupEnv(envKey); ok {
			if v := strings.TrimSpace(raw); v != "" {
				return v, nil
			}
		}
	}
	if opts != nil && opts.Prompt != nil {
		fmt.Println()
		fmt.Println("Qoder CN uses a Personal Access Token (PAT) for login.")
		fmt.Println("Generate one at https://qoder.com.cn/account/integrations")
		value, err := opts.Prompt("Enter Qoder CN personal access token (pt-...): ")
		if err != nil {
			return "", fmt.Errorf("qoder-cn auth: failed to read PAT: %w", err)
		}
		if v := strings.TrimSpace(value); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("qoder-cn auth: personal access token is required (pass via Prompt, Metadata[%q], or %s)",
		qoderCNPATMetadataKey, qoderCNPATEnvKeys[0])
}

// resolveCNUserInfo fetches the CN account identity (user ID, name, email) from
// /api/v1/userinfo using the freshly-exchanged job token. Best effort: on any
// failure it returns empty strings so login still succeeds and the chat request
// surfaces the signing error (rather than blocking the login flow). Uses
// FetchUserInfoFull because the jobToken/exchange response does not include a
// user_id, and the COSY signing scheme requires one.
func (a *QoderCNAuthenticator) resolveCNUserInfo(ctx context.Context, authSvc *qoder.QoderAuth, accessToken string) (userID, name, email string) {
	uid, n, e, err := authSvc.FetchUserInfoFull(ctx, accessToken)
	if err != nil {
		log.Warnf("qoder-cn: fetch user info failed (login continues with empty user id): %v", err)
		return "", "", ""
	}
	return uid, n, e
}

func (a *QoderCNAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	pat, err := a.resolveCNPAT(opts)
	if err != nil {
		return nil, err
	}

	authSvc := qoder.NewQoderAuthForProvider(cfg, a.Provider())

	tokenData, err := authSvc.ExchangeJobToken(ctx, pat)
	if err != nil {
		return nil, fmt.Errorf("qoder-cn authentication failed: %w", err)
	}

	// The jobToken/exchange response does not include a user_id (unlike the
	// device-flow poll response). Resolve it from /api/v1/userinfo with the
	// freshly-exchanged job token — the COSY signing scheme requires a
	// non-empty UserID, and the management UI / model registry key off it too.
	// Best effort: on failure we proceed with empty values and let the chat
	// request surface the signing error rather than blocking login.
	tokenStorage := authSvc.CreateTokenStorage(tokenData, "")
	userID, name, email := a.resolveCNUserInfo(ctx, authSvc, tokenData.AccessToken)
	tokenStorage.UserID = userID
	tokenStorage.PersonalToken = pat
	tokenStorage.Type = "qoder-cn"

	// Resolve a label for the auth file name. Preference order:
	//   1. email returned by /userinfo
	//   2. opts.Metadata[email|alias] supplied by the caller
	//   3. userID — stable per account, deterministic file name
	//   4. timestamp — last-resort unique fallback so non-interactive
	//      flows (Docker, scripts) never block on a prompt.
	label := strings.TrimSpace(email)
	if label == "" && opts.Metadata != nil {
		label = strings.TrimSpace(opts.Metadata["email"])
		if label == "" {
			label = strings.TrimSpace(opts.Metadata["alias"])
		}
	}
	if label == "" {
		label = strings.TrimSpace(userID)
	}
	if label == "" {
		label = fmt.Sprintf("user-%d", time.Now().UnixMilli())
	}

	tokenStorage.Email = label
	tokenStorage.Name = name

	fileName := fmt.Sprintf("qoder-cn-%s.json", label)
	// expires_at must be RFC3339 so Auth.ExpirationTime() (which reads the
	// metadata map, not the storage struct) can schedule the next refresh.
	metadata := map[string]any{
		"email":      label,
		"name":       name,
		"user_id":    userID,
		"expires_at": time.UnixMilli(tokenData.ExpireTime).UTC().Format(time.RFC3339),
	}

	fmt.Println("Qoder CN authentication successful")
	if name != "" {
		fmt.Printf("Logged in as %s <%s>\n", name, label)
	}

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Storage:  tokenStorage,
		Metadata: metadata,
	}, nil
}
