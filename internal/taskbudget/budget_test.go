package taskbudget

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeRecord(t *testing.T, root, task, workflow, data string) string {
	t.Helper()
	dir := filepath.Join(root, "openspec", "changes", task)
	if workflow == "small" {
		dir = filepath.Join(root, "openspec", "changes", ".usage", task)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, fileName)
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}
func estimate(min, max int64) Entry {
	return Entry{Estimate: &Estimate{Min: min, Max: max, Basis: "user forecast"}}
}

func TestCompareRetainsIncompleteAndUnknownActuals(t *testing.T) {
	t.Parallel()
	entry := estimate(10, 20)
	entry.Limit = &Limit{N: 20, Source: "human record"}
	tests := []struct {
		name            string
		actual          Actual
		estimate, limit string
	}{
		{"unknown", Actual{Basis: "no instrumentation"}, "unknown", "unknown"},
		{"partial below", Actual{N: 5, Known: true}, "incomplete", "incomplete"},
		{"partial inside", Actual{N: 15, Known: true}, "incomplete", "incomplete"},
		{"partial exceeded", Actual{N: 21, Known: true}, "above", "exceeded"},
		{"complete below", Actual{N: 5, Known: true, Complete: true}, "below", "within"},
		{"complete within", Actual{N: 20, Known: true, Complete: true}, "within", "within"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(entry, tt.actual)
			if got.Actual != tt.actual || got.EstimateStatus != tt.estimate || got.LimitStatus != tt.limit {
				t.Fatalf("got %+v", got)
			}
		})
	}
	if got := Compare(Entry{}, Actual{}); got.EstimateStatus != "absent" || got.LimitStatus != "absent" {
		t.Fatal(got)
	}
}

func TestSetPreservesOtherUnitBytesAndUnspecifiedFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	raw := `{ "estimate" : { "min": 0, "max": 15, "basis": "manual seconds" } }`
	file := writeRecord(t, root, "demo", "small", `{"version":1,"task":"demo","entries":{"seconds":`+raw+`}}`)
	if _, err := Set(root, "demo", "", Tokens, estimate(0, 100)); err != nil {
		t.Fatal(err)
	}
	record, err := Set(root, "demo", "", Tokens, Entry{Limit: &Limit{N: 150, Source: "user approval reference"}})
	if err != nil {
		t.Fatal(err)
	}
	if record.Entries[Tokens].Estimate.Max != 100 || record.Entries[Tokens].Limit.N != 150 {
		t.Fatal(record)
	}
	data, err := os.ReadFile(file)
	if err != nil || !bytes.Contains(data, []byte(raw)) {
		t.Fatalf("other unit changed: %s, %v", data, err)
	}
	if _, err := Reset(root, "demo", Tokens); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(file)
	if err != nil || !bytes.Contains(data, []byte(raw)) {
		t.Fatalf("other unit changed on reset: %s, %v", data, err)
	}
	sibling := filepath.Join(filepath.Dir(file), "other.json")
	if err := os.WriteFile(sibling, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if record, err := Reset(root, "demo", Seconds); err != nil || record != nil {
		t.Fatal(record, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("last reset retained file", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "keep" {
		t.Fatal("reset touched sibling", err)
	}
}

func TestStrictRecordValidation(t *testing.T) {
	t.Parallel()
	valid := `{"version":1,"task":"demo","entries":{"tokens":{"estimate":{"min":0,"max":0,"basis":"manual"},"limit":{"n":0,"source":"human"}}}}`
	tests := []struct{ name, record string }{
		{"missing min", strings.Replace(valid, `"min":0,`, "", 1)},
		{"missing max", strings.Replace(valid, `"max":0,`, "", 1)},
		{"missing limit zero", strings.Replace(valid, `"n":0,`, "", 1)},
		{"duplicate", strings.Replace(valid, `"min":0`, `"min":0,"min":1`, 1)},
		{"case alias", strings.Replace(valid, `"min":0`, `"min":0,"Min":1`, 1)},
		{"unknown", strings.Replace(valid, `"min":0`, `"min":0,"extra":1`, 1)},
		{"overflow", strings.Replace(valid, `"max":0`, `"max":9223372036854775808`, 1)},
		{"negative", strings.Replace(valid, `"min":0`, `"min":-1`, 1)},
		{"range", strings.Replace(valid, `"min":0`, `"min":1`, 1)},
		{"unknown unit", strings.Replace(valid, `"tokens":`, `"calls":`, 1)},
		{"missing source", strings.Replace(valid, `,"source":"human"`, "", 1)},
		{"wrong task", strings.Replace(valid, `"task":"demo"`, `"task":"other"`, 1)},
		{"trailing", valid + ` {}`},
		{"null estimate", `{"version":1,"task":"demo","entries":{"tokens":{"estimate":null}}}`},
		{"empty", `{"version":1,"task":"demo","entries":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeRecord(t, root, "demo", "small", tt.record)
			if err := ValidateRecords(root, "demo"); err == nil {
				t.Fatal("accepted invalid record")
			}
			if _, err := Set(root, "demo", "", Seconds, estimate(0, 1)); err == nil {
				t.Fatal("silently overwrote invalid record")
			}
		})
	}
	root := t.TempDir()
	writeRecord(t, root, "demo", "small", valid)
	record, err := Load(root, "demo")
	if err != nil || record.Entries[Tokens].Limit.N != 0 {
		t.Fatal("explicit zero rejected", err)
	}
}

func TestOptionalLoadAndPromotion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if record, err := Load(root, "demo"); err != nil || record != nil {
		t.Fatal(record, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("read created files", err)
	}
	if _, err := Set(root, "demo", "full", Tokens, estimate(0, 10)); err == nil {
		t.Fatal("explicit full workflow created a missing task structure")
	}
	if _, err := Set(root, "demo", "", Tokens, estimate(0, 10)); err != nil {
		t.Fatal(err)
	}
	small := filepath.Join(root, "openspec", "changes", ".usage", "demo", fileName)
	fullDir := filepath.Join(root, "openspec", "changes", "demo")
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record, err := Load(root, "demo")
	if err != nil || record.Workflow != "full" {
		t.Fatal(record, err)
	}
	if _, err := os.Stat(small); err != nil {
		t.Fatal("show moved file", err)
	}
	if _, err := Set(root, "demo", "small", Tokens, estimate(0, 12)); err == nil {
		t.Fatal("accepted workflow conflict")
	}
	if _, err := Set(root, "demo", "full", Seconds, estimate(0, 12)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(small); !os.IsNotExist(err) {
		t.Fatal("promotion retained small file", err)
	}
	if record, err := Load(root, "demo"); err != nil || len(record.Entries) != 2 {
		t.Fatal(record, err)
	}
	tasks, err := Tasks(root)
	if err != nil || len(tasks) != 1 || tasks[0] != "demo" {
		t.Fatal(tasks, err)
	}
	writeRecord(t, root, "demo", "small", `{"version":1,"task":"demo","entries":{"seconds":{"limit":{"n":1,"source":"manual"}}}}`)
	if _, err := Load(root, "demo"); err == nil {
		t.Fatal("merged competing histories")
	}
}

func TestUnsafeAndBoundedRecords(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file link", "directory link", "oversize", "directory file"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			file := writeRecord(t, root, "demo", "small", `{}`)
			switch kind {
			case "oversize":
				if err := os.WriteFile(file, bytes.Repeat([]byte(" "), maxRecordSize+1), 0o644); err != nil {
					t.Fatal(err)
				}
			case "file link":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "file"), file); err != nil {
					t.Skip(err)
				}
			case "directory link":
				if err := os.RemoveAll(filepath.Dir(file)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Dir(file)); err != nil {
					t.Skip(err)
				}
			case "directory file":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(file, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(root, "demo"); err == nil {
				t.Fatal("accepted unsafe record")
			}
			if _, err := Set(root, "demo", "", Tokens, estimate(0, 1)); err == nil {
				t.Fatal("overwrote unsafe record")
			}
		})
	}
}

func TestConcurrentUnitUpdatesPreserveBoth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, unit := range []string{Tokens, Seconds} {
		wg.Go(func() { _, err := Set(root, "demo", "", unit, estimate(0, 1)); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	record, err := Load(root, "demo")
	if err != nil || len(record.Entries) != 2 {
		t.Fatal(record, err)
	}
}

func TestInvalidUpdatesDoNotWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, unit string
		entry      Entry
	}{
		{"negative", Tokens, estimate(-1, 1)},
		{"range", Tokens, estimate(2, 1)},
		{"unsupported", "bytes", estimate(0, 1)},
		{"absent", Tokens, Entry{}},
		{"source", Tokens, Entry{Limit: &Limit{N: 1}}},
		{"negative limit", Tokens, Entry{Limit: &Limit{N: -1, Source: "manual"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := Set(root, "demo", "", tt.unit, tt.entry); err == nil {
				t.Fatal("accepted invalid update")
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatal("invalid update wrote files", err)
			}
		})
	}
}

func ExampleCompare() {
	entry := Entry{Limit: &Limit{N: 100, Source: "user-provided reference"}}
	partial := Compare(entry, Actual{N: 80, Known: true, Complete: false, Basis: "observed lower bound"})
	fmt.Println(partial.LimitStatus)
	// Output: incomplete
}
