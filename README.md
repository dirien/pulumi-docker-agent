# Pulumi Docker Agent Provider

[![build](https://github.com/dirien/pulumi-docker-agent/actions/workflows/build.yml/badge.svg)](https://github.com/dirien/pulumi-docker-agent/actions/workflows/build.yml)
[![GitHub release](https://img.shields.io/github/v/release/dirien/pulumi-docker-agent)](https://github.com/dirien/pulumi-docker-agent/releases)
[![npm](https://img.shields.io/npm/v/@ediri/pulumi-docker-agent)](https://www.npmjs.com/package/@ediri/pulumi-docker-agent)
[![PyPI](https://img.shields.io/pypi/v/ediri_docker_agent)](https://pypi.org/project/ediri_docker_agent/)
[![NuGet](https://img.shields.io/nuget/v/ediri.DockerAgent)](https://www.nuget.org/packages/ediri.DockerAgent)
[![Go Reference](https://pkg.go.dev/badge/github.com/dirien/pulumi-docker-agent/sdk.svg)](https://pkg.go.dev/github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent)
[![License](https://img.shields.io/github/license/dirien/pulumi-docker-agent)](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE)

A native [Pulumi](https://www.pulumi.com) provider for [Docker Agent](https://docs.docker.com/ai/docker-agent/) (`docker agent`, formerly cagent). Define multi-agent AI configurations as code with typed, validated inputs, publish them to any OCI registry as artifacts that are byte-compatible with `docker agent share push`, and read shared agents back. Anyone can then run them with `docker agent run <reference>`.

- **Typed configurations.** Every key of the Docker Agent configuration is a typed input. That covers agents, sub-agents and handoffs, models and routing, custom providers, MCP, RAG and built-in toolsets, commands, skills, hooks, permissions, sandbox mode (`runtime`), budgets, evaluators, structured output, flavors and metadata. Each object also has an `extra` map for keys newer than the pinned docker-agent version.
- **Validated during preview.** The provider embeds the docker-agent configuration loader (`github.com/docker/docker-agent` v1.150.0, config version `"16"`), so unknown fields, undefined models and missing sub-agents fail before anything is pushed. No `docker` CLI is needed on the machine that runs Pulumi.
- **Deterministic.** The YAML is rendered deterministically, the artifact layer is reproducible, and a no-op `pulumi up` does not re-push.
- **Compatible.** Artifacts pushed by the provider work with `docker agent run`, `docker agent share pull` and `docker agent serve api`, and `getAgent` reads artifacts pushed by the CLI.
- **Local configuration too.** `UserSettings` manages docker-agent's user configuration (`~/.config/cagent/config.yaml`) and `AgentsIgnore` a project's `.agentsignore`, both typed, validated and with drift detection.
- **Agents in the deployment.** `AgentRun` runs a prompt against an agent during `pulumi up`, in process with the docker-agent Go SDK, and exposes the answer (and structured output) to other resources. Tool calls that need approval are rejected unless you opt in.

## Resources and functions

| Token | Kind | Description |
|---|---|---|
| `docker-agent:index:AgentConfig` | Resource | A typed, validated docker-agent configuration: every configuration key (agents, models, providers, toolsets, MCP and RAG definitions, commands, skills, hooks, permissions, sandbox mode, budgets, evaluators, structured output, flavors) is a typed input. Outputs deterministic `yaml`, `sha256`, `defaultAgent`, `agentNames`, `requiredEnvVars` and `dockerAgentVersion`. Pure and offline: no external side effects. |
| `docker-agent:index:AgentArtifact` | Resource | Pushes a configuration to an OCI registry (the `docker agent share push` equivalent). Takes extra manifest `annotations`; outputs `digest`, `pinnedReference` (`registry/repo@sha256:...`), `contentSha256`, `created` and the final `manifestAnnotations`. Changing `reference` replaces the resource; `deleteBehavior` is `retain` (default) or `delete`. |
| `docker-agent:index:getAgent` | Function | Reads a shared agent by tag or digest (the `docker agent share pull` equivalent, without writing files). Returns `yaml`, `digest`, `pinnedReference`, `annotations`, `metadata`, `agentNames` and `defaultAgent`. |
| `docker-agent:index:validateAgentConfig` | Function | Validates arbitrary agent YAML and returns `valid`, `errors`, `agentNames`, `defaultAgent` and `requiredEnvVars`. Never fails on invalid YAML. |
| `docker-agent:index:UserSettings` | Resource | Docker Agent's [user configuration](https://docs.docker.com/ai/docker-agent/configuration/user-settings/) (`~/.config/cagent/config.yaml`): every key of docker-agent's `settings` block (TUI, safety, permissions, hooks, keybindings, layout), `aliases`, global `providers`, `defaultModel`, `modelsGateway`, `board`, `credentialHelper` and `sandboxAllowlist`, plus `extra`. Validated with docker-agent's own checks; outputs `yaml` (exactly what docker-agent writes) and `sha256`. With `path`, writes the file (mode 0600) and reports drift on refresh; without it, only renders. |
| `docker-agent:index:AgentsIgnore` | Resource | A [`.agentsignore`](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/) file from ordered `.gitignore`-style `patterns` and an optional `comment`, validated with go-git's gitignore rules (the library docker-agent uses). Outputs `content` and `sha256`; with `directory`, writes `<directory>/.agentsignore` and reports drift on refresh. |
| `docker-agent:index:AgentRun` | Resource | Runs a prompt against an agent configuration (`yaml` or an OCI `reference`) during `pulumi up`, in the provider process, with model credentials from `env` (secret) or the environment. Outputs the final answer (`output`), `structuredOutput` (when the agent declares structured output), `agentName`, `usage` (tokens and estimated cost), `toolCalls` and `ranAt`. Never runs during previews; any input change, including `triggers`, runs it again. With the default `toolApproval: deny`, only tool calls docker-agent approves without asking run (read-only tools, `permissions` allow rules); `allow` runs every tool call. Tools run on the machine executing `pulumi up`. |

Provider configuration: `docker-agent:registryAuth` (a list of `{address, username, password}`; falls back to Docker credentials from `DOCKER_CONFIG` / `~/.docker/config.json`) and `docker-agent:skipValidation` (env `DOCKER_AGENT_SKIP_VALIDATION`).

## Installing

| Language | Package | Install |
|---|---|---|
| TypeScript/JavaScript | [`@ediri/pulumi-docker-agent`](https://www.npmjs.com/package/@ediri/pulumi-docker-agent) | `npm install @ediri/pulumi-docker-agent` or `yarn add @ediri/pulumi-docker-agent` |
| Python | [`ediri_docker_agent`](https://pypi.org/project/ediri_docker_agent/) | `pip install ediri_docker_agent` |
| Go | [`github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent`](https://pkg.go.dev/github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent) | `go get github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent` |
| .NET | [`ediri.DockerAgent`](https://www.nuget.org/packages/ediri.DockerAgent) | `dotnet add package ediri.DockerAgent` |
| YAML | n/a | use the `docker-agent:index:<Type>` tokens |
| HCL | n/a | declare `docker-agent = { source = "pulumi/docker-agent", version = "0.1.0" }` in `required_providers`, then `pulumi install` ([details](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#hcl)) |

The SDKs download the provider plugin automatically. To install it by hand:

```bash
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
```

[Pulumi HCL](https://www.pulumi.com/docs/iac/languages-sdks/hcl/) programs (`runtime: hcl`) need that command once before `pulumi install`, because a `required_providers` source does not say where the plugin is published (or commit the `sdks/dirien-docker-agent/hcl.sdk.json` descriptor that `pulumi package add docker-agent@0.1.0` writes). In HCL the resources are `docker-agent_agent_config`, `docker-agent_agent_artifact` and `docker-agent_agent_run`, the functions are the `docker-agent_agent` and `docker-agent_validate_agent_config` data sources, properties are snake_case, and lists of objects such as `agents` are repeated nested blocks.

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

Python, Go, C#, YAML and HCL versions of this program are in [docs/_index.md](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/_index.md), and runnable examples are in [examples/](https://github.com/dirien/pulumi-docker-agent/tree/main/examples). Configuration options, registry authentication and the environment variables agents need at runtime are documented in [docs/installation-configuration.md](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md).

## Examples

[`examples/`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples) has the same program in six languages: [YAML](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/yaml), [TypeScript](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/nodejs), [Python](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/python), [Go](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/go), [C#](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/dotnet) and [Pulumi HCL](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/hcl). Each one:

1. defines an `AgentConfig` with a `root` coordinator and a `researcher` sub-agent that uses the DuckDuckGo MCP server (`ref: docker:duckduckgo`), a named Anthropic model whose key comes from `AGENT_ANTHROPIC_API_KEY` (`tokenKey`), and metadata;
2. pushes it with an `AgentArtifact` to `<registry>/examples/<language>-agent:v1`, where `registry` is a stack setting (default `localhost:5000`), with `deleteBehavior: delete`;
3. reads it back with `getAgent` and validates a hand-written `agent.yaml` with `validateAgentConfig`;
4. exports `digest`, `pinnedReference`, `runCommand` (`docker agent run <pinnedReference>`), `requiredEnvVars`, the pushed and pulled YAML, and the validation result.

[`examples/docker-serve-ts`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/docker-serve-ts) combines the provider with the [Pulumi Docker provider](https://www.pulumi.com/registry/packages/docker/): it runs a `registry:2` on a user-defined network, pushes the agent there, and serves it with `docker agent serve api` in a `docker/docker-agent:1.150.0` container with a health check on `/health`.

[`examples/user-settings-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/user-settings-yaml) writes Docker Agent's user configuration and a `.agentsignore` file into a scratch directory with `UserSettings` and `AgentsIgnore`, and renders a second user configuration for a container image.

[`examples/agent-run-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/agent-run-yaml) runs a change-review agent with `AgentRun` during `pulumi up` and exports its structured answer. Its model provider is any OpenAI-compatible endpoint (a stack setting); `make test_examples` points it at a fake server, so no API key is needed.

The examples run against the provider and SDKs built from this repository: each `Pulumi.yaml` points `plugins.providers` at `../../bin`, and each program references the local SDK build. Every example's README explains how to switch to the published packages. `make test_examples` runs all of them end to end (see [Testing locally](#testing-locally-against-registry2)).

## Configuration coverage

The `AgentConfig` inputs are generated from the `agent-schema.json` that ships with the pinned docker-agent release (`provider/internal/schemagen`, `make generate_config_types`). **Every configuration key that Docker Agent documents and validates is a typed input**, with the schema's descriptions as API docs. The exceptions are the values the schema itself leaves free-form: `providerOpts`, MCP `config`, JSON schemas in `structuredOutput.schema`, `apiConfig.outputSchema` and script `args`, and flavor patches. YAML shorthands map to typed inputs as follows:

- The bare-string command form is `simpleCommands`; the object form is `commands`.
- `skills: true` is `skills: ["local"]`; inline skills go in `inlineSkills`.
- `thinking_budget` is `thinkingBudget` (an effort level) or `thinkingBudgetTokens`.
- `defer` is `defer` or `deferTools`.

Tests enforce the coverage:

- [`coverage_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/coverage_test.go) walks every definition, property and union branch of the pinned schema. It fails on any value without a typed field, unless the value is on a short, commented allowlist of free-form values and equivalent shorthand forms.
- [`roundtrip_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/roundtrip_test.go) takes every example configuration that docker-agent ships, decodes it into the typed inputs with nothing left over, renders it again and checks that docker-agent loads the same configuration.
- [`docs_examples_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/provider/pkg/render/docs_examples_test.go) writes at least one example from each page of the [configuration docs](https://docs.docker.com/ai/docker-agent/configuration/overview/) as typed inputs and checks that docker-agent loads the same configuration as the docs' YAML.

## How the artifact format matches `docker agent share push`

`AgentArtifact` does not shell out to the CLI. It builds the artifact with [go-containerregistry](https://github.com/google/go-containerregistry), reproducing the wire format that docker-agent v1.150.0 pushes (verified against `registry:2`):

| Part | Value |
|---|---|
| Manifest media type | `application/vnd.oci.image.manifest.v1+json` (`schemaVersion: 2`) |
| `artifactType` | `application/vnd.docker.agent.config.v1+json` |
| Config blob | `application/vnd.docker.container.image.v1+json`, a near-empty image config whose `rootfs.diff_ids[0]` is the sha256 of the YAML |
| Layer (exactly one) | `application/vnd.docker.image.rootfs.diff.tar.gzip`, the **gzip of the raw YAML bytes** (not a tar), written with no name and mtime 0 so identical YAML gives an identical layer digest |
| Required annotations | `io.docker.agent.version` and `io.docker.cagent.version` = the pinned docker-agent version. `docker agent share pull` rejects manifests without them. |
| Other annotations | `org.opencontainers.image.created`, `org.opencontainers.image.description` (`OCI artifact containing agent.yaml`), and from the YAML `metadata` block: `author` → `org.opencontainers.image.authors`, `license` → `org.opencontainers.image.licenses`, `version` → `org.opencontainers.image.revision`, `tags` → `io.docker.agent.tags` (comma-separated). Extra annotations come from the `annotations` input. |

Two deliberate differences from the CLI:

- The CLI stamps `org.opencontainers.image.created` with the current time on every push, so pushing identical YAML twice yields two manifest digests. The provider stamps it on create and update only and keeps it in state, so `pulumi up` with no changes does not re-push and `pulumi refresh` does not flap.
- The CLI also writes a copy into its local store (`~/.cagent/store`). The provider works purely in memory, and `getAgent` writes nothing to disk (the CLI's `share pull` writes `<registry>_<repo>:<tag>.yaml` into the current directory).

`getAgent` accepts layers that are gzip-compressed (as pushed by the CLI) or plain YAML, and like the CLI it rejects manifests without the `io.docker.agent.version`/`io.docker.cagent.version` annotation. Signed (`--key`) and encrypted (`--encrypt`) artifacts are not supported yet.

## Local development

Prerequisites: Go 1.27 (`GOTOOLCHAIN=auto` downloads it), [Pulumi CLI](https://www.pulumi.com/docs/install/) (version in `.pulumi.version`), [pulumictl](https://github.com/pulumi/pulumictl), Node.js 22 and Yarn, Python 3.9+, .NET 8 SDK, Docker, and [golangci-lint](https://golangci-lint.run/) v2.

| Target | What it does |
|---|---|
| `make provider` | Build `bin/pulumi-resource-docker-agent` |
| `make schema` | Regenerate `provider/cmd/pulumi-resource-docker-agent/schema.json` (committed, version stripped) |
| `make codegen` | `schema` plus regenerate every SDK under `sdk/` (committed) |
| `make build_sdks` | Build all SDKs; or one of `build_nodejs`, `build_python`, `build_go`, `build_dotnet`. Accepts `PROVIDER_VERSION=x.y.z` |
| `make test_provider` | Unit and in-process integration tests (`go test ./...` in `provider/`, using an in-memory registry) |
| `make install_plugin` | Install the local binary as the `docker-agent` plugin so `pulumi up` uses it |
| `make lint` | `golangci-lint` with `.golangci.yml` |
| `make test_compat` | Compatibility tests against the real `docker-agent` CLI (needs `docker-agent` on PATH and Docker; starts its own `registry:2`) |
| `make test_examples` | End to end: builds the provider and the nodejs, python and dotnet SDKs, then runs `up`, `preview`, `refresh` and `destroy` for every program in `examples/` against a registry (`DOCKER_AGENT_TEST_REGISTRY`, or a throwaway `registry:2` it starts and removes). `EXAMPLES_RUN=TestExampleGo` selects one language |

The provider binary is about 115 MB (about 40 MB without `AgentRun`): `AgentRun` links docker-agent's runtime with every model provider SDK (Anthropic, OpenAI, Gemini and Vertex AI, Bedrock, Docker Model Runner) and every built-in toolset, so that every configuration the `docker agent` CLI accepts also runs in the provider. A cold `make provider` takes about 20 s on 18 cores (9 s without it) and fills about 1.8 GB of Go build cache. The provider tests and `make test_examples` run agents against a fake OpenAI-compatible server (`provider/pkg/agentrun/fakeopenai`), so they need no model API keys.

CI (`.github/workflows/build.yml`) runs lint, `make provider`, `make test_provider`, `make codegen` (and fails if the committed schema or SDKs are stale), builds every SDK, installs the matching `docker-agent` CLI, and runs `make test_compat` and `make test_examples` against `registry:2`. If you change a resource or function, run `make codegen` and commit `schema.json` and `sdk/`.

### Testing locally against `registry:2`

No Pulumi Cloud account is needed. The automated way runs every example:

```bash
make test_examples
```

It builds the provider and the nodejs, python and dotnet SDKs, starts a throwaway `registry:2` on a free localhost port (with deletes enabled) unless `DOCKER_AGENT_TEST_REGISTRY` points at a running registry, and runs [`examples/examples_test.go`](https://github.com/dirien/pulumi-docker-agent/blob/main/examples/examples_test.go) with [pulumitest](https://github.com/pulumi/providertest) and a `file://` backend in a temp dir. For each language it runs `up`, `preview` and `refresh` (both must report no changes) and `destroy`, checks the outputs and the manifest in the registry, and, when the `docker-agent` CLI is on `PATH`, pulls every artifact with `docker-agent share pull` and `docker-agent debug config`. The registry container is removed afterwards. Use `EXAMPLES_RUN=TestExampleNodejs` to run one language.

To run an example by hand:

```bash
# 1. A registry. Delete support lets the examples use deleteBehavior: delete.
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2

# 2. Build the provider (the examples load it from bin/) and the SDK of your language
make provider build_nodejs

# 3. Run the example with a local backend
cd examples/nodejs
export PULUMI_BACKEND_URL="file://$(mktemp -d)" PULUMI_CONFIG_PASSPHRASE=local
pulumi stack init dev
pulumi install --no-plugins   # dependencies only; the plugin comes from ../../bin
pulumi up --yes
pulumi up --yes               # the second run must report no changes

# 4. Check compatibility with the real CLI
TELEMETRY_ENABLED=false docker agent share pull "$(pulumi stack output pinnedReference)"
TELEMETRY_ENABLED=false docker agent debug config "$(pulumi stack output pinnedReference)"

# 5. Clean up
pulumi destroy --yes && pulumi stack rm dev --yes
docker rm -f agent-registry
```

`pulumi install --no-plugins` matters for local SDK builds: they declare a development plugin version that has no GitHub release, and plain `pulumi install` would try to download it. `pulumi up` uses the binary from `plugins.providers` in `Pulumi.yaml`. Port 5000 is taken on macOS by AirPlay Receiver; map another port (for example `-p 5001:5000`) and run `pulumi config set registry localhost:5001`.

## Maintenance

### Upgrading docker-agent

docker-agent releases every few days, and its configuration schema often changes with a release. The [`upgrade-docker-agent`](https://github.com/dirien/pulumi-docker-agent/actions/workflows/upgrade-docker-agent.yml) workflow checks for a new release daily. When it finds one, it runs `make upgrade_docker_agent` and opens a pull request named `upgrade-docker-agent-to-vX.Y.Z`. The pull request description is a report of the schema changes: config version, added and removed definitions and properties, type and enum changes, and any new key that lands in a free-form value. Every step records its result. If any step fails (`go get`, the generator meeting a schema construct it has no rule for, a compile break in `make codegen`, or the provider tests), the pull request is still opened or updated. It is titled `[needs attention]`, labelled `needs-attention`, and its description shows the step results and the failing step's output. When nothing could be changed (for example `go get` failed), the pull request carries an empty commit. A failure therefore never just turns the scheduled run red. The job that builds the new release has a read-only token and no secrets. A separate job applies the resulting patch, refuses changes outside `provider/`, `sdk/` and `CHANGELOG.md`, pushes with git hooks disabled and opens the pull request. If the push succeeded but opening the pull request failed, the next run pushes the branch again and opens it. Dependabot ignores `github.com/docker/docker-agent`, because a bare `go.mod` bump would skip the regeneration.

To upgrade locally:

```bash
make check_docker_agent_upgrade                          # current=… latest=… upgrade=true|false
make upgrade_docker_agent                                # to the latest release
make upgrade_docker_agent DOCKER_AGENT_VERSION=v1.151.0  # to a specific release
```

`make upgrade_docker_agent` runs these steps:

1. `go get` the release and run `go mod tidy`.
2. Refresh the vendored `agent-schema.json` and the upstream example configurations.
3. Regenerate the typed inputs and the generated constants: `DockerAgentVersion`, the default config version (the highest value of the schema's `version` enum) and the toolset and provider lists.
4. Run `make codegen` and `make test_provider`.
5. Write `bin/upgrade-report.md` (the schema changes, a table of step results and the failing step's output), `bin/upgrade-status` (`ok`, `go-get-failed`, `generate-failed`, `codegen-failed`, `tests-failed` or `script-failed`) and a CHANGELOG entry.

A failing step does not stop the target: later steps are skipped, the status and report are written, and the target exits 0 (`STRICT=1` makes it exit non-zero).

If the generator meets a new union type, inline object or free-form value, it stops and names the schema path. Add a rule for that path in `provider/internal/schemagen/overrides.go`. A plain new property needs no change. CI runs `make check_config_types`, which fails when the generated code does not match the docker-agent version in `provider/go.mod`.

`make test_provider` also guards `UserSettings` against new docker-agent user settings: `TestUserSettingsCoversDockerAgentUserConfig` walks docker-agent's `userconfig.Config` (and the hook, provider and model types it embeds) and fails for every YAML key that `provider/pkg/provider/usersettings_types.go` does not model, and the tests in `provider/pkg/usersettings/upstream_test.go` compare the TUI option values and keybinding tables with the docker-agent source. Add the new key as a typed input (with its `yaml` tag) and run `make codegen`.

## Release process

Releases are cut from `main` by pushing a semver tag. `.github/workflows/release.yml` then:

1. runs GoReleaser (`.goreleaser.yml`) to build `pulumi-resource-docker-agent-v<version>-<os>-<arch>.tar.gz` for `linux`, `darwin` and `windows` on `amd64` and `arm64`, plus a checksums file, a keyless cosign signature bundle (`*_checksums.txt.sigstore.json`) and SBOMs, and publishes them as a GitHub release with a generated changelog;
2. tags the Go SDK module as `sdk/v<version>` on the same commit;
3. builds the npm, PyPI and NuGet SDKs from the committed sources with the version injected, checks that the worktree is still clean, and publishes `@ediri/pulumi-docker-agent`, `ediri_docker_agent` (wheel and sdist) and `ediri.DockerAgent`.

```bash
git checkout main && git pull
# update CHANGELOG.md, commit, then:
git tag v0.1.0
git push origin v0.1.0
```

Tags with a pre-release suffix (`v0.2.0-rc.1`) become GitHub pre-releases and are published to npm under the `dev` dist-tag. The Pulumi Registry ignores pre-releases. Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) so the changelog groups `feat:` and `fix:` changes.

Repository secrets used by the release workflow: `NPM_TOKEN`, `PYPI_PASSWORD` (a PyPI API token; the user is `__token__`) and `NUGET_PUBLISH_KEY`. `GITHUB_TOKEN` is provided by Actions.

To build the release archives locally without publishing: `goreleaser release --snapshot --clean --skip=publish,sign,sbom`.

## Publishing to the Pulumi Registry

The package is listed in the [Pulumi Registry](https://www.pulumi.com/registry/) as a community package. Checklist for the first listing:

- [ ] The repository `github.com/dirien/pulumi-docker-agent` is public, with `LICENSE` (Apache-2.0) and this README.
- [ ] `provider/cmd/pulumi-resource-docker-agent/schema.json` is committed **without** a `version` field (`make schema` strips it), has `displayName: Docker Agent` (equal to the `title` in `docs/_index.md`), `publisher: dirien`, a `logoUrl`, `pluginDownloadURL: github://api.github.com/dirien/pulumi-docker-agent`, and `category/` and `kind/native` keywords.
- [ ] `docs/_index.md` has front matter, no H1, an Installation chooser with all seven language tabs, Example Usage, and Configuration. Links are absolute.
- [ ] Tag `v0.1.0` and push it. Wait for the `release` workflow to finish.
- [ ] Check the [GitHub release](https://github.com/dirien/pulumi-docker-agent/releases/tag/v0.1.0): it is **not** a pre-release and has the six assets `pulumi-resource-docker-agent-v0.1.0-{linux,darwin,windows}-{amd64,arm64}.tar.gz`, plus the checksums file. Check that the `sdk/v0.1.0` tag exists.
- [ ] Self-test the probes the Registry runs:

  ```bash
  pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent   # blocking
  npm view @ediri/pulumi-docker-agent@0.1.0 version
  pip download ediri_docker_agent==0.1.0 --only-binary :all: --no-deps -d /tmp/pypi-check
  go list -m github.com/dirien/pulumi-docker-agent/sdk@v0.1.0
  ```

- [ ] Open a pull request against [pulumi/registry](https://github.com/pulumi/registry) that changes **only** `community-packages/package-list.json`, adding this entry to the `include` array (`dirien` is already in `publisher-names.json`):

  ```json
  {
    "repoSlug": "dirien/pulumi-docker-agent",
    "schemaFile": "provider/cmd/pulumi-resource-docker-agent/schema.json"
  }
  ```

  Title: `Add dirien/pulumi-docker-agent community package`. Include the line `maintainer (@dirien)` in the body. If an automated check is red, fix it and comment `/check`. A maintainer runs `/preview` and merges.
- [ ] After the merge, new GitHub releases are picked up automatically (twice a day). `v0.x` versions are listed as public preview, `v1.0.0` and later as GA.

## Contributing

See [CONTRIBUTING.md](https://github.com/dirien/pulumi-docker-agent/blob/main/CONTRIBUTING.md) and the [Code of Conduct](https://github.com/dirien/pulumi-docker-agent/blob/main/CODE-OF-CONDUCT.md).

## License

[Apache License 2.0](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE). Docker and Docker Agent are trademarks of Docker, Inc. This project is not affiliated with or endorsed by Docker, Inc.
