package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func coordinatorJSON(t *testing.T, r *kitsutest.Repo, task string, args ...string) map[string]any {
	t.Helper()
	result := r.Kitsu(append([]string{"task", "show", task}, args...)...)
	return stageJSON(t, result)
}

func coordinatorSection(t *testing.T, view map[string]any, name string) map[string]any {
	t.Helper()
	sections, ok := view["sections"].(map[string]any)
	if !ok {
		t.Fatalf("missing sections: %v", view)
	}
	section, ok := sections[name].(map[string]any)
	if !ok {
		t.Fatalf("missing section %s: %v", name, sections)
	}
	return section
}

func TestCoordinatorSnapshotAndDelta(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	before := r.Git("status", "--porcelain")
	first := coordinatorJSON(t, r, "fix-login", "--compact")
	if len(first["sections"].(map[string]any)) != 8 || coordinatorSection(t, first, "prerequisites")["completion"] != "not_certified" {
		t.Fatal("incomplete snapshot or invented completion", first)
	}
	if first["kind"] != "snapshot" || first["task"] != "fix-login" || first["version"] != float64(1) {
		t.Fatal(first)
	}
	if interval := coordinatorSection(t, first, "interval"); interval["dispatch_id"] != id || interval["stage"] != "review" || interval["status"] != "active" {
		t.Fatal(interval)
	}
	if task := coordinatorSection(t, first, "task"); task["stage"] != "unknown" || task["workflow"] != "small" {
		t.Fatal("small history invented a resumable stage", task)
	}
	if checks := coordinatorSection(t, first, "checks"); checks["verified"] != false {
		t.Fatal("missing check evidence hidden", checks)
	}
	unchanged := coordinatorJSON(t, r, "fix-login", "--since", first["snapshot_id"].(string))
	if unchanged["kind"] != "delta" || len(unchanged["sections"].(map[string]any)) != 0 {
		t.Fatal("unchanged facts repeated", unchanged)
	}
	if len(stringMustJSON(unchanged)) >= len(stringMustJSON(first))/2 {
		t.Fatal("unchanged response did not reduce serialized observation bytes")
	}
	if result := r.Kitsu("check", "run"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	next := coordinatorJSON(t, r, "fix-login", "--since", unchanged["snapshot_id"].(string))
	if checks := coordinatorSection(t, next, "checks"); checks["verified"] != true {
		t.Fatal(checks)
	}
	if _, present := next["sections"].(map[string]any)["task"]; present {
		t.Fatal("unchanged task record repeated", next)
	}
	stageJSON(t, r.Kitsu("task", "finish", id, "--expect-checks", "pass"))
	finished := coordinatorJSON(t, r, "fix-login", "--since", next["snapshot_id"].(string))
	if interval := coordinatorSection(t, finished, "interval"); interval["status"] != "none" {
		t.Fatal("finished interval not explicitly cleared", interval)
	}
	reconstructed := first["sections"].(map[string]any)
	for _, delta := range []map[string]any{unchanged, next, finished} {
		for key, value := range delta["sections"].(map[string]any) {
			reconstructed[key] = value
		}
	}
	full := coordinatorJSON(t, r, "fix-login", "--compact")
	if !reflect.DeepEqual(reconstructed, full["sections"]) {
		t.Fatal("applying deltas did not reconstruct a fresh full snapshot", reconstructed, full)
	}
	if after := r.Git("status", "--porcelain"); after != before {
		t.Fatal("state observation changed tracked files", before, after)
	}
	plain := r.Kitsu("task", "show", "fix-login", "--json")
	var legacy uxTaskView
	if plain.Code != 0 || json.Unmarshal([]byte(plain.Stdout), &legacy) != nil || legacy.Workflow != "small" || strings.Contains(plain.Stdout, "snapshot_id") {
		t.Fatal("ordinary show changed", plain.Output())
	}
}

func TestCoordinatorSnapshotRefusesUnknownBaseAndSmallBudget(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	stagePrepare(t, r, "reviewer", "review")
	for _, args := range [][]string{
		{"--since", strings.Repeat("0", 64)},
		{"--since", "../../outside"},
		{"--compact", "--max-bytes", "512"},
		{"--max-bytes", "16384"},
	} {
		result := r.Kitsu(append([]string{"task", "show", "fix-login"}, args...)...)
		if result.Code == 0 || result.Stdout != "" {
			t.Fatalf("invalid state request succeeded: %v: %s", args, result.Output())
		}
	}
	files, err := filepath.Glob(r.Path(".git/kitsu-stages/snapshot-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatal("failed requests published a baseline", files, err)
	}
}

func TestCoordinatorSnapshotRejectsInvalidBaselines(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*coordinatorSnapshot)
	}{
		{"task", func(s *coordinatorSnapshot) { s.Task = "other-task" }},
		{"worktree", func(s *coordinatorSnapshot) { s.Worktree += "-elsewhere" }},
		{"version", func(s *coordinatorSnapshot) { s.Version++ }},
		{"expired", func(s *coordinatorSnapshot) { s.Created = time.Now().Add(-25 * time.Hour) }},
		{"future", func(s *coordinatorSnapshot) { s.Created = time.Now().Add(5 * time.Minute) }},
		{"incomplete", func(s *coordinatorSnapshot) { delete(s.Sections, "findings") }},
		{"null section", func(s *coordinatorSnapshot) { s.Sections["findings"] = json.RawMessage("null") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			stagePrepare(t, r, "reviewer", "review")
			first := coordinatorJSON(t, r, "fix-login", "--compact")
			var saved coordinatorSnapshot
			name := ".git/kitsu-stages/snapshot-" + first["snapshot_id"].(string) + ".json"
			if err := json.Unmarshal([]byte(r.Read(name)), &saved); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&saved)
			saved.ID = ""
			saved.ID = contextpack.Key(saved)
			r.Write(".git/kitsu-stages/snapshot-"+saved.ID+".json", stringMustJSON(saved))
			result := r.Kitsu("task", "show", "fix-login", "--since", saved.ID)
			if result.Code == 0 || result.Stdout != "" || !strings.Contains(result.Stderr, "fresh snapshot") {
				t.Fatalf("invalid baseline accepted: %s", result.Output())
			}
		})
	}
}

func TestCoordinatorSnapshotPrivateCacheSafetyAndRetention(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	stagePrepare(t, r, "reviewer", "review")
	first := coordinatorJSON(t, r, "fix-login", "--compact")
	file := ".git/kitsu-stages/snapshot-" + first["snapshot_id"].(string) + ".json"
	original := r.Read(file)
	r.Write(file, strings.Replace(original, `"version":1`, `"version":2`, 1))
	tampered := r.Kitsu("task", "show", "fix-login", "--since", first["snapshot_id"].(string))
	if tampered.Code == 0 || tampered.Stdout != "" {
		t.Fatal("tampered baseline accepted", tampered.Output())
	}
	if err := os.WriteFile(r.Path(file), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(r.Path(file))
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("snapshot not private", info, err)
	}
	for i := 0; i < coordinatorSnapshotLimit+2; i++ {
		// Populate only evictable snapshots; active intervals must survive pruning.
		r.Write(".git/kitsu-stages/snapshot-"+contextpack.Key(i)+".json", "{}")
	}
	coordinatorJSON(t, r, "fix-login", "--compact")
	files, err := filepath.Glob(r.Path(".git/kitsu-stages/snapshot-*.json"))
	if err != nil || len(files) > coordinatorSnapshotLimit || !r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("cache retention violated interval ownership", files, err)
	}
	last := coordinatorJSON(t, r, "fix-login", "--compact")
	file = ".git/kitsu-stages/snapshot-" + last["snapshot_id"].(string) + ".json"
	if err := os.Remove(r.Path(file)); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"private":"do not read"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, r.Path(file)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result := r.Kitsu("task", "show", "fix-login", "--since", last["snapshot_id"].(string))
	if result.Code == 0 || result.Stdout != "" || strings.Contains(result.Output(), "do not read") {
		t.Fatal("linked baseline was followed", result.Output())
	}
}

func TestCoordinatorSnapshotPolicyAndUntrustedReturn(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			d := protocolPrepare(t, r, host)
			first := coordinatorJSON(t, r, "fix-login", "--compact")
			request := protocolRequest(d)
			request["findings"] = []workerFinding{{Severity: "major", Path: "source.txt", Line: 1, Message: "PRIVATE-LONG-FINDING: retain only by reference"}}
			request["blockers"] = []string{"PRIVATE-BLOCKER: human follow-up needed"}
			stageJSON(t, protocolReturn(t, r, d, request))
			next := coordinatorJSON(t, r, "fix-login", "--since", first["snapshot_id"].(string))
			claims := coordinatorSection(t, next, "interval")["return"].(map[string]any)
			if claims["authority"] != "untrusted-worker-claims" || claims["findings"] != float64(1) || claims["blockers"] != float64(1) || claims["observations_current"] != true || strings.Contains(stringMustJSON(next), "PRIVATE-") {
				t.Fatal("claims were lost, printed in full or given authority", next)
			}
			r.Write("kitsu-roles.toml", "version=1\n["+host+".reviewer]\nmodel='different-model'\n")
			changed := coordinatorJSON(t, r, "fix-login", "--since", next["snapshot_id"].(string))
			interval := coordinatorSection(t, changed, "interval")
			if len(interval["policy_changes"].([]any)) == 0 || interval["return"].(map[string]any)["observations_current"] != false || !strings.Contains(stringMustJSON(coordinatorSection(t, changed, "prerequisites")), "abandon_and_reprepare") {
				t.Fatal("policy change not exposed", changed)
			}
			coordinatorSection(t, changed, "policy")
		})
	}
}

func TestCoordinatorSnapshotRecordedFindingsAndRawChanges(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	const task = "full-task"
	const reviews = "openspec/changes/full-task/reviews.md"
	text := "# Reviews\n\n## Findings\n\n| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n| F1 | code | 1 | major | source.txt:1 | PRIVATE-FINDING | open | - |\n"
	r.Write("openspec/changes/full-task/proposal.md", "# Task\nA task with an unresolved finding.\n")
	r.Write(reviews, text)
	first := coordinatorJSON(t, r, task, "--compact")
	findings := coordinatorSection(t, first, "findings")
	open := findings["open"].([]any)
	if len(open) != 1 || open[0].(map[string]any)["severity"] != "major" || open[0].(map[string]any)["path"] != reviews || strings.Contains(stringMustJSON(first), "PRIVATE-FINDING") {
		t.Fatal("finding not kept by original reference", first)
	}
	r.Write(reviews, strings.Replace(text, "| open | - |", "| resolved | Fixed in source |", 1))
	next := coordinatorJSON(t, r, task, "--since", first["snapshot_id"].(string))
	if len(coordinatorSection(t, next, "findings")["open"].([]any)) != 0 {
		t.Fatal("resolved finding not cleared", next)
	}
	// Ignore a record after discovery: raw task identity still tracks it.
	r.Write(".gitignore", reviews+"\n")
	ignored := coordinatorJSON(t, r, task, "--compact")
	r.Write(reviews, strings.Replace(r.Read(reviews), "PRIVATE-FINDING", "CHANGED-FINDING", 1))
	changed := coordinatorJSON(t, r, task, "--since", ignored["snapshot_id"].(string))
	if _, ok := changed["sections"].(map[string]any)["source"]; !ok {
		t.Fatal("ignored raw task edit vanished from delta", changed)
	}
}

func TestCoordinatorSnapshotTracksHeadAndOtherTaskIntervals(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("openspec/changes/full-task/proposal.md", "# A separate full task\n")
	dispatch := stagePrepare(t, r, "reviewer", "review")
	first := coordinatorJSON(t, r, "full-task", "--compact")
	if interval := coordinatorSection(t, first, "interval"); interval["status"] != "other_task" || interval["dispatch_id"] != dispatch["dispatch_id"] {
		t.Fatal("other task's worktree interval was hidden", interval)
	}
	r.Git("add", "-A")
	r.Git("commit", "-qm", "fixture commit for HEAD observation")
	next := coordinatorJSON(t, r, "full-task", "--since", first["snapshot_id"].(string))
	a, b := coordinatorSection(t, first, "source"), coordinatorSection(t, next, "source")
	if a["state"].(map[string]any)["tree"] != b["state"].(map[string]any)["tree"] || a["state"].(map[string]any)["head"] == b["state"].(map[string]any)["head"] {
		t.Fatal("HEAD-only event was lost", a, b)
	}
	if coordinatorSection(t, next, "prerequisites")["completion"] != "not_certified" {
		t.Fatal("local fixture commit became workflow approval", next)
	}
}
