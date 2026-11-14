package commitgate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// unitLog is the log of the only run record of unit, as an absolute path.
func unitLog(t *testing.T, g *gate) string {
	t.Helper()
	files := g.RecordFiles("unit")
	if len(files) != 1 {
		t.Fatalf("unit has %d run records, want 1: %v", len(files), files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Log string `json:"log"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("%s: %v", files[0], err)
	}
	if record.Log == "" {
		t.Fatalf("the run record %s names no log", files[0])
	}
	if !filepath.IsAbs(record.Log) {
		return filepath.Join(g.StoreDir(), record.Log)
	}
	return record.Log
}

// Requirement "Denial message", scenario "What a denial says".
func TestWhatADenialSays(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "broken\n")
	g.mustRun(5, "unit")
	log := unitLog(t, g)

	result := g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	reason := result.Stderr
	assertLineWith(t, reason, "unit", "failed")
	if !strings.Contains(reason, log) && !strings.Contains(reason, filepath.Base(log)) {
		t.Errorf("the reason does not hold the log of the failed run %s:\n%s", log, reason)
	}
	assertLineWith(t, reason, "lint", "not run")
	if !strings.Contains(reason, "cw run") {
		t.Errorf("the reason does not hold the command cw run:\n%s", reason)
	}
}

// Requirement "Denial message", scenario "What a denial does not say".
func TestWhatADenialDoesNotSay(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	var denials []cwtest.Result

	// A commit of content without a run.
	g.Write("src/app.txt", "ok")
	denials = append(denials, g.commitDenied("pre-commit", "commit", "-a", "-m", "change"))

	// A commit of content on which a check failed.
	g.Write("src/app.txt", "broken\n")
	g.mustRun(5)
	denials = append(denials, g.commitDenied("pre-commit", "commit", "-a", "-m", "change"))

	// A commit the hook cannot judge.
	g.Write("cw.toml", cwtest.DemoDeclaration+"tiemout = \"5m\"\n")
	g.Git("add", "cw.toml")
	denials = append(denials, g.commitDenied("pre-commit", "commit", "-m", "change"))
	g.Git("checkout", "-q", "HEAD", "--", "cw.toml")

	// A push of a commit made around the hook.
	g.commitNoVerify("skip")
	before := g.Git("ls-remote", g.remote, "refs/heads/main")
	g.resetExits()
	push := g.TryGit("push", "origin", "main")
	if push.Code == 0 {
		t.Errorf("git push exited 0, want a denial:\n%s", push.Output())
	}
	if after := g.Git("ls-remote", g.remote, "refs/heads/main"); after != before {
		t.Errorf("the push was sent: origin main moved from %q to %q", before, after)
	}
	g.assertHookExit("pre-push", 3)
	denials = append(denials, push)

	for i, denial := range denials {
		for _, word := range []string{"--no-verify", "observe", "uninstall"} {
			if strings.Contains(denial.Output(), word) {
				t.Errorf("denial %d holds %q:\n%s", i+1, word, denial.Output())
			}
		}
	}
}

// Requirement "Declaration change notice", scenario "A check is removed".
func TestRemovedCheckIsNoticed(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("cw.toml", "version = 1\n\n[[check]]\nid = \"lint\"\nrun = [\"sh\", \"lint.sh\"]\n")
	g.mustRun(0)
	result := g.commitRecorded("commit", "-a", "-m", "change")
	assertLineWith(t, result.Stderr, "unit", "no longer declared")
}

// Requirement "Declaration change notice", scenario "A check is added".
func TestAddedCheckIsNotNoticed(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("cw.toml", cwtest.DemoDeclaration+"\n[[check]]\nid = \"build\"\nrun = [\"sh\", \"-c\", \"exit 0\"]\n")
	g.Git("add", "cw.toml")
	g.mustRun(0)
	g.resetExits()
	result := g.TryGit("commit", "-m", "change")
	if g.exits != "" && len(g.hookExits("pre-commit")) == 0 {
		t.Errorf("the pre-commit hook did not call cw hook pre-commit:\n%s", result.Output())
	}
	output := result.Output()
	if strings.Contains(output, "no longer declared") || namesWord(output, "build") {
		t.Errorf("the hook printed a notice about the declaration:\n%s", output)
	}
}
