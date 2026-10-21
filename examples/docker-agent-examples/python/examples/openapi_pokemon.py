# Generated from docker-agent v1.151.0 examples/openapi-pokemon.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "openapi-pokemon",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4.1-mini",
            "description": "Pokédex lookup with complete API responses",
            "instruction": """\
Look up species using pokemon_retrieve with id set to their name.
Report types and sum the six stats[].base_stat values.
Use returned facts, not remembered values.
""",
            "toolsets": [
                {
                    "type": "openapi",
                    "url": "https://raw.githubusercontent.com/PokeAPI/pokeapi/master/openapi.yml",
                    "tools": ["pokemon_retrieve"],
                    "max_output_bytes": 0,
                },
            ],
        },
    ],
)
