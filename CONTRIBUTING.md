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
- for the Java SDK and `examples/java`: Gradle 9 running on a JDK 17 or later, plus a JDK 11, which `sdk/java/build.gradle` uses as its toolchain (CI installs Temurin 11 and 21 and Gradle 9.7.1)
- Docker (for `registry:2` and end-to-end tests)
- [golangci-lint](https://golangci-lint.run/) v2

Repository layout:

| Path | Contents |
|---|---|
| `provider/` | The provider Go module (`github.com/dirien/pulumi-docker-agent/provider`), built with `pulumi-go-provider`'s `infer` package |
| `provider/cmd/pulumi-resource-docker-agent/` | `main.go` and the committed `schema.json` |
| `provider/cmd/pulumi-converter-docker-agent/`, `provider/pkg/pcl/` | The converter plugin (`pulumi convert --from docker-agent`, `pulumi import --from docker-agent`), which writes PCL that the Pulumi CLI turns into a program |
| `provider/internal/` | The generators: `schemagen` (the typed inputs), `schemadiff` (the schema change report of an upgrade), `examplegen` (`examples/docker-agent-examples`) and `docsgen` (the Example Usage sections) |
| `sdk/` | Generated SDKs, committed. Do not edit by hand. |
| `examples/` | Example programs, run by `make test_examples`. `examples/docker-agent-examples` is generated (see below), except its project files. |
| `docs/` | Pulumi Registry pages (`_index.md`, `installation-configuration.md`). The Example Usage regions of `_index.md` are generated from `docs/yaml` (see below). |
| `docs/yaml/` | The Example Usage programs of the API docs and of `docs/_index.md`, written once in Pulumi YAML |
| `policy/` | The policy pack for Pulumi Policies (`docker-agent-policies`, TypeScript) |

## Making a change

1. Fork the repository and create a branch.
2. Make your change in `provider/` and add or update tests. Unit tests run against an in-memory OCI registry, so they need no network.
3. Run the checks:

   ```bash
   make lint
   make test_provider
   ```

   If you changed `policy/`, or an input that a policy reads, also run `make build_nodejs test_policy` (the pack depends on the Node.js SDK built into `sdk/nodejs/bin`). If you changed the converter, also run `make test_examples EXAMPLES_RUN=TestConvert`.

4. If you changed a resource, function, input, output, description or an example in `docs/yaml`, regenerate the generated files and commit them (`schema.json`, `provider/pkg/provider/embed`, `docs/_index.md` and `sdk/`):

   ```bash
   make codegen
   git status
   ```

   `make codegen` runs `generate_config_types` (the typed inputs and `examples/docker-agent-examples`), `docs` (the Example Usage sections), `schema` and the SDK generators, in this order. CI fails when the committed examples, schema or SDKs differ from what `make codegen` produces. On a pull request, the "SDK breaking-change check" job compares `schema.json` with the base branch's using [schema-tools](https://github.com/pulumi/schema-tools) (`scripts/schema-compare.sh`), writes the report to its job summary, and labels the pull request `breaking-sdk-change` when SDK users would have to change their programs (a removed or renamed property, a string that became an enum, a new required input). That is allowed, but say so in `CHANGELOG.md`. Pull requests from forks and Dependabot only get the job summary, because their token cannot add labels. The daily docker-agent upgrade pull request carries the same report in its description and the same label.
5. For behaviour that touches a registry, run the end-to-end examples against a local `registry:2` (`make test_examples`, or one example by hand as described in [Running an example](https://github.com/dirien/pulumi-docker-agent/blob/main/examples/README.md#running-an-example)), and check that the real CLI still accepts the artifact:

   ```bash
   docker agent share pull <pinnedReference>
   docker agent debug config <pinnedReference>
   ```

6. Update `docs/_index.md` (outside the generated regions), `docs/installation-configuration.md`, `docs/yaml` and `README.md` if user-facing behaviour changed, and add an entry under `## [Unreleased]` in `CHANGELOG.md`.
7. Open a pull request. Use a [Conventional Commits](https://www.conventionalcommits.org/) title (`feat: ...`, `fix: ...`, `docs: ...`, `chore: ...`): the release notes are grouped by these prefixes.

## Documentation examples

The Example Usage sections of the API docs (`provider/pkg/provider/embed/*-examples.md`, embedded in the resource and function descriptions) and the language choosers of `docs/_index.md` (between `<!-- docsgen begin: ... -->` and `<!-- docsgen end -->`) are generated: never edit them by hand. Write or change the Pulumi YAML program in `docs/yaml/<name>-examples.yaml` (`index-examples.yaml` for `docs/_index.md`) and run `make docs`, which is part of `make codegen`. `provider/internal/docsgen` converts each program with `pulumi convert --generate-only` against `bin/` into TypeScript, Python, Go, C#, Java and HCL, and the YAML tab shows the program as written. The YAML converter and the HCL language plugin are pinned in the `Makefile` (`PULUMI_CONVERTER_YAML_VERSION`, `PULUMI_HCL_VERSION`).

The header of [`docs/yaml/agentconfig-examples.yaml`](https://github.com/dirien/pulumi-docker-agent/blob/main/docs/yaml/agentconfig-examples.yaml) describes the format. `make docs` fails when a program generator cannot convert a program or loses a resource name, and `make test_examples` previews every program (`TestDocsExamples`) and compiles the Go and TypeScript results (`TestDocsSnippetsGo`, `TestDocsSnippetsTypeScript`); the other languages are not compiled. Some constructs convert into code that does not compile or does something else, so avoid them:

- read files into a variable with `fn::readFile` instead of inline,
- export whole objects instead of fields of `structuredOutput` or of a function's object output,
- use no resource options (the C# generator drops `customTimeouts`, for example).

Check the other languages by hand when you use a new construct.

## Upgrading docker-agent

docker-agent releases every few days, and its configuration schema often changes with a release. Do not bump `github.com/docker/docker-agent` in `provider/go.mod` by hand: a bare `go.mod` bump would skip the regeneration, so Dependabot ignores it. Run `make upgrade_docker_agent [DOCKER_AGENT_VERSION=vX.Y.Z]` instead (`make check_docker_agent_upgrade` prints whether a newer release exists), or let the daily `upgrade-docker-agent` workflow open the pull request. The target:

1. Runs `go get` for the release and `go mod tidy`.
2. Regenerates everything derived from the release: the vendored `provider/pkg/render/schema/agent-schema.json`, the upstream examples in `provider/pkg/render/testdata/upstream/`, the typed inputs (`provider/pkg/render/config_types.gen.go`), the generated constants (`provider/pkg/render/version.gen.go` with `DockerAgentVersion` and the default config version, the highest value of the schema's `version` enum; `provider/pkg/validate/upstream.gen.go` with the toolset and provider lists) and the TypeScript and Python programs of every upstream example in `examples/docker-agent-examples` (`provider/internal/examplegen`).
3. Runs `make codegen`, which regenerates the docs examples, `schema.json` and the SDKs.
4. Runs `make test_provider`.
5. Writes `bin/upgrade-report.md` (the configuration schema changes, the SDK-breaking changes, the examples that were added, removed or changed, a table of step results and the failing step's output), `bin/upgrade-status` (`ok`, `go-get-failed`, `generate-failed`, `codegen-failed`, `tests-failed` or `script-failed`) and a CHANGELOG entry under `## [Unreleased]`.

A failing step does not stop the target: the later steps are skipped, the status and the report are written, and the target exits 0 (`STRICT=1` makes it exit non-zero). `provider/internal/schemagen/upgrade_script_test.go` checks this behaviour for every failing step with stand-ins for `go` and `make`; extend it when you change the script.

The workflow opens a pull request `upgrade-docker-agent-to-vX.Y.Z` whose description is the report. When a step failed, the pull request is still opened or updated, titled `[needs attention]` and labelled `needs-attention`; when nothing could be changed (for example `go get` failed), it carries an empty commit. Finish the upgrade on that branch. The job that builds the upgrade has a read-only token and no secrets. A separate job applies the resulting patch, refuses changes outside `provider/`, `sdk/`, `CHANGELOG.md`, `docs/_index.md` and the generated files of `examples/docker-agent-examples`, pushes with git hooks disabled and opens the pull request; if the push succeeded but opening the pull request failed, the next run opens it. Hand-written files that name the docker-agent version, such as the `docker/docker-agent` image of `examples/docker-serve-ts`, are not updated: change them on the branch.

Review the report. If the generator meets a new union type, inline object or free-form value, it stops and names the schema path: add a rule for that path in `provider/internal/schemagen/overrides.go` (a plain new property needs no rule). If a new key landed in a free-form value, add the matching entry to the allowlists in `provider/pkg/render/coverage_test.go`, each with its reason. Generated files are never edited by hand: `make check_config_types` (part of CI) fails when they differ from what `make generate_config_types` produces for the docker-agent version in `provider/go.mod`.

`make test_provider` also guards `UserSettings` against new docker-agent user settings: `TestUserSettingsCoversDockerAgentUserConfig` walks docker-agent's `userconfig.Config` (and the hook, provider and model types it embeds) and fails for every YAML key that `provider/pkg/provider/usersettings_types.go` does not model, and the tests in `provider/pkg/usersettings/upstream_test.go` compare the TUI option values and keybinding tables with the docker-agent source. Add the new key as a typed input (with its `yaml` tag) and run `make codegen`.

String properties with an `enum` of at least two non-numeric values become Pulumi enums: typed constants in every SDK, and a Check failure on the property for any other value unless `skipValidation` is set. A new enum gets a deterministic default name, its owner type plus the property name (`HookDefinition.on_error` would be `HookDefinitionOnError`), so an upgrade never stops on it. Give it a better name in `enumTypes` in `overrides.go` (and value names in `enumValueNames` where the default PascalCase reads badly) before a release ships the default name: renaming a published enum breaks Go, .NET and Java programs. A set that grows with most docker-agent releases, or that accepts user-defined names, belongs in `volatileEnums` instead, with the reason; it stays a string that lists the allowed values in its description. The report's "SDK-breaking changes" section lists enum values that were removed or renamed and inputs that became enums; call them out in the release notes. The `UserSettings` TUI enums are hand-written in `provider/pkg/provider/usersettings_types.go`, and tests compare them with the docker-agent source.

`provider/internal/examplegen` stops on an upstream example it cannot write with typed inputs: a key without an input (extend the types; a key docker-agent never reads goes into `unreadKeys` in `provider/pkg/render/unread.go`, with the reason), a validation error other than the known in-memory limits, or an HCL construct it does not handle. Its tests check that every upstream comment appears in both programs and that each program renders its golden file; `make test_examples` runs the programs.

## Releasing

Maintainers release by pushing a `vX.Y.Z` tag on `main`. [Releasing](https://github.com/dirien/pulumi-docker-agent/blob/main/README.md#releasing) in the README describes the release workflow and has the release checklist.

### Maven Central (Java SDK)

The `publish_java` job of `.github/workflows/release.yml` publishes the Java SDK as `io.github.dirien:docker-agent` when `PUBLISH_JAVA` is `"true"` in the workflow, which it is not for now, through the Central Portal's [OSSRH Staging API](https://central.sonatype.org/publish/publish-portal-ossrh-staging-api/), which the generated `sdk/java/build.gradle` targets (gradle-nexus publish-plugin: `publishToSonatype closeAndReleaseSonatypeStagingRepository`). Until the four secrets below are set, the job logs a notice and skips itself, and the rest of the release succeeds; the Java package then first appears with the first release after the setup. Publishing to Maven Central is permanent: a version cannot be changed or deleted. One-time setup:

- [ ] Sign in to the [Central Portal](https://central.sonatype.com/) with the `dirien` GitHub account. That verifies the `io.github.dirien` namespace automatically; check that it is listed as verified under [Namespaces](https://central.sonatype.com/publishing/namespaces). (With another login, add the namespace and verify it with the temporary public repository the Portal asks for.)
- [ ] Generate a user token on the Portal's [user tokens page](https://central.sonatype.com/usertoken) and store its two halves as the repository secrets `PUBLISH_REPO_USERNAME` and `PUBLISH_REPO_PASSWORD`. It must be a Portal token (old OSSRH credentials get a 401), and tokens can expire: renew the secrets with the token.
- [ ] Create a GPG key for signing (RSA 4096, no expiry or a long one), publish the public key to a key server Central checks, for example `gpg --keyserver keyserver.ubuntu.com --send-keys <KEYID>`, and store the ASCII-armored private key (`gpg --armor --export-secret-keys <KEYID>`) as `SIGNING_KEY` and its passphrase as `SIGNING_PASSWORD`. The generated `build.gradle` reads only these two (`useInMemoryPgpKeys(key, password)`) and signs with the primary key, so no `SIGNING_KEY_ID` is needed, but the primary key must be able to sign (gpg's default `[SC]`).
- [ ] After the first publishing release, check the deployment under [Deployments](https://central.sonatype.com/publishing/deployments) (a failed validation is listed there with its reasons), and that `https://repo1.maven.org/maven2/io/github/dirien/docker-agent/` lists the version, usually within 30 minutes.

The POM metadata that Central requires and pulumi-java leaves empty for non-Pulumi packages (project name, developer, SCM URLs) is filled in by `scripts/java-sdk.init.gradle`, which `make build_java` and the release job pass to Gradle; `make build_java` builds exactly the jars and the POM that get published (`sdk/java/build/libs`, `sdk/java/build/publications/mainPublication/pom-default.xml`). `PUBLISH_JAVA: "false"` skips Java in a release without removing the secrets.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](https://github.com/dirien/pulumi-docker-agent/blob/main/LICENSE).
