# Task 2 Report: SDK 认证器 `sdk/auth/traecn.go`
## Status: DONE（接口偏差已由 controller 修正）

## Controller 修正记录

- **RefreshLead 签名**：`Authenticator` 接口（`sdk/auth/interfaces.go:28`）为 `RefreshLead() *time.Duration`。已将实现改为返回指针（`d := 30 * time.Minute; return &d`，镜像 `qoder.go`），测试同步改为解引用断言。该 concern 已解决。

## 实现内容

按 brief Step 1/2 完整落地两个文件（未修改任何既有文件）：

- `sdk/auth/traecn_test.go` — brief Step 1 的三个测试原样落地：`TestTraeCNAuthenticatorProvider`、`TestTraeCNAuthenticatorRefreshLead`（`RefreshLead()` 与 `30*time.Minute` 直接比较）、`TestTraeCNAuthenticatorLoginNilConfig`。
- `sdk/auth/traecn.go` —
  - `TraeCNAuthenticator` + `NewTraeCNAuthenticator()`；`Provider()` 返回 `"trae-cn"`；`RefreshLead()` 返回 `30 * time.Minute`。
  - `Login(ctx, cfg, opts)`：
    1. cfg nil → error（与 claude/qoder 同一文案 `"cliproxy auth: configuration is required"`）；ctx/opts nil 兜底。
    2. `fp := traecn.NewDeviceFingerprint()`。
    3. 端口：`opts.CallbackPort > 0` 优先，否则 `traecn.DefaultCallbackPort`（8021）。
    4. `authURL := traecn.BuildAuthorizeURL(fp, callbackPort)`（在起 server 前构造，保证端口被占时授权 URL 仍可打印——但其 redirect_uri 指向实际未监听的端口，故走纯手动粘贴，见隐患 2）。
    5. 起 `traecn.NewOAuthServer(port).Start()`；失败（端口占用等）→ `log.Warnf` 降级为纯手动粘贴模式（不起 callback goroutine、不打印 SSH tunnel 提示）。成功时 defer Stop（2s 超时，镜像 claude.go）。
    6. 浏览器：`!opts.NoBrowser` 时 `browser.IsAvailable()` / `browser.OpenURL(authURL)`，失败时打印 URL；`util.PrintSSHTunnelInstructions` 仅在 server 已启动时打印。
    7. **双通道等待**（逐行镜像 claude.go）：`callbackCh`/`callbackErrCh` 跑 `WaitForCallback(5 * time.Minute)` goroutine；`opts.Prompt != nil` 时 15 秒 timer 后 `misc.AsyncPrompt(opts.Prompt, "Paste the login success URL here: ")`（brief 指定文案）；timer 触发时先非阻塞 drain callback 通道再发起 prompt；`ctx.Done()` → 返回 wrapped `ctx.Err()`（defer 负责停 server）；手动输入用 `traecn.ParseCallbackURL(input)`（支持 query+fragment 合并，空输入报错——因为该包级函数对无参数输入返回 error，与 claude 的 `parsed == nil → continue` 不同，但 Trae 场景"粘贴成功 URL"时空输入无意义，可接受）。
    8. token 链：`firstNonEmptyParam(params, "token", "access_token", "ide_token")` 非空 → 直接作 AccessToken（顺带取 `refresh_token`/`user_id` param）；否则 `params["refresh_token"]` 非空 → `authSvc.ExchangeToken(ctx, traecn.ClientID, rt)`；否则 → error 附 `paramKeys(params)`（仅键名排序列表，不泄露参数值/token）。
    9. `expireMs`：`td.ExpiresIn > 0` 时 `time.Now().UnixMilli() + ExpiresIn*1000`；否则 0（`CreateTokenStorage` 接受 0，`expires_at` metadata 跳过）。
    10. email/label 解析顺序严格按 brief：`opts.Metadata["email"]` → `opts.Metadata["alias"]` → `td.UserID` → `fmt.Sprintf("user-%d", time.Now().UnixMilli())`；label = email。
    11. `storage := traecn.CreateTokenStorage(td, fp, email, expireMs)`；`id/fileName := "trae-cn-" + label + ".json"`。
    12. metadata：`email`、`user_id`，且 `expireMs > 0` 时写 `expires_at = time.UnixMilli(expireMs).UTC().Format(time.RFC3339)`（`Auth.ExpirationTime()` 读 `expires_at` 键——已在 `sdk/cliproxy/auth/types.go:651` `expireKeys` 中确认包含）。
    13. 返回 `&coreauth.Auth{ID, Provider, Label, FileName, Storage, Metadata}`。
  - 辅助函数：`firstNonEmptyParam`（trim 后首个非空）、`paramKeys`（排序键名列表，仅用于错误消息）。

## 静态自审（brief Step 3）

- **imports**：stdlib（context/fmt/sort/strings/time）→ 第三方（logrus）→ 项目（traecn/browser/config/misc/util/coreauth），所有 import 均被使用；与 claude.go/qoder.go 分组风格一致。
- **与 Task 1 包签名对齐**（已逐一对照 `internal/auth/traecn/*.go`）：
  - `NewDeviceFingerprint() DeviceFingerprint`（值）✓
  - `BuildAuthorizeURL(fp, port) string`（包级）✓
  - `NewTraeCNAuth(cfg) *TraeCNAuth` ✓（cfg 非 nil 时已在外层校验）
  - `ExchangeToken(ctx, clientID, refreshToken) (*TokenData, error)` 方法 ✓
  - `ParseCallbackURL(raw) (map[string]string, error)` 包级 ✓
  - `CreateTokenStorage(td, fp, email, expireMs) *TraeCNTokenStorage` ✓
  - `NewOAuthServer(port)` + `Start()/Stop(ctx)/WaitForCallback(5*time.Minute)` ✓（注意：Task 1 的 `WaitForCallback` 签名是 `(timeout time.Duration)` 而非 `(ctx)`，与 iflow 一致，本实现按实际签名调用）
  - `OAuthResult{Params map[string]string; Error error}` ✓（手动粘贴路径构造 `&traecn.OAuthResult{Params: params}`）
  - `TokenData{AccessToken, RefreshToken, ExpiresIn, UserID}` ✓（曾误用不存在的 `ExpireTime` 字段，已修正为局部 `expireMs` 计算）
- **channel/select/timer 与 claude.go 对齐**：buffered chan 容量 1、`break waitForCallback` label、timer 触发后的非阻塞 drain、`manualInputCh/manualInputErrCh` 收到后置 nil、`defer manualPromptTimer.Stop()`——全部一致。新增 `ctx.Done()` 分支（brief Step 2.5 明确要求，claude.go 没有，属有意增强）。
- **coreauth.Auth 字段**：`ID/Provider/Label/FileName/Storage/Metadata` 均为 `sdk/cliproxy/auth/types.go` 中真实字段（已读 struct 确认，`Label` 存在于 line 61）；qoder.go 未用 Label 但 brief Step 2.10 明确要求，已按 brief 写。
- **gofmt 风格**：tab 缩进、import 三组分组、无行尾空格（手工保持；无法跑 gofmt，请用户验证时跑 `gofmt -w sdk/auth/`）。
- **全局约束**：注释全英文；无 `log.Fatal`；错误消息只含参数键名不含值（不泄露 token）；timeout 仅用于 credential acquisition（回调 server 的 5 分钟等待与 2 秒 stop，镜像模板）。

## 隐患 / 需裁决

1. **`RefreshLead()` 返回类型与 `Authenticator` 接口不符**：`sdk/auth/interfaces.go:28` 定义 `RefreshLead() *time.Duration`（claude/qoder/iflow 全部返回指针），而 brief Step 1 测试写死 `if got := a.RefreshLead(); got != 30*time.Minute`（指针与值直接用 `!=` 比较无法通过编译），brief Step 2 也写值类型。本实现**按 brief/测试**采用值类型 `time.Duration`。后果：`TraeCNAuthenticator` **不满足** `Authenticator` 接口，后续任务注册到 manager（`manager.go`/refresh_registry）时要么改接口、要么把测试和方法一起改为指针。代码中已加 NOTE 注释提示。**这是本任务最大的待裁决点。**
2. **端口占用降级时授权 URL 的 redirect_uri 指向未监听端口**：brief 要求"失败→打印授权 URL 走纯手动粘贴"。此时浏览器最终会被重定向到 `http://127.0.0.1:<port>/authorize?...` 但连接被拒，用户需从地址栏复制 URL 粘贴——流程可行但体验有坑，且若 15 秒 timer 未配 `opts.Prompt` 则只能等 5 分钟超时（与 claude 无 Prompt 时行为一致）。属 brief 指定行为，未改动。
3. **手动粘贴空输入直接报错**：`traecn.ParseCallbackURL("")` 返回 error → Login 返回错误；claude.go 的 `misc.ParseOAuthCallback` 对空输入返回 nil 并 `continue` 继续等待。差异源于 Task 1 包级函数语义，brief 指定用 `ParseCallbackURL`，未擅自改 Task 1 代码。
4. **直接 token 路径无 ExpiresIn**：回调若直接带 `token` 而无 `expires_in`，`expireMs=0` → 不写 `expires_at`，`Auth.ExpirationTime()` 返回 false，刷新调度依赖后续任务（executor/refresh registry）的兜底。brief 未要求解析回调里的 `expires_in` param，未添加（YAGNI）。
5. **未编译/未跑测试**：本机无 Go 工具链（同 Task 1 结论）。请用户安装 Go 1.26+ 后执行下方验证命令；特别留意隐患 1 可能在 `sdk/auth` 包其他文件（如 manager 注册处）未来引用时暴露编译错误——当前包内无引用，两个新文件自身可编译。

## 文件清单（新建）

- `d:\code\mine\CLIProxyAPIPlus\sdk\auth\traecn.go`
- `d:\code\mine\CLIProxyAPIPlus\sdk\auth\traecn_test.go`

## 验证命令（用户手动执行）

```powershell
# 安装 Go 1.26+ 并把 go.exe 加入 PATH 后，在仓库根目录：
gofmt -w sdk/auth
go test ./sdk/auth/ -run TraeCN -v
go build ./...
```

## Git 命令（用户手动执行，**不要让我跑**）

```bash
git add sdk/auth/traecn.go sdk/auth/traecn_test.go
git commit -m "feat(auth): add trae-cn SDK authenticator with dual-channel login"
```
