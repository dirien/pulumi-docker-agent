#!/usr/bin/env bash
# Shows how the parts of Codex that AgentRun relies on (provider/pkg/agentrun/harness_codex.go,
# codexauth.go, codexenv.go) changed between two Codex releases, for example
#
#   scripts/codex-diff.sh rust-v0.162.1 rust-v0.163.0
#
# Review the diff before raising codexTestedVersion: the JSONL events of codex exec, its flags,
# the order of its logins, its error messages, its sandbox check, its config layers, its process
# handling and the environment it gives its commands. Needs gh (GitHub's API; no Codex install).
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 <old tag> <new tag>" >&2
	exit 2
fi
old=$1
new=$2
files=(
	# codex exec: its events, flags, exit codes and SIGINT handling.
	codex-rs/exec/src/exec_events.rs
	codex-rs/exec/src/event_processor_with_jsonl_output.rs
	codex-rs/exec/src/cli.rs
	codex-rs/exec/src/lib.rs
	codex-rs/utils/cli/src/shared_options.rs
	codex-rs/utils/cli/src/config_override.rs
	# The root CLI: the global -c, the sandbox subcommand AgentRun's check runs, --profile.
	codex-rs/cli/src/main.rs
	codex-rs/cli/src/lib.rs
	codex-rs/cli/src/debug_sandbox.rs
	codex-rs/cli/src/debug_sandbox/cloud_config.rs
	codex-rs/cli/src/exit_status.rs
	# The npm launcher's signal forwarding, and arg0 ($CODEX_HOME/.env, the temp-dir refusal,
	# the sandbox helper).
	codex-cli/bin/codex.js
	codex-rs/arg0/src/lib.rs
	# Logins: their order, forced_login_method and forced_chatgpt_workspace_id, workload
	# identity, auth.json.
	codex-rs/login/src/auth/manager.rs
	codex-rs/login/src/auth/storage.rs
	codex-rs/login/src/auth/workload_identity.rs
	codex-rs/protocol/src/auth.rs
	codex-rs/protocol/src/error.rs
	# config.toml: its keys, layers (the project layer's trust and denied keys), the permission
	# syntax, openai_base_url, model providers (requires_openai_auth, env_key).
	codex-rs/config/src/config_toml.rs
	codex-rs/config/src/loader/mod.rs
	codex-rs/core/src/config/mod.rs
	codex-rs/model-provider-info/src/lib.rs
	codex-rs/utils/home-dir/src/lib.rs
	# The environment of commands, MCP servers and the trace context (TRACEPARENT).
	codex-rs/core/src/exec_env.rs
	codex-rs/protocol/src/shell_environment.rs
	codex-rs/rmcp-client/src/stdio_server_launcher.rs
	codex-rs/otel/src/trace_context.rs
	# Process groups (setsid) and the Linux sandbox (bubblewrap).
	codex-rs/utils/pty/src/process_group.rs
	codex-rs/utils/pty/src/pty.rs
	codex-rs/linux-sandbox/src/bwrap.rs
	codex-rs/linux-sandbox/src/launcher.rs
)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
changed=0
for f in "${files[@]}"; do
	for ref in "$old" "$new"; do
		if ! gh api "repos/openai/codex/contents/$f?ref=$ref" --jq .content 2>/dev/null | base64 -d >"$tmp/$ref"; then
			echo "(missing at $ref: $f)" >"$tmp/$ref"
		fi
	done
	if ! cmp -s "$tmp/$old" "$tmp/$new"; then
		changed=1
		echo "=== $f"
		diff -u --label "$old/$f" --label "$new/$f" "$tmp/$old" "$tmp/$new" || true
	fi
done
if [ "$changed" -eq 0 ]; then
	echo "no changes in the files AgentRun relies on"
fi
