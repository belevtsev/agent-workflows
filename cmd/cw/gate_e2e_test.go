package main_test

import (
	"bytes"
	"errors"
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
// script, pushes, and merges a branch without conflicts, which runs
// pre-merge-commit instead of pre-commit. Oracle: git's exit status, the
// gate's deny text in git's output, and that no commit or remote branch was
// created (`git rev-parse`). Uninstall then leaves the hooks directory as it
// was, byte for byte.
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
	// A branch whose change merges without conflicts.
	hooksGit(t, home, repo, "switch", "-q", "-c", "side")
	hooksWriteFile(t, repo, "side.txt", "side\n")
	hooksGit(t, home, repo, "add", "side.txt")
	hooksGit(t, home, repo, "commit", "-q", "-m", "side")
	hooksGit(t, home, repo, "switch", "-q", "main")
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
		// Last: a stopped merge leaves the merge in progress.
		{"merge commit", "git", []string{"merge", "--no-ff", "--no-edit", "side"}},
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
	if after := gateSnapshot(t, hooks); after != before {
		t.Fatalf("hooks directory after uninstall:\n%s\nwant, as before install:\n%s", after, before)
	}
}

// gateSnapshot lists each entry of dir, hidden ones included, with its mode
// and bytes; a missing dir, as after `git init` with an empty template, is
// "missing", unlike an empty one.
func gateSnapshot(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	} else if err != nil {
		t.Fatal(err)
	}
	var snapshot strings.Builder
	snapshot.WriteString("directory\n")
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		snapshot.WriteString(entry.Name() + " " + info.Mode().String() + "\n" + string(data) + "\n")
	}
	return snapshot.String()
}

// TestGateInstallLetsAMergeOfTheFetchedBaseBranchThrough installs the hooks
// with the built cw, opens an unverified task, and merges without conflicts
// three ways: a local branch, the base branch through git pull, which
// fetches it, and the fetched base branch through git merge. Oracle (F4
// review, design decision): git's exit status, the gate's deny text in git's
// output for the local branch only, and the merge commit's parents
// (`git rev-parse`): git pull and git merge of the remote-tracking branch
// make a merge commit whose second parent is the remote's commit, while the
// local branch's merge leaves HEAD where it was.
func TestGateInstallLetsAMergeOfTheFetchedBaseBranchThrough(t *testing.T) {
	binary := buildPlainCW(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	remote := hooksCanonicalTempDir(t)
	other := filepath.Join(hooksCanonicalTempDir(t), "other")
	hooksGit(t, home, remote, "init", "-q", "--bare")
	hooksGit(t, home, repo, "init", "-q")
	hooksWriteFile(t, repo, "source.txt", "first\n")
	hooksGit(t, home, repo, "add", "-A")
	hooksGit(t, home, repo, "commit", "-q", "-m", "initial")
	hooksGit(t, home, repo, "remote", "add", "origin", remote)
	hooksGit(t, home, repo, "push", "-q", "origin", "main")
	// The base branch moves on in another clone; repo has not fetched it.
	hooksGit(t, home, filepath.Dir(other), "clone", "-q", remote, other)
	hooksWriteFile(t, other, "upstream.txt", "upstream\n")
	hooksGit(t, home, other, "add", "upstream.txt")
	hooksGit(t, home, other, "commit", "-q", "-m", "upstream")
	hooksGit(t, home, other, "push", "-q", "origin", "main")
	// Local work on main, and a local branch, both before the task.
	hooksWriteFile(t, repo, "local.txt", "local\n")
	hooksGit(t, home, repo, "add", "local.txt")
	hooksGit(t, home, repo, "commit", "-q", "-m", "local")
	hooksGit(t, home, repo, "switch", "-q", "-c", "side")
	hooksWriteFile(t, repo, "side.txt", "side\n")
	hooksGit(t, home, repo, "add", "side.txt")
	hooksGit(t, home, repo, "commit", "-q", "-m", "side")
	hooksGit(t, home, repo, "switch", "-q", "main")

	env := []string{"HOME=" + home, "PATH=" + filepath.Dir(binary) + ":/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "CW_TASK_STORE=" + store,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	run := func(t *testing.T, dir, program string, args ...string) (int, string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), program, args...)
		command.Env, command.Dir = env, dir
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
	succeed := func(t *testing.T, program string, args ...string) {
		t.Helper()
		if code, output := run(t, repo, program, args...); code != 0 {
			t.Fatalf("%s %v: exit %d\n%s", program, args, code, output)
		}
	}
	revision := func(t *testing.T, dir, name string) string {
		t.Helper()
		_, output := run(t, dir, "git", "rev-parse", "--verify", "--quiet", name)
		return strings.TrimSpace(output)
	}
	local, upstream := revision(t, repo, "HEAD"), revision(t, other, "HEAD")

	succeed(t, binary, "gate", "install", "--repo", repo)
	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	hooksWriteFile(t, filepath.Dir(plan), "plan.json", `{"title":"Gated","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	succeed(t, binary, "task", "start", "--repo", repo, "--plan", plan, "--policy", filepath.Join(module, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	// A local branch is still judged as a commit, and denied.
	code, output := run(t, repo, "git", "merge", "--no-ff", "--no-edit", "side")
	if code != 1 || !strings.Contains(output, record.ID) || !strings.Contains(output, "cw task verify --task "+record.Directory) {
		t.Fatalf("merge of the local branch: exit %d, want 1 with the gate's deny text naming %s:\n%s", code, record.ID, output)
	}
	if got := revision(t, repo, "HEAD"); got != local {
		t.Fatalf("merge of the local branch moved HEAD from %s to %s", local, got)
	}
	succeed(t, "git", "merge", "--abort")

	for _, merge := range []struct {
		name string
		args []string
	}{
		{"git pull, which fetches the base branch", []string{"pull", "-q", "--no-rebase", "--no-edit", "origin", "main"}},
		{"git merge of the fetched base branch", []string{"merge", "-q", "--no-ff", "--no-edit", "origin/main"}},
	} {
		succeed(t, "git", "reset", "-q", "--hard", local)
		if code, output := run(t, repo, "git", merge.args...); code != 0 {
			t.Fatalf("%s: exit %d, want 0\n%s", merge.name, code, output)
		}
		if first, second := revision(t, repo, "HEAD^1"), revision(t, repo, "HEAD^2"); first != local || second != upstream {
			t.Fatalf("%s: merge commit parents %s and %s, want %s and %s", merge.name, first, second, local, upstream)
		}
	}
}
