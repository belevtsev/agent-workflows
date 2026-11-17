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
	maxCoreLines       = 6250
	maxRolesLines      = 4000
	maxAdviserLines    = 4000 // CLI advisers plus native typed providers and usage records.
	maxSkillLines      = 2000
	maxUXLines         = 3500 // Shared routing, help, preferences, forms and completion.
	maxOnboardingLines = 1200 // Local setup diagnostics and explicit host handoff.
	maxContextLines    = 2000 // Opt-in Go collection, briefs, compact checks and private artifacts.
	maxWorkerLines     = 1200 // Saved worker dispatch and local stage intervals; no model execution.
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
	core, roles, advisers, skills, ux, onboarding, contextLines, workers := 0, 0, 0, 0, 0, 0, 0, 0
	for _, file := range repoFiles(t) {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || strings.Contains(file, "/testdata/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte("\n"))
		if strings.HasPrefix(file, "internal/cli/worker_") {
			workers += lines
		} else if strings.HasPrefix(file, "internal/contextpack/") || strings.HasPrefix(file, "internal/cli/compact_") {
			contextLines += lines
		} else if strings.HasPrefix(file, "internal/onboarding/") || strings.HasPrefix(file, "internal/cli/onboarding") {
			onboarding += lines
		} else if strings.HasPrefix(file, "internal/cli/ux_") || strings.HasPrefix(file, "internal/shellwords/ux_") {
			ux += lines
		} else if strings.HasPrefix(file, "internal/skills/") || strings.HasPrefix(file, "internal/cli/skills") || strings.HasPrefix(file, "internal/roles/skills") {
			skills += lines
		} else if strings.HasPrefix(file, "internal/advisers/") || strings.HasPrefix(file, "internal/cli/adviser") || strings.HasPrefix(file, "internal/cli/consult") {
			advisers += lines
		} else if rolesCode(file) {
			roles += lines
		} else {
			core += lines
		}
	}
	t.Logf("gate: %d of %d lines; roles: %d of %d lines", core, maxCoreLines, roles, maxRolesLines)
	t.Logf("advisers: %d of %d lines", advisers, maxAdviserLines)
	t.Logf("skills: %d of %d lines", skills, maxSkillLines)
	t.Logf("CLI presentation: %d of %d lines", ux, maxUXLines)
	t.Logf("agent onboarding: %d of %d lines", onboarding, maxOnboardingLines)
	t.Logf("compact context: %d of %d lines", contextLines, maxContextLines)
	t.Logf("worker handoffs: %d of %d lines", workers, maxWorkerLines)
	if workers > maxWorkerLines {
		t.Errorf("worker handoffs hold %d lines of Go, want at most %d", workers, maxWorkerLines)
	}
	if contextLines > maxContextLines {
		t.Errorf("compact context holds %d lines of Go, want at most %d", contextLines, maxContextLines)
	}
	if core > maxCoreLines {
		t.Errorf("the gate holds %d lines of Go, want at most %d", core, maxCoreLines)
	}
	if roles > maxRolesLines {
		t.Errorf("the role workflow holds %d lines of Go, want at most %d", roles, maxRolesLines)
	}
	if advisers > maxAdviserLines {
		t.Errorf("advisers hold %d lines of Go, want at most %d", advisers, maxAdviserLines)
	}
	if skills > maxSkillLines {
		t.Errorf("skill integration holds %d lines of Go, want at most %d", skills, maxSkillLines)
	}
	if ux > maxUXLines {
		t.Errorf("CLI presentation holds %d lines of Go, want at most %d", ux, maxUXLines)
	}
	if onboarding > maxOnboardingLines {
		t.Errorf("agent onboarding holds %d lines of Go, want at most %d", onboarding, maxOnboardingLines)
	}
}
