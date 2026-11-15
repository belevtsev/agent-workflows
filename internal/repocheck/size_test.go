package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep separate budgets for the gate, the role workflow and configurable
// advisers. Test files and testdata do not count.
const (
	maxCoreLines    = 6250
	maxRolesLines   = 4000
	maxAdviserLines = 3500
)

// rolesCode says whether a Go file belongs to the role workflow.
func rolesCode(file string) bool {
	for _, prefix := range []string{
		"internal/roles/", "internal/change/", "internal/tokens/",
		"internal/cli/roles", "internal/cli/change", "internal/cli/tokens",
	} {
		if strings.HasPrefix(file, prefix) {
			return true
		}
	}
	return false
}

func TestGoCodeStaysWithinItsSizeLimits(t *testing.T) {
	core, roles, advisers := 0, 0, 0
	for _, file := range repoFiles(t) {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || strings.Contains(file, "/testdata/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte("\n"))
		if strings.HasPrefix(file, "internal/advisers/") || strings.HasPrefix(file, "internal/cli/adviser") || strings.HasPrefix(file, "internal/cli/consult") {
			advisers += lines
		} else if rolesCode(file) {
			roles += lines
		} else {
			core += lines
		}
	}
	t.Logf("gate: %d of %d lines; roles: %d of %d lines", core, maxCoreLines, roles, maxRolesLines)
	t.Logf("advisers: %d of %d lines", advisers, maxAdviserLines)
	if core > maxCoreLines {
		t.Errorf("the gate holds %d lines of Go, want at most %d", core, maxCoreLines)
	}
	if roles > maxRolesLines {
		t.Errorf("the role workflow holds %d lines of Go, want at most %d", roles, maxRolesLines)
	}
	if advisers > maxAdviserLines {
		t.Errorf("advisers hold %d lines of Go, want at most %d", advisers, maxAdviserLines)
	}
}
