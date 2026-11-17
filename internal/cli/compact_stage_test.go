package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func stagePrepare(t *testing.T, r *kitsutest.Repo, role, stage string) map[string]any {
	t.Helper()
	r.Write("assignment.txt", "Inspect the regression and perform only your assigned role.\n")
	result := r.Kitsu("task", "prepare", "fix-login", "--role", role, "--stage", stage, "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "assignment.txt")
	return stageJSON(t, result)
}

func stageJSON(t *testing.T, result kitsutest.Result) map[string]any {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" {
		t.Fatalf("stage command failed: %s", result.Output())
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &value); err != nil {
		t.Fatalf("stage JSON: %v: %s", err, result.Output())
	}
	if len(result.Stdout) > 16384 {
		t.Fatal("unbounded stage output")
	}
	return value
}

func stageID(t *testing.T, dispatch map[string]any) string {
	t.Helper()
	id, ok := dispatch["dispatch_id"].(string)
	if !ok || len(id) != 64 {
		t.Fatalf("missing dispatch identity: %v", dispatch)
	}
	return id
}

func TestStagePrepareFinishOrdersReceiptsAndBlocksOverlap(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	dispatch := stagePrepare(t, r, "reviewer", "review")
	id := stageID(t, dispatch)
	before := r.Git("status", "--porcelain")
	blocked := r.Kitsu("task", "prepare", "other-task", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "assignment.txt")
	if blocked.Code == 0 || !strings.Contains(blocked.Stderr, "unfinished") || r.Git("status", "--porcelain") != before {
		t.Fatalf("overlap mutated selection receipts: %s", blocked.Output())
	}
	if result := r.Kitsu("role", "check", "reviewer", "--from", dispatch["baseline"].(string)); result.Code != 0 {
		t.Fatal("selection receipt was written after baseline", result.Output())
	}
	receipt := stageJSON(t, r.Kitsu("task", "finish", id))
	if receipt["write_bounds_validated"] != true || receipt["checks_verified"] != false || receipt["status"] != "finished" {
		t.Fatal("missing checks falsely green or local write bounds missing", receipt)
	}
	again := stageJSON(t, r.Kitsu("task", "finish", id))
	if receipt["receipt_id"] != again["receipt_id"] {
		t.Fatal("repeat finish created a new receipt")
	}
	stagePrepare(t, r, "reviewer", "review")
}

func TestStageActiveIntervalBlocksAllBriefReceiptMutations(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	stagePrepare(t, r, "reviewer", "review")
	before := r.Git("status", "--porcelain")
	for _, task := range []string{"fix-login", "other-task"} {
		for _, saved := range []bool{false, true} {
			args := []string{"task", "brief", task, "--role", "reviewer", "--stage", "review", "--round", "2", "--host", "codex", "--workflow", "small"}
			if saved {
				args = append(args, "--save", "--assignment", "assignment.txt")
			}
			result := r.Kitsu(args...)
			if result.Code == 0 || !strings.Contains(result.Stderr, "unfinished") || r.Git("status", "--porcelain") != before {
				t.Fatalf("brief changed receipts within active interval: %s", result.Output())
			}
		}
	}
}

func TestStageInsufficientBudgetDoesNotCreateReceiptOrActiveStage(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("assignment.txt", "Review independently.\n")
	result := r.Kitsu("task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "assignment.txt", "--max-bytes", "512")
	if result.Code == 0 || r.Exists("openspec/changes/.skills/fix-login") || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("failed budget mutated stage: %s", result.Output())
	}
	stagePrepare(t, r, "reviewer", "review")
}

func TestStageConcurrentPreparationRefusesHeldLedger(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("assignment.txt", "Review independently.\n")
	dir := r.Path(".git/kitsu-stages")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := fslock.Acquire(context.Background(), r.Path(".git/kitsu-stages.lock"), fslock.Options{Wait: fslock.NoWait})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	result := r.Kitsu("task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "assignment.txt")
	if result.Code == 0 || !strings.Contains(result.Stderr, "busy") || r.Exists("openspec/changes/.skills/fix-login") {
		t.Fatalf("contending preparation mutated receipts: %s", result.Output())
	}
}

func TestStageQECanRecordActualCurrentRegressionRed(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "fail")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	id := stageID(t, stagePrepare(t, r, "quality-engineer", "tests"))
	r.Write("tests/regression.txt", "Regression specification.\n")
	if result := r.Kitsu("check", "run"); result.Code != 5 {
		t.Fatal(result.Output())
	}
	r.EditRecord(r.RecordFiles("fail")[0], func(record map[string]any) {
		record["ended_at"] = time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	})
	if result := r.Kitsu("task", "finish", id, "--expect-checks", "fail"); result.Code == 0 {
		t.Fatal("expired failure met regression red expectation")
	}
	if result := r.Kitsu("check", "run", "--force"); result.Code != 5 {
		t.Fatal(result.Output())
	}
	receipt := stageJSON(t, r.Kitsu("task", "finish", id, "--expect-checks", "fail"))
	if receipt["checks_verified"] != false || receipt["write_bounds_validated"] != true {
		t.Fatal("QE red receipt certified checks or lost write bounds", receipt)
	}
}

func TestStageRejectsTamperedFinishedReceipt(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id))
	path := ".git/kitsu-stages/" + id + ".finish.json"
	data := strings.Replace(r.Read(path), `"checks_verified":false`, `"checks_verified":true`, 1)
	r.Write(path, data)
	if result := r.Kitsu("task", "finish", id); result.Code == 0 || !strings.Contains(result.Stderr, "tampered") {
		t.Fatalf("tampered finish receipt accepted: %s", result.Output())
	}
}

func TestStageRejectsHiddenRawWriteAlongsideAllowedTreeEdit(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	r.Write(".gitattributes", "tests/existing.txt text eol=lf\n")
	r.Write("tests/existing.txt", "existing regression\n")
	r.Git("config", "core.safecrlf", "false")
	id := stageID(t, stagePrepare(t, r, "developer", "code"))
	r.Write("tests/existing.txt", "existing regression\r\n")
	r.Write("source.txt", "allowed production edit\n")
	result := r.Kitsu("task", "finish", id)
	if result.Code == 0 || !strings.Contains(result.Stderr, "outside") {
		t.Fatalf("hidden test edit accepted alongside production edit: %s", result.Output())
	}
}

func TestStageRepeatRejectsRehashedUnauthorizedReceipt(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id))
	r.Write("source.txt", "unauthorized reviewer edit\n")
	page := compactResult(t, r.Run(`{"version":1,"operations":[{"id":"state","kind":"git"}]}`, filepath.Join(kitsutest.Binary(t), "kitsu"), "context", "collect", "--request", "-"), 0)
	var receipt stageReceipt
	if err := json.Unmarshal([]byte(r.Read(".git/kitsu-stages/"+id+".finish.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.State = page.State
	receipt.ID = ""
	receipt.ID = contextpack.Key(receipt)
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(".git/kitsu-stages/"+id+".finish.json", string(data))
	r.Write(".git/kitsu-stages/active.json", `"`+id+`"`)
	result := r.Kitsu("task", "finish", id)
	if result.Code == 0 || !r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("rehashed receipt approved writes or released active interval: %s", result.Output())
	}
}

func TestStageRepeatRejectsRehashedReceiptSemantics(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id))
	var receipt stageReceipt
	if err := json.Unmarshal([]byte(r.Read(".git/kitsu-stages/"+id+".finish.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Scope, receipt.Task, receipt.Policy, receipt.Writes = "Full approval", "other-task", "wrong-policy", false
	receipt.ID = ""
	receipt.ID = contextpack.Key(receipt)
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(".git/kitsu-stages/"+id+".finish.json", string(data))
	if result := r.Kitsu("task", "finish", id); result.Code == 0 {
		t.Fatalf("rehashed receipt semantics accepted: %s", result.Output())
	}
}

func TestStageForgedBaselineReceiptCannotReleaseUnauthorizedCurrentWrites(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id))
	path := ".git/kitsu-stages/" + id + ".finish.json"
	var receipt stageReceipt
	if err := json.Unmarshal([]byte(r.Read(path)), &receipt); err != nil {
		t.Fatal(err)
	}
	// Keep the historically allowed baseline tree, but forge a fresh completion.
	receipt.Created = time.Now().UTC()
	receipt.ID = ""
	receipt.ID = contextpack.Key(receipt)
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(path, string(data))
	r.Write(".git/kitsu-stages/active.json", `"`+id+`"`)
	r.Write("source.txt", "unauthorized current reviewer write\n")
	result := r.Kitsu("task", "finish", id)
	if result.Code == 0 || !r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("forged historical baseline released active interval: %s", result.Output())
	}
	blocked := r.Kitsu("task", "prepare", "other-task", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "assignment.txt")
	if blocked.Code == 0 || !strings.Contains(blocked.Stderr, "unfinished") {
		t.Fatalf("forged historical receipt allowed next preparation: %s", blocked.Output())
	}
}

func TestStageExplicitlyAbandonsStaleCompletedActivePointer(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id))
	// Model a crash after the durable finish receipt, before active pointer removal.
	r.Write(".git/kitsu-stages/active.json", `"`+id+`"`)
	r.Write("source.txt", "source changed after historical completion\n")
	result := r.Kitsu("task", "finish", id)
	if result.Code == 0 || !r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("historical finish claimed current success: %s", result.Output())
	}
	original := r.Read(".git/kitsu-stages/" + id + ".finish.json")
	stageJSON(t, r.Kitsu("task", "finish", id, "--abandon", "--reason", "Source changed after an interrupted finalization."))
	if r.Read(".git/kitsu-stages/"+id+".finish.json") != original {
		t.Fatal("abandonment overwrote the historical finish receipt")
	}
	stagePrepare(t, r, "reviewer", "review")
}

func TestStageExplicitAbandonmentRecoversInterruptedCleanupAfterLaterEdits(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	stageJSON(t, r.Kitsu("task", "finish", id, "--abandon", "--reason", "Interrupted worker."))
	r.Write(".git/kitsu-stages/active.json", `"`+id+`"`)
	r.Write("source.txt", "source changed after prior abandonment\n")
	receipt := stageJSON(t, r.Kitsu("task", "finish", id, "--abandon", "--reason", "Interrupted worker."))
	if receipt["write_bounds_validated"] != false || receipt["checks_verified"] != false || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("repeat abandonment gave approval or failed cleanup", receipt)
	}
	stagePrepare(t, r, "reviewer", "review")
}

func TestStageLedgerRejectsSymlinkAndReplacementWithoutExternalWrites(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial symlink", true: "replacement"}[replace], func(t *testing.T) {
			t.Parallel()
			gitDir, outside := t.TempDir(), t.TempDir()
			s := &session{repo: &gitx.Repo{GitDir: gitDir}}
			var ledger *stageLedger
			if replace {
				var err error
				ledger, err = openStageLedger(context.Background(), s)
				if err != nil {
					t.Fatal(err)
				}
				defer ledger.close()
				if err := os.Rename(filepath.Join(gitDir, "kitsu-stages"), filepath.Join(gitDir, "original")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, filepath.Join(gitDir, "kitsu-stages")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if replace {
				if err := ledger.write("escaped.json", "bad"); err == nil {
					t.Fatal("replaced ledger accepted a write")
				}
			} else if opened, err := openStageLedger(context.Background(), s); err == nil {
				opened.close()
				t.Fatal("symlink ledger accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("ledger changed external directory: %v %v", entries, err)
			}
		})
	}
}

func TestStageWriteViolationLeavesStageOpenForRepair(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	dispatch := stagePrepare(t, r, "reviewer", "review")
	id := stageID(t, dispatch)
	r.Write("source.txt", "unauthorized edit\n")
	result := r.Kitsu("task", "finish", id)
	if result.Code == 0 || !strings.Contains(result.Stderr, "outside") {
		t.Fatalf("write violation accepted: %s", result.Output())
	}
	r.Write("source.txt", "original\n")
	stageJSON(t, r.Kitsu("task", "finish", id))
}

func TestStageChecksExpectationsUseExistingCurrentEvidence(t *testing.T) {
	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, mode)
			dispatch := stagePrepare(t, r, "reviewer", "review")
			id := stageID(t, dispatch)
			if result := r.Kitsu("task", "finish", id, "--expect-checks", mode); result.Code == 0 {
				t.Fatal("missing check evidence met expectation")
			}
			run := r.Kitsu("check", "run")
			if mode == "pass" && run.Code != 0 || mode == "fail" && run.Code != 5 {
				t.Fatal(run.Output())
			}
			count := len(r.RecordFiles(mode))
			receipt := stageJSON(t, r.Kitsu("task", "finish", id, "--expect-checks", mode))
			if receipt["checks_verified"] != (mode == "pass") || len(r.RecordFiles(mode)) != count {
				t.Fatal("finish changed evidence or misreported verdict", receipt)
			}
			if result := r.Kitsu("task", "finish", id, "--expect-checks", "any"); result.Code == 0 {
				t.Fatal("repeat finish changed its expectation")
			}
		})
	}
}

func TestStageStalePolicyCanOnlyBeAbandoned(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	id := stageID(t, stagePrepare(t, r, "quality-engineer", "tests"))
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['**']\n")
	if result := r.Kitsu("task", "finish", id); result.Code == 0 || !strings.Contains(result.Stderr, "policy") {
		t.Fatalf("changed write policy accepted: %s", result.Output())
	}
	abandoned := stageJSON(t, r.Kitsu("task", "finish", id, "--abandon", "--reason", "Policy changed; prepare a fresh worker."))
	if abandoned["status"] != "abandoned" || abandoned["write_bounds_validated"] != false || abandoned["checks_verified"] != false {
		t.Fatal("abandon granted approval", abandoned)
	}
	stagePrepare(t, r, "quality-engineer", "tests")
}

func TestStageRejectsChangedCheckDeclarationEvenWithinTestPaths(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['**']\n")
	id := stageID(t, stagePrepare(t, r, "quality-engineer", "tests"))
	r.Write("kitsu.toml", "version=1\n")
	if result := r.Kitsu("task", "finish", id); result.Code == 0 || !strings.Contains(result.Stderr, "check policy") {
		t.Fatalf("changed check declaration accepted: %s", result.Output())
	}
	stageJSON(t, r.Kitsu("task", "finish", id, "--abandon", "--reason", "Check policy needs a fresh baseline."))
}

func TestStageRejectsTamperedDispatchAndSavedAssignment(t *testing.T) {
	for _, target := range []string{"dispatch", "assignment"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			dispatch := stagePrepare(t, r, "reviewer", "review")
			id := stageID(t, dispatch)
			path := dispatch["path"].(string)
			if target == "dispatch" {
				path = r.Path(".git/kitsu-stages/" + id + ".json")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n', 'x')
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if result := r.Kitsu("task", "finish", id); result.Code == 0 {
				t.Fatal("tampered stage accepted")
			}
		})
	}
}

func TestStageRepeatRejectsStaleEvidenceAndSource(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	id := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	if result := r.Kitsu("check", "run"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	stageJSON(t, r.Kitsu("task", "finish", id, "--expect-checks", "pass"))
	r.EditRecord(r.RecordFiles("pass")[0], func(record map[string]any) {
		record["ended_at"] = time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	})
	if result := r.Kitsu("task", "finish", id, "--expect-checks", "pass"); result.Code == 0 {
		t.Fatal("stale successful receipt accepted")
	}
	r.Write("source.txt", "new content\n")
	if result := r.Kitsu("task", "finish", id); result.Code == 0 {
		t.Fatal("receipt reused for changed content")
	}
}

func TestStageWorktreesAreIsolatedAndRestartRetainsActiveInterval(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("assignment.txt", "Review independently.\n")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "fixture")
	otherPath := filepath.Join(t.TempDir(), "linked")
	r.Git("worktree", "add", "--detach", otherPath, "HEAD")
	other := r.In(otherPath)
	one := stageID(t, stagePrepare(t, r, "reviewer", "review"))
	two := stageID(t, stagePrepare(t, other, "reviewer", "review"))
	if result := other.Kitsu("task", "finish", one); result.Code == 0 {
		t.Fatal("another worktree's dispatch accepted")
	}
	stageJSON(t, r.Kitsu("task", "finish", one))
	stageJSON(t, other.Kitsu("task", "finish", two))
}
