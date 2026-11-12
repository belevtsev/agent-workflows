package taskflow

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// Build hash-valid no-op history from a native task fixture. This exercises the
// real reader/writer limit without claiming thousands of actual task actions.
func extendTaskHistory(t *testing.T, record Record, revision int) Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision)))
	if err != nil {
		t.Fatal(err)
	}
	for record.Revision < revision {
		record.PreviousSHA256 = digest(data)
		record.Revision++
		data, err = encode(record)
		if err != nil {
			t.Fatal(err)
		}
		// Preserve hash chaining over original bytes, including historical JSON
		// whitespace rather than relying on the writer's current encoding.
		if record.Revision%2 == 0 {
			data = append(append([]byte(" \n"), data...), '\n')
		}
		if err := os.WriteFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(record.Directory)
	if err != nil || loaded.Revision != revision {
		t.Fatalf("invalid boundary fixture: revision=%d err=%v", loaded.Revision, err)
	}
	return loaded
}

func taskArtifactNames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestRevisionLimitPreservesReadableLastRevisionAndRefusesSideEffects(t *testing.T) {
	t.Parallel()
	plan := Plan{Title: "Read source", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "Read the complete source", AllowNonExecution: true}}}
	record, ref := fixtureTask(t, plan)
	initial, err := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	input := assessment(ref)
	input.Requirements[0].NonExecutionReason = "Read the complete source; no executable change."
	input.Decision.NonExecutionReason = input.Requirements[0].NonExecutionReason
	if _, err = Assess(t.Context(), record.Directory, input); err != nil {
		t.Fatal(err)
	}
	// A bound context (D5 "advisers") for the refused consultation below.
	beforeContext, err := PrepareContext(t.Context(), record.Directory, "before")
	if err != nil {
		t.Fatal(err)
	}
	if record, err = Load(record.Directory); err != nil {
		t.Fatal(err)
	}
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	if result, err := Verify(t.Context(), record.Directory); err != nil || !result.Complete {
		t.Fatalf("last readable revision was refused: complete=%t err=%v", result.Complete, err)
	}
	lastPath := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", maxTaskRevisions))
	last, err := os.ReadFile(lastPath)
	if err != nil {
		t.Fatal(err)
	}
	names := taskArtifactNames(t, record.Directory)
	if result, err := Verify(t.Context(), record.Directory); !errors.Is(err, ErrRevisionLimit) || result.Complete {
		t.Fatalf("exhausted verification claimed success: complete=%t err=%v", result.Complete, err)
	}
	called := false
	if _, err := update(t.Context(), Env{}, record.Directory, func(root *os.Root, _ *Record) error {
		called = true
		return writeNew(root, "unexpected-callback-artifact.json", []byte("{}"))
	}); !errors.Is(err, ErrRevisionLimit) || called {
		t.Fatalf("exhausted history invoked callback: called=%t err=%v", called, err)
	}
	consultation := writeConsultation(t, beforeContext, nil)
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", consultation); !errors.Is(err, ErrRevisionLimit) {
		t.Fatalf("consultation copied despite exhausted history: %v", err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || stored.Version != Version || stored.Plan.CoordinatorID != record.Plan.CoordinatorID || stored.Verification == nil || !stored.Verification.Complete {
		t.Fatalf("last native result was lost: revision=%d err=%v", stored.Revision, err)
	}
	currentInitial, initialErr := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
	currentLast, lastErr := os.ReadFile(lastPath)
	if initialErr != nil || lastErr != nil || !bytes.Equal(initial, currentInitial) || !bytes.Equal(last, currentLast) || !slices.Equal(names, taskArtifactNames(t, record.Directory)) {
		t.Fatal("capacity refusal changed history or left an orphan artifact")
	}
}

func TestCheckStartRequiresTerminalCapacityBeforeLogOrChild(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "must-not-execute")
	record, _ := fixtureTask(t, helperPlan(t, "small", "mutate", marker))
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	names := taskArtifactNames(t, record.Directory)
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	if !errors.Is(err, ErrRevisionLimit) || run.ID != "" || run.Log != "" {
		t.Fatalf("check started without terminal capacity: id=%q log=%q err=%v", run.ID, run.Log, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused check executed its child: %v", err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != record.Revision || len(stored.Checks) != 0 || !slices.Equal(names, taskArtifactNames(t, record.Directory)) {
		t.Fatalf("refused check changed artifacts or records: revision=%d err=%v", stored.Revision, err)
	}
}

func TestRevisionCapacitySerializesCompetingWriters(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error {
			close(entered)
			<-release
			record.EvidenceRevision++
			return nil
		})
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first writer failed before owning capacity: %v", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	// While the first writer holds the task lock, a competing writer waits for
	// it until its context ends, and never runs its change.
	called := false
	competing, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, secondErr := update(competing, Env{}, record.Directory, func(_ *os.Root, _ *Record) error { called = true; return nil })
	cancel()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(secondErr, context.DeadlineExceeded) || called {
		t.Fatalf("competing writer bypassed the lock: called=%t err=%v", called, secondErr)
	}
	if _, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, _ *Record) error { called = true; return nil }); !errors.Is(err, ErrRevisionLimit) || called {
		t.Fatalf("competing retry consumed exhausted capacity: called=%t err=%v", called, err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || stored.EvidenceRevision != 1 {
		t.Fatalf("serialized boundary history is unreadable: revision=%d err=%v", stored.Revision, err)
	}
}

// checkBarrierLifetime bounds a barrier test by the go test deadline, leaving a
// margin so a slow run fails with its own message before the test binary
// panics. Fixture setup can take tens of seconds under -race on a busy
// runner, so a fixed budget started before setup is not used.
func checkBarrierLifetime(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	const margin, fallback = 30 * time.Second, 4 * time.Minute
	deadline, ok := t.Deadline()
	if !ok || time.Until(deadline) > fallback+margin {
		deadline = time.Now().Add(fallback)
	} else {
		deadline = deadline.Add(-margin)
	}
	return context.WithDeadline(t.Context(), deadline)
}

func checkBarrier(t *testing.T, lifetime context.Context) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	stopClosing := context.AfterFunc(lifetime, func() { listener.Close() })
	t.Cleanup(func() {
		stopClosing()
		listener.Close()
	})
	return listener
}

// checkBarrierIODeadline bounds barrier I/O by the test's own lifetime only.
// A shorter fixed budget is a timing assumption: under -race on a loaded
// runner, RunCheck's locked revision write plus the instrumented child's startup can
// outlast any small constant before the child dials back.
func checkBarrierIODeadline(t *testing.T, lifetime context.Context) time.Time {
	t.Helper()
	deadline, ok := lifetime.Deadline()
	if !ok {
		t.Fatal("check barrier lifetime has no deadline")
	}
	return deadline
}

type acceptedBarrier struct {
	connection net.Conn
	err        error
}

// acceptCheckBarrier waits until check id's child reaches its execution
// barrier. If RunCheck returns first, it fails with that outcome at once, so a check
// that never spawns or loses its child cannot hide behind an accept timeout.
func acceptCheckBarrier(t *testing.T, lifetime context.Context, listener *net.TCPListener, id string, done <-chan checkOutcome) net.Conn {
	t.Helper()
	if err := listener.SetDeadline(checkBarrierIODeadline(t, lifetime)); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan acceptedBarrier, 1)
	go func() {
		connection, err := listener.Accept()
		accepted <- acceptedBarrier{connection: connection, err: err}
	}()
	var connection net.Conn
	select {
	case result := <-accepted:
		if result.err != nil {
			t.Fatal(result.err)
		}
		connection = result.connection
	case outcome := <-done:
		listener.Close()
		if result := <-accepted; result.connection != nil {
			result.connection.Close()
		}
		t.Fatalf("check %q finished before reaching its execution barrier: status=%q err=%v", id, outcome.run.Status, outcome.err)
	}
	stopClosing := context.AfterFunc(lifetime, func() { connection.Close() })
	t.Cleanup(func() {
		stopClosing()
		connection.Close()
	})
	if err := connection.SetReadDeadline(checkBarrierIODeadline(t, lifetime)); err != nil {
		t.Fatal(err)
	}
	ready := make([]byte, len(id))
	if _, err := io.ReadFull(connection, ready); err != nil || string(ready) != id {
		t.Fatalf("child did not reach its execution barrier: id=%q err=%v", ready, err)
	}
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	return connection
}

type checkOutcome struct {
	run CheckRun
	err error
}

func startBarrierCheck(t *testing.T, ctx context.Context, record Record, id string) <-chan checkOutcome {
	t.Helper()
	done := make(chan checkOutcome, 1)
	go func() {
		run, err := RunCheck(ctx, record.Directory, id)
		done <- checkOutcome{run: run, err: err}
	}()
	return done
}

func awaitCheckOutcome(t *testing.T, lifetime context.Context, done <-chan checkOutcome) checkOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-lifetime.Done():
		t.Fatal("owned check did not finish")
		return checkOutcome{}
	}
}

func TestRunningChecksRetainEveryTerminalSlotAtRevisionLimit(t *testing.T) {
	t.Parallel()
	lifetime, cancelLifetime := checkBarrierLifetime(t)
	defer cancelLifetime()
	deadline, _ := lifetime.Deadline()
	deadlineArg := deadline.Format(time.RFC3339Nano)
	listener := checkBarrier(t, lifetime)
	marker := filepath.Join(t.TempDir(), "third-check-must-not-run")
	plan := helperPlan(t, "small", "barrier", listener.Addr().String(), "first", deadlineArg)
	plan.Checks[0].ID, plan.Checks[0].TimeoutSeconds = "first", 120
	plan.Requirements[0].CheckIDs = []string{"first"}
	second, third := plan.Checks[0], plan.Checks[0]
	second.ID, third.ID = "second", "third"
	second.Argv = append(append([]string(nil), second.Argv[:3]...), "barrier", listener.Addr().String(), second.ID, deadlineArg)
	third.Argv = append(append([]string(nil), third.Argv[:3]...), "mutate", marker)
	plan.Checks = append(plan.Checks, second, third)
	record, _ := fixtureTask(t, plan)
	record = extendTaskHistory(t, record, maxTaskRevisions-4)
	firstDone := startBarrierCheck(t, lifetime, record, "first")
	first := acceptCheckBarrier(t, lifetime, listener, "first", firstDone)
	secondDone := startBarrierCheck(t, lifetime, record, "second")
	secondConnection := acceptCheckBarrier(t, lifetime, listener, "second", secondDone)
	refuseCompetingUpdates := func() {
		t.Helper()
		names := taskArtifactNames(t, record.Directory)
		called := false
		if _, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error { called = true; record.EvidenceRevision++; return nil }); !errors.Is(err, ErrRevisionLimit) || called {
			t.Fatalf("metadata writer spent a terminal slot: called=%t err=%v", called, err)
		}
		if run, err := RunCheck(lifetime, record.Directory, "third"); !errors.Is(err, ErrRevisionLimit) || run.ID != "" {
			t.Fatalf("new check spent another check's terminal slot: id=%q err=%v", run.ID, err)
		}
		if !slices.Equal(names, taskArtifactNames(t, record.Directory)) {
			t.Fatal("capacity refusal left an orphan artifact")
		}
	}
	refuseCompetingUpdates()
	if err := first.SetWriteDeadline(checkBarrierIODeadline(t, lifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitCheckOutcome(t, lifetime, firstDone); outcome.err != nil || outcome.run.Status != "passed" {
		t.Fatalf("first terminal reservation lost: status=%q err=%v", outcome.run.Status, outcome.err)
	}
	refuseCompetingUpdates()
	if err := secondConnection.SetWriteDeadline(checkBarrierIODeadline(t, lifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err := secondConnection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitCheckOutcome(t, lifetime, secondDone); outcome.err != nil || outcome.run.Status != "passed" {
		t.Fatalf("second terminal reservation lost: status=%q err=%v", outcome.run.Status, outcome.err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || len(stored.Checks) != 2 {
		t.Fatalf("final check history unreadable: revision=%d err=%v", stored.Revision, err)
	}
	for _, run := range stored.Checks {
		if run.Status != "passed" || run.FinishedAt == nil {
			t.Fatalf("terminal check was not durable: id=%q status=%q", run.ID, run.Status)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("third check child executed: %v", err)
	}
}

func TestCancellationSpendsReservedTerminalRevisionAtLimit(t *testing.T) {
	t.Parallel()
	lifetime, cancelLifetime := checkBarrierLifetime(t)
	defer cancelLifetime()
	deadline, _ := lifetime.Deadline()
	listener := checkBarrier(t, lifetime)
	plan := helperPlan(t, "small", "barrier", listener.Addr().String(), "focused", deadline.Format(time.RFC3339Nano))
	plan.Checks[0].TimeoutSeconds = 120
	record, _ := fixtureTask(t, plan)
	record = extendTaskHistory(t, record, maxTaskRevisions-2)
	ctx, cancel := context.WithCancel(lifetime)
	defer cancel()
	done := startBarrierCheck(t, ctx, record, "focused")
	acceptCheckBarrier(t, lifetime, listener, "focused", done)
	cancel()
	outcome := awaitCheckOutcome(t, lifetime, done)
	if !errors.Is(outcome.err, context.Canceled) || outcome.run.Status != "cancelled" {
		t.Fatalf("cancellation lost reserved result: status=%q err=%v", outcome.run.Status, outcome.err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || len(stored.Checks) != 1 || stored.Checks[0].Status != "cancelled" || stored.Checks[0].FinishedAt == nil {
		t.Fatalf("cancelled terminal revision not durable: revision=%d err=%v", stored.Revision, err)
	}
}

func TestTerminalReservationRequiresItsOwnerAndPreservesAbandonedLock(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	runID := strings.Repeat("a", 32)
	record, err := updateWithCapacity(t.Context(), Env{}, record.Directory, 2, "", func(_ *os.Root, record *Record) error {
		record.Checks = append(record.Checks, CheckRun{ID: runID, CheckID: "focused", Status: "running", Argv: record.Plan.Checks[0].Argv, StartedAt: time.Now().UTC(), Before: record.Baseline})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	called := false
	finish := func(_ *os.Root, record *Record) error {
		called = true
		now := time.Now().UTC()
		record.Checks[0].Status, record.Checks[0].FinishedAt = "cancelled", &now
		return nil
	}
	if _, err := updateWhenUnlocked(t.Context(), Env{}, record.Directory, strings.Repeat("b", 32), finish); err == nil || errors.Is(err, ErrRevisionLimit) || called {
		t.Fatalf("unowned terminal reservation was accepted: called=%t err=%v", called, err)
	}
	lock := filepath.Join(record.Directory, ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(lock) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := updateWhenUnlocked(ctx, Env{}, record.Directory, runID, finish); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("abandoned terminal lock claimed success: called=%t err=%v", called, err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("terminal writer removed an abandoned lock: %v", err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions-1 || stored.Checks[0].Status != "running" {
		t.Fatalf("abandoned lock hid prior running history: revision=%d err=%v", stored.Revision, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := updateWhenUnlocked(t.Context(), Env{}, record.Directory, runID, finish); err != nil || !called {
		t.Fatalf("owner lost its terminal reservation after unlock: called=%t err=%v", called, err)
	}
	stored, err = Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || stored.Checks[0].Status != "cancelled" || stored.Checks[0].FinishedAt == nil {
		t.Fatalf("terminal reservation was not persisted at limit: revision=%d err=%v", stored.Revision, err)
	}
}

// TestLockHolderProcess is the child process of the killed-holder tests, not a
// test of its own. With CW_TASKFLOW_LOCK_HOLD set to task or publish, it takes
// that lock on the task directory it runs in, prints "locked" and holds the
// lock until it is killed or its stdin closes. Without the variable it returns
// at once.
func TestLockHolderProcess(t *testing.T) {
	hold := func() {
		fmt.Println("locked")
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
	switch os.Getenv("CW_TASKFLOW_LOCK_HOLD") {
	case "task":
		_, _ = update(t.Context(), Env{}, ".", func(*os.Root, *Record) error {
			hold()
			return errors.New("the holder writes no revision")
		})
	case "publish":
		task, err := OpenPublicationTask(".")
		if err != nil {
			t.Fatal(err)
		}
		defer task.Close()
		if err := task.Lock(); err != nil {
			t.Fatal(err)
		}
		hold()
	}
}

// startLockHolder starts TestLockHolderProcess as a child that holds the kind
// of lock (task or publish) on directory, and returns once the child says it
// holds it.
func startLockHolder(t *testing.T, kind, directory string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestLockHolderProcess$")
	child.Env = append(os.Environ(), "CW_TASKFLOW_LOCK_HOLD="+kind)
	child.Dir = directory
	child.Stderr = os.Stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	if line, err := bufio.NewReader(stdout).ReadString('\n'); line != "locked\n" {
		t.Fatalf("the %s lock holder did not take its lock: %q %v", kind, line, err)
	}
	return child
}

// killLockHolder kills holder with SIGKILL, which gives it no chance to
// release anything, and reaps it.
func killLockHolder(t *testing.T, holder *exec.Cmd) {
	t.Helper()
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
}

// TestKilledTaskWriterLeavesTaskWritable: a writer killed with SIGKILL while
// it holds the task lock, before it writes its revision, leaves the task
// writable. Oracle: the next update succeeds within one second, and the chain
// loads with that update as revision 2.
func TestKilledTaskWriterLeavesTaskWritable(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	killLockHolder(t, startLockHolder(t, "task", record.Directory))
	started := time.Now()
	_, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error { record.EvidenceRevision++; return nil })
	if elapsed := time.Since(started); err != nil || elapsed >= time.Second {
		t.Fatalf("update after a killed writer: err=%v after %s, want success within 1s", err, elapsed)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != 2 || stored.EvidenceRevision != 1 {
		t.Fatalf("chain after a killed writer: revision=%d evidence=%d err=%v, want revision 2", stored.Revision, stored.EvidenceRevision, err)
	}
}

// TestConcurrentWritersAllAppendRevisions: writers of one task wait for the
// task lock instead of failing. Oracle: 20 concurrent updates all succeed, and
// the chain loads with 20 new revisions holding all 20 increments.
func TestConcurrentWritersAllAppendRevisions(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	const writers = 20
	start := make(chan struct{})
	results := make(chan error, writers)
	for range writers {
		go func() {
			<-start
			_, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error { record.EvidenceRevision++; return nil })
			results <- err
		}()
	}
	close(start)
	for range writers {
		if err := <-results; err != nil {
			t.Errorf("concurrent writer failed: %v", err)
		}
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != record.Revision+writers || stored.EvidenceRevision != writers {
		t.Fatalf("chain after %d writers: revision=%d evidence=%d err=%v", writers, stored.Revision, stored.EvidenceRevision, err)
	}
}

// TestWriterWaitsForLegacyLockDirectory: a .lock directory, which cw
// v3.0.0-rc.1 holds while it writes, makes a writer wait instead of failing.
// Oracle: while the directory stays, the writer holds .lockfile and has
// written nothing; once it is removed, the update succeeds as revision 2.
func TestWriterWaitsForLegacyLockDirectory(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	legacy := filepath.Join(record.Directory, ".lock")
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error { record.EvidenceRevision++; return nil })
		done <- err
	}()
	// The writer takes .lockfile before it waits for .lock, so .lockfile is
	// busy exactly while it waits.
	probe := time.NewTicker(time.Millisecond)
	defer probe.Stop()
	bound := time.After(20 * time.Second)
	for waiting := false; !waiting; {
		select {
		case err := <-done:
			t.Fatalf("writer did not wait for the .lock directory: %v", err)
		case <-bound:
			t.Fatal("writer never took .lockfile")
		case <-probe.C:
		}
		lock, err := fslock.Acquire(t.Context(), filepath.Join(record.Directory, ".lockfile"), fslock.Options{Wait: fslock.NoWait})
		if errors.Is(err, fslock.ErrBusy) {
			waiting = true
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		lock.Close()
	}
	if _, err := os.Lstat(filepath.Join(record.Directory, "record-000002.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("writer wrote while the .lock directory stayed: %v", err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("writer failed once the .lock directory was removed: %v", err)
	}
	if stored, err := Load(record.Directory); err != nil || stored.Revision != 2 {
		t.Fatalf("chain after the wait: revision=%d err=%v, want 2", stored.Revision, err)
	}
}

// TestLegacyLockDirectoryOutlastingTheWaitFailsLocked: a .lock directory that
// stays for the whole wait fails with ErrTaskLocked, whose message says to
// inspect the interrupted writer, and is never removed. Oracle: the error, the
// directory still in place, and .lockfile released for the next writer.
func TestLegacyLockDirectoryOutlastingTheWaitFailsLocked(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	legacy := filepath.Join(record.Directory, ".lock")
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if unlock, err := lockTask(t.Context(), root, 50*time.Millisecond); !errors.Is(err, ErrTaskLocked) || errors.Is(err, errTaskBusy) || unlock != nil {
		t.Fatalf("lock with a .lock directory that stays: err=%v, want ErrTaskLocked", err)
	}
	if info, err := os.Lstat(legacy); err != nil || !info.IsDir() {
		t.Fatalf("the .lock directory was removed or replaced: %v", err)
	}
	// A child that another test forks in this process shares the lock
	// descriptor until its exec closes it, so the probe tolerates a short
	// wait; a writer that kept the lock would hold it for good.
	lock, err := fslock.Acquire(t.Context(), filepath.Join(record.Directory, ".lockfile"), fslock.Options{Wait: time.Second})
	if err != nil {
		t.Fatalf("the refused writer kept .lockfile: %v", err)
	}
	lock.Close()
}

// TestWriterBehindALegacyLockDirectoryFailsLocked: when a .lock directory of
// cw v3.0.0-rc.1 exists, a writer whose wait for .lockfile ends fails with
// ErrTaskLocked, which names .lock, like the writer ahead of it that holds
// .lockfile while it waits for .lock. Oracle: ErrTaskLocked, not errTaskBusy.
func TestWriterBehindALegacyLockDirectoryFailsLocked(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	// The wait tolerates a forked child that still shares the descriptor
	// cw task start locked with, as in the test above.
	ahead, err := fslock.Acquire(t.Context(), filepath.Join(record.Directory, ".lockfile"), fslock.Options{Wait: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer ahead.Close()
	if err := os.Mkdir(filepath.Join(record.Directory, ".lock"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if unlock, err := lockTask(t.Context(), root, 50*time.Millisecond); !errors.Is(err, ErrTaskLocked) || errors.Is(err, errTaskBusy) || unlock != nil {
		t.Fatalf("writer behind a .lock directory: err=%v, want ErrTaskLocked", err)
	}
}

// TestCancelledWriterStopsWaitingForTheTaskLock: the wait for the task lock
// ends with the caller's context, so an interrupted cw command does not wait
// out the 10 s bound. Oracle: while another writer holds the lock,
// PrepareContext with a 100 ms context returns within 2 s with an error that
// matches context.DeadlineExceeded, and the holder's revision is the only one
// written.
func TestCancelledWriterStopsWaitingForTheTaskLock(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := update(t.Context(), Env{}, record.Directory, func(_ *os.Root, record *Record) error {
			close(entered)
			<-release
			record.EvidenceRevision++
			return nil
		})
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("holder failed before taking the lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := PrepareContext(ctx, record.Directory, "final")
	elapsed := time.Since(started)
	close(release)
	if holderErr := <-done; holderErr != nil {
		t.Fatal(holderErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) || elapsed >= 2*time.Second {
		t.Fatalf("PrepareContext behind a held lock: err=%v after %s, want context.DeadlineExceeded within 2s", err, elapsed)
	}
	if stored, err := Load(record.Directory); err != nil || stored.Revision != 2 {
		t.Fatalf("chain after the cancelled writer: revision=%d err=%v, want only the holder's revision", stored.Revision, err)
	}
}
