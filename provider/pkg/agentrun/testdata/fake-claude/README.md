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
| `FAKE_CLAUDE_PIDFILE` | Where the `hang*`, `term-trap`, `leftover` and `detached-writer` modes write the PID of the child they start, and where `background-session`, `session-orphan`, `double-fork`, `session-timeout`, `oneshot`, `oneshot-daemon`, `cookie-parent` and `env-cleared` append the PIDs of the processes they start, one per line. Default `./.fake-claude.pid`. |
| `FAKE_CLAUDE_IGNORE_TERM` | When set, the fake and every process it starts ignore SIGTERM (for example `leftover`, to test the SIGKILL that follows). |
| `FAKE_CLAUDE_TRAP_TERM` | When set, the fake itself logs `term` when it gets SIGTERM and carries on; the processes it starts do not inherit this (they stop on SIGTERM). |
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
cookie=set|unset               (DOCKER_AGENT_RUN_COOKIE, the run's cookie, non-empty; never its value)
stdin=<the prompt>             (last, because a prompt can span lines)
```

Mode `term-trap`, and every mode with `FAKE_CLAUDE_TRAP_TERM`, also appends a line `term` when the fake receives
SIGTERM.

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
| `hang-detached` | Prints the init line, starts `sleep 300` in a session of its own (`setsid`, or `perl -MPOSIX` on macOS; PID to the pidfile), then sleeps. The group signals do not reach the detached child and the fake does not stop it; the runner stops it because it recorded it. |
| `term-trap` | Prints the init line, starts the detached child and sleeps. On SIGTERM it logs `term`, waits 0.3 s (so that a SIGKILL right after the SIGTERM leaves the child running), kills the detached child, and exits 143, as a CLI that stops the commands it started would. It writes the child's PID to the pidfile once the trap is set. |
| `stderr-open` | Starts `sleep 5` that keeps stdout and stderr open, prints `session.jsonl`, exits 0. |
| `leftover` | Starts `sleep 300` in its process group with its output redirected (PID to the pidfile), prints `session.jsonl`, exits 0. |
| `detached-writer` | Prints `session.jsonl`, starts a shell in a session of its own that inherits stdout and writes `tick` to it every 0.2 s until the pipe is closed (its PID to the pidfile), exits 0. |
| `background-session` | Like a command that Claude Code ran in the background and left running: starts a shell in a session of its own that runs `sleep 300` as its child and waits for it (as Claude Code runs `zsh -c '... eval <command>'` in a new session), appends the shell's PID and the sleep's to the pidfile, works 1 s more (the runner reads the process table every 0.2 s), prints `session.jsonl` and exits 0. On SIGTERM the shell appends its PID to `<pidfile>.term`, takes 0.3 s to stop and then appends its PID to `<pidfile>.stopped`, so a SIGKILL right after the SIGTERM leaves no line in `.stopped`, and a second SIGTERM meanwhile a second line in `.term` (with dash, which runs traps within traps). Nothing is written when the signal is ignored. |
| `session-orphan` | As `background-session`, and the shell also starts a subshell that starts another `sleep 300` and exits at once: that sleep's parent is gone before it can be recorded, but it stays in the shell's process group and session. Appends 3 PIDs (shell, sleep, orphaned sleep). |
| `double-fork` | A daemon: a shell in a session of its own starts `sleep 300` in another session of its own (the sleep's PID to the pidfile) and exits after 1 s, so the sleep belongs to init (or a subreaper) when the fake prints `session.jsonl` and exits 0, 0.5 s later. |
| `oneshot` | Three times, 0.1 s apart: a shell in a session of its own runs `sleep 300 >/dev/null 2>&1 &` and exits at once, as Claude Code runs `cmd >/dev/null 2>&1 &` in one Bash call (the sleeps' PIDs to the pidfile). The sleeps' parent is gone before a read of the process table can see it; they still have the run's cookie. Prints `session.jsonl` and exits 0, 0.5 s later. |
| `oneshot-daemon` | A double fork: a shell in a session of its own starts `sleep 300` in another session of its own and exits at once (the sleep's PID to the pidfile). Prints `session.jsonl` and exits 0, 0.5 s later. |
| `cookie-parent` | A shell in a session of its own starts, in another session of its own, a shell with the run's cookie and exits at once; that shell starts `sleep 300` without the cookie (`env -i`) in a session of its own and waits for it (the shell's PID, then the sleep's, to the pidfile). Prints `session.jsonl` and exits 0, 0.5 s later. Only a walk below the process the cookie found reaches the sleep. |
| `env-cleared` | As `oneshot-daemon`, but the sleep runs with a cleared environment (`env -i PATH=/usr/bin:/bin`), so the run's cookie does not reach it: the documented limit. Tests kill it. |
| `session-timeout` | Prints the init line, starts a command in the background and one in the foreground (each a shell in a session of its own with `sleep 300` as its child, as in `background-session`; 4 PIDs, the background command's first) and waits for the foreground one, so the run times out. With `FAKE_CLAUDE_TRAP_TERM`, a SIGTERM to the fake does not end the wait: the fake exits only when the foreground command ended. |
| `empty-result` | Prints `session-empty-result.jsonl`: `session.jsonl` with an empty `result`, as Claude Code reports it when its last message does not end with text. Exits 0. |
| `empty` | Prints `session-empty.jsonl`: the init line and a successful result with an empty `result`, without any text. Exits 0. |
| `deferred` | Prints `session-deferred.jsonl`: some text and a successful result with an empty `result` and a `deferred_tool_use` (`AskUserQuestion`). Exits 0. |

## Fixtures

The `*.jsonl` files are constructed in the shapes the Claude Code CLI writes with `--print --verbose
--include-partial-messages --output-format stream-json` and that `github.com/rumpl/harness/claudecode` parses. The init
line of each reports `"apiKeySource":"none"` and `"claude_code_version":"2.1.283"`.
`TestFakeClaudeStreamMatchesTheParser` (`../../harness_test.go`) feeds every fixture to that parser, so a driver
upgrade that changes the parsing fails there first.
