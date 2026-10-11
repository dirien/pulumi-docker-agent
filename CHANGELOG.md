# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). GitHub release notes are generated from commit messages; this file summarises user-facing changes.

## [Unreleased]

## [0.1.0] - TBD

_The release date is set when the `v0.1.0` tag is pushed._

Initial release of the native Pulumi provider for [Docker Agent](https://docs.docker.com/ai/docker-agent/), built with `pulumi-go-provider` and pinned to `github.com/docker/docker-agent` v1.150.0 (configuration version `"16"`).

### Added

#### Agent configurations and sharing

- `AgentConfig` resource: every key of the docker-agent configuration (version `"16"`) is a typed input, generated from docker-agent's `agent-schema.json` with its descriptions: ordered agents (sub-agents, handoffs, routing, fallback models, harness, cache, hooks, commands, skills, structured output, ...), models (routing, auth, costs, capabilities, thinking and task budgets), custom providers, toolsets of all 30 types, reusable `mcps`, `rag`, `toolsets`, `commands` and `skills` definitions, permissions, sandbox mode (`runtime.sandbox`, `runtime.networkAllowlist`, `runtime.safety`), `budget`/`budgets`, evaluators, flavors and metadata. Each object keeps an `extra` escape hatch for keys of newer docker-agent releases; at the top level, `extra` cannot override a typed section (Check fails instead of silently replacing, for example, `agents`). YAML shorthands map to typed inputs: `simpleCommands` for bare-string commands, `inlineSkills` next to `skills`, `thinkingBudgetTokens` next to `thinkingBudget` and `deferTools` next to `defer`. Renders deterministic YAML and outputs `yaml`, `sha256`, `defaultAgent`, `agentNames`, `requiredEnvVars` and `dockerAgentVersion`. Configurations are validated during preview with the docker-agent loader, including the 30 known toolset types. Tests check that every property of the schema has a typed input, and that all upstream example configurations and an example from every page of the configuration docs can be expressed with typed inputs.
- `AgentArtifact` resource: pushes an agent configuration to an OCI registry with the same wire format as `docker agent share push` (`artifactType` `application/vnd.docker.agent.config.v1+json`, a single gzip-of-YAML layer, `io.docker.agent.version`/`io.docker.cagent.version` annotations, and metadata annotations). Stable `created` timestamp kept in state, drift detection on refresh, `deleteBehavior` `retain` (default) or `delete`, and support for plain-HTTP registries.
- `getAgent` function: reads a shared agent by tag or digest, returns its YAML, digest, annotations, metadata and agent names, and writes nothing to disk. Reads artifacts pushed by the `docker agent` CLI.
- `validateAgentConfig` function: validates arbitrary docker-agent YAML and reports errors instead of failing.
- Inputs that are unknown during a preview (because they depend on another resource's output) stay unknown for every resource. Previously a computed `agents` list of `AgentConfig` failed the preview with "Missing required field 'agents'".

#### Running agents

- `AgentRun` resource: runs a prompt against an agent configuration (`yaml`, or an OCI `reference` pulled with the provider's registry credentials) during `pulumi up`, in process with the docker-agent Go SDK, and outputs the final answer (`output`), `structuredOutput` (for agents with structured output), `agentName`, `usage` (tokens and estimated cost), `toolCalls` and `ranAt`. It never runs during previews; any input change, including `triggers`, runs it again, and refresh and delete do nothing. Model credentials come from the secret `env` input and, optionally, the environment (`inheritEnvironment`); a secret `env` does not make the answer secret (`outputSecret` does). `toolApproval: deny` (default) only runs tool calls docker-agent approves without asking (read-only tools, `permissions` allow rules, the configuration's own safety mode) and tells the model about the rejected ones; `allow` runs every tool call like `--yolo`. Each run uses private, temporary docker-agent data, config and cache directories and a temporary working directory (or `workingDir`), ignores the user's docker-agent settings, keeps docker-agent's logs (which can quote API errors) off the provider's stderr, and redacts secret inputs from errors. Runs in one provider process execute one at a time, bounded by `timeoutSeconds`. Coding-harness agents are not supported. The provider binary grows from about 40 MB to about 115 MB because it links docker-agent's runtime with every model provider and toolset.
- `AgentRun` under `toolApproval: deny` ignores approvals from `pre_tool_use` and `permission_request` hooks; the `allowHookApprovals` input accepts them.
- `AgentRun` with `inheritEnvironment: false` gives command hooks, `shell`/`script`/`background_jobs` tools and MCP/LSP servers started with `command` only system variables plus their own `env`. `docker:` MCP servers, filesystem `post_edit` commands, the `open_url` launcher and on-demand tool installation still inherit the provider environment (documented).
- `AgentRun` errors, warnings and logs also redact credential-named environment variables and `token_key` values; answers that contain one of these values are stored as secrets.
- `AgentRun` previews show a rerun when an input becomes unknown; changes to `timeoutSeconds` or `outputSecret` alone update the state without running the agent again; import fails with a clear error.
- `examples/agent-run-yaml`, tested end to end against a fake OpenAI-compatible server.

#### Local configuration files

- `UserSettings` resource: Docker Agent's user configuration file (`~/.config/cagent/config.yaml`, see [User Settings](https://docs.docker.com/ai/docker-agent/configuration/user-settings/)) with typed inputs for every key of docker-agent's user configuration: the `settings` block (TUI options, `safety`, `permissions`, `hooks`, `keybindings`, `layout`, ...), `aliases`, global `providers`, `defaultModel`, `modelsGateway`, `board`, `credentialHelper` and `sandboxAllowlist`, plus `extra` escape hatches. It renders the file exactly as docker-agent writes it and validates it with docker-agent's own checks. With `path`, it writes the file atomically (mode 0600), reports changes made outside of Pulumi as drift on refresh and removes it on delete; without `path` it only renders `yaml`. A test fails when a docker-agent upgrade adds a user-configuration key that is not modelled.
- `AgentsIgnore` resource: a [`.agentsignore`](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/) file from ordered `.gitignore`-style `patterns` and an optional `comment`, validated with go-git's gitignore rules (the library docker-agent uses), written to `directory` with the same create, drift and delete behaviour.
- `UserSettings` and `AgentsIgnore` never replace an existing file silently: the preview fails unless `overwrite: true` is set. A file the resource takes over is kept as `<file>.pulumi-backup` (output `backupPath`) and restored on delete.
- `UserSettings` and `AgentsIgnore` keep symbolic links (for example from dotfile managers): the link target is written, refreshed, backed up and restored, and the link itself is never replaced or deleted.
- A managed file deleted outside of Pulumi is reported as drift on refresh and written again by the next `pulumi up`.
- `examples/user-settings-yaml`, tested end to end with the other examples.

#### Provider configuration, SDKs and languages

- Provider configuration: `registryAuth` (per-registry credentials, with the Docker credential chain as fallback), `skipValidation` (also `DOCKER_AGENT_SKIP_VALIDATION`) and `forceHttps` (also `DOCKER_AGENT_FORCE_HTTPS`): refuse the plain-HTTP fallback for private (RFC 1918) registry addresses, which would send credentials unencrypted, so that only localhost and loopback registries may use plain HTTP unless a resource sets `insecure: true`.
- SDKs for TypeScript/JavaScript (`@ediri/pulumi-docker-agent`), Python (`ediri_docker_agent`), Go (`github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent`) and .NET (`ediri.DockerAgent`).
- Pulumi HCL support (`runtime: hcl`): the provider works in Terraform-style `.tf` programs through `required_providers { docker-agent = { source = "pulumi/docker-agent", version = "0.1.0" } }`, with the resources `docker-agent_agent_config`, `docker-agent_agent_artifact`, `docker-agent_agent_run`, `docker-agent_user_settings` and `docker-agent_agents_ignore`, and the data sources `docker-agent_agent` (`getAgent`) and `docker-agent_validate_agent_config` (`validateAgentConfig`). The Registry docs have HCL tabs, and `examples/hcl` is tested end to end with the other examples.

#### Maintenance and release

- Typed inputs generated from the pinned `agent-schema.json` (`make generate_config_types`). Tests check that every property of the schema has a typed input, and that all upstream example configurations and an example from every page of the configuration docs can be expressed with typed inputs. CI (`make check_config_types`) fails when the generated types do not match `provider/go.mod`.
- `make upgrade_docker_agent` and the daily `upgrade-docker-agent` workflow: they bump the pinned docker-agent, regenerate the typed inputs, schema and SDKs from the new `agent-schema.json`, run the tests and open a pull request with a report of the configuration schema changes. Every step records its result: when any step fails (`go get`, the generator, `make codegen` or the tests), the pull request is still opened or updated, labelled `needs-attention`, with the failing step and its output. The job that builds the new release has no write access or secrets; a separate job applies the patch (only `provider/`, `sdk/` and `CHANGELOG.md`), pushes with git hooks disabled and opens the pull request, also on the next run if opening it failed. Dependabot no longer bumps docker-agent.
- Plugin binaries for Linux, macOS and Windows on amd64 and arm64, with checksums, a cosign signature bundle and SBOMs.

[Unreleased]: https://github.com/dirien/pulumi-docker-agent/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dirien/pulumi-docker-agent/releases/tag/v0.1.0
