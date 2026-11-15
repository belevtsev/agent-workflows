//go:build windows

package platform_test

import (
	"slices"
	"strings"
	"testing"
)

// fromPowerShell runs a command line in PowerShell. Each line that a program
// prints on standard error is turned back into its own text: PowerShell
// would otherwise wrap it in an error record.
func fromPowerShell(command string) []string {
	return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command",
		"& { " + command + " } 2>&1 | ForEach-Object { \"$_\" }; exit $LASTEXITCODE"}
}

// Requirement "Hooks on Windows", scenario "A commit from PowerShell and from
// the command prompt".
func TestCommitFromPowerShellAndFromTheCommandPrompt(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	before := head(repo)
	repo.Write("README.md", editedReadme)

	psTrace := traced(t, repo)
	ps := fromPowerShell("git commit -a -m docs")
	viaPS := repo.Run("", ps[0], ps[1:]...)
	// PowerShell carries standard error on its standard output here.
	viaPS.Stderr, viaPS.Stdout = viaPS.Output(), ""
	assertDenied(t, repo, before, viaPS, psTrace)

	cmdTrace := traced(t, repo)
	viaCmd := repo.Run("", "cmd", "/c", "git commit -a -m docs")
	assertDenied(t, repo, before, viaCmd, cmdTrace)

	psReason := reasonLines(viaPS.Stderr)
	cmdReason := reasonLines(viaCmd.Stderr)
	if len(cmdReason) == 0 || !slices.Equal(psReason, cmdReason) {
		t.Errorf("the reasons differ\nfrom PowerShell:\n  %s\nfrom cmd.exe:\n  %s",
			strings.Join(psReason, "\n  "), strings.Join(cmdReason, "\n  "))
	}
	if t.Failed() {
		t.Logf("from PowerShell (exit status %d):\n%s", viaPS.Code, viaPS.Output())
		t.Logf("from cmd.exe (exit status %d):\n%s", viaCmd.Code, viaCmd.Output())
	}
}

// Requirement "Hooks on Windows", scenario "A checked commit from PowerShell".
func TestCheckedCommitFromPowerShell(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	before := head(repo)
	repo.Write("README.md", editedReadme)

	ps := fromPowerShell("gatecub run; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; git commit -a -m docs")
	result := repo.Run("", ps[0], ps[1:]...)

	if after := head(repo); after == before {
		t.Errorf("HEAD did not move: the commit was not recorded")
	} else if subject := repo.Git("log", "-1", "--format=%s"); subject != "docs" {
		t.Errorf("the last commit is %q, want docs", subject)
	}
	if t.Failed() {
		t.Logf("PowerShell (exit status %d):\n%s", result.Code, result.Output())
	}
}
