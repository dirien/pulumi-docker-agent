# Docker Agent policy pack

A [Pulumi Policies](https://www.pulumi.com/docs/discovery-governance/concepts/policy-as-code/) policy pack
(`docker-agent-policies`) for the Docker Agent provider. It checks the inputs of `AgentConfig`, `AgentRun`, `AgentArtifact` and `UserSettings`
resources during `pulumi preview` and `pulumi up`, in every Pulumi language.

| Policy | Default | Checks |
|---|---|---|
| `agentrun-tool-approval-deny` | mandatory | `AgentRun` keeps `toolApproval: deny` (the default) and sets neither `allowHookApprovals` nor `allowHarnessAgents`. `allow` runs every tool call of the agent on the machine that runs `pulumi up`; `allowHookApprovals` lets a `pre_tool_use` or `permission_request` hook of the configuration approve any tool call under `deny`; `allowHarnessAgents` runs coding-harness agents (Claude Code), whose CLI runs every tool call (shell commands, file edits) without approval, whatever `toolApproval` says. Resources in `allowedResources` are exempt. Implements `remediateResource`. |
| `agentrun-reference-pinned` | advisory | `AgentRun.reference` is pinned by digest (`registry/repository@sha256:...`). |
| `agentconfig-no-autonomous-safety` | mandatory | Neither `runtime.safety` nor an agent's `safety` is `autonomous`. |
| `permissions-no-wildcard-allow` | mandatory | `permissions.allow` of an `AgentConfig` and the global `settings.permissions.allow` of `UserSettings` do not approve every tool (`*`, `?*`, also with argument conditions) or every call of a tool in `dangerousTools` (default `shell`, `run_background_job`, `write_file`, `edit_file`, `create_directory`, `remove_directory`, `run_background_agent`): a pattern such as `shell`, `sh*`, `shell:cmd=*` or `*_file`. docker-agent runs such calls without asking, also under `AgentRun`'s `toolApproval: deny`. Rules with argument conditions (`shell:cmd=git status*`) and other tools (`read_*`) pass. Patterns in `allowedPatterns` and resources in `allowedResources` are exempt. |
| `usersettings-no-autonomous-safety` | mandatory | `UserSettings` sets neither `settings.safety: autonomous` nor `settings.yolo: true`, and no alias sets `safety: autonomous` or `yolo: true`. These settings beat the safety mode of every agent configuration the user runs with `docker agent run`. |
| `agentconfig-sandbox-for-local-tools` | advisory | Agents with `shell`, `script`, `filesystem` (`file`) or `background_jobs` toolsets, their own or shared through `useToolsets`, have `runtime.sandbox: true`. |
| `agentconfig-no-literal-credentials` | mandatory | `env` and `headers` maps (toolsets, MCP definitions, `remote`, hooks, `providerOpts.http_headers`, `extra`) hold `${env.NAME}` references instead of literal credentials, and `tokenKey` names an environment variable. |
| `agentconfig-allowed-model-providers` | disabled | Every model provider is in `allowedProviders`: `models.*.provider` and `provider/model` references (`model`, `compactionModel`, `titleModel`, `fallback.models`, toolset and hook models, RAG models, alloy models). Custom providers count as their `provider` type (default `openai`). |
| `agentconfig-allowed-mcp-refs` | disabled | MCP toolsets and `mcps` definitions use a `ref` in `allowedRefs`, a `remote.url` in `allowedRemoteUrls`, and `command` only with `allowCommand: true`. |
| `agentartifact-allowed-registries` | disabled | `AgentArtifact.reference` (push) and `AgentRun.reference` (pull) are in `allowedRegistries`: a host (`ghcr.io`), a repository prefix (`ghcr.io/my-org`) or a wildcard (`*.azurecr.io`). Docker Hub is `docker.io`. |

The literal-credentials check flags values whose name contains `authorization`, `api_key`, `token`, `secret`,
`password`, `credential`, `access_key`, `private_key` or `cookie` and that keep 8 or more characters once the
`${...}` placeholders and a leading `Bearer`/`Basic`/`Token` are removed, and values of any name that look like
well-known keys (`sk-...`, `ghp_...`, `github_pat_...`, `glpat-...`, `xox?-...`, `AKIA...`, `AIza...`, `hf_...`,
literal `Bearer`/`Basic` credentials). A Pulumi secret does not make such a value safe: `AgentConfig` writes it into
its `yaml` output and every artifact pushed from it. Pass credentials as `${env.NAME}` and provide the variable where
the agent runs, for example in `AgentRun`'s secret `env` input or a Pulumi ESC environment.

`runtime.sandbox` only applies to `docker agent run`. `AgentRun` runs the agent inside the provider process and
does not use a sandbox, which is what `agentrun-tool-approval-deny` guards. Add an `AgentRun` that runs Claude Code
on purpose, for example on a disposable CI runner, to `allowedResources`.

Violation messages name where a problem is (a resource, an agent, a toolset, a list index) and never repeat a
value that may hold a credential: a remote MCP URL shows only its scheme and host, an MCP `command` and a
permission rule's argument values are not shown, and registry policies name only the registry host of a reference.

Values that are unknown during `pulumi preview` (outputs of resources that are not created yet) are skipped in the
preview and checked during `pulumi up`. The allowlist policies fail closed: enabled without a list, they allow
nothing.

## Use it locally

The pack depends on the Node.js SDK built from this repository (`file:../sdk/nodejs/bin`, copied into
`node_modules` because `.npmrc` sets `install-links=true`), because no release of the SDK is on npm yet. Once
0.1.0 is published, `package.json` should depend on `@ediri/pulumi-docker-agent@^0.1.0` instead, which a published
pack needs anyway (see [Publish it to Pulumi Cloud](#publish-it-to-pulumi-cloud)).

```bash
# From the repository root
make provider build_nodejs
cd policy && npm install && cd ..

# Run the policies against any program, in any language
cd examples/yaml
pulumi preview --policy-pack ../../policy
pulumi up --policy-pack ../../policy
```

A mandatory violation fails the preview or update. Policies are configured with a JSON file, keyed by policy name.
A policy's value is an enforcement level (`advisory`, `mandatory`, `remediate` or `disabled`) or an object with
`enforcementLevel` and the policy's settings:

```json
{
    "agentrun-tool-approval-deny": {
        "enforcementLevel": "remediate",
        "allowedResources": ["sandboxed-*"]
    },
    "agentconfig-sandbox-for-local-tools": "mandatory",
    "agentconfig-allowed-model-providers": {
        "enforcementLevel": "mandatory",
        "allowedProviders": ["anthropic", "amazon-bedrock"]
    },
    "agentconfig-allowed-mcp-refs": {
        "enforcementLevel": "mandatory",
        "allowedRefs": ["docker:duckduckgo", "docker:github-official"],
        "allowedRemoteUrls": ["https://mcp.example.com/*"],
        "allowCommand": false
    },
    "agentartifact-allowed-registries": {
        "enforcementLevel": "mandatory",
        "allowedRegistries": ["ghcr.io/my-org"]
    },
    "permissions-no-wildcard-allow": {
        "enforcementLevel": "mandatory",
        "dangerousTools": ["shell", "write_file", "edit_file", "mcp:github:create_*"],
        "allowedPatterns": ["edit_file"]
    }
}
```

```bash
pulumi preview --policy-pack ../../policy --policy-pack-config policy-config.json
```

With `remediate`, `agentrun-tool-approval-deny` sets `toolApproval` to `deny`, and `allowHookApprovals` and
`allowHarnessAgents` to `false`, on every `AgentRun` that is not in `allowedResources` instead of failing, and the
preview shows the change. An `AgentRun` of a coding-harness agent then fails when it runs, because the opt-in is
missing.

## Tests

```bash
cd policy
npm test                     # type-checks and runs the unit tests (node:test)
npx tsc --noEmit -p .        # type-check only
```

`examples/policy_test.go` previews `examples/testdata/policy-violations` (which violates every policy) and
`examples/yaml` (which passes) with the pack:

```bash
cd examples && go test -run TestPolicyPack -v ./...
```

## Publish it to Pulumi Cloud

Pulumi Cloud enforces published packs on every stack of a [policy group](https://www.pulumi.com/docs/discovery-governance/concepts/policy-as-code/policy-groups/). A published pack cannot reference a file in
this repository, so depend on the published SDK first and raise `version` in `package.json` for every publish:

```bash
cd policy
npm install @ediri/pulumi-docker-agent@^0.1.0   # replaces the file: dependency

pulumi policy publish <org-name>
pulumi policy enable <org-name>/docker-agent-policies latest --config policy-config.json
```

`pulumi policy enable` takes `--policy-group <name>` to enable the pack for a specific policy group instead of the
default one, and `pulumi policy validate-config <org-name>/docker-agent-policies <version> --config <file>` checks a
configuration file against a published version. A [Pulumi ESC](https://www.pulumi.com/docs/esc/) environment
attached to the pack in a policy group can also supply the configuration through its `policyConfig` property.
