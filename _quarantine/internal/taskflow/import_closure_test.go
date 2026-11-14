package taskflow_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestTaskflowImportClosureExcludesAdviserProviders keeps the adviser HTTP
// providers out of the task flow: taskflow reads a consultation only as the
// internal/consult/record summary, through the validator its Env is given, so
// neither it nor any package it imports may pull in internal/consult or
// net/http. Its tests may still import internal/consult.
// Oracle: `go list -deps` of this package, without tests, lists neither
// internal/consult nor net/http or its subpackages; internal/consult/record is
// allowed.
func TestTaskflowImportClosureExcludesAdviserProviders(t *testing.T) {
	forbidden := []string{"github.com/belevtsev/agent-workflows/v3/internal/consult", "net/http"}
	command := exec.CommandContext(t.Context(), "go", "list", "-deps", ".")
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for dependency := range strings.FieldsSeq(string(output)) {
		for _, path := range forbidden {
			if dependency == path || path == "net/http" && strings.HasPrefix(dependency, path+"/") {
				t.Errorf("taskflow's import closure includes %s", dependency)
			}
		}
	}
}
