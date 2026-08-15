# Task 4 Brief: Executor `internal/runtime/executor/traecn_executor.go`

**Files:**
- Create: `internal/runtime/executor/traecn_executor.go`
- Test: `internal/runtime/executor/traecn_executor_test.go`

模板（写代码前先读）：`internal/runtime/executor/qoder_executor.go` — 结构、`ExecuteStream` 的 SSE→`sdktranslator.TranslateStream` 模式、`Execute` 流累积、`Refresh` 深拷贝语义（约 783-831 行）、`FetchQoderModels`（约 911-970 行）、**包级 `emitDone`（约 527 行，已存在，直接复用，不得重复定义）**。

上游协议与设备指纹来自 Task 1 的 `internal/auth/traecn/` 包：`TraeCNTokenStorage`（字段 `Token/RefreshToken/DeviceID/MachineID/DeviceBrand/DeviceCPU/DeviceType/OSVersion/IDEVersion/IDEVersionCode/IDEVersionType`）、常量 `traecn.ClientID`、`traecn.ChatURL`、`traecn.ModelListURL`。

## Step 1: 先写测试 `traecn_executor_test.go`

```go
package executor

import (
	"strings"
	"testing"
)

func TestTraeCNExecutorIdentifier(t *testing.T) {
	e := NewTraeCNExecutor(nil)
	if got := e.Identifier(); got != "trae-cn" {
		t.Fatalf("Identifier() = %q, want trae-cn", got)
	}
}

func TestBuildTraeCNChatBody(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "be helpful"},
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
		{"role": "user", "content": "how are you"},
	}
	body := buildTraeCNChatBody(messages, "trae-cn/doubao-pro")
	if body["model_name"] != "doubao-pro" {
		t.Fatalf("model_name = %v", body["model_name"])
	}
	if body["user_input"] != "how are you" {
		t.Fatalf("user_input = %v", body["user_input"])
	}
	history, ok := body["chat_history"].([]map[string]any)
	if !ok || len(history) != 3 {
		t.Fatalf("chat_history = %v", body["chat_history"])
	}
	for i, h := range history {
		if h["status"] != "success" {
			t.Fatalf("history[%d] missing status: %v", i, h)
		}
	}
	if body["intent_name"] != "general_qa_intent" {
		t.Fatalf("intent_name = %v", body["intent_name"])
	}
	if body["conversation_id"] == "" || body["session_id"] == "" {
		t.Fatal("conversation_id/session_id must be generated UUIDs")
	}
	if body["current_turn"] != 1 {
		t.Fatalf("current_turn = %v", body["current_turn"])
	}
}

func TestTraeCNEventToChunk(t *testing.T) {
	chunk := traeCNEventToChunk("output", `{"response":"hello","reasoning_content":"thinking"}`, "doubao-pro")
	if chunk == nil || !strings.Contains(string(chunk), `"content":"hello"`) {
		t.Fatalf("output chunk = %s", chunk)
	}
	if !strings.Contains(string(chunk), `"reasoning_content":"thinking"`) {
		t.Fatalf("reasoning passthrough missing: %s", chunk)
	}
	if c := traeCNEventToChunk("metadata", `{"prompt_completion_id":"x"}`, "m"); c != nil {
		t.Fatalf("metadata should be skipped, got %s", c)
	}
	usage := traeCNEventToChunk("token_usage", `{"prompt_tokens":3,"completion_tokens":5}`, "m")
	if usage == nil || !strings.Contains(string(usage), `"prompt_tokens":3`) {
		t.Fatalf("usage chunk = %s", usage)
	}
	errChunk := traeCNEventToChunk("error", `{"message":"boom"}`, "m")
	if errChunk == nil || !strings.Contains(string(errChunk), "boom") || !strings.Contains(string(errChunk), `"finish_reason":"error"`) {
		t.Fatalf("error chunk = %s", errChunk)
	}
}
```

## Step 2: 实现 `traecn_executor.go`

骨架：

```go
package executor

// TraeCNExecutor proxies OpenAI chat requests to the Trae CN IDE model API.
type TraeCNExecutor struct{ cfg *config.Config }

func NewTraeCNExecutor(cfg *config.Config) *TraeCNExecutor { return &TraeCNExecutor{cfg: cfg} }
func (e *TraeCNExecutor) Identifier() string               { return "trae-cn" }
```

方法签名必须与 qoder executor 完全一致（`PrepareRequest`/`HttpRequest`/`Execute`/`ExecuteStream`/`Refresh`，照抄 qoder 的签名与返回类型）。

实现要点：

1. `applyTraeCNHeaders(req *http.Request, s *traecn.TraeCNTokenStorage)`：`x-app-id=traecn.ClientID`、`x-ide-token=s.Token`、`x-device-id=s.DeviceID`、`x-machine-id=s.MachineID`、`x-device-brand`、`x-device-cpu`、`x-device-type`、`x-os-version`、`x-ide-version`、`x-ide-version-code`、`x-ide-version-type`（非空才设）。

2. `buildTraeCNChatBody(messages []map[string]any, model string) map[string]any`：
   - `model_name`：剥离 `trae-cn/` 前缀
   - `chat_history`：除最后一条消息，每条加 `status:"success"`、`locale:"zh-cn"`（content 用 `traeCNContentText` 拍平）
   - `user_input`：最后一条消息的文本
   - `conversation_id`/`session_id`：`uuid.NewString()`
   - `current_turn`：user 角色消息总数 − 1（下限 0）；`valid_turns` = `current_turn + 1`
   - 固定字段：`intent_name:"general_qa_intent"`、`is_preset:true`、`generate_suggested_questions:false`、`context_resolvers:[]`、`multi_media:[]`、`provider:""`
   - `variables`：JSON **字符串**（`{"locale":"zh-cn","current_time":"<RFC3339>"}`）

3. `traeCNContentText(content any) string`：`string` 直返；`[]any` content-part 取 `type=="text"` 的 `text` 拼接。

4. `traeCNEventToChunk(event, data, model string) []byte`：
   - `metadata` → nil
   - `output` → OpenAI chunk，delta `{role:"assistant", content: response, reasoning_content?}`
   - `token_usage` → chunk 带 `usage{prompt_tokens,completion_tokens,total_tokens}` + 空 choices
   - `done` → chunk `finish_reason:"stop"`，空 delta
   - `error` → chunk delta content=错误消息 + `finish_reason:"error"`
   - 未知事件 → nil

5. `ExecuteStream(ctx, auth, req, opts)`：
   - storage 断言 `*traecn.TraeCNTokenStorage`，失败返回 typed error
   - 格式翻译：非 OpenAI 源格式先 `sdktranslator.TranslateRequest` 转 OpenAI（镜像 qoder）
   - `buildTraeCNChatBody` → POST `traecn.ChatURL`，`Accept: text/event-stream`
   - 非 200：`io.ReadAll` 限 4KB → 状态错误（含 status 与截断 body，不含 token）；401/403 额外 `log.Warn("trae-cn: token rejected, re-run --trae-cn-login")`
   - SSE 逐行解析（`bufio.Scanner`，`event:`/`data:` 对），`traeCNEventToChunk` 转 chunk，加 `data: ` 前缀入流，`done` 后 `emitDone`（复用 qoder 包级函数）+ `[DONE]`
   - 出口经 `sdktranslator.TranslateStream` 转回请求的源格式（镜像 qoder 双向翻译）
   - 上游连接建立后不设超时

6. `Execute(...)`：复用 ExecuteStream，累积 chunk 为非流式 OpenAI 响应（镜像 qoder `Execute`）。

7. `Refresh(ctx, auth) (*coreauth.Auth, error)`：
   - storage 无 `RefreshToken` → `log.Debug` 后原样返回 auth（不报错，不阻塞其他凭证）
   - 否则 `traecn.NewTraeCNAuth(e.cfg).ExchangeToken(...)`
   - **深拷贝**：`cp := *storage`；更新 `cp.Token/RefreshToken/ExpireTime/LastRefresh`
   - `updated := auth.Clone()`；`updated.Storage = &cp`；`updated.Metadata["expires_at"] = time.UnixMilli(cp.ExpireTime).UTC().Format(time.RFC3339)`
   - **不调 SaveTokenToFile**（conductor 经 `m.Update` 落盘）

8. `FetchTraeCNModels(ctx, auth, cfg) []*registry.ModelInfo`：
   - GET `traecn.ModelListURL + "?type=llm_raw_chat"`，带全部设备指纹头；http.Client `Timeout: 15s`（模型拉取属凭证获取阶段）
   - 解析 `model_configs[].name` → `&registry.ModelInfo{ID: "trae-cn/" + name, OwnedBy: "trae-cn", Type: "trae-cn"}`
   - 任何失败 → `log.Warn` + 回退 `registry.GetTraeCNModels()`

## Step 3: 验证

**本机无 Go 工具链，无法跑 go test/go build。** 改为静态自查：
- 方法签名与 qoder executor 完全一致（逐个对照 PrepareRequest/HttpRequest/Execute/ExecuteStream/Refresh 的签名）
- `emitDone` 不重复定义（直接调用包级已有函数）
- import 与 qoder executor 对齐 + 新增 `internal/auth/traecn`
- Refresh 深拷贝语义正确（`cp := *storage`，不 SaveTokenToFile）
- gofmt 风格

## Step 4: Commit

**不要执行任何 git 命令。** 在报告里给出 git 命令文本：

```bash
git add internal/runtime/executor/traecn_executor.go internal/runtime/executor/traecn_executor_test.go
git commit -m "feat(executor): add trae-cn executor with SSE translation and token refresh"
```
