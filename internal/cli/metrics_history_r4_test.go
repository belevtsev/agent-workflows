package cli

// End-to-end tests for contract metrics-history revision 4 (R23) through
// Execute, written from requirements-r4.md and requirements-r1.md only. The
// transcript fixture is writeClaudeSession (metrics_import_test.go): six
// distinct keyed requests in session s1, none of which is in any history
// written below, so an append that ran would always change the history.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mh4Entries lists every path under root (relative, sorted), so a lock file,
// temporary file or directory created next to the history is observable.
// A missing root yields nil.
func mh4Entries(t *testing.T, root string) []string {
	t.Helper()
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	var entries []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(entries)
	return entries
}

// mh4Rejected asserts the rejection of --dry-run, which no metrics command
// has: exit 2, an unknown-flag error naming it, and nothing on stdout.
func mh4Rejected(t *testing.T, stdout string, err error) {
	t.Helper()
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("R23: --dry-run: err %v exit %d, want a usage error (exit 2)", err, ExitCode(err))
	}
	if !strings.Contains(err.Error(), "unknown flag: --dry-run") {
		t.Fatalf("R23: error %q does not mention --dry-run", err.Error())
	}
	if stdout != "" {
		t.Fatalf("R23: stdout %q, want empty (no AppendResult)", stdout)
	}
}

// mh4ExistingLines are two valid R1 records of other sessions.
func mh4ExistingLines(t *testing.T) []string {
	return []string{
		mhRecordLine(t, "s0", "req_old", opusModel, "2026-11-05T10:00:00Z", 7, 3),
		mhRecordLine(t, "s9", "req_older", sonnetModel, "2026-11-04T09:00:00Z", 5, 4),
	}
}

// --dry-run metrics import --append on a fresh state is a usage error that
// creates nothing. Fails when the flag is ignored (exit 0, an AppendResult, a
// history written), when the rejection is a runtime error (exit 1) or is not
// the unknown flag --dry-run, or when the history, its lock or the metrics directory is
// created before (or despite) the rejection.
func TestMetricsHistoryDryRunAppendRejectedCreatesNothing(t *testing.T) {
	_, main := writeClaudeSession(t)
	t.Run("default history under --state-dir", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(t.TempDir(), "state")
		history := filepath.Join(state, "metrics", "history.jsonl")
		stdout, _, err := env.run(t, "--state-dir", state, "metrics", "import", "--dry-run", "--claude-transcript", main, "--append")
		mh4Rejected(t, stdout, err)
		mhAbsent(t, history, "R23 history")
		mhAbsent(t, history+".lock", "R23 history lock")
		mhAbsent(t, filepath.Join(state, "metrics"), "R23 <state>/metrics")
	})
	t.Run("default history under the home state directory", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(env.home, ".local", "state", "agent-workflows")
		stdout, _, err := env.run(t, "metrics", "import", "--dry-run", "--claude-transcript", main, "--append")
		mh4Rejected(t, stdout, err)
		mhAbsent(t, filepath.Join(state, "metrics", "history.jsonl"), "R23 history")
		mhAbsent(t, filepath.Join(state, "metrics", "history.jsonl.lock"), "R23 history lock")
		mhAbsent(t, filepath.Join(state, "metrics"), "R23 <state>/metrics")
	})
	t.Run("--history H", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(t.TempDir(), "state")
		dir := t.TempDir()
		history := filepath.Join(dir, "costs", "h.jsonl")
		stdout, _, err := env.run(t, "--state-dir", state, "metrics", "import", "--dry-run", "--claude-transcript", main, "--append", "--history", history)
		mh4Rejected(t, stdout, err)
		mhAbsent(t, history, "R23 history")
		mhAbsent(t, history+".lock", "R23 history lock")
		mhAbsent(t, filepath.Join(state, "metrics"), "R23 <state>/metrics")
		if got := mh4Entries(t, dir); !slices.Equal(got, []string{"."}) {
			t.Fatalf("R23: %s holds %v after a rejected append, want nothing created", dir, got)
		}
	})
}

// R23: --dry-run metrics import --append leaves an existing history
// byte-identical. Fails when the rejected command still appends the six new
// requests, rewrites or truncates the history, or leaves a lock or temporary
// file next to it.
func TestMetricsHistoryDryRunAppendLeavesExistingHistoryUnchanged(t *testing.T) {
	_, main := writeClaudeSession(t)
	t.Run("default history under --state-dir", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(t.TempDir(), "state")
		history := filepath.Join(state, "metrics", "history.jsonl")
		writeLines(t, history, mh4ExistingLines(t)...)
		before, entries := mhRead(t, history), mh4Entries(t, state)
		stdout, _, err := env.run(t, "--state-dir", state, "metrics", "import", "--dry-run", "--claude-transcript", main, "--append")
		mh4Rejected(t, stdout, err)
		if after := mhRead(t, history); !bytes.Equal(after, before) {
			t.Fatalf("R23: history changed by a rejected append:\nbefore %q\nafter  %q", before, after)
		}
		mhAbsent(t, history+".lock", "R23 history lock")
		if got := mh4Entries(t, state); !slices.Equal(got, entries) {
			t.Fatalf("R23: state directory %v after a rejected append, want %v", got, entries)
		}
	})
	t.Run("--history H", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(t.TempDir(), "state")
		history := mhWriteHistory(t, mh4ExistingLines(t)...)
		dir := filepath.Dir(history)
		before, entries := mhRead(t, history), mh4Entries(t, dir)
		stdout, _, err := env.run(t, "--state-dir", state, "metrics", "import", "--dry-run", "--claude-transcript", main, "--append", "--history", history)
		mh4Rejected(t, stdout, err)
		if after := mhRead(t, history); !bytes.Equal(after, before) {
			t.Fatalf("R23: history changed by a rejected append:\nbefore %q\nafter  %q", before, after)
		}
		mhAbsent(t, history+".lock", "R23 history lock")
		mhAbsent(t, filepath.Join(state, "metrics"), "R23 <state>/metrics")
		if got := mh4Entries(t, dir); !slices.Equal(got, entries) {
			t.Fatalf("R23: %s holds %v after a rejected append, want %v", dir, got, entries)
		}
	})
}

// --dry-run is not a metrics flag: metrics import without --append and
// metrics history reject it as an unknown flag (exit 2), print nothing and
// write nothing. Fails while a global --dry-run is accepted and ignored.
func TestMetricsImportAndHistoryRejectDryRun(t *testing.T) {
	_, main := writeClaudeSession(t)
	t.Run("import without --append", func(t *testing.T) {
		env := newMHEnv(t)
		state := filepath.Join(t.TempDir(), "state")
		stdout, _, err := env.run(t, "--state-dir", state, "metrics", "import", "--dry-run", "--claude-transcript", main)
		mh4Rejected(t, stdout, err)
		mhAbsent(t, filepath.Join(state, "metrics"), "<state>/metrics")
	})
	t.Run("history --history H", func(t *testing.T) {
		env := newMHEnv(t)
		history := mhWriteHistory(t, mh4ExistingLines(t)...)
		before := mhRead(t, history)
		stdout, _, err := env.run(t, "metrics", "history", "--dry-run", "--history", history)
		mh4Rejected(t, stdout, err)
		if after := mhRead(t, history); !bytes.Equal(after, before) {
			t.Fatalf("metrics history changed the history:\nbefore %q\nafter  %q", before, after)
		}
	})
}
