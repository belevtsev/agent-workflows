package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Every cw task command that takes --task fails without it as a usage error,
// before it reads its input or any task: exit 2 with "--task DIR is
// required". Run from inside a task directory, so a command that resolved the
// empty --task to the working directory would read or change that task.
//
// Oracle: docs/operations.md "Exit statuses" gives 2 to a missing option, and
// "--task DIR is required" is what cw publish, timeline, abandon and add-checks
// print; nothing is printed on stdout, the task directory keeps its files and
// bundle writes no --output.
func TestTaskCommandsWithoutTaskAreUsageErrors(t *testing.T) {
	h := newTaskBundleHarness(t)
	scratch := taskBundleDir(t)
	input := filepath.Join(scratch, "input.json")
	if err := os.WriteFile(input, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(scratch, "bundle.json")
	cases := map[string][]string{
		"abandon":    {"--reason", "given up"},
		"accept":     {"--input", input},
		"add-checks": {"--input", input},
		"assess":     {"--input", input},
		"assign":     {"--input", input},
		"authorship": {},
		"brief":      {"--stage", "tests"},
		"bundle":     {"--review-id", "review1", "--output", output},
		"consult":    {"--phase", "before", "--input", input},
		"context":    {"--phase", "before"},
		"handoff":    {"--input", input},
		"receipt":    {},
		"run":        {"--check", "check1"},
		"show":       {},
		"timeline":   {},
		"trace":      {},
		"usage":      {"--claude-transcript", input},
		"verify":     {},
	}
	task, _, err := NewCommand(h.config).Find([]string{"task"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range task.Commands() {
		if _, listed := cases[command.Name()]; command.Flags().Lookup("task") != nil && !listed {
			t.Errorf("cw task %s takes --task but has no case here", command.Name())
		}
	}
	t.Chdir(h.record.Directory)
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			before := taskBundleFiles(t, h.record.Directory)
			h.out.Reset()
			err := Execute(t.Context(), append([]string{"task", name}, args...), h.config)
			if err == nil || ExitCode(err) != ExitUsage || err.Error() != "--task DIR is required" {
				t.Errorf("cw task %s without --task: %v (exit %d); want %q, exit %d", name, err, ExitCode(err), "--task DIR is required", ExitUsage)
			}
			if h.out.Len() != 0 {
				t.Errorf("cw task %s without --task printed %q", name, h.out.String())
			}
			if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
				t.Errorf("cw task %s without --task changed the task in the working directory", name)
			}
			if _, statErr := os.Lstat(output); name == "bundle" && statErr == nil {
				t.Errorf("cw task %s without --task wrote %s", name, output)
			}
		})
	}
}
