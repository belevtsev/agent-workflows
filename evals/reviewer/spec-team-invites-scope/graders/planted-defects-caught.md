---
type: llm
focus: { source: file, path: artefacts/spec-review.md }
---

The change folder has two planted defects. (1) Scope creep: the requirement
`Invite reminder email` (a reminder 48 hours before expiry), its scenario,
task 2.1 and the last What Changes bullet in `proposal.md` come from no
source; TEAM-210's acceptance criteria 1 to 4 never mention reminders.
(2) Missing failure mode: TEAM-210 criterion 1 says "Only team admins can
send invites", but `Admin sends an invite` has only the admin success
scenario and no scenario in which a non-admin (for example a plain `member`
of the team) tries to invite. TEAM-210 does not give the status code for
that case.

PASS if the review has one entry per finding, each with an ID (`S1`,
`S2`, ...), a severity of `blocker`, `major` or `minor`, a category, a
location, evidence and what would resolve it; one finding has category
"scope creep", locates `Invite reminder email` or task 2.1 (ideally both),
and cites as evidence that TEAM-210 asks for no reminder (quoting or
listing its criteria), resolving it by removing the requirement and task or
getting a source for them; another finding has category "missing failure
mode", severity `blocker` or `major`, locates `Admin sends an invite`, cites
TEAM-210 criterion 1, and asks for a scenario in which a non-admin's invite
is refused with no invite created or emailed, with the response code taken
from a source or raised as an open question rather than invented; every
finding cites evidence; and the review ends with the sources read and what
was not reviewed.
FAIL if either planted defect is missing or filed under a different
category, the missing-failure-mode finding is rated `minor`, a finding
lacks evidence, the review invents the non-admin status code as if the
source gave it, or the review raises a preference without evidence as a
finding.
