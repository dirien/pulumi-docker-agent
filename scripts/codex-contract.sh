#!/usr/bin/env bash
# Checks that the installed Codex CLI still accepts what AgentRun passes it
# (provider/pkg/agentrun/harness_codex.go): the flags of `codex exec`, also with `resume`, the
# sandbox check `codex sandbox -- true`, and `codex --version`. It reads --help output only: it
# starts no session, needs no login and calls no model.
#
# Run it on the machine that runs pulumi up (for example in the release candidate's macOS check),
# and before raising codexTestedVersion. CODEX selects another executable.
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

version=$("$codex" --version)
echo "installed: $version (AgentRun was tested with Codex 0.162)"

root=$("$codex" --help)
check "$root" "codex --help" "--config" "exec" "sandbox"

exec_help=$("$codex" exec --help)
check "$exec_help" "codex exec --help" --json --color --skip-git-repo-check --sandbox workspace-write \
	--dangerously-bypass-approvals-and-sandbox --model --output-schema --output-last-message

resume_help=$("$codex" exec resume --help)
check "$resume_help" "codex exec resume --help" "SESSION_ID" "PROMPT"

sandbox_help=$("$codex" sandbox --help)
check "$sandbox_help" "codex sandbox --help" "COMMAND"

if [ "$fail" -ne 0 ]; then
	echo "the installed codex does not accept AgentRun's arguments: see provider/pkg/agentrun/harness_codex.go"
	exit 1
fi
echo "OK: codex accepts AgentRun's arguments"
