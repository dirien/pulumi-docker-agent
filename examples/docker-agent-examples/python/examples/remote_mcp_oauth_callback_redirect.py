# Generated from docker-agent v1.154.0 examples/remote_mcp_oauth_callback_redirect.yaml by `make generate_config_types`. Do not edit.
#
# Example: Remote MCP server with a custom OAuth callback redirect URL.
#
# Some authorization servers refuse http://localhost or http://127.0.0.1
# redirect URIs (they require HTTPS or a pre-registered URL). Use
# `callbackRedirectURL` to advertise a public URL to the authorization
# server while still receiving the final callback on the local loopback
# interface.
#
# The literal placeholder ${callbackPort} is replaced at runtime with the
# actual port the local callback server is listening on (either
# `callbackPort` when set, or a random free port otherwise).
#
# The external service at redirect.example.com is expected to 302-redirect
# the browser back to http://127.0.0.1:${callbackPort}/callback, preserving
# the OAuth query parameters (code, state, ...).

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "remote_mcp_oauth_callback_redirect",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4.1-mini",
            "description": "Assistant with a remote MCP tool using a custom OAuth redirect URL",
            "instruction": "You are a helpful assistant with access to remote tools.",
            "toolsets": [
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://mcp.example.com/mcp",
                        "transport_type": "streamable",
                        "oauth": {
                            "client_id": "your-client-id",
                            "client_secret": "your-client-secret",
                            "callback_port": 8765,
                            "callback_redirect_url": "https://redirect.example.com/cb?port=${callbackPort}",
                            "scopes": ["read", "write"],
                        },
                    },
                },
            ],
        },
    ],
)
