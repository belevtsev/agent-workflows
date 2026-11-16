# Reviews: add-agent-onboarding

Targeted independent review covered local readiness evidence, installed-bundle layouts,
literal launch arguments, terminals and exit handling, foreground process ownership,
permission/trust boundaries, session context, and missing-CLI bootstrap.

An isolated reproduction found that Codex's effective features.plugins=false was ignored:
agent check returned ready:true despite disabled plugin support. The fix honors that feature
through configuration precedence and has regression coverage. Re-review rebuilt the same
fixture binary and observed exit 5 with ready:false and an explicit disabled-plugin finding.

A suspected Claude skillOverrides problem was checked against the current
[official skill guide](https://code.claude.com/docs/en/skills#evaluate-and-iterate) and existing
plugin-scope policy: plugin skills are exempt, so the observed ready:true
result is correct. A regression now records that distinction. No additional actionable issue
was found in this targeted scope. Fixtures use actual repository bundles with isolated local
settings and fake host executables; they prove no real host trust or model execution.

No complete-stage review or human approval has been recorded. The formal tables remain empty.

## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |

## Findings

| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- |

## Approvals

| What | Date | Content |
| --- | --- | --- |
