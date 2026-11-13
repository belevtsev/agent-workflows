package taskflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// MaxBriefBytes bounds a stage brief (D15): its JSON encoding and its text,
// each with the final newline, are at most 4 KiB.
const MaxBriefBytes = 4 << 10

// ErrUnknownStage is the refusal of a stage brief for a stage that is not
// one of the flow's stages or not in the task's profile; the command line
// named it, so the CLI exits with the usage status.
var ErrUnknownStage = errors.New("unknown stage")

// stageReferences maps each stage to the one role contract a fresh worker of
// that stage loads, relative to the plugin root.
var stageReferences = map[string]string{
	StageSpec:           "spec-writer.md",
	StageSpecReview:     "spec-review.md",
	StageDesign:         "architect.md",
	StageDesignReview:   "design-review.md",
	StageTests:          "test-engineer.md",
	StageImplementation: "implementer.md",
	StageTestReview:     "test-review.md",
	StageCodeReview:     "code-review.md",
	StageAdversarial:    "adversarial-tester.md",
}

// contractsDir holds the role contracts stageReferences names.
const contractsDir = "skills/task-orchestration/references/contracts"

// reviewedStages maps each review stage to the author stage it reviews: the
// findings of a review belong to both.
var reviewedStages = map[string]string{
	StageSpecReview:   StageSpec,
	StageDesignReview: StageDesign,
	StageTestReview:   StageTests,
	StageCodeReview:   StageImplementation,
	StageAdversarial:  StageImplementation,
}

// Brief is what a fresh worker of one stage starts from (D15), built from the
// record by StageBrief. Every entry is listed whole or not at all; what does
// not fit in MaxBriefBytes is counted in Omitted.
type Brief struct {
	TaskID  string `json:"task_id"`
	Title   string `json:"title,omitempty"`
	Stage   string `json:"stage"`
	Profile string `json:"profile"`
	// RoundCap and RoundsLeft are set for a stage whose author rounds the
	// profile caps; RoundsLeft is never below 0.
	RoundCap   int  `json:"round_cap,omitzero"`
	RoundsLeft *int `json:"rounds_left,omitempty"`
	// Reference is the one role contract the stage's worker loads.
	Reference string `json:"reference"`
	// Assignment is the latest assignment of the stage, whose stored contract,
	// bundle and inputs Files lists as absolute paths.
	Assignment   string             `json:"assignment,omitempty"`
	Files        []string           `json:"files"`
	Checks       []string           `json:"checks"`
	Requirements []BriefRequirement `json:"requirements"`
	// Findings are the stage's open findings, newest first.
	Findings []BriefFinding `json:"findings"`
	Omitted  BriefOmitted   `json:"omitted,omitzero"`
}

// BriefRequirement is one plan requirement.
type BriefRequirement struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// BriefFinding is one open finding: ID is "<assignment id>/<finding id>", as
// a brief's fixes names it, and Report the stored report of its result.
type BriefFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Report   string `json:"report,omitempty"`
}

// BriefOmitted counts what the brief does not list for lack of room.
type BriefOmitted struct {
	Title        bool `json:"title,omitzero"`
	Files        int  `json:"files,omitzero"`
	Checks       int  `json:"checks,omitzero"`
	Requirements int  `json:"requirements,omitzero"`
	Findings     int  `json:"findings,omitzero"`
}

// StageBrief builds the brief of stage from record. The stage must be in the
// record's profile; a record without one has no stages. The brief fits in
// MaxBriefBytes by construction: the fixed members are bounded, and the title,
// files, checks, requirements and findings are added whole, in that order,
// while both encodings still fit. Each of these five parts takes at most an
// equal share of the room left when it starts, so none starves the parts
// after it, and what it leaves passes on. The findings stop at the first that
// does not fit, so every unlisted finding is older than every listed one.
func StageBrief(record Record, stage string) (Brief, error) {
	if !slices.Contains(stageOrder, stage) {
		return Brief{}, fmt.Errorf("%w %q: use one of %s", ErrUnknownStage, stage, strings.Join(stageOrder, ", "))
	}
	profile := record.Profile
	if profile == nil {
		return Brief{}, fmt.Errorf("%w %s: the task was started before profiles and has no stages", ErrUnknownStage, stage)
	}
	if !slices.Contains(profile.Stages, stage) {
		return Brief{}, fmt.Errorf("%w %s: it is not in profile %s; use one of %s", ErrUnknownStage, stage, profile.Name, strings.Join(profile.Stages, ", "))
	}
	brief := Brief{TaskID: record.ID, Stage: stage, Profile: profile.Name, Reference: contractsDir + "/" + stageReferences[stage],
		Files: []string{}, Checks: []string{}, Requirements: []BriefRequirement{}, Findings: []BriefFinding{}}
	if limit, capped := profile.Rounds[stage]; capped {
		left := max(limit-roundsUsed(record)[stage], 0)
		brief.RoundCap, brief.RoundsLeft = limit, &left
	}
	var files []string
	for i := len(record.Assignments) - 1; i >= 0; i-- {
		if assigned := record.Assignments[i]; assigned.Brief.Stage == stage {
			brief.Assignment = assigned.Brief.ID
			for _, name := range append([]string{assigned.ContractFile, assigned.BundleFile}, assigned.InputFiles...) {
				if name != "" {
					files = append(files, filepath.Join(record.Directory, name))
				}
			}
			break
		}
	}
	if size, err := brief.size(); err != nil || size > MaxBriefBytes {
		// The fixed members are bounded far below the limit.
		return Brief{}, fmt.Errorf("stage brief core does not fit in %d bytes", MaxBriefBytes)
	}
	// add appends item with put while the brief still fits within limit, and
	// reports whether it did; a refused item leaves the brief unchanged.
	add := func(limit int, put func(*Brief)) bool {
		next := brief
		next.Files, next.Checks = slices.Clip(next.Files), slices.Clip(next.Checks)
		next.Requirements, next.Findings = slices.Clip(next.Requirements), slices.Clip(next.Findings)
		put(&next)
		if size, err := next.size(); err != nil || size > limit {
			return false
		}
		brief = next
		return true
	}
	// share is the size limit of the next of n parts still to place: an equal
	// share of the room left, so a part that uses less leaves the rest to the
	// parts after it.
	share := func(n int) int {
		size, _ := brief.size()
		return size + (MaxBriefBytes-size)/n
	}
	limit := share(5)
	brief.Omitted.Title = !add(limit, func(b *Brief) { b.Title = record.Plan.Title })
	limit = share(4)
	for _, file := range files {
		if !add(limit, func(b *Brief) { b.Files = append(b.Files, file) }) {
			brief.Omitted.Files++
		}
	}
	limit = share(3)
	for _, check := range allChecks(record) {
		if !add(limit, func(b *Brief) { b.Checks = append(b.Checks, check.ID) }) {
			brief.Omitted.Checks++
		}
	}
	limit = share(2)
	for _, requirement := range record.Plan.Requirements {
		entry := BriefRequirement{ID: requirement.ID, Description: requirement.Description}
		if !add(limit, func(b *Brief) { b.Requirements = append(b.Requirements, entry) }) {
			brief.Omitted.Requirements++
		}
	}
	open := openFindings(record, stage)
	for i, finding := range open {
		if !add(MaxBriefBytes, func(b *Brief) { b.Findings = append(b.Findings, finding) }) {
			brief.Omitted.Findings = len(open) - i
			break
		}
	}
	return brief, nil
}

// openFindings returns the findings of stage, newest first: those of the
// results of its own assignments and of the review stage that reviews it,
// whose latest disposition is none yet or accepted (acknowledged but not
// resolved). Within one result they keep the result's order.
func openFindings(record Record, stage string) []BriefFinding {
	var open []BriefFinding
	for i := len(record.WorkerResults) - 1; i >= 0; i-- {
		result := record.WorkerResults[i].Result
		assigned := findAssignment(record, result.AssignmentID)
		if assigned == nil || assigned.Brief.Stage != stage && reviewedStages[assigned.Brief.Stage] != stage {
			continue
		}
		report := ""
		if result.Report != nil {
			report = filepath.Join(record.Directory, ReportFileName(result.AssignmentID))
		}
		for _, finding := range result.Findings {
			if status := findingStatus(record, result.AssignmentID, finding.ID); status != "pending" && status != "accepted" {
				continue
			}
			open = append(open, BriefFinding{ID: result.AssignmentID + "/" + finding.ID, Severity: finding.severity(), Summary: finding.Summary, Report: report})
		}
	}
	return open
}

// findingStatus returns the latest disposition of a finding of assignment's
// result, or "pending" when no acceptance disposes of it.
func findingStatus(record Record, assignment, finding string) string {
	for i := len(record.WorkerAcceptances) - 1; i >= 0; i-- {
		acceptance := record.WorkerAcceptances[i].Acceptance
		if acceptance.AssignmentID != assignment {
			continue
		}
		for _, disposition := range acceptance.Findings {
			if disposition.FindingID == finding {
				return disposition.Status
			}
		}
	}
	return "pending"
}

// worstOmitted is above every count a brief can omit (at most 256 results of
// at most 256 findings each, and fewer requirements, checks and files), so a
// brief measured with it is no smaller than the same brief with its actual
// counts.
var worstOmitted = BriefOmitted{Title: true, Files: 999999, Checks: 999999, Requirements: 999999, Findings: 999999}

// size returns the larger of the brief's two encodings, each with its final
// newline, measured with worstOmitted in place of its counts, which are known
// only once every entry is placed.
func (brief Brief) size() (int, error) {
	brief.Omitted = worstOmitted
	data, err := EncodeBrief(brief)
	if err != nil {
		return 0, err
	}
	return max(len(data), len(brief.Text())), nil
}

// EncodeBrief returns the --json form of a brief: compact JSON and a newline.
func EncodeBrief(brief Brief) ([]byte, error) {
	data, err := encode(brief)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Text returns the default form of a brief: one line per fact, with each
// listed entry on its own indented line, ending in a newline.
func (brief Brief) Text() string {
	var out strings.Builder
	indent := func(text string) string { return strings.ReplaceAll(text, "\n", "\n    ") }
	fmt.Fprintf(&out, "task %s", brief.TaskID)
	if brief.Title != "" {
		fmt.Fprintf(&out, ": %s", indent(brief.Title))
	}
	fmt.Fprintf(&out, "\nstage %s, profile %s", brief.Stage, brief.Profile)
	if brief.RoundsLeft != nil {
		fmt.Fprintf(&out, ", %d of %d author rounds left", *brief.RoundsLeft, brief.RoundCap)
	}
	fmt.Fprintf(&out, "\nload %s\n", brief.Reference)
	if len(brief.Files) > 0 {
		fmt.Fprintf(&out, "files of assignment %s:\n", brief.Assignment)
		for _, file := range brief.Files {
			fmt.Fprintf(&out, "  %s\n", file)
		}
	}
	if len(brief.Checks) > 0 {
		fmt.Fprintf(&out, "checks: %s\n", strings.Join(brief.Checks, ", "))
	}
	if len(brief.Requirements) > 0 {
		out.WriteString("requirements:\n")
		for _, requirement := range brief.Requirements {
			fmt.Fprintf(&out, "  %s: %s\n", requirement.ID, indent(requirement.Description))
		}
	}
	if len(brief.Findings) > 0 {
		out.WriteString("open findings, newest first:\n")
		for _, finding := range brief.Findings {
			fmt.Fprintf(&out, "  %s (%s): %s\n", finding.ID, finding.Severity, indent(finding.Summary))
			if finding.Report != "" {
				fmt.Fprintf(&out, "    report %s\n", finding.Report)
			}
		}
	}
	var omitted []string
	if brief.Omitted.Title {
		omitted = append(omitted, "the title")
	}
	for _, count := range []struct {
		n    int
		what string
	}{{brief.Omitted.Files, "files"}, {brief.Omitted.Checks, "checks"}, {brief.Omitted.Requirements, "requirements"}, {brief.Omitted.Findings, "older findings"}} {
		if count.n > 0 {
			omitted = append(omitted, fmt.Sprintf("%d %s", count.n, count.what))
		}
	}
	if len(omitted) > 0 {
		fmt.Fprintf(&out, "not listed: %s; read them with cw task show --task %s --field FIELD\n", strings.Join(omitted, ", "), brief.TaskID)
	}
	return out.String()
}
