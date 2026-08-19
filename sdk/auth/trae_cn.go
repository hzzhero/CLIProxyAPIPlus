package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// TraeCNAuthenticator implements the Authenticator interface for the
// Trae China deployment (www.trae.cn / api.trae.cn). The flow follows
// the same "authorization code with PKCE + local callback server"
// pattern used by cockpit-tools' Rust implementation.
type TraeCNAuthenticator struct {
	// CallbackPort pins the local HTTP server to a specific port.
	// Zero (default) means the OS assigns a free port.
	CallbackPort int
}

// NewTraeCNAuthenticator returns a Trae CN authenticator with default
// settings.
func NewTraeCNAuthenticator() *TraeCNAuthenticator { return &TraeCNAuthenticator{} }

// Provider returns "trae-cn" — the key under which this authenticator
// is registered in the store registry.
func (a *TraeCNAuthenticator) Provider() string { return "trae-cn" }

// RefreshLead returns how far before the nominal expiry the scheduler
// should attempt a token refresh. Trae access tokens are typically
// long-lived; we surface a 24-hour lead so any nightly/weekly renewal
// has plenty of runway.
func (a *TraeCNAuthenticator) RefreshLead() *time.Duration {
	d := 24 * time.Hour
	return &d
}

// Login performs the full OAuth flow: GetLoginGuidance → PKCE →
// callback server → open browser → wait callback → ExchangeToken →
// GetUserInfo → build & save Auth record.
func (a *TraeCNAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("trae-cn auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	endpoints := trae.EndpointsForProvider(a.Provider())
	authSvc := trae.NewTraeAuth(cfg, endpoints)
	dc := trae.DefaultDeviceContext(endpoints.ClientIDStandard, endpoints)

	// Allow caller metadata to override device context values.
	if opts.Metadata != nil {
		if v := strings.TrimSpace(opts.Metadata["machine_id"]); v != "" {
			dc.MachineID = v
		}
		if v := strings.TrimSpace(opts.Metadata["device_id"]); v != "" {
			dc.DeviceID = v
		}
		if v := strings.TrimSpace(opts.Metadata["x_app_version"]); v != "" {
			dc.XAppVersion = v
		}
		if v := strings.TrimSpace(opts.Metadata["plugin_version"]); v != "" {
			dc.PluginVersion = v
		}
		if v := strings.TrimSpace(opts.Metadata["client_id"]); v != "" {
			dc.ClientID = v
		}
	}
	isSolo := opts.Metadata != nil && strings.EqualFold(opts.Metadata["solo"], "true")

	loginTraceID := trae.RandomLoginTraceID()

	loginHost, err := authSvc.GetLoginGuidance(ctx, loginTraceID)
	if err != nil {
		return nil, fmt.Errorf("trae-cn auth: %w", err)
	}
	log.Debugf("trae-cn auth: login host = %s", loginHost)

	pkce, err := trae.GeneratePKCECodes()
	if err != nil {
		return nil, fmt.Errorf("trae-cn auth: %w", err)
	}

	// Start local callback server
	callbackPort := a.CallbackPort
	if opts.CallbackPort > 0 {
		callbackPort = opts.CallbackPort
	}
	server := trae.NewOAuthServer(callbackPort)
	if err := server.Start(); err != nil {
		return nil, fmt.Errorf("trae-cn auth: callback server failed to start: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if stopErr := server.Stop(stopCtx); stopErr != nil {
			log.Warnf("trae-cn auth: callback server stop error: %v", stopErr)
		}
	}()
	actualPort := server.Port()
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/authorize", actualPort)

	authURL, err := authSvc.BuildAuthURL(loginHost, loginTraceID, callbackURL, pkce.CodeChallenge, dc, isSolo)
	if err != nil {
		return nil, fmt.Errorf("trae-cn auth: failed to build authorization URL: %w", err)
	}

	a.presentAuthURL(authURL, actualPort, opts.NoBrowser)

	callback, err := a.waitForCallback(ctx, opts, server)
	if err != nil {
		return nil, fmt.Errorf("trae-cn auth: %w", err)
	}
	if callback.Error != "" {
		return nil, fmt.Errorf("trae-cn auth: provider returned error %s: %s", callback.Error, callback.ErrorDescription)
	}
	if callback.AuthCode == "" && callback.RefreshToken == "" {
		return nil, fmt.Errorf("trae-cn auth: callback contained neither authCode nor refreshToken")
	}

	// Use callback-provided login host when possible to honor any
	// server-directed regional redirect.
	effectiveHost := loginHost
	if callback.LoginHost != "" {
		effectiveHost = callback.LoginHost
	}

	exchangeRes, err := authSvc.ExchangeToken(ctx, effectiveHost, dc,
		callback.AuthCode, callback.RefreshToken, pkce.CodeVerifier, callbackURL)
	if err != nil {
		// Shortcut: if the callback already handed us a usable
		// CloudIDEToken, skip exchange and mint a response from that.
		if callback.CloudIDEToken != "" {
			log.Warnf("trae-cn auth: ExchangeToken failed (%v); using shortcut CloudIDEToken from callback", err)
			exchangeRes = &trae.ExchangeResult{
				Resp: &trae.ExchangeResponse{
					AccessToken:  callback.CloudIDEToken,
					RefreshToken: callback.RefreshToken,
					TokenType:    "Bearer",
				},
				UsedOrigin: effectiveHost,
			}
		} else {
			return nil, fmt.Errorf("trae-cn auth: %w", err)
		}
	}

	// ------------------------------
	// User info resolution strategy
	// ------------------------------
	// 1. The CN redirect carries a complete userInfo JSON inside the
	//    callback parameters. Use it first — it avoids the extra
	//    GetUserInfo RPC (which needs additional permissions and is the
	//    source of the user's reported "login failed even though auth
	//    code exists" issue).
	// 2. Fallback to GetUserInfo when the callback did not include
	//    userInfo (e.g. older flows or non-CN regions).
	// 3. If GetUserInfo fails we still log the error but continue so
	//    the user's auth can be saved.
	var userInfo *trae.UserInfoResponse
	fromCb := trae.ParseUserInfoFromCallback(callback)
	if fromCb != nil && fromCb.UserID != "" && fromCb.Nickname != "" {
		userInfo = fromCb
		log.Debug("trae-cn auth: using userInfo from callback (skipped GetUserInfo RPC)")
	} else {
		log.Debug("trae-cn auth: callback missing userInfo, fetching via GetUserInfo")
		infoOrigins := []string{exchangeRes.UsedOrigin}
		if effectiveHost != "" && effectiveHost != exchangeRes.UsedOrigin {
			infoOrigins = append([]string{effectiveHost}, infoOrigins...)
		}
		remote, errInfo := authSvc.GetUserInfo(ctx, infoOrigins, exchangeRes.Resp.AccessToken)
		if errInfo != nil {
			log.Debugf("trae-cn auth: GetUserInfo non-fatal failure: %v", errInfo)
			userInfo = &trae.UserInfoResponse{}
		} else {
			userInfo = remote
		}
		// Merge any fields we can salvage from callback userInfo
		// (sometimes GetUserInfo is partial and callback has extras).
		if fromCb != nil {
			if userInfo.UserID == "" {
				userInfo.UserID = fromCb.UserID
			}
			if userInfo.Nickname == "" {
				userInfo.Nickname = fromCb.Nickname
			}
			if userInfo.Email == "" {
				userInfo.Email = fromCb.Email
			}
		}
	}

	return a.buildAuthRecord(dc, endpoints, callback, exchangeRes, userInfo)
}

// presentAuthURL opens the user's browser or prints the URL with
// SSH tunnel hints.
func (a *TraeCNAuthenticator) presentAuthURL(authURL string, port int, noBrowser bool) {
	fmt.Println("Trae CN 授权流程已启动")
	if !noBrowser {
		fmt.Println("正在打开浏览器完成 Trae CN 登录授权……")
		if !browser.IsAvailable() {
			log.Warn("检测不到可用浏览器，请手动打开下方 URL 完成授权")
			util.PrintSSHTunnelInstructions(port)
			fmt.Printf("授权 URL:\n%s\n", authURL)
			return
		}
		if err := browser.OpenURL(authURL); err != nil {
			log.Warnf("自动打开浏览器失败 (%v)；请手动打开下方 URL 完成授权", err)
			util.PrintSSHTunnelInstructions(port)
			fmt.Printf("授权 URL:\n%s\n", authURL)
			return
		}
		fmt.Println("如果浏览器没有自动打开，请手动访问：")
		fmt.Println(authURL)
		return
	}
	util.PrintSSHTunnelInstructions(port)
	fmt.Printf("请打开以下 URL 完成 Trae CN 登录授权：\n%s\n", authURL)
}

// waitForCallback multiplexes the local HTTP callback server with the
// optional manual-paste fallback (if opts.Prompt is set).
func (a *TraeCNAuthenticator) waitForCallback(ctx context.Context, opts *LoginOptions, server *trae.OAuthServer) (*trae.CallbackParams, error) {
	const callbackTimeout = 10 * time.Minute

	callbackCh := make(chan *trae.CallbackParams, 1)
	callbackErrCh := make(chan error, 1)
	go func() {
		cb, err := server.WaitForCallback(callbackTimeout)
		if err != nil {
			callbackErrCh <- err
			return
		}
		callbackCh <- cb
	}()

	var manualPromptC <-chan time.Time
	var manualPromptTimer *time.Timer
	if opts.Prompt != nil {
		manualPromptTimer = time.NewTimer(15 * time.Second)
		manualPromptC = manualPromptTimer.C
		defer manualPromptTimer.Stop()
	}
	var manualInputCh <-chan string
	var manualInputErrCh <-chan error

	for {
		select {
		case cb := <-callbackCh:
			return cb, nil
		case err := <-callbackErrCh:
			if strings.Contains(err.Error(), "timeout") {
				return nil, fmt.Errorf("等待授权回调超时 (%s)，请重新执行登录命令", callbackTimeout)
			}
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-manualPromptC:
			manualPromptC = nil
			if manualPromptTimer != nil {
				manualPromptTimer.Stop()
			}
			// avoid blocking: poll channels once before prompting
			select {
			case cb := <-callbackCh:
				return cb, nil
			case err := <-callbackErrCh:
				return nil, err
			default:
			}
			manualInputCh, manualInputErrCh = misc.AsyncPrompt(
				opts.Prompt,
				"粘贴 Trae CN 授权回调 URL 后按回车继续（或直接回车继续等待）：",
			)
		case input := <-manualInputCh:
			manualInputCh = nil
			manualInputErrCh = nil
			cb := parseManualTraeCallback(input)
			if cb == nil {
				continue
			}
			return cb, nil
		case errManual := <-manualInputErrCh:
			return nil, fmt.Errorf("读取手动粘贴的回调 URL 失败: %w", errManual)
		}
	}
}

// parseManualTraeCallback parses the callback URL a user pastes in
// headless environments. It intentionally accepts a superset of
// inputs: full URLs, bare query strings, fragment strings.
//
// Returns nil for blank input so the wait loop can continue waiting.
//
// NOTE: All extraction semantics are delegated to trae.ParseCallbackFromURL
// (single source of truth). This function only normalizes user input into
// a parseable *url.URL.
func parseManualTraeCallback(input string) *trae.CallbackParams {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	candidate := input
	if !strings.Contains(candidate, "://") {
		if strings.HasPrefix(candidate, "?") || strings.HasPrefix(candidate, "#") {
			candidate = "http://127.0.0.1/" + candidate
		} else if strings.Contains(candidate, "=") {
			candidate = "http://127.0.0.1/?" + candidate
		} else {
			candidate = "http://" + candidate
		}
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return nil
	}
	return trae.ParseCallbackFromURL(u)
}

// buildAuthRecord assembles the final coreauth.Auth from the results
// of each HTTP step.
func (a *TraeCNAuthenticator) buildAuthRecord(
	dc trae.DeviceContext,
	endpoints trae.Endpoints,
	callback *trae.CallbackParams,
	ex *trae.ExchangeResult,
	info *trae.UserInfoResponse,
) (*coreauth.Auth, error) {
	storage := &trae.TraeTokenStorage{
		Provider:     a.Provider(),
		AccessToken:  ex.Resp.AccessToken,
		RefreshToken: ex.Resp.RefreshToken,
		TokenType:    ex.Resp.TokenType,
		ExpiresAt:    ex.Resp.ExpiresAt,
		ClientID:     dc.ClientID,
		LoginHost:    callback.LoginHost,
		LoginRegion:  callback.LoginRegion,
		LoginTraceID: callback.LoginTraceID,
		UserTag:      callback.UserTag,
		Email:        info.Email,
		UserID:       info.UserID,
		Nickname:     info.Nickname,
		// Device identity captured at exchange time so later refresh
		// DeviceProof flows can rebuild the exact DeviceInfo + re-sign
		// with the same P-256 key the server already recorded.
		AccountAPIHost:      ex.UsedOrigin,
		DeviceID:            dc.DeviceID,
		MachineID:           dc.MachineID,
		XAppVersion:         dc.XAppVersion,
		DeviceInfo:          ex.DeviceInfo,
		DevicePrivateKeyPEM: devicePrivateKey(ex.DeviceKeyPair),
		DevicePublicKeyPEM:  devicePublicKey(ex.DeviceKeyPair),
	}

	label := info.Email
	if label == "" {
		label = info.Nickname
	}
	if label == "" {
		label = info.UserID
	}
	if label == "" {
		digest := sha256.Sum256([]byte(ex.Resp.AccessToken))
		label = "trae-cn-" + hex.EncodeToString(digest[:])[:8]
	}

	fileName := credentialFileName(label, a.Provider())

	metadata := map[string]any{
		"email":        info.Email,
		"user_id":      info.UserID,
		"nickname":     info.Nickname,
		"login_host":   callback.LoginHost,
		"login_region": callback.LoginRegion,
		"user_tag":     callback.UserTag,
		"client_id":    dc.ClientID,
	}
	// Expose expires_at for the scheduler
	if ex.Resp.ExpiresAt > 0 {
		metadata["expires_at"] = ex.Resp.ExpiresAt
	}

	fmt.Println("✓ Trae CN 登录成功")
	if info.Email != "" {
		fmt.Printf("  账号: %s\n", info.Email)
	}
	if info.Nickname != "" {
		fmt.Printf("  昵称: %s\n", info.Nickname)
	}
	fmt.Printf("  凭证文件: auths/%s\n", fileName)

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Storage:  storage,
		Metadata: metadata,
		// The trae-cn provider is routed through the generic
		// OpenAICompatExecutor, which resolves its upstream base URL and
		// bearer token from the auth Attributes (base_url / api_key).
		// If base_url is missing the executor aborts every request with
		// "missing provider baseURL", so we must populate it here. The
		// OpenAI-compatible gateway lives under the exchange origin at
		// /v1 (e.g. https://api.trae.com.cn/v1). The bearer token is the
		// OAuth access token; we also mirror it into the x-cloudide-token
		// header for endpoints that accept it.
		compatBaseURL := strings.TrimRight(ex.UsedOrigin, "/") + "/v1"
		accessToken := strings.TrimSpace(ex.Resp.AccessToken)

		Attributes: map[string]string{
			"email":        info.Email,
			"nickname":     info.Nickname,
			"login_region": callback.LoginRegion,
			"region_id":    regionID(endpoints, callback.LoginRegion, ex.UsedOrigin),
			"base_url":     compatBaseURL,
			"api_key":      accessToken,
			"header:x-cloudide-token": accessToken,
		},
	}, nil
}

// credentialFileName turns a human-readable label into a safe JSON
// filename that lives under auths/.
func credentialFileName(label, provider string) string {
	safe := strings.ToLower(label)
	safe = strings.TrimSpace(safe)
	// email-style separator preservation; everything else is dropped.
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "<", "_", ">", "_",
		"|", "_", "?", "_", "*", "_", "\"", "_", " ", "_",
	)
	safe = replacer.Replace(safe)
	if safe == "" {
		safe = provider + "-account"
	}
	// Short hash for disambiguation when two different accounts end up
	// producing the same sanitized label.
	digest := sha256.Sum256([]byte(label + ":" + provider))
	shortTag := hex.EncodeToString(digest[:])[:6]
	return fmt.Sprintf("%s-%s-%s.json", provider, safe, shortTag)
}

// regionID returns a short stable region tag used in Attributes.
func regionID(endpoints trae.Endpoints, loginRegion, origin string) string {
	r := strings.ToLower(strings.TrimSpace(loginRegion))
	if r == "" && strings.Contains(strings.ToLower(origin), ".cn") {
		r = "cn"
	}
	if r == "" {
		if endpoints.IsCN() {
			r = "cn"
		} else {
			r = "sg"
		}
	}
	return r
}

// devicePrivateKey returns the PEM of the P-256 device private key or
// an empty string when the exchange did not capture one (e.g. shortcut
// CloudIDEToken path, or refresh flow).
func devicePrivateKey(kp *trae.DeviceKeyPair) string {
	if kp == nil {
		return ""
	}
	return kp.PrivateKeyPEM
}

// devicePublicKey returns the PEM of the P-256 device public key.
func devicePublicKey(kp *trae.DeviceKeyPair) string {
	if kp == nil {
		return ""
	}
	return kp.PublicKeyPEM
}
