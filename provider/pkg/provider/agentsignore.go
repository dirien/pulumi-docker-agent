// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"path/filepath"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentsignore"
)

// AgentsIgnore manages a docker-agent `.agentsignore` file.
type AgentsIgnore struct{}

const agentsIgnoreDoc = `A Docker Agent [` + "`.agentsignore`" + `](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/)
file: paths the agent must not see.

The filesystem toolset hides matching paths from listings and searches and refuses to read, write or edit them.
docker-agent uses the nearest ` + "`.agentsignore`" + ` at or above the working directory; patterns use
` + "`.gitignore`" + ` syntax and are anchored to the directory that contains the file. ` + "`.agentsignore`" + ` governs
the filesystem toolset only. It is not a sandbox: an agent with the ` + "`shell`" + ` toolset can still read the files,
so combine it with permissions or sandbox mode for a real boundary.

The patterns are validated with go-git's gitignore implementation, the library docker-agent uses: patterns that
docker-agent would read differently (leading or trailing whitespace, a leading ` + "`#`" + `, line breaks) or that can
never match (invalid globs, ` + "`**`" + ` mixed into a path segment) fail during preview, and negations that re-include
nothing are reported as warnings.

Without ` + "`directory`" + `, the resource only renders ` + "`content`" + `, for example to add the file to a container
image. With ` + "`directory`" + `, the provider writes ` + "`<directory>/.agentsignore`" + ` on create and update
(atomically, mode 0644, creating missing directories) and reports changes made outside of Pulumi as drift on
` + "`pulumi refresh`" + `. The directory is resolved on the machine that runs Pulumi; relative paths are relative to
the Pulumi project directory. A symbolic link at ` + "`<directory>/.agentsignore`" + ` is kept: the file it points to is
written.

If the project already has a ` + "`.agentsignore`" + ` with different content, the preview fails instead of replacing it.
Copy its patterns into ` + "`patterns`" + `, or set ` + "`overwrite: true`" + `. Either way the existing file is moved to
` + "`.agentsignore.pulumi-backup`" + ` (output ` + "`backupPath`" + `), and deleting the resource moves it back. Deleting the
resource removes a file that the provider created, and leaves a file adopted with ` + "`pulumi import`" + ` in place.
Set the ` + "`retainOnDelete`" + ` resource option to keep the file.`

var agentsIgnoreImportDoc = `An existing ` + "`.agentsignore`" + ` can be imported with its project directory (or the
file's path) as ID: the comment block at the top of the file becomes ` + "`comment`" + ` and every other line a
pattern (comments between patterns are not imported). The file is adopted as for ` + "`UserSettings`" + `: the import
takes no backup, ` + "`overwrite`" + ` is not needed, and deleting the resource leaves the file in place. The resource
ID is the file's absolute path.` +
	importCommand("docker-agent:index:AgentsIgnore", "project", "./my-service")

// Annotate describes the resource.
func (r *AgentsIgnore) Annotate(a infer.Annotator) {
	a.Describe(&r, describe(agentsIgnoreDoc, agentsIgnoreExamples, agentsIgnoreImportDoc))
}

// AgentsIgnoreArgs are the inputs of AgentsIgnore.
type AgentsIgnoreArgs struct {
	Patterns  []string `pulumi:"patterns"`
	Comment   *string  `pulumi:"comment,optional"`
	Directory *string  `pulumi:"directory,optional"`
	Overwrite *bool    `pulumi:"overwrite,optional"`
}

// Annotate describes the inputs.
func (a *AgentsIgnoreArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Patterns, "The patterns, in order, one per line, in `.gitignore` syntax: `secrets.env` "+
		"matches that name at any depth, `/secrets.env` only next to the file, `build/` a directory and "+
		"everything below it, `docs/**/*.draft.md` nested paths, and `!public.key` re-includes a path an earlier "+
		"pattern excluded. Comments belong in `comment`.")
	an.Describe(&a.Comment, "A comment written at the top of the file. Each line is prefixed with `# `.")
	an.Describe(&a.Directory, "The project directory to write `.agentsignore` into. Resolved on the machine that "+
		"runs Pulumi: `~` is that user's home directory and relative paths are relative to the Pulumi project "+
		"directory. When unset, nothing is written and the resource only renders `content`.")
	an.Describe(&a.Overwrite, "Replace an existing `.agentsignore` with different content when the resource is "+
		"created or `directory` changes. Without it, the preview fails instead. An existing file is always moved "+
		"to `.agentsignore.pulumi-backup` first and moved back when the resource is deleted.")
	an.SetDefault(&a.Overwrite, false)
}

// AgentsIgnoreState is the state of AgentsIgnore.
type AgentsIgnoreState struct {
	AgentsIgnoreArgs
	Content    string  `pulumi:"content"`
	SHA256     string  `pulumi:"sha256"`
	FilePath   *string `pulumi:"filePath,optional"`
	BackupPath *string `pulumi:"backupPath,optional"`
}

// Annotate describes the outputs.
func (s *AgentsIgnoreState) Annotate(an infer.Annotator) {
	an.Describe(&s.Content, "The rendered `.agentsignore` file. After a refresh it holds the file's current "+
		"content, so drift shows up in the next diff.")
	an.Describe(&s.SHA256, "The hex-encoded SHA-256 of `content`.")
	an.Describe(&s.FilePath, "The absolute path of the written file. Unset when `directory` is unset.")
	an.Describe(&s.BackupPath, "Where the `.agentsignore` that already existed was moved when the resource took it "+
		"over. Deleting the resource moves it back. Unset when there was no file, and for an imported resource.")
}

// Check validates the patterns.
func (*AgentsIgnore) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AgentsIgnoreArgs], error) {
	args, failures, err := infer.DefaultCheck[AgentsIgnoreArgs](ctx, req.NewInputs)
	if err != nil || len(failures) > 0 {
		return infer.CheckResponse[AgentsIgnoreArgs]{Inputs: args, Failures: failures}, err
	}
	if v := req.NewInputs.Get("directory"); args.Directory != nil && !v.HasComputed() {
		if _, err := resolveTarget(args.Directory); err != nil {
			failures = append(failures, p.CheckFailure{Property: "directory", Reason: err.Error()})
		}
	}
	if req.NewInputs.Get("patterns").HasComputed() {
		return infer.CheckResponse[AgentsIgnoreArgs]{Inputs: args, Failures: failures}, nil
	}
	errs, warnings := agentsignore.Validate(args.Patterns)
	for _, e := range errs {
		failures = append(failures, p.CheckFailure{Property: "patterns", Reason: e.String()})
	}
	for _, w := range warnings {
		p.GetLogger(ctx).Warning(".agentsignore " + w.String())
	}
	if len(failures) == 0 && !req.NewInputs.HasComputed() {
		// Surface a conflict with an existing file already in the preview.
		target, err := agentsIgnoreTarget(args)
		old, oldErr := agentsIgnoreTarget(AgentsIgnoreArgs{Directory: oldInputString(req.OldInputs, "directory")})
		if err == nil && oldErr == nil {
			content := agentsignore.Render(derefStr(args.Comment), args.Patterns)
			if reason := checkClaim(target, req.OldInputs, old.path, content, isTrue(args.Overwrite)); reason != "" {
				failures = append(failures, p.CheckFailure{Property: "directory", Reason: reason})
			}
		}
	}
	return infer.CheckResponse[AgentsIgnoreArgs]{Inputs: args, Failures: failures}, nil
}

func agentsIgnoreTarget(args AgentsIgnoreArgs) (localFileTarget, error) {
	dir, err := resolveTarget(args.Directory)
	if err != nil || dir == "" {
		return localFileTarget{}, err
	}
	return localFileTarget{
		path: filepath.Join(dir, agentsignore.FileName), mode: 0o644, dirMode: 0o755,
		what: "the existing patterns",
	}, nil
}

func agentsIgnoreState(args AgentsIgnoreArgs) (AgentsIgnoreState, localFileTarget, error) {
	state := AgentsIgnoreState{AgentsIgnoreArgs: args}
	if state.Patterns == nil {
		state.Patterns = []string{}
	}
	target, err := agentsIgnoreTarget(args)
	state.FilePath = strPtrOrNil(target.path)
	state.Content = agentsignore.Render(derefStr(args.Comment), args.Patterns)
	state.SHA256 = agentsignore.SHA256(state.Content)
	return state, target, err
}

// Create renders the file and writes it when `directory` is set.
func (*AgentsIgnore) Create(
	ctx context.Context, req infer.CreateRequest[AgentsIgnoreArgs],
) (infer.CreateResponse[AgentsIgnoreState], error) {
	state, target, err := agentsIgnoreState(req.Inputs)
	if req.DryRun {
		return infer.CreateResponse[AgentsIgnoreState]{ID: req.Name, Output: state}, nil
	}
	if err != nil {
		return infer.CreateResponse[AgentsIgnoreState]{}, err
	}
	backup, warning, err := target.apply(state.Content, isTrue(req.Inputs.Overwrite), "", "", false)
	if err != nil {
		return infer.CreateResponse[AgentsIgnoreState]{}, err
	}
	logWarning(ctx, warning)
	state.BackupPath = strPtrOrNil(backup)
	return infer.CreateResponse[AgentsIgnoreState]{ID: req.Name, Output: state}, nil
}

// Update re-renders the file and rewrites (or moves) it.
func (*AgentsIgnore) Update(
	ctx context.Context, req infer.UpdateRequest[AgentsIgnoreArgs, AgentsIgnoreState],
) (infer.UpdateResponse[AgentsIgnoreState], error) {
	state, target, err := agentsIgnoreState(req.Inputs)
	if req.DryRun {
		state.BackupPath = req.State.BackupPath
		return infer.UpdateResponse[AgentsIgnoreState]{Output: state}, nil
	}
	if err != nil {
		return infer.UpdateResponse[AgentsIgnoreState]{}, err
	}
	oldPath := derefStr(req.State.FilePath)
	backup, warning, err := target.apply(state.Content, isTrue(req.Inputs.Overwrite),
		oldPath, derefStr(req.State.BackupPath), adoptedFile(req.ID, oldPath))
	if err != nil {
		return infer.UpdateResponse[AgentsIgnoreState]{}, err
	}
	logWarning(ctx, warning)
	state.BackupPath = strPtrOrNil(backup)
	return infer.UpdateResponse[AgentsIgnoreState]{Output: state}, nil
}

// Read detects drift between the state and the file. Without a state, it imports (adopts)
// the existing file named by the ID.
func (*AgentsIgnore) Read(
	ctx context.Context, req infer.ReadRequest[AgentsIgnoreArgs, AgentsIgnoreState],
) (infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState], error) {
	state := req.State
	if state.SHA256 == "" {
		return importAgentsIgnore(ctx, req.ID)
	}
	if path := derefStr(state.FilePath); path != "" {
		content, changed, gone, err := refreshContent(path, state.Content)
		if err != nil {
			return infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState]{}, err
		}
		if gone || changed {
			// A deleted file reads as empty content, so the next `up` writes it again and the
			// backup (if any) stays tracked.
			state.Content = content
			state.SHA256 = agentsignore.SHA256(content)
		}
	}
	return infer.ReadResponse[AgentsIgnoreArgs, AgentsIgnoreState]{ID: req.ID, Inputs: req.Inputs, State: state}, nil
}

// Delete moves the backup of the file that existed before back, or removes the managed
// file. An adopted file (imported with `pulumi import`) is left in place.
func (*AgentsIgnore) Delete(ctx context.Context, req infer.DeleteRequest[AgentsIgnoreState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, releaseFile(ctx, req.ID, derefStr(req.State.FilePath), derefStr(req.State.BackupPath))
}

// Diff compares the inputs and detects drift found by a refresh.
func (*AgentsIgnore) Diff(
	_ context.Context, req infer.DiffRequest[AgentsIgnoreArgs, AgentsIgnoreState],
) (infer.DiffResponse, error) {
	olds, news := req.State.AgentsIgnoreArgs, req.Inputs
	diff := map[string]p.PropertyDiff{}
	if !equalValues(olds.Patterns, news.Patterns) {
		diff["patterns"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if derefStr(olds.Comment) != derefStr(news.Comment) {
		diff["comment"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if !equalValues(olds.Directory, news.Directory) {
		diff["directory"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if isTrue(olds.Overwrite) != isTrue(news.Overwrite) {
		diff["overwrite"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if agentsignore.Render(derefStr(news.Comment), news.Patterns) != req.State.Content {
		diff["content"] = p.PropertyDiff{Kind: p.Update}
	}
	if target, err := agentsIgnoreTarget(news); err == nil && target.path != derefStr(req.State.FilePath) {
		diff["filePath"] = p.PropertyDiff{Kind: p.Update}
	}
	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// WireDependencies declares how secretness flows. The outputs are always known because
// Create and Update compute them during previews; withLocalFilePreview marks them
// unknown when an input is unknown.
func (*AgentsIgnore) WireDependencies(f infer.FieldSelector, args *AgentsIgnoreArgs, state *AgentsIgnoreState) {
	content := f.InputField(&args.Patterns).Secret()
	comment := f.InputField(&args.Comment).Secret()
	for _, out := range []any{&state.Content, &state.SHA256} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).DependsOn(content, comment)
	}
	f.OutputField(&state.FilePath).AlwaysKnown()
	f.OutputField(&state.FilePath).DependsOn(f.InputField(&args.Directory).Secret())
}
