package taskflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// noAdvisersLimitation is the verify limitation for a task bound to no advisers.
const noAdvisersLimitation = "no advisers were configured when the task started; decisions rest on source evidence and checks only"

// fixedVerifyLimitations open every verification, in this order, whatever the
// task holds.
var fixedVerifyLimitations = [...]string{
	"Source hashes, line ranges and record structure do not prove semantic correctness, complete requirements, test adequacy, or omitted facts.",
	"Approved checks execute local code without a sandbox; adviser credentials are removed from their environment. No adviser network calls are made by task commands.",
	"Worker and coordinator identities are declarations, not authenticated runtime identities. Ownership checks validate declared changes and scoped content freshness; they do not isolate processes or detect all undeclared edits. Closing a handoff does not stop worker processes.",
	"Scoped worker freshness conservatively hashes all entries under owned subtrees, including generated files, and rejects symlinks or exceeded bounds. Exact source-file scopes or whole-project '.' scope avoid depending on unsupported generated subtrees.",
}

// FixedVerifyLimitations returns a copy of the limitations that Verify reports
// first for every task, in order. Callers that print a compact view can drop
// these exact strings and keep only the task-specific limitations.
func FixedVerifyLimitations() []string { return slices.Clone(fixedVerifyLimitations[:]) }

// Start captures the immutable before-work baseline and creates private records
// outside the selected project. No project files are written. advisers is the
// task's immutable adviser binding (provider -> pinned model, possibly empty):
// the advisers configured when the task starts. Later policy or credential
// changes never change it.
func Start(ctx context.Context, repo, store string, plan Plan, advisers map[string]string) (Record, error) {
	return start(ctx, repo, store, plan, advisers, nil)
}

func start(ctx context.Context, repo, store string, plan Plan, advisers map[string]string, packs *packChoice) (Record, error) {
	if plan.CoordinatorID == "" {
		plan.CoordinatorID = "/root"
	}
	if err := ValidatePlan(&plan); err != nil {
		return Record{}, err
	}
	advisers = maps.Clone(advisers)
	if advisers == nil {
		advisers = map[string]string{}
	}
	if err := consult.ValidateBinding(advisers); err != nil {
		return Record{}, err
	}
	if conflict, ok := adviserClaimConflict(plan, advisers); ok {
		return Record{}, errors.New(conflict + `; set "required": true and run the checkpoints, or, if checkpoints are truly unneeded, give the real reason`)
	}
	snapshot, files, err := projectctx.CaptureManifest(ctx, repo)
	if err != nil {
		return Record{}, fmt.Errorf("capture complete task baseline: %w", err)
	}
	packSet, err := packs.bind(snapshot, files)
	if err != nil {
		return Record{}, err
	}
	manifest, err := encode(files)
	if err != nil {
		return Record{}, err
	}
	if len(manifest) > MaxInputBytes {
		return Record{}, errors.New("baseline manifest exceeds the 8 MiB limit")
	}
	store, err = privateStore(store, snapshot.Root)
	if err != nil {
		return Record{}, err
	}
	parent, err := os.OpenRoot(store)
	if err != nil {
		return Record{}, err
	}
	defer parent.Close()
	id, err := randomID()
	if err != nil {
		return Record{}, err
	}
	if err := parent.Mkdir(id, 0700); err != nil {
		return Record{}, err
	}
	directory := filepath.Join(store, id)
	initializationError := func(err error) error {
		return fmt.Errorf("initialize task at %s; inspect and reload this path before retrying: %w", directory, err)
	}
	root, err := parent.OpenRoot(id)
	if err != nil {
		return Record{}, initializationError(err)
	}
	defer root.Close()
	now := time.Now().UTC()
	record := Record{Version: Version, ID: id, Directory: directory, Revision: 1, CreatedAt: now, UpdatedAt: now, Plan: plan, Baseline: snapshot, BaselineManifest: &ManifestRef{File: baselineManifestFile, SHA256: digest(manifest), Files: len(files)}, Advisers: advisers, Contexts: map[string]Context{}, Checks: []CheckRun{}, Assessments: map[string]BoundAssessment{}, Consultations: map[string]Attachment{}}
	record.Packs = packSet
	if packs != nil {
		// StartWithPacks fixes the test rules; Start keeps the earlier ones (R1).
		record.TestRules = TestRulesV1
	}
	record.Contexts["before"] = Context{TaskID: id, Phase: "before", Snapshot: snapshot, Advisers: advisers}
	data, err := encode(record)
	if err != nil {
		return Record{}, initializationError(err)
	}
	if len(data) > MaxInputBytes {
		return Record{}, initializationError(errors.New("initial task revision exceeds size limit"))
	}
	if err := writeNew(root, baselineManifestFile, manifest); err != nil {
		return Record{}, initializationError(err)
	}
	if err := writeNew(root, "record-000001.json", data); err != nil {
		return Record{}, initializationError(err)
	}
	if err := syncTaskDirectory(parent); err != nil {
		return Record{}, initializationError(fmt.Errorf("task was published but its storage directory durability is uncertain: %w", err))
	}
	return record, nil
}

// PrepareContext emits the exact phase/source binding shared with advisers.
// A final-phase context may be refreshed after further source changes; any old
// final consultation, assessment and checks then remain visibly stale.
func PrepareContext(ctx context.Context, path, phase string) (Context, error) {
	if !phaseValid(phase) {
		return Context{}, errors.New("phase must be before or final")
	}
	var result Context
	_, err := update(path, func(root *os.Root, record *Record) error {
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if phase == "before" && !equalSnapshot(snapshot, record.Baseline) {
			return errors.New("before context no longer matches the baseline; start a new task")
		}
		if phase == "final" && record.Plan.Kind == "substantial" {
			if _, err := readConsultation(root, *record, "before", record.Baseline); err != nil {
				return fmt.Errorf("final context needs resolved before consultation: %w", err)
			}
		}
		result = Context{TaskID: record.ID, Phase: phase, Snapshot: snapshot, Advisers: record.Advisers}
		if phase == "final" {
			if problems := localEvidenceProblems(root, *record, snapshot); len(problems) > 0 {
				return fmt.Errorf("final context requires completed applicable evidence: %s", strings.Join(problems, "; "))
			}
			result.EvidenceSHA256, err = evidenceBinding(*record)
			if err != nil {
				return err
			}
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while preparing task context")
		}
		record.Contexts[phase] = result
		record.Verification = nil
		return nil
	})
	return result, err
}

func validateRefs(snapshot projectctx.Snapshot, refs []SourceRef) error {
	if len(refs) == 0 || len(refs) > 256 {
		return errors.New("evidence needs 1..256 source references")
	}
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, ref := range refs {
		if ref.Path == "" || filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path || ref.Path == "." || strings.Contains(ref.Path, "\\") || ref.Path == ".." || strings.HasPrefix(ref.Path, ".."+string(os.PathSeparator)) || ref.StartLine < 1 || ref.EndLine < ref.StartLine {
			return errors.New("source references need a clean relative path and existing positive line range")
		}
		info, err := root.Lstat(ref.Path)
		if err != nil {
			return fmt.Errorf("source reference %s: %w", ref.Path, err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("source references must point to regular files, not symlinks")
		}
		file, err := root.Open(ref.Path)
		if err != nil {
			return err
		}
		opened, statErr := file.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			file.Close()
			return errors.New("source reference changed while opening")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 16<<20+1))
		file.Close()
		if readErr != nil {
			return readErr
		}
		if len(data) > 16<<20 {
			return errors.New("referenced source exceeds 16 MiB limit")
		}
		if digest(data) != ref.SHA256 {
			return fmt.Errorf("source reference hash differs for %s", ref.Path)
		}
		lines := strings.Count(string(data), "\n")
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if ref.EndLine > lines {
			return fmt.Errorf("source reference line range does not exist in %s", ref.Path)
		}
	}
	return nil
}

func latestRun(record Record, id string) *CheckRun {
	for i := len(record.Checks) - 1; i >= 0; i-- {
		if record.Checks[i].CheckID == id {
			return &record.Checks[i]
		}
	}
	return nil
}

func bindChecks(record Record, snapshot projectctx.Snapshot, ids []string) ([]string, error) {
	if !uniqueIDs(ids) {
		return nil, errors.New("check references must be distinct check IDs")
	}
	runs := []string{}
	for _, id := range ids {
		run := latestRun(record, id)
		if run == nil || !passedAt(*run, snapshot) {
			return nil, fmt.Errorf("check %s lacks a passed run at the current source snapshot", id)
		}
		runs = append(runs, run.ID)
	}
	return runs, nil
}

func permittedNonExecution(record Record) bool {
	for _, requirement := range record.Plan.Requirements {
		if !requirement.AllowNonExecution {
			return false
		}
	}
	return true
}

func Assess(ctx context.Context, path string, input AssessmentInput) (Record, error) {
	if len(input.Requirements) == 0 && input.Decision == nil {
		return Record{}, errors.New("assessment must update a requirement or the Coordinator decision")
	}
	return update(path, func(_ *os.Root, record *Record) error {
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if input, err = completeAssessment(snapshot, input); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, assessment := range input.Requirements {
			var requirement *Requirement
			for i := range record.Plan.Requirements {
				if record.Plan.Requirements[i].ID == assessment.ID {
					requirement = &record.Plan.Requirements[i]
					break
				}
			}
			if requirement == nil || seen[assessment.ID] {
				return errors.New("assessment contains an unknown or duplicate requirement")
			}
			seen[assessment.ID] = true
			if assessment.Status != "satisfied" && assessment.Status != "pending" {
				return errors.New("requirement status must be satisfied or pending")
			}
			var runs []string
			if assessment.Status == "satisfied" {
				if err := validateRefs(snapshot, assessment.Evidence); err != nil {
					return err
				}
				if len(assessment.CheckIDs) == 0 {
					if !requirement.AllowNonExecution || !nonblank(assessment.NonExecutionReason) {
						return fmt.Errorf("requirement %s needs check evidence or explicitly permitted non-execution reasoning", assessment.ID)
					}
				} else {
					for _, id := range requirement.CheckIDs {
						if !slices.Contains(assessment.CheckIDs, id) {
							return fmt.Errorf("requirement %s omits planned check %s", assessment.ID, id)
						}
					}
				}
				var bindErr error
				runs, bindErr = bindChecks(*record, snapshot, assessment.CheckIDs)
				if bindErr != nil {
					return bindErr
				}
			} else if len(assessment.Evidence) > 0 || len(assessment.CheckIDs) > 0 {
				return errors.New("pending assessments must not claim verified evidence")
			}
			record.Assessments[assessment.ID] = BoundAssessment{Assessment: assessment, Snapshot: snapshot, CheckRuns: runs}
		}
		if input.Decision != nil {
			decision := *input.Decision
			if !nonblank(decision.Reason) || (decision.Status != "resolved" && decision.Status != "unresolved") {
				return errors.New("Coordinator decision needs resolved/unresolved status and a reason")
			}
			var runs []string
			if decision.Status == "resolved" {
				if err := validateRefs(snapshot, decision.Evidence); err != nil {
					return err
				}
				if len(decision.CheckIDs) == 0 && (!permittedNonExecution(*record) || !nonblank(decision.NonExecutionReason)) {
					return errors.New("resolved Coordinator decision needs check evidence or permitted non-execution reasoning")
				}
				runs, err = bindChecks(*record, snapshot, decision.CheckIDs)
				if err != nil {
					return err
				}
			}
			record.Decision = &BoundDecision{Decision: decision, Snapshot: snapshot, CheckRuns: runs}
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while validating assessment")
		}
		record.Verification = nil
		record.EvidenceRevision++
		return nil
	})
}

func checkStoredRun(root *os.Root, record Record, snapshot projectctx.Snapshot, id string) error {
	var run *CheckRun
	for i := range record.Checks {
		if record.Checks[i].ID == id {
			if run != nil {
				return errors.New("duplicate check run ID")
			}
			run = &record.Checks[i]
		}
	}
	if run == nil {
		return errors.New("referenced check run is missing")
	}
	ids, err := bindChecks(record, snapshot, []string{run.CheckID})
	if err != nil || len(ids) != 1 || ids[0] != id {
		return errors.New("referenced check run is no longer the current passed run")
	}
	approved, found := findCheck(record, run.CheckID)
	if !found || !slices.Equal(approved.Argv, run.Argv) {
		return errors.New("check argv differs from the explicit plan")
	}
	data, err := privateRead(root, run.Log, MaxLogBytes)
	if err != nil {
		return err
	}
	if digest(data) != run.LogSHA256 || run.FinishedAt == nil {
		return errors.New("check log hash or completion metadata differs")
	}
	return nil
}

func evidenceBinding(record Record) (string, error) {
	data, err := encode(struct {
		Revision    int                        `json:"revision"`
		Plan        Plan                       `json:"plan"`
		Amendments  []Amendment                `json:"amendments,omitempty"`
		Checks      []CheckRun                 `json:"checks"`
		Assessments map[string]BoundAssessment `json:"assessments"`
		Decision    *BoundDecision             `json:"decision"`
		Assignments []WorkerAssignment         `json:"assignments"`
		Results     []BoundWorkerResult        `json:"worker_results"`
		Acceptances []BoundWorkerAcceptance    `json:"worker_acceptances"`
	}{record.EvidenceRevision, record.Plan, record.Amendments, record.Checks, record.Assessments, record.Decision, record.Assignments, record.WorkerResults, record.WorkerAcceptances})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

// localEvidenceProblems is shared by final-context preparation and completion.
// The final adviser checkpoint can only bind already completed evidence.
func localEvidenceProblems(root *os.Root, record Record, snapshot projectctx.Snapshot) []string {
	problems := workerEvidenceProblems(root, record, snapshot)
	problem := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	for _, run := range record.Checks {
		if run.Status == "running" || run.FinishedAt == nil {
			problem("owned check run %s (%s) is nonterminal", run.ID, run.CheckID)
		}
	}
	for _, requirement := range record.Plan.Requirements {
		bound, ok := record.Assessments[requirement.ID]
		if !ok || bound.Assessment.Status != "satisfied" {
			problem("requirement %s is not satisfied", requirement.ID)
			continue
		}
		if !equalSnapshot(snapshot, bound.Snapshot) {
			problem("requirement %s evidence is stale", requirement.ID)
			continue
		}
		if err := validateRefs(snapshot, bound.Assessment.Evidence); err != nil {
			problem("requirement %s: %v", requirement.ID, err)
		}
		runs, err := bindChecks(record, snapshot, bound.Assessment.CheckIDs)
		if err != nil || !slices.Equal(runs, bound.CheckRuns) {
			problem("requirement %s check references are invalid", requirement.ID)
		}
		if len(runs) == 0 && (!requirement.AllowNonExecution || !nonblank(bound.Assessment.NonExecutionReason)) {
			problem("requirement %s lacks permitted non-execution reasoning", requirement.ID)
		}
		if len(runs) > 0 {
			for _, id := range requirement.CheckIDs {
				if !slices.Contains(bound.Assessment.CheckIDs, id) {
					problem("requirement %s omits planned check %s", requirement.ID, id)
				}
			}
		}
		for _, id := range bound.CheckRuns {
			if err := checkStoredRun(root, record, snapshot, id); err != nil {
				problem("requirement %s: %v", requirement.ID, err)
			}
		}
	}
	for _, check := range allChecks(record) {
		if run := latestRun(record, check.ID); run != nil && run.Status != "passed" {
			problem("latest run of %s is %s", check.ID, run.Status)
		}
	}
	decision := record.Decision
	if decision == nil || decision.Decision.Status != "resolved" || !nonblank(decision.Decision.Reason) {
		problem("Codex task decision is unresolved")
	} else {
		if !equalSnapshot(snapshot, decision.Snapshot) {
			problem("Codex task decision is stale")
		}
		if err := validateRefs(snapshot, decision.Decision.Evidence); err != nil {
			problem("Codex task decision: %v", err)
		}
		runs, err := bindChecks(record, snapshot, decision.Decision.CheckIDs)
		if err != nil || !slices.Equal(runs, decision.CheckRuns) {
			problem("Codex task decision has invalid check references")
		}
		if len(runs) == 0 && (!permittedNonExecution(record) || !nonblank(decision.Decision.NonExecutionReason)) {
			problem("Codex task decision lacks permitted non-execution reasoning")
		}
		for _, id := range decision.CheckRuns {
			if err := checkStoredRun(root, record, snapshot, id); err != nil {
				problem("Codex task decision: %v", err)
			}
		}
	}
	return problems
}

// Verify records a mechanical completion gate. It does not claim to prove the
// meaning of source, the adequacy of tests, or the truth of Codex's reasoning.
func Verify(ctx context.Context, path string) (Verification, error) {
	var result Verification
	_, err := update(path, func(root *os.Root, record *Record) error {
		snapshot, files, err := projectctx.CaptureManifest(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return fmt.Errorf("completion requires a complete current source snapshot: %w", err)
		}
		manifest, err := encode(files)
		if err != nil {
			return err
		}
		result = Verification{TaskID: record.ID, CurrentSnapshot: snapshot, SourceSHA256: digest(manifest), Problems: []string{}, CheckedAt: time.Now().UTC(), Limitations: FixedVerifyLimitations()}
		problem := func(format string, args ...any) {
			result.Problems = append(result.Problems, fmt.Sprintf(format, args...))
		}
		result.Problems = append(result.Problems, localEvidenceProblems(root, *record, snapshot)...)
		result.Limitations = append(result.Limitations, deferredFindingLimitations(*record)...)
		findingIssues, findingNotes := rejectedFindingReport(root, *record)
		result.Problems = append(result.Problems, findingIssues...)
		result.Limitations = append(result.Limitations, findingNotes...)
		contractIssues, contractNotes := contractProblems(root, *record)
		result.Problems = append(result.Problems, contractIssues...)
		result.Limitations = append(result.Limitations, contractNotes...)
		view := newSourceView(snapshot, files)
		defer view.close()
		traceIssues, traceNotes := traceVerifyEntries(root, *record, view)
		result.Problems = append(result.Problems, traceIssues...)
		result.Limitations = append(result.Limitations, traceNotes...)
		if gated(*record) {
			// R16, R15 and R14, then R23's limitation, before the
			// substantial-task block.
			authorshipIssues, authorshipNotes := authorshipVerifyEntries(view.authorship(root, *record), *record)
			result.Problems = append(result.Problems, authorshipIssues...)
			result.Limitations = append(result.Limitations, authorshipNotes...)
		}
		if record.Plan.Kind == "substantial" {
			if !equalSnapshot(record.Baseline, snapshot) {
				executionIssues, executionNotes := executionProblems(ctx, root, *record, snapshot, files)
				result.Problems = append(result.Problems, executionIssues...)
				result.Limitations = append(result.Limitations, executionNotes...)
			}
			if len(record.Advisers) == 0 {
				result.Limitations = append(result.Limitations, noAdvisersLimitation)
			}
			required := checkpointsRequired(record.Plan)
			if !required {
				result.Limitations = append(result.Limitations, "adviser checkpoints not required: "+record.Plan.Checkpoints.Reason)
			}
			_, beforeAttached := record.Consultations["before"]
			_, finalAttached := record.Consultations["final"]
			if conflict, ok := adviserClaimConflict(record.Plan, record.Advisers); ok && !(beforeAttached && finalAttached) {
				// A usable before checkpoint is the one final-context preparation accepts.
				_, beforeErr := readConsultation(root, *record, "before", record.Baseline)
				switch {
				case beforeErr == nil:
					conflict += "; attach the final checkpoint"
				case equalSnapshot(record.Baseline, snapshot):
					conflict += "; attach the before and final checkpoints, or abandon this record and start a corrected one"
				default:
					conflict += "; the source changed since start, so the before checkpoint can no longer be attached and a new record must not start over the changed source: stop and ask the user how to proceed"
				}
				result.Problems = append(result.Problems, conflict)
			}
			for _, phase := range []string{"before", "final"} {
				// A checkpoint attached although not required is still validated.
				if _, attached := record.Consultations[phase]; !required && !attached {
					continue
				}
				expected := snapshot
				if phase == "before" {
					expected = record.Baseline
				}
				consultation, err := readConsultation(root, *record, phase, expected)
				if err != nil {
					problem("%s consultation: %v", phase, err)
					continue
				}
				for _, provider := range slices.Sorted(maps.Keys(record.Advisers)) {
					if status := consultation.Providers[provider].Status; status != "success" {
						result.Limitations = append(result.Limitations, fmt.Sprintf("%s %s coverage is %s; the coordinator's recorded source/check decision supplies the fallback.", phase, provider, status))
					}
				}
			}
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			problem("source changed during completion verification")
		}
		result.Complete = len(result.Problems) == 0
		record.Verification = &result
		return nil
	})
	if err != nil {
		return result, err
	}
	if !result.Complete {
		return result, errors.New("task completion evidence is incomplete; inspect problems")
	}
	return result, nil
}
