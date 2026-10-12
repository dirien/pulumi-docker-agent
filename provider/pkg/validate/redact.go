// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Redacted is the replacement for secret values in messages.
const Redacted = "[secret]"

// Redact replaces every occurrence of the given secret values in msg with [secret]. For
// multi-line secrets (such as a secret YAML document) each line and each `key: value`
// value of at least 8 characters is redacted as well.
//
// Messages often quote values in an escaped form: tool-call arguments and results are JSON,
// and Go errors quote with %q. A secret is therefore also redacted in its JSON-escaped forms
// (with and without HTML escaping of <, > and &, and with non-ASCII characters as \u escapes,
// as Python's json module writes them) and its Go-quoted forms. When msg is a JSON object or
// array, its strings are decoded, redacted and encoded again, so that a secret is found
// whatever escaping the JSON uses.
func Redact(msg string, secrets []string) string {
	candidates := redactionCandidates(secrets)
	if len(candidates) == 0 {
		return msg
	}
	if out, ok := redactJSON(msg, candidates); ok {
		msg = out
	}
	return replaceCandidates(msg, candidates)
}

// redactionCandidates returns the strings to replace for the secrets, longest first so that
// a value is not partially replaced by a shorter one.
func redactionCandidates(secrets []string) []string {
	var candidates []string
	add := func(s string, minLen int) {
		s = strings.TrimSpace(s)
		if len(s) < minLen {
			return
		}
		candidates = append(candidates, s)
		candidates = append(candidates, escapedForms(s)...)
	}
	for _, s := range secrets {
		add(s, 4)
		if !strings.Contains(s, "\n") {
			continue
		}
		for _, line := range strings.Split(s, "\n") {
			add(line, 8)
			if _, v, ok := strings.Cut(line, ": "); ok {
				add(strings.Trim(strings.TrimSpace(v), `"'`), 8)
			}
		}
	}
	slices.SortFunc(candidates, func(a, b string) int {
		if d := len(b) - len(a); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return slices.Compact(candidates)
}

// escapedForms returns the forms other than s itself in which messages quote s: inside a
// JSON string (Go's encoding/json with and without HTML escaping, ASCII-only like Python's
// json.dumps, and a JSON string within a JSON string) and inside a Go-quoted string.
func escapedForms(s string) []string {
	jsonHTML := jsonEscape(s, true)
	jsonPlain := jsonEscape(s, false)
	forms := []string{
		jsonHTML,
		jsonPlain,
		asciiEscape(jsonHTML),
		asciiEscape(jsonPlain),
		jsonEscape(jsonHTML, true),
		jsonEscape(jsonPlain, false),
		goQuote(strconv.Quote(s)),
		goQuote(strconv.QuoteToASCII(s)),
	}
	return slices.DeleteFunc(forms, func(f string) bool { return f == s })
}

// jsonEscape returns s as the content of a JSON string, without the quotes.
func jsonEscape(s string, escapeHTML bool) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(b.String(), `"`), "\n"), `"`)
}

// asciiEscape replaces the non-ASCII characters of a JSON string's content with \u escapes
// (UTF-16 surrogate pairs above U+FFFF), as Python's json.dumps does by default.
func asciiEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < utf8.RuneSelf {
			b.WriteRune(r)
			continue
		}
		if r1, r2 := utf16.EncodeRune(r); r1 != utf8.RuneError {
			fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
			continue
		}
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	return b.String()
}

// goQuote strips the quotes of a strconv.Quote result.
func goQuote(q string) string {
	return q[1 : len(q)-1]
}

func replaceCandidates(msg string, candidates []string) string {
	for _, c := range candidates {
		msg = strings.ReplaceAll(msg, c, Redacted)
	}
	return msg
}

// redactJSON redacts the strings (keys and values) of msg when it is a JSON object or array
// and returns it encoded again, compact and in its original order. It reports false when msg
// is not such a document or contains no secret, and the caller keeps msg as it is.
func redactJSON(msg string, candidates []string) (string, bool) {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
		return "", false
	}
	type frame struct {
		object bool
		n      int // the keys and values written so far
	}
	var (
		stack   []frame
		out     strings.Builder
		changed bool
	)
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	// separate writes the separator before the next key or value of the innermost container.
	separate := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		switch {
		case top.object && top.n%2 == 1:
			out.WriteByte(':')
		case top.n > 0:
			out.WriteByte(',')
		}
	}
	written := func() {
		if len(stack) > 0 {
			stack[len(stack)-1].n++
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				separate()
				out.WriteRune(rune(v))
				stack = append(stack, frame{object: v == '{'})
			default:
				out.WriteRune(rune(v))
				stack = stack[:len(stack)-1]
				written()
			}
		case string:
			separate()
			r := replaceCandidates(v, candidates)
			changed = changed || r != v
			out.WriteByte('"')
			out.WriteString(jsonEscape(r, false))
			out.WriteByte('"')
			written()
		case json.Number:
			separate()
			out.WriteString(v.String())
			written()
		case bool:
			separate()
			out.WriteString(strconv.FormatBool(v))
			written()
		case nil:
			separate()
			out.WriteString("null")
			written()
		}
	}
	if !changed {
		return "", false
	}
	return out.String(), true
}
