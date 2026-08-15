# Task 5 Brief: 注册点改动（5 处）

**Files:**
- Modify: `sdk/cliproxy/service.go`
- Modify: `sdk/auth/filestore.go`
- Modify: `internal/watcher/synthesizer/file.go`
- Modify: `internal/registry/model_definitions.go`

模板：所有改动都镜像现有 `qoder-cn` 的对应位置，先 grep/read 找到它们。

## Step 1: `sdk/cliproxy/service.go` 两处 case

先找现有 `case "qoder-cn"` 的两处位置（约 line 1133-1136 executor 注册、line 2111-2116 模型解析），各加一个 case。executor 注册处：

```go
	case "trae-cn":
		s.coreManager.RegisterExecutor(executor.NewTraeCNExecutor(s.cfg))
```

模型解析处：

```go
	case "trae-cn":
		models = executor.FetchTraeCNModels(ctx, auth, s.cfg)
		models = applyExcludedModels(models, excluded)
```

（确切代码以现有 qoder-cn case 的变量名为准；FetchTraeCNModels 的实参签名照抄 FetchQoderModels 对应调用。）

## Step 2: `sdk/auth/filestore.go` 反序列化分支

先找 line 351 附近 `if provider == "qoder" || provider == "qoder-cn"` 的分支，照着加 trae-cn 分支：

```go
	} else if provider == "trae-cn" {
		var storage traecn.TraeCNTokenStorage
		if err := json.Unmarshal(raw, &storage); err != nil {
			return nil, fmt.Errorf("failed to parse trae-cn token storage: %w", err)
		}
		if storage.Type == "" {
			storage.Type = provider
		}
		auth.Storage = &storage
	}
```

（精确结构以现有 qoder 分支为准——变量名/raw 来源/错误格式照抄；import 加 `internal/auth/traecn`。）

## Step 3: `internal/watcher/synthesizer/file.go` 反序列化分支

line 214 附近同型位置，与 Step 2 相同的分支（**漏掉此分支会导致热重载后 Storage==nil**）。

## Step 4: `internal/registry/model_definitions.go`

1. `GetStaticModelDefinitionsByChannel` switch（qoder-cn 约 line 319-320）加：

```go
	case "trae-cn":
		return GetTraeCNModels()
```

2. 文件尾部（`GetQoderCNModels` 附近，约 line 886）加：

```go
// GetTraeCNModels returns the static fallback model list for trae-cn.
// The authoritative list is fetched dynamically via executor.FetchTraeCNModels;
// this fallback only applies when the upstream model_list call fails.
func GetTraeCNModels() []*ModelInfo {
	return []*ModelInfo{}
}
```

（返回类型以该文件里 `GetQoderCNModels` 的实际返回类型为准——若它返回 `[]*ModelInfo` 就用它；若返回别的类型，保持一致。）

## Step 5: 验证

**本机无 Go 工具链，无法跑 go build。** 改为静态自查：
- 4 个文件全部改到；每处都与 qoder-cn 对应位置对齐
- import 加了 `internal/auth/traecn`（filestore.go 和 synthesizer/file.go）
- `registry.GetTraeCNModels()` 现在已定义（消除 Task 4 的悬空引用）
- gofmt 风格

## Step 6: Commit

**不要执行任何 git 命令。** 在报告里给出 git 命令文本：

```bash
git add sdk/cliproxy/service.go sdk/auth/filestore.go internal/watcher/synthesizer/file.go internal/registry/model_definitions.go
git commit -m "feat: register trae-cn provider across service, filestore, watcher, registry"
```
