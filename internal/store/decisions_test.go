package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func record(t *testing.T, s *Store, d Decision) {
	t.Helper()
	if err := s.Record(t.Context(), d); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestRecordAndReadDecisions(t *testing.T) {
	s := newStore(t)
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	first := Decision{Time: at, Hook: "pre-commit", Mode: Enforce, Tree: "t1", Outcome: "allowed"}
	second := Decision{
		Time: at.Add(time.Minute), Hook: "pre-commit", Mode: Enforce, Tree: "t2", Outcome: "denied",
		Checks: map[string]string{"unit": "failed", "lint": "not_run"},
	}
	third := Decision{Time: at.Add(2 * time.Minute), Hook: "pre-push", Mode: Observe, Outcome: "denied", Ref: "refs/heads/main", Cause: CauseInvalidDeclaration}
	for _, d := range []Decision{first, second, third} {
		record(t, s, d)
	}
	got, unreadable, err := s.Decisions(at.Add(-time.Hour))
	if err != nil || unreadable != 0 {
		t.Fatalf("Decisions: %v, unreadable %d", err, unreadable)
	}
	want := []Decision{first, second, third}
	if len(got) != len(want) {
		t.Fatalf("got %d decisions, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) {
			t.Errorf("decision %d time = %v", i, got[i].Time)
		}
		got[i].Time = want[i].Time
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("decision %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "decisions", "2026-11.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := countLines(data); lines != 3 {
		t.Errorf("the month file holds %d lines, want 3", lines)
	}
}

func countLines(data []byte) int {
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func TestDecisionsWithoutDirectory(t *testing.T) {
	s := newStore(t)
	got, unreadable, err := s.Decisions(time.Time{})
	if err != nil || unreadable != 0 || got != nil {
		t.Errorf("Decisions = %v, %d, %v", got, unreadable, err)
	}
}

func TestDecisionsSince(t *testing.T) {
	s := newStore(t)
	times := []time.Time{
		time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC),
	}
	for i, at := range times {
		record(t, s, Decision{Time: at, Hook: "pre-commit", Mode: Enforce, Outcome: "allowed", Tree: string(rune('a' + i))})
	}
	cases := []struct {
		since time.Time
		want  int
	}{
		{time.Time{}, 4},
		{times[1], 3},
		{times[1].Add(time.Second), 2},
		{times[2], 2},
		{times[3], 1},
		{times[3].Add(time.Nanosecond), 0},
	}
	for _, tc := range cases {
		got, _, err := s.Decisions(tc.since)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != tc.want {
			t.Errorf("Decisions(%v) = %d decisions, want %d", tc.since, len(got), tc.want)
		}
		for _, d := range got {
			if d.Time.Before(tc.since) {
				t.Errorf("Decisions(%v) returned one made at %v", tc.since, d.Time)
			}
		}
	}
}

func TestDecisionsCountsUnreadableLines(t *testing.T) {
	s := newStore(t)
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	record(t, s, Decision{Time: at, Hook: "pre-commit", Mode: Enforce, Outcome: "allowed"})
	path := filepath.Join(s.Dir, "decisions", "2026-11.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("{not json\n{\"time\":\"2026-11-14T10:00:00Z\"}\n")
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	record(t, s, Decision{Time: at.Add(time.Second), Hook: "pre-push", Mode: Enforce, Outcome: "allowed"})
	got, unreadable, err := s.Decisions(at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || unreadable != 2 {
		t.Errorf("Decisions = %d decisions, %d unreadable; want 2 and 2", len(got), unreadable)
	}
}

func TestDecisionsIgnoresOtherFiles(t *testing.T) {
	s := newStore(t)
	writeFile(t, filepath.Join(s.Dir, "decisions", "write.lock"), nil)
	writeFile(t, filepath.Join(s.Dir, "decisions", "notes.txt"), []byte("{"))
	got, unreadable, err := s.Decisions(time.Time{})
	if err != nil || unreadable != 0 || len(got) != 0 {
		t.Errorf("Decisions = %v, %d, %v", got, unreadable, err)
	}
}

func setAge(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestSweep(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	files := map[string]time.Time{
		"logs/old.log":                    now.Add(-LogAge - time.Hour),
		"logs/new.log":                    now.Add(-LogAge + time.Hour),
		"runs/unit/old/r1.json":           now.Add(-RecordAge - time.Hour),
		"runs/unit/new/r2.json":           now.Add(-RecordAge + time.Hour),
		"runs/unit/mixed/r3.json":         now.Add(-RecordAge - time.Hour),
		"runs/unit/mixed/r4.json":         now.Add(-time.Hour),
		"decisions/2025-10.jsonl":         now.Add(-RecordAge - time.Hour),
		"decisions/2026-11.jsonl":         now.Add(-time.Hour),
		"tmp/index-1-dead":                now.Add(-25 * time.Hour),
		"tmp/index-2-live":                now.Add(-time.Minute),
		"tmp/index-3-held":                now.Add(-25 * time.Hour), // backdated to its index; its process still holds it
		"tmp/index-3-held.hold":           now.Add(-time.Minute),
		"tmp/index-4-left":                now.Add(-25 * time.Hour),
		"tmp/index-4-left.hold":           now.Add(-25 * time.Hour),
		"mode":                            now.Add(-2 * RecordAge),
		"runs/lint/k/older-than-log.json": now.Add(-LogAge - time.Hour),
	}
	for rel, at := range files {
		path := filepath.Join(s.Dir, filepath.FromSlash(rel))
		writeFile(t, path, []byte("x"))
		setAge(t, path, at)
	}
	s.Sweep(t.Context(), now)

	gone := []string{"logs/old.log", "runs/unit/old/r1.json", "runs/unit/old", "runs/unit/mixed/r3.json", "decisions/2025-10.jsonl", "tmp/index-1-dead", "tmp/index-4-left", "tmp/index-4-left.hold"}
	kept := []string{"logs/new.log", "runs/unit/new/r2.json", "runs/unit/mixed/r4.json", "decisions/2026-11.jsonl", "tmp/index-2-live", "tmp/index-3-held", "tmp/index-3-held.hold", "mode", "runs/lint/k/older-than-log.json"}
	for _, rel := range gone {
		if exists(filepath.Join(s.Dir, filepath.FromSlash(rel))) {
			t.Errorf("Sweep kept %s", rel)
		}
	}
	for _, rel := range kept {
		if !exists(filepath.Join(s.Dir, filepath.FromSlash(rel))) {
			t.Errorf("Sweep removed %s", rel)
		}
	}
}

func TestSweepWithoutStore(t *testing.T) {
	s := newStore(t)
	s.Sweep(t.Context(), time.Now())
	if exists(s.Dir) {
		t.Error("Sweep created the store")
	}
}
