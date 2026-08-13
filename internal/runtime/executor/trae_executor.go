package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	traeauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/trae"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
)

const (
	traeCNAuthType  = "trae-cn"
	traeCNUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
)

// TraeCNExecutor executes requests against the Trae CN SOLO agent API.
// The API uses a session-based flow: POST /chat_sessions to create a session
// with the initial message, then GET /chat_sessions/{id}/events to stream
// the response via SSE. The SSE events use a custom format (plan_item with
// cumulative text, token_usage, done, error) — not OpenAI-compatible — so
// this executor translates between OpenAI chat completion format and Trae's
// custom format.
type TraeCNExecutor struct {
	cfg *config.Config
}

// NewTraeCNExecutor creates a new Trae CN executor.
func NewTraeCNExecutor(cfg *config.Config) *TraeCNExecutor {
	return &TraeCNExecutor{cfg: cfg}
}

// Identifier returns the provider identifier.
func (e *TraeCNExecutor) Identifier() string {
	return traeCNAuthType
}

// traeCredentials extracts the JWT token from auth storage or metadata.
func traeCredentials(auth *cliproxyauth.Auth) (*traeauth.TraeTokenStorage, error) {
	if auth == nil {
		return nil, fmt.Errorf("trae-cn: auth is nil")
	}
	// Prefer storage struct if available (set during login)
	if storage, ok := auth.Storage.(*traeauth.TraeTokenStorage); ok && storage != nil {
		return storage, nil
	}
	// Fall back to metadata (set during refresh)
	token := metaStringValue(auth.Metadata, "token")
	if token == "" {
		return nil, fmt.Errorf("trae-cn: missing token in auth metadata")
	}
	return &traeauth.TraeTokenStorage{
		Token:        token,
		RefreshToken: metaStringValue(auth.Metadata, "refresh_token"),
		UserID:       metaStringValue(auth.Metadata, "user_id"),
		ClientID:     metaStringValue(auth.Metadata, "client_id"),
		WebID:        metaStringValue(auth.Metadata, "web_id"),
		Scope:        metaStringValue(auth.Metadata, "scope"),
		Tenant:       metaStringValue(auth.Metadata, "tenant"),
		Region:       metaStringValue(auth.Metadata, "region"),
		AIRegion:     metaStringValue(auth.Metadata, "ai_region"),
		UserIdentity: metaStringValue(auth.Metadata, "user_identity"),
		Type:         traeCNAuthType,
	}, nil
}

// apiBase returns the SOLO agent API base URL, with env var override.
func traeAPIBase() string {
	if v := os.Getenv("TRAE_CN_API_BASE"); v != "" {
		return v
	}
	return traeauth.DefaultAPIBase
}

// buildCommonParams builds the common_params JSON string from stored provider data.
func buildCommonParams(storage *traeauth.TraeTokenStorage) string {
	params := map[string]string{
		"web_id":         storage.WebID,
		"biz_user_id":    storage.BizUserID,
		"user_unique_id": storage.UserUniqueID,
		"scope":          storage.Scope,
		"tenant":         storage.Tenant,
		"region":         storage.Region,
		"ai_region":      storage.AIRegion,
		"user_identity":  storage.UserIdentity,
	}
	b, _ := json.Marshal(params)
	return string(b)
}

// flattenMessages converts OpenAI messages array to Trae's query format.
// Trae expects messages as an array of objects with role and content,
// where content is a JSON-encoded array of text blocks.
func flattenMessages(messages []interface{}) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content := extractContentGeneric(msgMap["content"])

		// Trae expects content as a JSON-encoded array of text blocks
		textBlocks := []map[string]string{
			{"type": "text", "text": content},
		}
		contentJSON, _ := json.Marshal(textBlocks)

		out = append(out, map[string]interface{}{
			"role":    role,
			"content": string(contentJSON),
		})
	}
	return out
}

// createChatSession creates a SOLO agent chat session and returns the session ID
// and the reply_to_message_id for streaming events.
func createChatSession(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, storage *traeauth.TraeTokenStorage, model, query string) (sessionID, messageID string, err error) {
	apiBase := traeAPIBase()
	url := apiBase + "/chat_sessions"

	reqBody := map[string]interface{}{
		"mode":           "solo_agent_chat",
		"environment_id": "0",
		"initial_message": map[string]interface{}{
			"query":        query,
			"model_name":   model,
			"agent_type":   "solo_agent_chat",
			"model_selection_strategy": "auto",
			"common_params": buildCommonParams(storage),
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("trae-cn: failed to create session request: %w", err)
	}
	applyTraeHeaders(httpReq, storage)

	httpClient := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("trae-cn: session creation failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("trae-cn: close session response body error: %v", errClose)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("trae-cn: failed to read session response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("trae-cn: session creation returned status %d: %s", resp.StatusCode, summarizeErrorBody(resp.Header.Get("Content-Type"), respBody))
	}

	// Parse response: {"data":{"chat_session_id":"...","reply_to_message_id":"..."}}
	var result struct {
		Code int `json:"code"`
		Data struct {
			ChatSessionID    string `json:"chat_session_id"`
			ReplyToMessageID string `json:"reply_to_message_id"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("trae-cn: failed to parse session response: %w", err)
	}
	if result.Data.ChatSessionID == "" {
		return "", "", fmt.Errorf("trae-cn: empty session ID in response: %s", string(respBody))
	}

	return result.Data.ChatSessionID, result.Data.ReplyToMessageID, nil
}

// applyTraeHeaders sets the required headers for Trae SOLO agent API requests.
func applyTraeHeaders(req *http.Request, storage *traeauth.TraeTokenStorage) {
	req.Header.Set("Authorization", "Cloud-IDE-JWT "+storage.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", traeCNUserAgent)
	req.Header.Set("X-Trae-Client-Type", "web")
	req.Header.Set("X-Preferenced-Language", "en")
	req.Header.Set("x-user-region", "CN")
}

// traeSSEEvent represents a parsed SSE event from Trae's event stream.
type traeSSEEvent struct {
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

// streamSessionEvents connects to the SSE event stream for a chat session.
// It sends parsed events to the out channel and closes it when done.
func streamSessionEvents(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, storage *traeauth.TraeTokenStorage, sessionID, messageID string, out chan<- traeSSEEvent) {
	defer close(out)

	apiBase := traeAPIBase()
	url := fmt.Sprintf("%s/chat_sessions/%s/events?reply_to_message_id=%s", apiBase, sessionID, messageID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		out <- traeSSEEvent{EventType: "error", Data: json.RawMessage(fmt.Sprintf(`{"message":"failed to create events request: %s"}`, err.Error()))}
		return
	}
	applyTraeHeaders(httpReq, storage)

	httpClient := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		out <- traeSSEEvent{EventType: "error", Data: json.RawMessage(fmt.Sprintf(`{"message":"events request failed: %s"}`, err.Error()))}
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("trae-cn: close events response body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		out <- traeSSEEvent{EventType: "error", Data: json.RawMessage(fmt.Sprintf(`{"message":"events returned status %d: %s"}`, resp.StatusCode, string(body)))}
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(nil, 52_428_800) // 50MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Trae SSE format: "data: {json}" or event-specific lines
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 {
			continue
		}

		var event traeSSEEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		if event.EventType == "" {
			// Try to infer event type from data content
			var raw map[string]interface{}
			if err := json.Unmarshal(data, &raw); err == nil {
				if _, hasThought := raw["thought"]; hasThought {
					event.EventType = "plan_item"
					event.Data = data
				} else if _, hasUsage := raw["token_usage"]; hasUsage {
					event.EventType = "token_usage"
					event.Data = data
				} else if _, hasDone := raw["done"]; hasDone {
					event.EventType = "done"
					event.Data = data
				} else if _, hasErr := raw["error"]; hasErr {
					event.EventType = "error"
					event.Data = data
				}
			}
		}

		select {
		case out <- event:
		case <-ctx.Done():
			return
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case out <- traeSSEEvent{EventType: "error", Data: json.RawMessage(fmt.Sprintf(`{"message":"scanner error: %s"}`, err.Error()))}:
		case <-ctx.Done():
		}
	}
}

// renderNewText extracts the new (delta) text from a plan_item event.
// Trae's plan_item events contain a cumulative "thought" field — each event
// contains the full text so far, not just the delta. This function tracks
// the last seen text per event ID and returns only the new portion.
func renderNewText(lastText *string, data json.RawMessage) string {
	var event struct {
		ID      string `json:"id"`
		Thought string `json:"thought"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return ""
	}

	// If the thought is a prefix of what we've already seen, no new text
	if event.Thought == *lastText {
		return ""
	}

	// Extract the new portion
	if strings.HasPrefix(event.Thought, *lastText) {
		delta := event.Thought[len(*lastText):]
		*lastText = event.Thought
		return delta
	}

	// Text changed entirely — emit full text as delta
	delta := event.Thought
	*lastText = event.Thought
	return delta
}

// buildOpenAIDeltaChunk builds an OpenAI-compatible streaming chunk.
func buildOpenAIDeltaChunk(model, content string, finishReason string) []byte {
	chunk := map[string]interface{}{
		"id":      "trae-cn-" + uuid.NewString(),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"content": content,
				},
				"finish_reason": nil,
			},
		},
	}
	if finishReason != "" {
		chunk["choices"].([]map[string]interface{})[0]["finish_reason"] = finishReason
		chunk["choices"].([]map[string]interface{})[0]["delta"] = map[string]interface{}{}
	}
	b, _ := json.Marshal(chunk)
	return b
}

// buildOpenAICompletion builds an OpenAI-compatible non-streaming response.
func buildOpenAICompletion(model, content string) []byte {
	resp := map[string]interface{}{
		"id":      "trae-cn-" + uuid.NewString(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// ExecuteStream executes a streaming request against the Trae CN SOLO agent API.
func (e *TraeCNExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	storage, err := traeCredentials(auth)
	if err != nil {
		return nil, err
	}

	// Translate non-OpenAI formats to OpenAI before extracting messages
	payload := req.Payload
	if opts.SourceFormat != "" && opts.SourceFormat != sdktranslator.FormatOpenAI {
		payload = sdktranslator.TranslateRequest(opts.SourceFormat, sdktranslator.FormatOpenAI, req.Model, payload, false)
	}

	// Parse request to get model and messages
	var chatReq map[string]interface{}
	if err := json.Unmarshal(payload, &chatReq); err != nil {
		return nil, fmt.Errorf("trae-cn: failed to parse request: %w", err)
	}

	// Map model name — strip provider prefix
	model, _ := chatReq["model"].(string)
	traeModel := strings.TrimPrefix(model, "trae-cn/")

	// Extract and flatten messages
	messagesRaw, _ := chatReq["messages"].([]interface{})
	flatMessages := flattenMessages(messagesRaw)

	// Build the query: join all message contents into a single text
	// Trae SOLO API takes a single "query" field, so we serialize the full
	// conversation as the query
	queryBytes, _ := json.Marshal(flatMessages)

	reporter := helps.NewExecutorUsageReporter(ctx, e, traeModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	// Create chat session
	sessionID, messageID, err := createChatSession(ctx, e.cfg, auth, storage, traeModel, string(queryBytes))
	if err != nil {
		return nil, err
	}

	log.Debugf("trae-cn: created session %s, message %s for model %s", sessionID, messageID, traeModel)

	// Stream events
	eventCh := make(chan traeSSEEvent, 32)
	go streamSessionEvents(ctx, e.cfg, auth, storage, sessionID, messageID, eventCh)

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)

		var streamParam any
		var lastText string

		for event := range eventCh {
			switch event.EventType {
			case "plan_item":
				delta := renderNewText(&lastText, event.Data)
				if delta == "" {
					continue
				}
				// Build OpenAI delta chunk and translate to client format
				chunkBytes := buildOpenAIDeltaChunk(model, delta, "")
				ssePayload := append([]byte("data: "), chunkBytes...)

				to := sdktranslator.FormatOpenAI
				from := opts.SourceFormat
				if from == "" {
					from = to
				}
				frames := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, payload, ssePayload, &streamParam)
				for _, frame := range frames {
					select {
					case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
					case <-ctx.Done():
						return
					}
				}

			case "token_usage":
				// Parse usage and publish
				var usageEvent struct {
					TokenUsage struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					} `json:"token_usage"`
				}
				if err := json.Unmarshal(event.Data, &usageEvent); err == nil {
					detail := usage.Detail{
						InputTokens:  usageEvent.TokenUsage.InputTokens,
						OutputTokens: usageEvent.TokenUsage.OutputTokens,
						TotalTokens:  usageEvent.TokenUsage.InputTokens + usageEvent.TokenUsage.OutputTokens,
					}
					reporter.Publish(ctx, detail)
				}

			case "done":
				// Emit final chunk with finish_reason
				chunkBytes := buildOpenAIDeltaChunk(model, "", "stop")
				ssePayload := append([]byte("data: "), chunkBytes...)

				to := sdktranslator.FormatOpenAI
				from := opts.SourceFormat
				if from == "" {
					from = to
				}
				frames := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, payload, ssePayload, &streamParam)
				for _, frame := range frames {
					select {
					case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
					case <-ctx.Done():
						return
					}
				}
				// Emit [DONE] terminator
				doneFrames := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, payload, []byte("[DONE]"), &streamParam)
				for _, frame := range doneFrames {
					select {
					case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
					case <-ctx.Done():
						return
					}
				}
				reporter.EnsurePublished(ctx)
				return

			case "error":
				var errEvent struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				msg := "unknown error"
				if err := json.Unmarshal(event.Data, &errEvent); err == nil && errEvent.Error.Message != "" {
					msg = errEvent.Error.Message
				}
				streamErr := statusErr{code: http.StatusInternalServerError, msg: msg}
				reporter.PublishFailure(ctx)
				out <- cliproxyexecutor.StreamChunk{Err: streamErr}
				return
			}
		}

		// Event channel closed without explicit "done" — emit terminator
		chunkBytes := buildOpenAIDeltaChunk(model, "", "stop")
		ssePayload := append([]byte("data: "), chunkBytes...)

		to := sdktranslator.FormatOpenAI
		from := opts.SourceFormat
		if from == "" {
			from = to
		}
		frames := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, payload, ssePayload, &streamParam)
		for _, frame := range frames {
			select {
			case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
			case <-ctx.Done():
				return
			}
		}
		doneFrames := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, payload, []byte("[DONE]"), &streamParam)
		for _, frame := range doneFrames {
			select {
			case out <- cliproxyexecutor.StreamChunk{Payload: frame}:
			case <-ctx.Done():
				return
			}
		}
		reporter.EnsurePublished(ctx)
	}()

	return &cliproxyexecutor.StreamResult{
		Headers: http.Header{},
		Chunks:  out,
	}, nil
}

// Execute executes a non-streaming request against the Trae CN SOLO agent API.
func (e *TraeCNExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	// Translate source format to OpenAI, then use ExecuteStream internally
	internalReq := req
	internalOpts := opts
	if opts.SourceFormat != "" && opts.SourceFormat != sdktranslator.FormatOpenAI {
		internalReq.Payload = sdktranslator.TranslateRequest(opts.SourceFormat, sdktranslator.FormatOpenAI, req.Model, req.Payload, false)
	}
	internalOpts.SourceFormat = sdktranslator.FormatOpenAI

	streamResult, err := e.ExecuteStream(ctx, auth, internalReq, internalOpts)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	// Accumulate all chunks
	var content strings.Builder
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return cliproxyexecutor.Response{}, chunk.Err
		}
		raw := chunk.Payload
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("[DONE]")) {
			continue
		}
		// Parse OpenAI chunk to extract delta content
		var oiChunk map[string]interface{}
		if err := json.Unmarshal(raw, &oiChunk); err == nil {
			if choices, ok := oiChunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if contentStr, ok := delta["content"].(string); ok {
							content.WriteString(contentStr)
						}
					}
				}
			}
		}
	}

	// Build final OpenAI completion response
	responseBytes := buildOpenAICompletion(req.Model, content.String())

	// Translate back to client's source format
	var param any
	out := sdktranslator.TranslateNonStream(ctx, sdktranslator.FormatOpenAI, opts.SourceFormat, req.Model, opts.OriginalRequest, internalReq.Payload, responseBytes, &param)

	return cliproxyexecutor.Response{
		Payload: []byte(out),
		Headers: streamResult.Headers,
	}, nil
}

// Refresh re-exchanges the stored refresh token for a new JWT.
func (e *TraeCNExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("trae-cn: auth is nil")
	}

	storage, err := traeCredentials(auth)
	if err != nil {
		return auth, nil // can't refresh without credentials
	}

	if storage.RefreshToken == "" {
		log.Warn("trae-cn: refresh skipped — no refresh token available; re-run --trae-cn-login")
		return auth, nil
	}

	authSvc := traeauth.NewTraeAuth(e.cfg)
	newStorage, err := authSvc.RefreshToken(ctx, storage.ClientID, storage.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("trae-cn: token refresh failed: %w", err)
	}

	// Preserve fields not returned by ExchangeToken
	if newStorage.WebID == "" {
		newStorage.WebID = storage.WebID
	}
	if newStorage.ClientID == "" {
		newStorage.ClientID = storage.ClientID
	}

	updated := auth.Clone()
	updated.Storage = newStorage
	if updated.Metadata == nil {
		updated.Metadata = map[string]any{}
	}
	updated.Metadata["token"] = newStorage.Token
	updated.Metadata["refresh_token"] = newStorage.RefreshToken
	updated.Metadata["user_id"] = newStorage.UserID
	updated.Metadata["client_id"] = newStorage.ClientID
	updated.Metadata["expires_at"] = newStorage.TokenExpireAt.UTC().Format(time.RFC3339)
	now := time.Now()
	updated.UpdatedAt = now
	updated.LastRefreshedAt = now

	log.Infof("trae-cn: refreshed token (expires_at=%s)", newStorage.TokenExpireAt.Format(time.RFC3339))
	return updated, nil
}

// CountTokens is not supported for Trae CN.
func (e *TraeCNExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, fmt.Errorf("trae-cn: count tokens not supported")
}

// HttpRequest executes a raw HTTP request with Trae CN authentication.
func (e *TraeCNExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("trae-cn executor: request is nil")
	}
	storage, err := traeCredentials(auth)
	if err != nil {
		return nil, err
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	applyTraeHeaders(req, storage)
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(req)
}
