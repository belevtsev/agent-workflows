package gate

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/gatecub/v3/internal/decl"
	"github.com/belevtsev/gatecub/v3/internal/evidence"
	"github.com/belevtsev/gatecub/v3/internal/store"
)

const zero = "0000000000000000000000000000000000000000"

// banned are the words a denial must never hold.
var banned = []string{"--no-verify", "observe", "uninstall"}

func checkDenial(t *testing.T, r Ruling, st *store.Store, words ...string) string {
	t.Helper()
	text := r.Denial(st)
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Errorf("the denial lacks %q:\n%s", w, text)
		}
	}
	for _, w := range banned {
		if strings.Contains(text, w) {
			t.Errorf("the denial holds %q:\n%s", w, text)
		}
	}
	return text
}

func checkEndsWithRun(t *testing.T, text string) {
	t.Helper()
	if !strings.HasSuffix(text, "  gatecub run\n") {
		t.Errorf("the denial does not end with the command gatecub run:\n%s", text)
	}
}

func checkState(t *testing.T, r Ruling, id, want string) {
	t.Helper()
	if r.Verdict == nil {
		t.Fatalf("ruling without a verdict: %+v", r)
	}
	for _, c := range r.Verdict.Checks {
		if c.ID == id {
			if c.State != want {
				t.Errorf("%s is %q, want %q", id, c.State, want)
			}
			return
		}
	}
	t.Errorf("the verdict has no check %s", id)
}

func TestCommitAllowedWhenVerified(t *testing.T) {
	e := demo(t)
	e.passAll(e.treeOf("HEAD"))
	r := e.gate.Commit(t.Context(), "pre-commit")
	if !r.Allowed || r.Cause != "" || r.Verdict == nil || !r.Verdict.Verified {
		t.Errorf("ruling = %+v", r)
	}
	if r.Tree != e.treeOf("HEAD") {
		t.Errorf("Tree = %s, want the tree of the index", r.Tree)
	}
	if len(r.Notes) != 0 {
		t.Errorf("notes = %q", r.Notes)
	}
	if d := r.Decision(store.Enforce, now); d.Outcome != "allowed" || d.Checks != nil || d.Cause != "" {
		t.Errorf("decision = %+v", d)
	}
	if exists(filepath.Join(e.repo.CommonDir, "gatecub", "tmp")) {
		t.Error("the index copy directory is left")
	}
}

func TestCommitDeniedWhenNotRun(t *testing.T) {
	e := demo(t)
	e.passAll(e.treeOf("HEAD"))
	e.write("src/app.txt", "ok, edited\n")
	e.git("add", "-A")
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed || r.Cause != "" {
		t.Fatalf("ruling = %+v", r)
	}
	checkState(t, r, "unit", evidence.NotRun)
	checkState(t, r, "lint", evidence.NotRun)
	if r.Elsewhere || r.Differs != "" {
		t.Errorf("the working tree holds the commit, yet Elsewhere=%v Differs=%q", r.Elsewhere, r.Differs)
	}
	text := checkDenial(t, r, e.gate.Store, "this commit", "unit: not run on this content", "lint: not run on this content")
	checkEndsWithRun(t, text)
	d := r.Decision(store.Enforce, now)
	if want := map[string]string{"unit": "not_run", "lint": "not_run"}; d.Outcome != "denied" || !reflect.DeepEqual(d.Checks, want) {
		t.Errorf("decision = %+v", d)
	}
}

func TestCommitDeniedWhenFailed(t *testing.T) {
	e := demo(t)
	e.write("src/app.txt", "broken\n")
	e.git("add", "-A")
	tree := e.git("write-tree")
	failed := e.ran(tree, "unit", store.Failed)
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed {
		t.Fatalf("ruling = %+v", r)
	}
	checkState(t, r, "unit", evidence.Failed)
	text := checkDenial(t, r, e.gate.Store, "unit: failed with exit status 3", e.gate.Store.Abs(failed.Log), "lint: not run")
	checkEndsWithRun(t, text)
	if d := r.Decision(store.Enforce, now); d.Checks["unit"] != "failed" || d.Checks["lint"] != "not_run" {
		t.Errorf("decision checks = %v", d.Checks)
	}
}

func TestCommitJudgesIndexNotWorktree(t *testing.T) {
	e := demo(t)
	e.write("src/app.txt", "ok, edited\n")
	e.git("add", "src/app.txt")
	e.write("src/new.txt", "left out\n")
	snapshot, err := e.repo.Snapshot(t.Context(), e.gate.Store.Tmp())
	if err != nil {
		t.Fatal(err)
	}
	e.passAll(snapshot)
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed {
		t.Fatalf("a commit that leaves out a checked file was allowed: %+v", r)
	}
	if !r.Elsewhere || !strings.Contains(r.Differs, "src/new.txt") || !strings.Contains(r.Differs, "as a whole is verified") {
		t.Errorf("Elsewhere = %v, Differs = %q", r.Elsewhere, r.Differs)
	}
	text := checkDenial(t, r, e.gate.Store, "src/new.txt")
	checkEndsWithRun(t, text)
}

func TestCommitOfOnePathNamesOnlyItsChecks(t *testing.T) {
	e := demo(t)
	e.passAll(e.treeOf("HEAD"))
	e.write("README.md", "# docs\n")
	e.git("add", "README.md")
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed {
		t.Fatal("allowed")
	}
	checkState(t, r, "unit", evidence.Satisfied)
	checkState(t, r, "lint", evidence.NotRun)
	text := r.Denial(e.gate.Store)
	if strings.Contains(text, "unit") {
		t.Errorf("the denial names the satisfied unit:\n%s", text)
	}
}

func TestCommitWithoutDeclaration(t *testing.T) {
	e := demo(t)
	e.git("rm", "-q", "gatecub.toml")
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed || r.Cause != store.CauseNoDeclaration || r.Verdict != nil {
		t.Fatalf("ruling = %+v", r)
	}
	checkDenial(t, r, e.gate.Store, "no declaration")
	d := r.Decision(store.Enforce, now)
	if d.Outcome != "denied" || d.Cause != store.CauseNoDeclaration || d.Checks != nil {
		t.Errorf("decision = %+v", d)
	}
	// The tree of HEAD declares both checks; the notice lists them.
	joined := strings.Join(r.Notes, "\n")
	for _, id := range []string{"`unit` is no longer declared", "`lint` is no longer declared"} {
		if !strings.Contains(joined, id) {
			t.Errorf("notes %q lack %q", r.Notes, id)
		}
	}
}

func TestCommitWithInvalidDeclaration(t *testing.T) {
	e := demo(t)
	lines := strings.Split(declaration, "\n")
	lines = append(lines[:8], append([]string{`tiemout = "5m"`}, lines[8:]...)...)
	e.write("gatecub.toml", strings.Join(lines, "\n"))
	e.git("add", "gatecub.toml")
	r := e.gate.Commit(t.Context(), "pre-commit")
	if r.Allowed || r.Cause != store.CauseInvalidDeclaration {
		t.Fatalf("ruling = %+v", r)
	}
	checkDenial(t, r, e.gate.Store, decl.FileName+":9", "tiemout")
	if r.Because() != "the declaration is invalid" {
		t.Errorf("Because = %q", r.Because())
	}
}

func TestCommitNoticeOfChangedDeclaration(t *testing.T) {
	e := demo(t)
	e.write("gatecub.toml", `version = 1

[[check]]
id = "lint"
run = ["sh", "lint.sh", "-v"]
timeout = "5m"

[[check]]
id = "build"
run = ["make"]
`)
	e.git("add", "gatecub.toml")
	tree := e.git("write-tree")
	e.ran(tree, "lint", store.Passed)
	e.ran(tree, "build", store.Passed)
	r := e.gate.Commit(t.Context(), "pre-commit")
	if !r.Allowed {
		t.Fatalf("ruling = %+v", r)
	}
	joined := strings.Join(r.Notes, "\n")
	for _, want := range []string{"`unit` is no longer declared", "`lint`", "`run`", "`timeout`"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "build") {
		t.Errorf("a new check got a notice:\n%s", joined)
	}
}

func TestCommitAddingCheckGivesNoNotice(t *testing.T) {
	e := demo(t)
	e.write("gatecub.toml", declaration+"\n[[check]]\nid = \"build\"\nrun = [\"make\"]\n")
	e.git("add", "gatecub.toml")
	r := e.gate.Commit(t.Context(), "pre-commit")
	if len(r.Notes) != 0 {
		t.Errorf("notes = %q", r.Notes)
	}
}

func TestMergeSubject(t *testing.T) {
	e := demo(t)
	e.write("README.md", "x\n")
	e.git("add", "README.md")
	r := e.gate.Commit(t.Context(), "pre-merge-commit")
	if r.Allowed || r.Hook != "pre-merge-commit" {
		t.Fatalf("ruling = %+v", r)
	}
	checkDenial(t, r, e.gate.Store, "this merge is denied")
}

func push(e *env, lines ...string) []Ruling {
	return e.gate.Push(e.t.Context(), "origin", strings.NewReader(strings.Join(lines, "\n")+"\n"))
}

func TestPushVerifiedTip(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	e.passAll(e.treeOf("HEAD"))
	rulings := push(e, "refs/heads/main "+head+" refs/heads/main "+zero)
	if len(rulings) != 1 || !rulings[0].Allowed {
		t.Fatalf("rulings = %+v", rulings)
	}
	r := rulings[0]
	if r.Hook != "pre-push" || r.Ref != "refs/heads/main" || r.Commit != head || r.Tree != e.treeOf("HEAD") {
		t.Errorf("ruling = %+v", r)
	}
	if d := r.Decision(store.Enforce, now); d.Ref != "refs/heads/main" || d.Hook != "pre-push" || d.Outcome != "allowed" {
		t.Errorf("decision = %+v", d)
	}
}

func TestPushUnverifiedTip(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	rulings := push(e, "refs/heads/main "+head+" refs/heads/main "+zero)
	if len(rulings) != 1 || rulings[0].Allowed || rulings[0].Verdict == nil {
		t.Fatalf("rulings = %+v", rulings)
	}
	r := rulings[0]
	if r.Elsewhere {
		t.Error("the working tree holds the tip, yet Elsewhere is set")
	}
	text := checkDenial(t, r, e.gate.Store, "refs/heads/main", "unit", "lint")
	checkEndsWithRun(t, text)
}

func TestPushOfOtherBranchSaysWhereToRun(t *testing.T) {
	e := demo(t)
	e.passAll(e.treeOf("HEAD"))
	e.git("checkout", "-q", "-b", "feature")
	e.write("src/app.txt", "broken\n")
	feature := e.commit("broken")
	e.ran(e.treeOf("HEAD"), "unit", store.Failed)
	e.git("checkout", "-q", "main")
	rulings := push(e, "refs/heads/feature "+feature+" refs/heads/feature "+zero)
	if len(rulings) != 1 || rulings[0].Allowed {
		t.Fatalf("rulings = %+v", rulings)
	}
	r := rulings[0]
	if !r.Elsewhere || r.Commit != feature {
		t.Errorf("Elsewhere = %v, Commit = %s", r.Elsewhere, r.Commit)
	}
	text := checkDenial(t, r, e.gate.Store, "refs/heads/feature", "unit: failed", "checkout of that commit", feature)
	checkEndsWithRun(t, text)
}

func TestPushDeletionAllowed(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	rulings := push(e, "(delete) "+zero+" refs/heads/old "+head)
	if len(rulings) != 1 || !rulings[0].Allowed || rulings[0].Ref != "refs/heads/old" {
		t.Errorf("rulings = %+v", rulings)
	}
}

func TestPushThatAddsNothingAllowed(t *testing.T) {
	e := demo(t)
	first := e.git("rev-parse", "HEAD")
	e.write("README.md", "2\n")
	e.commit("second")
	e.write("README.md", "3\n")
	third := e.commit("third")
	// The branch is moved back: the remote's commit contains the new tip.
	rulings := push(e, "HEAD~2 "+first+" refs/heads/main "+third)
	if len(rulings) != 1 || !rulings[0].Allowed {
		t.Errorf("moving a branch back: rulings = %+v", rulings)
	}
	// The same commit again.
	rulings = push(e, "refs/heads/main "+third+" refs/heads/main "+third)
	if len(rulings) != 1 || !rulings[0].Allowed {
		t.Errorf("pushing what the remote has: rulings = %+v", rulings)
	}
}

func TestPushBranchUpdateThatAddsIsJudged(t *testing.T) {
	e := demo(t)
	first := e.git("rev-parse", "HEAD")
	e.write("README.md", "2\n")
	second := e.commit("second")
	rulings := push(e, "refs/heads/main "+second+" refs/heads/main "+first)
	if len(rulings) != 1 || rulings[0].Allowed {
		t.Errorf("a fast-forward without runs: rulings = %+v", rulings)
	}
}

func TestPushNewBranchAtCommitAnotherBranchHas(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	e.git("update-ref", "refs/remotes/origin/main", head)
	rulings := push(e, "refs/remotes/origin/main "+head+" refs/heads/release "+zero)
	if len(rulings) != 1 || rulings[0].Allowed {
		t.Errorf("a new branch whose tip is not verified: rulings = %+v", rulings)
	}
}

func TestPushTag(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	e.git("tag", "-a", "-m", "v1", "v1")
	tag := e.git("rev-parse", "v1")

	// The remote does not hold the commit: the tag is judged by its commit.
	rulings := push(e, "refs/tags/v1 "+tag+" refs/tags/v1 "+zero)
	if len(rulings) != 1 || rulings[0].Allowed || rulings[0].Commit != head || rulings[0].Tree != e.treeOf("HEAD") {
		t.Fatalf("rulings = %+v", rulings)
	}
	checkDenial(t, rulings[0], e.gate.Store, "refs/tags/v1")

	// A remote-tracking branch of the remote holds it: nothing is added.
	e.git("update-ref", "refs/remotes/origin/main", head)
	rulings = push(e, "refs/tags/v1 "+tag+" refs/tags/v1 "+zero)
	if len(rulings) != 1 || !rulings[0].Allowed {
		t.Errorf("a tag on a commit the remote has: rulings = %+v", rulings)
	}
	// Another remote's branch does not count.
	rulings = e.gate.Push(t.Context(), "upstream", strings.NewReader("refs/tags/v1 "+tag+" refs/tags/v1 "+zero+"\n"))
	if len(rulings) != 1 || rulings[0].Allowed {
		t.Errorf("a tag judged against another remote: rulings = %+v", rulings)
	}
}

func TestPushOneRulingPerRef(t *testing.T) {
	e := demo(t)
	head := e.git("rev-parse", "HEAD")
	e.passAll(e.treeOf("HEAD"))
	e.git("checkout", "-q", "-b", "feature")
	e.write("README.md", "x\n")
	feature := e.commit("feature")
	rulings := push(e,
		"refs/heads/main "+head+" refs/heads/main "+zero,
		"malformed line",
		"",
		"refs/heads/feature "+feature+" refs/heads/feature "+zero,
	)
	if len(rulings) != 2 {
		t.Fatalf("got %d rulings, want 2: %+v", len(rulings), rulings)
	}
	if !rulings[0].Allowed || rulings[1].Allowed {
		t.Errorf("allowed = %v, %v; want true, false", rulings[0].Allowed, rulings[1].Allowed)
	}
}

func TestPushWithoutDeclaration(t *testing.T) {
	e := demo(t)
	e.git("rm", "-q", "gatecub.toml")
	head := e.commit("remove")
	rulings := push(e, "refs/heads/main "+head+" refs/heads/main "+zero)
	if len(rulings) != 1 || rulings[0].Allowed || rulings[0].Cause != store.CauseNoDeclaration {
		t.Fatalf("rulings = %+v", rulings)
	}
	checkDenial(t, rulings[0], e.gate.Store, "refs/heads/main", "no declaration")
}

func TestPushOfTreeAndBlob(t *testing.T) {
	e := demo(t)
	tree := e.treeOf("HEAD")
	e.passAll(tree)
	rulings := push(e, "x "+tree+" refs/tags/t "+zero)
	if len(rulings) != 1 || !rulings[0].Allowed || rulings[0].Tree != tree {
		t.Errorf("a verified tree: rulings = %+v", rulings)
	}
	blob := e.git("rev-parse", "HEAD:README.md")
	rulings = push(e, "x "+blob+" refs/tags/b "+zero)
	if len(rulings) != 1 || rulings[0].Allowed || rulings[0].Cause != store.CauseTree {
		t.Errorf("a blob: rulings = %+v", rulings)
	}
}

func verdict(checks ...evidence.Check) *evidence.Verdict {
	return &evidence.Verdict{Tree: "t", Checks: checks}
}

func check(id, state string, deciding *store.Record) evidence.Check {
	return evidence.Check{Check: decl.Check{ID: id}, State: state, Deciding: deciding}
}

func TestDenialText(t *testing.T) {
	st := store.Open(t.TempDir())
	code := 3
	failed := &store.Record{RunID: "r1", Outcome: store.Failed, ExitStatus: &code, Log: "logs/r1.log"}
	timedOut := &store.Record{RunID: "r2", Outcome: store.TimedOut, Log: "logs/r2.log"}
	v := verdict(
		check("unit", evidence.Failed, failed),
		check("lint", evidence.NotRun, nil),
		check("slow", evidence.TimedOut, timedOut),
		check("ok", evidence.Satisfied, nil),
	)
	cases := []struct {
		name  string
		r     Ruling
		words []string
	}{
		{"commit here", Ruling{Hook: "pre-commit", Verdict: v}, []string{"this commit is denied"}},
		{"commit elsewhere", Ruling{Hook: "pre-commit", Verdict: v, Elsewhere: true, Differs: "The working tree holds other content than this commit."}, []string{"other content", "Make the working tree hold exactly"}},
		{"merge", Ruling{Hook: "pre-merge-commit", Verdict: v}, []string{"this merge is denied"}},
		{"push of a commit elsewhere", Ruling{Hook: "pre-push", Ref: "refs/heads/feature", Commit: "abc123", Verdict: v, Elsewhere: true}, []string{"the push of refs/heads/feature", "git worktree add --detach ../gatecub-check abc123"}},
		{"push of a tree elsewhere", Ruling{Hook: "pre-push", Ref: "refs/tags/t", Verdict: v, Elsewhere: true}, []string{"checkout of that content"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			words := append([]string{
				"unit: failed with exit status 3; log: " + st.Abs("logs/r1.log"),
				"lint: not run on this content",
				"slow: timed out; log: " + st.Abs("logs/r2.log"),
			}, tc.words...)
			text := checkDenial(t, tc.r, st, words...)
			checkEndsWithRun(t, text)
			if strings.Contains(text, "ok:") {
				t.Errorf("the denial names a satisfied check:\n%s", text)
			}
		})
	}
}

func TestDenialOfCauses(t *testing.T) {
	st := store.Open(t.TempDir())
	for _, cause := range []string{store.CauseNoDeclaration, store.CauseInvalidDeclaration, store.CauseTree, store.CauseStore, store.CauseInternal} {
		r := Cannot("pre-commit", cause, "reason for "+cause)
		checkDenial(t, r, st, "this commit is denied", "reason for "+cause)
		if r.Because() == "" {
			t.Errorf("Because for %s is empty", cause)
		}
		if d := r.Decision(store.Observe, now); d.Outcome != "denied" || d.Cause != cause || d.Mode != store.Observe || d.Checks != nil {
			t.Errorf("decision = %+v", d)
		}
	}
}

func TestDecision(t *testing.T) {
	at := time.Date(2026, 11, 14, 11, 30, 15, 999, time.FixedZone("x", 3600))
	v := verdict(
		check("unit", evidence.Failed, nil),
		check("lint", evidence.NotRun, nil),
		check("slow", evidence.TimedOut, nil),
		check("ok", evidence.Satisfied, nil),
	)
	r := Ruling{Hook: "pre-push", Ref: "refs/heads/main", Tree: "abc", Verdict: v}
	d := r.Decision(store.Enforce, at)
	want := store.Decision{
		Time: time.Date(2026, 11, 14, 10, 30, 15, 0, time.UTC), Hook: "pre-push", Mode: store.Enforce, Tree: "abc",
		Outcome: "denied", Ref: "refs/heads/main",
		Checks: map[string]string{"unit": "failed", "lint": "not_run", "slow": "timed_out"},
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("Decision =\n%+v\nwant\n%+v", d, want)
	}
	if d.Time.Location() != time.UTC {
		t.Errorf("time zone = %v", d.Time.Location())
	}
	allowed := Ruling{Hook: "pre-commit", Tree: "abc", Allowed: true, Verdict: verdict(check("ok", evidence.Satisfied, nil))}
	if d := allowed.Decision(store.Observe, at); d.Outcome != "allowed" || d.Checks != nil || d.Mode != store.Observe {
		t.Errorf("allowed decision = %+v", d)
	}
}

func TestObserved(t *testing.T) {
	r := Ruling{Hook: "pre-commit", Verdict: verdict(check("unit", evidence.Failed, nil), check("lint", evidence.NotRun, nil))}
	line := r.Observed()
	if strings.Count(line, "\n") != 1 || !strings.Contains(line, "would have been denied because `unit` failed and `lint` was not run") {
		t.Errorf("Observed = %q", line)
	}
	invalid := Cannot("pre-commit", store.CauseInvalidDeclaration, "x")
	if !strings.Contains(invalid.Observed(), "the declaration is invalid") {
		t.Errorf("Observed = %q", invalid.Observed())
	}
}
