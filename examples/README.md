# Examples

Every example deploys the same scenario, so you can compare the languages side by side:

1. An `AgentConfig` with a `root` coordinator agent and a `researcher` sub-agent that uses the
   DuckDuckGo MCP server from the Docker MCP Catalog (`ref: docker:duckduckgo`). Both agents use a
   named Anthropic model (`claude`) whose API key is read from `AGENT_ANTHROPIC_API_KEY`
   (`tokenKey`). The configuration also has a `metadata` block.
2. An `AgentArtifact` that pushes the configuration to `<registry>/examples/<language>-agent:v1`, like
   `docker agent share push`. `registry` is a stack setting that defaults to `localhost:5000`. The
   examples set `deleteBehavior: delete`, so `pulumi destroy` also removes the artifact.
3. `getAgent`, which reads the artifact back by digest, like `docker agent share pull`.
4. `validateAgentConfig`, which checks the hand-written `agent.yaml` next to the program.

In HCL these are `resource "docker-agent_agent_config"`, `resource "docker-agent_agent_artifact"`,
`data "docker-agent_agent"` and `data "docker-agent_validate_agent_config"` (see [`hcl/`](hcl)).

| Directory | Language |
|---|---|
| [`yaml/`](yaml) | Pulumi YAML |
| [`nodejs/`](nodejs) | TypeScript |
| [`python/`](python) | Python |
| [`go/`](go) | Go |
| [`dotnet/`](dotnet) | C# |
| [`hcl/`](hcl) | Pulumi HCL (`runtime: hcl`, Terraform-style `.tf` files) |

[`docker-serve-ts/`](docker-serve-ts) goes one step further: it combines the provider with the
[Pulumi Docker provider](https://www.pulumi.com/registry/packages/docker/) to run its own `registry:2`
on a user-defined network, push the agent there, and serve it with `docker agent serve api` in a
`docker/docker-agent` container that has a health check on `/health`.

[`user-settings-yaml/`](user-settings-yaml) covers the two configuration files that are not agent
definitions: `UserSettings` writes Docker Agent's user configuration (`~/.config/cagent/config.yaml`:
settings, aliases, global providers, the default model, the sandbox allowlist) and `AgentsIgnore` writes a
`.agentsignore` file. It writes into a scratch directory (`outputDir`, default `out/`) and needs no registry.

[`agent-run-yaml/`](agent-run-yaml) runs an agent during `pulumi up` with `AgentRun`: a change-review agent
with structured output answers a prompt, and the program exports its `risk` and `summary`. The model provider is
any OpenAI-compatible endpoint (`baseUrl` stack setting, default OpenAI) and the API key is the secret `apiKey`
setting. `make test_examples` runs it against a fake OpenAI-compatible server, so it needs no API key and no registry.

The language examples export the same outputs:

| Output | Value |
|---|---|
| `digest` | The manifest digest (`sha256:...`) |
| `pinnedReference` | `<registry>/examples/<language>-agent@sha256:...` |
| `runCommand` | `docker agent run <pinnedReference>` |
| `requiredEnvVars` | The environment variables the agent needs where it runs (`AGENT_ANTHROPIC_API_KEY`) |
| `agentYaml` / `pulledYaml` | The pushed YAML and the YAML that `getAgent` read back. They are identical. |
| `pulledAgents`, `pulledMetadata` | Agent names and metadata from `getAgent` |
| `rawConfigValid`, `rawConfigAgents`, `rawConfigErrors` | The result of `validateAgentConfig` for `agent.yaml` |

## Running an example

The examples use the provider and the SDKs built from this repository: each `Pulumi.yaml` points
`plugins.providers` at `../../bin`, and each program references the local SDK build (see the example's
README). To use the published packages instead, follow the "Outside this repository" notes.

```bash
# From the repository root: a local registry (deletes enabled for deleteBehavior: delete),
# the provider binary and the SDK of your language.
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider build_nodejs        # or build_python / build_dotnet; Go, YAML and HCL need no SDK build

cd examples/nodejs
export PULUMI_BACKEND_URL="file://$HOME/.pulumi-local" PULUMI_CONFIG_PASSPHRASE=local
pulumi stack init dev
pulumi install --no-plugins       # dependencies only; the plugin comes from ../../bin
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up
```

`pulumi install --no-plugins` installs the program's dependencies without trying to download the
plugin version that the local SDK build declares (a development version that has no release).

Then run the agent with the real CLI:

```bash
export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"
```

Clean up with `pulumi destroy`, `pulumi stack rm dev` and `docker rm -f agent-registry`.

## Tests

[`examples_test.go`](examples_test.go) runs every example with
[pulumitest](https://github.com/pulumi/providertest): `up`, `preview` (expects no changes),
`refresh` (expects no changes) and `destroy`, using a `file://` backend in a temp dir. It checks the
outputs (the rendered YAML must equal [`testdata/research-assistant.yaml`](testdata/research-assistant.yaml)
in every language, and `getAgent` must return it unchanged), reads the manifest straight from the
registry, and checks that `destroy` deleted it. When the `docker-agent` CLI is on `PATH`, it also runs
`docker-agent share pull` and `docker-agent debug config` against each pushed artifact.
`TestExampleDockerServe` runs `docker-serve-ts` (it needs Docker and uses its own registry) and checks
`/health` and `/api/agents` of the served agent. `TestExampleUserSettings` runs `user-settings-yaml`
with `outputDir` in a temp dir: it compares the written files with [`testdata/user-settings.yaml`](testdata/user-settings.yaml) and
[`testdata/agentsignore`](testdata/agentsignore), checks their modes, lists the aliases with the
`docker-agent` CLI when it is on `PATH`, checks that `refresh` reports an edited file as drift and `up`
reverts it, and that `destroy` removes both files.

```bash
make test_examples                                   # builds everything, starts a throwaway registry:2
make test_examples EXAMPLES_RUN=TestExampleGo        # one language
DOCKER_AGENT_TEST_REGISTRY=localhost:5000 make test_examples   # reuse a running registry
```
