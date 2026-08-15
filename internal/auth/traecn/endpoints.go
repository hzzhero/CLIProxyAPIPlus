// Package traecn provides authentication and token handling for the Trae CN
// (ByteDance AI IDE, China edition) OAuth flow.
package traecn

const (
	// ClientID is the Trae IDE public client identifier. There is no secret;
	// the upstream expects ClientSecret to be the literal "-".
	ClientID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	// DefaultCallbackPort is the default local port for the OAuth callback server.
	DefaultCallbackPort = 8021

	// AuthorizeURL is the browser-facing Trae CN authorization page.
	AuthorizeURL = "https://www.trae.com.cn/authorization"
	// AuthBase is the Trae CN account/auth host.
	AuthBase = "https://www.trae.com.cn"
	// APIBase is the CN ExchangeToken node; verify via packet capture.
	APIBase = "https://api-cn-central.trae.com.cn"
	// ModelAPIBase is the Trae CN model/inference host.
	ModelAPIBase = "https://trae-api-cn.mchost.guru"

	// GetRefreshTokenURL exchanges/refresh credentials against the auth host.
	GetRefreshTokenURL = AuthBase + "/cloudide/api/v3/trae/oauth/GetRefreshToken"
	// ExchangeTokenURL trades a refresh token for a short-lived access token.
	ExchangeTokenURL = APIBase + "/cloudide/api/v3/trae/oauth/ExchangeToken"
	// ModelListURL lists available models.
	ModelListURL = ModelAPIBase + "/api/ide/v1/model_list"
	// ChatURL is the chat/completion endpoint.
	ChatURL = ModelAPIBase + "/api/ide/v1/chat"
)
