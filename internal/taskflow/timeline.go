package taskflow

import (
	"cmp"
	"slices"
	"time"
)

// NotAvailable stands in for a timestamp the record does not hold and for
// every duration computed from one. A missing value is never shown as zero.
const NotAvailable = "n/a"

// Timeline is the wall-time report of one task record that 'cw task timeline'
// prints. Every timestamp is UTC RFC 3339 with nanoseconds (time.RFC3339Nano)
// and every duration is rounded to the millisecond and written as
// time.Duration.String does; either is NotAvailable when an endpoint is missing.
type Timeline struct {
	// Task is the record's task directory and TaskID its task ID.
	Task   string `json:"task"`
	TaskID string `json:"task_id"`
	// StartedAt is the record's created_at and UpdatedAt its updated_at;
	// Elapsed is the duration from the first to the second.
	StartedAt string `json:"started_at"`
	UpdatedAt string `json:"updated_at"`
	Elapsed   string `json:"elapsed"`
	// Entries holds one entry per worker assignment and one per check run,
	// ordered by start time; it is empty, never nil, when there are none.
	Entries []TimelineEntry `json:"entries"`
	// OmittedEntries counts the earlier entries that Last left out.
	OmittedEntries int `json:"omitted_entries,omitzero"`
}

// Last keeps the last n entries, the latest by start time, and counts the
// earlier ones in OmittedEntries. n of 0 or less keeps every entry.
func (timeline Timeline) Last(n int) Timeline {
	if n <= 0 || len(timeline.Entries) <= n {
		return timeline
	}
	timeline.OmittedEntries += len(timeline.Entries) - n
	timeline.Entries = slices.Clone(timeline.Entries[len(timeline.Entries)-n:])
	return timeline
}

// TimelineEntry is one assignment (Kind "assignment") or one check run (Kind
// "check"). The fields of the other kind are empty and omitted from JSON.
type TimelineEntry struct {
	Kind string `json:"kind"`
	// ID is the assignment ID or the check run ID.
	ID string `json:"id"`
	// Stage is the assignment's role or the run's plan check ID.
	Stage string `json:"stage"`
	// Agent is the assignment's agent ID.
	Agent string `json:"agent,omitempty"`
	// State is the status of the assignment's latest acceptance, else of its
	// result, else "assigned"; for a check run, the run's status.
	State string `json:"state"`
	// Assignment timestamps and durations: assigned_at → submitted_at
	// (handoff) → accepted_at (latest acceptance).
	AssignedAt      string `json:"assigned_at,omitempty"`
	SubmittedAt     string `json:"submitted_at,omitempty"`
	AcceptedAt      string `json:"accepted_at,omitempty"`
	AssignToHandoff string `json:"assign_to_handoff,omitempty"`
	HandoffToAccept string `json:"handoff_to_accept,omitempty"`
	// Check run timestamps and duration.
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Duration   string `json:"duration,omitempty"`
}

// BuildTimeline reports the wall time of record from the timestamps it already
// stores. It reads record only and never consults the current time.
func BuildTimeline(record Record) Timeline {
	results := make(map[string]BoundWorkerResult, len(record.WorkerResults))
	for _, result := range record.WorkerResults {
		if _, seen := results[result.Result.AssignmentID]; !seen {
			results[result.Result.AssignmentID] = result
		}
	}
	// A later acceptance of the same assignment replaces an earlier one.
	acceptances := make(map[string]BoundWorkerAcceptance, len(record.WorkerAcceptances))
	for _, acceptance := range record.WorkerAcceptances {
		acceptances[acceptance.Acceptance.AssignmentID] = acceptance
	}
	items := make([]timelineItem, 0, len(record.Assignments)+len(record.Checks))
	for _, assignment := range record.Assignments {
		items = append(items, assignmentItem(assignment, results, acceptances))
	}
	for _, run := range record.Checks {
		items = append(items, checkItem(run))
	}
	slices.SortStableFunc(items, compareTimelineItems)
	entries := make([]TimelineEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, item.entry)
	}
	return Timeline{
		Task:      record.Directory,
		TaskID:    record.ID,
		StartedAt: timelineInstant(record.CreatedAt),
		UpdatedAt: timelineInstant(record.UpdatedAt),
		Elapsed:   timelineDuration(record.CreatedAt, record.UpdatedAt),
		Entries:   entries,
	}
}

const (
	timelineAssignment = "assignment"
	timelineCheck      = "check"
)

// timelineItem carries an entry with the start instant it is ordered by; a
// zero start is a missing one.
type timelineItem struct {
	entry TimelineEntry
	start time.Time
}

func assignmentItem(assignment WorkerAssignment, results map[string]BoundWorkerResult, acceptances map[string]BoundWorkerAcceptance) timelineItem {
	id := assignment.Brief.ID
	state := "assigned"
	var submitted, accepted time.Time
	if result, ok := results[id]; ok {
		state = result.Result.Status
		submitted = result.SubmittedAt
	}
	if acceptance, ok := acceptances[id]; ok {
		state = acceptance.Acceptance.Status
		accepted = acceptance.AcceptedAt
	}
	return timelineItem{start: assignment.AssignedAt, entry: TimelineEntry{
		Kind:            timelineAssignment,
		ID:              id,
		Stage:           assignment.Brief.Role,
		Agent:           assignment.Brief.AgentID,
		State:           state,
		AssignedAt:      timelineInstant(assignment.AssignedAt),
		SubmittedAt:     timelineInstant(submitted),
		AcceptedAt:      timelineInstant(accepted),
		AssignToHandoff: timelineDuration(assignment.AssignedAt, submitted),
		HandoffToAccept: timelineDuration(submitted, accepted),
	}}
}

func checkItem(run CheckRun) timelineItem {
	var finished time.Time
	if run.FinishedAt != nil {
		finished = *run.FinishedAt
	}
	return timelineItem{start: run.StartedAt, entry: TimelineEntry{
		Kind:       timelineCheck,
		ID:         run.ID,
		Stage:      run.CheckID,
		State:      run.Status,
		StartedAt:  timelineInstant(run.StartedAt),
		FinishedAt: timelineInstant(finished),
		Duration:   timelineDuration(run.StartedAt, finished),
	}}
}

// compareTimelineItems orders present starts by instant before missing ones,
// then assignments before checks, then IDs by byte order.
func compareTimelineItems(a, b timelineItem) int {
	aMissing, bMissing := a.start.IsZero(), b.start.IsZero()
	if aMissing != bMissing {
		if aMissing {
			return 1
		}
		return -1
	}
	if !aMissing {
		if order := a.start.Compare(b.start); order != 0 {
			return order
		}
	}
	return cmp.Or(
		cmp.Compare(timelineKindRank(a.entry.Kind), timelineKindRank(b.entry.Kind)),
		cmp.Compare(a.entry.ID, b.entry.ID),
	)
}

func timelineKindRank(kind string) int {
	if kind == timelineAssignment {
		return 0
	}
	return 1
}

func timelineInstant(instant time.Time) string {
	if instant.IsZero() {
		return NotAvailable
	}
	return instant.UTC().Format(time.RFC3339Nano)
}

func timelineDuration(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return NotAvailable
	}
	return to.Sub(from).Round(time.Millisecond).String()
}

// The summary objects fix the key order of each 'cw task timeline --summary'
// line by their field order.
type timelineSummaryTask struct {
	TaskID         string `json:"task_id"`
	StartedAt      string `json:"started_at"`
	UpdatedAt      string `json:"updated_at"`
	Elapsed        string `json:"elapsed"`
	OmittedEntries int    `json:"omitted_entries,omitzero"`
}

type timelineSummaryAssignment struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Stage           string `json:"stage"`
	Agent           string `json:"agent"`
	State           string `json:"state"`
	Start           string `json:"start"`
	AssignToHandoff string `json:"assign_to_handoff"`
	HandoffToAccept string `json:"handoff_to_accept"`
}

type timelineSummaryCheck struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Stage    string `json:"stage"`
	State    string `json:"state"`
	Start    string `json:"start"`
	Duration string `json:"duration"`
}

func timelineSummaryEntry(entry TimelineEntry) any {
	if entry.Kind == timelineAssignment {
		return timelineSummaryAssignment{
			Kind: entry.Kind, ID: entry.ID, Stage: entry.Stage, Agent: entry.Agent, State: entry.State,
			Start: entry.AssignedAt, AssignToHandoff: entry.AssignToHandoff, HandoffToAccept: entry.HandoffToAccept,
		}
	}
	return timelineSummaryCheck{
		Kind: entry.Kind, ID: entry.ID, Stage: entry.Stage, State: entry.State,
		Start: entry.StartedAt, Duration: entry.Duration,
	}
}

// mustEncodeTimelineLine encodes a summary object. The objects hold only
// strings, and every string a loadable record holds was decoded as valid
// UTF-8, so an encoding failure is a programming error and panics rather than
// yielding a partial summary.
func mustEncodeTimelineLine(value any) string {
	data, err := encode(value)
	if err != nil {
		panic("taskflow: encode timeline summary: " + err.Error())
	}
	return string(data)
}

// SummaryLines returns the compact form of the timeline that 'cw task timeline
// --summary' prints as JSON Lines: a task object, then one object per entry in
// entry order, each a single-line JSON object without a trailing newline.
func (timeline Timeline) SummaryLines() []string {
	lines := make([]string, 0, 1+len(timeline.Entries))
	lines = append(lines, mustEncodeTimelineLine(timelineSummaryTask{
		TaskID: timeline.TaskID, StartedAt: timeline.StartedAt, UpdatedAt: timeline.UpdatedAt, Elapsed: timeline.Elapsed,
		OmittedEntries: timeline.OmittedEntries,
	}))
	for _, entry := range timeline.Entries {
		lines = append(lines, mustEncodeTimelineLine(timelineSummaryEntry(entry)))
	}
	return lines
}
