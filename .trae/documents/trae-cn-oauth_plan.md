# Trae CN OAuth 支持实现计划

## Repository Research

### 参考项目 (cockpit-tools, Rust) 的 Trae CN OAuth 流程

Trae 平台分 4 种：Trae、TraeSolo、TraeCn、TraeSoloCn。CN 版与国际版差异仅在域名：

- **Login Guidance URLs (CN)**：
  - `https://api.trae.cn/cloudide/api/v3/trae/GetLoginGuidance`
  - `https://api.trae.com.cn/cloudide/api/v3/trae/GetLoginGuidance`
  - `https://www.trae.cn/cloudide/api/v3/trae/GetLoginGuidance`
  - 请求体：`{ loginTraceID: "..." }`，响应提取 `Result.LoginHost`
  - 如果获取失败，CN 会 fallback 到默认 `https://www.trae.cn`

- **默认授权域名 (CN)**：`www.trae.cn`（国际版 `www.trae.ai`）

- **Account API origins (CN)**：
  - `https://api.trae.cn`（normal，CN 无 SG/US/USTTP 区域）
  - `https://api.trae.com.cn`
  - 候选 origins 还包含 login_host 本身 + `api.` 前缀

- **Client ID**：
  - 标准版 (Trae/TraeCn)：`ono9krqynydwx5`
  - Solo 版 (TraeSolo/TraeSoloCn)：`en1oxy7wnw8j9n`

- **OAuth 完整流程**：
  1. 生成 `login_trace_id` (UUID)，调用 `GetLoginGuidance` 获取 login_host
  2. 生成 PKCE pair (code_verifier 48 bytes → URL_SAFE_NO_PAD base64; SHA256 → code_challenge)
  3. 启动本地回调 HTTP 服务器，监听随机端口，路径 `/authorize`
  4. 构建 verification_uri（路径 `/authorization`，query 参数含 `login_version=1`, `auth_from=trae|solo`, `login_channel=native_ide`, `plugin_version`, `auth_type=local`, `client_id`, `redirect=0`, `login_trace_id`, `auth_callback_url=http://127.0.0.1:{port}/authorize`, `machine_id`, `device_id`, `x_device_id`, `x_machine_id`, `x_device_brand`, `x_device_type`, `x_os_version`, `x_env`, `x_app_version`, `x_app_type`, `code_challenge`, `code_challenge_method=S256`，Solo 版再加 `hide_saas_login=true`）
  5. 打开浏览器，等待回调（参数：`authCode`/`authCodeInfo`、`refreshToken`、`loginHost`、`loginRegion`、`loginTraceID`、`x-cloudide-token`/`accessToken`、`userTag`）
  6. ExchangeToken：优先走 `{account_origin}/trae/api/v3/oauth/ExchangeToken`，其次 `{origin}/cloudide/api/v3/trae/oauth/ExchangeToken`，请求体含 `AuthCode`, `Code`, `RefreshToken`, `ClientID`, `ClientSecret="-"`, `CodeVerifier`, `RedirectUri`, `DeviceInfo`, `DevicePublicKey`（含 P-256 ECDSA 密钥对）
  7. GetUserInfo：`{origin}/cloudide/api/v3/trae/GetUserInfo` 拿 email/nickname/user_id
  8. 保存账户信息

### 当前项目 (CLIProxyAPIPlus, Go) 的 OAuth 架构

1. **`sdk/auth/*.go`（Authenticator 层）**：每个 provider 一个文件，实现 `Authenticator` 接口（`Provider()`, `Login(ctx, cfg, opts) (*coreauth.Auth, error)`, `RefreshLead() *time.Duration`）。参考：`sdk/auth/codex.go`（回调服务器 + PKCE）、`sdk/auth/codebuddy.go`（纯 polling）、`sdk/auth/qoder.go`（设备流 + CN 变体通过 `EndpointsForProvider`）。
2. **`internal/auth/<provider>/`（辅助包）**：放 provider 特定的 HTTP 调用、token 结构、PKCE、oauth_server。例如 `internal/auth/codex/`、`internal/auth/qoder/`。
3. **`internal/cmd/<provider>_login.go`（命令入口）**：调用 `newAuthManager().Login(ctx, "provider", cfg, authOpts)`，如 `internal/cmd/qoder_cn_login.go`。
4. **`internal/cmd/auth_manager.go`**：`newAuthManager()` 注册所有 Authenticator。
5. **`sdk/auth/refresh_registry.go`**：`init()` 中 `registerRefreshLead("provider", factory)` 注册刷新策略。
6. **`cmd/server/main.go`**：声明 flag（`trae-cn-login bool`）→ 加入 `commandModeOptions` → 在 `isOneShotCommandMode` 中加入 → flag 解析后 dispatch：`else if traeCNLogin { cmd.DoTraeCNLogin(cfg, options) }`。
7. **`internal/misc/oauth.go`**：已提供 `GenerateRandomState()`、`ParseOAuthCallback()`、`AsyncPrompt()` 通用工具。
8. **Token 存储**：通过 `coreauth.Auth{ID, Provider, FileName, Label, Storage, Metadata}` + `sdkAuth.GetTokenStore().Save()` 持久化到 `auths/` 目录。

## Files and Modules

- 新建 `sdk/auth/trae_cn.go`：`TraeCNAuthenticator` 实现 `Authenticator` 接口
- 新建 `internal/auth/trae/endpoints.go`：CN / 国际版 URL 常量与 `EndpointsForProvider()`
- 新建 `internal/auth/trae/pkce.go`：PKCE code_verifier / code_challenge 生成
- 新建 `internal/auth/trae/oauth_server.go`：本地回调 HTTP 服务器（路径 `/authorize`，HTML 成功/失败/待处理页面，超时 10 分钟，手动粘贴回调 URL 回退）
- 新建 `internal/auth/trae/token.go`：`TraeTokenData`、`TraeDeviceContext`、`TraeCallbackResult` 结构
- 新建 `internal/auth/trae/trae_auth.go`：`TraeAuth` 服务类，封装 `GetLoginGuidance`、`BuildAuthURL`、`ExchangeToken`、`GetUserInfo` 四步 HTTP 调用
- 新建 `internal/cmd/trae_cn_login.go`：`DoTraeCNLogin(cfg, options)` 命令入口
- 修改 `internal/cmd/auth_manager.go`：在 `newAuthManager()` 注册 `sdkAuth.NewTraeCNAuthenticator()`
- 修改 `sdk/auth/refresh_registry.go`：`registerRefreshLead("trae-cn", ...)`
- 修改 `cmd/server/main.go`：新增 `trae-cn-login` flag，加入 `commandModeOptions`、`isOneShotCommandMode`，在 dispatch 分支调用 `cmd.DoTraeCNLogin`
- 修改 `cmd/server/main_test.go`：`commandModeOptions` 测试用例新增 `trae-cn login`

## Implementation Steps

1. **创建 endpoint + 常量层**（`internal/auth/trae/endpoints.go`）
   - 定义 CN LoginGuidance 三 URL、默认 auth domain、account origins
   - 定义 `auth_from`、client_id 常量（先支持标准版 Trae CN，Solo CN 可后续扩展 metadata）
   - `EndpointsForProvider(provider string) Endpoints`：识别 `"trae-cn"` 返回 CNEndpoints

2. **创建 PKCE 工具**（`internal/auth/trae/pkce.go`）
   - `GeneratePKCECodes() (verifier, challenge string, err)`：verifier 为 48 字节的 URL-safe base64 no padding，challenge 为 SHA256 后的 URL-safe base64 no padding。
   - 标准 Go 实现：`crypto/rand` → `encoding/base64.RawURLEncoding` → `crypto/sha256`

3. **创建 token 结构**（`internal/auth/trae/token.go`）
   - `LoginGuidanceResponse` + 提取 `LoginHost` 的辅助方法
   - `ExchangeRequest`, `ExchangeResponse`（含 AccessToken, RefreshToken, ExpiresAt, TokenType）
   - `UserInfoResponse`（含 Email, UserID, Nickname）
   - `CallbackParams`（AuthCode, RefreshToken, LoginHost, LoginRegion, LoginTraceID, CloudIDEToken, UserTag, Error, ErrorDescription）

4. **创建回调服务器**（`internal/auth/trae/oauth_server.go`）
   - 参考 `internal/auth/codex/oauth_server.go` 模式
   - 支持随机端口绑定（默认 0 → OS 分配）或 opts.CallbackPort
   - `/authorize` 路由：解析 query + fragment 参数，构造 `CallbackParams`
   - 返回 HTML 页面（参考 codex/html_templates.go 的结构，简单的成功/失败/待处理三态，中文文案与 cockpit-tools 一致即可）
   - `WaitForCallback(timeout 10min) (*CallbackParams, error)`，同时支持手动粘贴回调 URL（通过 `misc.ParseOAuthCallback` + `misc.AsyncPrompt`）

5. **创建 TraeAuth 服务类**（`internal/auth/trae/trae_auth.go`）
   - `NewTraeAuth(cfg, endpoints) *TraeAuth`
   - `GetLoginGuidance(ctx, loginTraceID) (loginHost string, err)`：依次尝试 3 个 CN endpoint，全部失败则 fallback 到默认 `https://www.trae.cn`
   - `BuildAuthURL(loginHost, loginTraceID, callbackURL, codeChallenge, deviceCtx) string`：按照 cockpit-tools 的 query 参数表构建 `/authorization` URL（使用 `url.Values`，并确保 `auth_callback_url` 不做双重 encode）
   - `ExchangeToken(ctx, loginHost, deviceCtx, authCode, refreshToken, codeVerifier, redirectURI) (*ExchangeResponse, actualOrigin string, err)`：按优先级尝试候选 origins（auth_code → 走 account origin；refresh_token → 走 candidate_api_origins），对两种 exchange 路径都尝试
   - `GetUserInfo(ctx, actualOrigin, accessToken) (*UserInfoResponse, error)`：调用 `/cloudide/api/v3/trae/GetUserInfo`
   - Device context：默认值（machine_id = UUID；device_id = "0"；x_device_type 根据 runtime.GOOS；x_device_brand 简单值；x_app_version = "3.5.66"；x_app_type = "stable"；plugin_version = "local"）。不读 Trae 安装目录或日志，保持 CLI 工具轻量。
   - 暂不实现 P-256 设备密钥对，`DevicePublicKey` / `DeviceInfo` 留空字符串或省略（cockpit-tools 代码中 ExchangeToken 也会在缺失时回退）

6. **创建 Authenticator 层**（`sdk/auth/trae_cn.go`）
   - `TraeCNAuthenticator` 结构体（默认 CallbackPort 0 表示随机）
   - `Provider() string` → `"trae-cn"`
   - `RefreshLead() *time.Duration` → 返回 `24 * time.Hour`（Trae token 有效期通常较长，设置 24h lead 以支持自动刷新调度观察）
   - `Login(ctx, cfg, opts) (*coreauth.Auth, error)`：
     1. 生成 `login_trace_id = uuid.NewString()`
     2. 调用 `GetLoginGuidance` 拿 loginHost
     3. 生成 PKCE pair
     4. 启动回调服务器，拿到 port，build `callbackURL = http://127.0.0.1:{port}/authorize`
     5. build verification_uri，自动开浏览器或打印 URL（遵循 `opts.NoBrowser`、`browser.IsAvailable()`、`util.PrintSSHTunnelInstructions`）
     6. `WaitForCallback` → 拿到 authCode 或 refreshToken
     7. `ExchangeToken` + `GetUserInfo`
     8. 组装 `coreauth.Auth`：`ID = fmt.Sprintf("trae-cn-%s.json", userInfo.UserID_or_email)`，`Provider = "trae-cn"`，`Label = email`，`Metadata = { access_token, refresh_token, email, user_id, nickname, expires_at, login_region, login_host, user_tag, client_id, login_trace_id }`，`Storage` 可复用 `TraeTokenData` 包装后赋值（或直接用 `metadata` + 一个简化的 token storage 结构体，参考 codex）
     9. 服务器 stop defer、超时处理、错误包装

7. **创建命令入口**（`internal/cmd/trae_cn_login.go`）：参考 `qoder_cn_login.go`，调用 `manager.Login(ctx, "trae-cn", cfg, authOpts)`，输出保存路径和成功信息。

8. **注册 Authenticator**：
   - `internal/cmd/auth_manager.go`：`newAuthManager()` 追加 `sdkAuth.NewTraeCNAuthenticator()`
   - `sdk/auth/refresh_registry.go`：`init()` 里追加 `registerRefreshLead("trae-cn", func() Authenticator { return NewTraeCNAuthenticator() })`

9. **接入 CLI flag 分发**（`cmd/server/main.go`）：
   - 结构体加 `traeCNLogin bool` 字段
   - `isOneShotCommandMode` 加 `opts.traeCNLogin`
   - flag 定义加 `flag.BoolVar(&traeCNLogin, "trae-cn-login", false, "Login to Trae CN using OAuth")`
   - dispatch 处加 `else if traeCNLogin { cmd.DoTraeCNLogin(cfg, options) }`

10. **更新测试矩阵**（`cmd/server/main_test.go`）：`commandModeOptions` 中加 `{name: "trae-cn login", opts: commandModeOptions{traeCNLogin: true}, want: true}`

11. **编译验证**：`go build -o cli-proxy-api ./cmd/server`，确保无错误。

12. **格式化代码**：`gofmt -w .`

## Dependencies and Considerations

- **crypto/rand, crypto/sha256, encoding/base64**：Go 标准库即可实现 PKCE，无需新增依赖。
- **github.com/google/uuid**：已在 `internal/auth/qoder/qoder_auth.go` 中使用，项目已有依赖。
- **net/http** 原生 HTTP 客户端即可（不需要 utls / fancy transport，trae.cn 的 TLS 是标准配置）。
- **device context 默认值**：不做 Trae 安装目录扫描（与 cockpit-tools 不同），CLI 工具运行在 headless/容器环境是常态，使用固定默认值 + UUID 即可。如果服务端对字段强校验，可后续加 Metadata 覆盖。
- **P-256 设备密钥对**：cockpit-tools 中有生成，但 ExchangeToken 请求中 DevicePublicKey 与 DeviceInfo 仅在某些场景是强校验（主要是设备绑定）。MVP 阶段先省略这些字段；若实际运行 ExchangeToken 失败，再补充 `crypto/ecdsa` + `x509` 生成 SPKI PEM。
- **手动粘贴回调回退**：与 codex 流程一致，15 秒后触发 prompt，支持直接粘贴 callback URL。
- **CN LoginGuidance 全部失败时的 fallback**：cockpit-tools 中 CN 可以 fallback 到默认 `https://www.trae.cn`，我们同样处理（国际版是直接报错）。
- **错误包装**：不使用 `log.Fatal`，返回 error 给上层；logrus 结构化日志。

## Validation

1. **构建通过**：执行 `go build -o cli-proxy-api ./cmd/server`，无报错。
2. **格式化通过**：`gofmt -w .` 之后无差异。
3. **测试通过**：`go test ./cmd/server/... ./internal/cmd/... ./sdk/auth/... ./internal/auth/trae/...`（至少确保新增测试用例在 `main_test.go` 通过）。
4. **帮助输出包含 flag**：`./cli-proxy-api.exe -h 2>&1 | findstr trae-cn-login` 能看到 flag 说明。
5. **commandMode 测试**：`go test -v -run TestIsOneShot ./cmd/server/...` 新增的 trae-cn login case 返回 true。

## Risks

- **风险 1：ExchangeToken / GetUserInfo 字段名与实际接口不匹配**：cockpit-tools 中使用了多层路径 pick（`Result.LoginHost`、`result.LoginHost`、`data.result.LoginHost` 等），MVP 只支持常见两层。处理：在 `trae_auth.go` 中使用 gjson 或多路径提取函数，和 qoder/codex 一致；失败时打印完整响应体（DEBUG 级别日志）便于排查。
- **风险 2：device context 字段不足导致接口拒绝请求**：服务端可能强校验 `client_id`、`plugin_version`、`x_app_version`、`machine_id`。处理：所有字段都填默认值，必要时通过 `opts.Metadata` 支持覆盖；MVP 版本至少保证 `client_id` 与 CN 客户端一致（`ono9krqynydwx5`）。
- **风险 3：回调端口在沙箱/受限环境无法绑定**：与 codex 一致，提供 `--oauth-callback-port` 覆盖 + 手动粘贴回调 URL fallback；SSH 隧道说明沿用 `util.PrintSSHTunnelInstructions`。
- **风险 4：与现有内部 auth 包命名冲突**（如已有 trae 相关内容）：项目中目前没有任何 `trae` 相关的 Go 源文件，风险低；`internal/auth/trae/` 作为独立 package 不会冲突。
