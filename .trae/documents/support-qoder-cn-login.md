# 支持 `-qoder-cn-login`（Qoder 中国国内版）— 执行计划

## Summary

CLIProxyAPI 已完整实现 `qoder`（国际版）provider：浏览器设备流登录、COSY 签名、动态模型列表、用量查询。但国际版端点（`qoder.sh` / `qoder.com`）在国内访问受限，且 Qoder CN（前身通义灵码/Lingma，2026-05-20 升级更名）使用独立的国内端点（`qoder.com.cn` / `gateway.qoder.com.cn`），账号体系也不同。

本计划新增 `-qoder-cn-login`：CN 仅支持 **PAT（Personal Access Token）登录**（无浏览器设备流），用户从 `https://qoder.com.cn/account/integrations` 获取 `pt-...`，通过 `POST {openapi}/api/v1/jobToken/exchange` 换取短期 job token（`jt-...`，约 24h）。COSY 签名方案两版完全一致（`BuildAuthHeaders` 已把 URL 作为参数，签名 region 无关）。

**预期结果**：`-qoder-cn-login` 交互式输入 PAT → 换取 job token → 保存为 `qoder-cn-<label>.json`；服务自动注册 `qoder-cn/*` 模型；job token 到期前由 conductor 调度器自动用 PAT 重新交换并落盘。

## Current State Analysis（经代码验证）

### CN 与国际版端点对照

| 用途 | 国际版（现有 const） | CN 版（新增） |
|---|---|---|
| 推理/聊天/模型列表 base | `https://api3.qoder.sh/`（`QoderChatBase`） | `https://gateway.qoder.com.cn/` |
| OpenAPI | `https://openapi.qoder.sh`（`QoderOpenAPIBase`） | `https://openapi.qoder.com.cn` |
| Center | `https://center.qoder.sh`（`QoderCenterBase`） | `https://gateway.qoder.com.cn` |
| 模型列表 | `{base}algo/api/v2/model/list?Encode=1` | 同左（换 base） |
| 聊天 | `{base}algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1` | 同左（换 base） |
| PAT→job token 交换 | —（CN 独有） | `{openapi}/api/v1/jobToken/exchange` |
| UserInfo | `{openapi}/api/v1/userinfo` | 同左（换 openapi） |
| Usage | `{openapi}/api/v2/quota/usage` | 同左（换 openapi） |
| Refresh token | `{center}/algo/api/v3/user/refresh_token` | 同左（换 center；CN 实际走 PAT 重新交换，不走此端点） |
| 登录页 | `https://qoder.com/device/selectAccounts`（设备流） | 无（PAT only） |

### 现有代码关键位置（已验证行号）

- [internal/auth/qoder/qoder_auth.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/qoder_auth.go)：const 块 L19-59；`QoderAuth` struct L133-135（仅 `httpClient`）；`NewQoderAuth` L138-142；`InitiateDeviceFlow` 用 `QoderLoginURL` L157；`PollForToken` 用 `QoderOAuthTokenEndpoint` L179；`RefreshTokens` 用 `QoderRefreshTokenEndpoint` L280；`FetchUserInfo` 用 `QoderUserInfoEndpoint` L330；`CreateTokenStorage` L391-404。
- [internal/auth/qoder/api.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/api.go)：const 块 L12-28（`QoderInferURL`/`QoderChatURL`/`QoderChatURLEncoded`/`QoderModelListURL`）；`ModelMap` L35-50；`doRefreshToken` L55；`RefreshTokenIfNeeded` L78。
- [internal/auth/qoder/qoder_token.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/qoder_token.go)：`QoderTokenStorage` struct L17-66；`SaveTokenToFile` L174，`ts.Type = "qoder"` 硬编码于 L176。
- [internal/runtime/executor/qoder_executor.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/runtime/executor/qoder_executor.go)：`QoderExecutor` struct L31-33；`NewQoderExecutor` L36-40；`Identifier` L43-45；`ExecuteStream` L48，模型前缀剥离 `qoder/` L76，`QoderChatURLEncoded` 用于签名 L193 与请求 L206，日志里 `QoderChatURL` L239，session salt `"qoder-session"` L112；`Refresh` no-op L747-752；`FetchQoderModels` L827，`QoderModelListURL` L837/L849，`ID="qoder/"+key`/`OwnedBy="qoder"`/`Type="qoder"` L910-914，回退 `GetQoderModels()` 多处；`FetchQoderUsage` L1019，硬编码 `usageURL` L1025。
- [sdk/auth/qoder.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/qoder.go)：`QoderAuthenticator` L17；`Provider()` L24 返回 `"qoder"`；`RefreshLead()` L28-37 返回 `24h`；`Login()` L39-133。
- [sdk/auth/gitlab.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/gitlab.go)：`requireInput`/`resolveString` L355-388 —— PAT 输入模板（支持 `opts.Metadata` + 环境变量回退 + `opts.Prompt`）。
- [internal/cmd/qoder_login.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/cmd/qoder_login.go)：`DoQoderLogin` L20-60，默认 promptFn L28-36。
- [internal/cmd/auth_manager.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/cmd/auth_manager.go)：`newAuthManager` L14-31，`NewQoderAuthenticator()` 在 L28。
- [sdk/auth/refresh_registry.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/refresh_registry.go)：`init()` L9-21，`registerRefreshLead("qoder", ...)` 在 L20 —— 调度器自动刷新注册点。
- [sdk/auth/filestore.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/filestore.go)：qoder storage 反序列化 L351-361（`if provider == "qoder"`，空 Type 设为 `"qoder"`）。
- [internal/watcher/synthesizer/file.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/watcher/synthesizer/file.go)：qoder storage 反序列化 L214-229（热重载路径，第二处反序列化点）。
- [sdk/cliproxy/service.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/cliproxy/service.go)：执行器注册 `case "qoder"` L1133-1134；模型注册 `case "qoder"` L2109-2111。
- [internal/registry/model_definitions.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/registry/model_definitions.go)：`staticModelsJSON.Qoder` L30；静态查找 `case "qoder"` L317-318；`GetQoderModels` L873-875；`allModels` 含 `data.Qoder` L354。
- [cmd/server/main.go](file:///d:/code/mine/CLIProxyAPIPlus/cmd/server/main.go)：`commandModeOptions.qoderLogin` L108；`isOneShotCommandMode` 含 `opts.qoderLogin` L135；`var qoderLogin` L171；flag 定义 L214；opts 字面量 L725；派发 L873-874。
- [cmd/server/main_test.go](file:///d:/code/mine/CLIProxyAPIPlus/cmd/server/main_test.go)：测试表 L121。
- [internal/auth/qoder/qoder_auth_test.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/qoder_auth_test.go)：L49-50 引用 `QoderLoginURL` —— **const 必须保留**。
- [sdk/cliproxy/auth/types.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/cliproxy/auth/types.go)：`Auth.Clone()` L262-287，`copyAuth := *a` 对 `Storage` 接口是**浅拷贝**；`ExpirationTime()` L626-634 只读 `Metadata` 的 `expires_at` 等键（不读 `QoderTokenStorage.ExpireTime`）；`expireKeys` L651。
- [sdk/cliproxy/auth/conductor.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/cliproxy/auth/conductor.go)：refresh 流程 L5994 调 `exec.Refresh(ctx, cloned)` → L6051 调 `m.Update(ctx, updated)` 自动落盘。**`Refresh` 不要调 `SaveTokenToFile`**。

## Proposed Changes

### A. 端点参数化与认证核心（internal/auth/qoder/）

**A1. 新增 [internal/auth/qoder/endpoints.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/endpoints.go)**

定义 `Endpoints` 结构体 + 两个实例 + 选择函数：

```go
package qoder

// Endpoints groups the region-specific URLs for a Qoder deployment.
type Endpoints struct {
    ChatBase            string // 推理/聊天/模型列表 base
    OpenAPIBase         string
    CenterBase          string
    LoginURL            string // 设备流登录页（CN 不用）
    OAuthTokenEndpoint  string // 设备流 poll（CN 不用）
    RefreshTokenEndpoint string
    UserInfoEndpoint    string
    JobTokenExchangeURL string // CN 独有：PAT→job token
    UsageURL            string
    ChatURL             string // = ChatBase + "/algo" + QoderSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common"
    ChatURLEncoded      string // = ChatURL + "&Encode=1"
    ModelListURL        string // = ChatBase + "/algo/api/v2/model/list"
}

// GlobalEndpoints holds the international (qoder.sh) URLs.
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
    // JobTokenExchangeURL 留空：国际版无 PAT 交换
}

// CNEndpoints holds the China (qoder.com.cn) URLs.
var CNEndpoints = Endpoints{
    ChatBase:             "https://gateway.qoder.com.cn",
    OpenAPIBase:          "https://openapi.qoder.com.cn",
    CenterBase:           "https://gateway.qoder.com.cn",
    LoginURL:             "", // CN 无设备流
    OAuthTokenEndpoint:   "",
    RefreshTokenEndpoint: "https://gateway.qoder.com.cn/algo/api/v3/user/refresh_token",
    UserInfoEndpoint:     "https://openapi.qoder.com.cn/api/v1/userinfo",
    JobTokenExchangeURL:  "https://openapi.qoder.com.cn/api/v1/jobToken/exchange",
    UsageURL:             "https://openapi.qoder.com.cn/api/v2/quota/usage",
    ChatURL:              "https://gateway.qoder.com.cn" + "/algo" + QoderSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common",
    ChatURLEncoded:       "https://gateway.qoder.com.cn" + "/algo" + QoderSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1",
    ModelListURL:         "https://gateway.qoder.com.cn/algo/api/v2/model/list",
}

// EndpointsForProvider returns the endpoint set for "qoder" (global) or "qoder-cn".
func EndpointsForProvider(provider string) Endpoints {
    if strings.EqualFold(strings.TrimSpace(provider), "qoder-cn") {
        return CNEndpoints
    }
    return GlobalEndpoints
}
```

保留 `qoder_auth.go`/`api.go` 现有 `Qoder*` const 不动（`qoder_auth_test.go` L49 仍引用 `QoderLoginURL`）。

**A2. 改 [internal/auth/qoder/qoder_auth.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/qoder_auth.go)**

- `QoderAuth` struct（L133）增加 `endpoints Endpoints` 字段。
- `NewQoderAuth(cfg)`（L138）保持返回 global 端点：`return &QoderAuth{httpClient: ..., endpoints: GlobalEndpoints}`。
- 新增 `NewQoderAuthForProvider(cfg, provider) *QoderAuth`：用 `EndpointsForProvider(provider)`。
- `InitiateDeviceFlow`（L157）/`PollForToken`（L179）/`RefreshTokens`（L280）/`FetchUserInfo`（L330）内的 const URL 改用 `qa.endpoints.*`。
- 新增 `ExchangeJobToken(ctx, pat) (*QoderTokenData, error)`：POST `qa.endpoints.JobTokenExchangeURL`，body `{"personal_token":pat}`，解析返回的 job token（`jt-...`）+ refresh token + 过期时间，构造 `QoderTokenData` 返回。响应字段名以参考实现 simonsmh/pi-provider-qoder 为准（`token`/`refresh_token`/`expires_at` 或 `expire_time`）。

**A3. 改 [internal/auth/qoder/qoder_token.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/qoder_token.go)**

- `QoderTokenStorage` struct（L17）增加 `PersonalToken string json:"personal_token,omitempty"`（持久化 PAT 用于重新交换）。在 `MachineType` 字段后、`ModelConfigs` 前插入。
- `SaveTokenToFile`（L174）：L176 `ts.Type = "qoder"` 改为 `if strings.TrimSpace(ts.Type) == "" { ts.Type = "qoder" }`，避免覆盖 CN 的 `"qoder-cn"` 类型。需 `import "strings"`。

**A4. [internal/auth/qoder/api.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/auth/qoder/api.go)** 不动

`doRefreshToken`/`RefreshTokenIfNeeded` 仅 global 调用方使用；CN 走执行器 `Refresh`。加一行注释说明即可，不参数化。

### B. 认证器与登录命令

**B1. 改 [sdk/auth/qoder.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/qoder.go)**

保留 `QoderAuthenticator`（global/浏览器）。新增 `QoderCNAuthenticator`：

```go
// QoderCNAuthenticator implements PAT-based login for Qoder China.
type QoderCNAuthenticator struct{}

func NewQoderCNAuthenticator() *QoderCNAuthenticator { return &QoderCNAuthenticator{} }

func (a *QoderCNAuthenticator) Provider() string { return "qoder-cn" }

func (a *QoderCNAuthenticator) RefreshLead() *time.Duration {
    // CN job token ~24h；留 1h 提前量，到期前 conductor 触发 Refresh 用 PAT 重新交换。
    d := 1 * time.Hour
    return &d
}

func (a *QoderCNAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
    // 1. 校验 cfg；opts==nil 时给默认空 LoginOptions
    // 2. 通过 requireInput 模式（镜像 gitlab.go L374-388）获取 PAT：
    //    - opts.Metadata["personal_token"] 优先
    //    - 环境变量 QODER_CN_PERSONAL_TOKEN 回退
    //    - opts.Prompt("Enter Qoder CN personal access token (pt-...): ") 交互输入
    //    缺失则返回清晰错误（不要 panic）
    // 3. authSvc := qoder.NewQoderAuthForProvider(cfg, "qoder-cn")
    // 4. tokenData, err := authSvc.ExchangeJobToken(ctx, pat)
    // 5. name, email := authSvc.FetchUserInfo(ctx, tokenData.AccessToken)（best effort）
    // 6. storage := authSvc.CreateTokenStorage(tokenData, "")  // CN 无 machineID
    //    storage.PersonalToken = pat
    //    storage.Type = "qoder-cn"
    //    storage.Email = label; storage.Name = name
    // 7. label 解析顺序：email → opts.Metadata[email|alias] → tokenData.UserID → 时间戳回退
    // 8. 构造 metadata := map[string]any{
    //        "email": label, "name": name, "user_id": tokenData.UserID,
    //        "expires_at": time.UnixMilli(tokenData.ExpireTime).UTC().Format(time.RFC3339),
    //    }
    //    **关键**：必须写 expires_at RFC3339，ExpirationTime() 才能读到（types.go L626）
    // 9. fileName := fmt.Sprintf("qoder-cn-%s.json", label)
    // 10. return &coreauth.Auth{ID: fileName, Provider: "qoder-cn", FileName: fileName, Storage: storage, Metadata: metadata}, nil
}
```

PAT 输入辅助：可内联实现，或抽 `requireInput` 私有方法复用 gitlab 模式。环境变量名 `QODER_CN_PERSONAL_TOKEN`。

**B2. 新增 [internal/cmd/qoder_cn_login.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/cmd/qoder_cn_login.go)**

镜像 `qoder_login.go`（L20-60），`DoQoderCNLogin(cfg, options)` 调 `manager.Login(ctx, "qoder-cn", cfg, authOpts)`。默认 promptFn 同 L28-36。

**B3. 改 [internal/cmd/auth_manager.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/cmd/auth_manager.go)**

L28 后追加 `sdkAuth.NewQoderCNAuthenticator(),` 到 `NewManager` 参数列表。

**B4. 改 [sdk/auth/refresh_registry.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/refresh_registry.go)**

L20 后追加：
```go
registerRefreshLead("qoder-cn", func() Authenticator { return NewQoderCNAuthenticator() })
```
**关键**：这是调度器自动刷新的注册点，与 `auth_manager.go`（CLI 登录路径）是两处独立注册，不加则 CN auth 永不自动刷新。

### C. CLI flag（cmd/server/main.go）

**C1. 改 [cmd/server/main.go](file:///d:/code/mine/CLIProxyAPIPlus/cmd/server/main.go)**

- `commandModeOptions`（L108 `qoderLogin bool` 后）加 `qoderCNLogin bool`。
- `isOneShotCommandMode`（L135 `opts.qoderLogin ||` 后）加 `opts.qoderCNLogin ||`。
- L171 `var qoderLogin bool` 后加 `var qoderCNLogin bool`。
- L214 flag 定义后加 `flag.BoolVar(&qoderCNLogin, "qoder-cn-login", false, "Login to Qoder (CN) using a personal access token")`。
- L725 opts 字面量加 `qoderCNLogin: qoderCNLogin,`。
- L873-874 派发处加 `else if qoderCNLogin { cmd.DoQoderCNLogin(cfg, options) }`。

### D. 执行器（internal/runtime/executor/qoder_executor.go）

**D1. 改 [internal/runtime/executor/qoder_executor.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/runtime/executor/qoder_executor.go)**

- `QoderExecutor` struct（L31）加 `providerKey string` 字段。
- `NewQoderExecutor`（L36）设 `providerKey: "qoder"`；新增 `NewQoderCNExecutor(cfg)` 设 `providerKey: "qoder-cn"`。
- `Identifier()`（L43）返回 `e.providerKey`。
- 加私有 `func (e *QoderExecutor) endpoints() qoderauth.Endpoints { return qoderauth.EndpointsForProvider(e.providerKey) }`。
- `ExecuteStream`（L48）：
  - L76 模型前缀剥离：`qoderModel := strings.TrimPrefix(model, e.providerKey+"/")` 后再 `strings.TrimPrefix(qoderModel, "qoder/")` 兜底（兼容 CN 请求 `qoder-cn/auto` 与历史 `qoder/auto`）。
  - L112 session salt 改为 `e.providerKey + "-session"`。
  - L193 签名 URL：`qoderauth.QoderChatURLEncoded` → `e.endpoints().ChatURLEncoded`。
  - L206 请求 URL：同上。
  - L239 日志 URL：`qoderauth.QoderChatURL` → `e.endpoints().ChatURL`。
- `FetchQoderModels`（L827）：签名：保留 `(ctx, auth, cfg)`，内部读 `provider := auth.Provider`（而非固定 `"qoder"`），`ep := qoderauth.EndpointsForProvider(provider)`，`prefix := provider + "/"`（CN 为 `"qoder-cn/"`）。
  - L837/L849 `qoderauth.QoderModelListURL` → `ep.ModelListURL`。
  - L910-914 `ID`/`OwnedBy`/`Type` 用 `provider` 变量（`ID = prefix + key`、`OwnedBy = provider`、`Type = provider`）。
  - 所有 `registry.GetQoderModels()` 回退点：CN 回退 `registry.GetQoderCNModels()`，global 回退 `registry.GetQoderModels()`。抽一个 `func fallbackModels(provider string) []*registry.ModelInfo`。
- `FetchQoderUsage`（L1019）：读 `provider := auth.Provider`，L1025 `usageURL` 改为 `qoderauth.EndpointsForProvider(provider).UsageURL`。日志前缀按 provider 区分（可选）。
- `Refresh`（L747）：
  - global（`e.providerKey == "qoder"`）：保持 no-op。
  - CN 分支：
    ```go
    storage, ok := auth.Storage.(*qoderauth.QoderTokenStorage)
    if !ok || storage.PersonalToken == "" {
        return auth, nil  // 无 PAT 无法刷新，保持现状
    }
    authSvc := qoderauth.NewQoderAuthForProvider(e.cfg, "qoder-cn")
    tokenData, err := authSvc.ExchangeJobToken(ctx, storage.PersonalToken)
    if err != nil {
        return nil, fmt.Errorf("qoder-cn refresh: exchange job token: %w", err)
    }
    // 深拷贝 storage（Auth.Clone 对 Storage 是浅拷贝，types.go L262）
    cp := *storage
    cp.Token = tokenData.AccessToken
    cp.RefreshToken = tokenData.RefreshToken
    cp.ExpireTime = tokenData.ExpireTime
    cp.LastRefresh = time.Now().Format(time.RFC3339)
    updated := auth.Clone()
    updated.Storage = &cp
    if updated.Metadata == nil {
        updated.Metadata = map[string]any{}
    }
    updated.Metadata["expires_at"] = time.UnixMilli(tokenData.ExpireTime).UTC().Format(time.RFC3339)
    return updated, nil
    ```
  - **不要在执行器里调 `SaveTokenToFile`** —— conductor 会通过 `m.Update`（conductor.go L6051）自动落盘。

### E. 服务注册（sdk/cliproxy/service.go）

**E1. 改 [sdk/cliproxy/service.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/cliproxy/service.go)**

- L1133-1134 后加 `case "qoder-cn": s.coreManager.RegisterExecutor(executor.NewQoderCNExecutor(s.cfg))`。
- L2109-2111 后加 `case "qoder-cn": models = executor.FetchQoderModels(context.Background(), a, s.cfg); models = applyExcludedModels(models, excluded)`。

### F. auth 文件加载（两处独立反序列化点）

**F1. 改 [sdk/auth/filestore.go](file:///d:/code/mine/CLIProxyAPIPlus/sdk/auth/filestore.go) L351**

`if provider == "qoder"` → `if provider == "qoder" || provider == "qoder-cn"`，且 L355-357 `storage.Type` 空时设为 `provider`（不要硬编码 `"qoder"`）：
```go
if strings.TrimSpace(storage.Type) == "" {
    storage.Type = provider
}
```

**F2. 改 [internal/watcher/synthesizer/file.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/watcher/synthesizer/file.go) L214**

`if provider == "qoder"` → `if provider == "qoder" || provider == "qoder-cn"`，L224-226 `storage.Type` 空时设为 `provider`。**不加则 CN auth 文件热重载后 `Storage==nil`，请求报 `invalid auth storage type`。**

### G. 静态模型回退

**G1. 改 [internal/registry/model_definitions.go](file:///d:/code/mine/CLIProxyAPIPlus/internal/registry/model_definitions.go)**

- L318 `case "qoder": return GetQoderModels()` 后加 `case "qoder-cn": return GetQoderCNModels()`。
- L875 `GetQoderModels` 后新增：
  ```go
  // GetQoderCNModels returns Qoder model definitions namespaced under qoder-cn/.
  // CN shares the same upstream model set as the international edition; only the
  // ID prefix / OwnedBy / Type differ.
  func GetQoderCNModels() []*ModelInfo {
      base := cloneModelInfos(getModels().Qoder)
      for _, m := range base {
          if m == nil {
              continue
          }
          m.ID = "qoder-cn/" + m.ID
          m.OwnedBy = "qoder-cn"
          m.Type = "qoder-cn"
      }
      return base
  }
  ```
  **不要**改 `staticModelsJSON` 结构或 `model_updater.go` —— CN 与国际版共享同一上游模型集。

### H. 测试

**H1. 改 [cmd/server/main_test.go](file:///d:/code/mine/CLIProxyAPIPlus/cmd/server/main_test.go) L121**

L121 后加 `{name: "qoder-cn login", opts: commandModeOptions{qoderCNLogin: true}, want: true},`。

## Assumptions & Decisions

1. **PAT-only 登录**：CN 不支持浏览器设备流，`-qoder-cn-login` 交互式提示输入 PAT。管理 API（`/qoder-auth-url` 设备流）**不**为 CN 添加对应路由 —— PAT 输入本质交互式，纯 TUI/管理模式部署需在主机先跑一次 `-qoder-cn-login`。
2. **job token 自动续期**：CN job token ~24h。通过 `refresh_registry.go` 注册 `RefreshLead=1h`，conductor 调度器到期前调 `QoderExecutor.Refresh` 用持久化 PAT 重新交换，结果由 conductor 自动落盘（不调 `SaveTokenToFile`）。
3. **`expires_at` 元数据**：`Auth.ExpirationTime()`（types.go L626）只读 `Metadata` 的 `expires_at` 等键，不读 `QoderTokenStorage.ExpireTime`。登录和刷新时必须把过期时间以 RFC3339 写入 `storage.Metadata["expires_at"]`，`SaveTokenToFile` 经 `misc.MergeMetadata` 摊平进 JSON，下次加载时调度器才能看到真实过期时间。
4. **深拷贝 storage**：`Auth.Clone()`（types.go L262）对 `Storage` 接口是浅拷贝（`copyAuth := *a`），CN `Refresh` 必须构造新的 `*QoderTokenStorage`（`cp := *storage` 后改字段），避免与并发 `ExecuteStream` 竞争。
5. **保留现有 const**：`QoderLoginURL` 等被 `qoder_auth_test.go` L49 引用，作为 `GlobalEndpoints` 的字段来源保留，不删除。
6. **两处反序列化点**：`filestore.go`（启动/管理路径）与 `synthesizer/file.go`（热重载路径）独立，都必须加 `qoder-cn` 分支。
7. **环境变量**：PAT 支持环境变量 `QODER_CN_PERSONAL_TOKEN` 回退（非交互场景），优先级低于 `opts.Metadata["personal_token"]`。
8. **COSY 签名 region 无关**：`BuildAuthHeaders` 已把 URL 作为参数，无需改动 `cosy.go`。
9. **`ExchangeJobToken` 响应字段**：以参考实现 simonsmh/pi-provider-qoder `src/cosy.ts` 的逆向结果为准；若上游字段名有出入，按实际响应调整解析（防御性：`token` 为空时返回清晰错误）。

## Verification

1. **编译与格式化**（本地无 Go 环境，需在有 Go 环境机器执行）：
   ```bash
   gofmt -w .
   go build -o test-output ./cmd/server && rm test-output
   go test ./internal/auth/qoder/... ./internal/runtime/executor/... ./sdk/cliproxy/... ./sdk/auth/... ./cmd/server/... ./internal/registry/... ./internal/watcher/synthesizer/...
   ```
2. **登录冒烟**：`./cli-proxy-api -qoder-cn-login`，输入有效 CN PAT（`pt-...`），确认输出 `qoder-cn-<label>.json` 且文件含 `"type":"qoder-cn"`、`"personal_token":"pt-..."`、`"expires_at":"<RFC3339>"`、`"token":"jt-..."`。
3. **模型注册**：启动服务，`GET /v1/models` 应列出 `qoder-cn/auto`、`qoder-cn/ultimate` 等（来自动态 `/algo/api/v2/model/list`；上游不可达时回退静态 `GetQoderCNModels()`）。
4. **聊天**：向 `qoder-cn/auto` 发聊天请求，确认走 `gateway.qoder.com.cn` 且流式返回正常（COSY 签名验证通过）。
5. **自动续期**：将 auth 文件 `expires_at` 改为近未来（如 `now+5min`），等待调度器触发（`RefreshLead=1h` 意味着到期前 1h 触发；临时测试可调小），确认磁盘上 `token`（`jt-...`）和 `expires_at` 被更新，证明 conductor 驱动的 PAT 重新交换 + 落盘端到端生效。
6. **热重载**：手动编辑 CN auth 文件触发 watcher，确认 `Storage` 非空、请求不报 `invalid auth storage type for qoder`。
7. **回归**：`-qoder-login`（国际版）登录与聊天不受影响，`qoder/auto` 仍走 `api3.qoder.sh`。

## 实施顺序（建议）

1. A3（`qoder_token.go` 加 `PersonalToken` + 改 `Type` 赋值）—— 独立、对现有 qoder 也更安全，先做。
2. A1（`endpoints.go` 新文件）—— 纯新增，无破坏。
3. A2（`qoder_auth.go` 参数化 + `ExchangeJobToken`）。
4. B1（`QoderCNAuthenticator`）。
5. B2/B3/B4（登录命令 + manager + refresh_registry 注册）。
6. C1（CLI flag）。
7. D1（执行器 CN 适配）。
8. E1（service.go 注册）。
9. F1/F2（两处反序列化点）。
10. G1（静态模型回退）。
11. H1（测试）。
12. 编译 + 测试 + 冒烟验证。
