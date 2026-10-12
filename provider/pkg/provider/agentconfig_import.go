// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config/hcl"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/localfile"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// ociScheme is the prefix of AgentConfig import IDs that name an OCI artifact.
const ociScheme = "oci://"

// errAgentConfigImportID explains the accepted AgentConfig import IDs.
var errAgentConfigImportID = errors.New("the import ID must be `file://<path>`, a path ending in `.yaml`, " +
	"`.yml` or `.hcl`, or `oci://<registry>/<repository>:<tag>` (or `@sha256:<digest>`)")

// Read implements `pulumi import` for AgentConfig: it reads the docker-agent configuration
// the import ID names and decodes it into the typed inputs. AgentConfig has nothing outside
// of the Pulumi state, so a refresh returns the state unchanged (withAgentConfigRead keeps the
// refresh off this path entirely).
func (*AgentConfig) Read(
	ctx context.Context, req infer.ReadRequest[AgentConfigArgs, AgentConfigState],
) (infer.ReadResponse[AgentConfigArgs, AgentConfigState], error) {
	if !isAgentConfigImport(req.State) {
		return infer.ReadResponse[AgentConfigArgs, AgentConfigState](req), nil
	}
	args, err := importAgentConfig(ctx, req.ID)
	if err != nil {
		return infer.ReadResponse[AgentConfigArgs, AgentConfigState]{}, fmt.Errorf("importing %s: %w", req.ID, err)
	}
	// The state Create computes for these inputs.
	state, err := buildAgentConfigState(ctx, "agent", args)
	if err != nil {
		return infer.ReadResponse[AgentConfigArgs, AgentConfigState]{}, fmt.Errorf("importing %s: %w", req.ID, err)
	}
	return infer.ReadResponse[AgentConfigArgs, AgentConfigState]{ID: req.ID, Inputs: args, State: state}, nil
}

// isAgentConfigImport tells an import (the engine sends no state) from a refresh: every
// created AgentConfig has a rendered YAML and at least one agent.
func isAgentConfigImport(state AgentConfigState) bool {
	return state.YAML == "" && len(state.Agents) == 0
}

// importAgentConfig reads and decodes the configuration named by id into checked inputs.
//
// Only configurations that docker-agent loads are imported: the original is validated as it
// is (a local configuration with `instruction_file` references only after they are inlined,
// because docker-agent resolves them only when it loads a file), and the imported inputs must
// pass the same checks as `pulumi preview`. With the provider's skipValidation, an invalid
// configuration is imported anyway and the resource gets `skipValidation: true`.
func importAgentConfig(ctx context.Context, id string) (AgentConfigArgs, error) {
	src, err := readAgentConfigSource(ctx, id)
	if err != nil {
		return AgentConfigArgs{}, err
	}
	dec, err := render.DecodeYAML(src.data)
	if err != nil {
		return AgentConfigArgs{}, fmt.Errorf("decoding %s: %w", src.file, err)
	}
	if len(dec.Untyped) > 0 {
		keys := make([]string, 0, len(dec.Untyped))
		for _, u := range dec.Untyped {
			keys = append(keys, string(u))
		}
		p.GetLogger(ctx).Infof("%s: keys without a typed input are kept in `extra`: %s", src.file, strings.Join(keys, ", "))
	}
	doc := dec.Document

	var problem error
	if src.dir == "" || !slices.ContainsFunc(doc.Agents, func(a render.Agent) bool { return len(a.InstructionFile) > 0 }) {
		problem = validate.Validate(ctx, src.file, src.data).Error()
	}
	if src.dir != "" {
		if err := inlineInstructionFiles(ctx, &doc, src.dir); err != nil {
			return AgentConfigArgs{}, err
		}
	}
	if _, err := render.Render(doc); err != nil {
		return AgentConfigArgs{}, fmt.Errorf("%s: %w", src.file, err)
	}
	args, err := withDefaults(ctx, AgentConfigArgs{Document: doc})
	if err != nil {
		return AgentConfigArgs{}, err
	}
	if problem == nil {
		rendered, err := render.Render(args.document())
		if err != nil {
			return AgentConfigArgs{}, fmt.Errorf("%s: %w", src.file, err)
		}
		problem = validate.Validate(ctx, src.file, []byte(rendered)).Error()
	}
	if problem != nil {
		if !skipValidation(ctx, nil) {
			return AgentConfigArgs{}, fmt.Errorf("invalid docker-agent configuration: %w. To import it anyway, set the "+
				"provider's `skipValidation` (DOCKER_AGENT_SKIP_VALIDATION=true): the imported resource then gets "+
				"`skipValidation: true`", problem)
		}
		p.GetLogger(ctx).Warningf("%s is not a valid docker-agent %s configuration, so the imported resource sets "+
			"`skipValidation: true`: %v", src.file, DockerAgentVersion, problem)
		args.SkipValidation = ptr(true)
	}
	return args, nil
}

// agentConfigSource is a configuration read for an import.
type agentConfigSource struct {
	file string // the file or reference, for messages
	dir  string // the directory of a local file, "" for an OCI artifact
	data []byte // the YAML (HCL is converted)
}

// readAgentConfigSource reads the configuration named by an import ID: a local YAML or HCL
// file (relative paths are relative to the Pulumi project directory, like every other local
// path of the provider) or an OCI artifact. HCL is converted to YAML with docker-agent's own
// converter.
func readAgentConfigSource(ctx context.Context, id string) (agentConfigSource, error) {
	id = strings.TrimSpace(id)
	if len(id) >= len(ociScheme) && strings.EqualFold(id[:len(ociScheme)], ociScheme) {
		return readOCIAgentConfig(ctx, id[len(ociScheme):])
	}
	path := trimFileScheme(id)
	switch {
	case path == "":
		return agentConfigSource{}, errAgentConfigImportID
	case path == id && !isConfigFileName(path):
		return agentConfigSource{}, fmt.Errorf("%q is not a configuration file: %w", id, errAgentConfigImportID)
	}
	file, err := localfile.Resolve(path)
	if err != nil {
		return agentConfigSource{}, err
	}
	data, exists, err := localfile.Read(file)
	switch {
	case err != nil:
		return agentConfigSource{}, fmt.Errorf("reading %s: %w", file, err)
	case !exists:
		return agentConfigSource{}, fmt.Errorf("%s does not exist", file)
	case len(data) > localfile.MaxReadSize:
		return agentConfigSource{}, fmt.Errorf("%s is larger than %d bytes", file, localfile.MaxReadSize)
	}
	if hcl.IsHCLSource(file, data) {
		if data, err = hcl.ToYAML(data, file); err != nil {
			return agentConfigSource{}, err
		}
	}
	return agentConfigSource{file: file, dir: filepath.Dir(file), data: data}, nil
}

func isConfigFileName(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".hcl":
		return true
	}
	return false
}

// readOCIAgentConfig pulls an agent artifact with the provider's registry settings, like
// getAgent.
func readOCIAgentConfig(ctx context.Context, ref string) (agentConfigSource, error) {
	if ref == "" {
		return agentConfigSource{}, errAgentConfigImportID
	}
	pulled, err := ociagent.Pull(ctx, ref, registryOptions(ctx, nil))
	if err != nil {
		return agentConfigSource{}, err
	}
	data := pulled.YAML
	if hcl.LooksLikeHCL(data) {
		// Convert in an empty directory: file() calls of a pulled artifact must not read
		// files of the machine running Pulumi.
		dir, err := os.MkdirTemp("", "docker-agent-import-")
		if err != nil {
			return agentConfigSource{}, err
		}
		defer os.RemoveAll(dir) //nolint:errcheck
		if data, err = hcl.ToYAML(data, filepath.Join(dir, "agent.hcl")); err != nil {
			return agentConfigSource{}, err
		}
	}
	return agentConfigSource{file: ref, data: data}, nil
}

// inlineInstructionFiles replaces the `instruction_file` references of a local configuration
// with the files' contents, the way docker-agent loads them: relative to the configuration's
// directory, confined to it, and joined with a blank line. The provider validates
// configurations in memory, where relative files cannot be read, and docker-agent inlines the
// files itself whenever it pushes or loads a configuration, so the result is the same agent.
// Agents that set both `instruction` and `instruction_file` are left alone; validation
// reports them as docker-agent does.
func inlineInstructionFiles(ctx context.Context, doc *render.Document, dir string) error {
	var root *os.Root
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if len(a.InstructionFile) == 0 || (a.Instruction != nil && *a.Instruction != "") {
			continue
		}
		if root == nil {
			var err error
			if root, err = os.OpenRoot(dir); err != nil {
				return fmt.Errorf("opening %s: %w", dir, err)
			}
			defer root.Close() //nolint:errcheck
		}
		parts := make([]string, 0, len(a.InstructionFile))
		for _, f := range a.InstructionFile {
			if !filepath.IsLocal(f) {
				return fmt.Errorf("agent %q: instruction_file %q must be a local relative path inside %s", a.Name, f, dir)
			}
			data, err := root.ReadFile(filepath.ToSlash(f))
			if err != nil {
				return fmt.Errorf("agent %q: reading instruction_file %q: %w", a.Name, f, err)
			}
			parts = append(parts, string(data))
		}
		p.GetLogger(ctx).Infof("agent %q: the instruction_file contents (%s) are imported as `instruction`",
			a.Name, strings.Join(a.InstructionFile, ", "))
		a.Instruction = ptr(strings.Join(parts, "\n\n"))
		a.InstructionFile = nil
	}
	return nil
}

// withAgentConfigRead keeps a refresh of an AgentConfig exactly as it was before imports
// were supported: the state and inputs the engine sent are returned as they are (the
// resource is pure, there is nothing to read). An import goes to AgentConfig.Read, and the
// imported resource gets its name as ID, like a created one.
func withAgentConfigRead(prov p.Provider) p.Provider {
	read := prov.Read
	prov.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		if req.Urn.Type() != agentConfigType {
			return read(ctx, req)
		}
		if !isEmptyValue(req.Properties.Get("yaml")) || !isEmptyValue(req.Properties.Get("agents")) {
			return p.ReadResponse{ID: req.ID, Properties: req.Properties, Inputs: req.Inputs}, nil
		}
		resp, err := read(ctx, req)
		if err == nil && resp.ID != "" {
			resp.ID = req.Urn.Name()
		}
		return resp, err
	}
	return prov
}

// isEmptyValue reports whether v is null, an empty string or an empty list.
func isEmptyValue(v property.Value) bool {
	switch {
	case v.IsNull():
		return true
	case v.IsString():
		return v.AsString() == ""
	case v.IsArray():
		return v.AsArray().Len() == 0
	}
	return false
}
