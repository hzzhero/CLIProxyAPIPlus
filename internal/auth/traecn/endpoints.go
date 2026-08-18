package traecn

// Endpoints for Trae CN (Chinese version).
// Domains are inferred from reverse-engineering of the Trae IDE;
// verify via packet capture if upstream endpoints change.
const (
	// ClientID is the Trae IDE public client identifier (no secret; ClientSecret is "-").
	ClientID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"

	// DefaultCallbackPort is the port used for the local OAuth callback server.
	DefaultCallbackPort = 8021

	// AuthorizeURL is the base URL for the authorization page.
	AuthorizeURL = "https://www.trae.com.cn"

	// AuthBase is the base URL for authentication API calls (GetRefreshToken).
	AuthBase = "https://www.trae.com.cn"

	// APIBase is the base URL for token exchange and model API calls.
	// CN ExchangeToken node; verify via packet capture.
	APIBase = "https://api-cn-central.trae.com.cn"

	// ModelAPIBase is the base URL for model list and chat endpoints.
	ModelAPIBase = "https://trae-api-cn.mchost.guru"

	// GetRefreshTokenURL requests a refresh token after browser authorization.
	GetRefreshTokenURL = AuthBase + "/cloudide/api/v3/trae/oauth/GetRefreshToken"

	// ExchangeTokenURL exchanges a refresh token for an IDE access token.
	ExchangeTokenURL = APIBase + "/cloudide/api/v3/trae/oauth/ExchangeToken"

	// ModelListURL fetches available model names.
	ModelListURL = ModelAPIBase + "/api/ide/v1/model_list"

	// ChatURL is the SSE chat endpoint.
	ChatURL = ModelAPIBase + "/api/ide/v1/chat"
)
