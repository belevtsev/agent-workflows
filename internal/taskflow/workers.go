package taskflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

// WorkerBrief is immutable coordinator-supplied work scope. OwnedPaths covers
// each named repository-relative path and its descendants; "." covers the full
// repository. Only implementation roles may declare edits. A brief grants no
// publication, messaging, deployment, or other authorization.
type WorkerBrief struct {
	ID            string   `json:"id"`
	AgentID       string   `json:"agent_id"`
	Role          string   `json:"role"`
	Goal          string   `json:"goal"`
	OwnedPaths    []string `json:"owned_paths"`
	Constraints   []string `json:"constraints"`
	CheckIDs      []string `json:"check_ids"`
	StopCondition string   `json:"stop_condition"`
	// Bundle names the filled reviewer bundle; a review assignment in a record
	// with a baseline manifest requires it, and no other role may carry one.
	Bundle *BundleRef `json:"bundle,omitempty"`
}

type WorkerAssignment struct {
	TaskID     string              `json:"task_id"`
	Brief      WorkerBrief         `json:"brief"`
	Snapshot   projectctx.Snapshot `json:"snapshot"`
	SHA256     string              `json:"sha256"`
	AssignedAt time.Time           `json:"assigned_at"`
	// BundleFile is the immutable copy of the brief's bundle in the task
	// directory; its bytes hash to Brief.Bundle.SHA256.
	BundleFile string `json:"bundle_file,omitempty"`
}

// WorkerChangedFile binds a declared current regular file to its bytes, or an
// absent file to Deleted=true and an empty SHA256. This declaration does not
// establish which process changed the file or detect omitted changes.
type WorkerChangedFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted"`
}

type WorkerFinding struct {
	ID       string      `json:"id"`
	Summary  string      `json:"summary"`
	Evidence []SourceRef `json:"evidence"`
}

type WorkerResult struct {
	TaskID           string              `json:"task_id"`
	AssignmentID     string              `json:"assignment_id"`
	AssignmentSHA256 string              `json:"assignment_sha256"`
	AgentID          string              `json:"agent_id"`
	Snapshot         projectctx.Snapshot `json:"snapshot"`
	Status           string              `json:"status"`
	Summary          string              `json:"summary"`
	Evidence         []SourceRef         `json:"evidence"`
	ChangedFiles     []WorkerChangedFile `json:"changed_files"`
	CheckIDs         []string            `json:"check_ids"`
	Findings         []WorkerFinding     `json:"findings"`
	Uncertainties    []string            `json:"uncertainties"`
}

type BoundWorkerResult struct {
	Result               WorkerResult `json:"result"`
	SHA256               string       `json:"sha256"`
	CheckRuns            []string     `json:"check_runs"`
	ScopeSHA256          string       `json:"scope_sha256"`
	ReviewEvidenceSHA256 string       `json:"review_evidence_sha256,omitempty"`
	SubmittedAt          time.Time    `json:"submitted_at"`
}

// FindingDisposition records the coordinator's judgment. The reviewer has no
// veto: a rejection needs source evidence and a reason, just as a resolution
// does. An accepted finding remains acknowledged but unresolved, and cannot
// satisfy completion until a later disposition resolves or rejects it. A
// deferred finding also needs source evidence and a reason; it does not block
// completion but verify lists it as a limitation.
type FindingDisposition struct {
	FindingID string      `json:"finding_id"`
	Status    string      `json:"status"`
	Reason    string      `json:"reason"`
	Evidence  []SourceRef `json:"evidence"`
}

type WorkerAcceptance struct {
	TaskID        string               `json:"task_id"`
	AssignmentID  string               `json:"assignment_id"`
	ResultSHA256  string               `json:"result_sha256"`
	CoordinatorID string               `json:"coordinator_id"`
	Status        string               `json:"status"`
	Reason        string               `json:"reason"`
	Evidence      []SourceRef          `json:"evidence"`
	CheckIDs      []string             `json:"check_ids"`
	Findings      []FindingDisposition `json:"findings"`
}

type BoundWorkerAcceptance struct {
	Acceptance WorkerAcceptance    `json:"acceptance"`
	Snapshot   projectctx.Snapshot `json:"snapshot"`
	CheckRuns  []string            `json:"check_runs"`
	AcceptedAt time.Time           `json:"accepted_at"`
}

func validAgentID(value string) bool {
	return len(value) > 0 && len(value) <= 256 && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func boundedText(value string) bool { return nonblank(value) && len(value) <= 16384 }

func boundedTexts(values []string) bool {
	if len(values) > 256 {
		return false
	}
	for _, value := range values {
		if !boundedText(value) {
			return false
		}
	}
	return true
}

func cleanWorkerPath(value string, whole bool) bool {
	if value == "." {
		return whole
	}
	if value == "" || len(value) > 4096 || path.IsAbs(value) || filepath.IsAbs(value) || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") || strings.ContainsAny(value, "\\:\x00") || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".git" {
			return false
		}
	}
	return true
}

func scopeContains(scope, file string) bool {
	return scope == "." || scope == file || strings.HasPrefix(file, scope+"/")
}

func validWorkerSnapshot(record Record, snapshot projectctx.Snapshot) bool {
	return snapshot.Root == record.Baseline.Root && snapshot.SelectedPath == record.Baseline.SelectedPath && strings.HasPrefix(snapshot.Fingerprint, "sha256:") && validSHA256(strings.TrimPrefix(snapshot.Fingerprint, "sha256:")) && snapshot.FileCount >= 0
}

func validateBrief(record Record, brief WorkerBrief) error {
	if brief.Constraints == nil || brief.CheckIDs == nil {
		return errors.New("worker brief constraints and check_ids must be explicit arrays; use [] when empty")
	}
	if !identifier.MatchString(brief.ID) || !validAgentID(brief.AgentID) || !slices.Contains([]string{"implementation", "review", "evidence", "verification"}, brief.Role) || !boundedText(brief.Goal) || !boundedText(brief.StopCondition) || !boundedTexts(brief.Constraints) {
		return errors.New("worker brief needs a distinct identifier, bounded agent identity, supported role, goal, constraints and stop_condition")
	}
	if len(brief.OwnedPaths) == 0 || len(brief.OwnedPaths) > 256 {
		return errors.New("worker brief needs 1..256 owned_paths")
	}
	for i, owned := range brief.OwnedPaths {
		if !cleanWorkerPath(owned, true) {
			return errors.New("owned_paths must be clean repository-relative paths; '.' covers the whole repository")
		}
		for _, earlier := range brief.OwnedPaths[:i] {
			if scopeContains(earlier, owned) || scopeContains(owned, earlier) {
				return errors.New("owned_paths must not duplicate or overlap within a brief")
			}
		}
	}
	if brief.Bundle != nil && (brief.Role != "review" || !filepath.IsAbs(brief.Bundle.Path) || filepath.Clean(brief.Bundle.Path) != brief.Bundle.Path || len(brief.Bundle.Path) > 4096 || strings.ContainsFunc(brief.Bundle.Path, unicode.IsControl) || !validSHA256(brief.Bundle.SHA256)) {
		return errors.New("only a review brief carries a bundle, with a clean absolute path and its SHA-256")
	}
	if !uniqueIDs(brief.CheckIDs) || len(brief.CheckIDs) > 256 {
		return errors.New("worker check_ids must be distinct planned check IDs")
	}
	for _, id := range brief.CheckIDs {
		if !slices.ContainsFunc(record.Plan.Checks, func(check Check) bool { return check.ID == id }) {
			return fmt.Errorf("worker brief references unplanned check %s", id)
		}
	}
	return nil
}

func assignmentHash(assignment WorkerAssignment) (string, error) {
	data, err := encode(struct {
		TaskID   string              `json:"task_id"`
		Brief    WorkerBrief         `json:"brief"`
		Snapshot projectctx.Snapshot `json:"snapshot"`
	}{assignment.TaskID, assignment.Brief, assignment.Snapshot})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func workerResultHash(bound BoundWorkerResult) (string, error) {
	data, err := encode(struct {
		Result               WorkerResult `json:"result"`
		CheckRuns            []string     `json:"check_runs"`
		ScopeSHA256          string       `json:"scope_sha256"`
		ReviewEvidenceSHA256 string       `json:"review_evidence_sha256"`
	}{bound.Result, bound.CheckRuns, bound.ScopeSHA256, bound.ReviewEvidenceSHA256})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func findAssignment(record Record, id string) *WorkerAssignment {
	for i := range record.Assignments {
		if record.Assignments[i].Brief.ID == id {
			return &record.Assignments[i]
		}
	}
	return nil
}

func findWorkerResult(record Record, id string) *BoundWorkerResult {
	for i := range record.WorkerResults {
		if record.WorkerResults[i].Result.AssignmentID == id {
			return &record.WorkerResults[i]
		}
	}
	return nil
}

func latestAcceptance(record Record, id string) *BoundWorkerAcceptance {
	for i := len(record.WorkerAcceptances) - 1; i >= 0; i-- {
		if record.WorkerAcceptances[i].Acceptance.AssignmentID == id {
			return &record.WorkerAcceptances[i]
		}
	}
	return nil
}

// AssignWorker records a brief at the actual current source. It does not launch
// or authorize a process, and IDs are declarations rather than authentication.
// A substantial task dispatches no worker before its before checkpoint is
// attached with a resolved decision, unless its plan declares checkpoints not
// required; a before checkpoint attached anyway is still validated.
func AssignWorker(ctx context.Context, taskPath string, brief WorkerBrief) (Record, error) {
	return update(taskPath, func(root *os.Root, record *Record) error {
		_, attached := record.Consultations["before"]
		if record.Plan.Kind == "substantial" && (checkpointsRequired(record.Plan) || attached) {
			if _, err := readConsultation(root, *record, "before", record.Baseline); err != nil {
				return fmt.Errorf("adjudicate and attach the before checkpoint before dispatching workers: %w", err)
			}
		}
		if err := validateBrief(*record, brief); err != nil {
			return err
		}
		if findAssignment(*record, brief.ID) != nil {
			return errors.New("worker assignment ID is immutable; use a new ID")
		}
		if len(record.Assignments) >= 256 {
			return errors.New("task worker assignment limit reached")
		}
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if err := validateWorkerOwnership(*record, snapshot, brief); err != nil {
			return err
		}
		assignment := WorkerAssignment{TaskID: record.ID, Brief: brief, Snapshot: snapshot, AssignedAt: time.Now().UTC()}
		assignment.SHA256, err = assignmentHash(assignment)
		if err != nil {
			return err
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while assigning worker")
		}
		if err := validateWorkerOwnership(*record, after, brief); err != nil {
			return err
		}
		if record.BaselineManifest != nil && brief.Role == "review" || brief.Bundle != nil {
			if assignment.BundleFile, err = storeReviewBundle(ctx, root, *record, brief, after); err != nil {
				return err
			}
		}
		record.Assignments = append(record.Assignments, assignment)
		record.EvidenceRevision++
		record.Verification = nil
		return nil
	})
}

func refsShape(refs []SourceRef, required bool) error {
	if len(refs) > 256 || required && len(refs) == 0 {
		return errors.New("worker evidence needs bounded source references")
	}
	for _, ref := range refs {
		if !cleanWorkerPath(ref.Path, false) || ref.StartLine < 1 || ref.EndLine < ref.StartLine || !validSHA256(ref.SHA256) {
			return errors.New("invalid worker source reference path, line range or SHA256")
		}
	}
	return nil
}

func validateResult(record Record, assigned WorkerAssignment, result WorkerResult) error {
	if result.TaskID != record.ID || result.AssignmentID != assigned.Brief.ID || result.AssignmentSHA256 != assigned.SHA256 || result.AgentID != assigned.Brief.AgentID || !validWorkerSnapshot(record, result.Snapshot) {
		return errors.New("worker result task, assignment hash, agent identity or source snapshot differs")
	}
	if assigned.Brief.Role == "review" && result.Status == "completed" && !equalSnapshot(assigned.Snapshot, result.Snapshot) {
		return errors.New("review source changed since its immutable assignment; assign a new review at the current source")
	}
	if result.Evidence == nil || result.ChangedFiles == nil || result.CheckIDs == nil || result.Findings == nil || result.Uncertainties == nil {
		return errors.New("worker result evidence, changed_files, check_ids, findings and uncertainties must be explicit arrays; use [] when empty")
	}
	if !slices.Contains([]string{"completed", "blocked", "cancelled"}, result.Status) || !boundedText(result.Summary) || !boundedTexts(result.Uncertainties) {
		return errors.New("worker result needs a terminal completed/blocked/cancelled status, summary and bounded uncertainties")
	}
	if err := refsShape(result.Evidence, result.Status == "completed"); err != nil {
		return err
	}
	if !uniqueIDs(result.CheckIDs) || len(result.CheckIDs) > 256 {
		return errors.New("worker result check_ids must be distinct planned IDs")
	}
	for _, id := range result.CheckIDs {
		if !slices.Contains(assigned.Brief.CheckIDs, id) {
			return fmt.Errorf("worker result check %s is outside its brief", id)
		}
	}
	if result.Status == "completed" {
		for _, id := range assigned.Brief.CheckIDs {
			if !slices.Contains(result.CheckIDs, id) {
				return fmt.Errorf("completed worker result omits brief check %s", id)
			}
		}
	}
	if len(result.ChangedFiles) > 256 || assigned.Brief.Role != "implementation" && len(result.ChangedFiles) != 0 {
		return errors.New("only implementation workers may declare changed files (at most 256)")
	}
	seen := map[string]bool{}
	for _, changed := range result.ChangedFiles {
		if !cleanWorkerPath(changed.Path, false) || seen[changed.Path] || changed.Deleted && changed.SHA256 != "" || !changed.Deleted && !validSHA256(changed.SHA256) {
			return errors.New("changed files need distinct clean paths and SHA256, or explicit deletion without SHA256")
		}
		seen[changed.Path] = true
		if !slices.ContainsFunc(assigned.Brief.OwnedPaths, func(owned string) bool { return scopeContains(owned, changed.Path) }) {
			return fmt.Errorf("changed file %s is outside worker ownership", changed.Path)
		}
	}
	if len(result.Findings) > 256 {
		return errors.New("worker result has too many findings")
	}
	seen = map[string]bool{}
	for _, finding := range result.Findings {
		if !identifier.MatchString(finding.ID) || seen[finding.ID] || !boundedText(finding.Summary) {
			return errors.New("worker findings need distinct identifiers and bounded summaries")
		}
		seen[finding.ID] = true
		if err := refsShape(finding.Evidence, true); err != nil {
			return err
		}
	}
	return nil
}

// workerChecks preserves the status of actual terminal check runs. A completed
// result uses the normal passed-check binder; blocked/cancelled results may cite
// failed or cancelled runs without ever upgrading them to success.
func workerChecks(record Record, snapshot projectctx.Snapshot, ids []string, completed bool) ([]string, error) {
	if completed {
		return bindChecks(record, snapshot, ids)
	}
	runs := []string{}
	for _, id := range ids {
		run := latestRun(record, id)
		if run == nil || run.Status == "running" || run.FinishedAt == nil || run.After == nil || !equalSnapshot(snapshot, *run.After) {
			return nil, fmt.Errorf("check %s lacks an actual terminal run at the result source", id)
		}
		runs = append(runs, run.ID)
	}
	return runs, nil
}

func readWorkerFile(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("worker file must be regular, not a symlink")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("worker file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("worker file exceeds the 64 MiB content bound")
	}
	return data, nil
}

func validateChangedFiles(snapshot projectctx.Snapshot, changes []WorkerChangedFile) error {
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, changed := range changes {
		if _, err := inspectWorkerPath(root, changed.Path, changed.Deleted); err != nil {
			return err
		}
		if changed.Deleted {
			if _, err := root.Lstat(changed.Path); !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("declared deletion %s is not absent", changed.Path)
			}
			continue
		}
		data, err := readWorkerFile(root, changed.Path)
		if err != nil {
			return fmt.Errorf("changed file %s: %w", changed.Path, err)
		}
		if digest(data) != changed.SHA256 {
			return fmt.Errorf("changed file %s hash differs", changed.Path)
		}
	}
	return nil
}

// scopeBinding conservatively hashes all bytes under declared writer scopes.
// Whole-repository ownership uses the complete project's source identity. This
// is a freshness check, not process isolation or change attribution.
func scopeBinding(snapshot projectctx.Snapshot, owned []string) (string, error) {
	if slices.Contains(owned, ".") {
		return strings.TrimPrefix(snapshot.Fingerprint, "sha256:"), nil
	}
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	type entry struct {
		Path   string      `json:"path"`
		Kind   string      `json:"kind"`
		SHA256 string      `json:"sha256"`
		Mode   fs.FileMode `json:"mode"`
	}
	entries := []entry{}
	var total int
	for _, scope := range owned {
		if _, err := inspectWorkerPath(root, scope, true); err != nil {
			return "", err
		}
		if _, err := root.Lstat(scope); errors.Is(err, fs.ErrNotExist) {
			entries = append(entries, entry{Path: scope, Kind: "missing"})
			continue
		} else if err != nil {
			return "", err
		}
		err = fs.WalkDir(root.FS(), scope, func(name string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if len(entries) >= 30000 {
				return errors.New("worker ownership scope exceeds the 30000 entry bound")
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			kind, hash := "directory", ""
			if !item.IsDir() {
				data, err := readWorkerFile(root, name)
				if err != nil {
					return err
				}
				total += len(data)
				if total > 64<<20 {
					return errors.New("worker ownership scope exceeds 64 MiB")
				}
				kind, hash = "file", digest(data)
			}
			entries = append(entries, entry{Path: name, Kind: kind, SHA256: hash, Mode: info.Mode()})
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("conservative worker scope binding: %w; use exact source-file scopes or '.' for the complete eligible project snapshot when a subtree contains generated files or symlinks", err)
		}
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.Path, b.Path) })
	data, err := encode(entries)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

// A review binds the other worker records and all check states present when it
// was submitted. Its own immutable assignment/result and coordinator acceptance
// are excluded so recording the review does not immediately invalidate itself.
func reviewEvidenceBinding(record Record, ownID string) (string, error) {
	assignments := []WorkerAssignment{}
	results := []BoundWorkerResult{}
	acceptances := []BoundWorkerAcceptance{}
	for _, value := range record.Assignments {
		if value.Brief.ID != ownID {
			assignments = append(assignments, value)
		}
	}
	for _, value := range record.WorkerResults {
		if value.Result.AssignmentID != ownID {
			results = append(results, value)
		}
	}
	for _, value := range record.WorkerAcceptances {
		if value.Acceptance.AssignmentID != ownID {
			acceptances = append(acceptances, value)
		}
	}
	data, err := encode(struct {
		Checks      []CheckRun              `json:"checks"`
		Assignments []WorkerAssignment      `json:"assignments"`
		Results     []BoundWorkerResult     `json:"worker_results"`
		Acceptances []BoundWorkerAcceptance `json:"worker_acceptances"`
	}{record.Checks, assignments, results, acceptances})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func validateLiveWorkerRefs(snapshot projectctx.Snapshot, result WorkerResult) error {
	if len(result.Evidence) > 0 {
		if err := validateWorkerRefs(snapshot, result.Evidence); err != nil {
			return err
		}
	}
	for _, finding := range result.Findings {
		if err := validateWorkerRefs(snapshot, finding.Evidence); err != nil {
			return err
		}
	}
	return validateChangedFiles(snapshot, result.ChangedFiles)
}

func SubmitWorkerResult(ctx context.Context, taskPath string, input WorkerResult) (Record, error) {
	return update(taskPath, func(root *os.Root, record *Record) error {
		assigned := findAssignment(*record, input.AssignmentID)
		if assigned == nil {
			return errors.New("worker result references an unknown assignment")
		}
		if findWorkerResult(*record, input.AssignmentID) != nil {
			return errors.New("assignment already has an immutable terminal result; use a new assignment ID")
		}
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if input, err = completeWorkerResult(*record, *assigned, snapshot, input); err != nil {
			return err
		}
		if err := validateResult(*record, *assigned, input); err != nil {
			return err
		}
		if !equalSnapshot(snapshot, input.Snapshot) {
			return errors.New("worker result snapshot differs from actual current source")
		}
		if input.Status == "completed" {
			if err := validateWorkerOwnership(*record, snapshot, assigned.Brief); err != nil {
				return err
			}
		}
		if err := validateLiveWorkerRefs(snapshot, input); err != nil {
			return err
		}
		runs, err := workerChecks(*record, snapshot, input.CheckIDs, input.Status == "completed")
		if err != nil {
			return err
		}
		for _, id := range runs {
			if err := workerCheckArtifact(root, *record, id); err != nil {
				return err
			}
		}
		bound := BoundWorkerResult{Result: input, CheckRuns: runs, SubmittedAt: time.Now().UTC()}
		if input.Status == "completed" {
			bound.ScopeSHA256, err = scopeBinding(snapshot, assigned.Brief.OwnedPaths)
		} else {
			bound.ScopeSHA256 = strings.TrimPrefix(snapshot.Fingerprint, "sha256:")
		}
		if err != nil {
			return err
		}
		if assigned.Brief.Role == "review" {
			bound.ReviewEvidenceSHA256, err = reviewEvidenceBinding(*record, input.AssignmentID)
			if err != nil {
				return err
			}
		}
		bound.SHA256, err = workerResultHash(bound)
		if err != nil {
			return err
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while recording worker result")
		}
		if input.Status == "completed" {
			if err := validateWorkerOwnership(*record, after, assigned.Brief); err != nil {
				return err
			}
		}
		record.WorkerResults = append(record.WorkerResults, bound)
		record.EvidenceRevision++
		record.Verification = nil
		return nil
	})
}

func validateAcceptance(record Record, result BoundWorkerResult, input WorkerAcceptance) error {
	if input.Evidence == nil || input.CheckIDs == nil || input.Findings == nil {
		return errors.New("worker acceptance evidence, check_ids and findings must be explicit arrays; use [] when empty")
	}
	if input.TaskID != record.ID || input.AssignmentID != result.Result.AssignmentID || input.ResultSHA256 != result.SHA256 || input.CoordinatorID != record.Plan.CoordinatorID || !boundedText(input.Reason) || !slices.Contains([]string{"accepted", "unused", "rejected"}, input.Status) {
		return errors.New("worker acceptance needs the exact task/result hash, declared coordinator, accepted/unused/rejected status and reason")
	}
	if input.Status == "accepted" && result.Result.Status != "completed" {
		return errors.New("blocked or cancelled handoffs cannot be accepted as completed work; use unused with an explicit rationale")
	}
	if err := refsShape(input.Evidence, input.Status == "accepted"); err != nil {
		return err
	}
	if !uniqueIDs(input.CheckIDs) || len(input.CheckIDs) > 256 {
		return errors.New("acceptance check_ids must be distinct planned IDs")
	}
	for _, id := range input.CheckIDs {
		if !slices.ContainsFunc(record.Plan.Checks, func(check Check) bool { return check.ID == id }) {
			return fmt.Errorf("acceptance references unknown check %s", id)
		}
	}
	if len(input.Findings) != len(result.Result.Findings) {
		return errors.New("coordinator must explicitly dispose every worker finding")
	}
	seen := map[string]bool{}
	for _, disposition := range input.Findings {
		if disposition.Evidence == nil {
			return errors.New("finding disposition evidence must be an explicit array; use [] when empty")
		}
		if seen[disposition.FindingID] || !slices.ContainsFunc(result.Result.Findings, func(finding WorkerFinding) bool { return finding.ID == disposition.FindingID }) || !slices.Contains([]string{"accepted", "rejected", "resolved", "deferred"}, disposition.Status) || !boundedText(disposition.Reason) {
			return errors.New("finding dispositions need distinct known IDs, accepted/rejected/resolved/deferred status and reason")
		}
		seen[disposition.FindingID] = true
		if err := refsShape(disposition.Evidence, disposition.Status != "accepted"); err != nil {
			return err
		}
	}
	return nil
}

func workerResultFresh(record Record, assigned WorkerAssignment, bound BoundWorkerResult, snapshot projectctx.Snapshot) error {
	if err := validateWorkerOwnership(record, snapshot, assigned.Brief); err != nil {
		return err
	}
	if assigned.Brief.Role == "review" {
		if !equalSnapshot(snapshot, bound.Result.Snapshot) {
			return errors.New("review does not cover the entire current source snapshot")
		}
		binding, err := reviewEvidenceBinding(record, assigned.Brief.ID)
		if err != nil {
			return err
		}
		if binding != bound.ReviewEvidenceSHA256 {
			return errors.New("review is stale against worker or check evidence; assign a fresh review")
		}
	} else {
		if snapshot.Head != bound.Result.Snapshot.Head || snapshot.Branch != bound.Result.Snapshot.Branch {
			return errors.New("worker handoff Git identity is stale")
		}
		current, err := scopeBinding(snapshot, assigned.Brief.OwnedPaths)
		if err != nil {
			return err
		}
		if current != bound.ScopeSHA256 {
			return errors.New("worker handoff ownership scope is stale")
		}
	}
	return validateLiveWorkerRefs(snapshot, bound.Result)
}

// AcceptWorkerResult records the coordinator's judgment of one exact immutable
// result. Rejudgment appends another acceptance. Unused handoffs require a reason
// and never satisfy a completed-work or independent-review gate.
func AcceptWorkerResult(ctx context.Context, taskPath string, input WorkerAcceptance) (Record, error) {
	return update(taskPath, func(root *os.Root, record *Record) error {
		result := findWorkerResult(*record, input.AssignmentID)
		if result == nil {
			return errors.New("acceptance references a missing worker result; record a terminal handoff first")
		}
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if input, err = completeAcceptance(*record, *result, snapshot, input); err != nil {
			return err
		}
		if err := validateAcceptance(*record, *result, input); err != nil {
			return err
		}
		if len(record.WorkerAcceptances) >= 1024 {
			return errors.New("task worker acceptance limit reached")
		}
		if input.Status == "accepted" {
			assigned := findAssignment(*record, input.AssignmentID)
			if err := workerResultFresh(*record, *assigned, *result, snapshot); err != nil {
				return err
			}
			for _, id := range result.CheckRuns {
				if err := workerCheckArtifact(root, *record, id); err != nil {
					return err
				}
			}
		}
		if len(input.Evidence) > 0 {
			if err := validateWorkerRefs(snapshot, input.Evidence); err != nil {
				return err
			}
		}
		for _, disposition := range input.Findings {
			if len(disposition.Evidence) > 0 {
				if err := validateWorkerRefs(snapshot, disposition.Evidence); err != nil {
					return err
				}
			}
		}
		runs, err := bindChecks(*record, snapshot, input.CheckIDs)
		if err != nil {
			return err
		}
		for _, id := range runs {
			if err := checkStoredRun(root, *record, snapshot, id); err != nil {
				return err
			}
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while accepting worker result")
		}
		if input.Status == "accepted" {
			if err := workerResultFresh(*record, *findAssignment(*record, input.AssignmentID), *result, after); err != nil {
				return err
			}
		}
		record.WorkerAcceptances = append(record.WorkerAcceptances, BoundWorkerAcceptance{Acceptance: input, Snapshot: snapshot, CheckRuns: runs, AcceptedAt: time.Now().UTC()})
		record.EvidenceRevision++
		record.Verification = nil
		return nil
	})
}

func workerCheckArtifact(root *os.Root, record Record, id string) error {
	for _, run := range record.Checks {
		if run.ID != id {
			continue
		}
		data, err := privateRead(root, run.Log, MaxLogBytes)
		if err != nil {
			return err
		}
		if run.FinishedAt == nil || !validSHA256(run.LogSHA256) || digest(data) != run.LogSHA256 {
			return errors.New("worker check artifact hash or completion metadata differs")
		}
		return nil
	}
	return errors.New("worker check run is missing")
}

func independentReviewer(record Record, assigned WorkerAssignment) bool {
	if assigned.Brief.AgentID == record.Plan.CoordinatorID {
		return false
	}
	for _, other := range record.Assignments {
		if other.Brief.Role == "implementation" && other.Brief.AgentID == assigned.Brief.AgentID {
			return false
		}
	}
	return true
}

// A retired finding is obsolete only when at least one cited source has
// demonstrably changed. Unavailable source is unknown, not proof of retirement.
func findingSourceObsolete(snapshot projectctx.Snapshot, refs []SourceRef) bool {
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return false
	}
	defer root.Close()
	for _, ref := range refs {
		if _, err := inspectWorkerPath(root, ref.Path, true); err != nil {
			continue
		}
		info, err := root.Lstat(ref.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			return true
		}
		data, err := readWorkerFile(root, ref.Path)
		if err == nil && digest(data) != ref.SHA256 {
			return true
		}
	}
	return false
}

// deferredFindingLimitations lists every deferred disposition of each
// assignment's latest acceptance. A deferral is a disclosed limitation, not a
// problem; its evidence stays subject to the same freshness rule as a
// resolution.
func deferredFindingLimitations(record Record) []string {
	limitations := []string{}
	for _, assigned := range record.Assignments {
		accepted := latestAcceptance(record, assigned.Brief.ID)
		if accepted == nil {
			continue
		}
		for _, disposition := range accepted.Acceptance.Findings {
			if disposition.Status == "deferred" {
				limitations = append(limitations, fmt.Sprintf("worker assignment %s finding %s deferred: %s", assigned.Brief.ID, disposition.FindingID, disposition.Reason))
			}
		}
	}
	return limitations
}

func workerEvidenceProblems(root *os.Root, record Record, snapshot projectctx.Snapshot) []string {
	problems := []string{}
	currentReview := false
	for _, assigned := range record.Assignments {
		result := findWorkerResult(record, assigned.Brief.ID)
		if result == nil {
			problems = append(problems, fmt.Sprintf("worker assignment %s has no terminal handoff", assigned.Brief.ID))
			continue
		}
		accepted := latestAcceptance(record, assigned.Brief.ID)
		if accepted == nil || accepted.Acceptance.Status == "rejected" {
			problems = append(problems, fmt.Sprintf("worker assignment %s lacks coordinator acceptance or explicit unused rationale", assigned.Brief.ID))
			continue
		}
		unresolved := false
		for _, disposition := range accepted.Acceptance.Findings {
			if disposition.Status == "accepted" {
				unresolved = true
				currentFinding := true
				if accepted.Acceptance.Status == "unused" {
					for _, finding := range result.Result.Findings {
						if finding.ID == disposition.FindingID {
							currentFinding = !findingSourceObsolete(snapshot, finding.Evidence)
							break
						}
					}
				}
				if currentFinding {
					problems = append(problems, fmt.Sprintf("worker assignment %s finding %s remains acknowledged but unresolved", assigned.Brief.ID, disposition.FindingID))
				}
			}
		}
		if accepted.Acceptance.Status == "unused" {
			continue
		}
		if result.Result.Status != "completed" {
			problems = append(problems, fmt.Sprintf("worker assignment %s is not completed", assigned.Brief.ID))
			continue
		}
		fresh := workerResultFresh(record, assigned, *result, snapshot)
		if fresh == nil {
			if len(accepted.Acceptance.Evidence) > 0 {
				fresh = validateWorkerRefs(snapshot, accepted.Acceptance.Evidence)
			}
			for _, disposition := range accepted.Acceptance.Findings {
				if fresh == nil && len(disposition.Evidence) > 0 {
					fresh = validateWorkerRefs(snapshot, disposition.Evidence)
				}
			}
		}
		// Old reviews remain a truthful historical record; only a fresh review
		// can discharge the current gate. They need not be deleted or rewritten.
		if fresh != nil && assigned.Brief.Role != "review" {
			problems = append(problems, fmt.Sprintf("worker assignment %s: %v", assigned.Brief.ID, fresh))
		}
		for _, id := range result.CheckRuns {
			if err := workerCheckArtifact(root, record, id); err != nil {
				problems = append(problems, fmt.Sprintf("worker assignment %s: %v", assigned.Brief.ID, err))
			}
		}
		for _, id := range accepted.CheckRuns {
			if err := workerCheckArtifact(root, record, id); err != nil {
				problems = append(problems, fmt.Sprintf("worker acceptance %s: %v", assigned.Brief.ID, err))
			}
		}
		if assigned.BundleFile != "" {
			if err := reviewBundleArtifact(root, assigned); err != nil {
				problems = append(problems, fmt.Sprintf("worker assignment %s: %v", assigned.Brief.ID, err))
			}
		}
		if assigned.Brief.Role == "review" && !unresolved && len(assigned.Brief.OwnedPaths) == 1 && assigned.Brief.OwnedPaths[0] == "." && independentReviewer(record, assigned) && fresh == nil {
			currentReview = true
		}
	}
	if record.Plan.Kind == "substantial" && !equalSnapshot(record.Baseline, snapshot) && !currentReview {
		problems = append(problems, "changed substantial task requires an accepted completed independent review at the entire current source (owned_paths ['.']; reviewer differs from coordinator and all implementation workers)")
	}
	return problems
}
