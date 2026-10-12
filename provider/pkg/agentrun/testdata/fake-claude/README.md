# Fake `claude` for AgentRun tests

`claude` is a POSIX `sh` stand-in for the Claude Code CLI. AgentRun's coding-harness tests put it on `PATH` instead of
the real CLI, which would run with `--dangerously-skip-permissions` on the developer's Claude login. It never calls a
model: it logs how it was started, changes the working directory the way a real run would, and prints a recorded
`stream-json` session from the `*.jsonl` files next to it.

## Using it

- Copy `claude` and the `*.jsonl` files into one directory of a test's own (for example `t.TempDir()`) and make
  `claude` executable (mode 0755), rather than relying on git's mode bits. The script finds the fixtures next to
  itself.
- Build the test's `PATH` from that directory, the directories of other tools the test needs (`pulumi`, `node`), and
  `/usr/bin:/bin`. Assert that `claude` resolves to the fake in that `PATH`.
- Fail the test when the log is missing after a run: a missing log means the fake did not run.

## Input

The prompt comes on stdin (`prompt=$(cat)`); AgentRun never passes it as an argument.

- A prompt that starts with `Delete` is a delete prompt. When `.fail-delete` exists in the working directory, the fake
  behaves as mode `error`; otherwise it runs `rm -f NOTES.md` and behaves as mode `ok`, whatever `FAKE_CLAUDE_MODE`
  says.
- Any other prompt is written to `NOTES.md` in the working directory, and the fake then behaves as `FAKE_CLAUDE_MODE`
  says.

## Environment variables

| Variable | Meaning |
|---|---|
| `FAKE_CLAUDE_MODE` | The behaviour, see below. Default `ok`. It can come from the provider's environment or from the AgentRun's `env`. |
| `FAKE_CLAUDE_LOG` | The log file. Default `./.fake-claude.log` in the working directory. |
| `FAKE_CLAUDE_PIDFILE` | Where the `hang*`, `term-trap` and `leftover` modes write the PID of the child they start. Default `./.fake-claude.pid`. |
| `FAKE_CLAUDE_IGNORE_TERM` | When set, the fake and every process it starts ignore SIGTERM (for example `leftover`, to test the SIGKILL that follows). |
| `FAKE_CLAUDE_STDERR` | Extra text the fake writes to stderr, in every mode (for redaction tests). |
| `FAKE_CLAUDE_KEY_SOURCE` | Replaces `"apiKeySource":"none"` in the init line, for example with `ANTHROPIC_API_KEY`. (The name avoids `API_KEY`: AgentRun redacts the values of credential-named variables.) |
| `FAKE_MARKER` | Logged as `marker=`, to tell whether the provider's environment reached the CLI. |

AgentRun redacts every value of an AgentRun's `env` (of 4 characters or more) from errors, warnings and progress. A
mode passed through `env`, such as `error`, therefore shows as `[secret]` wherever the word appears in a message;
tests that assert on such words set the mode in the provider's environment instead.

## Log

Each invocation appends one entry to the log. It never contains a token value.

```text
--- invocation
argc=<number of arguments>
arg=<argument>                 (one line per argument)
cwd=<working directory, as pwd prints it>
realcwd=<working directory, symlinks resolved>
pwd_env=<PWD the CLI was started with (from /proc on Linux, else $PWD)>
home=<HOME>
pid=<PID of the fake>
mode=<the effective mode>
oauth_token=set|unset          (CLAUDE_CODE_OAUTH_TOKEN non-empty)
api_key=set|unset              (ANTHROPIC_API_KEY non-empty)
claudecode=set|unset           (CLAUDECODE non-empty)
is_sandbox=<IS_SANDBOX>
config_dir=<CLAUDE_CONFIG_DIR>
marker=<FAKE_MARKER>
stdin=<the prompt>             (last, because a prompt can span lines)
```

Mode `term-trap` also appends a line `term` when it receives SIGTERM.

## Modes

| Mode | Behaviour |
|---|---|
| `ok` (default) | Prints `session.jsonl` and exits 0. The result is `Done: NOTES.md lists the changes.`, after one `Write` tool call. |
| `token` | Prints `session-token.jsonl` with `__TOKEN__` replaced by `$CLAUDE_CODE_OAUTH_TOKEN` in a `Bash` tool call's input, its result and the result text. |
| `json` | Prints `session-json.jsonl`: the result is `{"risk":"low","summary":"fake review"}` in a fenced `json` code block. |
| `error` | Prints the init line and an `is_error` result (`api_error_status` 401, `Failed to authenticate: OAuth session expired and could not be refreshed`), writes `auth failed for $CLAUDE_CODE_OAUTH_TOKEN` to stderr, exits 1. |
| `error-subtype` | Prints the init line and an `error_during_execution` result with `errors` and no `result`, exits 1. |
| `contention` | Prints the init line and an `is_error` result saying that another Claude Code process is refreshing the login, exits 1. |
| `root` | Writes Claude Code's root refusal (`... cannot be used with root/sudo privileges ...`) to stderr, exits 1. |
| `no-result` | Prints the init line and an assistant message without a result line, exits 0. |
| `tool-error` | Prints `session-tool-error.jsonl`: a `Bash` call whose failed result reads like a docker-agent rejection (`Tool 'shell' is denied by ...`), then a result. Exits 0. |
| `hang` | Ignores SIGTERM, prints the init line, starts `sleep 300` in its process group (PID to the pidfile), then sleeps. Only SIGKILL stops it and its child. |
| `hang-detached` | Prints the init line, starts `sleep 300` in a session of its own (`setsid`, or `perl -MPOSIX` on macOS; PID to the pidfile), then sleeps. The detached child survives the group signals; tests kill it. |
| `term-trap` | Prints the init line, starts the detached child and sleeps. On SIGTERM it logs `term`, waits 0.3 s (so that a SIGKILL right after the SIGTERM leaves the child running), kills the detached child, and exits 143, as Claude Code stops the shell commands it started. It writes the child's PID to the pidfile once the trap is set. |
| `stderr-open` | Starts `sleep 5` that keeps stdout and stderr open, prints `session.jsonl`, exits 0. |
| `leftover` | Starts `sleep 300` in its process group with its output redirected (PID to the pidfile), prints `session.jsonl`, exits 0. |
| `detached-writer` | Prints `session.jsonl`, starts a shell in a session of its own that inherits stdout and writes `tick` to it every 0.2 s until the pipe is closed (its PID to the pidfile), exits 0. |
| `empty-result` | Prints `session-empty-result.jsonl`: `session.jsonl` with an empty `result`, as Claude Code reports it when its last message does not end with text. Exits 0. |
| `empty` | Prints `session-empty.jsonl`: the init line and a successful result with an empty `result`, without any text. Exits 0. |
| `deferred` | Prints `session-deferred.jsonl`: some text and a successful result with an empty `result` and a `deferred_tool_use` (`AskUserQuestion`). Exits 0. |

## Fixtures

The `*.jsonl` files are constructed in the shapes the Claude Code CLI writes with `--print --verbose
--include-partial-messages --output-format stream-json` and that `github.com/rumpl/harness/claudecode` parses. The init
line of each reports `"apiKeySource":"none"` and `"claude_code_version":"2.1.283"`.
`TestFakeClaudeStreamMatchesTheParser` (`../../harness_test.go`) feeds every fixture to that parser, so a driver
upgrade that changes the parsing fails there first.
