# Docker Agent example (Python)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/python-agent:v1`, reads it back with
`get_agent`, and validates the hand-written [`agent.yaml`](agent.yaml) with `validate_agent_config`.
See [the examples overview](../README.md) for the scenario and the outputs.

## Run it

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider build_python

cd examples/python
pulumi stack init dev
pulumi install --no-plugins                 # creates ./venv and installs requirements.txt
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`requirements.txt` installs the SDK that `make build_python` writes to `sdk/python/bin`, and
`Pulumi.yaml` points `plugins.providers` at `../../bin`.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and replace the `../../sdk/python/bin` line in
`requirements.txt` with the published SDK, which downloads the provider plugin automatically:

```text
ediri_docker_agent>=0.1.0,<1.0.0
```
