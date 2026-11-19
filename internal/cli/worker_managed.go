package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/onboarding"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

type managedStage struct {
	Profile    string `json:"profile"`
	Index      string `json:"index"`
	Head       string `json:"head"`
	MergeHeads string `json:"merge_heads"`
	Target     string `json:"commit_target"`
}

func managedProfileFile(task string) string {
	return "managed-profile-" + contextpack.Key(task) + ".json"
}

func managedProfileKey(s *session, b workerBrief) (string, error) {
	profile, err := roles.ResolveWorkflow(s.repo.Root)
	if err != nil {
		return "", err
	}
	selected, ok := profile[b.Role]
	if !ok || selected.Host != b.Host || selected.Model != b.Model ||
		contextpack.Key(selected.Skills) != contextpack.Key(b.Skills) {
		return "", fmt.Errorf("configured route for %s changed while preparing the worker", b.Role)
	}
	return contextpack.Key(profile), nil
}

func checkSavedManagedProfile(l *stageLedger, task, key string) (bool, error) {
	var saved string
	err := l.read(managedProfileFile(task), &saved)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !stageValidID(saved) || saved != key {
		return false, fmt.Errorf("configured role assignments for %s changed; restore the saved profile or choose a new task", task)
	}
	return true, nil
}

func prepareManagedStage(ctx context.Context, s *session, l *stageLedger, b workerBrief) (*managedStage, error) {
	if !b.Configured {
		_, err := checkSavedManagedProfile(l, b.Task, "")
		return nil, err
	}
	key, err := managedProfileKey(s, b)
	if err != nil {
		return nil, compactFailure(err)
	}
	if _, err := checkSavedManagedProfile(l, b.Task, key); err != nil {
		return nil, compactFailure(err)
	}
	current, err := managedGitState(ctx, s)
	if err != nil {
		return nil, err
	}
	current.Profile = key
	return &current, nil
}

func saveManagedProfile(ctx context.Context, s *session, l *stageLedger, r stageRecord) error {
	if r.Managed == nil {
		return nil
	}
	if err := validateManagedStageGit(ctx, s, r, r.Prepared.State); err != nil {
		return err
	}
	b := r.Prepared.Brief
	key, err := managedProfileKey(s, b)
	if err != nil {
		return compactFailure(err)
	}
	if key != r.Managed.Profile {
		return failf("configured role assignments changed during preparation")
	}
	found, err := checkSavedManagedProfile(l, b.Task, key)
	if err != nil || found {
		return err
	}
	return l.write(managedProfileFile(b.Task), key)
}

func managedGitState(ctx context.Context, s *session) (managedStage, error) {
	index, err := s.repo.IndexTree(ctx, s.store.Tmp())
	if err != nil {
		return managedStage{}, err
	}
	head, _, err := s.repo.CommitOf(ctx, "HEAD")
	if err != nil {
		return managedStage{}, err
	}
	merge, err := s.repo.MergeParents(ctx)
	if err != nil {
		return managedStage{}, err
	}
	target, err := s.repo.CommitTarget(ctx)
	return managedStage{Index: index, Head: head, MergeHeads: merge, Target: target}, err
}

func validateManagedStageGit(ctx context.Context, s *session, r stageRecord, state contextpack.State) error {
	want := r.Managed
	if want == nil {
		return nil
	}
	got, err := managedGitState(ctx, s)
	if err != nil {
		return err
	}
	if got.Target != want.Target {
		return errors.New("checkout branch changed during the configured stage")
	}
	if r.Prepared.Brief.Role == "git" {
		return validateManagedGitCandidate(ctx, s, r, state, got)
	}
	if got.Head != want.Head || got.MergeHeads != want.MergeHeads || got.Index != want.Index || state.Head != want.Head {
		return errors.New("HEAD, merge parents or index changed during the configured worker stage")
	}
	return nil
}

func validateConfiguredStage(b workerBrief) error {
	if !b.Configured || b.Workflow != "full" || b.Stage == "lookup" {
		return nil
	}
	report := b.State.Report
	if report == nil {
		return failf("configured full task has no OpenSpec change record")
	}
	stage := report.Stage
	allowed := false
	switch b.Stage {
	case "spec", "spec-review":
		allowed = stage == change.StageSpec
	case "design", "design-review":
		allowed = stage == change.StageDesign
	case "tests":
		allowed = stage == change.StageTests || stage == change.StageCode || stage == change.StageReview
	case "code":
		// An approved design can authorize an empty interface before tests.
		allowed = stage == change.StageTests || stage == change.StageCode || stage == change.StageReview
	case "review":
		allowed = stage == change.StageCode || stage == change.StageReview
	case "commit":
		allowed = stage == change.StageCommit && report.BrokenRules == 0
	}
	if !allowed {
		return failf("OpenSpec task %s is at %s; resolve its outstanding work and approvals before starting %s", b.Task, stage, b.Stage)
	}
	return nil
}

type managedWorkerRun struct {
	Version   int                       `json:"version"`
	Dispatch  string                    `json:"dispatch_id"`
	Host      string                    `json:"host"`
	Role      string                    `json:"role"`
	Session   string                    `json:"host_session,omitempty"`
	Reviewer  string                    `json:"review_worker,omitempty"`
	Status    string                    `json:"status"`
	Result    string                    `json:"result_path,omitempty"`
	SHA256    string                    `json:"result_sha256,omitempty"`
	Requested roles.Model               `json:"requested"`
	Usage     *onboarding.WorkflowUsage `json:"usage,omitempty"`
}

func managedRunFile(id string) string { return id + ".launch.json" }

func readManagedRun(l *stageLedger, id string, r stageRecord) (managedWorkerRun, error) {
	var run managedWorkerRun
	if err := l.read(managedRunFile(id), &run); err != nil {
		return run, err
	}
	brief := r.Prepared.Brief
	if run.Version != 1 || run.Dispatch != id || run.Host != brief.Host || run.Role != brief.Role ||
		run.Requested != brief.Model || run.Session != "" && !nativeSessionID.MatchString(run.Session) {
		return run, errors.New("configured worker execution does not match its dispatch")
	}
	switch run.Status {
	case "running", "failed", "unresolved":
		if run.Result != "" || run.SHA256 != "" || run.Reviewer != "" {
			return run, errors.New("incomplete configured worker execution contains a handback")
		}
	case "returned":
		if run.Session == "" || run.Result != filepath.Join(l.root.Name(), id+".handback.json") || !stageValidID(run.SHA256) {
			return run, errors.New("configured worker handback does not match its execution")
		}
		if err := validateManagedReviewIdentity(run); err != nil {
			return run, err
		}
	default:
		return run, errors.New("invalid configured worker execution state")
	}
	return run, nil
}

func startManagedWorker(ctx context.Context, cfg Config, s *session, id string, budget int) error {
	record, run, lock, err := reserveManagedRun(ctx, s, id, budget)
	if err != nil {
		return err
	}
	if lock == nil {
		if run.Status != "returned" {
			return failf("configured worker for %s is %s; inspect host session %q before abandoning the interval", id, run.Status, run.Session)
		}
		return printManagedRun(cfg, run, budget)
	}
	defer func() { _ = lock.Close() }()
	if err := executeManagedWorker(ctx, s, record, &run); err != nil {
		return err
	}
	return printManagedRun(cfg, run, budget)
}

func executeManagedWorker(ctx context.Context, s *session, record stageRecord, run *managedWorkerRun) error {
	scratch, err := os.MkdirTemp(s.repo.GitDir, "kitsu-managed-")
	if err != nil {
		return failManagedWorker(ctx, s, run, true, err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	d := nativeDispatchRecord(record, run.Dispatch)
	prompt := d.Native.Prompt
	if d.Role == "git" {
		prompt += "\nThe configured git assignment is read-only. Inspect the candidate, checks and intended changed paths. Report blockers. Kitsu handles any separately authorized staged commit."
	}
	workerCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	worker := onboarding.WorkflowWorker{
		Host: d.Host, Root: d.Worktree, Role: d.Role, Prompt: prompt,
		Model: d.Native.Model, Effort: d.Native.Effort, Scratch: scratch,
		ReadOnly: d.Role == "lookup" || d.Role == "reviewer" || d.Role == "git",
		Schema:   managedWorkerSchema,
		OnSession: func(id string) error {
			if run.Session != "" && run.Session != id {
				return errors.New("native host did not return the reserved review parent")
			}
			run.Session = id
			return updateManagedRun(workerCtx, s, *run, nil)
		},
	}
	if d.Role == "reviewer" {
		worker.ReviewPrompt = func(parent string) (string, error) {
			run.Session = parent
			if err := updateManagedRun(workerCtx, s, *run, nil); err != nil {
				return "", err
			}
			return managedReviewPrompt(d, record), nil
		}
	}
	result, err := onboarding.RunWorkflowWorker(workerCtx, worker)
	if err != nil {
		return failManagedWorker(ctx, s, run, result.Exited, err)
	}
	return completeManagedWorker(ctx, s, record, run, result)
}

func completeManagedWorker(ctx context.Context, s *session, record stageRecord, run *managedWorkerRun,
	result onboarding.WorkflowResult,
) error {
	if !result.Terminal || !result.Exited || run.Session == "" || result.Session != run.Session {
		return failManagedWorker(ctx, s, run, result.Exited, errors.New("native worker did not return a complete result"))
	}
	payload := result.Payload
	if run.Role == "reviewer" {
		review, err := completedManagedReview(ctx, s, record, *run)
		if err != nil {
			return failManagedWorker(ctx, s, run, true, err)
		}
		run.Reviewer, payload = review.Worker, review.Result
	}
	request, err := readWorkerReturn(bytes.NewReader(payload), "-")
	if err == nil {
		err = matchWorkerReturn(record, request)
	}
	if err == nil && !managedHandbackMatchesRun(request, *run) {
		err = errors.New("native worker response does not match its dispatch and host session")
	}
	if err != nil {
		return failManagedWorker(ctx, s, run, true, err)
	}
	run.Status, run.Usage = "returned", &result.Usage
	run.SHA256 = workerBriefHash(payload)
	run.Result = filepath.Join(s.repo.GitDir, "kitsu-stages", run.Dispatch+".handback.json")
	return updateManagedRun(ctx, s, *run, payload)
}

func failManagedWorker(ctx context.Context, s *session, run *managedWorkerRun, exited bool, err error) error {
	run.Status, run.Reviewer = "unresolved", ""
	if exited {
		run.Status = "failed"
	}
	return errors.Join(err, updateManagedRun(ctx, s, *run, nil))
}

func reserveManagedRun(ctx context.Context, s *session, id string, budget int) (stageRecord, managedWorkerRun, *fslock.Lock, error) {
	var run managedWorkerRun
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return stageRecord{}, run, nil, err
	}
	defer l.close()
	r, err := l.record(id, s.repo.Root)
	if err != nil {
		return r, run, nil, err
	}
	if r.Managed == nil {
		return r, run, nil, errors.New("configured execution requires a prepared version 2 role assignment")
	}
	run, err = readManagedRun(l, id, r)
	if !errors.Is(err, os.ErrNotExist) {
		if err == nil && run.Status == "returned" {
			_, err = readManagedHandback(l, r, run)
		}
		return r, run, nil, err
	}
	if err := checkManagedRunReady(ctx, s, l, id, r); err != nil {
		return r, run, nil, err
	}
	b := r.Prepared.Brief
	run = managedWorkerRun{Version: 1, Dispatch: id, Host: b.Host, Role: b.Role, Status: "running", Requested: b.Model}
	preview := run
	preview.Status, preview.Session = "returned", strings.Repeat("0", 128)
	if b.Role == "reviewer" {
		preview.Reviewer = strings.Repeat("0", 128)
	}
	preview.Result, preview.SHA256 = filepath.Join(l.root.Name(), id+".handback.json"), strings.Repeat("0", 64)
	if _, err := boundedStageJSON(preview, budget); err != nil {
		return r, run, nil, err
	}
	lock, err := privateStageLock(ctx, l.gitRoot, s.repo.GitDir, "kitsu-managed-worker.lock", fslock.NoWait)
	if err != nil {
		return r, run, nil, err
	}
	if err := l.write(managedRunFile(id), run); err != nil {
		_ = lock.Close()
		return r, run, nil, err
	}
	return r, run, lock, nil
}

func checkManagedRunReady(ctx context.Context, s *session, l *stageLedger, id string, r stageRecord) error {
	active, err := l.active()
	if err != nil {
		return err
	}
	if active != id {
		return errors.New("configured execution requires the active prepared interval")
	}
	if active, err := workflowActiveTask(l, ""); err != nil {
		return err
	} else if active != "" {
		return failf("reconcile daily task %s before executing a managed worker", active)
	}
	if err := verifyDispatchReference(ctx, s, l, r); err != nil {
		return err
	}
	profile, err := managedProfileKey(s, r.Prepared.Brief)
	if err != nil {
		return err
	}
	if saved, err := checkSavedManagedProfile(l, r.Prepared.Brief.Task, profile); err != nil || !saved || profile != r.Managed.Profile {
		return errors.Join(err, errors.New("configured worker's saved profile is unavailable or changed"))
	}
	if err := validateStageWrites(ctx, s, r, r.Prepared.State); err != nil {
		return err
	}
	e, err := contextpack.Open(s.repo)
	if err != nil {
		return err
	}
	state, err := e.Snapshot(ctx)
	if err != nil || state != r.Prepared.State {
		return errors.Join(err, errors.New("candidate changed before configured worker launch; reconcile the prepared interval"))
	}
	b := r.Prepared.Brief
	if b.Workflow != "full" {
		return nil
	}
	views, err := uxTaskViews(ctx, s, b.Task)
	if err != nil {
		return err
	}
	b.State = views[0]
	return validateConfiguredStage(b)
}

func updateManagedRun(ctx context.Context, s *session, run managedWorkerRun, payload []byte) error {
	if run.Status != "running" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
	}
	l, err := openStageLedgerWait(ctx, s, time.Second)
	if err != nil {
		return err
	}
	defer l.close()
	active, err := l.active()
	if err != nil || active != run.Dispatch {
		return errors.Join(err, errors.New("configured execution lost its managed interval"))
	}
	if payload != nil {
		if err := l.writeBytes(run.Dispatch+".handback.json", payload); err != nil {
			return err
		}
	}
	return l.write(managedRunFile(run.Dispatch), run)
}

func readManagedHandback(l *stageLedger, r stageRecord, run managedWorkerRun) (workerReturnRequest, error) {
	var request workerReturnRequest
	if run.Role == "reviewer" {
		if _, err := readManagedReview(l, r, run); err != nil {
			return request, err
		}
	}
	if err := l.verify(); err != nil {
		return request, err
	}
	data, err := readSavedWorkerBrief(l.root, run.Dispatch+".handback.json")
	if err != nil || workerBriefHash(data) != run.SHA256 {
		return request, errors.Join(err, errors.New("configured worker handback changed"))
	}
	request, err = readWorkerReturn(bytes.NewReader(data), "-")
	if err != nil || !managedHandbackMatchesRun(request, run) {
		return request, errors.Join(err, errors.New("configured worker handback does not match its execution"))
	}
	return request, matchWorkerReturn(r, request)
}

func managedHandbackMatchesRun(request workerReturnRequest, run managedWorkerRun) bool {
	worker := run.Session
	if run.Role == "reviewer" {
		worker = run.Reviewer
	}
	return request.DispatchID == run.Dispatch && (request.Worker == nil || request.Worker.ID == worker)
}

func verifyManagedHandback(l *stageLedger, r stageRecord, request workerReturnRequest) error {
	if r.Managed == nil {
		return nil
	}
	run, err := readManagedRun(l, request.DispatchID, r)
	if errors.Is(err, os.ErrNotExist) {
		if r.Prepared.Brief.Role == "reviewer" {
			return errors.New("run the prepared review with native hook observation before returning it")
		}
		return nil // An explicitly launched manual worker can still report its work.
	}
	if err != nil {
		return err
	}
	if run.Status != "returned" {
		return errors.New("configured worker has no complete native handback")
	}
	saved, err := readManagedHandback(l, r, run)
	if err != nil {
		return err
	}
	if contextpack.Key(saved) != contextpack.Key(request) {
		return errors.New("worker return differs from the recorded native handback")
	}
	return nil
}

func printManagedRun(cfg Config, run managedWorkerRun, budget int) error {
	data, err := boundedStageJSON(run, budget)
	if err != nil {
		return err
	}
	_, err = cfg.Out.Write(data)
	return err
}

var managedWorkerSchema = json.RawMessage(`{
  "type": "object", "additionalProperties": false,
  "required": ["version", "dispatch_id", "task", "packet_sha256", "changed_paths", "checks", "findings", "blockers"],
  "properties": {
    "version": {"type":"integer"},
    "dispatch_id": {"type":"string"},
    "task": {"type":"string"},
    "packet_sha256": {"type":"string"},
    "changed_paths": {"type":"array","items":{"type":"string"}},
    "checks": {"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","state","run_id"],
      "properties":{"id":{"type":"string"},"state":{"type":"string"},"run_id":{"type":"string"}}}},
    "findings": {"type":"array","items":{"type":"object","additionalProperties":false,"required":["severity","path","line","message"],
      "properties":{"severity":{"type":"string","enum":["blocker","major","minor"]},"path":{"type":"string"},"line":{"type":"integer"},"message":{"type":"string"}}}},
    "blockers": {"type":"array","items":{"type":"string"}}
  }
}`)
