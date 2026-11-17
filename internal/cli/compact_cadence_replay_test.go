package cli

import (
	"maps"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Reuse the actual red/repair/review/hooked-commit oracle. A smaller transport
// result is useful only when all required facts and the final endpoint survive.
func TestCompactCadenceLifecycleEquivalence(t *testing.T) {
	full, lean := coordinatorReplayFixture(t, true), coordinatorReplayFixture(t, true)
	lean.lean, lean.cadence = true, true
	full.lifecycle()
	lean.lifecycle()
	if !maps.Equal(full.facts, lean.facts) {
		t.Fatal("lean cadence lost original inputs, findings or task completion", full.facts, lean.facts)
	}
	for _, h := range []*coordinatorReplay{full, lean} {
		verifyReplayLedger(t, &h.workflowMeter)
		if h.executed != 3 || h.reused != 2 || h.returns != 6 || h.facts["done"] == "" || h.facts["provider_usage"] != "unknown" || h.facts["model_quality"] != "unknown" {
			t.Fatal("transport change weakened lifecycle evidence", h.facts, h.executed, h.reused, h.returns)
		}
		followups, dispatches := 0, 0
		for _, record := range h.transports {
			if record.Boundary == "fake-native-followup" {
				followups++
			}
			if record.Boundary == "fake-native-dispatch" {
				dispatches++
			}
			if h.lean && strings.Contains(record.Request, "kitsu task dispatch ") {
				t.Fatal("launch required another response to recover mandatory fields")
			}
		}
		if dispatches != 6 || h.cadence && followups != 0 || !h.cadence && followups != 6 {
			t.Fatal("missing native work or redundant completion followups", dispatches, followups)
		}
	}
	if full.calls-lean.calls != 6 || lean.requestBytes+lean.responseBytes >= full.requestBytes+full.responseBytes {
		t.Fatal("scripted launch/cadence transport did not remove repeated handbacks")
	}
	t.Logf("offline host transports: full=%d calls/%d bytes; launch/final-return=%d calls/%d bytes; provider tokens and model quality unknown", full.calls, full.requestBytes+full.responseBytes, lean.calls, lean.requestBytes+lean.responseBytes)
}

type cadenceTransition struct {
	Finish, Prepare kitsutest.Result
	Prepared        bool
}

// This is the documented host recipe, not a new CLI stage-advancement API.
// The caller must already have inspected semantic red/review decisions and
// observed terminal native completion. Each real CLI result is still metered.
func replayCadenceTransition(h *coordinatorReplay, id, assignment string, args []string, finishCode, prepareCode int) cadenceTransition {
	h.t.Helper()
	out := cadenceTransition{Finish: h.run("", "kitsu", finishCode, "task", "finish", id, "--expect-checks", "pass")}
	if out.Finish.Code != 0 {
		return out
	}
	out.Prepare = h.run(assignment, "kitsu", prepareCode, args...)
	out.Prepared = out.Prepare.Code == 0
	return out
}

func TestCompactCadenceFinishFailureBlocksPreparation(t *testing.T) {
	h := coordinatorReplayFixture(t, true)
	d, _ := h.prepare("reviewer", "review", 1, "Inspect primary source without writing any file.\n")
	h.write("src/label.txt", "unexpected reviewer write\n")
	id := d["dispatch_id"].(string)
	before := len(h.transports)
	next := []string{"task", "prepare", workflowTask, "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--dispatch", "--view", "launch"}
	result := replayCadenceTransition(h, id, "Review primary source independently.\n", next, 1, 0)
	if !strings.Contains(result.Finish.Stderr, "write") {
		t.Fatal("fixture failed without observing the actual write violation", result)
	}
	if result.Prepared || len(h.transports) != before+1 || h.repo.Exists(".git/kitsu-stages/"+id+".finish.json") || !h.repo.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("failed write validation advanced or released the interval", result)
	}
	if h.facts["done"] != "" {
		t.Fatal("failed transition claimed task completion")
	}
	verifyReplayLedger(t, &h.workflowMeter)
}

func TestCompactCadencePreparationFailurePreservesFinishedInterval(t *testing.T) {
	h := coordinatorReplayFixture(t, true)
	h.repo.Write("src/label.txt", "anonymous λ\n")
	h.repo.Write("tests/regression.txt", "anonymous\n")
	d, _ := h.prepare("reviewer", "review", 1, "Review primary source independently; report every finding.\n")
	h.check(0, true, false)
	id := d["dispatch_id"].(string)
	// Invalid next-role input fails after a valid finish. It must not roll back
	// that finish or invent a new active worker/completed task.
	next := []string{"task", "prepare", workflowTask, "--role", "unknown", "--stage", "commit", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--dispatch", "--view", "launch"}
	before := len(h.transports)
	result := replayCadenceTransition(h, id, "Commit through normal hooks.\n", next, 0, 2)
	if result.Prepared || len(h.transports) != before+2 || !h.repo.Exists(".git/kitsu-stages/"+id+".finish.json") || h.repo.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("partial transition lost actual finish or invented preparation", result)
	}
	if h.facts["done"] != "" {
		t.Fatal("failed preparation claimed completion")
	}
	verifyReplayLedger(t, &h.workflowMeter)
}
