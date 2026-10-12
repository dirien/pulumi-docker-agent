# Converted from budget.yaml.
#
# Run budgets — stop an agent before it spends more than you meant
# ==========================================================================
#
# A budget caps what a session may consume in money, tokens, or working
# time. Every limit is optional and an unset limit is unlimited.
#
# Spend accumulates for the life of the session, across every message —
# it does not reset each time you hit enter. And max_time counts the time
# the agents actually spend working, so reading and typing is free.
#
# There are two ways to declare one:
#
#   budget:            a single run-wide ceiling, charged for every agent
#   budgets:           named budgets an agent opts into by name
#
# This example runs against OpenRouter (an OpenAI-compatible endpoint) so
# you can try it with an OpenRouter key instead of a first-party provider:
#
#   export OPENROUTER_API_KEY=<your-key>
#   docker agent run examples/budget.yaml \
#     "count from 1 to 40, calling the shell once per number"
#
# The TUI's Token Usage panel lists every active budget by name and tracks
# spend against it live, breaking each down per agent once more than one
# agent draws on it. When a ceiling is crossed the run stops with a message
# naming the exact YAML path to raise. The token cap below is deliberately
# low so the counting demo trips it partway through.
#
# providers.openrouter:
#   OpenRouter exposes an OpenAI-compatible API, so it is configured as a
#   custom provider with an explicit base_url. Its key is read from
#   OPENROUTER_API_KEY.
#
# models.budget-model.cost:
#   A custom base_url endpoint is not in the models.dev catalogue, so the
#   runtime cannot price it automatically — and an unpriced model does not
#   count against a max_cost limit. Declaring cost here (USD per 1M
#   tokens) makes max_cost enforceable. These match gpt-4o-mini's list
#   price; edit to match whatever model you point at.
#
# budgets:
#   Named budgets. An agent opts in by listing a name under its `budgets`.
#
#   A name is ONE shared pot: `shell-work` below is referenced by both agents,
#   so together they cannot spend more than it allows. That is what keeps a
#   budget meaningful when an agent fans out — ten sub-agents on one budget
#   still cannot spend ten times the limit. Use distinct names when you want
#   independent pots.
#
# budget:
#   The run-wide budget applies to every agent on top of any named budget it
#   references. The first ceiling to be exhausted — run-wide or named — stops
#   the run.
#
# budget.max_time:
#   Time the agents spend working — the sum of their turn durations, not
#   wall-clock since the session opened.
#
# agents.root.budgets:
#   root draws on the run-wide budget plus the shared shell-work pot.
#
# agents.researcher.budgets:
#   researcher shares shell-work with root — the same pot, not a copy —
#   and additionally has its own working-time ceiling.
resource budget "docker-agent:index:AgentConfig" {
  providers = {
    openrouter = {
      baseUrl  = "https://openrouter.ai/api/v1"
      tokenKey = "OPENROUTER_API_KEY"
    }
  }
  models = {
    "budget-model" = {
      provider = "openrouter"
      model    = "openai/gpt-4.1-mini"
      cost = {
        input  = 0.15
        output = 0.6
      }
    }
  }
  budgets = {
    "shell-work" = {
      maxCost   = 0.03
      maxTokens = 8000
    }
    research = {
      maxTime = "1m"
    }
  }
  budget = {
    maxCost   = 0.05
    maxTokens = 15000
    maxTime   = "2m"
  }
  agents = [
    {
      name        = "root"
      model       = "budget-model"
      description = "An agent on a short leash."
      instruction = <<EOT
You are a helpful assistant with shell access. Be concise.
Delegate research questions to the researcher sub-agent.
EOT
      toolsets = [
        {
          type = "shell"
        },
      ]
      subAgents = ["researcher"]
      budgets   = ["shell-work"]
    },
    {
      name        = "researcher"
      model       = "budget-model"
      description = "Answers questions without touching the shell."
      instruction = <<EOT
You answer questions concisely. You have no tools.
EOT
      budgets     = ["shell-work", "research"]
    },
  ]

  options {
    pluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
  }
}
