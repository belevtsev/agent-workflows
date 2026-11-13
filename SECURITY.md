# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories on the belevtsev/agent-workflows repository (Security tab, "Report a vulnerability"). Do not open a public issue or pull request for a security problem.

Please include the affected version, steps to reproduce, and the impact you expect. You can expect an acknowledgement, and a fix or explanation as time allows. This is a single-maintainer project, so there are no response-time guarantees.

## Scope

In scope:

- The `cw` binary and its subcommands.
- `cw install --host codex` and `cw uninstall --host codex`, and the files they write or remove in Codex configuration and `~/.agents/skills`.
- The plugin manifests in `.claude-plugin/`, `.codex-plugin/` and `.agents/plugins/`.
- Skills that run commands on the user's machine.

Out of scope: vulnerabilities in third-party tools, model providers, or Codex and Claude Code themselves; report those upstream.

## Threat model

The verify gate is a guardrail for a cooperative agent that skipped a step, such as committing before `cw task verify` passed. It is not a defence against an agent that sets out to bypass it.

- **Asset.** The task store under `~/.local/state/agent-workflows` (`$XDG_STATE_HOME/agent-workflows` when that is set, the state directory given with the global `--state-dir` flag, or a store given with `--store` or `CW_TASK_STORE`). Its records hold each task's plan, check runs, reviews and verification, and the commit and push gate decides from them.
- **Actor.** The coding agent itself. It runs as the same user (same UID) as `cw`, with the same `PATH`. It can write everything that user can write, including the task store and the repository, and can put its own program ahead of any on `PATH`.
- **What the gate does.** It asks the agent to stop. On Claude Code, and on Codex once its hooks are trusted, the hook denies a `git commit` or `git push` it recognises while a governing task has no passing `cw task verify` on the current source, and names the task and the command to run; `cw publish` refuses in the same case.

Non-goals. The gate does not try to stop:

- **Tampering with the task store.** The agent can write, rewrite or corrupt any record. The revision hash chain is plain SHA-256 with no key, so a rewritten chain validates, and a record that does not load is skipped instead of gating.
- **A rebuilt toolchain, or a C compiler that skips the tests.** A check whose program is `go` runs only a go command whose build information names `cmd/go`, with its own tree as `GOROOT`, so a script named `go` first on `PATH`, or a `GOROOT` of links to the real tree with its own `go.env`, is refused ([which go a check runs](docs/operations.md#which-go-a-check-runs)); `cw task verify` reads the Go rule's package graph and `-race` probe from a go command it verifies the same way. That shows what the binary is, not that it reports honestly: the same user can rebuild `cmd/go` from modified source, or keep the binary and edit the standard library or tools in a GOROOT it can write. That is a limit of the design. A check that runs another program, such as `make`, runs whatever `PATH` names. The Go evidence rule credits a `go test` run only under the configuration that its verified go command reported with `go env` just before the run, which the run then gets in its environment, where a go env file cannot change it: the `GOFLAGS` it applied, from the environment or a go env file, must pass the flag allowlist, and its `GOOS`/`GOARCH` must be the go command's host's, since `go test` hands a test binary built for another platform to a `go_<goos>_<goarch>_exec` program on `PATH`. Known gap, open until a later fix: `cw` does not verify the C compiler. `go test -race` and cgo builds run the compiler that `CC`, `go env -w CC` or a `gcc` first on `PATH` chooses, and one that makes the test binary report a pass without running a test earns full credit.
- **Containing checks.** A check runs with the user's rights. `cw task start` refuses some programs by name (shells, `env`, `sudo`, network and deployment tools, git other than inspection commands, `cw publish`), but `make`, `python3 -c`, `go run` or a script can still do anything, including commit and push. The refusal guards against accidents; it is not a sandbox. So does the environment filter: a check does not see variables whose names look like credentials unless its `env_passthrough` names them, which the plan records and verify lists, but it can read whatever files the user can ([check environment](docs/operations.md#what-a-check-runs-with)).
- **Self-declared agent IDs.** Worker independence compares the `agent_id` and `coordinator_id` strings written into briefs and the plan. Nothing authenticates them, so one agent can record every role.
- **`cw task abandon` run by the agent.** Any process that can run `cw` can abandon a task, and an abandoned task never gates. The hook asks the user before a `cw task abandon` it can see in a shell command (under Codex it denies it), but not one run from a script, `make` or a plan check.
- **Owner approval is text the agent supplies.** `cw task assign` and `cw task run --owner-approval` record whatever words they are given; the hook asks a person to confirm them on Claude Code only (under Codex it denies the command), and not for a command run from a script, `make` or a plan check.
- **Git run by scripts and other programs.** The hook reads the text of a Bash command, so git run by a script, a Makefile, another language, an alias or a command form the hook does not parse goes unchecked, unless `cw gate install` wrote git's own pre-commit, pre-push and pre-merge-commit hooks for the repository ([git hooks](docs/hooks.md#installing-the-git-hooks)); a commit made through the GitHub API goes unchecked either way. Codex runs the hook only after the user trusts it; until then only `cw publish` applies the gate there.

[Known bypasses](docs/hooks.md#known-bypasses) lists the known ways around the gate today. None of them is a vulnerability, and neither is a new bypass that needs the agent to set out to defeat the gate. If the hook misses an everyday command that a cooperative agent writes, report it as an ordinary bug in a public issue, not through a security advisory.

## Credentials and `cw consult`

`cw consult` sends evidence excerpts to third-party adviser APIs (TypeSafe, SubQ, OpenAI). It does this only when a provider is enabled, its key is set in the environment or saved with `cw keys set`, and you opted in to it: by saving its key with `cw keys set`, by listing it under `advisers.enabled` in your preferences, or by passing `--policy`. A key in the environment alone, such as an `OPENAI_API_KEY` in your shell profile, binds no adviser to a new task and is not used by an ad hoc consultation; `cw doctor` reports it as "key present, adviser not enabled". A task keeps the binding recorded at `cw task start`. A request that carries a task context is sent to that context's advisers only when the task's record, in the task store the hook reads, holds that exact context, so a request cannot name advisers its task did not bind. A task started before this rule existed still keeps sending to the advisers it bound. Without keys, nothing is sent. Saved keys are stored in `~/.config/agent-workflows` (or `$XDG_CONFIG_HOME/agent-workflows`, or `--config-dir`). Treat that directory as secret, and review what evidence you share before running a consultation.

## Supported versions

Only the newest v3 release, release candidates included, receives security fixes. v2.x is end-of-life and receives no fixes; v3 is a fresh install with no upgrade path from it.
