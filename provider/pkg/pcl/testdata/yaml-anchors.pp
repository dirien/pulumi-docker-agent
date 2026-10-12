# Converted from yaml-anchors.yaml.
#
# This example demonstrates YAML anchors (&name), aliases (*name) and merge
# keys (<<). Declare a value once and reuse it across agents instead of
# copy-pasting the same block.
#
# An anchor has to sit on a real value inside a known section (here, real
# agents). A separate top-level block such as `defaults:` is rejected as an
# unknown field.
#
# agents.root.instruction:
#   `&house_rules` anchors this instruction so it can be reused verbatim.
#
# agents.reviewer:
#   `&specialist` anchors this whole agent so it can be merged into others.
#
# agents.reviewer.instruction:
#   reuse the same instruction, declared once
#
# agents.documenter:
#   Merge everything from `reviewer` (model, instruction, toolsets), then
#   override a single field. Overriding a merged key works in `agents:`.
resource yamlAnchors "docker-agent:index:AgentConfig" {
  __logicalName = "yaml-anchors"
  models = {
    sonnet = {
      provider  = "anthropic"
      model     = "claude-sonnet-5"
      maxTokens = 64000
    }
  }
  agents = [
    {
      name        = "root"
      model       = "sonnet"
      description = "Coordinator that delegates to specialist sub-agents."
      instruction = <<EOT
You are part of the Acme engineering team.
Cite the files you looked at and keep changes minimal.
EOT
      subAgents   = ["reviewer", "documenter"]
    },
    {
      name        = "reviewer"
      model       = "sonnet"
      description = "Reviews code changes for correctness and clarity."
      instruction = <<EOT
You are part of the Acme engineering team.
Cite the files you looked at and keep changes minimal.
EOT
      toolsets = [
        {
          type = "filesystem"
        },
      ]
    },
    {
      name        = "documenter"
      description = "Writes and updates project documentation."
      model       = "sonnet"
      instruction = <<EOT
You are part of the Acme engineering team.
Cite the files you looked at and keep changes minimal.
EOT
      toolsets = [
        {
          type = "filesystem"
        },
      ]
    },
  ]

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
  }
}
