package taskflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// These tests read the current configuration on the real path: probeReuse
// resolves the go command, reads its go env and the task's run key secret,
// as verify does before its lock.

// committedTask starts plan in a Git repository holding source.txt.
func committedTask(t *testing.T, plan Plan) Record {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("first line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reuseGit(t, repo, "init", "-q")
	reuseGit(t, repo, "add", "-A")
	reuseGit(t, repo, "commit", "-qm", "base")
	record, err := StartWithPacks(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func reuseGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// passThenCommit passes check, then commits nothing, so the snapshot moves
// and the bytes stay.
func passThenCommit(t *testing.T, record Record, check string) {
	t.Helper()
	if run, err := RunCheck(t.Context(), record.Directory, check); err != nil {
		t.Fatalf("RunCheck %s: %s, %v", check, run.Status, err)
	}
	reuseGit(t, record.Baseline.Root, "commit", "-q", "--allow-empty", "-m", "same bytes")
}

// reusedNow judges the latest run of check as verify does: probes before the
// lock, then credit at the current manifest.
func reusedNow(t *testing.T, record Record, check string) (bool, string) {
	t.Helper()
	ctx := context.Background()
	probes := productionEnv.probeReuse(ctx, record.Directory)
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, files, err := productionEnv.source().Manifest(ctx, loaded.Baseline.SelectedPath)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := openTask(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return credited(loaded, *latestRun(loaded, check), productionEnv.credit(ctx, taskDirectory{root}, snapshot, files, probes))
}

// Oracle: with nothing changed but the commit, the go check and the helper
// check are reused; a go env file's GOFLAGS, read by the real go env, changes
// the go check's environment key.
func TestReuseReadsTheGoEnvFile(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	plan := helperPlan(t, "small", "pass")
	plan.Checks = append(plan.Checks, Check{ID: "toolchain", Argv: []string{"go", "version"}, TimeoutSeconds: 120})
	record := committedTask(t, plan)
	passThenCommit(t, record, "toolchain")
	passThenCommit(t, record, "focused")
	for _, check := range []string{"toolchain", "focused"} {
		if ok, reason := reusedNow(t, record, check); !ok || reason != reasonReused {
			t.Fatalf("check %s unchanged: %v, %q; want reused", check, ok, reason)
		}
	}
	goenv := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(goenv, []byte("GOFLAGS=-tags=integration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", goenv)
	if ok, reason := reusedNow(t, record, "toolchain"); ok || reason != reasonEnvKey {
		t.Fatalf("under a go env file: %v, %q; want false, %q", ok, reason, reasonEnvKey)
	}
}

// Oracle: another value of a variable env_passthrough names changes the key.
func TestReuseReadsEnvPassthrough(t *testing.T) {
	t.Setenv("CW_X2_PASS", "first")
	record := committedTask(t, passthroughPlan(t, helperPlan(t, "small", "pass"), "CW_X2_PASS"))
	passThenCommit(t, record, "focused")
	if ok, reason := reusedNow(t, record, "focused"); !ok {
		t.Fatalf("same passthrough value: %v, %q; want reused", ok, reason)
	}
	t.Setenv("CW_X2_PASS", "second")
	if ok, reason := reusedNow(t, record, "focused"); ok || reason != reasonEnvKey {
		t.Fatalf("other passthrough value: %v, %q; want false, %q", ok, reason, reasonEnvKey)
	}
}

// Oracle: a non-go check under GOFLAGS that differ only in a redacted value
// gets another key: the unredacted GOFLAGS is keyed for every check.
func TestReuseKeysTheUnredactedGOFLAGSOfAnyCheck(t *testing.T) {
	t.Setenv("GOFLAGS", "-ldflags=-X=main.mode=strict")
	record := committedTask(t, helperPlan(t, "small", "pass"))
	passThenCommit(t, record, "focused")
	t.Setenv("GOFLAGS", "-ldflags=-X=main.mode=lax")
	if ok, reason := reusedNow(t, record, "focused"); ok || reason != reasonEnvKey {
		t.Fatalf("other redacted GOFLAGS value: %v, %q; want false, %q", ok, reason, reasonEnvKey)
	}
}

// Oracle: another go binary, as ResolveGo reports it, ends the reuse.
func TestReuseReadsTheGoBinary(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	plan.Checks[0].Argv = []string{"go", "version"}
	record := committedTask(t, plan)
	passThenCommit(t, record, "focused")
	real := resolveGo
	t.Cleanup(func() { resolveGo = real })
	resolveGo = func(ctx context.Context, path, dir string, env []string) (langpack.GoTool, error) {
		tool, err := real(ctx, path, dir, env)
		tool.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
		return tool, err
	}
	if ok, reason := reusedNow(t, record, "focused"); ok || reason != reasonTool {
		t.Fatalf("another go binary: %v, %q; want false, %q", ok, reason, reasonTool)
	}
}

// Oracle: without the task's run-key-secret no key can be recomputed, so
// nothing is reused.
func TestReuseNeedsTheRunKeySecret(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	passThenCommit(t, record, "focused")
	if err := os.Rename(filepath.Join(record.Directory, runKeySecretFile), filepath.Join(t.TempDir(), "moved")); err != nil {
		t.Fatal(err)
	}
	if ok, reason := reusedNow(t, record, "focused"); ok || reason[:len(reasonUnreadable)] != reasonUnreadable {
		t.Fatalf("secret moved: %v, %q; want false, %q", ok, reason, reasonUnreadable)
	}
}

// Oracle: credit reads no configuration itself: a check that probeReuse did
// not read before the lock is not reused, and credited never resolves go.
func TestCreditReadsNoConfigurationUnderTheLock(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	plan.Checks[0].Argv = []string{"go", "version"}
	record := committedTask(t, plan)
	passThenCommit(t, record, "focused")
	real := resolveGo
	t.Cleanup(func() { resolveGo = real })
	resolveGo = func(context.Context, string, string, []string) (langpack.GoTool, error) {
		t.Error("credit resolved the go command")
		return langpack.GoTool{}, nil
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, files, err := productionEnv.source().Manifest(t.Context(), loaded.Baseline.SelectedPath)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := openTask(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ok, reason := credited(loaded, *latestRun(loaded, "focused"), productionEnv.credit(t.Context(), taskDirectory{root}, snapshot, files, nil))
	if ok || reason != reasonUnreadable+": "+errNotProbed.Error() {
		t.Fatalf("unprobed check: %v, %q; want false, not read before the lock", ok, reason)
	}
}

// Oracle: a check that needs source.txt at 0600 passes; after chmod 0640,
// which keeps source_sha256, credit is withheld for the permission bits and
// cw task run executes the check, which now fails.
func TestReuseEndsAtAPermissionChange(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "private", "source.txt"))
	first, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatalf("RunCheck at 0600: %s, %v", first.Status, err)
	}
	if err := os.Chmod(filepath.Join(record.Baseline.Root, "source.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if ok, reason := reusedNow(t, record, "focused"); ok || reason != reasonModes {
		t.Fatalf("after chmod 0640: %v, %q; want false, %q", ok, reason, reasonModes)
	}
	run, reused, err := productionEnv.RunOrReuseCheck(t.Context(), record.Directory, "focused")
	if reused != "" || run.ID == first.ID || run.Status != "failed" || err == nil {
		t.Fatalf("cw task run after chmod: run %s %s, reused %q, %v; want a new failed run", run.ID, run.Status, reused, err)
	}
}
