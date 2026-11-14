package cli

import (
	json "encoding/json/v2"
	"maps"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// taskShowSummary is what cw task show prints without --field or --full: the
// task's state in one line and the record's top-level members, which --field
// selects. Its size grows with the plan and the assignments, not with every
// check run.
type taskShowSummary struct {
	TaskID       string            `json:"task_id"`
	Title        string            `json:"title"`
	Kind         string            `json:"kind"`
	Revision     int               `json:"revision"`
	Abandoned    bool              `json:"abandoned,omitzero"`
	Requirements map[string]string `json:"requirements"`
	Checks       map[string]string `json:"checks"`
	Assignments  map[string]string `json:"assignments,omitempty"`
	Verification *showVerification `json:"verification,omitempty"`
	Fields       []string          `json:"fields"`
}

// showVerification is the stored verification in brief.
type showVerification struct {
	Complete bool `json:"complete"`
	Problems int  `json:"problems"`
}

// Values of the show summary for a requirement without an assessment and a
// check without a run.
const (
	showUnassessed = "unassessed"
	showNotRun     = "not_run"
)

// showSummary reports each plan requirement's assessment status, each check's
// latest run status (the plan's checks and those amendments added), each
// assignment's state as cw task timeline reports it, the stored verification
// and the record's top-level members.
func showSummary(record taskflow.Record) (taskShowSummary, error) {
	value := taskShowSummary{
		TaskID: record.ID, Title: record.Plan.Title, Kind: record.Plan.Kind, Revision: record.Revision,
		Abandoned: record.Abandonment != nil, Requirements: map[string]string{}, Checks: map[string]string{},
	}
	for _, requirement := range record.Plan.Requirements {
		value.Requirements[requirement.ID] = showUnassessed
		if bound, ok := record.Assessments[requirement.ID]; ok {
			value.Requirements[requirement.ID] = bound.Assessment.Status
		}
	}
	for _, check := range record.Plan.Checks {
		value.Checks[check.ID] = showNotRun
	}
	for _, amendment := range record.Amendments {
		for _, check := range amendment.Checks {
			value.Checks[check.ID] = showNotRun
		}
	}
	for _, run := range record.Checks {
		value.Checks[run.CheckID] = run.Status
	}
	for _, entry := range taskflow.BuildTimeline(record).Entries {
		if entry.Kind == "assignment" {
			if value.Assignments == nil {
				value.Assignments = map[string]string{}
			}
			value.Assignments[entry.ID] = entry.State
		}
	}
	if record.Verification != nil {
		value.Verification = &showVerification{Complete: record.Verification.Complete, Problems: len(record.Verification.Problems)}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return taskShowSummary{}, err
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		return taskShowSummary{}, err
	}
	value.Fields = slices.Sorted(maps.Keys(members))
	return value, nil
}
