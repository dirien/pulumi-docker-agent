// Builds a two-agent Docker Agent configuration, pushes it to an OCI registry and reads it back.
package main

import (
	"os"

	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		// The registry to push to. The default is a local registry:2 container.
		registry := cfg.Get("registry")
		if registry == "" {
			registry = "localhost:5000"
		}

		// A typed docker-agent configuration, validated during `pulumi preview`.
		assistant, err := dockeragent.NewAgentConfig(ctx, "research-assistant", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"claude": dockeragent.ModelDefArgs{
					Provider:  pulumi.String("anthropic"),
					Model:     pulumi.String("claude-opus-5-5"),
					MaxTokens: pulumi.Int(32000),
					// The environment variable that holds the API key where the agent runs.
					TokenKey: pulumi.String("AGENT_ANTHROPIC_API_KEY"),
				},
			},
			Agents: dockeragent.AgentArray{
				dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("claude"),
					Description: pulumi.String("Answers questions and delegates web research"),
					Instruction: pulumi.String("You are a helpful assistant. Delegate questions that need current " +
						"information to the researcher agent, then summarise its findings."),
					SubAgents: pulumi.StringArray{pulumi.String("researcher")},
				},
				dockeragent.AgentArgs{
					Name:        pulumi.String("researcher"),
					Model:       pulumi.String("claude"),
					Description: pulumi.String("Searches the web with DuckDuckGo"),
					Instruction: pulumi.String("Search the web and answer with short summaries that cite their sources."),
					AddDate:     pulumi.Bool(true),
					// The DuckDuckGo MCP server from the Docker MCP Catalog.
					Toolsets: dockeragent.ToolsetArray{
						dockeragent.ToolsetArgs{
							Type: pulumi.String("mcp"),
							Ref:  pulumi.String("docker:duckduckgo"),
						},
					},
				},
			},
			Metadata: &dockeragent.MetadataArgs{
				Author:      pulumi.String("Pulumi Docker Agent examples"),
				License:     pulumi.String("Apache-2.0"),
				Description: pulumi.String("A research assistant that delegates web searches to a sub-agent"),
				Version:     pulumi.String("1.0.0"),
				Tags:        pulumi.StringArray{pulumi.String("research"), pulumi.String("example")},
			},
			// Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only
			// lets it reach the model API.
			Runtime: &dockeragent.RuntimeDefaultsArgs{
				Sandbox:          pulumi.Bool(true),
				NetworkAllowlist: pulumi.StringArray{pulumi.String("api.anthropic.com")},
				Safety:           pulumi.String("balanced"),
			},
		})
		if err != nil {
			return err
		}

		// Share the configuration as an OCI artifact, like `docker agent share push`.
		artifact, err := dockeragent.NewAgentArtifact(ctx, "research-assistant", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String(registry + "/examples/go-agent:v1"),
			Yaml:      assistant.Yaml,
			// Remove the artifact from the registry on `pulumi destroy`.
			DeleteBehavior: dockeragent.DeleteBehaviorDelete,
		})
		if err != nil {
			return err
		}

		// Read the shared agent back from the registry, like `docker agent share pull`.
		pulled := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: artifact.PinnedReference,
		})

		// Validate a hand-written agent file without pushing it.
		raw, err := os.ReadFile("agent.yaml")
		if err != nil {
			return err
		}
		rawConfig := dockeragent.ValidateAgentConfigOutput(ctx, dockeragent.ValidateAgentConfigOutputArgs{
			Yaml: pulumi.String(string(raw)),
		})

		ctx.Export("digest", artifact.Digest)
		ctx.Export("pinnedReference", artifact.PinnedReference)
		ctx.Export("runCommand", pulumi.Sprintf("docker agent run %s", artifact.PinnedReference))
		ctx.Export("requiredEnvVars", assistant.RequiredEnvVars)
		ctx.Export("agentYaml", assistant.Yaml)
		ctx.Export("pulledYaml", pulled.Yaml())
		ctx.Export("pulledAgents", pulled.AgentNames())
		ctx.Export("pulledMetadata", pulled.Metadata())
		ctx.Export("rawConfigValid", rawConfig.Valid())
		ctx.Export("rawConfigAgents", rawConfig.AgentNames())
		ctx.Export("rawConfigErrors", rawConfig.Errors())
		return nil
	})
}
