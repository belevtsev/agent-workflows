package gate_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGateImportClosureExcludesDoctorDependencies keeps cw doctor's
// environment probes out of the commit gate: the gate decides from the task
// store and the source snapshot, so neither it nor any package it imports,
// taskflow and projectctx included, may pull in the doctor, the credential
// store, Graphify or the TOML parser the doctor reads Codex config with.
// Oracle: `go list -deps` of this package lists none of the forbidden import
// paths or their subpackages.
func TestGateImportClosureExcludesDoctorDependencies(t *testing.T) {
	const module = "github.com/belevtsev/agent-workflows/v3/internal/"
	forbidden := []string{module + "doctor", module + "credentials", module + "graphify", "github.com/pelletier/go-toml"}
	command := exec.CommandContext(t.Context(), "go", "list", "-deps", ".")
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for dependency := range strings.FieldsSeq(string(output)) {
		for _, path := range forbidden {
			if dependency == path || strings.HasPrefix(dependency, path+"/") {
				t.Errorf("the gate's import closure includes %s", dependency)
			}
		}
	}
}
