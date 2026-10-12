// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker-agent/pkg/config/latest"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/localfile"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/usersettings"
)

// importUserSettings adopts an existing docker-agent user configuration file (`pulumi
// import`, the ID is the file's path): docker-agent's own types parse it, and the result is
// decoded into the typed inputs. Nothing is written. The resource ID becomes the file's
// absolute path, which marks the file as adopted (see adoptedFile).
func importUserSettings(
	ctx context.Context, id string,
) (infer.ReadResponse[UserSettingsArgs, UserSettingsState], error) {
	fail := func(err error) (infer.ReadResponse[UserSettingsArgs, UserSettingsState], error) {
		return infer.ReadResponse[UserSettingsArgs, UserSettingsState]{}, fmt.Errorf("importing %s: %w", id, err)
	}
	path := portablePath(trimFileScheme(strings.TrimSpace(id)))
	data, file, err := readImportedFile(path)
	if err != nil {
		return fail(err)
	}
	doc, canonical, err := usersettings.Canonical(data)
	if err != nil {
		return fail(fmt.Errorf("parsing %s: %w", file, err))
	}
	var args UserSettingsArgs
	if err := usersettings.Decode(doc, &args); err != nil {
		return fail(fmt.Errorf("%s cannot be expressed with the typed inputs: %w", file, err))
	}
	if rendered, err := renderUserSettings(args); err != nil || rendered != canonical {
		return fail(fmt.Errorf("%s cannot be expressed with the typed inputs (please report this): the inputs "+
			"render\n%s\ninstead of\n%s", file, rendered, canonical))
	}
	args.Path = &path
	if args, err = withDefaults(ctx, args); err != nil {
		return fail(err)
	}
	if _, errs, _ := checkUserSettings(args, true); len(errs) > 0 {
		problems := strings.Join(errs, "; ")
		if !skipValidation(ctx, nil) {
			return fail(fmt.Errorf("invalid docker-agent user settings: %s. To import them anyway, set the "+
				"provider's `skipValidation` (DOCKER_AGENT_SKIP_VALIDATION=true): the imported resource then gets "+
				"`skipValidation: true`", problems))
		}
		p.GetLogger(ctx).Warningf("%s has settings docker-agent %s rejects, so the imported resource sets "+
			"`skipValidation: true`: %s", file, DockerAgentVersion, problems)
		args.SkipValidation = ptr(true)
	}

	state, target, err := userSettingsState(args)
	if err != nil {
		return fail(err)
	}
	if state.YAML != string(data) {
		warnReformat(ctx, file)
	}
	return infer.ReadResponse[UserSettingsArgs, UserSettingsState]{ID: target.path, Inputs: args, State: state}, nil
}

// readImportedFile reads the file at path (as the user wrote it) for an import. It returns
// the content and the absolute path.
func readImportedFile(path string) ([]byte, string, error) {
	file, err := localfile.Resolve(path)
	if err != nil {
		return nil, "", err
	}
	data, exists, err := localfile.Read(file)
	switch {
	case err != nil:
		return nil, "", fmt.Errorf("reading %s: %w", file, err)
	case !exists:
		return nil, "", fmt.Errorf("%s does not exist", file)
	case len(data) > localfile.MaxReadSize:
		return nil, "", fmt.Errorf("%s is larger than %d bytes", file, localfile.MaxReadSize)
	}
	return data, file, nil
}

// warnReformat explains that an adopted file differs from what the provider writes.
func warnReformat(ctx context.Context, file string) {
	p.GetLogger(ctx).Warningf("%s is not written the way the provider writes it (comments, blank lines, key "+
		"order, quoting or defaults). The import leaves the file as it is and no update is needed, but `pulumi "+
		"refresh` reports the difference as drift and the next `pulumi up` then rewrites the file in the "+
		"provider's format, with the same meaning", file)
}

// portablePath writes an absolute path inside the home directory as `~/...`, so the
// generated program works for other users too. Other paths are returned as they are.
func portablePath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || !filepath.IsLocal(rel) {
		return path
	}
	return "~/" + filepath.ToSlash(rel)
}

// SetYAMLValue decodes the scalar forms docker-agent writes for a thinking budget: an effort
// level or a number of tokens.
func (b *UserThinkingBudget) SetYAMLValue(v any) (bool, error) {
	if s, ok := v.(string); ok {
		*b = UserThinkingBudget{Effort: &s}
		return true, nil
	}
	if n, ok := usersettings.IntValue(v); ok {
		*b = UserThinkingBudget{Tokens: ptr(int(n))}
		return true, nil
	}
	return false, fmt.Errorf("expected an effort level or a number of tokens, got %v", v)
}

// SetYAMLValue decodes the integer shorthand docker-agent writes for a token budget.
func (b *UserTaskBudget) SetYAMLValue(v any) (bool, error) {
	if n, ok := usersettings.IntValue(v); ok {
		*b = UserTaskBudget{Total: int(n)}
		return true, nil
	}
	return false, nil
}

// SetYAMLValue decodes the `provider/model` shorthand docker-agent writes for a model that
// only names its provider and model.
func (m *UserModel) SetYAMLValue(v any) (bool, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	ref, err := latest.ParseModelRef(s)
	if err != nil {
		return false, err
	}
	*m = UserModel{Provider: &ref.Provider, Model: &ref.Model}
	return true, nil
}
