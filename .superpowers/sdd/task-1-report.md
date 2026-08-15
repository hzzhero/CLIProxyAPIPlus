# Task 1 Report: `internal/auth/traecn/` 认证核心包

## Status: BLOCKED (实现已写完，但本机无 Go 工具链，无法编译/跑测试)

## 实现内容

按 brief Step 2-5 完整实现了 5 个文件（新包 `internal/auth/traecn`）：

- `endpoints.go` — 常量 `ClientID`, `DefaultCallbackPort=8021`, `AuthorizeURL`, `AuthBase`, `APIBase`（注释标明 `// CN ExchangeToken node; verify via packet capture`）, `ModelAPIBase`，以及派生的 `GetRefreshTokenURL`, `ExchangeTokenURL`, `ModelListURL`, `ChatURL`。
- `trae_token.go` — `TraeCNTokenStorage` 结构体（含 `Token/RefreshToken/Email/Name/UserID/ExpireTime(ms)/LastRefresh/Type` 及完整 `x_*` 设备指纹字段），方法：
  - `SaveTokenToFile(path)`：Type 空时设 `"trae-cn"`；`misc.MergeMetadata` 合并 → 写 `.tmp-traecn-*` 临时文件 → `os.Rename` 原子提交（镜像 `qoder_token.go`）。
  - `SetMetadata()`：把 `email` / `user_id` / `expire_time`（ms → RFC3339）写入 `Metadata`（为 nil 先初始化）。
  - `IsExpired()`：`ExpireTime > 0 && time.Now().UnixMilli() >= ExpireTime`。
- `trae_auth.go` —
  - `DeviceFingerprint` + `NewDeviceFingerprint()`（`uuid.NewString()` 生成 DeviceID/MachineID，静态 Windows 客户端指纹 `Microsoft/x86_64/Windows/10.0.19045/2.0.0/20000/stable`，使用项目既有依赖 `github.com/google/uuid`）。
  - `BuildAuthorizeURL(fp, port)`：包级函数，`AuthorizeURL` + `url.Values` 编码（`client_id/x_device_id/x_machine_id/x_device_brand/x_device_type/x_os_version/redirect_uri=http://127.0.0.1:<port>/authorize`）。
  - `TokenData{AccessToken,RefreshToken,ExpiresIn,UserID}`。
  - `TraeCNAuth` + `NewTraeCNAuth(cfg *config.Config)`：cfg 为 nil 用 `http.DefaultClient`；非 nil 用 `util.SetProxy(&cfg.SDKConfig, &http.Client{})`（镜像 qoder）。
  - `ExchangeToken(ctx, clientID, refreshToken)`：POST `ExchangeTokenURL`，JSON body `{"ClientID":...,"RefreshToken":...,"ClientSecret":"-","UserID":""}`；解析 `Result.Token/RefreshToken/ExpiresIn/UserID`；空 token → `fmt.Errorf("trae-cn: exchange returned empty token (endpoint may have changed, check endpoints.go)")`。
  - `ParseCallbackURL(raw)`：包级函数，合并 query 与 fragment 的所有参数；无参数或 URL 解析失败 → error。
  - `CreateTokenStorage(td, fp, email, expireMs)`：组装 storage，`LastRefresh=time.Now().Format(time.RFC3339)`，并调用 `SetMetadata()`。
- `oauth_server.go` — 镜像 `internal/auth/iflow/oauth_server.go`：`OAuthServer{Start/Stop/WaitForCallback}`，回调路径 `/authorize`，`OAuthResult{Params map[string]string; Error error}`（整包 query 参数回传），成功时返回内联 HTML "Login Successful. You can close this window and return to the terminal."；error 参数时返回 400 与可读错误页。HTTP server 的 Read/WriteTimeout 10s（镜像 iflow；属于 credential acquisition 阶段的限制，符合全局约束）。
- `trae_auth_test.go` — brief Step 1 三个测试原样落地：`TestBuildAuthorizeURL`、`TestParseCallbackURL`（含 fragment、garbage 用例）、`TestTokenStorageSaveAndType`。

## TDD 证据

### RED
- 命令：`go test ./internal/auth/traecn/`
- 输出（PowerShell）：
  ```
  go : The term 'go' is not recognized as the name of a cmdlet, function, script file, or operable program.
  ```
- 说明：本机没有可用的 Go 工具链。`[Environment]::GetEnvironmentVariable("GOROOT","User")` 返回 `C:\Program Files\Go`，但该目录实际不存在（`Test-Path "C:\Program Files\Go\bin\go.exe"` → False）；`C:\Users\HE\go\bin` 也在 PATH 里但目录为空；`where.exe go` 找不到。因此无法按 TDD 流程真正跑出"包不存在"的失败——但这与包尚未实现等效（无法编译）。

### GREEN
- 无法获得。需要用户安装 Go 1.26+ 后执行验证命令。

## 文件清单（新建）

- `d:\code\mine\CLIProxyAPIPlus\internal\auth\traecn\endpoints.go`
- `d:\code\mine\CLIProxyAPIPlus\internal\auth\traecn\trae_token.go`
- `d:\code\mine\CLIProxyAPIPlus\internal\auth\traecn\trae_auth.go`
- `d:\code\mine\CLIProxyAPIPlus\internal\auth\traecn\oauth_server.go`
- `d:\code\mine\CLIProxyAPIPlus\internal\auth\traecn\trae_auth_test.go`

未修改任何既有文件。

## 自审

- **完整性**：覆盖 brief Step 2-5 全部要求；测试文件与 brief Step 1 完全一致。
- **约束**：注释全英文；无 `log.Fatal`；defer 关闭错误用 `_ = resp.Body.Close()` / `_ = tmp.Close()` 包装；只在 credential acquisition 的 HTTP server 上使用 10s Read/WriteTimeout（镜像 iflow，属于 OAuth 本地回调监听，符合全局超时约束的"credential acquisition 阶段允许"精神）。
- **YAGNI**：未加额外接口/方法；`ExchangeToken` 未做重试（后续任务再决定）；`OAuthServer` 完全镜像 iflow 模板，未发明新抽象。
- **测试真实性**：`TestTokenStorageSaveAndType` 走真实 `SaveTokenToFile` 写盘 + 读回 + Unmarshal 验证 `Type="trae-cn"` 与 `Token/DeviceID` 持久化；`TestBuildAuthorizeURL`/`TestParseCallbackURL` 验证真实 URL 构造与解析行为。
- **镜像忠实度**：`SaveTokenToFile` 的 temp+atomic rename 与 `OAuthServer` 的 Start/Stop/WaitForCallback 与模板逐行对齐，仅改了包名/类型名/回调路径/成功页文案/结果结构。

## 隐患 / 待用户验证

1. **未编译/未跑测试**：本机无 Go 工具链，所有验证只能依赖用户环境。请安装 Go 1.26+ 后跑下方"验证命令"。
2. **`SetMetadata` 与 `MergeMetadata` 的键冲突**：`SaveTokenToFile` 通过 `misc.MergeMetadata(ts, ts.Metadata)` 把 metadata 展平进顶层 JSON。`SetMetadata` 写入的 `email`/`user_id`/`expire_time` 与结构体自身 json tag 相同，值相同（email/user_id），但 `expire_time` 结构体是 ms int、metadata 是 RFC3339 字符串——展平时 metadata 会覆盖结构体的 int，**导致保存到磁盘的 `expire_time` 变成字符串**。这是 brief 指定行为（`SetMetadata()` 把 ExpireTime 转 RFC3339），但与 `TestTokenStorageSaveAndType` 的 `json.Unmarshal` 回读 `ExpireTime int64` 不冲突（测试未读 ExpireTime）。如果后续 executor 需要回读 ms int，需在加载侧做兼容或在 SetMetadata 里不要写入 `expire_time`（仅写 `email`/`user_id`）。**建议后续任务决定**。
3. **ExchangeToken 响应结构**：按 brief 假设上游返回 `{"Result":{"Token":...,"RefreshToken":...,"ExpiresIn":...,"UserID":...}}`（PascalCase 嵌套）。若抓包发现 schema 不同，需改 `exchangeTokenResponse`。
4. **`APIBase` 节点**：brief 已标注 `// CN ExchangeToken node; verify via packet capture`，需抓包确认。

## 验证命令（用户手动执行）

```powershell
# 安装 Go 1.26+ 并把 go.exe 加入 PATH 后，在仓库根目录：
gofmt -w internal/auth/traecn
go test ./internal/auth/traecn/ -v
go build ./...
```

## Git 命令（用户手动执行，**不要让我跑**）

```bash
git add internal/auth/traecn/
git commit -m "feat(auth): add trae-cn auth core package (endpoints, token storage, oauth server)"
```
