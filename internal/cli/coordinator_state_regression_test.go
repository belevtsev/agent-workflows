package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func TestCoordinatorSnapshotExternalSkillsForBothHosts(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			skill := filepath.Join(t.TempDir(), "SKILL.md")
			writeSkillFixture(t, skill, "external-review")
			r.Write("kitsu-roles.toml", fmt.Sprintf("version=1\n[%s.reviewer]\nskills=[%q]\n", host, filepath.ToSlash(skill)))
			d := protocolPrepare(t, r, host)
			first := coordinatorJSON(t, r, "fix-login", "--compact")
			if err := os.WriteFile(skill, []byte("---\nname: external-review\ndescription: updated policy\n---\nInspect changed contracts.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			changed := coordinatorJSON(t, r, "fix-login", "--since", first["snapshot_id"].(string))
			if _, present := changed["sections"].(map[string]any)["source"]; present {
				t.Fatal("external skill edit changed repository source identity")
			}
			if coordinatorSection(t, first, "policy")["skills"] == coordinatorSection(t, changed, "policy")["skills"] || len(coordinatorSection(t, changed, "interval")["policy_changes"].([]any)) == 0 {
				t.Fatal("external skill change invisible", changed)
			}
			if err := os.Remove(skill); err != nil {
				t.Fatal(err)
			}
			unavailable := coordinatorJSON(t, r, "fix-login", "--compact")
			if len(coordinatorSection(t, unavailable, "policy")["problems"].([]any)) == 0 || !strings.Contains(stringMustJSON(coordinatorSection(t, unavailable, "prerequisites")), "skill_policy_unavailable") {
				t.Fatal("unavailable selection silently discarded", unavailable)
			}
			if !r.Exists(".git/kitsu-stages/" + stageID(t, d) + ".json") {
				t.Fatal("view discarded the original interval")
			}
		})
	}
}

func TestCoordinatorSnapshotFailedExpiredAndDamagedChecks(t *testing.T) {
	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, mode)
			stagePrepare(t, r, "reviewer", "review")
			result := r.Kitsu("check", "run")
			if result.Code != 0 && result.Code != 5 {
				t.Fatal(result.Output())
			}
			first := coordinatorJSON(t, r, "fix-login", "--compact")
			checks := coordinatorSection(t, first, "checks")
			if mode == "fail" {
				if checks["verified"] != false || !strings.Contains(stringMustJSON(checks), `"state":"failed"`) {
					t.Fatal("failed check hidden", checks)
				}
				return
			}
			if checks["verified"] != true {
				t.Fatal(checks)
			}
			files, err := filepath.Glob(r.Path(".git/kitsu/runs/pass/*/*.json"))
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			var record store.Record
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			ended := time.Now().Add(-evidence.MaxAge - time.Hour)
			record.StartedAt, record.EndedAt = ended.Add(-time.Second), &ended
			if err := os.WriteFile(files[0], []byte(stringMustJSON(record)), 0o600); err != nil {
				t.Fatal(err)
			}
			expired := coordinatorJSON(t, r, "fix-login", "--since", first["snapshot_id"].(string))
			if coordinatorSection(t, expired, "checks")["verified"] != false || !strings.Contains(stringMustJSON(coordinatorSection(t, expired, "prerequisites")), "current_passing_checks") {
				t.Fatal("expired evidence reused as passing", expired)
			}
			if err := os.WriteFile(files[0], []byte("{ damaged"), 0o600); err != nil {
				t.Fatal(err)
			}
			damaged := coordinatorJSON(t, r, "fix-login", "--since", expired["snapshot_id"].(string))
			if checks := coordinatorSection(t, damaged, "checks"); checks["verified"] != false || len(checks["warnings"].([]any)) == 0 {
				t.Fatal("damaged evidence warning omitted", damaged)
			}
		})
	}
}

func TestCoordinatorSnapshotSmallAdviserCheckpointsStayMissingOffline(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	// A deliberately unavailable command must never be launched by a view.
	config := &advisers.Config{
		Advisers:    map[string]advisers.Definition{"local": {Model: "offline-only", Command: []string{"nonexistent-private-provider", "{model}"}, Timeout: time.Minute}},
		Access:      map[string][]string{"coordinator": {"local"}},
		Checkpoints: []advisers.Checkpoint{{ID: "review-advice", Stage: "review", Role: "coordinator", Advisers: []string{"local"}}},
	}
	if _, err := advisers.Bind(r.Dir, "fix-login", "small", config, false); err != nil {
		t.Fatal(err)
	}
	view := coordinatorJSON(t, r, "fix-login", "--compact")
	if !strings.Contains(stringMustJSON(coordinatorSection(t, view, "prerequisites")), "adviser_checkpoints") || !strings.Contains(stringMustJSON(view), "review-advice") || strings.Contains(stringMustJSON(view), "nonexistent-private-provider") {
		t.Fatal("checkpoint missing or private command leaked", view)
	}
	answers, err := advisers.ReadConsultations(r.Dir, "fix-login")
	if err != nil || len(answers) != 0 {
		t.Fatal("view created a consultation", answers, err)
	}
}

func TestCoordinatorSnapshotRefusesBusyLedgerAndLinkedTaskRecords(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	stagePrepare(t, r, "reviewer", "review")
	before := r.Read(".git/kitsu-stages/active.json")
	lock, err := fslock.Acquire(t.Context(), r.Path(".git/kitsu-stages.lock"), fslock.Options{Wait: fslock.NoWait})
	if err != nil {
		t.Fatal(err)
	}
	blocked := r.Kitsu("task", "show", "fix-login", "--compact")
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if blocked.Code == 0 || blocked.Stdout != "" || !strings.Contains(blocked.Stderr, "busy") || r.Read(".git/kitsu-stages/active.json") != before {
		t.Fatal("busy view mutated interval", blocked.Output())
	}
	r.Write("openspec/changes/full-task/proposal.md", "# Proposal\n")
	outside := filepath.Join(t.TempDir(), "reviews.md")
	if err := os.WriteFile(outside, []byte("PRIVATE-OUTSIDE-RECORD"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, r.Path("openspec/changes/full-task/reviews.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	linked := r.Kitsu("task", "show", "full-task", "--compact")
	if linked.Code == 0 || linked.Stdout != "" || strings.Contains(linked.Output(), "PRIVATE-OUTSIDE-RECORD") {
		t.Fatal("linked task history was read", linked.Output())
	}
}

func TestCoordinatorSnapshotNormalizeFilterChild(t *testing.T) {
	if os.Getenv("KITSU_STATE_NORMALIZE_FILTER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	fmt.Print("normalized\n")
	os.Exit(0)
}

func TestCoordinatorSnapshotRawEditsHiddenByGitFilter(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	stagePrepare(t, r, "reviewer", "review")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r.Setenv("KITSU_STATE_NORMALIZE_FILTER", "1")
	filter := "'" + strings.ReplaceAll(filepath.ToSlash(program), "'", "'\\''") + "' -test.run=^TestCoordinatorSnapshotNormalizeFilterChild$"
	r.Git("config", "filter.stable.clean", filter)
	r.Write(".gitattributes", "source.txt filter=stable\n")
	first := coordinatorJSON(t, r, "fix-login", "--compact")
	r.Write("source.txt", "different raw content\n")
	changed := coordinatorJSON(t, r, "fix-login", "--since", first["snapshot_id"].(string))
	a, b := coordinatorSection(t, first, "source"), coordinatorSection(t, changed, "source")
	if a["state"].(map[string]any)["tree"] != b["state"].(map[string]any)["tree"] || a["write_content_key"] == b["write_content_key"] {
		t.Fatal("raw edit concealed by clean filter was lost", a, b)
	}
}

func TestCoordinatorSnapshotComparisonRejectsChangingObservations(t *testing.T) {
	// Exercise a deterministic observation boundary without racing timing or
	// adding test hooks to production collection.
	_, root := guardRepo(t, "")
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join(root, "openspec/changes/full-task"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, e, err := compactEngine(ctx, Config{Out: io.Discard, Err: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	l, err := openStageLedger(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()
	first, err := coordinatorSections(ctx, s, e, l, "full-task")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("changed between observations\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := coordinatorSections(ctx, s, e, l, "full-task")
	if err != nil {
		t.Fatal(err)
	}
	if sameCoordinatorSections(first, second) {
		t.Fatal("changed observations compare equal")
	}
}
