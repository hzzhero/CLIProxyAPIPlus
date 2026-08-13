# Plan: Add Trae CN Provider Support

## Summary

Add `trae-cn` as a new provider in CLIProxyAPIPlus, enabling users to access AI models through Trae CN's SOLO agent API using their Trae CN account credits. The implementation follows the same architectural pattern as CodeBuddy (OAuth login + custom executor + model registry), but targets Trae's SOLO agent API (session-based, JWT auth) instead of an OpenAI-compatible endpoint.

## Current State Analysis

- **CodeBuddy** is the closest reference implementation: browser OAuth polling flow + OpenAI-compatible executor + static models. Files span `internal/auth/codebuddy/`, `sdk/auth/codebuddy.go`, `internal/runtime/executor/codebuddy_executor.go`, `internal/registry/codebuddy_models.go`, `internal/cmd/codebuddy_login.go`.
- **Qoder/QoderCN** shows the CN variant pattern: same executor with different endpoints, PAT-based login for CN.
- **No Trae provider** exists anywhere in the codebase. The `.trae/` directory is IDE config, not source code.
- **Registration points** are consistent: `service.go` (executor + models switch), `auth_manager.go` (authenticator), `refresh_registry.go` (refresh lead), `main.go` (CLI flag), `logger_plugin.go` (usage logging).

## Trae SOLO Agent API (Reverse-Engineered)

Reference: [OmniRoute](https://github.com/diegosouzapw/OmniRoute) `TraeExecutor` + [trae-api](https://github.com/A-23187/trae-api) auth.js script.

### API Endpoints

| Purpose | International | CN (assumed, needs verification) |
|---|---|---|
| SOLO API base | `https://core-normal.trae.ai/api/remote/v1` | `https://core-normal.trae.com.cn/api/remote/v1` |
| OAuth host | `https://api-us-east.trae.ai` | `https://api-cn.trae.com.cn` |
| Authorization page | `https://www.trae.ai/authorization` | `https://www.trae.com.cn/authorization` |
| GetRefreshToken | `POST {oauth_host}/cloudide/api/v3/trae/oauth/GetRefreshToken` | same path, CN host |
| ExchangeToken | `POST {oauth_host}/cloudide/api/v3/trae/oauth/ExchangeToken` | same path, CN host |

### Auth Flow

1. **Authorization**: Open browser to `{auth_page}?client_id={client_id}`. User logs in.
2. **GetRefreshToken**: `POST {oauth_host}/cloudide/api/v3/trae/oauth/GetRefreshToken` with body `{"clientID": "{client_id}"}`. Returns `{Result: {RefreshToken: "..."}}`.
3. **ExchangeToken**: `POST {oauth_host}/cloudide/api/v3/trae/oauth/ExchangeToken` with body `{"ClientID": "{client_id}", "RefreshToken": "{refresh_token}", "ClientSecret": "-", "UserID": ""}`. Returns:
   ```json
   {
     "ResponseMetadata": {"Error": null},
     "Result": {
       "Token": "JWT...",
       "RefreshToken": "...",
       "TokenExpireAt": "...",
       "RefreshExpireAt": "...",
       "TokenExpireDuration": 1209600,
       "UserID": "...",
       "TenantID": "..."
     }
   }
   ```
4. **JWT lifetime**: ~14 days. **RefreshToken lifetime**: ~7 months.
5. **Client ID**: `en1oxy7wnw8j9n` (international, from OmniRoute). CN client_id may differ - needs verification. Will be configurable via env var `TRAE_CN_CLIENT_ID`.

### Chat API (SOLO Agent)

1. **Create session**: `POST {base}/chat_sessions`
   ```json
   {
     "mode": "code",
     "environment_id": "default",
     "initial_message": {
       "chat_session_id": "",
       "content": [],
       "query": "[{\"type\":\"text\",\"data\":{\"content\":\"user input\"}}]",
       "model_name": "auto",
       "agent_type": "solo_agent_remote",
       "model_selection_strategy": "auto",
       "common_params": "{...}"
     },
     "env": "remote",
     "auto_create_project": false,
     "origin": "web"
   }
   ```
   Response: `{code: 0, data: {chat_session_id: "...", message_id: "..."}}`

2. **Stream events**: `GET {base}/chat_sessions/{sessionId}/events?reply_to_message_id={messageId}`
   SSE events:
   - `plan_item`: `{id: "...", thought: "cumulative text"}` - assistant output (cumulative per plan-item id)
   - `token_usage`: `{prompt_tokens, completion_tokens, total_tokens}`
   - `done`: completion signal
   - `error`: `{code, message}`

3. **Headers**:
   ```
   Authorization: Cloud-IDE-JWT {jwt}
   Content-Type: application/json
   X-Trae-Client-Type: web
   X-Preferenced-Language: zh-cn
   x-user-region: CN
   Referer: https://solo.trae.com.cn/
   User-Agent: Mozilla/5.0 ...
   ```

4. **Common params** (JSON string in initial_message):
   ```json
   {
     "language": "zh-cn",
     "app_language": "zh-cn",
     "quality": "stable",
     "app_version": "1.0.0.1229",
     "web_id": "",
     "user_identity": "Free",
     "is_freshman": "0",
     "biz_user_id": "",
     "user_unique_id": "",
     "scope": "marscode-cn",
     "tenant": "marscode",
     "region": "CN",
     "aiRegion": "CN",
     "is_privacy_mode": 0,
     "privacy_mode": "off",
     "solo_chat_mode": "code"
   }
   ```

### Models (from OmniRoute, CN availability may differ)

| Model ID | Display Name |
|---|---|
| `auto` | Auto (server picks) |
| `work` | Work (fast auto) |
| `gemini-3.1-pro` | Gemini 3.1 Pro |
| `gemini-3-flash-solo` | Gemini 3 Flash |
| `minimax-m3` | MiniMax M3 |
| `minimax-m2.7` | MiniMax M2.7 |
| `kimi-k2.5` | Kimi K2.5 |
| `gpt-5.4` | GPT 5.4 |
| `gpt-5.2` | GPT 5.2 |

## Proposed Changes

### New Files

#### 1. `internal/auth/trae/trae_auth.go`
Trae OAuth and token management logic.

- **`TraeAuth` struct**: httpClient, cfg, oauthHost, apiBase, authPageURL, clientID
- **`NewTraeAuth(cfg)`**: Constructor with CN defaults (env var overrides for `TRAE_CN_OAUTH_HOST`, `TRAE_CN_API_BASE`, `TRAE_CN_CLIENT_ID`, `TRAE_CN_AUTH_PAGE`)
- **`FetchAuthState(ctx)`**: Generates a random device_id/machine_id if needed, constructs the authorization URL `{authPageURL}?client_id={client_id}`
- **`PollForRefreshToken(ctx, clientID)`**: Polls `POST {oauthHost}/cloudide/api/v3/trae/oauth/GetRefreshToken` with `{"clientID": clientID}` every 5 seconds for up to 5 minutes. Returns refresh token when available.
- **`ExchangeToken(ctx, clientID, refreshToken)`**: Calls `POST {oauthHost}/cloudide/api/v3/trae/oauth/ExchangeToken` with `{ClientID, RefreshToken, ClientSecret: "-", UserID: ""}`. Returns JWT + new refresh token + user info.
- **`RefreshToken(ctx, clientID, refreshToken)`**: Same as ExchangeToken - re-exchanges refresh token for new JWT.
- **`FetchModels(ctx, jwt)`**: Calls `GET {apiBase}/models` to fetch available models dynamically.

Constants:
```go
const (
    DefaultOAuthHost    = "https://api-cn.trae.com.cn"
    DefaultAPIBase      = "https://core-normal.trae.com.cn/api/remote/v1"
    DefaultAuthPage     = "https://www.trae.com.cn/authorization"
    DefaultClientID     = "en1oxy7wnw8j9n" // may need CN-specific value
    pollInterval        = 5 * time.Second
    maxPollDuration     = 5 * time.Minute
)
```

#### 2. `internal/auth/trae/token.go`
Token storage struct for persistence.

```go
type TraeTokenStorage struct {
    Token            string    `json:"token"`             // Cloud-IDE-JWT
    RefreshToken     string    `json:"refresh_token"`
    UserID           string    `json:"user_id"`
    TenantID         string    `json:"tenant_id"`
    ClientID         string    `json:"client_id"`
    TokenExpireAt    time.Time `json:"token_expire_at"`
    RefreshExpireAt  time.Time `json:"refresh_expire_at"`
    Type             string    `json:"type"`              // "trae-cn"
    // Provider-specific data for common_params
    WebID            string    `json:"web_id"`
    BizUserID        string    `json:"biz_user_id"`
    UserUniqueID     string    `json:"user_unique_id"`
    Scope            string    `json:"scope"`             // "marscode-cn"
    Tenant           string    `json:"tenant"`            // "marscode"
    Region           string    `json:"region"`            // "CN"
    AIRegion         string    `json:"ai_region"`         // "CN"
    UserIdentity     string    `json:"user_identity"`     // "Free"
}
```
- **`SaveTokenToFile()`**: Persists to `auths/trae-cn-{userID}.json`

#### 3. `internal/auth/trae/errors.go`
Sentinel errors and user-friendly messages.

```go
var (
    ErrPollingTimeout    = errors.New("trae: polling timed out")
    ErrAccessDenied      = errors.New("trae: access denied")
    ErrTokenFetchFailed  = errors.New("trae: token fetch failed")
    ErrRefreshTokenInvalid = errors.New("trae: refresh token invalid")
)
```

#### 4. `internal/runtime/executor/trae_executor.go`
SOLO agent executor with custom request/response translation.

- **`TraeExecutor` struct**: cfg, providerKey ("trae-cn")
- **`NewTraeCNExecutor(cfg)`**: Constructor
- **`Identifier()`**: Returns `"trae-cn"`
- **`Execute(ctx, auth, req, opts)`**: Non-streaming. Creates session, streams events to completion, accumulates text, returns as OpenAI chat.completion (translated to source format).
- **`ExecuteStream(ctx, auth, req, opts)`**: Streaming. Creates session, streams events, converts `plan_item` cumulative text to OpenAI delta chunks, emits usage on `token_usage`, emits finish on `done`.
- **`Refresh(ctx, auth)`**: Calls `TraeAuth.RefreshToken()` with stored refresh token, updates auth metadata.
- **`PrepareRequest(req, auth)`**: Sets headers (Authorization: Cloud-IDE-JWT, X-Trae-Client-Type, etc.)
- **`HttpRequest(ctx, auth, req)`**: Standard interface method.
- **`CountTokens(...)`**: Returns 0 (not supported by Trae API).

Key translation logic:
- **`flattenQuery(messages)`**: Converts OpenAI messages to Trae's query format. Flattens all messages into a single JSON-encoded string of text blocks: `[{"type":"text","data":{"content":"..."}}]`. System messages prefixed with `[System]`, assistant messages prefixed with `[Assistant]`.
- **`buildCommonParams(storage)`**: Builds the common_params JSON string from stored provider-specific data.
- **`createSession(ctx, headers, query, model, storage)`**: POST /chat_sessions, returns sessionId + messageId.
- **`streamEvents(ctx, headers, sessionId, messageId, onEvent)`**: GET /chat_sessions/{sessionId}/events, parses SSE, invokes callback per event.
- **`renderNewText(data, state)`**: Handles cumulative `thought` field in `plan_item` events - tracks per-id thoughts and emits only new text.

#### 5. `internal/registry/trae_models.go`
Static fallback model list.

```go
func GetTraeCNModels() []registry.ModelInfo {
    // Returns models with ID prefixed "trae-cn/", OwnedBy "trae-cn", Type "trae-cn"
    // Models: auto, work, gemini-3.1-pro, gemini-3-flash-solo, minimax-m3, minimax-m2.7, kimi-k2.5, gpt-5.4, gpt-5.2
}
```

#### 6. `sdk/auth/trae.go`
SDK authenticator implementing the `Authenticator` interface.

- **`TraeCNAuthenticator` struct**
- **`Provider()`**: Returns `"trae-cn"`
- **`RefreshLead()`**: Returns 24h (JWT ~14 days)
- **`Login(ctx, cfg, opts)`**:
  1. Create `TraeAuth` instance
  2. Call `FetchAuthState()` to get authorization URL
  3. Print URL, open browser (unless `--no-browser`)
  4. Call `PollForRefreshToken()` until available
  5. Call `ExchangeToken()` to get JWT + user info
  6. Build `TraeTokenStorage` and save to file
  7. Return `*coreauth.Auth` with provider "trae-cn", metadata containing JWT, refresh_token, user_id, and provider-specific data

#### 7. `internal/cmd/trae_login.go`
CLI login command entrypoint.

```go
func DoTraeCNLogin(cfg *config.Config, options *LoginOptions) {
    // Same pattern as DoCodeBuddyLogin:
    // manager := newAuthManager()
    // manager.Login(ctx, "trae-cn", cfg, authOpts)
}
```

### Files to Modify

#### 1. `sdk/cliproxy/service.go`

**Executor registration** (after line 1136, the `qoder-cn` case):
```go
case "trae-cn":
    s.coreManager.RegisterExecutor(executor.NewTraeCNExecutor(s.cfg))
```

**Model registration** (after line 2116, the `qoder-cn` case):
```go
case "trae-cn":
    models = registry.GetTraeCNModels()
    models = applyExcludedModels(models, excluded)
```

#### 2. `internal/cmd/auth_manager.go`

Add `sdkAuth.NewTraeCNAuthenticator()` to the manager constructor (after line 29):
```go
sdkAuth.NewQoderCNAuthenticator(),
sdkAuth.NewTraeCNAuthenticator(),  // <-- add
```

#### 3. `sdk/auth/refresh_registry.go`

Add after line 21:
```go
registerRefreshLead("trae-cn", func() Authenticator { return NewTraeCNAuthenticator() })
```

#### 4. `cmd/server/main.go`

**Struct field** (after line 109):
```go
traeCNLogin bool
```

**isOneShotCommandMode** (after line 137):
```go
opts.traeCNLogin
```

**Variable declaration** (after line 174):
```go
var traeCNLogin bool
```

**Flag registration** (after line 218):
```go
flag.BoolVar(&traeCNLogin, "trae-cn-login", false, "Login to Trae (CN) using browser OAuth flow")
```

**commandModeOptions construction** (after line 730):
```go
traeCNLogin: traeCNLogin,
```

**Login handler** (after line 881, the qoderCNLogin handler):
```go
} else if traeCNLogin {
    cmd.DoTraeCNLogin(cfg, options)
```

#### 5. `internal/usage/logger_plugin.go`

Add `"trae-cn"` to the known providers list (line 633):
```go
"gitlab", "cursor", "kiro", "kilo", "kimi", "iflow", "codebuddy", "trae-cn", "local":
```

## Assumptions & Decisions

1. **CN endpoints are inferred** from the international URL patterns. The exact CN SOLO API base URL, OAuth host, and client_id need verification. All endpoints are configurable via environment variables (`TRAE_CN_OAUTH_HOST`, `TRAE_CN_API_BASE`, `TRAE_CN_CLIENT_ID`, `TRAE_CN_AUTH_PAGE`).

2. **OAuth flow uses polling** (like CodeBuddy): After opening the browser, we poll `GetRefreshToken` until a refresh token is available. This assumes the endpoint returns an error/pending status before the user authorizes. If it doesn't support polling, we may need to use a callback server approach instead.

3. **No translator directory needed**: The Trae SOLO API uses a completely custom format. Translation is handled directly in the executor (flatten messages to query string, parse SSE events to OpenAI chunks). This is different from CodeBuddy (which reuses the OpenAI translator) but necessary because Trae's API is not OpenAI-compatible.

4. **Model list is static** initially. Dynamic model fetching (`GET /models`) can be added later as an enhancement. The static list is based on OmniRoute's Trae provider configuration.

5. **Provider name is `trae-cn`** (not `trae`), following the `qoder-cn` pattern. This leaves room for a future `trae` (international) provider.

6. **No thinking/reasoning support** initially. The SOLO API returns `plan_item` events with cumulative `thought` text. Reasoning content extraction can be added later if the API supports it.

7. **No tool/function calling support** initially. The SOLO agent API is an agent API, not a raw LLM API. Tool calling would need separate handling.

## Verification Steps

1. **Compile check**: `go build -o test-output ./cmd/server && rm test-output`
2. **Format**: `gofmt -w .`
3. **Tests**: `go test ./internal/auth/trae/... ./internal/runtime/executor/... ./internal/registry/...`
4. **Manual login test**: `go run ./cmd/server --trae-cn-login` (requires verifying CN endpoints)
5. **Model list**: Verify `trae-cn/` prefixed models appear in `/v1/models`
6. **Chat test**: Send a request to `trae-cn/auto` and verify response
