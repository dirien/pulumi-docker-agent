# Run an agent during `pulumi up` (Pulumi YAML)

Defines a change-review agent with `AgentConfig` and runs it with `AgentRun`:

- The agent uses a custom OpenAI-compatible provider whose `baseUrl` and `model` are stack settings
  (default: OpenAI and `gpt-5-mini`). The API key comes from the secret `apiKey` setting through
  `AgentRun`'s `env`, so it is stored encrypted and never written into the agent configuration.
- The agent declares `structuredOutput`, so `AgentRun` returns its answer as JSON in
  `structuredOutput`, and the program exports `risk` and `summary` from it.
- The agent runs in the provider process during `pulumi up`, never during `pulumi preview`. A
  `pulumi up` without changes keeps the stored answer; changing any input, for example the
  `revision` setting that feeds `triggers`, runs it again.

The agent has no tools. With tools, keep in mind that they run on the machine executing
`pulumi up`, and that the default `toolApproval: deny` only runs the tool calls docker-agent approves
without asking, such as read-only tools and calls that a `permissions` allow rule matches (see the
`AgentRun` documentation).

## Run it

```bash
# From the repository root
make provider

cd examples/agent-run-yaml
pulumi stack init dev
pulumi config set --secret apiKey <your-openai-api-key>
# Any other OpenAI-compatible endpoint works too:
# pulumi config set baseUrl https://openrouter.ai/api/v1 && pulumi config set model openai/gpt-5-mini
pulumi up

pulumi stack output risk
pulumi stack output summary

pulumi config set revision 2 && pulumi up   # ask again
pulumi destroy && pulumi stack rm dev       # nothing to clean up: a run leaves nothing behind
```

`make test_examples` runs this program against a fake OpenAI-compatible server started by the
test, so it needs no API key.

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the released plugin:

```bash
pulumi plugin install resource docker-agent --server github://api.github.com/dirien/pulumi-docker-agent
```
