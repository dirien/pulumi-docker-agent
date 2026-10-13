// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// The policies of the Docker Agent policy pack. index.ts registers them; the tests import this
// module directly, because constructing a PolicyPack starts the policy analyzer server.
//
// Values that the provider may turn into enums later (toolApproval, safety, toolset types) are
// compared as plain strings, so the policies keep working when an SDK types them as enums with the
// same string values.

import * as dockeragent from "@ediri/pulumi-docker-agent";
import {
    remediateResourceOfType,
    ReportViolation,
    ResourceValidationArgs,
    ResourceValidationPolicy,
    UnknownValueError,
    validateResourceOfType,
} from "@pulumi/policy";

type Obj = Record<string, unknown>;

// ---------------------------------------------------------------- helpers

/**
 * Reads a value that may be unknown during `pulumi preview` (an output of another resource that is
 * not created yet). Unknown values read as `undefined`, so a policy skips them during the preview
 * and checks them during the update, when the engine runs the policies again with known values.
 */
export function known<T>(read: () => T): T | undefined {
    try {
        return read();
    } catch (e) {
        if (e instanceof UnknownValueError) {
            return undefined;
        }
        throw e;
    }
}

function isObj(v: unknown): v is Obj {
    return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Returns obj[key], or undefined when it is unknown. */
function get(obj: unknown, key: string): unknown {
    if (!isObj(obj) && !Array.isArray(obj)) {
        return undefined;
    }
    return known(() => (obj as Obj)[key]);
}

/** Returns the known entries of an object (or array), skipping unknown values. */
function entries(obj: unknown): [string, unknown][] {
    if (!isObj(obj) && !Array.isArray(obj)) {
        return [];
    }
    const out: [string, unknown][] = [];
    for (const key of Object.keys(obj)) {
        const value = get(obj, key);
        if (value !== undefined) {
            out.push([key, value]);
        }
    }
    return out;
}

/** Returns the known elements of an array value, or [] for anything else. */
function list(v: unknown): unknown[] {
    return Array.isArray(v) ? entries(v).map(([, x]) => x) : [];
}

/** Returns a known string value as a plain string (enum values included). */
function str(v: unknown): string | undefined {
    return typeof v === "string" ? String(v) : undefined;
}

/** Returns the known strings of a string-array value. */
function strings(v: unknown): string[] {
    return list(v).map(str).filter((s): s is string => s !== undefined);
}

function escapeRegExp(s: string): string {
    return s.replace(/[.+?^${}()|[\]\\]/g, "\\$&");
}

/** Matches a value against a pattern in which `*` matches any sequence of characters. */
export function globMatch(pattern: string, value: string, ignoreCase = false): boolean {
    const re = new RegExp("^" + pattern.split("*").map(escapeRegExp).join(".*") + "$", ignoreCase ? "i" : "");
    return re.test(value);
}

function matchesAny(value: string, patterns: string[], ignoreCase = false): boolean {
    return patterns.some(p => globMatch(p, value, ignoreCase));
}

function quote(values: string[]): string {
    return values.map(v => `"${v}"`).join(", ");
}

/**
 * Describes a URL for a message without what may be a credential: only its scheme and host (no
 * user info, path, query or fragment). Anything that does not parse as a URL is not shown.
 */
export function urlOrigin(value: string): string | undefined {
    try {
        const u = new URL(value);
        return u.host ? `${u.protocol}//${u.host}` : undefined;
    } catch {
        return undefined;
    }
}

/** A plain name, such as a Docker MCP Catalog ref ("docker:duckduckgo"), that is safe to show. */
const plainName = /^[A-Za-z0-9][A-Za-z0-9._\/:-]{0,127}$/;

/** A registry host (with an optional port) that is safe to show. */
const registryHost = /^[A-Za-z0-9.-]+(:[0-9]+)?$/;

/** The configuration of a policy, with the defaults applied for missing values. */
function config<T extends object>(args: ResourceValidationArgs, defaults: T): T {
    return { ...defaults, ...args.getConfig<Partial<T>>() };
}

// ---------------------------------------------------------------- AgentConfig accessors

/** The agents of an AgentConfig (a list of agents with a `name`). */
function agentsOf(cfg: Obj): Obj[] {
    return list(get(cfg, "agents")).filter(isObj);
}

function agentName(agent: Obj, index: number): string {
    return str(get(agent, "name")) ?? `agents[${index}]`;
}

/**
 * The toolsets an agent uses: its own `toolsets` and the shared toolsets of the configuration that
 * it names in `useToolsets`.
 */
function toolsetsOf(cfg: Obj, agent: Obj): Obj[] {
    const shared = get(cfg, "toolsets");
    const named = strings(get(agent, "useToolsets")).map(name => get(shared, name));
    return [...list(get(agent, "toolsets")), ...named].filter(isObj);
}

/** Every toolset of the configuration with a label for messages. */
function allToolsets(cfg: Obj): { label: string; toolset: Obj }[] {
    const out: { label: string; toolset: Obj }[] = [];
    agentsOf(cfg).forEach((agent, i) => {
        list(get(agent, "toolsets")).forEach((t, j) => {
            if (isObj(t)) {
                out.push({ label: `agent "${agentName(agent, i)}" toolsets[${j}]`, toolset: t });
            }
        });
    });
    for (const [name, t] of entries(get(cfg, "toolsets"))) {
        if (isObj(t)) {
            out.push({ label: `toolset "${name}"`, toolset: t });
        }
    }
    return out;
}

// ---------------------------------------------------------------- agentrun-tool-approval-deny

export interface ToolApprovalConfig {
    /**
     * AgentRun resources that may set `toolApproval: allow`, `allowHookApprovals: true` or
     * `allowHarnessAgents: true`, by name or URN (`*` wildcards).
     */
    allowedResources: string[];
}

function isAllowlisted(args: ResourceValidationArgs, allowed: string[]): boolean {
    return matchesAny(args.name, allowed) || matchesAny(args.urn, allowed);
}

/**
 * What makes an AgentRun run tool calls that docker-agent would ask a user to confirm: toolApproval
 * "allow", approvals from hooks, and coding-harness agents, whose CLI runs its own tools.
 */
function unattendedApprovals(run: object): { approval?: string; hooks: boolean; harness: boolean } {
    const approval = str(known(() => (run as Obj).toolApproval));
    const hooks = known(() => (run as Obj).allowHookApprovals) === true;
    const harness = known(() => (run as Obj).allowHarnessAgents) === true;
    return { approval: approval !== undefined && approval !== "deny" ? approval : undefined, hooks, harness };
}

export const agentRunToolApprovalDeny: ResourceValidationPolicy = {
    name: "agentrun-tool-approval-deny",
    description: "AgentRun must keep toolApproval \"deny\" and must not set allowHookApprovals or " +
        "allowHarnessAgents: \"allow\" runs every tool call of the agent (shell commands, file writes, fetches) " +
        "unattended on the machine that runs `pulumi up`, allowHookApprovals lets the configuration's hooks approve " +
        "any tool call under \"deny\", and allowHarnessAgents runs coding-harness agents (Claude Code, Codex), whose " +
        "CLI runs its own tool calls without approval.",
    enforcementLevel: "mandatory",
    remediationSteps: "Remove toolApproval or set it to \"deny\", remove allowHookApprovals and " +
        "allowHarnessAgents, or add the resource to allowedResources of this policy. With the enforcement level " +
        "\"remediate\", the policy sets toolApproval to \"deny\", and allowHookApprovals and allowHarnessAgents " +
        "to false.",
    configSchema: {
        properties: {
            allowedResources: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "AgentRun resources that may set toolApproval \"allow\", allowHookApprovals or " +
                    "allowHarnessAgents, by resource name or URN. `*` matches any characters.",
            },
        },
    },
    validateResource: validateResourceOfType(dockeragent.AgentRun, (run, args, reportViolation) => {
        const { approval, hooks, harness } = unattendedApprovals(run);
        if ((approval === undefined && !hooks && !harness) ||
            isAllowlisted(args, config<ToolApprovalConfig>(args, { allowedResources: [] }).allowedResources)) {
            return;
        }
        if (approval !== undefined) {
            reportViolation(`AgentRun "${args.name}" sets toolApproval "${approval}": every tool call of the agent ` +
                "runs without approval on the machine that runs `pulumi up`. Keep the default \"deny\", or add the " +
                "resource to allowedResources of this policy.");
        } else if (hooks) {
            // With toolApproval "allow", hook approvals change nothing.
            reportViolation(`AgentRun "${args.name}" sets allowHookApprovals: a pre_tool_use or permission_request ` +
                "hook of the configuration can approve any tool call under toolApproval \"deny\", so the hook, not " +
                "docker-agent's rules, decides what runs on the machine that runs `pulumi up`. Remove " +
                "allowHookApprovals, or add the resource to allowedResources of this policy.");
        }
        if (harness) {
            // toolApproval does not apply to Claude Code, and for Codex it only selects the sandbox, so this is
            // reported on its own.
            reportViolation(`AgentRun "${args.name}" sets allowHarnessAgents: a coding-harness agent runs every tool ` +
                "call of its CLI (shell commands, file edits) without approval on the machine that runs `pulumi up` " +
                "(Claude Code always, Codex in its workspace-write sandbox under toolApproval \"deny\", which limits " +
                "writes and network but not reads). Remove allowHarnessAgents, or add the resource to " +
                "allowedResources of this policy.");
        }
    }),
    remediateResource: remediateResourceOfType(dockeragent.AgentRun, (run, args) => {
        const { approval, hooks, harness } = unattendedApprovals(run);
        if ((approval === undefined && !hooks && !harness) ||
            isAllowlisted(args, config<ToolApprovalConfig>(args, { allowedResources: [] }).allowedResources)) {
            return undefined;
        }
        if (approval !== undefined) {
            (run as unknown as Obj).toolApproval = "deny";
        }
        if (hooks) {
            (run as unknown as Obj).allowHookApprovals = false;
        }
        if (harness) {
            (run as unknown as Obj).allowHarnessAgents = false;
        }
        return run;
    }),
};

// ---------------------------------------------------------------- agentrun-reference-pinned

const digestPattern = /@sha256:[0-9a-f]{64}$|@sha512:[0-9a-f]{128}$/;

/** Reports whether an OCI reference is pinned by digest. */
export function isPinned(reference: string): boolean {
    return digestPattern.test(reference);
}

export const agentRunReferencePinned: ResourceValidationPolicy = {
    name: "agentrun-reference-pinned",
    description: "AgentRun should pin `reference` by digest (`registry/repository@sha256:...`), so a moved " +
        "tag cannot change the agent that runs.",
    enforcementLevel: "advisory",
    remediationSteps: "Use the pinnedReference output of AgentArtifact or getAgent, or append the manifest " +
        "digest to the reference.",
    validateResource: validateResourceOfType(dockeragent.AgentRun, (run, args, reportViolation) => {
        const reference = str(known(() => run.reference));
        if (reference === undefined || isPinned(reference)) {
            return;
        }
        reportViolation(`AgentRun "${args.name}" runs an agent from ${describeRegistry(reference)} by tag, not by ` +
            "digest: the agent is pulled when it runs, and whoever can push that tag decides what runs. Use a " +
            "reference with @sha256:..., for example the pinnedReference output of AgentArtifact or getAgent.");
    }),
};

// ---------------------------------------------------------------- agentconfig-no-autonomous-safety

export const agentConfigNoAutonomousSafety: ResourceValidationPolicy = {
    name: "agentconfig-no-autonomous-safety",
    description: "AgentConfig must not use the \"autonomous\" safety mode, which auto-approves every tool call " +
        "for whoever runs the agent.",
    enforcementLevel: "mandatory",
    remediationSteps: "Use \"restricted\" for unattended runs, or \"balanced\" or \"strict\" for interactive " +
        "use. (UserSettings can override an agent's safety mode on a user's machine, which " +
        "usersettings-no-autonomous-safety checks.)",
    validateResource: validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
        const c = cfg as unknown as Obj;
        const where: string[] = [];
        const runtime = get(c, "runtime");
        for (const [label, value] of [
            ["runtime.safety", get(runtime, "safety")],
            ["runtime.extra.safety", get(get(runtime, "extra"), "safety")],
        ] as const) {
            if (str(value) === "autonomous") {
                where.push(label);
            }
        }
        agentsOf(c).forEach((agent, i) => {
            if (str(get(agent, "safety")) === "autonomous" || str(get(get(agent, "extra"), "safety")) === "autonomous") {
                where.push(`agent "${agentName(agent, i)}"`);
            }
        });
        if (where.length > 0) {
            reportViolation(`AgentConfig "${args.name}" uses the "autonomous" safety mode (${where.join(", ")}), ` +
                "which approves every tool call without asking. Use \"restricted\", \"balanced\" or \"strict\".");
        }
    }),
};

// ---------------------------------------------------------------- agentconfig-sandbox-for-local-tools

export interface SandboxConfig {
    /** Toolset types that run commands or touch files on the machine running the agent. */
    toolsetTypes: string[];
}

export const defaultLocalToolsetTypes = ["shell", "script", "filesystem", "file", "background_jobs"];

export const agentConfigSandboxForLocalTools: ResourceValidationPolicy = {
    name: "agentconfig-sandbox-for-local-tools",
    description: "AgentConfig agents that use local toolsets (shell, script, filesystem, background_jobs) " +
        "should run in a sandbox (`runtime.sandbox: true`).",
    enforcementLevel: "advisory",
    remediationSteps: "Set runtime.sandbox to true, so `docker agent run` runs the agent in a Docker sandbox " +
        "by default. (AgentRun runs agents on the machine that runs `pulumi up`, in a child process of the provider, " +
        "and does not use the sandbox.)",
    configSchema: {
        properties: {
            toolsetTypes: {
                type: "array",
                items: { type: "string" },
                default: defaultLocalToolsetTypes,
                description: "Toolset types that run commands or access files on the machine that runs the agent.",
            },
        },
    },
    validateResource: validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
        const c = cfg as unknown as Obj;
        if (get(get(c, "runtime"), "sandbox") === true) {
            return;
        }
        const local = config<SandboxConfig>(args, { toolsetTypes: defaultLocalToolsetTypes }).toolsetTypes;
        const offenders: string[] = [];
        agentsOf(c).forEach((agent, i) => {
            const types = toolsetsOf(c, agent)
                .map(t => str(get(t, "type")))
                .filter((t): t is string => t !== undefined && local.includes(t));
            if (types.length > 0) {
                offenders.push(`"${agentName(agent, i)}" (${[...new Set(types)].join(", ")})`);
            }
        });
        if (offenders.length > 0) {
            reportViolation(`AgentConfig "${args.name}" gives local tools to agents ${offenders.join(", ")} but ` +
                "does not set runtime.sandbox: true, so `docker agent run` runs them directly on the user's " +
                "machine.");
        }
    }),
};

// ---------------------------------------------------------------- agentconfig-no-literal-credentials

/** Keys of maps whose values are environment variables or HTTP headers. */
const credentialMapKeys = new Set(["env", "headers", "httpHeaders", "http_headers"]);

/** Names of variables and headers that carry credentials. */
const credentialName = /authorization|api[-_]?key|token|secret|passw(or)?d|credential|access[-_]?key|private[-_]?key|cookie/i;

/** Values that look like well-known credentials, whatever their name. */
const credentialValue = [
    /\bsk-[A-Za-z0-9_-]{20,}/, // OpenAI, Anthropic (sk-ant-...), DeepSeek, ...
    /\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{30,}/, // GitHub tokens
    /\bgithub_pat_[A-Za-z0-9_]{30,}/,
    /\bglpat-[A-Za-z0-9_-]{20,}/, // GitLab
    /\bxox[abprs]-[A-Za-z0-9-]{10,}/, // Slack
    /\b(AKIA|ASIA)[0-9A-Z]{16}\b/, // AWS access key IDs
    /\bAIza[0-9A-Za-z_-]{35}\b/, // Google API keys
    /\bhf_[A-Za-z0-9]{30,}/, // Hugging Face
    /\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{16,}/, // literal Authorization header values
];

/** Placeholders that docker-agent expands when the agent runs, for example `${env.GITHUB_TOKEN}`. */
const placeholder = /\$\{[^}]*\}/g;

/** Reports whether a variable or header value holds a literal credential. */
export function isLiteralCredential(name: string, value: string): boolean {
    const literal = value.replace(placeholder, "");
    if (credentialValue.some(re => re.test(literal))) {
        return true;
    }
    if (!credentialName.test(name)) {
        return false;
    }
    // "Bearer ${env.TOKEN}" is fine; "Bearer abcdef..." or a bare key is not. Short values such as
    // "1" or "true" (for example TOKEN_LIMIT: "100") are not credentials.
    const rest = literal.replace(/^\s*(Bearer|Basic|Token)\s*/i, "").trim();
    return rest.length >= 8;
}

const envVarName = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Walks an AgentConfig and collects literal credentials in env and header maps and token keys. */
function findLiteralCredentials(value: unknown, path: string, out: string[]): void {
    for (const [key, child] of entries(value)) {
        const childPath = Array.isArray(value) ? `${path}[${key}]` : path ? `${path}.${key}` : key;
        if (credentialMapKeys.has(key) && isObj(child)) {
            for (const [name, v] of entries(child)) {
                const s = str(v);
                if (s !== undefined && isLiteralCredential(name, s)) {
                    out.push(`${childPath}.${name}`);
                }
            }
            continue;
        }
        if (key === "tokenKey" || key === "token_key") {
            const s = str(child);
            if (s !== undefined && !envVarName.test(s)) {
                out.push(childPath);
            }
            continue;
        }
        findLiteralCredentials(child, childPath, out);
    }
}

export const agentConfigNoLiteralCredentials: ResourceValidationPolicy = {
    name: "agentconfig-no-literal-credentials",
    description: "AgentConfig must not contain literal credentials in `env` or `headers` (or a key in " +
        "`tokenKey`): they end up in the rendered YAML and in every artifact pushed from it. Use " +
        "`${env.NAME}` references instead.",
    enforcementLevel: "mandatory",
    remediationSteps: "Replace the value with a `${env.NAME}` reference (for example `Authorization: Bearer " +
        "${env.GITHUB_TOKEN}`) and provide the variable where the agent runs: AgentRun's secret `env` input, a " +
        "Pulumi ESC environment, or the shell of `docker agent run`. A Pulumi secret does not help here: the " +
        "value is still written into the configuration.",
    validateResource: validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
        const found: string[] = [];
        findLiteralCredentials(cfg, "", found);
        if (found.length > 0) {
            reportViolation(`AgentConfig "${args.name}" contains literal credentials at ${found.join(", ")}. ` +
                "They are written into the YAML and every artifact pushed from it. Use ${env.NAME} references " +
                "(tokenKey takes the name of the variable) and set the values where the agent runs.");
        }
    }),
};

// ---------------------------------------------------------------- agentconfig-allowed-model-providers

export interface ModelProvidersConfig {
    /** Model provider types the configurations may use, for example "anthropic" or "amazon-bedrock". */
    allowedProviders: string[];
}

/** Keys whose string value references a model (`name` of `models`, or `provider/model`). */
const modelRefKeys = new Set([
    "model", "compactionModel", "titleModel", "chatModel", "embeddingModel",
    "compaction_model", "title_model", "chat_model", "embedding_model",
]);
/** Keys whose string-array value references models. */
const modelRefListKeys = new Set(["models", "firstAvailable", "first_available"]);

/** The provider type behind a provider name: custom providers resolve to their `provider` (default openai). */
function providerType(cfg: Obj, name: string): { type: string; via?: string } {
    const custom = get(get(cfg, "providers"), name);
    if (isObj(custom)) {
        return { type: str(get(custom, "provider")) ?? "openai", via: name };
    }
    return { type: name };
}

/** Collects `provider/model` references outside of the `models` definitions. */
function collectModelRefs(value: unknown, path: string, out: { path: string; ref: string }[]): void {
    for (const [key, child] of entries(value)) {
        const childPath = Array.isArray(value) ? `${path}[${key}]` : path ? `${path}.${key}` : key;
        // A model definition's own `model` is the model ID ("openai/gpt-5-mini" is a valid OpenRouter
        // model ID), and a coding harness names the model of an external tool: neither is a reference.
        if (path === "" && key === "models") {
            for (const [name, def] of entries(child)) {
                for (const [k, v] of entries(def)) {
                    if (k !== "model" && k !== "provider") {
                        collectModelRefs({ [k]: v }, `models.${name}`, out);
                    }
                }
            }
            continue;
        }
        if (key === "harness") {
            continue;
        }
        if (modelRefKeys.has(key) && typeof child === "string") {
            out.push({ path: childPath, ref: child });
        } else if (modelRefListKeys.has(key) && Array.isArray(child)) {
            strings(child).forEach((ref, i) => out.push({ path: `${childPath}[${i}]`, ref }));
        } else {
            collectModelRefs(child, childPath, out);
        }
    }
}

/** Lists the model provider types an AgentConfig uses, with where they are used. */
export function modelProviderUses(cfg: Obj): { type: string; where: string }[] {
    const uses: { type: string; where: string }[] = [];
    const models = get(cfg, "models");
    for (const [name, def] of entries(models)) {
        const provider = str(get(def, "provider"));
        if (provider === undefined) {
            continue;
        }
        const { type, via } = providerType(cfg, provider);
        uses.push({ type, where: via ? `model "${name}" (provider "${via}")` : `model "${name}"` });
    }
    const refs: { path: string; ref: string }[] = [];
    collectModelRefs(cfg, "", refs);
    for (const { path, ref } of refs) {
        // An alloy model lists several models separated by commas.
        for (const part of ref.split(",").map(s => s.trim())) {
            if (part === "" || get(models, part) !== undefined || !part.includes("/")) {
                continue; // a named model (checked above), or not a provider/model reference
            }
            const { type, via } = providerType(cfg, part.slice(0, part.indexOf("/")));
            uses.push({ type, where: via ? `${path} "${part}" (provider "${via}")` : `${path} "${part}"` });
        }
    }
    return uses;
}

export const agentConfigAllowedModelProviders: ResourceValidationPolicy = {
    name: "agentconfig-allowed-model-providers",
    description: "AgentConfig may only use the model providers in allowedProviders. Disabled by default; " +
        "an enabled policy without allowedProviders allows none.",
    enforcementLevel: "disabled",
    remediationSteps: "Use a model of an allowed provider, or add the provider to allowedProviders. Custom " +
        "providers are checked by their provider type (`provider`, default \"openai\").",
    configSchema: {
        properties: {
            allowedProviders: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Allowed model provider types, for example \"anthropic\", \"openai\", " +
                    "\"amazon-bedrock\", \"google\" or \"dmr\".",
            },
        },
    },
    validateResource: validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
        const allowed = config<ModelProvidersConfig>(args, { allowedProviders: [] }).allowedProviders;
        const denied = modelProviderUses(cfg as unknown as Obj).filter(u => !allowed.includes(u.type));
        if (denied.length > 0) {
            reportViolation(`AgentConfig "${args.name}" uses model providers that are not allowed: ` +
                `${denied.map(d => `${d.type} by ${d.where}`).join("; ")}. Allowed: ` +
                `${allowed.length > 0 ? quote(allowed) : "none (set allowedProviders)"}.`);
        }
    }),
};

// ---------------------------------------------------------------- agentconfig-allowed-mcp-refs

export interface McpConfig {
    /** Allowed `ref` values of MCP toolsets and definitions, for example "docker:duckduckgo" or "docker:*". */
    allowedRefs: string[];
    /** Allowed URLs of remote MCP servers (`remote.url`); `*` matches any characters. */
    allowedRemoteUrls: string[];
    /** Allow MCP servers that the agent starts with `command`. */
    allowCommand: boolean;
}

/**
 * Checks one MCP server definition (an `mcp` toolset or an entry of `mcps`). The messages name the
 * location and show a ref only when it is a plain name and a URL only by scheme and host: a URL or
 * a command can carry a credential (a token in the query, user info, a `--token` argument).
 */
function checkMcpServer(cfg: Obj, label: string, server: Obj, conf: McpConfig, out: string[]): void {
    const ref = str(get(server, "ref"));
    if (ref !== undefined) {
        if (isObj(get(get(cfg, "mcps"), ref))) {
            return; // a named definition of `mcps`, checked on its own
        }
        if (!matchesAny(ref, conf.allowedRefs)) {
            const origin = urlOrigin(ref);
            out.push(origin !== undefined ? `${label} ref (${origin})` :
                !ref.includes("//") && plainName.test(ref) ? `${label} ref "${ref}"` : `${label} ref`);
        }
    }
    const url = str(get(get(server, "remote"), "url"));
    if (url !== undefined && !matchesAny(url, conf.allowedRemoteUrls)) {
        const origin = urlOrigin(url);
        out.push(origin !== undefined ? `${label} remote (${origin})` : `${label} remote`);
    }
    const command = str(get(server, "command"));
    if (command !== undefined && !conf.allowCommand) {
        out.push(`${label} command`);
    }
}

export const agentConfigAllowedMcpRefs: ResourceValidationPolicy = {
    name: "agentconfig-allowed-mcp-refs",
    description: "AgentConfig may only use the MCP servers in allowedRefs (and allowedRemoteUrls; servers " +
        "started with `command` need allowCommand). Disabled by default.",
    enforcementLevel: "disabled",
    remediationSteps: "Use an allowed MCP server, or add its ref (for example \"docker:duckduckgo\", or " +
        "\"docker:*\" for the Docker MCP Catalog) to allowedRefs.",
    configSchema: {
        properties: {
            allowedRefs: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Allowed MCP refs, for example \"docker:duckduckgo\". `*` matches any characters.",
            },
            allowedRemoteUrls: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Allowed remote MCP server URLs. `*` matches any characters.",
            },
            allowCommand: {
                type: "boolean",
                default: false,
                description: "Allow MCP servers that the agent starts as local processes with `command`.",
            },
        },
    },
    validateResource: validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
        const c = cfg as unknown as Obj;
        const conf = config<McpConfig>(args, { allowedRefs: [], allowedRemoteUrls: [], allowCommand: false });
        const denied: string[] = [];
        for (const { label, toolset } of allToolsets(c)) {
            if (str(get(toolset, "type")) === "mcp") {
                checkMcpServer(c, label, toolset, conf, denied);
            }
        }
        for (const [name, def] of entries(get(c, "mcps"))) {
            if (isObj(def)) {
                checkMcpServer(c, `mcps "${name}"`, def, conf, denied);
            }
        }
        if (denied.length > 0) {
            reportViolation(`AgentConfig "${args.name}" uses MCP servers that are not allowed: ${denied.join("; ")}. ` +
                `Allowed refs: ${conf.allowedRefs.length > 0 ? quote(conf.allowedRefs) : "none (set allowedRefs)"}.`);
        }
    }),
};

// ---------------------------------------------------------------- agentartifact-allowed-registries

export interface RegistriesConfig {
    /**
     * Registries (or repository prefixes) that agents may be pushed to and run from, for example
     * "ghcr.io/my-org" or "*.azurecr.io".
     */
    allowedRegistries: string[];
}

const dockerHubHosts = new Set(["docker.io", "index.docker.io", "registry-1.docker.io"]);

/** Splits an OCI reference into its registry host and repository path, like the docker CLI. */
export function parseReference(reference: string): { registry: string; repository: string } {
    let name = reference.split("@")[0];
    const slash = name.lastIndexOf("/");
    const colon = name.lastIndexOf(":");
    if (colon > slash) {
        name = name.slice(0, colon); // drop the tag
    }
    const first = name.split("/")[0];
    if (name.includes("/") && (first.includes(".") || first.includes(":") || first === "localhost")) {
        const registry = first.toLowerCase();
        const repository = name.slice(first.length + 1);
        return { registry: dockerHubHosts.has(registry) ? "docker.io" : registry, repository };
    }
    return { registry: "docker.io", repository: name.includes("/") ? name : `library/${name}` };
}

/** Names the registry of a reference for a message, without the repository, tag or digest. */
export function describeRegistry(reference: string): string {
    const { registry } = parseReference(reference);
    return registryHost.test(registry) ? `registry "${registry}"` : "a registry";
}

/** Reports whether a reference is in one of the allowed registries or repository prefixes. */
export function isAllowedRegistry(reference: string, allowed: string[]): boolean {
    const { registry, repository } = parseReference(reference);
    const full = `${registry}/${repository}`;
    return allowed.some(entry => {
        const e = entry.replace(/\/+$/, "");
        return globMatch(e, registry, true) || globMatch(e, full, true) ||
            full.toLowerCase().startsWith(e.toLowerCase() + "/");
    });
}

function checkRegistry(kind: string, reference: string | undefined, args: ResourceValidationArgs,
    reportViolation: ReportViolation): void {
    if (reference === undefined) {
        return;
    }
    const allowed = config<RegistriesConfig>(args, { allowedRegistries: [] }).allowedRegistries;
    if (!isAllowedRegistry(reference, allowed)) {
        reportViolation(`${kind} "${args.name}" uses a reference in ${describeRegistry(reference)} that is not ` +
            `allowed. Allowed: ${allowed.length > 0 ? quote(allowed) : "none (set allowedRegistries)"}.`);
    }
}

export const agentArtifactAllowedRegistries: ResourceValidationPolicy = {
    name: "agentartifact-allowed-registries",
    description: "AgentArtifact may only push to, and AgentRun may only run agents from, the registries in " +
        "allowedRegistries. Disabled by default.",
    enforcementLevel: "disabled",
    remediationSteps: "Push the agent to an allowed registry, or add the registry or repository prefix to " +
        "allowedRegistries.",
    configSchema: {
        properties: {
            allowedRegistries: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Allowed registry hosts or repository prefixes, for example \"ghcr.io/my-org\" or " +
                    "\"*.azurecr.io\". Docker Hub is \"docker.io\". `*` matches any characters.",
            },
        },
    },
    validateResource: [
        validateResourceOfType(dockeragent.AgentArtifact, (artifact, args, reportViolation) =>
            checkRegistry("AgentArtifact", str(known(() => artifact.reference)), args, reportViolation)),
        validateResourceOfType(dockeragent.AgentRun, (run, args, reportViolation) =>
            checkRegistry("AgentRun", str(known(() => run.reference)), args, reportViolation)),
    ],
};

// ---------------------------------------------------------------- permissions-no-wildcard-allow

export interface WildcardPermissionsConfig {
    /**
     * Tools whose every call a `permissions.allow` pattern must not approve: tools that run commands or
     * change files. Names as docker-agent reports them, for example "shell" or "mcp:github:create_issue".
     */
    dangerousTools: string[];
    /** Allow patterns that are accepted anyway, compared exactly (for example "write_file"). */
    allowedPatterns: string[];
    /** AgentConfig and UserSettings resources that are exempt, by name or URN (`*` wildcards). */
    allowedResources: string[];
}

export const defaultDangerousTools = [
    "shell", "run_background_job", "write_file", "edit_file", "create_directory", "remove_directory",
    "run_background_agent",
];

/** The ":key=" boundary that starts an argument condition of a permission pattern (as in docker-agent). */
const argCondition = /:(\w+)=/g;

/** Splits a permission pattern into its tool-name glob and its argument conditions, like docker-agent. */
export function parsePermission(pattern: string): { tool: string; conditions: [string, string][] } {
    const keys = [...pattern.matchAll(argCondition)].map(m => m[1]);
    const values = pattern.split(/:\w+=/);
    return { tool: values[0], conditions: keys.map((k, i) => [k, values[i + 1]]) };
}

/** Translates a docker-agent permission glob (`*`, `?`, `[...]`, `\x`, case-insensitive) into a RegExp. */
export function permissionGlob(pattern: string): RegExp {
    const literal = (ch: string) => ch.replace(/[.*+?^${}()|[\]\\/-]/g, "\\$&");
    let re = "";
    for (let i = 0; i < pattern.length; i++) {
        const c = pattern[i];
        if (c === "*") {
            re += "[\\s\\S]*";
        } else if (c === "?") {
            re += "[\\s\\S]";
        } else if (c === "\\" && i + 1 < pattern.length) {
            re += literal(pattern[++i]);
        } else if (c === "[" && pattern.indexOf("]", i + 2) > 0) {
            const end = pattern.indexOf("]", i + 2);
            let body = pattern.slice(i + 1, end);
            const negate = body.startsWith("^");
            body = (negate ? body.slice(1) : body).replace(/\\(.)/g, "$1").replace(/[\\\]^[]/g, "\\$&");
            re += `[${negate ? "^" : ""}${body}]`;
            i = end;
        } else {
            re += literal(c);
        }
    }
    return new RegExp(`^${re}$`, "i");
}

/** Reports whether a glob matches every value: only `*` and `?`, with at least one `*`. */
function matchesEverything(glob: string): boolean {
    return /^[*?]*\*[*?]*$/.test(glob);
}

/**
 * Describes why an allow pattern approves every call of a tool (or of every tool), or returns
 * undefined when it is narrower. Only the tool part of the pattern is shown: argument values can
 * hold anything.
 */
export function toolWideAllow(pattern: string, dangerousTools: string[]): string | undefined {
    const { tool, conditions } = parsePermission(pattern);
    if (matchesEverything(tool)) {
        return `"${tool}"${conditions.length > 0 ? " with argument conditions" : ""} matches every tool`;
    }
    if (!conditions.every(([, value]) => matchesEverything(value))) {
        return undefined; // the conditions narrow it down to some calls
    }
    const glob = permissionGlob(tool);
    const tools = dangerousTools.filter(t => glob.test(t));
    if (tools.length === 0) {
        return undefined;
    }
    const shown = plainName.test(tool) || /^[A-Za-z0-9*?._:-]+$/.test(tool) ? `"${tool}"` : "a pattern";
    return `${shown} approves every call of ${tools.join(", ")}`;
}

/** Collects the tool-wide allow patterns of a permissions object, labelled with their location. */
function toolWideAllows(permissions: unknown, path: string, conf: WildcardPermissionsConfig, out: string[]): void {
    list(get(permissions, "allow")).forEach((v, i) => {
        const pattern = str(v);
        if (pattern === undefined || conf.allowedPatterns.includes(pattern)) {
            return;
        }
        const why = toolWideAllow(pattern, conf.dangerousTools);
        if (why !== undefined) {
            out.push(`${path}.allow[${i}] (${why})`);
        }
    });
}

function wildcardPermissionsConfig(args: ResourceValidationArgs): WildcardPermissionsConfig {
    return config<WildcardPermissionsConfig>(args, {
        dangerousTools: defaultDangerousTools, allowedPatterns: [], allowedResources: [],
    });
}

function reportToolWideAllows(kind: string, args: ResourceValidationArgs, found: string[],
    reportViolation: ReportViolation): void {
    if (found.length > 0) {
        reportViolation(`${kind} "${args.name}" auto-approves whole tools with permission rules: ` +
            `${found.join("; ")}. docker-agent runs every matching call without asking, also under AgentRun's ` +
            "toolApproval \"deny\". Narrow the rules with argument conditions (for example \"shell:cmd=git status*\"), " +
            "or add the pattern to allowedPatterns of this policy.");
    }
}

export const permissionsNoWildcardAllow: ResourceValidationPolicy = {
    name: "permissions-no-wildcard-allow",
    description: "The `permissions.allow` rules of AgentConfig and the global `settings.permissions.allow` rules " +
        "of UserSettings must not approve every tool (`*`) or every call of a tool that runs commands or changes " +
        "files (`shell`, `shell:cmd=*`, `write_file`, ...): docker-agent runs such calls without asking, also " +
        "under AgentRun's toolApproval \"deny\".",
    enforcementLevel: "mandatory",
    remediationSteps: "Narrow the rule with argument conditions (for example \"shell:cmd=git status*\"), move the " +
        "tool to `ask`, or add the pattern to allowedPatterns (or the resource to allowedResources) of this policy.",
    configSchema: {
        properties: {
            dangerousTools: {
                type: "array",
                items: { type: "string" },
                default: defaultDangerousTools,
                description: "Tools that an allow rule must not approve without argument conditions.",
            },
            allowedPatterns: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Allow patterns that are accepted anyway, compared exactly.",
            },
            allowedResources: {
                type: "array",
                items: { type: "string" },
                default: [],
                description: "Resources that are exempt, by resource name or URN. `*` matches any characters.",
            },
        },
    },
    validateResource: [
        validateResourceOfType(dockeragent.AgentConfig, (cfg, args, reportViolation) => {
            const conf = wildcardPermissionsConfig(args);
            if (isAllowlisted(args, conf.allowedResources)) {
                return;
            }
            const c = cfg as unknown as Obj;
            const found: string[] = [];
            toolWideAllows(get(c, "permissions"), "permissions", conf, found);
            toolWideAllows(get(get(c, "extra"), "permissions"), "extra.permissions", conf, found);
            reportToolWideAllows("AgentConfig", args, found, reportViolation);
        }),
        validateResourceOfType(dockeragent.UserSettings, (us, args, reportViolation) => {
            const conf = wildcardPermissionsConfig(args);
            if (isAllowlisted(args, conf.allowedResources)) {
                return;
            }
            const settings = get(us as unknown as Obj, "settings");
            const found: string[] = [];
            toolWideAllows(get(settings, "permissions"), "settings.permissions", conf, found);
            toolWideAllows(get(get(settings, "extra"), "permissions"), "settings.extra.permissions", conf, found);
            reportToolWideAllows("UserSettings", args, found, reportViolation);
        }),
    ],
};

// ---------------------------------------------------------------- usersettings-no-autonomous-safety

export const userSettingsNoAutonomousSafety: ResourceValidationPolicy = {
    name: "usersettings-no-autonomous-safety",
    description: "UserSettings must not make docker-agent auto-approve every tool call: no \"autonomous\" " +
        "safety mode and no `yolo`, globally (`settings`) or for an alias. These settings beat the safety mode " +
        "of the agent configurations the user runs.",
    enforcementLevel: "mandatory",
    remediationSteps: "Remove settings.safety, settings.yolo and the aliases' safety and yolo options, or use " +
        "\"restricted\", \"balanced\" or \"strict\".",
    validateResource: validateResourceOfType(dockeragent.UserSettings, (us, args, reportViolation) => {
        const u = us as unknown as Obj;
        const where: string[] = [];
        const settings = get(u, "settings");
        const extra = get(settings, "extra");
        for (const [label, value] of [
            ["settings.safety", str(get(settings, "safety")) === "autonomous"],
            ["settings.yolo", get(settings, "yolo") === true],
            ["settings.extra.safety", str(get(extra, "safety")) === "autonomous"],
            ["settings.extra.YOLO", get(extra, "YOLO") === true],
        ] as const) {
            if (value) {
                where.push(label);
            }
        }
        for (const [name, alias] of entries(get(u, "aliases"))) {
            if (str(get(alias, "safety")) === "autonomous" || get(alias, "yolo") === true) {
                where.push(`alias "${name}"`);
            }
        }
        if (where.length > 0) {
            reportViolation(`UserSettings "${args.name}" makes docker-agent approve every tool call without ` +
                `asking (${where.join(", ")}): \"autonomous\" safety or yolo, which beats the safety mode of every ` +
                "agent the user runs. Use \"restricted\", \"balanced\" or \"strict\".");
        }
    }),
};

/** Every policy of the pack, in the order they are reported. */
export const policies: ResourceValidationPolicy[] = [
    agentRunToolApprovalDeny,
    agentRunReferencePinned,
    agentConfigNoAutonomousSafety,
    agentConfigSandboxForLocalTools,
    agentConfigNoLiteralCredentials,
    agentConfigAllowedModelProviders,
    agentConfigAllowedMcpRefs,
    agentArtifactAllowedRegistries,
    permissionsNoWildcardAllow,
    userSettingsNoAutonomousSafety,
];
