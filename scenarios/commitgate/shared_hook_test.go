package commitgate_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Hooks of other tools", scenario "A shared hook that already
// calls `kitsu`".
func TestSharedHookThatAlreadyCallsKitsu(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	shared := filepath.Join(hooksDir(t, repo), "pre-commit")
	hook := "#!/bin/sh\n" +
		"# written by another tool\n" +
		"echo another tool runs\n" +
		"kitsu hook pre-commit \"$@\"\n"
	writeProgram(t, shared, hook)
	before := fileSum(t, shared)

	result := repo.Kitsu("init")
	if result.Code != 0 {
		t.Errorf("kitsu init exited %d, want 0:\n%s", result.Code, result.Output())
	}
	if got := fileSum(t, shared); got != before {
		t.Errorf("the SHA-256 of pre-commit changed from %s to %s", before, got)
	}

	status := repo.Kitsu("status", "--json")
	var object struct {
		Hooks string `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(status.Stdout), &object); err != nil {
		t.Fatalf("kitsu status --json (exit %d) did not print one JSON object: %v\n%s", status.Code, err, status.Output())
	}
	if object.Hooks != "installed" {
		t.Errorf("kitsu status --json holds hooks %q, want installed:\n%s", object.Hooks, status.Stdout)
	}
}
