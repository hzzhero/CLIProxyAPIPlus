# Task 2 Brief: SDK 认证器 `sdk/auth/traecn.go`

**Files:**
- Create: `sdk/auth/traecn.go`
- Test: `sdk/auth/traecn_test.go`

模板（写代码前先读）：
- `sdk/auth/claude.go` — 双通道等待循环（callbackCh goroutine + 15 秒后 `misc.AsyncPrompt` 手动粘贴提示，select 竞速）的精确实现
- `sdk/auth/qoder.go` — label 解析顺序、`expires_at` RFC3339 元数据写法、`coreauth.Auth` 组装
- Task 1 已完成的 `internal/auth/traecn/` 包（`DeviceFingerprint`/`NewDeviceFingerprint`/`BuildAuthorizeURL`/`TraeCNAuth`/`NewTraeCNAuth`/`ExchangeToken`/`ParseCallbackURL`/`CreateTokenStorage`/`NewOAuthServer`/`DefaultCallbackPort`/`ClientID`）

## Step 1: 先写测试 `sdk/auth/traecn_test.go`

```go
package auth

import (
	"context"
	"testing"
	"time"
)

func TestTraeCNAuthenticatorProvider(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if got := a.Provider(); got != "trae-cn" {
		t.Fatalf("Provider() = %q, want trae-cn", got)
	}
}

func TestTraeCNAuthenticatorRefreshLead(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if got := a.RefreshLead(); got != 30*time.Minute {
		t.Fatalf("RefreshLead() = %v, want 30m", got)
	}
}

func TestTraeCNAuthenticatorLoginNilConfig(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if _, err := a.Login(context.Background(), nil, &LoginOptions{}); err == nil {
		t.Fatal("expected error for nil config")
	}
}
```

## Step 2: 实现 `sdk/auth/traecn.go`

`TraeCNAuthenticator` 结构 + `NewTraeCNAuthenticator()`，方法：

- `Provider() string { return "trae-cn" }`
- `RefreshLead() time.Duration { return 30 * time.Minute }`
- `Login(ctx, cfg, opts) (*coreauth.Auth, error)`：
  1. cfg nil → error
  2. `fp := traecn.NewDeviceFingerprint()`
  3. 端口：`opts.CallbackPort > 0` 用之，否则 `traecn.DefaultCallbackPort`；起 `traecn.NewOAuthServer(port)`，失败（端口占用）→ 打印授权 URL 走纯手动粘贴
  4. 打印授权 URL `traecn.BuildAuthorizeURL(fp, port)`；`opts.NoBrowser` 为 false 时尝试开浏览器（复用 claude.go 的开浏览器辅助方式）
  5. **双通道等待**（镜像 claude.go）：`callbackCh` 跑 `server.WaitForCallback(ctx)` goroutine；15 秒 timer 后调 `misc.AsyncPrompt(opts.Prompt, "Paste the login success URL here: ")` 到 `manualCh`；`select` 竞速；ctx 取消 → 停 server 返回错误
  6. 从结果 params 取 token 链：
     - `params["token"|"access_token"|"ide_token"]` 非空 → 直接用作 AccessToken
     - 否则 `params["refresh_token"]` 非空 → `auth.ExchangeToken(ctx, traecn.ClientID, rt)`
     - 否则 → error 含 `paramKeys(params)`（所有键名列表，便于排查真实回调格式）
  7. `storage := traecn.CreateTokenStorage(td, fp, email, expireMs)`；email 来源顺序：`opts.Metadata["email"]` → `opts.Metadata["alias"]` → td.UserID → 时间戳
  8. label 同 email 解析顺序；`id := "trae-cn-" + label + ".json"`
  9. metadata 必写 `expires_at`：`time.UnixMilli(expireMs).UTC().Format(time.RFC3339)`（`Auth.ExpirationTime()` 只读这个键）
  10. 返回 `&coreauth.Auth{ID: id, Provider: "trae-cn", Label: label, Storage: storage, Metadata: map[string]any{...}}`
- 辅助函数：`firstNonEmptyParam(params map[string]string, keys ...string) string`、`paramKeys(params map[string]string) []string`（排序）

## Step 3: 验证

**本机无 Go 工具链，无法跑 go test/go build。** 改为：
- 仔细静态自查：import 完整、签名与 Task 1 包一致（`traecn.NewDeviceFingerprint()` 返回 `DeviceFingerprint` 值、`traecn.BuildAuthorizeURL(fp, port)` 包级函数、`traecn.NewTraeCNAuth(cfg)` 返回 `*TraeCNAuth`、`ExchangeToken(ctx, clientID, refreshToken)` 方法、`traecn.CreateTokenStorage(td, fp, email, expireMs)`、`traecn.NewOAuthServer(port)` 镜像 iflow 的 Start/Stop/WaitForCallback）
- 对照 `sdk/auth/claude.go` 的双通道循环确保 channel/select/timer 逻辑一致
- 确认 `coreauth.Auth` 字段名与 `sdk/auth/qoder.go` 用法一致
- gofmt 无法跑，请手动保持 gofmt 风格（tab 缩进、import 分组：标准库/第三方/本项目）

## Step 4: Commit

**不要执行任何 git 命令。** 在报告里给出 git 命令文本：

```bash
git add sdk/auth/traecn.go sdk/auth/traecn_test.go
git commit -m "feat(auth): add trae-cn SDK authenticator with dual-channel login"
```
