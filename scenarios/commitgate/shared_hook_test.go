package commitgate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Hooks of other tools", scenario "A shared hook that already
// calls `cw`".
func TestSharedHookThatAlreadyCallsCW(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	shared := filepath.Join(hooksDir(t, repo), "pre-commit")
	hook := "#!/bin/sh\n" +
		"# written by another tool\n" +
		"echo another tool runs\n" +
		"cw hook pre-commit \"$@\"\n"
	if err := os.WriteFile(shared, []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	before := fileSum(t, shared)

	result := repo.CW("init")
	if result.Code != 0 {
		t.Errorf("cw init exited %d, want 0:\n%s", result.Code, result.Output())
	}
	if got := fileSum(t, shared); got != before {
		t.Errorf("the SHA-256 of pre-commit changed from %s to %s", before, got)
	}

	status := repo.CW("status", "--json")
	var object struct {
		Hooks string `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(status.Stdout), &object); err != nil {
		t.Fatalf("cw status --json (exit %d) did not print one JSON object: %v\n%s", status.Code, err, status.Output())
	}
	if object.Hooks != "installed" {
		t.Errorf("cw status --json holds hooks %q, want installed:\n%s", object.Hooks, status.Stdout)
	}
}
