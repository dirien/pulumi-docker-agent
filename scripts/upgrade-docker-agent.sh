#!/usr/bin/env bash
# Upgrade the pinned github.com/docker/docker-agent module to a newer release and regenerate
# everything derived from it: the vendored agent-schema.json and example configurations, the
# typed AgentConfig inputs and generated constants, the example programs in
# examples/docker-agent-examples, schema.json and the SDKs. Then run the provider tests and write
# a Markdown report of the configuration schema changes and the example changes plus a CHANGELOG
# entry.
#
# Usage:
#   scripts/upgrade-docker-agent.sh [--check] [vX.Y.Z]
#   make upgrade_docker_agent [DOCKER_AGENT_VERSION=vX.Y.Z]
#   make check_docker_agent_upgrade
#
# Environment:
#   DOCKER_AGENT_VERSION  target release (default: the latest docker/docker-agent GitHub release)
#   CHECK_ONLY=1          same as --check: print current=/latest=/upgrade= and exit (also appended
#                         to $GITHUB_OUTPUT when set)
#   SKIP_CODEGEN=1        skip `make codegen` (schema.json and SDKs)
#   SKIP_TESTS=1          skip `make test_provider`
#   STRICT=1              exit non-zero when any step failed
#   GH_TOKEN/GITHUB_TOKEN token for the GitHub API (used by gh, and by curl when gh is missing)
#
# Every step after the version check records its result, so a failure in any of them (`go get`,
# the generator meeting a schema construct without a rule, a compile break in `make codegen`,
# failing tests) still produces a status and a report for the pull request. The script exits 0
# once the upgrade was attempted, whatever the status, unless STRICT=1.
#
# Outputs (in bin/, which is gitignored):
#   agent-schema.<current>.json  the schema vendored before the upgrade
#   upgrade-<step>.log           the output of each step (go-get, generate, codegen, test)
#   upgrade-report.md            schema change report, step results and the failing step's log
#   upgrade-status               ok, go-get-failed, generate-failed, codegen-failed, tests-failed
#                                or script-failed
#   upgrade-failed-step          the step that failed (empty when status is ok)
set -euo pipefail

readonly MODULE=github.com/docker/docker-agent
readonly REPO=docker/docker-agent
readonly SCHEMA=provider/pkg/render/schema/agent-schema.json

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

step() { printf '\n==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}
usage() { sed -n '2,/^set -euo/{/^#/s/^# \{0,1\}//p;}' "${BASH_SOURCE[0]}"; }

# semver_gt A B: succeeds when A is a higher semantic version than B (leading "v" ignored).
semver_gt() {
  local a="${1#v}" b="${2#v}"
  a="${a%%+*}"
  b="${b%%+*}"
  [ "$a" = "$b" ] && return 1
  local a_core="${a%%-*}" b_core="${b%%-*}"
  if [ "$a_core" = "$b_core" ]; then
    # Same version core: a release is higher than any of its pre-releases.
    case "$a" in *-*) ;; *) return 0 ;; esac
    case "$b" in *-*) ;; *) return 1 ;; esac
  else
    a="$a_core"
    b="$b_core"
  fi
  [ "$(printf '%s\n%s\n' "$a" "$b" | sort -V | tail -n 1)" = "$a" ]
}

latest_release() {
  local tag="" json token
  if command -v gh >/dev/null 2>&1; then
    tag="$(gh api "repos/$REPO/releases/latest" --jq .tag_name 2>/dev/null || true)"
  fi
  if [ -z "$tag" ]; then
    token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
    local -a curl_args=(-fsSL -H "Accept: application/vnd.github+json")
    if [ -n "$token" ]; then
      curl_args+=(-H "Authorization: Bearer $token")
    fi
    json="$(curl "${curl_args[@]}" "https://api.github.com/repos/$REPO/releases/latest")" ||
      die "could not query the latest $REPO release (set DOCKER_AGENT_VERSION to skip the lookup)"
    if command -v jq >/dev/null 2>&1; then
      tag="$(printf '%s' "$json" | jq -r '.tag_name // empty')"
    else
      tag="$(printf '%s\n' "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1 |
        sed 's/.*"\([^"]*\)"$/\1/')" || true
    fi
  fi
  [ -n "$tag" ] || die "could not determine the latest $REPO release"
  printf '%s\n' "$tag"
}

current_version() {
  (cd provider && go list -m -f '{{.Version}}' "$MODULE")
}

# ---------------------------------------------------------------- arguments
check_only="${CHECK_ONLY:-0}"
target="${DOCKER_AGENT_VERSION:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --check) check_only=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    v[0-9]* | [0-9]*) target="$1" ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done

command -v go >/dev/null 2>&1 || die "go is required"

# ---------------------------------------------------------------- (a) versions
[ "$check_only" = 1 ] || step "Resolve versions"
current="$(current_version)"
[ -n "$current" ] || die "$MODULE is not required by provider/go.mod"
if [ -z "$target" ]; then
  target="$(latest_release)"
fi
target="v${target#v}"

upgrade=false
if semver_gt "$target" "$current"; then
  upgrade=true
fi

if [ "$check_only" = 1 ]; then
  printf 'current=%s\nlatest=%s\nupgrade=%s\n' "$current" "$target" "$upgrade"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf 'current=%s\nlatest=%s\nupgrade=%s\n' "$current" "$target" "$upgrade" >>"$GITHUB_OUTPUT"
  fi
  exit 0
fi

if [ "$upgrade" != true ]; then
  echo "docker-agent $current is up to date (latest $target)"
  exit 0
fi
echo "Upgrading $MODULE from $current to $target"

mkdir -p bin
old_schema="bin/agent-schema.$current.json"
report=bin/upgrade-report.md
status_file=bin/upgrade-status
failed_step_file=bin/upgrade-failed-step
rm -f "$report" "$status_file" "$failed_step_file" bin/upgrade-*.log

status=ok
failed_step=""
failed_log=""
step_rows=()

# fence returns a Markdown code fence that does not occur in file $1.
fence() {
  if grep -q '```' "$1" 2>/dev/null; then printf '~~~~'; else printf '```'; fi
}

# on_exit makes sure a status and a report exist even when the script stops unexpectedly, so
# the workflow can always open the pull request.
# shellcheck disable=SC2329 # invoked by the EXIT trap
on_exit() {
  local rc=$?
  if [ ! -f "$status_file" ]; then
    printf 'script-failed\n' >"$status_file"
    printf '%s\n' "${failed_step:-upgrade-docker-agent.sh}" >"$failed_step_file"
    # shellcheck disable=SC2016 # literal Markdown backticks
    printf '# docker-agent %s → %s: upgrade failed\n\n`scripts/upgrade-docker-agent.sh` stopped unexpectedly (exit %s) during "%s". See the workflow log.\n' \
      "$current" "$target" "$rc" "${failed_step:-setup}" >"$report"
  fi
}
trap on_exit EXIT

# run_step NAME LOG FAIL_STATUS COMMAND...: runs COMMAND, sending its output to the terminal and
# to LOG. On failure it records FAIL_STATUS, the step and its log, and returns 1.
run_step() {
  local name=$1 log=$2 fail_status=$3
  shift 3
  step "$name (log: $log)"
  if "$@" 2>&1 | tee "$log"; then
    step_rows+=("| \`$name\` | ok |")
    return 0
  fi
  step_rows+=("| \`$name\` | **failed** |")
  status=$fail_status
  failed_step=$name
  failed_log=$log
  warn "$name failed; see $log"
  return 1
}

skip_step() {
  step_rows+=("| \`$1\` | skipped |")
}

# shellcheck disable=SC2329 # invoked through run_step
go_get() { (cd provider && go get "$MODULE@$target" && go mod tidy); }

# example_names turns golden file paths (stdin) into a comma-separated list of example names.
example_names() {
  # shellcheck disable=SC2016 # literal Markdown backticks
  sed -n 's|^.*/\([^/]*\)\.yaml$|`\1`|p' | paste -sd, - | sed 's/,/, /g'
}

# examples_report prints the docker-agent examples that the regenerated example programs
# (examples/docker-agent-examples, one golden YAML per example in testdata/) added, removed or
# changed, compared with the git index.
examples_report() {
  local dir=examples/docker-agent-examples/testdata
  local added removed changed
  git rev-parse --git-dir >/dev/null 2>&1 || return 0
  added="$(git ls-files --others --exclude-standard -- "$dir" | example_names)"
  removed="$(git ls-files --deleted -- "$dir" | example_names)"
  changed="$(git diff --name-only --diff-filter=M -- "$dir" | example_names)"
  printf '\n## Example programs\n\n'
  # shellcheck disable=SC2016 # literal Markdown backticks
  printf '`make generate_config_types` regenerated the TypeScript and Python programs in `examples/docker-agent-examples` from the examples of docker-agent %s.\n\n' "$target"
  if [ -z "$added$removed$changed" ]; then
    printf 'No example was added, removed or changed.\n'
    return 0
  fi
  [ -z "$added" ] || printf -- '- Added: %s\n' "$added"
  [ -z "$removed" ] || printf -- '- Removed: %s\n' "$removed"
  [ -z "$changed" ] || printf -- '- Changed (the rendered YAML differs): %s\n' "$changed"
}

# ---------------------------------------------------------------- (b) go.mod
step "Save the vendored schema of $current to $old_schema"
if [ -f "$SCHEMA" ]; then
  cp "$SCHEMA" "$old_schema"
else
  warn "$SCHEMA not found; using agent-schema.json from the $current module"
  dir="$(cd provider && go mod download -json "$MODULE@$current" | sed -n 's/^[[:space:]]*"Dir":[[:space:]]*"\(.*\)",*$/\1/p')"
  [ -f "$dir/agent-schema.json" ] || die "agent-schema.json not found in $MODULE@$current"
  cp "$dir/agent-schema.json" "$old_schema"
fi

run_step "go get $MODULE@$target" bin/upgrade-go-get.log go-get-failed go_get || true

# ---------------------------------------------------------------- (c)-(e) typed inputs
if [ "$status" = ok ]; then
  run_step "make generate_config_types" bin/upgrade-generate.log generate-failed make generate_config_types || true
else
  skip_step "make generate_config_types"
fi

# ---------------------------------------------------------------- (f) codegen
if [ "${SKIP_CODEGEN:-0}" = 1 ]; then
  skip_step "make codegen (SKIP_CODEGEN=1)"
elif [ "$status" = ok ]; then
  run_step "make codegen" bin/upgrade-codegen.log codegen-failed make codegen || true
else
  skip_step "make codegen"
fi

# ---------------------------------------------------------------- (g) tests
if [ "${SKIP_TESTS:-0}" = 1 ]; then
  skip_step "make test_provider (SKIP_TESTS=1)"
elif [ "$status" = ok ]; then
  run_step "make test_provider" bin/upgrade-test.log tests-failed make test_provider || true
else
  skip_step "make test_provider"
fi

# ---------------------------------------------------------------- (h) report and changelog
step "Write the report to $report"
# The new schema is the one of the downloaded module, so the report also works when the
# generator failed. When `go get` failed there is no new schema.
new_schema=""
if [ "$status" != go-get-failed ]; then
  new_dir="$(cd provider && go list -m -f '{{.Dir}}' "$MODULE" 2>/dev/null || true)"
  if [ -n "$new_dir" ] && [ -f "$new_dir/agent-schema.json" ]; then
    new_schema="$new_dir/agent-schema.json"
  fi
fi
if [ -n "$new_schema" ] && (cd provider && go run ./internal/schemagen diff \
  -old "../$old_schema" -new "$new_schema" -from "$current" -to "$target") >"$report.tmp"; then
  mv "$report.tmp" "$report"
else
  rm -f "$report.tmp"
  printf '# docker-agent %s → %s: configuration schema changes\n\nThe schema change report could not be generated (see "Upgrade steps").\n' \
    "$current" "$target" >"$report"
fi
{
  case "$status" in
  go-get-failed | generate-failed) ;;
  *) examples_report ;;
  esac
  printf '\n## Upgrade steps\n\n| Step | Result |\n|---|---|\n'
  printf '%s\n' "${step_rows[@]}"
  if [ "$status" != ok ]; then
    f="$(fence "$failed_log")"
    # shellcheck disable=SC2016 # literal Markdown backticks
    printf '\n## Failure: `%s`\n\nStatus `%s`. Last 150 lines of the step output:\n\n%stext\n' "$failed_step" "$status" "$f"
    tail -n 150 "$failed_log"
    printf '%s\n' "$f"
  fi
} >>"$report"

if [ -n "$new_schema" ]; then
  step "Add a CHANGELOG entry"
  if ! (cd provider && go run ./internal/schemagen changelog -file ../CHANGELOG.md \
    -old "../$old_schema" -new "$new_schema" -from "$current" -to "$target"); then
    warn "CHANGELOG.md could not be updated"
  fi
fi

printf '%s\n' "$failed_step" >"$failed_step_file"
printf '%s\n' "$status" >"$status_file"

# ---------------------------------------------------------------- summary
step "Summary"
printf 'docker-agent:  %s → %s\n' "$current" "$target"
printf 'status:        %s (%s)\n' "$status" "$status_file"
[ -z "$failed_step" ] || printf 'failed step:   %s (%s)\n' "$failed_step" "$failed_log"
printf 'report:        %s\n' "$report"
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  echo "changed files:"
  git status --short | sed 's/^/  /'
fi

if [ "$status" != ok ] && [ "${STRICT:-0}" = 1 ]; then
  die "the upgrade needs attention: $failed_step failed (STRICT=1)"
fi
exit 0
