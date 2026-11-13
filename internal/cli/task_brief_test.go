package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: docs/operations.md "Exit statuses" and "Start each stage from its
// brief". cw task brief with an unknown stage, or one outside the task's
// lite profile, exits 2 (usage) naming the stage; a stage of the profile
// prints the text brief, or with --json the same brief as JSON, at most
// 4,096 bytes. No run adds, removes or changes a file in the task directory,
// so no revision is written.
func TestTaskBriefRefusesAStageOutsideTheProfileAndWritesNothing(t *testing.T) {
	h := newTaskBundleHarness(t)
	before := taskBundleFiles(t, h.record.Directory)
	for _, c := range []struct{ stage, want string }{
		{"deploy", `unknown stage "deploy"`},
		{"spec", "unknown stage spec: it is not in profile lite"},
	} {
		h.out.Reset()
		err := Execute(t.Context(), []string{"task", "brief", "--task", h.record.Directory, "--stage", c.stage}, h.config)
		if err == nil || ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("brief --stage %s = %v (exit %d); want exit %d naming %q", c.stage, err, ExitCode(err), ExitUsage, c.want)
		}
		if h.out.Len() != 0 {
			t.Fatalf("brief --stage %s printed %q", c.stage, h.out.String())
		}
	}

	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "brief", "--task", h.record.Directory, "--stage", "tests"}, h.config); err != nil {
		t.Fatal(err)
	}
	text := h.out.String()
	if want := "task " + h.record.ID + ": Review project source\nstage tests, profile lite, 2 of 2 author rounds left\n"; !strings.HasPrefix(text, want) || len(text) > taskflow.MaxBriefBytes {
		t.Fatalf("brief text (%d bytes) =\n%s\nwant prefix\n%s", len(text), text, want)
	}

	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "brief", "--task", h.record.Directory, "--stage", "tests", "--json"}, h.config); err != nil {
		t.Fatal(err)
	}
	var brief taskflow.Brief
	if err := json.Unmarshal(h.out.Bytes(), &brief); err != nil || brief.Text() != text || h.out.Len() > taskflow.MaxBriefBytes {
		t.Fatalf("brief --json = %s (%v); want the text brief's struct", h.out.String(), err)
	}

	if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatalf("task directory changed: before %d files, after %d", len(before), len(after))
	}
}
