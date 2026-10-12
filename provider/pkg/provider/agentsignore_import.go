// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentsignore"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/localfile"
)

// importAgentsIgnore adopts an existing `.agentsignore` file (`pulumi import`, the ID is the
// project directory or the file itself): the comment block at the top becomes `comment` and
// the other lines `patterns`. Nothing is written. The resource ID becomes the file's absolute
// path, which marks the file as adopted (see adoptedFile).
func importAgentsIgnore(
	ctx context.Context, id string,
) (infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState], error) {
	fail := func(err error) (infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState], error) {
		return infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState]{}, fmt.Errorf("importing %s: %w", id, err)
	}
	dir := portablePath(trimFileScheme(strings.TrimSpace(id)))
	resolved, err := localfile.Resolve(dir)
	if err != nil {
		return fail(err)
	}
	if filepath.Base(resolved) == agentsignore.FileName {
		dir = filepath.Dir(dir)
	} else if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
		return fail(fmt.Errorf("%s is neither a directory nor a %s file", resolved, agentsignore.FileName))
	}
	data, file, err := readImportedFile(filepath.Join(dir, agentsignore.FileName))
	if err != nil {
		return fail(err)
	}

	parsed := agentsignore.Parse(string(data))
	if errs, _ := agentsignore.Validate(parsed.Patterns); len(errs) > 0 {
		problems := make([]string, 0, len(errs))
		for _, e := range errs {
			problems = append(problems, e.String())
		}
		return fail(fmt.Errorf("%s has patterns docker-agent reads differently or never matches: %s. Fix them "+
			"in the file and import it again", file, strings.Join(problems, "; ")))
	}
	if parsed.DroppedComments > 0 {
		p.GetLogger(ctx).Warningf("%s has %d comment line(s) between patterns. `comment` only holds the comment "+
			"at the top of the file, so they are not imported", file, parsed.DroppedComments)
	}
	args := AgentsIgnoreArgs{Patterns: parsed.Patterns, Comment: strPtrOrNil(parsed.Comment), Directory: &dir}
	if args, err = withDefaults(ctx, args); err != nil {
		return fail(err)
	}

	state, target, err := agentsIgnoreState(args)
	if err != nil {
		return fail(err)
	}
	if state.Content != string(data) {
		warnReformat(ctx, file)
	}
	return infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState]{ID: target.path, Inputs: args, State: state}, nil
}
