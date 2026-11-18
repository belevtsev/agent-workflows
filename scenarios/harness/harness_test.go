// Package harness_test drives the shipped CLI in isolated real repositories.
// Host judgments are scripted; behavior commands, Git and evidence are real.
package harness_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type state struct {
	Next, Ticket, Delivery string
	CommitTarget           string `json:"commit_target"`
	Verified               bool
	Proof                  *struct {
		Run     string `json:"run_id"`
		Outcome string
	} `json:"behavioral_assertion"`
}

type flow struct {
	t     *testing.T
	repo  *kitsutest.Repo
	id    string
	state state
}

func (f *flow) call(code int, args ...string) {
	f.t.Helper()
	argv := []string{"task", args[0], f.id, "--json"}
	r := f.repo.Kitsu(append(argv, args[1:]...)...)
	if r.Code != code {
		f.t.Fatalf("%v = %d want %d\n%s", argv, r.Code, code, r.Output())
	}
	if code != 0 && code != 5 {
		return
	}
	f.state = state{}
	if err := json.Unmarshal([]byte(r.Stdout), &f.state); err != nil {
		f.t.Fatal(err, r.Output())
	}
}

func (f *flow) want(next string) {
	f.t.Helper()
	if f.state.Next != next {
		f.t.Fatalf("next = %s, want %s", f.state.Next, next)
	}
}
func (f *flow) record(action, actor, note string, extra ...string) {
	f.t.Helper()
	f.want(action)
	f.call(0, append([]string{"record", action, "--actor", actor, "--note", note, "--ticket", f.state.Ticket}, extra...)...)
}
func (f *flow) review(actor, note string, extra ...string) {
	f.t.Helper()
	f.call(0, "claim", "--actor", "host", "--ticket", f.state.Ticket)
	f.record("review", actor, note, extra...)
}

func TestGoExistingFixRepairVerificationAndRecovery(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("go.mod", "module example.test/format\n\ngo 1.22\n")
	r.Write("label.go", "package format\nfunc Label(x int) int {return x}\n")
	r.Write("label_test.go", `package format
import "testing"
func TestPositive(t *testing.T){if Label(2)!=2{t.Fatal("positive")}}
func TestNegative(t *testing.T){if Label(-2)!=2{t.Fatal("negative")}}
`)
	r.Git("add", "-A")
	r.Git("commit", "-qm", "baseline")
	// Plausible user fix handles one input but misses the general case.
	r.Write("label.go", "package format\nfunc Label(x int) int {if x == -1{return 1};return x}\n")
	if got := r.Run("", "go", "test", "./..."); got.Code == 0 || !strings.Contains(got.Output(), "negative") {
		t.Fatal("fixture did not expose the missed behavior", got.Output())
	}
	f := flow{t: t, repo: r, id: "label"}
	f.call(0, "open", "--request", "Review the existing fix, repair, test and commit", "--entry", "review", "--until", "deliver", "--repair", "--commit")
	f.record("intake", "host", "Negative values should return their magnitude")
	f.call(0, "claim", "--actor", "host", "--ticket", f.state.Ticket)
	f.call(1, "claim", "--actor", "duplicate", "--ticket", f.state.Ticket)
	f.call(0, "next")
	f.call(1, "record", "review", "--actor", "host", "--ticket", f.state.Ticket, "--note", "approve")
	f.record("review", "reviewer-1", "Negative two still fails", "--finding", "major label.go: only -1 is handled; TestNegative reproduces")
	f.want("repair")
	ticket := f.state.Ticket
	r.Write("label.go", "package format\nfunc Label(x int) int {if x<0{return -x};return x}\n")
	f.call(0, "next")
	if f.state.Ticket != ticket {
		t.Fatal("lost interrupted repair")
	}
	f.record("repair", "host", "Handle all negative fixture inputs")
	f.review("reviewer-2", "Checked positive and negative examples")
	f.want("verify")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "magnitude", "--", "kitsu-toolchain-definitely-missing")
	f.want("verify")
	testTicket := f.state.Ticket
	args := []string{"test", "--ticket", testTicket, "--expect", "positive and negative magnitude", "--", "go", "test", "./..."}
	f.call(0, args...)
	f.want("commit")
	run := f.state.Proof.Run
	f.call(0, args...)
	if f.state.Proof.Run != run {
		t.Fatal("identical successful assertion executed twice")
	}
	f.call(1, "commit", "--message", "fix magnitude", "--ticket", f.state.Ticket)
	r.Git("add", "label.go")
	f.call(0, "next")
	f.call(1, "commit", "--message", "fix magnitude", "--ticket", f.state.Ticket)
	// Simulate Git succeeding immediately before the host died.
	r.Git("commit", "-qm", "fix magnitude")
	f.call(0, "next")
	f.want("done")
	if !f.state.Verified || f.state.Delivery != "committed" {
		t.Fatalf("delivery = %+v", f.state)
	}
	f.call(0, "commit", "--message", "fix magnitude", "--ticket", testTicket)
	if r.Git("rev-list", "--count", "HEAD") != "2" {
		t.Fatal("duplicate commit")
	}
	r.Write("label.go", "package format\nfunc Label(x int) int {return x}\n")
	f.call(0, "next")
	f.want("review")
	if f.state.Verified {
		t.Fatal("stale evidence certified another candidate")
	}
}

func TestJavaScriptPlanThenImplement(t *testing.T) {
	r := kitsutest.NewRepo(t)
	f := flow{t: t, repo: r, id: "export"}
	r.Write("DESIGN.md", "Export doubles the supplied value.\n")
	f.call(0, "open", "--request", "Plan from DESIGN.md", "--entry", "plan", "--until", "plan")
	f.record("intake", "host", "Read DESIGN.md")
	before := r.Git("status", "--porcelain")
	f.record("plan", "host", "Add double and assert double(3) is 6")
	f.want("done")
	if r.Git("status", "--porcelain") != before {
		t.Fatal("plan modified the source")
	}
	f.call(0, "extend", "--require-review", "--until", "verify")
	f.want("implement")
	r.Write("double.js", "module.exports = x => 2 * x;\n")
	r.Write("test.js", "if (require('./double')(3) !== 6) process.exit(1);\n")
	f.record("implement", "host", "Implemented the approved design and regression")
	f.review("review-js", "Checked export signature")
	js, err := exec.LookPath("node")
	if err != nil {
		js, err = exec.LookPath("bun")
	}
	if err != nil {
		t.Skip("requires Node.js or Bun for behavioral replay")
	}
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "double(3) returns 6", "--", js, "test.js")
	f.want("done")
	if h := r.Kitsu("task", "show", f.id, "--json"); h.Code != 0 || !strings.Contains(h.Stdout, "Add double") {
		t.Fatal("plan history unavailable", h.Output())
	}
}

func TestCppReviewOnlyPreservesIndexAndWorkingTree(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("size.cpp", "constexpr int bytes(int n){return n;}\n")
	r.Git("add", "size.cpp")
	r.Git("commit", "-qm", "start")
	r.Write("size.cpp", "constexpr int bytes(int n){return n*4;}\nstatic_assert(bytes(3)==12);\n")
	r.Git("add", "size.cpp")
	r.Write("notes.txt", "unrelated developer notes\n")
	index, status := r.Git("write-tree"), r.Git("status", "--porcelain")
	f := flow{t: t, repo: r, id: "cpp"}
	f.call(0, "open", "--request", "Review the staged C++ change", "--until", "review")
	f.record("intake", "host", "Inspect staged size.cpp")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("requires C++ compiler for review control")
	}
	if build := r.Run("", cxx, "-std=c++11", "-fsyntax-only", "size.cpp"); build.Code != 0 {
		t.Fatal(build.Output())
	}
	f.review("cpp-reviewer", "Static assertion passed; overflow still needs consideration", "--finding", "size.cpp: multiplication can overflow")
	f.want("done")
	if index != r.Git("write-tree") || status != r.Git("status", "--porcelain") || r.Read("notes.txt") != "unrelated developer notes\n" {
		t.Fatal("read-only review touched developer edits")
	}
	f.call(1, "record", "repair", "--actor", "host", "--ticket", "invalid", "--note", "change")
}

func TestDeclaredChecksAndEvidenceRecovery(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "verify-gate"}
	f.call(0, "open", "--request", "Verify repository behavior", "--until", "verify")
	f.record("intake", "host", "Original unit and lint requirements also apply")
	r.Write("BAD", "force full-suite failure")
	f.call(0, "next")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "fixture unit behavior", "--", "sh", "unit.sh")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("passing targeted assertion masked failing gate")
	}
	r.Remove("BAD")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "fixture unit behavior", "--", "sh", "unit.sh")
	f.want("done")
	log := ".git/kitsu/logs/" + f.state.Proof.Run + ".log"
	r.Remove(log)
	f.call(0, "next")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("missing local assertion log accepted")
	}
}

func TestExpiredScopeAndClaimRecovery(t *testing.T) {
	r := kitsutest.NewRepo(t)
	f := flow{t: t, repo: r, id: "pause"}
	f.call(0, "open", "--request", "Review this change", "--until", "review", "--time-limit", "1ns")
	f.want("paused")
	f.call(0, "extend", "--time-limit", "1m")
	f.record("intake", "host", "Inspect changed source")
	ticket := f.state.Ticket
	f.call(0, "claim", "--ticket", ticket, "--actor", "resumed-host")
	f.call(1, "record", "review", "--ticket", ticket, "--actor", "resumed-host", "--note", "claimant self-review")
	r.Write("new.txt", "changed concurrently\n")
	f.call(0, "next")
	f.call(1, "record", "review", "--ticket", ticket, "--actor", "stale-reviewer", "--note", "old candidate")
	f.want("reconcile")
	if f.state.Ticket != "" {
		t.Fatal("stale review exposed a fresh dispatch ticket", f.state)
	}
	f.call(0, "release", "--ticket", ticket)
	f.review("fresh-reviewer", "No findings against the new candidate")
	f.want("done")
}

func TestAuthorizedCommitPreservesOtherWork(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("README.md", "user-owned change\n")
	f := flow{t: t, repo: r, id: "ship"}
	f.call(0, "open", "--request", "Verify and commit the README change", "--entry", "verify", "--until", "deliver", "--commit")
	f.record("intake", "host", "README must mention the requested phrase")
	r.Git("add", "README.md")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "README mentions the requested phrase", "--", "git", "grep", "user-owned", "--", "README.md")
	f.want("commit")
	f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Update README")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Update README", "--include", "README.md")
	f.want("done")
	if f.state.Delivery != "committed" || r.Git("status", "--porcelain") != "" {
		t.Fatal("authorized commit was not delivered cleanly")
	}
	if got := r.Kitsu("task", "list", "--json"); got.Code != 0 || !strings.Contains(got.Stdout, "ship") {
		t.Fatal(got.Output())
	}
}

func TestPublishChecksActualBranchBeforeCompleting(t *testing.T) {
	r := kitsutest.Demo(t)
	remote := t.TempDir()
	r.Git("init", "--bare", remote)
	r.Git("remote", "add", "upstream", remote)
	f := flow{t: t, repo: r, id: "publish"}
	f.call(0, "open", "--request", "Verify and publish this existing commit", "--entry", "verify", "--until", "deliver", "--publish-to", "upstream/topic")
	f.record("intake", "host", "Check the unit behavior and confirm the published branch")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "fixture unit behavior", "--", "sh", "unit.sh")
	if files := r.RecordFiles("unit"); len(files) != 1 {
		t.Fatalf("same assertion and gate check executed %d times", len(files))
	}
	f.want("publish")
	f.call(1, "confirm", "--ticket", f.state.Ticket)
	r.Git("push", "upstream", "HEAD:refs/heads/topic")
	f.call(0, "next")
	f.want("publish")
	other := t.TempDir()
	r.Git("init", "--bare", other)
	r.Git("config", "--add", "remote.upstream.pushurl", remote)
	r.Git("config", "--add", "remote.upstream.pushurl", other)
	f.call(1, "confirm", "--ticket", f.state.Ticket)
	r.Git("push", other, "HEAD:refs/heads/topic")
	ticket := f.state.Ticket
	f.call(0, "confirm", "--ticket", ticket)
	f.want("done")
	f.call(0, "confirm", "--ticket", ticket)
	f.call(1, "confirm", "--ticket", ticket, "--pr", "https://github.com/example/other/pull/1")
	f.call(0, "extend", "--pr-base", "main")
	f.want("publish")
	f.call(2, "confirm", "--ticket", f.state.Ticket, "--pr", "invalid")
}

func TestDeliveryRecognizesAnAlreadyCommittedCandidate(t *testing.T) {
	r := kitsutest.Demo(t)
	head := r.Git("rev-parse", "HEAD")
	f := flow{t: t, repo: r, id: "existing-commit"}
	f.call(0, "open", "--request", "Verify and deliver the existing fix", "--entry", "verify", "--until", "deliver", "--commit")
	f.record("intake", "host", "Verify the commit already in the checkout")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "fixture unit behavior", "--", "sh", "unit.sh")
	f.want("done")
	if f.state.Delivery != "committed" || r.Git("rev-parse", "HEAD") != head {
		t.Fatal("existing commit was not recognized")
	}
}

func TestStagedReviewChangesWithIndexOnly(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("README.md", "staged one\n")
	r.Git("add", "README.md")
	r.Write("README.md", "working two\n")
	f := flow{t: t, repo: r, id: "staged"}
	f.call(0, "open", "--request", "Review the staged patch", "--until", "review")
	f.record("intake", "host", "Inspect the index")
	f.review("review-first", "Reviewed staged one")
	f.want("done")
	r.Git("add", "README.md")
	f.call(0, "next")
	f.want("review")
	f.call(0, "extend", "--until", "verify")
	f.want("review")
}

func TestInterruptedProofNeedsFinalObservation(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "interrupted-proof"}
	f.call(0, "open", "--request", "Verify this checkout", "--until", "verify")
	f.record("intake", "host", "Run unit test and repository checks")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	file := ".git/kitsu-stages/task-harness-" + f.id + ".json"
	var task map[string]any
	if err := json.Unmarshal([]byte(r.Read(file)), &task); err != nil {
		t.Fatal(err)
	}
	proof := task["proof"].(map[string]any)
	delete(proof, "checked_at")
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(file, string(data))
	f.call(0, "next")
	f.want("verify")
	run := f.state.Proof.Run
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	f.want("done")
	if f.state.Proof.Run != run {
		t.Fatal("discarded retained assertion after interruption")
	}
}

func TestCommitRecoversWhenHeadMovesWithWorkingPatchIntact(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "head-moved"}
	f.call(0, "open", "--request", "Verify and commit the change", "--entry", "verify", "--until", "deliver", "--commit")
	f.record("intake", "host", "Test and commit")
	r.Write("README.md", "proposed change\n")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	f.want("commit")
	r.Git("commit", "--allow-empty", "-qm", "parallel work")
	r.Git("add", "README.md")
	f.call(0, "next")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Apply new patch")
	f.want("done")
}

func TestBriefRetainsDecisionAndExpandsFullFindings(t *testing.T) {
	r := kitsutest.NewRepo(t)
	f := flow{t: t, repo: r, id: "brief"}
	f.call(0, "open", "--request", "Review the diff", "--until", "review")
	f.record("intake", "host", "Inspect the candidate")
	f.review("reviewer", "A corner case fails", "--finding", "Mismatching exported names")
	res := r.Kitsu("task", "next", f.id, "--brief")
	var v struct {
		Next    string
		Count   int  `json:"review_findings"`
		Current bool `json:"review_current"`
		Details []string
	}
	if res.Code != 0 || json.Unmarshal([]byte(res.Stdout), &v) != nil || v.Next != "done" || v.Count != 1 || !v.Current || len(v.Details) < 2 || v.Details[0] != "kitsu" {
		t.Fatal(res.Output())
	}
	full := r.Kitsu(v.Details[1:]...)
	if full.Code != 0 || !strings.Contains(full.Stdout, "Mismatching exported names") {
		t.Fatal(full.Output())
	}
}

func TestVerifiedCommitTicketSurvivesStagingExactlyTheVerifiedTree(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "stage-verified"}
	f.call(0, "open", "--request", "Verify and commit a change", "--entry", "verify", "--until", "deliver", "--commit")
	f.record("intake", "host", "Verify then commit the task files")
	r.Write("one", "one")
	r.Write("two", "two")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	ticket := f.state.Ticket
	r.Git("add", "one")
	if res := r.Kitsu("task", "commit", f.id, "--ticket", ticket, "--message", "Partial index"); res.Code == 0 {
		t.Fatal("committed an incomplete index")
	}
	r.Git("add", "two")
	f.call(0, "commit", "--ticket", ticket, "--message", "Verified change")
	f.want("done")
}

func TestReviewScopeExtensionChecksTheWholeDeliveryCandidate(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("staged.txt", "review this change")
	r.Git("add", "staged.txt")
	r.Write("untracked.txt", "also going into the verified tree")
	f := flow{t: t, repo: r, id: "partial-review"}
	f.call(0, "open", "--request", "Review the staged change", "--entry", "review", "--until", "review")
	f.record("intake", "host", "Inspect staged content")
	f.review("index-reviewer", "Staged change looks good")
	f.want("done")
	f.call(0, "extend", "--until", "deliver", "--commit")
	f.want("review")
	received := r.Kitsu("task", "claim", f.id, "--actor", "host", "--ticket", f.state.Ticket, "--brief")
	var expanded struct {
		Changes      []string `json:"review_changes"`
		ScopeChanged bool     `json:"review_scope_changed"`
	}
	if err := json.Unmarshal([]byte(received.Stdout), &expanded); received.Code != 0 || err != nil || !expanded.ScopeChanged || len(expanded.Changes) < 2 {
		t.Fatal("missing expanded scope", received.Output())
	}
	if diff := r.Git(expanded.Changes[1:]...); !strings.Contains(diff, "untracked.txt") || strings.Contains(diff, "staged.txt") {
		t.Fatalf("incremental review did not identify the previously excluded file: %s", diff)
	}
	f.record("review", "full-reviewer", "Checked both changed files")
	r.Git("commit", "--allow-empty", "--only", "-qm", "Intervening base change")
	f.call(0, "next")
	f.want("review")
	reservation := r.Kitsu("task", "claim", f.id, "--actor", "host", "--ticket", f.state.Ticket, "--brief")
	var receipt struct {
		Changes     []string `json:"review_changes"`
		BaseChanged bool     `json:"review_base_changed"`
	}
	if err := json.Unmarshal([]byte(reservation.Stdout), &receipt); reservation.Code != 0 || err != nil || !receipt.BaseChanged || len(receipt.Changes) < 2 || receipt.Changes[0] != "git" {
		t.Fatalf("missing re-review context: %s", reservation.Output())
	}
	if diff := r.Git(receipt.Changes[1:]...); diff != "" {
		t.Fatalf("the worktree stayed identical across the base change: %s", diff)
	}
	f.record("review", "new-base-reviewer", "Checked the diff against the new base")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "Run unit suite", "--", "sh", "unit.sh")
	r.Git("add", "untracked.txt")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver reviewed change", "--include", "staged.txt", "--include", "untracked.txt")
	f.want("done")
}
