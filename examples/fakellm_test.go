// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeLLM is a minimal OpenAI-compatible chat completions server for the AgentRun example,
// so the end-to-end test needs no real API key. It answers with a JSON document matching the
// requested response schema, or echoes the last user message, and reports 10 prompt and 5
// completion tokens. It streams server-sent events like the OpenAI API, because docker-agent
// streams. (provider/pkg/agentrun/fakeopenai is the scripted version the provider tests use;
// this module does not depend on the provider module.)
type fakeLLM struct {
	URL string // base URL for a custom provider's baseUrl (ends with /v1)

	mu       sync.Mutex
	requests []fakeLLMRequest
}

type fakeLLMRequest struct {
	Authorization string
	Model         string
	System        string
	User          string
	Schema        map[string]any
}

func newFakeLLM(t *testing.T) *fakeLLM {
	t.Helper()
	f := &fakeLLM{}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL + "/v1"
	return f
}

func (f *fakeLLM) received() []fakeLLMRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeLLMRequest(nil), f.requests...)
}

func (f *fakeLLM) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		ResponseFormat *struct {
			JSONSchema *struct {
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rec := fakeLLMRequest{Authorization: r.Header.Get("Authorization"), Model: req.Model}
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			rec.System += messageText(m.Content) + "\n"
		case "user":
			rec.User = messageText(m.Content)
		}
	}
	if rf := req.ResponseFormat; rf != nil && rf.JSONSchema != nil {
		rec.Schema = rf.JSONSchema.Schema
	}
	f.mu.Lock()
	f.requests = append(f.requests, rec)
	f.mu.Unlock()

	answer := "You said: " + rec.User
	if rec.Schema != nil {
		b, _ := json.Marshal(sampleFor(rec.Schema))
		answer = string(b)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(v map[string]any) {
		v["id"], v["object"], v["model"] = "chatcmpl-fake", "chat.completion.chunk", req.Model
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	choice := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	send(choice(map[string]any{"role": "assistant", "content": answer}, nil))
	send(choice(map[string]any{}, "stop"))
	send(map[string]any{"choices": []any{}, "usage": map[string]any{
		"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
	}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func messageText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// sampleFor builds a value matching a simple JSON schema: the first enum value, "sample" for
// strings, 1 for numbers, true for booleans and objects with every property.
func sampleFor(schema map[string]any) any {
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	switch schema["type"] {
	case "string":
		return "sample"
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{sampleFor(items)}
		}
		return []any{}
	default:
		out := map[string]any{}
		props, _ := schema["properties"].(map[string]any)
		for name, p := range props {
			if ps, ok := p.(map[string]any); ok {
				out[name] = sampleFor(ps)
			}
		}
		return out
	}
}
