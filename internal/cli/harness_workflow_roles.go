package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

type workflowReturn struct {
	Status    string                     `json:"status"`
	Note      string                     `json:"note"`
	Plan      *harness.WorkflowPlan      `json:"plan,omitempty"`
	Assertion *harness.WorkflowAssertion `json:"assertion,omitempty"`
}

func workflowSchema(role string) json.RawMessage {
	if role == "reviewer" {
		return json.RawMessage(`{"type":"object","properties":{"decision":{"type":"string","enum":["approve","findings"]},"note":{"type":"string"},"findings":{"type":"array","items":{"type":"string"}}},"required":["decision","note","findings"],"additionalProperties":false}`)
	}
	properties := `"status":{"type":"string","enum":["complete","blocked","test_defect"]},"note":{"type":"string"}`
	required := `"status","note"`
	switch role {
	case "architect":
		properties += `,"plan":{"type":"object","properties":{
			"code_paths":{"type":"array","items":{"type":"string"}},
			"test_paths":{"type":"array","items":{"type":"string"}},
			"baseline":{"type":"string","enum":["pass","fail"]},
			"shared":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"first":{"type":"string"},"last":{"type":"string"}},"required":["path","first","last"],"additionalProperties":false}}
		},"required":["code_paths","test_paths","baseline","shared"],"additionalProperties":false}`
		required += `,"plan"`
	case "quality-engineer":
		properties += `,"assertion":{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"}},"expect":{"type":"string"},"test_id":{"type":"string"}},"required":["argv","expect","test_id"],"additionalProperties":false}`
		required += `,"assertion"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}

func validateWorkflowSkills(settings roles.SpecialistSettings) error {
	for _, ref := range settings.Skills {
		current, err := skills.ReadMetadata(ref.Path, settings.Host)
		if err != nil || current.SHA256 != ref.SHA256 {
			return failf("saved skill %s changed or is unavailable; restore the selected skill before resuming", ref.ID)
		}
	}
	return nil
}

func workflowAssignment(ctx context.Context, s *session, task *harness.Task, v harnessView) (string, error) {
	w, a := task.Workflow, task.Workflow.Active
	var instructions string
	switch a.Role {
	case "architect":
		instructions = "Inspect the current repository and design the requested change. Write no files or index entries. Return the design and acceptance criteria in note. Choose explicit repository-relative code_paths and test_paths from actual project conventions. Use fail baseline for a demonstrated defect or missing behavior, pass for a refactor or test-only work. For a path shared by tests and production, record its exact, unique first and last test boundary lines. Report blocked if a scope decision is required."
	case "quality-engineer":
		instructions = "Write or select tests for the saved acceptance criteria. Change only allowed test paths and regions. Leave HEAD and the index untouched. Return a direct executable and arguments in assertion.argv, an observable assertion.expect, and the reviewed test ID in assertion.test_id for a failing baseline (otherwise empty). Kitsu runs and records the checks."
	case "reviewer":
		instructions = "Review the candidate read-only. Check the requested behavior and affected callers. Return approve or findings. Prefix each finding with test:, code:, or design: and include the relevant location."
	case "developer":
		instructions = "Implement or repair within the saved code paths. Preserve all tests, pre-existing unrelated work, HEAD, and the index. If a test needs correction, return test_defect with evidence before making changes. Kitsu runs verification and performs any authorized commit."
	}
	input := map[string]any{
		"task": task.ID, "attempt": a.ID, "candidate": a.Candidate, "request": task.Request,
		"scope": task.Scope, "plan": w.Plan, "intake_assertion": w.Intake, "assertion": w.Test,
		"reviewed_checks": s.workflowRunner.Offer().Policy.Checks, "test_paths": w.TestPolicy,
		"code_paths": s.workflowRunner.Offer().Policy.CodePaths,
	}
	if plan := task.Latest("plan"); plan != nil {
		input["design"] = plan.Note
	}
	if review := task.Latest("review"); review != nil && len(review.Findings) > 0 {
		input["findings"] = review.Findings
	}
	if failed := task.Proof; failed != nil && (failed.FailedAssertion() != nil || failed.FailedCheck != "") {
		input["failed_assertion"] = failed
	}
	if len(w.Baselines) > 0 {
		input["last_baseline"] = w.Baselines[len(w.Baselines)-1]
	}
	if a.Role == "reviewer" {
		input["verification"] = w.Verification
		if _, err := reservedReviewComparison(ctx, s, task.Scope, nil, v.Candidate, input); err != nil {
			return "", err
		}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	instructions += "\nUse only your assigned role. The runner supplies the approved profile and selected skills.\n"
	return fmt.Sprintf("%s\nContext: %s\nReturn only the structured role result.", instructions, payload), nil
}

func workflowUsedSession(task *harness.Task, id string) bool {
	return task.UsedSession(id) || slices.ContainsFunc(task.Workflow.Attempts, func(attempt harness.WorkflowAttempt) bool {
		return attempt.Session == id
	})
}

func routeWorkflowFindings(task *harness.Task, findings []string) error {
	if len(findings) == 0 || task.Scope.Until == "review" {
		return nil
	}
	if (task.Scope.Entry == "verify" || task.Scope.Entry == "review") && !task.Scope.Repair {
		return failf("review found defects. Authorize repair with kitsu task extend %s --repair before retrying.", task.ID)
	}
	var actions []string
	for _, finding := range findings {
		kind, _, _ := strings.Cut(finding, ":")
		switch kind {
		case "test":
			actions = append(actions, "tests")
		case "code":
			if task.Scope.Entry == "tests" {
				return failf("review found a production defect outside the test-only scope")
			}
			actions = append(actions, "repair")
		case "design":
			actions = append(actions, "plan", "tests")
			if task.Scope.Entry != "tests" {
				actions = append(actions, "repair")
			}
		default:
			return failf("review finding needs an explicit test, code, or design classification")
		}
	}
	task.Workflow.Pending = workflowCorrections(task.Workflow.Pending, actions...)
	return nil
}

func workflowCorrections(prior []string, add ...string) []string {
	var result []string
	for _, action := range []string{"plan", "tests", "repair"} {
		if slices.Contains(prior, action) || slices.Contains(add, action) {
			result = append(result, action)
		}
	}
	return result
}

func workflowEvent(a *harness.WorkflowAttempt, c harness.Snapshot, note string) harness.Event {
	return harness.Event{Action: a.Action, Actor: a.Session, Ticket: a.Ticket, Note: note, Candidate: c, At: time.Now().UTC()}
}

func completeWorkflowFailure(l *stageLedger, task *harness.Task, problem string) error {
	task.Workflow.Problem = problem
	finishWorkflowAttempt(task, "failed")
	return errors.Join(l.write(harnessFile(task.ID), task), failf("%s", problem))
}

func decodeWorkflowJSON(data []byte, into any) error {
	if len(data) > 64<<10 {
		return errors.New("workflow result exceeds its size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("workflow result must be one JSON object")
	}
	return nil
}
