package pushgate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// The scenarios in this file come from the second challenge of 14 November
// 2026 (openspec/production-plan.md, Independent challenge).

// noDeclarationSection is the section of the documents that a denial of a
// tree without kitsu.toml names.
const noDeclarationSection = "docs/operations.md#a-tree-without-kitsutoml"

// Requirement "Updates that add nothing", scenario "Notes are not judged".
func TestNotesAreNotJudged(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Git("notes", "add", "-m", "reviewed", "HEAD")
	repo.Write("src/app.txt", "broken\n")
	notes := refOn(repo, "refs/notes/commits")
	if notes == "" {
		t.Fatal("git notes add made no refs/notes/commits")
	}

	push := repo.TryGit("push", "origin", "refs/notes/commits")
	assertSent(t, push, origin, map[string]string{"refs/notes/commits": notes})
}

// Requirement "Updates that add nothing", scenario "A tag below a branch of
// the same push".
func TestATagBelowABranchOfTheSamePush(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	repo.Write("README.md", "# before kitsu\n")
	old := commitAround(repo, "before kitsu")
	repo.Git("tag", "v0")
	repo.Write("kitsu.toml", "version = 1\n\n[[check]]\nid = \"unit\"\nrun = [\"git\", \"--version\"]\n")
	tip := commitAround(repo, "declare unit")
	bare := filepath.Join(filepath.Dir(repo.Dir), "origin.git")
	repo.Git("init", "-q", "--bare", "-b", "main", bare)
	repo.Git("remote", "add", "origin", bare)
	origin := repo.In(bare)
	mustKitsu(t, repo, 0, "init")
	checked(t, repo)

	// Alone, the tag gives the remote a commit whose tree has no kitsu.toml.
	push := repo.TryGit("push", "origin", "v0")
	reason := assertDenied(t, push, origin, map[string]string{"refs/tags/v0": ""})
	if !strings.Contains(reason, "refs/tags/v0") || !strings.Contains(reason, noDeclarationSection) {
		t.Errorf("the reason does not name refs/tags/v0 and %s:\n%s", noDeclarationSection, reason)
	}

	// With the verified branch that contains it, the tag adds nothing.
	push = repo.TryGit("push", "origin", "main", "v0")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": tip, "refs/tags/v0": old})
}

// Requirement "Push rule", scenario "A change outside the checked paths
// needs no other checkout".
func TestAChangeOutsideTheCheckedPathsNeedsNoOtherCheckout(t *testing.T) {
	t.Parallel()
	repo, origin := withOrigin(t)
	// Every check has paths; notes.txt is in none of them.
	declaration := strings.Replace(repo.Read("kitsu.toml"), "run = [\"sh\", \"lint.sh\"]\n", "run = [\"sh\", \"lint.sh\"]\npaths = [\"lint.sh\"]\n", 1)
	if !strings.Contains(declaration, "paths = [\"lint.sh\"]") {
		t.Fatalf("lint did not get paths:\n%s", declaration)
	}
	repo.Write("kitsu.toml", declaration)
	commitAround(repo, "every check has paths")
	repo.Git("push", "-q", "origin", "main")
	mustKitsu(t, repo, 0, "init")
	before := refOn(repo, "HEAD")
	repo.Write("src/app.txt", "ok\n\n")
	tip := commitAround(repo, "change")
	repo.Write("notes.txt", "not part of any commit\n")

	push := repo.TryGit("push", "origin", "main")
	reason := assertDenied(t, push, origin, map[string]string{"refs/heads/main": before})
	if !strings.Contains(reason, "kitsu: to produce the missing evidence, run:\n  kitsu run\n") {
		t.Errorf("the reason does not give the plain instruction to run kitsu run:\n%s", reason)
	}
	for _, word := range []string{"git worktree add", "checkout"} {
		if strings.Contains(reason, word) {
			t.Errorf("the reason holds %q, although the working tree holds the same checked content:\n%s", word, reason)
		}
	}

	checked(t, repo)
	push = repo.TryGit("push", "origin", "main")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": tip})
}

// Requirement "Push rule", scenario "A branch without project code needs a
// declaration".
func TestABranchWithoutProjectCodeNeedsADeclaration(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Git("checkout", "-q", "--orphan", "site")
	repo.Git("rm", "-r", "-q", "-f", ".")
	repo.Write("index.html", "<p>site</p>\n")
	commitAround(repo, "site")

	push := repo.TryGit("push", "origin", "site")
	reason := assertDenied(t, push, origin, map[string]string{"refs/heads/site": ""})
	if !strings.Contains(reason, noDeclarationSection) {
		t.Errorf("the reason does not name %s:\n%s", noDeclarationSection, reason)
	}
	for _, word := range []string{"kitsu init", "kitsu uninstall"} {
		if strings.Contains(reason, word) {
			t.Errorf("the reason names %q:\n%s", word, reason)
		}
	}

	repo.Write("kitsu.toml", "version = 1\n")
	tip := commitAround(repo, "declare no checks")
	push = repo.TryGit("push", "origin", "site")
	assertSent(t, push, origin, map[string]string{"refs/heads/site": tip})
	if runs, err := os.ReadDir(filepath.Join(repo.StoreDir(), "runs")); err == nil && len(runs) > 0 {
		t.Errorf("a run was recorded: %v", runs)
	}
}

// Requirement "Removal", scenario "`kitsu uninstall` says what it removed".
func TestUninstallSaysWhatItRemoved(t *testing.T) {
	t.Parallel()
	repo, _ := gated(t)
	store := repo.StoreDir()

	result := mustKitsu(t, repo, 0, "uninstall")
	want := "removed the hooks kitsu wrote, 3 in all, and the records of kitsu in " + store
	if !strings.Contains(result.Stdout, want) {
		t.Errorf("kitsu uninstall does not say %q:\n%s", want, result.Output())
	}
}

// Requirement "Removal", scenario "A hooks directory of another owner keeps
// what is not exactly `kitsu`'s".
func TestAHooksDirectoryKeepsAFileThatIsNotExactlyCws(t *testing.T) {
	t.Parallel()
	repo, _ := withOrigin(t)
	mustKitsu(t, repo, 0, "init")
	own := hooksDir(t, repo)
	data, err := os.ReadFile(filepath.Join(own, "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	marker := strings.Split(script, "\n")[1]
	if !strings.HasPrefix(marker, "#") || !strings.Contains(marker, "kitsu uninstall") {
		t.Fatalf("the second line of kitsu's hook is not its marker: %q", marker)
	}

	// The team's directory holds kitsu's script as it is, and a hook of the
	// team that holds the marker line and more.
	writeProgram(t, repo.Path(".githooks/pre-push"), script)
	team := "#!/bin/sh\n" + marker + "\nsh team-lint.sh || exit 1\nkitsu hook pre-commit \"$@\"\n"
	writeProgram(t, repo.Path(".githooks/pre-commit"), team)
	repo.Git("config", "core.hooksPath", ".githooks")

	result := repo.Kitsu("uninstall")
	if result.Code != 4 {
		t.Errorf("kitsu uninstall exited %d, want 4\n%s", result.Code, result.Output())
	}
	if got := repo.Read(".githooks/pre-commit"); got != team {
		t.Errorf("kitsu uninstall changed the team's pre-commit:\n%s", got)
	}
	for _, path := range []string{repo.Path(".githooks/pre-push"), filepath.Join(own, "pre-push"), filepath.Join(own, "pre-commit"), filepath.Join(own, "pre-merge-commit")} {
		if exists(path) {
			t.Errorf("%s, a file that is exactly kitsu's script, is still there", path)
		}
	}
	output := result.Output()
	if !strings.Contains(output, ".githooks") || !strings.Contains(output, `kitsu hook pre-commit "$@"`) {
		t.Errorf("kitsu uninstall does not name the team's pre-commit and the line to remove:\n%s", output)
	}
	if strings.Contains(output, "pre-push\n") {
		t.Errorf("kitsu uninstall names pre-push, which it removed:\n%s", output)
	}
}
