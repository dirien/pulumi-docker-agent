// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Unit tests for the policies. They call each policy the way the policy analyzer does: with the
// resource inputs wrapped in @pulumi/policy's unknown-checking proxy, the policy configuration
// (defaults merged by the engine are passed explicitly) and a reportViolation callback.

import * as assert from "node:assert/strict";
import { describe, test } from "node:test";

import { ResourceValidationArgs, ResourceValidationPolicy, unknownCheckingProxy } from "@pulumi/policy";

import * as p from "../policies";

// The sentinel @pulumi/policy uses for a string that is unknown during preview (not part of the
// typed API).
// eslint-disable-next-line @typescript-eslint/no-var-requires
const unknownString: string = require("@pulumi/policy/proxy").unknownStringValue;

const AgentConfig = "docker-agent:index:AgentConfig";
const AgentRun = "docker-agent:index:AgentRun";
const AgentArtifact = "docker-agent:index:AgentArtifact";
const UserSettings = "docker-agent:index:UserSettings";

class NotApplicable extends Error {}

/** Builds a fake credential at run time, so secret scanners do not flag the test source. */
const fake = (prefix: string): string => prefix + "0123456789abcdefghijklmnopqrstuvwxyz";

function makeArgs(type: string, name: string, props: Record<string, unknown>,
    config: Record<string, unknown>): ResourceValidationArgs {
    return {
        type,
        name,
        urn: `urn:pulumi:dev::policy-tests::${type}::${name}`,
        props: unknownCheckingProxy(props),
        opts: {
            protect: false,
            ignoreChanges: [],
            aliases: [],
            customTimeouts: { createSeconds: 0, updateSeconds: 0, deleteSeconds: 0 },
            additionalSecretOutputs: [],
        },
        stackTags: new Map(),
        isType: (cls: unknown) => (cls as { __pulumiType?: string }).__pulumiType === type,
        asType: ((cls: unknown) => (cls as { __pulumiType?: string }).__pulumiType === type ? props : undefined) as
            ResourceValidationArgs["asType"],
        getConfig: <T extends object>() => config as T,
        notApplicable: (reason?: string): never => {
            throw new NotApplicable(reason);
        },
    };
}

/** Runs a policy's validations against one resource and returns the violation messages. */
async function validate(policy: ResourceValidationPolicy, type: string, props: Record<string, unknown>,
    config: Record<string, unknown> = {}, name = "res"): Promise<string[]> {
    const violations: string[] = [];
    const validations = Array.isArray(policy.validateResource) ? policy.validateResource : [policy.validateResource!];
    for (const validation of validations) {
        try {
            await validation(makeArgs(type, name, props, config), message => violations.push(message));
        } catch (e) {
            if (!(e instanceof NotApplicable)) {
                throw e;
            }
        }
    }
    return violations;
}

async function remediate(policy: ResourceValidationPolicy, type: string, props: Record<string, unknown>,
    config: Record<string, unknown> = {}, name = "res"): Promise<Record<string, unknown> | undefined> {
    try {
        const result = await policy.remediateResource!(makeArgs(type, name, props, config));
        return result ? { ...result } : undefined;
    } catch (e) {
        if (e instanceof NotApplicable) {
            return undefined;
        }
        throw e;
    }
}

describe("agentrun-tool-approval-deny", () => {
    const policy = p.agentRunToolApprovalDeny;

    test("is mandatory", () => assert.equal(policy.enforcementLevel, "mandatory"));

    test("allows deny and the default", async () => {
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", toolApproval: "deny" }), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi" }), []);
    });

    test("rejects allow", async () => {
        const v = await validate(policy, AgentRun, { prompt: "hi", toolApproval: "allow" }, {}, "review");
        assert.equal(v.length, 1);
        assert.match(v[0], /AgentRun "review" sets toolApproval "allow"/);
    });

    test("allows allowlisted resources by name, URN and wildcard", async () => {
        const props = { prompt: "hi", toolApproval: "allow" };
        assert.deepEqual(await validate(policy, AgentRun, props, { allowedResources: ["review"] }, "review"), []);
        assert.deepEqual(await validate(policy, AgentRun, props,
            { allowedResources: ["urn:pulumi:dev::policy-tests::docker-agent:index:AgentRun::review"] }, "review"), []);
        assert.deepEqual(await validate(policy, AgentRun, props, { allowedResources: ["sandboxed-*"] },
            "sandboxed-ci"), []);
        assert.equal((await validate(policy, AgentRun, props, { allowedResources: ["other"] }, "review")).length, 1);
    });

    test("skips an unknown value during preview", async () => {
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", toolApproval: unknownString }), []);
    });

    test("ignores other resource types", async () => {
        assert.deepEqual(await validate(policy, AgentConfig, { toolApproval: "allow" }), []);
    });

    test("remediates allow to deny", async () => {
        assert.deepEqual(await remediate(policy, AgentRun, { prompt: "hi", toolApproval: "allow" }),
            { prompt: "hi", toolApproval: "deny" });
        assert.equal(await remediate(policy, AgentRun, { prompt: "hi", toolApproval: "deny" }), undefined);
        assert.equal(await remediate(policy, AgentRun, { prompt: "hi" }), undefined);
        assert.equal(await remediate(policy, AgentRun, { prompt: "hi", toolApproval: "allow" },
            { allowedResources: ["res"] }), undefined);
    });

    test("rejects hook approvals under deny", async () => {
        const v = await validate(policy, AgentRun, { prompt: "hi", allowHookApprovals: true }, {}, "review");
        assert.equal(v.length, 1);
        assert.match(v[0], /AgentRun "review" sets allowHookApprovals: a pre_tool_use or permission_request hook/);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", toolApproval: "deny", allowHookApprovals: false }), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", allowHookApprovals: true },
            { allowedResources: ["review"] }, "review"), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", allowHookApprovals: unknownString }), []);
        // With toolApproval "allow", hook approvals change nothing: one violation, about "allow".
        const both = await validate(policy, AgentRun, { prompt: "hi", toolApproval: "allow", allowHookApprovals: true });
        assert.equal(both.length, 1);
        assert.match(both[0], /sets toolApproval "allow"/);
    });

    test("remediates hook approvals", async () => {
        assert.deepEqual(await remediate(policy, AgentRun, { prompt: "hi", allowHookApprovals: true }),
            { prompt: "hi", allowHookApprovals: false });
        assert.deepEqual(await remediate(policy, AgentRun, { prompt: "hi", toolApproval: "allow", allowHookApprovals: true }),
            { prompt: "hi", toolApproval: "deny", allowHookApprovals: false });
        assert.equal(await remediate(policy, AgentRun, { prompt: "hi", allowHookApprovals: true },
            { allowedResources: ["res"] }), undefined);
    });
});

describe("agentrun-reference-pinned", () => {
    const policy = p.agentRunReferencePinned;
    const digest = "sha256:" + "a".repeat(64);

    test("is advisory", () => assert.equal(policy.enforcementLevel, "advisory"));

    test("accepts digests, yaml and unknown references", async () => {
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", reference: `ghcr.io/org/agent@${digest}` }), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", reference: `ghcr.io/org/agent:v1@${digest}` }), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", yaml: "agents: {}" }), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", reference: unknownString }), []);
    });

    test("flags tags", async () => {
        const v = await validate(policy, AgentRun, { prompt: "hi", reference: "ghcr.io/org/agent:v1" });
        assert.equal(v.length, 1);
        assert.match(v[0], /runs an agent from registry "ghcr\.io" by tag, not by digest/);
        assert.doesNotMatch(v[0], /org\/agent|:v1/, "the message names the registry only");
        assert.equal((await validate(policy, AgentRun, { prompt: "hi", reference: "ghcr.io/org/agent@sha256:abc" })).length, 1);
    });
});

describe("agentconfig-no-autonomous-safety", () => {
    const policy = p.agentConfigNoAutonomousSafety;

    test("allows other modes", async () => {
        assert.deepEqual(await validate(policy, AgentConfig, {
            runtime: { safety: "restricted" },
            agents: [{ name: "root", safety: "balanced" }],
        }), []);
    });

    test("flags runtime and agent safety", async () => {
        const v = await validate(policy, AgentConfig, {
            runtime: { safety: "autonomous" },
            agents: [{ name: "root", safety: "autonomous" }, { name: "helper", extra: { safety: "autonomous" } }],
        }, {}, "cfg");
        assert.equal(v.length, 1);
        assert.match(v[0], /runtime\.safety, agent "root", agent "helper"/);
    });
});

describe("agentconfig-sandbox-for-local-tools", () => {
    const policy = p.agentConfigSandboxForLocalTools;
    const config = { toolsetTypes: p.defaultLocalToolsetTypes };

    test("flags local toolsets without a sandbox, including shared toolsets", async () => {
        const v = await validate(policy, AgentConfig, {
            toolsets: { files: { type: "filesystem" } },
            agents: [
                { name: "root", toolsets: [{ type: "shell" }, { type: "think" }] },
                { name: "writer", useToolsets: ["files"] },
                { name: "searcher", toolsets: [{ type: "mcp", ref: "docker:duckduckgo" }] },
            ],
        }, config);
        assert.equal(v.length, 1);
        assert.match(v[0], /"root" \(shell\), "writer" \(filesystem\)/);
        assert.doesNotMatch(v[0], /searcher/);
    });

    test("passes with runtime.sandbox", async () => {
        assert.deepEqual(await validate(policy, AgentConfig, {
            runtime: { sandbox: true },
            agents: [{ name: "root", toolsets: [{ type: "shell" }] }],
        }, config), []);
    });

    test("uses the configured toolset types", async () => {
        const props = { agents: [{ name: "root", toolsets: [{ type: "fetch" }] }] };
        assert.deepEqual(await validate(policy, AgentConfig, props, config), []);
        assert.equal((await validate(policy, AgentConfig, props, { toolsetTypes: ["fetch"] })).length, 1);
    });
});

describe("agentconfig-no-literal-credentials", () => {
    const policy = p.agentConfigNoLiteralCredentials;

    test("accepts env references", async () => {
        assert.deepEqual(await validate(policy, AgentConfig, {
            models: { claude: { provider: "anthropic", model: "claude-opus-5-5", tokenKey: "AGENT_ANTHROPIC_API_KEY" } },
            agents: [{
                name: "root",
                toolsets: [
                    { type: "mcp", command: "gh-mcp", env: { GITHUB_TOKEN: "${env.GITHUB_TOKEN}", LOG_LEVEL: "debug" } },
                    { type: "fetch", headers: { Authorization: "Bearer ${env.API_TOKEN}", Accept: "application/json" } },
                    { type: "shell", env: { MAX_TOKENS: "100" } },
                ],
            }],
        }), []);
    });

    test("flags literal values in env, headers, remote headers and tokenKey", async () => {
        const v = await validate(policy, AgentConfig, {
            models: { gpt: { provider: "openai", model: "gpt-5-mini", tokenKey: fake("sk-proj-") } },
            mcps: { github: { remote: { url: "https://api.githubcopilot.com/mcp/", headers: { Authorization: "Bearer " + fake("gho_") } } } },
            agents: [{
                name: "root",
                toolsets: [
                    { type: "mcp", command: "gh-mcp", env: { GITHUB_TOKEN: fake("ghp_") } },
                    { type: "api", apiConfig: { endpoint: "https://example.com", headers: { "X-Api-Key": "my-literal-api-key" } } },
                    { type: "shell", env: { HARMLESS_NAME: fake("sk-ant-api03-") } },
                ],
            }],
        }, {}, "cfg");
        assert.equal(v.length, 1);
        for (const path of [
            "models.gpt.tokenKey",
            "mcps.github.remote.headers.Authorization",
            "agents[0].toolsets[0].env.GITHUB_TOKEN",
            "agents[0].toolsets[1].apiConfig.headers.X-Api-Key",
            "agents[0].toolsets[2].env.HARMLESS_NAME",
        ]) {
            assert.ok(v[0].includes(path), `${path} not reported in: ${v[0]}`);
        }
        assert.doesNotMatch(v[0], /0123456789abcdef/, "the message must not repeat the credential");
    });

    test("detects credentials by name and by value", () => {
        assert.ok(p.isLiteralCredential("API_KEY", "abcdefgh12345"));
        assert.ok(p.isLiteralCredential("Authorization", "Basic dXNlcjpwYXNzd29yZA=="));
        assert.ok(p.isLiteralCredential("ANY", "AKIAIOSFODNN7EXAMPLE"));
        assert.ok(!p.isLiteralCredential("Authorization", "Bearer ${env.TOKEN}"));
        assert.ok(!p.isLiteralCredential("TOKEN_LIMIT", "4096"));
        assert.ok(!p.isLiteralCredential("LOG_LEVEL", "debug-verbose-mode"));
    });
});

describe("agentconfig-allowed-model-providers", () => {
    const policy = p.agentConfigAllowedModelProviders;

    test("is disabled by default", () => assert.equal(policy.enforcementLevel, "disabled"));

    test("lists the providers of named models, inline references and custom providers", () => {
        const uses = p.modelProviderUses({
            providers: { llm: { apiType: "openai_chatcompletions", baseUrl: "https://llm.example.com/v1" } },
            models: {
                claude: { provider: "anthropic", model: "claude-opus-5-5", titleModel: "google/gemini-3-flash" },
                router: { provider: "openrouter", model: "openai/gpt-5-mini" },
                local: { provider: "llm", model: "qwen" },
            },
            agents: [
                { name: "root", model: "claude", compactionModel: "openai/gpt-5-mini", fallback: { models: ["dmr/ai/qwen3"] } },
                { name: "alloy", model: "anthropic/claude-opus-5-5,amazon-bedrock/global.anthropic.claude-opus-5" },
                { name: "coder", model: "claude", harness: { type: "claude-code", model: "opus" } },
            ],
        });
        assert.deepEqual(uses.map(u => u.type).sort(),
            ["amazon-bedrock", "anthropic", "anthropic", "dmr", "google", "openai", "openai", "openrouter"]);
    });

    test("rejects providers that are not allowed, and everything without a list", async () => {
        const props = { agents: [{ name: "root", model: "openai/gpt-5-mini" }] };
        assert.deepEqual(await validate(policy, AgentConfig, props, { allowedProviders: ["openai"] }), []);
        const v = await validate(policy, AgentConfig, props, { allowedProviders: ["anthropic"] });
        assert.equal(v.length, 1);
        assert.match(v[0], /openai by agents\[0\]\.model "openai\/gpt-5-mini"/);
        assert.match((await validate(policy, AgentConfig, props, { allowedProviders: [] }))[0], /none \(set allowedProviders\)/);
    });
});

describe("agentconfig-allowed-mcp-refs", () => {
    const policy = p.agentConfigAllowedMcpRefs;
    const defaults = { allowedRefs: [], allowedRemoteUrls: [], allowCommand: false };

    test("checks refs, named definitions, remote URLs and commands", async () => {
        const props = {
            mcps: { search: { ref: "docker:duckduckgo" }, custom: { command: "npx", args: ["my-mcp"] } },
            agents: [{
                name: "root",
                toolsets: [
                    { type: "mcp", ref: "search" },
                    { type: "mcp", ref: "docker:github-official" },
                    { type: "mcp", remote: { url: "https://mcp.example.com/sse" } },
                    { type: "fetch" },
                ],
            }],
        };
        const v = await validate(policy, AgentConfig, props, { ...defaults, allowedRefs: ["docker:duckduckgo"] });
        assert.equal(v.length, 1);
        assert.match(v[0], /ref "docker:github-official"/);
        assert.match(v[0], /agent "root" toolsets\[2\] remote \(https:\/\/mcp\.example\.com\)/);
        assert.match(v[0], /mcps "custom" command/);
        assert.doesNotMatch(v[0], /ref "docker:duckduckgo"|ref "search"/);
        assert.doesNotMatch(v[0], /\/sse|npx/, "URLs show only scheme and host, commands are not shown");

        assert.deepEqual(await validate(policy, AgentConfig, props, {
            allowedRefs: ["docker:*"], allowedRemoteUrls: ["https://mcp.example.com/*"], allowCommand: true,
        }), []);
    });

    test("never repeats credentials of URLs, refs and commands", async () => {
        const token = fake("ghp_");
        const v = await validate(policy, AgentConfig, {
            mcps: { tool: { command: `my-mcp --token ${token}` } },
            agents: [{
                name: "root",
                toolsets: [
                    { type: "mcp", remote: { url: `https://user:${token}@mcp.example.com:8443/sse?token=${token}#x` } },
                    { type: "mcp", ref: `https://${token}@catalog.example.com/server` },
                    { type: "mcp", ref: `docker:server --token=${token}` },
                    { type: "mcp", remote: { url: `not a url ${token}` } },
                ],
            }],
        }, defaults);
        assert.equal(v.length, 1);
        assert.doesNotMatch(v[0], /0123456789abcdef/, "the message must not repeat the credential");
        assert.match(v[0], /toolsets\[0\] remote \(https:\/\/mcp\.example\.com:8443\)/);
        assert.match(v[0], /toolsets\[1\] ref \(https:\/\/catalog\.example\.com\)/);
        assert.match(v[0], /toolsets\[2\] ref;/);
        assert.match(v[0], /toolsets\[3\] remote;/);
        assert.match(v[0], /mcps "tool" command\./);
    });
});

describe("agentartifact-allowed-registries", () => {
    const policy = p.agentArtifactAllowedRegistries;

    test("parses references like the docker CLI", () => {
        assert.deepEqual(p.parseReference("ghcr.io/org/agent:v1"), { registry: "ghcr.io", repository: "org/agent" });
        assert.deepEqual(p.parseReference("localhost:5000/examples/agent@sha256:abc"),
            { registry: "localhost:5000", repository: "examples/agent" });
        assert.deepEqual(p.parseReference("myorg/agent:latest"), { registry: "docker.io", repository: "myorg/agent" });
        assert.deepEqual(p.parseReference("agent"), { registry: "docker.io", repository: "library/agent" });
        assert.deepEqual(p.parseReference("index.docker.io/myorg/agent"), { registry: "docker.io", repository: "myorg/agent" });
    });

    test("matches hosts, repository prefixes and wildcards", () => {
        assert.ok(p.isAllowedRegistry("ghcr.io/org/agent:v1", ["ghcr.io"]));
        assert.ok(p.isAllowedRegistry("ghcr.io/org/agent:v1", ["ghcr.io/org"]));
        assert.ok(!p.isAllowedRegistry("ghcr.io/other/agent:v1", ["ghcr.io/org"]));
        assert.ok(!p.isAllowedRegistry("ghcr.io/organisation/agent:v1", ["ghcr.io/org"]));
        assert.ok(p.isAllowedRegistry("acme.azurecr.io/agents/a:v1", ["*.azurecr.io"]));
        assert.ok(p.isAllowedRegistry("myorg/agent:v1", ["docker.io/myorg"]));
        assert.ok(!p.isAllowedRegistry("ghcr.io/org/agent:v1", []));
    });

    test("checks AgentArtifact and AgentRun references", async () => {
        const config = { allowedRegistries: ["ghcr.io/org"] };
        assert.deepEqual(await validate(policy, AgentArtifact, { reference: "ghcr.io/org/agent:v1", yaml: "x" }, config), []);
        assert.match((await validate(policy, AgentArtifact, { reference: "docker.io/someone/agent:v1", yaml: "x" },
            config))[0], /registry "docker\.io"/);
        const run = (await validate(policy, AgentRun, { reference: "quay.io/org/agent:v1", prompt: "hi" }, config))[0];
        assert.match(run, /AgentRun "res" uses a reference in registry "quay\.io" that is not allowed/);
        assert.doesNotMatch(run, /org\/agent|:v1/, "the message names the registry only");
        assert.deepEqual(await validate(policy, AgentRun, { yaml: "x", prompt: "hi" }, config), []);
        assert.deepEqual(await validate(policy, AgentConfig, { agents: [] }, config), []);
    });
});

describe("permissions-no-wildcard-allow", () => {
    const policy = p.permissionsNoWildcardAllow;
    const defaults = { dangerousTools: p.defaultDangerousTools, allowedPatterns: [], allowedResources: [] };

    test("is mandatory", () => assert.equal(policy.enforcementLevel, "mandatory"));

    test("parses patterns like docker-agent", () => {
        assert.deepEqual(p.parsePermission("shell"), { tool: "shell", conditions: [] });
        assert.deepEqual(p.parsePermission("shell:cmd=git status*:cwd=/src/*"),
            { tool: "shell", conditions: [["cmd", "git status*"], ["cwd", "/src/*"]] });
        assert.deepEqual(p.parsePermission("mcp:github:create_issue:repo=a:b"),
            { tool: "mcp:github:create_issue", conditions: [["repo", "a:b"]] });
        assert.ok(p.permissionGlob("SH*").test("shell"));
        assert.ok(p.permissionGlob("[rw]rite_file").test("write_file"));
        assert.ok(!p.permissionGlob("[^w]rite_file").test("write_file"));
        assert.ok(p.permissionGlob("read\\_file").test("read_file"));
        assert.ok(!p.permissionGlob("read_*").test("write_file"));
    });

    test("flags patterns that approve every tool or every call of a dangerous tool", () => {
        for (const pattern of ["*", "**", "?*", "*:cmd=ls*", "shell", "SHELL", "sh*", "shell:cmd=*", "shell:cmd=*:cwd=*",
            "*_file", "write_file", "edit_file", "run_background_job"]) {
            assert.ok(p.toolWideAllow(pattern, p.defaultDangerousTools) !== undefined, pattern);
        }
        for (const pattern of ["read_*", "shell:cmd=git status*", "shell:cmd=ls", "write_marker", "think",
            "mcp:github:list_*", "write_file:path=/tmp/*"]) {
            assert.equal(p.toolWideAllow(pattern, p.defaultDangerousTools), undefined, pattern);
        }
        assert.ok(p.toolWideAllow("mcp:github:*", ["mcp:github:create_issue"]) !== undefined);
    });

    test("checks AgentConfig and UserSettings permissions", async () => {
        const secret = fake("ghp_");
        const v = await validate(policy, AgentConfig, {
            permissions: { allow: ["read_*", "*", "shell", `*:cmd=curl -H ${secret}`], deny: ["*"] },
            agents: [{ name: "root" }],
        }, defaults, "cfg");
        assert.equal(v.length, 1);
        assert.match(v[0], /AgentConfig "cfg" auto-approves whole tools/);
        assert.match(v[0], /permissions\.allow\[1\] \("\*" matches every tool\)/);
        assert.match(v[0], /permissions\.allow\[2\] \("shell" approves every call of shell\)/);
        assert.match(v[0], /permissions\.allow\[3\] \("\*" with argument conditions matches every tool\)/);
        assert.doesNotMatch(v[0], /allow\[0\]|0123456789abcdef/);

        const us = await validate(policy, UserSettings, {
            settings: { permissions: { allow: ["shell:cmd=*"] }, extra: { permissions: { allow: ["write_file"] } } },
        }, defaults, "settings");
        assert.equal(us.length, 1);
        assert.match(us[0], /UserSettings "settings" .*settings\.permissions\.allow\[0\] \("shell" approves every call of shell\)/);
        assert.match(us[0], /settings\.extra\.permissions\.allow\[0\] \("write_file" approves every call of write_file\)/);
    });

    test("accepts narrow rules, allowed patterns and resources, and unknown values", async () => {
        const props = { permissions: { allow: ["read_*", "shell:cmd=git *", "write_marker"] }, agents: [] };
        assert.deepEqual(await validate(policy, AgentConfig, props, defaults), []);
        const wide = { permissions: { allow: ["write_file"] }, agents: [] };
        assert.deepEqual(await validate(policy, AgentConfig, wide, { ...defaults, allowedPatterns: ["write_file"] }), []);
        assert.deepEqual(await validate(policy, AgentConfig, wide, { ...defaults, allowedResources: ["trusted-*"] },
            "trusted-cfg"), []);
        assert.deepEqual(await validate(policy, AgentConfig, wide, { ...defaults, dangerousTools: ["shell"] }), []);
        assert.deepEqual(await validate(policy, AgentConfig, { permissions: { allow: [unknownString] }, agents: [] },
            defaults), []);
        assert.deepEqual(await validate(policy, AgentRun, { prompt: "hi", permissions: { allow: ["*"] } }, defaults), []);
    });
});

describe("usersettings-no-autonomous-safety", () => {
    const policy = p.userSettingsNoAutonomousSafety;

    test("is mandatory", () => assert.equal(policy.enforcementLevel, "mandatory"));

    test("allows other modes", async () => {
        assert.deepEqual(await validate(policy, UserSettings, {
            settings: { safety: "balanced", yolo: false },
            aliases: { reviewer: { path: "a.yaml", safety: "restricted" } },
        }), []);
        assert.deepEqual(await validate(policy, UserSettings, {}), []);
    });

    test("flags autonomous safety and yolo, globally and per alias", async () => {
        const v = await validate(policy, UserSettings, {
            settings: { safety: "autonomous", yolo: true, extra: { YOLO: true } },
            aliases: {
                pirate: { path: "pirate.yaml", yolo: true },
                coder: { path: "coder.yaml", safety: "autonomous" },
                reviewer: { path: "reviewer.yaml", safety: "strict" },
            },
        }, {}, "settings");
        assert.equal(v.length, 1);
        assert.match(v[0], /UserSettings "settings" makes docker-agent approve every tool call without asking /);
        assert.match(v[0], /\(settings\.safety, settings\.yolo, settings\.extra\.YOLO, alias "pirate", alias "coder"\)/);
        // docker-agent auto-approves with YOLO even when safety is set.
        assert.equal((await validate(policy, UserSettings, { settings: { safety: "strict", yolo: true } })).length, 1);
    });

    test("ignores other resource types", async () => {
        assert.deepEqual(await validate(policy, AgentConfig, { settings: { safety: "autonomous" }, agents: [] }), []);
    });
});

test("policy names are unique", () => {
    const names = p.policies.map(x => x.name);
    assert.equal(new Set(names).size, names.length);
});
