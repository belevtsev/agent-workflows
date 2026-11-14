# Project guidance when configuration is requested

Read this reference only for an explicit task to create or revise agent guidance. Skill discovery itself does not modify configuration.

Use the configuration surface named by the user or already used by the project. Preserve unrelated guidance and established invocation policy. A project can use several harnesses; do not edit every configuration file merely because it exists.

| Surface | Format consideration |
| --- | --- |
| `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` | Existing Markdown guidance; update the relevant section in place. |
| `.cursor/rules/*.mdc` | Individual rule files with frontmatter; the directory is not a Markdown file. |
| `.github/copilot-instructions.md` | Repository Markdown guidance for Copilot. |

Keep durable repository facts here: toolchain source, generated-file ownership, required execution environment, and links to maintained workflows. Prefer conditional skill pointers to language-wide activation. For example:

```markdown
Use the repository's documented verification commands and environment.
For changes to goroutine ownership or shutdown, consult the Go concurrency guidance.
For a module upgrade, consult the dependency-management guidance.
```

Only include pointers that name installed skills and actual repository needs. Do not create required-skill lists or change implicit invocation unless the user requested that policy. If migrating an existing blanket-loading rule as part of authorized cleanup, replace it with the concrete repository invariant or conditional workflow it was meant to protect.

The [Cursor example](../assets/cursor-go-skills.mdc) is an optional, conditionally selected template for a requested Cursor setup. Adapt it to the project's chosen policy rather than copying it into every project. Report the exact files changed and what guidance they now provide.
