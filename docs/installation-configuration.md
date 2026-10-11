---
title: Docker Agent Installation & Configuration
meta_desc: Information on how to install and configure the Pulumi Docker Agent provider, including registry authentication and runtime credentials.
layout: package
---

## Installation

The Pulumi Docker Agent provider is available as a package in these Pulumi languages:

* JavaScript/TypeScript: [`@ediri/pulumi-docker-agent`](https://www.npmjs.com/package/@ediri/pulumi-docker-agent)
* Python: [`ediri_docker_agent`](https://pypi.org/project/ediri_docker_agent/)
* Go: [`github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent`](https://pkg.go.dev/github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent)
* .NET: [`ediri.DockerAgent`](https://www.nuget.org/packages/ediri.DockerAgent)
* YAML: no SDK needed, use the `docker-agent:index:<Type>` tokens directly
* HCL: no SDK needed, declare `pulumi/docker-agent` in `required_providers` (see [HCL](#hcl))

### Node.js (JavaScript/TypeScript)

```bash
npm install @ediri/pulumi-docker-agent
```

or

```bash
yarn add @ediri/pulumi-docker-agent
```

### Python

```bash
pip install ediri_docker_agent
```

### Go

```bash
go get github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent
```

### .NET

```bash
dotnet add package ediri.DockerAgent
```

### HCL

[Pulumi HCL](https://www.pulumi.com/docs/iac/languages-sdks/hcl/) programs (`runtime: hcl` in `Pulumi.yaml`, Terraform-style `.tf` files) use the provider without an SDK. Declare it in `required_providers`. The `pulumi/` prefix selects the native Pulumi provider (any other source is bridged from a Terraform provider), the version must be an exact version rather than a constraint, and the declaration is required: without it, HCL looks for a Terraform provider called `docker-agent`.

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}
```

`pulumi/dirien/docker-agent` (the form `pulumi convert --language hcl` writes, with the package's namespace) works as well.

The `source` names the package but not where its plugin is published, so install the plugin from the GitHub release once, then run `pulumi install`:

```bash
pulumi plugin install resource docker-agent 0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi install
```

So that teammates and CI only need `pulumi install`, run `pulumi package add docker-agent@0.1.0` once after installing the plugin. It writes `sdks/dirien-docker-agent/hcl.sdk.json`, a package descriptor that records the version and the GitHub download URL; commit it, as the Pulumi HCL documentation recommends for everything under `sdks/`. `pulumi install` then downloads the plugin from that URL. A `plugin_download_url` in a `provider` block's `pulumi { }` block does not help here: it is not used when plugins are installed.

Resource and function names follow the Terraform convention, `<package>_<name in snake_case>`, and functions drop their `get` prefix:

| Pulumi token | HCL |
|---|---|
| `docker-agent:index:AgentConfig` | `resource "docker-agent_agent_config" "<name>"` |
| `docker-agent:index:AgentArtifact` | `resource "docker-agent_agent_artifact" "<name>"` |
| `docker-agent:index:getAgent` | `data "docker-agent_agent" "<name>"` |
| `docker-agent:index:validateAgentConfig` | `data "docker-agent_validate_agent_config" "<name>"` |
| provider configuration | `provider "docker-agent" { ... }` |

Property names are snake_case (`subAgents` is `sub_agents`, `tokenKey` is `token_key`, `pinnedReference` is `pinned_reference`). Lists of objects are written as repeated nested blocks: one `agents { }` block per agent, in order, `toolsets { }` blocks inside an agent, and `registry_auth { }` blocks in the provider block. Maps and single objects such as `models`, `metadata`, `permissions` and `extra` are attributes (`models = { ... }`). The hyphen is part of the HCL identifier, so references read `docker-agent_agent_config.assistant.yaml` and `data.docker-agent_agent.pulled.agent_names`:

```hcl
resource "docker-agent_agent_config" "assistant" {
  models = {
    fast = {
      provider   = "openai"
      model      = "gpt-5-mini"
      max_tokens = 16000
    }
  }

  agents {
    name       = "root"
    model      = "fast"
    sub_agents = ["researcher"]
  }

  agents {
    name  = "researcher"
    model = "fast"

    toolsets {
      type = "mcp"
      ref  = "docker:duckduckgo"
    }
  }
}

resource "docker-agent_agent_artifact" "assistant" {
  reference       = "ghcr.io/my-org/agents/assistant:v1"
  yaml            = docker-agent_agent_config.assistant.yaml
  delete_behavior = "delete"
}

data "docker-agent_agent" "assistant" {
  reference = docker-agent_agent_artifact.assistant.pinned_reference
}

data "docker-agent_validate_agent_config" "draft" {
  yaml = file("${path.module}/agent.yaml")
}
```

Configure the provider with a `provider` block, for example with registry credentials from a sensitive variable:

```hcl
variable "ghcr_token" {
  type      = string
  sensitive = true
}

provider "docker-agent" {
  force_https = true

  registry_auth {
    address  = "ghcr.io"
    username = "my-github-user"
    password = var.ghcr_token
  }
}
```

Declare the `provider` block from the start: adding it to a stack that already uses the default provider moves the resources to the new provider, which replaces them. A runnable HCL program is in [`examples/hcl`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/hcl).

### Provider plugin

The SDKs download the provider plugin from the [GitHub releases](https://github.com/dirien/pulumi-docker-agent/releases) automatically. To install a specific version by hand, for example in an air-gapped CI image, run:

```bash
pulumi plugin install resource docker-agent <version> --server github://api.github.com/dirien/pulumi-docker-agent
```

For example, `pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent`. Release archives are published for Linux, macOS and Windows on `amd64` and `arm64`.

## Configuration

The provider is configured through the `docker-agent` configuration namespace. Every option is optional: with no configuration the provider pushes to and pulls from registries with your Docker credentials.

- `registryAuth` (Optional) — A list of registry credentials used to push, pull and delete agent artifacts. Each entry has:
  - `address` (Required) — The registry host the credentials apply to, for example `ghcr.io`, `docker.io`, `registry.example.com:5000` or `localhost:5000`.
  - `username` (Optional) — The user name.
  - `password` (Optional, Secret) — The password or access token. Always set it with `pulumi config set --secret`.

  Registries without a matching entry fall back to the Docker credentials described below.
- `forceHttps` (Optional) — Refuse plain-HTTP connections to registries other than `localhost` and loopback addresses, including private (RFC 1918) addresses that would otherwise fall back to plain HTTP. Defaults to `false`. May also be set with the `DOCKER_AGENT_FORCE_HTTPS` environment variable. See [Plain HTTP and self-signed registries](#plain-http-and-self-signed-registries).
- `skipValidation` (Optional) — Skip validating agent configurations against the docker-agent library bundled with the provider, for every `AgentConfig`, `AgentArtifact` and `UserSettings`. Use it when your configurations use fields that are newer than the docker-agent version the provider pins (see Compatibility below). Defaults to `false`. May also be set with the `DOCKER_AGENT_SKIP_VALIDATION` environment variable (`true`/`false`). Each resource also has its own `skipValidation` input.

### Setting registry credentials

With the CLI:

```bash
pulumi config set --path 'docker-agent:registryAuth[0].address' ghcr.io
pulumi config set --path 'docker-agent:registryAuth[0].username' <github-user>
pulumi config set --secret --path 'docker-agent:registryAuth[0].password' <github-token>
```

which results in this stack configuration:

```yaml
config:
  docker-agent:registryAuth:
    - address: ghcr.io
      username: <github-user>
      password:
        secure: <encrypted>
```

You can also pass credentials to an explicit provider instance, for example to read them from another resource or from [Pulumi ESC](https://www.pulumi.com/docs/esc/):

```typescript
import * as fs from "fs";
import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

const ghcr = new dockeragent.Provider("ghcr", {
    registryAuth: [{
        address: "ghcr.io",
        username: config.require("ghcrUser"),
        password: config.requireSecret("ghcrToken"),
    }],
});

// Publish an existing docker-agent YAML file as is.
const artifact = new dockeragent.AgentArtifact("assistant", {
    reference: "ghcr.io/my-org/agents/assistant:v1",
    yaml: fs.readFileSync("agent.yaml", "utf8"),
}, { provider: ghcr });
```

### Docker credentials fallback

When no `registryAuth` entry matches a registry, the provider uses the standard Docker credential chain, the same one `docker agent share push` uses:

1. `$DOCKER_CONFIG/config.json` when the `DOCKER_CONFIG` environment variable is set, otherwise `~/.docker/config.json`.
2. The `auths` entries in that file, and the credential helpers it configures through `credsStore` and `credHelpers` (for example `osxkeychain`, `desktop`, `pass`, `ecr-login`).

So `docker login ghcr.io` before `pulumi up` is enough on a workstation. In CI, log in with the registry's login action or point `DOCKER_CONFIG` at a directory that contains a `config.json`. `DOCKER_AUTH_CONFIG` is not read.

When `registryAuth` has an entry for a registry, the provider sends those credentials from the first request. Otherwise `getAgent` (and a refresh of an `AgentArtifact`) first tries anonymous access, like the `docker agent` CLI, and retries with your Docker credentials when the registry answers `401`, `403`, `404` or `429`, so public agents can be read without any configuration.

### Plain HTTP and self-signed registries

The provider uses go-containerregistry, the same registry client as the `docker agent` CLI. It tries HTTPS first and falls back to plain HTTP when HTTPS fails for these hosts:

- `localhost` and `*.localhost`, such as `registry:2` started with `docker run -d -p 5000:5000 registry:2`,
- loopback addresses (`127.0.0.1`, `::1`),
- private (RFC 1918) addresses: `10.0.0.0/8`, `172.16.0.0/12` and `192.168.0.0/16`, for example `192.168.1.10:5000`.

Over plain HTTP, registry credentials are sent unencrypted. Set the provider option `forceHttps: true` (or `DOCKER_AGENT_FORCE_HTTPS=true`) to refuse plain HTTP for everything except `localhost` and loopback addresses.

For any other registry that serves plain HTTP or uses a self-signed TLS certificate, and to allow plain HTTP for a private address while `forceHttps` is on, set `insecure: true` on the `AgentArtifact` resource or the `getAgent` call.

### Deleting artifacts

By default (`deleteBehavior: retain`), destroying an `AgentArtifact` leaves the artifact in the registry, because many registries do not allow deleting manifests. With `deleteBehavior: delete` the provider deletes the manifest by digest. If the registry refuses (for example `registry:2` without `REGISTRY_STORAGE_DELETE_ENABLED=true` returns `405 UNSUPPORTED`), the provider logs a warning instead of failing.

Deleting a manifest by digest removes every tag that points to it. Two `AgentArtifact` resources that push the same YAML and annotations to different tags of one repository in the same second (for example `:v1` and `:latest`) produce the same digest. Before deleting, the provider therefore lists the repository's tags and keeps the manifest, with a warning, when another tag still points to it or when the tags cannot be listed.

A refresh detects when someone else moved the tag. The state then records the other manifest, so the next `pulumi up` pushes the configured agent again; if the tag now holds something that is not a docker-agent artifact (for example a container image), the refresh logs a warning and the next update pushes the agent again as well. Destroying the resource after such a refresh (with `deleteBehavior: delete`) deletes the manifest the tag points to at that time.

## Local files

`UserSettings` (Docker Agent's `~/.config/cagent/config.yaml`) and `AgentsIgnore` (a project's `.agentsignore`) write files on the machine that runs the Pulumi engine: your workstation for a local `pulumi up`, the CI runner or the Pulumi Deployments worker otherwise. `~` in `path` or `directory` is the home directory of the user running Pulumi, and relative paths are relative to the Pulumi project directory. docker-agent reads its user configuration from `~/.config/cagent/config.yaml` unless `--config-dir` points elsewhere.

- Previews never write. `pulumi up` writes the file atomically (a temporary file in the same directory, renamed into place) and creates missing directories: `config.yaml` gets mode `0600` in a `0700` directory, like docker-agent writes it, and `.agentsignore` gets `0644`.
- An existing file is never replaced silently. When the resource is created (or `path`/`directory` changes) and a file with different content is already there, `pulumi preview` fails and names the file; `pulumi up` refuses as well. Copy what you want to keep (for example the aliases `docker agent alias add` wrote) into the inputs, move the file away, or set `overwrite: true`. A file with identical content needs no `overwrite`.
- In both cases the existing file is first moved to `<file>.pulumi-backup` in the same directory (output `backupPath`). `pulumi destroy`, removing `path`/`directory` or moving the resource to another path moves the backup back, with its original content and mode. A file the provider created itself is deleted instead. An existing `.pulumi-backup` is never replaced: the preview fails until you move it away. If you delete the backup, destroy deletes the file and logs a warning.
- The resource owns the whole file. `pulumi refresh` reports content changed outside of Pulumi as drift, including changes docker-agent makes itself (`docker agent alias add`, the TUI's `/settings` dialog, `docker agent sandbox allow`), and the next `pulumi up` writes the declared content again. A deleted file shows up as drift too and is written again.
- Symbolic links are followed and kept. If `~/.config/cagent/config.yaml` links into a dotfiles repository, the provider writes, refreshes, backs up (`<file in the repository>.pulumi-backup`) and restores the file the link points to. A link that points to a missing file makes the provider create that file, and delete removes only that file. The link itself is never replaced or deleted.
- Changing `path` or `directory` moves the file: the new path is taken over as on create, then the old one is restored or deleted as on destroy.
- `pulumi destroy` never deletes a directory. To keep the managed file (and the backup), set the `retainOnDelete` resource option.
- Without `path` (or `directory`), nothing is written and the resource only renders the content (`yaml` or `content` output), for example to copy into a container image or a Docker Sandboxes kit.
- Importing existing files is not supported; declare the resource and run `pulumi up`.

`UserSettings` renders the file with docker-agent's own types, so the result is byte-for-byte what docker-agent writes for the same settings. Its `extra` inputs (top level and `settings`) take keys that a newer docker-agent added; `skipValidation` turns off docker-agent's semantic checks (for example a safety mode or TUI option it does not know yet).

## Runtime credentials for agents

Rendering, validating and publishing configurations needs no model API keys, and validation runs offline. Model credentials are read where the agent runs: from the environment of whoever runs it with `docker agent run`, `docker agent serve api` or a container based on `docker/docker-agent`, or, for `AgentRun`, by the provider during `pulumi up` (see [AgentRun credentials](#agentrun-credentials)).

The `requiredEnvVars` output of `AgentConfig` and `validateAgentConfig` lists the variables a configuration needs, on a best-effort basis. The default variables per provider are:

| Model provider | Environment variables |
|---|---|
| `openai` | `OPENAI_API_KEY` |
| `anthropic` | `ANTHROPIC_API_KEY` |
| `google` | `GOOGLE_API_KEY` or `GEMINI_API_KEY` (or Vertex AI with `GOOGLE_GENAI_USE_VERTEXAI`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`) |
| `amazon-bedrock` | `AWS_BEARER_TOKEN_BEDROCK`, or standard AWS credentials (`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, `AWS_PROFILE`, `AWS_ROLE_ARN`) plus `AWS_REGION` |
| `dmr` (Docker Model Runner) | none |
| `mistral` | `MISTRAL_API_KEY` |
| `openrouter` | `OPENROUTER_API_KEY` |
| `github-copilot` | `GITHUB_TOKEN` or `GH_TOKEN` |
| `xai` | `XAI_API_KEY` |
| `groq` | `GROQ_API_KEY` |
| `deepseek` | `DEEPSEEK_API_KEY` |
| `huggingface` | `HF_TOKEN` |

Other OpenAI-compatible aliases follow the same pattern (for example `NVIDIA_API_KEY`, `TOGETHER_API_KEY`, `CEREBRAS_API_KEY`, `FIREWORKS_API_KEY`). The [Docker Agent model provider documentation](https://docs.docker.com/ai/docker-agent/) has the full list.

Never put a secret into an agent configuration: it ends up in the published artifact. Instead:

- Set `tokenKey` on a model or custom provider to the **name** of the environment variable that holds the key, for example `tokenKey: MY_OPENAI_KEY`.
- Use `${env.VAR}` placeholders in toolset `env` and `headers` values. docker-agent expands them when the agent runs.

`docker agent doctor <reference>` checks that every required variable is set before you run an agent.

### AgentRun credentials

`AgentRun` runs the agent inside the provider process on the machine that runs `pulumi up`, so it needs the same variables. It resolves them, and every `${env.NAME}` reference of the configuration, from:

1. its `env` input, a map of variable names to values that is always stored as a secret. Set the values from secret config, for example `env: { OPENAI_API_KEY: config.requireSecret("openaiApiKey") }`;
2. unless `inheritEnvironment` is `false`, the environment of the Pulumi CLI, for example an `OPENAI_API_KEY` exported in the shell or set as a CI secret.

docker-agent's other credential sources are never used: its `~/.config/cagent/.env` file, the credential helper of the user settings, Docker Desktop secrets and `op://` 1Password references. With `inheritEnvironment: false`, a run depends on nothing but its inputs. `OPENAI_BASE_URL` (and the other base-URL variables the model SDKs read) also comes from the environment of the Pulumi CLI; prefer a custom provider with `baseUrl` in the configuration.

The processes a configuration starts (command hooks, `shell`, `script` and `background_jobs` tools, MCP and LSP servers) inherit the environment of the Pulumi CLI by default, and command hooks also receive `env`. With `inheritEnvironment: false`, AgentRun starts them with the system variables only (`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `TMPDIR`, `TZ`, ...) plus their toolset's own `env`, and command hooks with the system variables plus `env`. A toolset passes a credential to its process explicitly, for example `env: {GITHUB_TOKEN: "${env.GITHUB_TOKEN}"}`. `docker:` MCP servers (the Docker MCP Gateway), filesystem `post_edit` commands, the `open_url` launcher and docker-agent's on-demand tool installation always inherit the whole environment. The [AgentRun documentation](https://www.pulumi.com/registry/packages/docker-agent/#where-tools-run) has the details.

An invalid key fails the update with the model's error message. The provider redacts the values of `env`, of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...) and of the variables the configuration names as `token_key` from errors, warnings and logs. When the agent's answer contains one of these values, its `output` and `structuredOutput` are stored as secrets.

## Compatibility

Each provider release pins one docker-agent version (the `dockerAgentVersion` output of `AgentConfig` and `AgentArtifact`), validates configurations with that version's loader, and writes it into the `io.docker.agent.version` and `io.docker.cagent.version` manifest annotations that `docker agent share pull` requires.

docker-agent releases often and adds fields to its latest configuration version (`"16"`). If you use a field that the pinned version does not know yet, validation reports `unknown field`. You can:

- upgrade the provider, or
- set `skipValidation: true` on the resource, or `docker-agent:skipValidation` (or `DOCKER_AGENT_SKIP_VALIDATION=true`) for the whole stack, and
- use the `extra` input that every `AgentConfig` object has (the configuration, agents, models, providers, toolsets, hooks and so on) to add fields that the pinned version does not model yet. Keys in `extra` are written to the YAML as is (snake_case). Every field of the pinned version is a typed input.

`instructionFile` (`instruction_file`) only works with `skipValidation`, because configurations are validated in memory, where relative files cannot be read. Read the file in your program and pass its contents as `instruction`, which is also what `docker agent share push` does when it publishes an agent.
