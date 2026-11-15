// Package commitgate_test holds the commit-side scenarios of the
// specification publish-gate
// (openspec/changes/reduce-cw-to-evidence-gate/specs/publish-gate/spec.md):
// hook installation, the commit and merge rules, fail-closed hooks, the
// denial message and the declaration change notice.
package commitgate_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// hookNames are the hooks that gatecub init installs.
var hookNames = []string{"pre-commit", "pre-merge-commit", "pre-push"}

// gate is the shared fixture of publish-gate: the repository demo with the
// hooks installed and a remote origin whose main is at the first commit.
// The gatecub that the hooks find on PATH is a wrapper that runs the gatecub under
// test unchanged and notes the exit status of each `gatecub hook` call, so that
// a test can see the hook's own status behind git's.
type gate struct {
	*gatecubtest.Repo
	t      *testing.T
	remote string
	exits  string // the file the wrapper appends "<hook name> <status>" to
}

// newGate builds the shared fixture.
func newGate(t *testing.T) *gate {
	t.Helper()
	repo := gatecubtest.Demo(t)
	g := &gate{Repo: repo, t: t, remote: filepath.Join(t.TempDir(), "origin.git")}
	repo.Git("init", "-q", "--bare", "-b", "main", g.remote)
	repo.Git("remote", "add", "origin", g.remote)
	repo.Git("push", "-q", "origin", "main")
	g.wrapGatecub()
	if result := repo.Gatecub("init"); result.Code != 0 {
		t.Fatalf("gatecub init exited %d:\n%s", result.Code, result.Output())
	}
	return g
}

// wrapGatecub puts the noting wrapper first on PATH.
func (g *gate) wrapGatecub() {
	g.t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	dir := g.t.TempDir()
	g.exits = filepath.Join(dir, "exits.log")
	real := filepath.Join(gatecubtest.Binary(g.t), "gatecub")
	script := "#!/bin/sh\n" +
		"'" + real + "' \"$@\"\n" +
		"status=$?\n" +
		"if [ \"$1\" = hook ]; then printf '%s %s\\n' \"$2\" \"$status\" >> '" + g.exits + "'; fi\n" +
		"exit $status\n"
	writeProgram(g.t, filepath.Join(dir, "gatecub"), script)
	g.Setenv("PATH", dir+string(os.PathListSeparator)+envValue(g.Env(), "PATH"))
}

// envValue is the last value of a variable in an environment list.
func envValue(env []string, name string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value = v
		}
	}
	return value
}

// resetExits forgets the hook statuses noted so far.
func (g *gate) resetExits() {
	g.t.Helper()
	if g.exits == "" {
		return
	}
	if err := os.WriteFile(g.exits, nil, 0o644); err != nil {
		g.t.Fatal(err)
	}
}

// hookExits are the exit statuses of `gatecub hook <name>` since the last reset.
func (g *gate) hookExits(name string) []int {
	g.t.Helper()
	data, err := os.ReadFile(g.exits)
	if err != nil && !os.IsNotExist(err) {
		g.t.Fatal(err)
	}
	var out []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		hook, status, ok := strings.Cut(line, " ")
		if !ok || hook != name {
			continue
		}
		n, err := strconv.Atoi(status)
		if err != nil {
			g.t.Fatalf("bad line %q in %s", line, g.exits)
		}
		out = append(out, n)
	}
	return out
}

// assertHookExit checks that `gatecub hook <name>` ran and its last call exited
// with the given status. It checks nothing where the wrapper cannot be used.
func (g *gate) assertHookExit(name string, want int) {
	g.t.Helper()
	if g.exits == "" {
		return
	}
	exits := g.hookExits(name)
	if len(exits) == 0 {
		g.t.Errorf("the %s hook did not call gatecub hook %s", name, name)
		return
	}
	if got := exits[len(exits)-1]; got != want {
		g.t.Errorf("gatecub hook %s exited %d, want %d", name, got, want)
	}
}

// head is the commit HEAD names.
func (g *gate) head() string {
	g.t.Helper()
	return g.Git("rev-parse", "HEAD")
}

// mustRun runs gatecub run and requires the given exit status.
func (g *gate) mustRun(want int, args ...string) gatecubtest.Result {
	g.t.Helper()
	result := g.Gatecub(append([]string{"run"}, args...)...)
	if result.Code != want {
		g.t.Fatalf("gatecub run %s exited %d, want %d:\n%s", strings.Join(args, " "), result.Code, want, result.Output())
	}
	return result
}

// checked brings the repository to the checked state.
func (g *gate) checked() {
	g.t.Helper()
	g.mustRun(0)
}

// commitDenied runs a git command that would record a commit and requires
// a denial by the named hook: git fails, HEAD is unchanged, the hook exited
// 3 and a reason is printed on standard error. It returns how git ended.
func (g *gate) commitDenied(hook string, args ...string) gatecubtest.Result {
	g.t.Helper()
	before := g.head()
	g.resetExits()
	result := g.TryGit(args...)
	if result.Code == 0 {
		g.t.Errorf("git %s exited 0, want a denial:\n%s", strings.Join(args, " "), result.Output())
	}
	if after := g.head(); after != before {
		g.t.Errorf("git %s moved HEAD from %s to %s", strings.Join(args, " "), before, after)
	}
	g.assertHookExit(hook, 3)
	if strings.TrimSpace(result.Stderr) == "" {
		g.t.Errorf("git %s printed no reason on standard error", strings.Join(args, " "))
	}
	return result
}

// commitRecorded runs a git command that records a commit and requires that
// it did.
func (g *gate) commitRecorded(args ...string) gatecubtest.Result {
	g.t.Helper()
	before := g.head()
	result := g.TryGit(args...)
	if result.Code != 0 {
		g.t.Errorf("git %s exited %d, want the commit recorded:\n%s", strings.Join(args, " "), result.Code, result.Output())
	}
	if after := g.head(); after == before {
		g.t.Errorf("git %s left HEAD at %s", strings.Join(args, " "), before)
	}
	return result
}

// commitNoVerify records everything as a commit without the hooks, to build
// a history for a scenario.
func (g *gate) commitNoVerify(message string) {
	g.t.Helper()
	g.Git("add", "-A")
	g.Git("commit", "-q", "--no-verify", "-m", message)
}

// lineWith says whether one line of text holds every one of the words.
func lineWith(text string, words ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// assertLineWith fails the test unless one line of text holds every word.
func assertLineWith(t *testing.T, text string, words ...string) {
	t.Helper()
	if !lineWith(text, words...) {
		t.Errorf("no line holds all of %q:\n%s", words, text)
	}
}

// namesWord says whether text names a word on its own, such as a check id.
func namesWord(text, word string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_.-])`).MatchString(text)
}

// notRun is the state the denial message requirement names for a check
// without a run on the content.
const notRun = "not run on this content"
