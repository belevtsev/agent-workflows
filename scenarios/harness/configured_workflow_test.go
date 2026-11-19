package harness_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestConfiguredWorkflowRequiresMachineRegisteredRunner(t *testing.T) {
	r := kitsutest.NewRepo(t)
	got := r.Kitsu("task", "run", "Plan a change", "--id", "test", "--until", "plan")
	if got.Code == 0 || !strings.Contains(got.Output(), "configured workflow paused") || r.Exists(".git/kitsu-stages") {
		t.Fatalf("configured run without a registered runner: %s", got.Output())
	}
}

func TestConfiguredWorkflowRejectsLegacyApprovalAndNativeLaunch(t *testing.T) {
	r := kitsutest.NewRepo(t)
	for _, input := range [][]string{
		{"--resume", "test", "--approve-assertion"},
		{"Plan a change", "--native-host", "codex", "--actor", "session"},
	} {
		got := r.Kitsu(append([]string{"task", "run"}, input...)...)
		if got.Code == 0 || !strings.Contains(got.Output(), "runner") && !strings.Contains(got.Output(), "supervised") {
			t.Fatalf("accepted legacy arguments %v: %s", input, got.Output())
		}
	}
}
