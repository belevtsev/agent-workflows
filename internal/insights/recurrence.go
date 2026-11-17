package insights

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/store"
)

// Recurrence is an inspection hint about repeated recorded outcomes. Neither
// an identical failure nor an author observation confirms a common root cause.
type Recurrence struct {
	Check          string       `json:"check"`
	ContentKey     string       `json:"content_key"`
	PolicyKey      string       `json:"recorded_definition_key"`
	Outcome        string       `json:"outcome"`
	Runs           int          `json:"runs"`
	RunSamples     []string     `json:"run_samples"`
	OmittedRuns    int          `json:"omitted_runs"`
	LastRun        string       `json:"last_run"`
	LatestRun      string       `json:"latest_recorded_run"`
	LatestOutcome  string       `json:"latest_recorded_outcome"`
	Observation    string       `json:"observation"`
	NoteReferences []NoteSource `json:"note_references"`
	Signal         string       `json:"signal"`
}

// Recurrences reports truncation rather than implying a complete failure history.
type Recurrences struct {
	Items   []Recurrence `json:"items"`
	Omitted int          `json:"omitted_groups"`
	Scope   string       `json:"scope"`
}

// RecordedDefinitionKey fingerprints exactly what existing run records preserve:
// check identity, argv and dependent paths. It is not a full task policy hash.
func RecordedDefinitionKey(record store.Record) string {
	paths := record.Paths
	if len(paths) == 0 {
		paths = nil
	}
	data, _ := json.Marshal(struct {
		ID         string
		Run, Paths []string
	}{record.ID, record.Run, paths})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// RepeatedFailures groups explicitly supplied runs only. Callers are responsible
// for task attribution; timestamp overlap never establishes ownership.
func RepeatedFailures(records []store.Record, notes Notes, limit int) Recurrences {
	out := Recurrences{Items: []Recurrence{}, Scope: "Recorded failed and timed-out attempts on identical check/content/argv/dependencies only; no confirmed root cause, automatic retry, suppressed check or completion authority. Author observations remain claims."}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	groups := map[string][]store.Record{}
	latest := map[string]store.Record{}
	seen := map[string]bool{}
	for _, record := range records {
		if record.RunID == "" || record.ContentKey == "" {
			continue
		}
		base := record.ID + "\x00" + record.ContentKey + "\x00" + RecordedDefinitionKey(record)
		if previous, ok := latest[base]; !ok || recurrenceAfter(record, previous) {
			latest[base] = record
		}
		if record.Outcome != store.Failed && record.Outcome != store.TimedOut {
			continue
		}
		key := base + "\x00" + record.Outcome
		identity := key + "\x00" + record.RunID
		if seen[identity] {
			continue
		}
		seen[identity] = true
		groups[key] = append(groups[key], record)
	}
	keys := make([]string, 0, len(groups))
	for key, runs := range groups {
		if len(runs) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(out.Items) == limit {
			out.Omitted++
			continue
		}
		runs := groups[key]
		sort.Slice(runs, func(i, j int) bool { return recurrenceAfter(runs[j], runs[i]) })
		last := runs[len(runs)-1]
		item := Recurrence{Check: last.ID, ContentKey: last.ContentKey, PolicyKey: RecordedDefinitionKey(last), Outcome: last.Outcome, Runs: len(runs), LastRun: last.RunID, RunSamples: []string{}, NoteReferences: []NoteSource{}, Observation: "not_recorded", Signal: "inspect_repeated_unchanged_failure"}
		current := latest[last.ID+"\x00"+last.ContentKey+"\x00"+item.PolicyKey]
		item.LatestRun, item.LatestOutcome = current.RunID, current.Outcome
		if item.LatestOutcome == "" {
			item.LatestOutcome = "unfinished_unknown"
		}
		if current.Outcome == store.Passed {
			item.Signal = "historical_recurrence_followed_by_pass"
		}
		for _, record := range runs[max(0, len(runs)-8):] {
			item.RunSamples = append(item.RunSamples, record.RunID)
		}
		item.OmittedRuns = len(runs) - len(item.RunSamples)
		matchRecurrenceNotes(&item, notes)
		out.Items = append(out.Items, item)
	}
	return out
}

func recurrenceAfter(a, b store.Record) bool {
	if a.Seq > 0 && b.Seq > 0 && a.Seq != b.Seq {
		return a.Seq > b.Seq
	}
	if a.StartedAt.Equal(b.StartedAt) {
		return a.RunID > b.RunID
	}
	return a.StartedAt.After(b.StartedAt)
}

func matchRecurrenceNotes(item *Recurrence, notes Notes) {
	if len(notes.Problems) > 0 || notes.Omitted > 0 {
		item.Observation = "unknown"
	}
	for _, note := range notes.Observations {
		fields := note.Fields
		plain := func(key string) string { return strings.Trim(fields[key], "` ") }
		if plain("check") != item.Check || plain("content key") != item.ContentKey || plain("policy key") != item.PolicyKey || plain("run") != item.LastRun {
			continue
		}
		item.Observation = "recorded_author_claim"
		if len(item.NoteReferences) < 8 {
			item.NoteReferences = append(item.NoteReferences, NoteSource{Path: note.Path, Line: note.Line, Kind: "hypothesis", Format: "author-note"})
		}
	}
}
