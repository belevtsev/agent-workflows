package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func diagnosticSession(t *testing.T) *session {
	t.Helper()
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	if err := os.Mkdir(git, 0o700); err != nil {
		t.Fatal(err)
	}
	return &session{repo: &gitx.Repo{Root: dir, GitDir: git, CommonDir: git}, store: store.Open(git)}
}

func diagnosticWrite(t *testing.T, name string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func diagnosticTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if info.Mode().IsRegular() {
			data, err = os.ReadFile(name)
			if err != nil {
				return err
			}
		}
		result[name] = fmt.Sprintf("%s/%s/%x", info.Mode(), info.ModTime(), sha256.Sum256(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestUsageDiagnosticsRepeatedReadsCreateNoLedgerLocksChecksOrHostWork(t *testing.T) {
	t.Parallel()
	s := diagnosticSession(t)
	before := diagnosticTree(t, s.repo.Root)
	for range 2 {
		got, err := collectUsageDiagnostics(context.Background(), s, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.CapturedTokens.Status != "unknown" || got.ManagedIntervals.Complete || got.Checks.Runs != 0 {
			t.Fatalf("missing evidence became measured: %+v", got)
		}
	}
	if after := diagnosticTree(t, s.repo.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostic read mutated the clone")
	}
	start := tokens.Start{Version: 1, Host: tokens.Codex, Session: "one", Agent: "agent-1", Task: "one-task", Stage: "code", Round: 1, Known: true, Models: map[string]tokens.Counts{"model": {Input: 10, CacheRead: 20, Output: 5}}}
	diagnosticWrite(t, filepath.Join(s.store.Dir, "tokens", "one", "agent-1.json"), start)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := when.Add(time.Second)
	record := store.Record{RunID: "run", ID: "check", ContentKey: "key", Run: []string{"must-not-run"}, StartedAt: when, EndedAt: &end, Outcome: store.SourceChanged}
	diagnosticWrite(t, filepath.Join(s.store.Dir, "runs", "check", "key", "run.json"), record)
	before = diagnosticTree(t, s.repo.Root)
	for range 2 {
		got, err := collectUsageDiagnostics(context.Background(), s, "one-task")
		if err != nil {
			t.Fatal(err)
		}
		if got.CapturedTokens.Known != 35 || got.CapturedTokens.Complete || got.Checks.Runs != 0 {
			t.Fatalf("stored accounting changed or unassociated run included: %+v", got)
		}
	}
	if after := diagnosticTree(t, s.repo.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("reading existing records mutated the clone")
	}
}

func TestUsageDiagnosticsReadsOriginalStageReceiptsWithoutMutation(t *testing.T) {
	t.Parallel()
	s := diagnosticSession(t)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := stageRecord{Version: 1, Worktree: s.repo.Root, Created: when, Prepared: preparedWorkerBrief{Brief: workerBrief{Task: "one-task", Policy: "policy"}}}
	id := contextpack.Key(record)
	diagnosticWrite(t, filepath.Join(s.repo.GitDir, "kitsu-stages", id+".json"), record)
	receipt := stageReceipt{Version: 1, DispatchID: id, Task: "one-task", Status: "finished", Created: when.Add(3 * time.Second), Policy: "policy", Writes: true, ExpectChecks: "any", Scope: stageReceiptScope, Checks: []stageCheck{{ID: "check", RunID: "run", State: "satisfied"}}}
	receipt.ID = contextpack.Key(receipt)
	diagnosticWrite(t, filepath.Join(s.repo.GitDir, "kitsu-stages", id+".finish.json"), receipt)
	run := store.Record{ID: "check", RunID: "run", ContentKey: "key", Run: []string{"must-not-run"}, StartedAt: when, EndedAt: &receipt.Created, Outcome: store.Passed}
	diagnosticWrite(t, filepath.Join(s.store.Dir, "runs", "check", "key", "run.json"), run)
	before := diagnosticTree(t, s.repo.Root)
	for range 2 {
		input, err := collectUsageDiagnosticInput(context.Background(), s, "one-task")
		if err != nil {
			t.Fatal(err)
		}
		if len(diagnosticRunRecords(input)) != 1 {
			t.Fatal("original receipt reference lost")
		}
		got, err := collectUsageDiagnostics(context.Background(), s, "one-task")
		if err != nil {
			t.Fatal(err)
		}
		if got.ManagedIntervals.ObservedSeconds != 3 || !got.ManagedIntervals.Complete || got.Checks.AssociatedRuns != 1 {
			t.Fatalf("incorrect stored projection: %+v", got)
		}
	}
	if after := diagnosticTree(t, s.repo.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("receipt read mutated ledger")
	}
	// Existing recovery preserves the original finish receipt while explicitly
	// abandoning its stale active pointer after interrupted cleanup.
	abandoned := stageReceipt{Version: 1, DispatchID: id, Task: "one-task", Status: "abandoned", Created: receipt.Created.Add(time.Hour), Policy: "policy", Scope: stageReceiptScope, Reason: "Recover interrupted cleanup"}
	abandoned.ID = contextpack.Key(abandoned)
	diagnosticWrite(t, filepath.Join(s.repo.GitDir, "kitsu-stages", id+".abandon.json"), abandoned)
	got, err := collectUsageDiagnostics(context.Background(), s, "one-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.ManagedIntervals.ObservedSeconds != 3 || got.ManagedIntervals.Abandoned != 1 || got.ManagedIntervals.Closed != 1 || got.Checks.AssociatedRuns != 1 || len(got.ManagedIntervals.Sources) != 3 {
		t.Fatalf("cleanup erased or extended original interval: %+v", got)
	}
	// A rehashed but semantically incomplete receipt still cannot invent an end.
	receipt.Created = time.Time{}
	receipt.ID = ""
	receipt.ID = contextpack.Key(receipt)
	diagnosticWrite(t, filepath.Join(s.repo.GitDir, "kitsu-stages", id+".finish.json"), receipt)
	if _, err := collectUsageDiagnostics(context.Background(), s, "one-task"); err == nil {
		t.Fatal("incomplete stage receipt was accepted")
	}
}

func TestUsageDiagnosticsRefusesUnsafeAndDamagedRecords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *session)
	}{
		{"symlink ledger", func(t *testing.T, s *session) {
			if err := os.Symlink(t.TempDir(), filepath.Join(s.repo.GitDir, "kitsu-stages")); err != nil {
				t.Skip(err)
			}
		}},
		{"symlink run directory", func(t *testing.T, s *session) {
			dir := filepath.Join(s.store.Dir, "runs")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "check")); err != nil {
				t.Skip(err)
			}
		}},
		{"damaged stage receipt", func(t *testing.T, s *session) {
			diagnosticWrite(t, filepath.Join(s.repo.GitDir, "kitsu-stages", strings.Repeat("a", 64)+".finish.json"), map[string]any{"version": 1})
		}},
		{"damaged check", func(t *testing.T, s *session) {
			diagnosticWrite(t, filepath.Join(s.store.Dir, "runs", "check", "key", "run.json"), map[string]any{"id": "check"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := diagnosticSession(t)
			tc.setup(t, s)
			if _, err := collectUsageDiagnostics(context.Background(), s, ""); err == nil {
				t.Fatal("unsafe or damaged evidence reported as zero")
			}
		})
	}
	// A malformed usage file is an explicit unknown sample and prevents a
	// complete capture; diagnostics never reopens the host transcript.
	s := diagnosticSession(t)
	file := filepath.Join(s.store.Dir, "tokens", "one", "agent.json")
	diagnosticWrite(t, file, map[string]any{"version": 0})
	got, err := collectUsageDiagnostics(context.Background(), s, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.DamagedRecords != 1 || got.CapturedTokens.Status != "unknown" || len(got.UnknownSources) != 1 || got.UnknownSources[0] != file {
		t.Fatalf("damaged usage hidden: %+v", got)
	}
}
