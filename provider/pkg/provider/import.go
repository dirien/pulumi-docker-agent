// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/mapper"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// ---------------------------------------------------------------------------------------
// `pulumi import` (AgentConfig, AgentArtifact, UserSettings, AgentsIgnore).
//
// The engine imports a resource by calling Read with the import ID and an empty state. It
// stores the inputs Read returns as the resource's inputs, generates the program code from
// them (`pulumi import --out`), and later diffs them against the checked inputs of that
// program. Read therefore returns the inputs exactly as Check would produce them, defaults
// included (withDefaults), and a state computed the same way Create computes it, so the
// first preview after an import shows no changes.
//
//   - AgentConfig (agentconfig_import.go): the ID is `file://<path>`, a path ending in
//     `.yaml`, `.yml` or `.hcl`, or `oci://<reference>`. The configuration is decoded into
//     the typed inputs (render.DecodeYAML). The resource keeps no link to the source: its ID
//     becomes the resource name, as for a created AgentConfig, and a refresh returns the
//     state unchanged (withAgentConfigRead).
//   - AgentArtifact (agentartifact_import.go): the ID is a tag, `registry/repository:tag`,
//     or a digest reference that a single tag of the repository points to.
//   - UserSettings and AgentsIgnore (usersettings_import.go, agentsignore_import.go): the
//     ID is the file (or, for AgentsIgnore, its directory). The file is adopted: nothing is
//     written or backed up, and the resource ID is the file's absolute path, which marks the
//     resource as adopted (see adoptedFile). Deleting an adopted resource leaves the file in
//     place instead of removing it, because Pulumi did not create it.
//   - AgentRun cannot be imported: a run has nothing to read back.
// ---------------------------------------------------------------------------------------

// inferProviderMarker is the key pulumi-go-provider's CheckConfig adds to every provider
// configuration it checked.
const inferProviderMarker = "__pulumi-go-provider-infer"

// withImportedProviderConfig handles the default provider that `pulumi import` creates. The
// engine treats it differently from the default provider of a deployment (pulumi/pulumi
// pkg/resource/deploy/import.go):
//
//   - It passes the stack's raw configuration to CheckConfig and Configure, where structured
//     values such as `docker-agent:registryAuth` are JSON strings. pulumi-go-provider cannot
//     decode them, so the import would fail. Both decode them first (typedConfig).
//   - It stores that raw configuration instead of the checked one, without the defaults
//     (`forceHttps: false`, ...), and without a version, while an SDK's default provider pins
//     one. pulumi-go-provider replaces a provider on any configuration change, so the preview
//     after an import would replace every imported, protected resource. The default provider
//     of the program that follows the import inherits those outputs. When the old inputs or
//     outputs were never checked (they lack pulumi-go-provider's marker), DiffConfig compares
//     only what was recorded, typed, and treats the rest as unchanged.
//
// DiffConfig does not call CheckConfig itself: pulumi-go-provider's CheckConfig decodes into
// the provider's own configuration and would change it.
func withImportedProviderConfig(prov p.Provider) p.Provider {
	checkConfig, configure, diffConfig := prov.CheckConfig, prov.Configure, prov.DiffConfig
	prov.CheckConfig = func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
		req.Inputs = typedConfig(req.Inputs)
		return checkConfig(ctx, req)
	}
	prov.Configure = func(ctx context.Context, req p.ConfigureRequest) error {
		req.Args = typedConfig(req.Args)
		return configure(ctx, req)
	}
	prov.DiffConfig = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		_, checkedInputs := req.OldInputs.GetOk(inferProviderMarker)
		_, checkedState := req.State.GetOk(inferProviderMarker)
		if checkedInputs && checkedState {
			return diffConfig(ctx, req)
		}
		recorded := typedConfig(req.OldInputs)
		old := property.Map{}
		for k, v := range req.Inputs.All {
			// pulumi-go-provider diffs the configuration without the engine's `version` and its
			// own `__` keys, but drops them only from the new configuration.
			if k != "version" && !strings.HasPrefix(k, "__") {
				old = old.Set(k, v)
			}
		}
		for k, v := range recorded.All {
			if k == "version" || strings.HasPrefix(k, "__") {
				continue
			}
			if nv, ok := req.Inputs.GetOk(k); ok && withoutSecrets(nv).Equals(withoutSecrets(v)) {
				continue
			}
			old = old.Set(k, v)
		}
		req.OldInputs, req.State = old, old
		// The version is diffed from the old state. Without a recorded version, the provider
		// served the import with the plugin the program uses.
		if v, ok := recorded.GetOk("version"); ok {
			req.State = req.State.Set("version", v)
		} else if v, ok := req.Inputs.GetOk("version"); ok {
			req.State = req.State.Set("version", v)
		}
		return diffConfig(ctx, req)
	}
	return prov
}

// structuredConfig lists the provider configuration keys whose values are not strings.
var structuredConfig = func() map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeFor[Config]()
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
		typ := f.Type
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if name != "" && typ.Kind() != reflect.String {
			out[name] = true
		}
	}
	return out
}()

// typedConfig decodes the JSON strings the engine passes for structured configuration values
// (`pulumi config set --path docker-agent:registryAuth[0].address ...`, `forceHttps: true`)
// when it creates a default provider for `pulumi import`. Secretness is kept.
func typedConfig(m property.Map) property.Map {
	for k, v := range m.All {
		if !structuredConfig[k] || !v.IsString() {
			continue
		}
		var x any
		if err := json.Unmarshal([]byte(v.AsString()), &x); err != nil {
			continue
		}
		typed := resource.FromResourcePropertyValue(resource.NewPropertyValue(x))
		m = m.Set(k, typed.WithSecret(v.Secret()))
	}
	return m
}

// withoutSecrets returns v with every secret marker removed, to compare values.
func withoutSecrets(v property.Value) property.Value {
	v = v.WithSecret(false)
	switch {
	case v.IsArray():
		items := make([]property.Value, 0, v.AsArray().Len())
		for _, e := range v.AsArray().All {
			items = append(items, withoutSecrets(e))
		}
		return property.New(items)
	case v.IsMap():
		entries := map[string]property.Value{}
		for k, e := range v.AsMap().All {
			entries[k] = withoutSecrets(e)
		}
		return property.New(entries)
	}
	return v
}

// fileScheme is the optional prefix of import IDs that name a local file.
const fileScheme = "file://"

// withDefaults returns args with the defaults that Check applies (Annotator.SetDefault).
// When args do not pass pulumi-go-provider's own checks (for example an enum value this
// provider does not list), they are returned unchanged and the engine reports the check
// failures after the import.
func withDefaults[I any](ctx context.Context, args I) (I, error) {
	tidyFree(reflect.ValueOf(&args).Elem())
	m, err := mapper.Unmap(args)
	if err != nil {
		return args, fmt.Errorf("encoding the imported inputs: %w", err)
	}
	inputs := resource.FromResourcePropertyValue(resource.NewObjectProperty(resource.NewPropertyMapFromMap(m))).AsMap()
	checked, failures, err := infer.DefaultCheck[I](ctx, inputs)
	if err != nil || len(failures) > 0 {
		return args, nil //nolint:nilerr // the engine runs Check again and reports the failures
	}
	return checked, nil
}

// tidyFree prepares decoded inputs for the Pulumi property model: free-form values (`any`)
// get string map keys (YAML allows others), and empty optional lists and maps become nil.
// Both forms render the same document, and nil keeps `{}` and `[]` out of the generated
// program code.
func tidyFree(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			tidyFree(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			fv := v.Field(i)
			_, opts, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
			if (fv.Kind() == reflect.Slice || fv.Kind() == reflect.Map) && fv.Len() == 0 &&
				strings.Contains(opts, "optional") && fv.CanSet() {
				fv.Set(reflect.Zero(fv.Type()))
				continue
			}
			tidyFree(fv)
		}
	case reflect.Interface:
		if !v.IsNil() && v.CanSet() {
			v.Set(reflect.ValueOf(freeValue(v.Elem().Interface())))
		}
	case reflect.Slice:
		for i := range v.Len() {
			tidyFree(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			// Map values are not addressable: tidy a copy and store it.
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			tidyFree(e)
			v.SetMapIndex(k, e)
		}
	}
}

// freeValue converts a free-form YAML value into the types the Pulumi property model
// supports: mappings with non-string keys get their keys formatted as strings.
func freeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = freeValue(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[fmt.Sprint(k)] = freeValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = freeValue(e)
		}
		return out
	}
	return v
}

// trimFileScheme removes an optional `file://` prefix from a local import ID.
func trimFileScheme(id string) string {
	if len(id) >= len(fileScheme) && strings.EqualFold(id[:len(fileScheme)], fileScheme) {
		return id[len(fileScheme):]
	}
	return id
}

// adoptedFile reports whether a local-file resource adopted the file at path through
// `pulumi import`: the import sets the resource ID to the file's absolute path, while a
// created resource has its name as ID. An adopted file existed before Pulumi managed it and
// has no backup, so deleting the resource, or moving it to another path, leaves the file in
// place instead of removing it.
func adoptedFile(id, path string) bool {
	return path != "" && id == path
}
