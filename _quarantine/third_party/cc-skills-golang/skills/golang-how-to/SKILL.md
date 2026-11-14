---
name: golang-how-to
description: Find the appropriate Go specialist when the needed workflow is unclear, using task boundaries and available tools to select focused guidance.
license: MIT
metadata:
  author: samber
  version: 1.4.0
  openclaw:
    emoji: 🧭
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - gopls
    install:
    - kind: go
      package: golang.org/x/tools/gopls@latest
      bins:
      - gopls
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness. Requires git.
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(git:*) Agent AskUserQuestion LSP Bash(gopls:*) mcp__gopls__*
---

# Go skill discovery

Use this entrypoint when the relevant Go workflow is unclear or two specialists appear to overlap. An ordinary edit can proceed directly with the repository instructions and the relevant specialist, if any. Select guidance for the requested outcome; a related topic or import alone does not require loading another skill.

- For competing workflow boundaries, read [disambiguation](references/disambiguation.md).
- For the broader catalog, read [skills by category](references/by-category.md).
- For choosing local code intelligence, published package documentation, or vulnerability analysis, read [tool selection](references/tool-selection.md).
- Only when the user requests agent-configuration work, read [project configuration](references/project-config.md).

Library skills (`golang-samber-*`, `golang-uber-fx`, `golang-uber-dig`, `golang-google-wire`, `golang-swagger`, `golang-graphql`, `golang-spf13-viper`) are not in the automatic skill listing. When one applies, read its `SKILL.md` from the sibling directory `../<name>/SKILL.md` next to this skill instead of invoking it as a skill.

Keep the selection small. Read a second specialist only when a concrete part of the task needs its expertise. Library skills apply to a dependency already in use or an explicitly requested evaluation. No routing action changes project configuration, installs a tool, or creates an external issue.
