package pilot

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	pilotHelper       = flag.String("pilot-test-helper", "", "offline unit-test child only")
	pilotGradeRequest = flag.String("pilot-test-grade-request", "", "offline unit-test grader request")
)

func TestPilotScheduleIsPairedDeterministicAndComplete(t *testing.T) {
	m := manifest{Repeats: 3, Seed: 20261116, Controllers: 6}
	for i := range 12 {
		m.Cases = append(m.Cases, pilotCase{ID: fmt.Sprintf("case-%02d", i)})
	}
	first, second := schedule(m, "digest"), schedule(m, "digest")
	if !reflect.DeepEqual(first, second) || len(first.Runs) != 72 {
		t.Fatal("schedule is not reproducible or complete")
	}
	seen := map[string]bool{}
	for i, run := range first.Runs {
		if seen[run.ID] || run.Order != i+1 {
			t.Fatal("run identity/order repeated", run)
		}
		seen[run.ID] = true
		if i%2 == 0 {
			next := first.Runs[i+1]
			if next.CaseID != run.CaseID || next.Repeat != run.Repeat || next.Arm == run.Arm || next.Controller != run.Controller || run.Controller != (i/2)%6 {
				t.Fatal("paired block was separated", run, next)
			}
		}
	}
	m.Seed++
	if reflect.DeepEqual(first.Runs, schedule(m, "digest").Runs) {
		t.Fatal("randomization did not respond to its seed")
	}
}

func TestPilotFreezeDetectsDriftAndPreservesManifest(t *testing.T) {
	path, output := unitManifest(t)
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil || len(p.Runs) != 72 {
		t.Fatal(m, p, err)
	}
	before, err := os.ReadFile(filepath.Join(output, "frozen-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.CoordinatorPrompt.Path, []byte("changed instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFrozen(t.Context(), output); err == nil {
		t.Fatal("changed instructions accepted")
	}
	after, err := os.ReadFile(filepath.Join(output, "frozen-manifest.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("frozen input changed", err)
	}
}

func TestPilotEndToEndOfflineAccountingAndResume(t *testing.T) {
	path, output := unitManifest(t)
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
	if result.Status != "completed" || !result.Measured || !functionalPass(&result) || result.BaselineTree == result.CandidateTree {
		t.Fatal("fake measured attempt was not retained/graded", result)
	}
	if !result.Usage.Complete || result.Usage.Total != 618 || result.Usage.Input != 510 || result.Usage.CacheRead != 90 || result.Usage.Output != 18 || len(result.Usage.Threads) != 5 || result.Usage.ReasoningOutput == nil || *result.Usage.ReasoningOutput != 9 {
		t.Fatal("parent/worker usage was missing, duplicated, or reasoning added twice", result.Usage)
	}
	if result.Method.Status != "unknown" || len(result.Method.Findings) != 0 {
		t.Fatal("mechanical checks falsely certified full protocol compliance", result.Method)
	}
	patch, err := os.ReadFile(result.Patch.Path)
	if err != nil || !strings.Contains(string(patch), "newly-added.txt") {
		t.Fatal("untracked candidate content was omitted", string(patch), err)
	}
	if err := executeCell(t.Context(), m, p, selected, output); err == nil {
		t.Fatal("existing measured run silently retried")
	}
	if err := os.MkdirAll(filepath.Join(output, "runs", p.Runs[1].ID), 0o700); err != nil {
		t.Fatal(err)
	}
	summary, err := summarize(output, m, p)
	if err != nil || summary.Measured != 1 || summary.Results != 1 || len(summary.Interrupted) != 1 || len(summary.Unclaimed) != 70 {
		t.Fatal("resume lost a measured, interrupted, or pending identity", summary, err)
	}
}

func TestPilotRejectsUnapprovedCalibrationAndSnapshotHistory(t *testing.T) {
	path, output := unitManifest(t)
	var m manifest
	if err := readJSON(path, &m); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(m.Cases[0].Snapshot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := freeze(t.Context(), path, output); err == nil {
		t.Fatal("solver snapshot retained original history")
	}
	if err := os.Remove(filepath.Join(m.Cases[0].Snapshot, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(m.Calibration.Path, map[string]any{"version": 1, "approved": false}, false); err != nil {
		t.Fatal(err)
	}
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeCell(t.Context(), m, p, p.Runs[0], output); err == nil {
		t.Fatal("paid execution permitted before calibration")
	}
	if _, err := os.Stat(filepath.Join(output, "runs")); !os.IsNotExist(err) {
		t.Fatal("unapproved calibration claimed measured work", err)
	}
}

func TestPilotDuplicateTraceAliasesAreCountedOnce(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	traceDir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(traceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := fakeRollout("root-001", "", "lookup", "normal", 200, 50, 10, 5)
	for _, name := range []string{"first.jsonl", "alias.jsonl"} {
		if err := os.WriteFile(filepath.Join(traceDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	usage, _, err := collectUsage(home, ".codex/sessions", filepath.Join(base, "run"), "root-001")
	if err != nil || !usage.Complete || usage.Total != 210 || len(usage.Threads) != 1 {
		t.Fatal(usage, err)
	}
}

func unitManifest(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	m := manifest{Version: 1, ID: "offline-unit-fixture", Topology: "kitsu-small-path", Model: "gpt-6.1-sol", Effort: "max", Seed: 17, Repeats: 3, TimeoutSeconds: 10,
		CoordinatorPrompt:  asset{Path: write("common.txt", "Offline fake-host unit fixture {task}.")},
		ObservationPrompts: map[string]asset{"normal": {Path: write("normal.txt", "NORMAL_OBSERVATION")}, "compact": {Path: write("compact.txt", "COMPACT_OBSERVATION")}},
		Host:               pilotHost{Argv: []string{program, "-test.run=^TestPilotHelperProcess$", "-pilot-test-helper=host"}, PassEnv: []string{"PATH"}, Rollouts: ".codex/sessions"},
		Grader:             pilotGrader{Argv: []string{program, "-test.run=^TestPilotHelperProcess$", "-pilot-test-helper=grader", "-pilot-test-grade-request={grade_request}"}},
		Calibration:        asset{Path: filepath.Join(root, "calibration.json")}}
	if err := writeJSON(m.Calibration.Path, map[string]any{"version": 1, "approved": true, "model": m.Model, "effort": m.Effort, "subagents_verified": true, "usage_verified": true, "isolation_verified": true, "cli_version": "offline-fake", "evidence": map[string]any{"kind": "unit-test-only", "paid_calls": 0}}, false); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		id := fmt.Sprintf("case-%02d", i)
		snapshot := filepath.Join(root, id)
		if err := os.Mkdir(snapshot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snapshot, "README.md"), []byte("offline fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		m.Cases = append(m.Cases, pilotCase{ID: id, Snapshot: snapshot, Prompt: asset{Path: write(id+".txt", "Exercise the offline helper only.")}, Provenance: json.RawMessage(`{"kind":"offline-unit-fixture-not-a-real-task"}`)})
	}
	path := filepath.Join(root, "input.json")
	if err := writeJSON(path, m, false); err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(root, "output")
}

func TestPilotHelperProcess(t *testing.T) {
	if *pilotHelper == "" {
		t.Skip("offline child only")
	}
	switch *pilotHelper {
	case "host":
		prompt, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		arm := "normal"
		if strings.Contains(string(prompt), "COMPACT_OBSERVATION") {
			arm = "compact"
		}
		dir := filepath.Join(os.Getenv("HOME"), ".codex", "sessions")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"tests", "code", "review", "commit"} {
			if err := os.WriteFile(filepath.Join(dir, stage+".jsonl"), fakeRollout("worker-"+stage, "root-001", stage, arm, 100, 10, 2, 1), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "root.jsonl"), fakeRollout("root-001", "", "", arm, 200, 50, 10, 5), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("newly-added.txt", []byte("retained new candidate file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Println(`{"type":"thread.started","thread_id":"root-001"}`)
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":200,"cached_input_tokens":50,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":5}}`)
	case "grader":
		var request struct {
			Version   int    `json:"version"`
			CaseID    string `json:"case_id"`
			Candidate string `json:"candidate_worktree"`
			Output    string `json:"output_path"`
			Baseline  string `json:"baseline_tree"`
			Tree      string `json:"candidate_tree"`
			Prompt    string `json:"visible_prompt_sha256"`
		}
		if err := readJSON(*pilotGradeRequest, &request); err != nil {
			t.Fatal(err)
		}
		grade := map[string]any{"version": 1, "case_id": request.CaseID, "candidate_id": "offline-candidate", "infrastructure_status": "ok"}
		for _, field := range []string{"eligibility", "acceptance", "regressions", "patch_integrity"} {
			grade[field] = map[string]bool{"passed": true}
		}
		if err := writeJSON(request.Output, grade, false); err != nil {
			t.Fatal(err)
		}
	case "sleep":
		time.Sleep(10 * time.Second)
	default:
		t.Fatal("unknown fake helper")
	}
}

func fakeRollout(id, parent, stage, arm string, input, cached, output, reasoning int64) []byte {
	var rows []map[string]any
	rows = append(rows, map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "parent_thread_id": parent}})
	rows = append(rows, map[string]any{"type": "turn_context", "payload": map[string]any{"model": "gpt-6.1-sol", "effort": "max"}})
	if stage != "" {
		rows = append(rows, map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": "kitsu-task: offline-case " + stage + " 1\nOffline fixture."}})
	} else {
		for range 4 {
			args := `{"fork_turns":"none","model":"gpt-6.1-sol","reasoning_effort":"max"}`
			rows = append(rows, map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "spawn_agent", "arguments": args}})
		}
		if arm == "compact" {
			rows = append(rows, map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "exec_command", "arguments": `{"cmd":"kitsu context collect --request public.json"}`}})
		}
	}
	usage := map[string]int64{"input_tokens": input, "cached_input_tokens": cached, "cache_write_input_tokens": 0, "output_tokens": output, "reasoning_output_tokens": reasoning}
	rows = append(rows, map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": usage, "last_token_usage": usage}}})
	var out strings.Builder
	for _, row := range rows {
		data, _ := json.Marshal(row)
		out.Write(data)
		out.WriteByte('\n')
	}
	return []byte(out.String())
}
