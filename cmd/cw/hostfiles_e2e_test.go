package main

// End-to-end tests for test contract TC-1 (revision 5), command line.
//
// Requirement mapping:
//   E2E-1 TestHostfilesE2E/install_reinstall_uninstall (install step)
//   E2E-2 TestHostfilesE2E/install_reinstall_uninstall (reinstall step)
//   E2E-3 TestHostfilesE2E/install_reinstall_uninstall (uninstall step)
//   E2E-4 TestHostfilesE2E/install_usage_errors
//   E2E-5 TestHostfilesE2E/default_checkout
//   E2E-6 TestHostfilesE2E/migrate_v2_dry_run
//   E2E-7 TestHostfilesE2E/help_lists_v3_commands
//   E2E-8 TestHostfilesE2E/migrate_v2_keeps_v3_block
//   E2E-9 TestHostfilesE2E/uninstall_without_links_into_checkout (rev 5)
//   E2E-10 TestHostfilesE2E/relative_paths (rev 5)
//   CLI-1 TestHostfilesE2E/removed_v2_commands_and_flags, TestHostfilesE2E/dry_run_is_usage_error,
//         TestHostfilesE2E/version_json
//   CLI-2 TestHostfilesE2E/directory_precedence
//
// All identifiers are prefixed with hfe2e to stay clear of package main.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	hfe2eStart    = "<!-- agent-workflows-start -->"
	hfe2eEnd      = "<!-- agent-workflows-end -->"
	hfe2eBody     = "# Global rules\nrule one\n"
	hfe2eDefaults = "# Recommended Codex settings\nmodel = \"gpt-test\"\n\n[features]\nexample = true\n"
)

var hfe2eRegs = map[string]string{"alpha": "skills/alpha", "beta": "skills/group/beta"}

func TestHostfilesE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end test builds the cw binary")
	}
	bin := hfe2eBuild(t)

	t.Run("install_reinstall_uninstall", func(t *testing.T) {
		e := hfe2eNewEnv(t, bin)
		checkout := filepath.Join(e.root, "checkout")
		hfe2eWriteCheckout(t, checkout, true)
		skills := filepath.Join(e.home, ".agents", "skills")
		agentsMD := filepath.Join(e.codex, "AGENTS.md")

		// E2E-1
		stdout, stderr, code := e.run(t, "install", "--host", "codex", "--from", checkout)
		if code != 0 {
			t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		for name, dir := range hfe2eRegs {
			hfe2eAssertLink(t, filepath.Join(skills, name), filepath.Join(checkout, dir))
		}
		wantBlock := hfe2eStart + "\n" + hfe2eBody + hfe2eEnd + "\n"
		if got := hfe2eRead(t, agentsMD); got != wantBlock {
			t.Errorf("AGENTS.md = %q, want %q", got, wantBlock)
		}
		if !strings.Contains(stdout, strings.TrimSpace(hfe2eDefaults)) {
			t.Errorf("stdout does not contain codex-defaults.toml text\nstdout:\n%s", stdout)
		}
		hfe2eAssertAbsent(t, filepath.Join(e.codex, "config.toml"))

		// E2E-2
		before := hfe2eSnapshot(t, e.home, e.codex)
		stdout, stderr, code = e.run(t, "install", "--host", "codex", "--from", checkout)
		if code != 0 {
			t.Fatalf("second install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		hfe2eDiff(t, before, hfe2eSnapshot(t, e.home, e.codex))

		// E2E-3
		stdout, stderr, code = e.run(t, "uninstall", "--host", "codex", "--from", checkout)
		if code != 0 {
			t.Fatalf("uninstall exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		for name := range hfe2eRegs {
			hfe2eAssertAbsent(t, filepath.Join(skills, name))
		}
		if data, err := os.ReadFile(agentsMD); err == nil {
			if strings.Contains(string(data), hfe2eStart) || strings.Contains(string(data), hfe2eEnd) {
				t.Errorf("AGENTS.md still contains the managed block: %q", data)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("read AGENTS.md: %v", err)
		}
	})

	t.Run("install_usage_errors", func(t *testing.T) {
		// E2E-4
		for _, args := range [][]string{
			{"install", "--host", "claude"},
			{"install"},
		} {
			e := hfe2eNewEnv(t, bin)
			stdout, stderr, code := e.run(t, args...)
			if code != 2 {
				t.Errorf("cw %s exit %d, want 2\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
			}
		}
	})

	t.Run("default_checkout", func(t *testing.T) {
		// E2E-5
		e := hfe2eNewEnv(t, bin)
		checkout := filepath.Join(e.dataHome, "agent-workflows", "src")
		hfe2eWriteCheckout(t, checkout, false)
		stdout, stderr, code := e.run(t, "install", "--host", "codex")
		if code != 0 {
			t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		skills := filepath.Join(e.home, ".agents", "skills")
		for name, dir := range hfe2eRegs {
			hfe2eAssertLink(t, filepath.Join(skills, name), filepath.Join(checkout, dir))
		}
		stdout, stderr, code = e.run(t, "uninstall", "--host", "codex")
		if code != 0 {
			t.Fatalf("uninstall exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		for name := range hfe2eRegs {
			hfe2eAssertAbsent(t, filepath.Join(skills, name))
		}
	})

	t.Run("migrate_v2_dry_run", func(t *testing.T) {
		// E2E-6
		e := hfe2eNewEnv(t, bin)
		want := hfe2eWriteV2(t, e)
		// Snapshot the trees a migration acts on (not the whole XDG state home, where cw may
		// keep unrelated bookkeeping of its own).
		st := filepath.Join(e.stateHome, "agent-workflows")
		roots := []string{
			filepath.Join(e.home, ".claude"), filepath.Join(e.home, ".agents"), filepath.Join(e.home, ".local"),
			e.codex, filepath.Join(st, "current"), filepath.Join(st, "releases"), filepath.Join(st, "state.json"),
			filepath.Join(st, "journal.json"), filepath.Join(st, "workflow-tasks"),
		}
		before := hfe2eSnapshot(t, roots...)
		stdout, stderr, code := e.run(t, "migrate-v2", "--dry-run")
		if code != 0 {
			t.Fatalf("migrate-v2 --dry-run exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		hfe2eDiff(t, before, hfe2eSnapshot(t, roots...))
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			var hits []string
			for _, p := range want {
				if hfe2eLineHasPath(line, p) {
					hits = append(hits, p)
				}
			}
			if len(hits) > 1 {
				t.Errorf("stdout line %d names more than one action path %v: %q", i+1, hits, line)
			}
		}
		for _, p := range want {
			found := false
			for _, line := range lines {
				if hfe2eLineHasPath(line, p) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("dry-run output does not name %s\nstdout:\n%s", p, stdout)
			}
		}
	})

	t.Run("help_lists_v3_commands", func(t *testing.T) {
		// E2E-7
		e := hfe2eNewEnv(t, bin)
		stdout, stderr, code := e.run(t)
		if code != 0 {
			t.Fatalf("cw exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		help := stdout + "\n" + stderr
		for _, cmd := range []string{"install", "uninstall", "migrate-v2"} {
			if !regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(cmd) + `($|[^A-Za-z0-9-])`).MatchString(help) {
				t.Errorf("help does not list %q\n%s", cmd, help)
			}
		}
		for _, cmd := range []string{"update", "rollback", "recover", "status"} {
			if hfe2eListsCommand(help, cmd) {
				t.Errorf("help still lists removed command %q\n%s", cmd, help)
			}
		}
	})

	t.Run("migrate_v2_keeps_v3_block", func(t *testing.T) {
		// E2E-8: without v2 state, migrate-v2 leaves the v3 install alone.
		e := hfe2eNewEnv(t, bin)
		checkout := filepath.Join(e.root, "checkout")
		hfe2eWriteCheckout(t, checkout, false)
		if stdout, stderr, code := e.run(t, "install", "--host", "codex", "--from", checkout); code != 0 {
			t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		agentsMD := filepath.Join(e.codex, "AGENTS.md")
		before := hfe2eRead(t, agentsMD)
		links := hfe2eSnapshot(t, filepath.Join(e.home, ".agents"))
		stdout, stderr, code := e.run(t, "migrate-v2")
		if code != 0 {
			t.Fatalf("migrate-v2 exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if got := hfe2eRead(t, agentsMD); got != before {
			t.Errorf("AGENTS.md changed by migrate-v2\n before: %q\n  after: %q", before, got)
		}
		hfe2eDiff(t, links, hfe2eSnapshot(t, filepath.Join(e.home, ".agents")))
	})

	t.Run("uninstall_without_links_into_checkout", func(t *testing.T) {
		// E2E-9: an uninstall that removes no link still removes the block, keeps links into the
		// install checkout and prints a hint naming the selected checkout and --from.
		cases := []struct {
			name       string
			args       func(e *hfe2eEnv, a, b string) []string
			selected   func(e *hfe2eEnv, a, b string) string
			wantNotice bool
		}{
			{"other --from",
				func(e *hfe2eEnv, a, b string) []string { return []string{"uninstall", "--host", "codex", "--from", b} },
				func(e *hfe2eEnv, a, b string) string { return b }, true},
			{"default checkout",
				func(e *hfe2eEnv, a, b string) []string { return []string{"uninstall", "--host", "codex"} },
				func(e *hfe2eEnv, a, b string) string { return filepath.Join(e.dataHome, "agent-workflows", "src") }, true},
			{"same --from removes links",
				func(e *hfe2eEnv, a, b string) []string { return []string{"uninstall", "--host", "codex", "--from", a} },
				func(e *hfe2eEnv, a, b string) string { return a }, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := hfe2eNewEnv(t, bin)
				a := filepath.Join(e.root, "checkout-a")
				b := filepath.Join(e.root, "checkout-b")
				hfe2eWriteCheckout(t, a, false)
				hfe2eWriteCheckout(t, b, false)
				if stdout, stderr, code := e.run(t, "install", "--host", "codex", "--from", a); code != 0 {
					t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
				}
				args := tc.args(e, a, b)
				stdout, stderr, code := e.run(t, args...)
				if code != 0 {
					t.Fatalf("cw %s exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
				}
				agentsMD := filepath.Join(e.codex, "AGENTS.md")
				if data, err := os.ReadFile(agentsMD); err == nil {
					if strings.Contains(string(data), hfe2eStart) || strings.Contains(string(data), hfe2eEnd) {
						t.Errorf("AGENTS.md still contains the managed block: %q", data)
					}
				} else if !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("read AGENTS.md: %v", err)
				}
				skills := filepath.Join(e.home, ".agents", "skills")
				for name, dir := range hfe2eRegs {
					if tc.wantNotice {
						hfe2eAssertLink(t, filepath.Join(skills, name), filepath.Join(a, dir))
					} else {
						hfe2eAssertAbsent(t, filepath.Join(skills, name))
					}
				}
				want := "no skill links into " + tc.selected(e, a, b)
				found, anyNotice := false, false
				for _, line := range strings.Split(stdout, "\n") {
					if strings.Contains(line, "no skill links into") {
						anyNotice = true
					}
					if strings.Contains(line, want) && strings.Contains(line, "--from") {
						found = true
					}
				}
				if tc.wantNotice && !found {
					t.Errorf("stdout has no line containing %q and %q\nstdout:\n%s", want, "--from", stdout)
				}
				if !tc.wantNotice && anyNotice {
					t.Errorf("stdout has a no-links notice although links were removed\nstdout:\n%s", stdout)
				}
			})
		}
	})

	t.Run("relative_paths", func(t *testing.T) {
		// E2E-10: relative --from, --home and --state-dir, run from a parent directory.
		t.Run("install_relative_from", func(t *testing.T) {
			e := hfe2eNewEnv(t, bin)
			checkout := filepath.Join(e.root, "checkout")
			hfe2eWriteCheckout(t, checkout, false)
			stdout, stderr, code := e.run(t, "install", "--host", "codex", "--from", "checkout")
			if code != 0 {
				t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			skills := filepath.Join(e.home, ".agents", "skills")
			for name, dir := range hfe2eRegs {
				target, err := os.Readlink(filepath.Join(skills, name))
				if err != nil {
					t.Errorf("readlink %s: %v", name, err)
					continue
				}
				if !filepath.IsAbs(target) {
					t.Errorf("link %s target %q is not absolute", name, target)
				}
				if want := filepath.Join(checkout, dir); !hfe2eSamePath(target, want) {
					t.Errorf("link %s target = %q, want %q (same directory)", name, target, want)
				}
			}
		})
		t.Run("migrate_relative_home_default_state_dir", func(t *testing.T) {
			e := hfe2eNewEnv(t, bin)
			e.noStateEnv = true // the default state dir then lives under the selected home
			alt := filepath.Join(hfe2eReal(t, e.root), "alt-home")
			st := filepath.Join(alt, ".local", "state", "agent-workflows")
			links := hfe2eWriteSmallV2(t, alt, filepath.Join(alt, ".claude"), st)
			stdout, stderr, code := e.run(t, "--home", "alt-home", "migrate-v2")
			if code != 0 {
				t.Fatalf("migrate-v2 exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			for _, p := range append(links, filepath.Join(st, "current")) {
				hfe2eAssertAbsent(t, p)
			}
		})
		t.Run("migrate_relative_state_dir", func(t *testing.T) {
			e := hfe2eNewEnv(t, bin)
			st := filepath.Join(hfe2eReal(t, e.root), "custom", "agent-workflows")
			links := hfe2eWriteSmallV2(t, e.home, filepath.Join(e.home, ".claude"), st)
			stdout, stderr, code := e.run(t, "--state-dir", filepath.Join("custom", "agent-workflows"), "migrate-v2")
			if code != 0 {
				t.Fatalf("migrate-v2 exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			for _, p := range append(links, filepath.Join(st, "current")) {
				hfe2eAssertAbsent(t, p)
			}
		})
	})

	t.Run("removed_v2_commands_and_flags", func(t *testing.T) {
		// CLI-1: removed commands and root flags are usage errors and create nothing under HOME.
		for _, args := range [][]string{
			{"update"},
			{"rollback"},
			{"recover"},
			{"status"},
			{"setup"},
			{"--manager-protocol"},
			{"--release-identity"},
			{"--verify-release", "v1.0.0"},
			{"--bundle-identity"},
			{"--preset", "balanced"},
			{"--settings", "s.json"},
			{"--prepared-handoff", "h"},
			{"--shell", "bash"},
			{"--adopt-personal-skills"},
			{"--forget"},
			{"--apply"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				e := hfe2eNewEnv(t, bin)
				stdout, stderr, code := e.run(t, args...)
				if code != 2 {
					t.Errorf("cw %s exit %d, want 2\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
				}
				hfe2eAssertEmptyDir(t, e.home)
			})
		}
	})

	t.Run("dry_run_is_usage_error", func(t *testing.T) {
		// CLI-1: install/uninstall --dry-run exit 2 and change nothing.
		t.Run("install", func(t *testing.T) {
			e := hfe2eNewEnv(t, bin)
			hfe2eWriteCheckout(t, filepath.Join(e.dataHome, "agent-workflows", "src"), true)
			before := hfe2eSnapshot(t, e.home, e.codex)
			stdout, stderr, code := e.run(t, "install", "--host", "codex", "--dry-run")
			if code != 2 {
				t.Errorf("install --dry-run exit %d, want 2\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			hfe2eDiff(t, before, hfe2eSnapshot(t, e.home, e.codex))
		})
		t.Run("uninstall", func(t *testing.T) {
			e := hfe2eNewEnv(t, bin)
			checkout := filepath.Join(e.dataHome, "agent-workflows", "src")
			hfe2eWriteCheckout(t, checkout, true)
			if stdout, stderr, code := e.run(t, "install", "--host", "codex", "--from", checkout); code != 0 {
				t.Fatalf("install exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			before := hfe2eSnapshot(t, e.home, e.codex)
			stdout, stderr, code := e.run(t, "uninstall", "--host", "codex", "--dry-run")
			if code != 2 {
				t.Errorf("uninstall --dry-run exit %d, want 2\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			hfe2eDiff(t, before, hfe2eSnapshot(t, e.home, e.codex))
		})
	})

	t.Run("version_json", func(t *testing.T) {
		// CLI-1: cw version prints exactly one JSON object with exactly these keys.
		e := hfe2eNewEnv(t, bin)
		stdout, stderr, code := e.run(t, "version")
		if code != 0 {
			t.Fatalf("cw version exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		dec := json.NewDecoder(strings.NewReader(stdout))
		var obj map[string]json.RawMessage
		if err := dec.Decode(&obj); err != nil {
			t.Fatalf("cw version stdout is not a JSON object: %v\n%s", err, stdout)
		}
		var extra json.RawMessage
		if err := dec.Decode(&extra); err == nil {
			t.Errorf("cw version printed more than one JSON value\n%s", stdout)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if got, want := strings.Join(keys, ","), "arch,built_at,os,revision,version"; got != want {
			t.Errorf("cw version keys = %s, want %s\n%s", got, want, stdout)
		}
	})

	t.Run("directory_precedence", func(t *testing.T) {
		// CLI-2: Codex home = --codex-home, else $CODEX_HOME, else $HOME/.codex;
		// skills home = --home, else $HOME.
		cases := []struct {
			name         string
			codexFlag    bool
			homeFlag     bool
			codexEnvSet  bool
			wantCodex    func(e *hfe2eEnv, flagCodex, flagHome string) string
			wantHome     func(e *hfe2eEnv, flagHome string) string
			notCodexDirs func(e *hfe2eEnv, flagCodex, flagHome string) []string
			notHomeDirs  func(e *hfe2eEnv, flagHome string) []string
		}{
			{
				name: "flags win over environment", codexFlag: true, homeFlag: true, codexEnvSet: true,
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return fc },
				wantHome:  func(e *hfe2eEnv, fh string) string { return fh },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{e.codex, filepath.Join(e.home, ".codex"), filepath.Join(fh, ".codex")}
				},
				notHomeDirs: func(e *hfe2eEnv, fh string) []string { return []string{e.home} },
			},
			{
				name: "codex flag without CODEX_HOME", codexFlag: true,
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return fc },
				wantHome:  func(e *hfe2eEnv, fh string) string { return e.home },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{filepath.Join(e.home, ".codex")}
				},
			},
			{
				name: "CODEX_HOME and HOME", codexEnvSet: true,
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return e.codex },
				wantHome:  func(e *hfe2eEnv, fh string) string { return e.home },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{filepath.Join(e.home, ".codex")}
				},
			},
			{
				name:      "HOME/.codex fallback",
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return filepath.Join(e.home, ".codex") },
				wantHome:  func(e *hfe2eEnv, fh string) string { return e.home },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{e.codex}
				},
			},
			{
				name: "home flag selects the Codex home fallback", homeFlag: true,
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return filepath.Join(fh, ".codex") },
				wantHome:  func(e *hfe2eEnv, fh string) string { return fh },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{e.codex, filepath.Join(e.home, ".codex")}
				},
				notHomeDirs: func(e *hfe2eEnv, fh string) []string { return []string{e.home} },
			},
			{
				name: "CODEX_HOME wins over home flag fallback", homeFlag: true, codexEnvSet: true,
				wantCodex: func(e *hfe2eEnv, fc, fh string) string { return e.codex },
				wantHome:  func(e *hfe2eEnv, fh string) string { return fh },
				notCodexDirs: func(e *hfe2eEnv, fc, fh string) []string {
					return []string{filepath.Join(fh, ".codex"), filepath.Join(e.home, ".codex")}
				},
				notHomeDirs: func(e *hfe2eEnv, fh string) []string { return []string{e.home} },
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := hfe2eNewEnv(t, bin)
				e.noCodexEnv = !tc.codexEnvSet
				checkout := filepath.Join(e.root, "checkout")
				hfe2eWriteCheckout(t, checkout, false)
				flagCodex := filepath.Join(e.root, "flag-codex")
				flagHome := filepath.Join(e.root, "flag-home")
				hfe2eMkdir(t, flagCodex)
				hfe2eMkdir(t, flagHome)
				args := []string{"install", "--host", "codex", "--from", checkout}
				if tc.codexFlag {
					args = append(args, "--codex-home", flagCodex)
				}
				if tc.homeFlag {
					args = append(args, "--home", flagHome)
				}
				stdout, stderr, code := e.run(t, args...)
				if code != 0 {
					t.Fatalf("cw %s exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
				}
				wantBlock := hfe2eStart + "\n" + hfe2eBody + hfe2eEnd + "\n"
				if got := hfe2eRead(t, filepath.Join(tc.wantCodex(e, flagCodex, flagHome), "AGENTS.md")); got != wantBlock {
					t.Errorf("AGENTS.md = %q, want %q", got, wantBlock)
				}
				for _, d := range tc.notCodexDirs(e, flagCodex, flagHome) {
					hfe2eAssertAbsent(t, filepath.Join(d, "AGENTS.md"))
				}
				skills := filepath.Join(tc.wantHome(e, flagHome), ".agents", "skills")
				for name, dir := range hfe2eRegs {
					hfe2eAssertLink(t, filepath.Join(skills, name), filepath.Join(checkout, dir))
				}
				if tc.notHomeDirs != nil {
					for _, d := range tc.notHomeDirs(e, flagHome) {
						hfe2eAssertAbsent(t, filepath.Join(d, ".agents"))
					}
				}
			})
		}
	})
}

func hfe2eAssertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("read %s: %v", dir, err)
		return
	}
	for _, ent := range ents {
		t.Errorf("unexpected entry created under HOME: %s", filepath.Join(dir, ent.Name()))
	}
}

// hfe2eListsCommand reports whether help lists cmd as a command: a line whose first field is cmd,
// or "cw <cmd>" anywhere.
func hfe2eListsCommand(help, cmd string) bool {
	if regexp.MustCompile(`\bcw\s+` + regexp.QuoteMeta(cmd) + `\b`).MatchString(help) {
		return true
	}
	for _, line := range strings.Split(help, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.TrimRight(fields[0], ":,") == cmd {
			return true
		}
	}
	return false
}

// hfe2eLineHasPath reports whether line contains p as a whole path token.
func hfe2eLineHasPath(line, p string) bool {
	isPathByte := func(b byte) bool {
		return b == '/' || b == '.' || b == '-' || b == '_' ||
			(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
	for from := 0; ; {
		i := strings.Index(line[from:], p)
		if i < 0 {
			return false
		}
		i += from
		j := i + len(p)
		if (i == 0 || !isPathByte(line[i-1])) && (j == len(line) || !isPathByte(line[j]) || (line[j] == '.' && (j+1 == len(line) || !isPathByte(line[j+1])))) {
			return true
		}
		from = i + 1
	}
}

// hfe2eReal resolves symlinks in p (darwin temp dirs live behind /var -> /private/var), so link
// targets are spelled the way a process that resolves relative paths with getcwd sees them.
func hfe2eReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// hfe2eSamePath reports whether a and b name the same path, lexically or after resolving symlinks.
func hfe2eSamePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// hfe2eWriteSmallV2 writes a minimal v2 state dir st (with state.json) and owned host links under
// home and claude; it returns the link paths.
func hfe2eWriteSmallV2(t *testing.T, home, claude, st string) []string {
	t.Helper()
	hfe2eWrite(t, filepath.Join(st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
	hfe2eSymlink(t, "releases/r1", filepath.Join(st, "current"))
	hfe2eWrite(t, filepath.Join(st, "state.json"), "{}\n", 0o644)
	links := map[string]string{
		filepath.Join(claude, "skills", "foo"):          filepath.Join(st, "current", "skills", "foo"),
		filepath.Join(home, ".agents", "skills", "foo"): filepath.Join(st, "current", "skills", "foo"),
		filepath.Join(home, ".local", "bin", "cw"):      filepath.Join(st, "current", "bin", "cw"),
	}
	var out []string
	for p, target := range links {
		hfe2eSymlink(t, target, p)
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

type hfe2eEnv struct {
	bin, root, home, codex, stateHome, dataHome, configHome string
	noCodexEnv                                              bool // omit CODEX_HOME from the environment
	noStateEnv                                              bool // omit XDG_STATE_HOME from the environment
}

func hfe2eNewEnv(t *testing.T, bin string) *hfe2eEnv {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	e := &hfe2eEnv{
		bin:        bin,
		root:       root,
		home:       filepath.Join(root, "home"),
		codex:      filepath.Join(root, "codex"),
		stateHome:  filepath.Join(root, "xdg-state"),
		dataHome:   filepath.Join(root, "xdg-data"),
		configHome: filepath.Join(root, "xdg-config"),
	}
	for _, d := range []string{e.home, e.codex, e.stateHome, e.dataHome, e.configHome} {
		hfe2eMkdir(t, d)
	}
	return e
}

func (e *hfe2eEnv) environ() []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.home,
		"XDG_DATA_HOME=" + e.dataHome,
		"XDG_CONFIG_HOME=" + e.configHome,
	}
	if !e.noStateEnv {
		env = append(env, "XDG_STATE_HOME="+e.stateHome)
	}
	if !e.noCodexEnv {
		env = append(env, "CODEX_HOME="+e.codex)
	}
	return env
}

func (e *hfe2eEnv) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Env = e.environ()
	cmd.Dir = e.root
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run cw %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

func hfe2eBuild(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cw")
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", bin, ".")
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func hfe2eWriteCheckout(t *testing.T, dir string, withDefaults bool) {
	t.Helper()
	for _, rel := range hfe2eRegs {
		hfe2eWrite(t, filepath.Join(dir, filepath.FromSlash(rel), "SKILL.md"), "# skill\n", 0o644)
	}
	hfe2eWrite(t, filepath.Join(dir, "global", "AGENTS.md"), hfe2eBody, 0o644)
	if withDefaults {
		hfe2eWrite(t, filepath.Join(dir, "global", "codex-defaults.toml"), hfe2eDefaults, 0o644)
	}
	data, err := json.Marshal(map[string]any{"registrations": hfe2eRegs, "global_instructions": "global/AGENTS.md"})
	if err != nil {
		t.Fatal(err)
	}
	hfe2eWrite(t, filepath.Join(dir, "skills-manifest.json"), string(data), 0o644)
}

// hfe2eWriteV2 lays out a v2 installation and returns the absolute paths a migration acts on.
func hfe2eWriteV2(t *testing.T, e *hfe2eEnv) []string {
	t.Helper()
	st := filepath.Join(e.stateHome, "agent-workflows")
	claude := filepath.Join(e.home, ".claude")
	hfe2eWrite(t, filepath.Join(st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o444)
	hfe2eWrite(t, filepath.Join(st, "releases", "r1", "bin", "cw"), "#!/bin/sh\n", 0o555)
	hfe2eWrite(t, filepath.Join(st, "releases", "r1", "agents", "cw-reviewer.md"), "r\n", 0o444)
	hfe2eSymlink(t, "releases/r1", filepath.Join(st, "current"))
	hfe2eWrite(t, filepath.Join(st, "state.json"), "{}\n", 0o644)
	hfe2eWrite(t, filepath.Join(st, "journal.json"), "[]\n", 0o644)
	hfe2eWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)

	links := map[string]string{
		filepath.Join(claude, "skills", "foo"):            filepath.Join(st, "current", "skills", "foo"),
		filepath.Join(e.home, ".agents", "skills", "foo"): filepath.Join(st, "current", "skills", "foo"),
		filepath.Join(claude, "agents", "cw-reviewer.md"): filepath.Join(st, "current", "agents", "cw-reviewer.md"),
		filepath.Join(e.home, ".local", "bin", "cw"):      filepath.Join(st, "current", "bin", "cw"),
	}
	want := []string{
		filepath.Join(claude, "CLAUDE.md"),
		filepath.Join(e.codex, "AGENTS.md"),
		filepath.Join(st, "current"),
		filepath.Join(st, "releases"),
		filepath.Join(st, "state.json"),
		filepath.Join(st, "journal.json"),
	}
	for p, target := range links {
		hfe2eSymlink(t, target, p)
		want = append(want, p)
	}
	hfe2eWrite(t, filepath.Join(claude, "CLAUDE.md"), "# mine\n\n"+hfe2eStart+"\nmanaged\n"+hfe2eEnd+"\n", 0o644)
	hfe2eWrite(t, filepath.Join(e.codex, "AGENTS.md"), hfe2eStart+"\nmanaged\n"+hfe2eEnd+"\n", 0o644)
	// Make the release tree read-only like a v2 release.
	for _, d := range []string{
		filepath.Join(st, "releases", "r1", "skills", "foo"),
		filepath.Join(st, "releases", "r1", "skills"),
		filepath.Join(st, "releases", "r1", "bin"),
		filepath.Join(st, "releases", "r1", "agents"),
		filepath.Join(st, "releases", "r1"),
	} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(want)
	return want
}

func hfe2eMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func hfe2eWrite(t *testing.T, p, content string, mode fs.FileMode) {
	t.Helper()
	hfe2eMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func hfe2eSymlink(t *testing.T, target, p string) {
	t.Helper()
	hfe2eMkdir(t, filepath.Dir(p))
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func hfe2eRead(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

func hfe2eAssertLink(t *testing.T, p, want string) {
	t.Helper()
	got, err := os.Readlink(p)
	if err != nil {
		t.Errorf("readlink %s: %v", p, err)
		return
	}
	if got != want {
		t.Errorf("readlink %s = %q, want %q", p, got, want)
	}
}

func hfe2eAssertAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s should not exist (lstat err %v)", p, err)
	}
}

// hfe2eSnapshot records kind, permissions, link targets and file contents under roots.
func hfe2eSnapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	s := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && p == root {
					s[p] = "absent"
					return nil
				}
				return err
			}
			info, err := os.Lstat(p)
			if err != nil {
				return err
			}
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				s[p] = "symlink -> " + target
			case info.IsDir():
				s[p] = fmt.Sprintf("dir %v", info.Mode().Perm())
			default:
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				s[p] = fmt.Sprintf("file %v %q", info.Mode().Perm(), data)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return s
}

func hfe2eDiff(t *testing.T, before, after map[string]string) {
	t.Helper()
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		b, okb := before[k]
		a, oka := after[k]
		switch {
		case !okb:
			t.Errorf("new entry %s: %s", k, a)
		case !oka:
			t.Errorf("entry removed %s (was %s)", k, b)
		case a != b:
			t.Errorf("entry changed %s\n before: %s\n  after: %s", k, b, a)
		}
	}
}
