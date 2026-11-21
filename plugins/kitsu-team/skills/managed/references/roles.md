# Role settings and supervised runs

Read this when the developer asks to change role settings or explicitly requests a supervised run.

## Change role settings

When the developer asks to change role settings, work in the Git worktree. If `kitsu-roles.toml` is absent, run `kitsu role init --json` to use their saved install host. Use `--host codex|claude` at initialization when they supply a different default. Keep existing version 1 files until the developer chooses to migrate. Run direct commands such as `kitsu role set reviewer --host claude --model opus --reasoning high --json`, supplying only the requested fields, then show `kitsu role list`. These commands update the repository's role settings for new tasks. Keep personal settings and active assignments out of this edit.

## Supervised runs

For an explicitly requested supervised run, use `kitsu task run REQUEST --until OUTCOME --json`. Resume in the original worktree with `kitsu task run --resume ID --json`. Kitsu pins version 2 role settings or the installed default and delegates to the administrator-registered runner. The runner supplies signed outcomes from isolated jobs. If setup is missing or the attempt is unresolved, report the diagnostic and consult the runner operator. Keep using the same saved task on resume.
