// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package fakeopenai is a scripted, OpenAI-compatible chat completions server for tests.
// Point a docker-agent custom provider at it:
//
//	providers:
//	  fake:
//	    api_type: openai_chatcompletions
//	    base_url: <Server.URL>
//	    token_key: FAKE_API_KEY
//	models:
//	  m:
//	    provider: fake
//	    model: fake-model
//
// It answers POST <URL>/chat/completions with server-sent events when the request
// streams (docker-agent always does) and with a plain JSON completion otherwise. It also
// serves a text-only subset of POST <URL>/responses (the Responses API docker-agent uses for
// the `openai` provider's newer models, reachable with OPENAI_BASE_URL=<URL>): text and
// structured answers, no tool calls. It only uses the standard library.
package fakeopenai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// JSON keys used in several responses.
const (
	keyObject = "object"
	keyType   = "type"
)

// Message is one message of a request.
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolCall is a function call, requested by the model or replayed in the history.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Request is a decoded chat completions request.
type Request struct {
	Model         string
	Messages      []Message
	Tools         []string
	Stream        bool
	Authorization string
	// Schema is the JSON schema of a `json_schema` response_format, if any.
	Schema map[string]any
}

// LastUser returns the content of the last user message.
func (r Request) LastUser() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

// System returns the system messages joined by newlines.
func (r Request) System() string {
	var parts []string
	for _, m := range r.Messages {
		if m.Role == "system" || m.Role == "developer" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// ToolResults returns the tool results that follow the last assistant message with tool
// calls, i.e. the answers to the model's previous request.
func (r Request) ToolResults() []Message {
	var out []Message
	for i := len(r.Messages) - 1; i >= 0; i-- {
		m := r.Messages[i]
		if m.Role != "tool" {
			break
		}
		out = append([]Message{m}, out...)
	}
	return out
}

// HasTool reports whether the request offers a tool.
func (r Request) HasTool(name string) bool {
	for _, t := range r.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// Reply scripts the answer to one request.
type Reply struct {
	// Content is the assistant text.
	Content string
	// ToolCalls are requested instead of (or in addition to) Content.
	ToolCalls []ToolCall
	// Status, when set and not 200, makes the server answer with this HTTP status and
	// an OpenAI-style error carrying ErrorMessage.
	Status       int
	ErrorMessage string
	// Delay is waited before answering (or until the client gives up).
	Delay time.Duration
	// PromptTokens and CompletionTokens are reported as usage (defaults: 10 and 5).
	PromptTokens, CompletionTokens int
}

// Handler decides the reply to a request.
type Handler func(Request) Reply

// Server is a running fake.
type Server struct {
	// URL is the base URL for a custom provider's base_url (it ends with /v1).
	URL string

	srv     *httptest.Server
	handler Handler
	token   string

	mu       sync.Mutex
	requests []Request
}

// Option configures a Server.
type Option func(*Server)

// WithToken makes the server reject requests without `Authorization: Bearer <token>`
// with a 401, like OpenAI does for a bad API key.
func WithToken(token string) Option {
	return func(s *Server) { s.token = token }
}

// New starts a server. A nil handler means Default.
func New(handler Handler, opts ...Option) *Server {
	if handler == nil {
		handler = Default
	}
	s := &Server{handler: handler}
	for _, o := range opts {
		o(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.chatCompletions)
	mux.HandleFunc("/v1/responses", s.responses)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{keyObject: "list", "data": []any{}})
	})
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL + "/v1"
	return s
}

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req, err := decodeRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req.Authorization = r.Header.Get("Authorization")
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	if s.token != "" && req.Authorization != "Bearer "+s.token {
		// Like OpenAI, echo (part of) the presented key: callers must not leak it.
		presented := strings.TrimPrefix(req.Authorization, "Bearer ")
		writeError(w, http.StatusUnauthorized, "Incorrect API key provided: "+presented+".", "invalid_request_error")
		return
	}

	reply := s.handler(req)
	if reply.Delay > 0 {
		select {
		case <-time.After(reply.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if reply.Status != 0 && reply.Status != http.StatusOK {
		writeError(w, reply.Status, reply.ErrorMessage, "server_error")
		return
	}
	if reply.PromptTokens == 0 {
		reply.PromptTokens = 10
	}
	if reply.CompletionTokens == 0 {
		reply.CompletionTokens = 5
	}
	if req.Stream {
		streamReply(w, req, reply)
		return
	}
	writeJSON(w, http.StatusOK, completion(req, reply))
}

// responses serves the Responses API (text only).
func (s *Server) responses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req, err := decodeResponsesRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req.Authorization = r.Header.Get("Authorization")
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	if s.token != "" && req.Authorization != "Bearer "+s.token {
		presented := strings.TrimPrefix(req.Authorization, "Bearer ")
		writeError(w, http.StatusUnauthorized, "Incorrect API key provided: "+presented+".", "invalid_request_error")
		return
	}
	reply := s.handler(req)
	if reply.Delay > 0 {
		select {
		case <-time.After(reply.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if reply.Status != 0 && reply.Status != http.StatusOK {
		writeError(w, reply.Status, reply.ErrorMessage, "server_error")
		return
	}
	if len(reply.ToolCalls) > 0 {
		writeError(w, http.StatusNotImplemented, "fakeopenai: tool calls are only supported on /chat/completions", "server_error")
		return
	}
	if reply.PromptTokens == 0 {
		reply.PromptTokens = 10
	}
	if reply.CompletionTokens == 0 {
		reply.CompletionTokens = 5
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(v map[string]any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", v[keyType], b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	const itemID = "msg_fake"
	send(map[string]any{keyType: "response.output_text.delta", "item_id": itemID, "output_index": 0, "content_index": 0,
		"delta": reply.Content})
	send(map[string]any{keyType: "response.completed", "response": map[string]any{
		"id": "resp_fake", keyObject: "response", "status": "completed", "model": req.Model,
		"output": []any{map[string]any{keyType: "message", "id": itemID, "role": "assistant", "status": "completed",
			"content": []any{map[string]any{keyType: "output_text", "text": reply.Content, "annotations": []any{}}}}},
		"usage": map[string]any{
			"input_tokens": reply.PromptTokens, "output_tokens": reply.CompletionTokens,
			"total_tokens": reply.PromptTokens + reply.CompletionTokens,
		},
	}})
}

func decodeResponsesRequest(body []byte) (Request, error) {
	var raw struct {
		Model        string `json:"model"`
		Stream       bool   `json:"stream"`
		Instructions string `json:"instructions"`
		Input        []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"input"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Text *struct {
			Format *struct {
				Type   string         `json:"type"`
				Schema map[string]any `json:"schema"`
			} `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Request{}, fmt.Errorf("invalid request body: %w", err)
	}
	req := Request{Model: raw.Model, Stream: raw.Stream}
	if raw.Instructions != "" {
		req.Messages = append(req.Messages, Message{Role: "system", Content: raw.Instructions})
	}
	for _, in := range raw.Input {
		if in.Role != "" {
			req.Messages = append(req.Messages, Message{Role: in.Role, Content: contentText(in.Content)})
		}
	}
	for _, t := range raw.Tools {
		req.Tools = append(req.Tools, t.Name)
	}
	if raw.Text != nil && raw.Text.Format != nil && raw.Text.Format.Type == "json_schema" {
		req.Schema = raw.Text.Format.Schema
	}
	return req, nil
}

func finishReason(reply Reply) string {
	if len(reply.ToolCalls) > 0 {
		return "tool_calls"
	}
	return "stop"
}

func toolCallsJSON(reply Reply, withIndex bool) []map[string]any {
	var out []map[string]any
	for i, tc := range reply.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}
		m := map[string]any{
			"id":       id,
			keyType:    "function",
			"function": map[string]any{"name": tc.Name, "arguments": tc.Arguments},
		}
		if withIndex {
			m["index"] = i
		}
		out = append(out, m)
	}
	return out
}

func usage(reply Reply) map[string]any {
	return map[string]any{
		"prompt_tokens":     reply.PromptTokens,
		"completion_tokens": reply.CompletionTokens,
		"total_tokens":      reply.PromptTokens + reply.CompletionTokens,
	}
}

func completion(req Request, reply Reply) map[string]any {
	msg := map[string]any{"role": "assistant", "content": reply.Content}
	if tcs := toolCallsJSON(reply, false); len(tcs) > 0 {
		msg["tool_calls"] = tcs
	}
	return map[string]any{
		"id":      "chatcmpl-fake",
		keyObject: "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finishReason(reply)}},
		"usage":   usage(reply),
	}
}

func streamReply(w http.ResponseWriter, req Request, reply Reply) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	chunk := func(choices []any, extra map[string]any) {
		c := map[string]any{
			"id":      "chatcmpl-fake",
			keyObject: "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": choices,
		}
		for k, v := range extra {
			c[k] = v
		}
		b, _ := json.Marshal(c)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	delta := func(d map[string]any, finish any) []any {
		return []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}
	}

	chunk(delta(map[string]any{"role": "assistant", "content": ""}, nil), nil)
	if reply.Content != "" {
		// Two chunks, to exercise reassembly.
		half := len(reply.Content) / 2
		for _, part := range []string{reply.Content[:half], reply.Content[half:]} {
			if part != "" {
				chunk(delta(map[string]any{"content": part}, nil), nil)
			}
		}
	}
	if tcs := toolCallsJSON(reply, true); len(tcs) > 0 {
		chunk(delta(map[string]any{"tool_calls": tcs}, nil), nil)
	}
	chunk(delta(map[string]any{}, finishReason(reply)), nil)
	chunk([]any{}, map[string]any{"usage": usage(reply)})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, keyType: typ, "code": nil}})
}

func decodeRequest(body []byte) (Request, error) {
	var raw struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		ResponseFormat *struct {
			Type       string `json:"type"`
			JSONSchema *struct {
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Request{}, fmt.Errorf("invalid request body: %w", err)
	}
	req := Request{Model: raw.Model, Stream: raw.Stream}
	for _, m := range raw.Messages {
		msg := Message{Role: m.Role, Content: contentText(m.Content), ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range raw.Tools {
		req.Tools = append(req.Tools, t.Function.Name)
	}
	if rf := raw.ResponseFormat; rf != nil && rf.Type == "json_schema" && rf.JSONSchema != nil {
		req.Schema = rf.JSONSchema.Schema
	}
	return req, nil
}

// contentText flattens a message content: a string or an array of parts.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return string(raw)
}

// Default answers with a JSON document matching the response schema when the request
// asks for structured output, and otherwise with "You said: <last user message>".
func Default(req Request) Reply {
	if req.Schema != nil {
		b, _ := json.Marshal(Sample(req.Schema))
		return Reply{Content: string(b)}
	}
	return Reply{Content: "You said: " + req.LastUser()}
}

// Sample builds a value that satisfies a simple JSON schema: the first enum value or
// const, "sample" for strings, 1 for numbers, true for booleans, one-element arrays and
// objects with every property.
func Sample(schema map[string]any) any {
	if v, ok := schema["const"]; ok {
		return v
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	typ, _ := schema["type"].(string)
	if types, ok := schema["type"].([]any); ok && len(types) > 0 {
		typ, _ = types[0].(string)
	}
	switch typ {
	case "object", "":
		out := map[string]any{}
		props, _ := schema["properties"].(map[string]any)
		for name, p := range props {
			if ps, ok := p.(map[string]any); ok {
				out[name] = Sample(ps)
			}
		}
		return out
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{Sample(items)}
		}
		return []any{}
	case "string":
		return "sample"
	case "integer", "number":
		return 1
	case "boolean":
		return true
	default:
		return nil
	}
}
