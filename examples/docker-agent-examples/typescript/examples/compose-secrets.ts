// Generated from docker-agent v1.152.0 examples/compose-secrets.yaml by `make generate_config_types`. Do not edit.
//
// Example: Using Docker Compose secrets with docker-agent
//
// Docker Compose mounts secrets as files under /run/secrets/, and
// docker-agent reads from this location automatically.
//
// See the full guide: https://docker.github.io/docker-agent/guides/secrets/
//
// To run this example:
//
//   echo -n "sk-ant-..." > .anthropic_api_key
//   docker compose up
//
// compose.yaml:
//
//   services:
//     agent:
//       image: docker/docker-agent
//       command: run --exec /app/agent.yaml "Hello!"
//       secrets:
//         - ANTHROPIC_API_KEY
//       volumes:
//         - ./agent.yaml:/app/agent.yaml:ro
//
//   secrets:
//     ANTHROPIC_API_KEY:
//       file: ./.anthropic_api_key

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("compose-secrets", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "A helpful assistant running with Docker Compose secrets",
            instruction: `\
You are a helpful assistant. Your API key was securely provided
via Docker Compose secrets mounted at /run/secrets/.
`,
        },
    ],
});

export const yaml = config.yaml;
