# Task 3 Brief: CLI 接线

**Files:**
- Create: `internal/cmd/traecn_login.go`
- Modify: `internal/cmd/auth_manager.go`
- Modify: `sdk/auth/refresh_registry.go`
- Modify: `cmd/server/main.go`
- Modify: `cmd/server/main_test.go`

模板（写代码前先读）：
- `internal/cmd/qoder_login.go` — `DoQoderLogin` 完整模式（构造 manager、调 Login、默认 promptFn）
- `cmd/server/main.go` 现有 `qoderCNLogin` 的 6 个触点（约 line 109/137/174/218/730/880）
- `internal/cmd/auth_manager.go` 认证器列表（约 line 28-29）
- `sdk/auth/refresh_registry.go` registerRefreshLead 注册（约 line 20-21）

## Step 1: `internal/cmd/traecn_login.go`

镜像 `internal/cmd/qoder_login.go` 的 `DoQoderLogin`：`func DoTraeCNLogin(cfg *config.Config, options *LoginOptions)` — 构造 manager、调 `manager.Login(ctx, "trae-cn", ...)`，默认 promptFn 用 `misc.PromptForInput`（照抄 qoder 的默认 prompt 写法）。注意签名与 qoder_login 完全一致（返回值/参数列表照抄）。

## Step 2: `internal/cmd/auth_manager.go`

在认证器列表（现有 `sdkAuth.NewQoderCNAuthenticator(),` 之后）加一行：

```go
		sdkAuth.NewTraeCNAuthenticator(),
```

## Step 3: `sdk/auth/refresh_registry.go`

在现有 `registerRefreshLead("qoder-cn", ...)` 之后加：

```go
	registerRefreshLead("trae-cn", func() Authenticator { return NewTraeCNAuthenticator() })
```

不加此行则 token 永不自动刷新。

## Step 4: `cmd/server/main.go`（6 触点，参照 qoderCNLogin 现有位置）

1. struct `commandModeOptions` 加字段：`traeCNLogin bool`（紧挨 `qoderCNLogin`）
2. `isOneShotCommandMode`：`|| opts.traeCNLogin`
3. flag 变量区：`var traeCNLogin bool`
4. flag 注册：`flag.BoolVar(&traeCNLogin, "trae-cn-login", false, "Login to Trae CN using browser OAuth")`
5. opts 字面量：`traeCNLogin: traeCNLogin,`
6. dispatch 分支：`} else if opts.traeCNLogin { cmd.DoTraeCNLogin(cfg, options) }`（位置镜像 qoder-cn 分支）

## Step 5: `cmd/server/main_test.go` 测试表加行

```go
		{name: "trae-cn login", opts: commandModeOptions{traeCNLogin: true}, want: true},
```

## Step 6: 验证

**本机无 Go 工具链，无法跑 go test/go build。** 改为静态自查：
- `DoTraeCNLogin` 签名与 `DoQoderLogin` 一致
- 6 触点全部改到（struct 字段/isOneShotCommandMode/flag var/BoolVar/opts 字面量/dispatch）
- import 路径与 qoder_login.go 一致
- gofmt 风格（tab 缩进、字段对齐与相邻行一致）

## Step 7: Commit

**不要执行任何 git 命令。** 在报告里给出 git 命令文本：

```bash
git add internal/cmd/traecn_login.go internal/cmd/auth_manager.go sdk/auth/refresh_registry.go cmd/server/main.go cmd/server/main_test.go
git commit -m "feat(cli): wire up --trae-cn-login flag and refresh registry"
```
