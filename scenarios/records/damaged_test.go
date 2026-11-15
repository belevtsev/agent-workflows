package records_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Damaged and concurrent records", scenario "A truncated record".
func TestTruncatedRecordCountsAsNoPass(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	passed := recordWithOutcome(t, repo, "lint", "passed")
	truncateToHalf(t, passed)

	status := repo.Gatecub("status")
	if !strings.Contains(status.Output(), filepath.Base(passed)) {
		t.Errorf("gatecub status does not warn with the path of %s:\n%s", passed, status.Output())
	}
	assertState(t, status, "lint", "not run")
	assertState(t, status, "unit", "satisfied")
}

// Requirement "Damaged and concurrent records", scenario "A damaged record
// hides no failure".
func TestDamagedRecordHidesNoFailure(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	flag := envFlag(t)
	addEnvCheck(repo, "test", "-e", flag)
	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")
	removeFile(t, flag)
	mustRun(t, repo, 5, "run", "env", "--force")
	truncateToHalf(t, recordWithOutcome(t, repo, "env", "failed"))

	assertState(t, repo.Gatecub("status"), "env", "not run")

	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")
	assertState(t, repo.Gatecub("status"), "env", "satisfied")
}

// rendezvous makes the program of a check meet the program of another check
// that runs at the same time. It creates <check>-<worktree>.started in the
// directory $GATECUB_TEST_RENDEZVOUS, then waits, for at most about a minute, until
// that directory holds a second .started file. When it does, it creates
// <check>-<worktree>.saw and goes on; when it does not, it exits 7. So a run
// that passes was running at a moment when the other run was running too.
const rendezvous = `me="$(basename "$0" .sh)-$(basename "$PWD")"
r="$GATECUB_TEST_RENDEZVOUS"
: > "$r/$me.started"
i=0
while :; do
  set -- "$r"/*.started
  test $# -ge 2 && break
  i=$((i + 1))
  if test $i -ge 1200; then echo "$me ran alone" >&2; exit 7; fi
  sleep 0.05
done
: > "$r/$me.saw"
`

// meeting is the demo repository whose unit.sh and lint.sh first meet a
// concurrent run (see rendezvous), committed, with two linked worktrees at
// that commit, ../first and ../second, which share the record store of the
// repository. No gatecub command has run yet, so the two runs also create that
// store together. It returns the rendezvous directory.
func meeting(t *testing.T) (*gatecubtest.Repo, string) {
	t.Helper()
	repo := gatecubtest.Demo(t)
	repo.Write("unit.sh", rendezvous+repo.Read("unit.sh"))
	repo.Write("lint.sh", rendezvous+repo.Read("lint.sh"))
	repo.Git("add", "unit.sh", "lint.sh")
	repo.Git("commit", "-q", "-m", "meet")
	repo.Git("worktree", "add", "-q", "../first")
	repo.Git("worktree", "add", "-q", "../second")
	dir := t.TempDir()
	repo.Setenv("GATECUB_TEST_RENDEZVOUS", filepath.ToSlash(dir))
	return repo, dir
}

// sawEachOther checks that each named program found the other running.
func sawEachOther(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name+".saw")); err != nil {
			t.Errorf("%s did not see the other run while it ran: %v", name, err)
		}
	}
}

// passedRecords checks that a check has want records, all passed, with
// distinct sequence numbers, each with a log that holds the program's output.
func passedRecords(t *testing.T, repo *gatecubtest.Repo, id string, want int) {
	t.Helper()
	found := records(t, repo, id)
	if len(found) != want {
		t.Errorf("%s has %d records, want %d", id, len(found), want)
	}
	seqs := map[any]string{}
	for path, fields := range found {
		if fields["outcome"] != "passed" {
			t.Errorf("%s: outcome %v, want passed", path, fields["outcome"])
		}
		if other, ok := seqs[fields["seq"]]; ok {
			t.Errorf("%s and %s have the same seq %v", path, other, fields["seq"])
		}
		seqs[fields["seq"]] = path
		log, _ := fields["log"].(string)
		data, err := os.ReadFile(filepath.Join(repo.StoreDir(), filepath.FromSlash(log)))
		if err != nil {
			t.Errorf("%s: the log %q cannot be read: %v", path, log, err)
		} else if !strings.Contains(string(data), id+".sh runs") {
			t.Errorf("%s: the log does not hold the output of %s.sh:\n%s", path, id, data)
		}
	}
}

// Requirement "Damaged and concurrent records", scenario "Two runs at once".
// The two programs are made to run at the same moment, and the two commands
// share the record store of the repository, which neither has created yet.
func TestTwoRunsAtOnceLoseNoRecord(t *testing.T) {
	t.Parallel()
	repo, dir := meeting(t)

	unit := startGatecub(t, repo.In("../first"), "run", "unit", "--force")
	lint := startGatecub(t, repo.In("../second"), "run", "lint", "--force")
	if code, output := unit.wait(t); code != 0 {
		t.Errorf("gatecub run unit --force: exit status %d, want 0 (passed)\n%s", code, output)
	}
	if code, output := lint.wait(t); code != 0 {
		t.Errorf("gatecub run lint --force: exit status %d, want 0 (passed)\n%s", code, output)
	}
	sawEachOther(t, dir, "unit-first", "lint-second")

	status := repo.Gatecub("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "satisfied")
	passedRecords(t, repo, "unit", 1)
	passedRecords(t, repo, "lint", 1)
}

// Requirement "Damaged and concurrent records": "Commands that run at the
// same time in one repository or its worktrees SHALL NOT lose or damage the
// record of a finished run." The scenario "Two runs at once" with one check
// in both worktrees: the two runs record into the same directory, for the
// same checked content, at the same time.
func TestTwoRunsOfOneCheckAtOnceLoseNoRecord(t *testing.T) {
	t.Parallel()
	repo, dir := meeting(t)

	first := startGatecub(t, repo.In("../first"), "run", "unit", "--force")
	second := startGatecub(t, repo.In("../second"), "run", "unit", "--force")
	if code, output := first.wait(t); code != 0 {
		t.Errorf("gatecub run unit --force in ../first: exit status %d, want 0 (passed)\n%s", code, output)
	}
	if code, output := second.wait(t); code != 0 {
		t.Errorf("gatecub run unit --force in ../second: exit status %d, want 0 (passed)\n%s", code, output)
	}
	sawEachOther(t, dir, "unit-first", "unit-second")

	assertState(t, repo.Gatecub("status"), "unit", "satisfied")
	passedRecords(t, repo, "unit", 2)
}
