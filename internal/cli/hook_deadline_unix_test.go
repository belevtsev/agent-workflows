//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests for contract probe-abort revision 2, requirements R8-R9: the
// pre-publish hook's own deadline bounds source capture.

func hdQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hdInstallHangingCaptureGit puts a fake git first on PATH. Every capture
// probe (an invocation carrying core.untrackedCache=false) records its PID,
// touches the returned marker and then sleeps for minutes; every other
// invocation, such as the hook's own Git, runs the real git with the
// original PATH. Cleanup kills every recorded sleeper.
func hdInstallHangingCaptureGit(t *testing.T, realGit, sleep, originalPath string) string {
	t.Helper()
	dir := hkCanonicalTempDir(t)
	marker := filepath.Join(dir, "capture-reached")
	pids := filepath.Join(dir, "pids")
	script := strings.Join([]string{
		"#!/bin/sh",
		`case " $* " in`,
		"*' core.untrackedCache=false '*)",
		"\techo $$ >> " + hdQuote(pids),
		"\t: > " + hdQuote(marker),
		"\texec " + hdQuote(sleep) + " 300 ;;",
		"esac",
		"PATH=" + hdQuote(originalPath),
		"export PATH",
		"exec " + hdQuote(realGit) + ` "$@"`,
		"",
	}, "\n")
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pids)
		if err != nil {
			return
		}
		for _, field := range strings.Fields(string(data)) {
			if pid, err := strconv.Atoi(field); err == nil && pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	t.Setenv("PATH", bin+string(os.PathListSeparator)+originalPath)
	return marker
}

// R8: with a verified substantial task for the repository and every capture
// probe hanging for minutes, `cw hook pre-publish` for a commit allows (nil
// error, no output) within prePublishTimeout + 10 seconds, after reaching
// source capture. prePublishTimeout is 10 seconds because it also bounds the
// hook's real Git calls (contract timing rules).
func TestPrePublishCaptureDeadlineAllows(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep is not on PATH: %v", err)
	}
	originalPath := os.Getenv("PATH")

	h := newHKHarness(t)
	repo := hkRepo(t)
	source := []byte("source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	hkGit(t, repo, "add", "source.txt")
	hkGit(t, repo, "commit", "-q", "-m", "init")
	record := h.start(t, repo, hkSubstantialPlan)
	h.assessAndVerify(t, record, source)

	marker := hdInstallHangingCaptureGit(t, realGit, sleep, originalPath)
	const deadline = 10 * time.Second
	h.config.prePublishTimeout = deadline

	start := time.Now()
	got := h.prePublish(t, repo, "git commit -m x")
	elapsed := time.Since(start)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the hook never reached source capture: %v", err)
	}
	hkExpectAllow(t, got)
	// Hang guard only (contract timing rules: at least 20 s of slack): the
	// hook must not wait for the minutes-long capture probes.
	if limit := deadline + 20*time.Second; elapsed > limit {
		t.Fatalf("hook took %v, want at most %v", elapsed, limit)
	}
}

// R9: the default pre-publish deadline is positive and below the 30 second
// hook timeout, so cw exits on its own before the hook runner kills it.
func TestHookPublishTimeoutBelowHookTimeout(t *testing.T) {
	if hookPublishTimeout <= 0 || hookPublishTimeout >= 30*time.Second {
		t.Fatalf("hookPublishTimeout is %v, want positive and below 30s", hookPublishTimeout)
	}
}
