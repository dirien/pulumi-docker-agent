# Docker Agent example (Java)

Builds a two-agent Docker Agent configuration (a `root` coordinator and a `researcher` sub-agent with
the DuckDuckGo MCP server), pushes it to `<registry>/examples/java-agent:v1`, reads it back with
`DockeragentFunctions.getAgent`, and validates the hand-written [`agent.yaml`](agent.yaml) with
`DockeragentFunctions.validateAgentConfig`. See [the examples overview](../README.md) for the scenario
and the outputs.

## Run it

You need Gradle 9 on a JDK 17 or later, and a JDK 11 for the SDK's toolchain (see `sdk/java/build.gradle`).

```bash
# From the repository root
docker run -d --name agent-registry -p 5000:5000 -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2
make provider

cd examples/java
pulumi stack init dev
pulumi install --no-plugins                 # runs `gradle build`, which also compiles sdk/java
pulumi config set registry localhost:5000   # optional, this is the default
pulumi up

export AGENT_ANTHROPIC_API_KEY=<your-anthropic-api-key>
docker agent run "$(pulumi stack output pinnedReference)"

pulumi destroy && pulumi stack rm dev
```

`settings.gradle` includes `../../sdk/java`, the Java SDK in this repository, as a Gradle composite
build in place of `io.github.dirien:docker-agent`, and `Pulumi.yaml` points `plugins.providers` at
`../../bin`. The program exports `pulledMetadata` as a map because Pulumi Java cannot serialize the
SDK's output types as stack outputs.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and the `includeBuild` block from `settings.gradle`.
`build.gradle` then resolves the published SDK from Maven Central, which downloads the provider plugin
automatically:

```groovy
implementation "io.github.dirien:docker-agent:0.1.0"
```

The Java SDK is published from the first release that has Maven Central publishing enabled.
