# Local configuration files (Pulumi YAML)

Manages the two Docker Agent configuration files that are not agent definitions:

- `UserSettings` writes the [user configuration](https://docs.docker.com/ai/docker-agent/configuration/user-settings/)
  (`~/.config/cagent/config.yaml`): TUI settings, the default safety mode, global permissions and hooks,
  keybindings, aliases, a global model provider, the default model and the sandbox allowlist.
- `AgentsIgnore` writes a [`.agentsignore`](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/)
  file that hides secrets from the agent's filesystem tools.
- A second `UserSettings` without `path` only renders YAML, for example to bake into a container image.

Both resources validate during `pulumi preview` and never write during a preview. To keep the example
harmless, it writes into `outputDir` (default `out/` next to `Pulumi.yaml`) instead of your home directory.

## Run it

```bash
# From the repository root
make provider

cd examples/user-settings-yaml
pulumi stack init dev
pulumi config set outputDir /tmp/docker-agent-example   # optional, the default is ./out
pulumi up

cat "$(pulumi stack output settingsPath)"
docker agent alias list --config-dir "$(dirname "$(pulumi stack output settingsPath)")"

pulumi destroy && pulumi stack rm dev   # removes both files
```

Edit either file and run `pulumi refresh`: the change shows up as drift, and the next `pulumi up`
writes the declared content again. Set the `retainOnDelete` resource option to keep a file on
`pulumi destroy`.

When you point `path` at your real `~/.config/cagent/config.yaml`, the preview fails if that file
already exists with other content, so your aliases are not lost by accident. Copy what you want to keep
into the program, or add `overwrite: true`: the provider then moves the existing file to
`config.yaml.pulumi-backup` and moves it back on `pulumi destroy`. A symlinked file (dotfile managers)
stays a link; the file it points to is written.

`Pulumi.yaml` points `plugins.providers` at `../../bin`, the binary that `make provider` builds.

## Outside this repository

Remove the `plugins` block from `Pulumi.yaml` and install the released plugin:

```bash
pulumi plugin install resource docker-agent --server github://api.github.com/dirien/pulumi-docker-agent
```
