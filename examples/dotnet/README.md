# Docker Agent example (C#)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/dotnet-agent:v1`, reads it back with
`GetAgent.Invoke`, and validates the hand-written [`agent.yaml`](agent.yaml) with
`ValidateAgentConfig.Invoke`. See [the examples overview](../README.md) for the scenario and the outputs.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider build_dotnet

cd examples/dotnet
pulumi stack init dev
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`DockerAgentExample.csproj` builds the SDK in `sdk/dotnet` through a `ProjectReference`
(`make build_dotnet` writes the `version.txt` it embeds), and `Pulumi.yaml` points
`plugins.providers` at `../../bin`. The program uses `new Pulumi.Config()` because the SDK also has an
`ediri.DockerAgent.Config` namespace.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and replace the `ProjectReference` with the published
SDK, which downloads the provider plugin automatically:

```bash
dotnet add package ediri.DockerAgent
```
