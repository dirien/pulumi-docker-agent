#!/usr/bin/env bash
# Checks that the installed Codex CLI still accepts what AgentRun passes it
# (provider/pkg/agentrun/harness_codex.go): `codex --version`, the flags of `codex exec` (also
# `--color=never`, and with `resume`), and the sandbox check AgentRun runs before the first turn
# under toolApproval deny, `codex --config=sandbox_mode="workspace-write" sandbox -- true`, which
# it runs for real. Nothing here starts a session, needs a login or calls a model.
#
# Run it on the machine that runs pulumi up (for example in the release candidate's macOS check),
# and before raising codexTestedVersion. CODEX selects another executable. It exits 1 when the
# CLI does not accept AgentRun's arguments; a sandbox that cannot start on this machine is
# reported, but it is not a contract failure (toolApproval allow still runs).
set -euo pipefail

codex=${CODEX:-codex}
fail=0
check() { # check <help text> <what> <pattern...>
	local text=$1 what=$2
	shift 2
	for p in "$@"; do
		if ! grep -q -- "$p" <<<"$text"; then
			echo "FAIL: $what does not list $p"
			fail=1
		fi
	done
}

# AgentRun fails a run whose codex --version exits non-zero or prints no version.
if ! version=$("$codex" --version 2>&1); then
	echo "FAIL: codex --version exited with an error: $(head -n 1 <<<"$version")"
	exit 1
fi
if ! grep -Eq '[0-9]+\.[0-9]+' <<<"$version"; then
	echo "FAIL: codex --version printed no version: $(head -n 1 <<<"$version")"
	fail=1
fi
echo "installed: $version (AgentRun was tested with Codex 0.162)"

root=$("$codex" --help)
check "$root" "codex --help" "--config" "exec" "sandbox"

exec_help=$("$codex" exec --help)
check "$exec_help" "codex exec --help" --json --color --skip-git-repo-check --sandbox workspace-write \
	--dangerously-bypass-approvals-and-sandbox --model --output-schema --output-last-message
# --color=never: the value must be one the CLI accepts.
if ! grep -- '--color' <<<"$exec_help" | grep -q never && ! grep -A3 -- '--color' <<<"$exec_help" | grep -q never; then
	echo "FAIL: codex exec --help does not list never as a value of --color"
	fail=1
fi

resume_help=$("$codex" exec resume --help)
check "$resume_help" "codex exec resume --help" "SESSION_ID" "PROMPT"

sandbox_help=$("$codex" sandbox --help)
check "$sandbox_help" "codex sandbox --help" "COMMAND"

# The sandbox check itself, as AgentRun runs it (it needs no login and calls no model).
probe_err=$(mktemp)
trap 'rm -f "$probe_err"' EXIT
set +e
"$codex" '--config=sandbox_mode="workspace-write"' sandbox -- true 2>"$probe_err" </dev/null
probe=$?
set -e
first=$(grep -m 1 -v '^[[:space:]]*$' "$probe_err" || true)
echo "sandbox check (codex --config=sandbox_mode=\"workspace-write\" sandbox -- true): exit $probe${first:+, stderr: $first}"
if [ "$probe" -ne 0 ]; then
	if grep -Eq 'unexpected argument|unrecognized subcommand|unrecognized option|unknown option|Found argument' "$probe_err"; then
		echo "FAIL: codex does not accept the sandbox check's arguments"
		fail=1
	else
		echo "NOTE: codex's sandbox does not start here: under toolApproval deny AgentRun fails codex runs on this machine"
	fi
fi

if [ "$fail" -ne 0 ]; then
	echo "the installed codex does not accept AgentRun's arguments: see provider/pkg/agentrun/harness_codex.go"
	exit 1
fi
echo "OK: codex accepts AgentRun's arguments"
