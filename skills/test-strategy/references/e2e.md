# End-to-end tests for a CLI or service

Read this when the behaviour at risk is what a user observes from the built
artefact: exit status, output, files and side effects. Unit and integration
tests exercise code; an end-to-end test exercises the product. Keep them few:
one per user-visible scenario no lower-level test can prove, usually install,
first run, repeat run, failure and clean-up.

## Run the real artefact

- Build the binary the release process builds, with the same flags and version
  stamp, and run that file. Importing the main package and calling its entry
  function is an integration test, not end-to-end.
- For a service, start the built binary on a port the test allocates, wait for
  its readiness endpoint, and stop it with the signal users send. Assert on
  responses, not on internal state.
- Drive it as users do: argv, stdin, environment, configuration files, HTTP.
  Never set up state through package internals.

## Isolate the environment

- Create a fresh temporary root per run; put home, configuration, state, cache
  and working directories under it and remove it after.
- Set every environment variable the program reads to a path inside that root
  (`HOME`, `XDG_*`, `TMPDIR`, tool homes) and clear the rest; anything
  inherited is a hidden input.
- Put stubs for tools the program must not call first on `PATH`; they record
  the call and fail, so an unwanted invocation fails the test.
- Cut the network: an empty trust store or an unroutable proxy makes a stray
  fetch fail fast instead of hanging.
- Use `TERM=dumb`, `LC_ALL=C` and empty stdin so output is plain and a prompt
  cannot block. An escape sequence in captured output means the program tried
  to show interactive UI: fail.

## Assert observable behaviour

- Assert exit status, structured output (prefer `--json` and decode it)
  and the state on disk: which files exist, their modes and contents, and that
  files the program does not own are untouched.
- Snapshot the root before and after a dry run or a no-op repeat run and
  require equality; this catches unrequested writes.
- Assert on the names, versions and paths the product promises, not wording.

## Keep them deterministic

- Bound every subprocess with a timeout and kill its process group on expiry,
  so a hang is a reported failure, not a stuck CI job.
- Cap captured output, so a runaway log fails fast.
- Use directory names with spaces so quoting defects surface here first.
- Control the clock and randomness through the program's flags or environment
  where behaviour depends on them; otherwise assert on properties (a valid
  semantic version) instead of exact values.
- Never sleep to synchronise; wait on a readiness signal with a deadline.
- Run on every platform and shell the product supports, or say which the test
  could not cover.

## Worked example: `cw` end-to-end tests

The tests in `cmd/cw` build the real binary once with a plain `go build` and
drive it as a user or a host would, through its process boundary only.

- `cmd/cw/hooks_test.go` runs each command in the plugin's `hooks/hooks.json`
  through `sh -c` with a clean environment (`PATH`, `HOME`, `CW_TASK_STORE`,
  `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`), a temporary Git repository and
  the hook's JSON on stdin. It covers `cw` on `PATH`, a missing `cw` and a stub
  older than the hook command, and asserts the exit status and the exact stdout
  the host reads.
- `cmd/cw/hostfiles_e2e_test.go` runs `cw install --host codex`, a repeat
  install and `cw uninstall --host codex` against temporary `HOME`,
  `CODEX_HOME` and XDG directories and a fixture checkout. It snapshots the
  trees before and after, so a repeat install or a rejected `--dry-run` that
  writes anything fails, and checks usage errors (exit 2) leave `HOME` empty.
- `cmd/cw/task_e2e_test.go` drives `cw task start`, `run`, `assess` and `verify`
  in a temporary repository and state directory, and asserts `verify`'s exit
  code and summary: complete after passing evidence, and incomplete with the
  named problem before assessment, after a failed check, and after the source
  changes.

Each case is named after the requirement it guards, so a red run names the broken
promise.
