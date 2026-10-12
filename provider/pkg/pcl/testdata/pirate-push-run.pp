# Converted from pirate.yaml, the same configuration as pirate.hcl.
resource pirate "docker-agent:index:AgentConfig" {
  agents = [
    {
      name           = "root"
      description    = "An agent that talks like a pirate"
      instruction    = "Always answer by talking like a pirate."
      welcomeMessage = <<EOT
Ahoy! I be yer pirate guide, ready to set sail on the seas o' knowledge!

What be yer quest? 🏴‍☠️
EOT
      model          = "auto"
    },
  ]

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
    version           = "0.1.0"
  }
}

# Pushes the configuration of pirate.yaml to ghcr.io/acme/agents/pirate:latest as an OCI artifact.
# Registry credentials come from the provider's `registryAuth` or the Docker credential store
# (`docker login`).
resource pirateArtifact "docker-agent:index:AgentArtifact" {
  __logicalName = "pirate"
  reference     = "ghcr.io/acme/agents/pirate:latest"
  yaml          = pirate.yaml

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
    version           = "0.1.0"
  }
}

output piratePinnedReference {
  value = pirateArtifact.pinnedReference
}

# Sends the prompt to the default agent of pirate.yaml during `pulumi up` (never during a preview)
# and exports the answer. The model credentials, such as the variables in the AgentConfig's
# `requiredEnvVars` output, are read from the environment of the Pulumi CLI (`inheritEnvironment`
# defaults to true), so the program holds no secrets. To keep a key in the stack instead, store it
# with `pulumi config set --secret` and pass it in `env`. Tool calls that docker-agent would ask
# to confirm are denied (`toolApproval` defaults to `deny`).
resource pirateRun "docker-agent:index:AgentRun" {
  __logicalName = "pirate"
  yaml          = pirate.yaml
  prompt        = "Tell me about $${treasure}."

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
    version           = "0.1.0"
  }
}

output pirateOutput {
  value = pirateRun.output
}
