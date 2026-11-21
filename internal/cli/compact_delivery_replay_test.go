package cli

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

const deliveryBehavior = "Preserve anonymous fallback and the complete λ marker. Keep existing tests unchanged. Read src/label.txt and tests/regression.txt; report every finding and blocker.\n"
const deliverySupervision = "Coordinator supervision: start all four fresh roles in order; supervise deadlines and aggregate budgets; resolve policy and skills; prepare and finish every interval; inspect original red then green; maintain task state and review dispositions; obtain required approvals; allow only Git to commit through hooks with current passing checks and a clean final tree. Workers must reread support/task.md and support/compact.md.\n"

// This microreplay isolates delivery/observation overhead. Full red/green,
// review/repair and hooked Git completion are covered by the lifecycle replay.
func TestCompactDeliveryOfflineReplay(t *testing.T) {
	var first *coordinatorReplay
	var expected map[string]string
	var rows []map[string]any
	for _, focused := range []bool{false, true} {
		h := deliveryFixture(t)
		facts := deliveryReplay(h, focused)
		verifyReplayLedger(t, &h.workflowMeter)
		arm := "duplicated_compact"
		if focused {
			arm = "focused_compact"
			if !maps.Equal(expected, facts) || h.calls >= first.calls || h.requestBytes+h.responseBytes >= first.requestBytes+first.responseBytes {
				t.Fatal("focused delivery lost required facts or retained repeated transport", expected, facts)
			}
		} else {
			first, expected = h, facts
		}
		row := map[string]any{"arm": arm, "calls": h.calls, "request_bytes": h.requestBytes, "response_bytes": h.responseBytes,
			"native_reads": h.nativeReads, "checks_executed": h.executed, "worker_returns": h.returns,
			"expansions": h.expansions, "rejected_calls": h.rejected, "full_workflow_measured": false}
		rows = append(rows, row)
		t.Logf("%s", stringMustJSON(row))
	}
	saveDeliveryReport(t, rows)
}

func deliveryFixture(t *testing.T) *coordinatorReplay {
	t.Helper()
	h := coordinatorReplayFixture(t, true)
	h.repo.Setenv("KITSU_TEST_COORDINATOR_QUIET", "1")
	for out, name := range map[string]string{
		"support/task.md":    "skills/task/SKILL.md",
		"support/compact.md": roles.ManagedSkill + "/references/compact.md",
	} {
		body, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		h.repo.Write(out, string(body))
	}
	h.repo.Write("src/label.txt", "anonymous λ\n")
	h.repo.Write("tests/regression.txt", "anonymous\n")
	h.repo.Git("add", "-A")
	h.repo.Git("commit", "-qm", "ready offline delivery fixture")
	return h
}

func deliveryReplay(h *coordinatorReplay, focused bool) map[string]string {
	h.t.Helper()
	facts := map[string]string{"provider_tokens": "unknown", "model_quality": "unknown", "behavior": deliveryBehavior}
	// The coordinator loads the full guides once in both arms; baseline worker
	// rereads are separate charged reads, never inferred from a receipt.
	h.read(h.repo.Path("support/task.md"))
	h.read(h.repo.Path("support/compact.md"))
	if !focused {
		h.run("", "kitsu", 1, "task", "show", workflowTask, "--compact")
		h.rejected++
	}
	for index, role := range []string{"quality-engineer", "developer", "reviewer", "git"} {
		stage := []string{"tests", "code", "review", "commit"}[index]
		assignment := deliveryBehavior + "Perform only the " + role + " role.\n"
		if !focused {
			assignment += deliverySupervision
		}
		d, b := h.prepare(role, stage, 1, assignment)
		if index == 0 {
			view := workflowJSON[coordinatorView](h.t, h.run("", "kitsu", 0, "task", "show", workflowTask, "--compact").Stdout)
			if view.Kind != "snapshot" || view.Scope != coordinatorScope || !strings.Contains(string(view.Sections["prerequisites"]), "not_certified") {
				h.t.Fatal("state before/after history changed authority", view)
			}
		}
		if !focused {
			h.read(h.repo.Path("support/task.md"))
			h.read(h.repo.Path("support/compact.md"))
		}
		source, tests := h.read(h.repo.Path("src/label.txt")), h.read(h.repo.Path("tests/regression.txt"))
		facts[role] = fmt.Sprint(b.Model, b.Writes, b.TestPaths, b.Skills[0].SHA256, b.Constraints, source, tests)
		before := len(h.repo.RecordFiles("regression"))
		args := []string{"check", "run", "--force"}
		if !focused {
			args = append(args, "--compact", "--json")
		}
		result := h.run("", "kitsu", 0, args...)
		if !focused {
			page := h.decodePage(result.Stdout)
			meta := workflowJSON[compactCheckMeta](h.t, string(page.Metadata))
			if len(page.Items) != 1 || !meta.Verified || len(meta.Checks) != 1 || meta.Checks[0].Record == nil {
				h.t.Fatal("compact check lost the actual passing verdict")
			}
		}
		h.executed += len(h.repo.RecordFiles("regression")) - before
		h.currentChecks = []stageCheck{} // No extra status call just to fill optional claims.
		h.complete(d, []string{}, []map[string]any{}, "pass")
	}
	if h.executed != 4 || h.returns != 4 || h.exists(".git/kitsu-stages/active.json") {
		h.t.Fatal("a worker/check was skipped or interval left active")
	}
	return facts
}
