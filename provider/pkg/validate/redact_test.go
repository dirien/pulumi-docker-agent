// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Secrets with characters that JSON and Go quoting escape.
var escapedSecrets = []string{`p@ss"w0rd\with-quote`, `a<b>&c`, "ünïcödé-sécret", "tab\there-1234", "emoji-🔑-key"}

// TestRedactEscapedForms checks that a secret is redacted in the escaped forms in which tool
// arguments, tool results and error messages quote it, not only verbatim.
func TestRedactEscapedForms(t *testing.T) {
	t.Parallel()
	for _, secret := range escapedSecrets {
		jsonHTML, _ := json.Marshal(secret)
		jsonPlain := `"` + jsonEscape(secret, false) + `"`
		nested, _ := json.Marshal(string(jsonHTML))
		forms := map[string]string{
			"raw":                   secret,
			"JSON":                  string(jsonHTML),
			"JSON without HTML":     jsonPlain,
			"JSON, ASCII only":      `"` + asciiEscape(jsonEscape(secret, false)) + `"`,
			"JSON within JSON":      string(nested),
			"Go %q":                 fmt.Sprintf("%q", secret),
			"Go %+q":                fmt.Sprintf("%+q", secret),
			"JSON object, verbatim": `{"cmd":"login --password ` + jsonEscape(secret, false) + `"}`,
		}
		for name, form := range forms {
			msg := "value " + form + " end"
			got := Redact(msg, []string{secret})
			assert.Contains(t, got, Redacted, "%s form of %q: %s", name, secret, got)
			assert.NotContains(t, got, strings.Trim(form, `"`), "%s form of %q leaks: %s", name, secret, got)
		}
	}
}

// TestRedactJSONDocuments checks that the strings of a JSON object or array are decoded and
// redacted, whatever escaping the JSON uses, and that the document keeps its order.
func TestRedactJSONDocuments(t *testing.T) {
	t.Parallel()
	secret := `p@ss"w0rd\with-quote`
	// \u escapes for every character, as no encoder writes by default.
	var escaped strings.Builder
	for _, r := range secret {
		fmt.Fprintf(&escaped, `\u%04X`, r)
	}
	doc := `{"z": 1, "cmd": "login ` + escaped.String() + `", "list": [true, null, 2.50, {"k": "a<b>&c"}]}`
	got := Redact(doc, []string{secret, "a<b>&c"})
	assert.Equal(t, `{"z":1,"cmd":"login [secret]","list":[true,null,2.50,{"k":"[secret]"}]}`, got)

	// A document without secrets is returned as it is.
	plain := `{"b": "x",  "a": [1, 2]}`
	assert.Equal(t, plain, Redact(plain, []string{secret}))
	// A key can be a secret too, here with HTML characters as Go escapes them.
	htmlSecret := "a<b>&c"
	htmlEscaped := jsonEscape(htmlSecret, true)
	require.Equal(t, "a"+bs+"u003cb"+bs+"u003e"+bs+"u0026c", htmlEscaped)
	assert.Equal(t, `{"[secret]":"v"}`, Redact(`{"`+htmlEscaped+`": "v"}`, []string{htmlSecret}))
	// Text that is not a JSON document is redacted verbatim and in its escaped forms.
	assert.Equal(t, `{"cmd": "[secret]" (truncated`, Redact(`{"cmd": "`+htmlEscaped+`" (truncated`, []string{htmlSecret}))
}

// bs is a backslash, to spell out escapes in the tests.
const bs = `\`
