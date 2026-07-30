# Plan: Add Agnes (CN) Authentication Support

## Summary

Add `-agnes-login` flag to support Agnes AI China edition (`https://app.agnes-ai.cn/`) authentication. Agnes uses a static `sk-...` API key (Bearer token) against an OpenAI-compatible API at `https://apihub.agnes-ai.cn/v1`. No OAuth, no token exchange, no refresh — the API key is the entire credential lifecycle.

**Key insight**: Since Agnes is fully OpenAI-compatible, we reuse the existing `OpenAICompatExecutor` — no new executor or translator needed. The implementation is much lighter than qoder-cn.

## Current State Analysis

### Agnes API (from research)

- **Base URL (CN)**: `https://apihub.agnes-ai.cn/v1`
- **Auth**: `Authorization: Bearer sk-...` (static, no expiry, no refresh)
- **Endpoints**: `/v1/chat/completions`, `/v1/models` (OpenAI-compatible)
- **Models**: `agnes-2.0-flash` (512K), `agnes-2.5-flash`, `agnes-2.5-pro-alpha` (1M)
- **No login/token-exchange/user-info endpoints** — the API key is created manually in the dashboard at `https://platform.agnes-ai.cn/`

### Reusable infrastructure

- `OpenAICompatExecutor` (`internal/runtime/executor/openai_compat_executor.go:39`) — stateless executor that reads `base_url` and `api_key` from `auth.Attributes`, sets `Authorization: Bearer`, and routes to `<baseURL>/chat/completions`
- `openAICompatInfoFromAuth` (`sdk/cliproxy/service.go:835`) — detects compat providers via `auth.Attributes["compat_name"]`
- GitLab PAT login pattern (`sdk/auth/gitlab.go:229`) — reference for prompt/env/metadata API key input

### Critical loading-path detail

The filestore (`sdk/auth/filestore.go:330`) and synthesizer (`internal/watcher/synthesizer/file.go:183`) construct `auth.Attributes` with only `path`/`source`/`source_backend`/`email` from the metadata. The `base_url` and `api_key` fields are NOT automatically copied from metadata to Attributes. We must add a branch for "agnes" to copy these fields, otherwise `resolveCredentials` will return empty strings after reload.

## Proposed Changes

### 1. `sdk/auth/agnes.go` (NEW) — AgnesAuthenticator

Implements the `Authenticator` interface (`sdk/auth/interfaces.go:25`).

```go
type AgnesAuthenticator struct{}

func NewAgnesAuthenticator() Authenticator { return &AgnesAuthenticator{} }
func (AgnesAuthenticator) Provider() string { return "agnes" }
func (AgnesAuthenticator) RefreshLead() *time.Duration { return nil } // static key, no refresh
```

**Login flow** (mirrors GitLab `loginPAT` pattern):
1. Resolve API key from `opts.Metadata["api_key"]` → env `AGNES_API_KEY` / `AGNES_PERSONAL_ACCESS_TOKEN` → interactive prompt
2. Resolve base URL from `opts.Metadata["base_url"]` → env `AGNES_BASE_URL` → default `https://apihub.agnes-ai.cn/v1`
3. Optionally validate the key by calling `GET <baseURL>/models` (best-effort; on failure, log warning and proceed)
4. Build metadata map: `type`, `api_key`, `base_url`, `compat_name`, `provider_key`, `auth_kind`
5. Build Attributes map: `api_key`, `base_url`, `compat_name`, `provider_key` — so `OpenAICompatExecutor.resolveCredentials` and `openAICompatInfoFromAuth` work immediately without filestore post-processing
6. File name: `agnes-<label>.json` where label = email (if available from /models validation) or `user-<timestamp>`
7. Return `&coreauth.Auth{Provider: "agnes", Storage: nil, Metadata: metadata, Attributes: attributes}`

**No Storage struct** — the API key is stored in the metadata map and copied to Attributes. The filestore saves `auth.Metadata` as JSON directly (the `auth.Storage == nil` path at `filestore.go:120`).

### 2. `internal/cmd/agnes_login.go` (NEW) — DoAgnesLogin

Mirrors `internal/cmd/xai_login.go` pattern. Calls `manager.Login(ctx, "agnes", cfg, authOpts)`.

### 3. `internal/cmd/auth_manager.go` (MODIFY) — Register AgnesAuthenticator

Add `sdkAuth.NewAgnesAuthenticator()` to the `NewManager(...)` argument list in `newAuthManager()`.

### 4. `sdk/auth/refresh_registry.go` (MODIFY) — Register agnes refresh

Add `registerRefreshLead("agnes", func() Authenticator { return NewAgnesAuthenticator() })`. `RefreshLead()` returns nil so the scheduler never schedules a refresh.

### 5. `cmd/server/main.go` (MODIFY) — Add `-agnes-login` flag

7 touchpoints (same pattern as qoder-cn):
1. `commandModeOptions` struct: add `agnesLogin bool`
2. `isOneShotCommandMode`: add `opts.agnesLogin`
3. `var agnesLogin bool` declaration
4. `flag.BoolVar(&agnesLogin, "agnes-login", ...)` registration
5. opts literal: `agnesLogin: agnesLogin`
6. dispatch: `} else if agnesLogin { cmd.DoAgnesLogin(cfg, options) }`

### 6. `internal/registry/model_definitions.go` (MODIFY) — Static models

Add `case "agnes": return GetAgnesModels()` to `GetModelsByProvider` switch (around line 320).

Add `GetAgnesModels()` function returning hardcoded `[]*ModelInfo`:
```go
func GetAgnesModels() []*ModelInfo {
    return []*ModelInfo{
        {ID: "agnes/agnes-2.0-flash", Object: "model", OwnedBy: "agnes", Type: "agnes", DisplayName: "Agnes 2.0 Flash", ContextLength: 512000},
        {ID: "agnes/agnes-2.5-flash", Object: "model", OwnedBy: "agnes", Type: "agnes", DisplayName: "Agnes 2.5 Flash", ContextLength: 512000},
        {ID: "agnes/agnes-2.5-pro-alpha", Object: "model", OwnedBy: "agnes", Type: "agnes", DisplayName: "Agnes 2.5 Pro Alpha", ContextLength: 1000000},
    }
}
```

No `Agnes` field added to the `modelDefinitions` struct — these are standalone builtins (no models.json dependency).

### 7. `sdk/cliproxy/service.go` (MODIFY) — Executor + model registration

**`baselineExecutorAuths()` (line 1019)**: Add `"agnes"` to the providers list.

**`registerExecutorForAuth` (line 1099 switch)**: Add:
```go
case "agnes":
    s.coreManager.RegisterExecutor(executor.NewOpenAICompatExecutor("agnes", s.cfg))
```
This is AFTER the `openAICompatInfoFromAuth` early-return. Since we set `compat_name` in Attributes at login time, the early-return will actually handle it. But for the `baselineExecutorAuths` path (where Attributes are empty), we need this switch case as fallback.

**Model registration switch (line 1989)**: Add:
```go
case "agnes":
    models = registry.GetAgnesModels()
    models = applyExcludedModels(models, excluded)
```

### 8. `sdk/auth/filestore.go` (MODIFY) — Copy api_key/base_url to Attributes on load

After the `email` copy block (around line 350), add:
```go
if provider == "agnes" {
    if v, ok := metadata["api_key"].(string); ok && v != "" {
        auth.Attributes["api_key"] = v
    }
    if v, ok := metadata["base_url"].(string); ok && v != "" {
        auth.Attributes["base_url"] = v
    }
    if v, ok := metadata["compat_name"].(string); ok && v != "" {
        auth.Attributes["compat_name"] = v
    } else {
        auth.Attributes["compat_name"] = "agnes"
    }
    if v, ok := metadata["provider_key"].(string); ok && v != "" {
        auth.Attributes["provider_key"] = v
    } else {
        auth.Attributes["provider_key"] = "agnes"
    }
}
```

### 9. `internal/watcher/synthesizer/file.go` (MODIFY) — Same as filestore

After the qoder/qoder-cn block (around line 229), add the same `if provider == "agnes"` block to copy `api_key`/`base_url`/`compat_name`/`provider_key` from metadata to `a.Attributes`.

### 10. `cmd/server/main_test.go` (MODIFY) — Test case

Add `{name: "agnes login", opts: commandModeOptions{agnesLogin: true}, want: true}` to the `isOneShotCommandMode` test table.

## Assumptions & Decisions

1. **Provider key**: `"agnes"` (not `"agnes-cn"`) — the user asked for the CN version, but since the international and CN versions use the same API keys and only differ in base URL, we default to the CN base URL and allow override. This keeps the provider key simple.
2. **No custom Storage struct** — the API key is stored in the metadata map. The filestore's `auth.Storage == nil` path handles persistence directly.
3. **No refresh** — `RefreshLead()` returns nil. Static API keys never expire.
4. **Reuse `OpenAICompatExecutor`** — no new executor or translator. The executor is registered via the switch case in `registerExecutorForAuth` and via `openAICompatInfoFromAuth` detection.
5. **Static models** — hardcoded in `GetAgnesModels()`. No dynamic model fetching (keeps implementation simple; can be added later if needed).
6. **Best-effort key validation** — at login time, optionally call `GET <baseURL>/models` to validate the key and fetch the user's email/label. On failure, log a warning and proceed (the key might still work for chat).
7. **Base URL default**: `https://apihub.agnes-ai.cn/v1` (CN). Overridable via `AGNES_BASE_URL` env or `Metadata["base_url"]`.

## Verification

```bash
gofmt -w .
go build -o test-output ./cmd/server && rm test-output
go test ./internal/registry/... ./sdk/auth/... ./sdk/cliproxy/... ./cmd/server/...
```

Manual smoke test:
```bash
# Login with API key
./cli-proxy-api -agnes-login
# Enter sk-... when prompted

# Verify auth file created
ls auths/agnes-*.json

# Start server and test chat
./cli-proxy-api
curl http://localhost:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"agnes/agnes-2.0-flash","messages":[{"role":"user","content":"Hello"}],"stream":false}'
```
