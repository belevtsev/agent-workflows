// Package advisers_test exercises configured adviser CLIs through the public
// commands, using only this test binary as a local adviser.
package advisers_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

const (
	helperFlag       = "--kitsu-scenario-adviser"
	task             = "choose-design"
	fullFolder       = "openspec/changes/" + task + "/"
	fullAdvice       = fullFolder + "advisers/"
	smallAdvice      = "openspec/changes/.advice/" + task + "/"
	recommendation   = "The adviser recommends the first option; the coordinator decides.\n"
	reviewCheckpoint = `[[checkpoints]]
id = "review-advice"
stage = "review"
role = "reviewer"
advisers = ["local"]
`
	allCheckpoints = `[[checkpoints]]
id = "spec-advice"
stage = "spec"
role = "spec-writer"
advisers = ["local"]
[[checkpoints]]
id = "design-advice"
stage = "design"
role = "architect"
advisers = ["local"]
` + reviewCheckpoint
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == helperFlag {
		os.Exit(runHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func runHelper(args []string) int {
	if len(args) < 2 || os.Getenv("KITSU_ADVISER_CALL") != "1" {
		return 80
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 81
	}
	if err := os.WriteFile(os.Getenv("KITSU_SCENARIO_ADVISER_PROMPT"), prompt, 0o600); err != nil {
		return 82
	}
	calls, err := os.OpenFile(os.Getenv("KITSU_SCENARIO_ADVISER_CALLS"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 83
	}
	_, writeErr := calls.WriteString(args[1] + "\n")
	closeErr := calls.Close()
	if writeErr != nil || closeErr != nil {
		return 84
	}
	fmt.Fprint(os.Stderr, "adviser diagnostic; fixture-secret-is-not-recorded")
	switch args[0] {
	case "nonzero":
		fmt.Fprint(os.Stdout, "partial recommendation")
		return 17
	case "timeout":
		fmt.Fprint(os.Stdout, "partial recommendation")
		time.Sleep(20 * time.Second)
	case "overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", advisers.MaxAnswer+1))
	case "empty":
		fmt.Fprint(os.Stdout, " \n")
	case "missing-answer":
		fmt.Fprint(os.Stdout, "stdout must not replace a missing answer file")
	case "mutate":
		if err := os.WriteFile("src/app.txt", []byte("changed during consultation\n"), 0o600); err != nil {
			return 85
		}
		fmt.Fprint(os.Stdout, recommendation)
	case "answer":
		if len(args) == 3 {
			if err := os.WriteFile(args[2], []byte(recommendation), 0o600); err != nil {
				return 86
			}
			fmt.Fprint(os.Stdout, "ignored stdout because the answer file is authoritative")
		} else {
			fmt.Fprint(os.Stdout, recommendation)
		}
	default:
		return 87
	}
	return 0
}

type fixture struct {
	*kitsutest.Repo
	t             *testing.T
	configuration string
	policy        string
	personal      string
	calls         string
	prompt        string
}

func newFixture(t *testing.T, mode, checkpoints string, answerFile bool) *fixture {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	configDir := filepath.Join(repo.Home, "config")
	repo.Setenv("XDG_CONFIG_HOME", configDir)
	repo.Setenv("APPDATA", configDir)
	repo.Setenv("GORACE", "atexit_sleep_ms=0")
	f := &fixture{Repo: repo, t: t, calls: filepath.Join(repo.Home, "calls"), prompt: filepath.Join(repo.Home, "prompt")}
	repo.Setenv("KITSU_SCENARIO_ADVISER_CALLS", f.calls)
	repo.Setenv("KITSU_SCENARIO_ADVISER_PROMPT", f.prompt)
	if runtime.GOOS == "darwin" {
		configDir = filepath.Join(repo.Home, "Library", "Application Support")
	}
	f.personal = filepath.Join(configDir, "kitsu", "advisers.toml")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := []string{binary, helperFlag, mode, "{model}"}
	if answerFile {
		command = append(command, "{answer_file}")
	}
	argv, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	f.policy = "[access]\ndefault = [\"local\"]\ndeveloper = []\n" + checkpoints
	timeout := "5s"
	if mode == "timeout" {
		timeout = "1s"
	}
	f.configuration = fmt.Sprintf("version = 1\n[advisers.local]\nmodel = \"configured-model\"\ncommand = %s\ntimeout = %q\n%s",
		argv, timeout, f.policy)
	repo.Write(advisers.FileName, f.configuration)
	repo.Write("kitsu.toml", "version = 1\n")
	repo.Write("kitsu-roles.toml", "version = 1\n[paths]\ntests = [\"tests/**\"]\n")
	repo.Write("src/app.txt", "initial code\n")
	repo.Write("evidence.md", "Only this explicitly selected evidence is sent.\n")
	repo.Write("unlisted.md", "unlisted-private-context")
	repo.Write("request.json", `{"version":1,"kind":"advice","question":"What should the coordinator consider?","context":["evidence.md"]}`)
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "adviser scenario fixture")
	return f
}

func (f *fixture) addFullRecord() {
	f.t.Helper()
	f.Write(fullFolder+"proposal.md", "# Proposal\n\nSource: owner's request.\n")
	f.Write(fullFolder+"specs/greeting/spec.md", `# greeting

## ADDED Requirements

### Requirement: Greeting

The app SHALL greet the user.

Source: owner's request.

#### Scenario: A greeting

- **WHEN** the app runs
- **THEN** it greets the user
`)
	f.Write(fullFolder+"design.md", `# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| app | Prints a greeting. | none | no | src/app.txt |

## Rules

| Rule | Checked by |
| --- | --- |
| The app greets. | review |
`)
	f.Write(fullFolder+"tasks.md", "# Tasks\n\n- [ ] 1.1 Implement Greeting. Size: S. Depends on: none. Check: unit. Source: the specification.\n")
}

func (f *fixture) changeStatus() change.Report {
	f.t.Helper()
	return decode[change.Report](f.t, requireCode(f.t, f.Kitsu("change", "status", task, "--json"), 0))
}

func (f *fixture) writeHumanApprovals() {
	f.t.Helper()
	content := f.changeStatus().Content
	f.Write(fullFolder+"reviews.md", fmt.Sprintf(`# Reviews

## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |
| spec | 1 | 2026-11-15 | %[1]s |
| design | 1 | 2026-11-15 | %[2]s |

## Findings

| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- |

## Approvals

| What | Date | Content |
| --- | --- | --- |
| spec | 2026-11-15 | %[1]s |
| design | 2026-11-15 | %[2]s |
`, content.Spec, content.Design))
}

func requireChangeStage(t *testing.T, report change.Report, stage, missing string) {
	t.Helper()
	if report.Stage != stage || !strings.Contains(strings.Join(report.Missing, "\n"), missing) || report.BrokenRules != 0 {
		t.Fatalf("change status = %#v; want stage %q missing %q with valid files", report, stage, missing)
	}
}

func (f *fixture) usePersonalDefinitions() {
	f.t.Helper()
	_, definitions, _ := strings.Cut(f.configuration, "version = 1\n")
	definitions, _, _ = strings.Cut(definitions, "[access]")
	f.writePersonal("version = 1\n" + definitions)
	f.Write(advisers.FileName, "version = 1\n"+f.policy)
}

func (f *fixture) writePersonal(text string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.personal), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(f.personal, []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func requireCode(t *testing.T, got kitsutest.Result, want int) kitsutest.Result {
	t.Helper()
	if got.Code != want {
		t.Fatalf("exit status = %d, want %d:\n%s", got.Code, want, got.Output())
	}
	return got
}

func decode[T any](t *testing.T, result kitsutest.Result) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(result.Stdout), &value); err != nil {
		t.Fatalf("CLI did not print one JSON value: %v\n%s", err, result.Output())
	}
	return value
}

func (f *fixture) bind(workflow string, refresh bool) advisers.Binding {
	f.t.Helper()
	args := []string{"advisers", "bind", "--task", task, "--workflow", workflow, "--json"}
	if refresh {
		args = append(args, "--refresh")
	}
	return decode[advisers.Binding](f.t, requireCode(f.t, f.Kitsu(args...), 0))
}

func (f *fixture) consult(role, stage, checkpoint, request string, want int) advisers.Consultation {
	f.t.Helper()
	args := []string{"consult", "--task", task, "--role", role, "--stage", stage, "--adviser", "local", "--request", "-", "--json"}
	if checkpoint != "" {
		args = append(args, "--checkpoint", checkpoint)
	}
	return decode[advisers.Consultation](f.t, requireCode(f.t, f.Run(request, kitsuProgram(f.t), args...), want))
}

func (f *fixture) resolve(id, disposition, reason, choice string) advisers.Disposition {
	f.t.Helper()
	args := []string{"advisers", "resolve", "--task", task, id, "--disposition", disposition, "--reason", reason, "--json"}
	if choice != "" {
		args = append(args, "--choice", choice)
	}
	return decode[advisers.Disposition](f.t, requireCode(f.t, f.Kitsu(args...), 0))
}

func (f *fixture) check(want int, checkpoint string) advisers.Status {
	f.t.Helper()
	args := []string{"advisers", "check", "--task", task, "--json"}
	if checkpoint != "" {
		args = append(args, "--checkpoint", checkpoint)
	}
	return decode[advisers.Status](f.t, requireCode(f.t, f.Kitsu(args...), want))
}

func (f *fixture) callCount() int {
	f.t.Helper()
	data, err := os.ReadFile(f.calls)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func kitsuProgram(t *testing.T) string {
	t.Helper()
	name := "kitsu"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(kitsutest.Binary(t), name)
}

func requireState(t *testing.T, status advisers.Status, checkpoint, state string) {
	t.Helper()
	for _, requirement := range status.Requirements {
		if requirement.Checkpoint == checkpoint && requirement.State == state {
			return
		}
	}
	t.Fatalf("checkpoint %q has no state %q: %#v", checkpoint, state, status)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func paths(t *testing.T, repo *kitsutest.Repo, pattern string) []string {
	t.Helper()
	found, err := filepath.Glob(repo.Path(pattern))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func assertTokenAdvisers(t *testing.T, repo *kitsutest.Repo, calls int) {
	t.Helper()
	type adviserUsage struct {
		Calls    int    `json:"calls"`
		Usage    string `json:"usage"`
		Complete bool   `json:"complete"`
		Items    []struct {
			ID             string `json:"id"`
			Task           string `json:"task"`
			Adviser        string `json:"adviser"`
			RequestedModel string `json:"requested_model"`
			Outcome        string `json:"outcome"`
		} `json:"items"`
	}
	for _, args := range [][]string{{"tokens", "--json"}, {"tokens", "--task", task, "--json"}} {
		result := requireCode(t, repo.Kitsu(args...), 0)
		report := decode[struct {
			Advisers adviserUsage `json:"advisers"`
		}](t, result)
		got := report.Advisers
		if got.Calls != calls || got.Usage != "unknown" || got.Complete || len(got.Items) != calls {
			t.Fatalf("adviser token accounting = %#v, want %d calls with unknown usage", got, calls)
		}
		for _, item := range got.Items {
			if item.ID == "" || item.Task != task || item.Adviser != "local" || item.RequestedModel != "configured-model" || item.Outcome == "" {
				t.Fatalf("adviser token entry = %#v", item)
			}
		}
		if slices.Contains(args, "--task") {
			report := decode[struct {
				Complete bool `json:"complete"`
				Starts   int  `json:"starts"`
				Total    int  `json:"total"`
			}](t, result)
			if report.Complete || report.Starts != 0 || report.Total != 0 {
				t.Fatalf("unknown adviser usage altered role totals or claimed completeness: %#v", report)
			}
		}
	}
}

func assertRoleTables(t *testing.T, repo *kitsutest.Repo) {
	t.Helper()
	for _, host := range []string{"claude", "codex"} {
		table := requireCode(t, repo.Kitsu("roles", "--host", host), 0).Stdout
		header, _, _ := strings.Cut(table, "\n")
		if strings.Join(strings.Fields(header), " ") != "ROLE MODEL REASONING ADVISERS" {
			t.Fatalf("%s role table header = %q", host, header)
		}
		result := requireCode(t, repo.Kitsu("roles", "--host", host, "--json"), 0)
		report := decode[struct {
			Host  string `json:"host"`
			Roles []struct {
				Role      string   `json:"role"`
				Model     string   `json:"model"`
				Reasoning string   `json:"reasoning"`
				Advisers  []string `json:"advisers"`
			} `json:"roles"`
		}](t, result)
		if report.Host != host || len(report.Roles) != 8 {
			t.Fatalf("host role report = %#v", report)
		}
		seen := map[string]bool{}
		for _, row := range report.Roles {
			seen[row.Role] = true
			if row.Model == "" || row.Reasoning == "" {
				t.Fatalf("role model/reasoning missing: %#v", row)
			}
			if row.Role == "developer" {
				wantModel := "gpt-6.1-terra"
				if host == "claude" {
					wantModel = "sonnet"
				}
				if row.Model != wantModel || row.Reasoning != "high" || len(row.Advisers) != 0 {
					t.Fatalf("developer host row = %#v", row)
				}
			} else if !slices.Equal(row.Advisers, []string{"local"}) {
				t.Fatalf("role %s did not inherit adviser access: %#v", row.Role, row)
			}
		}
		if !seen["coordinator"] || !seen["developer"] || !seen["reviewer"] {
			t.Fatalf("host table omits workflow roles: %#v", seen)
		}
	}
}

func TestConsultationWorkflow(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "answer", reviewCheckpoint, true)
	f.addFullRecord()
	binding := f.bind("full", false)
	if binding.Version != 1 || binding.Workflow != "full" || binding.Advisers["local"].Model != "configured-model" {
		t.Fatalf("binding = %#v", binding)
	}
	bindingText := f.Read(fullAdvice + "bindings/" + binding.ID + ".json")
	if strings.Contains(bindingText, helperFlag) || strings.Contains(bindingText, "command") {
		t.Fatal("portable binding contains a local executable")
	}
	request := `{"version":1,"kind":"decision","question":"Which design should the coordinator choose?","choices":[{"id":"first","text":"Use the existing component"},{"id":"second","text":"Build a new component"}],"context":["evidence.md"]}`
	consultation := f.consult("reviewer", "review", "review-advice", request, 0)
	if consultation.Execution.Outcome != "success" || consultation.Execution.Answer != recommendation ||
		consultation.Execution.ActualModel != "unknown" || consultation.Execution.UsageKnown {
		t.Fatalf("consultation = %#v", consultation)
	}
	prompt := readFile(t, f.prompt)
	for _, text := range []string{"Task: " + task, "Requester: reviewer", "Stage: review", "Which design", "first", "Only this explicitly selected evidence"} {
		if !strings.Contains(prompt, text) {
			t.Errorf("CLI stdin lacks %q: %s", text, prompt)
		}
	}
	if strings.Contains(prompt, "unlisted-private-context") || f.callCount() != 1 {
		t.Fatal("implicit context was sent or the adviser ran more than once")
	}
	exchange := fullAdvice + "exchanges/" + consultation.ID + "/"
	savedRequest := f.Read(exchange + "request.json")
	resultBefore := f.Read(exchange + "result.json")
	if !strings.Contains(savedRequest, `"outcome": "pending"`) || strings.Contains(resultBefore, "fixture-secret") {
		t.Fatal("pending request or private diagnostic isolation was lost")
	}
	requireState(t, f.check(5, ""), "review-advice", "undisposed")
	requireCode(t, f.Kitsu("advisers", "resolve", "--task", task, consultation.ID,
		"--disposition", "accept", "--reason", "Choose an option", "--choice", "not-a-choice"), 1)
	first := f.resolve(consultation.ID, "reject", "Reuse keeps the dependency boundary clear.", "first")
	if first.Choice != "first" || first.Consultation != consultation.ID {
		t.Fatalf("coordinator disposition = %#v", first)
	}
	if !f.check(0, "").Ready {
		t.Fatal("a fresh, disposed consultation did not satisfy its checkpoint")
	}
	second := f.resolve(consultation.ID, "defer", "Revisit the second option after measurement.", "second")
	if second.ID == first.ID || len(paths(t, f.Repo, fullAdvice+"dispositions/*.json")) != 2 || f.Read(exchange+"result.json") != resultBefore {
		t.Fatal("a later coordinator decision rewrote earlier evidence")
	}
	result := requireCode(t, f.Kitsu("consult", "--task", task, "--role", "coordinator", "--stage", "code",
		"--adviser", "local", "--request", "request.json", "--json"), 0)
	advice := decode[advisers.Consultation](t, result)
	if advice.Request.Kind != "advice" || advice.Request.Question == "" {
		t.Fatalf("file request = %#v", advice.Request)
	}
	f.resolve(advice.ID, "accept", "Keep the reusable boundary.", "")
	assertTokenAdvisers(t, f.Repo, 2)
}

func TestCheckpointFreshnessAndOfflineChecks(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "answer", allCheckpoints, false)
	f.usePersonalDefinitions()
	f.addFullRecord()
	f.Write(fullFolder+"tasks.md", strings.Replace(f.Read(fullFolder+"tasks.md"), "- [ ]", "- [x]", 1))
	f.Write("tests/greeting_test.txt", "TestGreeting\n")
	f.Write(fullFolder+"tests.md", "# Tests\n\n| Scenario | Test file | Text |\n| --- | --- | --- |\n| A greeting | tests/greeting_test.txt | TestGreeting |\n")
	f.writeHumanApprovals()
	originalReviews := f.Read(fullFolder + "reviews.md")
	f.bind("full", false)
	requireCode(t, f.Kitsu("change", "check", task), 0)
	requireChangeStage(t, f.changeStatus(), "spec", "adviser checkpoint spec-advice")
	initial := f.check(5, "")
	if len(initial.Requirements) != 3 {
		t.Fatalf("required checkpoints = %#v", initial.Requirements)
	}
	roles := map[string]string{"spec": "spec-writer", "design": "architect", "review": "reviewer"}
	for _, stage := range []string{"spec", "design", "review"} {
		c := f.consult(roles[stage], stage, stage+"-advice", `{"version":1,"kind":"advice","question":"What should change?"}`, 0)
		requireChangeStage(t, f.changeStatus(), stage, "undisposed")
		f.resolve(c.ID, "accept", "The coordinator accounted for this advice.", "")
	}
	f.check(0, "")
	afterAdvice := f.changeStatus()
	requireChangeStage(t, afterAdvice, "review", "review of the code")
	if strings.Contains(strings.Join(afterAdvice.Missing, "\n"), "adviser checkpoint") ||
		f.Read(fullFolder+"reviews.md") != originalReviews {
		t.Fatal("advice invented a human review or remained missing after disposition")
	}
	requireCode(t, f.Kitsu("change", "check", task), 0)
	for _, edit := range []struct {
		stage string
		path  string
	}{
		{stage: "spec", path: fullFolder + "specs/greeting/spec.md"},
		{stage: "design", path: fullFolder + "design.md"},
		{stage: "review", path: "src/app.txt"},
	} {
		f.Write(edit.path, f.Read(edit.path)+"\nChanged the consulted content.\n")
		requireState(t, f.check(5, edit.stage+"-advice"), edit.stage+"-advice", "stale")
		requireChangeStage(t, f.changeStatus(), edit.stage, "adviser checkpoint "+edit.stage+"-advice")
		c := f.consult(roles[edit.stage], edit.stage, edit.stage+"-advice",
			`{"version":1,"kind":"advice","question":"Reconsider the updated content."}`, 0)
		f.resolve(c.ID, "reject", "The updated evidence supports keeping the current boundary.", "")
		f.check(0, "")
		humanMissing := f.changeStatus()
		requireChangeStage(t, humanMissing, edit.stage, "review")
		if strings.Contains(strings.Join(humanMissing.Missing, "\n"), "adviser checkpoint") {
			t.Fatal("a fresh disposed adviser answer remained in change status")
		}
		f.writeHumanApprovals()
		requireChangeStage(t, f.changeStatus(), "review", "review of the code")
		requireCode(t, f.Kitsu("change", "check", task), 0)
	}
	if f.callCount() != 6 {
		t.Fatalf("calls = %d, want six explicit consultations", f.callCount())
	}
	f.Git("add", "-A")
	f.Git("commit", "-q", "-m", "portable adviser evidence")
	if err := os.Remove(f.personal); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "offline-clone")
	f.Git("clone", "-q", "--no-hardlinks", f.Dir, clone)
	offline := f.In(clone)
	report := decode[advisers.Status](t, requireCode(t, offline.Kitsu("advisers", "check", "--task", task, "--json"), 0))
	if !report.Ready || len(report.Requirements) != 3 {
		t.Fatalf("offline checkpoint report = %#v", report)
	}
	requireCode(t, offline.Kitsu("advisers", "status", "--task", task, "--json"), 0)
	requireCode(t, offline.Kitsu("advisers", "list", "--task", task, "--json"), 0)
	requireCode(t, offline.Kitsu("advisers", "list", "--json"), 1)
	if f.callCount() != 6 {
		t.Fatal("an offline inspection ran an adviser")
	}
}

func TestSmallTaskPromotion(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "answer", allCheckpoints, false)
	binding := f.bind("small", false)
	if binding.Workflow != "small" || f.Exists(fullFolder) || !f.Exists(smallAdvice+"binding.json") {
		t.Fatal("small binding created a full change folder or used the wrong history location")
	}
	initial := f.check(5, "")
	if len(initial.Requirements) != 1 || initial.Requirements[0].Stage != "review" {
		t.Fatalf("small task requirements = %#v, want review only", initial.Requirements)
	}
	c := f.consult("reviewer", "review", "review-advice", `{"version":1,"kind":"advice","question":"Review the small task."}`, 0)
	f.resolve(c.ID, "accept", "This is sufficient for the small change.", "")
	f.check(0, "")
	original := f.Read(smallAdvice + "exchanges/" + c.ID + "/request.json")
	if got := f.bind("small", false); got.ID != binding.ID {
		t.Fatal("ordinary bind silently refreshed the configuration")
	}
	requireCode(t, f.Kitsu("advisers", "bind", "--task", task, "--workflow", "full"), 1)
	promoted := f.bind("full", true)
	if promoted.ID == binding.ID || promoted.Workflow != "full" || f.Exists(smallAdvice) {
		t.Fatalf("promotion = %#v; small history still exists: %v", promoted, f.Exists(smallAdvice))
	}
	if f.Read(fullAdvice+"exchanges/"+c.ID+"/request.json") != original {
		t.Fatal("promotion rewrote the small consultation")
	}
	for _, name := range []string{"proposal.md", "design.md", "tasks.md", "tests.md", "reviews.md", "specs"} {
		if f.Exists(fullFolder + name) {
			t.Fatalf("promotion invented %s", name)
		}
	}
	promotedStatus := f.check(5, "")
	if len(promotedStatus.Requirements) != 3 {
		t.Fatalf("full requirements = %#v", promotedStatus.Requirements)
	}
	requireState(t, promotedStatus, "review-advice", "satisfied")
	f.addFullRecord()
	for _, stage := range []string{"spec", "design"} {
		role := "spec-writer"
		if stage == "design" {
			role = "architect"
		}
		consultation := f.consult(role, stage, stage+"-advice", `{"version":1,"kind":"advice","question":"Consider the promoted task."}`, 0)
		f.resolve(consultation.ID, "accept", "The full workflow accounts for the added scope.", "")
	}
	f.check(0, "")
	assertTokenAdvisers(t, f.Repo, 3)
}

func TestFailedConsultationsRemainRecorded(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		mode       string
		outcome    string
		answerFile bool
	}{
		{name: "nonzero", mode: "nonzero", outcome: "failed"},
		{name: "timeout", mode: "timeout", outcome: "timed_out"},
		{name: "overflow", mode: "overflow", outcome: "overflow"},
		{name: "empty", mode: "empty", outcome: "failed"},
		{name: "missing answer file", mode: "missing-answer", outcome: "failed", answerFile: true},
		{name: "content changed during call", mode: "mutate", outcome: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, test.mode, reviewCheckpoint, test.answerFile)
			f.addFullRecord()
			f.bind("full", false)
			c := f.consult("reviewer", "review", "review-advice", `{"version":1,"kind":"advice","question":"Consider this change."}`, 1)
			if c.Execution.Outcome != test.outcome || c.Execution.ActualModel != "unknown" || c.Execution.UsageKnown {
				t.Fatalf("failed execution = %#v", c.Execution)
			}
			exchange := fullAdvice + "exchanges/" + c.ID + "/"
			request := f.Read(exchange + "request.json")
			result := f.Read(exchange + "result.json")
			if !strings.Contains(request, `"outcome": "pending"`) || !strings.Contains(result, `"outcome": "`+test.outcome+`"`) {
				t.Fatal("the failed attempt lost its pending request or terminal result")
			}
			if strings.Contains(result, "fixture-secret") || f.callCount() != 1 {
				t.Fatal("diagnostics leaked into evidence or a failed call was retried")
			}
			if test.mode == "nonzero" || test.mode == "timeout" {
				if c.Execution.Answer != "partial recommendation" {
					t.Fatalf("partial answer = %q", c.Execution.Answer)
				}
			}
			if test.mode == "missing-answer" && c.Execution.Answer != "" {
				t.Fatal("stdout replaced the missing authoritative answer file")
			}
			if test.mode == "mutate" && !strings.Contains(c.Execution.Detail, "content changed") {
				t.Fatalf("concurrent content change was not reported: %#v", c.Execution)
			}
			if f.check(5, "").Ready {
				t.Fatal("a failed call satisfied the checkpoint")
			}
			f.resolve(c.ID, "accept", "Record the failure and seek another approach.", "")
			if f.check(5, "").Ready {
				t.Fatal("a disposition turned a failed call into successful evidence")
			}
		})
	}
}

func TestCLIConfigurationAccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "answer", "", false)
	f.Remove(advisers.FileName)
	empty := decode[advisers.Config](t, requireCode(t, f.Kitsu("advisers", "list", "--json"), 0))
	if len(empty.Advisers) != 0 {
		t.Fatal("advisers were preinstalled without configuration")
	}
	personal, _, _ := strings.Cut(f.configuration, "[access]")
	personal = strings.Replace(personal, "configured-model", "personal-shadow", 1)
	personal = strings.Replace(personal, `"5s"`, `"11s"`, 1)
	personal += "[advisers.personal]\nmodel = \"personal-only-model\"\ncommand = [\"unused-cli\", \"{model}\"]\n"
	f.writePersonal(personal)
	f.Write(advisers.FileName, f.configuration)
	merged := decode[advisers.Config](t, requireCode(t, f.Kitsu("advisers", "list", "--json"), 0))
	if len(merged.Advisers) != 2 || merged.Advisers["local"].Model != "configured-model" ||
		merged.Advisers["local"].Timeout != 5*time.Second || merged.Advisers["personal"].Model != "personal-only-model" {
		t.Fatalf("merged command configuration = %#v", merged)
	}
	f.bind("small", false)
	assertRoleTables(t, f.Repo)
	denied := decode[advisers.Config](t, requireCode(t, f.Kitsu("advisers", "list", "--role", "developer", "--json"), 0))
	if len(denied.Advisers) != 0 {
		t.Fatal("an explicit empty role access list inherited defaults")
	}
	allowed := decode[advisers.Config](t, requireCode(t, f.Kitsu("advisers", "list", "--role", "reviewer", "--json"), 0))
	if len(allowed.Advisers) != 1 {
		t.Fatal("an absent role list did not inherit default access")
	}
	bound := decode[advisers.Binding](t, requireCode(t, f.Kitsu("advisers", "list", "--task", task,
		"--role", "developer", "--json"), 0))
	if len(bound.Advisers) != 0 {
		t.Fatal("bound adviser JSON ignored the role filter")
	}
	base := []string{"consult", "--task", task, "--role", "developer", "--stage", "code", "--adviser", "local", "--request", "-"}
	requireCode(t, f.Run(`{"version":1,"kind":"advice","question":"Why?"}`, kitsuProgram(t), base...), 1)
	for _, args := range [][]string{
		{"advisers", "list", "--role", "unknown"},
		{"advisers", "bind", "--task", task, "--workflow", "unexpected"},
		{"consult", "--task", task, "--role", "unknown", "--stage", "review", "--adviser", "local", "--request", "-"},
		{"consult", "--task", task, "--role", "reviewer", "--stage", "review", "--adviser", "local", "--request", "-", "--unrecognized"},
		{"roles", "--host", "unknown"},
	} {
		requireCode(t, f.Kitsu(args...), 2)
	}
	requireCode(t, f.Run(`{"version":1,"kind":"advice","question":"First?","question":"Second?"}`, kitsuProgram(t),
		"consult", "--task", task, "--role", "reviewer", "--stage", "review", "--adviser", "local", "--request", "-"), 2)
	requireCode(t, f.Run(`{"version":1,"kind":"advice","question":"Why?"}`, kitsuProgram(t),
		"consult", "--task", task, "--role", "reviewer", "--stage", "review", "--adviser", "local",
		"--checkpoint", "unknown-checkpoint", "--request", "-"), 1)
	if f.callCount() != 0 || len(paths(t, f.Repo, smallAdvice+"exchanges/*/request.json")) != 0 {
		t.Fatal("denied or malformed requests ran a CLI or created consultation evidence")
	}
	c := f.consult("coordinator", "code", "", `{"version":1,"kind":"advice","question":"Consider the code."}`, 0)
	f.resolve(c.ID, "defer", "Wait for the implementation review.", "")
	old := decode[advisers.Binding](t, requireCode(t, f.Kitsu("advisers", "list", "--task", task, "--json"), 0))
	f.Write(advisers.FileName, strings.Replace(f.configuration, "configured-model", "changed-model", 1))
	if got := f.bind("small", false); got.ID != old.ID {
		t.Fatal("local definition drift silently refreshed an existing binding")
	}
	drift := requireCode(t, f.Run(`{"version":1,"kind":"advice","question":"Consider the code again."}`, kitsuProgram(t),
		"consult", "--task", task, "--role", "coordinator", "--stage", "code", "--adviser", "local", "--request", "-"), 1)
	if !strings.Contains(drift.Output(), "--refresh") || f.callCount() != 1 {
		t.Fatal("definition drift did not require an explicit refresh before invoking a CLI")
	}
	refreshed := f.bind("small", true)
	if refreshed.ID == old.ID || refreshed.Advisers["local"].Model != "changed-model" {
		t.Fatalf("refreshed binding = %#v", refreshed)
	}
	second := f.consult("coordinator", "code", "", `{"version":1,"kind":"advice","question":"Consider the refreshed definition."}`, 0)
	if second.Identity.Model != "changed-model" || second.BindingID != refreshed.ID || f.callCount() != 2 {
		t.Fatalf("refreshed consultation = %#v", second)
	}
	f.Write(advisers.FileName, "version = 2\n")
	requireCode(t, f.Kitsu("advisers", "list", "--json"), 1)
	requireCode(t, f.Kitsu("advisers", "list", "--task", task, "--json"), 0)
	requireCode(t, f.Kitsu("advisers", "status", "--task", task, "--json"), 0)
	requireCode(t, f.Kitsu("roles", "--host", "codex", "--task", task, "--json"), 0)
	if f.callCount() != 2 {
		t.Fatal("offline discovery or status ran an adviser")
	}
}
