# Task 3 Report: CLI 接线

## 实现内容

将 `trae-cn` provider 完整接入 CLI，镜像现有 `qoder-cn` 的接线模式。

### 新建文件

**`internal/cmd/traecn_login.go`**
- `func DoTraeCNLogin(cfg *config.Config, options *LoginOptions)` — 签名与 `DoQoderCNLogin` 完全一致
- 构造 `newAuthManager()`，默认 promptFn 用内联 `fmt.Scanln`（照抄 qoder 系列写法）
- 调 `manager.Login(context.Background(), "trae-cn", cfg, authOpts)`
- 错误处理：`errors.AsType[*sdkAuth.EmailRequiredError]` + 失败提示 + 成功提示（"Trae CN authentication failed/successful"）
- import：context / errors / fmt + internal/config + sdkAuth + logrus（与 qoder_cn_login.go 一致）

### 修改文件

**`internal/cmd/auth_manager.go`**（L30）
- 在 `sdkAuth.NewQoderCNAuthenticator(),` 之后新增 `sdkAuth.NewTraeCNAuthenticator(),`

**`sdk/auth/refresh_registry.go`**（L22）
- 在 `registerRefreshLead("qoder-cn", ...)` 之后新增 `registerRefreshLead("trae-cn", func() Authenticator { return NewTraeCNAuthenticator() })`
- 不加此行 token 永不自动刷新（`TraeCNAuthenticator.RefreshLead()` 返回 30min lead）

**`cmd/server/main.go`** — 6 触点全部修改：
| # | 位置 | 内容 |
|---|------|------|
| 1 | L110 struct `commandModeOptions` | `traeCNLogin        bool`（紧挨 `qoderCNLogin`，对齐到同列） |
| 2 | L139 `isOneShotCommandMode` | `opts.qoderCNLogin \|\|`<br>`opts.traeCNLogin`（末项无 `\|\|`） |
| 3 | L177 flag 变量区 | `var traeCNLogin bool` |
| 4 | L222 flag 注册 | `flag.BoolVar(&traeCNLogin, "trae-cn-login", false, "Login to Trae CN using browser OAuth")` |
| 5 | L735 opts 字面量 | `traeCNLogin:        traeCNLogin,`（对齐） |
| 6 | L887 dispatch 分支 | `} else if traeCNLogin { cmd.DoTraeCNLogin(cfg, options) }`（位于 qoderCNLogin 分支之后、else 之前） |

**`cmd/server/main_test.go`**（L123）
- 测试表新增：`{name: "trae-cn login", opts: commandModeOptions{traeCNLogin: true}, want: true},`

## 静态自查结果（Step 6）

本机无 Go 工具链，已按简报做静态自查，全部通过：

- ✅ `DoTraeCNLogin` 签名与 `DoQoderCNLogin` 一致：`func DoXxxLogin(cfg *config.Config, options *LoginOptions)`（无返回值）
- ✅ 6 触点全部改到（grep 验证 L110/139/177/222/735/887 均含 `traeCNLogin`）
- ✅ import 路径与 qoder_cn_login.go 完全一致
- ✅ gofmt 风格：tab 缩进；struct 字段对齐（`traeCNLogin` 11 字符 + 8 空格，与 `qoderCNLogin` 12 + 7 同列对齐 `bool`）；opts 字面量对齐一致
- ✅ dispatch 分支位置正确（qoderCNLogin 之后、else 兜底之前），与 qoder-cn 模式一致
- ✅ provider 字符串 `"trae-cn"` 与 Task 2 `TraeCNAuthenticator.Provider()` 返回值一致
- ✅ 构造器名 `NewTraeCNAuthenticator` 与 Task 2 `sdk/auth/traecn.go` L24 一致
- ✅ 无 `log.Fatal`/`log.Fatalf`；注释全英文；用户可见字符串语言与相邻代码一致（英文）

## 关注点

1. **默认 promptFn 用内联 `fmt.Scanln` 而非简报提到的 `misc.PromptForInput`**：简报 Step 1 括号里说"照抄 qoder 的默认 prompt 写法"，而 qoder_login.go / qoder_cn_login.go 的实际写法就是内联 `fmt.Scanln`，故照抄该写法。`misc.PromptForInput` 仅在 Task 2 的 `sdk/auth/traecn.go` 内部经 `misc.AsyncPrompt` 使用，CLI 层透传 `options.Prompt` 即可，二者不冲突。
2. **未跑 `go build`/`go test`/`gofmt`**：本机无 Go 工具链。以上仅为静态自查，建议在有工具链的环境执行 `gofmt -w . && go build -o cli-proxy-api ./cmd/server && go test ./cmd/server/` 复核。
3. **flag 帮助文案**：用 "Login to Trae CN using browser OAuth"（Trae CN 为浏览器 OAuth + 手动粘贴回退，区别于 qoder-cn 的 PAT），与简报 Step 4.4 一致。

## Git 命令文本（Step 7，未执行，待人工确认）

```bash
git add internal/cmd/traecn_login.go internal/cmd/auth_manager.go sdk/auth/refresh_registry.go cmd/server/main.go cmd/server/main_test.go
git commit -m "feat(cli): wire up --trae-cn-login flag and refresh registry"
```
