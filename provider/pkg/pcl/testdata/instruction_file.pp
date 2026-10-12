# Converted from instruction_file.yaml.
#
# agents.coordinator.instruction_file:
#   Instead of inlining a long prompt here, point at a Markdown file kept
#   next to the config. The path is relative to this config file's directory
#   and its contents are loaded as the agent's instruction at startup.
#
# agents.writer.instruction_file:
#   instruction_file also accepts a list of files. Their contents are
#   concatenated in order (separated by a blank line), so a shared preamble
#   can be reused across agents while each appends its own specifics.
#
# skipValidation:
#   `instruction_file` is read relative to the agent file when docker-agent loads it. The
#   provider validates the rendered YAML in memory, where there are no files, so validation is
#   skipped. The converter copied the instruction files next to the program.
resource instructionFile "docker-agent:index:AgentConfig" {
  __logicalName = "instruction_file"
  agents = [
    {
      name            = "coordinator"
      model           = "openai/gpt-5-mini"
      description     = "Routes work between specialist agents"
      instructionFile = ["instructions/coordinator.md"]
      subAgents       = ["writer"]
    },
    {
      name            = "writer"
      model           = "openai/gpt-5-mini"
      description     = "Drafts and edits written content"
      instructionFile = ["instructions/shared-preamble.md", "instructions/writer.md"]
    },
  ]
  skipValidation = true

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
  }
}
