# Run an agent from code with the Automation API (TypeScript)

A CI gate written with the [Pulumi Automation API](https://www.pulumi.com/docs/iac/concepts/automation-api/): the script
[`index.ts`](index.ts) contains an inline Pulumi program with

- an `AgentConfig` for a change-review agent whose answer must match a JSON schema (`structuredOutput`:
  `verdict`, `risk` and `summary`), using any OpenAI-compatible endpoint, and
- an `AgentRun` that sends the change description to the agent. The API key is a secret stack setting passed through
  `env`, with `inheritEnvironment: false`, so the run sees no other credential.

The script creates a stack in a local file backend (`STATE_DIR`, default `.state`), runs `up`, reads the answer with
`stack.outputs()`, runs `destroy` and removes the stack. It prints the answer as JSON on stdout (the engine output
goes to stderr) and exits with `1` when the verdict is `request_changes`, so a CI job fails on it.

The answer is memoized in the stack state: while the stack exists, another `up` with the same change description
and settings does not call the model again. Drop the `destroy` and `removeStack` calls to keep it, for example in a
backend shared by the CI jobs of one pull request.

## Run it

```bash
# From the repository root
make provider build_nodejs

cd examples/automation-ts
npm install
export LLM_API_KEY=<your-openai-api-key>           # or OPENAI_API_KEY
export DOCKER_AGENT_PROVIDER_PATH="$PWD/../../bin" # the provider built by `make provider`
npm start                                          # reviews the built-in sample change
npx tsc -p . && node bin/index.js change.txt       # reviews the description in change.txt
```

Other OpenAI-compatible endpoints work too, for example
`LLM_BASE_URL=https://openrouter.ai/api/v1 LLM_MODEL=openai/gpt-5-mini`. `PULUMI_CONFIG_PASSPHRASE` encrypts the
key in the stack configuration and state (a fixed example passphrase is used when it is unset).

`package.json` installs the SDK that `make build_nodejs` writes to `sdk/nodejs/bin`, and `.npmrc` sets
`install-links=true` so npm copies it instead of symlinking it.

`make test_examples` runs the script against a fake OpenAI-compatible server started by the test
(`TestAutomationAPI` in `examples/automation_test.go`), so it needs no API key and no registry.

## Outside this repository

Install the published SDK and leave `DOCKER_AGENT_PROVIDER_PATH` unset; the provider plugin is then downloaded on
first use. To preinstall it, for example in a CI image, run `pulumi plugin install resource docker-agent <version>
--server github://api.github.com/dirien/pulumi-docker-agent`.

```bash
npm install @ediri/pulumi-docker-agent
```
