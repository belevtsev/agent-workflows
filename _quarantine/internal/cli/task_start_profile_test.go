package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: docs/operations.md "Profiles". cw task start with a plan that names
// no profile records lite and its summary says so; a plan whose budget is
// above its profile's exits 1 as invalid input and creates no task.
func TestTaskStartRecordsTheProfileAndRefusesAHigherBudget(t *testing.T) {
	h := newSummaryCLIHarness(t)
	start := parseSummaryLineForTest(t, "start", h.start(true), "task_id", "profile", "advisers", "requirements", "checks")
	if got := summaryStringForTest(t, start, "profile"); got != "lite" {
		t.Errorf("start summary profile %q, want lite", got)
	}
	h.task = h.directoryOf(summaryStringForTest(t, start, "task_id"))
	if record := h.load(); record.Profile == nil || record.Profile.Name != "lite" || record.Profile.Budget.MaxUSD != 15 {
		t.Errorf("recorded profile %+v, want lite with 15 USD", record.Profile)
	}

	h = newSummaryCLIHarness(t)
	plan := strings.Replace(summaryCLITestPlan, `"kind":"small",`, `"kind":"small","profile":"standard","budget":{"max_usd":41},`, 1)
	if plan == summaryCLITestPlan {
		t.Fatal("precondition: the plan has no kind member to extend")
	}
	store := filepath.Join(t.TempDir(), "store")
	h.config.In = bytes.NewReader([]byte(plan))
	err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath, "--store", store}, h.config)
	const refused = "plan budget max_usd 41 exceeds profile standard's 40; a plan budget may only lower the profile's"
	if err == nil || err.Error() != refused || !errors.Is(err, taskflow.ErrInvalidInput) || ExitCode(err) != ExitFailure {
		t.Fatalf("start with a higher budget = %v (exit %d), want %q with exit %d", err, ExitCode(err), refused, ExitFailure)
	}
	if entries, _ := os.ReadDir(store); len(entries) != 0 {
		t.Errorf("a refused start left %d entries in the task store", len(entries))
	}
}
