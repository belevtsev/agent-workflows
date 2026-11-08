//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestHookPrePublishFailsOpenWithinItsDeadline requires that a source capture
// whose Git probe hangs cannot hold the pre-publish hook past its own
// deadline, and that the hook then exits 0 without a decision. The hook's
// own Git commands run normally; only the capture's probes (the only ones
// that pass core.untrackedCache=false) hang, far longer than the deadline.
func TestHookPrePublishFailsOpenWithinItsDeadline(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	repo := hkRepo(t)
	source := []byte("verified source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
		t.Fatal(err)
	}
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	h.assessAndVerify(t, record, source)

	marker := filepath.Join(t.TempDir(), "hits")
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"for arg do\n" +
		"  if [ \"$arg\" = 'core.untrackedCache=false' ]; then\n" +
		"    echo hit >> '" + marker + "'\n" +
		"    exec sleep 300\n" +
		"  fi\n" +
		"done\n" +
		"exec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	const deadline = 2 * time.Second
	h.config.prePublishTimeout = deadline
	started := time.Now()
	got := h.prePublish(t, repo, "git commit -m x")
	elapsed := time.Since(started)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the hook never reached a source capture: %v", err)
	}
	hkExpectAllow(t, got)
	// The capture's own probe budget is a minute; only the hook deadline
	// can end it this early.
	if elapsed > deadline+10*time.Second {
		t.Fatalf("hook took %s with a %s deadline", elapsed, deadline)
	}
}
