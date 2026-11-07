package v2migrate_test

// Tests for test contract TC-1 (revisions 1-6), package internal/v2migrate.
//
// Requirement mapping:
//   MV-1 TestRunRemovesV2Installation (owned links, relative and dangling targets)
//   MV-2 TestRunRemovesV2Installation (every entry not acted on is byte-for-byte untouched)
//   MV-3 TestRunRemovesV2Installation, TestRunStripsBlockPreservingSurroundingText,
//        TestRunKeepsBlocksWithoutStateFiles (rev 4: strip only while state.json or journal.json exists)
//   MV-4 TestRunRemovesV2Installation (read-only release trees, kept StateDir entries)
//   MV-5 TestRunDryRun
//   MV-6 TestRunSecondRunIsNoop, TestRunMissingDirectories
//   MV-7 TestRunRefusesMalformedMarkers
//   MV-8 TestRunRecordedHomes
//   MV-10 TestRunPartialFailureReturnsCompletedPrefix; planned order checked in every assertActions
//   MV-11 TestRunStateDirBehindSymlinkedParent
//   MV-12 TestRunResolvesRelativeDirectories (rev 5)
//   MV-13 TestRunRefusesDirectoryThatIsNotAStateDir (rev 5),
//         TestRunRefusesWithAnySingleV2Entry (rev 6 amendment: one of current/releases/bases/runtime suffices)
//   MV-14 TestRunNothingToMigrateWithoutV2Entries (rev 6 cases b, c, missing/empty dirs),
//         TestRunRerunOfCustomStateDir (rev 6 case a)

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/v2migrate"
)

const (
	startMarker = "<!-- agent-workflows-start -->"
	endMarker   = "<!-- agent-workflows-end -->"
)

func block(body string) string {
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return startMarker + "\n" + body + endMarker + "\n"
}

type v2fx struct {
	root, home, claude, codex, state string
	want                             []v2migrate.Action
}

func (f *v2fx) opts(dry bool) v2migrate.Options {
	return v2migrate.Options{Home: f.home, ClaudeHome: f.claude, CodexHome: f.codex, StateDir: f.state, DryRun: dry}
}

func (f *v2fx) claudeMD() string { return filepath.Join(f.claude, "CLAUDE.md") }
func (f *v2fx) agentsMD() string { return filepath.Join(f.codex, "AGENTS.md") }

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

// makeReadOnly sets every directory under p to 0555 and every file to 0444 (bottom-up).
func makeReadOnly(t *testing.T, p string) {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
		case d.IsDir():
			dirs = append(dirs, q)
		default:
			return os.Chmod(q, 0o444)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			t.Fatal(err)
		}
	}
}

// makeWritable restores owner write permission on every directory so t.TempDir cleanup succeeds.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(q string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(q, 0o755)
		}
		return nil
	})
}

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { makeWritable(root) })
	return root
}

// buildV2 lays out a v2 installation with owned and foreign entries and records the actions a
// real run must perform.
func buildV2(t *testing.T) *v2fx {
	t.Helper()
	root := newRoot(t)
	f := &v2fx{
		root:   root,
		home:   filepath.Join(root, "home"),
		claude: filepath.Join(root, "home", ".claude"),
		codex:  filepath.Join(root, "codex"),
		state:  filepath.Join(root, "state", "agent-workflows"),
	}
	st := f.state

	// StateDir contents.
	mustWrite(t, filepath.Join(st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
	mustWrite(t, filepath.Join(st, "releases", "r1", "agents", "cw-reviewer.md"), "reviewer\n", 0o644)
	mustWrite(t, filepath.Join(st, "releases", "r1", "agents", "reviewer.md"), "plain\n", 0o644)
	mustWrite(t, filepath.Join(st, "releases", "r1", "bin", "cw"), "#!/bin/sh\n", 0o755)
	mustWrite(t, filepath.Join(st, "releases", "r1", "bin", "other"), "#!/bin/sh\n", 0o755)
	mustSymlink(t, "releases/r1", filepath.Join(st, "current"))
	mustWrite(t, filepath.Join(st, "bases", "b1", "base.txt"), "base\n", 0o644)
	mustWrite(t, filepath.Join(st, "runtime", "lock", "pid"), "123\n", 0o644)
	mustWrite(t, filepath.Join(st, "state.json"), "{}\n", 0o644)
	mustWrite(t, filepath.Join(st, "journal.json"), "[]\n", 0o644)
	mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{\"id\":\"t1\"}\n", 0o644)
	mustWrite(t, filepath.Join(st, "other-entry", "keep.txt"), "keep\n", 0o644)
	mustWrite(t, filepath.Join(st, "notes.txt"), "notes\n", 0o644)
	makeReadOnly(t, filepath.Join(st, "releases"))
	makeReadOnly(t, filepath.Join(st, "bases"))

	claudeSkills := filepath.Join(f.claude, "skills")
	agentSkills := filepath.Join(f.home, ".agents", "skills")
	claudeAgents := filepath.Join(f.claude, "agents")
	localBin := filepath.Join(f.home, ".local", "bin")
	mustMkdir(t, claudeSkills)
	relBar, err := filepath.Rel(claudeSkills, filepath.Join(st, "releases", "r1", "skills", "bar"))
	if err != nil {
		t.Fatal(err)
	}

	// MV-1 owned links.
	mustSymlink(t, filepath.Join(st, "current", "skills", "foo"), filepath.Join(claudeSkills, "foo"))
	mustSymlink(t, relBar, filepath.Join(claudeSkills, "bar")) // relative and dangling
	mustSymlink(t, st+"/current/skills/x/../foo", filepath.Join(claudeSkills, "unclean"))
	mustSymlink(t, filepath.Join(st, "current", "skills", "foo"), filepath.Join(agentSkills, "foo"))
	mustSymlink(t, filepath.Join(st, "releases", "r0", "skills", "gone"), filepath.Join(agentSkills, "gone")) // dangling
	mustSymlink(t, filepath.Join(st, "current", "agents", "cw-reviewer.md"), filepath.Join(claudeAgents, "cw-reviewer.md"))
	mustSymlink(t, filepath.Join(st, "current", "bin", "cw"), filepath.Join(localBin, "cw"))

	// MV-2 not owned.
	mustWrite(t, filepath.Join(claudeSkills, "mine", "SKILL.md"), "mine\n", 0o644)
	mustSymlink(t, filepath.Join(root, "elsewhere", "ext"), filepath.Join(claudeSkills, "ext"))
	mustSymlink(t, filepath.Join(root, "state", "agent-workflows-old", "skills", "x"), filepath.Join(claudeSkills, "prefix"))
	mustSymlink(t, st+"/../elsewhere/esc", filepath.Join(claudeSkills, "escape"))
	mustSymlink(t, filepath.Join(st, "current", "skills", "foo"), filepath.Join(claudeSkills, "nested", "deep"))
	mustWrite(t, filepath.Join(agentSkills, "file.txt"), "user\n", 0o644)
	mustWrite(t, filepath.Join(agentSkills, "userdir", "SKILL.md"), "user\n", 0o644)
	mustSymlink(t, filepath.Join(st, "current", "agents", "reviewer.md"), filepath.Join(claudeAgents, "reviewer.md"))
	mustSymlink(t, filepath.Join(st, "current", "agents", "cw-reviewer.md"), filepath.Join(claudeAgents, "cw-notes.txt"))
	mustSymlink(t, filepath.Join(root, "elsewhere", "cw-ext.md"), filepath.Join(claudeAgents, "cw-ext.md"))
	mustWrite(t, filepath.Join(claudeAgents, "cw-mine.md"), "my agent\n", 0o644)
	mustSymlink(t, filepath.Join(st, "current", "bin", "other"), filepath.Join(localBin, "other"))
	mustSymlink(t, filepath.Join(st, "current", "bin", "cw"), filepath.Join(f.claude, "cw"))

	// MV-3 instruction files.
	mustWrite(t, f.claudeMD(), "# user prefs\n\n"+block("managed\n"), 0o644)
	mustWrite(t, f.agentsMD(), block("managed\n"), 0o644)

	f.want = []v2migrate.Action{
		{Kind: "unlink", Path: filepath.Join(claudeSkills, "foo")},
		{Kind: "unlink", Path: filepath.Join(claudeSkills, "bar")},
		{Kind: "unlink", Path: filepath.Join(claudeSkills, "unclean")},
		{Kind: "unlink", Path: filepath.Join(agentSkills, "foo")},
		{Kind: "unlink", Path: filepath.Join(agentSkills, "gone")},
		{Kind: "unlink", Path: filepath.Join(claudeAgents, "cw-reviewer.md")},
		{Kind: "unlink", Path: filepath.Join(localBin, "cw")},
		{Kind: "strip-block", Path: f.claudeMD()},
		{Kind: "strip-block", Path: f.agentsMD()},
		{Kind: "remove", Path: filepath.Join(st, "current")},
		{Kind: "remove", Path: filepath.Join(st, "releases")},
		{Kind: "remove", Path: filepath.Join(st, "bases")},
		{Kind: "remove", Path: filepath.Join(st, "runtime")},
		{Kind: "remove", Path: filepath.Join(st, "state.json")},
		{Kind: "remove", Path: filepath.Join(st, "journal.json")},
	}
	return f
}

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

func sortedActions(a []v2migrate.Action) []string {
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, x.Kind+" "+x.Path)
	}
	sort.Strings(out)
	return out
}

// assertActions compares action sets and checks the MV-10 planned order of got: every unlink,
// then every strip-block, then every remove.
func assertActions(t *testing.T, got, want []v2migrate.Action) {
	t.Helper()
	g, w := sortedActions(got), sortedActions(want)
	if strings.Join(g, "\n") != strings.Join(w, "\n") {
		t.Errorf("actions mismatch\n got:\n  %s\nwant:\n  %s", strings.Join(g, "\n  "), strings.Join(w, "\n  "))
	}
	assertPlannedOrder(t, got)
}

func assertPlannedOrder(t *testing.T, got []v2migrate.Action) {
	t.Helper()
	rank := map[string]int{"unlink": 0, "strip-block": 1, "remove": 2}
	last := -1
	for i, a := range got {
		r, ok := rank[a.Kind]
		if !ok {
			t.Errorf("action %d has unknown kind %q", i, a.Kind)
			continue
		}
		if r < last {
			t.Errorf("action %d (%s %s) is out of planned order unlink, strip-block, remove: %v", i, a.Kind, a.Path, got)
			return
		}
		last = r
	}
}

// assertOnlyActedOn checks that, between before and after, exactly the entries under gone
// disappeared, the files in stripped changed (checked by the caller), and nothing else changed.
func assertOnlyActedOn(t *testing.T, before, after map[string]string, gone, stripped []string) {
	t.Helper()
	for _, p := range gone {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should be gone (lstat err %v)", p, err)
		}
	}
	isStripped := map[string]bool{}
	for _, p := range stripped {
		isStripped[p] = true
	}
	expected := map[string]string{}
	for p, v := range before {
		removed := false
		for _, g := range gone {
			if under(p, g) {
				removed = true
				break
			}
		}
		if !removed && !isStripped[p] {
			expected[p] = v
		}
	}
	got := map[string]string{}
	for p, v := range after {
		if !isStripped[p] {
			got[p] = v
		}
	}
	diffSnapshots(t, expected, got)
}

func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

// MV-1, MV-2, MV-3, MV-4
func TestRunRemovesV2Installation(t *testing.T) {
	f := buildV2(t)
	before := snapshot(t, f.root)
	got, err := v2migrate.Run(f.opts(false))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertActions(t, got, f.want)

	// Removed paths (and everything below them) are gone.
	var gone []string
	for _, a := range f.want {
		if a.Kind == "unlink" || a.Kind == "remove" {
			gone = append(gone, a.Path)
		}
	}
	gone = append(gone, f.agentsMD()) // nothing remained after stripping
	for _, p := range gone {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should be gone (lstat err %v)", p, err)
		}
	}

	// MV-3: surrounding text preserved.
	if got := readFile(t, f.claudeMD()); got != "# user prefs\n" {
		t.Errorf("CLAUDE.md = %q, want %q", got, "# user prefs\n")
	}

	// MV-2, MV-4: everything else is untouched (content, mode, link target, mtime, inode).
	after := snapshot(t, f.root)
	expected := map[string]string{}
	for p, v := range before {
		removed := false
		for _, g := range gone {
			if under(p, g) {
				removed = true
				break
			}
		}
		if !removed && p != f.claudeMD() {
			expected[p] = v
		}
	}
	delete(after, f.claudeMD())
	diffSnapshots(t, expected, after)

	// Spot checks for kept StateDir data.
	if got := readFile(t, filepath.Join(f.state, "workflow-tasks", "t1", "task.json")); got != "{\"id\":\"t1\"}\n" {
		t.Errorf("task store content changed: %q", got)
	}
	if got := readFile(t, filepath.Join(f.state, "other-entry", "keep.txt")); got != "keep\n" {
		t.Errorf("other StateDir entry changed: %q", got)
	}
}

// MV-3 (rev 4): block in the middle is stripped while state.json or journal.json exists; a file
// without markers is untouched.
func TestRunStripsBlockPreservingSurroundingText(t *testing.T) {
	for _, stateFile := range []string{"state.json", "journal.json"} {
		t.Run(stateFile, func(t *testing.T) {
			root := newRoot(t)
			o := v2migrate.Options{
				Home:       filepath.Join(root, "home"),
				ClaudeHome: filepath.Join(root, "claude"),
				CodexHome:  filepath.Join(root, "codex"),
				StateDir:   filepath.Join(root, "state"),
			}
			recorded := filepath.Join(o.StateDir, stateFile)
			mustWrite(t, recorded, "{}\n", 0o644)
			claudeMD := filepath.Join(o.ClaudeHome, "CLAUDE.md")
			agentsMD := filepath.Join(o.CodexHome, "AGENTS.md")
			mustWrite(t, claudeMD, "top\n\n"+block("m\n")+"\nbottom\n", 0o600)
			mustWrite(t, agentsMD, "plain codex notes\n", 0o644)
			keep := snapshot(t, agentsMD)

			got, err := v2migrate.Run(o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertActions(t, got, []v2migrate.Action{
				{Kind: "strip-block", Path: claudeMD},
				{Kind: "remove", Path: recorded},
			})
			if got := readFile(t, claudeMD); got != "top\n\n\nbottom\n" {
				t.Errorf("CLAUDE.md = %q, want %q", got, "top\n\n\nbottom\n")
			}
			diffSnapshots(t, keep, snapshot(t, agentsMD))
		})
	}
}

// MV-3 (rev 4) negative case: without state.json and journal.json, well-formed blocks are never
// touched, while owned links and other v2 entries are still handled.
func TestRunKeepsBlocksWithoutStateFiles(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry=%v", dry), func(t *testing.T) {
			root := newRoot(t)
			o := v2migrate.Options{
				Home:       filepath.Join(root, "home"),
				ClaudeHome: filepath.Join(root, "home", ".claude"),
				CodexHome:  filepath.Join(root, "codex"),
				StateDir:   filepath.Join(root, "state", "agent-workflows"),
				DryRun:     dry,
			}
			mustWrite(t, filepath.Join(o.StateDir, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
			mustSymlink(t, "releases/r1", filepath.Join(o.StateDir, "current"))
			link := filepath.Join(o.ClaudeHome, "skills", "foo")
			mustSymlink(t, filepath.Join(o.StateDir, "current", "skills", "foo"), link)
			claudeMD := filepath.Join(o.ClaudeHome, "CLAUDE.md")
			agentsMD := filepath.Join(o.CodexHome, "AGENTS.md")
			mustWrite(t, claudeMD, "mine\n\n"+block("managed\n"), 0o644)
			mustWrite(t, agentsMD, block("v3 block\n"), 0o644)
			keep := snapshot(t, claudeMD, agentsMD)

			got, err := v2migrate.Run(o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertActions(t, got, []v2migrate.Action{
				{Kind: "unlink", Path: link},
				{Kind: "remove", Path: filepath.Join(o.StateDir, "current")},
				{Kind: "remove", Path: filepath.Join(o.StateDir, "releases")},
			})
			diffSnapshots(t, keep, snapshot(t, claudeMD, agentsMD))
		})
	}
}

// MV-8: host roots recorded by v2 in journal.json or state.json are handled in addition to the
// selected homes.
func TestRunRecordedHomes(t *testing.T) {
	type layout struct {
		root, st, home, claude, codex string
		c, x, c2, x2                  string // recorded homes (c2/x2: named only by state.json in the precedence case)
	}
	journal := func(l layout) string {
		return fmt.Sprintf(`{"version": 2, "codex_home": %q, "claude_home": %q}`, l.x, l.c)
	}
	stateHosts := func(c, x string) string {
		return fmt.Sprintf(`{"release": "r1", "hosts": {"codex": {"home": %q}, "claude": {"home": %q}}}`, x, c)
	}
	cases := []struct {
		name string
		// write creates journal.json and/or state.json and returns the StateDir files written.
		write func(t *testing.T, l layout) []string
		// handled lists which recorded homes must be processed: "c" (and "x") or none.
		handled bool
		same    bool // the recorded homes equal the selected ones
	}{
		{"journal records homes", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "journal.json"), journal(l), 0o644)
			return []string{"journal.json"}
		}, true, false},
		{"state records homes", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "state.json"), stateHosts(l.c, l.x), 0o644)
			return []string{"state.json"}
		}, true, false},
		{"journal wins over state", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "journal.json"), journal(l), 0o644)
			mustWrite(t, filepath.Join(l.st, "state.json"), stateHosts(l.c2, l.x2), 0o644)
			return []string{"journal.json", "state.json"}
		}, true, false},
		{"journal is a JSON array", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "journal.json"), "[1, 2, 3]\n", 0o644)
			return []string{"journal.json"}
		}, false, false},
		{"state is not JSON", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "state.json"), "not json at all", 0o644)
			return []string{"state.json"}
		}, false, false},
		{"state hosts has another shape", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "state.json"), `{"hosts": ["codex", "claude"]}`, 0o644)
			return []string{"state.json"}
		}, false, false},
		{"recorded homes equal selected", func(t *testing.T, l layout) []string {
			mustWrite(t, filepath.Join(l.st, "journal.json"),
				fmt.Sprintf(`{"codex_home": %q, "claude_home": %q}`, l.codex, l.claude), 0o644)
			return []string{"journal.json"}
		}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRoot(t)
			l := layout{
				root:   root,
				st:     filepath.Join(root, "state", "agent-workflows"),
				home:   filepath.Join(root, "home"),
				claude: filepath.Join(root, "home", ".claude"),
				codex:  filepath.Join(root, "codex"),
				c:      filepath.Join(root, "old-claude"),
				x:      filepath.Join(root, "old-codex"),
				c2:     filepath.Join(root, "older-claude"),
				x2:     filepath.Join(root, "older-codex"),
			}
			mustWrite(t, filepath.Join(l.st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
			mustWrite(t, filepath.Join(l.st, "releases", "r1", "agents", "cw-r.md"), "r\n", 0o644)
			mustSymlink(t, "releases/r1", filepath.Join(l.st, "current"))

			// Selected homes.
			selLink := filepath.Join(l.claude, "skills", "bar")
			mustSymlink(t, filepath.Join(l.st, "current", "skills", "bar"), selLink)
			mustWrite(t, filepath.Join(l.claude, "CLAUDE.md"), block("managed\n"), 0o644)
			mustWrite(t, filepath.Join(l.codex, "AGENTS.md"), "sel\n\n"+block("managed\n"), 0o644)

			// Recorded homes (and the ones only state.json names).
			populate := func(c, x string) {
				mustSymlink(t, filepath.Join(l.st, "current", "skills", "foo"), filepath.Join(c, "skills", "foo"))
				mustSymlink(t, filepath.Join(root, "elsewhere", "ext"), filepath.Join(c, "skills", "ext"))
				mustSymlink(t, filepath.Join(l.st, "current", "agents", "cw-r.md"), filepath.Join(c, "agents", "cw-r.md"))
				mustSymlink(t, filepath.Join(l.st, "current", "agents", "cw-r.md"), filepath.Join(c, "agents", "r.md"))
				mustWrite(t, filepath.Join(c, "agents", "cw-mine.md"), "mine\n", 0o644)
				mustWrite(t, filepath.Join(c, "CLAUDE.md"), "old claude\n\n"+block("managed\n"), 0o644)
				mustWrite(t, filepath.Join(x, "AGENTS.md"), "old codex\n\n"+block("managed\n"), 0o644)
			}
			if !tc.same {
				populate(l.c, l.x)
				populate(l.c2, l.x2)
			}
			files := tc.write(t, l)

			want := []v2migrate.Action{
				{Kind: "unlink", Path: selLink},
				{Kind: "strip-block", Path: filepath.Join(l.claude, "CLAUDE.md")},
				{Kind: "strip-block", Path: filepath.Join(l.codex, "AGENTS.md")},
				{Kind: "remove", Path: filepath.Join(l.st, "current")},
				{Kind: "remove", Path: filepath.Join(l.st, "releases")},
			}
			for _, f := range files {
				want = append(want, v2migrate.Action{Kind: "remove", Path: filepath.Join(l.st, f)})
			}
			if tc.handled {
				want = append(want,
					v2migrate.Action{Kind: "unlink", Path: filepath.Join(l.c, "skills", "foo")},
					v2migrate.Action{Kind: "unlink", Path: filepath.Join(l.c, "agents", "cw-r.md")},
					v2migrate.Action{Kind: "strip-block", Path: filepath.Join(l.c, "CLAUDE.md")},
					v2migrate.Action{Kind: "strip-block", Path: filepath.Join(l.x, "AGENTS.md")},
				)
			}

			before := snapshot(t, root)
			dry, err := v2migrate.Run(v2migrate.Options{Home: l.home, ClaudeHome: l.claude, CodexHome: l.codex, StateDir: l.st, DryRun: true})
			if err != nil {
				t.Fatalf("dry Run: %v", err)
			}
			assertActions(t, dry, want)
			diffSnapshots(t, before, snapshot(t, root))

			got, err := v2migrate.Run(v2migrate.Options{Home: l.home, ClaudeHome: l.claude, CodexHome: l.codex, StateDir: l.st})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertActions(t, got, want)

			var gone, stripped []string
			for _, a := range want {
				if a.Kind == "strip-block" {
					stripped = append(stripped, a.Path)
				} else {
					gone = append(gone, a.Path)
				}
			}
			gone = append(gone, filepath.Join(l.claude, "CLAUDE.md")) // held only the block
			assertOnlyActedOn(t, before, snapshot(t, root), gone, stripped)
			if got := readFile(t, filepath.Join(l.codex, "AGENTS.md")); got != "sel\n" {
				t.Errorf("selected AGENTS.md = %q, want %q", got, "sel\n")
			}
			if tc.handled {
				if got := readFile(t, filepath.Join(l.c, "CLAUDE.md")); got != "old claude\n" {
					t.Errorf("recorded CLAUDE.md = %q, want %q", got, "old claude\n")
				}
				if got := readFile(t, filepath.Join(l.x, "AGENTS.md")); got != "old codex\n" {
					t.Errorf("recorded AGENTS.md = %q, want %q", got, "old codex\n")
				}
			}
		})
	}
}

// MV-10: a real run that fails at an action returns an error and exactly the completed prefix
// of the planned list; later actions are not performed.
func TestRunPartialFailureReturnsCompletedPrefix(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := buildV2(t)
	readOnly := filepath.Join(f.home, ".agents", "skills")
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	planned, err := v2migrate.Run(f.opts(true))
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	assertActions(t, planned, f.want)

	got, err := v2migrate.Run(f.opts(false))
	if err == nil {
		t.Fatalf("Run returned nil error although %s is read-only", readOnly)
	}
	if len(got) >= len(planned) {
		t.Fatalf("Run returned %d actions, want a strict prefix of the %d planned", len(got), len(planned))
	}
	for i := range got {
		if got[i] != planned[i] {
			t.Fatalf("returned actions are not a prefix of the planned list\n got: %v\nplan: %v", got, planned)
		}
	}
	failing := planned[len(got)]
	if failing.Kind != "unlink" || filepath.Dir(failing.Path) != readOnly {
		t.Errorf("first unperformed action = %+v, want an unlink inside %s", failing, readOnly)
	}
	performed := func(a v2migrate.Action) bool {
		switch a.Kind {
		case "strip-block":
			data, err := os.ReadFile(a.Path)
			return err != nil || !strings.Contains(string(data), startMarker)
		default:
			_, err := os.Lstat(a.Path)
			return errors.Is(err, fs.ErrNotExist)
		}
	}
	for _, a := range got {
		if !performed(a) {
			t.Errorf("returned action was not performed: %+v", a)
		}
	}
	for _, a := range planned[len(got):] {
		if performed(a) {
			t.Errorf("action after the failure was performed: %+v", a)
		}
	}
}

// MV-11: StateDir reached through a symlinked parent; targets spelled under either the link or
// the real path are owned.
func TestRunStateDirBehindSymlinkedParent(t *testing.T) {
	root := newRoot(t)
	realSt := filepath.Join(root, "real", "agent-workflows")
	mustWrite(t, filepath.Join(realSt, "releases", "r1", "skills", "a", "SKILL.md"), "# a\n", 0o644)
	mustSymlink(t, "releases/r1", filepath.Join(realSt, "current"))
	mustSymlink(t, filepath.Join(root, "real"), filepath.Join(root, "link"))
	st := filepath.Join(root, "link", "agent-workflows")
	o := v2migrate.Options{
		Home:       filepath.Join(root, "home"),
		ClaudeHome: filepath.Join(root, "home", ".claude"),
		CodexHome:  filepath.Join(root, "codex"),
		StateDir:   st,
	}
	skills := filepath.Join(o.ClaudeHome, "skills")
	viaReal := filepath.Join(skills, "via-real")
	viaLink := filepath.Join(skills, "via-link")
	agentLink := filepath.Join(o.Home, ".agents", "skills", "via-real-release")
	mustSymlink(t, filepath.Join(realSt, "current", "skills", "a"), viaReal)
	mustSymlink(t, filepath.Join(st, "current", "skills", "b"), viaLink)
	mustSymlink(t, filepath.Join(realSt, "releases", "r1", "skills", "a"), agentLink)
	foreign := []string{filepath.Join(skills, "real-sibling"), filepath.Join(skills, "real-other")}
	mustSymlink(t, filepath.Join(root, "real", "agent-workflows-old", "x"), foreign[0])
	mustSymlink(t, filepath.Join(root, "real", "other", "x"), foreign[1])
	keep := snapshot(t, foreign...)

	got, err := v2migrate.Run(o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertPlannedOrder(t, got)
	var unlinks []v2migrate.Action
	for _, a := range got {
		if a.Kind == "unlink" {
			unlinks = append(unlinks, a)
		}
	}
	assertActions(t, unlinks, []v2migrate.Action{
		{Kind: "unlink", Path: viaReal},
		{Kind: "unlink", Path: viaLink},
		{Kind: "unlink", Path: agentLink},
	})
	for _, p := range []string{viaReal, viaLink, agentLink} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should be unlinked (lstat err %v)", p, err)
		}
	}
	diffSnapshots(t, keep, snapshot(t, foreign...))
}

// MV-5
func TestRunDryRun(t *testing.T) {
	f := buildV2(t)
	before := snapshot(t, f.root)
	dry, err := v2migrate.Run(f.opts(true))
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	assertActions(t, dry, f.want)
	diffSnapshots(t, before, snapshot(t, f.root))

	realRun, err := v2migrate.Run(f.opts(false))
	if err != nil {
		t.Fatalf("real Run: %v", err)
	}
	assertActions(t, realRun, dry)
}

// MV-6
func TestRunSecondRunIsNoop(t *testing.T) {
	f := buildV2(t)
	if _, err := v2migrate.Run(f.opts(false)); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	before := snapshot(t, f.root)
	got, err := v2migrate.Run(f.opts(false))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("second Run returned actions: %v", sortedActions(got))
	}
	diffSnapshots(t, before, snapshot(t, f.root))
}

// MV-6
func TestRunMissingDirectories(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, o v2migrate.Options)
	}{
		{"nothing exists", func(t *testing.T, o v2migrate.Options) {}},
		{"only empty home", func(t *testing.T, o v2migrate.Options) { mustMkdir(t, o.Home) }},
		{"empty state dir and claude home", func(t *testing.T, o v2migrate.Options) {
			mustMkdir(t, o.StateDir)
			mustMkdir(t, o.ClaudeHome)
		}},
		{"only task store", func(t *testing.T, o v2migrate.Options) {
			mustWrite(t, filepath.Join(o.StateDir, "workflow-tasks", "t", "task.json"), "{}\n", 0o644)
		}},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", tc.name, dry), func(t *testing.T) {
				root := newRoot(t)
				o := v2migrate.Options{
					Home:       filepath.Join(root, "home"),
					ClaudeHome: filepath.Join(root, "home", ".claude"),
					CodexHome:  filepath.Join(root, "codex"),
					StateDir:   filepath.Join(root, "state", "agent-workflows"),
					DryRun:     dry,
				}
				tc.setup(t, o)
				before := snapshot(t, root)
				got, err := v2migrate.Run(o)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if len(got) != 0 {
					t.Errorf("Run returned actions: %v", sortedActions(got))
				}
				diffSnapshots(t, before, snapshot(t, root))
			})
		}
	}
}

// MV-7
func TestRunRefusesMalformedMarkers(t *testing.T) {
	cases := []struct {
		name    string
		file    func(f *v2fx) string
		content string
	}{
		{"CLAUDE.md lone start", (*v2fx).claudeMD, "# mine\n" + startMarker + "\nhalf\n"},
		{"CLAUDE.md end before start", (*v2fx).claudeMD, endMarker + "\nx\n" + startMarker + "\n"},
		{"AGENTS.md two starts", (*v2fx).agentsMD, block("a\n") + startMarker + "\n"},
		{"AGENTS.md end marker without newline", (*v2fx).agentsMD, startMarker + "\na\n" + endMarker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := buildV2(t)
			mustWrite(t, tc.file(f), tc.content, 0o644)
			before := snapshot(t, f.root)
			_, err := v2migrate.Run(f.opts(false))
			if err == nil {
				t.Fatalf("Run returned nil error for malformed markers")
			}
			diffSnapshots(t, before, snapshot(t, f.root))
		})
	}
}

// MV-12: relative Home, ClaudeHome, CodexHome and StateDir are resolved against the working
// directory; the run matches the absolute one and reports absolute paths.
func TestRunResolvesRelativeDirectories(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(fmt.Sprintf("dry=%v", dry), func(t *testing.T) {
			f := buildV2(t)
			t.Chdir(f.root)
			abs, err := filepath.Abs(".")
			if err != nil {
				t.Fatal(err)
			}
			rebase := func(p string) string {
				rel, err := filepath.Rel(f.root, p)
				if err != nil {
					t.Fatal(err)
				}
				return filepath.Join(abs, rel)
			}
			var want []v2migrate.Action
			for _, a := range f.want {
				want = append(want, v2migrate.Action{Kind: a.Kind, Path: rebase(a.Path)})
			}
			o := v2migrate.Options{
				Home:       "home",
				ClaudeHome: filepath.Join("home", ".claude"),
				CodexHome:  "codex",
				StateDir:   filepath.Join("state", "agent-workflows"),
				DryRun:     dry,
			}
			before := snapshot(t, f.root)
			got, err := v2migrate.Run(o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			for _, a := range got {
				if !filepath.IsAbs(a.Path) {
					t.Errorf("action path is not absolute: %+v", a)
				}
			}
			assertActions(t, got, want)
			if dry {
				diffSnapshots(t, before, snapshot(t, f.root))
				return
			}
			for _, a := range f.want {
				if a.Kind == "strip-block" {
					continue
				}
				if _, err := os.Lstat(a.Path); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s should be gone after %s (lstat err %v)", a.Path, a.Kind, err)
				}
			}
			if got := readFile(t, f.claudeMD()); got != "# user prefs\n" {
				t.Errorf("CLAUDE.md = %q, want %q", got, "# user prefs\n")
			}
			if got := readFile(t, filepath.Join(f.state, "workflow-tasks", "t1", "task.json")); got != "{\"id\":\"t1\"}\n" {
				t.Errorf("task store content changed: %q", got)
			}
		})
	}
}

// MV-13: a state directory with neither state.json nor journal.json and a base name other than
// agent-workflows is refused with ErrNotStateDir; nothing changes and no actions are returned.
func TestRunRefusesDirectoryThatIsNotAStateDir(t *testing.T) {
	type built struct {
		root string
		o    v2migrate.Options
		want []v2migrate.Action // for cases that proceed
	}
	build := func(t *testing.T, name string, stateFiles []string) built {
		t.Helper()
		root := newRoot(t)
		st := filepath.Join(root, "state", name)
		mustWrite(t, filepath.Join(st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
		mustSymlink(t, "releases/r1", filepath.Join(st, "current"))
		mustWrite(t, filepath.Join(st, "bases", "b1", "base.txt"), "base\n", 0o644)
		mustWrite(t, filepath.Join(st, "runtime", "lock"), "1\n", 0o644)
		mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)
		for _, f := range stateFiles {
			mustWrite(t, filepath.Join(st, f), "{}\n", 0o644)
		}
		home := filepath.Join(root, "home")
		claude := filepath.Join(home, ".claude")
		claudeLink := filepath.Join(claude, "skills", "foo")
		agentLink := filepath.Join(home, ".agents", "skills", "foo")
		binLink := filepath.Join(home, ".local", "bin", "cw")
		mustSymlink(t, filepath.Join(st, "current", "skills", "foo"), claudeLink)
		mustSymlink(t, filepath.Join(st, "releases", "r1", "skills", "foo"), agentLink)
		mustSymlink(t, filepath.Join(st, "current", "bin", "cw"), binLink)
		claudeMD := filepath.Join(claude, "CLAUDE.md")
		mustWrite(t, claudeMD, "mine\n\n"+block("managed\n"), 0o644)
		b := built{
			root: root,
			o:    v2migrate.Options{Home: home, ClaudeHome: claude, CodexHome: filepath.Join(root, "codex"), StateDir: st},
			want: []v2migrate.Action{
				{Kind: "unlink", Path: claudeLink},
				{Kind: "unlink", Path: agentLink},
				{Kind: "unlink", Path: binLink},
				{Kind: "remove", Path: filepath.Join(st, "current")},
				{Kind: "remove", Path: filepath.Join(st, "releases")},
				{Kind: "remove", Path: filepath.Join(st, "bases")},
				{Kind: "remove", Path: filepath.Join(st, "runtime")},
			},
		}
		if len(stateFiles) > 0 {
			b.want = append(b.want, v2migrate.Action{Kind: "strip-block", Path: claudeMD})
		}
		for _, f := range stateFiles {
			b.want = append(b.want, v2migrate.Action{Kind: "remove", Path: filepath.Join(st, f)})
		}
		return b
	}

	for _, name := range []string{"my-state", "agent-workflows-old", "workflows"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("refuse/%s/dry=%v", name, dry), func(t *testing.T) {
				b := build(t, name, nil)
				b.o.DryRun = dry
				before := snapshot(t, b.root)
				got, err := v2migrate.Run(b.o)
				if !errors.Is(err, v2migrate.ErrNotStateDir) {
					t.Errorf("Run error = %v, want ErrNotStateDir", err)
				}
				if len(got) != 0 {
					t.Errorf("Run returned actions: %v", sortedActions(got))
				}
				diffSnapshots(t, before, snapshot(t, b.root))
			})
		}
	}

	proceed := []struct {
		name       string
		dir        string
		stateFiles []string
	}{
		{"other name with state.json", "my-state", []string{"state.json"}},
		{"other name with journal.json", "my-state", []string{"journal.json"}},
		{"agent-workflows without state files", "agent-workflows", nil},
	}
	for _, tc := range proceed {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("proceed/%s/dry=%v", tc.name, dry), func(t *testing.T) {
				b := build(t, tc.dir, tc.stateFiles)
				b.o.DryRun = dry
				before := snapshot(t, b.root)
				got, err := v2migrate.Run(b.o)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				assertActions(t, got, b.want)
				if dry {
					diffSnapshots(t, before, snapshot(t, b.root))
					return
				}
				for _, a := range b.want {
					if a.Kind == "strip-block" {
						continue
					}
					if _, err := os.Lstat(a.Path); !errors.Is(err, fs.ErrNotExist) {
						t.Errorf("%s should be gone (lstat err %v)", a.Path, err)
					}
				}
				if _, err := os.Stat(filepath.Join(b.o.StateDir, "workflow-tasks", "t1", "task.json")); err != nil {
					t.Errorf("task store removed: %v", err)
				}
			})
		}
	}

	t.Run("missing agent-workflows", func(t *testing.T) {
		root := newRoot(t)
		o := v2migrate.Options{
			Home:       filepath.Join(root, "home"),
			ClaudeHome: filepath.Join(root, "home", ".claude"),
			CodexHome:  filepath.Join(root, "codex"),
			StateDir:   filepath.Join(root, "state", "agent-workflows"),
		}
		mustWrite(t, filepath.Join(o.ClaudeHome, "CLAUDE.md"), "mine\n\n"+block("v3\n"), 0o644)
		before := snapshot(t, root)
		got, err := v2migrate.Run(o)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Run returned actions: %v", sortedActions(got))
		}
		diffSnapshots(t, before, snapshot(t, root))
	})
}

// hostLinksInto creates host links of every owned kind (MV-1 locations) pointing into dir, and
// well-formed managed blocks in both instruction files.
func hostLinksInto(t *testing.T, o v2migrate.Options, dir string) {
	t.Helper()
	mustSymlink(t, filepath.Join(dir, "src", "skills", "foo"), filepath.Join(o.Home, ".agents", "skills", "foo"))
	mustSymlink(t, filepath.Join(dir, "x"), filepath.Join(o.ClaudeHome, "skills", "bar"))
	mustSymlink(t, filepath.Join(dir, "src", "agents", "cw-r.md"), filepath.Join(o.ClaudeHome, "agents", "cw-r.md"))
	mustSymlink(t, filepath.Join(dir, "src", "bin", "cw"), filepath.Join(o.Home, ".local", "bin", "cw"))
	mustWrite(t, filepath.Join(o.ClaudeHome, "CLAUDE.md"), "mine\n\n"+block("managed\n"), 0o644)
	mustWrite(t, filepath.Join(o.CodexHome, "AGENTS.md"), block("v3 block\n"), 0o644)
}

// MV-14 (b), (c) and missing/empty directories: a StateDir holding none of the v2 entries has
// nothing to migrate, whatever its name; links into it and managed blocks stay.
func TestRunNothingToMigrateWithoutV2Entries(t *testing.T) {
	cases := []struct {
		name  string
		dir   string // base name of StateDir
		setup func(t *testing.T, st string)
	}{
		{"missing agent-workflows", "agent-workflows", func(t *testing.T, st string) {}},
		{"missing custom name", "my-state", func(t *testing.T, st string) {}},
		{"empty dir named state", "state", func(t *testing.T, st string) { mustMkdir(t, st) }},
		{"empty agent-workflows", "agent-workflows", func(t *testing.T, st string) { mustMkdir(t, st) }},
		{"v3 data dir holding only src", "agent-workflows", func(t *testing.T, st string) {
			mustWrite(t, filepath.Join(st, "src", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
			mustWrite(t, filepath.Join(st, "src", "skills-manifest.json"), "{}\n", 0o644)
			mustWrite(t, filepath.Join(st, "src", "agents", "cw-r.md"), "r\n", 0o644)
			mustWrite(t, filepath.Join(st, "src", "bin", "cw"), "#!/bin/sh\n", 0o755)
		}},
		{"dir named state with unrelated entries", "state", func(t *testing.T, st string) {
			mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)
			mustWrite(t, filepath.Join(st, "notes.txt"), "notes\n", 0o644)
			mustWrite(t, filepath.Join(st, "x"), "x\n", 0o644)
		}},
		{"custom name holding only workflow-tasks", "my-state", func(t *testing.T, st string) {
			mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)
		}},
		{"agent-workflows holding src and workflow-tasks", "agent-workflows", func(t *testing.T, st string) {
			mustWrite(t, filepath.Join(st, "src", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
			mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)
		}},
	}
	for _, tc := range cases {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry=%v", tc.name, dry), func(t *testing.T) {
				root := newRoot(t)
				o := v2migrate.Options{
					Home:       filepath.Join(root, "home"),
					ClaudeHome: filepath.Join(root, "home", ".claude"),
					CodexHome:  filepath.Join(root, "codex"),
					StateDir:   filepath.Join(root, "data", tc.dir),
					DryRun:     dry,
				}
				tc.setup(t, o.StateDir)
				hostLinksInto(t, o, o.StateDir)
				before := snapshot(t, root)
				got, err := v2migrate.Run(o)
				if err != nil {
					t.Errorf("Run error = %v, want nil", err)
				}
				if len(got) != 0 {
					t.Errorf("Run returned actions: %v", sortedActions(got))
				}
				diffSnapshots(t, before, snapshot(t, root))
			})
		}
	}
}

// MV-14 (a): rerunning on a custom-named StateDir after its v2 entries were removed.
func TestRunRerunOfCustomStateDir(t *testing.T) {
	root := newRoot(t)
	st := filepath.Join(root, "data", "my-state")
	o := v2migrate.Options{
		Home:       filepath.Join(root, "home"),
		ClaudeHome: filepath.Join(root, "home", ".claude"),
		CodexHome:  filepath.Join(root, "codex"),
		StateDir:   st,
	}
	mustWrite(t, filepath.Join(st, "state.json"), "{}\n", 0o644)
	mustWrite(t, filepath.Join(st, "releases", "r1", "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
	mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{\"id\":\"t1\"}\n", 0o644)
	link := filepath.Join(o.Home, ".agents", "skills", "foo")
	mustSymlink(t, filepath.Join(st, "releases", "r1", "skills", "foo"), link)

	got, err := v2migrate.Run(o)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	assertActions(t, got, []v2migrate.Action{
		{Kind: "unlink", Path: link},
		{Kind: "remove", Path: filepath.Join(st, "releases")},
		{Kind: "remove", Path: filepath.Join(st, "state.json")},
	})

	before := snapshot(t, root)
	for _, step := range []struct {
		name string
		dry  bool
	}{{"second real run", false}, {"dry run after it", true}} {
		o.DryRun = step.dry
		got, err := v2migrate.Run(o)
		if err != nil {
			t.Errorf("%s: error = %v, want nil", step.name, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: returned actions %v", step.name, sortedActions(got))
		}
	}
	diffSnapshots(t, before, snapshot(t, root))
	if got := readFile(t, filepath.Join(st, "workflow-tasks", "t1", "task.json")); got != "{\"id\":\"t1\"}\n" {
		t.Errorf("task store changed: %q", got)
	}
}

// MV-13 (rev 6 amendment): any one of current, releases, bases or runtime, without state.json or
// journal.json and outside a directory named agent-workflows, is refused with ErrNotStateDir.
func TestRunRefusesWithAnySingleV2Entry(t *testing.T) {
	for _, entry := range []string{"current", "releases", "bases", "runtime"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry=%v", entry, dry), func(t *testing.T) {
				root := newRoot(t)
				st := filepath.Join(root, "data", "my-state")
				o := v2migrate.Options{
					Home:       filepath.Join(root, "home"),
					ClaudeHome: filepath.Join(root, "home", ".claude"),
					CodexHome:  filepath.Join(root, "codex"),
					StateDir:   st,
					DryRun:     dry,
				}
				mustWrite(t, filepath.Join(st, entry, "skills", "foo", "SKILL.md"), "# foo\n", 0o644)
				mustWrite(t, filepath.Join(st, "workflow-tasks", "t1", "task.json"), "{}\n", 0o644)
				hostLinksInto(t, o, st)
				mustSymlink(t, filepath.Join(st, entry, "skills", "foo"), filepath.Join(o.ClaudeHome, "skills", "foo"))
				before := snapshot(t, root)
				got, err := v2migrate.Run(o)
				if !errors.Is(err, v2migrate.ErrNotStateDir) {
					t.Errorf("Run error = %v, want ErrNotStateDir", err)
				}
				if len(got) != 0 {
					t.Errorf("Run returned actions: %v", sortedActions(got))
				}
				diffSnapshots(t, before, snapshot(t, root))
			})
		}
	}
}
