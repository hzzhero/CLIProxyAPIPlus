# Task 5 Report: 注册点改动（5 处）

**Status:** DONE

## What was implemented

Four registration points for the `trae-cn` provider, each mirroring the existing `qoder-cn` template:

### 1. `sdk/cliproxy/service.go` — executor registry + model resolver (2 cases)

- **Executor registry** (line 1137, after `case "qoder-cn"`): added
  `case "trae-cn": s.coreManager.RegisterExecutor(executor.NewTraeCNExecutor(s.cfg))`
- **Model resolver** (line 2119, after the qoder-cn fetch branch): added
  `case "trae-cn": models = executor.FetchTraeCNModels(context.Background(), a, s.cfg); models = applyExcludedModels(models, excluded)`
  — argument signature matches `FetchQoderModels(context.Background(), a, s.cfg)` exactly.

### 2. `sdk/auth/filestore.go` — token-storage deserialization branch

- Added import `traecnauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"` (line 18).
- Added a parallel `if provider == "trae-cn"` block (line 363) after the existing qoder block, using the same `json.Marshal(metadata)` → `json.Unmarshal(raw, &storage)` → `strings.TrimSpace(storage.Type)` guard → `auth.Storage = &storage` pattern. Variable names (`raw`, `errMarshal`, `errUnmarshal`, `storage`) and structure match the qoder branch byte-for-byte.

### 3. `internal/watcher/synthesizer/file.go` — hot-reload deserialization branch

- Added import `traecnauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"` (line 16).
- Added a parallel `if provider == "trae-cn"` block (line 230) after the existing qoder block, using the same `json.Unmarshal(data, &storage)` → `storage.Type` guard → `a.Storage = &storage` pattern. Without this branch, hot-reloads would produce `Storage == nil`.

### 4. `internal/registry/model_definitions.go` — static model registry

- Added `case "trae-cn": return GetTraeCNModels()` in `GetStaticModelDefinitionsByChannel` (line 321), after `case "qoder-cn"`.
- Added `GetTraeCNModels() []*ModelInfo` at end of file (line 904), returning an empty slice per brief (the authoritative list comes from `executor.FetchTraeCNModels`; this is only the fallback). Return type `[]*ModelInfo` matches `GetQoderCNModels`.

## Static-review findings (no Go toolchain available)

- All 4 files modified; 5 registration points total (2 in service.go, 1 in filestore.go, 1 in synthesizer/file.go, 2 in model_definitions.go).
- Every new branch is placed immediately after its `qoder-cn` counterpart and uses identical structure/variable names.
- Import `traecnauth` added to both `sdk/auth/filestore.go` and `internal/watcher/synthesizer/file.go`; goimports-style grouping preserved (alphabetical within the third-party block).
- `registry.GetTraeCNModels()` is now defined, resolving the dangling reference from Task 4's `FetchTraeCNModels` fallback.
- Tab indentation used throughout (matches gofmt); no trailing whitespace introduced.
- `executor.NewTraeCNExecutor` and `executor.FetchTraeCNModels` confirmed to exist in `internal/runtime/executor/traecn_executor.go` with matching signatures (`cfg *config.Config` and `(ctx, auth *cliproxyauth.Auth, cfg *config.Config)` respectively).
- `traecn.TraeCNTokenStorage` confirmed in `internal/auth/traecn/trae_token.go` with `Type string` field, compatible with the deserialization guard.

## Concerns

None. The empty-slice fallback in `GetTraeCNModels()` is intentional per the brief; if the upstream model_list call fails and no cached models exist, the provider will present zero models until a successful fetch.

## Git commands (for user to run manually)

```bash
git add sdk/cliproxy/service.go sdk/auth/filestore.go internal/watcher/synthesizer/file.go internal/registry/model_definitions.go
git commit -m "feat: register trae-cn provider across service, filestore, watcher, registry"
```
