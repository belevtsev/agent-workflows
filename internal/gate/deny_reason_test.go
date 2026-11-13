package gate_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// denyStore is a task store directory of the length the hook reads by
// default, under a home directory.
const denyStore = "/home/someone/.local/state/agent-workflows/tasks"

// denyRecords returns count open records in denyStore that gate, each with
// an ID of the length cw task start writes and a title of a typical length,
// in the order a store returns them.
func denyRecords(count int) []taskflow.OpenRecord {
	open := make([]taskflow.OpenRecord, 0, count)
	for i := range count {
		id := fmt.Sprintf("%x", sha256.Sum256([]byte{byte(i)}))[:32]
		open = append(open, taskflow.OpenRecord{
			Record:  taskflow.Record{ID: id, Directory: denyStore + "/" + id, Plan: taskflow.Plan{Title: fmt.Sprintf("Retry failed uploads in the sync client, part %d", i+1)}},
			Problem: "its source changed after the last passing cw task verify",
		})
	}
	return open
}

// denyReasonLimit is the most bytes the deny reason may take for any number
// of gating records with denyRecords' fields: the fixed text, three named
// records and the count of the rest. Before the cap, two records took 1,213
// bytes, three 1,702, four 2,191 and ten 5,126; with it, ten take 1,337.
const denyReasonLimit = 1500

func TestDenyReasonNamesAtMostThreeTasks(t *testing.T) {
	for _, count := range []int{2, 3, 4, 10} {
		t.Run(fmt.Sprintf("%d tasks", count), func(t *testing.T) {
			repo, other := canonicalDir(t), canonicalDir(t)
			git := &fakeGit{}
			git.repository(repo)
			git.repository(other)
			open := denyRecords(count)
			store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open}}

			got := decide("git commit -m x", repo, true, true, git, store)
			if got.Permission != gate.Deny {
				t.Fatalf("Decide = %+v, want a deny", got)
			}
			reason := got.Reason
			// Oracle: the reason stays under one bound however many records
			// gate.
			if len(reason) > denyReasonLimit {
				t.Errorf("deny reason is %d bytes, want at most %d:\n%s", len(reason), denyReasonLimit, reason)
			}
			// Oracle: the first three records, in the order the store
			// returns them, are named with their directory once, in their
			// verify command; no other record is named.
			named := min(count, 3)
			at := -1
			for i, record := range open {
				directory := record.Record.Directory
				if i >= named {
					if strings.Contains(reason, record.Record.ID) {
						t.Errorf("deny reason names record %d of %d, %s, past the first three:\n%s", i+1, count, record.Record.ID, reason)
					}
					continue
				}
				if strings.Contains(reason, directory) {
					t.Errorf("deny reason names the task directory %s, want only its ID:\n%s", directory, reason)
				}
				verify := fmt.Sprintf("Task %s (%s): %s; run `cw task verify --task %s`.", record.Record.ID, record.Record.Plan.Title, record.Problem, record.Record.ID)
				index := strings.Index(reason, verify)
				if index <= at {
					t.Errorf("deny reason does not name record %d of %d as %q after record %d:\n%s", i+1, count, verify, i, reason)
				}
				at = index
			}
			// Oracle: abandoning needs the user's confirmation, said once.
			const abandon = " For a task that is finished or was given up, ask the user before abandoning it: `cw task abandon --task <task-id> --reason TEXT` needs their confirmation."
			if n := strings.Count(reason, abandon); n != 1 {
				t.Errorf("deny reason gives the abandon sentence %d times, want once:\n%s", n, reason)
			}
			// Oracle: past three, the reason counts the rest and names
			// `cw task list`, which lists them, instead of a task store; up
			// to three, it counts nothing.
			more := ""
			switch rest := count - named; {
			case rest == 1:
				more = " And 1 more open task (`cw task list` lists it);"
			case rest > 1:
				more = fmt.Sprintf(" And %d more open tasks (`cw task list` lists them);", rest)
			}
			if more != "" && strings.Count(reason, more) != 1 || more == "" && strings.Contains(reason, " more open task") {
				t.Errorf("deny reason for %d records does not count the rest as %q:\n%s", count, more, reason)
			}
			if strings.Contains(reason, denyStore) {
				t.Errorf("deny reason names the task store %s, want `cw task list` for the rest:\n%s", denyStore, reason)
			}

			// Oracle: the ask after a directory change the gate cannot follow
			// names the gating records exactly as the deny does.
			const preamble = "agent-workflows: git commit and push stay blocked while a task for this repository is open: each must pass cw task verify on the source being published, or be closed."
			const off = " To turn the gate off, set enforce_verify=false in the plugin config."
			records := strings.TrimSuffix(strings.TrimPrefix(reason, preamble), off)
			if records == reason {
				t.Fatalf("deny reason lacks its fixed text:\n%s", reason)
			}
			asked := decide(`cd "$OTHER" && git commit -m x`, repo, true, true, git, store)
			if asked.Permission != gate.Ask || !strings.Contains(asked.Reason, "is open:"+records+" Confirm only if") {
				t.Errorf("ask after an unresolved cd = %+v, want the deny's records%s", asked, records)
			}
		})
	}
}

func TestDenyReasonCountsALegacyRecordPastTheFirstThree(t *testing.T) {
	repo := canonicalDir(t)
	git := &fakeGit{}
	git.repository(repo)
	open := denyRecords(4)
	open[3].Legacy = true
	open[3].Problem = "this cw cannot read its record; move " + open[3].Record.Directory + " out of the task store"

	got := decide("git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open}})
	// Oracle: the count of the rest says how to clear a record that verify
	// and abandon cannot load, without naming it.
	want := " And 1 more open task (`cw task list` lists it); each needs `cw task verify` or the user's confirmation to abandon, " +
		"or, if this cw cannot read its record, moving its directory out of the task store."
	if got.Permission != gate.Deny || !strings.Contains(got.Reason, want) || strings.Contains(got.Reason, open[3].Record.ID) {
		t.Fatalf("Decide = %+v, want a deny that counts the legacy record as %q and does not name it", got, want)
	}
}
