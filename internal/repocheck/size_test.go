package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep separate budgets for the gate and its supporting subsystems.
// Test files and testdata do not count.
const (
	maxCoreLines         = 6431 // Includes strict pre-push framing with a denial of dropped input, held index copies and protections for runner approvals, local check approvals, host notices and the task ledger.
	maxRolesLines        = 4430 // Existing per-host settings, single-host assignments and repository role initialization.
	maxAdviserLines      = 4040 // Includes native advice and private configured story intake.
	maxSkillLines        = 2000
	maxUXLines           = 3790 // Shared routing, help, install defaults, forms, completion and approval.
	maxOnboardingLines   = 2014 // Local setup diagnostics, verified plugin installation and explicit host handoff.
	maxContextLines      = 2000 // Opt-in Go collection, briefs, compact checks and private artifacts.
	maxWorkerLines       = 1500 // Saved dispatch, local intervals, bounded QE hints and configured handback checks.
	maxProtocolLines     = 700  // Explicit native launch descriptions and bounded untrusted hand-backs.
	maxStateLines        = 750  // Bounded coordinator projections, retained baselines and section deltas.
	maxVerificationLines = 2200 // Behavioral plans, shared history and current local task observations.
	maxInsightLines      = 3000 // Read-only workflow diagnostics, optional notes and explicit budget metadata.
	maxConfiguredLines   = 2994 // Persisted role orchestration and the supervised runner protocol.
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

func configuredWorkflowCode(file string) bool {
	for _, prefix := range []string{
		"internal/harness/workflow", "internal/gitx/harness_workflow",
		"internal/cli/harness_workflow", "internal/cli/worker_managed", "internal/workflowrunner/",
	} {
		if strings.HasPrefix(file, prefix) {
			return true
		}
	}
	return false
}

func TestGoCodeStaysWithinItsSizeLimits(t *testing.T) {
	core, roles, advisers, skills, ux, onboarding, contextLines, workers, protocols, states := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	verificationLines := 0
	harnessLines := 0
	insightLines := 0
	releaseLines := 0
	configuredLines := 0
	for _, file := range repoFiles(t) {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || strings.Contains(file, "/testdata/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte("\n"))
		if configuredWorkflowCode(file) {
			configuredLines += lines
		} else if strings.HasPrefix(file, "internal/tickets/") || strings.HasPrefix(file, "internal/gitx/harness_") || strings.HasPrefix(file, "internal/harness/") || strings.HasPrefix(file, "internal/cli/harness_") {
			harnessLines += lines
		} else if strings.HasPrefix(file, "internal/insights/") || strings.HasPrefix(file, "internal/taskbudget/") || strings.HasPrefix(file, "internal/cli/usage_") {
			insightLines += lines
		} else if strings.HasPrefix(file, "internal/verification/") || strings.HasPrefix(file, "internal/cli/task_verification") || file == "internal/cli/check_plan.go" {
			verificationLines += lines
		} else if strings.HasPrefix(file, "internal/cli/coordinator_state") {
			states += lines
		} else if strings.HasPrefix(file, "internal/cli/worker_protocol") {
			protocols += lines
		} else if strings.HasPrefix(file, "internal/cli/worker_") {
			workers += lines
		} else if strings.HasPrefix(file, "internal/contextpack/") || strings.HasPrefix(file, "internal/cli/compact_") {
			contextLines += lines
		} else if strings.HasPrefix(file, "internal/onboarding/") || strings.HasPrefix(file, "internal/pluginsetup/") || strings.HasPrefix(file, "internal/cli/onboarding") {
			onboarding += lines
		} else if strings.HasPrefix(file, "internal/cli/ux_") || strings.HasPrefix(file, "internal/shellwords/ux_") {
			ux += lines
		} else if strings.HasPrefix(file, "internal/skills/") || strings.HasPrefix(file, "internal/cli/skills") || strings.HasPrefix(file, "internal/roles/skills") {
			skills += lines
		} else if strings.HasPrefix(file, "internal/advisers/") || strings.HasPrefix(file, "internal/cli/adviser") || strings.HasPrefix(file, "internal/cli/consult") {
			advisers += lines
		} else if strings.HasPrefix(file, "internal/releasecmd/") {
			releaseLines += lines
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
	t.Logf("worker protocols: %d of %d lines", protocols, maxProtocolLines)
	t.Logf("coordinator state: %d of %d lines", states, maxStateLines)
	t.Logf("task verification: %d of %d lines", verificationLines, maxVerificationLines)
	t.Logf("workflow insights: %d of %d lines", insightLines, maxInsightLines)
	const maxHarnessLines = 5961 // Includes PR repair, feature acceptance, verification order, reviewer input, the one-command task, receipt traces of replaced criteria and added authority, plain git commits and the pinned PR base repository.
	t.Logf("task harness: %d of %d lines", harnessLines, maxHarnessLines)
	const maxReleaseLines = 410 // Repository-only release assembly, integrity and publication.
	t.Logf("release automation: %d of %d lines", releaseLines, maxReleaseLines)
	t.Logf("configured workflows: %d of %d lines", configuredLines, maxConfiguredLines)
	if configuredLines > maxConfiguredLines {
		t.Errorf("configured workflows hold %d lines of Go, want at most %d", configuredLines, maxConfiguredLines)
	}
	if releaseLines > maxReleaseLines {
		t.Errorf("release automation holds %d lines of Go, want at most %d", releaseLines, maxReleaseLines)
	}
	if harnessLines > maxHarnessLines {
		t.Errorf("task harness holds %d lines of Go, want at most %d", harnessLines, maxHarnessLines)
	}
	if insightLines > maxInsightLines {
		t.Errorf("workflow insights hold %d lines of Go, want at most %d", insightLines, maxInsightLines)
	}
	if verificationLines > maxVerificationLines {
		t.Errorf("task verification holds %d lines of Go, want at most %d", verificationLines, maxVerificationLines)
	}
	if states > maxStateLines {
		t.Errorf("coordinator state holds %d lines of Go, want at most %d", states, maxStateLines)
	}
	if protocols > maxProtocolLines {
		t.Errorf("worker protocols hold %d lines of Go, want at most %d", protocols, maxProtocolLines)
	}
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
