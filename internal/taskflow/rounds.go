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

// roundsUsed counts the author rounds each capped stage has used:
// its assignments, except one that repeats an earlier assignment (a
// replacement for an exposed worker, or a correction round verify requires)
// and one whose latest acceptance closed it as unused. For spec and design
// the first such assignment is the first spec or design, not a revision, so
// it does not count; for tests the first dispatch counts.
func roundsUsed(record Record) map[string]int {
	used := map[string]int{}
	for _, assigned := range record.Assignments {
		stage := assigned.Brief.Stage
		if !slices.Contains(countedStages, stage) || assigned.Brief.Repeats != "" {
			continue
		}
		if accepted := latestAcceptance(record, assigned.Brief.ID); accepted != nil && accepted.Acceptance.Status == "unused" {
			continue
		}
		used[stage]++
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
		// Within the cap, or a brief that is no round, such as a repeat.
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

// OwnerApproval records the owner's words that let an operation go past a
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
var approvalLimits = []string{ApprovalRoundBudget}

// maxApprovalText bounds the owner's words, in bytes.
const maxApprovalText = 1024

// validApprovalText reports whether text can be the owner's words: not
// blank, at most maxApprovalText bytes of UTF-8, without control characters.
func validApprovalText(text string) bool {
	return nonblank(text) && len(text) <= maxApprovalText && utf8.ValidString(text) && !strings.ContainsFunc(text, unicode.IsControl)
}

// validateStoredApproval checks a stored assignment's approval: the owner's
// words, a known limit, and a time between the task's start and the
// assignment.
func validateStoredApproval(record Record, assigned WorkerAssignment) error {
	approval := assigned.OwnerApproval
	if approval == nil {
		return nil
	}
	if !validApprovalText(approval.Text) || !slices.Contains(approvalLimits, approval.For) || approval.At.Before(record.CreatedAt) || approval.At.After(assigned.AssignedAt) {
		return errors.New("stored worker assignment owner approval needs bounded text, a known limit and a time within the task before the assignment")
	}
	return nil
}

// checkRoundApproval checks an assignment's approval against the round
// budget the record held before it: a round past the cap needs one, and any
// other assignment carries none.
func checkRoundApproval(before Record, brief WorkerBrief, approval *OwnerApproval) error {
	over := overRoundBudget(before, brief)
	switch {
	case over != nil && (approval == nil || approval.For != ApprovalRoundBudget):
		return kindError{kind: ErrOverBudget, msg: over.Error(), cause: *over}
	case over == nil && approval != nil:
		return invalidf("worker brief %s goes past no limit of the task, so it takes no owner approval", brief.ID)
	}
	return nil
}
