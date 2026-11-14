package taskflow_test

// Independent tests for BuildTimeline and Timeline.SummaryLines, authored from
// interface contract revision 2 for `cw task timeline` (requirements TL-1 to
// TL-6, definitions D1 to D3). Every expected value is a hand-checked literal
// from the contract or, in the property tests, an independent model of the
// contract's rules. Records are built in memory with fixed timestamps.

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const tlnTaskID = "0123456789abcdef0123456789abcdef"

var (
	tlnBase = time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	// Fixed zones with half-hour and quarter-hour offsets, so local text and
	// UTC text differ in every component.
	tlnIST   = time.FixedZone("IST", 5*3600+30*60)
	tlnNST   = time.FixedZone("NST", -(3*3600 + 30*60))
	tlnChath = time.FixedZone("CHAST", 13*3600+45*60)
)

func tlnAt(d time.Duration) time.Time { return tlnBase.Add(d) }

func tlnPtr(t time.Time) *time.Time { return &t }

func tlnAssignment(id, role, agent string, at time.Time) taskflow.WorkerAssignment {
	return taskflow.WorkerAssignment{
		TaskID:     tlnTaskID,
		Brief:      taskflow.WorkerBrief{ID: id, AgentID: agent, Role: role, Goal: "g", OwnedPaths: []string{"."}, StopCondition: "s"},
		AssignedAt: at,
	}
}

func tlnResult(assignmentID, status string, at time.Time) taskflow.BoundWorkerResult {
	return taskflow.BoundWorkerResult{
		Result:      taskflow.WorkerResult{TaskID: tlnTaskID, AssignmentID: assignmentID, AgentID: "/root/agent", Status: status, Summary: "s"},
		SubmittedAt: at,
	}
}

func tlnAcceptance(assignmentID, status string, at time.Time) taskflow.BoundWorkerAcceptance {
	return taskflow.BoundWorkerAcceptance{
		Acceptance: taskflow.WorkerAcceptance{TaskID: tlnTaskID, AssignmentID: assignmentID, CoordinatorID: "/root", Status: status, Reason: "r"},
		AcceptedAt: at,
	}
}

func tlnCheck(id, checkID, status string, start time.Time, finish *time.Time) taskflow.CheckRun {
	return taskflow.CheckRun{ID: id, CheckID: checkID, Status: status, Argv: []string{"true"}, StartedAt: start, FinishedAt: finish}
}

// tlnBuild calls BuildTimeline and turns a panic into a test failure, so one
// failing case does not hide the others.
func tlnBuild(t *testing.T, record taskflow.Record) (timeline taskflow.Timeline) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("BuildTimeline panicked: %v", r)
		}
	}()
	return taskflow.BuildTimeline(record)
}

func tlnSummary(t *testing.T, timeline taskflow.Timeline) (lines []string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SummaryLines panicked: %v", r)
		}
	}()
	return timeline.SummaryLines()
}

func tlnEqualEntries(t *testing.T, got, want []taskflow.TimelineEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("entries: got %d, want %d\n got: %+v\nwant: %+v", len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got: %+v\nwant: %+v", i, got[i], want[i])
		}
	}
}

// tlnObjectKeys decodes one JSON object and returns its keys in order,
// failing when it is not a single object holding only string values.
func tlnObjectKeys(t *testing.T, line string) ([]string, map[string]string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(line))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		t.Fatalf("line %q is not a JSON object: %v %v", line, token, err)
	}
	var keys []string
	values := map[string]string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			t.Fatalf("line %q: key %v is not a string", line, keyToken)
		}
		valueToken, err := decoder.Token()
		if err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		value, ok := valueToken.(string)
		if !ok {
			t.Fatalf("line %q: value of %q is %v, want a string (TL-6 holds only string values)", line, key, valueToken)
		}
		keys = append(keys, key)
		values[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		t.Fatalf("line %q: object not closed: %v %v", line, token, err)
	}
	if _, err := decoder.Token(); err == nil {
		t.Fatalf("line %q holds more than one JSON value", line)
	}
	return keys, values
}

func tlnMarshalKeys(t *testing.T, value any) []string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// TL-1: task fields come verbatim from the record, elapsed ends at UpdatedAt
// (the last stored revision, in the past here, not now) and an empty record
// has a non-nil empty Entries. Fails if Task is rewritten, elapsed uses the
// current clock, or Entries is nil.
func TestTimelineTL1TaskFieldsAndEmptyEntries(t *testing.T) {
	record := taskflow.Record{
		Version:   1,
		ID:        tlnTaskID,
		Directory: "/private/var/tasks/dir with space/" + tlnTaskID,
		Revision:  3,
		CreatedAt: tlnBase,
		UpdatedAt: tlnAt(time.Hour + 2*time.Minute + 3*time.Second),
	}
	got := tlnBuild(t, record)
	want := taskflow.Timeline{
		Task:      "/private/var/tasks/dir with space/" + tlnTaskID,
		TaskID:    tlnTaskID,
		StartedAt: "2026-01-02T03:00:00Z",
		UpdatedAt: "2026-01-02T04:02:03Z",
		Elapsed:   "1h2m3s",
		Entries:   []taskflow.TimelineEntry{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildTimeline:\n got: %#v\nwant: %#v", got, want)
	}
	if got.Entries == nil {
		t.Fatal("Entries is nil; TL-1 requires a non-nil empty slice")
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"entries":[]`)) {
		t.Errorf("JSON %s lacks \"entries\":[]", data)
	}
}

// D1/D2 on TL-1 fields: present instants are written in UTC with RFC 3339
// nanoseconds whatever their stored location; a zero instant (in any
// location) is n/a and so is the elapsed time computed from it. Fails if the
// stored location leaks into the text, fractions are lost or padded, or a zero
// instant prints as a year-1 timestamp.
func TestTimelineD1D2TimestampText(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"whole second", tlnBase, "2026-01-02T03:00:00Z"},
		{"half second trims trailing zeros", tlnAt(500 * time.Millisecond), "2026-01-02T03:00:00.5Z"},
		{"nanoseconds", time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC), "2026-01-02T03:04:05.123456789Z"},
		{"positive offset converted to UTC", time.Date(2026, 1, 2, 8, 34, 5, 0, tlnIST), "2026-01-02T03:04:05Z"},
		{"negative offset crosses the date", time.Date(2026, 1, 1, 23, 34, 5, 0, tlnNST), "2026-01-02T03:04:05Z"},
		{"zero is n/a", time.Time{}, "n/a"},
		{"zero in a location is n/a", time.Time{}.In(tlnIST), "n/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			startedOnly := tlnBuild(t, taskflow.Record{ID: tlnTaskID, Directory: "/d", CreatedAt: tc.at, UpdatedAt: tlnAt(time.Hour)})
			if startedOnly.StartedAt != tc.want {
				t.Errorf("StartedAt = %q, want %q", startedOnly.StartedAt, tc.want)
			}
			updatedOnly := tlnBuild(t, taskflow.Record{ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tc.at})
			if updatedOnly.UpdatedAt != tc.want {
				t.Errorf("UpdatedAt = %q, want %q", updatedOnly.UpdatedAt, tc.want)
			}
			if tc.want == taskflow.NotAvailable {
				if startedOnly.Elapsed != "n/a" || updatedOnly.Elapsed != "n/a" {
					t.Errorf("Elapsed with a missing endpoint = %q, %q; want n/a for both", startedOnly.Elapsed, updatedOnly.Elapsed)
				}
			}
		})
	}
}

// D2: NotAvailable is the literal the contract names.
func TestTimelineD2NotAvailableLiteral(t *testing.T) {
	if taskflow.NotAvailable != "n/a" {
		t.Fatalf("NotAvailable = %q, want \"n/a\"", taskflow.NotAvailable)
	}
}

// D3 through Elapsed: durations are to.Sub(from) rounded to the millisecond
// (halves away from zero) and written by time.Duration.String; negatives are
// not clamped; a measured zero is 0s and a missing endpoint is n/a, never 0s.
func TestTimelineD3DurationText(t *testing.T) {
	cases := []struct {
		name     string
		from, to time.Time
		want     string
	}{
		{"zero", tlnBase, tlnBase, "0s"},
		{"250ms", tlnBase, tlnAt(250 * time.Millisecond), "250ms"},
		{"1.5s", tlnBase, tlnAt(1500 * time.Millisecond), "1.5s"},
		{"minutes", tlnBase, tlnAt(time.Minute + 2003*time.Millisecond), "1m2.003s"},
		{"hour", tlnBase, tlnAt(time.Hour), "1h0m0s"},
		{"more than a day", tlnBase, tlnAt(36 * time.Hour), "36h0m0s"},
		{"half millisecond rounds up", tlnBase, tlnAt(time.Hour + 2*time.Minute + 3*time.Second + 4500*time.Microsecond), "1h2m3.005s"},
		{"just below half rounds down", tlnBase, tlnAt(1499999 * time.Nanosecond), "1ms"},
		{"exact half rounds away from zero", tlnBase, tlnAt(1500 * time.Microsecond), "2ms"},
		{"sub-half-millisecond is a measured zero", tlnBase, tlnAt(499999 * time.Nanosecond), "0s"},
		{"one nanosecond is a measured zero", tlnBase, tlnAt(time.Nanosecond), "0s"},
		{"half millisecond", tlnBase, tlnAt(500 * time.Microsecond), "1ms"},
		{"negative is not clamped", tlnAt(2 * time.Second), tlnBase, "-2s"},
		{"negative half rounds away from zero", tlnAt(1500 * time.Microsecond), tlnBase, "-2ms"},
		{"endpoints in different locations", tlnBase.In(tlnIST), tlnAt(90 * time.Second).In(tlnNST), "1m30s"},
		{"same instant in different locations", tlnBase.In(tlnChath), tlnBase.In(tlnNST), "0s"},
		{"missing start", time.Time{}, tlnBase, "n/a"},
		{"missing end", tlnBase, time.Time{}, "n/a"},
		{"both missing is n/a, not 0s", time.Time{}, time.Time{}, "n/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tlnBuild(t, taskflow.Record{ID: tlnTaskID, Directory: "/d", CreatedAt: tc.from, UpdatedAt: tc.to})
			if got.Elapsed != tc.want {
				t.Errorf("Elapsed = %q, want %q", got.Elapsed, tc.want)
			}
		})
	}
}

// TL-2: one entry per assignment, with state, timestamps and the two
// durations taken from the matching result and the latest matching
// acceptance in slice order. Each case holds the assignment under test, which
// starts first, and a distractor assignment that starts later and owns its
// own result and acceptance, so a result or acceptance matched by position or
// by another assignment's ID fails the case.
func TestTimelineTL2AssignmentEntries(t *testing.T) {
	distractor := tlnAssignment("zz-other", "evidence", "/root/distractor", tlnAt(10*time.Hour))
	distractorResult := tlnResult("zz-other", "cancelled", tlnAt(11*time.Hour))
	distractorAcceptance := tlnAcceptance("zz-other", "rejected", tlnAt(12*time.Hour))

	cases := []struct {
		name        string
		assignment  taskflow.WorkerAssignment
		results     []taskflow.BoundWorkerResult
		acceptances []taskflow.BoundWorkerAcceptance
		want        taskflow.TimelineEntry
	}{
		{
			name:       "no result and no acceptance is assigned with n/a",
			assignment: tlnAssignment("impl-1", "implementation", "/root/implementer", tlnAt(time.Second)),
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "impl-1", Stage: "implementation", Agent: "/root/implementer", State: "assigned",
				AssignedAt: "2026-01-02T03:00:01Z", SubmittedAt: "n/a", AcceptedAt: "n/a",
				AssignToHandoff: "n/a", HandoffToAccept: "n/a",
			},
		},
		{
			name:       "result without acceptance takes the result status",
			assignment: tlnAssignment("rev-1", "review", "/root/reviewer one", tlnAt(2*time.Second)),
			results:    []taskflow.BoundWorkerResult{tlnResult("rev-1", "blocked", tlnAt(2250*time.Millisecond))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "rev-1", Stage: "review", Agent: "/root/reviewer one", State: "blocked",
				AssignedAt: "2026-01-02T03:00:02Z", SubmittedAt: "2026-01-02T03:00:02.25Z", AcceptedAt: "n/a",
				AssignToHandoff: "250ms", HandoffToAccept: "n/a",
			},
		},
		{
			name:        "accepted result",
			assignment:  tlnAssignment("ev-1", "evidence", "/root/worker", tlnAt(3*time.Second)),
			results:     []taskflow.BoundWorkerResult{tlnResult("ev-1", "completed", tlnAt(time.Minute+5003*time.Millisecond))},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("ev-1", "accepted", tlnAt(time.Hour+time.Minute+5003*time.Millisecond))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ev-1", Stage: "evidence", Agent: "/root/worker", State: "accepted",
				AssignedAt: "2026-01-02T03:00:03Z", SubmittedAt: "2026-01-02T03:01:05.003Z", AcceptedAt: "2026-01-02T04:01:05.003Z",
				AssignToHandoff: "1m2.003s", HandoffToAccept: "1h0m0s",
			},
		},
		{
			name:       "latest of several acceptances wins: rejected then unused",
			assignment: tlnAssignment("ver-1", "verification", "/root/verifier", tlnAt(4*time.Second)),
			results:    []taskflow.BoundWorkerResult{tlnResult("ver-1", "completed", tlnAt(5*time.Second))},
			acceptances: []taskflow.BoundWorkerAcceptance{
				tlnAcceptance("ver-1", "rejected", tlnAt(6*time.Second)),
				tlnAcceptance("zz-other", "accepted", tlnAt(12*time.Hour+30*time.Minute)),
				tlnAcceptance("ver-1", "unused", tlnAt(7500*time.Millisecond)),
			},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ver-1", Stage: "verification", Agent: "/root/verifier", State: "unused",
				AssignedAt: "2026-01-02T03:00:04Z", SubmittedAt: "2026-01-02T03:00:05Z", AcceptedAt: "2026-01-02T03:00:07.5Z",
				AssignToHandoff: "1s", HandoffToAccept: "2.5s",
			},
		},
		{
			// Fails if "latest" is chosen by the largest instant instead of the
			// last element in WorkerAcceptances order.
			name:       "latest is by slice order, not by time",
			assignment: tlnAssignment("ev-3", "evidence", "/root/worker", tlnAt(7*time.Second)),
			results:    []taskflow.BoundWorkerResult{tlnResult("ev-3", "cancelled", tlnAt(8*time.Second))},
			acceptances: []taskflow.BoundWorkerAcceptance{
				tlnAcceptance("ev-3", "accepted", tlnAt(20*time.Second)),
				tlnAcceptance("ev-3", "rejected", time.Time{}),
			},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ev-3", Stage: "evidence", Agent: "/root/worker", State: "rejected",
				AssignedAt: "2026-01-02T03:00:07Z", SubmittedAt: "2026-01-02T03:00:08Z", AcceptedAt: "n/a",
				AssignToHandoff: "1s", HandoffToAccept: "n/a",
			},
		},
		{
			name:        "acceptance without a result sets state and accepted_at only",
			assignment:  tlnAssignment("impl-2", "implementation", "/root/other worker", tlnAt(5*time.Second)),
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("impl-2", "unused", tlnAt(9*time.Second))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "impl-2", Stage: "implementation", Agent: "/root/other worker", State: "unused",
				AssignedAt: "2026-01-02T03:00:05Z", SubmittedAt: "n/a", AcceptedAt: "2026-01-02T03:00:09Z",
				AssignToHandoff: "n/a", HandoffToAccept: "n/a",
			},
		},
		{
			name:        "zero submitted_at is missing",
			assignment:  tlnAssignment("ev-2", "evidence", "/root/worker", tlnAt(6*time.Second)),
			results:     []taskflow.BoundWorkerResult{tlnResult("ev-2", "completed", time.Time{})},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("ev-2", "accepted", tlnAt(10*time.Second))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ev-2", Stage: "evidence", Agent: "/root/worker", State: "accepted",
				AssignedAt: "2026-01-02T03:00:06Z", SubmittedAt: "n/a", AcceptedAt: "2026-01-02T03:00:10Z",
				AssignToHandoff: "n/a", HandoffToAccept: "n/a",
			},
		},
		{
			name:        "zero assigned_at is missing; the handoff interval still measures",
			assignment:  tlnAssignment("ev-4", "evidence", "/root/worker", time.Time{}),
			results:     []taskflow.BoundWorkerResult{tlnResult("ev-4", "completed", tlnAt(30*time.Second))},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("ev-4", "accepted", tlnAt(31*time.Second))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ev-4", Stage: "evidence", Agent: "/root/worker", State: "accepted",
				AssignedAt: "n/a", SubmittedAt: "2026-01-02T03:00:30Z", AcceptedAt: "2026-01-02T03:00:31Z",
				AssignToHandoff: "n/a", HandoffToAccept: "1s",
			},
		},
		{
			// D3 (revision 3 domain: times in any order): negative entry
			// durations are written as computed, never clamped to 0s or n/a.
			name:        "submitted before assigned gives a negative assign_to_handoff",
			assignment:  tlnAssignment("neg-1", "evidence", "/root/worker", tlnAt(5*time.Second)),
			results:     []taskflow.BoundWorkerResult{tlnResult("neg-1", "completed", tlnAt(3*time.Second))},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("neg-1", "accepted", tlnAt(4500*time.Millisecond))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "neg-1", Stage: "evidence", Agent: "/root/worker", State: "accepted",
				AssignedAt: "2026-01-02T03:00:05Z", SubmittedAt: "2026-01-02T03:00:03Z", AcceptedAt: "2026-01-02T03:00:04.5Z",
				AssignToHandoff: "-2s", HandoffToAccept: "1.5s",
			},
		},
		{
			name:        "accepted before submitted gives a negative handoff_to_accept",
			assignment:  tlnAssignment("neg-2", "review", "/root/reviewer", tlnAt(time.Second)),
			results:     []taskflow.BoundWorkerResult{tlnResult("neg-2", "completed", tlnAt(4*time.Second))},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("neg-2", "rejected", tlnAt(2*time.Second))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "neg-2", Stage: "review", Agent: "/root/reviewer", State: "rejected",
				AssignedAt: "2026-01-02T03:00:01Z", SubmittedAt: "2026-01-02T03:00:04Z", AcceptedAt: "2026-01-02T03:00:02Z",
				AssignToHandoff: "3s", HandoffToAccept: "-2s",
			},
		},
		{
			name:        "instants stored in other locations are written in UTC",
			assignment:  tlnAssignment("ev-5", "evidence", "/root/worker", tlnAt(time.Second).In(tlnIST)),
			results:     []taskflow.BoundWorkerResult{tlnResult("ev-5", "completed", tlnAt(2*time.Second).In(tlnNST))},
			acceptances: []taskflow.BoundWorkerAcceptance{tlnAcceptance("ev-5", "accepted", tlnAt(2*time.Second).In(tlnChath))},
			want: taskflow.TimelineEntry{
				Kind: "assignment", ID: "ev-5", Stage: "evidence", Agent: "/root/worker", State: "accepted",
				AssignedAt: "2026-01-02T03:00:01Z", SubmittedAt: "2026-01-02T03:00:02Z", AcceptedAt: "2026-01-02T03:00:02Z",
				AssignToHandoff: "1s", HandoffToAccept: "0s",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := taskflow.Record{
				ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(24 * time.Hour),
				// The distractor comes first in every slice.
				Assignments:       []taskflow.WorkerAssignment{distractor, tc.assignment},
				WorkerResults:     append([]taskflow.BoundWorkerResult{distractorResult}, tc.results...),
				WorkerAcceptances: append([]taskflow.BoundWorkerAcceptance{distractorAcceptance}, tc.acceptances...),
			}
			got := tlnBuild(t, record)
			wantDistractor := taskflow.TimelineEntry{
				Kind: "assignment", ID: "zz-other", Stage: "evidence", Agent: "/root/distractor", State: "rejected",
				AssignedAt: "2026-01-02T13:00:00Z", SubmittedAt: "2026-01-02T14:00:00Z", AcceptedAt: "2026-01-02T15:00:00Z",
				AssignToHandoff: "1h0m0s", HandoffToAccept: "1h0m0s",
			}
			if tc.assignment.Brief.ID == "ver-1" {
				// The distractor's own later acceptance is the latest for it.
				wantDistractor.State = "accepted"
				wantDistractor.AcceptedAt = "2026-01-02T15:30:00Z"
				wantDistractor.HandoffToAccept = "1h30m0s"
			}
			want := []taskflow.TimelineEntry{tc.want, wantDistractor}
			if tc.assignment.AssignedAt.IsZero() {
				// A missing start sorts after every present start (TL-4).
				want = []taskflow.TimelineEntry{wantDistractor, tc.want}
			}
			tlnEqualEntries(t, got.Entries, want)
		})
	}
}

// TL-2: an assignment entry carries no check-run fields, in Go or in JSON.
func TestTimelineTL2AssignmentOmitsCheckFields(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{tlnAssignment("a1", "evidence", "/root/worker", tlnAt(time.Second))},
	}
	got := tlnBuild(t, record)
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	entry := got.Entries[0]
	if entry.StartedAt != "" || entry.FinishedAt != "" || entry.Duration != "" {
		t.Errorf("assignment entry has check fields: %+v", entry)
	}
	keys := tlnMarshalKeys(t, entry)
	want := []string{"accepted_at", "agent", "assign_to_handoff", "assigned_at", "handoff_to_accept", "id", "kind", "stage", "state", "submitted_at"}
	if !slices.Equal(keys, want) {
		t.Errorf("assignment JSON keys = %v, want %v", keys, want)
	}
}

// TL-3: one entry per check run with its status verbatim, start, finish and
// duration; a running check (nil finish) and a zero finish are n/a, and a
// missing start makes the duration n/a.
func TestTimelineTL3CheckEntries(t *testing.T) {
	const runID = "8f3a0c5e9d1b4a7f8e2c6d0b3a9f1e47"
	start := tlnAt(5 * time.Minute)
	cases := []struct {
		name string
		run  taskflow.CheckRun
		want taskflow.TimelineEntry
	}{
		{
			name: "passed",
			run:  tlnCheck(runID, "unit", "passed", start, tlnPtr(start.Add(1500*time.Millisecond))),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "passed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:05:01.5Z", Duration: "1.5s"},
		},
		{
			name: "failed",
			run:  tlnCheck(runID, "bad", "failed", start, tlnPtr(start.Add(time.Minute+2003*time.Millisecond))),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "bad", State: "failed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:06:02.003Z", Duration: "1m2.003s"},
		},
		{
			name: "running check has nil finish",
			run:  tlnCheck(runID, "unit", "running", start, nil),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "running",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "n/a", Duration: "n/a"},
		},
		{
			name: "zero finish is missing",
			run:  tlnCheck(runID, "unit", "passed", start, tlnPtr(time.Time{})),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "passed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "n/a", Duration: "n/a"},
		},
		{
			name: "zero start is missing",
			run:  tlnCheck(runID, "unit", "passed", time.Time{}, tlnPtr(start)),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "passed",
				StartedAt: "n/a", FinishedAt: "2026-01-02T03:05:00Z", Duration: "n/a"},
		},
		{
			name: "finish equal to start is a measured 0s",
			run:  tlnCheck(runID, "unit", "passed", start, tlnPtr(start)),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "passed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:05:00Z", Duration: "0s"},
		},
		{
			// D3: a finish before the start is a negative duration, not clamped.
			name: "finish before start gives a negative duration",
			run:  tlnCheck(runID, "unit", "failed", start, tlnPtr(start.Add(-2*time.Second))),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "failed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:04:58Z", Duration: "-2s"},
		},
		{
			name: "locations converted to UTC and compared as instants",
			run:  tlnCheck(runID, "unit", "passed", start.In(tlnIST), tlnPtr(start.Add(250*time.Millisecond).In(tlnNST))),
			want: taskflow.TimelineEntry{Kind: "check", ID: runID, Stage: "unit", State: "passed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:05:00.25Z", Duration: "250ms"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := taskflow.Record{ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
				Checks: []taskflow.CheckRun{tc.run}}
			got := tlnBuild(t, record)
			tlnEqualEntries(t, got.Entries, []taskflow.TimelineEntry{tc.want})
			if len(got.Entries) == 1 {
				keys := tlnMarshalKeys(t, got.Entries[0])
				wantKeys := []string{"duration", "finished_at", "id", "kind", "stage", "started_at", "state"}
				if !slices.Equal(keys, wantKeys) {
					t.Errorf("check JSON keys = %v, want %v", keys, wantKeys)
				}
			}
		})
	}
}

// TL-3 (last paragraph): assessments, consultations, decision, verification
// and abandonment yield no entry; only assignments and check runs do.
// The extra content is zero-valued, because only its presence matters here.
func TestTimelineTL3OtherContentYieldsNoEntry(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments:   []taskflow.WorkerAssignment{tlnAssignment("a1", "evidence", "/root/worker", tlnAt(time.Second))},
		Checks:        []taskflow.CheckRun{tlnCheck("0123456789abcdef0123456789abcdef", "ok", "passed", tlnAt(2*time.Second), tlnPtr(tlnAt(3*time.Second)))},
		Assessments:   map[string]taskflow.BoundAssessment{"r1": *new(taskflow.BoundAssessment)},
		Consultations: map[string]taskflow.Attachment{"c1": *new(taskflow.Attachment)},
		Decision:      new(taskflow.BoundDecision),
		Verification:  new(taskflow.Verification),
		Abandonment:   new(taskflow.Abandonment),
	}
	got := tlnBuild(t, record)
	if len(got.Entries) != 2 || got.Entries[0].Kind != "assignment" || got.Entries[1].Kind != "check" {
		t.Fatalf("entries = %+v, want exactly the assignment then the check", got.Entries)
	}
}

// TL-4: entries are ordered by start instant, not by text. "03:00:00.5Z"
// sorts before "03:00:00Z" as text, and local text of other zones sorts
// differently again, so a text sort fails this test.
func TestTimelineTL4OrderByInstantNotText(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{
			tlnAssignment("a", "evidence", "/root/a", tlnAt(time.Second).In(tlnNST)),
			tlnAssignment("z", "evidence", "/root/z", tlnBase),
		},
		Checks: []taskflow.CheckRun{
			tlnCheck("ffffffffffffffffffffffffffffffff", "ok", "passed", tlnAt(2*time.Second).In(tlnIST), nil),
			tlnCheck("00000000000000000000000000000001", "ok", "passed", tlnAt(500*time.Millisecond), nil),
		},
	}
	got := tlnBuild(t, record)
	var ids []string
	for _, entry := range got.Entries {
		ids = append(ids, entry.ID)
	}
	want := []string{"z", "00000000000000000000000000000001", "a", "ffffffffffffffffffffffffffffffff"}
	if !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// TL-4: equal start instants (some written in other locations) order
// assignments before checks, then by position in the record, the order the
// entries were written in. The check ID "0..." sorts before every assignment
// ID, and no kind's IDs are in byte order, so a missing kind tie-break or an
// ID comparison fails.
func TestTimelineTL4TieBreaks(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{
			tlnAssignment("b", "evidence", "/root/b", tlnBase.In(tlnIST)),
			tlnAssignment("a-1", "evidence", "/root/a-1", tlnBase),
			tlnAssignment("a", "review", "/root/a", tlnBase.In(tlnNST)),
			tlnAssignment("B", "implementation", "/root/B", tlnBase.In(tlnChath)),
		},
		Checks: []taskflow.CheckRun{
			tlnCheck("ffffffffffffffffffffffffffffffff", "ok", "running", tlnBase, nil),
			tlnCheck("00000000000000000000000000000000", "ok", "running", tlnBase.In(tlnIST), nil),
			tlnCheck("a0000000000000000000000000000000", "ok", "running", tlnBase.In(tlnNST), nil),
		},
	}
	got := tlnBuild(t, record)
	var ids []string
	for _, entry := range got.Entries {
		ids = append(ids, entry.ID)
	}
	want := []string{"b", "a-1", "a", "B", "ffffffffffffffffffffffffffffffff", "00000000000000000000000000000000", "a0000000000000000000000000000000"}
	if !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// TL-4: entries with a missing start come after every present start, even one
// far in the future, ordered assignment before check, then by position in
// the record. A sort that treats the zero time as the earliest instant fails.
func TestTimelineTL4MissingStartLast(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{
			tlnAssignment("b", "evidence", "/root/b", time.Time{}),
			tlnAssignment("z", "evidence", "/root/z", tlnAt(time.Second)),
			tlnAssignment("a", "evidence", "/root/a", time.Time{}.In(tlnIST)),
		},
		Checks: []taskflow.CheckRun{
			tlnCheck("abcabcabcabcabcabcabcabcabcabcab", "ok", "passed", time.Time{}, tlnPtr(tlnAt(time.Minute))),
			tlnCheck("ffffffffffffffffffffffffffffffff", "ok", "running", tlnAt(100*time.Hour), nil),
			tlnCheck("00000000000000000000000000000000", "ok", "running", time.Time{}, nil),
		},
	}
	got := tlnBuild(t, record)
	var ids []string
	for _, entry := range got.Entries {
		ids = append(ids, entry.ID)
	}
	want := []string{"z", "ffffffffffffffffffffffffffffffff", "b", "a", "abcabcabcabcabcabcabcabcabcabcab", "00000000000000000000000000000000"}
	if !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// tlnFixedRecord is a record covering every entry kind and state; it is
// rebuilt from scratch on every call so tests can compare a used record with
// an untouched twin.
func tlnFixedRecord() taskflow.Record {
	return taskflow.Record{
		Version: 1, ID: tlnTaskID, Directory: "/d", Revision: 9,
		CreatedAt: tlnBase.In(tlnIST), UpdatedAt: tlnAt(2 * time.Hour).In(tlnNST),
		Assignments: []taskflow.WorkerAssignment{
			tlnAssignment("c", "review", "/root/c", tlnAt(3*time.Second)),
			tlnAssignment("a", "evidence", "/root/a", tlnAt(time.Second).In(tlnChath)),
			tlnAssignment("b", "implementation", "/root/b", time.Time{}),
		},
		WorkerResults: []taskflow.BoundWorkerResult{
			tlnResult("c", "completed", tlnAt(4*time.Second)),
			tlnResult("a", "completed", tlnAt(2*time.Second)),
		},
		WorkerAcceptances: []taskflow.BoundWorkerAcceptance{
			tlnAcceptance("a", "rejected", tlnAt(5*time.Second)),
			tlnAcceptance("c", "accepted", tlnAt(6*time.Second)),
			tlnAcceptance("a", "unused", tlnAt(7*time.Second)),
		},
		Checks: []taskflow.CheckRun{
			tlnCheck("ffffffffffffffffffffffffffffffff", "bad", "failed", tlnAt(8*time.Second), tlnPtr(tlnAt(9*time.Second))),
			tlnCheck("00000000000000000000000000000000", "ok", "running", tlnAt(time.Second), nil),
		},
	}
}

// TL-5: BuildTimeline leaves its argument as it was: slices keep their
// elements and order (an in-place sort of the shared backing arrays fails).
func TestTimelineTL5DoesNotModifyRecord(t *testing.T) {
	record := tlnFixedRecord()
	assignments, results, acceptances, checks := record.Assignments, record.WorkerResults, record.WorkerAcceptances, record.Checks
	tlnBuild(t, record)
	twin := tlnFixedRecord()
	if !reflect.DeepEqual(record, twin) {
		t.Errorf("record changed by BuildTimeline:\n got: %+v\nwant: %+v", record, twin)
	}
	if !reflect.DeepEqual(assignments, twin.Assignments) || !reflect.DeepEqual(results, twin.WorkerResults) ||
		!reflect.DeepEqual(acceptances, twin.WorkerAcceptances) || !reflect.DeepEqual(checks, twin.Checks) {
		t.Error("BuildTimeline reordered or changed the record's slices")
	}
}

// TL-5: the result does not depend on time.Local (the in-process view of the
// TZ environment), and repeated calls give equal timelines and summaries.
func TestTimelineTL5IndependentOfLocalZoneAndRepeatable(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })

	time.Local = time.UTC
	first := tlnBuild(t, tlnFixedRecord())
	firstLines := tlnSummary(t, first)

	time.Local = tlnChath
	second := tlnBuild(t, tlnFixedRecord())
	secondLines := tlnSummary(t, second)

	time.Local = tlnNST
	third := tlnBuild(t, tlnFixedRecord())

	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, third) {
		t.Errorf("timeline depends on time.Local:\n UTC: %+v\nCHAST: %+v\n NST: %+v", first, second, third)
	}
	if !slices.Equal(firstLines, secondLines) {
		t.Errorf("summary depends on time.Local:\n%q\n%q", firstLines, secondLines)
	}
	if first.StartedAt != "2026-01-02T03:00:00Z" || first.UpdatedAt != "2026-01-02T05:00:00Z" || first.Elapsed != "2h0m0s" {
		t.Errorf("task fields = %q %q %q, want UTC text and 2h0m0s", first.StartedAt, first.UpdatedAt, first.Elapsed)
	}
}

// TL-5 example: reversing the assignment and check slices does not change
// the result.
func TestTimelineTL5ReorderedInputSameResult(t *testing.T) {
	want := tlnBuild(t, tlnFixedRecord())
	reordered := tlnFixedRecord()
	slices.Reverse(reordered.Assignments)
	slices.Reverse(reordered.Checks)
	got := tlnBuild(t, reordered)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reordered input changed the timeline:\n got: %+v\nwant: %+v", got, want)
	}
	// Hand-checked order of tlnFixedRecord: a (1s), check 000.. (1s), c (3s),
	// check fff.. (8s), b (missing start).
	var ids []string
	for _, entry := range got.Entries {
		ids = append(ids, entry.ID)
	}
	wantIDs := []string{"a", "00000000000000000000000000000000", "c", "ffffffffffffffffffffffffffffffff", "b"}
	if !slices.Equal(ids, wantIDs) {
		t.Errorf("order = %v, want %v", ids, wantIDs)
	}
}

// TL-6: the contract's own example, byte for byte.
func TestTimelineTL6ContractExample(t *testing.T) {
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/tasks/" + tlnTaskID,
		CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour + 2*time.Minute + 3*time.Second),
		Assignments:   []taskflow.WorkerAssignment{tlnAssignment("impl-1", "implementation", "/root/implementer", tlnAt(time.Second))},
		WorkerResults: []taskflow.BoundWorkerResult{tlnResult("impl-1", "completed", tlnAt(12*time.Minute+1500*time.Millisecond))},
		Checks:        []taskflow.CheckRun{tlnCheck("8f3a0c5e9d1b4a7f8e2c6d0b3a9f1e47", "unit", "running", tlnAt(5*time.Minute), nil)},
	}
	got := tlnSummary(t, tlnBuild(t, record))
	want := []string{
		`{"task_id":"0123456789abcdef0123456789abcdef","started_at":"2026-01-02T03:00:00Z","updated_at":"2026-01-02T04:02:03Z","elapsed":"1h2m3s"}`,
		`{"kind":"assignment","id":"impl-1","stage":"implementation","agent":"/root/implementer","state":"completed","start":"2026-01-02T03:00:01Z","assign_to_handoff":"12m0.5s","handoff_to_accept":"n/a"}`,
		`{"kind":"check","id":"8f3a0c5e9d1b4a7f8e2c6d0b3a9f1e47","stage":"unit","state":"running","start":"2026-01-02T03:05:00Z","duration":"n/a"}`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("SummaryLines:\n got: %q\nwant: %q", got, want)
	}
}

// TL-6: SummaryLines maps a Timeline's own field text (built here by hand, not
// by BuildTimeline): it omits task, submitted_at, accepted_at and
// finished_at, writes start from assigned_at or started_at, and keeps n/a.
func TestTimelineTL6SummaryFromTimelineFields(t *testing.T) {
	timeline := taskflow.Timeline{
		Task: "/tasks/must-not-appear", TaskID: "fedcba9876543210fedcba9876543210",
		StartedAt: "2026-01-02T03:00:00.5Z", UpdatedAt: "n/a", Elapsed: "n/a",
		Entries: []taskflow.TimelineEntry{
			{Kind: "assignment", ID: "ev-1", Stage: "evidence", Agent: "/root/other worker", State: "accepted",
				AssignedAt: "2026-01-02T03:00:03Z", SubmittedAt: "2026-01-02T03:01:05.003Z", AcceptedAt: "2026-01-02T04:01:05.003Z",
				AssignToHandoff: "1m2.003s", HandoffToAccept: "1h0m0s"},
			{Kind: "check", ID: "00000000000000000000000000000000", Stage: "unit", State: "passed",
				StartedAt: "2026-01-02T03:05:00Z", FinishedAt: "2026-01-02T03:05:01.5Z", Duration: "1.5s"},
			{Kind: "assignment", ID: "b", Stage: "review", Agent: "/root/b", State: "assigned",
				AssignedAt: "n/a", SubmittedAt: "n/a", AcceptedAt: "n/a", AssignToHandoff: "n/a", HandoffToAccept: "n/a"},
			{Kind: "check", ID: "ffffffffffffffffffffffffffffffff", Stage: "bad", State: "failed",
				StartedAt: "n/a", FinishedAt: "2026-01-02T03:06:00Z", Duration: "n/a"},
		},
	}
	got := tlnSummary(t, timeline)
	want := []string{
		`{"task_id":"fedcba9876543210fedcba9876543210","started_at":"2026-01-02T03:00:00.5Z","updated_at":"n/a","elapsed":"n/a"}`,
		`{"kind":"assignment","id":"ev-1","stage":"evidence","agent":"/root/other worker","state":"accepted","start":"2026-01-02T03:00:03Z","assign_to_handoff":"1m2.003s","handoff_to_accept":"1h0m0s"}`,
		`{"kind":"check","id":"00000000000000000000000000000000","stage":"unit","state":"passed","start":"2026-01-02T03:05:00Z","duration":"1.5s"}`,
		`{"kind":"assignment","id":"b","stage":"review","agent":"/root/b","state":"assigned","start":"n/a","assign_to_handoff":"n/a","handoff_to_accept":"n/a"}`,
		`{"kind":"check","id":"ffffffffffffffffffffffffffffffff","stage":"bad","state":"failed","start":"n/a","duration":"n/a"}`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("SummaryLines:\n got: %q\nwant: %q", got, want)
	}
}

// TL-6: a timeline without entries summarises as the task line alone.
func TestTimelineTL6EmptyTimelineOneLine(t *testing.T) {
	record := taskflow.Record{ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(250 * time.Millisecond)}
	got := tlnSummary(t, tlnBuild(t, record))
	want := []string{`{"task_id":"0123456789abcdef0123456789abcdef","started_at":"2026-01-02T03:00:00Z","updated_at":"2026-01-02T03:00:00.25Z","elapsed":"250ms"}`}
	if !slices.Equal(got, want) {
		t.Errorf("SummaryLines = %q, want %q", got, want)
	}
}

// TL-6: an agent ID may hold inner spaces, quotes, backslashes and non-ASCII
// text; each line still decodes as one single-line JSON object with string
// values that round-trip, in the contract's key order.
func TestTimelineTL6EscapedAgentRoundTrips(t *testing.T) {
	agent := `/root/a "quoted" \ agent é`
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{tlnAssignment("a1", "evidence", agent, tlnAt(time.Second))},
	}
	lines := tlnSummary(t, tlnBuild(t, record))
	if len(lines) != 2 {
		t.Fatalf("SummaryLines = %q, want 2 lines", lines)
	}
	if strings.ContainsAny(lines[1], "\n\r") {
		t.Errorf("line %q spans more than one line", lines[1])
	}
	keys, values := tlnObjectKeys(t, lines[1])
	wantKeys := []string{"kind", "id", "stage", "agent", "state", "start", "assign_to_handoff", "handoff_to_accept"}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("keys = %v, want %v", keys, wantKeys)
	}
	if values["agent"] != agent {
		t.Errorf("agent = %q, want %q", values["agent"], agent)
	}
}

// TL-6 encoder pin: the summary uses encoding/json/v2 with
// json.Deterministic(true), whose default escaping writes `<`, `>`, `&`,
// U+2028 and non-ASCII text as is and escapes only `"` and `\`. The expected
// bytes were checked by encoding a plain struct of strings with that encoder
// in a scratch program. Fails if the summary HTML-escapes `<>&` or escapes
// U+2028 (as encoding/json v1 does), or uses any other encoder's escaping.
func TestTimelineTL6EncoderEscapingBytes(t *testing.T) {
	agent := "/root/<a> & b x \"q\" \\ é"
	record := taskflow.Record{
		ID: tlnTaskID, Directory: "/d", CreatedAt: tlnBase, UpdatedAt: tlnAt(time.Hour),
		Assignments: []taskflow.WorkerAssignment{tlnAssignment("a1", "evidence", agent, tlnAt(time.Second))},
	}
	lines := tlnSummary(t, tlnBuild(t, record))
	want := []string{
		`{"task_id":"0123456789abcdef0123456789abcdef","started_at":"2026-01-02T03:00:00Z","updated_at":"2026-01-02T04:00:00Z","elapsed":"1h0m0s"}`,
		"{\"kind\":\"assignment\",\"id\":\"a1\",\"stage\":\"evidence\",\"agent\":\"/root/<a> & b x \\\"q\\\" \\\\ é\",\"state\":\"assigned\",\"start\":\"2026-01-02T03:00:01Z\",\"assign_to_handoff\":\"n/a\",\"handoff_to_accept\":\"n/a\"}",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("SummaryLines:\n got: %q\nwant: %q", lines, want)
	}
}

// ---- Property tests over the input domain (TL-1 to TL-6, D1 to D3) ----

var (
	tlnRoles             = []string{"implementation", "evidence", "review", "verification"}
	tlnResultStatuses    = []string{"completed", "blocked", "cancelled"}
	tlnAcceptStatuses    = []string{"accepted", "rejected", "unused"}
	tlnCheckStatuses     = []string{"running", "passed", "failed"}
	tlnAssignmentIDPool  = []string{"a", "B", "b", "a-1", "Z9", "impl_2", "a1", "x"}
	tlnSteps             = []time.Duration{0, 0, time.Nanosecond, 499999 * time.Nanosecond, 1500 * time.Microsecond, 500 * time.Millisecond, time.Second, time.Minute + 2003*time.Millisecond, time.Hour}
	tlnPropertyLocations = []*time.Location{time.UTC, tlnIST, tlnNST, tlnChath}
)

// tlnRandomRecord builds a record in the contract's input domain from a seed:
// events happen on a clock that mostly advances by steps that include zero
// (so start ties occur) and sub-millisecond amounts (so rounding is
// exercised), and sometimes steps backwards (revision 3 allows times in any
// order, so entry durations can be negative per D3); any timestamp may then
// be zero, stored in any location.
// Acceptances may name an assignment without a result. Assignment and check
// slices are shuffled, which TL-5 allows.
func tlnRandomRecord(seed uint64) taskflow.Record {
	r := rand.New(rand.NewPCG(seed, 0x74696d656c696e65))
	clock := time.Date(2026, 3, 4, 5, 6, 7, int(r.IntN(1_000_000_000)), time.UTC)
	tick := func() time.Time {
		step := tlnSteps[r.IntN(len(tlnSteps))]
		if r.IntN(5) == 0 {
			step = -step
		}
		clock = clock.Add(step)
		at := clock.In(tlnPropertyLocations[r.IntN(len(tlnPropertyLocations))])
		if r.IntN(6) == 0 {
			return time.Time{}.In(tlnPropertyLocations[r.IntN(len(tlnPropertyLocations))])
		}
		return at
	}
	record := taskflow.Record{Version: 1, ID: fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64()), Directory: "/tasks/property"}
	record.CreatedAt = tick()

	idOrder := r.Perm(len(tlnAssignmentIDPool))
	assignmentCount := r.IntN(6)
	checkCount := r.IntN(5)
	var assigned []string
	hasResult := map[string]bool{}
	nextAssignment, nextCheck := 0, 0
	for events := 0; events < 40; events++ {
		switch r.IntN(4) {
		case 0:
			if nextAssignment < assignmentCount {
				id := tlnAssignmentIDPool[idOrder[nextAssignment]]
				nextAssignment++
				record.Assignments = append(record.Assignments,
					tlnAssignment(id, tlnRoles[r.IntN(len(tlnRoles))], "/root/agent "+id, tick()))
				assigned = append(assigned, id)
			}
		case 1:
			if len(assigned) > 0 {
				id := assigned[r.IntN(len(assigned))]
				if !hasResult[id] {
					hasResult[id] = true
					record.WorkerResults = append(record.WorkerResults,
						tlnResult(id, tlnResultStatuses[r.IntN(len(tlnResultStatuses))], tick()))
				}
			}
		case 2:
			if len(assigned) > 0 && len(record.WorkerAcceptances) < 8 {
				id := assigned[r.IntN(len(assigned))]
				record.WorkerAcceptances = append(record.WorkerAcceptances,
					tlnAcceptance(id, tlnAcceptStatuses[r.IntN(len(tlnAcceptStatuses))], tick()))
			}
		case 3:
			if nextCheck < checkCount {
				nextCheck++
				id := fmt.Sprintf("%016x%016x", r.Uint64()&0xff, r.Uint64())
				start := tick()
				var finish *time.Time
				switch r.IntN(3) {
				case 0:
					finish = nil
				case 1:
					finish = tlnPtr(tick())
				default:
					if start.IsZero() {
						finish = tlnPtr(tick())
					} else {
						step := tlnSteps[r.IntN(len(tlnSteps))]
						if r.IntN(4) == 0 {
							step = -step
						}
						finish = tlnPtr(start.Add(step))
					}
				}
				record.Checks = append(record.Checks,
					tlnCheck(id, []string{"ok", "bad", "unit"}[r.IntN(3)], tlnCheckStatuses[r.IntN(len(tlnCheckStatuses))], start, finish))
			}
		}
	}
	record.UpdatedAt = tick()
	r.Shuffle(len(record.Assignments), func(i, j int) {
		record.Assignments[i], record.Assignments[j] = record.Assignments[j], record.Assignments[i]
	})
	r.Shuffle(len(record.Checks), func(i, j int) { record.Checks[i], record.Checks[j] = record.Checks[j], record.Checks[i] })
	return record
}

// Independent model of D1 to D3 and TL-1 to TL-4, written from the contract.

func tlnModelTime(at time.Time) string {
	if at.IsZero() {
		return "n/a"
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func tlnModelDuration(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return "n/a"
	}
	return to.Sub(from).Round(time.Millisecond).String()
}

type tlnModelEntry struct {
	start time.Time
	entry taskflow.TimelineEntry
}

func tlnModel(record taskflow.Record) taskflow.Timeline {
	var model []tlnModelEntry
	for _, assignment := range record.Assignments {
		id := assignment.Brief.ID
		state := "assigned"
		var submitted, accepted time.Time
		for _, result := range record.WorkerResults {
			if result.Result.AssignmentID == id {
				state = result.Result.Status
				submitted = result.SubmittedAt
			}
		}
		for _, acceptance := range record.WorkerAcceptances {
			if acceptance.Acceptance.AssignmentID == id {
				state = acceptance.Acceptance.Status
				accepted = acceptance.AcceptedAt
			}
		}
		model = append(model, tlnModelEntry{start: assignment.AssignedAt, entry: taskflow.TimelineEntry{
			Kind: "assignment", ID: id, Stage: assignment.Brief.Role, Agent: assignment.Brief.AgentID, State: state,
			AssignedAt: tlnModelTime(assignment.AssignedAt), SubmittedAt: tlnModelTime(submitted), AcceptedAt: tlnModelTime(accepted),
			AssignToHandoff: tlnModelDuration(assignment.AssignedAt, submitted), HandoffToAccept: tlnModelDuration(submitted, accepted),
		}})
	}
	for _, run := range record.Checks {
		var finished time.Time
		if run.FinishedAt != nil {
			finished = *run.FinishedAt
		}
		model = append(model, tlnModelEntry{start: run.StartedAt, entry: taskflow.TimelineEntry{
			Kind: "check", ID: run.ID, Stage: run.CheckID, State: run.Status,
			StartedAt: tlnModelTime(run.StartedAt), FinishedAt: tlnModelTime(finished), Duration: tlnModelDuration(run.StartedAt, finished),
		}})
	}
	kindRank := map[string]int{"assignment": 0, "check": 1}
	// Stable: entries equal in start and kind keep the record's order.
	slices.SortStableFunc(model, func(a, b tlnModelEntry) int {
		aMissing, bMissing := a.start.IsZero(), b.start.IsZero()
		switch {
		case !aMissing && bMissing:
			return -1
		case aMissing && !bMissing:
			return 1
		case !aMissing:
			if c := a.start.Compare(b.start); c != 0 {
				return c
			}
		}
		return kindRank[a.entry.Kind] - kindRank[b.entry.Kind]
	})
	entries := []taskflow.TimelineEntry{}
	for _, item := range model {
		entries = append(entries, item.entry)
	}
	return taskflow.Timeline{
		Task: record.Directory, TaskID: record.ID,
		StartedAt: tlnModelTime(record.CreatedAt), UpdatedAt: tlnModelTime(record.UpdatedAt),
		Elapsed: tlnModelDuration(record.CreatedAt, record.UpdatedAt), Entries: entries,
	}
}

// tlnInLocation returns a copy of record with every timestamp moved to loc;
// the instants (and zero values) stay the same.
func tlnInLocation(record taskflow.Record, loc *time.Location) taskflow.Record {
	move := func(at time.Time) time.Time { return at.In(loc) }
	record.CreatedAt, record.UpdatedAt = move(record.CreatedAt), move(record.UpdatedAt)
	record.Assignments = slices.Clone(record.Assignments)
	for i := range record.Assignments {
		record.Assignments[i].AssignedAt = move(record.Assignments[i].AssignedAt)
	}
	record.WorkerResults = slices.Clone(record.WorkerResults)
	for i := range record.WorkerResults {
		record.WorkerResults[i].SubmittedAt = move(record.WorkerResults[i].SubmittedAt)
	}
	record.WorkerAcceptances = slices.Clone(record.WorkerAcceptances)
	for i := range record.WorkerAcceptances {
		record.WorkerAcceptances[i].AcceptedAt = move(record.WorkerAcceptances[i].AcceptedAt)
	}
	record.Checks = slices.Clone(record.Checks)
	for i := range record.Checks {
		record.Checks[i].StartedAt = move(record.Checks[i].StartedAt)
		if record.Checks[i].FinishedAt != nil {
			record.Checks[i].FinishedAt = tlnPtr(move(*record.Checks[i].FinishedAt))
		}
	}
	return record
}

const tlnPropertySeeds = 400

// TL-1 to TL-4 with D1 to D3 over the domain: BuildTimeline equals the
// contract model for every generated record, which covers one entry per
// assignment and run, the n/a rule (n/a exactly when an endpoint is missing,
// never 0s for a missing one), UTC text, millisecond rounding and the full
// ordering rule including ties and missing starts.
func TestTimelinePropertyMatchesContractModel(t *testing.T) {
	negatives := map[string]int{}
	for seed := uint64(1); seed <= tlnPropertySeeds; seed++ {
		record := tlnRandomRecord(seed)
		got := tlnBuild(t, record)
		want := tlnModel(tlnRandomRecord(seed))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: BuildTimeline differs from the contract model\nrecord: %+v\n got: %+v\nwant: %+v", seed, record, got, want)
		}
		for _, entry := range want.Entries {
			for field, value := range map[string]string{"assign_to_handoff": entry.AssignToHandoff, "handoff_to_accept": entry.HandoffToAccept, "duration": entry.Duration} {
				if strings.HasPrefix(value, "-") {
					negatives[field]++
				}
			}
		}
	}
	// The generator must reach negative entry durations (D3), or the
	// property says nothing about them.
	for _, field := range []string{"assign_to_handoff", "handoff_to_accept", "duration"} {
		if negatives[field] == 0 {
			t.Errorf("generator produced no negative %s; widen it", field)
		}
	}
}

// TL-5 over the domain: moving every timestamp to another location, or
// calling twice, gives an equal timeline, and the argument is left unchanged.
// Shuffling the assignment and check slices keeps every entry and moves only
// entries equal in start and kind, which follow the record's order: the
// result is the contract model of the shuffled record.
func TestTimelinePropertyDeterministicAndPure(t *testing.T) {
	t.Parallel()
	for seed := uint64(1); seed <= tlnPropertySeeds; seed++ {
		record := tlnRandomRecord(seed)
		first := tlnBuild(t, record)
		if !reflect.DeepEqual(record, tlnRandomRecord(seed)) {
			t.Fatalf("seed %d: BuildTimeline modified its argument", seed)
		}
		if again := tlnBuild(t, record); !reflect.DeepEqual(first, again) {
			t.Fatalf("seed %d: second call differs:\n%+v\n%+v", seed, first, again)
		}
		r := rand.New(rand.NewPCG(seed, 1))
		shuffled := tlnRandomRecord(seed)
		r.Shuffle(len(shuffled.Assignments), func(i, j int) {
			shuffled.Assignments[i], shuffled.Assignments[j] = shuffled.Assignments[j], shuffled.Assignments[i]
		})
		r.Shuffle(len(shuffled.Checks), func(i, j int) { shuffled.Checks[i], shuffled.Checks[j] = shuffled.Checks[j], shuffled.Checks[i] })
		got := tlnBuild(t, shuffled)
		if want := tlnModel(shuffled); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: the timeline of the reordered record differs from the contract model:\n%+v\n%+v", seed, got, want)
		}
		byID := func(a, b taskflow.TimelineEntry) int {
			return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.ID, b.ID))
		}
		before, after := slices.Clone(first.Entries), got.Entries
		slices.SortFunc(before, byID)
		slices.SortFunc(after, byID)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("seed %d: reordering assignments or checks changed an entry:\n%+v\n%+v", seed, before, after)
		}
		for _, loc := range tlnPropertyLocations {
			if got := tlnBuild(t, tlnInLocation(tlnRandomRecord(seed), loc)); !reflect.DeepEqual(first, got) {
				t.Fatalf("seed %d: moving timestamps to %s changed the timeline:\n%+v\n%+v", seed, loc, first, got)
			}
		}
	}
}

// TL-6 over the domain: SummaryLines has 1+len(Entries) single-line objects
// with string values only, keys in the contract order and values equal to the
// timeline's field text, with start taken from assigned_at or started_at.
func TestTimelinePropertySummaryLines(t *testing.T) {
	taskKeys := []string{"task_id", "started_at", "updated_at", "elapsed"}
	assignmentKeys := []string{"kind", "id", "stage", "agent", "state", "start", "assign_to_handoff", "handoff_to_accept"}
	checkKeys := []string{"kind", "id", "stage", "state", "start", "duration"}
	for seed := uint64(1); seed <= tlnPropertySeeds; seed++ {
		want := tlnModel(tlnRandomRecord(seed))
		lines := tlnSummary(t, tlnBuild(t, tlnRandomRecord(seed)))
		if len(lines) != 1+len(want.Entries) {
			t.Fatalf("seed %d: %d lines, want %d", seed, len(lines), 1+len(want.Entries))
		}
		for i, line := range lines {
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("seed %d: line %d %q is not a single line", seed, i, line)
			}
		}
		keys, values := tlnObjectKeys(t, lines[0])
		if !slices.Equal(keys, taskKeys) {
			t.Fatalf("seed %d: task line keys %v, want %v", seed, keys, taskKeys)
		}
		if values["task_id"] != want.TaskID || values["started_at"] != want.StartedAt || values["updated_at"] != want.UpdatedAt || values["elapsed"] != want.Elapsed {
			t.Fatalf("seed %d: task line %q, want values from %+v", seed, lines[0], want)
		}
		for i, entry := range want.Entries {
			line := lines[i+1]
			keys, values := tlnObjectKeys(t, line)
			var wantValues map[string]string
			var wantKeys []string
			if entry.Kind == "assignment" {
				wantKeys = assignmentKeys
				wantValues = map[string]string{"kind": entry.Kind, "id": entry.ID, "stage": entry.Stage, "agent": entry.Agent,
					"state": entry.State, "start": entry.AssignedAt, "assign_to_handoff": entry.AssignToHandoff, "handoff_to_accept": entry.HandoffToAccept}
			} else {
				wantKeys = checkKeys
				wantValues = map[string]string{"kind": entry.Kind, "id": entry.ID, "stage": entry.Stage,
					"state": entry.State, "start": entry.StartedAt, "duration": entry.Duration}
			}
			if !slices.Equal(keys, wantKeys) {
				t.Fatalf("seed %d: line %q keys %v, want %v", seed, line, keys, wantKeys)
			}
			if !reflect.DeepEqual(values, wantValues) {
				t.Fatalf("seed %d: line %q values %v, want %v", seed, line, values, wantValues)
			}
		}
	}
}
