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

Declare the `provider` block from the start: adding it to a stack that already uses the default provider moves the resources to the new provider, which replaces them. A runnable HCL program is in [`examples/hcl`](https://github.com/dirien/pulumi-docker-agent/tree/main/examples/hcl).

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
- **Model keys for `AgentRun`, through `env`.** Read the key from configuration (`anthropicApiKey` above) as a secret and pass it to `env` as described in [AgentRun credentials](#agentrun-credentials), preferably with `inheritEnvironment: false`. The key is stored encrypted in the state, and credentials and `${env.NAME}` references resolve only from `env`.
- **Model keys for `AgentRun`, through the environment.** `pulumi preview`, `pulumi up` and the other stack commands export the environment's `environmentVariables` to the Pulumi CLI and the plugins it starts. With the default `inheritEnvironment: true`, `AgentRun` resolves `ANTHROPIC_API_KEY` from there without an `env` input, and nothing about the key is stored in the state. The processes the configuration starts (tools, hooks, MCP and LSP servers) inherit every exported variable too.
- **Amazon Bedrock without static keys.** `fn::open::aws-login` exchanges an OpenID Connect token of Pulumi Cloud for temporary credentials of an IAM role that may call `bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream` (see [Configuring OpenID Connect for AWS](https://www.pulumi.com/docs/esc/guides/configuring-oidc/aws/)). Export them as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`, with `AWS_REGION`, for `amazon-bedrock` models. docker-agent signs Bedrock requests with the AWS SDK's default credential chain, which reads the environment of the provider process (and the shared AWS configuration files) whatever `inheritEnvironment` is set to, so these credentials come from `environmentVariables`, not from `env`. A Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK` can be passed in `env` instead.
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

docker-agent's other credential sources are never used: its `~/.config/cagent/.env` file, the credential helper of the user settings, Docker Desktop secrets and `op://` 1Password references. With `inheritEnvironment: false`, a run depends on nothing but its inputs. `OPENAI_BASE_URL` (and the other base-URL variables the model SDKs read) also comes from the environment of the Pulumi CLI; prefer a custom provider with `baseUrl` in the configuration. Likewise, `amazon-bedrock` models sign their requests with the AWS SDK's default credential chain, which reads the standard AWS variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_PROFILE`, ...) of the Pulumi CLI and the shared AWS configuration files whatever `inheritEnvironment` is set to. Only a Bedrock API key (`AWS_BEARER_TOKEN_BEDROCK` or the model's `tokenKey`) and `AWS_REGION` follow `env` and `inheritEnvironment` like other credentials.

The processes a configuration starts (command hooks, `shell`, `script` and `background_jobs` tools, MCP and LSP servers) inherit the environment of the Pulumi CLI by default, and command hooks also receive `env`. With `inheritEnvironment: false`, AgentRun starts them with the system variables only (`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `TMPDIR`, `TZ`, ...) plus their toolset's own `env`, and command hooks with the system variables plus `env`. A toolset passes a credential to its process explicitly, for example `env: {GITHUB_TOKEN: "${env.GITHUB_TOKEN}"}`. `docker:` MCP servers (the Docker MCP Gateway), filesystem `post_edit` commands, the `open_url` launcher and docker-agent's on-demand tool installation always inherit the whole environment. [Where tools run](https://www.pulumi.com/registry/packages/docker-agent/#where-tools-run) on the overview page has the details.

An invalid key fails the update with the model's error message. The provider redacts the values of `env`, of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...) and of the variables the configuration names as `token_key` from errors, warnings and logs. When the agent's answer contains one of these values, its `output` and `structuredOutput` are stored as secrets.

### AgentRun progress and time limits

`logging` controls what `pulumi up` shows while an `AgentRun` runs. `status` (the default) shows the run's progress as status messages that the next one replaces (an agent started, a task was transferred or the conversation handed off, each tool call and whether it was approved or rejected, and why, a model that fell back to the next one) and ends with one summary line: duration, tool calls, input and output tokens and the estimated cost. `tools` also logs every tool call with its arguments, and `full` also logs the agents' messages and the tools' results. `none` shows nothing. Messages are redacted like errors (`env` values, secret inputs, credential-named environment variables and `tokenKey` variables). When the answer is secret (`outputSecret`, or a secret `prompt` or `yaml`), `tools` and `full` log no arguments, messages or results, and with a secret `yaml` or `agent` the messages name no agents, tools or models. Changing `logging` updates the state without running the agent again.

`AgentRun` has two time limits. `timeoutSeconds` (default 300) limits the agent run itself: when it runs out, the run is cancelled and the create or update fails with "the agent did not finish within ... (timeoutSeconds)". The [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/) resource option (`create`, `update`) limits the whole provider operation, including pulling a `reference` and waiting for another `AgentRun` of the same provider process to finish (runs execute one at a time); when it runs out, Pulumi cancels the operation and the run fails as cancelled. The shorter limit wins. Use `customTimeouts` to bound the deployment step as a whole and keep `timeoutSeconds` as the per-run safety limit. Changing `timeoutSeconds` alone does not run the agent again.

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
