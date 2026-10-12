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
| [`java/`](java) | Java (Gradle) |
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

[`agent-run-claude-code-ts/`](agent-run-claude-code-ts) runs Claude Code during `pulumi up` on the Claude subscription
of the machine (no API key): an `AgentConfig` with a `claude-code` harness, and two `AgentRun`s of it with
`allowHarnessAgents: true`. The first maintains `NOTES.md` in `workspace/` with a create, an update and a delete
prompt; the second reviews its answer and runs again whenever the first one ran. Claude Code runs its own tools
without approval, so read the example's README before you run it. `make test_examples` runs it with a fake `claude`
on a restricted `PATH`.

[`esc-yaml/`](esc-yaml) takes every credential from a Pulumi ESC environment ([`environment.yaml`](esc-yaml/environment.yaml)):
`pulumiConfig` sets `docker-agent:registryAuth` and the program settings, `environmentVariables` export
`ANTHROPIC_API_KEY` and AWS credentials from `fn::open::aws-login` for an `amazon-bedrock` agent. It needs Pulumi
Cloud; `make test_examples` previews it with plain stack configuration and checks the environment with the Pulumi
Cloud API only when `PULUMI_ACCESS_TOKEN` is set. CI sets the token only in a separate job on pushes to `main`, never
for pull requests.

[`automation-ts/`](automation-ts) runs a change-review agent from a TypeScript Automation API script (inline
program, file backend, `stack.outputs()`, destroy) and exits with 1 when the agent asks for changes.
`make test_examples` runs it against the fake OpenAI-compatible server.

[`testdata/policy-violations/`](testdata/policy-violations) violates every policy of the policy pack in
[`../policy`](../policy); `make test_examples` previews it, and `yaml/`, with `--policy-pack`.
[`testdata/agent-run-claude-code/`](testdata/agent-run-claude-code),
[`testdata/agent-run-upgrade/`](testdata/agent-run-upgrade),
[`testdata/agent-run-provider-upgrade/`](testdata/agent-run-provider-upgrade) and
[`testdata/agent-run-provider-config/`](testdata/agent-run-provider-config) are fixtures of the Claude Code, upgrade
and provider settings tests below.

[`docker-agent-examples/`](docker-agent-examples) has every example configuration that docker-agent ships as a
Pulumi program in TypeScript and in Python: one `AgentConfig` per example, with typed inputs and the comments of
the upstream file. The programs, their README and the golden
YAML files are generated by `make generate_config_types` from the pinned docker-agent release; only the project
files (`Pulumi.yaml`, `package.json`, `tsconfig.json`, `.npmrc`, `requirements.txt`) are written by hand. Each
project exports one stack output, `configs`, which maps every example name to its rendered YAML. They need no
registry.

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
make provider build_nodejs        # or build_python / build_dotnet; Go, Java, YAML and HCL need no SDK build

cd examples/nodejs
export PULUMI_BACKEND_URL="file://$HOME/.pulumi-local" PULUMI_CONFIG_PASSPHRASE=local
pulumi stack init dev
pulumi install --no-plugins       # dependencies only; the plugin comes from ../../bin
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up
```

`pulumi install --no-plugins` installs the program's dependencies without trying to download the
plugin version that the local SDK build declares (a development version that has no release).
On macOS, AirPlay Receiver may already use port 5000: map another port (for example `-p 5001:5000`) and
run `pulumi config set registry localhost:5001`.

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
reverts it, and that `destroy` removes both files. `TestExampleAgentRun` runs `agent-run-yaml` against a fake
OpenAI-compatible server and checks the structured answer, that the key is stored encrypted, that `preview` and
`refresh` do not run the agent again, that a new trigger does, and that `rerun: always` runs it on every `up`
(also `up --refresh`) while `refresh --expect-no-changes` passes. `TestExampleAgentRunClaudeCode` runs
`agent-run-claude-code-ts` with the fake `claude` of
[`provider/pkg/agentrun/testdata/fake-claude`](../provider/pkg/agentrun/testdata/fake-claude) and checks how the CLI
was started (prompt on stdin, working directory, environment), the outputs, the update prompt under
`rerun: always` and the delete prompt on `destroy`. `TestDockerAgentExamplesTypeScript` and
`TestDockerAgentExamplesPython` run the two projects of `docker-agent-examples`: the TypeScript one is
type-checked with `tsc --noEmit` (strict) first, then both run `preview` and `up`, the names in `configs` must
equal the files in [`docker-agent-examples/testdata`](docker-agent-examples/testdata) and every value must be
byte-identical to its file, a second `preview` must show no changes, and `destroy` removes the stack.

The other tests in this directory:

| File | Tests |
|---|---|
| [`java_test.go`](java_test.go) | `TestExampleJava` runs `java/` like the other languages, against the Java SDK sources in `sdk/java`. |
| [`docs_examples_test.go`](docs_examples_test.go) | `TestDocsExamples` previews every program of `docs/yaml`; `TestDocsSnippetsGo` and `TestDocsSnippetsTypeScript` compile the generated Go and TypeScript examples. |
| [`import_test.go`](import_test.go) | `TestImportTypeScript`, `TestImportPython` and `TestImportYAML` run `pulumi import` for an `AgentConfig`, a `UserSettings` and an `AgentsIgnore` (and, in YAML, an `AgentArtifact`), write the generated code into a program and check that the next preview shows no changes. |
| [`convert_test.go`](convert_test.go) | `TestConvertDockerAgentExamples` converts every upstream docker-agent example with `pulumi convert --from docker-agent`, runs the program and compares each configuration with its golden file; `TestConvertLanguages` compiles a selection of conversions in TypeScript, Python, Go, C#, Java and HCL. |
| [`esc_test.go`](esc_test.go) | `TestExampleESCProgram` previews `esc-yaml/`; `TestExampleESCEnvironment` checks its environment with the Pulumi Cloud API when `PULUMI_ACCESS_TOKEN` is set. |
| [`automation_test.go`](automation_test.go) | `TestAutomationAPI` runs `automation-ts/` against the fake OpenAI-compatible server. |
| [`policy_test.go`](policy_test.go) | `TestPolicyPack` previews `testdata/policy-violations` and `yaml/` with the policy pack. |
| [`harness_test.go`](harness_test.go) | `TestExampleAgentRunClaudeCode` (above), and the failure paths of a Claude Code `AgentRun` with `testdata/agent-run-claude-code` and the fake `claude`: redaction of the token (`TestAgentRunClaudeCodeRedaction`), an expired login (`TestAgentRunClaudeCodeFailure`), a timeout that stops the CLI with SIGTERM before SIGKILL (`TestAgentRunClaudeCodeTimeout`), and a failing delete prompt with the `DOCKER_AGENT_SKIP_DELETE_PROMPTS` escape hatch (`TestAgentRunClaudeCodeDeleteEscapeHatch`). Linux and macOS only. |
| [`upgrade_test.go`](upgrade_test.go) | Provider version upgrades, the way SDK programs do them: the programs pin the version of each resource (`options.version`), so the default provider changes, and the versions are plugins in a `PULUMI_HOME` of the test's own. `TestAgentRunUpgradeFromV011E2E` deploys `testdata/agent-run-upgrade` with the released v0.1.1 provider (downloaded from its GitHub release), upgrades to the version of the provider in `bin/` and expects `pulumi preview --expect-no-changes` to pass, no changes in refresh and up, and no new run. `TestAgentRunProviderUpgradeE2E` upgrades `testdata/agent-run-provider-upgrade` (a `deletePrompt` in a `workingDir`) between two versions of `bin/` and expects neither a rerun nor the delete prompt until `pulumi destroy`. Set `DOCKER_AGENT_TEST_SKIP_UPGRADE=1` to skip `TestAgentRunUpgradeFromV011E2E` offline. |
| [`provider_config_test.go`](provider_config_test.go) | Provider settings changes on `testdata/agent-run-provider-config` (an `AgentArtifact` and an `AgentRun` with a `deletePrompt`), with a proxy in front of the registry that needs basic authentication: `TestProviderSettingsUpdateInPlaceE2E` rotates the `registryAuth` password and runs one command with `DOCKER_AGENT_SKIP_VALIDATION=true`, `TestProviderSettingsAddRegistryAuthE2E` adds `registryAuth` together with an artifact that needs it. Both expect the provider to be updated in place (same ID, no resource replaced, no new run) and check in the gRPC log that Pulumi configured it with the new settings. |

```bash
make test_examples                                   # builds everything, starts a throwaway registry:2
make test_examples EXAMPLES_RUN=TestExampleGo        # one language
DOCKER_AGENT_TEST_REGISTRY=localhost:5000 make test_examples   # reuse a running registry
```
