package taskflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
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
		if stage := assigned.Brief.Stage; slices.Contains(countedStages, stage) && isRound(record, assigned) {
			used[stage]++
		}
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

// isRound reports whether assigned is a round: it repeats no earlier
// assignment (a replacement for an exposed worker, or a correction round
// verify requires), and its latest acceptance did not close it as unused.
func isRound(record Record, assigned WorkerAssignment) bool {
	if assigned.Brief.Repeats != "" {
		return false
	}
	accepted := latestAcceptance(record, assigned.Brief.ID)
	return accepted == nil || accepted.Acceptance.Status != "unused"
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

// ApprovalEscalation is OwnerApproval.For of a fix round past the fix
// rounds its profile allows on one finding or requirement that is still
// open. It also covers the stage's round cap when the brief passes both.
const ApprovalEscalation = "escalation"

// approvalLimits are the limits an OwnerApproval may name.
var approvalLimits = []string{ApprovalRoundBudget, ApprovalEscalation}

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

// passedLimit returns the limit of the record's profile that assigning brief
// at snapshot would pass, as OwnerApproval.For names it, with its refusal
// typed ErrOverBudget; it returns "" and nil when the brief passes none. An
// escalation comes first: approving it approves the round too.
func passedLimit(record Record, snapshot projectctx.Snapshot, brief WorkerBrief) (string, error) {
	if escalation := escalated(record, snapshot, brief); escalation != nil {
		return ApprovalEscalation, kindError{kind: ErrOverBudget, msg: escalation.Error(), cause: *escalation}
	}
	if over := overRoundBudget(record, brief); over != nil {
		return ApprovalRoundBudget, kindError{kind: ErrOverBudget, msg: over.Error(), cause: *over}
	}
	return "", nil
}

// checkApproval checks an assignment's approval against the limits the
// record held before it, at the assignment's snapshot: a brief that passes a
// limit needs an approval for that limit, and any other carries none.
func checkApproval(before Record, snapshot projectctx.Snapshot, brief WorkerBrief, approval *OwnerApproval) error {
	limit, refusal := passedLimit(before, snapshot, brief)
	switch {
	case refusal != nil && (approval == nil || approval.For != limit):
		return refusal
	case refusal == nil && approval != nil:
		return invalidf("worker brief %s goes past no limit of the task, so it takes no owner approval", brief.ID)
	}
	return nil
}

// FixRound is one earlier fix round on a finding or requirement: its
// assignment and the first line of that assignment's result summary.
type FixRound struct {
	Assignment string
	Summary    string
}

// ErrEscalation is the refusal of a fix round on a finding or requirement
// that is still open after the fix rounds its profile allows. It matches
// ErrOverBudget.
type ErrEscalation struct {
	// Key is the finding or requirement; a finding is the first of its
	// reopens chain.
	Key     FixRef
	Profile string
	// Rounds counts the earlier fix rounds on Key, which History lists.
	Rounds  int
	History []FixRound
}

func (e ErrEscalation) Error() string {
	key := "requirement " + e.Key.Requirement
	if e.Key.Finding != "" {
		key = "finding " + e.Key.Finding
	}
	return fmt.Sprintf("%s is still open after %d fix rounds, the most profile %s allows; the owner chooses between approving one more fix round and changing the approach", key, e.Rounds, e.Profile)
}

// findingAt returns the finding that key ("<assignment id>/<finding id>")
// names in results, or nil.
func findingAt(results []BoundWorkerResult, key string) *WorkerFinding {
	assignment, id, ok := strings.Cut(key, "/")
	if !ok {
		return nil
	}
	for i := range results {
		if results[i].Result.AssignmentID != assignment {
			continue
		}
		for j := range results[i].Result.Findings {
			if results[i].Result.Findings[j].ID == id {
				return &results[i].Result.Findings[j]
			}
		}
	}
	return nil
}

// fixKey is the key fix counts under: a finding follows its reopens chain to
// the first finding, so a finding filed again counts as the earlier one.
func fixKey(record Record, fix FixRef) FixRef {
	if fix.Finding == "" {
		return fix
	}
	key := fix.Finding
	// Each reopens names a finding of an earlier result (validateReopens),
	// so the chain ends within one step per result.
	for range len(record.WorkerResults) {
		finding := findingAt(record.WorkerResults, key)
		if finding == nil || finding.Reopens == "" {
			break
		}
		key = finding.Reopens
	}
	return FixRef{Finding: key}
}

// fixAssignments lists, per key, the assignments that are rounds (isRound)
// and fix it, in order; a brief that fixes two findings of one chain counts
// once.
func fixAssignments(record Record) map[FixRef][]string {
	assignments := map[FixRef][]string{}
	for _, assigned := range record.Assignments {
		if !isRound(record, assigned) {
			continue
		}
		seen := map[FixRef]bool{}
		for _, fix := range assigned.Brief.Fixes {
			if key := fixKey(record, fix); !seen[key] {
				seen[key] = true
				assignments[key] = append(assignments[key], assigned.Brief.ID)
			}
		}
	}
	return assignments
}

// fixRounds counts the earlier fix rounds on each finding or requirement.
func fixRounds(record Record) map[FixRef]int {
	rounds := map[FixRef]int{}
	for key, ids := range fixAssignments(record) {
		rounds[key] = len(ids)
	}
	return rounds
}

// stillOpen reports whether fix is still open at snapshot. A finding is open
// until the latest acceptance of its result disposes it as other than
// accepted (acknowledged but unresolved): one without a disposition yet is
// open. A requirement is open while one of its checks lacks a passed run at
// snapshot (bindChecks).
func stillOpen(record Record, snapshot projectctx.Snapshot, fix FixRef) bool {
	if fix.Requirement != "" {
		at := slices.IndexFunc(record.Plan.Requirements, func(r Requirement) bool { return r.ID == fix.Requirement })
		if at < 0 {
			return false
		}
		_, err := bindChecks(record, snapshot, record.Plan.Requirements[at].CheckIDs)
		return err != nil
	}
	assignment, id, _ := strings.Cut(fix.Finding, "/")
	accepted := latestAcceptance(record, assignment)
	if accepted == nil {
		return true
	}
	at := slices.IndexFunc(accepted.Acceptance.Findings, func(d FindingDisposition) bool { return d.FindingID == id })
	return at < 0 || accepted.Acceptance.Findings[at].Status == "accepted"
}

// escalated returns the refusal of brief when it fixes a finding or
// requirement that is still open after the fix rounds the record's profile
// allows, and nil otherwise. A record without a profile, and a brief that is
// no round, have no limit.
func escalated(record Record, snapshot projectctx.Snapshot, brief WorkerBrief) *ErrEscalation {
	if record.Profile == nil || !isRound(record, WorkerAssignment{Brief: brief}) {
		return nil
	}
	history := fixAssignments(record)
	for _, fix := range brief.Fixes {
		key := fixKey(record, fix)
		ids := history[key]
		if len(ids) < record.Profile.FixRounds || !stillOpen(record, snapshot, fix) {
			continue
		}
		escalation := &ErrEscalation{Key: key, Profile: record.Profile.Name, Rounds: len(ids)}
		for _, id := range ids {
			summary := "no result yet"
			if result := findWorkerResult(record, id); result != nil {
				summary, _, _ = strings.Cut(result.Result.Summary, "\n")
			}
			escalation.History = append(escalation.History, FixRound{Assignment: id, Summary: summary})
		}
		return escalation
	}
	return nil
}

// validateReopens checks each finding's reopens against the results recorded
// before result (earlier): it names a finding of one of them, so never the
// finding itself.
func validateReopens(earlier []BoundWorkerResult, result WorkerResult) error {
	for _, finding := range result.Findings {
		if finding.Reopens == "" {
			continue
		}
		if finding.Reopens == result.AssignmentID+"/"+finding.ID {
			return fmt.Errorf("worker finding %s reopens itself; reopens names an earlier finding", finding.ID)
		}
		if findingAt(earlier, finding.Reopens) == nil {
			return fmt.Errorf("worker finding %s reopens %s, which no earlier worker result holds; write it as <assignment id>/<finding id>", finding.ID, finding.Reopens)
		}
	}
	return nil
}
