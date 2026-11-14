//go:build unix

package cli

import (
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// tlSeed starts, through the CLI with a fixed clock, one task in each state
// in repo and an open task in other, and returns the harness, with its clock
// set to the time the list runs, and the records by state.
func tlSeed(t *testing.T) (h *hkHarness, repo, other string, records map[string]taskflow.Record) {
	t.Helper()
	repo, other = hkRepo(t), hkRepo(t)
	source := []byte("listed source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
		t.Fatal(err)
	}
	h = newHKHarness(t)
	base := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	at := base
	h.config.Now = func() time.Time { return at }
	records = map[string]taskflow.Record{}

	at = base.Add(-taskflow.MaxOpenAge - 24*time.Hour)
	records[taskflow.StateStale] = h.start(t, repo, hkTitled("Stale task"))
	at = base.Add(-3 * time.Hour)
	records[taskflow.StateAbandoned] = h.start(t, repo, strings.Replace(hkSmallPlan, `"title":"S"`, `"title":"Abandoned task"`, 1))
	at = base.Add(-150 * time.Minute)
	h.abandon(t, records[taskflow.StateAbandoned], "given up")
	at = base.Add(-2 * time.Hour)
	unreadable := h.start(t, repo, hkTitled("Unreadable task"))
	// A gap in the revision chain: Load refuses the record, but its only
	// revision still decodes and names the repository.
	if err := os.Rename(filepath.Join(unreadable.Directory, "record-000001.json"), filepath.Join(unreadable.Directory, "record-000002.json")); err != nil {
		t.Fatal(err)
	}
	records[taskflow.StateUnreadable] = unreadable
	at = base.Add(-time.Hour)
	records[taskflow.StateVerified] = h.start(t, repo, hkTitled("Verified task"))
	h.assessAndVerify(t, records[taskflow.StateVerified], source)
	at = base.Add(-30 * time.Minute)
	records[taskflow.StateOpen] = h.start(t, repo, hkTitled(`Open\ttask`))
	at = base.Add(-10 * time.Minute)
	records["other"] = h.start(t, other, hkTitled("Other repository task"))
	at = base
	return h, repo, other, records
}

// tlList runs cw task list with args in dir and returns its output, with
// each seeded task ID replaced by a placeholder of the same length, so the
// columns stay as cw aligned them.
func tlList(t *testing.T, h *hkHarness, dir string, records map[string]taskflow.Record, args ...string) string {
	t.Helper()
	h.config.WorkingDir = func() (string, error) { return dir, nil }
	h.out.Reset()
	if err := Execute(t.Context(), append([]string{"task", "list"}, args...), h.config); err != nil {
		t.Fatalf("task list %v: %v\n%s", args, err, h.out.String())
	}
	got := h.out.String()
	for state, record := range records {
		got = strings.ReplaceAll(got, record.ID, fmt.Sprintf("%-32s", "<"+state+">"))
	}
	return got
}

func TestTaskListPrintsTheRepositoryTasksAsText(t *testing.T) {
	// Oracle: testdata/task-list.golden, written by hand from the seeded
	// tasks: newest first, the open, verified and unreadable tasks of this
	// repository with their kind, last update in UTC and title (a tab in a
	// title printed as a space), then a count of the hidden abandoned and
	// stale ones. The other repository's task is not listed.
	h, repo, _, records := tlSeed(t)
	want, err := os.ReadFile(filepath.Join("testdata", "task-list.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := tlList(t, h, repo, records); got != string(want) {
		t.Fatalf("cw task list printed\n%s\nwant\n%s", got, want)
	}

	// Oracle: --all also lists the abandoned and stale tasks and, without
	// --repo, the other repository's, with each task's repository last.
	got := tlList(t, h, repo, records, "--all")
	got = strings.ReplaceAll(got, repo, "<repo>")
	want, err = os.ReadFile(filepath.Join("testdata", "task-list-all.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if other := records["other"].Baseline.Root; !strings.Contains(got, other) {
		t.Fatalf("cw task list --all does not name the other repository %s:\n%s", other, got)
	} else if got = strings.ReplaceAll(got, other, "<other>"); got != string(want) {
		t.Fatalf("cw task list --all printed\n%s\nwant\n%s", got, want)
	}
}

func TestTaskListJSONCarriesEachTaskState(t *testing.T) {
	// Oracle: --json lists the same tasks as the text, each with its ID,
	// state and directory, and counts the two hidden ones; --repo selects
	// the other repository from anywhere, and --all --repo lists every
	// state of one repository.
	h, repo, other, records := tlSeed(t)
	type listed struct {
		Tasks []struct {
			ID        string `json:"task_id"`
			State     string `json:"state"`
			Directory string `json:"directory"`
			Error     string `json:"error"`
		} `json:"tasks"`
		Hidden int `json:"hidden"`
	}
	decode := func(t *testing.T, args ...string) listed {
		t.Helper()
		h.config.WorkingDir = func() (string, error) { return t.TempDir(), nil }
		h.out.Reset()
		if err := Execute(t.Context(), append([]string{"task", "list", "--json"}, args...), h.config); err != nil {
			t.Fatalf("task list %v: %v", args, err)
		}
		var value listed
		if err := json.Unmarshal(h.out.Bytes(), &value); err != nil {
			t.Fatalf("task list --json printed %q: %v", h.out.String(), err)
		}
		return value
	}
	states := func(value listed) []string {
		var got []string
		for _, task := range value.Tasks {
			want := records[task.State]
			if task.State == taskflow.StateOpen && task.ID == records["other"].ID {
				want = records["other"]
			}
			if task.ID != want.ID || task.Directory == "" || (task.Error != "") != (task.State == taskflow.StateUnreadable) {
				t.Errorf("listed task %+v, want task %s for state %s", task, want.ID, task.State)
			}
			got = append(got, task.State)
		}
		return got
	}

	value := decode(t, "--repo", repo)
	if got := strings.Join(states(value), " "); got != "open verified unreadable" || value.Hidden != 2 {
		t.Fatalf("cw task list --json --repo REPO states %q hidden %d, want \"open verified unreadable\" hidden 2", got, value.Hidden)
	}
	value = decode(t, "--repo", other)
	if len(value.Tasks) != 1 || value.Tasks[0].ID != records["other"].ID || value.Hidden != 0 {
		t.Fatalf("cw task list --json --repo OTHER = %+v, want only %s", value, records["other"].ID)
	}
	value = decode(t, "--repo", repo, "--all")
	if got := strings.Join(states(value), " "); got != "open verified unreadable abandoned stale" || value.Hidden != 0 {
		t.Fatalf("cw task list --json --all --repo REPO states %q hidden %d, want every state and none hidden", got, value.Hidden)
	}
}

func TestTaskListOutsideARepositoryNeedsRepoOrAll(t *testing.T) {
	// Oracle: outside a Git repository the list cannot tell which tasks
	// are this repository's, so it fails and names --repo and --all.
	h := newHKHarness(t)
	dir := hkCanonicalTempDir(t)
	h.config.WorkingDir = func() (string, error) { return dir, nil }
	err := Execute(t.Context(), []string{"task", "list"}, h.config)
	if err == nil || !strings.Contains(err.Error(), "--repo DIR") || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("cw task list outside a repository: %v, want an error naming --repo DIR and --all", err)
	}
}
