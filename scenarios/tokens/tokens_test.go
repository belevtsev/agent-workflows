package tokens_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Scenario: A recorded session
func TestARecordedSession(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	out := tokensJSON(t, repo, "--task", "add-greeting")
	wantNumber(t, out, "total", 655000)
	if out["complete"] != true {
		t.Errorf("complete is %v, want true", out["complete"])
	}
}

// Scenario: A directory that is not gated
func TestADirectoryThatIsNotGated(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.NewRepo(t)
	s := buildS1(t, variant{})
	for i, n := range s.Notices {
		if r := recordNotice(t, repo, s.Host, n); r.Code != 0 {
			t.Errorf("notice %d: exit status %d, want 0\n%s", i+1, r.Code, r.Output())
		}
	}
	if _, err := os.Lstat(repo.StoreDir()); err == nil {
		t.Errorf("%s was created", repo.StoreDir())
	}
	if got := repo.Git("status", "--porcelain", "--ignored", "--untracked-files=all"); got != "" {
		t.Errorf("files were written in the repository:\n%s", got)
	}
	var written []string
	_ = filepath.WalkDir(repo.Home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			written = append(written, path)
		}
		return nil
	})
	if len(written) > 0 {
		t.Errorf("files were written in the home directory: %v", written)
	}
}

// Scenario: A notice is given twice
func TestANoticeIsGivenTwice(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	s := buildS1(t, variant{})
	record(t, repo, s)
	developer := -1
	for i, st := range s1Starts() {
		if st.Label == "gatecub-task: add-greeting code 1" {
			developer = i
		}
	}
	if r := recordNotice(t, repo, s.Host, s.Notices[developer]); r.Code != 0 {
		t.Fatalf("the notice given again: exit status %d\n%s", r.Code, r.Output())
	}
	wantNumber(t, tokensJSON(t, repo, "--task", "add-greeting"), "total", 655000)
}

// Scenario: Tokens go to the role of the stage
func TestTokensGoToTheRoleOfTheStage(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	roles := entries(t, tokensJSON(t, repo, "--task", "add-greeting"), "roles")
	reviewer := find(t, roles, map[string]any{"role": "reviewer"})
	wantNumber(t, reviewer, "starts", 4)
	wantNumber(t, reviewer, "total", 130000)
	developer := find(t, roles, map[string]any{"role": "developer"})
	wantNumber(t, developer, "starts", 2)
	wantNumber(t, developer, "total", 240000)
}

// Scenario: A start without a label
func TestAStartWithoutALabel(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{NoGitLabel: true})
	out := tokensJSON(t, repo)
	unlabelled := entries(t, out, "unlabelled")
	if len(unlabelled) != 1 {
		t.Fatalf("unlabelled holds %d entries, want one start:\n%s", len(unlabelled), show(unlabelled))
	}
	wantNumber(t, unlabelled[0], "total", 5000)
	if starts, ok := unlabelled[0]["starts"]; ok && starts != float64(1) {
		t.Errorf("the unlabelled entry has starts %v, want 1", starts)
	}
	task := find(t, entries(t, out, "tasks"), map[string]any{"task": "add-greeting"})
	wantNumber(t, task, "total", 650000)
}

// Scenario: The four kinds
func TestTheFourKinds(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	stages := entries(t, tokensJSON(t, repo, "--task", "add-greeting"), "stages")
	code := find(t, stages, map[string]any{"stage": "code", "round": 1})
	if code["role"] != "developer" {
		t.Errorf("role is %v, want developer", code["role"])
	}
	wantNumber(t, code, "input", 20000)
	wantNumber(t, code, "cache_write", 30000)
	wantNumber(t, code, "cache_read", 140000)
	wantNumber(t, code, "output", 10000)
	wantNumber(t, code, "total", 200000)
}

// Scenario: No money
func TestNoMoney(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	out := tokensText(t, repo, "--task", "add-greeting")
	if strings.TrimSpace(out) == "" {
		t.Fatal("gatecub tokens --task add-greeting printed nothing")
	}
	if i := strings.IndexAny(out, "$€£¥₴₹₽¢₩₺₪฿"); i >= 0 {
		t.Errorf("the output holds a currency sign at %d:\n%s", i, out)
	}
	price := regexp.MustCompile(`(?i)\b(price[sd]?|pricing|usd|eur|gbp|dollars?|euros?)\b`)
	if m := price.FindString(out); m != "" {
		t.Errorf("the output names a price: %q\n%s", m, out)
	}
}

// Scenario: The split of one task
func TestTheSplitOfOneTask(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	out := tokensJSON(t, repo, "--task", "add-greeting")
	wantNumber(t, out, "writing", 430000)
	flow, ok := field(t, out, "flow").(map[string]any)
	if !ok {
		t.Fatalf("flow is not an object: %s", show(out["flow"]))
	}
	wantNumber(t, flow, "total", 225000)
	wantNumber(t, flow, "coordination", 40000)
	wantNumber(t, flow, "review", 130000)
	wantNumber(t, flow, "lookup", 10000)
	wantNumber(t, flow, "git", 5000)
	wantNumber(t, flow, "rework", 40000)
	wantNumber(t, out, "flow_share", 34)
}

// Scenario: The report in words
func TestTheReportInWords(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	out := strings.Join(strings.Fields(tokensText(t, repo, "--task", "add-greeting")), " ")
	const want = "225,000 of 655,000 tokens, 34%, went to the workflow and not to the first writing of the specification, the design, the tests and the code"
	if !strings.Contains(out, want) {
		t.Errorf("the output does not say %q:\n%s", want, out)
	}
}

// Scenario: Two tasks in one session
func TestTwoTasksInOneSession(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{SecondTask: true})
	out := tokensJSON(t, repo)
	session := find(t, entries(t, out, "sessions"), map[string]any{"session": s1Session})
	wantNumber(t, session, "total", 40000)
	tasks, _ := session["tasks"].([]any)
	names := map[any]bool{}
	for _, n := range tasks {
		names[n] = true
	}
	if len(tasks) != 2 || !names["add-greeting"] || !names["fix-typo"] {
		t.Errorf("the session names the tasks %v, want add-greeting and fix-typo", session["tasks"])
	}
	task := find(t, entries(t, out, "tasks"), map[string]any{"task": "add-greeting"})
	wantNumber(t, task, "total", 615000)
}

// Scenario: A record that cannot be read
func TestARecordThatCannotBeRead(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{EmptyArchitect: true})
	out := tokensJSON(t, repo, "--task", "add-greeting")
	architect := find(t, entries(t, out, "roles"), map[string]any{"role": "architect"})
	if architect["total"] != "unknown" {
		t.Errorf("the architect's total is %v, want unknown", architect["total"])
	}
	if out["complete"] != false {
		t.Errorf("complete is %v, want false", out["complete"])
	}
	wantNumber(t, out, "total", 575000)
}

// Scenario: The report says that it is incomplete
func TestTheReportSaysThatItIsIncomplete(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{EmptyArchitect: true})
	out := strings.Join(strings.Fields(tokensText(t, repo, "--task", "add-greeting")), " ")
	one := regexp.MustCompile(`(?i)\b(1|one)\b start[^.]*\bunknown\b|\bunknown\b[^.]*\b(1|one)\b start`)
	if !one.MatchString(out) {
		t.Errorf("the output does not say that the tokens of one start are unknown:\n%s", out)
	}
	atLeast := regexp.MustCompile(`(?i)every sum is at least the number shown`)
	if !atLeast.MatchString(out) {
		t.Errorf("the output does not say that every sum is at least the number shown:\n%s", out)
	}
}

// Scenario: No text is kept
func TestNoTextIsKept(t *testing.T) {
	t.Parallel()
	repo := recorded(t, variant{})
	files := 0
	err := filepath.WalkDir(repo.StoreDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path) //nolint:gosec // a file under the test's repository
		if err != nil {
			return err
		}
		if strings.Contains(string(data), developerSentence) || strings.Contains(string(data), "Make the greeter print") {
			t.Errorf("%s holds a sentence of the developer brief", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("no file under %s: nothing was recorded to search", repo.StoreDir())
	}
}

// Scenario: A Codex session
func TestACodexSession(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	record(t, repo, buildS2(t))
	out := tokensJSON(t, repo, "--task", "fix-empty-app")
	roles := entries(t, out, "roles")
	unknown := false
	for _, role := range []string{"quality-engineer", "developer", "reviewer", "git"} {
		e := find(t, roles, map[string]any{"role": role})
		switch total := e["total"].(type) {
		case float64:
		case string:
			if total != "unknown" {
				t.Errorf("%s: total %q, want a number or unknown", role, total)
			}
			unknown = true
		default:
			t.Errorf("%s: total %v, want a number or unknown", role, e["total"])
		}
	}
	// The record of the git start holds only token_count events without
	// info: it holds no count.
	if e := find(t, roles, map[string]any{"role": "git"}); e["total"] != "unknown" {
		t.Errorf("git: total %v, want unknown (its record holds no count)", e["total"])
	}
	// Each other start is counted once from its last running total, though
	// a total is written twice, and the session's own 20,000 are added.
	for role, want := range map[string]int{"quality-engineer": 30000, "developer": 50000, "reviewer": 15000} {
		wantNumber(t, find(t, roles, map[string]any{"role": role}), "total", want)
	}
	wantNumber(t, out, "total", 115000)
	if unknown && out["complete"] != false {
		t.Errorf("complete is %v, want false when a role is unknown", out["complete"])
	}
}
