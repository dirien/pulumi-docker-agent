# Contributing to pulumi-docker-agent

Thanks for your interest in improving the Pulumi Docker Agent provider. Bug reports, documentation fixes and pull requests are all welcome.

By participating you agree to follow the [Code of Conduct](https://github.com/dirien/pulumi-docker-agent/blob/main/CODE-OF-CONDUCT.md).

## Reporting issues

Open an [issue](https://github.com/dirien/pulumi-docker-agent/issues) with:

- the provider version (`pulumi plugin ls`) and the `dockerAgentVersion` output of the affected resource,
- a minimal program, or the agent YAML, that reproduces the problem,
- what you expected and what happened, including the full error output.

Please report security problems privately through [GitHub security advisories](https://github.com/dirien/pulumi-docker-agent/security/advisories/new) rather than in a public issue.

## Development setup

You need:

- Go 1.27 (with the default `GOTOOLCHAIN=auto`, any recent Go downloads it)
- the Pulumi CLI at the version in `.pulumi.version`
- [pulumictl](https://github.com/pulumi/pulumictl)
- Node.js 22 with Yarn, Python 3.9 or later, and the .NET 8 SDK (to build the SDKs)
- Docker (for `registry:2` and end-to-end tests)
- [golangci-lint](https://golangci-lint.run/) v2

Repository layout:

| Path | Contents |
|---|---|
| `provider/` | The provider Go module (`github.com/dirien/pulumi-docker-agent/provider`), built with `pulumi-go-provider`'s `infer` package |
| `provider/cmd/pulumi-resource-docker-agent/` | `main.go` and the committed `schema.json` |
| `sdk/` | Generated SDKs, committed. Do not edit by hand. |
| `examples/` | Example programs, run by `make test_examples` |
| `docs/` | Pulumi Registry pages (`_index.md`, `installation-configuration.md`) |

## Making a change

1. Fork the repository and create a branch.
2. Make your change in `provider/` and add or update tests. Unit tests run against an in-memory OCI registry, so they need no network.
3. Run the checks:

   ```bash
   make lint
   make test_provider
   ```

4. If you changed a resource, function, input, output or description, regenerate and commit the schema and SDKs:

   ```bash
   make codegen
   git add provider/cmd/pulumi-resource-docker-agent/schema.json sdk/
   ```

   CI fails when the committed schema or SDKs differ from what `make codegen` produces.
5. For behaviour that touches a registry, run the end-to-end examples against a local `registry:2` (see "Testing locally against registry:2" in the [README](https://github.com/dirien/pulumi-docker-agent/blob/main/README.md)), and check that the real CLI still accepts the artifact:

   ```bash
   docker agent share pull <pinnedReference>
   docker agent debug config <pinnedReference>
   ```

6. Update `docs/_index.md`, `docs/installation-configuration.md` and `README.md` if user-facing behaviour changed, and add an entry under `## [Unreleased]` in `CHANGELOG.md`.
7. Open a pull request. Use a [Conventional Commits](https://www.conventionalcommits.org/) title (`feat: ...`, `fix: ...`, `docs: ...`, `chore: ...`): the release notes are grouped by these prefixes.

## Upgrading docker-agent

Do not bump `github.com/docker/docker-agent` in `provider/go.mod` by hand. Dependabot ignores it on purpose. Run `make upgrade_docker_agent [DOCKER_AGENT_VERSION=vX.Y.Z]` instead, or let the daily `upgrade-docker-agent` workflow open the pull request. The target:

1. Updates `go.mod` and `go.sum`.
2. Regenerates everything derived from the release: the vendored `provider/pkg/render/schema/agent-schema.json`, the upstream examples in `provider/pkg/render/testdata/upstream/`, the typed inputs (`provider/pkg/render/config_types.gen.go`) and the generated constants (`provider/pkg/render/version.gen.go`, `provider/pkg/validate/upstream.gen.go`).
3. Regenerates `schema.json` and the SDKs.
4. Runs the provider tests.
5. Writes a schema change report to `bin/upgrade-report.md` and adds a CHANGELOG entry.

Every step records its result in `bin/upgrade-status` and in the report's step table, and a failing step's output goes into the report. The workflow opens or updates a `[needs attention]` pull request for any failure, so finish the upgrade on that branch. `provider/internal/schemagen/upgrade_script_test.go` checks this behaviour for every failing step with stand-ins for `go` and `make`; extend it when you change the script.

Review the report. If a new key landed in a free-form value, or the generator asked for a rule, edit `provider/internal/schemagen/overrides.go`. Then add the matching entry to the allowlists in `provider/pkg/render/coverage_test.go`, each with its reason. Generated files are never edited by hand: `make check_config_types` (part of CI) fails when they differ from `make generate_config_types`.

## Releasing

Maintainers release by pushing a `vX.Y.Z` tag on `main`; see "Release process" in the [README](https://github.com/dirien/pulumi-docker-agent/blob/main/README.md).

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE).
