package pilot

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A host-side audit is separate from model claims and functional grading. Its
// content binding prevents a review of one candidate blessing another tree.
// The solver never receives this file or the private experiment output root.
type protocolAudit struct {
	Version                 int      `json:"version"`
	Profile                 string   `json:"profile,omitempty"`
	RunID                   string   `json:"run_id"`
	CandidateTree           string   `json:"candidate_tree"`
	AssignedArm             string   `json:"assigned_arm"`
	Compliant               *bool    `json:"compliant"`
	ExecutedCompactCollect  int      `json:"executed_compact_collect"`
	ExecutedCompactCheck    int      `json:"executed_compact_check"`
	FreshWorkers            []string `json:"fresh_workers"`
	CorrectModelsAndEffort  *bool    `json:"correct_models_and_effort"`
	IndependentReview       *bool    `json:"independent_review"`
	RequiredRoleWriteChecks *bool    `json:"required_role_write_checks"`
	TestBeforeCode          *bool    `json:"test_before_code"`
	CurrentContentEvidence  *bool    `json:"current_content_check_evidence"`
	AuthorizedDispositions  *bool    `json:"authorized_dispositions"`
	EvidencePaths           []string `json:"evidence_paths"`
	Limitations             []string `json:"limitations"`
}

func TestProtocolKnownViolationDominatesMissingEvidence(t *testing.T) {
	dir := t.TempDir()
	yes, no := true, false
	result := runResult{Run: cell{ID: "case-r1-normal", Arm: "normal"}, CandidateTree: "candidate", Method: methodReport{Facts: map[string]any{}}}
	audit := protocolAudit{Version: 1, RunID: result.Run.ID, CandidateTree: result.CandidateTree, AssignedArm: "normal", Compliant: &no,
		CorrectModelsAndEffort: nil, IndependentReview: &yes, RequiredRoleWriteChecks: &yes, TestBeforeCode: &yes, CurrentContentEvidence: &yes, AuthorizedDispositions: &yes,
		FreshWorkers: []string{"quality-engineer", "developer", "reviewer", "git"}, EvidencePaths: []string{"retained-evidence"}}
	if err := writeJSON(filepath.Join(dir, "protocol.json"), audit, false); err != nil {
		t.Fatal(err)
	}
	if err := applyProtocolAudit(dir, &result); err != nil {
		t.Fatal(err)
	}
	if result.Method.Status != "violated" {
		t.Fatal("missing evidence erased an established violation", result.Method)
	}
	audit.CandidateTree = "different-candidate"
	if err := writeJSON(filepath.Join(dir, "protocol.json"), audit, false); err != nil {
		t.Fatal(err)
	}
	if err := applyProtocolAudit(dir, &result); err == nil {
		t.Fatal("protocol audit blessed different candidate content")
	}
}

func applyProtocolAudit(runDir string, result *runResult) error {
	path := filepath.Join(runDir, "protocol.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	var audit protocolAudit
	if err := readJSON(path, &audit); err != nil {
		return err
	}
	arm := audit.AssignedArm
	if arm == "off" {
		arm = "normal"
	}
	if arm == "on" {
		arm = "compact"
	}
	if audit.Version != 1 || audit.RunID != result.Run.ID || audit.CandidateTree != result.CandidateTree || arm != result.Run.Arm {
		return fmt.Errorf("protocol audit is not bound to this run and candidate: %s", result.Run.ID)
	}
	if audit.Profile != result.Profile {
		return fmt.Errorf("protocol audit profile differs from this result: %s", result.Run.ID)
	}
	status := "verified"
	unknown, violated := false, false
	for _, observed := range []*bool{audit.Compliant, audit.CorrectModelsAndEffort, audit.IndependentReview, audit.RequiredRoleWriteChecks, audit.TestBeforeCode, audit.CurrentContentEvidence, audit.AuthorizedDispositions} {
		if observed == nil {
			unknown = true
		} else if !*observed {
			violated = true
		}
	}
	if unknown {
		status = "unknown"
	}
	if violated {
		status = "violated"
	}
	for _, worker := range []string{"quality-engineer", "developer", "reviewer", "git"} {
		if !slices.Contains(audit.FreshWorkers, worker) {
			status = "violated"
		}
	}
	if result.Run.Arm == "normal" && (audit.ExecutedCompactCollect != 0 || audit.ExecutedCompactCheck != 0) {
		status = "violated"
	}
	if result.Profile == canaryProfile && audit.ExecutedCompactCollect != 0 {
		status = "violated"
	}
	if result.Run.Arm == "compact" && result.Profile == canaryProfile && result.Run.CaseID == "qs-sparse-array-limit" && audit.ExecutedCompactCheck == 0 {
		status = "violated"
	}
	if result.Run.Arm == "compact" && result.Profile != canaryProfile && (audit.ExecutedCompactCollect == 0 || audit.ExecutedCompactCheck == 0) {
		status = "violated"
	}
	if len(audit.EvidencePaths) == 0 && status == "verified" {
		status = "unknown"
	}
	digest, err := hashFile(path)
	if err != nil {
		return err
	}
	result.Protocol = &asset{path, digest}
	result.Method.Status = status
	result.Method.Facts["host_protocol_audit"] = audit
	return nil
}
