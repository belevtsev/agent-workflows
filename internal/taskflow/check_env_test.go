package taskflow

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// passthroughPlan returns plan with names as the env_passthrough of each of
// its checks, set in the plan's JSON as a coordinator writes it and decoded
// as cw task start decodes it.
func passthroughPlan(t *testing.T, plan Plan, names ...string) Plan {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	for _, check := range members["checks"].([]any) {
		check.(map[string]any)["env_passthrough"] = names
	}
	if data, err = json.Marshal(members); err != nil {
		t.Fatal(err)
	}
	var decoded Plan
	if err := decodeBytes(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// TestCheckReceivesTheFilteredVariablesItsEnvPassthroughNames runs a check
// that prints its environment. Oracle: the plan item, "a child that prints
// its environment sees the named variable and not another secret": the
// variable the check's env_passthrough names reaches it with its value
// although its name contains PASSWORD, another variable the filter drops
// does not, and verify lists the named variable as a limitation.
func TestCheckReceivesTheFilteredVariablesItsEnvPassthroughNames(t *testing.T) {
	t.Setenv("CW_PASSTHROUGH_DB_PASSWORD", "named-value")
	t.Setenv("CW_PASSTHROUGH_OTHER_SECRET", "unnamed-value")
	plan := passthroughPlan(t, helperPlan(t, "small", "environ"), "CW_PASSTHROUGH_DB_PASSWORD")
	record, _ := fixtureTask(t, plan)
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatalf("RunCheck: %s, %v", run.Status, err)
	}
	output, err := os.ReadFile(filepath.Join(record.Directory, run.Log))
	if err != nil {
		t.Fatal(err)
	}
	environment := strings.Split(string(output), "\n")
	if !slices.Contains(environment, "CW_PASSTHROUGH_DB_PASSWORD=named-value") {
		t.Errorf("the check did not receive the variable its env_passthrough names:\n%s", output)
	}
	if strings.Contains(string(output), "CW_PASSTHROUGH_OTHER_SECRET") || strings.Contains(string(output), "unnamed-value") {
		t.Errorf("the check received a filtered variable its env_passthrough does not name:\n%s", output)
	}
	want := "check focused received CW_PASSTHROUGH_DB_PASSWORD through env_passthrough, where cw's environment set them"
	if verified := mustVerifyReport(t, record); !slices.Contains(verified.Limitations, want) {
		t.Errorf("verify limitations %q lack %q", verified.Limitations, want)
	}
}

// TestValidatePlanAdmitsOnlyBoundedEnvPassthroughNames starts plans whose
// check names variables in env_passthrough. Oracle: the plan item (names
// only, matching ^[A-Z_][A-Z0-9_]*$, at most 32), a bound of 128 bytes per
// name, and the names the filter or cw's own settings exist to keep from a
// check: BASH_ENV and ENV (a shell's startup file), GOROOT and GOTOOLCHAIN
// (which cw sets for a go check, so a passthrough cannot undo the verified
// go command), and the four tokens gh authenticates with.
func TestValidatePlanAdmitsOnlyBoundedEnvPassthroughNames(t *testing.T) {
	many := func(n int) []string {
		names := make([]string, n)
		for i := range names {
			names[i] = fmt.Sprintf("VAR_%d", i)
		}
		return names
	}
	for _, tc := range []struct {
		names []string
		want  string // "" when the plan is admitted
	}{
		{[]string{"DB_PASSWORD", "GOOGLE_APPLICATION_CREDENTIALS", "_PRIVATE_KEY_2", "GOFLAGS"}, ""},
		{many(32), ""},
		{[]string{strings.Repeat("A", 128)}, ""},
		{many(33), "env_passthrough names at most 32 variables"},
		{[]string{strings.Repeat("A", 129)}, "env_passthrough names a variable of 129 bytes, over 128"},
		{[]string{"db_password"}, `env_passthrough name "db_password" does not match`},
		{[]string{"1PASSWORD"}, `env_passthrough name "1PASSWORD" does not match`},
		{[]string{"DB-PASSWORD"}, `env_passthrough name "DB-PASSWORD" does not match`},
		{[]string{"DB_PASSWORD=x"}, `env_passthrough name "DB_PASSWORD=x" does not match`},
		{[]string{""}, `env_passthrough name "" does not match`},
		{[]string{"DB_PASSWORD", "DB_PASSWORD"}, "env_passthrough names DB_PASSWORD twice"},
		{[]string{"BASH_ENV"}, "env_passthrough cannot name BASH_ENV: a shell"},
		{[]string{"ENV"}, "env_passthrough cannot name ENV: a shell"},
		{[]string{"DB_PASSWORD", "GOROOT"}, "env_passthrough cannot name GOROOT: cw sets it"},
		{[]string{"GOTOOLCHAIN"}, "env_passthrough cannot name GOTOOLCHAIN: cw sets it"},
		{[]string{"GH_TOKEN"}, "env_passthrough cannot name GH_TOKEN: gh publishes with it"},
		{[]string{"GITHUB_TOKEN"}, "env_passthrough cannot name GITHUB_TOKEN: gh publishes with it"},
		{[]string{"GH_ENTERPRISE_TOKEN"}, "env_passthrough cannot name GH_ENTERPRISE_TOKEN: gh publishes with it"},
		{[]string{"GITHUB_ENTERPRISE_TOKEN"}, "env_passthrough cannot name GITHUB_ENTERPRISE_TOKEN: gh publishes with it"},
	} {
		label := strings.Join(tc.names, ",")
		if len(label) > 40 {
			label = label[:40]
		}
		t.Run(label, func(t *testing.T) {
			plan := passthroughPlan(t, helperPlan(t, "small", "pass"), tc.names...)
			err := ValidatePlan(&plan)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidatePlan refused %q: %v", tc.names, err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), "check focused: "+tc.want) {
				t.Fatalf("ValidatePlan(%q) = %v; want an error starting %q", tc.names, err, "check focused: "+tc.want)
			}
		})
	}
}

// TestRunCheckRefusesAStoredEnvPassthroughThatAdmissionRefuses rewrites a
// stored plan so that its check's env_passthrough names GITHUB_TOKEN.
// Oracle: like a check's argv (validateCommandShape against validateCommand),
// a stored plan is checked for shape only, so it still loads, but RunCheck
// admits the check again and refuses it before it records or runs anything; a
// stored name of the wrong shape does not load.
func TestRunCheckRefusesAStoredEnvPassthroughThatAdmissionRefuses(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "publication-token")
	record, _ := fixtureTask(t, helperPlan(t, "small", "environ"))
	record = rewriteFirstRevision(t, record, func(first *Record) {
		first.Plan = passthroughPlan(t, first.Plan, "GITHUB_TOKEN")
	})
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	if want := "check focused: env_passthrough cannot name GITHUB_TOKEN"; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("RunCheck = %s, %v; want an error starting %q", run.Status, err, want)
	}
	if loaded, err := Load(record.Directory); err != nil || loaded.Revision != record.Revision || len(loaded.Checks) != 0 {
		t.Errorf("RunCheck wrote revision %d with %d runs (%v); want none", loaded.Revision, len(loaded.Checks), err)
	}
	plan := passthroughPlan(t, helperPlan(t, "small", "pass"), "lower")
	if err := validateRecordedPlan(plan); err == nil || !strings.Contains(err.Error(), `env_passthrough name "lower" does not match`) {
		t.Errorf("a stored plan whose env_passthrough names lower loads: %v", err)
	}
}

// TestCheckSpecKeepsTheVerifiedGoAndTheGoRuleOverEnvPassthrough builds a go
// check whose env_passthrough names GOROOT, GOTOOLCHAIN and GOFLAGS, as a
// record that bypassed admission could. Oracle: os/exec uses the last value
// of a repeated variable, so GOROOT and GOTOOLCHAIN=local of the verified go
// command (G6) still win over cw's environment; GOFLAGS, which reaches every
// check whether named or not, is recorded as the check sees it, and the Go
// rule gives a run with -exec or -toolexec in it no credit.
func TestCheckSpecKeepsTheVerifiedGoAndTheGoRuleOverEnvPassthrough(t *testing.T) {
	verified := langpack.GoTool{Path: "/verified/bin/go", Version: "go1.27.4", GOROOT: "/verified"}
	resolveGo = func(context.Context, string, string, []string) (langpack.GoTool, error) { return verified, nil }
	t.Cleanup(func() { resolveGo = langpack.ResolveGo })
	t.Setenv("GOROOT", "/elsewhere")
	t.Setenv("GOTOOLCHAIN", "go1.99.0+path")
	argv := []string{"go", "test", "-json", "./..."}
	plan := executionPlan(argv)
	plan.Checks[0].TimeoutSeconds = 10
	check := passthroughPlan(t, plan, "GOFLAGS", "GOROOT", "GOTOOLCHAIN").Checks[0]
	last := func(environment []string, name string) string {
		value := ""
		for _, entry := range environment {
			if key, v, _ := strings.Cut(entry, "="); key == name {
				value = v
			}
		}
		return value
	}
	for _, goflags := range []string{"-exec=/bin/true", "-toolexec=/bin/true"} {
		t.Setenv("GOFLAGS", goflags)
		spec, tool, err := checkSpec(t.Context(), check, t.TempDir())
		if err != nil || tool == nil || *tool != verified {
			t.Fatalf("checkSpec: tool %+v, %v; want %+v", tool, err, verified)
		}
		if got := last(spec.Env, "GOROOT"); got != "/verified" {
			t.Errorf("the go check runs with GOROOT %q, want the verified tree", got)
		}
		if got := last(spec.Env, "GOTOOLCHAIN"); got != "local" {
			t.Errorf("the go check runs with GOTOOLCHAIN %q, want local", got)
		}
		recorded := langpack.RedactGOFLAGS(environmentValue(spec.Env, "GOFLAGS"))
		if recorded != goflags || last(spec.Env, "GOFLAGS") != goflags {
			t.Errorf("GOFLAGS recorded %q, seen %q; want %q for both", recorded, last(spec.Env, "GOFLAGS"), goflags)
		}
		if langpack.GoTestsCredited(langpack.Run{Argv: argv, GOFLAGS: recorded}) {
			t.Errorf("a run with GOFLAGS %q earns Go rule credit", goflags)
		}
	}
	if !langpack.GoTestsCredited(langpack.Run{Argv: argv}) {
		t.Fatal("precondition: the argv without GOFLAGS earns no credit")
	}
}
