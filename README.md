# cw

`cw` is becoming a small tool for any git repository, in any language:

- **A gate.** A commit or a push is allowed only when every check the
  repository declares in `cw.toml` has a recorded passing run on exactly that
  content. Git hooks enforce it, for people and for coding agents alike.
- **Roles.** A task goes from a specification to tested code through seven
  roles, each with one job, its own model and a limit on what it may change.
- **A token count.** `cw tokens` says what a task cost, role by role, and how
  much of it the workflow itself used.

## State

This branch is work in progress. No release of it exists, and nothing here
has been measured to improve anything.

- What is being built, and why: [the production plan](openspec/production-plan.md).
- The previous workflow (skills, agents, task records) is kept on the branch
  `workflow-v3`. On this branch its files are under `_quarantine/`, as reading
  material; nothing there is built or tested.

## Build

```sh
go build -o cw ./cmd/cw
./cw version
```

See [development](docs/development.md) for the checks.

## License

MIT, see [LICENSE](LICENSE). Third-party code: [THIRD_PARTY.md](THIRD_PARTY.md).
