package pilot

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Posthoc only. Compile this under a distinct path; never replace the frozen
// runner, production binary, input guides, grader or assignments during a run.
func TestFocusedExportOffline(t *testing.T) {
	if *focusedRoot == "" || *pilotAnalysisOutput == "" {
		t.Skip("explicit retained experiment and new export required; no model invocation")
	}
	trial := filepath.Join(*focusedRoot, "trial")
	var m manifest
	var p plan
	if err := readJSON(filepath.Join(trial, "frozen-manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	if err := readJSON(filepath.Join(trial, "plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(*pilotAnalysisOutput, 0o700); err != nil {
		t.Fatal("new export directory required", err)
	}
	runs := [][]string{{"run_id", "arm", "execution", "seconds", "accounting_complete", "all_sessions_terminal", "sessions", "acceptance", "regressions", "patch_integrity", "protocol", "input_uncached", "input_total", "cached_input", "output", "total", "reasoning_subset"}}
	sessions := [][]string{{"run_id", "arm", "session_id", "parent_id", "role", "models", "efforts", "known", "terminal", "input_uncached", "input_total", "cached_input", "output", "total", "reasoning_subset"}}
	results := []map[string]any{}
	var total int64
	for _, selected := range p.Runs {
		var r runResult
		if err := readJSON(filepath.Join(trial, "runs", selected.ID, "result.json"), &r); err != nil {
			t.Fatal("every planned run must have a retained result, including stopped attempts", err)
		}
		var grade gradeSummary
		if err := json.Unmarshal(r.Grade, &grade); err != nil {
			t.Fatal(err)
		}
		closed := len(r.Usage.Threads) > 0
		for _, thread := range r.Usage.Threads {
			terminal, err := focusedSessionClosed(thread.File.Path)
			if err != nil {
				t.Fatal(err)
			}
			closed = closed && terminal
			role := thread.Stage
			if thread.ParentID == "" {
				role = "coordinator"
			}
			sessions = append(sessions, []string{selected.ID, selected.Arm, thread.ID, thread.ParentID, role, fmt.Sprint(thread.Models), fmt.Sprint(thread.Efforts), strconv.FormatBool(thread.Known), strconv.FormatBool(terminal), focusedNum(thread.InputUncached), focusedNum(thread.InputTotal), focusedNum(thread.CacheRead), focusedNum(thread.Output), focusedNum(thread.Counts.Sum()), focusedReasoning(thread.ReasoningOutput)})
		}
		seconds := ""
		if r.Execution != nil {
			seconds = fmt.Sprintf("%.3f", float64(r.Execution.ElapsedNS)/1e9)
		}
		runs = append(runs, []string{selected.ID, selected.Arm, r.Status, seconds, strconv.FormatBool(r.Usage.Complete), strconv.FormatBool(closed), strconv.Itoa(len(r.Usage.Threads)), strconv.FormatBool(grade.Acceptance.Passed), strconv.FormatBool(grade.Regressions.Passed), strconv.FormatBool(grade.Integrity.Passed), r.Method.Status, focusedNum(r.Usage.InputUncached), focusedNum(r.Usage.InputTotal), focusedNum(r.Usage.Counts.CacheRead), focusedNum(r.Usage.Counts.Output), focusedNum(r.Usage.Total), focusedReasoning(r.Usage.ReasoningOutput)})
		total += r.Usage.Total
		counts := analysisTokenCounts(r.Usage.Counts, r.Usage.ReasoningOutput)
		counts.Units, counts.ReasoningKnownUnit, counts.ReasoningObserved = len(r.Usage.Threads), 0, 0
		for _, thread := range r.Usage.Threads {
			if thread.ReasoningOutput != nil {
				counts.ReasoningKnownUnit++
				counts.ReasoningObserved += *thread.ReasoningOutput
			}
		}
		results = append(results, map[string]any{"run": selected, "execution": r.Status, "seconds": seconds, "accounting_complete": r.Usage.Complete && closed, "all_sessions_terminal": closed, "usage_unknown": r.Usage.Unknown, "grade": grade, "protocol_status": r.Method.Status, "protocol_findings": r.Method.Findings, "protocol_limit": "Exact encrypted native dispatch equality and reviewer-input neutrality remain unknown; final artifacts and actual returned command evidence require separate audit.", "counts": counts})
	}
	var experiment map[string]any
	data, err := os.ReadFile(filepath.Join(*focusedRoot, "experiment.json"))
	if err != nil || json.Unmarshal(data, &experiment) != nil {
		t.Fatal("experiment provenance unavailable", err)
	}
	// The preparation receipt records a pending budget at preparation time.
	// The separate actual user authorization is authoritative for paid execution.
	var approval map[string]any
	data, err = os.ReadFile(filepath.Join(*focusedRoot, "authorization.json"))
	if err != nil || json.Unmarshal(data, &approval) != nil || approval["approved"] != true {
		t.Fatal("approved follow-up budget receipt unavailable", err)
	}
	binaries := map[string]string{}
	for _, a := range m.Host.Assets {
		switch name := filepath.Base(a.Path); name {
		case "kitsu", "pilot.test", "codex", "codex-code-mode-host", "codex-exec-server":
			binaries[name] = a.SHA256
		}
	}
	if approval["experiment"] != m.ID || approval["production_binary_sha256"] != binaries["kitsu"] || experiment["production_binary_sha256"] != binaries["kitsu"] {
		t.Fatal("approval, frozen binary and source provenance differ")
	}
	budget, err := canaryReadLedger(trial, *m.Budget)
	if err != nil {
		t.Fatal(err)
	}
	manifestHash, err := hashFile(filepath.Join(trial, "frozen-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	summary := map[string]any{"version": 1, "experiment": m.ID, "profile": m.Profile, "production": experiment, "approved_budget": approval, "manifest_sha256": manifestHash, "model": m.Model, "effort": m.Effort, "runs": results, "measured_captured_total": total, "aggregate_captured_total": budget.Captured, "aggregate_reconciles": total == budget.Captured, "new_probe_tokens": 0, "tail_unknown": budget.TailUnknown, "stop_reason": budget.StopReason, "savings_ratio": nil, "provider_cost": nil, "scope": "Descriptive actual session usage for one historical task, one attempt per arm. Not proof of general quality equivalence or isolated feature savings."}
	summary["frozen_executables_sha256"] = binaries
	summary["task_snapshot_sha256"], summary["task_prompt_sha256"], summary["coordinator_prompt_sha256"] = m.Cases[0].SnapshotSHA256, m.Cases[0].Prompt.SHA256, m.CoordinatorPrompt.SHA256
	summary["arm_policy_sha256"] = map[string]string{"normal": m.ObservationPrompts["normal"].SHA256, "compact": m.ObservationPrompts["compact"].SHA256}
	delete(experiment, "budget_approval")
	experiment["preparation_paid_calls"] = experiment["paid_calls"]
	delete(experiment, "paid_calls")
	if err := writeJSON(filepath.Join(*pilotAnalysisOutput, "summary.json"), summary, true); err != nil {
		t.Fatal(err)
	}
	for name, rows := range map[string][][]string{"runs.csv": runs, "sessions.csv": sessions} {
		var body bytes.Buffer
		if err := csv.NewWriter(&body).WriteAll(rows); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*pilotAnalysisOutput, name), body.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hashes := map[string]string{}
	for _, name := range []string{"runs.csv", "sessions.csv", "summary.json"} {
		hashes[name], err = hashFile(filepath.Join(*pilotAnalysisOutput, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(filepath.Join(*pilotAnalysisOutput, "sha256.json"), hashes, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("sanitized actual usage: measured=%d ledger=%d; no new probe", total, budget.Captured)
}

func focusedNum(n int64) string { return strconv.FormatInt(n, 10) }

func TestFocusedCompletedRunOffline(t *testing.T) {
	if *focusedRoot == "" || *pilotRun == "next" || *pilotRun == "all" {
		t.Skip("explicit retained root and one completed run id required; no model invocation")
	}
	var r runResult
	if err := readJSON(filepath.Join(*focusedRoot, "trial/runs", *pilotRun, "result.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || !r.Usage.Complete || r.Execution == nil || r.Execution.ExitCode != 0 || r.Execution.Stopped || r.Execution.Cancelled || r.Execution.Cleanup != "" {
		t.Fatal("completed execution and complete accounting required before next launch")
	}
	root := ""
	for _, thread := range r.Usage.Threads {
		if thread.ParentID == "" {
			if root != "" {
				t.Fatal("multiple coordinator roots")
			}
			root = thread.ID
		}
	}
	roles := map[string]int{}
	var total int64
	for _, thread := range r.Usage.Threads {
		closed, err := focusedSessionClosed(thread.File.Path)
		if err != nil || !closed || !thread.Known || len(thread.Models) != 1 || thread.Models[0] != "gpt-6.1-sol" || len(thread.Efforts) != 1 || thread.Efforts[0] != "high" {
			t.Fatal("unknown tail or incorrect model/effort", thread.ID, err)
		}
		if thread.ParentID != "" && thread.ParentID != root {
			t.Fatal("worker was not started by coordinator")
		}
		roles[thread.Stage]++
		total += thread.Counts.Sum()
	}
	for _, role := range []string{"tests", "code", "review", "commit"} {
		if roles[role] < 1 {
			t.Fatal("required worker absent", role)
		}
	}
	if root == "" || total != r.Usage.Total {
		t.Fatal("session counts do not reconcile")
	}
	var grade gradeSummary
	if err := json.Unmarshal(r.Grade, &grade); err != nil {
		t.Fatal(err)
	}
	t.Logf("completed native sessions=%d tokens=%d acceptance=%t regressions=%t integrity=%t; exact encrypted dispatch remains unknown", len(r.Usage.Threads), total, grade.Acceptance.Passed, grade.Regressions.Passed, grade.Integrity.Passed)
}

func focusedReasoning(n *int64) string {
	if n == nil {
		return "unknown"
	}
	return focusedNum(*n)
}

func focusedSessionClosed(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	started, completed, last := 0, 0, ""
	for {
		var e struct {
			Type    string                `json:"type"`
			Payload struct{ Type string } `json:"payload"`
		}
		if err := d.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			return false, err
		}
		if e.Type == "event_msg" {
			switch e.Payload.Type {
			case "task_started":
				started++
				last = "started"
			case "task_complete":
				completed++
				last = "completed"
			case "turn_aborted":
				last = "aborted"
			}
		}
	}
	return completed > 0 && completed >= started && last == "completed", nil
}

func TestFocusedSessionFinalTailRemainsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []string
		want   bool
	}{
		{"empty", nil, false},
		{"complete", []string{"task_started", "task_complete"}, true},
		{"followup", []string{"task_started", "task_complete", "task_started"}, false},
		{"abort", []string{"task_started", "turn_aborted"}, false},
		{"two turns", []string{"task_started", "task_complete", "task_started", "task_complete"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data bytes.Buffer
			for _, event := range tc.events {
				fmt.Fprintf(&data, "{\"type\":\"event_msg\",\"payload\":{\"type\":%q}}\n", event)
			}
			path := filepath.Join(t.TempDir(), "trace.jsonl")
			if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			closed, err := focusedSessionClosed(path)
			if err != nil || closed != tc.want {
				t.Fatal("tail completeness changed", closed, err)
			}
		})
	}
}
