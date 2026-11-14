package taskflow

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// MaxReceiptBytes bounds a task receipt, as MaxBriefBytes bounds a stage
// brief: its JSON encoding and its text, each with the final newline, are at
// most 4 KiB.
const MaxReceiptBytes = 4 << 10

// maxReceiptReason bounds the bytes of the unknown-usage reason a receipt
// quotes (its first line); maxReceiptSources the dollar sources it names.
const (
	maxReceiptReason  = 400
	maxReceiptSources = 3
)

// TaskReceipt is what a task used and where it stands, built from the record
// alone by Receipt: the profile with its round caps, the fix rounds on each
// open finding or requirement, every owner approval with the owner's words,
// the usage against the budget, the checks, the findings and the wall time.
// It stores nothing. Every listed entry is whole; what does not fit in
// MaxReceiptBytes is counted in Omitted.
type TaskReceipt struct {
	TaskID string `json:"task_id"`
	// Profile is empty for a task started before profiles.
	Profile string `json:"profile"`
	// State is open, complete or incomplete (the stored verification), or
	// abandoned.
	State string `json:"state"`
	// Rounds lists each stage the profile caps, in flow order.
	Rounds   []ReceiptRounds `json:"rounds"`
	Usage    ReceiptUsage    `json:"usage"`
	Checks   ReceiptChecks   `json:"checks"`
	Findings []FindingCount  `json:"findings"`
	WallTime ReceiptWallTime `json:"wall_time"`
	// OpenFindings are the findings still open, newest first; FixRounds the
	// open findings and requirements that took fix rounds; Approvals every
	// owner approval, in time order.
	OpenFindings []ReceiptFinding  `json:"open_findings"`
	FixRounds    []ReceiptFixRound `json:"fix_rounds"`
	Approvals    []ReceiptApproval `json:"approvals"`
	Omitted      ReceiptOmitted    `json:"omitted,omitzero"`
}

// ReceiptRounds is the author rounds a capped stage used and its cap.
type ReceiptRounds struct {
	Stage string `json:"stage"`
	Used  int    `json:"used"`
	Cap   int    `json:"cap"`
}

// ReceiptUsage is the task's usage (UsageOf) against its budget. USD and
// Tokens are nil when unknown, never 0, and UnknownReason then says why, cut
// to its first line of at most 400 bytes. KnownUSD is the known lower bound
// of unknown dollars (knownFloor), set only when above 0. Budget is nil for a
// task without a profile; StopRaised reports the owner's cost-budget approval.
type ReceiptUsage struct {
	USD           *float64       `json:"usd"`
	USDEstimated  bool           `json:"usd_estimated,omitempty"`
	KnownUSD      *float64       `json:"known_usd,omitempty"`
	DollarSources []DollarSource `json:"dollar_sources,omitempty"`
	Tokens        *UsageTokens   `json:"tokens"`
	UnknownReason string         `json:"unknown_reason,omitempty"`
	Budget        *Budget        `json:"budget,omitempty"`
	StopRaised    bool           `json:"stop_raised,omitempty"`
}

// ReceiptChecks counts the planned checks (plan and add-checks), the check
// runs recorded and those of them that did not pass, and the checks the
// stored verification credited by reuse; Reused is nil before a verification.
type ReceiptChecks struct {
	Planned  int  `json:"planned"`
	Executed int  `json:"executed"`
	Failed   int  `json:"failed"`
	Reused   *int `json:"reused"`
}

// FindingCount counts the findings of one severity whose latest disposition
// is Status (pending when none disposes of them).
type FindingCount struct {
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// ReceiptFinding is one open finding, its summary cut to its first line of at
// most 200 bytes.
type ReceiptFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Summary  string `json:"summary"`
}

// ReceiptFixRound is the fix rounds an open finding or requirement took and
// the profile's allowance for it.
type ReceiptFixRound struct {
	Key     string `json:"key"`
	Rounds  int    `json:"rounds"`
	Allowed int    `json:"allowed"`
}

// ReceiptApproval is one owner approval with the owner's words; Assignment
// names the assignment it was given on, and is empty for the task-level
// cost-budget approval.
type ReceiptApproval struct {
	For        string    `json:"for"`
	Text       string    `json:"text"`
	At         time.Time `json:"at"`
	Assignment string    `json:"assignment,omitempty"`
}

// ReceiptWallTime is the record's created_at and updated_at and the time
// between them, as the timeline writes them.
type ReceiptWallTime struct {
	StartedAt string `json:"started_at"`
	UpdatedAt string `json:"updated_at"`
	Elapsed   string `json:"elapsed"`
}

// ReceiptOmitted counts what the receipt does not list for lack of room.
type ReceiptOmitted struct {
	DollarSources int `json:"dollar_sources,omitzero"`
	OpenFindings  int `json:"open_findings,omitzero"`
	FixRounds     int `json:"fix_rounds,omitzero"`
	Approvals     int `json:"approvals,omitzero"`
}

var (
	severityOrder = []string{"blocker", "major", "minor"}
	statusOrder   = []string{"pending", "accepted", "resolved", "deferred", "rejected"}
)

// Receipt builds the receipt of record. It reads the record only. The fixed
// part is bounded; then the open findings, the fix rounds and the approvals
// are added whole, in that order, each within an equal share of the room left
// when it starts, so the open findings come first and none starves the
// others. The open findings stop at the first that does not fit, so every
// unlisted one is older than every listed one.
func Receipt(record Record) TaskReceipt {
	receipt := TaskReceipt{TaskID: record.ID, State: "open", Rounds: []ReceiptRounds{}, Findings: []FindingCount{},
		OpenFindings: []ReceiptFinding{}, FixRounds: []ReceiptFixRound{}, Approvals: []ReceiptApproval{},
		WallTime: ReceiptWallTime{StartedAt: timelineInstant(record.CreatedAt), UpdatedAt: timelineInstant(record.UpdatedAt), Elapsed: timelineDuration(record.CreatedAt, record.UpdatedAt)}}
	switch {
	case record.Abandonment != nil:
		receipt.State = "abandoned"
	case record.Verification != nil && record.Verification.Complete:
		receipt.State = "complete"
	case record.Verification != nil:
		receipt.State = "incomplete"
	}
	if profile := record.Profile; profile != nil {
		receipt.Profile = profile.Name
		used := roundsUsed(record)
		for _, stage := range stageOrder {
			if limit, capped := profile.Rounds[stage]; capped {
				receipt.Rounds = append(receipt.Rounds, ReceiptRounds{Stage: stage, Used: used[stage], Cap: limit})
			}
		}
	}
	receipt.Usage, receipt.Omitted.DollarSources = receiptUsage(record)
	receipt.Checks = receiptChecks(record)
	open := receiptFindings(record, &receipt)

	add := func(limit int, put func(*TaskReceipt)) bool {
		next := receipt
		next.OpenFindings, next.FixRounds, next.Approvals = slices.Clip(next.OpenFindings), slices.Clip(next.FixRounds), slices.Clip(next.Approvals)
		put(&next)
		if next.size() > limit {
			return false
		}
		receipt = next
		return true
	}
	share := func(n int) int {
		size := receipt.size()
		return size + (MaxReceiptBytes-size)/n
	}
	limit := share(3)
	for i, finding := range open {
		if !add(limit, func(r *TaskReceipt) { r.OpenFindings = append(r.OpenFindings, finding) }) {
			receipt.Omitted.OpenFindings = len(open) - i
			break
		}
	}
	limit = share(2)
	for _, round := range receiptFixRounds(record) {
		if !add(limit, func(r *TaskReceipt) { r.FixRounds = append(r.FixRounds, round) }) {
			receipt.Omitted.FixRounds++
		}
	}
	for _, approval := range receiptApprovals(record) {
		if !add(MaxReceiptBytes, func(r *TaskReceipt) { r.Approvals = append(r.Approvals, approval) }) {
			receipt.Omitted.Approvals++
		}
	}
	return receipt
}

// receiptUsage is the task's usage against its budget, and the count of
// dollar sources past maxReceiptSources.
func receiptUsage(record Record) (ReceiptUsage, int) {
	usage := UsageOf(record)
	result := ReceiptUsage{USD: usage.USD, USDEstimated: usage.USDEstimated, Tokens: usage.Tokens, UnknownReason: clipLine(usage.UnknownReason, maxReceiptReason)}
	if usage.USD == nil {
		if floor := knownFloor(record); floor > 0 {
			result.KnownUSD = &floor
		}
	}
	if record.Profile != nil {
		budget := record.Profile.Budget
		result.Budget, result.StopRaised = &budget, costApproved(record.OwnerApprovals)
	}
	// The source of each session's latest known dollars, in session order.
	latest := map[[2]string]*DollarSource{}
	var order [][2]string
	for _, sample := range record.Usage {
		k := [2]string{sample.Source, sample.SessionID}
		if _, seen := latest[k]; !seen {
			order = append(order, k)
		}
		if sample.DollarSource != nil {
			latest[k] = sample.DollarSource
		}
	}
	omitted := 0
	for _, k := range order {
		if source := latest[k]; source != nil && !slices.Contains(result.DollarSources, *source) {
			if len(result.DollarSources) == maxReceiptSources {
				omitted++
				continue
			}
			result.DollarSources = append(result.DollarSources, *source)
		}
	}
	return result, omitted
}

// receiptChecks counts the checks and runs. A run that did not pass is one
// whose status is other than passed and running.
func receiptChecks(record Record) ReceiptChecks {
	checks := ReceiptChecks{Planned: len(allChecks(record)), Executed: len(record.Checks)}
	for _, run := range record.Checks {
		if run.Status != "passed" && run.Status != "running" {
			checks.Failed++
		}
	}
	if verification := record.Verification; verification != nil {
		reused := 0
		for _, check := range allChecks(record) {
			prefix := reuseLimitationPrefix(check.ID)
			if slices.ContainsFunc(verification.Limitations, func(line string) bool { return strings.HasPrefix(line, prefix) }) {
				reused++
			}
		}
		checks.Reused = &reused
	}
	return checks
}

// receiptFindings counts every finding by severity and status into receipt
// and returns the open ones (pending or accepted), newest result first.
func receiptFindings(record Record, receipt *TaskReceipt) []ReceiptFinding {
	counts := map[[2]string]int{}
	var open []ReceiptFinding
	for i := len(record.WorkerResults) - 1; i >= 0; i-- {
		result := record.WorkerResults[i].Result
		for _, finding := range result.Findings {
			status := findingStatus(record, result.AssignmentID, finding.ID)
			counts[[2]string{finding.severity(), status}]++
			if status == "pending" || status == "accepted" {
				open = append(open, ReceiptFinding{ID: result.AssignmentID + "/" + finding.ID, Severity: finding.severity(), Status: status, Summary: historySummary(finding.Summary)})
			}
		}
	}
	for _, severity := range severityOrder {
		for _, status := range statusOrder {
			if n := counts[[2]string{severity, status}]; n > 0 {
				receipt.Findings = append(receipt.Findings, FindingCount{Severity: severity, Status: status, Count: n})
			}
		}
	}
	return open
}

// receiptFixRounds lists each finding or requirement that took fix rounds and
// is still open, in the order of its first fix round. A requirement is judged
// at the stored verification's source, as verify bound it; before a
// verification, one with checks is open until the latest run of each passed.
func receiptFixRounds(record Record) []ReceiptFixRound {
	assignments := fixAssignments(record)
	allowed := 0
	if record.Profile != nil {
		allowed = record.Profile.FixRounds
	}
	var source *creditSource
	if verification := record.Verification; verification != nil {
		source = recordedCredit(verification.CurrentSnapshot, verification.SourceSHA256)
	}
	var rounds []ReceiptFixRound
	seen := map[FixRef]bool{}
	for _, assigned := range record.Assignments {
		for _, fix := range assigned.Brief.Fixes {
			key := fixKey(record, fix)
			if seen[key] || len(assignments[key]) == 0 {
				continue
			}
			seen[key] = true
			if !receiptKeyOpen(record, source, key) {
				continue
			}
			name := EscalatedKey{Key: key}.Name()
			rounds = append(rounds, ReceiptFixRound{Key: name, Rounds: len(assignments[key]), Allowed: allowed})
		}
	}
	return rounds
}

func receiptKeyOpen(record Record, source *creditSource, key FixRef) bool {
	if key.Requirement == "" || source != nil {
		return stillOpen(record, source, key)
	}
	at := slices.IndexFunc(record.Plan.Requirements, func(r Requirement) bool { return r.ID == key.Requirement })
	if at < 0 {
		return false
	}
	checks := record.Plan.Requirements[at].CheckIDs
	if len(checks) == 0 {
		return record.Assessments[key.Requirement].Assessment.Status != "satisfied"
	}
	return slices.ContainsFunc(checks, func(id string) bool {
		run := latestRun(record, id)
		return run == nil || run.Status != "passed"
	})
}

// receiptApprovals lists every owner approval, on assignments and on the
// record, in time order.
func receiptApprovals(record Record) []ReceiptApproval {
	var approvals []ReceiptApproval
	for _, assigned := range record.Assignments {
		for _, approval := range assigned.OwnerApprovals {
			approvals = append(approvals, ReceiptApproval{For: approval.For, Text: approval.Text, At: approval.At, Assignment: assigned.Brief.ID})
		}
	}
	for _, approval := range record.OwnerApprovals {
		approvals = append(approvals, ReceiptApproval{For: approval.For, Text: approval.Text, At: approval.At})
	}
	slices.SortStableFunc(approvals, func(a, b ReceiptApproval) int { return a.At.Compare(b.At) })
	return approvals
}

// worstReceiptOmitted is above every count a receipt can omit, so a receipt
// measured with it is no smaller than with its actual counts.
var worstReceiptOmitted = ReceiptOmitted{DollarSources: 999999, OpenFindings: 999999, FixRounds: 999999, Approvals: 999999}

// size is the larger of the receipt's two encodings, measured with
// worstReceiptOmitted in place of its list counts.
func (receipt TaskReceipt) size() int {
	sources := receipt.Omitted.DollarSources
	receipt.Omitted = worstReceiptOmitted
	receipt.Omitted.DollarSources = sources
	data, err := EncodeReceipt(receipt)
	if err != nil {
		return MaxReceiptBytes + 1
	}
	return max(len(data), len(receipt.Text()))
}

// EncodeReceipt returns the --json form of a receipt: compact JSON and a
// newline.
func EncodeReceipt(receipt TaskReceipt) ([]byte, error) {
	data, err := encode(receipt)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Text returns the default form of a receipt: one line per fact, each listed
// entry on its own indented line, ending in a newline. Unknown usage reads
// "unknown (REASON)", never a 0.
func (receipt TaskReceipt) Text() string {
	var out strings.Builder
	fmt.Fprintf(&out, "receipt of task %s: %s\n", receipt.TaskID, receipt.State)
	if receipt.Profile == "" {
		out.WriteString("profile: none (started before profiles: no round caps and no budget)\n")
	} else {
		fmt.Fprintf(&out, "profile: %s\n", receipt.Profile)
	}
	if len(receipt.Rounds) > 0 {
		var rounds []string
		for _, r := range receipt.Rounds {
			rounds = append(rounds, fmt.Sprintf("%s %d of %d", r.Stage, r.Used, r.Cap))
		}
		fmt.Fprintf(&out, "author rounds: %s\n", strings.Join(rounds, ", "))
	}
	receipt.Usage.writeText(&out, receipt.Omitted.DollarSources)
	checks := receipt.Checks
	reused := "reused n/a (not verified)"
	if checks.Reused != nil {
		reused = fmt.Sprintf("%d reused at verify", *checks.Reused)
	}
	fmt.Fprintf(&out, "checks: %d planned, %d runs executed, %d failed, %s\n", checks.Planned, checks.Executed, checks.Failed, reused)
	counts := []string{}
	for i, count := range receipt.Findings {
		entry := fmt.Sprintf("%d %s", count.Count, count.Status)
		if i == 0 || receipt.Findings[i-1].Severity != count.Severity {
			entry = count.Severity + " " + entry
		}
		counts = append(counts, entry)
	}
	if len(counts) == 0 {
		counts = append(counts, "none")
	}
	fmt.Fprintf(&out, "findings: %s\n", strings.Join(counts, ", "))
	wall := receipt.WallTime
	fmt.Fprintf(&out, "wall time: %s, from %s to %s\n", wall.Elapsed, wall.StartedAt, wall.UpdatedAt)
	if len(receipt.OpenFindings) > 0 {
		out.WriteString("open findings, newest first:\n")
		for _, finding := range receipt.OpenFindings {
			fmt.Fprintf(&out, "  %s (%s, %s): %s\n", finding.ID, finding.Severity, finding.Status, finding.Summary)
		}
	}
	if len(receipt.FixRounds) > 0 {
		out.WriteString("fix rounds on open findings and requirements:\n")
		for _, round := range receipt.FixRounds {
			fmt.Fprintf(&out, "  %s: %d of %d allowed\n", round.Key, round.Rounds, round.Allowed)
		}
	}
	if len(receipt.Approvals) > 0 {
		out.WriteString("owner approvals:\n")
		for _, approval := range receipt.Approvals {
			on := ""
			if approval.Assignment != "" {
				on = " on assignment " + approval.Assignment
			}
			fmt.Fprintf(&out, "  %s%s at %s: \"%s\"\n", approval.For, on, timelineInstant(approval.At), approval.Text)
		}
	}
	var omitted []string
	for _, count := range []struct {
		n    int
		what string
	}{{receipt.Omitted.OpenFindings, "older open findings"}, {receipt.Omitted.FixRounds, "fix-round keys"}, {receipt.Omitted.Approvals, "owner approvals"}} {
		if count.n > 0 {
			omitted = append(omitted, fmt.Sprintf("%d %s", count.n, count.what))
		}
	}
	if len(omitted) > 0 {
		fmt.Fprintf(&out, "not listed: %s; read them with cw task show --task %s --full\n", strings.Join(omitted, ", "), receipt.TaskID)
	}
	return out.String()
}

// writeText writes the usage and tokens lines.
func (usage ReceiptUsage) writeText(out *strings.Builder, omittedSources int) {
	budget := "the task has no budget"
	if b := usage.Budget; b != nil {
		stop := percent(b.StopAt) + "%"
		if usage.StopRaised {
			stop = "100% (raised by the owner's cost-budget approval)"
		}
		budget = fmt.Sprintf("budget %s USD, new work stops at %s", dollars(b.MaxUSD), stop)
	}
	if usage.USD == nil {
		fmt.Fprintf(out, "usage: unknown (%s)", usage.UnknownReason)
		if usage.KnownUSD != nil {
			fmt.Fprintf(out, "; at least an estimated %s USD is known", dollars(*usage.KnownUSD))
		}
		fmt.Fprintf(out, "; %s\n", budget)
	} else {
		fmt.Fprintf(out, "usage: an estimated %s USD", dollars(*usage.USD))
		if b := usage.Budget; b != nil && b.MaxUSD > 0 {
			fmt.Fprintf(out, ", %s%% of the", percent(*usage.USD/b.MaxUSD))
		} else {
			out.WriteString(";")
		}
		fmt.Fprintf(out, " %s\n", budget)
	}
	if len(usage.DollarSources) > 0 {
		var labels []string
		for i := range usage.DollarSources {
			labels = append(labels, usage.DollarSources[i].label())
		}
		if omittedSources > 0 {
			labels = append(labels, fmt.Sprintf("%d more", omittedSources))
		}
		fmt.Fprintf(out, "dollars priced by: %s\n", strings.Join(labels, "; "))
	}
	switch t := usage.Tokens; {
	case t == nil && usage.USD == nil:
		out.WriteString("tokens: unknown (as above)\n")
	case t == nil:
		fmt.Fprintf(out, "tokens: unknown (%s)\n", usage.UnknownReason)
	default:
		fmt.Fprintf(out, "tokens: %d uncached input, %d cache writes, %d cache reads, %d output\n", t.UncachedInput, t.CacheWrite, t.CacheRead, t.Output)
	}
}
