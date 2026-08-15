package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	traecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// TraeCNExecutor proxies OpenAI chat requests to the Trae CN IDE model API.
// Every upstream call carries the device fingerprint captured at login time
// (x-device-id / x-machine-id / x-ide-version / ...) — the upstream rejects
// requests without the full header set.
type TraeCNExecutor struct{ cfg *config.Config }

// Compile-time assertion that TraeCNExecutor satisfies the ProviderExecutor contract.
var _ cliproxyauth.ProviderExecutor = (*TraeCNExecutor)(nil)

// NewTraeCNExecutor creates a new Trae CN executor.
func NewTraeCNExecutor(cfg *config.Config) *TraeCNExecutor { return &TraeCNExecutor{cfg: cfg} }

// Identifier returns the provider identifier.
func (e *TraeCNExecutor) Identifier() string { return "trae-cn" }

// PrepareRequest injects Trae CN credentials and the device fingerprint into
// the outgoing HTTP request.
func (e *TraeCNExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	storage, ok := traecnStorage(auth)
	if !ok {
		return fmt.Errorf("invalid auth storage type for trae-cn: %T", authStorage(auth))
	}
	applyTraeCNHeaders(req, storage)
	return nil
}

// HttpRequest injects Trae CN authentication into the HTTP request and executes it.
func (e *TraeCNExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("trae-cn executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute executes a non-streaming request against the Trae CN chat API. It
// reuses ExecuteStream and accumulates the deltas into a single OpenAI
// chat.completion response, mirroring QoderExecutor.Execute.
func (e *TraeCNExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	// Translate the client's SourceFormat payload into OpenAI up-front, then
	// run the stream as OpenAI->OpenAI so chunks arrive as raw OpenAI JSON
	// we can accumulate.
	internalReq := req
	internalOpts := opts
	if opts.SourceFormat != "" && opts.SourceFormat != sdktranslator.FormatOpenAI {
		internalReq.Payload = sdktranslator.TranslateRequest(
			opts.SourceFormat, sdktranslator.FormatOpenAI,
			req.Model, req.Payload, false)
	}
	internalOpts.SourceFormat = sdktranslator.FormatOpenAI

	streamResult, err := e.ExecuteStream(ctx, auth, internalReq, internalOpts)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	// Accumulate all chunks
	var content strings.Builder
	var reasoning strings.Builder
	finishReason := "stop"
	var usage map[string]interface{}

	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return cliproxyexecutor.Response{}, chunk.Err
		}

		// ExecuteStream was called with SourceFormat=FormatOpenAI so
		// TranslateStream strips the "data:" prefix and returns raw JSON.
		// Skip empty or [DONE] payloads.
		raw := chunk.Payload
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("[DONE]")) {
			continue
		}

		var oiChunk map[string]interface{}
		if err := json.Unmarshal(raw, &oiChunk); err != nil {
			continue
		}
		if u, ok := oiChunk["usage"].(map[string]interface{}); ok {
			usage = u
		}
		if choices, ok := oiChunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if contentStr, ok := delta["content"].(string); ok {
						content.WriteString(contentStr)
					}
					if reasoningStr, ok := delta["reasoning_content"].(string); ok {
						reasoning.WriteString(reasoningStr)
					}
				}
				if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
					finishReason = fr
				}
			}
		}
	}

	// Build final response
	message := map[string]interface{}{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	response := map[string]interface{}{
		"id":      fmt.Sprintf("trae-cn-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
	}
	if usage != nil {
		response["usage"] = usage
	}

	responseBytes, _ := json.Marshal(response)

	// Translate the OpenAI-format response back to the client's expected
	// SourceFormat. Reuse internalReq.Payload — that is already the
	// OpenAI-translated payload computed above.
	var param any
	responseBytes = sdktranslator.TranslateNonStream(ctx, sdktranslator.FormatOpenAI, opts.SourceFormat, req.Model, opts.OriginalRequest, internalReq.Payload, responseBytes, &param)

	return cliproxyexecutor.Response{
		Payload: responseBytes,
		Headers: streamResult.Headers,
	}, nil
}

// CountTokens estimates the token count for the request. The Trae CN API
// exposes no token-counting endpoint, so this mirrors QoderExecutor's
// placeholder: translate to OpenAI, then estimate ~1 token per 4 characters.
func (e *TraeCNExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	// Translate non-openai formats before extracting messages
	payload := req.Payload
	if opts.SourceFormat != "" && opts.SourceFormat != sdktranslator.FormatOpenAI {
		payload = sdktranslator.TranslateRequest(opts.SourceFormat, sdktranslator.FormatOpenAI, req.Model, payload, false)
	}

	// Simple estimation: 1 token ≈ 4 characters
	var chatReq map[string]interface{}
	if err := json.Unmarshal(payload, &chatReq); err != nil {
		return cliproxyexecutor.Response{}, err
	}

	messagesRaw, _ := chatReq["messages"].([]interface{})
	totalChars := 0
	for _, msg := range messagesRaw {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			content := extractContentGeneric(msgMap["content"])
			totalChars += len(content)
		}
	}

	estimatedTokens := totalChars / 4
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	response := map[string]interface{}{
		"usage": map[string]int{
			"prompt_tokens":     estimatedTokens,
			"completion_tokens": 0,
			"total_tokens":      estimatedTokens,
		},
	}

	responseBytes, _ := json.Marshal(response)
	return cliproxyexecutor.Response{
		Payload: responseBytes,
	}, nil
}

// ExecuteStream executes a streaming request against the Trae CN chat API.
// The upstream speaks SSE with typed events (metadata/output/token_usage/
// done/error); each event is mapped onto an OpenAI chat.completion.chunk and
// then run through sdktranslator.TranslateStream so the client's SourceFormat
// dictates the actual wire bytes.
func (e *TraeCNExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	storage, ok := traecnStorage(auth)
	if !ok {
		return nil, fmt.Errorf("invalid auth storage type for trae-cn: %T", authStorage(auth))
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
	reporter := helps.NewExecutorUsageReporter(ctx, e, model, auth)
	defer reporter.TrackFailure(ctx, &err)

	messagesRaw, _ := chatReq["messages"].([]interface{})
	messages := make([]map[string]any, 0, len(messagesRaw))
	for _, msg := range messagesRaw {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			messages = append(messages, msgMap)
		}
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("trae-cn: request contains no messages")
	}

	reqBody := buildTraeCNChatBody(messages, model)
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, traecn.ChatURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	applyTraeCNHeaders(httpReq, storage)

	// No timeout after the upstream connection is established — the stream
	// can stay open for as long as the model keeps generating.
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpClient = reporter.TrackHTTPClient(httpClient)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		if httpResp.StatusCode == http.StatusUnauthorized || httpResp.StatusCode == http.StatusForbidden {
			log.Warn("trae-cn: token rejected, re-run --trae-cn-login")
		}
		return nil, newTraeCNStatusError(httpResp.StatusCode, truncate(string(body), 300))
	}

	// Create streaming channel
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() { _ = httpResp.Body.Close() }()

		// Shared across all TranslateStream calls in this stream — the
		// translator carries open-block / sequence state through it.
		var streamParam any

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800) // 50MB max line

		var event string
		var data []byte
		// dispatch converts one complete SSE event (event: + data: pair) into
		// an OpenAI chunk and feeds it through TranslateStream.
		dispatch := func() bool {
			if len(data) == 0 {
				event = ""
				return true
			}
			ev, raw := event, string(data)
			event, data = "", nil
			chunkBytes := traeCNEventToChunk(ev, raw, model)
			if ev == "done" {
				emitDone(ctx, out, opts.SourceFormat, req.Model, opts.OriginalRequest, payload, &streamParam)
				reporter.EnsurePublished(ctx)
				return false
			}
			if chunkBytes == nil {
				return true
			}
			// Reconstruct an OpenAI-compatible SSE line ("data: {chunk}") —
			// the translators expect the data: prefix.
			ssePayload := append([]byte("data: "), chunkBytes...)
			if detail, ok := helps.ParseOpenAIStreamUsage(ssePayload); ok {
				reporter.Publish(ctx, detail)
			}

			to := sdktranslator.FormatOpenAI
			from := opts.SourceFormat
			if from == "" {
				from = to
			}
			frames := sdktranslator.TranslateStream(ctx, to, from,
				req.Model, opts.OriginalRequest, payload, ssePayload, &streamParam)
			for _, frame := range frames {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
				case <-ctx.Done():
					return false
				}
			}
			return true
		}

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				// Blank line terminates an SSE event block.
				if !dispatch() {
					return
				}
				continue
			}
			if bytes.HasPrefix(line, []byte("event:")) {
				event = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
				continue
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				d := bytes.TrimPrefix(line, []byte("data:"))
				d = bytes.TrimPrefix(d, []byte(" "))
				if len(data) > 0 {
					data = append(data, '\n')
				}
				data = append(data, d...)
				continue
			}
			// Ignore comments and unknown fields.
		}
		// Flush any trailing event not terminated by a blank line.
		if len(data) > 0 {
			if !dispatch() {
				return
			}
		}

		// Scanner loop exited naturally (EOF). Emit a terminating
		// "data: [DONE]" / format-specific stop frame so the client closes
		// the stream cleanly.
		emitDone(ctx, out, opts.SourceFormat, req.Model, opts.OriginalRequest, payload, &streamParam)
		if err := scanner.Err(); err != nil {
			streamErr := fmt.Errorf("scanner error: %w", err)
			reporter.PublishFailure(ctx, streamErr)
			out <- cliproxyexecutor.StreamChunk{Err: streamErr}
			return
		}
		reporter.EnsurePublished(ctx)
	}()

	return &cliproxyexecutor.StreamResult{
		Headers: httpResp.Header.Clone(),
		Chunks:  out,
	}, nil
}

// Refresh handles token refresh for the Trae CN provider. The upstream
// ExchangeToken endpoint trades the persisted refresh token for a fresh
// short-lived access token.
//
// The conductor persists the returned auth via Manager.Update, so this method
// must NOT call SaveTokenToFile itself. Storage is deep-copied to avoid racing
// concurrent ExecuteStream reads on the same struct.
func (e *TraeCNExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("trae-cn executor: auth is nil")
	}

	storage, ok := auth.Storage.(*traecn.TraeCNTokenStorage)
	if !ok || storage == nil {
		return auth, nil
	}
	if strings.TrimSpace(storage.RefreshToken) == "" {
		// No refresh token persisted — cannot refresh. Leave the auth as-is
		// so the next request surfaces the expiry; do not block other
		// credentials from refreshing.
		log.Debug("trae-cn: refresh skipped — no refresh token persisted; re-run --trae-cn-login")
		return auth, nil
	}

	authSvc := traecn.NewTraeCNAuth(e.cfg)
	tokenData, err := authSvc.ExchangeToken(ctx, traecn.ClientID, storage.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("trae-cn refresh: exchange token: %w", err)
	}

	// Auth.Clone() performs a shallow copy of the Storage interface field, so
	// we construct a fresh *TraeCNTokenStorage to avoid racing concurrent
	// ExecuteStream reads on the same struct.
	cp := *storage
	cp.Token = tokenData.AccessToken
	if strings.TrimSpace(tokenData.RefreshToken) != "" {
		cp.RefreshToken = tokenData.RefreshToken
	}
	cp.ExpireTime = time.Now().UnixMilli() + tokenData.ExpiresIn*1000
	cp.LastRefresh = time.Now().Format(time.RFC3339)

	updated := auth.Clone()
	updated.Storage = &cp
	if updated.Metadata == nil {
		updated.Metadata = map[string]any{}
	}
	// ExpirationTime() reads expires_at from Metadata (not the storage
	// struct), so the next refresh must be scheduled against the new expiry.
	updated.Metadata["expires_at"] = time.UnixMilli(cp.ExpireTime).UTC().Format(time.RFC3339)

	log.Infof("trae-cn: refreshed access token (expires_at=%s)", updated.Metadata["expires_at"])
	return updated, nil
}

// traecnStorage extracts the Trae CN token storage from an auth record.
func traecnStorage(auth *cliproxyauth.Auth) (*traecn.TraeCNTokenStorage, bool) {
	if auth == nil {
		return nil, false
	}
	storage, ok := auth.Storage.(*traecn.TraeCNTokenStorage)
	if !ok || storage == nil {
		return nil, false
	}
	return storage, true
}

// authStorage returns the raw storage field for error formatting.
func authStorage(auth *cliproxyauth.Auth) interface{} {
	if auth == nil {
		return nil
	}
	return auth.Storage
}

// applyTraeCNHeaders sets the Trae CN auth + device fingerprint headers.
// Optional headers are only set when non-empty.
func applyTraeCNHeaders(req *http.Request, s *traecn.TraeCNTokenStorage) {
	req.Header.Set("x-app-id", traecn.ClientID)
	req.Header.Set("x-ide-token", s.Token)
	setIfNotEmpty(req, "x-device-id", s.DeviceID)
	setIfNotEmpty(req, "x-machine-id", s.MachineID)
	setIfNotEmpty(req, "x-device-brand", s.DeviceBrand)
	setIfNotEmpty(req, "x-device-cpu", s.DeviceCPU)
	setIfNotEmpty(req, "x-device-type", s.DeviceType)
	setIfNotEmpty(req, "x-os-version", s.OSVersion)
	setIfNotEmpty(req, "x-ide-version", s.IDEVersion)
	setIfNotEmpty(req, "x-ide-version-code", s.IDEVersionCode)
	setIfNotEmpty(req, "x-ide-version-type", s.IDEVersionType)
}

func setIfNotEmpty(req *http.Request, key, value string) {
	if strings.TrimSpace(value) != "" {
		req.Header.Set(key, value)
	}
}

// buildTraeCNChatBody assembles the Trae CN chat request body from OpenAI
// chat messages. The last message becomes user_input; everything before it
// goes into chat_history with a success status.
func buildTraeCNChatBody(messages []map[string]any, model string) map[string]any {
	modelName := strings.TrimPrefix(model, "trae-cn/")

	userTurns := 0
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role == "user" {
			userTurns++
		}
	}
	currentTurn := userTurns - 1
	if currentTurn < 0 {
		currentTurn = 0
	}

	last := len(messages) - 1
	history := make([]map[string]any, 0, last)
	for _, msg := range messages[:last] {
		role, _ := msg["role"].(string)
		history = append(history, map[string]any{
			"role":    role,
			"content": traeCNContentText(msg["content"]),
			"status":  "success",
			"locale":  "zh-cn",
		})
	}
	userInput := traeCNContentText(messages[last]["content"])

	variables, _ := json.Marshal(map[string]any{
		"locale":       "zh-cn",
		"current_time": time.Now().Format(time.RFC3339),
	})

	return map[string]any{
		"model_name":                   modelName,
		"chat_history":                 history,
		"user_input":                   userInput,
		"conversation_id":              uuid.NewString(),
		"session_id":                   uuid.NewString(),
		"current_turn":                 currentTurn,
		"valid_turns":                  currentTurn + 1,
		"intent_name":                  "general_qa_intent",
		"is_preset":                    true,
		"generate_suggested_questions": false,
		"context_resolvers":            []any{},
		"multi_media":                  []any{},
		"provider":                     "",
		"variables":                    string(variables),
	}
}

// traeCNContentText flattens OpenAI message content into plain text: strings
// pass through verbatim; content-part arrays keep only type=="text" parts.
func traeCNContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if part["type"] != "text" {
				continue
			}
			if text, ok := part["text"].(string); ok {
				sb.WriteString(text)
			}
		}
		return sb.String()
	default:
		return ""
	}
}

// traeCNEventToChunk maps one upstream SSE event onto an OpenAI
// chat.completion.chunk JSON payload. It returns nil for events that carry
// no client-visible data (metadata, unknown events).
func traeCNEventToChunk(event, data, model string) []byte {
	switch event {
	case "output":
		var payload struct {
			Response         string `json:"response"`
			ReasoningContent string `json:"reasoning_content"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		delta := map[string]any{
			"role":    "assistant",
			"content": payload.Response,
		}
		if payload.ReasoningContent != "" {
			delta["reasoning_content"] = payload.ReasoningContent
		}
		return marshalTraeCNChunk(model, delta, "", nil)
	case "token_usage":
		var payload struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		total := payload.TotalTokens
		if total == 0 {
			total = payload.PromptTokens + payload.CompletionTokens
		}
		usage := map[string]any{
			"prompt_tokens":     payload.PromptTokens,
			"completion_tokens": payload.CompletionTokens,
			"total_tokens":      total,
		}
		return marshalTraeCNChunk(model, nil, "", usage)
	case "done":
		return marshalTraeCNChunk(model, map[string]any{}, "stop", nil)
	case "error":
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		msg := payload.Message
		if msg == "" {
			msg = "upstream error"
		}
		delta := map[string]any{
			"role":    "assistant",
			"content": msg,
		}
		return marshalTraeCNChunk(model, delta, "error", nil)
	default:
		// metadata and unknown events carry no client-visible data.
		return nil
	}
}

// marshalTraeCNChunk renders one OpenAI chat.completion.chunk. When delta is
// nil the choices array is emitted empty (usage-only chunk).
func marshalTraeCNChunk(model string, delta map[string]any, finishReason string, usage map[string]any) []byte {
	chunk := map[string]any{
		"id":      "chatcmpl-" + uuid.NewString(),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
	}
	if delta == nil {
		chunk["choices"] = []any{}
	} else {
		choice := map[string]any{
			"index": 0,
			"delta": delta,
		}
		if finishReason != "" {
			choice["finish_reason"] = finishReason
		}
		chunk["choices"] = []map[string]any{choice}
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	out, err := json.Marshal(chunk)
	if err != nil {
		return nil
	}
	return out
}

// traeCNStatusError implements StatusError for Trae CN API errors.
type traeCNStatusError struct {
	status  int
	message string
}

func newTraeCNStatusError(status int, message string) *traeCNStatusError {
	return &traeCNStatusError{status: status, message: message}
}

func (e *traeCNStatusError) Error() string {
	return fmt.Sprintf("Trae CN API error %d: %s", e.status, e.message)
}

func (e *traeCNStatusError) StatusCode() int {
	return e.status
}

// FetchTraeCNModels retrieves the live model list from the Trae CN
// model_list endpoint and converts it into ModelInfo entries. Falls back to
// the static registry if the auth lacks credentials, the request fails, or
// the response is malformed. The 15s timeout is allowed because model
// fetching happens during the credential-acquisition phase.
func FetchTraeCNModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*registry.ModelInfo {
	storage, ok := traecnStorage(auth)
	if !ok || storage.Token == "" {
		log.Debug("trae-cn: no token, returning static models")
		return registry.GetTraeCNModels()
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traecn.ModelListURL+"?type=llm_raw_chat", nil)
	if err != nil {
		log.Warnf("trae-cn: build model list request: %v", err)
		return registry.GetTraeCNModels()
	}
	req.Header.Set("Accept", "application/json")
	applyTraeCNHeaders(req, storage)

	httpClient := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 15*time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Warnf("trae-cn: model list fetch canceled: %v", err)
		} else {
			log.Warnf("trae-cn: model list fetch failed: %v", err)
		}
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

	configs := gjson.GetBytes(body, "model_configs")
	if !configs.Exists() || !configs.IsArray() {
		log.Warn("trae-cn: model list response missing 'model_configs' array")
		return registry.GetTraeCNModels()
	}

	models := make([]*registry.ModelInfo, 0, 16)
	configs.ForEach(func(_, entry gjson.Result) bool {
		name := entry.Get("name").String()
		if name == "" {
			return true
		}
		models = append(models, &registry.ModelInfo{
			ID:      "trae-cn/" + name,
			OwnedBy: "trae-cn",
			Type:    "trae-cn",
		})
		return true
	})

	if len(models) == 0 {
		log.Warn("trae-cn: model list returned no models, falling back to static")
		return registry.GetTraeCNModels()
	}

	log.Infof("trae-cn: fetched %d models from model_list", len(models))
	return models
}
