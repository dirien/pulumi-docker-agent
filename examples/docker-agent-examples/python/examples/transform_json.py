# Generated from docker-agent v1.150.0 examples/transform_json.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "transform_json",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4.1",
            "description": "Retrieve compact Pokémon facts with a native JSON response transform.",
            "instruction": "Retrieve Pokémon facts using the catalog tool; failed evidence requires review.",
            "toolsets": [
                {
                    "type": "openapi",
                    "url": "https://raw.githubusercontent.com/PokeAPI/pokeapi/master/openapi.yml",
                    "tools": ["pokemon_retrieve"],
                    "max_output_bytes": 0,
                },
            ],
            "hooks": {
                "tool_response_transform": [
                    {
                        "matcher": "pokemon_retrieve",
                        "hooks": [
                            {
                                "type": "builtin",
                                "command": "transform_json",
                                "args": ["name", "types", "stats"],
                            },
                        ],
                    },
                ],
            },
        },
    ],
)
