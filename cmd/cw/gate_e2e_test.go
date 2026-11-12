package main_test

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestGateInstallStopsCommitsAndPushesThePluginHookCannotSee installs the
// hooks with the built cw, which git's hooks find on PATH as they would an
// installed cw, then commits with an open unverified task in two ways the
// plugin hook does not recognise, through a git alias and from a shell
// script, and pushes. Oracle: git's exit status, the gate's deny text in
// git's output, and that no commit or remote branch was created
// (`git rev-parse`). Uninstall then leaves the hooks directory as it was,
// byte for byte.
func TestGateInstallStopsCommitsAndPushesThePluginHookCannotSee(t *testing.T) {
	binary := buildPlainCW(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	remote := hooksCanonicalTempDir(t)
	hooksGit(t, home, repo, "init", "-q")
	hooksGit(t, home, remote, "init", "-q", "--bare")
	hooksWriteFile(t, repo, "source.txt", "first\n")
	hooksGit(t, home, repo, "add", "-A")
	hooksGit(t, home, repo, "commit", "-q", "-m", "initial")
	hooks := filepath.Join(repo, ".git", "hooks")
	before := gateSnapshot(t, hooks)

	env := []string{"HOME=" + home, "PATH=" + filepath.Dir(binary) + ":/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "CW_TASK_STORE=" + store,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	run := func(t *testing.T, program string, args ...string) (int, string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), program, args...)
		command.Env, command.Dir = env, repo
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		err := command.Run()
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit.ExitCode(), output.String()
		} else if err != nil {
			t.Fatalf("%s %v did not run: %v", program, args, err)
		}
		return 0, output.String()
	}
	cw := func(t *testing.T, args ...string) {
		t.Helper()
		if code, output := run(t, binary, args...); code != 0 {
			t.Fatalf("cw %v: exit %d\n%s", args, code, output)
		}
	}
	revision := func(t *testing.T, dir, name string) string {
		t.Helper()
		_, output := run(t, "git", "-C", dir, "rev-parse", "--verify", "--quiet", name)
		return strings.TrimSpace(output)
	}

	cw(t, "gate", "install", "--repo", repo)
	hooksWriteFile(t, repo, "source.txt", "second\n")
	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	hooksWriteFile(t, filepath.Dir(plan), "plan.json", `{"title":"Gated","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	cw(t, "task", "start", "--repo", repo, "--plan", plan, "--policy", filepath.Join(module, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(hooksCanonicalTempDir(t), "commit-everything")
	hooksWriteFile(t, filepath.Dir(script), filepath.Base(script), "git commit -q -am 'from a script'\n")
	head := revision(t, repo, "HEAD")
	for _, attempt := range []struct {
		name    string
		program string
		args    []string
	}{
		{"git alias", "git", []string{"-c", "alias.ci=commit", "ci", "-q", "-am", "through an alias"}},
		{"shell script", "/bin/sh", []string{script}},
		{"push", "git", []string{"push", "-q", remote, "HEAD:refs/heads/main"}},
	} {
		code, output := run(t, attempt.program, attempt.args...)
		if code != 1 || !strings.Contains(output, record.ID) || !strings.Contains(output, "cw task verify --task "+record.Directory) {
			t.Fatalf("%s: exit %d, want 1 with the gate's deny text naming %s:\n%s", attempt.name, code, record.ID, output)
		}
		if got := revision(t, repo, "HEAD"); got != head {
			t.Fatalf("%s moved HEAD from %s to %s", attempt.name, head, got)
		}
		if got := revision(t, remote, "refs/heads/main"); got != "" {
			t.Fatalf("%s created the remote branch at %s", attempt.name, got)
		}
	}

	cw(t, "gate", "uninstall", "--repo", repo)
	if after := gateSnapshot(t, hooks); !maps.Equal(after, before) {
		t.Fatalf("hooks directory after uninstall:\n%v\nwant, as before install:\n%v", after, before)
	}
}

// gateSnapshot maps each entry of dir to its mode and bytes; a missing dir,
// as after `git init` with an empty template, has none.
func gateSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		snapshot[entry.Name()] = info.Mode().String() + "\n" + string(data)
	}
	return snapshot
}
