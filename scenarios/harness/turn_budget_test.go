package harness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// The turn budget is what a coding agent spends on Kitsu itself when it
// follows skills/task/SKILL.md on a happy path: the commands it runs and the
// bytes they return. Each command is one more model turn that reads the whole
// conversation again, so the number of commands is the cost that matters.
//
// A budget only goes down. When a path gets shorter, lower its number here in
// the same change. Raising one needs a measured reason in the pull request.
// Replies hold absolute paths, so the byte budgets leave room for a longer
// temporary directory; the command counts are exact.
type turnBudget struct {
	calls         int // commands the coordinator runs
	replyBytes    int // bytes those commands print for the coordinator
	reviewerCalls int // commands the independent reviewer runs
	reviewerBytes int // bytes the reviewer's commands print
}

var turnBudgets = map[string]turnBudget{
	"verify staged work and commit":      {calls: 1, replyBytes: 220},
	"implement and commit":               {calls: 1, replyBytes: 220},
	"verify, then a plain git commit":    {calls: 1, replyBytes: 220},
	"implement, required review, commit": {calls: 2, replyBytes: 1100, reviewerCalls: 1, reviewerBytes: 1700},
	"bug ticket to verified commit":      {calls: 2, replyBytes: 850},
}

// Text that every task or every session carries: the skills in words, as
// `wc -w` counts them, and the session-start notice in bytes.
const (
	taskSkillWords    = 227
	gateSkillWords    = 1440
	sessionStartBytes = 630
)

// turns counts the Kitsu commands of one task as the coordinator sees them.
type turns struct {
	t          *testing.T
	repo       *kitsutest.Repo
	id         string
	calls      int
	replyBytes int
	next       string
	ticket     string // of the reserved review, for the reviewer's handoff
}

// do is one coordinator command: task do with its short default reply.
func (m *turns) do(code int, args ...string) {
	m.t.Helper()
	got := m.repo.Kitsu(append([]string{"task", "do", m.id}, args...)...)
	m.calls++
	m.replyBytes += len(got.Stdout) + len(got.Stderr)
	if got.Code != code {
		m.t.Fatalf("task do %v = %d, want %d\n%s", args, got.Code, code, got.Output())
	}
	var reply struct {
		Next  string `json:"next"`
		Claim struct{ Ticket string }
	}
	if err := json.Unmarshal([]byte(got.Stdout), &reply); err != nil {
		m.t.Fatalf("task do did not print one JSON reply: %v\n%s", err, got.Output())
	}
	m.next, m.ticket = reply.Next, reply.Claim.Ticket
}

func (m *turns) want(next string) {
	m.t.Helper()
	if m.next != next {
		m.t.Fatalf("next = %q, want %q", m.next, next)
	}
}

// within fails when the task spent more commands than its budget, or fewer: a
// shorter path must lower the budget, so that it cannot grow back unnoticed.
func (m *turns) within(name string, reviewerCalls, reviewerBytes int) {
	m.t.Helper()
	b, ok := turnBudgets[name]
	if !ok {
		m.t.Fatalf("no turn budget is named %q", name)
	}
	m.t.Logf("%s: %d coordinator commands returning %d bytes, %d reviewer commands returning %d bytes",
		name, m.calls, m.replyBytes, reviewerCalls, reviewerBytes)
	if m.calls != b.calls || reviewerCalls != b.reviewerCalls {
		m.t.Errorf("%s took %d coordinator and %d reviewer commands. The budget is %d and %d: lower it for a shorter path, or shorten the path",
			name, m.calls, reviewerCalls, b.calls, b.reviewerCalls)
	}
	if m.replyBytes > b.replyBytes || reviewerBytes > b.reviewerBytes {
		m.t.Errorf("%s returned %d bytes to the coordinator and %d to the reviewer. The budget is %d and %d",
			name, m.replyBytes, reviewerBytes, b.replyBytes, b.reviewerBytes)
	}
}

func TestTurnBudgetVerifyStagedWorkAndCommit(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	m := &turns{t: t, repo: r, id: "budget-verify"}
	m.do(0, "--entry", "verify", "--commit", "--request", "Verify the staged answer and commit it if it passes",
		"--expect", "answer is 42", "--message", "Deliver the verified answer", "--include", "answer.txt",
		"--", "git", "grep", "--cached", "-e", "^42$", "--", "answer.txt")
	m.want("done")
	m.within("verify staged work and commit", 0, 0)
}

// The model may commit with Git itself. That costs no further Kitsu command:
// the receipt finds the commit.
func TestTurnBudgetVerifyThenPlainGitCommit(t *testing.T) {
	r := plainRepo(t)
	m := plainTask(t, r, "budget-plain", "a.txt")
	m.want("commit")
	r.Git("commit", "-qm", "Add a")
	markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`")
	m.within("verify, then a plain git commit", 0, 0)
}

var trackReadme = []string{"--commit", "--request", "Track the README", "--expect", "README is tracked", "--message", "Add README",
	"--include", "README.md", "--", "git", "ls-files", "--error-unmatch", "--", "README.md"}

func TestTurnBudgetImplementAndCommit(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	m := &turns{t: t, repo: r, id: "budget-implement"}
	m.do(0, trackReadme...)
	m.want("done")
	m.within("implement and commit", 0, 0)
}

func TestTurnBudgetImplementRequiredReviewCommit(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	m := &turns{t: t, repo: r, id: "budget-review"}
	command := append([]string{"--require-review", "--actor", "coordinator", "--claim-review", "claude"}, trackReadme...)
	m.do(0, command...)
	m.want("review")
	ticket := m.ticket

	// The host starts the reviewer and its hooks report the worker: neither is
	// a Kitsu command of a model. The reviewer reads its handoff once.
	handoff := r.Kitsu("task", "show", m.id, "--review-ticket", ticket, "--review-brief")
	if handoff.Code != 0 {
		t.Fatal(handoff.Output())
	}
	sendNativeReviewEvent(t, r, "claude", "SubagentStart", "coordinator", "worker", "")
	finishNativeFixture(t, r, m.id, ticket, "claude", "coordinator", "worker", "Verified the staged documentation.")

	m.do(0, command...)
	m.want("done")
	m.within("implement, required review, commit", 1, len(handoff.Stdout)+len(handoff.Stderr))
}

func TestTurnBudgetBugTicketToVerifiedCommit(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("output.txt", "broken\n")
	r.Git("add", ".")
	r.Git("commit", "-qm", "baseline")
	source := filepath.Join(t.TempDir(), "export-bug.md")
	if err := os.WriteFile(source, []byte("# Export is broken\nReturn ready.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &turns{t: t, repo: r, id: "budget-bug"}
	m.do(5, "--from", source, "--expect", "output is ready", "--", "git", "grep", "-q", "-e", "^ready$", "--", "output.txt")
	m.want("repair")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	m.do(0, "--message", "Fix export")
	m.want("done")
	if receipt := r.Kitsu("task", "receipt", m.id, "--json"); receipt.Code != 0 || !strings.Contains(receipt.Stdout, r.Git("rev-parse", "HEAD")) {
		t.Fatalf("the delivery has no receipt for its commit:\n%s", receipt.Output())
	}
	m.within("bug ticket to verified commit", 0, 0)
}

func TestTurnBudgetFixedText(t *testing.T) {
	for path, budget := range map[string]int{
		"../../skills/task/SKILL.md":  taskSkillWords,
		"../../skills/kitsu/SKILL.md": gateSkillWords,
	} {
		text, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		if words := len(strings.Fields(string(text))); words > budget {
			t.Errorf("%s has %d words. The budget is %d", path, words, budget)
		}
	}
	r := kitsutest.Demo(t)
	dir, err := json.Marshal(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	notice := `{"cwd":` + string(dir) + `,"session_id":"0199a2b4-5c6d-7e8f-9a0b-1c2d3e4f5a6b"}`
	for _, host := range []string{"claude", "codex"} {
		got := r.Run(notice, filepath.Join(kitsutest.Binary(t), "kitsu"), "context", "--host", host)
		if got.Code != 0 || got.Stdout == "" || len(got.Stdout) > sessionStartBytes {
			t.Errorf("the %s session-start notice exited %d with %d bytes. The budget is %d\n%s",
				host, got.Code, len(got.Stdout), sessionStartBytes, got.Output())
		}
	}
}
