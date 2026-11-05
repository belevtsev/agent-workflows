package taskflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	plan := Plan{Title: "Read source", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "Read the complete source", AllowNonExecution: true}}}
	record, ref := fixtureTask(t, plan)
	initial, err := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	input := assessment(ref)
	input.Requirements[0].NonExecutionReason = "Read the complete source; no executable change."
	input.Decision.NonExecutionReason = input.Requirements[0].NonExecutionReason
	record, err = Assess(t.Context(), record.Directory, input)
	if err != nil {
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
	if result, err := Verify(t.Context(), record.Directory); !errors.Is(err, errTaskRevisionLimit) || result.Complete {
		t.Fatalf("exhausted verification claimed success: complete=%t err=%v", result.Complete, err)
	}
	called := false
	if _, err := update(record.Directory, func(root *os.Root, _ *Record) error {
		called = true
		return writeNew(root, "unexpected-callback-artifact.json", []byte("{}"))
	}); !errors.Is(err, errTaskRevisionLimit) || called {
		t.Fatalf("exhausted history invoked callback: called=%t err=%v", called, err)
	}
	consultation := writeConsultation(t, record.Contexts["before"], nil)
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", consultation); !errors.Is(err, errTaskRevisionLimit) {
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
	marker := filepath.Join(t.TempDir(), "must-not-execute")
	record, _ := fixtureTask(t, helperPlan(t, "small", "mutate", marker))
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	names := taskArtifactNames(t, record.Directory)
	run, err := Run(t.Context(), record.Directory, "focused")
	if !errors.Is(err, errTaskRevisionLimit) || run.ID != "" || run.Log != "" {
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
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = extendTaskHistory(t, record, maxTaskRevisions-1)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := update(record.Directory, func(_ *os.Root, record *Record) error {
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
	called := false
	_, secondErr := update(record.Directory, func(_ *os.Root, _ *Record) error { called = true; return nil })
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(secondErr, errTaskLocked) || called {
		t.Fatalf("competing writer bypassed the lock: called=%t err=%v", called, secondErr)
	}
	if _, err := update(record.Directory, func(_ *os.Root, _ *Record) error { called = true; return nil }); !errors.Is(err, errTaskRevisionLimit) || called {
		t.Fatalf("competing retry consumed exhausted capacity: called=%t err=%v", called, err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || stored.EvidenceRevision != 1 {
		t.Fatalf("serialized boundary history is unreadable: revision=%d err=%v", stored.Revision, err)
	}
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

func checkBarrierIODeadline(lifetime context.Context) time.Time {
	ioDeadline := time.Now().Add(15 * time.Second)
	if deadline, ok := lifetime.Deadline(); ok && deadline.Before(ioDeadline) {
		return deadline
	}
	return ioDeadline
}

func acceptCheckBarrier(t *testing.T, lifetime context.Context, listener *net.TCPListener, id string) net.Conn {
	t.Helper()
	if err := listener.SetDeadline(checkBarrierIODeadline(lifetime)); err != nil {
		t.Fatal(err)
	}
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	stopClosing := context.AfterFunc(lifetime, func() { connection.Close() })
	t.Cleanup(func() {
		stopClosing()
		connection.Close()
	})
	if err := connection.SetReadDeadline(checkBarrierIODeadline(lifetime)); err != nil {
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
		run, err := Run(ctx, record.Directory, id)
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
	lifetime, cancelLifetime := context.WithTimeout(t.Context(), 2*time.Minute)
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
	first := acceptCheckBarrier(t, lifetime, listener, "first")
	secondDone := startBarrierCheck(t, lifetime, record, "second")
	secondConnection := acceptCheckBarrier(t, lifetime, listener, "second")
	refuseCompetingUpdates := func() {
		t.Helper()
		names := taskArtifactNames(t, record.Directory)
		called := false
		if _, err := update(record.Directory, func(_ *os.Root, record *Record) error { called = true; record.EvidenceRevision++; return nil }); !errors.Is(err, errTaskRevisionLimit) || called {
			t.Fatalf("metadata writer spent a terminal slot: called=%t err=%v", called, err)
		}
		if run, err := Run(lifetime, record.Directory, "third"); !errors.Is(err, errTaskRevisionLimit) || run.ID != "" {
			t.Fatalf("new check spent another check's terminal slot: id=%q err=%v", run.ID, err)
		}
		if !slices.Equal(names, taskArtifactNames(t, record.Directory)) {
			t.Fatal("capacity refusal left an orphan artifact")
		}
	}
	refuseCompetingUpdates()
	if err := first.SetWriteDeadline(checkBarrierIODeadline(lifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitCheckOutcome(t, lifetime, firstDone); outcome.err != nil || outcome.run.Status != "passed" {
		t.Fatalf("first terminal reservation lost: status=%q err=%v", outcome.run.Status, outcome.err)
	}
	refuseCompetingUpdates()
	if err := secondConnection.SetWriteDeadline(checkBarrierIODeadline(lifetime)); err != nil {
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
	lifetime, cancelLifetime := context.WithTimeout(t.Context(), 2*time.Minute)
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
	acceptCheckBarrier(t, lifetime, listener, "focused")
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
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	runID := strings.Repeat("a", 32)
	record, err := updateWithCapacity(record.Directory, 2, "", func(_ *os.Root, record *Record) error {
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
	if _, err := updateWhenUnlocked(t.Context(), record.Directory, strings.Repeat("b", 32), finish); err == nil || errors.Is(err, errTaskRevisionLimit) || called {
		t.Fatalf("unowned terminal reservation was accepted: called=%t err=%v", called, err)
	}
	lock := filepath.Join(record.Directory, ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(lock) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := updateWhenUnlocked(ctx, record.Directory, runID, finish); !errors.Is(err, context.Canceled) || called {
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
	if _, err := updateWhenUnlocked(t.Context(), record.Directory, runID, finish); err != nil || !called {
		t.Fatalf("owner lost its terminal reservation after unlock: called=%t err=%v", called, err)
	}
	stored, err = Load(record.Directory)
	if err != nil || stored.Revision != maxTaskRevisions || stored.Checks[0].Status != "cancelled" || stored.Checks[0].FinishedAt == nil {
		t.Fatalf("terminal reservation was not persisted at limit: revision=%d err=%v", stored.Revision, err)
	}
}
