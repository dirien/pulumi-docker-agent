## What and why

<!-- Describe the change and link any related issue (e.g. "Fixes #123"). -->

## Checklist

- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `chore:` ...). The release changelog is generated from it.
- [ ] `make lint test_provider` passes locally.
- [ ] If the schema changed: ran `make codegen` and committed `provider/cmd/pulumi-resource-docker-agent/schema.json` and `sdk/`.
- [ ] If `github.com/docker/docker-agent` was bumped: updated the `DockerAgentVersion` constant.
- [ ] Docs updated where user-facing behaviour changed (`docs/_index.md`, `docs/installation-configuration.md`, `README.md`, `CHANGELOG.md`).
