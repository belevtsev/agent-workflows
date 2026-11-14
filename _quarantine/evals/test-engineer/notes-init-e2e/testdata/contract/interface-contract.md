# Interface contract: `notes init`

A command-line tool. Module `example.com/notes`, binary built from
`./cmd/notes`. This contract covers only the `init` subcommand.

## Invocation

`notes init [--dir PATH] [--json]`

## Requirements

- R1: `notes init` creates `PATH/.notes/config.json` containing exactly
  `{"version":1}` followed by a newline, with mode 0600, and the directory
  `PATH/.notes` with mode 0700. `PATH` defaults to the current working
  directory. Exit status 0.
- R2: With `--json`, stdout is one JSON object
  `{"created": true, "path": "<absolute PATH>/.notes"}` and nothing else is
  written to stdout. Without `--json`, stdout is the single line
  `initialized <absolute PATH>/.notes`.
- R3: Running `init` again on an initialized `PATH` exits 0, changes no file
  (contents, modes and modification times stay the same) and reports
  `"created": false` with `--json`.
- R4: If `PATH/.notes` exists and is a symbolic link or a regular file, the
  command exits 2, writes nothing through or beside it, and prints one line
  to stderr starting with `notes: `; with `--json` stdout is
  `{"error": "<message>"}`.
- R5: If `PATH` does not exist, the command exits 2 with the same error
  shape; it never creates `PATH`.
- R6: The command writes nothing outside `PATH/.notes` and reads no file
  outside `PATH`.
- R7: With `TERM=dumb` or a non-terminal stdout the output contains no
  escape sequences, and the command never waits for input.

## Scope

- No other subcommands, no configuration keys other than `version`.
