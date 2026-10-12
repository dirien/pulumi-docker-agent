// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	_ "embed" // the Example Usage sections
	"strings"
)

// The Example Usage sections of the resource and function descriptions, in every language the
// Pulumi Registry shows. `make docs` generates them from docs/yaml/<name>-examples.yaml with
// provider/internal/docsgen; do not edit them by hand.
var (
	//go:embed embed/agentconfig-examples.md
	agentConfigExamples string
	//go:embed embed/agentartifact-examples.md
	agentArtifactExamples string
	//go:embed embed/agentrun-examples.md
	agentRunExamples string
	//go:embed embed/usersettings-examples.md
	userSettingsExamples string
	//go:embed embed/agentsignore-examples.md
	agentsIgnoreExamples string
	//go:embed embed/getagent-examples.md
	getAgentExamples string
	//go:embed embed/validateagentconfig-examples.md
	validateAgentConfigExamples string
)

// describe joins the prose of a description, its generated examples and, for resources, the
// import section, which the Registry shows under "Import" (everything after "## Import").
func describe(prose, examples, importDoc string) string {
	doc := strings.TrimSpace(prose) + "\n\n" + strings.TrimSpace(examples)
	if importDoc != "" {
		doc += "\n\n## Import\n\n" + strings.TrimSpace(importDoc)
	}
	return doc
}

// importCommand formats a `pulumi import` example like the Registry's import sections.
func importCommand(token, name, id string) string {
	return "\n\n```sh\n$ pulumi import " + token + " " + name + " " + id + "\n```"
}
