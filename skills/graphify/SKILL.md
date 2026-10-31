---
name: graphify
description: Explore tracked Go source with an optional local structural graph when package relationships or candidate related symbols would help a repository investigation.
---

# Graphify

Use Graphify for a bounded structural view of Go source across a repository or
its modules. Known paths and symbols still use direct reads or `rg`; unfamiliar
behavior starts with the repository's focused `jbcontext search` convention when
available. Add a graph when relationships would help narrow that investigation.
An unavailable graph must not stop useful source discovery.

## Build and query

Resolve this skill's directory from the skill catalog. Run
`scripts/graphify_runner.py` with the dedicated Graphify environment's Python and
`-B`. The guarded runner supports macOS and Linux; the optional external runtime
must already be provisioned for this device.
If it is missing or incompatible, report that limitation and continue with source
reads, `rg`, semantic search, or available Go tooling. Do not install dependencies
as an incidental discovery step.

For an installed skill, the guarded interface is:

```sh
~/.venvs/agent-graphify/bin/python -B ~/.agents/skills/graphify/scripts/graphify_runner.py build --repo /path/to/repo
~/.venvs/agent-graphify/bin/python -B ~/.agents/skills/graphify/scripts/graphify_runner.py query --repo /path/to/repo 'Which packages and symbols relate to the installation transaction?' --budget 2000
```

Use the actual catalog path if the skill is registered elsewhere. Build the
snapshot for the intended worktree, then ask a focused structural question.
Prefer a narrow relationship question after locating candidate symbols. Broad
prose can produce noisy, truncated context. Treat omitted relationships as
unknown and use source discovery rather than automatically increasing the budget.
The runner snapshots eligible tracked Go source and module metadata, excluding
vendored and fixture trees such as `third_party`, `vendor`, `testdata`, and
`fixtures`. Untracked source is outside this graph's coverage.

The runner fixes local code-only extraction and disables LLM deduplication,
automatic refresh, and query logging. It launches the external CLI with a minimal
environment and temporary `HOME` and XDG configuration/cache directories so the
child does not load the user's provider configuration. Use this interface rather
than adding upstream modes, hooks, watch processes, cloud services, or MCP servers.

Caches belong outside the repository and immutable skill release. Their identity
includes the worktree, HEAD, tracked source content, extraction scope, and tool.
Cache child components and entries must not redirect through symlinks. Queries
refuse missing, stale, or incomplete snapshots. Rebuild after relevant tracked
source or tool changes; do not treat an older cache as current evidence.

## Verify the map against source

Treat extracted relationships and `INFERRED` relationships as qualified discovery
hints. Even an `EXTRACTED` call edge can target an unrelated same-named function;
check the actual call expression and wiring before adopting it. The graph is an
undirected structural map. It does not prove runtime call direction, resolved
interface implementations, dependency-injection targets,
or reachability. Follow candidate paths into current source and use available
`gopls` or other Go tooling to resolve symbols and callers when those claims matter.

Ground the answer in verified file and line references. State graph coverage and
unresolved relationships when material, including excluded or untracked code and
dynamic wiring the map cannot establish. A structural graph does not replace
tests, runtime evidence, or required review and consultation checkpoints. For
substantial work, preserve the required Jev, SubQ Snap, and OpenAI Decisions
checkpoints; Codex makes the decision from source evidence and verification.
