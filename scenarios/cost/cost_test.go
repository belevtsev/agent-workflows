// Package cost_test measures what one hook decision costs (Success measure 2
// and Open Question 10 of openspec/changes/reduce-cw-to-evidence-gate/
// proposal.md; task 10.1).
//
// How it measures:
//   - A decision is the pre-commit hook file that `cw init` writes, started
//     with sh as git starts it, in the root of a repository whose content is
//     verified, so the hook allows. The time runs from the start of sh to
//     its exit, so it holds the start of sh, the start of cw and the whole
//     decision. git's own work before and after the hook is not in it.
//   - The environment is the one the hook would get from git: the test
//     harness's clean environment, cw first on PATH, and GIT_INDEX_FILE
//     naming the index. Its output is discarded.
//   - Each repository gets one warm-up decision and then 15 measured ones,
//     one after the other. The median of the 15 is compared with the limit.
//     Every value is printed, on every system.
//   - The repositories are a copy of this repository's tracked files and a
//     generated repository of 30,000 files and 68 MiB. Each declares one
//     trivial check, `git --version`, and `cw run` records it once before
//     the measurement: the cost of a decision depends on the tree, not on
//     what the checks do.
//   - The limits are 150 ms and 500 ms, on Linux and macOS. On Windows the
//     test measures and reports only.
//   - After the decisions it reports two floors, measured the same way: a
//     hook file that only exits, and `cw version` started without sh.
//
// The test runs only when CW_COST=1, so that a plain `go test ./...` stays
// fast and is not judged by the speed of the machine it runs on.
package cost_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

const (
	warmups  = 1
	measured = 15

	ownLimit       = 150 * time.Millisecond
	generatedLimit = 500 * time.Millisecond

	generatedFiles = 30_000
	generatedBytes = 68 << 20
)

// declaration is the cw.toml of both measured repositories.
const declaration = `version = 1

[[check]]
id = "trivial"
run = ["git", "--version"]
`

func TestHookCostOnThisRepository(t *testing.T) {
	needCost(t)
	repo := cwtest.NewRepo(t)
	count, size := copyTracked(t, repo.Dir)
	t.Logf("a copy of this repository: %d files, %.1f MiB", count, float64(size)/(1<<20))
	measure(t, repo, ownLimit)
}

func TestHookCostOnAGeneratedRepository(t *testing.T) {
	needCost(t)
	repo := cwtest.NewRepo(t)
	generate(t, repo.Dir)
	t.Logf("a generated repository: %d files, %d MiB", generatedFiles, generatedBytes>>20)
	measure(t, repo, generatedLimit)
}

// needCost skips the test unless CW_COST=1.
func needCost(t *testing.T) {
	t.Helper()
	if os.Getenv("CW_COST") != "1" {
		t.Skip("the hook cost is measured only with CW_COST=1")
	}
}

// measure commits the files, installs the hooks, records the trivial check
// and then times the pre-commit hook.
func measure(t *testing.T, repo *cwtest.Repo, limit time.Duration) {
	t.Helper()
	repo.Write("cw.toml", declaration)
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "--no-verify", "-m", "measured content")
	for _, args := range [][]string{{"init"}, {"run"}} {
		if result := repo.CW(args...); result.Code != 0 {
			t.Fatalf("cw %s exited %d:\n%s", strings.Join(args, " "), result.Code, result.Output())
		}
	}

	hook := hookCommand(t, repo)
	t.Logf("the decision: %s", strings.Join(hook.Args, " "))
	// The hook must judge: content without a run is denied. Then the
	// repository goes back to its verified content.
	repo.Write("unverified.txt", "not checked\n")
	repo.Git("add", "unverified.txt")
	if _, err := decide(hook); err == nil {
		t.Fatal("the hook allowed content that has no run")
	}
	repo.Git("rm", "-q", "--cached", "unverified.txt")
	repo.Remove("unverified.txt")
	for range warmups {
		if _, err := decide(hook); err != nil {
			t.Fatalf("the warm-up decision did not allow: %v", err)
		}
	}
	times := make([]time.Duration, 0, measured)
	for range measured {
		took, err := decide(hook)
		if err != nil {
			t.Fatalf("a decision did not allow: %v", err)
		}
		times = append(times, took)
	}

	sorted := slices.Clone(times)
	slices.Sort(sorted)
	median := sorted[len(sorted)/2]
	t.Logf("%s/%s: %d decisions after %d warm-up: median %s, min %s, max %s, limit %s",
		runtime.GOOS, runtime.GOARCH, measured, warmups, ms(median), ms(sorted[0]), ms(sorted[len(sorted)-1]), ms(limit))
	t.Logf("each decision in order: %s", joinMS(times))
	switch {
	case runtime.GOOS == "windows":
		t.Logf("Windows: no limit applies; the number is reported only")
	case median > limit:
		t.Errorf("the median decision took %s, above the limit of %s", ms(median), ms(limit))
	}
	floors(t, repo, hook)
}

// floors reports what a decision cannot go below on this machine, so that a
// reader can tell the cost of cw from the cost of starting a hook at all: a
// hook file that only exits, started the same way, and cw started without sh.
func floors(t *testing.T, repo *cwtest.Repo, hook *exec.Cmd) {
	t.Helper()
	if len(hook.Args) == 2 { // sh and the hook file
		empty := filepath.Join(t.TempDir(), "pre-commit")
		if err := os.WriteFile(empty, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Logf("a hook file that only exits, started the same way: median %s", ms(medianOf(t, repo.Command(hook.Path, empty))))
	}
	direct := repo.Command(filepath.Join(cwtest.Binary(t), cwName()), "version")
	t.Logf("cw version, started without sh: median %s", ms(medianOf(t, direct)))
}

// medianOf runs a command as often as a decision is measured, after the same
// warm-up, and returns the median time.
func medianOf(t *testing.T, command *exec.Cmd) time.Duration {
	t.Helper()
	times := make([]time.Duration, 0, measured)
	for n := range warmups + measured {
		took, err := decide(command)
		if err != nil {
			t.Fatalf("%s: %v", strings.Join(command.Args, " "), err)
		}
		if n >= warmups {
			times = append(times, took)
		}
	}
	slices.Sort(times)
	return times[len(times)/2]
}

// hookCommand is the installed pre-commit hook as git starts it. Where no sh
// is found, which git on Windows brings along, it falls back to the line
// the hook file runs.
func hookCommand(t *testing.T, repo *cwtest.Repo) *exec.Cmd {
	t.Helper()
	gitDir := repo.Git("rev-parse", "--absolute-git-dir")
	file := filepath.Join(gitDir, "hooks", "pre-commit")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("cw init wrote no pre-commit hook: %v", err)
	}
	var command *exec.Cmd
	if sh, err := exec.LookPath("sh"); err == nil {
		command = repo.Command(sh, file)
	} else {
		t.Logf("no sh on PATH; the decision is cw hook pre-commit started directly")
		command = repo.Command(filepath.Join(cwtest.Binary(t), cwName()), "hook", "pre-commit")
	}
	command.Env = append(command.Env, "GIT_INDEX_FILE="+filepath.Join(gitDir, "index"))
	return command
}

func cwName() string {
	if runtime.GOOS == "windows" {
		return "cw.exe"
	}
	return "cw"
}

// decide runs the hook once and returns how long it took from start to exit.
func decide(template *exec.Cmd) (time.Duration, error) {
	command := exec.Command(template.Path, template.Args[1:]...)
	command.Dir, command.Env = template.Dir, template.Env
	var stderr strings.Builder
	command.Stderr = &stderr
	start := time.Now()
	err := command.Run()
	took := time.Since(start)
	if err != nil {
		return took, fmt.Errorf("%w\n%s", err, stderr.String())
	}
	return took, nil
}

// copyTracked copies the tracked files of the repository this test is part
// of into dir. It reads the working tree, so it needs no history.
func copyTracked(t *testing.T, dir string) (count int, size int64) {
	t.Helper()
	root := moduleRoot(t)
	list := exec.Command("git", "ls-files", "-z")
	list.Dir = root
	list.Env = withoutGit(os.Environ())
	out, err := list.Output()
	if err != nil {
		t.Fatalf("git ls-files in %s: %v", root, err)
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		source := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			continue // deleted in the working tree
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		count++
		size += int64(len(data))
	}
	return count, size
}

// withoutGit drops the variables by which git would read another repository,
// such as those a hook that runs this test sets.
func withoutGit(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			out = append(out, kv)
		}
	}
	return out
}

// moduleRoot is the directory of go.mod, found from the test's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// generate writes 30,000 files of 68 MiB in all into dir: 30 folders of 10
// folders of 100 files. Their text is made from a fixed seed, so every run
// writes the same bytes.
func generate(t *testing.T, dir string) {
	t.Helper()
	each, extra := generatedBytes/generatedFiles, generatedBytes%generatedFiles
	state := uint64(0x9e3779b97f4a7c15)
	const digits = "0123456789abcdef"
	for i := range generatedFiles {
		size := each
		if i < extra {
			size++
		}
		data := make([]byte, size)
		for j := range data {
			if j%64 == 63 {
				data[j] = '\n'
				continue
			}
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			data[j] = digits[state&15]
		}
		rel := filepath.Join(fmt.Sprintf("d%02d", i/1000), fmt.Sprintf("s%d", i/100%10), fmt.Sprintf("f%05d.txt", i))
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
}

func joinMS(times []time.Duration) string {
	parts := make([]string, len(times))
	for i, d := range times {
		parts[i] = fmt.Sprintf("%.1f", float64(d)/float64(time.Millisecond))
	}
	return strings.Join(parts, ", ") + " ms"
}
