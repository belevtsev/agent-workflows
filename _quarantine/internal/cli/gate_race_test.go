//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These tests use the seams gateAfterRead and gateLink to stand for another
// program that writes a hook between cw gate's check and its write, and for a
// file system without hard links.

// gtSwapAfterRead makes another program's hook appear at the path cw gate
// has just read, once, and returns how that hook looks.
func gtSwapAfterRead(t *testing.T) func() string {
	t.Helper()
	const foreign = "#!/bin/sh\necho another program's hook\n"
	swapped := ""
	gateAfterRead = func(path string) {
		gateAfterRead = nil
		swapped = path
		if err := os.WriteFile(path+".swap", []byte(foreign), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".swap", path); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { gateAfterRead = nil })
	return func() string {
		if swapped == "" {
			t.Fatal("cw gate wrote without reading the hook again")
		}
		return gtEntry(t, swapped)
	}
}

func TestGateNeverReplacesAHookWrittenAfterItsCheck(t *testing.T) {
	// Oracle (review finding): a hook another program writes between cw
	// gate's check and its write keeps its bytes, whether install found no
	// hook, or its own script to update, or uninstall found its own script to
	// remove; cw reports it as refused or left unchanged, and leaves no
	// temporary file in the hooks directory.
	gtIsolate(t, os.DevNull)
	foreign := "-rwx------ -> \n#!/bin/sh\necho another program's hook\n"
	for _, row := range []struct {
		name, command, want string
		prepare             func(t *testing.T, hook string)
	}{
		{"install over no hook", "install", "pre-commit: refused\n", func(*testing.T, string) {}},
		{"install over its own script", "install", "pre-commit: refused\n", func(t *testing.T, hook string) {
			hkWriteFile(t, filepath.Dir(hook), filepath.Base(hook), gtScript("pre-commit"))
		}},
		{"uninstall of its own script", "uninstall", "pre-commit: left unchanged (it changed while cw gate was removing it)\n", func(t *testing.T, hook string) {
			hkWriteFile(t, filepath.Dir(hook), filepath.Base(hook), gtScript("pre-commit"))
			if err := os.Chmod(hook, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			repo := hkRepo(t)
			hooks := filepath.Join(repo, ".git", "hooks")
			hkWriteFile(t, hooks, "pre-push", "#!/bin/sh\n")
			row.prepare(t, filepath.Join(hooks, "pre-commit"))
			swapped := gtSwapAfterRead(t)
			_, stdout, stderr := gtRun(t, row.command, "--repo", repo)
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("%s stdout:\n%s%s\nwant %q", row.command, stdout, stderr, row.want)
			}
			if got := swapped(); got != foreign {
				t.Fatalf("the hook written after the check is now\n%s\nwant\n%s", got, foreign)
			}
			if got := gtDir(t, hooks); strings.Contains(got, ".cw-") {
				t.Fatalf("temporary files left in %s:\n%s", hooks, got)
			}
		})
	}
}

func TestGateInstallFailsWithoutHardLinksAndChangesNothing(t *testing.T) {
	// Oracle (review finding): where the file system has no hard links, so
	// the hook cannot be put in place without the risk of replacing another,
	// install exits 1 with a message that says so and leaves the hooks
	// directory as it was, its own script with the old mode included.
	gtIsolate(t, os.DevNull)
	gateLink = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EPERM} }
	t.Cleanup(func() { gateLink = os.Link })
	for _, own := range []bool{false, true} {
		repo := hkRepo(t)
		hooks := filepath.Join(repo, ".git", "hooks")
		if own {
			hkWriteFile(t, hooks, "pre-commit", gtScript("pre-commit"))
		}
		before := gtDir(t, hooks)
		config, _ := testConfig(t)
		err := Execute(t.Context(), []string{"gate", "install", "--repo", repo}, config)
		if ExitCode(err) != ExitFailure || !strings.Contains(err.Error(), "links a hook into place, so that it never replaces a file another program put there, and this file system cannot") {
			t.Fatalf("install without hard links (own script %v): %v (exit %d)", own, err, ExitCode(err))
		}
		if got := gtDir(t, hooks); got != before {
			t.Fatalf("install without hard links (own script %v) changed %s:\n%s\nwant\n%s", own, hooks, got, before)
		}
	}
}
