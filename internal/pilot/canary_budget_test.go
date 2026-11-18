package pilot

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

func canaryUnitManifest(t *testing.T) (string, string, manifest) {
	t.Helper()
	path, output := unitManifest(t)
	var m manifest
	if err := readJSON(path, &m); err != nil {
		t.Fatal(err)
	}
	m.Profile, m.Effort, m.Repeats, m.Controllers, m.TimeoutSeconds = canaryProfile, "high", 1, 1, 480
	m.Cases = m.Cases[:2]
	m.Budget = &canaryBudget{Limit: 1000, StopReserve: 200, LaunchReserve: 200, PollMilliseconds: 10, NoCounterSeconds: 2}
	if err := writeJSON(path, m, false); err != nil {
		t.Fatal(err)
	}
	return path, output, m
}

func TestCanaryProfileDoesNotWeakenStudyValidation(t *testing.T) {
	path, output, m := canaryUnitManifest(t)
	if validStudyProfile(m) || !validCanaryProfile(m) {
		t.Fatal("canary leaked into original study profile")
	}
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	frozen, p, err := loadFrozen(t.Context(), output)
	if err != nil || len(p.Runs) != 4 || frozen.Effort != "high" {
		t.Fatal("canary freeze failed", p, err)
	}
	for _, test := range []struct {
		name   string
		change func(*manifest)
	}{
		{"no-explicit-profile", func(m *manifest) { m.Profile = "" }},
		{"wrong-model", func(m *manifest) { m.Model = "gpt-6-astra" }},
		{"wrong-effort", func(m *manifest) { m.Effort = "max" }},
		{"retries", func(m *manifest) { m.Repeats = 3 }},
		{"concurrency", func(m *manifest) { m.Controllers = 2 }},
		{"third-case", func(m *manifest) { m.Cases = append(m.Cases, pilotCase{ID: "third"}) }},
		{"missing-budget", func(m *manifest) { m.Budget = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := m
			test.change(&changed)
			if validStudyProfile(changed) || validCanaryProfile(changed) {
				t.Fatal("undeclared canary profile accepted")
			}
		})
	}
	rows := make([]analysisRun, 0, 4)
	for _, run := range p.Runs {
		row, err := analysisObserved(run, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	report := analysisBuild(m, p, rows)
	if len(report.Runs) != 4 || len(report.Pairs) != 2 || report.AllResults {
		t.Fatal("analysis lost four assigned identities")
	}
}

func TestCanaryLiveCounterDeduplicatesResetsAndPartialTails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trace.jsonl")
	data := fakeRollout("root-001", "", "", "normal", 100, 20, 10, 1)
	// The same event repeated is not an additional request. A subsequent reset
	// is additional usage; reasoning is already included in output.
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var token string
	for _, line := range lines {
		if strings.Contains(line, `"token_count"`) {
			token = line
		}
	}
	data = append(data, []byte(token+"\n"+`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"cached_input_tokens":5,"cache_write_input_tokens":0,"output_tokens":3}}}}`+"\n"+`{"partial":`)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	id, c, err := canaryTrace(path)
	if err != nil || id != "root-001" || c.Captured != 143 {
		t.Fatal("usage duplicated, lost reset, or counted reasoning twice", c, err)
	}
}

func TestCanaryBudgetStopsLocalDescendantsAndFurtherLaunches(t *testing.T) {
	_, output, m := canaryUnitManifest(t)
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := schedule(m, "").Runs[0]
	release, err := canaryClaim(output, *m.Budget)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(output, "runs", selected.ID)
	vars := map[string]string{"run_dir": runDir, "home": filepath.Join(runDir, "home"), "worktree": filepath.Join(runDir, "worktree")}
	if err := os.MkdirAll(vars["worktree"], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vars["home"], 0o700); err != nil {
		t.Fatal(err)
	}
	// Hold the gate until after the liveness check. A surviving grandchild must
	// still hold its own lock even if polling or process cleanup was delayed.
	gate, err := fslock.Acquire(t.Context(), filepath.Join(vars["home"], "descendant-exit.lock"), fslock.Options{Wait: fslock.NoWait})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gate.Close() })
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m.Host.Argv = []string{program, "-test.run=^TestCanaryBudgetProcess$", "-pilot-test-helper=budget-root"}
	// A second controller cannot start even before the first counter appears.
	if second, err := canaryClaim(output, *m.Budget); err == nil {
		second()
		t.Fatal("parallel launch accepted")
	}
	execution, err := canaryInvoke(t.Context(), m, output, selected, vars, "", executionEnv(m.Host, vars))
	release()
	if err != nil || !execution.Stopped || !execution.Cancelled || execution.Cleanup != "" {
		t.Fatal("budget failed to stop fake process tree", execution, err)
	}
	l, err := canaryReadLedger(output, *m.Budget)
	if err != nil || l.Captured != 825 || len(l.Counters) != 3 || !l.TailUnknown || l.StopReason != "captured_soft_stop_reserve" {
		t.Fatal("all descendant counters not charged", l, err)
	}
	if next, err := canaryClaim(output, *m.Budget); err == nil {
		next()
		t.Fatal("launch accepted after reserve stop and unknown tail")
	}
	// The final counter was written while the grandchild held this lock. The
	// kernel releases it on process exit. Allow for delayed Windows lock release.
	liveness, err := fslock.Acquire(t.Context(), filepath.Join(vars["home"], "descendant-alive.lock"),
		fslock.Options{NoCreate: true, Wait: 5 * time.Second})
	if err != nil {
		t.Fatal("stop did not release the grandchild's liveness lock", err)
	}
	if err := liveness.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCanaryLaunchReserveAndMissingResultFailClosed(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured int64
		tail     bool
		claimed  bool
	}{
		{name: "reserve", captured: 800}, {name: "tail", captured: 10, tail: true}, {name: "missing-result", claimed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := t.TempDir()
			b := canaryBudget{Limit: 1000, StopReserve: 200, LaunchReserve: 200}
			l, _ := canaryReadLedger(output, b)
			l.Captured = test.captured
			l.TailUnknown = test.tail
			if test.captured > 0 {
				l.Counters["run/thread"] = canaryCounter{Captured: test.captured}
			}
			if err := writeJSON(filepath.Join(output, "canary-budget.json"), l, false); err != nil {
				t.Fatal(err)
			}
			if test.claimed {
				if err := os.MkdirAll(filepath.Join(output, "runs", "interrupted"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if release, err := canaryClaim(output, b); err == nil {
				release()
				t.Fatal("unsafe further launch accepted")
			}
		})
	}
}

// Local fake processes only: root -> worker -> grandchild, plus a duplicate
// rollout alias. No model binary, API, or network call is involved.
func TestCanaryBudgetProcess(t *testing.T) {
	if !strings.HasPrefix(*pilotHelper, "budget-") {
		t.Skip("offline fake child only")
	}
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stage := strings.TrimPrefix(*pilotHelper, "budget-")
	id, parent, input := "root-001", "", int64(100)
	switch stage {
	case "worker":
		id, parent, input = "worker-001", "root-001", 200
	case "grandchild":
		id, parent, input = "grandchild-001", "worker-001", 510
		liveness, err := fslock.Acquire(t.Context(), filepath.Join(home, "descendant-alive.lock"),
			fslock.Options{Wait: fslock.NoWait})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = liveness.Close() }()
	}
	data := fakeRollout(id, parent, "", "normal", input, 0, 5, 1)
	if err := os.WriteFile(filepath.Join(dir, stage+".jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if stage == "root" {
		if err := os.WriteFile(filepath.Join(dir, "alias.jsonl"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if stage != "grandchild" {
		next := "worker"
		if stage == "worker" {
			next = "grandchild"
		}
		program, _ := os.Executable()
		child := exec.Command(program, "-test.run=^TestCanaryBudgetProcess$", "-pilot-test-helper=budget-"+next)
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = child.Wait() }()
	}
	if stage == "grandchild" {
		gate, err := fslock.Acquire(t.Context(), filepath.Join(home, "descendant-exit.lock"), fslock.Options{NoCreate: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := gate.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	time.Sleep(5 * time.Second)
}

func TestCanaryCumulativeBudgetAcrossRuns(t *testing.T) {
	_, output, m := canaryUnitManifest(t)
	runs := schedule(m, "").Runs
	l, _ := canaryReadLedger(output, *m.Budget)
	for i, run := range runs[:2] {
		dir := filepath.Join(output, "runs", run.ID, "home", m.Host.Rollouts)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "root.jsonl"), fakeRollout("same-thread-id", "", "", "normal", int64(300+i*100), 0, 10, 1), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := canaryRefresh(output, m, &l); err != nil {
		t.Fatal(err)
	}
	if l.Captured != 720 || len(l.Counters) != 2 {
		t.Fatal("run-scoped identities lost", l)
	}
	before, _ := json.Marshal(l.Counters)
	if err := canaryRefresh(output, m, &l); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(l.Counters)
	if string(before) != string(after) || l.Captured != 720 {
		t.Fatal("polling charged counters twice")
	}
	broken := filepath.Join(output, "runs", runs[0].ID, "home", m.Host.Rollouts, "root.jsonl")
	f, err := os.OpenFile(broken, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("malformed-complete-event\n")
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	fresh, _ := canaryReadLedger(output, *m.Budget)
	if err := canaryRefresh(output, m, &fresh); err == nil || fresh.Captured != 720 {
		t.Fatal("damaged tail erased known prefix or other descendant counters", fresh, err)
	}
}

func TestCanaryShortNativeChecksDoNotRequireCompactRetrieval(t *testing.T) {
	threads := []traceInfo{{ID: "root", Models: []string{"gpt-6.1-sol"}, Efforts: []string{"high"}}}
	for _, stage := range []string{"tests", "code", "review", "commit"} {
		threads = append(threads, traceInfo{ID: stage, ParentID: "root", Stage: stage, Models: []string{"gpt-6.1-sol"}, Efforts: []string{"high"}})
	}
	goCase := cell{CaseID: "pflag-unknown-flags", Arm: "compact"}
	if got := canaryMethod(threads, goCase, "gpt-6.1-sol", "high"); got.Status != "unknown" || len(got.Findings) != 0 {
		t.Fatal("native short Go check was forced to compact", got)
	}
	noisy := goCase
	noisy.CaseID = "qs-sparse-array-limit"
	if got := canaryMethod(threads, noisy, "gpt-6.1-sol", "high"); got.Status != "violated" {
		t.Fatal("noisy canary check failed to enforce assigned arm", got)
	}
	threads[0].Calls = []traceCall{{Name: "exec_command", Arguments: json.RawMessage(`{"cmd":"kitsu check run --compact"}`)}}
	if got := canaryMethod(threads, noisy, "gpt-6.1-sol", "high"); got.Status != "unknown" {
		t.Fatal("check-only compact arm incorrectly required repository compaction", got)
	}
	noisy.Arm = "normal"
	if got := canaryMethod(threads, noisy, "gpt-6.1-sol", "high"); got.Status != "violated" {
		t.Fatal("normal canary arm accepted compact checks", got)
	}
}

func TestCanaryOfflineRunRetainsGradeUsageAndRemovesCopiedCredential(t *testing.T) {
	path, output, m := canaryUnitManifest(t)
	m.Budget.Limit = 4000
	credential := filepath.Join(filepath.Dir(path), "fake-auth.json")
	if err := os.WriteFile(credential, []byte("offline-test-credential\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.Host.HomeFiles = []homeFile{{Source: credential, Destination: ".codex/auth.json"}}
	if err := writeJSON(m.Calibration.Path, map[string]any{"version": 1, "approved": true, "model": m.Model, "effort": "high", "subagents_verified": true, "usage_verified": true, "isolation_verified": true, "cli_version": "offline-fake"}, false); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, m, false); err != nil {
		t.Fatal(err)
	}
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	selected := p.Runs[0]
	if err := executeCell(t.Context(), m, p, selected, output); err != nil {
		t.Fatal(err)
	}
	var result runResult
	runDir := filepath.Join(output, "runs", selected.ID)
	if err := readJSON(filepath.Join(runDir, "result.json"), &result); err != nil {
		t.Fatal(err)
	}
	ledger, err := canaryReadLedger(output, *m.Budget)
	if err != nil || ledger.Captured != 618 || result.Usage.Total != 618 || !functionalPass(&result) {
		t.Fatal("canary lost original grading or all-thread accounting", ledger, result, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "home", ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("copied credential retained", err)
	}
	if data, err := os.ReadFile(credential); err != nil || string(data) != "offline-test-credential\n" {
		t.Fatal("source credential changed", err)
	}
}

func TestCanaryReconciliationKeepsLargerCapturedLowerBound(t *testing.T) {
	output := t.TempDir()
	b := canaryBudget{Limit: 1000, StopReserve: 200, LaunchReserve: 200}
	selected := cell{ID: "case-r1-normal"}
	l, _ := canaryReadLedger(output, b)
	l.Captured = 100
	l.Counters[selected.ID+"/root"] = canaryCounter{Captured: 100}
	if err := writeJSON(filepath.Join(output, "canary-budget.json"), l, false); err != nil {
		t.Fatal(err)
	}
	usage := usageReport{Complete: true, Total: 150}
	if err := canaryReconcile(output, selected, b, &usage); err == nil || usage.Complete {
		t.Fatal("mismatched live totals certified", usage, err)
	}
	l, err := canaryReadLedger(output, b)
	if err != nil || l.Captured != 150 || l.StopReason != "live_offline_counter_mismatch" {
		t.Fatal("larger observed total disappeared", l, err)
	}
}
