package platform_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// The migration starts with real legacy names and record fields rather than
// producing them through the renamed runtime. The scoped check's content and
// command survive the configuration rename, so its old passing receipt would
// satisfy the new declaration if the stores were accidentally shared.
func TestLegacyMigrationRequiresFreshEvidence(t *testing.T) {
	t.Parallel()
	for _, linked := range []bool{false, true} {
		name := "checkout"
		if linked {
			name = "linked worktree"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, legacyDir := legacyProject(t)
			history := migrationFiles(t, legacyDir)
			work := repo
			if linked {
				path := filepath.Join(t.TempDir(), "linked")
				repo.Git("worktree", "add", "--detach", path, "HEAD")
				work = repo.In(path)
				if work.StoreDir() != repo.StoreDir() {
					t.Fatal("linked worktree does not use the common record store")
				}
			}
			hooksDir := filepath.Join(filepath.Dir(legacyDir), "hooks")
			for _, hook := range migrationHooks {
				writeMigrationFile(t, filepath.Join(hooksDir, hook), legacyHook(hook), 0o755)
			}
			if result := work.Gatecub("run"); result.Code != 1 || !strings.Contains(result.Output(), "gatecub.toml") {
				t.Fatalf("legacy declaration was accepted: exit %d\n%s", result.Code, result.Output())
			}
			if result := work.Gatecub("roles", "check", "developer", "--from", "HEAD"); result.Code != 1 || !strings.Contains(result.Output(), "gatecub-roles.toml") {
				t.Fatalf("legacy role configuration was accepted: exit %d\n%s", result.Code, result.Output())
			}
			work.Git("mv", "cw.toml", "gatecub.toml")
			work.Git("mv", "cw-roles.toml", "gatecub-roles.toml")
			if result := work.Gatecub("init"); result.Code != 4 {
				t.Fatalf("init with legacy hooks exited %d, want 4\n%s", result.Code, result.Output())
			}
			backup := filepath.Join(filepath.Dir(legacyDir), "gatecub-migration-backup")
			if err := os.Mkdir(backup, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, hook := range migrationHooks {
				path := filepath.Join(hooksDir, hook)
				assertMigrationFile(t, path, legacyHook(hook))
				if err := os.Rename(path, filepath.Join(backup, hook)); err != nil {
					t.Fatal(err)
				}
			}
			if result := work.Gatecub("init"); result.Code != 0 {
				t.Fatalf("init after backing up legacy hooks: %s", result.Output())
			}
			if files := work.RecordFiles("git"); len(files) != 0 {
				t.Fatalf("init imported legacy receipts: %v", files)
			}
			if result := work.Gatecub("tokens", "--task", "add-greeting"); result.Code != 1 || !strings.Contains(result.Output(), "no start is recorded") {
				t.Fatalf("legacy token totals were imported: exit %d\n%s", result.Code, result.Output())
			}
			assertMigrationFile(t, filepath.Join(work.StoreDir(), "mode"), "enforce\n")
			before := head(work)
			trace := traced(t, work)
			commit := work.TryGit("commit", "-m", "migrate")
			assertDenied(t, work, before, commit, trace)
			if !lineWith(commit.Stderr, "git", "not run on this content") {
				t.Fatalf("legacy evidence did not produce the expected missing-check denial:\n%s", commit.Output())
			}
			if result := work.Gatecub("run"); result.Code != 0 {
				t.Fatalf("fresh checks: %s", result.Output())
			}
			fresh := work.RecordFiles("git")
			if len(fresh) != 1 {
				t.Fatalf("fresh run wrote %d receipts, want 1", len(fresh))
			}
			// Matching keys prove the legacy receipt was relevant, rather than
			// rejected merely because the configuration files were renamed.
			key := filepath.Base(filepath.Dir(fresh[0]))
			oldReceipt := filepath.Join("runs", "git", key, "20260101T000000Z-12345678.json")
			if _, found := history[oldReceipt]; !found {
				t.Fatalf("legacy receipt did not describe the checked content %s", key)
			}
			work.Git("commit", "-m", "migrate")
			if head(work) == before {
				t.Fatal("fresh evidence did not allow the migration commit")
			}
			work.Write("src/app.txt", "changed after the check\n")
			trace = traced(t, work)
			before = head(work)
			commit = work.TryGit("commit", "-a", "-m", "unchecked edit")
			assertDenied(t, work, before, commit, trace)
			if got := migrationFiles(t, legacyDir); !maps.Equal(got, history) {
				t.Error("migration or fresh checks changed the legacy history")
			}
			for _, hook := range migrationHooks {
				assertMigrationFile(t, filepath.Join(backup, hook), legacyHook(hook))
			}
		})
	}
}

func TestLegacyMigrationPreservesCustomHooks(t *testing.T) {
	t.Parallel()
	for _, configured := range []bool{false, true} {
		name := "default directory"
		if configured {
			name = "core.hooksPath"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, legacyDir := legacyProject(t)
			history := migrationFiles(t, legacyDir)
			hooksDir := filepath.Join(filepath.Dir(legacyDir), "hooks")
			if configured {
				hooksDir = t.TempDir()
				repo.Git("config", "core.hooksPath", hooksDir)
			}
			custom := func(program, hook string) string {
				return "#!/bin/sh\n# team-owned wrapper\nprintf 'team hook runs\\n'\n" + program + " hook " + hook + " \"$@\"\n"
			}
			for _, hook := range migrationHooks {
				writeMigrationFile(t, filepath.Join(hooksDir, hook), custom("cw", hook), 0o755)
			}
			repo.Git("mv", "cw.toml", "gatecub.toml")
			repo.Git("mv", "cw-roles.toml", "gatecub-roles.toml")
			if result := repo.Gatecub("init"); result.Code != 4 {
				t.Fatalf("init with custom legacy hooks: exit %d\n%s", result.Code, result.Output())
			}
			for _, hook := range migrationHooks {
				path := filepath.Join(hooksDir, hook)
				assertMigrationFile(t, path, custom("cw", hook))
				writeMigrationFile(t, path, custom("gatecub", hook), 0o755)
			}
			if result := repo.Gatecub("init"); result.Code != 0 {
				t.Fatalf("init after manually updating custom hooks: %s", result.Output())
			}
			for _, hook := range migrationHooks {
				assertMigrationFile(t, filepath.Join(hooksDir, hook), custom("gatecub", hook))
			}
			if configured && repo.Git("config", "--get", "core.hooksPath") != hooksDir {
				t.Error("init changed core.hooksPath")
			}
			if result := repo.Gatecub("run"); result.Code != 0 {
				t.Fatalf("fresh checks: %s", result.Output())
			}
			if result := repo.TryGit("commit", "-m", "migrate custom hooks"); result.Code != 0 || !strings.Contains(result.Output(), "team hook runs") {
				t.Errorf("commit lost the custom hook behavior: exit %d\n%s", result.Code, result.Output())
			}
			if got := migrationFiles(t, legacyDir); !maps.Equal(got, history) {
				t.Error("migration changed the legacy history")
			}
		})
	}
}

func TestLegacyMigrationPreservesFinalizationTree(t *testing.T) {
	t.Parallel()
	for _, collision := range []bool{false, true} {
		name := "transfer"
		if collision {
			name = "existing destination"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, _ := legacyProject(t)
			tree := repo.Git("rev-parse", "HEAD^{tree}")
			const oldRef = "refs/cw/finalize/add-greeting"
			const newRef = "refs/gatecub/finalize/add-greeting"
			repo.Git("update-ref", oldRef, tree)
			if collision {
				repo.Git("update-ref", newRef, "HEAD")
			}
			transaction := fmt.Sprintf("create %s %s\ndelete %s %s\n", newRef, tree, oldRef, tree)
			result := repo.Run(transaction, "git", "update-ref", "--stdin")
			if collision {
				if result.Code == 0 || repo.Git("rev-parse", oldRef) != tree || repo.Git("rev-parse", newRef) != head(repo) {
					t.Fatalf("failed transfer changed a saved baseline: %s", result.Output())
				}
				return
			}
			if result.Code != 0 || repo.Git("rev-parse", newRef) != tree || repo.TryGit("rev-parse", "--verify", oldRef).Code == 0 {
				t.Errorf("baseline transfer did not preserve the exact tree: %s", result.Output())
			}
		})
	}
}

func TestLegacyMigrationChangesTaskLabels(t *testing.T) {
	t.Parallel()
	repo, legacyDir := legacyProject(t)
	history := migrationFiles(t, legacyDir)
	repo.Git("mv", "cw.toml", "gatecub.toml")
	program := "gatecub"
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	program = filepath.Join(gatecubtest.Binary(t), program)
	for _, prefix := range []string{"cw", "gatecub"} {
		transcript := filepath.Join(t.TempDir(), "agent.jsonl")
		content := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"%s-task: add-greeting code 1\nImplement the greeting."}}
{"type":"assistant","message":{"id":"answer-1","model":"test-model","usage":{"input_tokens":10,"output_tokens":2}}}
`, prefix)
		writeMigrationFile(t, transcript, content, 0o600)
		notice, err := json.Marshal(map[string]string{
			"session_id": "migration", "agent_id": prefix, "agent_type": "gatecub:developer",
			"agent_transcript_path": transcript, "cwd": repo.Dir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result := repo.Run(string(notice), program, "tokens", "record", "--host", "claude"); result.Code != 0 {
			t.Fatalf("record %s label: %s", prefix, result.Output())
		}
	}
	result := repo.Gatecub("tokens", "--json")
	var report struct {
		Tasks []struct {
			Task   string `json:"task"`
			Total  int    `json:"total"`
			Starts int    `json:"starts"`
		} `json:"tasks"`
		Unlabelled []struct {
			Agent string `json:"agent"`
		} `json:"unlabelled"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil || result.Code != 0 {
		t.Fatalf("token report: %v\n%s", err, result.Output())
	}
	if len(report.Tasks) != 1 || report.Tasks[0].Task != "add-greeting" || report.Tasks[0].Total != 12 || report.Tasks[0].Starts != 1 {
		t.Errorf("only the Gatecub label should contribute to the task:\n%s", result.Output())
	}
	if len(report.Unlabelled) != 1 || report.Unlabelled[0].Agent != "agent-cw" {
		t.Errorf("the legacy label should leave its start unlabelled:\n%s", result.Output())
	}
	if got := migrationFiles(t, legacyDir); !maps.Equal(got, history) {
		t.Error("recording new task labels changed the legacy history")
	}
}

var migrationHooks = []string{"pre-commit", "pre-merge-commit", "pre-push"}

func legacyProject(t *testing.T) (*gatecubtest.Repo, string) {
	t.Helper()
	repo := gatecubtest.NewRepo(t)
	repo.Write("cw.toml", portableDeclaration+"paths = [\"src/app.txt\"]\n")
	repo.Write("cw-roles.toml", "version = 1\n[paths]\ntests = [\"tests/**\"]\n")
	repo.Write("src/app.txt", "checked content\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "legacy project")
	entries := repo.Git("ls-tree", "-r", "-z", "HEAD", "--", "src/app.txt")
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(entries)))
	const runID = "20260101T000000Z-12345678"
	ended := time.Now().UTC().Add(-time.Minute)
	record, err := json.Marshal(map[string]any{
		"run_id": runID, "id": "git", "run": []string{"git", "--version"},
		"paths": []string{"src/app.txt"}, "tree": repo.Git("rev-parse", "HEAD^{tree}"),
		"content_key": key, "seq": 1, "outcome": "passed", "exit_status": 0,
		"started_at": ended.Add(-time.Second), "ended_at": ended, "log": "logs/" + runID + ".log",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(filepath.Dir(repo.StoreDir()), "cw")
	for name, content := range map[string]string{
		filepath.Join("runs", "git", key, runID+".json"): string(record),
		filepath.Join("logs", runID+".log"):              "git version from the legacy run\n",
		"mode":                                           "observe\n",
		"decisions/2026-01.jsonl":                        "{\"time\":\"2026-01-01T00:00:00Z\",\"hook\":\"pre-commit\",\"mode\":\"observe\",\"outcome\":\"denied\",\"cause\":\"no_declaration\"}\n",
		"tokens/legacy-session/agent-legacy.json": `{"version":1,"host":"claude","session":"legacy-session","agent":"agent-legacy","record":"legacy.jsonl","task":"add-greeting","stage":"code","round":1,"known":true,"models":{"test-model":{"input":123,"cache_write":0,"cache_read":0,"output":7}}}
`,
	} {
		writeMigrationFile(t, filepath.Join(legacyDir, name), content, 0o600)
	}
	return repo, legacyDir
}

func legacyHook(name string) string {
	return fmt.Sprintf("#!/bin/sh\n# cw-hook: written by `cw init`. `cw uninstall` removes this file.\n"+`dirs=; IFS=:; set -f
for dir in $PATH; do case "$dir" in /*) dirs="${dirs:+$dirs:}$dir" ;; esac; done; unset IFS; set +f
cw=$([ -n "$dirs" ] && PATH=$dirs && command -v cw 2>/dev/null)
case "$cw" in /*) exec "$cw" hook %[1]s "$@" ;; esac
echo "cw: the cw program was not found in an entry of PATH that is an absolute path, so this %[1]s cannot be judged and is denied. An empty or relative entry is not searched." >&2
echo "cw: the install instruction is at https://github.com/belevtsev/agent-workflows#install" >&2
exit 3
`, name)
}

func writeMigrationFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func assertMigrationFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s: content changed or missing (error %v)", path, err)
	}
}

func migrationFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
