// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"os"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/localfile"
)

// The local-file resources (UserSettings, AgentsIgnore) render a document and, when a
// target path is set, keep a file on the machine running Pulumi in sync with it:
//
//   - Create and Update write the file atomically; previews never touch the file system.
//   - A file that already exists when the resource starts managing a path (on create, or
//     when the path changes) is never replaced silently. If its content differs from the
//     rendered document, Check (so already the preview) and Create/Update fail unless the
//     `overwrite` input is true. The existing file, whether it differs or not, is moved to
//     `<file>.pulumi-backup` (output `backupPath`) and moved back when the resource is
//     deleted or stops managing the path, so `pulumi destroy` returns the original file.
//     An existing backup is never replaced.
//   - Symbolic links are followed and kept (dotfile managers link these files into a
//     repository): the file a link points to is written, backed up, restored and removed;
//     the link itself is never replaced or deleted.
//   - Read (refresh) compares the file with the rendered document. Changed content is
//     reported as drift (the content output takes the file's content, so the next `up`
//     rewrites it); a missing file removes the resource from the state.
//   - Delete restores the backup, or removes the file when there is none. The
//     `retainOnDelete` resource option keeps both.
//   - Changing or removing the target path moves the file: the new path is claimed as on
//     create, then the old one is released as on delete.
//   - `pulumi import` adopts an existing file (usersettings_import.go, agentsignore_import.go):
//     Read decodes it into the inputs without writing or backing up anything, and the
//     resource ID becomes the file's absolute path (adoptedFile). Pulumi did not create an
//     adopted file, so releasing it (delete, or moving the resource to another path) leaves
//     it in place with its current content instead of removing it.

const (
	userSettingsType = tokens.Type(Name + ":index:UserSettings")
	agentsIgnoreType = tokens.Type(Name + ":index:AgentsIgnore")
)

// localFileTarget describes where and how a resource writes its file.
type localFileTarget struct {
	// path is the absolute path, or "" for a render-only resource.
	path    string
	mode    os.FileMode
	dirMode os.FileMode
	// what describes the file's existing content for conflict messages, for example
	// "your current docker-agent user configuration".
	what string
}

// sameFile reports whether two absolute paths name the same managed file, after
// following symbolic links (a link and its target are the same file).
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}
	ta, errA := localfile.Target(a)
	tb, errB := localfile.Target(b)
	return errA == nil && errB == nil && ta == tb
}

// claimProblem reports, without changing anything, why the resource cannot start managing
// t.path with content: an existing file with different content (unless overwrite), or an
// existing backup that a new backup would have to replace.
func (t localFileTarget) claimProblem(content string, overwrite bool) error {
	existing, exists, err := localfile.Read(t.path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", t.path, err)
	}
	if !exists {
		return nil
	}
	target, err := localfile.Target(t.path)
	if err != nil {
		return err
	}
	backup := target + localfile.BackupSuffix
	if string(existing) != content && !overwrite {
		return fmt.Errorf("%s already exists with different content. This resource manages the whole file, so "+
			"creating it would replace %s. Copy what you want to keep into the resource's inputs, or move the file "+
			"away, or set `overwrite: true`: the existing file is then moved to %s and moved back when the resource "+
			"is deleted", t.path, t.what, backup)
	}
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("%s already exists, so %s cannot be backed up before it is replaced. Move the old backup "+
			"away first", backup, t.path)
	}
	return nil
}

// install starts managing t.path: an existing file is moved to its backup (see
// claimProblem), then content is written. If the write fails, the backup is moved back.
// It returns the backup's path, or "" when no file existed.
func (t localFileTarget) install(content string, overwrite bool) (backup string, err error) {
	if err := t.claimProblem(content, overwrite); err != nil {
		return "", err
	}
	if _, exists, err := localfile.Read(t.path); err != nil {
		return "", err
	} else if exists {
		if backup, err = localfile.Backup(t.path); err != nil {
			return "", err
		}
	}
	if err := localfile.Write(t.path, []byte(content), t.mode, t.dirMode); err != nil {
		if backup != "" {
			_, _ = localfile.Restore(backup)
		}
		return "", err
	}
	return backup, nil
}

// release stops managing path: it moves the backup taken by install back, or removes the
// file when there is no backup. It returns a warning when the backup has disappeared.
func release(path, backup string) (warning string, err error) {
	if path == "" {
		return "", nil
	}
	if backup != "" {
		restored, err := localfile.Restore(backup)
		if err != nil {
			return "", err
		}
		if restored {
			return "", nil
		}
		warning = fmt.Sprintf("the backup %s of the file that existed before is gone, so %s is removed instead "+
			"of being restored", backup, path)
	}
	return warning, localfile.Remove(path)
}

// apply writes content to t and releases the file the previous state managed (oldPath,
// oldBackup) when the target changed; an adopted old file (oldAdopted) is left in place. It
// returns the backup for the new state.
func (t localFileTarget) apply(
	content string, overwrite bool, oldPath, oldBackup string, oldAdopted bool,
) (backup, warning string, err error) {
	if t.path != "" && sameFile(t.path, oldPath) {
		return oldBackup, "", localfile.Write(t.path, []byte(content), t.mode, t.dirMode)
	}
	if t.path != "" {
		if backup, err = t.install(content, overwrite); err != nil {
			return "", "", err
		}
	}
	if oldAdopted && oldBackup == "" {
		return backup, keptAdopted(oldPath), nil
	}
	if warning, err = release(oldPath, oldBackup); err != nil {
		return backup, warning, fmt.Errorf("releasing the previously managed file %s: %w", oldPath, err)
	}
	return backup, warning, nil
}

// releaseFile stops managing the file of the resource with the given ID on delete: like
// release, except that an adopted file without a backup is left in place.
func releaseFile(ctx context.Context, id, path, backup string) error {
	if adoptedFile(id, path) && backup == "" {
		p.GetLogger(ctx).Info(keptAdopted(path))
		return nil
	}
	warning, err := release(path, backup)
	logWarning(ctx, warning)
	return err
}

func keptAdopted(path string) string {
	return fmt.Sprintf("%s was adopted with `pulumi import`, so it is left in place instead of being removed", path)
}

// checkClaim runs claimProblem during Check when the resource is about to start managing
// a path: on create (no old inputs) or when the target changed. oldFile is the file the
// old inputs point to ("" when none). It returns "" when there is nothing to report.
func checkClaim(t localFileTarget, oldInputs property.Map, oldFile, content string, overwrite bool) string {
	if t.path == "" || (oldInputs.Len() > 0 && sameFile(oldFile, t.path)) {
		return ""
	}
	if err := t.claimProblem(content, overwrite); err != nil {
		return err.Error()
	}
	return ""
}

// oldInputString returns a string input from the old inputs, or nil.
func oldInputString(oldInputs property.Map, key string) *string {
	if v := oldInputs.Get(key); v.IsString() {
		s := v.AsString()
		return &s
	}
	return nil
}

func isTrue(b *bool) bool { return b != nil && *b }

func logWarning(ctx context.Context, warning string) {
	if warning != "" {
		p.GetLogger(ctx).Warning(warning)
	}
}

// refreshContent reads the managed file. gone is true when the file no longer exists.
// changed is true when its content differs from want. The content is returned only for
// the state; it is never part of an error.
func refreshContent(path, want string) (content string, changed, gone bool, err error) {
	data, exists, err := localfile.Read(path)
	if err != nil {
		return "", false, false, fmt.Errorf("refreshing %s: %w", path, err)
	}
	if !exists {
		return "", false, true, nil
	}
	return string(data), string(data) != want, false, nil
}

// resolveTarget resolves an optional user path.
func resolveTarget(path *string) (string, error) {
	if path == nil {
		return "", nil
	}
	return localfile.Resolve(*path)
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// withLocalFilePreview marks the rendered outputs of the local-file resources unknown
// during previews when the inputs they are rendered from are unknown. The resources
// compute their real outputs during previews (they are pure renders), so a preview shows
// the exact document `up` will write.
func withLocalFilePreview(prov p.Provider) p.Provider {
	create, update := prov.Create, prov.Update
	prov.Create = func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
		resp, err := create(ctx, req)
		if err == nil && req.DryRun {
			resp.Properties = localFilePreviewOutputs(req.Urn.Type(), req.Properties, resp.Properties)
		}
		return resp, err
	}
	prov.Update = func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
		resp, err := update(ctx, req)
		if err == nil && req.DryRun {
			resp.Properties = localFilePreviewOutputs(req.Urn.Type(), req.Inputs, resp.Properties)
		}
		return resp, err
	}
	return prov
}

func localFilePreviewOutputs(typ tokens.Type, inputs, out property.Map) property.Map {
	var contentKey, targetKey string
	switch typ {
	case userSettingsType:
		contentKey, targetKey = "yaml", "path"
	case agentsIgnoreType:
		contentKey, targetKey = "content", "directory"
	default:
		return out
	}
	contentUnknown := false
	for k, v := range inputs.All {
		if k != targetKey && v.HasComputed() {
			contentUnknown = true
		}
	}
	if contentUnknown {
		out = markUnknown(out, contentKey, "sha256")
	}
	if inputs.Get(targetKey).HasComputed() {
		out = markUnknown(out, "filePath")
	}
	return out
}
