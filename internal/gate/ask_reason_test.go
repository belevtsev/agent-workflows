package gate_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// askCase is one ask that names tasks: how to raise it for records, how it
// names one of them, and how it counts the rest.
type askCase struct {
	name string
	// decide raises the ask for open, records in denyStore that do not
	// gate; it may set fields of open.
	decide func(t *testing.T, open []taskflow.OpenRecord) gate.Decision
	// names is how the reason names a record it names.
	names func(open taskflow.OpenRecord) string
	// more is how the reason counts rest records past the first three.
	more func(rest int) string
}

var askCases = []askCase{
	{
		name: "unreadable records",
		decide: func(t *testing.T, open []taskflow.OpenRecord) gate.Decision {
			repo := canonicalDir(t)
			git := &fakeGit{}
			git.repository(repo)
			for i := range open {
				open[i].Unreadable = "invalid task JSON: unexpected EOF"
			}
			return decide("git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open}})
		},
		names: func(open taskflow.OpenRecord) string {
			return "Task directory " + open.Record.Directory + ": invalid task JSON: unexpected EOF; if the record is damaged,"
		},
		more: func(rest int) string {
			return fmt.Sprintf(" And %d more %s in %s; each needs a cw that can read it or, if it is damaged, its directory moved out of the task store. Confirm to run",
				rest, plural(rest, "unreadable task record"), denyStore)
		},
	},
	{
		name: "a push of uncommitted changes over verified records",
		decide: func(t *testing.T, open []taskflow.OpenRecord) gate.Decision {
			repo := canonicalDir(t)
			git := &fakeGit{}
			git.repository(repo)
			git.answer(repo, "status --porcelain --untracked-files=no --ignore-submodules=none", " M source.txt\n")
			return decide("git push origin main", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open}})
		},
		names: func(open taskflow.OpenRecord) string {
			return fmt.Sprintf("%s (%s)", open.Record.ID, open.Record.Plan.Title)
		},
		more: func(rest int) string {
			return fmt.Sprintf(", and %d more %s in %s passed cw task verify on the working tree", rest, plural(rest, "task"), denyStore)
		},
	},
	{
		name: "abandons",
		decide: func(t *testing.T, open []taskflow.OpenRecord) gate.Decision {
			tasks := map[string]taskflow.Record{}
			var command []string
			for _, record := range open {
				tasks[record.Record.Directory] = record.Record
				command = append(command, "cw task abandon --task "+record.Record.Directory+" --reason x")
			}
			return decide(strings.Join(command, "; "), canonicalDir(t), true, true, &fakeGit{}, &fakeStore{tasks: tasks})
		},
		names: func(open taskflow.OpenRecord) string {
			return fmt.Sprintf("task %s (%s) in %s", open.Record.ID, open.Record.Plan.Title, open.Record.Directory)
		},
		more: func(rest int) string {
			return fmt.Sprintf("; and %d more %s in %s. Confirm only if", rest, plural(rest, "task"), denyStore)
		},
	},
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}

func TestAsksNameAtMostThreeTasks(t *testing.T) {
	for _, c := range askCases {
		for _, count := range []int{3, 4, 10} {
			t.Run(fmt.Sprintf("%s, %d tasks", c.name, count), func(t *testing.T) {
				open := denyRecords(count)
				for i := range open {
					open[i].Problem = ""
				}
				got := c.decide(t, open)
				if got.Permission != gate.Ask {
					t.Fatalf("Decide = %+v, want an ask", got)
				}
				reason := got.Reason
				// Oracle: the first three records, in the order the store or
				// the command gives them, are named, each once and in that
				// order; no record past them is named.
				at := -1
				for i, record := range open {
					if i >= 3 {
						if strings.Contains(reason, record.Record.ID) {
							t.Errorf("reason names record %d of %d, %s, past the first three:\n%s", i+1, count, record.Record.ID, reason)
						}
						continue
					}
					names := c.names(record)
					if n := strings.Count(reason, names); n != 1 {
						t.Errorf("reason names record %d of %d as %q %d times, want once:\n%s", i+1, count, names, n, reason)
					}
					index := strings.Index(reason, names)
					if index <= at {
						t.Errorf("reason does not name record %d of %d after record %d:\n%s", i+1, count, i, reason)
					}
					at = index
				}
				// Oracle: past three, the reason counts the rest once, with
				// the task store they are in; up to three, it counts nothing.
				if rest := count - 3; rest > 0 {
					if more := c.more(rest); strings.Count(reason, more) != 1 {
						t.Errorf("reason for %d records does not count the rest as %q:\n%s", count, more, reason)
					}
				} else if strings.Contains(reason, " more ") {
					t.Errorf("reason for %d records counts some:\n%s", count, reason)
				}
			})
		}
	}
}

func TestAbandonAskCountsTasksItCannotIdentifyWithoutAStore(t *testing.T) {
	open := denyRecords(3)
	tasks := map[string]taskflow.Record{}
	var command []string
	for _, record := range open {
		tasks[record.Record.Directory] = record.Record
		command = append(command, "cw task abandon --task "+record.Record.Directory+" --reason x")
	}
	// The fourth abandon names a directory the gate cannot resolve, and the
	// fifth is run by xargs, which the gate cannot read.
	command = append(command, "cd $OTHER && cw task abandon --task task-9 --reason x", "echo /x | xargs cw task abandon --reason x --task")

	got := decide(strings.Join(command, "; "), canonicalDir(t), true, true, &fakeGit{}, &fakeStore{tasks: tasks})

	// Oracle: the three named tasks, then a count of the other two with no
	// task store, since the gate does not know where they are.
	want := "This command abandons " +
		fmt.Sprintf("task %s (%s) in %s; ", open[0].Record.ID, open[0].Record.Plan.Title, open[0].Record.Directory) +
		fmt.Sprintf("task %s (%s) in %s; ", open[1].Record.ID, open[1].Record.Plan.Title, open[1].Record.Directory) +
		fmt.Sprintf("task %s (%s) in %s; ", open[2].Record.ID, open[2].Record.Plan.Title, open[2].Record.Directory) +
		"and 2 more tasks. Confirm only if"
	if got.Permission != gate.Ask || !strings.Contains(got.Reason, want) {
		t.Fatalf("Decide = %+v, want an ask with %q", got, want)
	}
}
