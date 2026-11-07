package codexinstall_test

// Tests for test contract TC-1 (revision 1), package internal/codexinstall.
//
// Requirement mapping:
//   CI-1  TestMarkerConstants, TestUpsertBlockInsertsWhenNoMarkers
//   CI-2  TestUpsertBlockInsertsWhenNoMarkers
//   CI-3  TestUpsertBlockReplacesWellFormedPair
//   CI-4  TestMalformedMarkersAreRejected
//   CI-5  TestRemoveBlock, TestRemoveBlockRoundTrip
//   CI-6  TestInstallLinksEveryRegistrationAndInsertsBlock, TestInstallRelativeSourceIsMadeAbsolute,
//         TestInstallDoesNotResolveSourceSymlinks
//   CI-7  TestInstallLinksEveryRegistrationAndInsertsBlock, TestInstallUpdatesExistingAgentsMD,
//         TestInstallAddsMissingBodyNewline, TestInstallWritesThroughAgentsMDSymlink
//   CI-8  TestInstallIsIdempotent, TestInstallDoesNotResolveSourceSymlinks
//   CI-9  TestInstallOwnership
//   CI-10 TestInstallRefusesForeignOccupant
//   CI-11 TestInstallRefusesMalformedAgentsMD, TestInstallRejectsInvalidSource
//   CI-12 TestUninstallRemovesOwnedLinksOnly, TestUninstallWithoutUsableSource
//   CI-13 TestUninstallRemovesOwnedLinksOnly, TestUninstallDeletesEmptyAgentsMD,
//         TestUninstallReportsAbsentBlock, TestUninstallRefusesMalformedAgentsMD
//   CI-14 TestUninstallIsIdempotent
//   CI-15 TestInstallRecommendedConfig
//   CI-16 TestInstallWritesThroughDanglingAgentsMDSymlink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/codexinstall"
)

const (
	literalStart = "<!-- agent-workflows-start -->"
	literalEnd   = "<!-- agent-workflows-end -->"
	globalBody   = "# Global rules\n\nBe careful.\n"
)

// block is the CI-1 oracle: StartMarker, newline, body (with a final newline), EndMarker, newline.
func block(body string) string {
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return literalStart + "\n" + body + literalEnd + "\n"
}

// ---------------------------------------------------------------------------
// Pure helpers: UpsertBlock / RemoveBlock
// ---------------------------------------------------------------------------

// CI-1
func TestMarkerConstants(t *testing.T) {
	if codexinstall.StartMarker != literalStart {
		t.Errorf("StartMarker = %q, want %q", codexinstall.StartMarker, literalStart)
	}
	if codexinstall.EndMarker != literalEnd {
		t.Errorf("EndMarker = %q, want %q", codexinstall.EndMarker, literalEnd)
	}
}

// CI-1, CI-2
func TestUpsertBlockInsertsWhenNoMarkers(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		body string
		want string
	}{
		{"nil data", nil, "rule\n", literalStart + "\nrule\n" + literalEnd + "\n"},
		{"empty data body with final newline", []byte(""), "rule\n", literalStart + "\nrule\n" + literalEnd + "\n"},
		{"empty data body without final newline", []byte(""), "rule", literalStart + "\nrule\n" + literalEnd + "\n"},
		{"empty data multi-line body", []byte(""), "a\n\nb", literalStart + "\na\n\nb\n" + literalEnd + "\n"},
		{"data without final newline", []byte("abc"), "rule\n", "abc\n\n" + block("rule\n")},
		{"data with final newline", []byte("abc\n"), "rule\n", "abc\n\n" + block("rule\n")},
		{"data already ending in a blank line", []byte("abc\n\n"), "rule\n", "abc\n\n\n" + block("rule\n")},
		{"data is a single newline", []byte("\n"), "rule", "\n\n" + block("rule")},
		{"multi-line data", []byte("l1\nl2\n"), "x", "l1\nl2\n\n" + block("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := append([]byte(nil), tc.data...)
			out, status, err := codexinstall.UpsertBlock(tc.data, []byte(tc.body))
			if err != nil {
				t.Fatalf("UpsertBlock error: %v", err)
			}
			if status != "inserted" {
				t.Errorf("status = %q, want %q", status, "inserted")
			}
			if string(out) != tc.want {
				t.Errorf("output mismatch\n got: %q\nwant: %q", out, tc.want)
			}
			if !bytes.Equal(tc.data, orig) {
				t.Errorf("input data was mutated: %q -> %q", orig, tc.data)
			}
		})
	}
}

// CI-3
func TestUpsertBlockReplacesWellFormedPair(t *testing.T) {
	const before = "# mine\r\nkeep  trailing spaces  \n\n"
	const after = "\n\ttail line\nno final newline"
	cases := []struct {
		name   string
		data   string
		body   string
		want   string
		status string
	}{
		{"middle of file", before + block("old\n") + after, "new rules", before + block("new rules") + after, "replaced"},
		{"start of file", block("old\n") + "after\n", "new\n", block("new\n") + "after\n", "replaced"},
		{"end of file", "x\n\n" + block("old\n"), "new\n", "x\n\n" + block("new\n"), "replaced"},
		{"only the block", block("old\n"), "new\n", block("new\n"), "replaced"},
		{"user-edited content is owned",
			before + literalStart + "\nuser edit\nmore user edits\n\n" + literalEnd + "\n" + after,
			"new\n", before + block("new\n") + after, "replaced"},
		{"empty content between markers",
			before + literalStart + "\n" + literalEnd + "\n" + after,
			"new\n", before + block("new\n") + after, "replaced"},
		{"already current", before + block("same\n") + after, "same\n", before + block("same\n") + after, "unchanged"},
		{"already current, body lacks final newline", before + block("same") + after, "same", before + block("same") + after, "unchanged"},
		{"already current, only the block", block("same\n"), "same\n", block("same\n"), "unchanged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(tc.data)
			out, status, err := codexinstall.UpsertBlock(in, []byte(tc.body))
			if err != nil {
				t.Fatalf("UpsertBlock error: %v", err)
			}
			if status != tc.status {
				t.Errorf("status = %q, want %q", status, tc.status)
			}
			if string(out) != tc.want {
				t.Errorf("output mismatch\n got: %q\nwant: %q", out, tc.want)
			}
			if string(in) != tc.data {
				t.Errorf("input data was mutated")
			}
		})
	}
}

// CI-4
func TestMalformedMarkersAreRejected(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"only start marker", "top\n" + literalStart + "\nbody\n"},
		{"only end marker", "body\n" + literalEnd + "\n"},
		{"two start markers", literalStart + "\na\n" + literalStart + "\nb\n" + literalEnd + "\n"},
		{"two end markers", literalStart + "\na\n" + literalEnd + "\nb\n" + literalEnd + "\n"},
		{"end before start", literalEnd + "\na\n" + literalStart + "\n"},
		{"two well-formed pairs", block("a\n") + "mid\n" + block("b\n")},
		{"start marker not at start of line", "x " + literalStart + "\na\n" + literalEnd + "\n"},
		{"start marker indented", "  " + literalStart + "\na\n" + literalEnd + "\n"},
		{"end marker not at start of line", literalStart + "\na " + literalEnd + "\n"},
		{"start marker followed by text", literalStart + " trailing\na\n" + literalEnd + "\n"},
		{"end marker followed by text", literalStart + "\na\n" + literalEnd + " trailing\n"},
		{"end marker at EOF without newline", literalStart + "\na\n" + literalEnd},
		{"lone start marker mentioned in prose", "see " + literalStart + " for details\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := codexinstall.UpsertBlock([]byte(tc.data), []byte("x\n"))
			if !errors.Is(err, codexinstall.ErrMalformedBlock) {
				t.Errorf("UpsertBlock error = %v, want ErrMalformedBlock", err)
			}
			_, _, err = codexinstall.RemoveBlock([]byte(tc.data))
			if !errors.Is(err, codexinstall.ErrMalformedBlock) {
				t.Errorf("RemoveBlock error = %v, want ErrMalformedBlock", err)
			}
		})
	}
}

// CI-5
func TestRemoveBlock(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		want    string
		removed bool
	}{
		{"empty data", "", "", false},
		{"no markers", "hello\nworld", "hello\nworld", false},
		{"no markers with blank lines", "a\n\n\n", "a\n\n\n", false},
		{"block in the middle", "a\n" + block("m\n") + "b\n", "a\nb\n", true},
		{"block between blank lines", "a\n\n" + block("m\n") + "\nb\n", "a\n\n\nb\n", true},
		{"block at start", block("m\n") + "rest\n", "rest\n", true},
		{"block at end after separator", "abc\n\n" + block("m\n"), "abc\n", true},
		{"block at end without separator", "abc\n" + block("m\n"), "abc\n", true},
		{"only the block", block("m\n"), "", true},
		{"user-edited block", "top\n\n" + literalStart + "\nedited by user\n" + literalEnd + "\n", "top\n", true},
		{"preserves odd bytes around block", "a\r\n\t \n" + block("m\n") + "\n  z", "a\r\n\t \n\n  z", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, removed, err := codexinstall.RemoveBlock([]byte(tc.data))
			if err != nil {
				t.Fatalf("RemoveBlock error: %v", err)
			}
			if removed != tc.removed {
				t.Errorf("removed = %v, want %v", removed, tc.removed)
			}
			if string(out) != tc.want {
				t.Errorf("output mismatch\n got: %q\nwant: %q", out, tc.want)
			}
		})
	}
}

// CI-5 round trip.
func TestRemoveBlockRoundTrip(t *testing.T) {
	datas := []string{"", "\n", "abc\n", "abc\n\n", "l1\nl2\n", "x\r\n", "a\n\n\n"}
	bodies := []string{"b", "b\n", "multi\nline\n", "trailing blank\n\n"}
	for _, d := range datas {
		for _, b := range bodies {
			t.Run(fmt.Sprintf("%q+%q", d, b), func(t *testing.T) {
				up, _, err := codexinstall.UpsertBlock([]byte(d), []byte(b))
				if err != nil {
					t.Fatalf("UpsertBlock error: %v", err)
				}
				out, removed, err := codexinstall.RemoveBlock(up)
				if err != nil {
					t.Fatalf("RemoveBlock error: %v", err)
				}
				if !removed {
					t.Errorf("removed = false, want true")
				}
				if string(out) != d {
					t.Errorf("round trip mismatch\n got: %q\nwant: %q", out, d)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

type fx struct {
	root, source, home, codexHome string
}

func (f *fx) skillsDir() string { return filepath.Join(f.home, ".agents", "skills") }
func (f *fx) agentsMD() string  { return filepath.Join(f.codexHome, "AGENTS.md") }
func (f *fx) opts() codexinstall.Options {
	return codexinstall.Options{Source: f.source, Home: f.home, CodexHome: f.codexHome}
}

// newFx builds a checkout under root/checkout with the given registrations. Home exists; neither
// Home/.agents nor CodexHome exist.
func newFx(t *testing.T, regs map[string]string) *fx {
	return newFxBody(t, regs, globalBody)
}

func newFxBody(t *testing.T, regs map[string]string, body string) *fx {
	t.Helper()
	root := t.TempDir()
	f := &fx{
		root:      root,
		source:    filepath.Join(root, "checkout"),
		home:      filepath.Join(root, "home"),
		codexHome: filepath.Join(root, "codex"),
	}
	mustMkdir(t, f.home)
	writeCheckout(t, f.source, regs, body)
	return f
}

func writeCheckout(t *testing.T, source string, regs map[string]string, body string) {
	t.Helper()
	for _, dir := range regs {
		mustWrite(t, filepath.Join(source, filepath.FromSlash(dir), "SKILL.md"), "# skill\n", 0o644)
	}
	mustWrite(t, filepath.Join(source, "global", "AGENTS.md"), body, 0o644)
	writeManifest(t, source, regs)
}

func writeManifest(t *testing.T, source string, regs map[string]string) {
	t.Helper()
	m := map[string]any{
		"version":             3,
		"registrations":       regs,
		"global_instructions": "global/AGENTS.md",
		"ignored_field":       []string{"x", "y"},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(source, "skills-manifest.json"), string(data), 0o644)
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string, mode fs.FileMode) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, p string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

func assertLink(t *testing.T, p, want string) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Errorf("lstat %s: %v", p, err)
		return
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink (mode %v)", p, fi.Mode())
		return
	}
	got, err := os.Readlink(p)
	if err != nil {
		t.Errorf("readlink %s: %v", p, err)
		return
	}
	if got != want {
		t.Errorf("readlink %s = %q, want %q", p, got, want)
	}
}

func assertAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s should not exist (lstat err %v)", p, err)
	}
}

// snapshot records every entry under roots: kind, permissions, link targets, file contents,
// file modification times and inode numbers. Missing roots are recorded as absent.
func snapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	s := map[string]string{}
	for _, root := range roots {
		if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
			s[root] = "absent"
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				s[p] = "walk error: " + err.Error()
				return nil
			}
			info, err := os.Lstat(p)
			if err != nil {
				return err
			}
			var ino uint64
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				ino = uint64(st.Ino)
			}
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				s[p] = fmt.Sprintf("symlink -> %s ino=%d", target, ino)
			case info.IsDir():
				s[p] = fmt.Sprintf("dir %v", info.Mode().Perm())
			default:
				data, err := os.ReadFile(p)
				content := fmt.Sprintf("%q", data)
				if err != nil {
					content = "unreadable"
				}
				s[p] = fmt.Sprintf("file %v mtime=%d ino=%d %s", info.Mode().Perm(), info.ModTime().UnixNano(), ino, content)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return s
}

func diffSnapshots(t *testing.T, before, after map[string]string) {
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

func changesByPath(t *testing.T, r codexinstall.Report) map[string]codexinstall.Change {
	t.Helper()
	m := map[string]codexinstall.Change{}
	for _, c := range r.Changes {
		if prev, dup := m[c.Path]; dup {
			t.Errorf("duplicate change for %s: %+v and %+v", c.Path, prev, c)
		}
		m[c.Path] = c
	}
	return m
}

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

// CI-6, CI-7
func TestInstallLinksEveryRegistrationAndInsertsBlock(t *testing.T) {
	regs := map[string]string{
		"alpha":  "skills/alpha",
		"beta-2": "skills/group/beta-2",
		"9lives": "skills/9lives",
	}
	f := newFx(t, regs)
	rep, err := codexinstall.Install(f.opts())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if fi, err := os.Stat(f.codexHome); err != nil || !fi.IsDir() {
		t.Errorf("CodexHome not created: %v", err)
	}
	got := changesByPath(t, rep)
	for name, dir := range regs {
		p := filepath.Join(f.skillsDir(), name)
		want := filepath.Join(f.source, dir)
		assertLink(t, p, want)
		c, ok := got[p]
		if !ok {
			t.Errorf("no change reported for %s; changes: %+v", p, rep.Changes)
			continue
		}
		if c.Action != "link" || c.Target != want {
			t.Errorf("change for %s = %+v, want Action link Target %q", p, c, want)
		}
	}
	if len(rep.Changes) != len(regs) {
		t.Errorf("got %d changes, want %d: %+v", len(rep.Changes), len(regs), rep.Changes)
	}
	if rep.Instructions != f.agentsMD() {
		t.Errorf("Instructions = %q, want %q", rep.Instructions, f.agentsMD())
	}
	if rep.Block != "inserted" {
		t.Errorf("Block = %q, want inserted", rep.Block)
	}
	if got := readFile(t, f.agentsMD()); got != block(globalBody) {
		t.Errorf("AGENTS.md = %q, want %q", got, block(globalBody))
	}
	if fi, err := os.Stat(f.agentsMD()); err == nil && fi.Mode().Perm() != 0o644 {
		t.Errorf("AGENTS.md mode = %v, want 0644", fi.Mode().Perm())
	}
}

// CI-6: a relative Source is made absolute with filepath.Abs.
func TestInstallRelativeSourceIsMadeAbsolute(t *testing.T) {
	regs := map[string]string{"alpha": "skills/alpha"}
	f := newFx(t, regs)
	t.Chdir(f.root)
	abs, err := filepath.Abs("checkout")
	if err != nil {
		t.Fatal(err)
	}
	o := f.opts()
	o.Source = "checkout"
	if _, err := codexinstall.Install(o); err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := filepath.Join(abs, "skills", "alpha")
	if !filepath.IsAbs(want) {
		t.Fatalf("test bug: %q not absolute", want)
	}
	assertLink(t, filepath.Join(f.skillsDir(), "alpha"), want)
}

// CI-6, CI-8: symlinks in Source are not resolved, and the unresolved path is owned.
func TestInstallDoesNotResolveSourceSymlinks(t *testing.T) {
	regs := map[string]string{"alpha": "skills/alpha"}
	f := newFx(t, regs)
	alias := filepath.Join(f.root, "alias")
	mustSymlink(t, f.source, alias)
	o := f.opts()
	o.Source = alias
	if _, err := codexinstall.Install(o); err != nil {
		t.Fatalf("Install: %v", err)
	}
	link := filepath.Join(f.skillsDir(), "alpha")
	want := filepath.Join(alias, "skills", "alpha")
	assertLink(t, link, want)

	rep, err := codexinstall.Install(o)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	c := changesByPath(t, rep)[link]
	if c.Action != "unchanged" || c.Target != want {
		t.Errorf("second Install change = %+v, want unchanged with target %q", c, want)
	}
}

// CI-7
func TestInstallUpdatesExistingAgentsMD(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		mode     fs.FileMode
		want     string
		status   string
	}{
		{"text without block", "my notes\n", 0o600, "my notes\n\n" + block(globalBody), "inserted"},
		{"text without final newline", "my notes", 0o640, "my notes\n\n" + block(globalBody), "inserted"},
		{"stale block between user text",
			"top\n\n" + literalStart + "\nold managed text\n" + literalEnd + "\n\nbottom\n", 0o600,
			"top\n\n" + block(globalBody) + "\nbottom\n", "replaced"},
		{"user-edited block",
			literalStart + "\n" + globalBody + "user addition\n" + literalEnd + "\ntail\n", 0o644,
			block(globalBody) + "tail\n", "replaced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, map[string]string{"alpha": "skills/alpha"})
			mustWrite(t, f.agentsMD(), tc.existing, tc.mode)
			rep, err := codexinstall.Install(f.opts())
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			if rep.Block != tc.status {
				t.Errorf("Block = %q, want %q", rep.Block, tc.status)
			}
			if got := readFile(t, f.agentsMD()); got != tc.want {
				t.Errorf("AGENTS.md mismatch\n got: %q\nwant: %q", got, tc.want)
			}
			fi, err := os.Lstat(f.agentsMD())
			if err != nil {
				t.Fatal(err)
			}
			if !fi.Mode().IsRegular() {
				t.Errorf("AGENTS.md is no longer a regular file: %v", fi.Mode())
			}
			if fi.Mode().Perm() != tc.mode {
				t.Errorf("AGENTS.md mode = %v, want preserved %v", fi.Mode().Perm(), tc.mode)
			}
		})
	}
}

// CI-1, CI-7: the body file's missing final newline is added.
func TestInstallAddsMissingBodyNewline(t *testing.T) {
	f := newFxBody(t, map[string]string{"alpha": "skills/alpha"}, "no final newline")
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := literalStart + "\nno final newline\n" + literalEnd + "\n"
	if got := readFile(t, f.agentsMD()); got != want {
		t.Errorf("AGENTS.md = %q, want %q", got, want)
	}
}

// CI-7: a symlinked AGENTS.md is updated through the link and the link stays.
func TestInstallWritesThroughAgentsMDSymlink(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha"})
	realFile := filepath.Join(f.root, "dotfiles", "AGENTS.md")
	mustWrite(t, realFile, "x\n", 0o600)
	mustSymlink(t, realFile, f.agentsMD())
	rep, err := codexinstall.Install(f.opts())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if rep.Block != "inserted" {
		t.Errorf("Block = %q, want inserted", rep.Block)
	}
	assertLink(t, f.agentsMD(), realFile)
	if got, want := readFile(t, realFile), "x\n\n"+block(globalBody); got != want {
		t.Errorf("target file = %q, want %q", got, want)
	}
	if fi, err := os.Stat(realFile); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("target mode = %v, want 0600", fi.Mode().Perm())
	}
}

// CI-16: a dangling AGENTS.md symlink gets its target written and stays a symlink.
func TestInstallWritesThroughDanglingAgentsMDSymlink(t *testing.T) {
	t.Run("dangling from the start", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		target := filepath.Join(f.root, "dotfiles", "AGENTS.md")
		mustMkdir(t, filepath.Dir(target))
		mustSymlink(t, target, f.agentsMD())
		rep, err := codexinstall.Install(f.opts())
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		if rep.Block != "inserted" {
			t.Errorf("Block = %q, want inserted", rep.Block)
		}
		assertLink(t, f.agentsMD(), target)
		if got := readFile(t, target); got != block(globalBody) {
			t.Errorf("target = %q, want %q", got, block(globalBody))
		}
	})
	t.Run("uninstall then install", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		target := filepath.Join(f.root, "dotfiles", "AGENTS.md")
		mustWrite(t, target, block(globalBody), 0o644)
		mustSymlink(t, target, f.agentsMD())
		if _, err := codexinstall.Install(f.opts()); err != nil {
			t.Fatalf("first Install: %v", err)
		}
		assertLink(t, f.agentsMD(), target)

		rep, err := codexinstall.Uninstall(f.opts())
		if err != nil {
			t.Fatalf("Uninstall: %v", err)
		}
		if rep.Block != "removed" {
			t.Errorf("Uninstall Block = %q, want removed", rep.Block)
		}
		assertLink(t, f.agentsMD(), target) // the link stays, now dangling
		assertAbsent(t, target)

		rep, err = codexinstall.Install(f.opts())
		if err != nil {
			t.Fatalf("second Install: %v", err)
		}
		if rep.Block != "inserted" {
			t.Errorf("second Install Block = %q, want inserted", rep.Block)
		}
		assertLink(t, f.agentsMD(), target)
		if got := readFile(t, target); got != block(globalBody) {
			t.Errorf("target = %q, want %q", got, block(globalBody))
		}
		if fi, err := os.Stat(target); err == nil && fi.Mode().Perm() != 0o644 {
			t.Errorf("recreated target mode = %v, want 0644", fi.Mode().Perm())
		}
	})
}

// CI-8
func TestInstallIsIdempotent(t *testing.T) {
	regs := map[string]string{"alpha": "skills/alpha", "beta": "skills/nested/beta"}
	f := newFx(t, regs)
	mustWrite(t, f.agentsMD(), "user text\n", 0o600)
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	// Age AGENTS.md so that any rewrite is visible in the snapshot.
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(f.agentsMD(), old, old); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, f.home, f.codexHome, f.source)
	rep, err := codexinstall.Install(f.opts())
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	diffSnapshots(t, before, snapshot(t, f.home, f.codexHome, f.source))
	if rep.Block != "unchanged" {
		t.Errorf("Block = %q, want unchanged", rep.Block)
	}
	got := changesByPath(t, rep)
	for name, dir := range regs {
		p := filepath.Join(f.skillsDir(), name)
		c, ok := got[p]
		if !ok {
			t.Errorf("no change entry for %s", p)
			continue
		}
		if want := filepath.Join(f.source, dir); c.Action != "unchanged" || c.Target != want {
			t.Errorf("change for %s = %+v, want unchanged with target %q", p, c, want)
		}
	}
	for _, c := range rep.Changes {
		if c.Action != "unchanged" {
			t.Errorf("unexpected change on second Install: %+v", c)
		}
	}
}

// CI-9
func TestInstallOwnership(t *testing.T) {
	regs := map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"}
	f := newFx(t, regs)
	sd := f.skillsDir()
	mustMkdir(t, filepath.Join(f.source, "skills", "legacy"))
	mustMkdir(t, filepath.Join(f.root, "elsewhere"))
	rel := func(target string) string {
		r, err := filepath.Rel(sd, target)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Owned, registered, wrong target -> relink.
	mustSymlink(t, filepath.Join(f.source, "skills", "old-alpha"), filepath.Join(sd, "alpha"))
	mustSymlink(t, rel(filepath.Join(f.source, "skills", "legacy")), filepath.Join(sd, "beta"))
	// Owned, not registered -> unlink (absolute, relative, uncleaned).
	mustSymlink(t, filepath.Join(f.source, "skills", "stale"), filepath.Join(sd, "stale"))
	mustSymlink(t, rel(filepath.Join(f.source, "skills", "stale-rel")), filepath.Join(sd, "stale-rel"))
	mustSymlink(t, f.source+"/skills/x/../y", filepath.Join(sd, "stale-unclean"))
	// Not owned -> untouched.
	foreign := map[string]func(p string){
		"foreign":   func(p string) { mustSymlink(t, filepath.Join(f.root, "elsewhere", "foreign"), p) },
		"foreign-r": func(p string) { mustSymlink(t, "../../../elsewhere", p) },
		"sibling":   func(p string) { mustSymlink(t, f.source+"-old/skills/x", p) },
		"escape":    func(p string) { mustSymlink(t, f.source+"/skills/../../elsewhere", p) },
		"notes.txt": func(p string) { mustWrite(t, p, "notes\n", 0o644) },
		"mydir":     func(p string) { mustWrite(t, filepath.Join(p, "SKILL.md"), "mine\n", 0o644) },
	}
	var foreignPaths []string
	for name, mk := range foreign {
		p := filepath.Join(sd, name)
		mk(p)
		foreignPaths = append(foreignPaths, p)
	}
	before := snapshot(t, foreignPaths...)

	rep, err := codexinstall.Install(f.opts())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := changesByPath(t, rep)
	for name, dir := range regs {
		p := filepath.Join(sd, name)
		want := filepath.Join(f.source, dir)
		assertLink(t, p, want)
		if c := got[p]; c.Action != "relink" || c.Target != want {
			t.Errorf("change for %s = %+v, want relink with target %q", p, c, want)
		}
	}
	for _, name := range []string{"stale", "stale-rel", "stale-unclean"} {
		p := filepath.Join(sd, name)
		assertAbsent(t, p)
		if c := got[p]; c.Action != "unlink" || c.Target != "" {
			t.Errorf("change for %s = %+v, want unlink with empty target", p, c)
		}
	}
	diffSnapshots(t, before, snapshot(t, foreignPaths...))
	for _, p := range foreignPaths {
		if c, ok := got[p]; ok && c.Action != "skip" {
			t.Errorf("foreign entry %s reported as %+v", p, c)
		}
	}
}

// CI-10
func TestInstallRefusesForeignOccupant(t *testing.T) {
	occupants := []struct {
		name string
		make func(t *testing.T, f *fx, p string)
	}{
		{"regular file", func(t *testing.T, f *fx, p string) { mustWrite(t, p, "user file\n", 0o644) }},
		{"directory", func(t *testing.T, f *fx, p string) { mustWrite(t, filepath.Join(p, "SKILL.md"), "mine\n", 0o644) }},
		{"symlink outside source", func(t *testing.T, f *fx, p string) {
			target := filepath.Join(f.root, "elsewhere", "beta")
			mustMkdir(t, target)
			mustSymlink(t, target, p)
		}},
		{"dangling symlink outside source", func(t *testing.T, f *fx, p string) {
			mustSymlink(t, filepath.Join(f.root, "nowhere", "beta"), p)
		}},
		{"relative symlink outside source", func(t *testing.T, f *fx, p string) {
			mustSymlink(t, "../../../elsewhere/beta", p)
		}},
		{"symlink into prefix sibling of source", func(t *testing.T, f *fx, p string) {
			mustSymlink(t, f.source+"-other/skills/beta", p)
		}},
		{"symlink escaping source via dot-dot", func(t *testing.T, f *fx, p string) {
			mustSymlink(t, f.source+"/skills/../../elsewhere/beta", p)
		}},
	}
	for _, occ := range occupants {
		for _, codexExists := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/codexHomeExists=%v", occ.name, codexExists), func(t *testing.T) {
				f := newFx(t, map[string]string{"alpha": "skills/alpha", "beta": "skills/beta", "gamma": "skills/gamma"})
				sd := f.skillsDir()
				occ.make(t, f, filepath.Join(sd, "beta"))
				mustSymlink(t, filepath.Join(f.source, "skills", "stale"), filepath.Join(sd, "stale"))
				if codexExists {
					mustWrite(t, f.agentsMD(), "keep me\n", 0o644)
				}
				before := snapshot(t, f.home, f.codexHome, f.source)
				_, err := codexinstall.Install(f.opts())
				if !errors.Is(err, codexinstall.ErrNotOwned) {
					t.Fatalf("Install error = %v, want ErrNotOwned", err)
				}
				diffSnapshots(t, before, snapshot(t, f.home, f.codexHome, f.source))
			})
		}
	}
}

// CI-11: malformed markers in the existing AGENTS.md.
func TestInstallRefusesMalformedAgentsMD(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha"})
	mustSymlink(t, filepath.Join(f.source, "skills", "stale"), filepath.Join(f.skillsDir(), "stale"))
	mustWrite(t, f.agentsMD(), "notes\n"+literalStart+"\nhalf a block\n", 0o644)
	before := snapshot(t, f.home, f.codexHome)
	_, err := codexinstall.Install(f.opts())
	if !errors.Is(err, codexinstall.ErrMalformedBlock) {
		t.Fatalf("Install error = %v, want ErrMalformedBlock", err)
	}
	diffSnapshots(t, before, snapshot(t, f.home, f.codexHome))
}

// CI-11: unusable checkouts.
func TestInstallRejectsInvalidSource(t *testing.T) {
	rawManifest := func(regs any) string {
		data, err := json.Marshal(map[string]any{"registrations": regs, "global_instructions": "global/AGENTS.md"})
		if err != nil {
			panic(err)
		}
		return string(data)
	}
	withGood := func(name, dir string) string {
		return rawManifest(map[string]string{"good": "skills/good", name: dir})
	}
	type setupFn func(t *testing.T, f *fx) string // returns manifest content, "" to write none
	cases := []struct {
		name  string
		setup setupFn
	}{
		{"checkout missing", func(t *testing.T, f *fx) string {
			if err := os.RemoveAll(f.source); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
		{"manifest missing", func(t *testing.T, f *fx) string { return "" }},
		{"manifest not JSON", func(t *testing.T, f *fx) string { return "{not json" }},
		{"manifest unreadable", func(t *testing.T, f *fx) string {
			if os.Geteuid() == 0 {
				t.Skip("root can read mode 0000 files")
			}
			p := filepath.Join(f.source, "skills-manifest.json")
			mustWrite(t, p, rawManifest(map[string]string{"good": "skills/good"}), 0o000)
			t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
			return ""
		}},
		{"registrations missing", func(t *testing.T, f *fx) string {
			return `{"global_instructions": "global/AGENTS.md"}`
		}},
		{"registrations empty", func(t *testing.T, f *fx) string { return rawManifest(map[string]string{}) }},
		{"name upper case", func(t *testing.T, f *fx) string { return withGood("Alpha", "skills/alpha") }},
		{"name leading hyphen", func(t *testing.T, f *fx) string { return withGood("-alpha", "skills/alpha") }},
		{"name underscore", func(t *testing.T, f *fx) string { return withGood("al_pha", "skills/alpha") }},
		{"name space", func(t *testing.T, f *fx) string { return withGood("al pha", "skills/alpha") }},
		{"name slash", func(t *testing.T, f *fx) string { return withGood("al/pha", "skills/alpha") }},
		{"name empty", func(t *testing.T, f *fx) string { return withGood("", "skills/alpha") }},
		{"name leading dot", func(t *testing.T, f *fx) string { return withGood(".alpha", "skills/alpha") }},
		{"name with dot", func(t *testing.T, f *fx) string { return withGood("al.pha", "skills/alpha") }},
		{"name non-ASCII", func(t *testing.T, f *fx) string { return withGood("ålpha", "skills/alpha") }},
		{"path absolute", func(t *testing.T, f *fx) string {
			return withGood("alpha", filepath.Join(f.source, "skills", "alpha"))
		}},
		{"path with dot-dot inside source", func(t *testing.T, f *fx) string {
			return withGood("alpha", "skills/../skills/alpha")
		}},
		{"path escaping source", func(t *testing.T, f *fx) string {
			mustMkdir(t, filepath.Join(f.root, "outside"))
			return withGood("alpha", "../outside")
		}},
		{"path missing", func(t *testing.T, f *fx) string { return withGood("alpha", "skills/missing") }},
		{"path is a file", func(t *testing.T, f *fx) string { return withGood("alpha", "skills/alpha/SKILL.md") }},
		{"global instructions file missing", func(t *testing.T, f *fx) string {
			return `{"registrations": {"good": "skills/good"}, "global_instructions": "global/missing.md"}`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, map[string]string{"good": "skills/good", "alpha": "skills/alpha"})
			manifest := filepath.Join(f.source, "skills-manifest.json")
			if err := os.Remove(manifest); err != nil {
				t.Fatal(err)
			}
			if content := tc.setup(t, f); content != "" {
				mustWrite(t, manifest, content, 0o644)
			}
			before := snapshot(t, f.home, f.codexHome)
			_, err := codexinstall.Install(f.opts())
			if !errors.Is(err, codexinstall.ErrInvalidSource) {
				t.Fatalf("Install error = %v, want ErrInvalidSource", err)
			}
			diffSnapshots(t, before, snapshot(t, f.home, f.codexHome))
		})
	}
}

// CI-15
func TestInstallRecommendedConfig(t *testing.T) {
	const defaults = "# recommended\nmodel = \"gpt-test\"\n\n[features]\nx = true\n"
	t.Run("defaults present", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		mustWrite(t, filepath.Join(f.source, "global", "codex-defaults.toml"), defaults, 0o644)
		rep, err := codexinstall.Install(f.opts())
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		if rep.RecommendedConfig != defaults {
			t.Errorf("RecommendedConfig = %q, want %q", rep.RecommendedConfig, defaults)
		}
		assertAbsent(t, filepath.Join(f.codexHome, "config.toml"))
	})
	t.Run("defaults absent", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		rep, err := codexinstall.Install(f.opts())
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		if rep.RecommendedConfig != "" {
			t.Errorf("RecommendedConfig = %q, want empty", rep.RecommendedConfig)
		}
		assertAbsent(t, filepath.Join(f.codexHome, "config.toml"))
	})
	t.Run("existing config untouched", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		mustWrite(t, filepath.Join(f.source, "global", "codex-defaults.toml"), defaults, 0o644)
		cfg := filepath.Join(f.codexHome, "config.toml")
		mustWrite(t, cfg, "model = \"mine\"\n", 0o600)
		before := snapshot(t, cfg)
		if _, err := codexinstall.Install(f.opts()); err != nil {
			t.Fatalf("Install: %v", err)
		}
		diffSnapshots(t, before, snapshot(t, cfg))
	})
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

// CI-12, CI-13
func TestUninstallRemovesOwnedLinksOnly(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	mustWrite(t, f.agentsMD(), "mine\n", 0o600)
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	sd := f.skillsDir()
	mustSymlink(t, filepath.Join(f.source, "skills", "retired"), filepath.Join(sd, "stale"))
	mustSymlink(t, filepath.Join(f.root, "elsewhere", "ext"), filepath.Join(sd, "ext"))
	mustSymlink(t, f.source+"-other/skills/x", filepath.Join(sd, "sibling"))
	mustWrite(t, filepath.Join(sd, "notes.txt"), "notes\n", 0o644)
	mustWrite(t, filepath.Join(sd, "mine", "SKILL.md"), "mine\n", 0o644)
	mustWrite(t, filepath.Join(sd, "gamma"), "user file\n", 0o644)
	// gamma becomes a registration after Install, occupied by a foreign file.
	mustMkdir(t, filepath.Join(f.source, "skills", "gamma"))
	writeManifest(t, f.source, map[string]string{"alpha": "skills/alpha", "beta": "skills/beta", "gamma": "skills/gamma"})

	keep := []string{
		filepath.Join(sd, "ext"), filepath.Join(sd, "sibling"), filepath.Join(sd, "notes.txt"),
		filepath.Join(sd, "mine"), filepath.Join(sd, "gamma"),
	}
	before := snapshot(t, keep...)
	rep, err := codexinstall.Uninstall(f.opts())
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	diffSnapshots(t, before, snapshot(t, keep...))
	want := map[string]string{
		filepath.Join(sd, "alpha"): "unlink",
		filepath.Join(sd, "beta"):  "unlink",
		filepath.Join(sd, "stale"): "unlink",
		filepath.Join(sd, "gamma"): "skip",
	}
	for p, action := range want {
		if action == "unlink" {
			assertAbsent(t, p)
		}
	}
	got := changesByPath(t, rep)
	for p, action := range want {
		c, ok := got[p]
		if !ok {
			t.Errorf("no change reported for %s (want %s)", p, action)
			continue
		}
		if c.Action != action || c.Target != "" {
			t.Errorf("change for %s = %+v, want %s with empty target", p, c, action)
		}
	}
	for p, c := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected change reported: %+v", c)
		}
	}
	if rep.Block != "removed" {
		t.Errorf("Block = %q, want removed", rep.Block)
	}
	if rep.Instructions != f.agentsMD() {
		t.Errorf("Instructions = %q, want %q", rep.Instructions, f.agentsMD())
	}
	if got := readFile(t, f.agentsMD()); got != "mine\n" {
		t.Errorf("AGENTS.md = %q, want %q", got, "mine\n")
	}
}

// CI-12: Uninstall does not need the manifest.
func TestUninstallWithoutUsableSource(t *testing.T) {
	cases := []struct {
		name        string
		breakSource func(t *testing.T, f *fx)
	}{
		{"source removed", func(t *testing.T, f *fx) {
			if err := os.RemoveAll(f.source); err != nil {
				t.Fatal(err)
			}
		}},
		{"manifest corrupted", func(t *testing.T, f *fx) {
			mustWrite(t, filepath.Join(f.source, "skills-manifest.json"), "{broken", 0o644)
		}},
		{"manifest removed", func(t *testing.T, f *fx) {
			if err := os.Remove(filepath.Join(f.source, "skills-manifest.json")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
			mustWrite(t, f.agentsMD(), "mine\n", 0o644)
			if _, err := codexinstall.Install(f.opts()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			sd := f.skillsDir()
			foreign := filepath.Join(sd, "ext")
			mustSymlink(t, filepath.Join(f.root, "elsewhere"), foreign)
			userFile := filepath.Join(sd, "notes.txt")
			mustWrite(t, userFile, "n\n", 0o644)
			before := snapshot(t, foreign, userFile)
			tc.breakSource(t, f)

			rep, err := codexinstall.Uninstall(f.opts())
			if err != nil {
				t.Fatalf("Uninstall: %v", err)
			}
			assertAbsent(t, filepath.Join(sd, "alpha"))
			assertAbsent(t, filepath.Join(sd, "beta"))
			diffSnapshots(t, before, snapshot(t, foreign, userFile))
			got := changesByPath(t, rep)
			for _, name := range []string{"alpha", "beta"} {
				if c := got[filepath.Join(sd, name)]; c.Action != "unlink" {
					t.Errorf("change for %s = %+v, want unlink", name, c)
				}
			}
			for _, p := range []string{foreign, userFile} {
				if c, ok := got[p]; ok {
					t.Errorf("foreign entry reported: %+v", c)
				}
			}
			if rep.Block != "removed" {
				t.Errorf("Block = %q, want removed", rep.Block)
			}
			if got := readFile(t, f.agentsMD()); got != "mine\n" {
				t.Errorf("AGENTS.md = %q, want %q", got, "mine\n")
			}
		})
	}
}

// CI-13: AGENTS.md is deleted when nothing remains.
func TestUninstallDeletesEmptyAgentsMD(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha"})
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	rep, err := codexinstall.Uninstall(f.opts())
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if rep.Block != "removed" {
		t.Errorf("Block = %q, want removed", rep.Block)
	}
	assertAbsent(t, f.agentsMD())
	assertAbsent(t, filepath.Join(f.skillsDir(), "alpha"))
}

// CI-13: no block or no file -> "absent", nothing changes.
func TestUninstallReportsAbsentBlock(t *testing.T) {
	t.Run("nothing installed", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		before := snapshot(t, f.home, f.codexHome)
		rep, err := codexinstall.Uninstall(f.opts())
		if err != nil {
			t.Fatalf("Uninstall: %v", err)
		}
		if rep.Block != "absent" {
			t.Errorf("Block = %q, want absent", rep.Block)
		}
		for _, c := range rep.Changes {
			if c.Action == "unlink" {
				t.Errorf("unexpected unlink: %+v", c)
			}
		}
		diffSnapshots(t, before, snapshot(t, f.home, f.codexHome))
	})
	t.Run("file without block", func(t *testing.T) {
		f := newFx(t, map[string]string{"alpha": "skills/alpha"})
		mustWrite(t, f.agentsMD(), "plain notes\n", 0o600)
		before := snapshot(t, f.home, f.codexHome)
		rep, err := codexinstall.Uninstall(f.opts())
		if err != nil {
			t.Fatalf("Uninstall: %v", err)
		}
		if rep.Block != "absent" {
			t.Errorf("Block = %q, want absent", rep.Block)
		}
		diffSnapshots(t, before, snapshot(t, f.home, f.codexHome))
	})
}

// CI-13: malformed markers -> ErrMalformedBlock, nothing changes (links included).
func TestUninstallRefusesMalformedAgentsMD(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha"})
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	mustWrite(t, f.agentsMD(), readFile(t, f.agentsMD())+literalStart+"\n", 0o644)
	before := snapshot(t, f.home, f.codexHome, f.source)
	_, err := codexinstall.Uninstall(f.opts())
	if !errors.Is(err, codexinstall.ErrMalformedBlock) {
		t.Fatalf("Uninstall error = %v, want ErrMalformedBlock", err)
	}
	diffSnapshots(t, before, snapshot(t, f.home, f.codexHome, f.source))
}

// CI-14
func TestUninstallIsIdempotent(t *testing.T) {
	f := newFx(t, map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	mustWrite(t, f.agentsMD(), "mine\n", 0o644)
	if _, err := codexinstall.Install(f.opts()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	mustWrite(t, filepath.Join(f.skillsDir(), "notes.txt"), "n\n", 0o644)
	if _, err := codexinstall.Uninstall(f.opts()); err != nil {
		t.Fatalf("first Uninstall: %v", err)
	}
	before := snapshot(t, f.home, f.codexHome, f.source)
	rep, err := codexinstall.Uninstall(f.opts())
	if err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
	diffSnapshots(t, before, snapshot(t, f.home, f.codexHome, f.source))
	if rep.Block != "absent" {
		t.Errorf("Block = %q, want absent", rep.Block)
	}
	for _, c := range rep.Changes {
		if c.Action == "unlink" {
			t.Errorf("unexpected unlink on second Uninstall: %+v", c)
		}
	}
}
