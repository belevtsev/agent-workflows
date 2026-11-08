//go:build darwin || linux

package usagelog_test

// Independent tests for contract metrics-history revision 3 (requirements R21
// and R22), written from requirements-r3.md, requirements-r2.md,
// requirements-r1.md and contract-usagelog-r2.json only. Helpers come from the
// revision 1 and 2 tests in the same package: cleanSession, appendHistory,
// writeHistory, readBytes, wantCounts and historyLines (history_test.go), and
// holdLock, observation, stillBlocked, startAppend, startRead, lockSeed,
// lockWaitBound, sparseHistory and fileTail (history_r2_test.go).
//
// The file is limited to darwin and linux: the R21 tests hold flock(2) with
// syscall.Flock and find the call's lock descriptor through /dev/fd or
// /proc/self/fd, and the R22 tests rely on sparse files.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

// ---------------------------------------------------------------------------
// R21: a call waiting for the lock must not proceed on a replaced lock file.

// lockDescriptors counts this process's open descriptors that refer to the
// file currently at path (same device and inode).
func lockDescriptors(t *testing.T, path string) int {
	t.Helper()
	var want syscall.Stat_t
	if err := syscall.Stat(path, &want); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		fd, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		var st syscall.Stat_t
		// A descriptor closed or reused between the listing and Fstat only
		// yields an error or a different inode; neither is counted.
		if syscall.Fstat(fd, &st) == nil && st.Dev == want.Dev && st.Ino == want.Ino {
			n++
		}
	}
	return n
}

// onlyTestsLock checks, after the test takes its lock and before it starts the
// call under test, that the test's descriptor is the only one of the lock file
// at path, so a second descriptor seen later can only be the call's.
func onlyTestsLock(t *testing.T, path, what string) {
	t.Helper()
	if n := lockDescriptors(t, path); n != 1 {
		t.Fatalf("precondition: %d descriptors of %s before %s started, want only the test's", n, path, what)
	}
}

// awaitLockOpened waits, bounded by lockWaitBound, until the call under test
// holds its own descriptor of the lock file at path besides the test's one, so
// the test knows the call reached the lock before it swaps the file. It never
// bounds the call's work: it only polls this process's descriptor table. The
// one-descriptor precondition is checked by onlyTestsLock before the call
// starts, since the call may open the lock file as soon as it starts.
func awaitLockOpened[T any](t *testing.T, path string, ch <-chan T, what string) {
	t.Helper()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(lockWaitBound)
	for lockDescriptors(t, path) < 2 {
		select {
		case v := <-ch:
			t.Fatalf("R17: %s returned before it opened %s while another descriptor held the lock: %+v", what, path, v)
		case <-deadline:
			t.Fatalf("precondition: %s opened no descriptor of %s within %s; cannot tell that it waits for the lock", what, path, lockWaitBound)
		case <-tick.C:
		}
	}
}

func r21Await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(lockWaitBound):
		t.Fatalf("R21: %s did not return within %s after the lock was released", what, lockWaitBound)
		panic("unreachable")
	}
}

// r21Swaps are the two ways the lock file stops being the one the waiting
// call opened: renamed aside with a new regular file created at the name, or
// only removed.
var r21Swaps = []struct {
	name string
	swap func(t *testing.T, lock string)
}{
	{"renamed aside and recreated", func(t *testing.T, lock string) {
		t.Helper()
		if err := os.Rename(lock, lock+".aside"); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}},
	{"removed", func(t *testing.T, lock string) {
		t.Helper()
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
	}},
}

// Fails when AppendHistory, having waited for LOCK_EX on H + ".lock", proceeds
// (returns nil) after that lock file was renamed aside and recreated, or
// removed, while it waited; when it reports that as context.Canceled; when it
// writes H; or when an AppendHistory started after the swap, with nobody
// holding the lock file, does not succeed normally.
func TestAppendHistoryFailsWhenWaitedLockFileIsReplaced(t *testing.T) {
	for _, s := range r21Swaps {
		t.Run(s.name, func(t *testing.T) {
			main := cleanSession(t, false) // 4 keyed requests
			seed := lockSeed(t)            // 2 records: old_1, old_2
			start := time.Now()
			appendHistory(t, writeHistory(t, seed), main)
			window := observation(time.Since(start))

			history := writeHistory(t, seed)
			lock := history + ".lock"
			release := holdLock(t, lock, syscall.LOCK_EX)
			onlyTestsLock(t, lock, "AppendHistory")
			ch := startAppend(context.Background(), history, main)
			awaitLockOpened(t, lock, ch, "AppendHistory")
			stillBlocked(t, ch, window, "AppendHistory")
			s.swap(t, lock)
			release()

			got := r21Await(t, ch, "AppendHistory waiting on a replaced lock file")
			if got.err == nil {
				t.Fatalf("R21: AppendHistory proceeded on a lock file no longer at %s: %+v", lock, got.result)
			}
			if errors.Is(got.err, context.Canceled) {
				t.Fatalf("R21: AppendHistory reported the replaced lock file as context.Canceled: %v", got.err)
			}
			if now := readBytes(t, history); !bytes.Equal(now, []byte(seed)) {
				t.Fatalf("R21: AppendHistory on a replaced lock file wrote H:\n%q", now)
			}

			// Control: a call after the swap, with nobody holding the lock file
			// at the name, succeeds normally: 4 new requests after the 2 seeds.
			result, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{ClaudeTranscript: main})
			if err != nil {
				t.Fatalf("R21: AppendHistory after the swap with no holder failed: %v", err)
			}
			wantCounts(t, "R21 append after the swap", result, 4, 4, 0, 0, 0, 0)
			now := readBytes(t, history)
			if !bytes.HasPrefix(now, []byte(seed)) || len(historyLines(t, history)) != 6 {
				t.Fatalf("R21: after the control append H is not the 2 seeds followed by 4 records:\n%q", now)
			}
		})
	}
}

// Fails when ReadHistory, having waited for its shared lock on H + ".lock",
// proceeds (returns nil) after that lock file was renamed aside and recreated,
// or removed, while it waited; when it reports that as context.Canceled; or
// when a ReadHistory started after the swap, with nobody holding the lock
// file, does not succeed normally.
func TestReadHistoryFailsWhenWaitedLockFileIsReplaced(t *testing.T) {
	for _, s := range r21Swaps {
		t.Run(s.name, func(t *testing.T) {
			seed := lockSeed(t)
			start := time.Now()
			readHistory(t, writeHistory(t, seed), usagelog.HistoryFilter{}, nil)
			window := observation(time.Since(start))

			history := writeHistory(t, seed)
			lock := history + ".lock"
			release := holdLock(t, lock, syscall.LOCK_EX)
			onlyTestsLock(t, lock, "ReadHistory")
			ch := startRead(context.Background(), history)
			awaitLockOpened(t, lock, ch, "ReadHistory")
			stillBlocked(t, ch, window, "ReadHistory")
			s.swap(t, lock)
			release()

			got := r21Await(t, ch, "ReadHistory waiting on a replaced lock file")
			if got.err == nil {
				t.Fatalf("R21: ReadHistory proceeded on a lock file no longer at %s: records %d", lock, got.report.Records)
			}
			if errors.Is(got.err, context.Canceled) {
				t.Fatalf("R21: ReadHistory reported the replaced lock file as context.Canceled: %v", got.err)
			}
			if now := readBytes(t, history); !bytes.Equal(now, []byte(seed)) {
				t.Fatalf("R21: ReadHistory changed H:\n%q", now)
			}

			// Control: old_1 (7) + old_2 (5) = 2 records, uncached 12.
			report, err := usagelog.ReadHistory(t.Context(), history, usagelog.HistoryFilter{}, nil)
			if err != nil {
				t.Fatalf("R21: ReadHistory after the swap with no holder failed: %v", err)
			}
			if report.Records != 2 || report.Totals.UncachedInputTokens != 12 {
				t.Fatalf("R21: ReadHistory after the swap: records %d uncached %d, want 2 12",
					report.Records, report.Totals.UncachedInputTokens)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R22: an existing history over MaxHistoryBytes refuses even a no-op re-import.

func fileHead(t *testing.T, path string, n int) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	return buf
}

// Fails when AppendHistory succeeds on a history larger than MaxHistoryBytes
// whose stored records already hold every request of the transcript (a no-op
// re-import), or when such a call changes the file's size, first bytes or
// stored records.
func TestAppendHistoryOverBoundRefusesNoOpReimport(t *testing.T) {
	const bound = int64(usagelog.MaxHistoryBytes)
	main := cleanSession(t, false) // 4 keyed requests
	first := filepath.Join(t.TempDir(), "history.jsonl")
	wantCounts(t, "R22 first append", appendHistory(t, first, main), 4, 4, 0, 0, 0, 0)
	// "\n" ends the NUL-filled prefix line; then the 4 newline-terminated
	// records of this transcript.
	stored := append([]byte("\n"), readBytes(t, first)...)

	// Control: the same layout in a small file (a 64-byte NUL line, then the
	// records) is a no-op re-import that succeeds, so the refusal below is the
	// size bound and not an unrecognised fixture.
	small := writeHistory(t, strings.Repeat("\x00", 64)+string(stored))
	control := appendHistory(t, small, main)
	wantCounts(t, "R22 control no-op re-import", control, 4, 0, 4, 0, 0, 0)
	if control.HistoryDamagedLines != 1 {
		t.Fatalf("R22 control: history_damaged_lines %d, want 1 (the NUL line)", control.HistoryDamagedLines)
	}
	if now := readBytes(t, small); !bytes.Equal(now, append([]byte(strings.Repeat("\x00", 64)), stored...)) {
		t.Fatalf("R22 control: a no-op re-import changed the small history:\n%q", now)
	}

	for _, over := range []int64{1, 1 << 20} {
		t.Run(fmt.Sprintf("MaxHistoryBytes+%d", over), func(t *testing.T) {
			history := sparseHistory(t, bound+over, stored)
			tailN := int64(len(stored)) + 4096
			sizeBefore, tailBefore := fileTail(t, history, tailN)
			if sizeBefore != bound+over || !bytes.HasSuffix(tailBefore, stored) {
				t.Fatalf("fixture: size %d, want %d ending with the stored records", sizeBefore, bound+over)
			}
			headBefore := fileHead(t, history, 4096)

			result, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{ClaudeTranscript: main})
			if err == nil {
				t.Fatalf("R22: a no-op re-import of a history of MaxHistoryBytes+%d bytes succeeded: %+v", over, result)
			}
			sizeAfter, tailAfter := fileTail(t, history, tailN)
			if sizeAfter != sizeBefore {
				t.Fatalf("R22: the refused re-import changed the size %d -> %d", sizeBefore, sizeAfter)
			}
			if !bytes.Equal(tailAfter, tailBefore) {
				t.Fatalf("R22: the refused re-import changed the stored records at the tail")
			}
			if !bytes.Equal(fileHead(t, history, 4096), headBefore) {
				t.Fatalf("R22: the refused re-import changed the first bytes of the history")
			}
		})
	}
}
