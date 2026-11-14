package pushgate_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Requirement "Push rule", scenario "A commit the gate allowed is pushed".
func TestACommitTheGateAllowedIsPushed(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Write("README.md", "# demo\nmore\n")
	checked(t, repo)
	tip := commitAll(t, repo, "docs")

	push := repo.TryGit("push", "origin", "main")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": tip})
}

// Requirement "Push rule", scenario "A commit made around the hook is
// caught".
func TestACommitMadeAroundTheHookIsCaught(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	repo.Write("src/app.txt", "broken\n")
	repo.Git("commit", "-q", "-a", "--no-verify", "-m", "skip")

	push := repo.TryGit("push", "origin", "main")
	reason := assertDenied(t, push, origin, map[string]string{"refs/heads/main": before})
	if !holdsWord(reason, "unit") {
		t.Errorf("the reason does not name unit:\n%s", reason)
	}
}

// Requirement "Push rule", scenario "A cherry-picked commit needs a run".
func TestACherryPickedCommitNeedsARun(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	// Every check has a run on the first commit, so that the picked
	// commit's tree lacks a run of lint only.
	checked(t, repo)
	before := refOn(origin, "refs/heads/main")
	repo.Git("switch", "-q", "-c", "other")
	repo.Write("docs/a.md", "a\n")
	commitAround(repo, "a")
	repo.Write("docs/b.md", "b\n")
	picked := commitAround(repo, "b")
	repo.Git("switch", "-q", "main")
	repo.Git("cherry-pick", picked)
	tip := refOn(repo, "HEAD")

	push := repo.TryGit("push", "origin", "main")
	assertDenied(t, push, origin, map[string]string{"refs/heads/main": before})

	checked(t, repo)
	push = repo.TryGit("push", "origin", "main")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": tip})
}

// Requirement "Push rule", scenario "The branch that is pushed is the one
// judged".
func TestTheBranchThatIsPushedIsTheOneJudged(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Git("switch", "-q", "-c", "feature")
	repo.Write("src/app.txt", "broken\n")
	commitAround(repo, "broken")
	mustCW(t, repo, 5, "run")
	repo.Git("switch", "-q", "main")
	checked(t, repo)

	push := repo.TryGit("push", "origin", "feature")
	reason := assertDenied(t, push, origin, map[string]string{"refs/heads/feature": ""})
	for _, want := range []string{"refs/heads/feature", "cw run", "checkout"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not hold %q:\n%s", want, reason)
		}
	}
}

// Requirement "Push rule", scenario "Only the tip is judged".
func TestOnlyTheTipIsJudged(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Git("switch", "-q", "-c", "feature")
	for _, name := range []string{"one", "two", "three"} {
		repo.Write(filepath.Join("docs", name+".md"), name+"\n")
		commitAround(repo, name)
	}
	repo.Git("switch", "-q", "main")
	repo.Write("README.md", "# demo\nmain\n")
	checked(t, repo)
	commitAll(t, repo, "main")
	repo.Git("switch", "-q", "feature")
	repo.Git("rebase", "-q", "main")
	tip := refOn(repo, "HEAD")
	if count := repo.Git("rev-list", "--count", "main..feature"); count != "3" {
		t.Fatalf("the rebase left %s commits on feature, want 3", count)
	}
	checked(t, repo)

	push := repo.TryGit("push", "origin", "feature")
	assertSent(t, push, origin, map[string]string{"refs/heads/feature": tip})
}

// Requirement "Push rule", scenario "An unchecked branch of the remote does
// not reach `main` unnoticed".
func TestAnUncheckedBranchOfTheRemoteDoesNotReachMainUnnoticed(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	// A second clone without hooks puts a commit on origin's wip.
	other := repo.In(filepath.Join(filepath.Dir(repo.Dir), "other"))
	repo.Git("clone", "-q", origin.Dir, other.Dir)
	other.Write("docs/wip.md", "wip\n")
	other.Git("add", "-A")
	other.Git("commit", "-q", "-m", "wip")
	other.Git("push", "-q", "origin", "HEAD:refs/heads/wip")

	repo.Git("fetch", "-q", "origin")
	repo.Git("merge", "-q", "--ff-only", "origin/wip")
	tip := refOn(repo, "HEAD")

	push := repo.TryGit("push", "origin", "main")
	assertDenied(t, push, origin, map[string]string{"refs/heads/main": before})

	checked(t, repo)
	push = repo.TryGit("push", "origin", "main")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": tip})
}

// Requirement "Push rule", scenario "One bad ref denies the whole push".
func TestOneBadRefDeniesTheWholePush(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	repo.Write("README.md", "# demo\nmore\n")
	checked(t, repo)
	commitAll(t, repo, "docs")
	repo.Git("switch", "-q", "-c", "feature")
	repo.Write("src/app.txt", "broken\n")
	commitAround(repo, "broken")
	repo.Git("switch", "-q", "main")

	push := repo.TryGit("push", "origin", "main", "feature")
	assertDenied(t, push, origin, map[string]string{"refs/heads/main": before, "refs/heads/feature": ""})
}

// Requirement "Push rule", scenario "A dry run is judged like a push".
func TestADryRunIsJudgedLikeAPush(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	repo.Write("README.md", "# demo\nmore\n")
	commitAround(repo, "docs")

	push := repo.TryGit("push", "--dry-run", "origin", "main")
	assertDenied(t, push, origin, map[string]string{"refs/heads/main": before})
}

// Requirement "Push rule", scenario "An annotated tag is judged by the commit
// it names".
func TestAnAnnotatedTagIsJudgedByTheCommitItNames(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	repo.Write("README.md", "# demo\nmore\n")
	commitAround(repo, "docs")
	repo.Git("tag", "-a", "-m", "v2", "v2")

	push := repo.TryGit("push", "origin", "v2")
	reason := assertDenied(t, push, origin, map[string]string{"refs/tags/v2": ""})
	if !strings.Contains(reason, "refs/tags/v2") {
		t.Errorf("the reason does not name refs/tags/v2:\n%s", reason)
	}
}
