# Docker Agent example (TypeScript)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/nodejs-agent:v1`, reads it back with
`getAgent`, and validates the hand-written [`agent.yaml`](agent.yaml) with `validateAgentConfig`.
See [the examples overview](../README.md) for the scenario and the outputs.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider build_nodejs

cd examples/nodejs
pulumi stack init dev
pulumi install --no-plugins                 # or: npm install
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`package.json` installs the SDK that `make build_nodejs` writes to `sdk/nodejs/bin` (`.npmrc` sets
`install-links=true` so npm copies it instead of symlinking it), and `Pulumi.yaml` points
`plugins.providers` at `../../bin`.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the published SDK, which downloads the
provider plugin automatically:

```bash
npm install @ediri/pulumi-docker-agent
```
