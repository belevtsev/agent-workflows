//go:build windows

package platform_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Requirement "Programs that a system lacks", scenario "A check written for
// another system".
func TestCheckWrittenForAnotherSystem(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"unit\"\nrun = [\"sh\", \"unit.sh\"]\n")
	// Any shell that ran unit.sh would leave this file.
	marker := filepath.Join(t.TempDir(), "unit-ran")
	repo.Write("unit.sh", "echo ran > '"+filepath.ToSlash(marker)+"'\n")
	path := pathWithoutSh(t, repo)
	for _, dir := range filepath.SplitList(path) {
		if holds(dir, "sh") {
			t.Fatalf("PATH still holds sh in %s", dir)
		}
	}
	repo.Setenv("PATH", path)

	result := repo.CW("run")

	if got := outcome(t, repo, "unit"); got != "failed" {
		t.Errorf("unit ended %q, want failed", got)
	}
	if !lineWith(result.Output(), "sh", "not found") {
		t.Errorf("no line of the output says that sh was not found")
	}
	if exists(marker) {
		t.Errorf("unit.sh was executed: cw found another shell")
	}
	if t.Failed() {
		t.Logf("PATH: %s", strings.ReplaceAll(path, ";", "\n  "))
		t.Logf("cw run (exit status %d):\n%s", result.Code, result.Output())
	}
}
