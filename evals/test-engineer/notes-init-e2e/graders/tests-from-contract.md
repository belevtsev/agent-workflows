---
type: llm
focus: { source: file, path: e2e/init_test.go }
---

PASS if `TestMain` builds the binary once into a temporary directory with
`go build` and every test runs that binary with `exec.Command`, a fresh
`t.TempDir()` working directory, an explicit minimal environment (`HOME` and
`TMPDIR` inside the temp root, `TERM=dumb`, `LC_ALL=C`, a `PATH` limited to
the Go toolchain and system binaries), empty stdin and a timeout via
`exec.CommandContext`; assertions cover exit status, decoded `--json`
output, the created file's contents and mode, and byte-for-byte unchanged
unrelated files; the repeat run asserts `created` is false and the directory
snapshot is unchanged; the symlink case asserts exit 2 and no write through
the link; and no test sleeps.
FAIL if tests import the implementation package or call its functions
directly, if the environment is inherited from the test process, if
assertions match free-form message text instead of exit codes and JSON
fields, or if a test cannot fail.
