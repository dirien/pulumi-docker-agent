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
* Java: [`io.github.dirien:docker-agent`](https://central.sonatype.com/artifact/io.github.dirien/docker-agent) (see [Java](#java))
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

### Java

The Java SDK is published to Maven Central as `io.github.dirien:docker-agent`; its Java package is `io.github.dirien.dockeragent`. With Gradle (`build.gradle`):

```groovy
dependencies {
    implementation "io.github.dirien:docker-agent:0.1.0"
}
```

With Maven (`pom.xml`):

```xml
<dependency>
    <groupId>io.github.dirien</groupId>
    <artifactId>docker-agent</artifactId>
    <version>0.1.0</version>
</dependency>
```

The SDK needs Java 11 or later and depends on `com.pulumi:pulumi` 1.x; a Pulumi Java project (`pulumi new java-gradle` or `java`) already declares it.

Java publishing starts with the first release that has the Maven Central credentials set up, so a release may exist without a Java package. If `io.github.dirien:docker-agent` is not on [Maven Central](https://central.sonatype.com/artifact/io.github.dirien/docker-agent) yet, install the plugin and generate the SDK locally from your project directory instead. `pulumi package add` records the package in `Pulumi.yaml` and prints how to use the generated SDK:

```bash
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi package add docker-agent@0.1.0
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
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi install
```

So that teammates and CI only need `pulumi install`, run `pulumi package add docker-agent@0.1.0` once after installing the plugin. It writes `sdks/dirien-docker-agent/hcl.sdk.json`, a package descriptor that records the version and the GitHub download URL; commit it, as the Pulumi HCL documentation recommends for everything under `sdks/`. `pulumi install` then downloads the plugin from that URL. A `plugin_download_url` in a `provider` block's `pulumi { }` block does not help here: it is not used when plugins are installed.

Resource and function names follow the Terraform convention, `<package>_<name in snake_case>`, and functions drop their `get` prefix:

| Pulumi token | HCL |
|---|---|
| `docker-agent:index:AgentConfig` | `resource "docker-agent_agent_config" "<name>"` |
| `docker-agent:index:AgentArtifact` | `resource "docker-agent_agent_artifact" "<name>"` |
| `docker-agent:index:AgentRun` | `resource "docker-agent_agent_run" "<name>"` |
| `docker-agent:index:UserSettings` | `resource "docker-agent_user_settings" "<name>"` |
| `docker-agent:index:AgentsIgnore` | `resource "docker-agent_agents_ignore" "<name>"` |
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

Adding the `provider` block to a stack that already uses the default provider moves the resources to the new provider. That updates nothing but the provider: the resources are kept and no `AgentRun` runs again (before v0.1.2, the move replaced them). A runnable HCL program is in [`examples/hcl`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/hcl).

### Provider plugin

The SDKs download the provider plugin from the [GitHub releases](https://github.com/dirien/pulumi-docker-agent/releases) automatically. To install a specific version by hand, for example in an air-gapped CI image, run:

```bash
pulumi plugin install resource docker-agent <version> --server github://api.github.com/dirien/pulumi-docker-agent
```

For example, `pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent`. Release archives are published for Linux, macOS and Windows on `amd64` and `arm64`. The converter plugin for `pulumi convert --from docker-agent` is a separate download from the same release; see [Converting existing agent files](#converting-existing-agent-files).

## Configuration

The provider is configured through the `docker-agent` configuration namespace. Every option is optional: with no configuration the provider pushes to and pulls from registries with your Docker credentials.

- `registryAuth` (Optional) — A list of registry credentials used to push, pull and delete agent artifacts. Each entry has:
  - `address` (Required) — The registry host the credentials apply to, for example `ghcr.io`, `docker.io`, `registry.example.com:5000` or `localhost:5000`.
  - `username` (Optional) — The user name.
  - `password` (Optional, Secret) — The password or access token. Always set it with `pulumi config set --secret`.

  Registries without a matching entry fall back to the Docker credentials described below.
- `forceHttps` (Optional) — Refuse plain-HTTP connections to registries other than `localhost` and loopback addresses, including private (RFC 1918) addresses that would otherwise fall back to plain HTTP. Defaults to `false`. May also be set with the `DOCKER_AGENT_FORCE_HTTPS` environment variable. See [Plain HTTP and self-signed registries](#plain-http-and-self-signed-registries).
- `skipValidation` (Optional) — Skip validating agent configurations against the docker-agent library bundled with the provider, for every `AgentConfig`, `AgentArtifact` and `UserSettings`. Use it when your configurations use fields that are newer than the docker-agent version the provider pins (see Compatibility below). Defaults to `false`. May also be set with the `DOCKER_AGENT_SKIP_VALIDATION` environment variable (`true`/`false`). Each resource also has its own `skipValidation` input. `AgentRun`'s own rules for coding-harness agents still apply to a `yaml` configuration that the bundled docker-agent can load.

Changing a setting, also only through `DOCKER_AGENT_FORCE_HTTPS` or `DOCKER_AGENT_SKIP_VALIDATION` in the environment of one command, updates the provider in place: Pulumi configures it again with the new settings, for example a rotated registry password, and no resource is replaced or runs again. Before v0.1.2, every change of a setting replaced every resource of the provider.

`DOCKER_AGENT_SKIP_DELETE_PROMPTS` is an environment variable without a setting: `true` in the environment of a `pulumi` command skips the `deletePrompt` of every `AgentRun` that the command deletes, with a warning per resource. A setting could not do this, because `pulumi destroy` reads the provider settings from the state, so a stack whose delete prompt fails could not be rescued with it. [Reruns and delete prompts](https://www.pulumi.com/registry/packages/docker-agent/#reruns-and-delete-prompts) on the overview page has the details.

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

You can also pass credentials to an explicit provider instance, for example to read them from another resource or from [Pulumi ESC](#using-pulumi-esc):

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

### Using Pulumi ESC

A [Pulumi ESC](https://www.pulumi.com/docs/esc/) environment keeps the registry credentials and the model API keys in one place for every stack that imports it, and for `docker agent run`. Importing an environment needs the Pulumi Cloud backend.

This environment, for example `<org>/docker-agent/dev`, provides registry credentials for `ghcr.io`, an Anthropic API key and short-lived AWS credentials for Amazon Bedrock:

```yaml
values:
  aws:
    login:
      fn::open::aws-login:
        oidc:
          roleArn: arn:aws:iam::123456789012:role/pulumi-esc-bedrock
          sessionName: pulumi-esc-docker-agent
          duration: 1h
  ghcrToken:
    fn::secret: <github-token>
  anthropicApiKey:
    fn::secret: <anthropic-api-key>
  pulumiConfig:
    docker-agent:registryAuth:
      - address: ghcr.io
        username: <github-user>
        password: ${ghcrToken}
    anthropicApiKey: ${anthropicApiKey}
  environmentVariables:
    ANTHROPIC_API_KEY: ${anthropicApiKey}
    AWS_ACCESS_KEY_ID: ${aws.login.accessKeyId}
    AWS_SECRET_ACCESS_KEY: ${aws.login.secretAccessKey}
    AWS_SESSION_TOKEN: ${aws.login.sessionToken}
    AWS_REGION: us-east-1
```

A stack imports it in its stack settings file, here `Pulumi.dev.yaml`, or with `pulumi config env add docker-agent/dev`:

```yaml
environment:
  - docker-agent/dev
```

- **Registry credentials.** The values under `pulumiConfig` become stack configuration, so `docker-agent:registryAuth` configures the default provider exactly like the `pulumi config set --path` commands above. Values wrapped in `fn::secret`, and values that interpolate them such as `${ghcrToken}`, arrive as Pulumi secrets. A key that is also set in `Pulumi.<stack>.yaml` keeps the stack's value.
- **Model keys for `AgentRun`, through `env`.** Read the key from configuration (`anthropicApiKey` above) as a secret and pass it to `env` as described in [AgentRun credentials](#agentrun-credentials), preferably with `inheritEnvironment: false`. The key is stored encrypted in the state, and credentials, `${env.NAME}` references and the variables the model SDKs read resolve only from `env`.
- **Model keys for `AgentRun`, through the environment.** `pulumi preview`, `pulumi up` and the other stack commands export the environment's `environmentVariables` to the Pulumi CLI and the plugins it starts. With the default `inheritEnvironment: true`, `AgentRun` resolves `ANTHROPIC_API_KEY` from there without an `env` input, and nothing about the key is stored in the state. The processes the configuration starts (tools, hooks, MCP and LSP servers) inherit every exported variable too.
- **Amazon Bedrock without static keys.** `fn::open::aws-login` exchanges an OpenID Connect token of Pulumi Cloud for temporary credentials of an IAM role that may call `bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream` (see [Configuring OpenID Connect for AWS](https://www.pulumi.com/docs/esc/guides/configuring-oidc/aws/)). Export them as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`, with `AWS_REGION`, for `amazon-bedrock` models, and use the default `inheritEnvironment: true`; or put them into `pulumiConfig` as secrets and pass them to `env`, which works with `inheritEnvironment: false` too. Before v0.2.0, the AWS SDK read them only from the environment of the provider process, also with `inheritEnvironment: false`; a stack that relied on that now needs them in `env` (see [AgentRun credentials](#agentrun-credentials)). A Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK` works in `env` as well. Google Vertex AI through ESC's `gcp-login` is not supported: it provides an access token, and the Google SDK reads application default credentials (`GOOGLE_APPLICATION_CREDENTIALS`, a key file) instead.
- **Running a published agent.** `pulumi env run` opens the environment and runs a command with its `environmentVariables`. `docker agent run` starts an interactive terminal UI, which needs `-i` (that also turns off the masking of secret values in the command's output); `--exec` runs one prompt without it:

  ```bash
  pulumi env run -i <org>/docker-agent/dev -- docker agent run "$(pulumi stack output pinnedReference)"
  pulumi env run <org>/docker-agent/dev -- docker agent run --exec "$(pulumi stack output pinnedReference)" "What changed in v2.4.0?"
  ```

The [`examples/esc-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/esc-yaml) program uses such an environment for an `AgentArtifact` and two `AgentRun`s, one with `env` and one with `inheritEnvironment`.

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
- `pulumi import` adopts an existing file instead: no backup is taken, `overwrite` is not needed, and destroying the resource leaves the file in place. See [Importing existing agents and files](#importing-existing-agents-and-files).

`UserSettings` renders the file with docker-agent's own types, so the result is byte-for-byte what docker-agent writes for the same settings. Its `extra` inputs (top level and `settings`) take keys that a newer docker-agent added; `skipValidation` turns off docker-agent's semantic checks (for example a safety mode or TUI option it does not know yet).

## Importing existing agents and files

`pulumi import` turns agent files, shared agents and Docker Agent dotfiles you already have into resources, and generates typed code for them in the language of your stack: the same inputs you would write by hand, not a YAML string. Add `--out <file>` to write the code to a file; without it, the CLI prints it. Then copy the code into your program. The next `pulumi preview` shows no changes. Like every `pulumi import`, the resources are protected (`protect: true` in the generated code).

```bash
# An agent file (YAML or HCL) or a shared agent, as a typed AgentConfig.
pulumi import docker-agent:index:AgentConfig assistant ./agent.yaml --out index.ts
pulumi import docker-agent:index:AgentConfig shared oci://ghcr.io/my-org/agents/assistant:v1

# An agent that was pushed with `docker agent share push`, as an AgentArtifact.
pulumi import docker-agent:index:AgentArtifact assistant ghcr.io/my-org/agents/assistant:v1

# Your docker-agent user configuration and a project's .agentsignore.
pulumi import docker-agent:index:UserSettings settings ~/.config/cagent/config.yaml
pulumi import docker-agent:index:AgentsIgnore project ./my-service
```

To import several resources at once, list them in an import file and run `pulumi import -f resources.json --out index.ts`:

```json
{
  "resources": [
    {"type": "docker-agent:index:AgentConfig", "name": "assistant", "id": "./agent.yaml"},
    {"type": "docker-agent:index:AgentArtifact", "name": "published", "id": "ghcr.io/my-org/agents/assistant:v1"},
    {"type": "docker-agent:index:UserSettings", "name": "settings", "id": "/home/me/.config/cagent/config.yaml"},
    {"type": "docker-agent:index:AgentsIgnore", "name": "project", "id": "./my-service"}
  ]
}
```

| Resource | Import ID | Resource ID after the import |
|---|---|---|
| `AgentConfig` | `file://<path>`, a path ending in `.yaml`, `.yml` or `.hcl`, or `oci://<registry>/<repository>:<tag>` (or `@sha256:<digest>`) | the resource name, as for a created `AgentConfig` |
| `AgentArtifact` | `<registry>/<repository>:<tag>`, `<registry>/<repository>:<tag>@sha256:<digest>` (the tag must point to that digest), or `<registry>/<repository>@sha256:<digest>` (one tag of the repository must point to it) | the tag, as for a created `AgentArtifact` |
| `UserSettings` | the path of the file, optionally as `file://<path>` | the file's absolute path |
| `AgentsIgnore` | the project directory, or the path of its `.agentsignore` | the file's absolute path |
| `AgentRun` | not importable: a run has nothing to read back | |

Relative paths are relative to the Pulumi project directory, like every other path of the provider, and `~` is your home directory. A path inside your home directory is written as `~/...` in the generated code, so the program works for other users too. `oci://` and `AgentArtifact` imports read the registry with the provider's `registryAuth` and the Docker credential store, the same way `getAgent` does (see [Setting registry credentials](#setting-registry-credentials)).

- **What `AgentConfig` imports.** Every key becomes a typed input, and the shorthands docker-agent accepts are expanded the way docker-agent reads them (an `instruction` list is joined with blank lines, `skills: true` becomes `["local"]`, a number of seconds becomes a duration). Keys that no typed input covers go to the `extra` input of the object they belong to. `instruction_file` references are read relative to the file's directory, the way docker-agent reads them, and imported as `instruction`; HCL files are converted with docker-agent's own converter, `file()` calls included. Comments, key order and YAML anchors are not kept. The imported resource is not linked to the file or artifact: the program is the source of truth from then on, and a refresh does not read the file again.
- **What `AgentArtifact` imports.** `reference` (the tag), the pushed `yaml` exactly as it is stored, the `annotations` that the provider would not have set itself, and `deleteBehavior: retain`.
- **Only valid configurations are imported.** The configuration (and user settings) must pass the same checks as `pulumi preview`, with the docker-agent version the provider is built with. To import a configuration that relies on a newer docker-agent, set the provider's `skipValidation` (for example `DOCKER_AGENT_SKIP_VALIDATION=true pulumi import ...`): the imported resource then gets `skipValidation: true`.
- **Local files are adopted.** The import reads the file and writes nothing; it takes no backup, and the resource needs no `overwrite`. `pulumi up` changes the file only when you change the inputs. Because Pulumi did not create the file, destroying the resource (after `pulumi state unprotect`, or after removing `protect` and running `pulumi up`) leaves the file in place, with the content Pulumi last wrote, instead of removing it. Moving the resource to another `path` or `directory` leaves the adopted file in place as well; the file at the new path is managed like a created one. Symbolic links are followed, as for created resources.
- **Formatting of adopted files.** `UserSettings` reads the file with docker-agent's own types, so everything docker-agent keeps is imported, including unknown keys at the top level and in `settings` (into `extra`); docker-agent drops unknown keys elsewhere, and so does the import. `AgentsIgnore` imports the comment block at the top of the file as `comment` and every other line as a pattern; comment lines between patterns are not imported (the import warns about them). When the file is not written the way the provider writes it (comments, blank lines, key order or quoting), the import warns: the file stays as it is, but `pulumi refresh` then reports the difference as drift, and the next `pulumi up` rewrites the file in the provider's format, with the same meaning.
- **Pulumi YAML programs.** Write the generated code to a file other than `Pulumi.yaml` or `Main.yaml` (for example `--out imported.yaml`) and paste it into your program: `--out` empties its file before the import reads the program.

## Converting existing agent files

`pulumi import` adds one resource to an existing program. `pulumi convert --from docker-agent` instead turns a whole directory of docker-agent agent files into a new Pulumi project in any language, with one typed `AgentConfig` per file. Unlike an import, it keeps the comments and the key order of the files.

The converter is a separate plugin, released with the provider. `pulumi convert` only downloads converters that Pulumi publishes, so install it once, with the same version as the provider:

```bash
pulumi plugin install converter docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
```

Then run it in the directory that holds your agent files:

```bash
pulumi convert --from docker-agent --language typescript --out ../my-agents

# Also push every configuration and run a prompt against it:
pulumi convert --from docker-agent --language python --out ../my-agents -- \
  --push-to ghcr.io/my-org/agents --run "Summarize the README"

# Convert only some files or directories:
pulumi convert --from docker-agent --language go --out ../my-agents -- agent.yaml team/
```

`--language` is one of `typescript`, `python`, `go`, `csharp`, `java`, `yaml` or `hcl`. The arguments after `--` go to the converter, in any order:

| Argument | Effect |
|---|---|
| `--push-to <registry>/<namespace>` | Adds an `AgentArtifact` per configuration that pushes it to `<registry>/<namespace>/<name>:latest`, and exports its `pinnedReference`. When only one configuration is converted, a full reference with a tag (`<registry>/<repository>:<tag>`) works too. |
| `--run "<prompt>"` | Adds an `AgentRun` per configuration that sends the prompt to its default agent during `pulumi up` and exports the answer. Model keys come from the environment of the Pulumi CLI (see [AgentRun credentials](#agentrun-credentials)), and tool calls that need approval are denied. |
| `--provider-version <version>` | The provider version the program pins. Defaults to the converter's version. |
| `<file or directory>...` | Converts only these paths, relative to the current directory. |

What the converter does:

- **Files.** It converts every `*.yaml` and `*.yml` file with a top-level `agents` key and every docker-agent `*.hcl` file. It skips hidden directories, `node_modules`, `vendor`, Python virtual environments, the Pulumi project and stack files, and the `--out` directory. A YAML file and an HCL file of the same name in the same directory that hold the same configuration are converted once. Resources are named after their path: `team/reviewer.yaml` becomes `team-reviewer`.
- **Typed inputs.** Every key becomes a typed input, in the order of the file, and multi-line strings keep their text. YAML anchors and merge keys are expanded. Keys without a typed input go to `extra`, with a warning. Keys that docker-agent never reads are left out, with a comment that quotes the original line.
- **Comments.** They are kept in front of each `AgentConfig`, each labelled with the key it was written at, because Pulumi's program generators keep only the comments in front of a resource.
- **`instruction_file`.** It is kept, and the instruction files are copied next to the program. Such an `AgentConfig` gets `skipValidation: true`, because the provider validates in memory, where there are no files, and it gets no `AgentArtifact` or `AgentRun`: docker-agent reads instruction files only next to a local agent file. In HCL files, `file("x.md")` becomes `readFile("x.md")`, and a `file()` call with template variables becomes the value docker-agent loaded, with a comment.
- **Plugin download.** Every resource of the program sets the `pluginDownloadURL` and `version` resource options, so the program downloads the provider from its GitHub release. A Pulumi HCL program still needs the provider plugin installed first (see [HCL](#hcl)).

The program generators of some languages have bugs that the converter cannot avoid. It warns when one of the Pulumi YAML or Go bugs affects your files. Java programs need a few fixes by hand:

- **"Pulumi YAML limitation".** A string such as `~` is written unquoted (quote it in `Main.yaml`), and a map key that is also an input name of its values (a model named `model`) is rejected by the Pulumi YAML type checker. Convert such files to another language.
- **"Pulumi Go limitation".** A list inside a free-form map is written as `pulumi.Any{...}`; change it to `pulumi.Any([]interface{}{...})`.
- **Java.** Change the imports of `com.dirien.dockeragent` to `io.github.dirien.dockeragent`, use enum constants instead of strings (`HookType.Command`), catch the `IOException` of `Files.readString`, escape the backslashes in text blocks, and compile with Java 15 or later instead of the generated Java 11 target. Resources whose names are not identifiers get their identifier as name (`yamlAnchors` instead of `yaml-anchors`).

The same converter imports shared agents: `pulumi import --from docker-agent <registry>/<repository>` imports every tag of a repository that holds an agent artifact as an `AgentArtifact`, and skips other tags with a warning. It lists the tags with your Docker credentials (`docker login`); add `-- --insecure` for a plain-HTTP or self-signed registry.

## Runtime credentials for agents

Rendering, validating and publishing configurations needs no model API keys, and validation runs offline. Model credentials are read where the agent runs: from the environment of whoever runs it with `docker agent run`, `docker agent serve api` or a container based on `docker/docker-agent`, or, for `AgentRun`, by a child process of the provider during `pulumi up` (see [AgentRun credentials](#agentrun-credentials)). Coding-harness agents need no model key: the Claude Code and Codex CLIs use their own logins (see [Claude Code credentials](#claude-code-credentials) and [Codex credentials](#codex-credentials)).

The `requiredEnvVars` output of `AgentConfig` and `validateAgentConfig` lists the variables a configuration needs, on a best-effort basis (a model with a `baseUrl` requires none there, even when its provider still needs its key when it runs). These are the model providers of docker-agent v1.150.0, the version this provider pins, with the variables they read. `tokenKey` on a model or a custom provider replaces the key variable by another name.

| Provider | Protocol and default endpoint | Credentials | Other variables it reads |
|---|---|---|---|
| `openai` | OpenAI Chat Completions or Responses (chosen per model), `https://api.openai.com/v1` | `OPENAI_API_KEY` | `OPENAI_BASE_URL` (the endpoint of a model without `baseUrl`); `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID` and `OPENAI_CUSTOM_HEADERS`, which the OpenAI SDK adds to every OpenAI-protocol model, including the aliases below |
| `anthropic` | Anthropic Messages, `https://api.anthropic.com` | `ANTHROPIC_API_KEY`, or workload identity federation (the model's `auth`) | `ANTHROPIC_BASE_URL`; `ANTHROPIC_AUTH_TOKEN`, which the Anthropic SDK sends as a bearer token next to the key when the environment has no `ANTHROPIC_API_KEY` (a key in another `tokenKey`); `ANTHROPIC_CUSTOM_HEADERS`, applied only when the environment has no `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_PROFILE` (nor a workload identity federation or a profile file of the Anthropic SDK) |
| `google` (Gemini API) | Gemini, `https://generativelanguage.googleapis.com` | `GOOGLE_API_KEY` or `GEMINI_API_KEY` | `GOOGLE_GEMINI_BASE_URL` |
| `google` on Vertex AI (`providerOpts.project` and `providerOpts.location`, or `GOOGLE_GENAI_USE_VERTEXAI=true` with `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION`; also Model Garden models with `providerOpts.publisher`) | Vertex AI, the endpoint of the location | Google application default credentials: `GOOGLE_APPLICATION_CREDENTIALS` (the path of a service account key file), otherwise the gcloud login of the machine (`gcloud auth application-default login`) or the metadata server of a Google Cloud machine | `GOOGLE_VERTEX_BASE_URL`, `GOOGLE_CLOUD_REGION` |
| `amazon-bedrock` | Bedrock Converse, the endpoint of the region | `AWS_BEARER_TOKEN_BEDROCK` (a Bedrock API key), otherwise the AWS SDK's default chain: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`, `AWS_PROFILE`, `AWS_ROLE_ARN` with `AWS_WEB_IDENTITY_TOKEN_FILE`, the shared files (`~/.aws/credentials`, `~/.aws/config`, SSO), container credentials and the instance metadata service | `AWS_REGION` or `AWS_DEFAULT_REGION` (or `providerOpts.region`), `AWS_ENDPOINT_URL_BEDROCK_RUNTIME` (or `providerOpts.endpoint_url`); `providerOpts.profile` and `providerOpts.role_arn` |
| `dmr` (Docker Model Runner) | OpenAI Chat Completions to the Model Runner | none | `MODEL_RUNNER_HOST`, otherwise the endpoint that `docker model status` reports through the Docker CLI's connection (`DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG`, `DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY`, `DOCKER_TLS`) |
| `chatgpt` (ChatGPT subscription) | OpenAI Responses, `https://chatgpt.com/backend-api/codex` | the ChatGPT login of `docker agent setup` (`~/.config/cagent/chatgpt-auth.json`), or a ChatGPT access token in `CHATGPT_OAUTH_TOKEN` | |
| `github-copilot` | OpenAI Chat Completions or Responses, `https://api.githubcopilot.com` | `GITHUB_TOKEN`, otherwise `GH_TOKEN` | |
| `azure` (Azure OpenAI) | OpenAI Chat Completions, the deployment's `baseUrl` (required) | `AZURE_API_KEY` | `providerOpts.api_version` |
| `ollama` | OpenAI Chat Completions, `http://localhost:11434/v1` | none; `OPENAI_API_KEY` of the environment is sent when it is set | `OLLAMA_HOST` is not read: set `baseUrl` for another host |
| `cloudflare-workers-ai` | OpenAI Chat Completions, `https://api.cloudflare.com/client/v4/accounts/${CLOUDFLARE_ACCOUNT_ID}/ai/v1` | `CLOUDFLARE_API_TOKEN` | `CLOUDFLARE_ACCOUNT_ID` |
| `cloudflare-ai-gateway` | OpenAI Chat Completions, `https://gateway.ai.cloudflare.com/v1/${CLOUDFLARE_ACCOUNT_ID}/${CLOUDFLARE_GATEWAY_ID}/compat` | `CLOUDFLARE_API_TOKEN` | `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_GATEWAY_ID` |
| A custom provider (`providers` with `apiType: openai_chatcompletions` or `openai_responses` and a `baseUrl`) | the configured protocol and `baseUrl` | the `tokenKey` variable; without `tokenKey`, no key is sent | A custom provider with `provider: <type>` gets the rules of that type instead. An `openai` model with a `baseUrl` and no `tokenKey` sends `OPENAI_API_KEY` of the environment, like `ollama`. |

The other aliases are OpenAI-compatible services with a fixed endpoint and one key, all over OpenAI Chat Completions (`opencode` uses Responses for GPT models):

| Provider | Default endpoint | Credentials |
|---|---|---|
| `baseten` | `https://inference.baseten.co/v1` | `BASETEN_API_KEY` |
| `cerebras` | `https://api.cerebras.ai/v1` | `CEREBRAS_API_KEY` |
| `daoxe` | `https://api.daoxe.com/v1` | `DAOXE_API_KEY` |
| `deepseek` | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` |
| `fireworks-ai` | `https://api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` |
| `groq` | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| `huggingface` | `https://router.huggingface.co/v1` | `HF_TOKEN` |
| `minimax` | `https://api.minimax.io/v1` | `MINIMAX_API_KEY` |
| `mistral` | `https://api.mistral.ai/v1` | `MISTRAL_API_KEY` |
| `moonshotai` | `https://api.moonshot.ai/v1` | `MOONSHOT_API_KEY` |
| `nebius` | `https://api.studio.nebius.com/v1` | `NEBIUS_API_KEY` |
| `nvidia` | `https://integrate.api.nvidia.com/v1` | `NVIDIA_API_KEY` |
| `opencode` | `https://opencode.ai/zen/v1` | `OPENCODE_API_KEY` |
| `opencode-go` | `https://opencode.ai/zen/go/v1` | `OPENCODE_API_KEY` |
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| `ovhcloud` | `https://oai.endpoints.kepler.ai.cloud.ovh.net/v1` | `OVH_AI_ENDPOINTS_ACCESS_TOKEN` |
| `requesty` | `https://router.requesty.ai/v1` | `REQUESTY_API_KEY` |
| `togetherai` | `https://api.together.xyz/v1` | `TOGETHER_API_KEY` |
| `vercel` | `https://ai-gateway.vercel.sh/v1` | `AI_GATEWAY_API_KEY` |
| `xai` | `https://api.x.ai/v1` | `XAI_API_KEY` |

A model or custom provider with a `baseUrl` sends the requests of an alias to another endpoint; `OPENAI_BASE_URL` does not apply to aliases. The [Docker Agent model provider documentation](https://docs.docker.com/ai/docker-agent/) describes each provider.

Never put a secret into an agent configuration: it ends up in the published artifact. Instead:

- Set `tokenKey` on a model or custom provider to the **name** of the environment variable that holds the key, for example `tokenKey: MY_OPENAI_KEY`.
- Use `${env.VAR}` placeholders in toolset `env` and `headers` values. docker-agent expands them when the agent runs.

`docker agent doctor <reference>` checks that every required variable is set before you run an agent with the CLI.

### AgentRun credentials

`AgentRun` runs each agent in a child process of the provider on the machine that runs `pulumi up`: the provider binary, started again for the run. The model SDKs read their variables (keys, base URLs, headers, AWS and Google credentials) from that process's environment, so `AgentRun` makes the child's environment exactly what the run may use:

1. its `env` input, a map of variable names to values that is always stored as a secret. Set the values from secret config, for example `env: { OPENAI_API_KEY: config.requireSecret("openaiApiKey") }`. `env` wins over everything else;
2. with `inheritEnvironment: true` (the default), the environment of the Pulumi CLI, for example an `OPENAI_API_KEY` exported in the shell, set as a CI secret or exported by a Pulumi ESC environment;
3. with `inheritEnvironment: false`, instead of that environment, only the base environment below, which holds no credentials.

Credentials, `${env.NAME}` references of the configuration and the SDKs' own variables all resolve from this environment, so every variable in the tables above works in `env`, `tokenKey` variables included. The child receives `env` over a pipe and sets it in its own environment: the values never appear in its command line or its initial environment, which other processes of the same user can read (`/proc/<pid>/environ` on Linux, the process table on macOS), and on Linux the child cannot be inspected by other processes. Before v0.2.0, runs were in the provider process, where the SDKs read the provider's whole environment whatever `inheritEnvironment` said and ignored their variables in `env`; the [CHANGELOG](https://github.com/dirien/pulumi-docker-agent/blob/main/CHANGELOG.md) lists what that changes for existing stacks.

The base environment of a run with `inheritEnvironment: false`, when the environment of the Pulumi CLI sets these variables:

| Group | Variables |
|---|---|
| System | `PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `LANG`, `LANGUAGE`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TERM`, `TMPDIR`, `TEMP`, `TMP`, `TZ`, and on Windows `SYSTEMROOT`, `SYSTEMDRIVE`, `COMSPEC`, `PATHEXT`, `WINDIR`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, `PROGRAMDATA`, `PROGRAMFILES` |
| Claude Code and Codex configuration (they select the CLI's login) | `CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `IS_SANDBOX`, `CODEX_HOME`, `CODEX_CA_CERTIFICATE` |
| Network | `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `ALL_PROXY` (also in lower case), `SSL_CERT_FILE`, `SSL_CERT_DIR` |
| Docker connection (Docker Model Runner, `docker:` MCP servers, OCI references) | `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG`, `DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY`, `DOCKER_TLS` |
| docker-agent settings | `DOCKER_AGENT_AUTO_INSTALL`, `DOCKER_AGENT_TOOLS_DIR`, `DOCKER_AGENT_DISABLE_DESKTOP_PROXY`, `DOCKER_AGENT_KIT_DIR`, `TELEMETRY_ENABLED`, `TELEMETRY_TAGS`, `SANDBOX_VM_ID` |
| Go runtime | `GODEBUG`, `GOMAXPROCS`, `GOMEMLIMIT`, `GOGC` |
| Go modules (docker-agent's tool installation runs `go install`) | `GOPROXY`, `GOFLAGS`, `GONOSUMDB`, `GONOPROXY`, `GOPRIVATE`, `GOSUMDB`, `GOINSECURE`, `GOTOOLCHAIN`, `GOENV`, `GOPATH`, `GOMODCACHE`, `GOCACHE`, `GOROOT`, `GOAUTH`, `GOVCS`, `GOTMPDIR` |
| Desktop session (Docker credential helpers, the Linux keyring) | `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` |

`GITHUB_TOKEN` and `GH_TOKEN`, which docker-agent's tool installation uses for GitHub's API (60 unauthenticated requests per hour), are credentials: pass them in `env` when the run installs tools.

**What reaches the processes of the configuration.** The base environment and `env` are the run child's environment. The processes the configuration starts get what [Where tools run](https://www.pulumi.com/registry/packages/docker-agent/#where-tools-run) on the overview page describes, built from the environment of the Pulumi CLI, not from the child's: command hooks and the Claude Code and Codex CLIs get `env`; `shell`, `script` and `background_jobs` tools, LSP servers and MCP servers started with `command` get only their toolset's own `env`, which can pass a value on explicitly, for example `env: {GITHUB_TOKEN: "${env.GITHUB_TOKEN}"}`. With `inheritEnvironment: false` they get the system variables instead of the environment of the Pulumi CLI (the other variables of the base environment are set to an empty value for them). A variable that only `env` sets is unset for `shell`, `script` and `background_jobs`, and empty for LSP servers, for a `shell` toolset with `sudo_askpass` and for a `background_jobs` toolset with `recall`. `docker:` MCP servers (the Docker MCP Gateway), filesystem `post_edit` commands, the `open_url` launcher, docker-agent's on-demand tool installation and docker-agent's own helper commands inherit the child's environment, `env` included: docker-agent starts them without letting `AgentRun` change their environment.

**Machine logins and SDK chains.** Some credentials are not environment variables, and the SDKs and CLIs find them on the machine whatever `inheritEnvironment` says:

| Source | Used by | Instead, in `env` |
|---|---|---|
| The `claude` login (macOS keychain, `~/.claude/.credentials.json`) | Claude Code agents | `CLAUDE_CODE_OAUTH_TOKEN` |
| The Codex login of `$CODEX_HOME` (default `~/.codex`) | Codex agents | `CODEX_HOME` of its own, `CODEX_API_KEY` or `CODEX_ACCESS_TOKEN` |
| The ChatGPT login of `docker agent setup` (`~/.config/cagent/chatgpt-auth.json`) | `chatgpt` models | `CHATGPT_OAUTH_TOKEN` |
| The AWS shared files (`~/.aws/credentials`, `~/.aws/config`, SSO), container credentials and the instance metadata service | `amazon-bedrock` models without credentials in `env` | `AWS_BEARER_TOKEN_BEDROCK`, or `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` (and `AWS_SESSION_TOKEN`); `AWS_EC2_METADATA_DISABLED: "true"`, `AWS_SHARED_CREDENTIALS_FILE: /dev/null` and `AWS_CONFIG_FILE: /dev/null` switch the machine sources off |
| The gcloud login (`~/.config/gcloud/application_default_credentials.json`) and the metadata server of a Google Cloud machine | Vertex AI models without `GOOGLE_APPLICATION_CREDENTIALS` | `GOOGLE_APPLICATION_CREDENTIALS`, the path of a service account key file, which wins over both |
| The Docker CLI's configuration and contexts (`~/.docker`) | Docker Model Runner, `docker:` MCP servers, `reference` | `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG` |

The preview warns when a Bedrock or Vertex AI model gets no credentials from `env` under `inheritEnvironment: false` and would use the machine's AWS or Google credentials, which may belong to another account. For a run that depends on nothing but its inputs, pass every credential in `env`, with the AWS switches above for Bedrock.

docker-agent's other credential sources are never used: its `~/.config/cagent/.env` file (the API keys `docker agent setup` stores), `/run/secrets`, the credential helper of the user settings, Docker Desktop secrets, the Docker models gateway and `op://` 1Password references. When a missing variable is in `~/.config/cagent/.env`, the error says so (it reads the names in the file, never the values).

**Redaction and secrets.** An invalid key fails the update with the model's error message, followed by the run's credential line. The provider redacts the values of `env`, of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...), of the variables the configuration names as `token_key`, and of the tokens of the ChatGPT and Codex logins a run uses, from errors, warnings and logs. When the agent's answer contains one of these values, its `output` and `structuredOutput` are stored as secrets. `env` values that are configuration rather than credentials are neither redacted nor make an answer secret: `AWS_REGION`, `AWS_DEFAULT_REGION`, `AWS_PROFILE`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`, `GOOGLE_CLOUD_REGION`, `GOOGLE_GENAI_USE_VERTEXAI`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_USE_FOUNDRY`, `DOCKER_AGENT_DEFAULT_MODEL`, `CAGENT_DEFAULT_MODEL`, `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, booleans (`true`, `false`, `yes`, `no`, `on`, `off`) and the base URLs `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_VERTEX_BASE_URL`, `MODEL_RUNNER_HOST` and `OLLAMA_HOST` when they are http(s) URLs without user info, query, fragment or a path segment that looks like a key (32 characters or more, or 20 or more that mix letters and digits). A credential-named variable is always secret, and so is a value that the program marked secret itself (for example `env: {AWS_REGION: ${secretRegion}}` in Pulumi YAML; the SDKs mark the whole `env` map secret, which does not count).

### Preview checks and the credential line

For a `yaml` configuration that is known during the preview, `AgentRun` checks the credentials of its models, and only when the operation runs the agent: a create, a change of an input that affects the run, `rerun: always`, or such an input that is unknown during the preview. Unchanged resources and changes that only update the state stay quiet; `pulumi up` checks twice (in its preview and its update), so a warning can show twice. A configuration pulled by `reference` is checked when it runs: the warnings appear at the start of the run, also of a delete prompt. The checks never call a model, a registry or the network, and never show a value.

| Preview message | What it means |
|---|---|
| "the configuration needs `<VAR>`, which env does not set ..." | A model, a `${env.NAME}` reference or a toolset needs the variable, and neither `env` nor (with `inheritEnvironment: true`) the environment of the Pulumi CLI sets it. The run would fail. Set it in `env`. The message adds when the variable is in the environment of the Pulumi CLI but not inherited, or in `~/.config/cagent/.env`, which `AgentRun` does not read. |
| "no first_available candidate has its credentials ..." | None of the candidates of a `first_available` model can run. Set the credentials of one of them. |
| "`<VAR>` is set in the environment of the Pulumi CLI, but with inheritEnvironment: false it no longer reaches `<model>` since v0.2.0: ..." | Before v0.2.0, the SDK used the variable despite `inheritEnvironment: false`. The message says what changes, for example "its requests go to https://api.openai.com/v1" for `OPENAI_BASE_URL`. Set it in `env`, or set the model's `baseUrl`, `tokenKey` or `providerOpts.endpoint_url`. |
| "`<VAR>` (inherited from the environment of the Pulumi CLI) ... as with docker agent run" | With `inheritEnvironment: true`, an SDK variable of your shell sends the model's requests, its key or a header elsewhere: `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, `GOOGLE_GEMINI_BASE_URL` or `GOOGLE_VERTEX_BASE_URL` for a model without `baseUrl`, `OPENAI_API_KEY` for a non-loopback `baseUrl` without `tokenKey` (for example Ollama Cloud), `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_CUSTOM_HEADERS` when the Anthropic SDK applies it (see the table above), and `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID` and `OPENAI_CUSTOM_HEADERS` for a non-OpenAI endpoint. `docker agent run` does the same. Set `inheritEnvironment: false`, `tokenKey` or `baseUrl`, or blank the variable in `env` as the message says. |
| "... gets no AWS credentials from env, so it uses this machine's AWS default chain ..." and "... gets no GOOGLE_APPLICATION_CREDENTIALS from env, so it uses this machine's Google application default credentials ..." | With `inheritEnvironment: false`, a Bedrock or Vertex AI model falls back to the machine sources of the table above. Pass the credentials in `env` if they may belong to another account. |
| "`<model>` uses a ChatGPT subscription, but this machine has no ChatGPT login and env does not set CHATGPT_OAUTH_TOKEN ..." | Sign in with `docker agent setup` on the machine that runs `pulumi up` (see [ChatGPT subscription](#chatgpt-subscription)). |
| "`<model>` uses a ChatGPT subscription, but env sets CHATGPT_OAUTH_TOKEN to an empty value, which hides the ChatGPT login of docker agent setup from the run, so the run fails ..." | An empty `CHATGPT_OAUTH_TOKEN` in `env` (or, inherited, in the environment of the Pulumi CLI) hides the login, as with `docker agent run`. Remove it from `env` (or unset it in your shell, or set `inheritEnvironment: false`), or pass a ChatGPT access token. |
| "agents.`<a>`.structured_output is not enforced by Amazon Bedrock ..." | docker-agent sends Bedrock models no schema, so the answer may not be JSON and the run then fails. Set `structuredOutput.mode: tool`. |
| "AgentRun does not use a Docker models gateway ..." and "bypass_models_gateway ... has no effect" | `DOCKER_AGENT_MODELS_GATEWAY` (or `CAGENT_MODELS_GATEWAY`) and `bypass_models_gateway` change nothing: the models call their providers directly, with their own credentials. |
| "agents.`<a>`.model auto resolves to `<provider>/<model>`, selected by DOCKER_AGENT_DEFAULT_MODEL from env, but the run's environment does not set its credential ..." | `DOCKER_AGENT_DEFAULT_MODEL` selects a model whose key the run would miss. Set the key in `env`, or select another model. |
| "agents.`<a>`.model auto resolves to `<provider>/<model>` (...)" (info) | What `model: auto` will use, and why (see [model auto](#model-auto-and-docker_agent_default_model)). |
| "`<model>` uses the ChatGPT login of docker agent setup on this machine (...)" (info) | The run can use, and may refresh, that login. |

The preview fails for a provider that is not declared in the configuration, with "AgentRun does not use user-level providers (from docker agent setup, or UserSettings providers): declare the provider under `providers` in the configuration".

**The credential line.** At the start of each run, unless `logging` is `none`, `pulumi up` shows the models the run can use and where each one's credential comes from, for example:

```
models: openai/gpt-5-mini (OPENAI_API_KEY from env); fallback anthropic/claude-haiku-4-5 (ANTHROPIC_API_KEY from the environment of the Pulumi CLI)
```

Models chosen by `model: auto` or `first_available` say so (`auto: anthropic/claude-sonnet-5 (...)`), and fallbacks, routing targets and compaction models are listed too. The sources include the SDKs' own fallbacks: "no key" for a custom provider without `tokenKey`, "no key: Docker Model Runner", "the ChatGPT login of docker agent setup, billed to the ChatGPT plan", ", and ANTHROPIC_AUTH_TOKEN from the environment of the Pulumi CLI as a bearer token", "this machine's AWS default credential chain (shared files, SSO, container or instance metadata)", "this machine's Google application default credentials (the gcloud login, or the metadata server)", and "`<VAR>` (not set)". The line names variables, never values; with a secret `yaml` or `agent`, it says "a model" instead of the model. An error for a model request that was rejected (401, 403) repeats it, and with `inheritEnvironment: false` an error for a rejected request or a failed connection adds the SDK variables of the Pulumi CLI's environment that no longer reach the models; a failure of a Claude Code or Codex CLI gets neither. Coding-harness agents have their own line after the run (see [Claude Code credentials](#claude-code-credentials) and [Codex credentials](#codex-credentials)).

### Costs

`usage.cost` is the cost of the run in US dollars as docker-agent computes it from the [models.dev](https://models.dev) catalog bundled with the provider (a snapshot of 2026-10-07), or from a model's own `cost` table, for models billed per token. It is `0` for models without known pricing (custom providers without `cost`, `dmr`, `ollama`, models newer than the snapshot) and for usage billed to a subscription: `chatgpt` models, a Claude Code agent with the Claude login or `CLAUDE_CODE_OAUTH_TOKEN`, and a Codex agent with a ChatGPT login (also a ChatGPT workspace token, workload identity or a keyring login). The run summary keeps the two apart and names the plan, with what its usage would cost at API prices where that can be estimated: "no per-token cost: billed to the ChatGPT plan", "no per-token cost: billed to the Claude subscription (API price about $0.0421)", or, in a configuration that uses both, "estimated cost $0.0123, and usage billed to the ChatGPT plan (API price about $0.0250)"; without either, "no cost estimate (no known pricing for the models)". A Claude Code agent that bills per token (`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, Bedrock, Vertex AI, Foundry) reports the cost Claude Code reports (`total_cost_usd`), and a Codex agent that does (`CODEX_API_KEY`, an API-key or Bedrock login, a `model_provider` without `requires_openai_auth`) an estimate at the catalog's API prices. The cost of a request that a rule-based router sends to another model is computed with the price of the router's own model ([docker/docker-agent#4576](https://github.com/docker/docker-agent/issues/4576)).

### ChatGPT subscription

A ChatGPT plan (Plus, Pro, Business) runs docker-agent's own runtime through the `chatgpt` provider: `model: chatgpt/gpt-5.6` (docker-agent's default ChatGPT model), or a `models` entry with `provider: chatgpt`. The ChatGPT backend serves the GPT-5 model family over the Responses API. Unlike a coding harness, the agent keeps everything `AgentRun` does for docker-agent agents: `toolApproval`, `permissions`, hooks and redaction.

1. Sign in once with `docker agent setup` (choose ChatGPT) as the user that runs `pulumi up`, on the machine that runs it. It opens a browser and stores the login in `~/.config/cagent/chatgpt-auth.json`.
2. Run the agent with `inheritEnvironment` `true` or `false`: the login is not an environment variable. The [`AgentRun` API docs](https://www.pulumi.com/registry/packages/docker-agent/api-docs/agentrun/) have an example.

`AgentRun` can use the login for a configuration that names a chatgpt model anywhere a model can be used (as a model, fallback, routing target, `first_available` candidate, compaction model, the model of a hook, ...) and whose run's environment does not set `CHATGPT_OAUTH_TOKEN`. `model: auto` alone never selects it; `DOCKER_AGENT_DEFAULT_MODEL: chatgpt/<model>` does. A chatgpt model with a `tokenKey` of its own uses that variable instead, and an empty `CHATGPT_OAUTH_TOKEN` hides the login, as with `docker agent run`, so the run fails (the preview warns). The run touches the login only when it asks for its token: a `first_available` candidate that is not chosen leaves it alone. Then `AgentRun`:

- takes a lock, `chatgpt-auth.json.lock` next to the login, which other `AgentRun` runs on the machine respect (the `docker agent` CLI does not), waiting for it up to 30 seconds;
- copies the login into the run's private config directory (mode `0600`, deleted after the run), and before a refresh copies it again when another process refreshed it (or signed in again) since;
- lets docker-agent refresh the copy when its access token expires within 5 minutes, and writes a refreshed login back to `~/.config/cagent/chatgpt-auth.json` at once, atomically, and only when the file is still the one the run copied or wrote last; otherwise it keeps the file. A refresh that the token endpoint refused is not tried again with the same login;
- after the run, writes back what the run could not, waiting up to 60 seconds for the lock, also when the operation was cancelled. When that fails too, it keeps the refreshed login as `chatgpt-auth.json.agentrun-<UTC time>` (for example `chatgpt-auth.json.agentrun-20261013T151925.123Z`, mode `0600`) next to the login, or in the temporary directory, and warns: the refresh replaced the refresh token, so move the file into place (`mv <kept> ~/.config/cagent/chatgpt-auth.json`) before the login is used again, or sign in again with `docker agent setup`.

A config directory that is read-only, or a file system without locks, works without the lock; the compare and swap still keeps a login that another process wrote. This is the only file of `~/.config/cagent` that `AgentRun` reads or writes. Its tokens, also those of a refreshed login, are redacted like credentials. When the token endpoint refuses a refresh, or a chatgpt model's request gets a 401, the error says that another docker agent process (the `docker agent` CLI, or another Pulumi provider) may have refreshed the login at the same time: run the command again, and sign in again with `docker agent setup` if it fails again. A configuration that names a chatgpt model and also uses `model: auto` without `DOCKER_AGENT_DEFAULT_MODEL` asks for the token while `auto` looks for a provider, so its runs copy (and, when it is about to expire, refresh) the login even when `auto` chooses another provider.

The run's credential line reads `chatgpt/gpt-5.6 (the ChatGPT login of docker agent setup, billed to the ChatGPT plan)`, `usage.cost` is `0` and the summary says "no per-token cost: billed to the ChatGPT plan". Runs count toward the plan's usage limits.

The login needs a browser sign-in on the machine, so it does not work in CI or Pulumi Deployments. `CHATGPT_OAUTH_TOKEN` in `env` takes a ChatGPT access token instead, which is short-lived and never refreshed; for unattended runs, prefer an API key provider such as `openai`.

### Ollama and Ollama Cloud

The `ollama` provider talks to an [Ollama](https://ollama.com) server over its OpenAI-compatible API, by default `http://localhost:11434/v1`, without a key. [`examples/agent-run-ollama-yaml`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/agent-run-ollama-yaml) runs an agent on it.

- **Local models.** Pull the model first (`ollama pull llama3.2`) and use `model: ollama/llama3.2`. For an Ollama server on another host, set the model's `baseUrl` (for example `http://gpu-box:11434/v1`): docker-agent does not read `OLLAMA_HOST`. Local models are slow on a CPU, so raise `timeoutSeconds`, and their context window is small by default: set `OLLAMA_CONTEXT_LENGTH` on the Ollama server for long instructions or many tools. If a model ignores the schema of `structuredOutput`, set `mode: tool`.
- **Ollama Cloud through the local Ollama.** Sign the local Ollama in to your ollama.com account (`ollama signin`, or the app), then use a cloud model by its cloud tag, for example `model: ollama/gpt-oss:120b-cloud`. Cloud models need no download: the local Ollama forwards the requests to Ollama Cloud with its account, so `AgentRun` needs no key, and the model runs where the local Ollama does.
- **The Ollama Cloud API directly**, without a local Ollama (for example in CI): declare a custom provider for `https://ollama.com/v1` with the key variable `OLLAMA_API_KEY`, create a key at [ollama.com/settings/keys](https://ollama.com/settings/keys) and pass it in `env`. Model names there have no cloud suffix (`gpt-oss:120b`; `https://ollama.com/api/tags` lists them):

  ```yaml
  resources:
    assistant:
      type: docker-agent:index:AgentConfig
      properties:
        providers:
          ollama-cloud:
            apiType: openai_chatcompletions
            baseUrl: https://ollama.com/v1
            tokenKey: OLLAMA_API_KEY
        agents:
          - name: root
            model: ollama-cloud/gpt-oss:120b
    answer:
      type: docker-agent:index:AgentRun
      properties:
        yaml: ${assistant.yaml}
        prompt: Summarise the release notes in three bullet points.
        inheritEnvironment: false
        env:
          OLLAMA_API_KEY: ${ollamaApiKey}
  ```

  Keep `tokenKey`: an `ollama` or `openai` model with a `baseUrl` and no `tokenKey` would send `OPENAI_API_KEY` of the run's environment there, and the preview warns about that with `inheritEnvironment: true`.

`usage.cost` is `0` for Ollama models, local or cloud, and the credential line says "no key" (or `OLLAMA_API_KEY from env`). With `inheritEnvironment: true` and an `OPENAI_API_KEY` in your shell, an `ollama` model sends that key to its endpoint, harmless for `localhost`; `inheritEnvironment: false` keeps it away.

### Amazon Bedrock and Google Vertex AI

- **Amazon Bedrock.** Pass a Bedrock API key as `AWS_BEARER_TOKEN_BEDROCK` (or the model's `tokenKey`), or AWS access keys as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`, with `AWS_REGION`, in `env`; `AWS_PROFILE` and `AWS_ROLE_ARN` with `AWS_WEB_IDENTITY_TOKEN_FILE` work too. Without credentials in `env` (and with `inheritEnvironment: false`, without them in the environment of the Pulumi CLI), the AWS SDK uses the machine's shared files, SSO, container credentials or instance metadata; the preview warns, and `AWS_EC2_METADATA_DISABLED: "true"`, `AWS_SHARED_CREDENTIALS_FILE: /dev/null` and `AWS_CONFIG_FILE: /dev/null` in `env` switch them off. A bearer token needs HTTPS, also for `providerOpts.endpoint_url`. docker-agent does not enforce `structuredOutput` on Bedrock, so set `mode: tool` (the preview warns).
- **Google Vertex AI.** Select Vertex AI with `providerOpts.project` and `providerOpts.location` on a `google` model, or with `GOOGLE_GENAI_USE_VERTEXAI: "true"`, `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` in `env`. Pass the path of a service account key file as `GOOGLE_APPLICATION_CREDENTIALS` in `env`; without it, the SDK uses the gcloud login of the machine (`gcloud auth application-default login`) or, on a Google Cloud machine, its metadata server, and the preview warns. A Pulumi ESC `gcp-login` provides an access token, which application default credentials do not read: Vertex AI through ESC's OpenID Connect is not supported.

### Docker Model Runner

A `dmr/<model>` model (for example `dmr/ai/qwen3`) needs no key. `AgentRun` connects to Docker Model Runner like `docker agent run`: when the `docker` CLI is on the `PATH` of the machine that runs `pulumi up`, through the Docker CLI's connection (the current context, or `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG` and the TLS variables), with `docker model status` finding the endpoint, so that command must work there (the Model Runner plugin). Without a `docker` CLI on the `PATH`, docker-agent probes the Model Runner's default endpoints itself, as before v0.2.0. `MODEL_RUNNER_HOST` in `env`, or the model's `baseUrl`, sets the endpoint instead and needs neither. A model that is not on the machine yet is pulled without asking: `pulumi up` shows "pulling the Docker Model Runner model ai/qwen3 (within timeoutSeconds)", the pull's output goes to the debug log, and the pull counts toward `timeoutSeconds`, so pull large models first (`docker model pull ai/qwen3`) or raise `timeoutSeconds`. `usage.cost` is `0`.

### model auto and DOCKER_AGENT_DEFAULT_MODEL

`model: auto` picks the first provider whose credential the run's environment (`env`, and with `inheritEnvironment: true` the environment of the Pulumi CLI) sets, in docker-agent's order: `anthropic`, `openai`, `chatgpt` (when the run's environment sets `CHATGPT_OAUTH_TOKEN`, or the configuration also names a chatgpt model, so that the ChatGPT login is used), `github-copilot`, `google`, `mistral`, `openrouter`, `baseten`, `ovhcloud`, `daoxe`, `groq`, `fireworks-ai`, `deepseek`, `cerebras`, `togetherai`, `huggingface`, `moonshotai`, `vercel`, `amazon-bedrock`, `opencode`, `opencode-go`, and otherwise Docker Model Runner (`dmr/ai/qwen3:latest`, or a model the machine already has, pulled within `timeoutSeconds` when it is missing). `DOCKER_AGENT_DEFAULT_MODEL` (or `CAGENT_DEFAULT_MODEL`) in `env`, or in the inherited environment, selects `provider/model` instead, as with `docker agent run`, whatever credentials are set; `DOCKER_AGENT_DEFAULT_MODEL: chatgpt/<model>` uses the ChatGPT login. The preview logs what `auto` resolves to, and warns when the run's environment does not set the credential of the model `DOCKER_AGENT_DEFAULT_MODEL` selects. In CI, an inherited `GITHUB_TOKEN` makes `auto` choose `github-copilot` when no other key is set: use `inheritEnvironment: false`, `DOCKER_AGENT_DEFAULT_MODEL` or a named model there. When the model of `auto` fails, the error says what `auto` resolved to and why ("model auto resolved to anthropic/claude-sonnet-5 (ANTHROPIC_API_KEY from env), which failed: ..."), and after a fallback to Docker Model Runner, what it looked for.

### UserSettings and AgentRun

`UserSettings` manages `~/.config/cagent/config.yaml` for the `docker agent` CLI. `AgentRun` reads none of it: each run gets private docker-agent config, data and cache directories, so the user settings, aliases, global `providers`, the default model, the models gateway and the credential helper, as well as the API keys that `docker agent setup` stores in `~/.config/cagent/.env`, do not apply. The ChatGPT login is the one exception (see [ChatGPT subscription](#chatgpt-subscription)). Declare custom providers under `providers` in the configuration (`AgentConfig.providers`), and pass keys in `env`.

### Claude Code credentials

A coding-harness agent (`harness: {type: claude-code}`, run with `allowHarnessAgents: true`) does not use docker-agent's model credentials: `AgentRun` starts the `claude` CLI, which authenticates itself. To run it on a Claude subscription without an API key, give it one of these logins:

- **The login of the machine.** Log in as the user that runs `pulumi up` with `claude auth login` (`--claudeai`, the Claude subscription, is the default), and check it with `claude auth status`. Claude Code keeps the login in the macOS keychain, or in `~/.claude/.credentials.json` on Linux. `AgentRun` passes `HOME` and `USER` to the CLI also with `inheritEnvironment: false`, so this works then too.
- **A long-lived token.** `claude setup-token` creates a token for your Claude subscription. Pass it as the `env` value `CLAUDE_CODE_OAUTH_TOKEN` from secret config, for example `env: { CLAUDE_CODE_OAUTH_TOKEN: config.requireSecret("claudeCodeOauthToken") }`. Use a token in CI, in SSH sessions (where the macOS keychain is locked), and when runs fail with "OAuth session expired and could not be refreshed", which non-interactive Claude Code reports when it cannot refresh the login of the machine. A token also avoids failures when another Claude Code process, such as your interactive session, refreshes that login at the same time. It is stored as a secret with `env` and redacted like every credential. Rotating it runs the agent again; until then, a `deletePrompt` uses the stored token.

In non-interactive mode, an `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` in the CLI's environment wins over `CLAUDE_CODE_OAUTH_TOKEN` and over the login, and bills API usage to it. `CLAUDE_CODE_OAUTH_TOKEN` wins over the login. `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX` and `CLAUDE_CODE_USE_FOUNDRY` route Claude Code to Amazon Bedrock, Google Vertex AI or Microsoft Foundry, and `ANTHROPIC_BASE_URL` sends its requests to another endpoint. docker-agent users often export `ANTHROPIC_API_KEY` for docker-agent's own Anthropic models, and `inheritEnvironment` defaults to `true`, so `AgentRun` protects the subscription:

- When `env` sets a non-empty `CLAUDE_CODE_OAUTH_TOKEN`, an inherited `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` is not passed to the CLI. A value set in `env` is passed, also an empty one: `env: { ANTHROPIC_API_KEY: "" }` blanks an inherited key while the CLI keeps the rest of the environment.
- With `inheritEnvironment: false`, the CLI gets only the system variables (`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `TMPDIR`, `TZ`, ...), the Claude configuration locations `CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME`, `IS_SANDBOX`, and `env`. The configuration locations are kept because they decide which keychain entry and credentials file Claude Code reads. Pass proxy and CA variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`, `NODE_EXTRA_CA_CERTS`) through `env`. This also keeps a `CLAUDE_CODE_OAUTH_TOKEN` exported in your shell from the CLI, which then uses the login of the machine: pass the token in `env` instead. The preview warns about that when `env` does not set the token (`env: { CLAUDE_CODE_OAUTH_TOKEN: "" }` keeps the login and silences the warning), and the line after the run and an authentication or login-refresh error say it too.
- The preview warns when the CLI would get one of the variables above that bill API usage or send the requests elsewhere (for a `yaml` configuration; a configuration pulled by `reference` gets the warning when it runs). The warning names the variable and where it comes from, never its value. After each run, unless `logging` is `none`, `pulumi up` shows `claude-code <version> (<model>), apiKeySource <source>`, the credential source that Claude Code reported. Any source other than `none` and `unknown` (`ANTHROPIC_API_KEY`, `apiKeyHelper`, `/login managed key`) bills API usage and adds a warning, also with `logging: none`. `none` only means that Claude Code used no API key, so the line adds what the CLI's environment selects instead, for example `apiKeySource none (the Claude login of this machine)`, `none (CLAUDE_CODE_OAUTH_TOKEN)`, `none (ANTHROPIC_AUTH_TOKEN)` or `none (Amazon Bedrock, CLAUDE_CODE_USE_BEDROCK)`, with `, through ANTHROPIC_BASE_URL` for a gateway. `ANTHROPIC_AUTH_TOKEN` and the cloud providers also add a warning.

Claude Code refuses `--dangerously-skip-permissions`, which docker-agent's harness driver passes, when it runs as root, as it often does in CI containers. Run `pulumi up` as a non-root user, or, in an isolated container, set `IS_SANDBOX=1` in `env`. The preview warns when the provider runs as root without it (for a `yaml` configuration known during the preview). [Coding-harness agents (Claude Code and Codex)](https://www.pulumi.com/registry/packages/docker-agent/#coding-harness-agents-claude-code-and-codex) on the overview page describes what the CLI runs and where.

### Codex credentials

A Codex agent (`harness: {type: codex}`, run with `allowHarnessAgents: true`) does not use docker-agent's model credentials either: `AgentRun` starts the `codex` CLI (tested with Codex 0.162; install it with `npm i -g @openai/codex` or `brew install codex`), which reads its login and `config.toml` from `CODEX_HOME` (default `~/.codex`). To run it on a ChatGPT plan without an API key:

- **A Codex home of its own (recommended).** Log in once into a directory that only `AgentRun` uses, as the user that runs `pulumi up`, and pass it in `env`:

  ```bash
  mkdir ~/.codex-agentrun
  CODEX_HOME=~/.codex-agentrun codex login        # codex login --device-auth without a browser
  CODEX_HOME=~/.codex-agentrun codex login status
  ```

  then `env: { CODEX_HOME: "/Users/<you>/.codex-agentrun" }` (an absolute path outside the temporary directory). This login is its own token family, so your interactive Codex and `AgentRun` never refresh the same login at once, and the runs do not pick up the MCP servers, hooks, profiles and trusted projects of your `~/.codex/config.toml`, and do not appear in your `codex resume` list. Never copy `auth.json` from another `CODEX_HOME`: both copies would rotate the same refresh token. A `CODEX_HOME` that does not exist, or is no directory, fails the preview.
- **The login of the machine.** Without `CODEX_HOME`, codex uses `~/.codex/auth.json` of the user that runs `pulumi up`, or the OS keyring when `cli_auth_credentials_store` in `config.toml` says so (locked in SSH sessions on macOS). A run can fail when your interactive Codex refreshes that login at the same time; the error then says to retry and recommends a Codex home of its own.

Codex picks its credential in this order: `CODEX_API_KEY` (bills API usage to that key; `OPENAI_ORGANIZATION` and `OPENAI_PROJECT` select the organization and project), `CODEX_ACCESS_TOKEN` (an access token of a ChatGPT Business or Enterprise workspace), workload identity (`OPENAI_IDENTITY_TOKEN_FILE` or `OPENAI_FEDERATION_RULE_ID`, set at all), then the stored login: a ChatGPT login (the plan), an API key stored by `codex login --with-api-key`, or Amazon Bedrock credentials. `OPENAI_API_KEY` is not used by Codex's built-in provider, so a key exported for docker-agent's own `openai` models does not switch Codex to API billing. A custom `model_provider` in `config.toml` decides the login and billing instead, unless it sets `requires_openai_auth = true`, which keeps Codex's own login and billing; `openai_base_url` sends the requests of the built-in provider, with Codex's login, to another endpoint. `AgentRun` reads `config.toml` like Codex: `/etc/codex/config.toml`, `$CODEX_HOME/config.toml`, at run time the `.codex/config.toml` files of a project that `config.toml` trusts (from the project root, the nearest `.git`, down to `workingDir`; without the keys Codex ignores there, such as `model_provider` and `openai_base_url`), and `/etc/codex/managed_config.toml`. `env: { CODEX_API_KEY: "" }` blanks an inherited key, and an inherited `CODEX_API_KEY` is not passed to codex when `env` sets `CODEX_ACCESS_TOKEN`. The preview warns when codex would bill API usage or a cloud account, and when it finds no login (`codex login`, `codex login --device-auth` on a machine without a browser).

When `forced_login_method` in Codex's `config.toml` (also `/etc/codex/config.toml` or `managed_config.toml`) does not allow the credential that codex would get, codex logs out: it deletes `auth.json` and the keyring entry, and exits. `AgentRun` refuses to start it instead, and the preview fails with "codex would delete the stored login of its CODEX_HOME (...)". The same holds when `forced_chatgpt_workspace_id` is set and codex would use `CODEX_ACCESS_TOKEN`: codex logs out when the token belongs to another workspace, or names none, and `AgentRun` cannot check the token's workspace. Workload identity with a `forced_login_method` other than `chatgpt` deletes nothing, but codex would not start, so the preview fails with "codex would refuse to start: ...". Log in again with the method `config.toml` requires, remove `forced_login_method`, pass no credential of the other kind in `env`, or give `AgentRun` a `CODEX_HOME` of its own.

With `inheritEnvironment: false`, codex gets the system variables, `CODEX_HOME`, `CODEX_CA_CERTIFICATE`, the proxy variables, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and `env`. Codex itself also loads `$CODEX_HOME/.env` (every variable but `CODEX_*`), which `inheritEnvironment` does not affect. In both cases, the markers of a parent Codex or Claude Code session (`CODEX_THREAD_ID`, `CODEX_SANDBOX`, `CLAUDECODE`, ...), the W3C trace context (`TRACEPARENT`, `TRACESTATE`, which Codex would forward to OpenAI) and an inherited `RUST_LOG` (at trace level Codex logs every command's environment) are removed unless `env` sets them, and `PWD` is the working directory. Codex passes its environment, `env` included, to the commands it runs, and the model sees what they print; the output is redacted in logs.

After each run, unless `logging` is `none`, `pulumi up` shows a line like `codex 0.162.1 (gpt-6.1-sol), sandbox workspace-write, auth the ChatGPT login of this machine (/Users/<you>/.codex-agentrun/auth.json, plan plus), CODEX_HOME /Users/<you>/.codex-agentrun, thread <id>, billed to the ChatGPT plan, not per token (API price about $0.0412)`. The login names `, through model_provider <id> of config.toml` or `, through openai_base_url <scheme://host> of config.toml` when `config.toml` sends it elsewhere, and the end of the line says how the usage is billed: on a ChatGPT plan, its estimate at API prices goes into the summary, not into `usage.cost`; billed per token, ", cost an API-price estimate"; or ", cost unknown (...)" when the model is not in the catalog or not known. `pulumi up` warns when the run billed API usage or another account (`CODEX_API_KEY`, an API-key or Bedrock login, a `model_provider` without `requires_openai_auth`) or when the installed Codex is not the tested release. Codex keeps a transcript of every turn, with every command's output, unredacted in `$CODEX_HOME/sessions/`, where `codex resume` shows it: another reason for a Codex home of its own.

### AgentRun progress and time limits

`logging` controls what `pulumi up` shows while an `AgentRun` runs. `status` (the default) shows the run's progress as status messages that the next one replaces (an agent started, a task was transferred or the conversation handed off, each tool call and whether it was approved or rejected, and why, a model that fell back to the next one), the credential line, and ends with one summary line: duration, tool calls, input and output tokens and the estimated per-token cost, or why there is none, with the plans that paid for the rest (see [Costs](#costs)). `tools` also logs every tool call with its arguments, and `full` also logs the agents' messages and the tools' results. `none` shows nothing. Messages are redacted like errors (`env` values, secret inputs, credential-named environment variables and `tokenKey` variables). When the answer is secret (`outputSecret`, or a secret `prompt`, `updatePrompt` or `yaml`), `tools` and `full` log no arguments, messages or results, and with a secret `yaml` or `agent` the messages name no agents, tools or models. Changing `logging` updates the state without running the agent again. For a coding-harness agent, the progress shows the tools that Claude Code or Codex reports it ran as "run by the harness" (or "run by the harness, failed") instead of approved or rejected.

`AgentRun` has two time limits. `timeoutSeconds` (default 300) limits the agent run itself, from loading the configuration (starting MCP servers, pulling a Docker Model Runner model) to the final answer: when it runs out, the run is cancelled and the create, update or delete fails with "the agent did not finish within ... (timeoutSeconds)". The [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/) resource option (`create`, `update`, `delete`) limits the whole provider operation, including pulling a `reference` and waiting for another `AgentRun` of the same provider process to finish (runs execute one at a time); when it runs out, Pulumi cancels the operation and the run fails with "the operation's time limit (customTimeouts) ran out before the agent finished". A cancelled operation, for example after Ctrl-C, fails with "the agent run was canceled", and when Pulumi stops the provider (SIGINT), the run stops within about 2 seconds with "the agent run was stopped because Pulumi is stopping the provider (SIGINT)". The shorter limit wins. Use `customTimeouts` to bound the deployment step as a whole and keep `timeoutSeconds` as the per-run safety limit. Changing `timeoutSeconds` alone does not run the agent again.

When a run ends, `AgentRun` stops every process it left running, for example a server that a `shell` tool started in the background, and warns: "the agent run left 1 process running when it ended (for example a command a tool ran in the background); AgentRun stopped it". It finds them by the run child's process group and by a random value in `DOCKER_AGENT_AGENTRUN_COOKIE` that every process of the run inherits.

A coding-harness agent works longer than a single model answer: allow 900 to 1800 seconds for coding tasks. While the `claude` or `codex` CLI runs, `AgentRun` records the processes started under it, also the shell commands that the CLI runs in sessions of their own, and the CLI gets a random value in `DOCKER_AGENT_RUN_COOKIE`, which these processes inherit, so that `AgentRun` also finds the ones whose parent exited before it saw them. When a limit runs out or the operation is cancelled, `AgentRun` stops the CLI (Codex first gets SIGINT, which ends its turn cleanly, and 2 seconds later SIGTERM), sends SIGTERM to the CLI's process group and to those processes, keeps reading the CLI's output while it shuts down, and sends SIGKILL to what is left 10 seconds later, or 1 second after the CLI exited if that comes first. After the CLI exited, what it left running (for example a command it ran in the background) gets SIGTERM and, 1 second later, SIGKILL, and `pulumi up` warns how many processes outside the CLI's process group were stopped (MCP servers that the CLI was still shutting down are stopped without a warning); the output is read for at most 2 more seconds. A process that left the CLI's process group and session, whose parent exited before the process table was read (about every 0.2 seconds), and whose environment does not show that value (started with `env -i`, or, on macOS, a program whose environment the system does not show to other processes, as for some signed programs under System Integrity Protection) can outlive the run, and so can everything when the provider itself is killed.

## Validation errors

`AgentConfig` validates the rendered configuration with docker-agent during preview and reports every independent problem at once, each attached to the input that caused it, for example `property agents[0].subAgents ... has a problem: invalid docker-agent configuration: agent 'root' references non-existent sub-agent 'missing'`. docker-agent stops at the first problem; the provider removes the faulty value and validates again to find the rest. A problem with a whole definition (a model, provider or toolset) is reported alone. Enum values outside the allowed set fail on their property in the same preview, and `skipValidation` turns both checks off. `UserSettings` problems carry their property too, and `AgentArtifact`, `AgentRun` and `validateAgentConfig` report every problem of a raw `yaml`.

The preview also warns, without failing, about `headers` and `env` values of an `AgentConfig` that look like literal credentials (use a `${env.NAME}` placeholder instead) and about an `AgentArtifact` whose `yaml` is secret: the artifact is pushed in plaintext.

## Compatibility

Each provider release pins one docker-agent version (the `dockerAgentVersion` output of `AgentConfig` and `AgentArtifact`), validates configurations with that version's loader, and writes it into the `io.docker.agent.version` and `io.docker.cagent.version` manifest annotations that `docker agent share pull` requires.

docker-agent releases often and adds fields to its latest configuration version, the `version` that `AgentConfig` writes by default. If you use a field that the pinned version does not know yet, validation reports `unknown field`. You can:

- upgrade the provider, or
- set `skipValidation: true` on the resource, or `docker-agent:skipValidation` (or `DOCKER_AGENT_SKIP_VALIDATION=true`) for the whole stack, and
- use the `extra` input that every `AgentConfig` object has (the configuration, agents, models, providers, toolsets, hooks and so on) to add fields that the pinned version does not model yet. Keys in `extra` are written to the YAML as is (snake_case). Every field of the pinned version is a typed input.

Closed value sets of the configuration, such as the `safety` modes, a hook's `type` and `on_error`, a provider's `api_type` or a harness `type`, are enums with named constants in every SDK (`SafetyMode.Restricted`, `dockeragent.SafetyModeRestricted`, ...). A value that the pinned version does not list yet fails on its property during preview; with `skipValidation` it is accepted. YAML and HCL take any string, and TypeScript, Python and Go take other values with a cast or conversion (for example `dockeragent.SafetyMode("<new-mode>")` in Go), but C# and Java enums cannot hold other values: set such a value through the `extra` map of the object instead, for example `runtime: { extra: { safety: "<new-mode>" }, ... }`, which replaces the typed key in the YAML.

`instructionFile` (`instruction_file`) only works with `skipValidation`, because configurations are validated in memory, where relative files cannot be read. Read the file in your program and pass its contents as `instruction`, which is also what `docker agent share push` does when it publishes an agent.
