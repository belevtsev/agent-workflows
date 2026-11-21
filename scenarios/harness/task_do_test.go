package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type doReply struct {
	Next, Delivery, Commit, Log, Then string
	Verified                          bool
}

// do runs one task do command and decodes its compact reply.
func do(t *testing.T, r *kitsutest.Repo, code int, args ...string) (doReply, kitsutest.Result) {
	t.Helper()
	got := r.Kitsu(append([]string{"task", "do"}, args...)...)
	if got.Code != code {
		t.Fatalf("task do %v = %d, want %d\n%s", args, got.Code, code, got.Output())
	}
	var reply doReply
	if code == 0 || code == 5 {
		if err := json.Unmarshal([]byte(got.Stdout), &reply); err != nil {
			t.Fatalf("task do did not print one JSON reply: %v\n%s", err, got.Output())
		}
	}
	return reply, got
}

func TestTaskDoVerifiesAndCommitsStagedWorkInOneCall(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	base := r.Git("rev-parse", "HEAD")
	r.Write("answer.txt", "42\n")
	r.Write("build/personal.txt", "developer draft\n")
	r.Git("add", "answer.txt")
	staged := r.Git("write-tree")

	reply, got := do(t, r, 0, "one-call", "--entry", "verify", "--commit", "--request", "Verify the staged answer and commit it if it passes",
		"--expect", "answer is 42", "--message", "Deliver the verified answer", "--include", "answer.txt",
		"--", "git", "grep", "--cached", "-e", "^42$", "--", "answer.txt")
	head := r.Git("rev-parse", "HEAD")
	if reply.Next != "done" || !reply.Verified || reply.Delivery != "committed" || reply.Commit != head || reply.Then != "" || len(got.Stdout) > 400 {
		t.Fatalf("one call did not finish the delivery in a short reply (%d bytes):\n%s", len(got.Stdout), got.Output())
	}
	if r.Git("rev-list", "--parents", "-1", head) != head+" "+base || r.Git("rev-parse", "HEAD^{tree}") != staged ||
		r.Git("status", "--porcelain") != "" || r.Read("build/personal.txt") != "developer draft\n" {
		t.Fatal("commit diverged from the staged candidate or changed unrelated work")
	}
	for _, id := range []string{"task-one-call", "unit", "lint"} {
		if files := r.RecordFiles(id); len(files) != 1 {
			t.Fatalf("%s has %d runs, want one", id, len(files))
		}
	}
	// The same command is safe to repeat: it reports the finished delivery.
	if again, _ := do(t, r, 0, "one-call", "--message", "Deliver the verified answer"); again.Next != "done" || again.Commit != head || r.Git("rev-parse", "HEAD") != head {
		t.Fatalf("repeating the command changed a finished delivery: %+v", again)
	}
}

func TestTaskDoStopsAtFailingAssertionAndContinues(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	base := r.Git("rev-parse", "HEAD")
	r.Write("answer.txt", "41\n")
	r.Git("add", "answer.txt")
	command := []string{"wrong-answer", "--commit", "--request", "Add the answer", "--expect", "answer is 42", "--message", "Add the answer",
		"--include", "answer.txt", "--", "git", "grep", "--cached", "-e", "^42$", "--", "answer.txt"}

	reply, got := do(t, r, 5, command...)
	if reply.Next != "implement" || reply.Verified || reply.Log == "" || reply.Then == "" || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("a failing assertion must stop before the commit and name its log:\n%s", got.Output())
	}
	// Nothing changed, so the same command only repeats the assertion.
	if reply, got = do(t, r, 5, command...); reply.Next != "implement" || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("an unchanged candidate was not retested:\n%s", got.Output())
	}
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	if reply, got = do(t, r, 0, command...); reply.Next != "done" || reply.Commit != r.Git("rev-parse", "HEAD") || reply.Commit == base {
		t.Fatalf("the corrected candidate was not delivered by the same command:\n%s", got.Output())
	}
}

func TestTaskDoRefusesToCommitWhenStagedContentDiffers(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	base := r.Git("rev-parse", "HEAD")
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	r.Write("notes.txt", "not staged\n")
	command := []string{"partly-staged", "--commit", "--request", "Add the answer", "--expect", "answer is 42", "--message", "Add the answer",
		"--include", "answer.txt", "--", "git", "grep", "-e", "^42$", "--", "answer.txt"}

	_, got := do(t, r, 1, command...)
	if !strings.Contains(got.Stderr, "staged content differs from the verified candidate") || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("an unstaged file was committed or the refusal is unclear:\n%s", got.Output())
	}
	r.Git("add", "notes.txt")
	// The task was opened with both files changed, so each needs confirmation.
	// One refusal names them all.
	if got := r.Kitsu("task", "do", command[0], "--message", "Add the answer"); got.Code != 1 ||
		!strings.Contains(got.Stderr, `pre-existing edit "answer.txt" requires explicit --include after confirming ownership; so do "notes.txt"`) {
		t.Fatalf("the refusal did not name every unconfirmed path:\n%s", got.Output())
	}
	if _, got = do(t, r, 1, command...); !strings.Contains(got.Stderr, `"notes.txt" requires explicit --include`) || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("an unconfirmed path was committed:\n%s", got.Output())
	}
	if reply, got := do(t, r, 0, append([]string{command[0], "--include", "notes.txt"}, command[1:]...)...); reply.Next != "done" || reply.Commit == base {
		t.Fatalf("the confirmed candidate was not delivered:\n%s", got.Output())
	}
}

func TestTaskDoStopsWhereADecisionIsNeeded(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	base := r.Git("rev-parse", "HEAD")

	// Opened before the edit: there is nothing to verify yet.
	if reply, got := do(t, r, 0, "staged-later", "--commit", "--request", "Track the README"); reply.Next != "implement" || !strings.Contains(reply.Then, "--expect") {
		t.Fatalf("a task without an assertion must wait at implement:\n%s", got.Output())
	}
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	assertion := []string{"--expect", "README is tracked", "--", "git", "ls-files", "--error-unmatch", "--", "README.md"}
	// Verified, but a commit needs its message.
	reply, got := do(t, r, 0, append([]string{"staged-later"}, assertion...)...)
	if reply.Next != "commit" || !reply.Verified || !strings.Contains(reply.Then, "--message") || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("a verified candidate must wait for a commit message:\n%s", got.Output())
	}
	// The edit was made after the task opened, so no path needs confirmation.
	if reply, got = do(t, r, 0, "staged-later", "--message", "Add README"); reply.Next != "done" || reply.Commit == base {
		t.Fatalf("the saved assertion did not carry the commit:\n%s", got.Output())
	}

	for _, refused := range [][]string{
		{"staged-later", "--request", "Another request"},
		{"staged-later", "--require-review"},
		{"no-request", "--expect", "x", "--", "true"},
		{"half-assertion", "--request", "x", "--expect", "y"},
		{"half-assertion", "--request", "x", "--", "true"},
		{"claim-without-actor", "--request", "x", "--claim-review", "claude"},
		{"message-without-scope", "--request", "x", "--message", "y"},
		{"ticket-with-request", "--from", "ticket.md", "--request", "x"},
	} {
		if got := r.Kitsu(append([]string{"task", "do"}, refused...)...); got.Code == 0 || got.Code == 5 {
			t.Errorf("task do %v was accepted:\n%s", refused, got.Output())
		}
	}
}

func TestTaskDoLeavesOtherWorkflowsToTheirOwnCommands(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	f := flow{t: t, repo: r, id: "verify-only"}
	f.call(0, "open", "--until", "verify", "--request", "Check the base")
	// A task opened for verification alone cannot be widened by task do.
	if got := r.Kitsu("task", "do", "verify-only", "--commit", "--message", "x"); got.Code == 0 || !strings.Contains(got.Stderr, "task extend") {
		t.Fatalf("task do widened a saved scope:\n%s", got.Output())
	}
	if got := r.Kitsu("task", "do", "verify-only", "--message", "x"); got.Code == 0 || !strings.Contains(got.Stderr, "no commit in its scope") {
		t.Fatalf("a message without commit scope was accepted:\n%s", got.Output())
	}
	reply, got := do(t, r, 0, "verify-only", "--expect", "the base exists", "--", "git", "rev-parse", "--verify", "HEAD")
	if reply.Next != "done" || !reply.Verified || reply.Commit != "" {
		t.Fatalf("task do did not finish a verification task opened by task open:\n%s", got.Output())
	}
}

func TestTaskDoCarriesReviewFindingsToASecondRound(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	base := r.Git("rev-parse", "HEAD")
	r.Write("README.md", "# Projcet\n")
	r.Git("add", "README.md")
	command := append([]string{"reviewed", "--require-review", "--actor", "coordinator", "--claim-review", "claude"}, trackReadme...)
	claimed := func() string {
		t.Helper()
		got := r.Kitsu(append([]string{"task", "do"}, command...)...)
		var reply struct {
			Next  string
			Claim struct{ Ticket string }
		}
		if err := json.Unmarshal([]byte(got.Stdout), &reply); got.Code != 0 || err != nil || reply.Next != "review" || reply.Claim.Ticket == "" {
			t.Fatalf("task do did not reserve the review:\n%s", got.Output())
		}
		return reply.Claim.Ticket
	}

	ticket := claimed()
	// Without the reviewer's result the command cannot pass the review.
	if _, got := do(t, r, 1, command...); !strings.Contains(got.Stderr, "review is pending") || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("a pending review did not stop the delivery:\n%s", got.Output())
	}
	sendNativeReviewEvent(t, r, "claude", "SubagentStart", "coordinator", "first-reviewer", "")
	finishNativeFixture(t, r, "reviewed", ticket, "claude", "coordinator", "first-reviewer", "The heading is misspelled.", "README.md: the heading says Projcet")
	_, got := do(t, r, 0, command...)
	if !strings.Contains(got.Stdout, `"next":"implement"`) || !strings.Contains(got.Stdout, "the heading says Projcet") || r.Git("rev-parse", "HEAD") != base {
		t.Fatalf("the finding did not return the task to its author with its text:\n%s", got.Output())
	}

	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	second := claimed()
	if second == ticket {
		t.Fatal("the corrected candidate reused the first review")
	}
	sendNativeReviewEvent(t, r, "claude", "SubagentStart", "coordinator", "second-reviewer", "")
	finishNativeFixture(t, r, "reviewed", second, "claude", "coordinator", "second-reviewer", "The heading is correct.")
	if reply, got := do(t, r, 0, command...); reply.Next != "done" || reply.Commit == base || r.Git("show", "HEAD:README.md") != "# Project" {
		t.Fatalf("the reviewed correction was not delivered:\n%s", got.Output())
	}
}
