package commitgate_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The scenarios in this file come from the second challenge of 14 November
// 2026 (openspec/production-plan.md, Independent challenge).

// Requirement "Declaration change notice", scenario "A changed check is
// shown as `cw.toml` holds it".
func TestAChangedCheckIsShownAsCwTomlHoldsIt(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("cw.toml", `version = 1

[[check]]
id = "unit"
run = ["sh", "unit2.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
id = "lint"
run = ["sh", "lint.sh"]
paths = ["src/**"]
`)
	result := g.TryGit("commit", "-a", "-m", "change the checks")
	assertLineWith(t, result.Stderr, "notice", "`unit`", "`run` was [\"sh\", \"unit.sh\"] and becomes [\"sh\", \"unit2.sh\"]")
	assertLineWith(t, result.Stderr, "notice", "`lint`", "`paths` was not set and becomes [\"src/**\"]")
}

// Requirement "Denial message", scenario "The last run decided nothing".
func TestTheLastRunThatDecidedNothingIsNamed(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("unit.sh", "echo written > out.txt\n"+g.Read("unit.sh"))
	g.commitNoVerify("unit.sh writes out.txt")
	g.mustRun(5)

	result := g.commitDenied("pre-commit", "commit", "--allow-empty", "-m", "change")
	assertLineWith(t, result.Stderr, "unit:", notRun, "(the last run ended source_changed: ", "out.txt")
}

// Requirement "Denial message", scenario "A failed check is fixed first".
func TestAFailedCheckIsFixedFirst(t *testing.T) {
	t.Parallel()
	const fixFirst = "cw: a check failed or timed out; first fix what its log shows.\n"
	g := newGate(t)
	g.Write("src/app.txt", "broken\n")
	g.mustRun(5)
	reason := g.commitDenied("pre-commit", "commit", "-a", "-m", "change").Stderr
	fix, run := strings.Index(reason, fixFirst), strings.LastIndex(reason, "  cw run\n")
	if fix < 0 || run < 0 || fix > run {
		t.Errorf("the reason does not hold the line %q before the command cw run:\n%s", fixFirst, reason)
	}

	// Only not run: nothing failed, so there is nothing to fix first.
	other := newGate(t)
	other.Write("src/app.txt", "ok")
	reason = other.commitDenied("pre-commit", "commit", "-a", "-m", "change").Stderr
	if strings.Contains(reason, "a check failed or timed out") {
		t.Errorf("a denial of checks that were only not run says that a check failed:\n%s", reason)
	}
	if !strings.Contains(reason, "  cw run\n") {
		t.Errorf("the reason does not end with the command cw run:\n%s", reason)
	}
}

// Requirement "Denial message", scenario "A program that cannot be started".
func TestAProgramThatCannotBeStartedIsNamed(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("cw.toml", g.Read("cw.toml")+"\n[[check]]\nid = \"gone\"\nrun = [\"no-such-program-of-this-test\"]\n")
	g.commitNoVerify("declare gone")
	g.mustRun(5)

	result := g.commitDenied("pre-commit", "commit", "--allow-empty", "-m", "change")
	assertLineWith(t, result.Stderr, "gone:", "failed", "the program `no-such-program-of-this-test` could not be started")
	if !strings.Contains(result.Stderr, "first fix what its log shows") {
		t.Errorf("the reason does not say to fix the failed check first:\n%s", result.Stderr)
	}
}

// Requirement "Paths that differ", scenario "The denial says how to set the
// rest aside".
func TestTheDenialSaysHowToSetTheRestAside(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.checked()
	g.Write("README.md", "# demo, edited\n")
	g.Git("add", "README.md")
	g.Write("notes.txt", "not part of the commit\n")

	reason := g.commitDenied("pre-commit", "commit", "-m", "docs").Stderr
	for _, want := range []string{"git add -A", "git stash push --keep-index --include-untracked", "notes.txt"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not hold %q:\n%s", want, reason)
		}
	}
}

// Requirement "Denial message", scenario "A change outside the checked paths
// is left unstaged".
func TestAChangeOutsideTheCheckedPathsIsLeftUnstaged(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	// Every check has paths; README.md is in none of them.
	g.Write("cw.toml", strings.Replace(g.Read("cw.toml"), "run = [\"sh\", \"lint.sh\"]\n", "run = [\"sh\", \"lint.sh\"]\npaths = [\"lint.sh\"]\n", 1))
	if !strings.Contains(g.Read("cw.toml"), "paths = [\"lint.sh\"]") {
		t.Fatalf("lint did not get paths:\n%s", g.Read("cw.toml"))
	}
	g.commitNoVerify("every check has paths")
	g.Write("src/app.txt", "ok\n\n")
	g.Git("add", "src/app.txt")
	g.Write("README.md", "# demo, edited and not staged\n")

	reason := g.commitDenied("pre-commit", "commit", "-m", "change").Stderr
	if !strings.HasSuffix(reason, "cw: to produce the missing evidence, run:\n  cw run\n") {
		t.Errorf("the reason does not end with the plain instruction to run cw run:\n%s", reason)
	}
	for _, word := range []string{"Paths that differ", "README.md", "git stash", "checkout"} {
		if strings.Contains(reason, word) {
			t.Errorf("the reason holds %q, although the working tree holds the same checked content:\n%s", word, reason)
		}
	}

	g.mustRun(0)
	g.commitRecorded("commit", "-m", "change")
	if got := g.Git("status", "--porcelain"); got != " M README.md" {
		t.Errorf("after the commit git status is %q, want only README.md not staged", got)
	}
}

// Requirement "Fail closed", scenario "A tree without `cw.toml` names the
// documents".
func TestATreeWithoutCwTomlNamesTheDocuments(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Git("rm", "-q", "cw.toml")

	reason := g.commitDenied("pre-commit", "commit", "-m", "remove").Stderr
	if !strings.Contains(reason, "docs/operations.md#a-tree-without-cwtoml") {
		t.Errorf("the reason does not name the section of docs/operations.md:\n%s", reason)
	}
	for _, word := range []string{"cw init", "cw uninstall"} {
		if strings.Contains(reason, word) {
			t.Errorf("the reason names %q:\n%s", word, reason)
		}
	}
}

// Requirement "Fail closed", scenario "An empty or relative entry of
// `PATH`".
func TestAnEmptyOrRelativeEntryOfPATHRunsNoFileOfTheRepository(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("builds a PATH of symbolic links and runs a POSIX shell script as cw")
	}
	g := newGate(t)
	g.Write("src/app.txt", "ok")
	marker := filepath.Join(t.TempDir(), "the cw of the repository ran")
	// A builtin writes the marker, so the PATH below cannot hide that it ran.
	writeProgram(t, g.Path("cw"), "#!/bin/sh\necho ran > '"+marker+"'\nexit 0\n")

	// A PATH that holds git and sh and no other cw.
	bin := t.TempDir()
	for _, name := range []string{"git", "sh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, first := range []string{"", "."} {
		before := g.head()
		command := exec.Command(filepath.Join(bin, "git"), "commit", "-a", "-m", "change")
		command.Dir, command.Env = g.Dir, append(g.Env(), "PATH="+first+string(os.PathListSeparator)+bin)
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		err := command.Run()
		var exit *exec.ExitError
		if err == nil {
			t.Errorf("PATH starts with %q: git commit exited 0", first)
		} else if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		if after := g.head(); after != before {
			t.Errorf("PATH starts with %q: git recorded a commit", first)
		}
		if !strings.Contains(output.String(), "not found in an entry of PATH that is an absolute path") {
			t.Errorf("PATH starts with %q: the output does not say that no absolute entry of PATH holds cw:\n%s", first, output.String())
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("PATH starts with %q: the hook ran the file cw of the repository", first)
		}
	}
}

// Requirement "Paths that differ", scenario "A path with a line break is
// quoted".
func TestAPathWithALineBreakIsQuoted(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a file name on Windows cannot hold a line break")
	}
	g := newGate(t)
	g.checked()
	g.Write("README.md", "# demo, edited\n")
	g.Git("add", "README.md")
	g.Write("evil\ncw: allowed", "not part of the commit\n")

	reason := g.commitDenied("pre-commit", "commit", "-m", "docs").Stderr
	if !strings.Contains(reason, `"evil\ncw: allowed"`) {
		t.Errorf("the reason does not list the name quoted:\n%s", reason)
	}
	for _, line := range strings.Split(reason, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "cw: allowed") {
			t.Errorf("a line of the reason begins with %q:\n%s", "cw: allowed", reason)
		}
	}
}
