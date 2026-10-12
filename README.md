# Pulumi Docker Agent Provider

[![build](https://github.com/dirien/pulumi-docker-agent/actions/workflows/build.yml/badge.svg)](https://github.com/dirien/pulumi-docker-agent/actions/workflows/build.yml)
[![GitHub release](https://img.shields.io/github/v/release/dirien/pulumi-docker-agent)](https://github.com/dirien/pulumi-docker-agent/releases)
[![npm](https://img.shields.io/npm/v/@ediri/pulumi-docker-agent)](https://www.npmjs.com/package/@ediri/pulumi-docker-agent)
[![PyPI](https://img.shields.io/pypi/v/ediri_docker_agent)](https://pypi.org/project/ediri_docker_agent/)
[![NuGet](https://img.shields.io/nuget/v/ediri.DockerAgent)](https://www.nuget.org/packages/ediri.DockerAgent)
[![Go Reference](https://pkg.go.dev/badge/github.com/dirien/pulumi-docker-agent/sdk.svg)](https://pkg.go.dev/github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent)
[![License](https://img.shields.io/github/license/dirien/pulumi-docker-agent)](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE)

A native [Pulumi](https://www.pulumi.com) provider for [Docker Agent](https://docs.docker.com/ai/docker-agent/) (`docker agent`, formerly cagent). Define multi-agent AI configurations as code with typed, validated inputs, publish them to any OCI registry as artifacts that are byte-compatible with `docker agent share push`, read shared agents back, and run agents during `pulumi up`. Anyone can run a published agent with `docker agent run <reference>`.

## Features

- **Typed configurations.** Every key of the Docker Agent configuration is a typed input: agents, sub-agents and handoffs, models and routing, custom providers, MCP, RAG and built-in toolsets, commands, skills, hooks, permissions, sandbox mode (`runtime`), budgets, evaluators, structured output, flavors and metadata. Closed value sets, such as safety modes and hook types, are enums in every SDK. Each object also has an `extra` map for keys that are newer than the pinned docker-agent release.
- **Validated during preview.** The provider embeds docker-agent's configuration loader, so unknown fields, undefined models and missing sub-agents fail before anything is pushed. Every independent problem is reported in the same preview, attached to the input that caused it (`agents[0].subAgents`). No `docker` CLI is needed on the machine that runs Pulumi.
- **Deterministic and compatible.** The YAML and the artifact layer are reproducible, so a `pulumi up` without changes pushes nothing. Pushed artifacts work with `docker agent run`, `docker agent share pull` and `docker agent serve api`, and `getAgent` reads artifacts that the CLI pushed.
- **Agents in the deployment.** `AgentRun` runs a prompt against an agent during `pulumi up`, in the provider process, and exposes the answer (and structured output) to other resources. Tool calls that need approval are rejected unless you opt in, secrets are redacted from errors and logs, and `pulumi up` shows the run's progress and a summary of tokens and cost.
- **Local configuration files.** `UserSettings` manages docker-agent's user configuration (`~/.config/cagent/config.yaml`) and `AgentsIgnore` a project's `.agentsignore`. Both are typed and validated, report drift, and back up a file they take over.
- **Import and convert.** `pulumi import docker-agent:index:AgentConfig assistant ./agent.yaml` turns an agent file, a shared agent or a Docker Agent dotfile into typed code in your language ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#importing-existing-agents-and-files)). `pulumi convert --from docker-agent --language typescript --out ../agents` turns a directory of docker-agent YAML and HCL files into a project with one typed `AgentConfig` per file, comments kept, and `-- --push-to <registry>/<namespace> --run "<prompt>"` adds an `AgentArtifact` and an `AgentRun` to each ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#converting-existing-agent-files)).
- **Policies and Pulumi ESC.** A [policy pack](https://github.com/dirien/pulumi-docker-agent/tree/main/policy) for Pulumi Policies requires `toolApproval: deny` without hook approvals and rejects permission rules that approve every call of a dangerous tool, the `autonomous` safety mode (and `yolo`) and literal credentials, with optional allowlists for model providers, MCP servers and registries. [Pulumi ESC](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#using-pulumi-esc) environments can supply registry credentials, model keys and short-lived Amazon Bedrock credentials.
- **Every Pulumi language.** SDKs for TypeScript, Python, Go, .NET and Java, plus Pulumi YAML and Pulumi HCL. Every resource and function has examples in all seven languages in the [API docs](https://www.pulumi.com/registry/packages/docker-agent/api-docs/).

## Resources and functions

| Name | Kind | Description |
|---|---|---|
| `AgentConfig` | Resource | A typed, validated docker-agent configuration. Outputs deterministic `yaml`, `sha256`, `defaultAgent`, `agentNames`, `requiredEnvVars` and `dockerAgentVersion`. It changes nothing outside the Pulumi state. |
| `AgentArtifact` | Resource | Pushes a configuration to an OCI registry, like `docker agent share push`. Outputs `digest`, `pinnedReference` (`registry/repo@sha256:...`), `contentSha256`, `created` and `manifestAnnotations`. `deleteBehavior` is `retain` (default) or `delete`. |
| `getAgent` | Function | Reads a shared agent by tag or digest, like `docker agent share pull`, without writing files. Returns `yaml`, `digest`, `pinnedReference`, `annotations`, `metadata`, `agentNames` and `defaultAgent`. |
| `validateAgentConfig` | Function | Validates raw agent YAML and returns `valid`, `errors`, `agentNames`, `defaultAgent` and `requiredEnvVars` instead of failing. |
| `AgentRun` | Resource | Runs a prompt against `yaml` or an OCI `reference` during `pulumi up`, never during a preview. Outputs `output`, `structuredOutput`, `agentName`, `usage` (tokens and estimated cost), `toolCalls` and `ranAt`. Tools run on the machine that runs `pulumi up`. |
| `UserSettings` | Resource | Docker Agent's [user configuration](https://docs.docker.com/ai/docker-agent/configuration/user-settings/): settings, aliases, global providers, the default model, the models gateway, the board, the credential helper and the sandbox allowlist. Renders `yaml` exactly as docker-agent writes it; with `path`, writes the file. |
| `AgentsIgnore` | Resource | A [`.agentsignore`](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/) file from ordered `.gitignore`-style `patterns`. Renders `content`; with `directory`, writes the file. |

The tokens are `docker-agent:index:<Name>`. The provider configuration is `registryAuth` (registry credentials, with your Docker credentials as fallback), `forceHttps` and `skipValidation`; see [Configuration](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#configuration).

## Installing

| Language | Package | Install |
|---|---|---|
| TypeScript/JavaScript | [`@ediri/pulumi-docker-agent`](https://www.npmjs.com/package/@ediri/pulumi-docker-agent) | `npm install @ediri/pulumi-docker-agent` |
| Python | [`ediri_docker_agent`](https://pypi.org/project/ediri_docker_agent/) | `pip install ediri_docker_agent` |
| Go | [`github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent`](https://pkg.go.dev/github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent) | `go get github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent` |
| .NET | [`ediri.DockerAgent`](https://www.nuget.org/packages/ediri.DockerAgent) | `dotnet add package ediri.DockerAgent` |
| Java | [`io.github.dirien:docker-agent`](https://central.sonatype.com/artifact/io.github.dirien/docker-agent), package `io.github.dirien.dockeragent` | Gradle `implementation "io.github.dirien:docker-agent:0.1.0"`, or the same coordinates in Maven. Not on Maven Central yet: until Java publishing is turned on, generate the SDK locally ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#java)). |
| Pulumi YAML | none | Install the plugin (below) and use the `docker-agent:index:<Name>` tokens. |
| Pulumi HCL | none | Declare `docker-agent = { source = "pulumi/docker-agent", version = "0.1.0" }` in `required_providers`, install the plugin (below), then run `pulumi install` ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#hcl)). |

The SDKs download the provider plugin from the GitHub release on first use. Pulumi HCL programs need it installed once by hand, because a `required_providers` source does not say where the plugin is published (or a committed package descriptor, see [HCL](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#hcl)). The same command preinstalls it in a CI image:

```bash
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
```

The converter for `pulumi convert --from docker-agent` and `pulumi import --from docker-agent` is a separate plugin of the same release:

```bash
pulumi plugin install converter docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
```

## Quick start

Start a local registry, then create a TypeScript program:

```bash
docker run -d --name agent-registry -p 5000:5000 registry:2

mkdir research-assistant && cd research-assistant
pulumi new typescript --yes
npm install @ediri/pulumi-docker-agent
```

`index.ts`:

```typescript
import * as dockeragent from "@ediri/pulumi-docker-agent";

const assistant = new dockeragent.AgentConfig("research-assistant", {
    models: {
        fast: { provider: "openai", model: "gpt-5-mini", maxTokens: 16000 },
    },
    agents: [
        {
            name: "root",
            model: "fast",
            description: "Answers questions and delegates web research",
            instruction: "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.",
            subAgents: ["researcher"],
        },
        {
            name: "researcher",
            model: "fast",
            description: "Searches the web with DuckDuckGo",
            instruction: "Search the web and answer with short summaries that cite their sources.",
            toolsets: [{ type: "mcp", ref: "docker:duckduckgo" }],
        },
    ],
    metadata: { author: "Jane Doe", license: "Apache-2.0", version: "1.0.0", tags: ["research"] },
});

const artifact = new dockeragent.AgentArtifact("research-assistant", {
    reference: "localhost:5000/agents/research-assistant:v1",
    yaml: assistant.yaml,
});

const pulled = dockeragent.getAgentOutput({ reference: artifact.pinnedReference });

export const pinnedReference = artifact.pinnedReference;
export const requiredEnvVars = assistant.requiredEnvVars;
export const pulledAgents = pulled.agentNames;
```

```bash
pulumi up

export OPENAI_API_KEY=<your-openai-api-key>
docker agent run localhost:5000/agents/research-assistant:v1
```

The same program in Python, Go, C#, Java, YAML and HCL is in the [Example Usage](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/_index.md#example-usage) of the overview page. On macOS, AirPlay Receiver may already use port 5000; map another port (`-p 5001:5000`) and change the reference.

## Documentation

- [Overview](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/_index.md) (the Pulumi Registry's package page): examples in every language, sandbox mode, local configuration files, `AgentRun` and where its tools run, importing and converting.
- [Installation & Configuration](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md): installing per language and for Pulumi HCL, registry credentials, Pulumi ESC, plain-HTTP registries, local files, importing, converting, model credentials, validation errors and compatibility with newer docker-agent releases.
- [API docs](https://www.pulumi.com/registry/packages/docker-agent/api-docs/): every resource, function, input and output, with examples in seven languages.
- [Policy pack](https://github.com/dirien/pulumi-docker-agent/blob/main/policy/README.md), [examples](https://github.com/dirien/pulumi-docker-agent/blob/main/examples/README.md) and the [changelog](https://github.com/dirien/pulumi-docker-agent/blob/main/CHANGELOG.md).

## Examples

| Example | What it shows |
|---|---|
| [`yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/yaml), [`nodejs`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/nodejs), [`python`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/python), [`go`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/go), [`dotnet`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/dotnet), [`java`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/java), [`hcl`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/hcl) | The same program in seven languages: a two-agent configuration pushed with `AgentArtifact`, read back with `getAgent`, and a hand-written `agent.yaml` checked with `validateAgentConfig`. |
| [`docker-serve-ts`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/docker-serve-ts) | With the [Pulumi Docker provider](https://www.pulumi.com/registry/packages/docker/): runs `registry:2`, pushes the agent there and serves it with `docker agent serve api` in a container with a health check. |
| [`user-settings-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/user-settings-yaml) | `UserSettings` and `AgentsIgnore` writing into a scratch directory, and a user configuration that is only rendered. |
| [`agent-run-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/agent-run-yaml) | `AgentRun` runs a change-review agent with structured output against any OpenAI-compatible endpoint. |
| [`esc-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/esc-yaml) | Registry credentials, an Anthropic key and Amazon Bedrock credentials (`fn::open::aws-login`) from a Pulumi ESC environment. |
| [`automation-ts`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/automation-ts) | A CI gate built with the Automation API that reads an agent's verdict with `stack.outputs()`. |
| [`docker-agent-examples`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/docker-agent-examples) | Every example configuration of the pinned docker-agent release as a TypeScript and a Python program, with typed inputs and the comments of the upstream files. Generated by `make generate_config_types`. |

The examples use the provider and the SDKs built from this repository, and each example's README says how to switch to the published packages. [`examples/README.md`](https://github.com/dirien/pulumi-docker-agent/blob/main/examples/README.md) explains how to run them.

## Configuration coverage

The `AgentConfig` inputs are generated from the `agent-schema.json` of the pinned docker-agent release (`provider/internal/schemagen`), with the schema's descriptions as API docs. Every configuration key that docker-agent documents and validates is a typed input. Only the values the schema itself leaves free-form are maps: `providerOpts`, MCP `config`, JSON schemas (`structuredOutput.schema`, `apiConfig.outputSchema`), script `args` and flavor patches. YAML shorthands have typed equivalents (`simpleCommands`, `inlineSkills`, `thinkingBudgetTokens`, `deferTools`), listed on the [overview page](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/_index.md#sandbox-mode-and-every-other-configuration-key). Tests enforce the coverage:

- [`coverage_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/coverage_test.go) walks every definition, property and union branch of the pinned schema and fails on any value without a typed field, unless it is on a short, commented allowlist of free-form values and equivalent shorthand forms.
- [`roundtrip_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/roundtrip_test.go) decodes every example configuration that docker-agent ships into the typed inputs with nothing left over, renders it again and checks that docker-agent loads the same configuration.
- [`docs_examples_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/docs_examples_test.go) writes at least one example from each page of the [configuration docs](https://docs.docker.com/ai/docker-agent/configuration/overview/) as typed inputs and checks that docker-agent loads the same configuration as the docs' YAML.

## How the artifact format matches `docker agent share push`

`AgentArtifact` does not shell out to the CLI. It builds the artifact with [go-containerregistry](https://github.com/google/go-containerregistry), reproducing the wire format that the pinned docker-agent release pushes (verified against `registry:2`):

| Part | Value |
|---|---|
| Manifest media type | `application/vnd.oci.image.manifest.v1+json` (`schemaVersion: 2`) |
| `artifactType` | `application/vnd.docker.agent.config.v1+json` |
| Config blob | `application/vnd.docker.container.image.v1+json`, a near-empty image config whose `rootfs.diff_ids[0]` is the sha256 of the YAML |
| Layer (exactly one) | `application/vnd.docker.image.rootfs.diff.tar.gzip`, the **gzip of the raw YAML bytes** (not a tar), written with no name and mtime 0 so identical YAML gives an identical layer digest |
| Required annotations | `io.docker.agent.version` and `io.docker.cagent.version` = the pinned docker-agent version. `docker agent share pull` rejects manifests without them. |
| Other annotations | `org.opencontainers.image.created`, `org.opencontainers.image.description` (`OCI artifact containing agent.yaml`), and from the YAML `metadata` block: `author` → `org.opencontainers.image.authors`, `license` → `org.opencontainers.image.licenses`, `version` → `org.opencontainers.image.revision`, `tags` → `io.docker.agent.tags` (comma-separated). Extra annotations come from the `annotations` input. |

Two deliberate differences from the CLI:

- The CLI stamps `org.opencontainers.image.created` with the current time on every push, so pushing identical YAML twice yields two manifest digests. The provider stamps it on create and update only and keeps it in state, so `pulumi up` with no changes does not push again and `pulumi refresh` does not flap.
- The CLI also writes a copy into its local store (`~/.cagent/store`). The provider works purely in memory, and `getAgent` writes nothing to disk (the CLI's `share pull` writes `<registry>_<repo>:<tag>.yaml` into the current directory).

`getAgent` accepts layers that are gzip-compressed (as pushed by the CLI) or plain YAML, and like the CLI it rejects manifests without the `io.docker.agent.version`/`io.docker.cagent.version` annotation. Signed (`--key`) and encrypted (`--encrypt`) artifacts are not supported.

## Development

[CONTRIBUTING.md](https://github.com/dirien/pulumi-docker-agent/blob/main/CONTRIBUTING.md) lists the prerequisites (Go, the Pulumi CLI of `.pulumi.version`, pulumictl, Node.js, Python, .NET, Gradle with two JDKs, Docker and golangci-lint) and how to make a change. The main targets (`make help` lists all of them):

| Target | What it does |
|---|---|
| `make provider` | Build `bin/pulumi-resource-docker-agent` |
| `make converter` | Build `bin/pulumi-converter-docker-agent`, the converter plugin |
| `make install_plugin`, `make install_converter` | Install the local binaries as the `docker-agent` resource and converter plugins |
| `make codegen` | `generate_config_types` (typed inputs and `examples/docker-agent-examples`), `docs` (the Example Usage sections, from `docs/yaml`), `schema`, then every SDK under `sdk/` (all committed) |
| `make build_sdks` | Build all SDKs, or one with `build_nodejs`, `build_python`, `build_go`, `build_dotnet` or `build_java` (the jars and POM that Maven Central gets). Accepts `PROVIDER_VERSION=x.y.z` |
| `make lint` | `golangci-lint` with `.golangci.yml` |
| `make test_provider` | Unit and in-process integration tests (`go test ./...` in `provider/`, against an in-memory registry and a fake OpenAI-compatible server) |
| `make test_compat` | Compatibility tests against the real `docker-agent` CLI (needs `docker-agent` on `PATH` and Docker; starts its own `registry:2`) |
| `make test_policy` | Type-check and unit-test the policy pack in `policy/` (after `make build_nodejs`) |
| `make test_examples` | End to end: builds the provider, the converter and the Node.js, Python and .NET SDKs (the Java example builds `sdk/java` itself), runs `up`, `preview`, `refresh` and `destroy` for every program in `examples/` against a registry (`DOCKER_AGENT_TEST_REGISTRY`, or a throwaway `registry:2`), previews and compiles the docs examples, runs `pulumi import`, and converts every upstream docker-agent example with the converter. `EXAMPLES_RUN=TestExampleGo` selects tests |
| `make snapshot` | Build all release archives into `dist/` without publishing |

The tests need no model API keys and no Pulumi Cloud account: agents run against a fake OpenAI-compatible server (`provider/pkg/agentrun/fakeopenai`), the examples use a `file://` backend, and the one check that uses the Pulumi Cloud API skips itself without `PULUMI_ACCESS_TOKEN`. The provider binary is about 115 MB (about 40 MB without `AgentRun`), because `AgentRun` links docker-agent's runtime with every model provider SDK and built-in toolset; a cold `make provider` takes about 20 s on 18 cores and fills about 1.8 GB of Go build cache. The converter does not link the runtime and is about 34 MB.

CI (`.github/workflows/build.yml`) runs lint, `make provider`, `make test_provider` and `make codegen` (and fails when the committed examples, schema or SDKs are stale), builds every SDK, installs the matching `docker-agent` CLI, and runs `make test_compat`, `make test_examples` and `make test_policy`. On pull requests it compares `schema.json` with the base branch using [schema-tools](https://github.com/pulumi/schema-tools) and labels SDK-breaking changes `breaking-sdk-change`. Only pushes to `main` check the ESC example environment with the Pulumi Cloud API, in a separate job that builds nothing and is the only one that gets `PULUMI_ACCESS_TOKEN`.

### Upgrading docker-agent

The daily [`upgrade-docker-agent`](https://github.com/dirien/pulumi-docker-agent/actions/workflows/upgrade-docker-agent.yml) workflow runs `make upgrade_docker_agent` when docker-agent has a new release, and opens a pull request `upgrade-docker-agent-to-vX.Y.Z` that regenerates the typed inputs, the generated example programs, the schema and the SDKs. Its description reports the configuration schema changes, the changed examples and the SDK-breaking changes. When a step fails, the pull request is still opened, labelled `needs-attention`, with the failing step's output. To upgrade locally:

```bash
make check_docker_agent_upgrade                          # current=… latest=… upgrade=true|false
make upgrade_docker_agent                                # to the latest release
make upgrade_docker_agent DOCKER_AGENT_VERSION=v1.151.0  # to a specific release
```

[Upgrading docker-agent](https://github.com/dirien/pulumi-docker-agent/blob/main/CONTRIBUTING.md#upgrading-docker-agent) in CONTRIBUTING.md describes the steps, the report and how to finish an upgrade that needs attention.

## Releasing

Releases are cut from `main` by pushing a `vX.Y.Z` tag. [`.github/workflows/release.yml`](https://github.com/dirien/pulumi-docker-agent/blob/main/.github/workflows/release.yml) then:

1. builds the provider and converter plugin archives with GoReleaser (`.goreleaser.yml`): `pulumi-resource-docker-agent-v<version>-<os>-<arch>.tar.gz` and `pulumi-converter-docker-agent-v<version>-<os>-<arch>.tar.gz` for `linux`, `darwin` and `windows` on `amd64` and `arm64`, plus a checksums file, a keyless cosign signature bundle (`*_checksums.txt.sigstore.json`) and SBOMs. It publishes them as a GitHub release whose notes are generated from the commit messages;
2. tags the Go SDK module `sdk/v<version>` on the same commit;
3. builds the npm, PyPI and NuGet packages from the committed SDK sources with the version injected, refuses to publish when the build changed a committed file, and publishes `@ediri/pulumi-docker-agent`, `ediri_docker_agent` (wheel and sdist) and `ediri.DockerAgent`;
4. builds the Java SDK the same way and publishes `io.github.dirien:docker-agent` to Maven Central, signed. This job is off for now (`PUBLISH_JAVA: "false"` in the workflow); it also skips itself without the Maven Central secrets, and the rest of the release succeeds either way.

Tags with a pre-release suffix (`v0.2.0-rc.1`) become GitHub pre-releases and are published to npm under the `dev` dist-tag; the Pulumi Registry ignores them. Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/), so the release notes group `feat:` and `fix:` changes.

### Release checklist

Once, before the first release:

- [ ] Set the repository secrets of the release workflow: `NPM_TOKEN` (an npm token that can publish `@ediri/pulumi-docker-agent`), `PYPI_PASSWORD` (a PyPI API token; the user is `__token__`), and `NUGET_PUBLISH_KEY`. Actions provides `GITHUB_TOKEN`.
- [ ] Only to publish the Java SDK (off for now): set `PUBLISH_JAVA: "true"` in the release workflow and the Maven Central secrets `PUBLISH_REPO_USERNAME` and `PUBLISH_REPO_PASSWORD` (a Central Portal user token), `SIGNING_KEY` (an ASCII-armored GPG private key) and `SIGNING_PASSWORD`, verify the `io.github.dirien` namespace on the Central Portal and publish the GPG public key to a key server. [Maven Central (Java SDK)](https://github.com/dirien/pulumi-docker-agent/blob/main/CONTRIBUTING.md#maven-central-java-sdk) in CONTRIBUTING.md has the steps.
- [ ] Optionally set `PULUMI_ACCESS_TOKEN`. Only the `build` workflow uses it, to check the ESC example environment with the Pulumi Cloud API on pushes to `main`; without it, that check skips itself.

For every release:

- [ ] Check that CI on `main` is green, including the codegen check: the release refuses to publish SDKs that differ from the committed sources.
- [ ] In `CHANGELOG.md`, move the entries under `## [Unreleased]` into the new version and replace `TBD` in its heading (`## [0.1.0] - TBD`) with the release date (`YYYY-MM-DD`). Commit and push to `main`.
- [ ] Tag that commit and push the tag:

  ```bash
  git checkout main && git pull
  git tag v0.1.0
  git push origin v0.1.0
  ```

- [ ] When the `release` workflow has finished, check the [GitHub release](https://github.com/dirien/pulumi-docker-agent/releases/tag/v0.1.0): it is **not** a pre-release and has twelve archives (six `pulumi-resource-docker-agent-v0.1.0-<os>-<arch>.tar.gz` and six `pulumi-converter-docker-agent-v0.1.0-<os>-<arch>.tar.gz`), `pulumi-docker-agent_0.1.0_checksums.txt` with its `.sigstore.json` bundle, and the SBOMs. Check that the `sdk/v0.1.0` tag exists and that the "Publish Java SDK" job published rather than skipped.
- [ ] Run the install probes that the Pulumi Registry runs:

  ```bash
  pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent   # blocking
  npm install --prefix "$(mktemp -d)" @ediri/pulumi-docker-agent@0.1.0
  pip download --no-deps --only-binary :all: -d "$(mktemp -d)" ediri_docker_agent==0.1.0
  (cd "$(mktemp -d)" && go mod init probe && go get github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent@v0.1.0)
  pulumi plugin install converter docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
  ```

After the first release:

- [ ] List the package in the Pulumi Registry (below).
- [ ] Switch the policy pack from the local SDK build to the published one: in `policy/package.json`, replace `file:../sdk/nodejs/bin` with `^0.1.0` ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/policy/README.md#use-it-locally)).

### Listing in the Pulumi Registry

The package is listed in the [Pulumi Registry](https://www.pulumi.com/registry/) as a community package. For the first listing:

- [ ] `provider/cmd/pulumi-resource-docker-agent/schema.json` is committed **without** a `version` field (`make schema` strips it) and has `displayName: Docker Agent` (equal to the `title` of `docs/_index.md`), `publisher: dirien`, a `logoUrl`, `pluginDownloadURL: github://api.github.com/dirien/pulumi-docker-agent` and the `category/utility` and `kind/native` keywords.
- [ ] `docs/_index.md` starts with front matter (`title`, `meta_desc`, `layout: package`), has no H1, and has `## Installation` with all seven language tabs, `## Example Usage` and `## Configuration`. Its links are absolute.
- [ ] Open a pull request against [pulumi/registry](https://github.com/pulumi/registry) that changes **only** `community-packages/package-list.json`, adding this entry to the `include` array (`dirien` is already a known publisher):

  ```json
  {
    "repoSlug": "dirien/pulumi-docker-agent",
    "schemaFile": "provider/cmd/pulumi-resource-docker-agent/schema.json"
  }
  ```

  Title it `Add dirien/pulumi-docker-agent community package` and say in the body that you maintain it (`@dirien`). Automated checks post a fact sheet; if one is red, fix it in this repository (cut a new release if needed) and comment `/check`. A Pulumi maintainer can run `/preview`, then reviews and merges.
- [ ] After the merge, the Registry picks up new GitHub releases automatically, twice a day. `v0.x` versions are listed as public preview, `v1.0.0` and later as generally available.

## Contributing

See [CONTRIBUTING.md](https://github.com/dirien/pulumi-docker-agent/blob/main/CONTRIBUTING.md) and the [Code of Conduct](https://github.com/dirien/pulumi-docker-agent/blob/main/CODE-OF-CONDUCT.md).

## License

[Apache License 2.0](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE). Docker and Docker Agent are trademarks of Docker, Inc. This project is not affiliated with or endorsed by Docker, Inc.
