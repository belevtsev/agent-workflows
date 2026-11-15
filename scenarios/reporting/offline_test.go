package reporting_test

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// isolation is the command line that runs a program in a network namespace
// of its own, which has only a loopback interface. It skips the test when
// the system does not allow one.
func isolation(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not installed, so no network namespace can be made")
	}
	var refusals []string
	for _, prefix := range [][]string{{"unshare", "-n"}, {"unshare", "-r", "-n"}} {
		output, err := exec.Command(prefix[0], append(prefix[1:], "true")...).CombinedOutput()
		if err == nil {
			return prefix
		}
		refusals = append(refusals, strings.Join(prefix, " ")+": "+strings.TrimSpace(string(output)))
	}
	t.Skipf("this system does not allow a network namespace:\n%s", strings.Join(refusals, "\n"))
	return nil
}

// step is one command of the scenario and how it ended.
type step struct {
	Command string
	Code    int
}

// session runs the commands of the scenario in a new demo, each behind
// prefix (none for the run with networking), and returns how each ended,
// the decisions and runs kitsu stats --json counts, and whether the commit was
// recorded.
func session(t *testing.T, prefix []string) ([]step, stats, bool) {
	t.Helper()
	repo := kitsutest.Demo(t)
	run := func(name string, args ...string) kitsutest.Result {
		t.Helper()
		if len(prefix) == 0 {
			return repo.Run("", name, args...)
		}
		return repo.Run("", prefix[0], append(append(append([]string(nil), prefix[1:]...), name), args...)...)
	}
	kitsu := kitsuProgram(t)
	var steps []step
	record := func(label string, result kitsutest.Result) {
		t.Helper()
		t.Logf("%s (exit %d):\n%s", label, result.Code, result.Output())
		steps = append(steps, step{label, result.Code})
	}

	if len(prefix) > 0 {
		// Only the loopback interface is in the namespace.
		result := run("cat", "/proc/self/net/dev")
		var interfaces []string
		for _, line := range strings.Split(result.Stdout, "\n") {
			if name, _, ok := strings.Cut(line, ":"); ok {
				interfaces = append(interfaces, strings.TrimSpace(name))
			}
		}
		if !reflect.DeepEqual(interfaces, []string{"lo"}) {
			t.Fatalf("the namespace has the interfaces %q, want only lo", interfaces)
		}
	}

	record("kitsu init", run(kitsu, "init"))
	record("kitsu add", run(kitsu, "add", "extra", "--", "sh", "-c", "exit 0"))
	record("kitsu run", run(kitsu, "run"))
	record("kitsu status", run(kitsu, "status"))
	repo.Write("README.md", "# demo, edited\n")
	record("kitsu run after an edit", run(kitsu, "run"))
	before := head(repo)
	record("git commit", run("git", "commit", "-a", "-q", "-m", "change"))
	committed := head(repo) != before
	record("kitsu stats", run(kitsu, "stats"))

	result := run(kitsu, "stats", "--json")
	record("kitsu stats --json", result)
	got := statsJSON(t, repo) // read outside the namespace, to compare
	return steps, got, committed
}

// Requirement "No network" (check-evidence), scenario "Everything works
// offline".
func TestEverythingWorksOffline(t *testing.T) {
	t.Parallel()
	prefix := isolation(t)

	wantSteps, wantStats, wantCommit := session(t, nil)
	gotSteps, gotStats, gotCommit := session(t, prefix)

	if !reflect.DeepEqual(gotSteps, wantSteps) {
		t.Errorf("offline the commands ended %v, with networking %v", gotSteps, wantSteps)
	}
	if gotCommit != wantCommit || !gotCommit {
		t.Errorf("the gated commit was recorded offline: %v, with networking: %v", gotCommit, wantCommit)
	}
	if !reflect.DeepEqual(gotStats.Runs, wantStats.Runs) || !reflect.DeepEqual(gotStats.Decisions, wantStats.Decisions) {
		t.Errorf("kitsu stats counts offline %+v %+v, with networking %+v %+v", gotStats.Runs, gotStats.Decisions, wantStats.Runs, wantStats.Decisions)
	}
}
