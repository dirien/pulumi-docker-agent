// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

import { PolicyPack } from "@pulumi/policy";
import { policies } from "./policies";

// The policies, their default enforcement levels and their configuration are documented in
// README.md. Configure them with `--policy-pack-config <file>` or in Pulumi Cloud.
new PolicyPack("docker-agent-policies", {
    description: "Guardrails for Docker Agent resources: tool approval and safety modes, sandboxing, " +
        "credentials, pinned references, and allowlists for model providers, MCP servers and registries.",
    policies,
});
