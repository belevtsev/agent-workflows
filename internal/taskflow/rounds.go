package taskflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// countedStages are the author stages whose rounds a profile caps, as
// worker-handoffs.md "Round budget" defines them. Implementation briefs are
// implementer fixes, which never count against this budget.
var countedStages = []string{StageSpec, StageDesign, StageTests}

// roundsUsed counts the author rounds each capped stage has used, as
// worker-handoffs.md "Round budget" defines them:
//
//   - The briefs of a stage with neither fixes nor repeats were assigned
//     before any brief of the stage returned (validateBriefStage), so
//     together they are one round, the stage's first.
//   - Each brief with fixes is a round: a return of findings, a fresh worker
//     for a finding, or a refreshed brief after an amendment.
//   - A brief that repeats an assignment that counts, or that a repeat of
//     one replaced, is no new round: it replaces an exposed worker or makes
//     a correction round verify requires. A repeat of an assignment that
//     does not count takes its place.
//   - An assignment closed unused whose handoff was not completed returned
//     no work, so it does not count. One whose completed work was closed
//     unused, such as a superseded round, still counts.
//
// For spec and design the first round is the first draft, not a revision,
// so it does not count; for tests the first dispatch counts.
func roundsUsed(record Record) map[string]int {
	used := map[string]int{}
	first := map[string]bool{}        // the assignment belongs to its stage's first round
	covered := map[string]bool{}      // the assignment counts, or replaces one that does
	firstCounted := map[string]bool{} // the stage's first round has counted
	for _, assigned := range record.Assignments {
		brief := assigned.Brief
		if !slices.Contains(countedStages, brief.Stage) {
			continue
		}
		if brief.Repeats != "" {
			if covered[brief.Repeats] {
				covered[brief.ID] = true
				continue
			}
			first[brief.ID] = first[brief.Repeats]
		} else {
			first[brief.ID] = len(brief.Fixes) == 0
		}
		if returnedNoWork(record, brief.ID) {
			continue
		}
		covered[brief.ID] = true
		if first[brief.ID] {
			if firstCounted[brief.Stage] {
				continue
			}
			firstCounted[brief.Stage] = true
		}
		used[brief.Stage]++
	}
	for _, stage := range []string{StageSpec, StageDesign} {
		switch used[stage] {
		case 0:
		case 1:
			delete(used, stage)
		default:
			used[stage]--
		}
	}
	return used
}

// returnedNoWork reports whether the assignment's latest acceptance closed it
// unused and its handoff was not completed, so its work never returned.
func returnedNoWork(record Record, id string) bool {
	accepted := latestAcceptance(record, id)
	result := findWorkerResult(record, id)
	return accepted != nil && accepted.Acceptance.Status == "unused" && result != nil && result.Result.Status != "completed"
}

// overRoundBudget returns the refusal of brief when assigning it would take
// its stage past the cap of the record's profile, and nil otherwise. A record
// without a profile has no cap.
func overRoundBudget(record Record, brief WorkerBrief) *ErrRoundBudget {
	if record.Profile == nil {
		return nil
	}
	limit, capped := record.Profile.Rounds[brief.Stage]
	if !capped {
		return nil
	}
	next := record
	next.Assignments = append(slices.Clip(record.Assignments), WorkerAssignment{Brief: brief})
	used, after := roundsUsed(record)[brief.Stage], roundsUsed(next)[brief.Stage]
	if after <= limit || after == used {
		// Within the cap, or a brief that is no new round.
		return nil
	}
	return &ErrRoundBudget{Stage: brief.Stage, Profile: record.Profile.Name, Used: used, Cap: limit, Needs: roundNeeds(record, brief)}
}

// roundNeeds describes what the refused round would answer: each finding the
// brief fixes, with its severity and summary, and each requirement.
func roundNeeds(record Record, brief WorkerBrief) []string {
	needs := make([]string, 0, len(brief.Fixes))
	for _, fix := range brief.Fixes {
		if fix.Requirement != "" {
			needs = append(needs, "requirement "+fix.Requirement)
			continue
		}
		assignment, id, _ := strings.Cut(fix.Finding, "/")
		line := "finding " + fix.Finding
		if result := findWorkerResult(record, assignment); result != nil {
			if at := slices.IndexFunc(result.Result.Findings, func(f WorkerFinding) bool { return f.ID == id }); at >= 0 {
				finding := result.Result.Findings[at]
				line = fmt.Sprintf("%s (%s): %s", line, finding.severity(), finding.Summary)
			}
		}
		needs = append(needs, line)
	}
	return needs
}

// ErrRoundBudget is the refusal of a brief that would take its stage past
// the author rounds its profile allows. It matches ErrOverBudget.
type ErrRoundBudget struct {
	Stage   string
	Profile string
	// Used counts the rounds the stage has used before the brief; Cap is
	// the profile's cap for it.
	Used, Cap int
	// Needs describes each finding or requirement the round would answer.
	Needs []string
}

func (e ErrRoundBudget) Error() string {
	return fmt.Sprintf("stage %s has used %d of the %d author rounds profile %s allows; another round needs the owner's approval", e.Stage, e.Used, e.Cap, e.Profile)
}

// OwnerApproval records the owner's words that let an operation go past one
// limit of the task's profile; For names the limit. The agent supplies the
// text: cw records it, and on Claude Code the plugin hook asks a person to
// confirm it.
type OwnerApproval struct {
	Text string    `json:"text"`
	For  string    `json:"for"`
	At   time.Time `json:"at"`
}

// ApprovalRoundBudget is OwnerApproval.For of an assignment past its stage's
// round cap.
const ApprovalRoundBudget = "round-budget"

// approvalLimits are the limits an OwnerApproval may name.
var approvalLimits = []string{ApprovalRoundBudget, ApprovalCostBudget}

// maxApprovalText bounds the owner's words, in bytes.
const maxApprovalText = 1024

// validApprovalText reports whether text can be the owner's words: not
// blank, at most maxApprovalText bytes of UTF-8, without control characters.
func validApprovalText(text string) bool {
	return nonblank(text) && len(text) <= maxApprovalText && utf8.ValidString(text) && !strings.ContainsFunc(text, unicode.IsControl)
}

// overLimit is one limit of the task's profile that a brief goes past, with
// its refusal, which matches ErrOverBudget.
type overLimit struct {
	For     string
	refusal error
}

// limitsOver returns every limit of the record's profile that assigning
// brief would go past.
func limitsOver(record Record, brief WorkerBrief) []overLimit {
	var over []overLimit
	if round := overRoundBudget(record, brief); round != nil {
		over = append(over, overLimit{For: ApprovalRoundBudget, refusal: kindError{kind: ErrOverBudget, msg: round.Error(), cause: *round}})
	}
	return over
}

// ownerApprovals turns each --owner-approval value into the approval of one
// limit the brief goes past. A value "LIMIT=TEXT", LIMIT a known limit,
// names its limit; any other value is the words for the one limit the brief
// goes past, and is refused when it goes past several. Words for a limit it
// does not go past, or twice for one limit, are refused as invalid input; a
// limit without words is refused with its own refusal, or, for several,
// with one that names them all. subject names the work in a refusal, such
// as "worker brief ID".
func ownerApprovals(over []overLimit, subject string, words []string) ([]OwnerApproval, error) {
	names := make([]string, 0, len(over))
	for _, limit := range over {
		names = append(names, limit.For)
	}
	var approvals []OwnerApproval
	for _, value := range words {
		limit, text := "", value
		if name, rest, ok := strings.Cut(value, "="); ok && slices.Contains(approvalLimits, name) {
			limit, text = name, rest
		}
		if !validApprovalText(text) {
			return nil, invalidf("owner approval text must be 1..%d bytes without control characters", maxApprovalText)
		}
		switch {
		case limit != "":
		case len(over) == 1:
			limit = over[0].For
		case len(over) > 1:
			return nil, invalidf("%s goes past %d limits (%s); give the owner's words for each as --owner-approval LIMIT=TEXT", subject, len(over), strings.Join(names, ", "))
		}
		if !slices.Contains(names, limit) {
			if limit == "" {
				return nil, invalidf("%s goes past no limit of the task, so it takes no owner approval", subject)
			}
			return nil, invalidf("%s does not go past %s, so it takes no owner approval for it", subject, limit)
		}
		if slices.ContainsFunc(approvals, func(a OwnerApproval) bool { return a.For == limit }) {
			return nil, invalidf("%s has the owner's approval for %s twice", subject, limit)
		}
		approvals = append(approvals, OwnerApproval{Text: text, For: limit})
	}
	var missing []error
	var missingNames []string
	for _, limit := range over {
		if !slices.ContainsFunc(approvals, func(a OwnerApproval) bool { return a.For == limit.For }) {
			missing, missingNames = append(missing, limit.refusal), append(missingNames, limit.For)
		}
	}
	switch len(missing) {
	case 0:
		return approvals, nil
	case 1:
		return nil, missing[0]
	}
	return nil, kindError{kind: ErrOverBudget, msg: fmt.Sprintf("%s goes past %s; each needs the owner's approval as --owner-approval LIMIT=TEXT", subject, strings.Join(missingNames, " and ")), cause: errors.Join(missing...)}
}

// validateStoredApprovals checks a stored assignment's approvals: for each,
// the owner's words, a known limit named once, and a time between the
// task's start and the assignment.
func validateStoredApprovals(record Record, assigned WorkerAssignment) error {
	for i, approval := range assigned.OwnerApprovals {
		if !validApprovalText(approval.Text) || !slices.Contains(approvalLimits, approval.For) || approval.At.Before(record.CreatedAt) || approval.At.After(assigned.AssignedAt) ||
			slices.ContainsFunc(assigned.OwnerApprovals[:i], func(earlier OwnerApproval) bool { return earlier.For == approval.For }) {
			return errors.New("stored worker assignment owner approval needs bounded text, a known limit named once and a time within the task before the assignment")
		}
	}
	return nil
}

// approvalsMatch reports whether a stored assignment's approvals name
// exactly the limits its brief went past in the record before it.
func approvalsMatch(before Record, brief WorkerBrief, approvals []OwnerApproval) bool {
	over := limitsOver(before, brief)
	if len(over) != len(approvals) {
		return false
	}
	for _, limit := range over {
		if !slices.ContainsFunc(approvals, func(a OwnerApproval) bool { return a.For == limit.For }) {
			return false
		}
	}
	return true
}
