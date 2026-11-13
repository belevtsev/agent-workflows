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
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestGitPreCommitHookRunsTheGate runs the built cw as a real git pre-commit
// hook, so the hook has git's environment: GIT_INDEX_FILE names the locked
// index of `git commit -a`, and GIT_PREFIX, GIT_EXEC_PATH and the author
// variables are set. Oracle: cw's exit status inside the hook (3, denied,
// when the gate blocks), git's own exit status (1 after any failing hook),
// cw's deny text on git's stderr, and whether `git rev-parse HEAD` moved. The sequence deny, allow
// after verify, deny after an edit shows that source capture inside the hook
// reads the source, and that its digest equals what cw task verify recorded
// outside git.
func TestGitPreCommitHookRunsTheGate(t *testing.T) {
	binary := buildPlainCW(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	hooksGit(t, home, repo, "init", "-q")
	hooksWriteFile(t, repo, "source.txt", "first\n")
	hooksWriteFile(t, repo, "a/x.go", "package a\n")
	hooksWriteFile(t, repo, "b/y.go", "package b\n")
	hooksGit(t, home, repo, "add", "-A")
	hooksGit(t, home, repo, "commit", "-q", "-m", "initial")
	// The hook records cw's exit status, which git does not report.
	status := filepath.Join(hooksCanonicalTempDir(t), "status")
	hooksWriteFile(t, repo, ".git/hooks/pre-commit", "#!/bin/sh\n'"+binary+"' hook git pre-commit \"$@\"\nstatus=$?\necho $status > '"+status+"'\nexit $status\n")
	if err := os.Chmod(filepath.Join(repo, ".git", "hooks", "pre-commit"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "CW_TASK_STORE=" + store}
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
	cw := func(t *testing.T, stdin []byte, args ...string) {
		t.Helper()
		if code, stdout, stderr := run(t, stdin, binary, args...); code != 0 {
			t.Fatalf("cw %v: exit %d\n%s%s", args, code, stdout, stderr)
		}
	}
	head := func(t *testing.T) string {
		t.Helper()
		_, stdout, _ := run(t, nil, "git", "rev-parse", "HEAD")
		return strings.TrimSpace(stdout)
	}
	// commit runs `git commit -am MESSAGE` and requires that cw exited with
	// code in the hook, that git exited 0 after a hook that did and 1
	// otherwise, a stderr holding each of contains, and HEAD moved only on
	// success.
	commit := func(t *testing.T, message string, code int, contains ...string) {
		t.Helper()
		before := head(t)
		got, _, stderr := run(t, nil, "git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-am", message)
		recorded, err := os.ReadFile(status)
		if err != nil || strings.TrimSpace(string(recorded)) != strconv.Itoa(code) {
			t.Fatalf("git commit -am %s: cw exited %q in the hook (%v), want %d; stderr:\n%s", message, recorded, err, code, stderr)
		}
		if want := min(code, 1); got != want {
			t.Fatalf("git commit -am %s: git exited %d, want %d; stderr:\n%s", message, got, want, stderr)
		}
		for _, want := range contains {
			if !strings.Contains(stderr, want) {
				t.Fatalf("git commit stderr does not contain %q:\n%s", want, stderr)
			}
		}
		if moved := head(t) != before; moved != (code == 0) {
			t.Fatalf("git commit exit %d, but HEAD moved: %v", got, moved)
		}
	}

	// A verify without check runs passes only on the source the task started
	// from, so the change to commit is made first.
	hooksWriteFile(t, repo, "source.txt", "second\n")
	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	hooksWriteFile(t, filepath.Dir(plan), "plan.json", `{"title":"Hooked","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	cw(t, nil, "task", "start", "--repo", repo, "--plan", plan, "--policy", filepath.Join(module, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	commit(t, "unverified", 3, record.ID, "cw task verify --task "+record.ID+"`")

	hash := sha256.Sum256([]byte("second\n"))
	ref := taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(hash[:])}
	reason := "Read-only source inspection; no executable behavior changed."
	assessment, err := json.Marshal(taskflow.AssessmentInput{
		Requirements: []taskflow.Assessment{{ID: "source", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason}},
		Decision:     &taskflow.Decision{Status: "resolved", Reason: "Actual source reference establishes this result.", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason},
	})
	if err != nil {
		t.Fatal(err)
	}
	cw(t, assessment, "task", "assess", "--full", "--task", record.Directory, "--input", "-")
	cw(t, nil, "task", "verify", "--full", "--task", record.Directory)
	commit(t, "verified", 0)

	hooksWriteFile(t, repo, "source.txt", "third\n")
	commit(t, "edited after verify", 3, record.ID)

	// With the task closed, a substantial change that only -a stages warns
	// and goes ahead: the gate measured the locked index git commits.
	cw(t, nil, "task", "abandon", "--task", record.Directory, "--reason", "test")
	hooksWriteFile(t, repo, "a/x.go", hooksLines(200))
	hooksWriteFile(t, repo, "b/y.go", hooksLines(200))
	commit(t, "unrecorded", 0, "agent-workflows: warning: a git hook cannot ask for confirmation, so this commit goes ahead", "this commit looks like a substantial change")
}
