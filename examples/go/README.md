# Docker Agent example (Go)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/go-agent:v1`, reads it back with
`GetAgentOutput`, and validates the hand-written [`agent.yaml`](agent.yaml) with
`ValidateAgentConfigOutput`. See [the examples overview](../README.md) for the scenario and the outputs.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider

cd examples/go
pulumi stack init dev
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`go.mod` replaces `github.com/dirien/pulumi-docker-agent/sdk` with `../../sdk`, the Go SDK in this
repository, and `Pulumi.yaml` points `plugins.providers` at `../../bin`.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and the `replace` directive from `go.mod`, then fetch
the published SDK, which downloads the provider plugin automatically:

```bash
go get github.com/dirien/pulumi-docker-agent/sdk@latest
```
