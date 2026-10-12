// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"regexp"
	"strings"
)

// unreadKeys are keys that example configurations shipped with docker-agent set although
// docker-agent never reads them, by path pattern (`*` stands for any list index or map key),
// with the reason. No typed input covers them: DecodeYAML keeps them in `extra` like any
// other unknown key, and the generated example programs leave them out.
var unreadKeys = map[string]string{
	// A RAG strategy keeps the keys it does not know as parameters, and none of the
	// strategies reads this one.
	`rag[*].strategies[*].extra["batch_size"]`: "docker-agent does not read the RAG strategy parameter `batch_size` " +
		"(the number of chunks per embedding request is `embedding_batch_size`)",
}

var unreadPatterns = func() map[*regexp.Regexp]string {
	out := map[*regexp.Regexp]string{}
	for pattern, reason := range unreadKeys {
		re := regexp.QuoteMeta(pattern)
		re = strings.ReplaceAll(re, `\[\*\]`, `\[(?:\d+|"(?:[^"\\]|\\.)*")\]`)
		out[regexp.MustCompile("^"+re+"$")] = reason
	}
	return out
}()

// UnreadKey reports whether the untyped key at p is a key that docker-agent's own examples
// set although docker-agent never reads it, and why.
func UnreadKey(p Path) (reason string, ok bool) {
	for re, reason := range unreadPatterns {
		if re.MatchString(string(p)) {
			return reason, true
		}
	}
	return "", false
}

// UnreadKeyNames returns the YAML keys of the unread keys, for checks against the
// docker-agent sources.
func UnreadKeyNames() []string {
	var out []string
	key := regexp.MustCompile(`\["([^"]+)"\]$`)
	for pattern := range unreadKeys {
		if m := key.FindStringSubmatch(pattern); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}
