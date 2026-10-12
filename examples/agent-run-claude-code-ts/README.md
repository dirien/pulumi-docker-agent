# Run Claude Code during `pulumi up` (TypeScript)

Runs a [Claude Code](https://code.claude.com/docs/en/overview) agent with `AgentRun`, on the Claude
subscription of the machine that runs Pulumi, without an API key:

1. An `AgentConfig` authors the agent once: a `root` agent with a `claude-code` harness (`model: sonnet`,
   `effort: low`) instead of a docker-agent model.
2. The `notes` `AgentRun` sets `allowHarnessAgents: true` and runs Claude Code in `./workspace`, where it writes
   `NOTES.md`. On reruns it sends `updatePrompt`, and when the resource is deleted it sends `deletePrompt`, which
   removes the file again. `deleteBeforeReplace` makes a replacement run that cleanup before the new create.
3. The `review` `AgentRun` runs the same agent with another prompt that contains the answer of `notes`, in a
   temporary directory, and runs again whenever `notes` ran (`triggers: [notes.ranAt]`).

Both runs set `inheritEnvironment: false`: Claude Code gets the system variables (`PATH`, `HOME`, `USER`, ...), its
configuration locations (`CLAUDE_CONFIG_DIR`, `XDG_*_HOME`) and `env`, but not an `ANTHROPIC_API_KEY` of your shell,
which it would use and bill instead of the subscription, or your Pulumi and cloud credentials. A
`CLAUDE_CODE_OAUTH_TOKEN` exported in your shell does not reach it either: without the `claudeCodeOauthToken` setting,
Claude Code uses the login of the machine, and the preview warns that the token of your shell is not used.

**Claude Code runs its own tools (shell commands, file edits, web requests) without approval**, on the machine that
runs `pulumi up` and with the permissions of your user. The prompts keep it to `workspace/` and a temporary directory,
but nothing enforces that: try the example on a machine or in a container you can throw away.

## Run it

You need Claude Code (`claude` on `PATH`) on Linux or macOS, logged in to a Claude subscription, as a user other
than root.

```bash
# From the repository root
make provider build_nodejs

claude auth login     # once; the Claude subscription (--claudeai) is the default
claude auth status

cd examples/agent-run-claude-code-ts
pulumi stack init dev
pulumi install --no-plugins   # npm dependencies only; the plugin comes from ../../bin
pulumi up

cat workspace/NOTES.md
pulumi stack output summary
pulumi stack output reviewText
```

Optional settings:

| Setting | Default | Description |
|---|---|---|
| `claudeCodeOauthToken` | not set | A long-lived token from `claude setup-token` (`pulumi config set --secret claudeCodeOauthToken <token>`), passed as `CLAUDE_CODE_OAUTH_TOKEN`. Use it in CI, over SSH (where the macOS keychain is locked), or when runs fail with "OAuth session expired and could not be refreshed". |
| `rerun` | `onChange` | `always` runs `notes` again on every `pulumi up`, with its `updatePrompt`, and `review` follows. `pulumi refresh` still reports no changes. |

`pulumi destroy` runs the delete prompt of `notes`, which deletes `workspace/NOTES.md`. If it fails, for example
because the login expired, fix the login and run it again, or skip the prompt once with
`DOCKER_AGENT_SKIP_DELETE_PROMPTS=true pulumi destroy`.

`make test_examples` runs this program with a fake `claude` (`provider/pkg/agentrun/testdata/fake-claude`) on a
restricted `PATH`, so it needs no Claude login and never starts the real CLI.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the published SDK:
`npm install @ediri/pulumi-docker-agent`.
