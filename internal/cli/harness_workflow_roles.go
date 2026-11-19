package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/onboarding"
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
		properties += `,"assertion":{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"}},"expect":{"type":"string"},"failure_marker":{"type":"string"}},"required":["argv","expect","failure_marker"],"additionalProperties":false}`
		required += `,"assertion"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}

func reserveWorkflowAttempt(ctx context.Context, s *session, l *stageLedger, task *harness.Task, step workflowStep, o workflowOptions, role string) (workflowStep, error) {
	w, v := task.Workflow, step.view
	settings := w.Roles[role]
	if err := validateWorkflowSkills(settings); err != nil {
		return blockedWorkflowStep(step, err.Error()), nil
	}
	rework := w.IsRework(v.Next)
	if rework && w.Rework >= w.ReworkMax {
		return blockedWorkflowStep(step, "Rework limit reached. Inspect the saved findings and grant more starts with --rework-limit if appropriate."), nil
	}
	attempt := &harness.WorkflowAttempt{
		ID: rand.Text(), Role: role, Host: settings.Host, Action: v.Next, Ticket: v.Ticket,
		Candidate: v.Candidate, Started: time.Now().UTC(), Native: o.nativeHost == settings.Host, Rework: rework,
	}
	if attempt.Native {
		attempt.Owner = o.actorID
	}
	if role == "quality-engineer" || role == "developer" {
		var err error
		attempt.Sources, err = stageRawSources(ctx, s, v.Candidate.Tree)
		if err != nil {
			return step, err
		}
		attempt.Shared, err = workflowSharedFingerprints(s.repo.Root, w.Plan.Shared)
		if err != nil {
			return step, err
		}
	}
	if role == "developer" && w.PreCode == nil {
		w.PreCode = &attempt.Candidate
		w.PreCodeSources = attempt.Sources
	}
	w.Active = attempt
	if rework {
		w.Rework++
	}
	step.attempt, step.settings = attempt, settings
	if role == "reviewer" && attempt.Native {
		setWorkflowReviewClaim(task, v, settings, attempt.Owner)
	}
	var err error
	step.assignment, err = workflowAssignment(ctx, s, task, v)
	if err != nil {
		return step, err
	}
	// The worktree claim must be visible before any worker can launch. A
	// crash between these writes leaves a claim with no active worker.
	if err := l.write(workflowActiveFile, workflowWorktreeClaim{Task: task.ID, Attempt: attempt.ID}); err != nil {
		return step, err
	}
	return step, l.write(harnessFile(task.ID), task)
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
	if a.Role == "reviewer" {
		if task.Claim == nil {
			return "", nil // the adapter will obtain and bind the native parent
		}
		return workflowReviewAssignment(ctx, s, task, v)
	}
	var instructions string
	switch a.Role {
	case "architect":
		instructions = "Inspect the current repository and design the requested change. Write no files or index entries. Return the design and acceptance criteria in note. Choose explicit repository-relative code_paths and test_paths from actual project conventions. Use fail baseline for a demonstrated defect or missing behavior, pass for a refactor or test-only work. For a path shared by tests and production, record its exact, unique first and last test boundary lines. Report blocked if a scope decision is required."
	case "quality-engineer":
		instructions = "Write or select tests for the saved acceptance criteria. Change only allowed test paths and regions. Leave HEAD and the index untouched. Return a direct executable and arguments in assertion.argv, an observable assertion.expect, and a specific failure_marker for a failing baseline (otherwise empty). Kitsu runs and records the checks."
	case "developer":
		instructions = "Implement or repair within the saved code paths. Preserve all tests, pre-existing unrelated work, HEAD, and the index. If a test needs correction, return test_defect with evidence before making changes. Kitsu runs verification and performs any authorized commit."
	}
	input := map[string]any{
		"task": task.ID, "attempt": a.ID, "candidate": a.Candidate, "request": task.Request,
		"scope": task.Scope, "plan": w.Plan, "assertion": w.Test, "skills": w.Roles[a.Role].Skills,
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
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	instructions += "\nUse only this role. Load selected skills from their saved paths. Use existing host permissions.\n"
	if a.Native {
		return fmt.Sprintf("%s\nContext: %s\nYour final message must be only {\"task\":%q,\"attempt\":%q,\"result\":{...}}. The nested result schema is %s.",
			instructions, payload, task.ID, a.ID, workflowSchema(a.Role)), nil
	}
	return fmt.Sprintf("%s\nContext: %s\nReturn only the structured role result.", instructions, payload), nil
}

func setWorkflowReviewClaim(task *harness.Task, v harnessView, settings roles.SpecialistSettings, parent string) {
	task.ReviewStarts = task.StartedReviews() + 1
	task.Claim = &harness.Claim{
		ReviewID: rand.Text(), Candidate: &v.Candidate, Reviewer: &settings,
		Ticket: v.Ticket, Owner: parent,
	}
	task.Workflow.Active.Owner = parent
}

func workflowReviewAssignment(ctx context.Context, s *session, task *harness.Task, v harnessView) (string, error) {
	input := map[string]any{"task": task.ID, "request": task.Request, "candidate": v.Candidate,
		"scope": task.Scope, "reviewer": task.Claim.Reviewer, "verification": task.Proof}
	if plan := task.Latest("plan"); plan != nil {
		input["design"] = plan.Note
	}
	guidance, err := reservedReviewComparison(ctx, s, task.Scope, nil, v.Candidate, input)
	if err != nil {
		return "", err
	}
	addNativeReviewInput(input, task, v.Candidate, "Review the exact candidate read-only."+guidance+
		" Prefix each finding with test:, code:, or design: to name the role that should address it.")
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	if task.Workflow.Active.Native {
		return string(data), nil
	}
	return "Your role is a native review parent. Start exactly one fresh read-only reviewer with the configured reviewer profile and wait for it. " +
		"Give it the following entire assignment. Its only final message must be the specified native_review_result JSON. " +
		"The Kitsu native hooks retain its result. Finish without writing any repository files.\n" + string(data), nil
}

func launchWorkflowAttempt(ctx context.Context, s *session, o workflowOptions, step workflowStep) error {
	a, settings := step.attempt, step.settings
	if !step.deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, step.deadline)
		defer cancel()
	}
	fmt.Fprintf(s.cfg.Err, "Kitsu task %s · %s on %s\n", o.id, a.Role, a.Host)
	worker := onboarding.WorkflowWorker{
		Host: a.Host, Root: s.repo.Root, Role: a.Role, Prompt: step.assignment,
		Model: settings.Model.Model, Effort: settings.Model.Effort,
		ReadOnly: a.Role == "reviewer" || a.Role == "architect",
		Schema:   workflowSchema(a.Role),
		Scratch:  s.repo.GitDir,
	}
	if a.Role == "reviewer" {
		worker.ReviewPrompt = func(parent string) (string, error) {
			return workflowReviewParent(ctx, s, o.id, a.ID, parent)
		}
	} else {
		worker.OnSession = func(id string) error {
			return workflowLinkSession(ctx, s, o.id, a.ID, id)
		}
	}
	result, runErr := onboarding.RunWorkflowWorker(ctx, worker)
	// Cancellation has already stopped the process. Use a short separate
	// window to durably record that outcome before the next resume.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return saveWorkflowReturn(recordCtx, s, o.id, a.ID, result, runErr)
}

func ownedWorkflowAttempt(l *stageLedger, id, attemptID string) (*harness.Task, error) {
	task, err := readWorkflow(l, id)
	if err != nil {
		return nil, err
	}
	if task.Workflow.Active == nil || task.Workflow.Active.ID != attemptID {
		return nil, failf("workflow attempt ownership changed")
	}
	return task, nil
}

func workflowLinkSession(ctx context.Context, s *session, id, attemptID, native string) error {
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return err
	}
	defer l.close()
	task, err := ownedWorkflowAttempt(l, id, attemptID)
	if err != nil {
		return err
	}
	a := task.Workflow.Active
	if !nativeSessionID.MatchString(native) || workflowUsedSession(task, native) || a.Session != "" && a.Session != native {
		return failf("workflow attempt did not supply a fresh matching native session")
	}
	a.Session = native
	return l.write(harnessFile(id), task)
}

func workflowReviewParent(ctx context.Context, s *session, id, attemptID, parent string) (string, error) {
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return "", err
	}
	defer l.close()
	task, err := ownedWorkflowAttempt(l, id, attemptID)
	if err != nil {
		return "", err
	}
	a := task.Workflow.Active
	v, err := observeHarness(ctx, s, task, false)
	if err != nil {
		return "", err
	}
	if !nativeSessionID.MatchString(parent) || a.Role != "reviewer" || task.Claim != nil || v.Next != "review" ||
		v.Ticket != a.Ticket || v.Candidate != a.Candidate {
		return "", failf("review parent cannot bind to this candidate and reservation")
	}
	setWorkflowReviewClaim(task, v, task.Workflow.Roles["reviewer"], parent)
	prompt, err := workflowReviewAssignment(ctx, s, task, v)
	if err != nil {
		return "", err
	}
	return prompt, l.write(harnessFile(id), task)
}

func saveWorkflowReturn(ctx context.Context, s *session, id, attemptID string, result onboarding.WorkflowResult, err error) error {
	l, ledgerErr := openStageLedger(ctx, s)
	if ledgerErr != nil {
		return errors.Join(err, ledgerErr)
	}
	defer l.close()
	task, readErr := ownedWorkflowAttempt(l, id, attemptID)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	w, a := task.Workflow, task.Workflow.Active
	a.Elapsed = result.Elapsed
	a.ObservedModel, a.ObservedEffort = result.ObservedModel, result.ObservedEffort
	if usage := result.Usage; usage.Available {
		a.Usage = map[string]int64{
			"input_tokens": usage.InputTokens, "cached_input_tokens": usage.CachedInputTokens,
			"cache_creation_input_tokens": usage.CacheCreationInputTokens, "output_tokens": usage.OutputTokens,
			"reasoning_output_tokens": usage.ReasoningOutputTokens,
		}
	}
	if err != nil || !result.Terminal {
		if !result.Exited {
			return errors.Join(err, failf("attempt %s remains outstanding; confirm its worker stopped before release", a.ID))
		}
		w.Problem = fmt.Sprintf("%s launch or result failed: %v", a.Role, err)
		finishWorkflowAttempt(task, "failed")
		return errors.Join(l.write(harnessFile(id), task), failf("%s; resume from a terminal with kitsu task run --resume %s --retry", w.Problem, id))
	}
	if a.Role == "reviewer" {
		view, err := observeHarness(ctx, s, task, false)
		if err != nil {
			return err
		}
		return acceptWorkflowReview(ctx, s, l, task, view)
	}
	if a.Session == "" || a.Session != result.Session {
		return completeWorkflowFailure(l, task, "worker result does not belong to the observed native session")
	}
	return acceptWorkflowReturn(ctx, s, l, task, result.Session, result.Payload)
}

func acceptWorkflowReturn(ctx context.Context, s *session, l *stageLedger, task *harness.Task, worker string, data json.RawMessage) error {
	w, a := task.Workflow, task.Workflow.Active
	var result workflowReturn
	if !nativeSessionID.MatchString(worker) || worker == a.Owner || workflowUsedSession(task, worker) || decodeWorkflowJSON(data, &result) != nil ||
		strings.TrimSpace(result.Note) == "" || len(result.Note) > 12<<10 {
		return completeWorkflowFailure(l, task, "worker did not return a bounded result from a fresh linked session")
	}
	a.Session = worker
	if result.Status == "blocked" {
		return completeWorkflowFailure(l, task, result.Note)
	}
	c, err := harnessCandidate(ctx, s, task)
	if err != nil {
		return err
	}
	if result.Status == "test_defect" && a.Role == "developer" && c == a.Candidate {
		w.Pending = workflowCorrections(w.Pending, "tests", "repair")
		finishWorkflowAttempt(task, "test_defect")
		task.Events = append(task.Events, workflowEvent(a, c, result.Note))
		return l.write(harnessFile(task.ID), task)
	}
	if result.Status != "complete" {
		return completeWorkflowFailure(l, task, "worker could not complete its assignment without changing scope")
	}
	if err := validateWorkflowReturn(ctx, s, task, c, result); err != nil {
		return completeWorkflowFailure(l, task, err.Error())
	}
	if a.Role == "architect" {
		w.Plan = result.Plan
	} else if a.Role == "quality-engineer" {
		w.Test, w.BaselineReady = result.Assertion, false
	}
	w.Candidate = &c
	w.ResolveCorrection(a.Action)
	task.Events = append(task.Events, workflowEvent(a, c, result.Note))
	finishWorkflowAttempt(task, "complete")
	return l.write(harnessFile(task.ID), task)
}

func workflowUsedSession(task *harness.Task, id string) bool {
	return task.UsedSession(id) || slices.ContainsFunc(task.Workflow.Attempts, func(attempt harness.WorkflowAttempt) bool {
		return attempt.Session == id
	})
}

func acceptWorkflowReview(ctx context.Context, s *session, l *stageLedger, task *harness.Task, view harnessView) error {
	a := task.Workflow.Active
	if view.Candidate != a.Candidate || task.Claim == nil || view.Next != "review" || view.Ticket != a.Ticket {
		return completeWorkflowFailure(l, task, "candidate or ownership changed during the native review")
	}
	o := harnessOptions{id: task.ID, action: "review", ticket: a.Ticket, observedReview: true}
	if err := recordHarness(l, task, o, view); err != nil {
		return completeWorkflowFailure(l, task, err.Error())
	}
	review := task.Latest("review")
	a.Session = review.Actor
	if err := routeWorkflowFindings(task, review.Findings); err != nil {
		task.Workflow.Problem = err.Error()
	}
	finishWorkflowAttempt(task, "complete")
	return l.write(harnessFile(task.ID), task)
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
