package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// taskIDShowID runs cw task show --task value --field id and returns the ID
// it printed, or the error.
func taskIDShowID(t *testing.T, h *summaryCLIHarness, value string) (string, error) {
	t.Helper()
	outcome := compactExecute(h, nil, "task", "show", "--task", value, "--field", "id")
	if outcome.err != nil {
		return "", outcome.err
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(outcome.stdout), &fields); err != nil {
		t.Fatalf("show --field id printed %q: %v", outcome.stdout, err)
	}
	return fields["id"], nil
}

// Oracle: --task takes a task ID as well as a directory. The ID names the
// task's directory in the default store, or in $CW_TASK_STORE when set, for
// reading (show, timeline, trace) and for writing (run, verify) commands; an
// unknown ID fails naming the directory it looked for; ./ID is a path; and
// every command's --task help says so.
func TestTaskCommandsTakeATaskID(t *testing.T) {
	h := newSummaryCLIHarness(t)
	record := wholeRecordForTest(t, "start", h.start(false))
	id := record.ID

	if got, err := taskIDShowID(t, h, id); err != nil || got != id {
		t.Fatalf("show --task %s: %q, %v; want the task's own ID", id, got, err)
	}
	for _, args := range [][]string{
		{"task", "timeline", "--task", id},
		{"task", "trace", "--task", id, "--summary"},
		{"task", "run", "--task", id, "--check", "version"},
	} {
		if outcome := compactExecute(h, nil, args...); outcome.err != nil || !strings.Contains(outcome.stdout, id) && args[1] != "run" {
			t.Errorf("%s: %s", strings.Join(args, " "), outcome.text())
		}
	}
	if stored, err := taskflow.Load(record.Directory); err != nil || len(stored.Checks) != 1 {
		t.Fatalf("run --task ID did not store one check run in the task directory: %v", err)
	}
	verify := compactExecute(h, nil, "task", "verify", "--task", id)
	if ExitCode(verify.err) != 5 || !strings.Contains(verify.stdout, `"task_id":"`+id+`"`) {
		t.Fatalf("verify --task ID: exit %d: %s", ExitCode(verify.err), verify.text())
	}

	missing := strings.Repeat("0", 32)
	if _, err := taskIDShowID(t, h, missing); err == nil || ExitCode(err) == ExitUsage || !strings.Contains(err.Error(), filepath.Join(filepath.Dir(record.Directory), missing)) {
		t.Fatalf("show --task with an unknown ID: %v (exit %d); want a failure naming the store's directory", err, ExitCode(err))
	}

	// Started before the working directory changes: the policy path is relative.
	store := filepath.Join(t.TempDir(), "store")
	other := taskIDStartIn(t, h, store)

	// ./ID is a path relative to the working directory, not a task ID.
	elsewhere := t.TempDir()
	h.config.WorkingDir = func() (string, error) { return elsewhere, nil }
	t.Chdir(elsewhere)
	if _, err := taskIDShowID(t, h, "./"+id); err == nil {
		t.Fatal("show --task ./ID resolved in the task store, want the relative path")
	}

	// $CW_TASK_STORE selects the store an ID is found in.
	h.config.Getenv = func(name string) string {
		if name == "CW_TASK_STORE" {
			return store
		}
		return ""
	}
	if got, err := taskIDShowID(t, h, other); err != nil || got != other {
		t.Fatalf("show --task ID with CW_TASK_STORE: %q, %v; want %s", got, err, other)
	}
	if _, err := taskIDShowID(t, h, id); err == nil {
		t.Fatal("show --task ID found a task outside CW_TASK_STORE")
	}

	task, _, err := NewCommand(h.config).Find([]string{"task"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range task.Commands() {
		if flag := command.Flags().Lookup("task"); flag != nil && !strings.Contains(flag.Usage, "task ID") {
			t.Errorf("cw task %s --task help %q does not mention the task ID", command.Name(), flag.Usage)
		}
	}
}

// taskIDStartIn starts a task in store and returns its ID, which start
// prints as "id" in the record or "task_id" in its summary.
func taskIDStartIn(t *testing.T, h *summaryCLIHarness, store string) string {
	t.Helper()
	output := h.execute([]byte(summaryCLITestPlan), "task", "start", "--plan", "-", "--policy", shippedPolicyPath, "--store", store)
	var printed struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(output, &printed); err != nil || printed.ID+printed.TaskID == "" {
		t.Fatalf("start --store printed %q: %v", output, err)
	}
	return printed.ID + printed.TaskID
}
