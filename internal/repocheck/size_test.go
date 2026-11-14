package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The size limits of the two changes under openspec/changes: the gate is at
// most 6,000 lines of Go, and the roles, the change record and the token
// report together at most 3,500. Test files and testdata do not count.
const (
	maxCoreLines  = 6000
	maxRolesLines = 3500
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
	core, roles := 0, 0
	for _, file := range repoFiles(t) {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || strings.Contains(file, "/testdata/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte("\n"))
		if rolesCode(file) {
			roles += lines
		} else {
			core += lines
		}
	}
	t.Logf("gate: %d of %d lines; roles: %d of %d lines", core, maxCoreLines, roles, maxRolesLines)
	if core > maxCoreLines {
		t.Errorf("the gate holds %d lines of Go, want at most %d", core, maxCoreLines)
	}
	if roles > maxRolesLines {
		t.Errorf("the role workflow holds %d lines of Go, want at most %d", roles, maxRolesLines)
	}
}
