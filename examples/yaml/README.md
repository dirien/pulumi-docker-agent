# Docker Agent example (Pulumi YAML)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/yaml-agent:v1`, reads it back with
`getAgent`, and validates the hand-written [`agent.yaml`](agent.yaml) with `validateAgentConfig`.
See [the examples overview](../README.md) for the scenario and the outputs.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider

cd examples/yaml
pulumi stack init dev
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the released plugin:

```bash
pulumi plugin install resource docker-agent --server github://api.github.com/dirien/pulumi-docker-agent
```
