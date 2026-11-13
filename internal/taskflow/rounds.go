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

// ApprovalEscalation is OwnerApproval.For of a fix round past the fix
// rounds its profile allows on one finding or requirement that is still
// open.
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

// overLimit is one limit of the task's profile that a brief goes past, with
// its refusal, which matches ErrOverBudget.
type overLimit struct {
	For     string
	refusal error
}

// limitsOver returns every limit of the record's profile that assigning
// brief at snapshot would go past: the stage's round cap, and the fix rounds
// of a finding or requirement still open (escalated).
func limitsOver(record Record, snapshot projectctx.Snapshot, brief WorkerBrief) []overLimit {
	var over []overLimit
	if round := overRoundBudget(record, brief); round != nil {
		over = append(over, overLimit{For: ApprovalRoundBudget, refusal: kindError{kind: ErrOverBudget, msg: round.Error(), cause: *round}})
	}
	if escalation := escalated(record, snapshot, brief); escalation != nil {
		over = append(over, overLimit{For: ApprovalEscalation, refusal: kindError{kind: ErrOverBudget, msg: escalation.Error(), cause: *escalation}})
	}
	return over
}

// ownerApprovals turns each --owner-approval value into the approval of one
// limit the brief goes past. A value "LIMIT=TEXT", LIMIT a known limit,
// names its limit; any other value is the words for the one limit the brief
// goes past, and is refused when it goes past several. Words for a limit it
// does not go past, or twice for one limit, are refused as invalid input; a
// limit without words is refused with its own refusal, or, for several,
// with one that names them all.
func ownerApprovals(over []overLimit, briefID string, words []string) ([]OwnerApproval, error) {
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
			return nil, invalidf("worker brief %s goes past %d limits (%s); give the owner's words for each as --owner-approval LIMIT=TEXT", briefID, len(over), strings.Join(names, ", "))
		}
		if !slices.Contains(names, limit) {
			if limit == "" {
				return nil, invalidf("worker brief %s goes past no limit of the task, so it takes no owner approval", briefID)
			}
			return nil, invalidf("worker brief %s does not go past %s, so it takes no owner approval for it", briefID, limit)
		}
		if slices.ContainsFunc(approvals, func(a OwnerApproval) bool { return a.For == limit }) {
			return nil, invalidf("worker brief %s has the owner's approval for %s twice", briefID, limit)
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
	return nil, kindError{kind: ErrOverBudget, msg: fmt.Sprintf("worker brief %s goes past %s; each needs the owner's approval as --owner-approval LIMIT=TEXT", briefID, strings.Join(missingNames, " and ")), cause: errors.Join(missing...)}
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
// exactly the limits its brief went past in the record before it, at the
// snapshot the assignment bound.
func approvalsMatch(before Record, snapshot projectctx.Snapshot, brief WorkerBrief, approvals []OwnerApproval) bool {
	over := limitsOver(before, snapshot, brief)
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

// fixesOf returns the fixes each assignment of record answers as a fix
// round, and the assignments that count as one, as roundsUsed counts rounds:
// a brief with fixes counts unless it returned no work (returnedNoWork); a
// repeat (which carries no fixes) of one that counts is no new round, and a
// repeat of one that does not count takes its place and its fixes.
func fixesOf(record Record) (fixes map[string][]FixRef, counts map[string]bool) {
	fixes, counts = map[string][]FixRef{}, map[string]bool{}
	covered := map[string]bool{} // the assignment counts, or replaces one that does
	for _, assigned := range record.Assignments {
		brief := assigned.Brief
		fixes[brief.ID] = brief.Fixes
		if brief.Repeats != "" {
			if covered[brief.Repeats] {
				covered[brief.ID] = true
				continue
			}
			fixes[brief.ID] = fixes[brief.Repeats]
		}
		if len(fixes[brief.ID]) == 0 || returnedNoWork(record, brief.ID) {
			continue
		}
		covered[brief.ID], counts[brief.ID] = true, true
	}
	return fixes, counts
}

// fixAssignments lists, per key, the assignments that count as fix rounds
// on it (fixesOf), in order; a brief that fixes two findings of one chain
// counts once.
func fixAssignments(record Record) map[FixRef][]string {
	fixes, counts := fixesOf(record)
	assignments := map[FixRef][]string{}
	for _, assigned := range record.Assignments {
		if !counts[assigned.Brief.ID] {
			continue
		}
		seen := map[FixRef]bool{}
		for _, fix := range fixes[assigned.Brief.ID] {
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
// snapshot itself (bindChecks): an assignment stores no source digest, so a
// run reused at another snapshot does not close it, and load judges the
// same way.
func stillOpen(record Record, snapshot projectctx.Snapshot, fix FixRef) bool {
	if fix.Requirement != "" {
		at := slices.IndexFunc(record.Plan.Requirements, func(r Requirement) bool { return r.ID == fix.Requirement })
		if at < 0 {
			return false
		}
		_, err := bindChecks(record, recordedCredit(snapshot, ""), record.Plan.Requirements[at].CheckIDs)
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
// no new fix round, have no limit; a repeat that takes the place of a round
// that returned no work answers that round's fixes.
func escalated(record Record, snapshot projectctx.Snapshot, brief WorkerBrief) *ErrEscalation {
	if record.Profile == nil {
		return nil
	}
	next := record
	next.Assignments = append(slices.Clip(record.Assignments), WorkerAssignment{Brief: brief})
	fixes, counts := fixesOf(next)
	if !counts[brief.ID] {
		return nil
	}
	history := fixAssignments(record)
	for _, fix := range fixes[brief.ID] {
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
