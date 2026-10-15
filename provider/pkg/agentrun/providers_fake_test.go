// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

// A scripted model server for providers_test.go that speaks every wire protocol docker-agent's
// model providers use, with the standard library only:
//
//   - OpenAI Chat Completions: POST .../chat/completions (SSE or JSON);
//   - OpenAI Responses: POST .../responses (SSE), with text, function calls and json_schema;
//   - Anthropic Messages: POST .../v1/messages (SSE or JSON), and .../v1/messages/count_tokens;
//   - Google Gemini / Vertex AI: POST ...:streamGenerateContent?alt=sse and ...:generateContent;
//   - Amazon Bedrock Converse: POST /model/{id}/converse-stream (AWS event stream) and
//     /model/{id}/converse;
//   - a Google OAuth token endpoint: POST /token (for service-account credentials).
//
// Every request is decoded into one protocol-neutral wireReq, a wireScript decides the
// answer (text, tool calls, usage), and the answer is encoded in the request's protocol.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type wireProto string

const (
	protoChat      wireProto = "openai-chat-completions"
	protoResponses wireProto = "openai-responses"
	protoAnthropic wireProto = "anthropic-messages"
	protoGemini    wireProto = "gemini-generate-content"
	protoBedrock   wireProto = "bedrock-converse"
)

// wireCall is a function call requested by the model or replayed in the history.
type wireCall struct{ ID, Name, Args string }

// wireMsg is one message of a request. Tool results have Role "tool".
type wireMsg struct {
	Role   string
	Text   string
	Calls  []wireCall
	CallID string
}

// wireReq is a decoded model request.
type wireReq struct {
	Proto    wireProto
	Path     string
	Query    url.Values
	Header   http.Header
	Model    string
	Stream   bool
	System   string
	Messages []wireMsg
	Tools    []string
	// Schema is the response schema sent with the protocol's native structured output.
	Schema map[string]any
	Body   map[string]any
}

func (r wireReq) lastUser() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" && r.Messages[i].Text != "" {
			return r.Messages[i].Text
		}
	}
	return ""
}

// toolResults returns the tool results after the last assistant message.
func (r wireReq) toolResults() []wireMsg {
	var out []wireMsg
	for i := len(r.Messages) - 1; i >= 0; i-- {
		m := r.Messages[i]
		if m.Role == "assistant" {
			break
		}
		if m.Role == "tool" {
			out = append([]wireMsg{m}, out...)
		}
	}
	return out
}

func (r wireReq) toolResultText() string {
	var parts []string
	for _, m := range r.toolResults() {
		parts = append(parts, m.Text)
	}
	return strings.Join(parts, "\n")
}

func (r wireReq) hasTool(name string) bool {
	for _, t := range r.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// credential returns the credential the request presents, whatever the protocol.
func (r wireReq) credential() string {
	for _, h := range []string{"X-Api-Key", "X-Goog-Api-Key", "Api-Key"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	if v := r.Query.Get("key"); v != "" {
		return v
	}
	a := r.Header.Get("Authorization")
	if strings.HasPrefix(a, "AWS4-HMAC-SHA256 ") {
		// SigV4: the access key ID is the credential's first part.
		if _, rest, ok := strings.Cut(a, "Credential="); ok {
			id, _, _ := strings.Cut(rest, "/")
			return "sigv4:" + id
		}
	}
	return strings.TrimPrefix(a, "Bearer ")
}

// wireReply scripts the answer to one request.
type wireReply struct {
	Text    string
	Calls   []wireCall
	In, Out int
	// Status, when set and not 200, answers with this HTTP status and an error.
	Status int
	Err    string
}

type wireScript func(wireReq) wireReply

// fakeModels is a running fake model server.
type fakeModels struct {
	URL string

	srv    *httptest.Server
	script wireScript

	mu        sync.Mutex
	reqs      []wireReq
	unhandled []string
}

func newFakeModels(t *testing.T, script wireScript) *fakeModels {
	t.Helper()
	f := &fakeModels{script: script}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.URL = f.srv.URL
	return f
}

// newFakeModelsTLS serves HTTPS (the AWS SDK sends bearer tokens over HTTPS only) and makes the
// run children trust its certificate: docker-agent's HTTP client clones http.DefaultTransport
// (pkg/httpclient/desktop_transport.go:85), whose TLS configuration a run child of the test
// binary sets from testFaults.rootCAs (childtest.go). This process trusts it too.
func newFakeModelsTLS(t *testing.T, script wireScript) *fakeModels {
	t.Helper()
	f := &fakeModels{script: script}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.URL = f.srv.URL
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	dt := http.DefaultTransport.(*http.Transport)
	prev, prevCAs := dt.TLSClientConfig, testFaults.rootCAs
	dt.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	testFaults.rootCAs = []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw}))}
	t.Cleanup(func() { dt.TLSClientConfig, testFaults.rootCAs = prev, prevCAs })
	return f
}

func (f *fakeModels) requests() []wireReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wireReq(nil), f.reqs...)
}

// other returns the requests the fake does not serve ("METHOD path").
func (f *fakeModels) other() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.unhandled...)
}

func (f *fakeModels) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p := r.URL.Path
	var proto wireProto
	switch {
	case r.Method == http.MethodPost && p == "/token":
		writeJSONBody(w, http.StatusOK, map[string]any{"access_token": "ya29.fake-google-access-token",
			"token_type": "Bearer", "expires_in": 3600})
		return
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/messages/count_tokens"):
		writeJSONBody(w, http.StatusOK, map[string]any{"input_tokens": 12})
		return
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/chat/completions"):
		proto = protoChat
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/responses"):
		proto = protoResponses
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/v1/messages"):
		proto = protoAnthropic
	case r.Method == http.MethodPost && (strings.HasSuffix(p, ":streamGenerateContent") || strings.HasSuffix(p, ":generateContent")):
		proto = protoGemini
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/model/") &&
		(strings.HasSuffix(p, "/converse-stream") || strings.HasSuffix(p, "/converse")):
		proto = protoBedrock
	default:
		f.mu.Lock()
		f.unhandled = append(f.unhandled, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		http.NotFound(w, r)
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "fake: invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	req := wireReq{Proto: proto, Path: p, Query: r.URL.Query(), Header: r.Header.Clone(), Body: raw}
	switch proto {
	case protoChat:
		decodeChat(&req, raw)
	case protoResponses:
		decodeResponses(&req, raw)
	case protoAnthropic:
		decodeAnthropic(&req, raw)
	case protoGemini:
		decodeGemini(&req, raw, p)
	case protoBedrock:
		decodeBedrock(&req, raw, p)
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()

	reply := f.script(req)
	if reply.In == 0 {
		reply.In = 12
	}
	if reply.Out == 0 {
		reply.Out = 3
	}
	for i := range reply.Calls {
		if reply.Calls[i].ID == "" {
			reply.Calls[i].ID = fmt.Sprintf("call_%d_%d", len(f.requests()), i)
		}
	}
	if reply.Status != 0 && reply.Status != http.StatusOK {
		writeProtoError(w, proto, reply.Status, reply.Err)
		return
	}
	switch proto {
	case protoChat:
		encodeChat(w, req, reply)
	case protoResponses:
		encodeResponses(w, req, reply)
	case protoAnthropic:
		encodeAnthropic(w, req, reply)
	case protoGemini:
		encodeGemini(w, req, reply)
	case protoBedrock:
		encodeBedrock(w, req, reply)
	}
}

// --- decoding helpers -------------------------------------------------------------------------

func str(v any) string { s, _ := v.(string); return s }

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func arr(v any) []any { a, _ := v.([]any); return a }

// text flattens a string or a list of text parts ({type,text} or {text}).
func text(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			b.WriteString(str(obj(p)["text"]))
		}
		return b.String()
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeChat(req *wireReq, raw map[string]any) {
	req.Model, _ = raw["model"].(string)
	req.Stream, _ = raw["stream"].(bool)
	for _, mv := range arr(raw["messages"]) {
		m := obj(mv)
		msg := wireMsg{Role: str(m["role"]), Text: text(m["content"]), CallID: str(m["tool_call_id"])}
		if msg.Role == "system" || msg.Role == "developer" {
			req.System += msg.Text + "\n"
		}
		for _, tc := range arr(m["tool_calls"]) {
			fn := obj(obj(tc)["function"])
			msg.Calls = append(msg.Calls, wireCall{ID: str(obj(tc)["id"]), Name: str(fn["name"]), Args: str(fn["arguments"])})
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range arr(raw["tools"]) {
		req.Tools = append(req.Tools, str(obj(obj(t)["function"])["name"]))
	}
	if rf := obj(raw["response_format"]); str(rf["type"]) == "json_schema" {
		req.Schema = obj(obj(rf["json_schema"])["schema"])
	}
}

func decodeResponses(req *wireReq, raw map[string]any) {
	req.Model, _ = raw["model"].(string)
	req.Stream, _ = raw["stream"].(bool)
	if s := str(raw["instructions"]); s != "" {
		req.System += s + "\n"
	}
	for _, iv := range arr(raw["input"]) {
		in := obj(iv)
		switch str(in["type"]) {
		case "function_call":
			req.Messages = append(req.Messages, wireMsg{Role: "assistant", Calls: []wireCall{{
				ID: str(in["call_id"]), Name: str(in["name"]), Args: str(in["arguments"])}}})
		case "function_call_output":
			req.Messages = append(req.Messages, wireMsg{Role: "tool", CallID: str(in["call_id"]), Text: text(in["output"])})
		default:
			msg := wireMsg{Role: str(in["role"]), Text: text(in["content"])}
			if msg.Role == "system" || msg.Role == "developer" {
				req.System += msg.Text + "\n"
			}
			if msg.Role != "" {
				req.Messages = append(req.Messages, msg)
			}
		}
	}
	for _, t := range arr(raw["tools"]) {
		req.Tools = append(req.Tools, str(obj(t)["name"]))
	}
	if f := obj(obj(raw["text"])["format"]); str(f["type"]) == "json_schema" {
		req.Schema = obj(f["schema"])
	}
}

func decodeAnthropic(req *wireReq, raw map[string]any) {
	req.Model, _ = raw["model"].(string)
	req.Stream, _ = raw["stream"].(bool)
	req.System = text(raw["system"])
	for _, mv := range arr(raw["messages"]) {
		m := obj(mv)
		role := str(m["role"])
		if s, ok := m["content"].(string); ok {
			req.Messages = append(req.Messages, wireMsg{Role: role, Text: s})
			continue
		}
		msg := wireMsg{Role: role}
		for _, bv := range arr(m["content"]) {
			b := obj(bv)
			switch str(b["type"]) {
			case "text":
				msg.Text += str(b["text"])
			case "tool_use":
				msg.Calls = append(msg.Calls, wireCall{ID: str(b["id"]), Name: str(b["name"]), Args: jsonString(b["input"])})
			case "tool_result":
				req.Messages = append(req.Messages, wireMsg{Role: "tool", CallID: str(b["tool_use_id"]), Text: text(b["content"])})
			}
		}
		if msg.Text != "" || len(msg.Calls) > 0 {
			req.Messages = append(req.Messages, msg)
		}
	}
	for _, t := range arr(raw["tools"]) {
		req.Tools = append(req.Tools, str(obj(t)["name"]))
	}
	if f := obj(obj(raw["output_config"])["format"]); str(f["type"]) == "json_schema" {
		req.Schema = obj(f["schema"])
	}
	if f := obj(raw["output_format"]); str(f["type"]) == "json_schema" {
		req.Schema = obj(f["schema"])
	}
}

func decodeGemini(req *wireReq, raw map[string]any, path string) {
	// .../models/{model}:streamGenerateContent
	name := path[strings.LastIndex(path, "/models/")+len("/models/"):]
	req.Model, _, _ = strings.Cut(name, ":")
	req.Stream = strings.HasSuffix(path, ":streamGenerateContent")
	req.System = text(obj(raw["systemInstruction"])["parts"])
	for _, cv := range arr(raw["contents"]) {
		c := obj(cv)
		role := str(c["role"])
		if role == "model" {
			role = "assistant"
		}
		msg := wireMsg{Role: role}
		for _, pv := range arr(c["parts"]) {
			p := obj(pv)
			switch {
			case p["functionCall"] != nil:
				fc := obj(p["functionCall"])
				msg.Calls = append(msg.Calls, wireCall{ID: str(fc["id"]), Name: str(fc["name"]), Args: jsonString(fc["args"])})
			case p["functionResponse"] != nil:
				fr := obj(p["functionResponse"])
				req.Messages = append(req.Messages, wireMsg{Role: "tool", CallID: cmpOr(str(fr["id"]), str(fr["name"])),
					Text: jsonString(fr["response"])})
			default:
				msg.Text += str(p["text"])
			}
		}
		if msg.Text != "" || len(msg.Calls) > 0 {
			req.Messages = append(req.Messages, msg)
		}
	}
	for _, tv := range arr(raw["tools"]) {
		for _, fd := range arr(obj(tv)["functionDeclarations"]) {
			req.Tools = append(req.Tools, str(obj(fd)["name"]))
		}
	}
	gc := obj(raw["generationConfig"])
	if s := obj(gc["responseJsonSchema"]); s != nil {
		req.Schema = s
	} else if s := obj(gc["responseSchema"]); s != nil {
		req.Schema = s
	}
}

func decodeBedrock(req *wireReq, raw map[string]any, path string) {
	id := strings.TrimPrefix(path, "/model/")
	id = strings.TrimSuffix(strings.TrimSuffix(id, "/converse-stream"), "/converse")
	req.Model, _ = url.PathUnescape(id)
	req.Stream = strings.HasSuffix(path, "/converse-stream")
	req.System = text(raw["system"])
	for _, mv := range arr(raw["messages"]) {
		m := obj(mv)
		msg := wireMsg{Role: str(m["role"])}
		for _, bv := range arr(m["content"]) {
			b := obj(bv)
			switch {
			case b["toolUse"] != nil:
				tu := obj(b["toolUse"])
				msg.Calls = append(msg.Calls, wireCall{ID: str(tu["toolUseId"]), Name: str(tu["name"]), Args: jsonString(tu["input"])})
			case b["toolResult"] != nil:
				tr := obj(b["toolResult"])
				var parts []string
				for _, cv := range arr(tr["content"]) {
					c := obj(cv)
					if c["json"] != nil {
						parts = append(parts, jsonString(c["json"]))
					} else {
						parts = append(parts, str(c["text"]))
					}
				}
				req.Messages = append(req.Messages, wireMsg{Role: "tool", CallID: str(tr["toolUseId"]), Text: strings.Join(parts, "\n")})
			default:
				msg.Text += str(b["text"])
			}
		}
		if msg.Text != "" || len(msg.Calls) > 0 {
			req.Messages = append(req.Messages, msg)
		}
	}
	for _, tv := range arr(obj(raw["toolConfig"])["tools"]) {
		req.Tools = append(req.Tools, str(obj(obj(tv)["toolSpec"])["name"]))
	}
	// Bedrock's Converse API has an outputConfig for structured output in newer API versions.
	if s := obj(obj(obj(raw["outputConfig"])["textFormat"])["structure"]); s != nil {
		req.Schema = s
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// --- encoding helpers -------------------------------------------------------------------------

func writeJSONBody(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProtoError(w http.ResponseWriter, proto wireProto, status int, msg string) {
	switch proto {
	case protoAnthropic:
		writeJSONBody(w, status, map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": msg}})
	case protoGemini:
		writeJSONBody(w, status, map[string]any{"error": map[string]any{"code": status, "message": msg, "status": "INTERNAL"}})
	case protoBedrock:
		w.Header().Set("X-Amzn-Errortype", "InternalServerException")
		writeJSONBody(w, status, map[string]any{"message": msg})
	default:
		writeJSONBody(w, status, map[string]any{"error": map[string]any{"message": msg, "type": "server_error", "code": nil}})
	}
}

type sse struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) *sse {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	return &sse{w: w, f: f}
}

// send writes one event; an empty event name writes a data-only event.
func (s *sse) send(event string, v any, sep string) {
	b, _ := json.Marshal(v)
	if event != "" {
		_, _ = fmt.Fprintf(s.w, "event: %s%s", event, sep)
	}
	_, _ = fmt.Fprintf(s.w, "data: %s%s%s", b, sep, sep)
	if s.f != nil {
		s.f.Flush()
	}
}

func encodeChat(w http.ResponseWriter, req wireReq, reply wireReply) {
	finish := "stop"
	if len(reply.Calls) > 0 {
		finish = "tool_calls"
	}
	usage := map[string]any{"prompt_tokens": reply.In, "completion_tokens": reply.Out, "total_tokens": reply.In + reply.Out}
	var calls []map[string]any
	for i, c := range reply.Calls {
		calls = append(calls, map[string]any{"index": i, "id": c.ID, "type": "function",
			"function": map[string]any{"name": c.Name, "arguments": c.Args}})
	}
	if !req.Stream {
		msg := map[string]any{"role": "assistant", "content": reply.Text}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		writeJSONBody(w, http.StatusOK, map[string]any{"id": "chatcmpl-fake", "object": "chat.completion", "created": 1,
			"model": req.Model, "choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage": usage})
		return
	}
	s := newSSE(w)
	chunk := func(choices []any, extra map[string]any) {
		c := map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": 1, "model": req.Model,
			"choices": choices}
		for k, v := range extra {
			c[k] = v
		}
		s.send("", c, "\n")
	}
	delta := func(d map[string]any, finish any) []any {
		return []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}
	}
	chunk(delta(map[string]any{"role": "assistant", "content": ""}, nil), nil)
	if reply.Text != "" {
		half := len(reply.Text) / 2
		for _, part := range []string{reply.Text[:half], reply.Text[half:]} {
			if part != "" {
				chunk(delta(map[string]any{"content": part}, nil), nil)
			}
		}
	}
	if len(calls) > 0 {
		chunk(delta(map[string]any{"tool_calls": calls}, nil), nil)
	}
	chunk(delta(map[string]any{}, finish), nil)
	chunk([]any{}, map[string]any{"usage": usage})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if s.f != nil {
		s.f.Flush()
	}
}

func encodeResponses(w http.ResponseWriter, req wireReq, reply wireReply) {
	s := newSSE(w)
	send := func(v map[string]any) { s.send(str(v["type"]), v, "\n") }
	resp := func(status string, output []any) map[string]any {
		r := map[string]any{"id": "resp_fake", "object": "response", "created_at": 1, "status": status, "model": req.Model,
			"output": output}
		if status == "completed" {
			r["usage"] = map[string]any{"input_tokens": reply.In, "output_tokens": reply.Out,
				"total_tokens": reply.In + reply.Out, "input_tokens_details": map[string]any{"cached_tokens": 0},
				"output_tokens_details": map[string]any{"reasoning_tokens": 0}}
		}
		return r
	}
	send(map[string]any{"type": "response.created", "sequence_number": 0, "response": resp("in_progress", []any{})})
	var output []any
	idx := 0
	if reply.Text != "" {
		const itemID = "msg_fake"
		item := map[string]any{"type": "message", "id": itemID, "role": "assistant", "status": "in_progress", "content": []any{}}
		send(map[string]any{"type": "response.output_item.added", "output_index": idx, "item": item})
		send(map[string]any{"type": "response.output_text.delta", "item_id": itemID, "output_index": idx, "content_index": 0,
			"delta": reply.Text})
		done := map[string]any{"type": "message", "id": itemID, "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "output_text", "text": reply.Text, "annotations": []any{}}}}
		send(map[string]any{"type": "response.output_item.done", "output_index": idx, "item": done})
		output = append(output, done)
		idx++
	}
	for _, c := range reply.Calls {
		itemID := "fc_" + c.ID
		added := map[string]any{"type": "function_call", "id": itemID, "call_id": c.ID, "name": c.Name, "arguments": "",
			"status": "in_progress"}
		send(map[string]any{"type": "response.output_item.added", "output_index": idx, "item": added})
		send(map[string]any{"type": "response.function_call_arguments.delta", "item_id": itemID, "output_index": idx,
			"delta": c.Args})
		send(map[string]any{"type": "response.function_call_arguments.done", "item_id": itemID, "output_index": idx,
			"arguments": c.Args})
		done := map[string]any{"type": "function_call", "id": itemID, "call_id": c.ID, "name": c.Name, "arguments": c.Args,
			"status": "completed"}
		send(map[string]any{"type": "response.output_item.done", "output_index": idx, "item": done})
		output = append(output, done)
		idx++
	}
	send(map[string]any{"type": "response.completed", "response": resp("completed", output)})
}

func encodeAnthropic(w http.ResponseWriter, req wireReq, reply wireReply) {
	stop := "end_turn"
	if len(reply.Calls) > 0 {
		stop = "tool_use"
	}
	if !req.Stream {
		var content []any
		if reply.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": reply.Text})
		}
		for _, c := range reply.Calls {
			var input any
			_ = json.Unmarshal([]byte(c.Args), &input)
			content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": input})
		}
		writeJSONBody(w, http.StatusOK, map[string]any{"id": "msg_fake", "type": "message", "role": "assistant",
			"model": req.Model, "content": content, "stop_reason": stop, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": reply.In, "output_tokens": reply.Out}})
		return
	}
	s := newSSE(w)
	send := func(v map[string]any) { s.send(str(v["type"]), v, "\n") }
	send(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_fake", "type": "message",
		"role": "assistant", "model": req.Model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": reply.In, "output_tokens": 1}}})
	idx := 0
	if reply.Text != "" {
		send(map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "text", "text": ""}})
		send(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "text_delta", "text": reply.Text}})
		send(map[string]any{"type": "content_block_stop", "index": idx})
		idx++
	}
	for _, c := range reply.Calls {
		send(map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "tool_use",
			"id": c.ID, "name": c.Name, "input": map[string]any{}}})
		send(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "input_json_delta",
			"partial_json": c.Args}})
		send(map[string]any{"type": "content_block_stop", "index": idx})
		idx++
	}
	send(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": reply.Out}})
	send(map[string]any{"type": "message_stop"})
}

func encodeGemini(w http.ResponseWriter, req wireReq, reply wireReply) {
	var parts []any
	if reply.Text != "" {
		parts = append(parts, map[string]any{"text": reply.Text})
	}
	for _, c := range reply.Calls {
		var args any
		_ = json.Unmarshal([]byte(c.Args), &args)
		parts = append(parts, map[string]any{"functionCall": map[string]any{"id": c.ID, "name": c.Name, "args": args}})
	}
	resp := map[string]any{
		"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts},
			"finishReason": "STOP", "index": 0}},
		"usageMetadata": map[string]any{"promptTokenCount": reply.In, "candidatesTokenCount": reply.Out,
			"totalTokenCount": reply.In + reply.Out},
		"modelVersion": req.Model, "responseId": "fake-response",
	}
	if !req.Stream {
		writeJSONBody(w, http.StatusOK, resp)
		return
	}
	newSSE(w).send("", resp, "\r\n")
}

// encodeBedrock answers in the AWS event stream encoding (application/vnd.amazon.eventstream).
func encodeBedrock(w http.ResponseWriter, req wireReq, reply wireReply) {
	stop := "end_turn"
	if len(reply.Calls) > 0 {
		stop = "tool_use"
	}
	usage := map[string]any{"inputTokens": reply.In, "outputTokens": reply.Out, "totalTokens": reply.In + reply.Out}
	if !req.Stream {
		var content []any
		if reply.Text != "" {
			content = append(content, map[string]any{"text": reply.Text})
		}
		for _, c := range reply.Calls {
			var input any
			_ = json.Unmarshal([]byte(c.Args), &input)
			content = append(content, map[string]any{"toolUse": map[string]any{"toolUseId": c.ID, "name": c.Name, "input": input}})
		}
		writeJSONBody(w, http.StatusOK, map[string]any{"output": map[string]any{"message": map[string]any{"role": "assistant",
			"content": content}}, "stopReason": stop, "usage": usage, "metrics": map[string]any{"latencyMs": 1}})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	event := func(name string, payload any) {
		b, _ := json.Marshal(payload)
		_, _ = w.Write(eventStreamMessage([][2]string{
			{":event-type", name}, {":content-type", "application/json"}, {":message-type", "event"},
		}, b))
		if flusher != nil {
			flusher.Flush()
		}
	}
	event("messageStart", map[string]any{"role": "assistant"})
	idx := 0
	if reply.Text != "" {
		event("contentBlockDelta", map[string]any{"contentBlockIndex": idx, "delta": map[string]any{"text": reply.Text}})
		event("contentBlockStop", map[string]any{"contentBlockIndex": idx})
		idx++
	}
	for _, c := range reply.Calls {
		event("contentBlockStart", map[string]any{"contentBlockIndex": idx, "start": map[string]any{"toolUse": map[string]any{
			"toolUseId": c.ID, "name": c.Name}}})
		event("contentBlockDelta", map[string]any{"contentBlockIndex": idx, "delta": map[string]any{"toolUse": map[string]any{
			"input": c.Args}}})
		event("contentBlockStop", map[string]any{"contentBlockIndex": idx})
		idx++
	}
	event("messageStop", map[string]any{"stopReason": stop})
	event("metadata", map[string]any{"usage": usage, "metrics": map[string]any{"latencyMs": 1}})
}

// eventStreamMessage encodes one AWS event stream message: prelude (total and header
// lengths, CRC32), string headers, payload, message CRC32.
func eventStreamMessage(headers [][2]string, payload []byte) []byte {
	var hb bytes.Buffer
	for _, h := range headers {
		hb.WriteByte(byte(len(h[0])))
		hb.WriteString(h[0])
		hb.WriteByte(7) // string
		_ = binary.Write(&hb, binary.BigEndian, uint16(len(h[1])))
		hb.WriteString(h[1])
	}
	total := 12 + hb.Len() + len(payload) + 4
	var msg bytes.Buffer
	_ = binary.Write(&msg, binary.BigEndian, uint32(total))
	_ = binary.Write(&msg, binary.BigEndian, uint32(hb.Len()))
	_ = binary.Write(&msg, binary.BigEndian, crc32.ChecksumIEEE(msg.Bytes()))
	msg.Write(hb.Bytes())
	msg.Write(payload)
	_ = binary.Write(&msg, binary.BigEndian, crc32.ChecksumIEEE(msg.Bytes()))
	return msg.Bytes()
}
