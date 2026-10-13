# Run an agent on Ollama during `pulumi up` (Pulumi YAML)

Defines a release-notes agent with `AgentConfig` and runs it with `AgentRun` on an [Ollama](https://ollama.com)
model, through docker-agent's `ollama` provider:

- **A local model.** The default `model` is `llama3.2`, served by the Ollama on this machine
  (`http://localhost:11434/v1`). Pull it first with `ollama pull llama3.2`.
- **An Ollama Cloud model through the local Ollama.** Sign the local Ollama in to your ollama.com account
  (`ollama signin`) and set `model` to a cloud model, for example `gpt-oss:120b-cloud`. Cloud models need no
  download: the local Ollama forwards the requests to Ollama Cloud with its account, so the program needs no key.
- **Another Ollama server.** Set `ollamaUrl`, for example to `http://gpu-box:11434/v1`. docker-agent does not read
  `OLLAMA_HOST`.

The run uses `inheritEnvironment: false`, so it gets no variables of the Pulumi CLI's environment: the `ollama`
provider would otherwise send an `OPENAI_API_KEY` of your shell to the Ollama server. The agent has no tools, so
nothing runs on this machine except the model calls. `usage.cost` is 0, because Ollama models have no catalog
pricing, and `pulumi up` shows the run's models as `models: ollama/llama3.2 (no key)`.

To call the Ollama Cloud API directly, without a local Ollama (for example in CI), use a custom provider with
`baseUrl: https://ollama.com/v1` and `tokenKey: OLLAMA_API_KEY` instead, and pass the key in `env`; the
[Ollama and Ollama Cloud](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/installation-configuration.md#ollama-and-ollama-cloud)
docs show it.

## Run it

```bash
# From the repository root
make provider

ollama pull llama3.2                          # or: ollama signin, for a cloud model

cd examples/agent-run-ollama-yaml
pulumi stack init dev
# pulumi config set model gpt-oss:120b-cloud  # an Ollama Cloud model through the local Ollama
pulumi up

pulumi stack output releaseNotes

pulumi config set revision 2 && pulumi up     # ask again
pulumi destroy && pulumi stack rm dev         # nothing to clean up: a run leaves nothing behind
```

Local models are slow on a CPU (`timeoutSeconds` is 600), and their context window is small by default: set
`OLLAMA_CONTEXT_LENGTH` on the Ollama server for longer instructions or many tools.

`make test_examples` runs this program against a fake OpenAI-compatible server started by the test, so it needs
no Ollama.

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the released plugin:

```bash
pulumi plugin install resource docker-agent --server github://api.github.com/dirien/pulumi-docker-agent
```
