package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var start = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func newRecord(t *testing.T, id, key string) Record {
	t.Helper()
	runID, err := NewRunID(start)
	if err != nil {
		t.Fatal(err)
	}
	return Record{
		RunID: runID, ID: id, Run: []string{"sh", id + ".sh"}, Paths: []string{"src/**"},
		Tree: "0123456789abcdef0123456789abcdef01234567", ContentKey: key, StartedAt: start,
	}
}

func ended(outcome string, code int, at time.Time) func(*Record) {
	return func(r *Record) {
		r.Outcome = outcome
		if code >= 0 {
			r.ExitStatus = &code
		}
		r.EndedAt = &at
	}
}

func begin(t *testing.T, s *Store, rec Record) *Run {
	t.Helper()
	run, err := s.Begin(t.Context(), rec)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return run
}

func finish(t *testing.T, run *Run, update func(*Record)) Record {
	t.Helper()
	rec, err := run.Finish(t.Context(), update)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return rec
}

func TestBeginWritesRecordWithoutOutcome(t *testing.T) {
	s := newStore(t)
	rec := newRecord(t, "unit", "k1")
	run := begin(t, s, rec)
	path := filepath.Join(s.Dir, "runs", "unit", "k1", rec.RunID+".json")

	var onDisk map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the record is not on disk while the run executes: %v", err)
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("the record is not JSON: %v", err)
	}
	if _, ok := onDisk["outcome"]; ok {
		t.Errorf("the record of an executing run has an outcome: %s", data)
	}
	if onDisk["log"] != "logs/"+rec.RunID+".log" {
		t.Errorf("log = %v", onDisk["log"])
	}
	if run.Record().Log != "logs/"+rec.RunID+".log" {
		t.Errorf("Record().Log = %q", run.Record().Log)
	}
	if !exists(filepath.Join(s.Dir, "logs")) {
		t.Error("Begin did not create the log directory")
	}

	records, damaged, err := s.Records("unit", "k1")
	if err != nil || len(damaged) != 0 || len(records) != 1 {
		t.Fatalf("Records = %v, %v, %v", records, damaged, err)
	}
	if !records[0].Running || records[0].Outcome != "" {
		t.Errorf("an executing run reads as Running=%v Outcome=%q", records[0].Running, records[0].Outcome)
	}
	finish(t, run, ended(Passed, 0, start.Add(time.Minute)))
}

func TestFinishWritesCompleteRecord(t *testing.T) {
	s := newStore(t)
	rec := newRecord(t, "unit", "k1")
	run := begin(t, s, rec)
	end := start.Add(time.Minute)
	got := finish(t, run, ended(Failed, 3, end))
	if got.Seq != 1 || got.Outcome != Failed || got.ExitStatus == nil || *got.ExitStatus != 3 {
		t.Errorf("Finish returned %+v", got)
	}

	dir := filepath.Join(s.Dir, "runs", "unit", "k1")
	data, err := os.ReadFile(filepath.Join(dir, rec.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("the finished record is not complete JSON: %v\n%s", err, data)
	}
	for _, key := range []string{"run_id", "id", "run", "paths", "tree", "content_key", "seq", "outcome", "exit_status", "started_at", "ended_at", "log"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("the finished record lacks %q: %s", key, data)
		}
	}
	if fields["outcome"] != Failed || fields["seq"] != float64(1) || fields["exit_status"] != float64(3) {
		t.Errorf("record = %s", data)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("a temporary file is left: %s", e.Name())
		}
		if e.Name() == rec.RunID+".lock" {
			t.Errorf("the run's lock file is left after Finish")
		}
	}

	records, _, err := s.Records("unit", "k1")
	if err != nil || len(records) != 1 || records[0].Running || records[0].Outcome != Failed {
		t.Errorf("Records after Finish = %+v, %v", records, err)
	}
}

func TestFinishWithoutExitStatus(t *testing.T) {
	s := newStore(t)
	run := begin(t, s, newRecord(t, "unit", "k1"))
	finish(t, run, ended(TimedOut, -1, start.Add(time.Minute)))
	data, err := os.ReadFile(filepath.Join(s.Dir, "runs", "unit", "k1", run.Record().RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "exit_status") {
		t.Errorf("a run that did not exit on its own has an exit_status: %s", data)
	}
}

func TestSeqGrowsByOneInOneDirectory(t *testing.T) {
	s := newStore(t)
	for want := int64(1); want <= 3; want++ {
		got := finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Passed, 0, start))
		if got.Seq != want {
			t.Errorf("run %d got seq %d", want, got.Seq)
		}
	}
	// A non-decisive end takes a number in the same order.
	if got := finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(SourceChanged, 0, start)); got.Seq != 4 {
		t.Errorf("source_changed run got seq %d, want 4", got.Seq)
	}
	// Another directory counts on its own.
	if got := finish(t, begin(t, s, newRecord(t, "unit", "k2")), ended(Passed, 0, start)); got.Seq != 1 {
		t.Errorf("first run in another key got seq %d, want 1", got.Seq)
	}
	if got := finish(t, begin(t, s, newRecord(t, "lint", "k1")), ended(Passed, 0, start)); got.Seq != 1 {
		t.Errorf("first run of another check got seq %d, want 1", got.Seq)
	}
}

func TestSeqDoesNotDependOnClock(t *testing.T) {
	s := newStore(t)
	first := finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Passed, 0, start.Add(time.Hour)))
	second := finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Failed, 1, start.Add(-time.Hour)))
	if second.Seq <= first.Seq {
		t.Errorf("seq %d of the later end is not above %d", second.Seq, first.Seq)
	}
}

func TestConcurrentFinishGetsDistinctSeq(t *testing.T) {
	s := newStore(t)
	const n = 8
	runs := make([]*Run, n)
	for i := range runs {
		runs[i] = begin(t, s, newRecord(t, "unit", "k1"))
	}
	seqs := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i, run := range runs {
		wg.Go(func() {
			rec, err := run.Finish(context.Background(), ended(Passed, 0, start))
			seqs[i], errs[i] = rec.Seq, err
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Finish: %v", err)
		}
	}
	slices.Sort(seqs)
	for i, seq := range seqs {
		if seq != int64(i+1) {
			t.Fatalf("seq values = %v, want 1..%d each once", seqs, n)
		}
	}
}

func TestTwoGoroutinesFinishInSameDirectory(t *testing.T) {
	s := newStore(t)
	a := begin(t, s, newRecord(t, "unit", "k1"))
	b := begin(t, s, newRecord(t, "unit", "k1"))
	var seqA, seqB int64
	var errA, errB error
	var wg sync.WaitGroup
	wg.Go(func() {
		rec, err := a.Finish(context.Background(), ended(Passed, 0, start))
		seqA, errA = rec.Seq, err
	})
	wg.Go(func() {
		rec, err := b.Finish(context.Background(), ended(Failed, 1, start))
		seqB, errB = rec.Seq, err
	})
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("Finish: %v, %v", errA, errB)
	}
	if seqA == seqB {
		t.Errorf("both runs got seq %d", seqA)
	}
	records, damaged, err := s.Records("unit", "k1")
	if err != nil || len(damaged) != 0 || len(records) != 2 {
		t.Errorf("Records = %v, %v, %v", records, damaged, err)
	}
}

func TestRecordsMissingDirectory(t *testing.T) {
	s := newStore(t)
	records, damaged, err := s.Records("unit", "nothing")
	if err != nil || records != nil || damaged != nil {
		t.Errorf("Records of nothing = %v, %v, %v", records, damaged, err)
	}
	if exists(s.Dir) {
		t.Error("Records created the store")
	}
}

func TestRecordsTruncatedFileIsDamaged(t *testing.T) {
	s := newStore(t)
	run := begin(t, s, newRecord(t, "unit", "k1"))
	finish(t, run, ended(Passed, 0, start))
	path := filepath.Join(s.Dir, "runs", "unit", "k1", run.Record().RunID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, data[:len(data)/2])

	records, damaged, err := s.Records("unit", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("a truncated record was read: %+v", records)
	}
	if len(damaged) != 1 || damaged[0].Path != path || damaged[0].Err == nil {
		t.Errorf("damaged = %+v, want %s", damaged, path)
	}
}

func TestRecordsInvalidRecords(t *testing.T) {
	cases := map[string]string{
		"not JSON":                  "{",
		"no run_id":                 `{"id":"unit","run":["x"],"content_key":"k1"}`,
		"no run":                    `{"run_id":"r","id":"unit","content_key":"k1"}`,
		"outcome without end time":  `{"run_id":"r","id":"unit","run":["x"],"content_key":"k1","outcome":"passed"}`,
		"empty file":                "",
		"array instead of a record": "[]",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			writeFile(t, filepath.Join(s.Dir, "runs", "unit", "k1", "r.json"), []byte(content))
			records, damaged, err := s.Records("unit", "k1")
			if err != nil || len(records) != 0 || len(damaged) != 1 {
				t.Errorf("Records = %v, %v, %v; want one damaged", records, damaged, err)
			}
		})
	}
}

func TestRecordsIgnoresOtherFiles(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(s.Dir, "runs", "unit", "k1")
	writeFile(t, filepath.Join(dir, ".tmp-123.json"), []byte("{"))
	writeFile(t, filepath.Join(dir, "r.json.damaged"), []byte("{"))
	writeFile(t, filepath.Join(dir, "seq.lock"), nil)
	records, damaged, err := s.Records("unit", "k1")
	if err != nil || len(records) != 0 || len(damaged) != 0 {
		t.Errorf("Records = %v, %v, %v; want nothing", records, damaged, err)
	}
}

// unfinished writes a record without an outcome, as a kitsu process that died
// leaves it.
func unfinished(t *testing.T, s *Store, lockFile bool) Record {
	t.Helper()
	rec := newRecord(t, "unit", "k1")
	rec.Log = "logs/" + rec.RunID + ".log"
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Dir, "runs", "unit", "k1")
	writeFile(t, filepath.Join(dir, rec.RunID+".json"), data)
	if lockFile {
		writeFile(t, filepath.Join(dir, rec.RunID+".lock"), nil)
	}
	return rec
}

func TestRecordsReportsDeadRunAsInterrupted(t *testing.T) {
	for _, lockFile := range []bool{true, false} {
		s := newStore(t)
		rec := unfinished(t, s, lockFile)
		records, damaged, err := s.Records("unit", "k1")
		if err != nil || len(damaged) != 0 || len(records) != 1 {
			t.Fatalf("Records = %v, %v, %v", records, damaged, err)
		}
		got := records[0]
		if got.RunID != rec.RunID || got.Outcome != Interrupted || got.Running {
			t.Errorf("lock file %v: dead run reads as Outcome=%q Running=%v", lockFile, got.Outcome, got.Running)
		}
	}
}

func TestRecordsReportsOpenRunAsRunning(t *testing.T) {
	s := newStore(t)
	run := begin(t, s, newRecord(t, "unit", "k1"))
	records, _, err := s.Records("unit", "k1")
	if err != nil || len(records) != 1 || !records[0].Running || records[0].Outcome != "" {
		t.Errorf("Records = %+v, %v; want one running", records, err)
	}
	finish(t, run, ended(Interrupted, -1, start))
	records, _, err = s.Records("unit", "k1")
	if err != nil || len(records) != 1 || records[0].Running || records[0].Outcome != Interrupted {
		t.Errorf("after Finish, Records = %+v, %v", records, err)
	}
}

func TestDecisiveFinishSetsDamagedAside(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(s.Dir, "runs", "unit", "k1")
	damagedPath := filepath.Join(dir, "broken.json")
	writeFile(t, damagedPath, []byte(`{"run_id":`))

	// An end that decides nothing keeps the damaged file in place.
	finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(SourceChanged, 0, start))
	if !exists(damagedPath) {
		t.Fatal("a source_changed end moved the damaged record")
	}
	finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Interrupted, -1, start))
	if !exists(damagedPath) {
		t.Fatal("an interrupted end moved the damaged record")
	}

	finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Failed, 1, start))
	if exists(damagedPath) || !exists(damagedPath+".damaged") {
		t.Errorf("a decisive end did not rename the damaged record to .damaged")
	}
	_, damaged, err := s.Records("unit", "k1")
	if err != nil || len(damaged) != 0 {
		t.Errorf("after the rename, damaged = %v, %v", damaged, err)
	}
}

func TestDamagedInOtherDirectoryStays(t *testing.T) {
	s := newStore(t)
	other := filepath.Join(s.Dir, "runs", "unit", "k2", "broken.json")
	writeFile(t, other, []byte("{"))
	finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Passed, 0, start))
	if !exists(other) {
		t.Error("a run on other content set aside a damaged record that is not its own")
	}
}

func TestAllRecords(t *testing.T) {
	s := newStore(t)
	if records, damaged, err := s.AllRecords(); err != nil || records != nil || damaged != nil {
		t.Errorf("AllRecords of an empty store = %v, %v, %v", records, damaged, err)
	}
	finish(t, begin(t, s, newRecord(t, "unit", "k1")), ended(Passed, 0, start))
	finish(t, begin(t, s, newRecord(t, "unit", "k2")), ended(Failed, 1, start))
	finish(t, begin(t, s, newRecord(t, "lint", "k3")), ended(Passed, 0, start))
	writeFile(t, filepath.Join(s.Dir, "runs", "lint", "k3", "bad.json"), []byte("{"))
	records, damaged, err := s.AllRecords()
	if err != nil || len(records) != 3 || len(damaged) != 1 {
		t.Errorf("AllRecords = %d records, %d damaged, %v", len(records), len(damaged), err)
	}
}
