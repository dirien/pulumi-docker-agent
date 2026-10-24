// Generated from docker-agent v1.152.0 examples/transform_json.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("transform_json", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4.1",
            description: "Retrieve compact Pokémon facts with a native JSON response transform.",
            instruction:
                "Retrieve Pokémon facts using the catalog tool; failed evidence requires review.",
            toolsets: [
                {
                    type: "openapi",
                    url: "https://raw.githubusercontent.com/PokeAPI/pokeapi/master/openapi.yml",
                    tools: ["pokemon_retrieve"],
                    maxOutputBytes: 0,
                },
            ],
            hooks: {
                toolResponseTransform: [
                    {
                        matcher: "pokemon_retrieve",
                        hooks: [
                            {
                                type: "builtin",
                                command: "transform_json",
                                args: ["name", "types", "stats"],
                            },
                        ],
                    },
                ],
            },
        },
    ],
});

export const yaml = config.yaml;
