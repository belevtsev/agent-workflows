//go:build linux

package pilot

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var pilotReviewGitMarker = flag.String("pilot-review-git-marker", "", "offline Git confinement helper only")

func TestPilotPostprocessingDoesNotExecuteSolverGitCommands(t *testing.T) {
	path, output := unitManifest(t)
	var m manifest
	if err := readJSON(path, &m); err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "unconfined-git-command")
	m.Host.Argv = []string{program, "-test.run=^TestPilotReviewGitMutator$", "-pilot-review-git-marker=" + marker}
	if err := writeJSON(path, m, false); err != nil {
		t.Fatal(err)
	}
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	runErr := executeCell(t.Context(), m, p, p.Runs[0], output)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("host postprocessing executed a command from solver-controlled Git configuration")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	var result runResult
	if err := readJSON(filepath.Join(output, "runs", p.Runs[0].ID, "result.json"), &result); err != nil {
		t.Fatal(err)
	}
	patch, err := os.ReadFile(result.Patch.Path)
	if err != nil || !strings.Contains(string(patch), "newly-added.txt") {
		t.Fatalf("confinement discarded candidate source: %v", err)
	}
}

func TestPilotReviewGitMutator(t *testing.T) {
	if *pilotReviewGitMarker == "" {
		t.Skip("offline child only")
	}
	// The fake solver only installs this command. It never invokes git add or
	// git diff; a marker therefore proves execution by later host processing.
	script := "#!/bin/sh\nprintf unconfined > " + pilotReviewShellQuote(*pilotReviewGitMarker) + "\ncat\n"
	if err := os.WriteFile(filepath.Join(".git", "pilot-review-filter"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"filter.pilot-review.clean", "diff.external"} {
		command := exec.CommandContext(t.Context(), "git", "config", setting, "./.git/pilot-review-filter")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("install harmless Git command: %v: %s", err, output)
		}
	}
	if err := os.WriteFile(".gitattributes", []byte("newly-added.txt filter=pilot-review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	*pilotHelper = "host"
	TestPilotHelperProcess(t)
}

func pilotReviewShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestPilotExportPreservesIgnoredBaselineFiles(t *testing.T) {
	path, output := unitManifest(t)
	var m manifest
	if err := readJSON(path, &m); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Cases {
		for name, content := range map[string]string{".gitignore": "*.generated\n", "fixture.generated": "original tracked fixture\n"} {
			if err := os.WriteFile(filepath.Join(c.Snapshot, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m.Host.Argv = []string{program, "-test.run=^TestPilotReviewIgnoredFilesMutator$", "-pilot-test-helper=host"}
	if err := writeJSON(path, m, false); err != nil {
		t.Fatal(err)
	}
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeCell(t.Context(), m, p, p.Runs[0], output); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := readJSON(filepath.Join(output, "runs", p.Runs[0].ID, "result.json"), &result); err != nil {
		t.Fatal(err)
	}
	patch, err := os.ReadFile(result.Patch.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), "fixture.generated") || !strings.Contains(string(patch), "+edited tracked fixture") {
		t.Fatal("candidate export dropped a tracked baseline file covered by .gitignore")
	}
	if strings.Contains(string(patch), "cache.generated") {
		t.Fatal("candidate export force-added a newly generated ignored artifact")
	}
}

func TestPilotReviewIgnoredFilesMutator(t *testing.T) {
	if *pilotHelper != "host" {
		t.Skip("offline child only")
	}
	for name, content := range map[string]string{"fixture.generated": "edited tracked fixture\n", "cache.generated": "new ignored artifact\n"} {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	TestPilotHelperProcess(t)
}
