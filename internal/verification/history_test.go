package verification_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func historyFixture() verification.Attempt {
	exit := 0
	return verification.Attempt{
		Version: 1, Task: "small-task", Workflow: "small",
		BaseTree: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		PolicyKey: strings.Repeat("c", 64), SkillsKey: strings.Repeat("d", 64),
		Selections: []verification.SelectionRef{{ID: strings.Repeat("e", 64), Host: "codex", Role: "developer"}},
		Results: []verification.Result{{
			ID: "reject-invalid", Expect: "reject invalid input", State: "passed", ExitStatus: &exit,
			RunID: "20261117T123456Z-0123abcd", ContentKey: strings.Repeat("f", 64),
			Tree: strings.Repeat("b", 40), LogSHA256: strings.Repeat("0", 64),
		}},
	}
}

func historyDirectory(root, workflow string) string {
	if workflow == "full" {
		return filepath.Join(root, "openspec/changes/small-task/verification")
	}
	return filepath.Join(root, "openspec/changes/.verification/small-task")
}

func historyWrite(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func historyPublish(t *testing.T, root string, a verification.Attempt) verification.History {
	t.Helper()
	h, err := verification.Publish(root, a.Task, a.Workflow, a)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHistoryPublishPreservesAttemptsAndIdempotence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := historyFixture()
	first := historyPublish(t, root, a)
	dir := historyDirectory(root, "small")
	oldTime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	files := []string{filepath.Join(dir, "binding.json"), filepath.Join(dir, "attempts", first.ID+".json")}
	for _, file := range files {
		if err := os.Chtimes(file, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	again := historyPublish(t, root, a)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("identical attempt changed its identity or content")
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || !info.ModTime().Equal(oldTime) {
			t.Fatalf("identical publication rewrote %s: %v", file, err)
		}
	}
	a.BaseTree = strings.Repeat("1", 64) // Explicit base adoption is a new attempt.
	a.SourceTree = strings.Repeat("2", 64)
	second := historyPublish(t, root, a)
	if second.ID == first.ID {
		t.Fatal("changed base retained the old attempt id")
	}
	loaded, err := verification.Load(root, a.Task)
	if err != nil || !reflect.DeepEqual(loaded, second) {
		t.Fatalf("load = %#v, %v; want current attempt", loaded, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "attempts"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("retained attempts = %v, %v", entries, err)
	}
	problems, err := verification.ValidateRecords(root, a.Task)
	if err != nil || len(problems) != 0 {
		t.Fatalf("offline validation = %v, %v", problems, err)
	}
	tasks, err := verification.Tasks(root)
	if err != nil || !slices.Equal(tasks, []string{a.Task}) {
		t.Fatalf("tasks = %v, %v", tasks, err)
	}
}

func TestHistoryPromotionPreservesCompleteHistory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := historyFixture()
	first := historyPublish(t, root, a)
	a.SourceTree = strings.Repeat("3", 40)
	second := historyPublish(t, root, a)
	if err := os.MkdirAll(filepath.Join(root, "openspec/changes", a.Task), 0o755); err != nil {
		t.Fatal(err)
	}
	a.Workflow = "full"
	full := historyPublish(t, root, a)
	if _, err := os.Lstat(historyDirectory(root, "small")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("small history remains: %v", err)
	}
	for _, id := range []string{first.ID, second.ID, full.ID} {
		if _, err := os.Stat(filepath.Join(historyDirectory(root, "full"), "attempts", id+".json")); err != nil {
			t.Fatalf("promotion lost attempt %s: %v", id, err)
		}
	}
	loaded, err := verification.Load(root, a.Task)
	if err != nil || loaded.ID != full.ID {
		t.Fatalf("promoted binding = %#v, %v", loaded, err)
	}
	a.Workflow = "small"
	if _, err := verification.Publish(root, a.Task, a.Workflow, a); err == nil {
		t.Fatal("allowed full-to-small demotion")
	}
}

func TestHistoryPromotionConflictsAndMissingTask(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing-full-task", "competing-full-history", "corrupt-small-history"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			first := historyPublish(t, root, a)
			if name != "missing-full-task" {
				if err := os.MkdirAll(filepath.Join(root, "openspec/changes", a.Task), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if name == "competing-full-history" {
				if err := os.MkdirAll(historyDirectory(root, "full"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if name == "corrupt-small-history" {
				historyWrite(t, filepath.Join(historyDirectory(root, "small"), "binding.json"), []byte("{}\n"))
			}
			a.Workflow = "full"
			if _, err := verification.Publish(root, a.Task, a.Workflow, a); err == nil {
				t.Fatal("unsafe promotion succeeded")
			}
			if _, err := os.Stat(filepath.Join(historyDirectory(root, "small"), "attempts", first.ID+".json")); err != nil {
				t.Fatalf("rejected promotion lost small history: %v", err)
			}
		})
	}
}

func TestHistoryAbsentDiffersFromCorrupt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := verification.Load(root, "small-task"); !errors.Is(err, verification.ErrNoHistory) {
		t.Fatalf("absent history = %v", err)
	}
	problems, err := verification.ValidateRecords(root, "small-task")
	if err != nil || len(problems) != 0 {
		t.Fatalf("absent validation = %v, %v", problems, err)
	}
	if err := os.MkdirAll(historyDirectory(root, "small"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := verification.Load(root, "small-task"); err == nil || errors.Is(err, verification.ErrNoHistory) {
		t.Fatalf("incomplete history treated as absent: %v", err)
	}
	if _, err := verification.Publish(root, "small-task", "small", historyFixture()); err == nil {
		t.Fatal("publication silently repaired an incomplete history")
	}
}

func TestHistoryRejectsInvalidClaims(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*verification.Attempt)
	}{
		{name: "bad-version", edit: func(a *verification.Attempt) { a.Version = 2 }},
		{name: "unsafe-task", edit: func(a *verification.Attempt) { a.Task = "../task" }},
		{name: "bad-workflow", edit: func(a *verification.Attempt) { a.Workflow = "fast" }},
		{name: "short-tree", edit: func(a *verification.Attempt) { a.BaseTree = "deadbeef" }},
		{name: "policy-path", edit: func(a *verification.Attempt) { a.PolicyKey = "/tmp/policy" }},
		{name: "missing-selections", edit: func(a *verification.Attempt) { a.Selections = nil }},
		{name: "missing-results", edit: func(a *verification.Attempt) { a.Results = nil }},
		{name: "selection-path", edit: func(a *verification.Attempt) { a.Selections[0].ID = "/tmp/skill" }},
		{name: "selection-host", edit: func(a *verification.Attempt) { a.Selections[0].Host = "other" }},
		{name: "selection-role", edit: func(a *verification.Attempt) { a.Selections[0].Role = "other" }},
		{name: "duplicate-selection", edit: func(a *verification.Attempt) { a.Selections = append(a.Selections, a.Selections[0]) }},
		{name: "duplicate-check", edit: func(a *verification.Attempt) { a.Results = append(a.Results, a.Results[0]) }},
		{name: "check-path", edit: func(a *verification.Attempt) { a.Results[0].ID = "../check" }},
		{name: "unknown-state", edit: func(a *verification.Attempt) { a.Results[0].State = "success" }},
		{name: "pending-with-evidence", edit: func(a *verification.Attempt) { a.Results[0].State = "pending" }},
		{name: "run-path", edit: func(a *verification.Attempt) { a.Results[0].RunID = "/tmp/run" }},
		{name: "log-path", edit: func(a *verification.Attempt) { a.Results[0].LogSHA256 = "/tmp/log" }},
		{name: "passed-with-unknown-exit", edit: func(a *verification.Attempt) { a.Results[0].ExitStatus = nil }},
		{name: "passed-with-failing-exit", edit: func(a *verification.Attempt) { exit := 1; a.Results[0].ExitStatus = &exit }},
		{name: "failed-with-zero-exit", edit: func(a *verification.Attempt) { a.Results[0].State = "failed" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := historyFixture()
			tc.edit(&a)
			if _, err := verification.Publish(t.TempDir(), a.Task, a.Workflow, a); err == nil {
				t.Fatal("invalid portable claims accepted")
			}
		})
	}
}

func TestHistoryRetainsRunnerStatesAndAbsentEvidence(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"passed", "failed", "timed_out", "interrupted", "source_changed", "pending", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			a := historyFixture()
			a.Results[0].State = state
			a.Results[0].ContentKey = strings.Repeat("f", 40) // Whole-tree keys are Git ids.
			if state == "failed" {
				exit := 17
				a.Results[0].ExitStatus = &exit
			}
			if state == "timed_out" || state == "interrupted" || state == "source_changed" {
				a.Results[0].ExitStatus = nil
			}
			if state == "pending" || state == "unavailable" {
				a.Results[0] = verification.Result{ID: "full-gate", State: state}
			}
			root := t.TempDir()
			published := historyPublish(t, root, a)
			loaded, err := verification.Load(root, a.Task)
			if err != nil || !reflect.DeepEqual(published, loaded) {
				t.Fatalf("state lost: %#v, %v", loaded, err)
			}
		})
	}
}

func TestHistoryStrictJSONAndIntegrity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func([]byte) []byte
	}{
		{name: "duplicate", edit: func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"version": 1,`), []byte("\"version\": 1,\n  \"version\": 1,"), 1)
		}},
		{name: "case-alias", edit: func(b []byte) []byte { return bytes.Replace(b, []byte(`"version"`), []byte(`"Version"`), 1) }},
		{name: "unknown", edit: func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"version": 1,`), []byte(`"raw_log": "private", "version": 1,`), 1)
		}},
		{name: "trailing", edit: func(b []byte) []byte { return append(b, []byte("{}\n")...) }},
		{name: "missing-required-field", edit: func(b []byte) []byte { return bytes.Replace(b, []byte(",\n      \"exit_status\": 0"), nil, 1) }},
		{name: "null-scalar", edit: func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"exit_status": 0`), []byte(`"exit_status": null`), 1)
		}},
		{name: "wrong-task", edit: func(b []byte) []byte { return bytes.Replace(b, []byte(`"small-task"`), []byte(`"other-task"`), 1) }},
		{name: "wrong-workflow", edit: func(b []byte) []byte { return bytes.Replace(b, []byte(`"small"`), []byte(`"full"`), 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			original := historyPublish(t, root, a)
			dir := historyDirectory(root, "small")
			old := filepath.Join(dir, "attempts", original.ID+".json")
			data, err := os.ReadFile(old)
			if err != nil {
				t.Fatal(err)
			}
			modified := tc.edit(data)
			if bytes.Equal(modified, data) {
				t.Fatal("mutation did not change fixture")
			}
			id := fmt.Sprintf("%x", sha256.Sum256(modified))
			historyWrite(t, filepath.Join(dir, "attempts", id+".json"), modified)
			// Struct field order is part of canonical pointer encoding.
			pointer := []byte(fmt.Sprintf("{\n  \"version\": 1,\n  \"id\": %q\n}\n", id))
			historyWrite(t, filepath.Join(dir, "binding.json"), pointer)
			if _, err := verification.Load(root, a.Task); err == nil {
				t.Fatal("malformed history loaded")
			}
			problems, err := verification.ValidateRecords(root, a.Task)
			if err != nil || len(problems) == 0 {
				t.Fatalf("offline validation missed damage: %v, %v", problems, err)
			}
			if _, err := verification.Publish(root, a.Task, a.Workflow, a); err == nil {
				t.Fatal("publication ignored malformed history")
			}
		})
	}
}

func TestHistoryDetectsOldAttemptTamperingAndForgedNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"old-tampering", "forged-filename", "missing-current", "bad-pointer"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			first := historyPublish(t, root, a)
			a.SourceTree = strings.Repeat("8", 40)
			current := historyPublish(t, root, a)
			dir := historyDirectory(root, "small")
			old := filepath.Join(dir, "attempts", first.ID+".json")
			switch name {
			case "old-tampering":
				data, err := os.ReadFile(old)
				if err != nil {
					t.Fatal(err)
				}
				historyWrite(t, old, bytes.Replace(data, []byte("reject invalid input"), []byte("changed expectation"), 1))
			case "forged-filename":
				if err := os.Rename(old, filepath.Join(dir, "attempts", strings.Repeat("9", 64)+".json")); err != nil {
					t.Fatal(err)
				}
			case "missing-current":
				if err := os.Remove(filepath.Join(dir, "attempts", current.ID+".json")); err != nil {
					t.Fatal(err)
				}
			case "bad-pointer":
				historyWrite(t, filepath.Join(dir, "binding.json"), []byte("{\n  \"version\": 2,\n  \"id\": \"no\"\n}\n"))
			}
			if _, err := verification.Load(root, a.Task); err == nil || errors.Is(err, verification.ErrNoHistory) {
				t.Fatalf("tampered history loaded or treated as absent: %v", err)
			}
		})
	}
}

func TestHistoryRejectsSymlinks(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"history-directory", "attempt-file", "binding-file", "attempt-directory", "task-directory"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			h := historyPublish(t, root, a)
			dir := historyDirectory(root, "small")
			target := dir
			switch name {
			case "attempt-file":
				target = filepath.Join(dir, "attempts", h.ID+".json")
			case "binding-file":
				target = filepath.Join(dir, "binding.json")
			case "attempt-directory":
				target = filepath.Join(dir, "attempts")
			case "task-directory":
				target = filepath.Join(root, "openspec/changes/.verification")
			}
			moved := target + "-real"
			if err := os.Rename(target, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, target); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if _, err := verification.Load(root, a.Task); err == nil {
				t.Fatal("linked history loaded")
			}
			if _, err := verification.Publish(root, a.Task, a.Workflow, a); err == nil {
				t.Fatal("linked history was written")
			}
			problems, err := verification.ValidateRecords(root, a.Task)
			if err != nil || len(problems) == 0 {
				t.Fatalf("linked record missed by offline validation: %v, %v", problems, err)
			}
		})
	}
}

func TestHistoryConcurrentPublications(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const count = 8
	var group sync.WaitGroup
	errors := make(chan error, count)
	for i := range count {
		group.Go(func() {
			a := historyFixture()
			a.SourceTree = fmt.Sprintf("%040x", i+1)
			_, err := verification.Publish(root, a.Task, a.Workflow, a)
			errors <- err
		})
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(historyDirectory(root, "small"), "attempts"))
	if err != nil || len(entries) != count {
		t.Fatalf("concurrent attempts = %d, %v; want %d", len(entries), err, count)
	}
	if _, err := verification.Load(root, "small-task"); err != nil {
		t.Fatal(err)
	}
}
