package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

const runnerFixtureRoles = `version = 2
[paths]
tests = ["checks/**"]
[roles.lookup]
host = "codex"
[roles.spec-writer]
host = "claude"
[roles.architect]
host = "codex"
[roles.quality-engineer]
host = "claude"
model = "sonnet"
effort = "high"
[roles.developer]
host = "codex"
[roles.reviewer]
host = "claude"
model = "opus"
effort = "high"
`

type signedRunnerFixture struct {
	t        *testing.T
	repo     *kitsutest.Repo
	client   *workflowrunner.Client
	mu       sync.Mutex
	jobs     []workflowrunner.Job
	returned map[string]workflowrunner.Receipt
	mutate   func(workflowrunner.Job, *workflowrunner.Receipt)
	release  bool
	pause    bool
}

func newWorkflowFixture(t *testing.T, mutate func(workflowrunner.Job, *workflowrunner.Receipt)) *signedRunnerFixture {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Write("kitsu-roles.toml", runnerFixtureRoles)
	r.Write("product.txt", "old\n")
	r.Write(".gitignore", "local-note.txt\n")
	r.Write("local-note.txt", "outside candidate\n")
	r.Write("check-input.txt", "fixed\n")
	r.Git("add", "-A")
	r.Git("commit", "-m", "starting candidate")
	t.Chdir(r.Dir)
	t.Setenv("GIT_COMMITTER_NAME", "Kitsu Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_AUTHOR_NAME", "Kitsu Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	f := &signedRunnerFixture{t: t, repo: r, mutate: mutate, returned: make(map[string]workflowrunner.Receipt)}
	f.policy()
	return f
}

func (f *signedRunnerFixture) policy() {
	f.t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		f.t.Fatal(err)
	}
	policy := workflowrunner.Policy{
		Declaration: undeclaredPolicy, TestPaths: []string{"checks/**"}, CodePaths: []string{"product.txt"},
		Checks: []workflowrunner.Check{
			{ID: "supplement", Argv: []string{"project-check", "supplement"}, TestID: "product-value"},
			{ID: "intake", Argv: []string{"project-check", "intake"}, Inputs: map[string]string{"check-input.txt": workflowrunner.BytesDigest([]byte("fixed\n"))}},
		},
	}
	caps := workflowrunner.Capabilities{Platform: runtime.GOOS + "/" + runtime.GOARCH, Profile: "fixture", Policy: policy,
		NativeTest: []string{"linux", "darwin", "windows"}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		var in workflowrunner.Request
		if err != nil || json.Unmarshal(data, &in) != nil || in.Version != workflowrunner.Version || in.Project != "fixture" {
			out.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		got := f.returned[in.Attempt]
		switch in.Action {
		case "probe":
			got.Status, got.Capabilities = "ready", &caps
		case "start":
			if got.Status == "" {
				job := *in.Job
				got = f.result(job)
				f.jobs = append(f.jobs, job)
				f.returned[in.Attempt] = got
				if f.pause {
					out.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
		case "status":
			if f.pause {
				got.Status, got.Stopped, got.Release = "running", false, false
			} else if f.release {
				got.Status, got.Stopped, got.Release = "stopped", true, true
			}
		case "cancel":
			got.Status, got.Stopped = "stopped", true
			f.returned[in.Attempt] = got
		}
		got.Request, got.Project = workflowrunner.BytesDigest(data), in.Project
		got.Attempt, got.Binding = in.Attempt, in.Binding
		got.Platform, got.Profile, got.Policy = caps.Platform, caps.Profile, workflowrunner.Digest(caps.Policy)
		receipt, _ := json.Marshal(got)
		_ = json.NewEncoder(out).Encode(workflowrunner.Signed{Receipt: receipt, Signature: ed25519.Sign(key, receipt)})
	}))
	f.t.Cleanup(server.Close)
	f.client, err = workflowrunner.Connect(f.t.Context(), workflowrunner.Registration{
		Worktree: f.repo.Dir, Project: "fixture", Endpoint: server.URL, ReceiptKey: pub,
		RootCA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})),
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *signedRunnerFixture) result(job workflowrunner.Job) workflowrunner.Receipt {
	got := workflowrunner.Receipt{Job: workflowrunner.Digest(job), Status: "returned", Stopped: true,
		Host: job.Host, Role: job.Role, Effort: job.Effort}
	if job.Role != "" {
		got.Session, got.Model = "session-"+job.Attempt, job.Model
		switch job.Model {
		case "opus", "sonnet", "haiku":
			got.Model = "claude-" + job.Model + "-4-5"
		}
	}
	switch job.Kind {
	case "plan":
		got.Result = json.RawMessage(`{"status":"complete","note":"Check product value","plan":{"code_paths":["product.txt"],"test_paths":["checks/**"],"baseline":"fail","shared":[]}}`)
	case "tests":
		got.Result = json.RawMessage(`{"status":"complete","note":"Check expected product","assertion":{"argv":["project-check","supplement"],"expect":"product is fixed","test_id":"product-value"}}`)
		got.Changes = []workflowrunner.File{{Path: "checks/product.rule", Mode: "100644", Data: []byte("fixed\n")}}
	case "implement", "repair":
		got.Result = json.RawMessage(`{"status":"complete","note":"Fix the product"}`)
		got.Changes = []workflowrunner.File{{Path: "product.txt", Mode: "100644", Data: []byte("fixed\n")}}
	case "review":
		got.AgentType = "kitsu-reviewer"
		got.Result = json.RawMessage(`{"decision":"approve","note":"The product matches the requirement","findings":[]}`)
	case "baseline", "verify":
		fixed := false
		for _, file := range job.Files {
			if file.Path == "local-note.txt" || file.Path == ".claude/settings.json" {
				f.t.Errorf("unaccepted file entered isolated snapshot: %s", file.Path)
			}
			if file.Path == "product.txt" && bytes.Equal(file.Data, []byte("fixed\n")) {
				fixed = true
			}
		}
		for _, id := range job.Checks {
			exit, outcome, testID := 0, "passed", ""
			if !fixed {
				exit, outcome, testID = 1, "test_failed", "product-value"
			}
			got.Checks = append(got.Checks, workflowrunner.CheckResult{ID: id, Outcome: outcome, TestID: testID, Exit: &exit})
		}
	}
	if f.mutate != nil {
		f.mutate(job, &got)
	}
	return got
}

func (f *signedRunnerFixture) run(args ...string) (string, error) {
	f.t.Helper()
	var out bytes.Buffer
	cfg := Config{Out: &out, Err: &out, In: strings.NewReader(""),
		workflowRunner: func(context.Context, string) (*workflowrunner.Client, error) { return f.client, nil }}
	err := Execute(f.t.Context(), append([]string{"task", "run"}, args...), cfg)
	return out.String(), err
}

func (f *signedRunnerFixture) task() harness.Task {
	f.t.Helper()
	var t harness.Task
	data := f.repo.Read(".git/kitsu-stages/" + harnessFile("case"))
	if err := json.Unmarshal([]byte(data), &t); err != nil {
		f.t.Fatal(err)
	}
	return t
}

func TestSupervisedWorkflowDeliversAndKeepsIntakeAssertion(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	out, err := f.run("Fix the product", "--id", "case", "--until", "deliver", "--commit", "--require-review",
		"--expect", "product contains fixed", "--", "project-check", "intake")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	task := f.task()
	if proof := task.CompletedDelivery(); proof == nil || !proof.Reviewed || task.Workflow.Intake.Argv[1] != "intake" {
		t.Fatalf("unexpected delivery: %+v", task)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var order []string
	for _, job := range f.jobs {
		order = append(order, job.Kind)
		if job.Kind == "verify" && !slices.Equal(job.Checks, []string{"intake", "supplement"}) {
			t.Fatalf("verification omitted intake: %v", job.Checks)
		}
	}
	if !slices.Equal(order, []string{"plan", "tests", "baseline", "implement", "verify", "review"}) {
		t.Fatal(order)
	}
	if hash := f.repo.Git("show", "HEAD:product.txt"); strings.TrimSpace(hash) != "fixed" {
		t.Fatal(hash)
	}
	if f.repo.Read("local-note.txt") != "outside candidate\n" {
		t.Fatal("the runner import changed unrelated local content")
	}
}

func TestSupervisedVerificationRejectsWrongResult(t *testing.T) {
	for _, kind := range []string{"unapproved-assertion", "invalid-baseline", "check-error", "protected-write", "ignored-write", "supplement-only", "wrong-reviewer", "wrong-model", "wrong-effort"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorkflowFixture(t, func(job workflowrunner.Job, got *workflowrunner.Receipt) {
				switch {
				case kind == "unapproved-assertion" && job.Kind == "tests":
					got.Result = json.RawMessage(`{"status":"complete","note":"Choose another check","assertion":{"argv":["other-check"],"expect":"product is fixed","test_id":"product-value"}}`)
				case kind == "invalid-baseline" && job.Kind == "baseline":
					got.Checks[0].TestID = "different-test"
				case kind == "check-error" && job.Kind == "baseline":
					got.Checks[0].Outcome = "error"
				case kind == "ignored-write" && job.Kind == "implement":
					got.Changes = append(got.Changes, workflowrunner.File{Path: "local-note.txt", Mode: "100644", Data: []byte("changed\n")})
				case kind == "protected-write" && job.Kind == "implement":
					got.Changes = append(got.Changes, workflowrunner.File{Path: ".claude/settings.json", Mode: "100644", Data: []byte("{}\n")})
				case kind == "supplement-only" && job.Kind == "verify":
					got.Checks = got.Checks[1:]
				case kind == "wrong-reviewer" && job.Kind == "review":
					got.AgentType = "lookup"
				case kind == "wrong-model" && job.Kind == "review":
					got.Model = "claude-haiku-4-5"
				case kind == "wrong-effort" && job.Kind == "review":
					got.Effort = ""
				}
			})
			out, err := f.run("Fix the product", "--id", "case", "--require-review", "--expect", "product contains fixed", "--", "project-check", "intake")
			if err == nil {
				t.Fatal("workflow accepted an invalid runner result", out)
			}
			task := f.task()
			if task.Workflow.Reviewed != nil || task.CompletedDelivery() != nil {
				t.Fatal("invalid output established review or delivery")
			}
			if (kind == "protected-write" || kind == "ignored-write") && f.repo.Read("product.txt") != "old\n" {
				t.Fatal("rejected import applied other edits")
			}
		})
	}
}

func TestSupervisedResumeRetainsResultAndRequiresReleaseAuthorization(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	f.pause = true
	if out, err := f.run("Plan a change", "--id", "case", "--until", "plan"); err == nil {
		t.Fatal(out)
	}
	a := f.task().Workflow.Active
	if a == nil {
		t.Fatal("runner reservation was lost")
	}
	if out, err := f.run("--resume", "case", "--release", a.ID); err == nil {
		t.Fatal("released a running job", out)
	}
	f.mu.Lock()
	f.pause = false
	f.mu.Unlock()
	if out, err := f.run("--resume", "case"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.jobs) != 1 || f.task().Workflow.Active != nil {
		t.Fatal("resuming did not consume the original result exactly once")
	}
}

// A person who starts a supervised run reads what the role workflow measured
// before the first job. Resuming the saved run does not repeat it.
func TestSupervisedRunStatesTheMeasuredCostBeforeItsFirstJob(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	f.pause = true
	started, _ := f.run("Plan a change", "--id", "case", "--until", "plan")
	cost, job := strings.Index(started, optInCost), strings.Index(started, "Kitsu task case · ")
	if cost < 0 || job >= 0 && job < cost || !strings.Contains(started, "Supervised runs are not measured.") {
		t.Fatalf("a new run did not state its cost before its first job:\n%s", started)
	}
	f.mu.Lock()
	f.pause = false
	f.mu.Unlock()
	if resumed, err := f.run("--resume", "case"); err != nil || strings.Contains(resumed, optInCost) {
		t.Fatalf("resuming repeated the cost or failed: %v\n%s", err, resumed)
	}
}

func TestSupervisedProofBindsCurrentAssertions(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	if out, err := f.run("Fix the product", "--id", "case"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	task := f.task()
	s, err := open(t.Context(), Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	s.workflowRunner = f.client
	if !currentWorkflowProof(s, &task) {
		t.Fatal("current signed proof rejected")
	}
	task.Workflow.Test.Expect = "different required behavior"
	if currentWorkflowProof(s, &task) {
		t.Fatal("proof remained current after assertion changed")
	}
}

func TestUnrelatedAgentHookIgnoresActiveLedgerLock(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	s, err := open(t.Context(), Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	l, err := openStageLedger(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()
	for _, host := range []string{"codex", "claude"} {
		var out bytes.Buffer
		body, _ := json.Marshal(map[string]string{"cwd": f.repo.Dir, "tool_name": "Agent"})
		if err := runGuard(t.Context(), Config{In: bytes.NewReader(body), Out: &out}, []string{"--host", host}); err != nil || out.Len() != 0 {
			t.Fatalf("%s: %v %s", host, err, &out)
		}
	}
}

func TestScenarioRepoClearsClaudeParentEnvironment(t *testing.T) {
	for _, key := range []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT"} {
		t.Setenv(key, "parent")
	}
	r := kitsutest.NewRepo(t)
	for _, assignment := range r.Env() {
		if strings.HasPrefix(assignment, "CLAUDECODE=") || strings.HasPrefix(assignment, "CLAUDE_CODE_CHILD_SESSION=") ||
			strings.HasPrefix(assignment, "CLAUDE_CODE_ENTRYPOINT=") {
			t.Fatalf("fixture inherited %s", assignment)
		}
	}
}

func TestSupervisedRunKeepsOriginalFailureThroughRepair(t *testing.T) {
	f := newWorkflowFixture(t, func(job workflowrunner.Job, got *workflowrunner.Receipt) {
		if job.Kind == "verify" {
			exit := 1
			got.Checks[0].Exit = &exit
			got.Checks[0].Outcome = "failed"
		}
	})
	out, err := f.run("Verify supplied patch", "--id", "case", "--entry", "verify", "--repair", "--rework-limit", "1",
		"--expect", "the original check passes", "--", "project-check", "intake")
	if err == nil {
		t.Fatal("the original assertion failed but the task succeeded", out)
	}
	task := f.task()
	if task.Workflow.Intake == nil || task.Workflow.Test == nil ||
		task.Proof.Outcome != "failed" || !slices.Equal(task.Workflow.Verification.Checks, []string{"intake", "supplement"}) {
		t.Fatalf("did not retain the required original assertion: %v\n%s", err, out)
	}
}

func TestSupervisedRunRejectsChangedReviewedInput(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	f.repo.Write("check-input.txt", "edited input\n")
	out, err := f.run("Verify supplied patch", "--id", "case", "--entry", "verify",
		"--expect", "the original check passes", "--", "project-check", "intake")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "requires unchanged input") || len(f.jobs) != 0 {
		t.Fatalf("modified check input reached the runner: %v\n%s", err, out)
	}
}

func TestSupervisedRunnerCheckReceivesExactStagedBytes(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	f.repo.Git("config", "core.autocrlf", "true")
	f.repo.Write("product.txt", "fixed\r\n")
	out, err := f.run("Verify and commit the patch", "--id", "case", "--entry", "verify", "--until", "deliver",
		"--commit", "--include", "product.txt", "--expect", "product is fixed", "--", "project-check", "intake")
	if err != nil {
		t.Fatalf("the check did not see staged Git bytes: %v\n%s", err, out)
	}
	task := f.task()
	if task.CompletedDelivery() == nil {
		t.Fatal("configured commit did not retain delivery evidence")
	}
}

func TestSupervisedResultWaitsForContendedLedger(t *testing.T) {
	f := newWorkflowFixture(t, func(job workflowrunner.Job, _ *workflowrunner.Receipt) {
		if job.Kind != "plan" {
			return
		}
		s, err := open(t.Context(), Config{}, true)
		if err != nil {
			t.Error(err)
			return
		}
		l, err := openStageLedger(t.Context(), s)
		if err != nil {
			t.Errorf("coordinator held the ledger during the runner call: %v", err)
			return
		}
		time.AfterFunc(100*time.Millisecond, l.close)
	})
	out, err := f.run("Plan the patch", "--id", "case", "--until", "plan")
	if err != nil {
		t.Fatalf("completed result lost during brief contention: %v\n%s", err, out)
	}
	if task := f.task(); task.Latest("plan") == nil {
		t.Fatal("completed plan was not retained")
	}
}

func TestSupervisedReleaseRequiresOperatorAuthorization(t *testing.T) {
	f := newWorkflowFixture(t, nil)
	f.pause = true
	if out, err := f.run("Plan a change", "--id", "case", "--until", "plan"); err == nil {
		t.Fatal(out)
	}
	a := f.task().Workflow.Active
	if a == nil {
		t.Fatal("no job reserved")
	}
	f.mu.Lock()
	f.pause = false
	f.mu.Unlock()
	if out, err := f.run("--resume", "case", "--release", a.ID); err == nil {
		t.Fatal("job released without authorization", out)
	}
	f.mu.Lock()
	f.release = true
	f.mu.Unlock()
	if out, err := f.run("--resume", "case", "--release", a.ID); err != nil {
		t.Fatalf("authorized stopped job could not be released: %v\n%s", err, out)
	}
	if task := f.task(); task.Workflow.Active != nil || task.Workflow.Attempts[0].Outcome != "released" {
		t.Fatal("release did not settle the reserved attempt")
	}
}
