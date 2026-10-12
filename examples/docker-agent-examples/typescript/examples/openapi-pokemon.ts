// Generated from docker-agent v1.150.0 examples/openapi-pokemon.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("openapi-pokemon", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4.1-mini",
            description: "Pokédex lookup with complete API responses",
            instruction: `\
Look up species using pokemon_retrieve with id set to their name.
Report types and sum the six stats[].base_stat values.
Use returned facts, not remembered values.
`,
            toolsets: [
                {
                    type: "openapi",
                    url: "https://raw.githubusercontent.com/PokeAPI/pokeapi/master/openapi.yml",
                    tools: ["pokemon_retrieve"],
                    maxOutputBytes: 0,
                },
            ],
        },
    ],
});

export const yaml = config.yaml;
