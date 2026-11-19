package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func TestConfiguredSharedFileKeepsRoleEditRegionsSeparate(t *testing.T) {
	dir := t.TempDir()
	const name = "module.source"
	region := harness.WorkflowTestRegion{Path: name, First: "begin tests", Last: "end tests"}
	write := func(production, test string) map[string]harness.WorkflowSharedFingerprint {
		t.Helper()
		body := production + "\nbegin tests\n" + test + "\nend tests\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := workflowSharedFingerprints(dir, []harness.WorkflowTestRegion{region})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := write("original code", "original test")
	plan := &harness.WorkflowPlan{CodePaths: []string{name}, TestPaths: []string{name}, Shared: []harness.WorkflowTestRegion{region}}
	for _, tc := range []struct {
		role, production, test string
		allowed                bool
	}{
		{"quality-engineer", "original code", "new test", true},
		{"quality-engineer", "changed code", "new test", false},
		{"developer", "changed code", "original test", true},
		{"developer", "changed code", "weakened test", false},
	} {
		w := harness.Workflow{Plan: plan, TestPolicy: []string{name}, Active: &harness.WorkflowAttempt{Role: tc.role, Shared: before}}
		if got := workflowPathAllowed(&w, name, write(tc.production, tc.test)); got != tc.allowed {
			t.Errorf("%s with %q and %q allowed=%t, want %t", tc.role, tc.production, tc.test, got, tc.allowed)
		}
	}
}
