// A CI gate built with the Pulumi Automation API: an inline Pulumi program defines a change-review agent
// (AgentConfig with structured output) and runs it once with AgentRun. The script deploys the program to a
// stack in a local file backend, reads the agent's structured answer with `stack.outputs()`, destroys the
// stack, prints the answer as JSON and exits with 1 when the agent asks for changes.
//
//   node bin/index.js [change-description-file]
//
// Settings (environment variables):
//   LLM_API_KEY                 API key of the model endpoint (default: OPENAI_API_KEY)
//   LLM_BASE_URL                any OpenAI-compatible chat completions endpoint (default: OpenAI)
//   LLM_MODEL                   the model (default: gpt-5-mini)
//   STATE_DIR                   directory of the file backend (default: ./.state)
//   DOCKER_AGENT_PROVIDER_PATH  directory with a pulumi-resource-docker-agent binary to use instead of the
//                               released plugin, for example the bin/ directory of this repository
//   PULUMI_CONFIG_PASSPHRASE    encrypts the API key in the stack configuration and state
import * as fs from "fs";
import * as path from "path";
import * as pulumi from "@pulumi/pulumi";
import { LocalWorkspace, ProjectSettings } from "@pulumi/pulumi/automation";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const projectName = "docker-agent-automation";
const stackName = "change-review";

/** The answer the agent must give: it is sent to the model as the response schema. */
interface Review {
    verdict: "approve" | "request_changes";
    risk: "low" | "medium" | "high";
    summary: string;
}

const defaultChange = "Upgrade the production PostgreSQL cluster from version 15 to 17 during the Sunday " +
    "maintenance window. The upgrade uses pg_upgrade in place; the rollback plan restores last night's backup.";

// The Pulumi program. It runs inside this process; every `pulumi up` evaluates it again.
async function program() {
    const config = new pulumi.Config();

    const reviewer = new dockeragent.AgentConfig("reviewer", {
        providers: {
            llm: {
                apiType: "openai_chatcompletions",
                baseUrl: config.require("baseUrl"),
                // The name of the environment variable that holds the key where the agent runs.
                tokenKey: "CHANGE_REVIEW_API_KEY",
            },
        },
        models: {
            reviewer: { provider: "llm", model: config.require("model") },
        },
        agents: [{
            name: "root",
            model: "reviewer",
            description: "Reviews infrastructure change requests",
            instruction: "You review infrastructure change requests. Approve a change only when it is low risk " +
                "or has a rollback plan. Rate its risk and explain your verdict in one sentence.",
            structuredOutput: {
                name: "change_review",
                strict: true,
                schema: {
                    type: "object",
                    properties: {
                        verdict: { type: "string", enum: ["approve", "request_changes"] },
                        risk: { type: "string", enum: ["low", "medium", "high"] },
                        summary: { type: "string" },
                    },
                    required: ["verdict", "risk", "summary"],
                    additionalProperties: false,
                },
            },
        }],
    });

    // Runs the agent during `pulumi up`. The state keeps the answer: another `up` with the same change
    // description and settings does not call the model again.
    const review = new dockeragent.AgentRun("review", {
        yaml: reviewer.yaml,
        prompt: config.require("change"),
        // The key is a secret stack setting and the only credential the run sees.
        env: { CHANGE_REVIEW_API_KEY: config.requireSecret("apiKey") },
        inheritEnvironment: false,
        timeoutSeconds: 120,
    });

    return {
        review: review.structuredOutput,
        usage: review.usage,
    };
}

async function main(): Promise<number> {
    const apiKey = process.env.LLM_API_KEY ?? process.env.OPENAI_API_KEY;
    if (!apiKey) {
        throw new Error("set LLM_API_KEY (or OPENAI_API_KEY) to the API key of the model endpoint");
    }
    const change = process.argv[2] ? fs.readFileSync(process.argv[2], "utf8") : defaultChange;
    const stateDir = path.resolve(process.env.STATE_DIR ?? ".state");
    fs.mkdirSync(stateDir, { recursive: true });

    // The project file the workspace writes. `plugins.providers` points the engine at a local provider
    // build; without it, the engine downloads the released plugin that the SDK names.
    const providerDir = process.env.DOCKER_AGENT_PROVIDER_PATH;
    const projectSettings: ProjectSettings & { plugins?: { providers: { name: string; path: string }[] } } = {
        name: projectName,
        runtime: "nodejs",
        ...(providerDir ? { plugins: { providers: [{ name: "docker-agent", path: path.resolve(providerDir) }] } } : {}),
    };

    const stack = await LocalWorkspace.createOrSelectStack({ projectName, stackName, program }, {
        projectSettings,
        envVars: {
            // A local file backend: no Pulumi Cloud account is needed.
            PULUMI_BACKEND_URL: `file://${stateDir}`,
            PULUMI_CONFIG_PASSPHRASE: process.env.PULUMI_CONFIG_PASSPHRASE ?? "docker-agent-automation-example",
            PULUMI_SKIP_UPDATE_CHECK: "true",
        },
    });

    await stack.setAllConfig({
        baseUrl: { value: process.env.LLM_BASE_URL ?? "https://api.openai.com/v1" },
        model: { value: process.env.LLM_MODEL ?? "gpt-5-mini" },
        change: { value: change },
        apiKey: { value: apiKey, secret: true },
    });

    // Engine output goes to stderr, so stdout only carries the result.
    const log = (out: string) => process.stderr.write(out);
    let review: Review;
    let usage: unknown;
    try {
        await stack.up({ onOutput: log });
        const outputs = await stack.outputs();
        review = outputs["review"].value as Review;
        usage = outputs["usage"].value;
    } finally {
        // A CI gate runs once per change: remove everything again. Keep the stack instead to reuse the
        // stored answer while the inputs stay the same.
        await stack.destroy({ onOutput: log });
        await stack.workspace.removeStack(stackName);
    }

    process.stdout.write(JSON.stringify({ ...review, usage }) + "\n");
    return review.verdict === "approve" ? 0 : 1;
}

main().then(
    code => { process.exitCode = code; },
    err => {
        process.stderr.write(`${err instanceof Error ? err.message : String(err)}\n`);
        process.exitCode = 2;
    },
);
