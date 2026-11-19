package harness

import (
	"fmt"
	"slices"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

// Workflow pins the role assignments at intake. Attempts and baseline evidence
// are kept separately from the proof of the delivered candidate.
type Workflow struct {
	LegacyApprovedArgv string `json:"approved_assertion,omitempty"` // Archives from the local launcher.

	Roles          map[string]roles.SpecialistSettings `json:"roles"`
	TestPolicy     []string                            `json:"test_policy,omitempty"`
	Plan           *WorkflowPlan                       `json:"plan,omitempty"`
	Test           *WorkflowAssertion                  `json:"assertion,omitempty"`
	Intake         *WorkflowAssertion                  `json:"intake_assertion,omitempty"`
	Authority      string                              `json:"runner_authority,omitempty"`
	Policy         string                              `json:"runner_policy,omitempty"`
	Profile        string                              `json:"runner_profile,omitempty"`
	Verification   *WorkflowEvidence                   `json:"runner_verification,omitempty"`
	Reviewed       *WorkflowEvidence                   `json:"runner_review,omitempty"`
	Baselines      []Proof                             `json:"baselines,omitempty"`
	BaselineReady  bool                                `json:"baseline_ready,omitempty"`
	PreCode        *Snapshot                           `json:"pre_code,omitempty"`
	PreCodeSources map[string]string                   `json:"pre_code_sources,omitempty"`
	Includes       []string                            `json:"commit_includes,omitempty"`
	Candidate      *Snapshot                           `json:"accepted_candidate,omitempty"`
	Pending        []string                            `json:"pending_corrections,omitempty"`
	Attempts       []WorkflowAttempt                   `json:"attempts,omitempty"`
	Active         *WorkflowAttempt                    `json:"active,omitempty"`
	Rework         int                                 `json:"rework"`
	ReworkMax      int                                 `json:"rework_limit"`
	Problem        string                              `json:"problem,omitempty"`
}

// A shared file has an explicit protected test region, identified by unique
// boundary lines. Neither a filename extension nor a language parser is used.
type WorkflowTestRegion struct {
	Path  string `json:"path"`
	First string `json:"first"`
	Last  string `json:"last"`
}

type WorkflowPlan struct {
	CodePaths []string             `json:"code_paths"`
	TestPaths []string             `json:"test_paths"`
	Shared    []WorkflowTestRegion `json:"shared"`
	Baseline  string               `json:"baseline"`
}

type WorkflowAssertion struct {
	LegacyFailureMarker string   `json:"failure_marker,omitempty"`
	Argv                []string `json:"argv"`
	Expect              string   `json:"expect"`
	TestID              string   `json:"test_id,omitempty"`
}

type WorkflowEvidence struct {
	Attempt   string   `json:"attempt"`
	Job       string   `json:"job"`
	Candidate Snapshot `json:"candidate"`
	Checks    []string `json:"checks,omitempty"`
}

type WorkflowAttempt struct {
	LegacyNative         bool                                 `json:"native,omitempty"`
	LegacyNativePrepared bool                                 `json:"native_prepared,omitempty"`
	Binding              string                               `json:"runner_binding,omitempty"`
	Job                  string                               `json:"runner_job,omitempty"`
	Import               string                               `json:"import_candidate,omitempty"`
	Checks               []string                             `json:"runner_checks,omitempty"`
	ID                   string                               `json:"id"`
	Role                 string                               `json:"role"`
	Host                 string                               `json:"host"`
	Action               string                               `json:"action"`
	Ticket               string                               `json:"ticket"`
	Owner                string                               `json:"owner,omitempty"`
	Session              string                               `json:"session,omitempty"`
	ObservedModel        string                               `json:"observed_model,omitempty"`
	ObservedEffort       string                               `json:"observed_effort,omitempty"`
	Candidate            Snapshot                             `json:"candidate"`
	Sources              map[string]string                    `json:"sources,omitempty"`
	Shared               map[string]WorkflowSharedFingerprint `json:"shared,omitempty"`
	Started              time.Time                            `json:"started"`
	Elapsed              time.Duration                        `json:"elapsed_ns,omitempty"`
	Outcome              string                               `json:"outcome,omitempty"`
	Usage                map[string]int64                     `json:"usage,omitempty"`
	Rework               bool                                 `json:"rework,omitempty"`
}

type WorkflowSharedFingerprint struct {
	Outside string `json:"outside"`
	Inside  string `json:"inside"`
}

func (w *Workflow) Valid() bool {
	for _, role := range []string{"lookup", "spec-writer", "architect", "quality-engineer", "developer", "reviewer"} {
		if _, ok := w.Roles[role]; !ok {
			return false
		}
	}
	for role, assignment := range w.Roles {
		if !roles.Known(role) || !slices.Contains(roles.Hosts, assignment.Host) {
			return false
		}
	}
	return w.Rework >= 0 && w.ReworkMax > 0
}

func ValidateWorkflowScope(scope Scope) error {
	if scope.Entry != "tests" {
		return scope.Validate()
	}
	if scope.Until != "verify" && scope.Until != "deliver" || scope.Repair || scope.ReviewBase != "" || scope.ReviewHead != "" {
		return fmt.Errorf("test-only work requires --until verify or deliver and permits only test changes")
	}
	return scope.validateDelivery()
}

func WorkflowRole(action string) string {
	switch action {
	case "plan":
		return "architect"
	case "tests":
		return "quality-engineer"
	case "implement", "repair":
		return "developer"
	case "review":
		return "reviewer"
	default:
		return ""
	}
}

func (w *Workflow) HasCorrection(action string) bool { return slices.Contains(w.Pending, action) }

func (w *Workflow) ResolveCorrection(action string) {
	w.Pending = slices.DeleteFunc(w.Pending, func(value string) bool { return value == action })
}

func (w *Workflow) IsRework(action string) bool {
	return action == "repair" || action == "tests" && w.HasCorrection(action)
}

func (t *Task) workflowNext(state Readiness) string {
	w := t.Workflow
	if w.Authority == "" || w.Problem != "" || t.NeedsRepairAuthority(state.Candidate) {
		return "blocked"
	}
	if t.Scope.Until == "plan" {
		if w.Plan == nil {
			return "plan"
		}
		return "done"
	}
	if next := t.workflowReviewEntry(state); next != "" {
		return next
	}
	needsCode := t.workflowNeedsCode()
	testOnly := t.Scope.Entry == "tests"
	if needsCode || testOnly {
		if next := t.workflowCodingAction(state, needsCode); next != "" {
			return next
		}
	}
	if !state.Verified {
		return "verify"
	}
	if t.requiresReview(state) {
		if !state.Reviewed {
			return "review"
		}
		if t.Scope.Until == "review" {
			return "done"
		}
		if review := t.Latest("review"); review != nil && len(review.Findings) > 0 {
			return "blocked"
		}
	}
	return t.deliveryAction(state.Candidate, state.Committed)
}

func (t *Task) workflowReviewEntry(state Readiness) string {
	if t.Scope.Entry != "review" {
		return ""
	}
	if t.Scope.Until == "review" {
		if state.Reviewed {
			return "done"
		}
		return "review"
	}
	if t.Latest("review") == nil {
		return "review"
	}
	return ""
}

func (t *Task) workflowNeedsCode() bool {
	if t.Scope.Entry == "plan" || t.Scope.Entry == "implement" {
		return true
	}
	if !t.Scope.Repair {
		return false
	}
	w := t.Workflow
	failed := t.Proof != nil && (t.Proof.FailedAssertion() != nil || t.Proof.FailedCheck != "")
	return failed || w.Plan != nil || len(w.Pending) > 0
}

func (t *Task) workflowCodingAction(state Readiness, needsCode bool) string {
	w := t.Workflow
	switch {
	case w.Plan == nil || w.HasCorrection("plan"):
		return "plan"
	case w.Test == nil || t.Latest("tests") == nil || w.HasCorrection("tests"):
		return "tests"
	case !w.BaselineReady:
		return "baseline"
	case !needsCode:
		return ""
	case t.Latest("implement", "repair") == nil:
		if t.Scope.Entry == "verify" || t.Scope.Entry == "review" {
			return "repair"
		}
		return "implement"
	case w.HasCorrection("repair") || t.needsCorrection(state.Candidate):
		return "repair"
	}
	return ""
}
