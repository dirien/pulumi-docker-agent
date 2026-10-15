# Credentials from Pulumi ESC (Pulumi YAML)

Takes every credential from a [Pulumi ESC](https://www.pulumi.com/docs/esc/) environment instead of stack
configuration or the shell:

- [`environment.yaml`](environment.yaml) defines the environment `docker-agent/dev`. Its `pulumiConfig` sets the
  provider's `docker-agent:registryAuth` (registry credentials for `AgentArtifact` and `getAgent`) and the program's
  `registryPrefix` and `anthropicApiKey`. Its `environmentVariables` export `ANTHROPIC_API_KEY` and the short-lived
  AWS credentials that `fn::open::aws-login` creates through OpenID Connect, for Amazon Bedrock.
- [`Pulumi.dev.yaml`](Pulumi.dev.yaml) imports the environment into the `dev` stack (`environment:`).
- [`Pulumi.yaml`](Pulumi.yaml) pushes an Anthropic agent to `<registryPrefix>/release-notes-assistant:v1` and runs
  two agents with `AgentRun`:
  - `answer` gets its key from the secret `anthropicApiKey` setting through `env`, with
    `inheritEnvironment: false`, so credentials resolve only from `env`.
  - `summary` uses an `amazon-bedrock` model with the default `inheritEnvironment: true`. `pulumi up` exports the
    environment's `environmentVariables` to the Pulumi CLI and its plugins, so the provider has the AWS credentials
    and `AWS_REGION`, and the run inherits them. With `inheritEnvironment: false`, pass them in `env` instead: since
    v0.2.0 the AWS SDK that docker-agent uses for Bedrock sees only `env` then.

Environments need the Pulumi Cloud backend: stacks in a file or object-storage backend cannot import them.

## Run it

```bash
# From the repository root
make provider

cd examples/esc-yaml
pulumi login                                     # Pulumi Cloud
# Put the ARN of an IAM role that trusts Pulumi Cloud (OIDC) and may invoke Bedrock models into
# environment.yaml, then create the environment and set the secrets:
pulumi env init <org>/docker-agent/dev --file environment.yaml
pulumi env set <org>/docker-agent/dev registry.username <github-user>
pulumi env set <org>/docker-agent/dev registry.token <github-token> --secret
pulumi env set <org>/docker-agent/dev anthropic.apiKey <anthropic-api-key> --secret

pulumi stack init <org>/dev                      # Pulumi.dev.yaml imports docker-agent/dev
pulumi config                                    # shows registryPrefix, anthropicApiKey and docker-agent:registryAuth
pulumi up

# Run the published agent with the environment's variables (-i: docker agent run is interactive).
pulumi env run -i <org>/docker-agent/dev -- docker agent run "$(pulumi stack output pinnedReference)"
# Or without the TUI:
pulumi env run <org>/docker-agent/dev -- docker agent run --exec "$(pulumi stack output pinnedReference)" "What changed in v2.4.0?"

pulumi destroy && pulumi stack rm dev
```

`pulumi config env add docker-agent/dev` adds the import to any other stack. Explicit stack configuration takes
precedence over the environment's `pulumiConfig`, so `pulumi config set` overrides a value for one stack.

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.

## Tests

`TestExampleESCProgram` in [`examples/esc_test.go`](../esc_test.go) previews the program with a file backend and the
values of the environment set as plain stack configuration, and checks that `environment.yaml` and `Pulumi.dev.yaml`
match the program. `TestExampleESCEnvironment` sends `environment.yaml` to the Pulumi Cloud API that checks
environment definitions without saving them; it runs only when `PULUMI_ACCESS_TOKEN` is set and creates nothing. CI
runs it in its own job on pushes to `main` only, so pull request code never gets the token.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the released plugin:

```bash
pulumi plugin install resource docker-agent --server github://api.github.com/dirien/pulumi-docker-agent
```
