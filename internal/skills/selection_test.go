package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func selectionEntry() Entry {
	return Entry{ID: "codex:user:testing", Name: "testing", Scope: "user", Path: "/private/local/skills/testing/SKILL.md", SHA256: strings.Repeat("a", 64)}
}

func TestSkillSelectionsArePortableImmutableAndOffline(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "openspec", "changes", "task-1", "skills")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := selectionEntry()
	id, err := RecordSelection(root, "task-1", "full", "codex", "developer", []Entry{entry})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(entry.Path)) || !bytes.Contains(data, []byte(`"load_status": "unverified"`)) {
		t.Fatalf("receipt exposes local paths or claims loading: %s", data)
	}
	if repeated, err := RecordSelection(root, "task-1", "full", "codex", "developer", []Entry{entry}); err != nil || repeated != id {
		t.Fatalf("repeating a selection was not idempotent: %s; %v", repeated, err)
	}
	entry.SHA256 = strings.Repeat("b", 64)
	if changed, err := RecordSelection(root, "task-1", "full", "codex", "developer", []Entry{entry}); err != nil || changed == id {
		t.Fatalf("changed instructions lost their new identity: %s; %v", changed, err)
	}
	if before, err := os.ReadFile(name); err != nil || !bytes.Equal(before, data) {
		t.Fatalf("old receipt was modified: %s; %v", before, err)
	}
	if problems, err := ValidateSelections(root, "task-1"); err != nil || len(problems) != 0 {
		t.Fatalf("offline validation failed: %v; %v", problems, err)
	}
	if err := os.WriteFile(name, append(data, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if problems, err := ValidateSelections(root, "task-1"); err != nil || len(problems) == 0 {
		t.Fatalf("modified receipt was accepted: %v; %v", problems, err)
	}
	entry.SHA256 = strings.Repeat("a", 64)
	if _, err := RecordSelection(root, "task-1", "full", "codex", "developer", []Entry{entry}); err == nil {
		t.Fatal("overwrote a modified immutable receipt")
	}
}

func TestSkillHistoryPromotesWithoutLosingReceipts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	id, err := RecordSelection(root, "sample", "small", "codex", "reviewer", []Entry{selectionEntry()})
	if err != nil {
		t.Fatal(err)
	}
	if tasks, err := SmallTasks(root); err != nil || len(tasks) != 1 || tasks[0] != "sample" {
		t.Fatalf("small history missing: %v; %v", tasks, err)
	}
	full, small := selectionPaths("sample")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(full))), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSelection(root, "sample", "full", "codex", "developer", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(full), id+".json")); err != nil {
		t.Fatalf("promotion lost the original receipt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(small))); !os.IsNotExist(err) {
		t.Fatalf("promotion kept a competing small history: %v", err)
	}
	if problems, err := ValidateSelections(root, "sample"); err != nil || len(problems) != 0 {
		t.Fatalf("promoted history invalid: %v; %v", problems, err)
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(small)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSelection(root, "sample", "full", "codex", "developer", nil); err == nil {
		t.Fatal("accepted competing histories")
	}
}

func TestSkillReceiptsRejectUnsafeLocationsAndInvalidSelections(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, task := range []string{"../outside", "task/name", "task--name", "UPPER"} {
		if _, err := RecordSelection(root, task, "small", "codex", "developer", nil); err == nil {
			t.Errorf("accepted task %q", task)
		}
	}
	if _, err := RecordSelection(root, "absent", "full", "codex", "developer", nil); err == nil {
		t.Fatal("created an unrequested full task")
	}
	bad := selectionEntry()
	bad.Disabled = true
	if _, err := RecordSelection(root, "sample", "small", "codex", "developer", []Entry{bad}); err == nil {
		t.Fatal("recorded a disabled skill")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "openspec")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := RecordSelection(root, "sample", "small", "codex", "developer", nil); err == nil {
		t.Fatal("followed a linked record directory")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside repository: %v; %v", entries, err)
	}
}

func TestConcurrentSkillReceiptsKeepEverySelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var wg sync.WaitGroup
	for n := range 8 {
		wg.Go(func() {
			entry := selectionEntry()
			entry.SHA256 = fmt.Sprintf("%064x", n)
			if _, err := RecordSelection(root, "concurrent", "small", "codex", "developer", []Entry{entry}); err != nil {
				t.Errorf("selection %d: %v", n, err)
			}
		})
	}
	wg.Wait()
	_, small := selectionPaths("concurrent")
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(small)))
	if err != nil || len(entries) != 8 {
		t.Fatalf("lost concurrent selections: %v; %v", entries, err)
	}
	if problems, err := ValidateSelections(root, "concurrent"); err != nil || len(problems) != 0 {
		t.Fatalf("concurrent history invalid: %v; %v", problems, err)
	}
}

func TestSkillSelectionTemporaryEntriesRejectUnsafeTypes(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"regular", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if _, err := RecordSelection(root, "temporary", "small", "codex", "developer", nil); err != nil {
				t.Fatal(err)
			}
			_, small := selectionPaths("temporary")
			name := filepath.Join(root, filepath.FromSlash(small), ".tmp-0123456789abcdef")
			switch kind {
			case "regular":
				// A publication can be in progress while an offline check runs.
				if err := os.WriteFile(name, []byte("unfinished JSON"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(name, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(t.TempDir(), name); err != nil {
					t.Skipf("symbolic links unavailable: %v", err)
				}
			}
			problems, err := ValidateSelections(root, "temporary")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "regular" {
				if len(problems) != 0 {
					t.Fatalf("in-progress regular file was not ignored: %v", problems)
				}
			} else if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), ".tmp-0123456789abcdef") {
				t.Fatalf("unsafe temporary %s was not diagnosed: %v", kind, problems)
			}
		})
	}
}
