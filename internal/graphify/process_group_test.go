//go:build linux || darwin

package graphify

import (
	"context"
	"encoding/json/v2"
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

const (
	// pgNormalExitLeaderScript records its pid ($4), starts one fork-storm
	// forker ($1 is the test binary, $2 its config) in the background with
	// its output redirected to a file ($5) and stdin from /dev/null, waits
	// until the forker reports it is forking ($3) and exits 0. It gives up
	// with exit 3 after about ten seconds.
	pgNormalExitLeaderScript = `echo "$$" > "$4"
"$1" fake-graphify "$2" extract . --code-only --no-dedup >"$5" 2>&1 </dev/null &
i=0
while [ ! -e "$3" ]; do
	i=$((i + 1))
	if [ "$i" -gt 1000 ]; then exit 3; fi
	sleep 0.01
done
exit 0`
	// pgSwarmLeaderScript records its pid ($4), starts $3 fork-swarm forkers
	// ($1 is the test binary, $2 their config) in the background with all
	// output redirected, and waits for them. None of the forkers is the group
	// leader.
	pgSwarmLeaderScript = `echo "$$" > "$4"
i=0
while [ "$i" -lt "$3" ]; do
	"$1" fake-graphify "$2" extract . --code-only --no-dedup >/dev/null 2>&1 </dev/null &
	i=$((i + 1))
done
wait`
	// pgSwarmForkers forkers with at most pgSwarmConcurrency descendants
	// each keep the group at about 32 * (12 + 1) + 1 = 417 live processes.
	pgSwarmForkers = 32
	// pgSwarmRestarts is how many times one run of the swarm test may start
	// a trial again because the start measured nothing: the leader ended by
	// itself, or a forker had stopped before the kill. A host that fails
	// more starts than that cannot run the swarm.
	pgSwarmRestarts = 10
	pgRunTimeout    = 30 * time.Second
	pgRunLimit      = 1 << 20
)

// pgFakeEnv is the exact environment of every leader in these tests. The race
// option stops a race-built fake from sleeping about a second at exit.
func pgFakeEnv() []string {
	return []string{"PATH=/usr/bin:/bin", "GORACE=atexit_sleep_ms=0"}
}

func pgRequireRegressionPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process-group cleanup regressions are measured on native Linux and macOS")
	}
}

func pgWriteFakeConfig(t *testing.T, path string, config fakeConfig) string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// pgLeaderGroupLive reads the pid a leader recorded and reports whether its
// group (the runner starts the leader with setsid, so pgid = pid) still has a
// live member.
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

func pgRequireNoMarkers(t *testing.T, markers, message string) {
	t.Helper()
	survivors, err := os.ReadDir(markers)
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 0 {
		names := make([]string, 0, len(survivors))
		for _, survivor := range survivors {
			names = append(names, survivor.Name())
		}
		t.Fatalf("%d %s: %v", len(names), message, names)
	}
}

func pgRefuseCleanupFailure(t *testing.T, trial int, err error) {
	t.Helper()
	if refused, ok := errors.AsType[*Error](err); ok && refused.Code == "process_cleanup" {
		t.Fatalf("trial %d: group still had a live member after the kill bound: %v", trial, err)
	}
}

// pgRequireSwarmDescendantScript proves the fork-swarm descendant creates its
// marker when its forker never released it and the call has returned, and
// stays silent when its forker released it.
func pgRequireSwarmDescendantScript(t *testing.T, root string) {
	t.Helper()
	returned := filepath.Join(root, "swarm-control-returned")
	if err := os.WriteFile(returned, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	orphanMarker, releasedMarker := filepath.Join(root, "swarm-control-orphan"), filepath.Join(root, "swarm-control-released")
	orphan := exec.Command("/bin/sh", "-c", pgSwarmDescendantScript, "sh", returned, orphanMarker)
	orphan.Stdin = strings.NewReader("")
	released := exec.Command("/bin/sh", "-c", pgSwarmDescendantScript, "sh", returned, releasedMarker)
	released.Stdin = strings.NewReader("x\n")
	for _, command := range []*exec.Cmd{orphan, released} {
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(orphanMarker); err != nil {
		t.Fatalf("swarm descendant cannot report a survivor: %v", err)
	}
	if _, err := os.Stat(releasedMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("swarm descendant reported a survivor although its forker released it: %v", err)
	}
}

type pgRunAttempt struct {
	code int
	err  error
}

// pgStartRun runs runWithOutputLimit in the background under a cancellable
// child of the test context. Cleanup cancels it and joins the call.
func pgStartRun(t *testing.T, arguments []string, directory string) (<-chan pgRunAttempt, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan pgRunAttempt, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		code, _, _, err := runWithOutputLimit(ctx, arguments, directory, pgFakeEnv(), pgRunTimeout, pgRunLimit)
		done <- pgRunAttempt{code: code, err: err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(15 * time.Second):
			t.Error("background runWithOutputLimit did not return after the test context ended")
		}
	})
	return done, cancel
}

func pgAwaitRun(t *testing.T, done <-chan pgRunAttempt, limit time.Duration) pgRunAttempt {
	t.Helper()
	select {
	case attempt := <-done:
		return attempt
	case <-time.After(limit):
		t.Fatalf("runWithOutputLimit did not return within %v", limit)
		return pgRunAttempt{}
	}
}

// pgAwaitForkers polls until want forkers report themselves forking in
// directory at the same time, and reports whether they did. A forker that
// stops by itself withdraws its report. It reports false when the call
// returned first without an error: the leader ended by itself, because a fork
// failed or forkers stopped before all of them were forking. That start
// measured nothing, so the caller starts the trial again. It fails the test if
// the call returned an error or the deadline passes.
func pgAwaitForkers(t *testing.T, directory string, want int, done <-chan pgRunAttempt, limit time.Duration) bool {
	t.Helper()
	deadline := time.After(limit)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) >= want {
			return true
		}
		select {
		case attempt := <-done:
			if attempt.err != nil {
				t.Fatalf("runWithOutputLimit returned with %d of %d forkers running: %d %v", len(entries), want, attempt.code, attempt.err)
			}
			t.Logf("the leader exited %d by itself with %d of %d forkers running", attempt.code, len(entries), want)
			return false
		case <-deadline:
			t.Fatalf("%d of %d forkers running after %v", len(entries), want, limit)
		case <-tick.C:
		}
	}
}

// pgSwarm is one started fork swarm: the call that runs its leader and the
// files of that start.
type pgSwarm struct {
	done     <-chan pgRunAttempt
	cancel   context.CancelFunc
	running  string
	returned string
	pidFile  string
}

// pgStartSwarm starts a leader with pgSwarmForkers fork-swarm forkers, names
// the files of this start after name and waits until every forker is forking.
// It reports false when the leader ended by itself before that.
func pgStartSwarm(t *testing.T, root, binary, markers, name string) (pgSwarm, bool) {
	t.Helper()
	swarm := pgSwarm{
		running:  filepath.Join(root, "swarm-running-"+name),
		returned: filepath.Join(root, "swarm-returned-"+name),
		pidFile:  filepath.Join(root, "leader-"+name+".pid"),
	}
	if err := os.Mkdir(swarm.running, 0o700); err != nil {
		t.Fatal(err)
	}
	config := pgWriteFakeConfig(t, filepath.Join(root, "swarm-"+name+".json"),
		fakeConfig{Mode: "fork-swarm", Running: swarm.running, Returned: swarm.returned, Markers: markers})
	arguments := []string{"/bin/sh", "-c", pgSwarmLeaderScript, "sh", binary, config, strconv.Itoa(pgSwarmForkers), swarm.pidFile}
	swarm.done, swarm.cancel = pgStartRun(t, arguments, root)
	return swarm, pgAwaitForkers(t, swarm.running, pgSwarmForkers, swarm.done, 20*time.Second)
}

// pgSwarmTrial starts one swarm, cancels the call once every forker is
// forking and fails the test when a member of the group outlives the return.
// It returns "" when the kill found every forker forking, and otherwise why
// this start measured nothing. The restarts so far keep the files of each
// start of a trial apart.
func pgSwarmTrial(t *testing.T, root, binary, markers string, trial, restarts int) string {
	swarm, forking := pgStartSwarm(t, root, binary, markers, fmt.Sprintf("%d-%d", trial, restarts))
	if !forking {
		return "the leader ended by itself before every forker was forking"
	}
	// Vary the phase of the fork loops the cancellation lands in.
	time.Sleep(time.Duration(trial%4) * 2 * time.Millisecond)
	swarm.cancel()
	attempt := pgAwaitRun(t, swarm.done, 15*time.Second)
	live, probeErr := pgLeaderGroupLive(swarm.pidFile)
	// From here on, any descendant of this trial still running outlived the call.
	if err := os.WriteFile(swarm.returned, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if attempt.err != nil && !errors.Is(attempt.err, context.Canceled) {
		t.Fatalf("trial %d: error = %v, want context.Canceled", trial, attempt.err)
	}
	pgRefuseCleanupFailure(t, trial, attempt.err)
	if probeErr != nil {
		t.Fatalf("trial %d: %v", trial, probeErr)
	}
	if live {
		t.Fatalf("trial %d: a member of the leader's process group was alive after runWithOutputLimit returned", trial)
	}
	if attempt.err == nil {
		return "the leader ended by itself before the cancellation"
	}
	reports, err := os.ReadDir(swarm.running)
	if err != nil {
		t.Fatal(err)
	}
	if stopped := pgSwarmForkers - len(reports); stopped > 0 {
		return fmt.Sprintf("%d of %d forkers had stopped by themselves before the kill", stopped, pgSwarmForkers)
	}
	return ""
}

// TestNormalExitKillsForkingBackgroundGroupUntilEmpty (GF-1) runs a leader
// that starts one redirected fork-storm forker in its group, waits until it
// is forking and exits 0. On macOS one group kill after the leader is reaped
// can miss a child being forked at that moment. Measured against a runner
// that kills once after a normal exit, 7, 9 and 10 of 40 trials left a
// survivor; one Go forker has at most one fork in flight, so each survivor
// marks a separate trial. At the lowest measured rate (7/40 = 17.5%) such a
// runner survives all 30 trials with probability 0.825^30 < 0.4% (0.85^30 <
// 0.8% even at 15%). The check, right after each return, that no member of
// the group is alive only adds detection.
func TestNormalExitKillsForkingBackgroundGroupUntilEmpty(t *testing.T) {
	pgRequireRegressionPlatform(t)
	const trials, observation = 30, 1500 * time.Millisecond
	root := t.TempDir()
	requireStormDescendantScript(t, root)
	markers := filepath.Join(root, "storm-survivors")
	if err := os.Mkdir(markers, 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for trial := range trials {
		running := filepath.Join(root, fmt.Sprintf("storm-running-%d", trial))
		returned := filepath.Join(root, fmt.Sprintf("storm-returned-%d", trial))
		pidFile := filepath.Join(root, fmt.Sprintf("leader-%d.pid", trial))
		forkerLog := filepath.Join(root, fmt.Sprintf("forker-%d.log", trial))
		config := pgWriteFakeConfig(t, filepath.Join(root, fmt.Sprintf("storm-%d.json", trial)),
			fakeConfig{Mode: "fork-storm", Running: running, Returned: returned, Markers: markers})
		arguments := []string{"/bin/sh", "-c", pgNormalExitLeaderScript, "sh", binary, config, running, pidFile, forkerLog}
		code, _, _, err := runWithOutputLimit(t.Context(), arguments, root, pgFakeEnv(), pgRunTimeout, pgRunLimit)
		live, probeErr := pgLeaderGroupLive(pidFile)
		// From here on, any descendant of this trial still running outlived the call.
		if writeErr := os.WriteFile(returned, nil, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil || code != 0 {
			t.Fatalf("trial %d: exit %d, error %v; want exit 0 and no error", trial, code, err)
		}
		if _, err := os.Stat(running); err != nil {
			t.Fatalf("trial %d: forker never reported running: %v", trial, err)
		}
		if probeErr != nil {
			t.Fatalf("trial %d: %v", trial, probeErr)
		}
		if live {
			t.Fatalf("trial %d: a member of the leader's process group was alive after runWithOutputLimit returned", trial)
		}
	}
	// A survivor reports within about 0.3 s of its trial's return marker.
	time.Sleep(observation)
	pgRequireNoMarkers(t, markers, "storm descendants outlived a normally exiting runWithOutputLimit")
}

// TestCancellationKillsNonLeaderForkersUntilEmpty (GF-2) cancels while 32
// forkers that are not the group leader (one shell leader runs "f & f & ...
// wait") fork continuously. Measured on macOS with forkers started this way,
// a runner that kills, joins the leader and kills again left 40 and 40
// survivors per 120 trials with 32 forkers (9 and 17 with 16 forkers); a
// runner that kills once left a survivor in 21-26 of 120 trials with a single
// forker. Survivors are processes, and one trial can leave several. If one
// affected trial leaves at most 4 survivors, 40/120 means at least 8.3% of
// trials are affected and kill-join-kill survives all 100 trials with
// probability 0.917^100 < 0.02%; even at the 16-forker rate 9/120 with at
// most 2 survivors per affected trial (3.75%), 0.9625^100 < 2.2%. The single
// kill fails a trial unless all 32 forkers escape (each about 18%): 0.82^32 <
// 0.2% per trial. This setup keeps the group at about 417 processes by
// giving each forker 12 short-lived descendants instead of 64 that live 0.2 s,
// so each forker still forks almost continuously; the rates above are from the
// measured setup and must be confirmed against mutant runners. The check,
// right after each return, that no member of the group is alive only adds
// detection. A trial counts only when the kill found all 32 forkers forking:
// when the leader ends by itself first, or a forker had stopped by itself
// and withdrawn its report, the trial starts again after the same checks, at
// most pgSwarmRestarts times in one run.
func TestCancellationKillsNonLeaderForkersUntilEmpty(t *testing.T) {
	pgRequireRegressionPlatform(t)
	const trials, observation = 100, 1500 * time.Millisecond
	root := t.TempDir()
	pgRequireSwarmDescendantScript(t, root)
	markers := filepath.Join(root, "swarm-survivors")
	if err := os.Mkdir(markers, 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	restarts := 0
	for trial := 0; trial < trials; {
		weak := pgSwarmTrial(t, root, binary, markers, trial, restarts)
		if weak == "" {
			trial++
			continue
		}
		restarts++
		if restarts > pgSwarmRestarts {
			t.Fatalf("trial %d: %d starts of this run measured nothing, the last because %s: this host cannot run the "+
				"swarm (too few free processes or too little CPU time)", trial, restarts, weak)
		}
		t.Logf("trial %d starts again: %s", trial, weak)
	}
	// A survivor reports within about 0.2 s of its trial's return marker.
	time.Sleep(observation)
	pgRequireNoMarkers(t, markers, "swarm descendants outlived a cancelled runWithOutputLimit")
}
