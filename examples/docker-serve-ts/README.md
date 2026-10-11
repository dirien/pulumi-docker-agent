# Serve a shared agent with Docker (TypeScript)

Combines this provider with the [Pulumi Docker provider](https://www.pulumi.com/registry/packages/docker/)
to run a pushed agent as an HTTP service:

1. A user-defined Docker network and a `registry:2` container on it. The registry is also published
   on a random localhost port.
2. The research assistant from the other examples, as an `AgentConfig`, pushed with an
   `AgentArtifact` through the published port to `localhost:<port>/examples/serve-agent:v1`.
3. A `docker/docker-agent:1.150.0` container on the same network that runs
   `serve api <registry-ip>:5000/examples/serve-agent@<digest> --listen 0.0.0.0:8080`, with a Docker
   health check on `/health`. Pulumi waits until the container is healthy (`wait: true`).

The server pulls the artifact by digest, so every change to the configuration pushes a new artifact
and replaces the server container. It addresses the registry by its IP address on the network
because docker-agent, like other go-containerregistry clients, speaks plain HTTP only to registries
on localhost or on a private IP address. A host name such as `registry:5000` would need TLS.

## Run it

```bash
# From the repository root (needs Docker)
make provider build_nodejs

cd examples/docker-serve-ts
pulumi stack init dev
pulumi install --no-plugins   # npm dependencies; `pulumi up` installs the docker plugin
pulumi up

curl "$(pulumi stack output healthUrl)"   # {"status":"ok","version":""}
curl "$(pulumi stack output agentsUrl)"   # the served agent configuration
```

Optional settings:

| Setting | Default | Description |
|---|---|---|
| `agentImage` | `docker/docker-agent:1.150.0` | The image that serves the agent. Pin a version: `latest` tracks docker-agent's main branch. |
| `agentPort` | a free port | The localhost port of the agent API (`pulumi config set agentPort 8080`). |
| `anthropicApiKey` | not set | Passed to the server as `AGENT_ANTHROPIC_API_KEY` so the agent can answer (`pulumi config set --secret anthropicApiKey ...`). Without it the server starts and lists the agent, but cannot run it. |

The API has no authentication, so the example publishes it on `127.0.0.1` only. For anything else,
add `--auth-token`. The researcher's `docker:duckduckgo` MCP toolset runs through the Docker MCP
Gateway, which needs access to a Docker daemon from inside the container; this example does not set
that up.

`pulumi destroy` removes the containers and the network but keeps the images (`keepLocally: true`).

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the published SDK:
`npm install @ediri/pulumi-docker-agent`.
