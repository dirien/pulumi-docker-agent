// Runs Claude Code as a coding-harness agent during `pulumi up` on the Claude subscription of the machine that
// runs Pulumi (no API key). AgentConfig authors the agent once; two AgentRuns use it.
import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();
// Optional: a token from `claude setup-token`, for CI or when the login of this machine expires:
// `pulumi config set --secret claudeCodeOauthToken <token>`.
const oauthToken = config.getSecret("claudeCodeOauthToken");
// `always` runs the notes agent on every `pulumi up`; `onChange` (default) only when an input changes.
const rerun = (config.get("rerun") ?? "onChange") as dockeragent.RerunPolicy;
// Without a token, Claude Code uses the `claude` login of this machine.
const env = oauthToken ? { CLAUDE_CODE_OAUTH_TOKEN: oauthToken } : undefined;

const writer = new dockeragent.AgentConfig("writer", {
    agents: [{
        name: "root",
        description: "Writes and reviews project notes with Claude Code",
        harness: { type: dockeragent.HarnessType.ClaudeCode, model: "sonnet", effort: dockeragent.HarnessEffort.Low },
    }],
});

// Run 1: maintains NOTES.md in ./workspace. Claude Code runs its own tools (shell, file edits) without approval there.
const notes = new dockeragent.AgentRun("notes", {
    yaml: writer.yaml,
    allowHarnessAgents: true,
    workingDir: "workspace",
    prompt: "Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.",
    // Sent on reruns while `prompt` is unchanged (triggers, rerun: always, ...).
    updatePrompt: "NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.",
    // Runs when this resource is deleted: `pulumi destroy`, removing it, or a replacement.
    deletePrompt: "Delete NOTES.md if it exists, then answer: done.",
    rerun,
    // Keep API keys (Claude Code would bill ANTHROPIC_API_KEY instead of the subscription) and Pulumi credentials
    // away from the CLI. HOME, PATH and the Claude configuration directory stay, so the machine's `claude` login
    // still works.
    inheritEnvironment: false,
    env,
    timeoutSeconds: 900,
}, {
    // The delete prompt undoes what the prompt creates, so a replacement runs it before the new create.
    deleteBeforeReplace: true,
});

// Run 2: the same agent with another prompt, run again whenever the notes run ran.
const review = new dockeragent.AgentRun("review", {
    yaml: writer.yaml,
    allowHarnessAgents: true,
    prompt: pulumi.interpolate`Review this summary of NOTES.md in two sentences: ${notes.output}`,
    triggers: [notes.ranAt],
    inheritEnvironment: false,
    env,
    timeoutSeconds: 600,
});

export const summary = notes.output;
export const reviewText = review.output;
export const usage = notes.usage;
export const toolCalls = notes.toolCalls;
