# Fake `codex` for AgentRun tests

`codex` is a POSIX `sh` stand-in for the Codex CLI (`codex exec --json` of Codex 0.162). AgentRun's coding-harness
tests put it on `PATH` instead of the real CLI, which would run on the developer's Codex login. It never calls a model:
it checks the arguments AgentRun passes, logs how it was started, changes the working directory the way a real run
would, and prints a recorded JSONL session from the `*.jsonl` files next to it.

## Using it

- Copy `codex`, the `*.jsonl` and the `*.last` files into one directory of a test's own (for example `t.TempDir()`)
  and make `codex` executable (mode 0755). The script finds the fixtures next to itself.
- Build the test's `PATH` from that directory and `/usr/bin:/bin`. Assert that `codex` resolves to the fake.
- Point `CODEX_HOME` at a directory of the test's own: the fake reads `$CODEX_HOME/config.toml` (see below), and
  AgentRun reads `config.toml` and `auth.json` there.
- Fail the test when the log is missing after a run: a missing log means the fake did not run.

## Invocations

| Arguments | Behaviour |
|---|---|
| `--version` | Logs `kind=version` and prints `codex-cli $FAKE_CODEX_VERSION` (default `0.162.1`). With `FAKE_CODEX_VERSION_FAIL`, writes an error to stderr and exits 1. |
| `--config=sandbox_mode="workspace-write" sandbox -- true` | The sandbox check AgentRun runs under `toolApproval: deny`. Logs `kind=sandbox`; behaves as `FAKE_CODEX_SANDBOX` says: `ok` (default, exit 0), `unavailable` (bubblewrap's user-namespace error on stderr, exit 1), `missing` (`error: unrecognized subcommand 'sandbox'`, exit 2), `hang` (starts `sleep 300`, its PID to the pidfile, and waits). Any other arguments exit 2 with `fake codex: ...`. |
| `exec ...` | A turn, see below. |

Anything else exits 2 with `fake codex: unexpected arguments`.

`bwrap` next to it is a fake bubblewrap for the sandbox check's fallback on Linux (a codex without `codex sandbox`):
it logs `kind=bwrap` and its arguments, and exits 0, or with `FAKE_BWRAP=unavailable` writes `bwrap: setting up uid
map: Permission denied` and exits 1.

## The contract of `exec`

The fake exits 2 with `fake codex: ...` on stderr when AgentRun breaks its contract with Codex:

- `--json`, `--color=never`, `--skip-git-repo-check` and `--output-last-message=<file>` are present;
- exactly one of `--dangerously-bypass-approvals-and-sandbox` (logged as `sandbox=off`) and
  `--sandbox=workspace-write` with `--config=approval_policy="never"` (logged as `sandbox=workspace-write`);
- `--model=<m>` and `--output-schema=<file>` are optional single arguments;
- every flag comes before `resume <thread>`, and the last argument is `-` (the prompt on stdin); no other positional
  argument (a prompt in argv) is accepted.

An unknown option gives `error: unexpected argument '<option>' found` and exit 2, as Codex's argument parser does.

Then, in order:

- the prompt is read from stdin; an empty one gives `No prompt provided via stdin.` and exit 1, as `codex exec -`
  does;
- a `CODEX_HOME` that is not a directory gives `Error finding codex home: CODEX_HOME points to "<dir>", but that path
  does not exist` and exit 1;
- when `$CODEX_HOME/config.toml` (default `~/.codex`) has a line `forced_login_method = "chatgpt"` and
  `CODEX_API_KEY` is set, the fake deletes `$CODEX_HOME/auth.json`, writes `Error: ChatGPT login is required, but an
  API key is currently being used. Logging out.` and exits 1, as Codex does. AgentRun must never start it then;
- a prompt that starts with `Delete` is a delete prompt: with `.fail-delete` in the working directory the fake behaves
  as mode `failed-auth`, otherwise it runs `rm -f NOTES.md` and behaves as mode `ok`. Any other prompt is written to
  `NOTES.md` in the working directory.

## Environment variables

| Variable | Meaning |
|---|---|
| `FAKE_CODEX_MODE` | The behaviour, see below. Default `ok`. |
| `FAKE_CODEX_LOG` | The log file. Default `./.fake-codex.log`. |
| `FAKE_CODEX_PIDFILE` | Where the process modes write or append the PIDs of the processes they start. Default `./.fake-codex.pid`. |
| `FAKE_CODEX_IGNORE_TERM` | When set, the fake and every process it starts ignore SIGTERM. |
| `FAKE_CODEX_STDERR` | Extra text the fake writes to stderr in every exec mode (for redaction tests). |
| `FAKE_CODEX_PRINT` | Replaces `__TOKEN__` in the fixtures (mode `token`). The name avoids `KEY`, `TOKEN` and `SECRET`: AgentRun redacts the values of credential-named variables, and the tests check that the login's tokens are redacted because they are in `auth.json`. |
| `FAKE_CODEX_ROTATED` | The new access token of mode `rotate`. |
| `FAKE_CODEX_NO_LAST` | When set, the fake writes no `--output-last-message` file, so the answer comes from the stream. |
| `FAKE_CODEX_VERSION`, `FAKE_CODEX_VERSION_FAIL`, `FAKE_CODEX_SANDBOX` | See the invocations. |
| `FAKE_MARKER` | Logged as `marker=`, to tell whether the provider's environment reached the CLI. |

## Log

Each invocation appends one entry. It never contains a credential value.

```text
--- invocation
kind=exec|version|sandbox
argc=<number of arguments>
arg=<argument>                 (one line per argument)
cwd=<working directory>
pid=<PID of the fake>
realcwd=<working directory, symlinks resolved>        (exec only, from here on)
pwd_env=<PWD the CLI was started with (from /proc on Linux, else $PWD)>
home=<HOME>
codex_home=<CODEX_HOME>
mode=<the effective mode>
sandbox=workspace-write|off
model=<the --model value>
resume=<the thread of resume>
output_schema=<the --output-schema file>
schema=<the schema file's content, on one line>      (when the file exists)
last_message=<the --output-last-message file>
codex_api_key=set|unset        (CODEX_API_KEY non-empty)
codex_access_token=set|unset   (CODEX_ACCESS_TOKEN non-empty)
openai_api_key=set|unset       (OPENAI_API_KEY non-empty)
codex_thread_id=set|unset      (CODEX_THREAD_ID, a marker of a parent Codex session)
codex_sandbox=set|unset        (CODEX_SANDBOX, another marker)
rust_log=<RUST_LOG>
https_proxy=<HTTPS_PROXY>
marker=<FAKE_MARKER>
cookie=set|unset               (DOCKER_AGENT_RUN_COOKIE)
agentrun_cookie=set|unset      (DOCKER_AGENT_AGENTRUN_COOKIE)
stdin=<the prompt>             (last, because a prompt can span lines)
```

The modes `hang`, `interrupt` and `term-trap` also append a line `int` when the fake gets SIGINT, and `interrupt` and
`term-trap` a line `term` when it gets SIGTERM.

## Modes

| Mode | Behaviour |
|---|---|
| `ok` (default) | Prints `session.jsonl`, writes `session.last` to the `--output-last-message` file, exits 0. With `resume <thread>` it behaves as `resume` (the thread's running usage total grows). |
| `json`, `schema`, `token`, `declined`, `tool-failed`, `item-error`, `retry`, `not-json`, `sandbox-denied`, `lagged`, `collab`, `resume` | The same with `session-<mode>.jsonl` and `session-<mode>.last`. |
| `failed-auth`, `refresh-reused`, `usage-limit`, `not-included`, `quota`, `unknown-model` | Prints `session-<mode>.jsonl` (an `error` event and `turn.failed` with Codex's message) and exits 1. |
| `rotate` | As Codex refreshing a ChatGPT login: rewrites `$CODEX_HOME/auth.json` with the access token `$FAKE_CODEX_ROTATED`, then prints `session-token.jsonl` with `__TOKEN__` replaced by that token. |
| `no-message` | A completed turn without an agent message and without a `.last` file; exits 0. |
| `incomplete` | `thread.started` and `turn.started` only; exits 0. |
| `exit-after-complete` | As `ok`, then writes an error to stderr and exits 1. |
| `cli-args` | `error: unexpected argument '--output-last-message' found`, exit 2 (a Codex that does not know AgentRun's arguments). |
| `mcp-failed` | The start of a session, then `Error: required MCP server ``docs`` failed to initialize: ...` on stderr, exit 1. |
| `hang` | Ignores SIGTERM (so does its child) and logs SIGINT, prints the start of a session, starts `sleep 300` in its group (PID to the pidfile) and waits. Only SIGKILL stops it. |
| `interrupt` | As Codex on SIGINT: prints the start of a session, starts `sleep 300` in a session of its own (PID to the pidfile once its traps are set) and one in its group; SIGINT logs `int`, kills the detached command and exits 1 without a terminal event. |
| `term-trap` | Prints the start of a session, starts the detached command and waits; SIGINT logs `int` and is otherwise ignored; SIGTERM logs `term`, waits 0.3 s, kills its commands and exits 143. |
| `hang-detached` | Prints the start of a session, starts `sleep 300` in a session of its own (PID to the pidfile) and runs `sleep 300`; SIGINT ends the fake at once and leaves both running. |
| `leftover` | Starts `sleep 300` in its group (PID to the pidfile), then as `ok`. |
| `background-session` | A command left running in the background: a shell in a session of its own with `sleep 300` as its child (the shell's PID and the sleep's appended to the pidfile; `<pidfile>.term` and `<pidfile>.stopped` as in the fake claude), 1 s later as `ok`. |
| `oneshot` | Three times: a shell in a session of its own runs `sleep 300 &` and exits at once (the sleeps' PIDs appended to the pidfile; only the run's cookie finds them), 0.5 s later as `ok`. |
| `mcp-group` | A stdio MCP server as Codex starts one: a direct child in a process group of its own in the CLI's session that takes 0.3 s to stop on SIGTERM (its PID appended to the pidfile), 0.5 s later as `ok`. |

## Fixtures

The `*.jsonl` files are written in the shapes of Codex 0.162's `exec_events.rs` and
`event_processor_with_jsonl_output.rs`: `thread.started`, `turn.started`, `item.started|updated|completed` with
`{"id":"item_N","type":...}` items, `turn.completed` with the thread's usage total (`input_tokens` includes
`cached_input_tokens`), `turn.failed`, and `error` events. A `web_search` item carries the key `id` twice (the item's and
the search's), as Codex's serializer writes it. `__THREAD__`, `__CWD__` (the real working directory, for the absolute
paths of `file_change`) and `__TOKEN__` are placeholders the fake replaces. A `*.last` file is the final message Codex
writes to the `--output-last-message` file. `TestFakeCodexStreamMatchesTheParser` (`../../harness_codex_test.go`) feeds
every fixture to the rumpl/harness codex driver's parser and validator, so a driver upgrade that changes the parsing
fails there first.
