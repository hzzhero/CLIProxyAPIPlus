package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	traecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
)

// TraeCNExecutor proxies OpenAI chat requests to the Trae CN IDE model API.
type TraeCNExecutor struct {
	cfg *config.Config
}

// NewTraeCNExecutor creates a new Trae CN executor.
func NewTraeCNExecutor(cfg *config.Config) *TraeCNExecutor {
	return &TraeCNExecutor{cfg: cfg}
}

// Identifier returns the provider identifier.
func (e *TraeCNExecutor) Identifier() string {
	return "trae-cn"
}

// PrepareRequest is a no-op for Trae CN as authentication is handled via headers.
func (e *TraeCNExecutor) PrepareRequest(ctx context.Context, authRecord *cliproxyauth.Auth, req *http.Request) error {
	return nil
}

// ExecuteStream executes a streaming request against Trae CN API.
func (e *TraeCNExecutor) ExecuteStream(ctx context.Context, authRecord *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	storage, ok := authRecord.Storage.(*traecn.TraeCNTokenStorage)
	if !ok {
		return nil, fmt.Errorf("invalid auth storage type for trae-cn: %T", authRecord.Storage)
	}

	// Translate non-openai formats to chat completions before extracting messages
	payload := req.Payload
	if opts.SourceFormat != "" && opts.SourceFormat != sdktranslator.FormatOpenAI {
		payload = sdktranslator.TranslateRequest(opts.SourceFormat, sdktranslator.FormatOpenAI, req.Model, payload, false)
	}

	// Parse request to get model and messages
	var chatReq map[string]interface{}
	if err := json.Unmarshal(payload, &chatReq); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	model, _ := chatReq["model"].(string)
	tracModel := strings.TrimPrefix(model, "trae-cn/")

	// Build chat body
	messagesRaw, _ := chatReq["messages"].([]interface{})
	body, err := buildTraeCNChatBody(messagesRaw, tracModel)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	httpClient := &http.Client{}
	reqURL := traecn.ChatURL

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	applyTraeCNHeaders(httpReq, storage)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := truncate(string(bodyBytes), 300)
		log.Warnf("trae-cn: upstream returned status %d: %s", resp.StatusCode, msg)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			log.Warn("trae-cn: token rejected, re-run --trae-cn-login")
		}
		return nil, newTraeCNStatusError(resp.StatusCode, msg)
	}

	// SSE stream parsing
	out := make(chan cliproxyexecutor.StreamChunk, 16)
	done := make(chan struct{})
	param := new(any)

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var event, data string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					emitDone(ctx, out, opts.SourceFormat, req.Model, payload, []byte("[DONE]"), param)
					break
				}
				if event != "" {
					chunk := traeCNEventToChunk(event, data, req.Model)
					if chunk != nil {
						select {
						case out <- cliproxyexecutor.StreamChunk{Payload: chunk}:
						case <-ctx.Done():
							return
						}
					}
				}
				event = ""
				data = ""
			}
		}
		if err := scanner.Err(); err != nil {
			log.Warnf("trae-cn: SSE scan error: %v", err)
		}
	}()

	return &cliproxyexecutor.StreamResult{Chunks: out, Done: done}, nil
}

// Execute executes a non-streaming request by accumulating stream chunks.
func (e *TraeCNExecutor) Execute(ctx context.Context, authRecord *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.Result, error) {
	stream, err := e.ExecuteStream(ctx, authRecord, req, opts)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for chunk := range stream.Chunks {
		buf.Write(chunk.Payload)
	}
	<-stream.Done

	return &cliproxyexecutor.Result{Body: buf.Bytes()}, nil
}

// Refresh attempts to refresh the token if needed.
func (e *TraeCNExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("trae-cn executor: auth is nil")
	}

	storage, ok := auth.Storage.(*traecn.TraeCNTokenStorage)
	if !ok || storage == nil {
		return auth, nil
	}

	if storage.RefreshToken == "" {
		log.Debug("trae-cn: no refresh token, skipping refresh")
		return auth, nil
	}

	authSvc := traecn.NewTraeCNAuth(e.cfg)
	tokenData, err := authSvc.ExchangeToken(ctx, traecn.ClientID, storage.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("trae-cn refresh: exchange token: %w", err)
	}

	cp := *storage
	cp.Token = tokenData.AccessToken
	cp.RefreshToken = tokenData.RefreshToken
	cp.ExpireTime = time.Now().Add(24 * time.Hour).UnixMilli()
	cp.LastRefresh = time.Now().Format(time.RFC3339)

	updated := auth.Clone()
	updated.Storage = &cp
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["expires_at"] = time.UnixMilli(cp.ExpireTime).UTC().Format(time.RFC3339)

	log.Infof("trae-cn: refreshed token (expires_at=%s)", updated.Metadata["expires_at"])
	return updated, nil
}

// FetchTraeCNModels retrieves the live model list from Trae CN.
func FetchTraeCNModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*registry.ModelInfo {
	storage, ok := auth.Storage.(*traecn.TraeCNTokenStorage)
	if !ok || storage == nil || storage.Token == "" {
		log.Debugf("trae-cn: no token, returning static models")
		return registry.GetTraeCNModels()
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traecn.ModelListURL+"?type=llm_raw_chat", nil)
	if err != nil {
		log.Warnf("trae-cn: build model list request: %v", err)
		return registry.GetTraeCNModels()
	}

	applyTraeCNHeaders(req, storage)
	req.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("trae-cn: model list fetch failed: %v", err)
		return registry.GetTraeCNModels()
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("trae-cn: read model list response: %v", err)
		return registry.GetTraeCNModels()
	}
	if resp.StatusCode != http.StatusOK {
		log.Warnf("trae-cn: model list returned %d: %s", resp.StatusCode, truncate(string(body), 300))
		return registry.GetTraeCNModels()
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Warnf("trae-cn: parse model list response: %v", err)
		return registry.GetTraeCNModels()
	}

	configs, ok := result["model_configs"].([]interface{})
	if !ok {
		log.Warnf("trae-cn: model list response missing 'model_configs' array")
		return registry.GetTraeCNModels()
	}

	models := make([]*registry.ModelInfo, 0, len(configs))
	for _, c := range configs {
		if cfgMap, ok := c.(map[string]interface{}); ok {
			if name, ok := cfgMap["name"].(string); ok {
				models = append(models, &registry.ModelInfo{
					ID:       "trae-cn/" + name,
					OwnedBy:  "trae-cn",
					Type:     "trae-cn",
				})
			}
		}
	}

	if len(models) == 0 {
		log.Warn("trae-cn: no models found in response")
		return registry.GetTraeCNModels()
	}

	return models
}

func applyTraeCNHeaders(req *http.Request, s *traecn.TraeCNTokenStorage) {
	req.Header.Set("x-app-id", traecn.ClientID)
	req.Header.Set("x-ide-token", s.Token)
	req.Header.Set("x-device-id", s.DeviceID)
	req.Header.Set("x-machine-id", s.MachineID)
	if s.DeviceBrand != "" {
		req.Header.Set("x-device-brand", s.DeviceBrand)
	}
	if s.DeviceCPU != "" {
		req.Header.Set("x-device-cpu", s.DeviceCPU)
	}
	if s.DeviceType != "" {
		req.Header.Set("x-device-type", s.DeviceType)
	}
	if s.OSVersion != "" {
		req.Header.Set("x-os-version", s.OSVersion)
	}
	if s.IDEVersion != "" {
		req.Header.Set("x-ide-version", s.IDEVersion)
	}
	if s.IDEVersionCode != "" {
		req.Header.Set("x-ide-version-code", s.IDEVersionCode)
	}
	if s.IDEVersionType != "" {
		req.Header.Set("x-ide-version-type", s.IDEVersionType)
	}
}

func buildTraeCNChatBody(messages []interface{}, model string) ([]byte, error) {
	// Find the index of the last user message
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if msgMap, ok := messages[i].(map[string]interface{}); ok {
			if role, _ := msgMap["role"].(string); role == "user" {
				lastUserIdx = i
				break
			}
		}
	}

	if lastUserIdx < 0 {
		// No user messages, return empty
		lastUserIdx = len(messages) - 1
	}

	var chatHistory []map[string]interface{}
	userInput := extractText(messages[lastUserIdx])

	// Build history (all messages before the last user message)
	for i := 0; i < lastUserIdx; i++ {
		if msgMap, ok := messages[i].(map[string]interface{}); ok {
			historyItem := map[string]interface{}{
				"role":    msgMap["role"],
				"content": extractText(msgMap["content"]),
				"status":  "success",
				"locale":  "zh-cn",
			}
			chatHistory = append(chatHistory, historyItem)
		}
	}

	currentTurn := countUserMessages(messages[:lastUserIdx])

	now := time.Now().Format(time.RFC3339)
	variables := map[string]interface{}{
		"locale":       "zh-cn",
		"current_time": now,
	}

	varsBytes, err := json.Marshal(variables)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"chat_history":           chatHistory,
		"user_input":             userInput,
		"conversation_id":        uuid.New().String(),
		"session_id":             uuid.New().String(),
		"current_turn":           currentTurn,
		"valid_turns":            currentTurn + 1,
		"model_name":             model,
		"intent_name":            "general_qa_intent",
		"is_preset":              true,
		"generate_suggested_questions": false,
		"context_resolvers":      []interface{}{},
		"multi_media":            []interface{}{},
		"provider":               "",
		"variables":              string(varsBytes),
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bodyBytes, nil
}

func countUserMessages(messages []interface{}) int {
	count := 0
	for _, msg := range messages {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			if role, _ := msgMap["role"].(string); role == "user" {
				count++
			}
		}
	}
	return count
}

func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, part := range v {
			if partMap, ok := part.(map[string]interface{}); ok {
				if partType, ok := partMap["type"].(string); ok && partType == "text" {
					if text, ok := partMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func traeCNEventToChunk(event, data, model string) []byte {
	switch event {
	case "metadata":
		// Skip metadata events
		return nil
	case "output":
		var output map[string]interface{}
		if err := json.Unmarshal([]byte(data), &output); err != nil {
			log.Warnf("trae-cn: parse output event: %v", err)
			return nil
		}
		response, _ := output["response"].(string)
		reasoningContent, _ := output["reasoning_content"].(string)

		delta := map[string]interface{}{
			"role":    "assistant",
			"content": response,
		}
		if reasoningContent != "" {
			delta["reasoning_content"] = reasoningContent
		}

		chunk := map[string]interface{}{
			"id":      uuid.New().String(),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"delta": delta,
				},
			},
		}
		bytes, _ := json.Marshal(chunk)
		return bytes
	case "token_usage":
		var usage map[string]interface{}
		if err := json.Unmarshal([]byte(data), &usage); err != nil {
			log.Warnf("trae-cn: parse token_usage event: %v", err)
			return nil
		}
		promptTokens, _ := usage["prompt_tokens"].(float64)
		completionTokens, _ := usage["completion_tokens"].(float64)
		totalTokens, _ := usage["total_tokens"].(float64)

		chunk := map[string]interface{}{
			"id":      uuid.New().String(),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{},
			"usage": map[string]interface{}{
				"prompt_tokens":     int(promptTokens),
				"completion_tokens": int(completionTokens),
				"total_tokens":      int(totalTokens),
			},
		}
		bytes, _ := json.Marshal(chunk)
		return bytes
	case "done":
		chunk := map[string]interface{}{
			"id":      uuid.New().String(),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				map[string]interface{}{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": "stop",
				},
			},
		}
		bytes, _ := json.Marshal(chunk)
		return bytes
	case "error":
		var errMap map[string]interface{}
		if err := json.Unmarshal([]byte(data), &errMap); err != nil {
			return nil
		}
		message, _ := errMap["message"].(string)
		if message == "" {
			message = "unknown error"
		}
		chunk := map[string]interface{}{
			"id":      uuid.New().String(),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"delta": map[string]interface{}{
						"content":       message,
						"finish_reason": "error",
					},
				},
			},
		}
		bytes, _ := json.Marshal(chunk)
		return bytes
	default:
		return nil
	}
}

type traeCNStatusError struct {
	status  int
	message string
}

func newTraeCNStatusError(status int, message string) error {
	return &traeCNStatusError{status: status, message: message}
}

func (e *traeCNStatusError) StatusCode() int {
	return e.status
}

func (e *traeCNStatusError) Error() string {
	return fmt.Sprintf("trae-cn API error: %d %s", e.status, e.message)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
