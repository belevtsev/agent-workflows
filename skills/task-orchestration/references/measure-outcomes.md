# Measure outcomes

Read this only when the user asks to measure tokens, cost or outcomes with the
experimental `cw metrics` commands.

```sh
cw metrics schema
cw metrics summarize --input-root /private/path/observations run-a.json run-b.json
cw metrics import --claude-transcript ~/.claude/projects/PROJECT/SESSION.jsonl [--prices prices.json]
cw metrics import --codex-session ~/.codex/sessions/YYYY/MM/DD/rollout-ID.jsonl --prices prices.json
cw metrics import --claude-transcript SESSION.jsonl --prices prices.json --composition [--summary]
cw metrics import --claude-transcript SESSION.jsonl --append [--history FILE]
cw metrics history [--history FILE] [--session ID] [--model M] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--prices prices.json]
```

`import` reads a session read-only and reports each agent (the main session and
every subagent or child rollout) with requests, uncached input, cache writes (5-minute
and 1-hour), cache reads, output and thinking tokens. Claude dollars are the
transcript's recorded `cost-state`, per model and for the session, never allocated to
agents; a model that `cost-state` lists but no transcript entry carries, such as
background calls, is reported as unattributed. Codex rollouts record no cost. `--prices` adds
per-agent estimates from a supplied table
(`{"currency","evidence","models":{"MODEL":{"input_per_million","output_per_million","cache_write_5m_per_million","cache_write_1h_per_million","cache_read_per_million"}}}`),
reported separately from recorded dollars; no price is built in. The report's
`usage` (and `cost`, when recorded) can be copied into an observation; there
`input_tokens` stays total input, and the optional breakdown counters must add up to
it. When a model's transcript and recorded totals differ, for example a `cost-state`
written before the session ended, the report shows the difference and leaves
`usage` and `cost` out.

`--append` (with `--claude-transcript` or `--codex-session`) keeps the counts after
the transcript is deleted. It appends one record per de-duplicated request to the cost history,
`<state>/metrics/history.jsonl` unless `--history` names another file, and prints what
it appended instead of the report. A record holds token counts, IDs, timestamps, the
model and agent labels, and no dollars. Requests already stored are skipped, so
importing again never double-counts. `--append` rejects `--prices`, `--composition`
and `--summary`. `cw metrics history` aggregates the history by model,
UTC day and session, and recomputes dollars from `--prices`.
[Cost history](../../../docs/operations.md#cost-history) covers the schema,
locking and damaged lines.

`--composition` (it needs `--prices`) adds where each agent's input came from. Each
agent is mapped to a role from the `cw task assign` brief whose assignment id its Agent
prompt names, else from its Agent description or agent type; unmapped agents count as
`other`, and the role coverage is reported.
Each request's new input, its total input minus the previous request's, is assigned
to the blocks appended in between: the previous output gets its output tokens
(all of the new input when that is smaller), and the rest is split by byte share across tool output (by tool), `cw`
command output, Skill loads, reference reads, Agent handoffs, user turns and other
context. New input with no visible block is `unrecorded`. The first request is split
into its visible blocks, estimated at the agent's measured bytes per token, and
`system_and_tools`, the remainder. That remainder, the skill listing and the
CLAUDE.md/AGENTS.md instructions form the fixed prefix. Per source the report gives
input tokens, input share, input dollars and re-read cost (tokens × later requests ×
cache-read rate). Input dollars allocate each request's priced input in context order:
cache reads first, then cache writes, then uncached input. It also lists repeated
identical tool calls (same tool and input hash). For an agent that ran
`cw task start`, `cw task consult`, a review `cw task assign` or `cw task verify` (as
commands, including after `if`, `do`, `env`, `nohup`, `sudo` or `timeout`; not text in
a heredoc, a comment or a quoted string, nor history copied into a resumed session), it
gives the stale-phase re-read: earlier-phase blocks, other than the initial context,
re-read in a later phase, beside an estimate for re-sending the initial context at
each boundary. The composition is reported only when the price-table total is within
1% of the recorded `cost-state` and the transcript has no tokens the recorded cost
lacks; otherwise it is withheld with a reason. Codex rollouts record no cost, so
their composition is reported as unsupported. `--summary` prints a short table
instead of JSON.

Use the emitted version-one schema for measured observations. Include project and
workload identity, source revision/fingerprint, environment, evidence and the
observation scope. Record real duration, usage and quality outcomes when available;
leave missing values absent. Baseline/candidate comparison requires comparable
cases and explicitly reports missing pairs and source/environment differences.

Synthetic acceptance cases establish those boundaries. Actual project diagnostics
establish the checked environments. Productivity, defect reduction and cost savings
require representative comparable observations; neither model agreement nor a
passing small fixture suite establishes those outcomes. Keep private reports and
provider content out of repositories and public releases.
