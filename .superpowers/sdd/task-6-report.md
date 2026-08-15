# Task 6 Report — trae-cn Code Review Fixes (C1, I1, I2, I3)

Machine has no Go toolchain: all verification is static (read/grep), no `go build`/`go test` run.

## C1 (Critical) — TraeCNExecutor missing CountTokens — FIXED

- Added `CountTokens` to `internal/runtime/executor/traecn_executor.go` (inserted just before `ExecuteStream`, after `Execute`), mirroring `qoder_executor.go:564-603` body-for-body: translate non-openai SourceFormat payloads to OpenAI via `sdktranslator.TranslateRequest`, unmarshal, sum `extractContentGeneric(msg["content"])` length over `messages`, estimate `totalChars/4` (min 1), return `{"usage":{prompt_tokens,completion_tokens:0,total_tokens}}` as `cliproxyexecutor.Response{Payload: ...}`.
- Signature verified verbatim against the interface in `sdk/cliproxy/auth/conductor.go:47`: `CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error)` — inside package `auth`, `*Auth` == `*cliproxyauth.Auth`, matching the executor file's import alias.
- Added compile-time assertion at `traecn_executor.go:33-34`: `var _ cliproxyauth.ProviderExecutor = (*TraeCNExecutor)(nil)` (interface name/package confirmed in conductor.go:36).
- `extractContentGeneric` reused from same package (`qoder_executor.go:429`); no new imports required (`context`, `encoding/json`, `sdktranslator`, `cliproxyauth`, `cliproxyexecutor` all already imported).

## I1 (Important) — goroutine leak in manual-paste path — FIXED

- `internal/auth/traecn/oauth_server.go:98-113`: `WaitForCallback` now takes `ctx context.Context` as first param and selects on `ctx.Done()` (returns `ctx.Err()`), alongside result/errChan/timeout. Doc comment updated.
- `sdk/auth/traecn.go:106-129`: Login derives `callbackCtx, cancelCallback := context.WithCancel(ctx)` with `defer cancelCallback()`, passes `callbackCtx` to `WaitForCallback`. The goroutine swallows the error when `callbackCtx.Err() != nil` (normal shutdown once Login returns via manual paste), so no spurious "callback wait failed" after a successful login; genuine wait errors still reach `callbackErrCh`.
- Only caller of the traecn `WaitForCallback` is `sdk/auth/traecn.go` (grep-verified; iflow/claude/codex/gitlab have their own servers, untouched). No test references.
- Existing callback path unchanged: result still delivered through `s.result` channel; server `Stop` defer untouched.

## I2 (Important) — token leakage in ExchangeToken non-200 branch — FIXED

- `internal/auth/traecn/trae_auth.go:127-129`: raw `string(body)` replaced with `truncateBody(body, 200)`; status code and `resp.Status` retained.
- Added `truncateBody(body []byte, n int) string` helper at `trae_auth.go:147-155`, mirroring `qoder_auth.go:555-560` (with "..." suffix on truncation). No headers or named token fields are logged; worst case is a 200-byte prefix of an upstream error page.

## I3 (Important) — inconsistent PrepareRequest error — FIXED

- `internal/runtime/executor/traecn_executor.go:48` (in `PrepareRequest`): now `fmt.Errorf("invalid auth storage type for trae-cn: %T", authStorage(auth))`, reusing the existing `authStorage` helper and matching `ExecuteStream`'s message exactly.

## Static verification notes

- No new imports added to any file; all referenced identifiers (`context`, `fmt`, `time`, `json`, `sdktranslator`, `cliproxyauth`, `cliproxyexecutor`, `extractContentGeneric`, `authStorage`, `truncateBody`) resolve within their packages.
- Interface assertion identifier confirmed: `ProviderExecutor` lives in `sdk/cliproxy/auth` (imported as `cliproxyauth` in the executor).
- No token-bearing values logged anywhere in the diff; error paths carry status codes and truncated bodies only.
- Comments in English only; no git commands were executed (per user rule).

## Git commands (for manual confirmation, NOT executed)

```bash
git add internal/runtime/executor/traecn_executor.go sdk/auth/traecn.go internal/auth/traecn/trae_auth.go internal/auth/traecn/oauth_server.go
git commit -m "fix(trae-cn): implement CountTokens, plug goroutine leak, redact token from error"
```

Follow-up recommended on a machine with Go: `gofmt -w .` and `go build -o test-output ./cmd/server && rm test-output`.
