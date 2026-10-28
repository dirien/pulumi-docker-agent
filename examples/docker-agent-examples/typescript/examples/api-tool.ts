// Generated from docker-agent v1.152.0 examples/api-tool.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("api-tool", {
    agents: [
        {
            name: "root",
            description: "Chess.com daily puzzle agent",
            instruction:
                "Call the daily-puzzle tool and print the puzzle in ascii with unicode chess pieces. Do not solve the puzzle for them, only provide hints and guidance.",
            model: "google/gemini-2.5-pro",
            welcomeMessage: `\
♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕

Welcome to the Chess.com daily puzzle agent!

How can I help you with today's chess puzzle?

♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕♔♕
`,
            toolsets: [
                {
                    type: "api",
                    apiConfig: {
                        instruction: "Get daily puzzle",
                        name: "daily-puzzle",
                        endpoint: "https://api.chess.com/pub/puzzle",
                        method: "GET",

                        // Optional: describe the API response for MCP/Code Mode consumers.
                        outputSchema: {
                            type: "object",
                            properties: {
                                title: { type: "string", description: "Puzzle title" },
                                url: { type: "string", description: "Link to the puzzle" },
                                fen: { type: "string", description: "Board state in FEN" },
                            },
                            required: ["title", "url"],
                        },
                    },
                },
            ],
            simpleCommands: { todays_puzzle: "Fetch and show today's puzzle" },
        },
    ],
});

export const yaml = config.yaml;
