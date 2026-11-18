package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestChangedDeclarationBlocksVerificationAndCommit(t *testing.T) {
	for _, change := range []string{"remove", "replace"} {
		t.Run(change, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "policy"}
			f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit the answer")
			f.record("intake", "host", "The answer should be 42")
			r.Write("answer.txt", "42\n")
			r.Git("add", "answer.txt")
			f.call(0, "next")
			f.want("verify")
			verify := f.state.Ticket
			assertion := []string{"test", "--ticket", verify, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt"}

			changeDeclaration := func() {
				t.Helper()
				if change == "remove" {
					r.Remove("kitsu.toml")
				} else {
					r.Write("kitsu.toml", "version = 1\n")
				}
				r.Git("add", "-A")
			}
			restoreDeclaration := func() {
				t.Helper()
				r.Write("kitsu.toml", kitsutest.DemoDeclaration)
				r.Git("add", "kitsu.toml")
			}
			changeDeclaration()
			assertPinnedPolicyBlocked(t, r, f.id)
			f.call(1, assertion...)
			restoreDeclaration()
			f.call(0, assertion...)
			f.want("commit")

			commit := f.state.Ticket
			changeDeclaration()
			assertPinnedPolicyBlocked(t, r, f.id)
			f.call(1, "commit", "--ticket", commit, "--message", "Deliver answer")
			restoreDeclaration()
			f.call(0, "commit", "--ticket", commit, "--message", "Deliver answer")
			f.want("done")
		})
	}
}

func assertPinnedPolicyBlocked(t *testing.T, r *kitsutest.Repo, id string) {
	t.Helper()
	var view struct {
		Next, Ticket string
		Verified     bool
		Policy       string `json:"repository_policy"`
		Problem      string `json:"repository_policy_error"`
	}
	result := r.Kitsu("task", "next", id, "--brief")
	opening := fmt.Sprintf("%x", sha256.Sum256([]byte(kitsutest.DemoDeclaration)))
	if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &view) != nil ||
		view.Next != "blocked" || view.Ticket != "" || view.Verified || view.Policy != opening || !strings.Contains(view.Problem, "differs from the opening policy") {
		t.Fatal(result.Output())
	}
}

func TestChangedVerifyOnlyCandidateNeedsRepairAuthority(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "verify-only"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Commit the supplied answer only if it passes")
	f.record("intake", "host", "The supplied answer should be 42")
	r.Write("answer.txt", "13\n")
	r.Git("add", "answer.txt")
	f.call(0, "next")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	f.want("verify")
	retry := f.state.Ticket
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	f.call(0, "next")
	f.want("blocked")
	if f.state.Ticket != "" || f.state.Verified || !strings.Contains(f.state.Delivery, "task extend verify-only --repair") {
		t.Fatal(f.state)
	}
	f.call(1, "test", "--ticket", retry, "--retry")
	f.call(1, "commit", "--ticket", retry, "--message", "Deliver answer without repair authority")

	r.Write("answer.txt", "13\n")
	r.Git("add", "answer.txt")
	f.call(0, "next")
	f.want("verify")
	f.call(5, "test", "--ticket", f.state.Ticket, "--retry")
	f.call(0, "extend", "--repair")
	f.want("repair")
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	f.record("repair", "host", "Corrected the answer with repair authority")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("commit")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver answer after repair")
	f.want("done")
}
