package reporting_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Recorded decisions", scenario "A denial is recorded with its
// reasons".
func TestADenialIsRecordedWithItsReasons(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Write("src/app.txt", "broken\n")
	if result := repo.CW("run", "unit"); result.Code == 0 {
		t.Fatalf("cw run unit exited 0 on broken content:\n%s", result.Output())
	}
	before := head(repo)
	if result := repo.TryGit("commit", "-a", "-m", "change"); result.Code == 0 || head(repo) != before {
		t.Fatalf("the commit was not denied (git exit %d):\n%s", result.Code, result.Output())
	}

	got := statsJSON(t, repo)
	denied := got.decisions("pre-commit", "enforce", "denied")
	if len(denied) != 1 || denied[0].Count != 1 {
		t.Fatalf("cw stats --json does not count one denied pre-commit decision: %+v", got.Decisions)
	}
	if n := denied[0].Checks["failed"]["unit"]; n != 1 {
		t.Errorf("unit is counted %d times under failed, want 1: %+v", n, denied[0].Checks)
	}
	if n := denied[0].Checks["not_run"]["lint"]; n != 1 {
		t.Errorf("lint is counted %d times under not_run, want 1: %+v", n, denied[0].Checks)
	}
}

// commitAllowed edits README.md, runs the checks and commits, and fails the
// test when the commit is not recorded.
func commitAllowed(t *testing.T, repo *cwtest.Repo, n int) {
	t.Helper()
	repo.Write("README.md", fmt.Sprintf("# demo %d\n", n))
	checked(t, repo)
	before := head(repo)
	if result := repo.TryGit("commit", "-a", "-q", "-m", fmt.Sprint("allowed ", n)); result.Code != 0 || head(repo) == before {
		t.Fatalf("commit %d was not recorded (git exit %d):\n%s", n, result.Code, result.Output())
	}
}

// commitDenied tries to commit everything and fails the test when the commit
// is recorded.
func commitDenied(t *testing.T, repo *cwtest.Repo, what string) {
	t.Helper()
	before := head(repo)
	if result := repo.TryGit("commit", "-a", "-q", "-m", what); result.Code == 0 || head(repo) != before {
		t.Fatalf("the commit %q was not denied (git exit %d):\n%s", what, result.Code, result.Output())
	}
}

// Requirement "Statistics", scenario "A week of work".
func TestAWeekOfWork(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	// unit fails when UNIT_FAIL is set, which changes nothing it reads: a
	// rerun without it passes on the same checked content. The change is
	// committed before the hooks are installed, so it is no decision.
	repo.Write("unit.sh", "echo unit.sh runs\ntest -z \"$UNIT_FAIL\" || exit 3\ntest \"$(cat src/app.txt)\" = ok || exit 3\n")
	repo.Git("commit", "-q", "-a", "-m", "unit can be made to fail")
	if result := repo.CW("init"); result.Code != 0 {
		t.Fatalf("cw init exited %d:\n%s", result.Code, result.Output())
	}

	// Five allowed commits; before the first, unit fails once and then
	// passes on the same content.
	repo.Write("README.md", "# demo 0\n")
	flaky := repo.Command(cwProgram(t), "run")
	flaky.Env = append(flaky.Env, "UNIT_FAIL=1")
	if output, err := flaky.CombinedOutput(); err == nil {
		t.Fatalf("cw run with UNIT_FAIL=1 exited 0:\n%s", output)
	}
	for n := range 5 {
		commitAllowed(t, repo, n)
	}

	// Two denials because unit failed.
	for _, content := range []string{"broken\n", "broken again\n"} {
		repo.Write("src/app.txt", content)
		if result := repo.CW("run"); result.Code == 0 {
			t.Fatalf("cw run exited 0 with src/app.txt %q:\n%s", content, result.Output())
		}
		commitDenied(t, repo, "unit failed")
	}

	// One denial with nothing run: new content for both checks.
	repo.Write("src/app.txt", "ok\n\n\n")
	commitDenied(t, repo, "nothing run")

	result := repo.CW("stats", "--since", "7d")
	if result.Code != 0 {
		t.Errorf("cw stats --since 7d exited %d, want 0", result.Code)
	}
	// The spec gives no wording for the text: each number must stand on a
	// line with the word it counts.
	for _, want := range []struct {
		words []string
		n     int
	}{
		{[]string{"pre-commit", "allowed"}, 5},
		{[]string{"pre-commit", "denied"}, 3},
		{[]string{"failed"}, 2},
		{[]string{"not", "run"}, 1},
	} {
		if !slices.ContainsFunc(strings.Split(result.Stdout, "\n"), func(line string) bool {
			if _, ok := lineWith(line, want.words...); !ok {
				return false
			}
			return holdsNumber(line, want.n)
		}) {
			t.Errorf("cw stats --since 7d prints no line with %q and %d:\n%s", want.words, want.n, result.Output())
		}
	}

	// The passes after a failure are a paragraph whose heading speaks of
	// passing after a failure.
	after := false
	for _, paragraph := range strings.Split(result.Stdout, "\n\n") {
		heading, body, _ := strings.Cut(paragraph, "\n")
		heading = strings.ToLower(heading)
		if strings.Contains(heading, "pass") && strings.Contains(heading, "after") {
			line, ok := lineWith(body, "unit")
			after = ok && holdsNumber(line, 1)
		}
	}
	if !after {
		t.Errorf("cw stats --since 7d does not report 1 pass after failure for unit:\n%s", result.Output())
	}

	got := statsJSON(t, repo, "--since", "7d")
	if n := got.count("pre-commit", "enforce", "allowed"); n != 5 {
		t.Errorf("%d allowed pre-commit decisions, want 5: %+v", n, got.Decisions)
	}
	if n := got.count("pre-commit", "enforce", "denied"); n != 3 {
		t.Errorf("%d denied pre-commit decisions, want 3: %+v", n, got.Decisions)
	}
	reasons := map[string]int{}
	for _, d := range got.decisions("pre-commit", "enforce", "denied") {
		for reason, n := range d.Reasons {
			reasons[reason] += n
		}
	}
	if reasons["failed"] != 2 || reasons["not_run"] != 1 {
		t.Errorf("the denials have the reasons %v, want failed 2 and not_run 1", reasons)
	}
	if n := got.PassAfterFailure["unit"]; n != 1 {
		t.Errorf("pass_after_failure counts %d for unit, want 1: %v", n, got.PassAfterFailure)
	}
}

// Requirement "Statistics", scenario "Observed denials are counted apart".
func TestObservedDenialsAreCountedApart(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	if result := repo.CW("init", "--observe"); result.Code != 0 {
		t.Fatalf("cw init --observe exited %d:\n%s", result.Code, result.Output())
	}
	repo.Write("src/app.txt", "broken\n")
	if result := repo.CW("run"); result.Code == 0 {
		t.Fatalf("cw run exited 0 on broken content:\n%s", result.Output())
	}
	if result := repo.TryGit("commit", "-a", "-m", "change"); result.Code != 0 {
		t.Fatalf("the observed commit exited %d:\n%s", result.Code, result.Output())
	}

	if result := repo.CW("stats"); result.Code != 0 || !strings.Contains(result.Stdout, "observe") {
		t.Errorf("cw stats (exit %d) does not show the observe mode:\n%s", result.Code, result.Output())
	}
	got := statsJSON(t, repo)
	if n := got.count("pre-commit", "observe", "denied"); n != 1 {
		t.Errorf("%d observed denials, want 1: %+v", n, got.Decisions)
	}
	if n := got.count("pre-commit", "enforce", "denied"); n != 0 {
		t.Errorf("%d enforced denials, want 0: %+v", n, got.Decisions)
	}
}

// Requirement "Exit statuses", scenario "The statuses in one session".
func TestTheStatusesInOneSession(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	statuses := hookStatuses(t, repo)
	checked(t, repo)

	var got []int
	got = append(got, repo.CW("status").Code)
	got = append(got, repo.CW("run", "--nosuchflag").Code)
	repo.Write("src/app.txt", "edited\n")
	got = append(got, repo.CW("status").Code)
	if want := []int{0, 2, 5}; !slices.Equal(got, want) {
		t.Errorf("the exit statuses are %v, want %v", got, want)
	}

	before := head(repo)
	result := repo.TryGit("commit", "-a", "-m", "change")
	if result.Code == 0 || head(repo) != before {
		t.Errorf("git commit -a was not denied (exit %d):\n%s", result.Code, result.Output())
	}
	if hooks := statuses(); !slices.Equal(hooks, []string{"pre-commit 3"}) {
		t.Errorf("cw hook ran with the exit statuses %q, want [pre-commit 3]:\n%s", hooks, result.Output())
	}
}
