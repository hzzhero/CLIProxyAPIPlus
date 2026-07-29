package qoder

import "strings"

// Endpoints groups the region-specific URLs for a Qoder deployment.
// The international (qoder.sh) and China (qoder.com.cn) editions share the
// same COSY signing scheme and API paths; only the host names differ, plus
// the CN edition adds a PAT→job token exchange endpoint.
type Endpoints struct {
	// ChatBase is the inference host used for chat / model list /
	// other algo-prefixed endpoints.
	ChatBase string
	// OpenAPIBase is the base URL for OpenAPI (userinfo / usage / jobToken).
	OpenAPIBase string
	// CenterBase is the base URL for the center API (refresh_token).
	CenterBase string
	// LoginURL is the device-flow login page (CN leaves it empty: PAT only).
	LoginURL string
	// OAuthTokenEndpoint is the device-flow poll URL (CN leaves it empty).
	OAuthTokenEndpoint string
	// RefreshTokenEndpoint is the refresh_token URL.
	RefreshTokenEndpoint string
	// UserInfoEndpoint is the /api/v1/userinfo URL.
	UserInfoEndpoint string
	// JobTokenExchangeURL is the PAT→job token exchange URL (CN only).
	JobTokenExchangeURL string
	// UsageURL is the /api/v2/quota/usage URL.
	UsageURL string
	// ChatURL is the full streaming chat endpoint URL (no Encode=1).
	ChatURL string
	// ChatURLEncoded is the chat URL with Encode=1 (WAF bypass body encoding).
	ChatURLEncoded string
	// ModelListURL is the /algo/api/v2/model/list URL.
	ModelListURL string
}

// GlobalEndpoints holds the international (qoder.sh) URLs. It mirrors the
// existing Qoder* consts so those consts remain the single source of truth
// (and continue to be referenced by tests / replay tooling).
var GlobalEndpoints = Endpoints{
	ChatBase:             QoderChatBase,
	OpenAPIBase:          QoderOpenAPIBase,
	CenterBase:           QoderCenterBase,
	LoginURL:             QoderLoginURL,
	OAuthTokenEndpoint:   QoderOAuthTokenEndpoint,
	RefreshTokenEndpoint: QoderRefreshTokenEndpoint,
	UserInfoEndpoint:     QoderUserInfoEndpoint,
	UsageURL:             QoderOpenAPIBase + "/api/v2/quota/usage",
	ChatURL:              QoderChatURL,
	ChatURLEncoded:       QoderChatURLEncoded,
	ModelListURL:         QoderModelListURL,
	// JobTokenExchangeURL intentionally empty: the international device-flow
	// provider does not use PAT exchange.
}

// CN endpoint hosts. The China edition routes inference through
// gateway.qoder.com.cn and OpenAPI through openapi.qoder.com.cn; both share
// the same /algo and /api path layout as the international edition.
const (
	cnChatBase    = "https://gateway.qoder.com.cn"
	cnOpenAPIBase = "https://openapi.qoder.com.cn"
	cnCenterBase  = "https://gateway.qoder.com.cn"
)

// CNEndpoints holds the China (qoder.com.cn) URLs. CN does not support the
// browser device flow, so LoginURL / OAuthTokenEndpoint are empty; login is
// PAT-only via JobTokenExchangeURL.
var CNEndpoints = Endpoints{
	ChatBase:             cnChatBase,
	OpenAPIBase:          cnOpenAPIBase,
	CenterBase:           cnCenterBase,
	LoginURL:             "",
	OAuthTokenEndpoint:   "",
	RefreshTokenEndpoint: cnCenterBase + "/algo/api/v3/user/refresh_token",
	UserInfoEndpoint:     cnOpenAPIBase + "/api/v1/userinfo",
	JobTokenExchangeURL:  cnOpenAPIBase + "/api/v1/jobToken/exchange",
	UsageURL:             cnOpenAPIBase + "/api/v2/quota/usage",
	ChatURL:              cnChatBase + "/algo" + QoderSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common",
	ChatURLEncoded:       cnChatBase + "/algo" + QoderSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1",
	ModelListURL:         cnChatBase + "/algo/api/v2/model/list",
}

// EndpointsForProvider returns the endpoint set for the given provider key.
// "qoder-cn" (case-insensitive) selects CNEndpoints; anything else selects
// GlobalEndpoints so unknown / empty values fall back to the international
// edition rather than breaking.
func EndpointsForProvider(provider string) Endpoints {
	if strings.EqualFold(strings.TrimSpace(provider), "qoder-cn") {
		return CNEndpoints
	}
	return GlobalEndpoints
}
