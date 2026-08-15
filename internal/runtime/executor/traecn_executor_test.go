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
