package harness_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func readReceiptMarkdown(f flow) string {
	f.t.Helper()
	out := f.repo.Kitsu("task", "receipt", f.id, "--markdown")
	if out.Code != 0 || out.Stderr != "" || !strings.HasPrefix(out.Stdout, "# Kitsu task receipt\n") {
		f.t.Fatal(out.Output())
	}
	return out.Stdout
}

func markdownContains(t *testing.T, body string, required ...string) {
	t.Helper()
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in receipt:\n%s", want, body)
		}
	}
}

func markdownOmits(t *testing.T, body string, private ...string) {
	t.Helper()
	for _, value := range private {
		if strings.Contains(body, value) {
			t.Errorf("shareable receipt exposed %q", value)
		}
	}
}

func TestMarkdownReceiptExportsOnlyTheSavedVerifiedPatch(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "share-verified-patch"}
	f.call(0, "open", "--entry", "verify", "--until", "verify", "--require-review", "--request", "private-original-request")
	f.record("intake", "private-session-id", "private-intake-note")
	r.Write("patch.txt", "a verified patch\n")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "private-expected-behavior", "--", "sh", "-c",
		"printf '%s\\n' \"$1\"; test -f patch.txt", "assertion", "private-argument-and-log")
	f.want("review")
	ledger := ".git/kitsu-stages/task-harness-" + f.id + ".json"
	before := r.Read(ledger)
	index, status := r.Read(".git/index"), r.Git("--no-optional-locks", "status", "--porcelain")
	logs := len(r.RecordFiles("task-" + f.id))
	body := readReceiptMarkdown(f)
	policy := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Read("kitsu.toml"))))
	markdownContains(t, body, "Verified patch (saved snapshot)", "Required review pending", "| `unit` | Passed |",
		"Pinned SHA-256 `"+policy+"`")
	markdownOmits(t, body, "private-original-request", "private-session-id", "private-intake-note", "private-expected-behavior",
		"private-argument-and-log", f.state.Proof.Run, r.Path(""), ".git/", "Verified commit")
	if r.Read(ledger) != before || r.Read(".git/index") != index || r.Git("--no-optional-locks", "status", "--porcelain") != status || len(r.RecordFiles("task-"+f.id)) != logs {
		t.Fatal("receipt export changed Git, the ledger or check evidence")
	}
	r.Write("patch.txt", "unrelated later edit\n")
	if again := readReceiptMarkdown(f); again != body {
		t.Fatal("later edits changed the saved verification receipt")
	}
}

func TestMarkdownReceiptDistinguishesVerificationOrderAndLegacyCommit(t *testing.T) {
	t.Run("verified at existing commit", func(t *testing.T) {
		r := kitsutest.NewRepo(t)
		r.Write("answer.txt", "42\n")
		r.Git("add", "answer.txt")
		r.Git("commit", "-qm", "existing answer")
		f := flow{t: t, repo: r, id: "existing-verified-receipt"}
		f.call(0, "open", "--entry", "verify", "--until", "verify", "--request", "Check the answer")
		f.record("intake", "host", "Inspect the supplied commit")
		f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "-q", "42", "--", "answer.txt")
		f.want("done")
		markdownContains(t, readReceiptMarkdown(f), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`",
			"already checked out when verification ran", "Undeclared on the verified tree")
	})
	t.Run("verified patch committed later", func(t *testing.T) {
		f := prepareHookCommit(t)
		r := f.repo
		f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver verified change")
		f.want("done")
		commit, tree := r.Git("rev-parse", "HEAD"), r.Git("rev-parse", "HEAD^{tree}")
		r.Git("commit", "--allow-empty", "-qm", "Unrelated later commit")
		current := r.Git("rev-parse", "HEAD")
		body := readReceiptMarkdown(f)
		markdownContains(t, body, "Verified commit `"+commit+"`", "Verification preceded the observed commit")
		markdownOmits(t, body, current)
		r.Remove(".git/kitsu/logs/" + f.state.Proof.Run + ".log")
		markdownContains(t, readReceiptMarkdown(f), "Passed (recorded, matching local log unavailable)")
		rewriteDeliveryLedger(f, func(task *harness.Task) {
			task.Version, task.Completion, task.CommitAttempt, task.Proof = 1, nil, nil, nil
			task.Latest("commit").ObservedCommit = ""
		})
		body = readReceiptMarkdown(f)
		markdownContains(t, body, "Verified tree `"+tree+"`", "no exact commit ID", "Saved run details unavailable")
		markdownOmits(t, body, commit, current)
	})
}

func TestMarkdownReceiptRequiresSavedSuccessfulVerification(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "failed-receipt"}
	f.call(0, "open", "--entry", "verify", "--until", "verify", "--request", "Check behavior")
	f.record("intake", "host", "Reproduce the failure")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "expected behavior", "--", "sh", "-c", "exit 1")
	out := r.Kitsu("task", "receipt", f.id, "--markdown")
	if out.Code != 1 || out.Stdout != "" || !strings.Contains(out.Stderr, "no completed verification evidence") {
		t.Fatal(out.Output())
	}
	for _, other := range []string{"--json", "--brief"} {
		out = r.Kitsu("task", "receipt", f.id, "--markdown", other)
		if out.Code != 2 || out.Stdout != "" {
			t.Fatal(out.Output())
		}
	}
	help := r.Kitsu("task", "receipt", "--help")
	if help.Code != 0 || !strings.Contains(help.Stdout, "--markdown") {
		t.Fatal(help.Output())
	}
}

func TestMarkdownReceiptRejectsFailedRepositoryCheck(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("unit.sh", "#!/bin/sh\nexit 1\n")
	f := flow{t: t, repo: r, id: "failed-required-check-receipt"}
	f.call(0, "open", "--entry", "verify", "--until", "verify", "--request", "Verify the candidate")
	f.record("intake", "host", "Check the public behavior and the declared check")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "passing behavior", "--", "sh", "-c", "exit 0")
	if f.state.Proof == nil || f.state.Proof.Outcome != "passed" || f.state.Verified {
		t.Fatalf("expected a passing behavior assertion and failed repository verification: %+v", f.state)
	}
	out := r.Kitsu("task", "receipt", f.id, "--markdown")
	if out.Code != 1 || out.Stdout != "" || !strings.Contains(out.Stderr, "no completed verification evidence") {
		t.Fatal(out.Output())
	}
}
