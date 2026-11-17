# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Frozen plan | Pin inputs and assign paired run identities | Public source archives and audited prompts | yes | Existing Go hashing and Git snapshot interfaces |
| Isolated solver | Execute the unchanged small-task workflow | Frozen plan, Codex CLI, API bridge | yes | Existing Kitsu task skill, roles, checks and gate |
| Independent grader | Evaluate frozen behavioral and regression checks | Solver candidate and private audited tests | yes | Existing test runners and Linux namespaces |
| Usage and report | Account for all descendants and compare paired outcomes | Solver traces and independent grader | yes | Existing Codex token reader |

## Rules

| Rule | Checked by |
| --- | --- |
| Ordinary tests never invoke paid model work | test |
| Frozen inputs and paired identities cannot silently change | test |
| Solver cannot read grading assets or fetch remote fixes | test |
| Independent grading cannot be replaced by candidate tests | test |
| Cumulative session usage is counted once and missing sessions remain unknown | test |
| Semantic workflow compliance and code quality require independent assessment | review |

## Execution

Implement explicitly invoked Go test helpers under `internal/pilot`, outside
production execution paths. Freeze content hashes and a seeded paired schedule
before measured trials. Copy only the selected pristine source into each fresh
Git repository and configure the same approved checks and Codex role policy.

Linux namespaces isolate solver files, processes and network. A Go CONNECT
bridge permits model-service traffic while denying solution fetches. A private
grader reconstructs candidates and restores independent test inputs outside the
solver namespace. Keep complete raw observations and session rollouts private.

Use unique session IDs and parent links to account for all solver descendants.
Corroborate actual models and reasoning from turn contexts. Do not add stdout
usage to rollout-owned usage. Separate functional results from workflow
compliance and infrastructure. Report paired outcomes and uncertainty at the
task-cluster level; avoid claims of universal noninferiority.
