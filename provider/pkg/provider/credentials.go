// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/environment"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// An AgentConfig is rendered into `yaml`, which ends up in the state, in files and in
// pushed artifacts. Credentials belong in `${env.NAME}` placeholders that docker-agent
// resolves at run time. Check warns about `headers` and `env` values that look like literal
// credentials instead: a value of a credential-named key (Authorization, X-Api-Key,
// GITHUB_TOKEN, ...) or a value with a well-known token shape. Placeholders, 1Password
// references and Pulumi secrets are not reported, and the value itself never appears in the
// warning.

var (
	// credentialKey matches header and environment variable names that hold credentials.
	credentialKey = regexp.MustCompile(`(?i)(authorization|api[-_]?key|token|secret|password|passwd|passphrase|` +
		`credential|access[-_]?key|private[-_]?key|cookie)`)
	// credentialValue matches values shaped like credentials whatever their key: an HTTP
	// authorization scheme or a well-known token prefix.
	credentialValue = regexp.MustCompile(`^(?i:bearer|basic|token)\s+\S{8,}|^(sk-|sk_live_|rk_live_|ghp_|gho_|ghs_|` +
		`ghu_|github_pat_|glpat-|xox[abpr]-|AKIA|ASIA|AIza|hf_|dckr_pat_)\S{8,}`)
)

// minCredential is the shortest value of a credential-named key that is reported: shorter
// values are flags ("1", "true") or modes, not credentials.
const minCredential = 8

// credentialMaps are the input names of the string maps that are checked.
var credentialMaps = []string{"headers", "env"}

// warnLiteralCredentials warns about every `headers` and `env` value of doc that looks like
// a literal credential. inputs are the raw inputs doc was decoded from: secret and unknown
// values are skipped.
func warnLiteralCredentials(ctx context.Context, doc render.Document, inputs property.Map) {
	var found []render.Path
	findLiteralCredentials(reflect.ValueOf(doc), property.New(inputs), "", &found)
	for _, path := range found {
		userLog(ctx).Warning(fmt.Sprintf("%s looks like a literal credential: it is written in plaintext to `yaml`, "+
			"and to every file and AgentArtifact made from it. Use a `${env.NAME}` placeholder that docker-agent "+
			"resolves at run time (for example from AgentRun `env`), or at least a Pulumi secret, which keeps it "+
			"encrypted in the state but not out of the YAML.", path))
	}
}

func findLiteralCredentials(v reflect.Value, pv property.Value, path render.Path, found *[]render.Path) {
	if pv.Secret() || pv.IsComputed() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			findLiteralCredentials(v.Elem(), pv, path, found)
		}
	case reflect.Struct:
		if !pv.IsMap() {
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
			if name == "" || !f.IsExported() {
				continue
			}
			fv, fpv := v.Field(i), pv.AsMap().Get(name)
			if slices.Contains(credentialMaps, name) && fv.Type() == reflect.TypeFor[map[string]string]() {
				for _, k := range slices.Sorted(maps.Keys(fv.Interface().(map[string]string))) {
					if e := fpv.AsMap().Get(k); fpv.IsMap() && !fpv.Secret() && e.IsString() && !e.Secret() &&
						literalCredential(k, e.AsString()) {
						*found = append(*found, path.Field(name).Key(k))
					}
				}
				continue
			}
			findLiteralCredentials(fv, fpv, path.Field(name), found)
		}
	case reflect.Slice:
		if pv.IsArray() {
			for i := range min(v.Len(), pv.AsArray().Len()) {
				findLiteralCredentials(v.Index(i), pv.AsArray().Get(i), path.Index(i), found)
			}
		}
	case reflect.Map:
		if pv.IsMap() && v.Type().Key().Kind() == reflect.String {
			keys := make([]string, 0, v.Len())
			for _, k := range v.MapKeys() {
				keys = append(keys, k.String())
			}
			slices.Sort(keys)
			for _, k := range keys {
				findLiteralCredentials(v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key())), pv.AsMap().Get(k),
					path.Key(k), found)
			}
		}
	}
}

// literalCredential reports whether value, set for key, looks like a literal credential.
func literalCredential(key, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(environment.Refs(value)) > 0 || strings.HasPrefix(value, "op://") {
		return false
	}
	return (credentialKey.MatchString(key) && len(value) >= minCredential) || credentialValue.MatchString(value)
}
