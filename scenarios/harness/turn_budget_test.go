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
	"verify staged work and commit":      {calls: 4, replyBytes: 2800},
	"implement and commit":               {calls: 5, replyBytes: 2900},
	"implement, required review, commit": {calls: 6, replyBytes: 4100, reviewerCalls: 1, reviewerBytes: 1700},
	"bug ticket to verified commit":      {calls: 7, replyBytes: 5600},
}

// Text that every task or every session carries: the skills in words, as
// `wc -w` counts them, and the session-start notice in bytes.
const (
	taskSkillWords    = 920
	gateSkillWords    = 1478
	sessionStartBytes = 880
)

// turns counts the Kitsu commands of one task as the coordinator sees them.
type turns struct {
	t          *testing.T
	repo       *kitsutest.Repo
	id         string
	calls      int
	replyBytes int
	next       string
	ticket     string
}

// spend counts one coordinator command and the bytes it returned.
func (m *turns) spend(code int, argv ...string) kitsutest.Result {
	m.t.Helper()
	got := m.repo.Kitsu(argv...)
	m.calls++
	m.replyBytes += len(got.Stdout) + len(got.Stderr)
	if got.Code != code {
		m.t.Fatalf("%v = %d, want %d\n%s", argv, got.Code, code, got.Output())
	}
	return got
}

// run is one coordinator command in the brief form the skill prescribes.
func (m *turns) run(code int, verb string, args ...string) {
	m.t.Helper()
	got := m.spend(code, append([]string{"task", verb, m.id, "--brief"}, args...)...)
	var reply struct {
		Next   string `json:"next"`
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &reply); err != nil {
		m.t.Fatalf("task %s did not print a brief reply: %v\n%s", verb, err, got.Output())
	}
	m.next, m.ticket = reply.Next, reply.Ticket
}

func (m *turns) want(next string) {
	m.t.Helper()
	if m.next != next {
		m.t.Fatalf("next = %q, want %q", m.next, next)
	}
}

func (m *turns) record(action, note string, extra ...string) {
	m.t.Helper()
	m.want(action)
	m.run(0, "record", append([]string{action, "--actor", "coordinator", "--ticket", m.ticket, "--note", note}, extra...)...)
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
	m.run(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the staged answer and commit it if it passes")
	m.record("intake", "The staged answer must equal 42")
	m.want("verify")
	m.run(0, "test", "--ticket", m.ticket, "--expect", "answer is 42", "--", "git", "grep", "--cached", "-e", "^42$", "--", "answer.txt")
	m.want("commit")
	m.run(0, "commit", "--ticket", m.ticket, "--message", "Deliver the verified answer", "--include", "answer.txt")
	m.want("done")
	m.within("verify staged work and commit", 0, 0)
}

func TestTurnBudgetImplementAndCommit(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	m := &turns{t: t, repo: r, id: "budget-implement"}
	m.run(0, "open", "--entry", "implement", "--until", "deliver", "--commit", "--request", "Track the README")
	m.record("intake", "The README is tracked")
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	m.record("implement", "Added README")
	m.want("verify")
	m.run(0, "test", "--ticket", m.ticket, "--expect", "README is tracked", "--", "git", "ls-files", "--error-unmatch", "--", "README.md")
	m.want("commit")
	m.run(0, "commit", "--ticket", m.ticket, "--message", "Add README")
	m.want("done")
	m.within("implement and commit", 0, 0)
}

func TestTurnBudgetImplementRequiredReviewCommit(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	m := &turns{t: t, repo: r, id: "budget-review"}
	m.run(0, "open", "--entry", "implement", "--until", "deliver", "--commit", "--require-review", "--request", "Track the README")
	m.record("intake", "The README is tracked")
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	m.record("implement", "Added README", "--claim-review", "claude")
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

	m.run(0, "record", "review", "--ticket", ticket, "--observed-review")
	m.want("verify")
	m.run(0, "test", "--ticket", m.ticket, "--expect", "README is tracked", "--", "git", "ls-files", "--error-unmatch", "--", "README.md")
	m.want("commit")
	m.run(0, "commit", "--ticket", m.ticket, "--message", "Add README")
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
	m.run(0, "bug", "--from", source)
	m.record("intake", "The public output must be ready.")
	m.want("verify")
	m.run(5, "test", "--ticket", m.ticket, "--expect", "output is ready", "--", "git", "grep", "-q", "-e", "^ready$", "--", "output.txt")
	m.want("repair")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	m.record("repair", "Fixed the output.")
	m.run(0, "test", "--ticket", m.ticket, "--retry")
	m.want("commit")
	m.run(0, "commit", "--ticket", m.ticket, "--message", "Fix export")
	m.want("receipt")
	m.spend(0, "task", "receipt", m.id, "--json")
	// Reading the final state is the test's own check, not a step of the path.
	if final := r.Kitsu("task", "next", m.id, "--brief"); final.Code != 0 || !strings.Contains(final.Stdout, `"next":"done"`) {
		t.Fatalf("the receipt did not finish the task:\n%s", final.Output())
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
