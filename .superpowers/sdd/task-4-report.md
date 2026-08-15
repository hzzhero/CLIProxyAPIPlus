# Task 4 Report: Executor `internal/runtime/executor/traecn_executor.go`

## What was done

Created two files:

1. **`internal/runtime/executor/traecn_executor.go`** (~700 lines) — the Trae CN runtime executor:
   - `TraeCNExecutor` struct + `NewTraeCNExecutor(cfg)` + `Identifier()` returning `"trae-cn"`
   - `PrepareRequest(req, auth) error` — injects device fingerprint headers via `applyTraeCNHeaders`
   - `HttpRequest(ctx, auth, req) (*http.Response, error)` — wraps `PrepareRequest` + proxy-aware client (timeout=0)
   - `Execute(ctx, auth, req, opts) (Response, error)` — translates non-OpenAI source format up-front, runs `ExecuteStream` with `SourceFormat=FormatOpenAI`, accumulates `content`/`reasoning_content`/`finish_reason`/`usage`, then `TranslateNonStream` back to the client's format
   - `ExecuteStream(ctx, auth, req, opts) (*StreamResult, error)` — asserts `*traecn.TraeCNTokenStorage`, translates payload to OpenAI, builds body via `buildTraeCNChatBody`, POSTs to `traecn.ChatURL` with `Accept: text/event-stream`, full device fingerprint headers, no timeout after connect. Parses SSE `event:`/`data:` pairs; `metadata` events are skipped, `output`/`token_usage`/`error` events map to OpenAI chunks via `traeCNEventToChunk` and flow through `sdktranslator.TranslateStream` + `helps.ParseOpenAIStreamUsage`; `done` triggers the package-level `emitDone` (reused from `qoder_executor.go`) + `[DONE]`. Non-200 responses are read with a 4KB limit and returned as `traeCNStatusError` (implements `StatusCode()`); 401/403 additionally log the `--trae-cn-login` hint.
   - `Refresh(ctx, auth) (*Auth, error)` — returns auth unchanged (with `log.Debug`) when no refresh token; otherwise calls `traecn.NewTraeCNAuth(e.cfg).ExchangeToken(ctx, traecn.ClientID, storage.RefreshToken)`, deep-copies storage (`cp := *storage`), updates `Token`/`RefreshToken`/`ExpireTime`/`LastRefresh`, sets `updated.Metadata["expires_at"]`. Does **not** call `SaveTokenToFile`.
   - `FetchTraeCNModels(ctx, auth, cfg) []*registry.ModelInfo` — GET `traecn.ModelListURL + "?type=llm_raw_chat"` with full fingerprint headers, 15s timeout (credential-acquisition phase). Parses `model_configs[].name` into `&registry.ModelInfo{ID: "trae-cn/"+name, OwnedBy: "trae-cn", Type: "trae-cn"}`. Every failure path logs `log.Warn` and falls back to `registry.GetTraeCNModels()`.
   - Helpers: `applyTraeCNHeaders` (x-app-id/x-ide-token always set; the 9 device fingerprint headers only when non-empty), `buildTraeCNChatBody` (strips `trae-cn/` prefix, `status:"success"`/`locale:"zh-cn"` on history, `current_turn = userTurns-1` floored at 0, `valid_turns = current_turn+1`, UUID conversation/session IDs, `variables` as a JSON string with `current_time` in RFC3339), `traeCNContentText` (string passthrough; `[]any` keeps only `type=="text"` parts), `traeCNEventToChunk` + `marshalTraeCNChunk`, `traeCNStatusError`.
   - Usage reporting wired via `helps.NewExecutorUsageReporter` / `TrackHTTPClient` / `Publish` / `PublishFailure` / `EnsurePublished`, mirroring the qoder executor.

2. **`internal/runtime/executor/traecn_executor_test.go`** — verbatim from the brief: `TestTraeCNExecutorIdentifier`, `TestBuildTraeCNChatBody`, `TestTraeCNEventToChunk`.

## Static review findings (Step 3 — no Go toolchain available)

- **Method signature parity** with qoder executor confirmed for all five methods: `PrepareRequest(*http.Request, *Auth) error`, `HttpRequest(ctx, *Auth, *http.Request) (*http.Response, error)`, `Execute(ctx, *Auth, Request, Options) (Response, error)`, `ExecuteStream(ctx, *Auth, Request, Options) (*StreamResult, error)`, `Refresh(ctx, *Auth) (*Auth, error)`.
- **No duplicate `emitDone`** — package-level function defined once in `qoder_executor.go:527`, called (not redefined) from the new file. Grep confirmed exactly one definition in the package.
- **No identifier collisions** — grep verified `setIfNotEmpty`, `traecnStorage`, `authStorage`, `applyTraeCNHeaders`, `buildTraeCNChatBody`, `traeCNContentText`, `traeCNEventToChunk`, `marshalTraeCNChunk`, `newTraeCNStatusError`, `FetchTraeCNModels`, `NewTraeCNExecutor` are unique in package `executor`. `truncate` is reused from `qoder_executor.go`.
- **Imports** — aligned with qoder executor (`bufio/bytes/context/encoding/json/errors/fmt/io/net/http/strings/time`, uuid, config, registry, helps, cliproxyauth, cliproxyexecutor, sdktranslator, logrus, gjson) plus new `traecn "...internal/auth/traecn"`. `gjson` is used only in `FetchTraeCNModels`; `errors` only there too (context.Canceled/DeadlineExceeded check). All imports are used.
- **Refresh deep-copy** — `cp := *storage` before mutation, `updated.Storage = &cp`, no `SaveTokenToFile`. Guard against empty `tokenData.RefreshToken` so a partial upstream response cannot wipe the stored refresh token.
- **Timeouts** — only `FetchTraeCNModels` uses 15s (ctx + http client), permitted as credential-acquisition. Chat path passes timeout 0.
- **Logging** — logrus only; no tokens in log fields; non-200 body truncated to 300 chars after a 4KB read cap.
- **gofmt** — tabs throughout, aligned struct fields (fixed one misaligned pair in `traeCNEventToChunk`), goimports-style grouping.
- **`registry.GetTraeCNModels()`** does not exist yet (Task 5); referenced per brief. Package will not compile until Task 5 lands — expected.

## Concerns

1. `registry.GetTraeCNModels()` is not yet defined — compilation of this package depends on Task 5 (per the brief's explicit instruction to reference it anyway).
2. `TokenData.ExpiresIn` semantics (seconds vs ms) were inferred from the qoder pattern (`ExpireTime` is ms epoch in storage, `ExpiresIn` treated as seconds). If the upstream returns ms, expiry would be scheduled far in the future. Worth validating with a live capture during integration testing.
3. Could not run `go build`/`go test` (no Go toolchain on this machine) — verification was static only.

## Git command text (for user to run manually — NOT executed)

```bash
git add internal/runtime/executor/traecn_executor.go internal/runtime/executor/traecn_executor_test.go
git commit -m "feat(executor): add trae-cn executor with SSE translation and token refresh"
```
