package main_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestGateJudgesTheContentACommitRecords verifies a task on a working tree,
// then commits from it with content staged in several ways, through both
// gates: `cw hook pre-publish`, given the agent's Bash command, and the
// pre-commit hook `cw gate install` writes, which git runs with the index it
// commits in GIT_INDEX_FILE. The working tree always holds the verified
// bytes, so only what the commit records differs between the cases. Oracle:
// a commit that records, for a path it changes, a mode or bytes other than
// the working tree's is denied by both: the pre-publish decision is deny and
// names the path and the task, git exits 1 with that text and HEAD stays.
// Every other commit, partial, with -a, with a path or after git add -A
// over a stale index, is allowed by both: no pre-publish output, git exits 0
// and HEAD holds the working tree's bytes of what it committed.
func TestGateJudgesTheContentACommitRecords(t *testing.T) {
	binary := buildPlainCW(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	hooksGit(t, home, repo, "init", "-q")
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "tool.sh", "keep.txt"} {
		hooksWriteFile(t, repo, name, name+" first\n")
	}
	hooksGit(t, home, repo, "add", "-A")
	hooksGit(t, home, repo, "commit", "-q", "-m", "initial")

	env := []string{"HOME=" + home, "PATH=" + filepath.Dir(binary) + ":/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "CW_TASK_STORE=" + store,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	run := func(t *testing.T, stdin []byte, program string, args ...string) (int, string, string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), program, args...)
		command.Env, command.Dir, command.Stdin = env, repo, bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit.ExitCode(), stdout.String(), stderr.String()
		} else if err != nil {
			t.Fatalf("%s %v did not run: %v", program, args, err)
		}
		return 0, stdout.String(), stderr.String()
	}
	must := func(t *testing.T, program string, args ...string) string {
		t.Helper()
		code, stdout, stderr := run(t, nil, program, args...)
		if code != 0 {
			t.Fatalf("%s %v: exit %d\n%s%s", program, args, code, stdout, stderr)
		}
		return stdout
	}
	head := func(t *testing.T) string {
		t.Helper()
		return strings.TrimSpace(must(t, "git", "rev-parse", "HEAD"))
	}
	// prePublish returns what cw hook pre-publish prints for bash run in repo.
	prePublish := func(t *testing.T, bash string) string {
		t.Helper()
		stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": repo, "tool_input": map[string]any{"command": bash}})
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := run(t, stdin, binary, "hook", "pre-publish")
		if code != 0 {
			t.Fatalf("cw hook pre-publish for %q: exit %d\n%s", bash, code, stderr)
		}
		return stdout
	}
	// stale stages other bytes for name, then puts its verified bytes back
	// on disk.
	stale := func(t *testing.T, name string) {
		t.Helper()
		verified, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		hooksWriteFile(t, repo, name, "never verified\n")
		must(t, "git", "add", "--", name)
		hooksWriteFile(t, repo, name, string(verified))
	}

	must(t, binary, "gate", "install", "--repo", repo)
	// The verified working tree: four changed files and a new one. A verify
	// without check runs passes only on the source the task started from.
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		hooksWriteFile(t, repo, name, name+" verified\n")
	}
	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	hooksWriteFile(t, filepath.Dir(plan), "plan.json", `{"title":"Staged","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	must(t, binary, "task", "start", "--repo", repo, "--plan", plan, "--policy", filepath.Join(module, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("a.txt verified\n"))
	ref := taskflow.SourceRef{Path: "a.txt", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(hash[:])}
	reason := "Read-only source inspection; no executable behavior changed."
	assessment, err := json.Marshal(taskflow.AssessmentInput{
		Requirements: []taskflow.Assessment{{ID: "source", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason}},
		Decision:     &taskflow.Decision{Status: "resolved", Reason: "Actual source reference establishes this result.", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := run(t, assessment, binary, "task", "assess", "--task", record.Directory, "--input", "-"); code != 0 {
		t.Fatalf("cw task assess: exit %d\n%s%s", code, stdout, stderr)
	}
	must(t, binary, "task", "verify", "--task", record.Directory)

	for _, denied := range []struct {
		name  string
		setup func(t *testing.T)
		path  string
	}{
		{"staged bytes with the verified bytes back on disk", func(t *testing.T) { stale(t, "a.txt") }, "a.txt"},
		{"a staged mode change alone", func(t *testing.T) { must(t, "git", "update-index", "--chmod=+x", "tool.sh") }, "tool.sh"},
		{"a staged removal of a file still on disk", func(t *testing.T) { must(t, "git", "rm", "-q", "--cached", "keep.txt") }, "keep.txt"},
	} {
		t.Run(denied.name, func(t *testing.T) {
			denied.setup(t)
			defer must(t, "git", "reset", "-q")
			const bash = "git commit -q -m x"
			// Each gate is judged on its own, so a failure shows which one let
			// the commit through.
			var decision hooksDecision
			stdout := prePublish(t, bash)
			if err := json.Unmarshal([]byte(stdout), &decision); err != nil || decision.HookSpecificOutput.PermissionDecision != "deny" ||
				!strings.Contains(decision.HookSpecificOutput.PermissionDecisionReason, record.ID) || !strings.Contains(decision.HookSpecificOutput.PermissionDecisionReason, `"`+denied.path+`"`) {
				t.Errorf("pre-publish printed %q (%v), want a deny naming task %s and %q", stdout, err, record.ID, denied.path)
			}
			before := head(t)
			code, gitStdout, stderr := run(t, nil, "sh", "-c", bash)
			if code != 1 || !strings.Contains(stderr, record.ID) || !strings.Contains(stderr, `"`+denied.path+`"`) {
				t.Errorf("%s through the pre-commit hook: git exit %d, want 1 with the deny naming %s and %q:\n%s%s", bash, code, record.ID, denied.path, gitStdout, stderr)
			}
			if after := head(t); after != before {
				t.Errorf("%s moved HEAD from %s to %s", bash, before, after)
			}
		})
	}

	for _, allowed := range []struct {
		name      string
		setup     func(t *testing.T)
		bash      string
		committed []string
	}{
		{"a partial commit of verified files", func(t *testing.T) { must(t, "git", "add", "a.txt") }, "git commit -q -m partial", []string{"a.txt"}},
		{"git commit PATH over a stale index", func(t *testing.T) { stale(t, "b.txt") }, "git commit -q -m path b.txt", []string{"b.txt"}},
		{"git commit -am over a stale index", func(t *testing.T) { stale(t, "c.txt") }, "git commit -q -am all", []string{"c.txt", "d.txt"}},
		{"git add -A then git commit over a stale index", func(t *testing.T) { stale(t, "e.txt") }, "git add -A && git commit -q -m add", []string{"e.txt"}},
	} {
		t.Run(allowed.name, func(t *testing.T) {
			allowed.setup(t)
			if stdout := prePublish(t, allowed.bash); stdout != "" {
				t.Fatalf("pre-publish for %q printed %s, want no output", allowed.bash, stdout)
			}
			before := head(t)
			if code, stdout, stderr := run(t, nil, "sh", "-c", allowed.bash); code != 0 {
				t.Fatalf("%s: exit %d\n%s%s", allowed.bash, code, stdout, stderr)
			}
			if head(t) == before {
				t.Fatalf("%s did not move HEAD", allowed.bash)
			}
			for _, name := range allowed.committed {
				if got, want := must(t, "git", "show", "HEAD:"+name), name+" verified\n"; got != want {
					t.Fatalf("HEAD holds %q for %s, want the verified %q", got, name, want)
				}
			}
		})
	}
}
