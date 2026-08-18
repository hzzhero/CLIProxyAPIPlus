package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	traecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestTraeCNExecutorIdentifier(t *testing.T) {
	e := NewTraeCNExecutor(nil)
	if got := e.Identifier(); got != "trae-cn" {
		t.Fatalf("Identifier() = %q, want trae-cn", got)
	}
}

func TestBuildTraeCNChatBody(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "be helpful"},
		map[string]interface{}{"role": "user", "content": "hello"},
		map[string]interface{}{"role": "assistant", "content": "hi"},
		map[string]interface{}{"role": "user", "content": "how are you"},
	}
	body, err := buildTraeCNChatBody(messages, "trae-cn/doubao-pro")
	if err != nil {
		t.Fatalf("buildTraeCNChatBody error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Failed to parse body: %v", err)
	}

	// model prefix stripped
	if parsed["model_name"] != "doubao-pro" {
		t.Fatalf("model_name = %v", parsed["model_name"])
	}

	// last user message is user_input
	if parsed["user_input"] != "how are you" {
		t.Fatalf("user_input = %v", parsed["user_input"])
	}

	// history excludes last message, each carries status
	history, _ := parsed["chat_history"].([]interface{})
	if len(history) != 3 {
		t.Fatalf("chat_history length = %d, want 3", len(history))
	}
	for i, h := range history {
		hm := h.(map[string]interface{})
		if hm["status"] != "success" {
			t.Fatalf("history[%d] missing status: %v", i, h)
		}
	}

	if parsed["intent_name"] != "general_qa_intent" {
		t.Fatalf("intent_name = %v", parsed["intent_name"])
	}

	if parsed["conversation_id"] == "" || parsed["session_id"] == "" {
		t.Fatal("conversation_id/session_id must be generated UUIDs")
	}

	// current_turn should be 1 (two user messages, last one is input)
	if parsed["current_turn"].(float64) != 1 {
		t.Fatalf("current_turn = %v", parsed["current_turn"])
	}
}

func TestTraeCNEventToChunk(t *testing.T) {
	// output event -> delta content
	chunk := traeCNEventToChunk("output", `{"response":"hello","reasoning_content":"thinking"}`, "doubao-pro")
	if chunk == nil {
		t.Fatal("output chunk should not be nil")
	}
	if !strings.Contains(string(chunk), `"content":"hello"`) {
		t.Fatalf("output chunk missing content: %s", string(chunk))
	}
	if !strings.Contains(string(chunk), `"reasoning_content":"thinking"`) {
		t.Fatalf("reasoning passthrough missing: %s", string(chunk))
	}

	// metadata event -> nil (skipped)
	if c := traeCNEventToChunk("metadata", `{"prompt_completion_id":"x"}`, "m"); c != nil {
		t.Fatalf("metadata should be skipped, got %s", string(c))
	}

	// token_usage -> usage payload
	usage := traeCNEventToChunk("token_usage", `{"prompt_tokens":3,"completion_tokens":5}`, "m")
	if usage == nil {
		t.Fatal("usage chunk should not be nil")
	}
	if !strings.Contains(string(usage), `"prompt_tokens":3`) {
		t.Fatalf("usage chunk missing prompt_tokens: %s", string(usage))
	}

	// done -> finish_reason stop
	done := traeCNEventToChunk("done", "", "m")
	if done == nil {
		t.Fatal("done chunk should not be nil")
	}
	if !strings.Contains(string(done), `"finish_reason":"stop"`) {
		t.Fatalf("done chunk missing finish_reason=stop: %s", string(done))
	}

	// error -> content + finish_reason error
	errChunk := traeCNEventToChunk("error", `{"message":"boom"}`, "m")
	if errChunk == nil {
		t.Fatal("error chunk should not be nil")
	}
	if !strings.Contains(string(errChunk), "boom") || !strings.Contains(string(errChunk), `"finish_reason":"error"`) {
		t.Fatalf("error chunk incorrect: %s", string(errChunk))
	}
}

func TestTraeCNExecutorPrepareRequest(t *testing.T) {
	e := NewTraeCNExecutor(nil)
	ctx := context.Background()
	auth := &cliproxyauth.Auth{}
	req := &http.Request{}
	if err := e.PrepareRequest(ctx, auth, req); err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
}
