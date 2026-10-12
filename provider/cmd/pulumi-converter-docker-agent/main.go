// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Command pulumi-converter-docker-agent is the Pulumi converter plugin for Docker Agent:
//
//	pulumi convert --from docker-agent --language <typescript|python|go|csharp|java|yaml|hcl> --out <dir>
//
// turns the docker-agent agent files of the current directory (`*.yaml`, `*.yml` and
// docker-agent `*.hcl`) into a Pulumi program with one AgentConfig per file, and
//
//	pulumi import --from docker-agent <registry>/<repository>
//
// imports the agent artifacts of an OCI repository as AgentArtifact resources. The program is
// written as PCL (package pkg/pcl), from which the CLI generates the program in the requested
// language. Arguments after `--` are passed to the converter, flags and paths in any order:
//
//	--push-to <registry>/<namespace>  also push each configuration with an AgentArtifact
//	--run <prompt>                    also run the prompt against each configuration with an AgentRun
//	--provider-version <version>      the docker-agent provider version to generate the program for
//	<file or directory>...            convert only these (relative to the current directory)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/blang/semver"
	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/rpcutil"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dirien/pulumi-docker-agent/provider"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/pcl"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println(provider.Version)
		return
	}
	handle, err := rpcutil.ServeWithOptions(rpcutil.ServeOptions{
		Init: func(srv *grpc.Server) error {
			pulumirpc.RegisterConverterServer(srv, plugin.NewConverterServer(&converter{version: provider.Version}))
			return nil
		},
		Options: rpcutil.OpenTracingServerInterceptorOptions(nil),
	})
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}
	// The plugin protocol: print the port, then serve until the CLI stops the plugin.
	fmt.Printf("%d\n", handle.Port)
	if err := <-handle.Done; err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

// converter implements the Pulumi converter protocol.
type converter struct {
	// version is the converter's version, which is also the provider version it generates
	// programs for.
	version string
}

func (*converter) Close() error { return nil }

// programArgs are the converter arguments of `pulumi convert ... -- <args>`.
type programArgs struct {
	pcl.Options
	paths []string
}

func parseProgramArgs(args []string, version string) (programArgs, error) {
	var out programArgs
	fs := flag.NewFlagSet("pulumi convert --from docker-agent ... --", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out.PushTo, "push-to", "",
		"push each configuration with an AgentArtifact to <registry>/<namespace>/<name>:latest")
	fs.StringVar(&out.Run, "run", "", "send this prompt to each configuration with an AgentRun")
	fs.StringVar(&out.ProviderVersion, "provider-version", releaseVersion(version),
		"the docker-agent provider version to generate the program for")
	// The flags may come before, between or after the paths; `--` ends the flags.
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return out, fmt.Errorf("%w; supported arguments: --push-to <registry>/<namespace>, --run <prompt>, "+
				"--provider-version <version>, and the files or directories to convert", err)
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			out.paths = append(out.paths, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		out.paths = append(out.paths, rest[0])
		args = rest[1:]
	}
	if v := out.ProviderVersion; v != "" {
		if _, err := semver.Parse(strings.TrimPrefix(v, "v")); err != nil {
			return out, fmt.Errorf("--provider-version %q: %w", v, err)
		}
		out.ProviderVersion = strings.TrimPrefix(v, "v")
	}
	return out, nil
}

// releaseVersion returns version when it is the version of a release, which the programs pin
// the provider to: the converter and the provider are released together, so the program uses
// exactly the inputs the converter knows. Development builds (`0.0.1-alpha.*`, snapshots,
// build metadata) pin nothing, and `pulumi convert` uses the installed provider.
func releaseVersion(version string) string {
	v, err := semver.Parse(strings.TrimPrefix(version, "v"))
	if err != nil || len(v.Build) > 0 {
		return ""
	}
	for _, pre := range v.Pre {
		s := strings.ToLower(pre.String())
		if strings.Contains(s, "alpha") || strings.Contains(s, "dev") || strings.Contains(s, "snapshot") {
			return ""
		}
	}
	return v.String()
}

// ConvertProgram writes the program for the agent files of req.SourceDirectory to
// req.TargetDirectory: main.pp, and the files the program or the configurations read.
func (c *converter) ConvertProgram(_ context.Context, req *plugin.ConvertProgramRequest) (*plugin.ConvertProgramResponse, error) {
	args, err := parseProgramArgs(req.Args, c.version)
	if err != nil {
		return failed("Invalid converter arguments", err), nil
	}
	var exclude []string
	if out := req.GeneratedProjectDirectory; out != "" {
		if !filepath.IsAbs(out) {
			out = filepath.Join(req.SourceDirectory, out)
		}
		if rel, err := filepath.Rel(req.SourceDirectory, out); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			exclude = append(exclude, out)
		}
	}
	configs, diags := pcl.Load(req.SourceDirectory, pcl.LoadOptions{Paths: args.paths, Exclude: exclude})
	if diags.HasErrors() {
		return &plugin.ConvertProgramResponse{Diagnostics: diags}, nil
	}
	if len(configs) == 0 {
		return &plugin.ConvertProgramResponse{Diagnostics: append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "No docker-agent configuration found",
			Detail: fmt.Sprintf("%s has no agent files: YAML files (*.yaml, *.yml) with a top-level `agents` key "+
				"or docker-agent HCL files (*.hcl).", req.SourceDirectory),
		})}, nil
	}
	program, warnings, err := pcl.Program(configs, args.Options)
	if err != nil {
		return &plugin.ConvertProgramResponse{Diagnostics: append(diags, failed("Cannot write the program", err).Diagnostics...)}, nil
	}
	diags = append(diags, warnings...)
	var files []string
	for _, cfg := range configs {
		files = append(files, cfg.Files...)
	}
	if err := writeProgram(req.SourceDirectory, req.TargetDirectory, program, files); err != nil {
		return nil, err
	}
	return &plugin.ConvertProgramResponse{Diagnostics: diags}, nil
}

// writeProgram writes main.pp to the target directory and copies the files the program or
// the configurations read (relative to the source directory, slash-separated) to the same path
// below it: `pulumi convert` copies every file of the target directory but the PCL next to
// the generated program, so the files keep their path relative to the program. Both
// directories are opened as an os.Root, so no path can leave them.
func writeProgram(srcDir, dstDir string, program []byte, files []string) error {
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(dstDir)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err := dst.WriteFile("main.pp", program, 0o600); err != nil {
		return err
	}
	for _, f := range files {
		name := filepath.FromSlash(f)
		data, err := src.ReadFile(name)
		if err != nil {
			return err
		}
		if err := dst.MkdirAll(filepath.Dir(name), 0o750); err != nil {
			return err
		}
		if err := dst.WriteFile(name, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func failed(summary string, err error) *plugin.ConvertProgramResponse {
	return &plugin.ConvertProgramResponse{Diagnostics: hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   err.Error(),
	}}}
}

// ConvertState lists the agent artifacts of an OCI repository for `pulumi import --from
// docker-agent <registry>/<repository> [-- --insecure]`.
func (c *converter) ConvertState(ctx context.Context, req *plugin.ConvertStateRequest) (*plugin.ConvertStateResponse, error) {
	fs := flag.NewFlagSet("pulumi import --from docker-agent", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	insecure := fs.Bool("insecure", false, "allow a plain-HTTP or self-signed registry")
	if err := fs.Parse(req.Args); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v; usage: pulumi import --from docker-agent "+
			"<registry>/<repository> [-- --insecure]", err)
	}
	if fs.NArg() != 1 {
		return nil, status.Error(codes.InvalidArgument,
			"expected one argument, the repository to import: pulumi import --from docker-agent <registry>/<repository>")
	}
	resources, diags, err := importArtifacts(ctx, fs.Arg(0), *insecure, releaseVersion(c.version), registryOptions(ctx)...)
	if err != nil {
		return nil, err
	}
	return &plugin.ConvertStateResponse{Resources: resources, Diagnostics: diags}, nil
}

func (*converter) ConvertSnippet(context.Context, *plugin.ConvertSnippetRequest) (*plugin.ConvertSnippetResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ConvertSnippet is not implemented")
}
