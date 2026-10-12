// Builds a two-agent Docker Agent configuration, pushes it to an OCI registry and reads it back.
package example;

import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentArtifact;
import io.github.dirien.dockeragent.AgentArtifactArgs;
import io.github.dirien.dockeragent.AgentConfig;
import io.github.dirien.dockeragent.AgentConfigArgs;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.enums.DeleteBehavior;
import io.github.dirien.dockeragent.enums.SafetyMode;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.GetAgentArgs;
import io.github.dirien.dockeragent.inputs.MetadataArgs;
import io.github.dirien.dockeragent.inputs.ModelDefArgs;
import io.github.dirien.dockeragent.inputs.RuntimeDefaultsArgs;
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
import io.github.dirien.dockeragent.inputs.ValidateAgentConfigArgs;
import io.github.dirien.dockeragent.outputs.Metadata;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            // The registry to push to. The default is a local registry:2 container.
            var registry = ctx.config().get("registry").orElse("localhost:5000");

            // A typed docker-agent configuration, validated during `pulumi preview`.
            var assistant = new AgentConfig("research-assistant", AgentConfigArgs.builder()
                .models(Map.of("claude", ModelDefArgs.builder()
                    .provider("anthropic")
                    .model("claude-opus-5-5")
                    .maxTokens(32000)
                    // The environment variable that holds the API key where the agent runs.
                    .tokenKey("AGENT_ANTHROPIC_API_KEY")
                    .build()))
                .agents(
                    AgentArgs.builder()
                        .name("root")
                        .model("claude")
                        .description("Answers questions and delegates web research")
                        .instruction("You are a helpful assistant. Delegate questions that need current "
                            + "information to the researcher agent, then summarise its findings.")
                        .subAgents("researcher")
                        .build(),
                    AgentArgs.builder()
                        .name("researcher")
                        .model("claude")
                        .description("Searches the web with DuckDuckGo")
                        .instruction("Search the web and answer with short summaries that cite their sources.")
                        .addDate(true)
                        // The DuckDuckGo MCP server from the Docker MCP Catalog.
                        .toolsets(ToolsetArgs.builder().type("mcp").ref("docker:duckduckgo").build())
                        .build())
                .metadata(MetadataArgs.builder()
                    .author("Pulumi Docker Agent examples")
                    .license("Apache-2.0")
                    .description("A research assistant that delegates web searches to a sub-agent")
                    .version("1.0.0")
                    .tags("research", "example")
                    .build())
                // Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only
                // lets it reach the model API.
                .runtime(RuntimeDefaultsArgs.builder()
                    .sandbox(true)
                    .networkAllowlist("api.anthropic.com")
                    .safety(SafetyMode.Balanced)
                    .build())
                .build());

            // Share the configuration as an OCI artifact, like `docker agent share push`.
            var artifact = new AgentArtifact("research-assistant", AgentArtifactArgs.builder()
                .reference(registry + "/examples/java-agent:v1")
                .yaml(assistant.yaml())
                // Remove the artifact from the registry on `pulumi destroy`.
                .deleteBehavior(DeleteBehavior.Delete)
                .build());

            // Read the shared agent back from the registry, like `docker agent share pull`.
            var pulled = DockeragentFunctions.getAgent(GetAgentArgs.builder()
                .reference(artifact.pinnedReference())
                .build());

            // Validate a hand-written agent file without pushing it.
            var rawConfig = DockeragentFunctions.validateAgentConfig(ValidateAgentConfigArgs.builder()
                .yaml(readFile("agent.yaml"))
                .build());

            ctx.export("digest", artifact.digest());
            ctx.export("pinnedReference", artifact.pinnedReference());
            ctx.export("runCommand", Output.format("docker agent run %s", artifact.pinnedReference()));
            ctx.export("requiredEnvVars", assistant.requiredEnvVars());
            ctx.export("agentYaml", assistant.yaml());
            ctx.export("pulledYaml", pulled.applyValue(result -> result.yaml()));
            ctx.export("pulledAgents", pulled.applyValue(result -> result.agentNames()));
            ctx.export("pulledMetadata", pulled.applyValue(result -> toMap(result.metadata())));
            ctx.export("rawConfigValid", rawConfig.applyValue(result -> result.valid()));
            ctx.export("rawConfigAgents", rawConfig.applyValue(result -> result.agentNames()));
            ctx.export("rawConfigErrors", rawConfig.applyValue(result -> result.errors()));
        });
    }

    // Stack outputs must be plain values: Pulumi Java does not serialize the SDK's output types.
    private static Map<String, Object> toMap(Metadata metadata) {
        var map = new LinkedHashMap<String, Object>();
        if (metadata != null) {
            metadata.author().ifPresent(value -> map.put("author", value));
            metadata.license().ifPresent(value -> map.put("license", value));
            metadata.description().ifPresent(value -> map.put("description", value));
            metadata.version().ifPresent(value -> map.put("version", value));
            map.put("tags", metadata.tags());
        }
        return map;
    }

    private static String readFile(String path) {
        try {
            return Files.readString(Path.of(path));
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }
}
