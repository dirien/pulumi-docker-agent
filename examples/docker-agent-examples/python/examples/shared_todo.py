# Generated from docker-agent v1.151.0 examples/shared-todo.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "shared-todo",
    agents=[
        {
            "name": "root",
            "model": "gpt",
            "description": "An agent to demonstrate how to setup a shared todo.",
            "instruction": """\
Here is your workflow:
  - Transfer to the `first` agent, the task should be: create a todo
  - Transfer to the `second` agent, the task should be: what todos do you have?
""",
            "sub_agents": ["first", "second"],
        },
        {
            "name": "first",
            "model": "gpt",
            "instruction": "Use the todos tool to create one random todo item",
            "toolsets": [{"type": "todo", "shared": True}],
        },
        {
            "name": "second",
            "model": "gpt",
            "instruction": "Get the todos and show them",
            "toolsets": [{"type": "todo", "shared": True}],
        },
    ],
    models={"gpt": {"provider": "openai", "model": "gpt-5.6-sol"}},
)
