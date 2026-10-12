# Converted from instructions_from_file.hcl.
#
# agents.root.instruction:
#   The file() helper reads a text file and injects its contents here.
#   Relative paths are resolved from this HCL file's directory.
#   Passing an object as a second argument renders the file as a template:
#   each key becomes a ${key} variable inside the file.
#
#   The HCL file renders instructions_from_file.md as a template with file() and the variables {
#   audience = "developers" style = "concise" }. The program holds the value docker-agent loaded
#   when the file was converted; read the file in the program instead to keep them in sync.
resource instructionsFromFile "docker-agent:index:AgentConfig" {
  __logicalName = "instructions_from_file"
  agents = [
    {
      name           = "root"
      description    = "Agent that loads its system prompt from a separate file"
      model          = "auto"
      instruction    = <<EOT
You are a helpful assistant for developers.

When you answer:
- be concise
- prefer bullet points when listing steps
- ask for clarification only if necessary
- mention when you are making an assumption
EOT
      welcomeMessage = "Hi! My instructions were loaded from instructions_from_file.md"
    },
  ]

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
  }
}
