package harness_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestMain(m *testing.M) {
	if os.Getenv("KITSU_CONFIGURED_FIXTURE") != "" {
		tool := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		switch tool {
		case "codex", "claude":
			fixtureHost(tool)
			os.Exit(0)
		case "configured-check":
			data, _ := os.ReadFile("product.txt")
			test, _ := os.ReadFile("case.rule")
			if string(data) != "fixed\n" || !slices.Contains([]string{"check product\n", "stricter product\n"}, string(test)) {
				fmt.Println("expected product is missing")
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestConfiguredWorkflowResumesThroughSameAssignments(t *testing.T) {
	for _, entry := range []string{"", "codex", "claude"} {
		t.Run("entry-"+entry, func(t *testing.T) {
			r := configuredRepo(t)
			args := []string{"task", "run", "Fix the exported product", "--id", "workflow", "--until", "deliver", "--commit", "--json"}
			if entry != "" {
				args = append(args, "--native-host", entry, "--actor", "entry-session")
			}
			steps := []string{"architect:codex", "quality-engineer:claude", "developer:codex"}
			invocation := preapprovedConfiguredRun(r, args...)
			for attempt := 0; attempt < 3; attempt++ {
				if invocation.Code != 0 {
					t.Fatal(invocation.Output())
				}
				var reply struct {
					Next    string
					Attempt *harness.WorkflowAttempt
				}
				if err := json.Unmarshal([]byte(invocation.Stdout), &reply); err != nil {
					t.Fatal(invocation.Output())
				}
				if reply.Next == "done" {
					break
				}
				if reply.Attempt == nil || reply.Attempt.Host != entry {
					t.Fatalf("unexpected native handoff: %s", invocation.Output())
				}
				resume := []string{"task", "run", "--resume", "workflow", "--native-host", entry, "--actor", "entry-session", "--json"}
				if duplicate := r.Kitsu(resume...); duplicate.Code == 0 || !strings.Contains(duplicate.Output(), "outstanding") {
					t.Fatalf("duplicate issued an assignment: %s", duplicate.Output())
				}
				observeConfiguredRole(t, r, *reply.Attempt)
				invocation = r.Kitsu(resume...)
			}
			if invocation.Code != 0 || !strings.Contains(invocation.Stdout, `"next": "done"`) {
				t.Fatal(invocation.Output())
			}
			saved := configuredRecord(t, r)
			var roles []string
			for _, a := range saved.Workflow.Attempts {
				roles = append(roles, a.Role+":"+a.Host)
			}
			if !slices.Equal(steps, roles) || len(saved.Workflow.Baselines) != 1 || saved.Workflow.Baselines[0].Outcome != "failed" ||
				saved.Proof == nil || saved.Proof.Outcome != "passed" {
				t.Fatalf("role order or evidence mismatch: %v", roles)
			}
			if headCount := r.Git("rev-list", "--count", "HEAD"); headCount != "2" {
				t.Fatalf("expected one authorized commit, history length %s", headCount)
			}
			first := r.Kitsu("task", "receipt", "workflow", "--markdown")
			if first.Code != 0 || !strings.Contains(first.Stdout, r.Git("rev-parse", "HEAD")) {
				t.Fatal(first.Output())
			}
			if again := r.Kitsu("task", "run", "--resume", "workflow"); again.Code != 0 || r.Git("rev-list", "--count", "HEAD") != "2" {
				t.Fatal(again.Output())
			}
			if again := r.Kitsu("task", "receipt", "workflow", "--markdown"); again.Stdout != first.Stdout {
				t.Fatalf("receipt changed on resume: %s", again.Output())
			}
		})
	}
}

func TestConfiguredWorkflowPinsInstalledDefaultWithoutRolesFile(t *testing.T) {
	r := kitsutest.NewRepo(t)
	if result := r.Kitsu("config", "set", "host", "claude", "--global"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	r.Setenv("KITSU_HOST", "codex")
	result := r.Kitsu("task", "run", "Plan an export", "--id", "workflow", "--until", "plan",
		"--native-host", "claude", "--actor", "entry-session", "--json")
	if result.Code != 0 {
		t.Fatal(result.Output())
	}
	saved := configuredRecord(t, r)
	if len(saved.Workflow.Roles) != 6 || saved.Workflow.Active == nil || !saved.Workflow.Active.Native ||
		saved.Workflow.Active.Role != "architect" || saved.Workflow.Active.Host != "claude" {
		t.Fatalf("the task reserved an unexpected role: %s", result.Output())
	}
	for role, settings := range saved.Workflow.Roles {
		if settings.Host != "claude" || settings.Model.Effort != "" {
			t.Fatalf("%s has unexpected settings: %+v", role, settings)
		}
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "kitsu-roles.toml")); !os.IsNotExist(err) {
		t.Fatalf("starting the task wrote repository settings: %v", err)
	}
}

func TestConfiguredWorkflowRejectsWrongRoleWritesAndCanResume(t *testing.T) {
	r := configuredRepo(t)
	r.Setenv("KITSU_FIXTURE_BAD_TEST_WRITER", "1")
	res := preapprovedConfiguredRun(r, "task", "run", "Fix the exported product", "--id", "workflow", "--until", "verify")
	if res.Code == 0 || !strings.Contains(res.Output(), "outside its saved write scope") {
		t.Fatal(res.Output())
	}
	saved := configuredRecord(t, r)
	if saved.Proof != nil || saved.Workflow.Active != nil || len(saved.Workflow.Attempts) != 2 {
		t.Fatalf("write violation was treated as progress: %#v", saved.Workflow)
	}
	if got := r.Read("product.txt"); got != "unauthorized write\n" {
		t.Fatalf("changed work was lost: %q", got)
	}
	if retry := r.Kitsu("task", "run", "--resume", "workflow", "--retry"); retry.Code == 0 || !strings.Contains(retry.Output(), "restore") {
		t.Fatal(retry.Output())
	}
	r.Setenv("KITSU_FIXTURE_BAD_TEST_WRITER", "")
	r.Write("product.txt", "broken\n")
	r.Remove("case.rule") // restore the exact accepted snapshot
	if retry := r.Kitsu("task", "run", "--resume", "workflow", "--retry"); retry.Code != 0 {
		t.Fatal(retry.Output())
	}
}

func configuredRepo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	bin := filepath.Join(r.Home, "workflow-bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "claude", "configured-check"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, name), raw, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	claude := filepath.Join(r.Home, "claude-config")
	if err := os.Mkdir(claude, 0o700); err != nil {
		t.Fatal(err)
	}
	trust, err := json.Marshal(map[string]any{"projects": map[string]any{r.Dir: map[string]bool{"hasTrustDialogAccepted": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, ".claude.json"), trust, 0o600); err != nil {
		t.Fatal(err)
	}
	r.Setenv("CLAUDE_CONFIG_DIR", claude)
	r.Setenv("CLAUDECODE", "")
	r.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	r.Setenv("KITSU_CONFIGURED_FIXTURE", "1")
	r.Setenv("PATH", bin+string(os.PathListSeparator)+kitsutest.Binary(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r.Write("product.txt", "broken\n")
	r.Write("kitsu-roles.toml", `version = 2
[roles.lookup]
host = "claude"
[roles.spec-writer]
host = "codex"
[roles.architect]
host = "codex"
model = "fixture-architect"
effort = "max"
[roles.quality-engineer]
host = "claude"
model = "fixture-test-writer"
effort = "high"
[roles.developer]
host = "codex"
model = "fixture-developer"
effort = "medium"
[roles.reviewer]
host = "codex"
model = "fixture-reviewer"
effort = "high"
`)
	r.Git("add", "--", "product.txt", "kitsu-roles.toml")
	r.Git("commit", "-qm", "Starting candidate")
	return r
}

func configuredRecord(t *testing.T, r *kitsutest.Repo) harness.Task {
	t.Helper()
	var task harness.Task
	if err := json.Unmarshal([]byte(r.Read(".git/kitsu-stages/task-harness-workflow.json")), &task); err != nil || task.Workflow == nil {
		t.Fatalf("cannot read persisted workflow: %v", err)
	}
	return task
}

func observeConfiguredRole(t *testing.T, r *kitsutest.Repo, a harness.WorkflowAttempt) {
	t.Helper()
	settings := configuredRecord(t, r).Workflow.Roles[a.Role].Model
	guardConfiguredLaunch(t, r, a, settings.Model, settings.Effort)
	worker := "native-" + a.ID
	notice := map[string]any{"cwd": r.Dir, "session_id": a.Owner, "agent_id": worker,
		"agent_type": a.Role, "hook_event_name": "SubagentStart"}
	send := func() {
		data, err := json.Marshal(notice)
		if err != nil {
			t.Fatal(err)
		}
		res := r.Run(string(data), configuredExecutable(t, "kitsu"), "usage", "record", "--host", a.Host)
		if res.Code != 0 {
			t.Fatal(res.Output())
		}
	}
	send()
	if a.Role == "quality-engineer" {
		r.Write("case.rule", "check product\n")
	} else if a.Role == "developer" {
		r.Write("product.txt", "fixed\n")
	}
	result, err := json.Marshal(map[string]any{"task": "workflow", "attempt": a.ID, "result": fixtureRoleResult(a.Role)})
	if err != nil {
		t.Fatal(err)
	}
	notice["hook_event_name"] = "SubagentStop"
	notice["last_assistant_message"] = string(result)
	send()
}

func guardConfiguredLaunch(t *testing.T, r *kitsutest.Repo, a harness.WorkflowAttempt, model, effort string) {
	t.Helper()
	check := func(model string) string {
		t.Helper()
		call := map[string]any{"cwd": r.Dir, "tool_name": "Agent", "session_id": a.Owner, "tool_input": map[string]any{
			"model": model, "effort": effort, "reasoning_effort": effort, "fork_context": false,
		}}
		data, _ := json.Marshal(call)
		return r.Run(string(data), configuredExecutable(t, "kitsu"), "guard", "--host", a.Host).Stdout
	}
	if wrong := check("other-model"); !strings.Contains(wrong, "deny") {
		t.Fatalf("unexpected permission for wrong model: %s", wrong)
	}
	if right := check(model); right != "" {
		t.Fatalf("rejected configured native launch: %s", right)
	}
	if duplicate := check(model); !strings.Contains(duplicate, "deny") {
		t.Fatalf("unexpected permission for a second native launch: %s", duplicate)
	}
}

func configuredExecutable(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(kitsutest.Binary(t), name)
}

func fixtureHost(host string) {
	input, _ := io.ReadAll(os.Stdin)
	model, effort := fixtureArgument("--model"), fixtureArgument("--effort")
	var role string
	switch model {
	case "fixture-architect":
		role = "architect"
		if host != "codex" || !slices.Contains(os.Args, `model_reasoning_effort="max"`) {
			panic("wrong architect binding")
		}
	case "fixture-test-writer":
		role = "quality-engineer"
		if host != "claude" || effort != "high" {
			panic("wrong test-writer binding")
		}
		fixtureWrite("case.rule", []byte("check product\n"))
		if strings.Contains(string(input), "revise the regression") {
			fixtureWrite("case.rule", []byte("stricter product\n"))
		}
		if os.Getenv("KITSU_FIXTURE_BAD_TEST_WRITER") == "1" {
			fixtureWrite("product.txt", []byte("unauthorized write\n"))
		}
	case "fixture-developer":
		role = "developer"
		if host != "codex" || !slices.Contains(os.Args, `model_reasoning_effort="medium"`) {
			panic("wrong implementation binding")
		}
		fixtureWrite("product.txt", []byte("fixed\n"))
		if os.Getenv("KITSU_FIXTURE_REPAIR") != "" && !strings.Contains(string(input), "failed_assertion") {
			fixtureWrite("product.txt", []byte("almost\n"))
		}
	default:
		panic("unexpected fixture model " + model)
	}
	if host == "codex" {
		out, _ := json.Marshal(fixtureRoleResult(role))
		fixtureWrite(fixtureArgument("--output-last-message"), out)
		fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\n", fmt.Sprintf("cli-%s-%d", role, os.Getpid()))
		fmt.Println(`{"type":"turn.started"}`)
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`)
		return
	}
	d := json.NewEncoder(os.Stdout)
	session := fmt.Sprintf("cli-%s-%d", role, os.Getpid())
	_ = d.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": session, "model": model})
	_ = d.Encode(map[string]any{"type": "result", "subtype": "success", "session_id": session, "structured_output": fixtureRoleResult(role)})
}

func fixtureArgument(flag string) string {
	if i := slices.Index(os.Args, flag); i >= 0 && i+1 < len(os.Args) {
		return os.Args[i+1]
	}
	return ""
}

func fixtureWrite(path string, contents []byte) {
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		panic(err)
	}
}

func fixtureRoleResult(role string) map[string]any {
	result := map[string]any{"status": "complete", "note": "fixture " + role}
	switch role {
	case "architect":
		result["plan"] = harness.WorkflowPlan{CodePaths: []string{"product.txt"}, TestPaths: []string{"case.rule"}, Shared: []harness.WorkflowTestRegion{}, Baseline: "fail"}
	case "quality-engineer":
		result["assertion"] = harness.WorkflowAssertion{Argv: []string{"configured-check"}, Expect: "export contains the product", Failure: "expected product is missing"}
	}
	return result
}

func TestConfiguredWorkflowReviewsPassingCandidateAndReplaysTestCorrection(t *testing.T) {
	r := configuredRepo(t)
	result := preapprovedConfiguredRun(r, "task", "run", "Fix the exported product", "--id", "workflow", "--until", "verify",
		"--require-review", "--native-host", "codex", "--actor", "coordinator", "--json")
	resume := []string{"task", "run", "--resume", "workflow", "--native-host", "codex", "--actor", "coordinator", "--json"}
	reviews := 0
	for range 7 {
		if result.Code != 0 {
			t.Fatal(result.Output())
		}
		var reply struct {
			Next    string
			Attempt *harness.WorkflowAttempt
		}
		if err := json.Unmarshal([]byte(result.Stdout), &reply); err != nil {
			t.Fatal(result.Output())
		}
		if reply.Next == "done" {
			break
		}
		if reply.Attempt == nil {
			t.Fatalf("missing native assignment: %s", result.Output())
		}
		if reply.Attempt.Role != "reviewer" {
			observeConfiguredRole(t, r, *reply.Attempt)
		} else {
			task := configuredRecord(t, r)
			if task.Proof == nil || task.Proof.Outcome != "passed" || task.Proof.CheckedAt.IsZero() ||
				task.Proof.Candidate != reply.Attempt.Candidate {
				t.Fatal("review was reserved before final verification of this candidate")
			}
			reviews++
			var findings []string
			if reviews == 1 {
				findings = []string{"test: revise the regression for the accepted behavior"}
			}
			observeConfiguredReview(t, r, task, findings)
		}
		result = r.Kitsu(resume...)
	}
	task := configuredRecord(t, r)
	if result.Code != 0 || !strings.Contains(result.Stdout, `"next": "done"`) || reviews != 2 || len(task.Workflow.Baselines) != 2 ||
		task.Workflow.Baselines[1].Outcome != "failed" || r.Read("product.txt") != "fixed\n" || r.Read("case.rule") != "stricter product\n" {
		t.Fatalf("test correction did not retain the pre-code baseline and fresh review: %s", result.Output())
	}
	for _, finding := range task.Events {
		if finding.Action == "review" && finding.NativeReview == nil {
			t.Fatal("required review accepted without native evidence")
		}
	}
}

func observeConfiguredReview(t *testing.T, r *kitsutest.Repo, task harness.Task, findings []string) {
	t.Helper()
	a := *task.Workflow.Active
	settings := task.Workflow.Roles["reviewer"].Model
	guardConfiguredLaunch(t, r, a, settings.Model, settings.Effort)
	notice := map[string]any{"cwd": r.Dir, "session_id": a.Owner, "agent_id": "reviewer-" + a.ID,
		"agent_type": "reviewer", "hook_event_name": "SubagentStart"}
	send := func() {
		t.Helper()
		data, _ := json.Marshal(notice)
		result := r.Run(string(data), configuredExecutable(t, "kitsu"), "usage", "record", "--host", a.Host)
		if result.Code != 0 {
			t.Fatal(result.Output())
		}
	}
	send()
	var reservation struct {
		Result map[string]any `json:"native_review_result"`
	}
	view := r.Kitsu("task", "show", task.ID, "--review-ticket", a.Ticket)
	if view.Code != 0 || json.Unmarshal([]byte(view.Stdout), &reservation) != nil || reservation.Result == nil {
		t.Fatal(view.Output())
	}
	decision := "approve"
	if len(findings) > 0 {
		decision = "findings"
	}
	reservation.Result["decision"] = decision
	reservation.Result["findings"] = append([]string{}, findings...)
	reservation.Result["note"] = "Reviewed current passing candidate"
	report, _ := json.Marshal(reservation.Result)
	notice["hook_event_name"] = "SubagentStop"
	notice["last_assistant_message"] = string(report)
	send()
}

func TestConfiguredWorkflowFailedVerificationReturnsToDeveloper(t *testing.T) {
	r := configuredRepo(t)
	r.Setenv("KITSU_FIXTURE_REPAIR", "1")
	result := preapprovedConfiguredRun(r, "task", "run", "Fix exported product", "--id", "workflow", "--until", "verify", "--json")
	task := configuredRecord(t, r)
	if result.Code != 0 || !strings.Contains(result.Stdout, `"next": "done"`) ||
		task.Workflow.Rework != 1 || task.Latest("repair") == nil || task.Proof == nil || task.Proof.Outcome != "passed" {
		t.Fatalf("failed verification did not lead to a scoped repair: %s", result.Output())
	}
}

func TestConfiguredWorkflowRequiresTrustBeforeExecutingDeclaredPolicy(t *testing.T) {
	r := configuredRepo(t)
	r.Write("kitsu.toml", `version = 1
[[check]]
id = "product"
run = ["configured-check"]
`)
	r.Git("add", "--", "kitsu.toml")
	r.Git("commit", "-qm", "Declare verification")
	result := r.Kitsu("task", "run", "Fix exported product", "--id", "workflow", "--until", "verify")
	task := configuredRecord(t, r)
	if result.Code == 0 || !strings.Contains(result.Output(), "kitsu check trust") ||
		len(task.Workflow.Baselines) != 0 || task.Latest("implement") != nil || r.Read("product.txt") != "broken\n" {
		t.Fatalf("unapproved checks did not stop the workflow before production edits: %s", result.Output())
	}
}

func TestConfiguredWorkflowKeepsOneWorktreeAssignment(t *testing.T) {
	r := configuredRepo(t)
	first := r.Kitsu("task", "run", "Design one", "--id", "workflow", "--until", "plan",
		"--native-host", "codex", "--actor", "parent", "--json")
	if first.Code != 0 {
		t.Fatal(first.Output())
	}
	second := r.Kitsu("task", "run", "Design two", "--id", "second", "--until", "plan",
		"--native-host", "codex", "--actor", "other-parent", "--json")
	if second.Code == 0 || !strings.Contains(second.Output(), "outstanding assignment for workflow") {
		t.Fatal(second.Output())
	}
	var reply struct{ Attempt *harness.WorkflowAttempt }
	if err := json.Unmarshal([]byte(first.Stdout), &reply); err != nil || reply.Attempt == nil {
		t.Fatal(first.Output())
	}
	observeConfiguredRole(t, r, *reply.Attempt)
	if got := r.Kitsu("task", "run", "--resume", "workflow"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	if got := r.Kitsu("task", "run", "--resume", "second"); got.Code != 0 {
		t.Fatal(got.Output())
	}
}

func preapprovedConfiguredRun(r *kitsutest.Repo, args ...string) kitsutest.Result {
	return r.Kitsu(append(args, "--expect", "export contains the product", "--", "configured-check")...)
}

func TestConfiguredWorkflowDoesNotExecuteUnapprovedWorkerArgv(t *testing.T) {
	r := configuredRepo(t)
	result := r.Kitsu("task", "run", "Fix exported product", "--id", "workflow")
	task := configuredRecord(t, r)
	if result.Code == 0 || !strings.Contains(result.Output(), "--approve-assertion") ||
		len(task.Workflow.Baselines) != 0 || task.Proof != nil || task.Latest("implement") != nil {
		t.Fatalf("unapproved worker command passed the execution boundary: %s", result.Output())
	}
}
