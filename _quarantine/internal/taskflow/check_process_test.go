//go:build unix

package taskflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pgGroupHasLiveMember reports whether process group pgid has a member that
// is not a zombie. It only ever sends signal 0, and only to a group led by a
// process this test started. kill(-pgid, 0) failing with ESRCH (no member) or
// EPERM (macOS: only zombies remain) means no live member. On Linux a zombie
// still accepts signal 0, so a success there is checked against /proc.
func pgGroupHasLiveMember(pgid int) (bool, error) {
	if pgid <= 1 || pgid == os.Getpid() || pgid == syscall.Getpgrp() {
		return false, fmt.Errorf("refusing to probe process group %d", pgid)
	}
	err := syscall.Kill(-pgid, 0)
	switch {
	case errors.Is(err, syscall.ESRCH), errors.Is(err, syscall.EPERM):
		return false, nil
	case err != nil:
		return false, err
	case runtime.GOOS == "linux":
		return pgLinuxGroupHasNonZombie(pgid)
	}
	return true, nil
}

// pgLinuxGroupHasNonZombie scans /proc for a member of pgid whose state is not
// zombie (Z) or dead (X).
func pgLinuxGroupHasNonZombie(pgid int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	want := strconv.Itoa(pgid)
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		stat := string(data)
		end := strings.LastIndexByte(stat, ')')
		if end < 0 {
			continue
		}
		// After the command name: state, parent pid, process group.
		fields := strings.Fields(stat[end+1:])
		if len(fields) < 3 || fields[2] != want {
			continue
		}
		if fields[0] != "Z" && fields[0] != "X" {
			return true, nil
		}
	}
	return false, nil
}

// pgLeaderGroupLive reads the pid the storm leader recorded and reports
// whether its process group still has a live member. The check runs in its
// own process group, which a process creates with its own pid as the id.
func pgLeaderGroupLive(pidFile string) (bool, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, err
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, err
	}
	return pgGroupHasLiveMember(pgid)
}

// pgRequireStormDescendantScript proves the storm descendant reports a
// survivor whose forker is gone once RunCheck has returned, and stays silent while
// its forker is alive.
func pgRequireStormDescendantScript(t *testing.T, root string) {
	t.Helper()
	gone := exec.Command("/bin/sh", "-c", "exit 0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	returned := filepath.Join(root, "control-returned")
	if err := os.WriteFile(returned, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	survivor, owned := filepath.Join(root, "control-survivor"), filepath.Join(root, "control-owned")
	orphan := exec.Command("/bin/sh", "-c", pgStormDescendantScript, "sh", strconv.Itoa(gone.ProcessState.Pid()), returned, survivor)
	live := exec.Command("/bin/sh", "-c", pgStormDescendantScript, "sh", strconv.Itoa(os.Getpid()), returned, owned)
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Fatalf("storm descendant cannot report a survivor: %v", err)
	}
	if _, err := os.Stat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("storm descendant reported a survivor while its forker was alive: %v", err)
	}
}

// TestNormalCheckExitKillsForkingBackgroundGroupUntilEmpty (TF-1) runs a
// check that starts one redirected storm forker in its process group, waits
// until it is forking and exits 0. On macOS one group kill after the command
// finishes can miss a child being forked at that moment. The forker matches
// the measured graphify fork-storm (64 descendants that live 0.2 s, ready
// after 8). Measured with that forker against a single kill after a normal
// exit, 7, 9 and 10 of 40 trials left a survivor; one Go forker has at most
// one fork in flight, so each survivor marks a separate trial. At the lowest
// measured rate (17.5%) a single-kill cleanup survives all 36 trials with
// probability 0.825^36 < 0.1%; if the check runner's own timing halved that
// rate (8.75%), 0.9125^36 < 4%. Each trial starts a fresh task and runs the
// check once; with Start, RunCheck and a helper leader and forker per trial the
// test is sized for about 0.3-0.8 s per trial (11-29 s) plus the observation.
// The check, right after each return, that no member of the check's group is
// alive only adds detection.
func TestNormalCheckExitKillsForkingBackgroundGroupUntilEmpty(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("owned process-group cleanup is available on native Linux and macOS builds")
	}
	const trials, observation = 36, 1500 * time.Millisecond
	root := t.TempDir()
	pgRequireStormDescendantScript(t, root)
	survivors := filepath.Join(root, "survivors")
	if err := os.Mkdir(survivors, 0o700); err != nil {
		t.Fatal(err)
	}
	for trial := range trials {
		directory := filepath.Join(root, fmt.Sprintf("trial-%d", trial))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		record, _ := fixtureTask(t, helperPlan(t, "small", "storm-leader", directory, survivors))
		run, err := RunCheck(t.Context(), record.Directory, "focused")
		live, probeErr := pgLeaderGroupLive(filepath.Join(directory, "leader-pid"))
		// From here on, any descendant of this trial still running outlived RunCheck.
		if writeErr := os.WriteFile(filepath.Join(directory, "returned"), nil, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil || run.Status != "passed" {
			t.Fatalf("trial %d: forking check: %+v %v", trial, run, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "running")); err != nil {
			t.Fatalf("trial %d: forker never reported running: %v", trial, err)
		}
		if probeErr != nil {
			t.Fatalf("trial %d: %v", trial, probeErr)
		}
		if live {
			t.Fatalf("trial %d: a member of the check's process group was alive after RunCheck returned", trial)
		}
	}
	// A survivor reports within about 0.3 s of its trial's return marker.
	time.Sleep(observation)
	entries, err := os.ReadDir(survivors)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("%d storm descendants outlived a passed check: %v", len(names), names)
	}
}

// storedSourceManifest fails unless the task holds the source manifest named
// by sum as a private file whose bytes hash to sum.
func storedSourceManifest(t *testing.T, record Record, sum string) {
	t.Helper()
	path := filepath.Join(record.Directory, sourceManifestName(sum))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the task holds no manifest for source %s: %v", sum, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || digest(data) != sum || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest %s: mode %v, sha256 %s, %v; want a private file whose bytes hash to its name", path, info.Mode(), digest(data), err)
	}
}

// TestRunCheckRecordsTheSourceDigestOfTheRun runs a check twice at one source
// and once after an edit. Oracle (X1): each run's source_sha256 equals
// SourceDigest of the tree it ran at; the task holds that manifest as
// manifest-<sha>.json, written once per digest, so a second run at the same
// source still passes; an edit gives a new digest and a second manifest.
func TestRunCheckRecordsTheSourceDigestOfTheRun(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	runAt := func() (CheckRun, string) {
		t.Helper()
		run, err := RunCheck(t.Context(), record.Directory, "focused")
		if err != nil {
			t.Fatalf("RunCheck: %s, %v", run.Status, err)
		}
		want, err := SourceDigest(t.Context(), record.Baseline.SelectedPath)
		if err != nil {
			t.Fatal(err)
		}
		return run, want
	}
	first, want := runAt()
	if first.SourceSHA256 != want {
		t.Fatalf("run source_sha256 = %q, want SourceDigest %q", first.SourceSHA256, want)
	}
	storedSourceManifest(t, record, want)
	if second, _ := runAt(); second.SourceSHA256 != want {
		t.Errorf("a second run at the same source recorded %q, want %q", second.SourceSHA256, want)
	}
	changedWorkerSource(t, record)
	third, changed := runAt()
	if third.SourceSHA256 != changed || changed == want {
		t.Errorf("a run after an edit recorded %q, want the new SourceDigest %q (old %q)", third.SourceSHA256, changed, want)
	}
	storedSourceManifest(t, record, changed)
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for i, sum := range []string{want, want, changed} {
		if loaded.Checks[i].SourceSHA256 != sum {
			t.Errorf("stored run %d source_sha256 = %q, want %q", i, loaded.Checks[i].SourceSHA256, sum)
		}
	}
}

// TestSubmitWorkerResultRecordsTheSourceDigestOfTheReview submits a completed
// review. Oracle (X1): the bound result's source_sha256 equals SourceDigest of
// the tree it was submitted at, and the submit stores that manifest: moved
// aside after the check that first wrote it, it is back after the submit.
func TestSubmitWorkerResultRecordsTheSourceDigestOfTheReview(t *testing.T) {
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	want, err := SourceDigest(t.Context(), record.Baseline.SelectedPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(record.Directory, sourceManifestName(want))
	if err := os.Rename(manifest, filepath.Join(t.TempDir(), "aside.json")); err != nil {
		t.Fatalf("precondition: the check run stored no manifest to move aside: %v", err)
	}
	record = mustSubmitWorker(t, record, workerResult(t, record, "review", "completed", ref))
	if bound := findWorkerResult(record, "review"); bound == nil || bound.SourceSHA256 != want {
		t.Fatalf("review result %+v, want source_sha256 %q", bound, want)
	}
	storedSourceManifest(t, record, want)
}

// keyedTask starts a task whose checks, focused (the helper) and toolchain
// (go version), both name CW_X1_DB_PASSWORD in env_passthrough.
func keyedTask(t *testing.T) Record {
	t.Helper()
	plan := helperPlan(t, "small", "pass")
	plan.Checks = append(plan.Checks, Check{ID: "toolchain", Argv: []string{"go", "version"}, TimeoutSeconds: 120})
	plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, "toolchain")
	record, _ := fixtureTask(t, passthroughPlan(t, plan, "CW_X1_DB_PASSWORD"))
	return record
}

// TestRunCheckKeysTheEnvironmentWithoutItsValues runs a go check under two
// CGO_ENABLED values and two env_passthrough values, and a non-go check
// whose env_passthrough names a variable under two values, unset and empty.
// Oracle (X1): cmd/go reports CGO_ENABLED from the environment, so the same
// environment gives the same env_key and another CGO_ENABLED another; each
// passthrough state gives its own key, for the go check too (review finding
// 4c on #146); the key is a hex SHA-256, and no task file but the check's
// own log holds the passthrough value.
func TestRunCheckKeysTheEnvironmentWithoutItsValues(t *testing.T) {
	record := keyedTask(t)
	key := func(checkID string) string {
		t.Helper()
		run, err := RunCheck(t.Context(), record.Directory, checkID)
		if err != nil {
			t.Fatalf("RunCheck %s: %s, %v", checkID, run.Status, err)
		}
		if !validSHA256(run.EnvKey) {
			t.Fatalf("run %s env_key = %q, want a hex SHA-256", checkID, run.EnvKey)
		}
		return run.EnvKey
	}
	t.Setenv("CGO_ENABLED", "0")
	off := key("toolchain")
	if again := key("toolchain"); again != off {
		t.Errorf("the same environment gave env_key %s, then %s", off, again)
	}
	t.Setenv("CGO_ENABLED", "1")
	on := key("toolchain")
	if on == off {
		t.Errorf("CGO_ENABLED 0 and 1 gave the same env_key %s", on)
	}
	const secret = "cw-x1-passthrough-value"
	t.Setenv("CW_X1_DB_PASSWORD", secret)
	if passed := key("toolchain"); passed == on {
		t.Errorf("the go check's env_key %s ignores its env_passthrough value", on)
	}
	t.Setenv("CW_X1_DB_PASSWORD", secret)
	set := key("focused")
	t.Setenv("CW_X1_DB_PASSWORD", secret+"-changed")
	changed := key("focused")
	if err := os.Unsetenv("CW_X1_DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	unset := key("focused")
	t.Setenv("CW_X1_DB_PASSWORD", "")
	empty := key("focused")
	if set == changed || set == unset || changed == unset || unset == empty {
		t.Errorf("passthrough env_keys set %s, changed %s, unset %s, empty %s; want all different", set, changed, unset, empty)
	}
	entries, err := os.ReadDir(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log") {
			continue // the check's own output, not what cw records
		}
		if data, err := os.ReadFile(filepath.Join(record.Directory, entry.Name())); err == nil && strings.Contains(string(data), secret) {
			t.Errorf("task file %s holds the env_passthrough value", entry.Name())
		}
	}
}

// TestRunKeyIsSaltedPerTask runs the same checks under the same environment
// in two tasks. Oracle (review finding 1 on #146): env_key is an HMAC under a
// random secret of each task, kept as a private task file, so within one
// task the same configuration gives the same key, two tasks give different
// keys, and a printed key does not confirm a guessed weak value offline.
func TestRunKeyIsSaltedPerTask(t *testing.T) {
	t.Setenv("CW_X1_DB_PASSWORD", "hunter2")
	first, second := keyedTask(t), keyedTask(t)
	key := func(record Record, checkID string) string {
		t.Helper()
		run, err := RunCheck(t.Context(), record.Directory, checkID)
		if err != nil {
			t.Fatalf("RunCheck %s: %s, %v", checkID, run.Status, err)
		}
		return run.EnvKey
	}
	for _, checkID := range []string{"focused", "toolchain"} {
		one := key(first, checkID)
		if again := key(first, checkID); again != one {
			t.Errorf("check %s: one task gave env_key %s, then %s", checkID, one, again)
		}
		if other := key(second, checkID); other == one {
			t.Errorf("check %s: two tasks gave the same env_key %s", checkID, one)
		}
	}
	for _, record := range []Record{first, second} {
		info, err := os.Stat(filepath.Join(record.Directory, runKeySecretFile))
		if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 32 {
			t.Errorf("task %s run key secret: %v, %v; want a private 32-byte file", record.ID, info, err)
		}
	}
}

// TestSourceOverTheManifestLimitIsRecordedWithoutADigest lowers the manifest
// bound below the fixture's manifest. Oracle (review finding 2 on #146): the
// check still runs and passes and a worker result is still accepted; both are
// recorded without source_sha256, and the task stores no source manifest.
func TestSourceOverTheManifestLimitIsRecordedWithoutADigest(t *testing.T) {
	maxSourceManifestBytes = 16
	t.Cleanup(func() { maxSourceManifestBytes = MaxInputBytes })
	record, ref := preparedChangedTask(t)
	if run := record.Checks[len(record.Checks)-1]; run.Status != "passed" || run.SourceSHA256 != "" {
		t.Fatalf("run over the manifest limit: %s, source_sha256 %q; want passed with none", run.Status, run.SourceSHA256)
	}
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, "review", "completed", ref))
	if bound := findWorkerResult(record, "review"); bound == nil || bound.SourceSHA256 != "" {
		t.Fatalf("result over the manifest limit %+v, want no source_sha256", bound)
	}
	if manifests, _ := filepath.Glob(filepath.Join(record.Directory, "manifest-*.json")); len(manifests) != 0 {
		t.Errorf("source manifests over the limit were stored: %q", manifests)
	}
}

// TestRunCheckRefusesAStoredManifestThatDiffersFromItsName replaces the
// manifest a first run stored with other bytes. Oracle (review finding 4b on
// #146): a manifest is written once per digest and an existing one must hash
// to its name, so the next run at the same source is refused, names the file
// and records no run.
func TestRunCheckRefusesAStoredManifestThatDiffersFromItsName(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, sourceManifestName(run.SourceSHA256)), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err == nil || !strings.Contains(err.Error(), "stored source manifest "+sourceManifestName(run.SourceSHA256)+" differs from its name") {
		t.Fatalf("RunCheck over a corrupt manifest: %v; want a refusal naming it", err)
	}
	if loaded, err := Load(record.Directory); err != nil || len(loaded.Checks) != 1 {
		t.Errorf("after the refusal the record has %d runs (%v), want 1", len(loaded.Checks), err)
	}
}

// TestLoadValidatesTheRunAndResultKeys stores a check run and a worker result
// with each X1 member missing or malformed. Oracle (review finding 4a on
// #146): a missing member loads, as in a record written before X1; a value
// that is not a hex SHA-256 fails validation. The result's hash is
// recomputed, so only the member's own check can refuse it.
func TestLoadValidatesTheRunAndResultKeys(t *testing.T) {
	t.Parallel()
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, "review", "completed", ref))
	if err := validateRecord(record); err != nil {
		t.Fatalf("precondition: the recorded task does not validate: %v", err)
	}
	for _, c := range []struct {
		name  string
		value string
		valid bool
	}{{"missing", "", true}, {"short", "abc123", false}, {"not hex", strings.Repeat("z", 64), false}, {"upper and long", strings.Repeat("A", 66), false}} {
		for _, member := range []string{"run source_sha256", "run env_key", "result source_sha256"} {
			t.Run(c.name+" "+member, func(t *testing.T) {
				changed, err := Load(record.Directory)
				if err != nil {
					t.Fatal(err)
				}
				run := &changed.Checks[len(changed.Checks)-1]
				switch member {
				case "run source_sha256":
					run.SourceSHA256 = c.value
				case "run env_key":
					run.EnvKey = c.value
				default:
					bound := &changed.WorkerResults[len(changed.WorkerResults)-1]
					bound.SourceSHA256 = c.value
					if bound.SHA256, err = workerResultHash(*bound); err != nil {
						t.Fatal(err)
					}
				}
				if err := validateRecord(changed); (err == nil) != c.valid {
					t.Errorf("validateRecord with %s %q: %v; want valid %v", member, c.value, err, c.valid)
				}
			})
		}
	}
}
