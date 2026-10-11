# Docker Agent example (Pulumi HCL)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/hcl-agent:v1`, reads it back with the
`docker-agent_agent` data source, and validates the hand-written [`agent.yaml`](agent.yaml) with the
`docker-agent_validate_agent_config` data source. It renders exactly the same YAML as the other
examples. See [the examples overview](../README.md) for the scenario and the outputs.

[Pulumi HCL](https://www.pulumi.com/docs/iac/languages-sdks/hcl/) runs Terraform-style `.tf` files
(`runtime: hcl` in `Pulumi.yaml`). The Pulumi CLI downloads the HCL language plugin on first use;
this example was tested with pulumi-hcl v0.18.4 and Pulumi CLI 3.268.0.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider

cd examples/hcl
pulumi stack init dev
pulumi install --no-plugins                 # fetches the HCL language plugin; the provider comes from ../../bin
pulumi config set registry localhost:5000   # optional, this is the default of the `registry` variable
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.
HCL variables are Pulumi config values, so `var.registry` is set with `pulumi config set registry ...`
(or `TF_VAR_registry`, or a `terraform.tfvars` file).

## How the provider maps to HCL

| Pulumi | HCL |
|---|---|
| `docker-agent:index:AgentConfig` | `resource "docker-agent_agent_config"` |
| `docker-agent:index:AgentArtifact` | `resource "docker-agent_agent_artifact"` |
| `docker-agent:index:getAgent` | `data "docker-agent_agent"` (the `get` prefix is dropped) |
| `docker-agent:index:validateAgentConfig` | `data "docker-agent_validate_agent_config"` |
| provider configuration | `provider "docker-agent" { ... }` |

- Property names are snake_case: `subAgents` is `sub_agents`, `maxTokens` is `max_tokens`,
  `pinnedReference` is `pinned_reference`, `deleteBehavior` is `delete_behavior`.
- Lists of objects are repeated nested blocks: one `agents { ... }` block per agent (in order), one
  `toolsets { ... }` block per toolset inside an agent, and `registry_auth { ... }` blocks in the
  provider block. Attribute syntax (`agents = [...]`) is rejected for these.
- Maps and single objects are attributes: `models = { claude = { ... } }`, `metadata = { ... }`,
  `extra = { ... }`.
- The hyphen in `docker-agent` is part of the HCL identifier, so references look like
  `docker-agent_agent_config.assistant.yaml` and `data.docker-agent_agent.pulled.agent_names`.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml`, pin the provider version in `required_providers`
(`version = "0.1.0"`; Pulumi providers take an exact version), and install the released plugin. The
`pulumi/docker-agent` source names the package but not where to download it from, and the plugin is
published on GitHub, so install it once:

```bash
pulumi plugin install resource docker-agent 0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi install
```

To let teammates and CI install it with a plain `pulumi install`, run `pulumi package add docker-agent@0.1.0`
once after the plugin install. It writes `sdks/dirien-docker-agent/hcl.sdk.json`, a descriptor that records
the GitHub download URL. Commit that file. This example does not commit one: it would make `pulumi install`
download the released plugin instead of using the local build.
