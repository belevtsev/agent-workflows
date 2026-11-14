package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
)

// initRun runs cw with the working directory and HOME under base and
// returns stdout and the error.
func initRun(t *testing.T, base string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	home := filepath.Join(base, "home")
	vars := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(base, "xdg-config"), "XDG_STATE_HOME": filepath.Join(base, "xdg-state")}
	err := cli.Execute(context.Background(), args, cli.Config{In: strings.NewReader(""), Out: &out, Err: &errOut,
		HomeDir: func() (string, error) { return home, nil }, WorkingDir: func() (string, error) { return base, nil },
		Getenv: func(k string) string { return vars[k] }})
	return out.String(), err
}

type initPlan struct {
	Title        string
	Kind         string
	Profile      string
	Requirements []struct {
		ID          string
		Description string
		CheckIDs    []string `json:"check_ids"`
	}
	Checks []struct {
		ID   string
		Argv []string
	}
	Checkpoints *struct {
		Required bool
		Reason   string
	}
}

func readInitPlan(t *testing.T, path string) initPlan {
	t.Helper()
	data, err := os.ReadFile(path)
	var plan initPlan
	if err != nil || json.Unmarshal(data, &plan) != nil {
		t.Fatalf("cannot read the plan %s: %v\n%s", path, err, data)
	}
	return plan
}

// Without a Go module cw task init writes a placeholder check that fails, and
// says to replace it. Oracle: plan.json in the working directory names the
// title, profile lite, one substantial requirement bound to check test with
// argv [false], and not-required checkpoints with a reason.
func TestTaskInitWritesPlaceholderWithoutPack(t *testing.T) {
	base := pkResolved(t)
	out, err := initRun(t, base, "task", "init", "--title", "Fix the parser")
	if err != nil || !strings.Contains(out, "replace its argv") || !strings.Contains(out, "cw task start --repo "+base+" --plan "+filepath.Join(base, "plan.json")) {
		t.Fatalf("task init gave %q, %v", out, err)
	}
	plan := readInitPlan(t, filepath.Join(base, "plan.json"))
	if plan.Title != "Fix the parser" || plan.Kind != "substantial" || plan.Profile != "lite" || len(plan.Requirements) != 1 ||
		plan.Requirements[0].Description != "Fix the parser" || strings.Join(plan.Requirements[0].CheckIDs, ",") != "test" ||
		len(plan.Checks) != 1 || plan.Checks[0].ID != "test" || strings.Join(plan.Checks[0].Argv, " ") != "false" ||
		plan.Checkpoints == nil || plan.Checkpoints.Required || plan.Checkpoints.Reason == "" {
		t.Fatalf("placeholder plan is %+v", plan)
	}
}

// In a Go module the check is the go pack's race command; strict keeps the
// checkpoints required; an existing file is never replaced; --dry-run writes
// nothing. Oracle: the decoded --json report and plan, the exit statuses and
// the bytes of the existing file.
func TestTaskInitUsesGoPackAndKeepsExistingFiles(t *testing.T) {
	base := pkResolved(t)
	if err := os.WriteFile(filepath.Join(base, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := initRun(t, base, "task", "init", "--title", "T", "--profile", "strict", "--out", "strict.json", "--json")
	var report struct {
		Plan, Profile, Pack  string
		Written, Placeholder bool
		CheckArgv            []string `json:"check_argv"`
	}
	if err != nil || json.Unmarshal([]byte(out), &report) != nil || report.Plan != filepath.Join(base, "strict.json") || !report.Written ||
		report.Pack != "go" || report.Placeholder || strings.Join(report.CheckArgv, " ") != "go test -race -shuffle=on ./..." {
		t.Fatalf("task init --json gave %q, %v", out, err)
	}
	if plan := readInitPlan(t, report.Plan); plan.Profile != "strict" || plan.Checkpoints != nil || plan.Checks[0].ID != "race" {
		t.Fatalf("strict plan is %+v", plan)
	}
	existing := filepath.Join(base, "plan.json")
	if err := os.WriteFile(existing, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := initRun(t, base, "task", "init", "--title", "T"); cli.ExitCode(err) != 1 || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("init over an existing plan gave %v, want exit 1 naming it", err)
	}
	if data, _ := os.ReadFile(existing); string(data) != "mine" {
		t.Fatalf("init replaced an existing file: %q", data)
	}
	out, err = initRun(t, base, "task", "init", "--title", "T", "--out", "dry.json", "--dry-run")
	if _, statErr := os.Stat(filepath.Join(base, "dry.json")); err != nil || !os.IsNotExist(statErr) || !strings.HasPrefix(out, "Would write") {
		t.Fatalf("--dry-run gave %q, %v and wrote the file (%v)", out, err, statErr)
	}
	for _, args := range [][]string{{"task", "init"}, {"task", "init", "--title", "T", "--profile", "big"}} {
		if _, err := initRun(t, base, args...); cli.ExitCode(err) != 2 {
			t.Errorf("%q gave %v, want exit 2", args, err)
		}
	}
}
