package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

const ciPR = "https://github.com/example/project/pull/17"

const ciDeclaration = `version = 1
require_review = true

[[check]]
id = "product"
run = ["git", "grep", "-q", "^ready$", "--", "output.txt"]

[[check]]
id = "tracked"
run = ["git", "ls-files", "--error-unmatch", "output.txt"]
`

func ciRepo(t *testing.T, content string) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Write("output.txt", content+"\n")
	r.Write("kitsu.toml", ciDeclaration)
	r.Git("add", ".")
	r.Git("commit", "-qm", "PR head")
	return r
}

func ciFixtureTrustPath(t *testing.T, r *kitsutest.Repo) string {
	t.Helper()
	configDir := r.Path(".git/test-config")
	r.Setenv("XDG_CONFIG_HOME", configDir)
	r.Setenv("APPDATA", configDir)
	if runtime.GOOS == "darwin" {
		configDir = filepath.Join(r.Home, "Library", "Application Support")
	}
	common, err := filepath.EvalSymlinks(r.Path(".git"))
	if err != nil {
		t.Fatal(err)
	}
	approval := filepath.Join(configDir, "kitsu", "check-trust", fmt.Sprintf("%x", sha256.Sum256([]byte(common))))
	if err := os.MkdirAll(filepath.Dir(approval), 0o700); err != nil {
		t.Fatal(err)
	}
	return approval
}

func approveCIRepo(t *testing.T, r *kitsutest.Repo) string {
	t.Helper()
	approval := ciFixtureTrustPath(t, r)
	if err := os.WriteFile(approval, []byte(fmt.Sprintf("%x\n", sha256.Sum256([]byte(r.Read("kitsu.toml"))))), 0o600); err != nil {
		t.Fatal(err)
	}
	return approval
}

func writeLocalCIApproval(t *testing.T, r *kitsutest.Repo, argv ...string) string {
	t.Helper()
	data, err := decl.Append([]byte("version = 1\n"), "product", argv, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	approval := ciFixtureTrustPath(t, r) + fmt.Sprintf(".ci-%x.toml", sha256.Sum256([]byte("product")))
	if err := os.WriteFile(approval, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func ciMetadata(head string) string {
	return fmt.Sprintf(`{"url":%q,"state":"OPEN","headRefOid":%q,"statusCheckRollup":[{"__typename":"CheckRun","name":"CI / unit","status":"COMPLETED","conclusion":"FAILURE"}]}`, ciPR, head)
}

func importCI(t *testing.T, r *kitsutest.Repo, head string) kitsutest.Result {
	t.Helper()
	return r.Run(ciMetadata(head), filepath.Join(kitsutest.Binary(t), "kitsu"), "task", "ci", "ci-repair", "--from", ciPR, "--check", "product", "--stdin", "--brief")
}

func TestFailedPRCheckReadsMetadataOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable fixture for gh")
	}
	r := ciRepo(t, "broken")
	approveCIRepo(t, r)
	response := fmt.Sprintf(`{"url":%q,"state":"OPEN","headRefOid":%q,"statusCheckRollup":[{"__typename":"StatusContext","context":"CI / unit","state":"FAILURE"}]}`, ciPR, r.Git("rev-parse", "HEAD"))
	r.Write(".git/ci-metadata.json", response)
	r.Write(".git/bin/gh", `#!/bin/sh
set -eu
test "$#" -eq 7
test "$1 $2 $3 $4 $5 $6" = "pr view https://github.com/example/project/pull/17 --json url,state,headRefOid,statusCheckRollup --jq"
test -n "$7"
test ! -e .git/gh-called
: > .git/gh-called
cat .git/ci-metadata.json
`)
	if err := os.Chmod(r.Path(".git/bin/gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.Setenv("PATH", r.Path(".git/bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "ci", "--from", ciPR, "--check", "product")
	f.want("repair")
	if !r.Exists(".git/gh-called") {
		t.Fatal("gh was not called for initial intake")
	}
	red := f.state.Proof.Run
	f.call(0, "ci", "--from", ciPR, "--check", "product")
	if f.state.Proof.Run != red {
		t.Fatal("reopening did not reuse the original failure")
	}
}

func TestFailedPRCheckReachesVerifiedCommit(t *testing.T) {
	r := ciRepo(t, "broken")
	head := r.Git("rev-parse", "HEAD")
	approval := approveCIRepo(t, r)
	first := importCI(t, r, head)
	var initial struct {
		Next string
		Log  *struct {
			harness.RunResult
			Path string
		} `json:"verification_log"`
	}
	if err := json.Unmarshal([]byte(first.Stdout), &initial); err != nil || first.Code != 0 || initial.Next != "repair" || initial.Log == nil {
		t.Fatalf("CI intake did not produce a local failure and a repair action: %s (%v)", first.Output(), err)
	}
	if log := initial.Log; log.CheckID != "product" || log.Outcome != "failed" || log.ExitStatus == nil || *log.ExitStatus != 1 || log.Path == "" {
		t.Fatalf("missing local failure evidence for repair: %+v", log)
	}
	resumed := r.Kitsu("task", "next", "ci-repair", "--brief")
	var handoff struct {
		Next, Ticket string
		PullRequest  struct {
			URL, Check string
			LocalCheck string `json:"local_check"`
			Reproduced bool
		} `json:"pull_request"`
		VerificationLog struct{ Path string } `json:"verification_log"`
	}
	if err := json.Unmarshal([]byte(resumed.Stdout), &handoff); err != nil || resumed.Code != 0 || handoff.Next != "repair" || handoff.Ticket == "" ||
		handoff.PullRequest.URL != ciPR || handoff.PullRequest.Check != "CI / unit" || handoff.PullRequest.LocalCheck != "product" ||
		!handoff.PullRequest.Reproduced || handoff.VerificationLog.Path != initial.Log.Path {
		t.Fatalf("brief resume omitted the saved PR and failed check: %s (%v)", resumed.Output(), err)
	}
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "ci", "--from", ciPR, "--check", "product", "--stdin") // Reopen without metadata.
	f.want("repair")
	red := f.state.Proof.Run
	f.call(0, "next")
	if f.state.Proof.Run != red {
		t.Fatal("reopening executed the check again")
	}
	f.call(1, "ci", "--from", ciPR, "--check", "tracked", "--stdin")
	f.call(1, "ci", "--from", ciPR, "--check", "product", "--job", "different", "--stdin")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	f.record("repair", "developer", "Restore ready output.")
	f.want("review")
	f.call(5, "commit", "--ticket", f.state.Ticket, "--message", "Restore ready output")
	f.want("review")
	f.review("reviewer", "Confirmed ready output.")
	f.want("verify")
	f.call(1, "test", "--ticket", f.state.Ticket, "--expect", "tracked passes", "--replace-reason", "different command",
		"--", "git", "ls-files", "--error-unmatch", "output.txt")
	if err := os.Remove(approval); err != nil {
		t.Fatal(err)
	}
	finish := []string{"commit", "--ticket", f.state.Ticket, "--message", "Restore ready output"}
	f.call(1, finish...)
	if current := r.Git("rev-parse", "HEAD"); current != head {
		t.Fatalf("unapproved verification committed %s", current)
	}
	approveCIRepo(t, r)
	f.call(0, finish...)
	f.want("done")
	result := r.Kitsu("task", "receipt", f.id, "--json")
	var receipt harness.DeliveryReceipt
	if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil || result.Code != 0 {
		t.Fatal(result.Output(), err)
	}
	check := receipt.Source.Check
	committed := r.Git("rev-parse", "HEAD")
	if receipt.Source.URL != ciPR || check == nil || check.Head != head || check.Name != "CI / unit" || check.LocalCheck != "product" ||
		receipt.Reproduction.ID != red || receipt.Commit != committed || receipt.Reproduction.Head != head ||
		receipt.RepositoryChecks != "passed" || len(receipt.RepositoryRuns) != 2 || receipt.OnCommit.ID == receipt.BeforeCommit.ID {
		t.Fatalf("receipt lost source or verification evidence: %+v", receipt)
	}
	markdownContains(t, readReceiptMarkdown(f), "<"+ciPR+">", head, committed, "CI / unit", "Behavior on the delivered commit · Passed")
	f.call(0, finish...)
	again := r.Kitsu("task", "receipt", f.id, "--json")
	if r.Git("rev-parse", "HEAD") != committed || again.Stdout != result.Stdout {
		t.Fatal("replaying a completed delivery changed the commit or receipt")
	}
}

func TestFailedPRCheckCommitStopsAtFailedGate(t *testing.T) {
	r := ciRepo(t, "broken")
	r.Write("kitsu.toml", strings.ReplaceAll(strings.ReplaceAll(ciDeclaration, "require_review = true\n", ""), `"--error-unmatch", "output.txt"`, `"--error-unmatch", "required.txt"`))
	r.Git("add", "kitsu.toml")
	r.Git("commit", "-qm", "Declare a second required check")
	approveCIRepo(t, r)
	head := r.Git("rev-parse", "HEAD")
	if result := importCI(t, r, head); result.Code != 0 {
		t.Fatal(result.Output())
	}
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "next")
	r.Write("output.txt", "ready\n")
	f.record("repair", "developer", "Restore ready output.")
	f.want("verify")
	f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Unstaged repair")
	if r.Git("rev-parse", "HEAD") != head {
		t.Fatal("unstaged repair advanced the branch")
	}
	r.Git("add", "output.txt")
	f.call(0, "next")
	failed := r.Kitsu("task", "commit", f.id, "--ticket", f.state.Ticket, "--message", "Restore ready output", "--brief")
	if failed.Code != 5 || !strings.Contains(failed.Stdout, `"check_id":"tracked"`) || r.Git("rev-parse", "HEAD") != head {
		t.Fatalf("required check did not stop delivery: %s", failed.Output())
	}
	f.call(1, "receipt")
}

func TestFailedPRCheckKeepsTheOpeningPRHead(t *testing.T) {
	r := ciRepo(t, "broken")
	approveCIRepo(t, r)
	head := r.Git("rev-parse", "HEAD")
	if result := importCI(t, r, head); result.Code != 0 {
		t.Fatal(result.Output())
	}
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "next")
	f.want("repair")
	ticket, red := f.state.Ticket, f.state.Proof.Run
	r.Git("commit", "--allow-empty", "-qm", "Advance the same branch")
	moved := r.Git("rev-parse", "HEAD")
	next := r.Kitsu("task", "next", f.id, "--brief")
	if next.Code != 0 || !strings.Contains(next.Stdout, `"next":"blocked"`) ||
		!strings.Contains(next.Stdout, "original PR head") || strings.Contains(next.Stdout, `"ticket":`) {
		t.Fatalf("the changed head still had repair authority: %s", next.Output())
	}
	f.call(1, "record", "repair", "--ticket", ticket, "--actor", "developer", "--note", "Use the updated branch.")
	f.call(5, "commit", "--ticket", ticket, "--message", "Deliver on the updated branch")
	f.call(1, "receipt")
	if r.Git("rev-parse", "HEAD") != moved {
		t.Fatal("blocked delivery moved the branch")
	}
	r.Git("update-ref", "HEAD", head, moved)
	f.call(0, "next")
	f.want("repair")
	if f.state.Proof.Run != red {
		t.Fatal("restoring the head lost the recorded failure")
	}
}

func TestFailedPRCheckFromPrivateLocalApproval(t *testing.T) {
	for _, withPolicy := range []bool{false, true} {
		t.Run(fmt.Sprintf("repository-policy=%t", withPolicy), func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			r.Write("output.txt", "broken\n")
			if withPolicy {
				r.Write("kitsu.toml", `version = 1
require_review = true

[[check]]
id = "tracked"
run = ["git", "ls-files", "--error-unmatch", "output.txt"]
`)
			}
			r.Git("add", ".")
			r.Git("commit", "-qm", "PR head")
			head := r.Git("rev-parse", "HEAD")
			if first := importCI(t, r, head); first.Code == 0 || !strings.Contains(first.Output(), "kitsu check trust") {
				t.Fatalf("CI repair started without approval: %s", first.Output())
			}
			command := []string{"git", "grep", "-q", "^ready$", "--", "output.txt"}
			approval := writeLocalCIApproval(t, r, command...)
			if withPolicy {
				if first := importCI(t, r, head); first.Code == 0 || !strings.Contains(first.Output(), "local declaration") {
					t.Fatalf("private approval bypassed the repository suite: %s", first.Output())
				}
				approveCIRepo(t, r)
			}
			if result := importCI(t, r, head); result.Code != 0 {
				t.Fatal(result.Output())
			}
			f := flow{t: t, repo: r, id: "ci-repair"}
			f.call(0, "next")
			r.Write("output.txt", "ready\n")
			r.Git("add", "output.txt")
			f.record("repair", "developer", "Restore ready output.")
			if withPolicy {
				f.want("review")
				f.call(5, "commit", "--ticket", f.state.Ticket, "--message", "Restore ready output")
				f.review("reviewer", "Confirmed ready output.")
			}
			f.want("verify")
			ticket := f.state.Ticket
			f.call(0, "test", "--ticket", ticket, "--retry")
			f.want("commit")
			finish := []string{"commit", "--ticket", f.state.Ticket, "--message", "Restore ready output"}
			writeLocalCIApproval(t, r, "git", "status", "--short")
			f.call(1, finish...)
			if r.Git("rev-parse", "HEAD") != head {
				t.Fatal("changed approval committed previously verified content")
			}
			writeLocalCIApproval(t, r, command...)
			f.call(0, finish...)
			f.want("done")
			result := r.Kitsu("task", "receipt", f.id, "--json")
			var receipt harness.DeliveryReceipt
			if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil || result.Code != 0 {
				t.Fatal(result.Output(), err)
			}
			if receipt.Source.Check == nil || receipt.Source.Check.ApprovalSHA256 != approval || receipt.Reproduction.Head != head {
				t.Fatalf("receipt lost the approved check or opening candidate: %+v", receipt)
			}
			if withPolicy && (receipt.RepositoryChecks != "passed" || receipt.RepositoryRuns["tracked"].ID == "") {
				t.Fatal("the repository check is missing from the receipt")
			}
			if !withPolicy && (receipt.RepositoryChecks != "undeclared" || r.Exists("kitsu.toml")) {
				t.Fatal("private approval changed repository policy")
			}
		})
	}
}

func TestFailedPRCheckResumesAfterReceiptCheckFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX post-commit hook")
	}
	r := ciRepo(t, "broken")
	r.Write("kitsu.toml", strings.ReplaceAll(strings.ReplaceAll(ciDeclaration, "require_review = true\n", ""), `"grep", "-q", "^ready$", "--", "output.txt"`, `"-c", "alias.verify-output=!git grep -q ^ready$ -- output.txt && test ! -e .git/fail-receipt", "verify-output"`))
	r.Git("add", "kitsu.toml")
	r.Git("commit", "-qm", "Declare a check with a local test marker")
	approveCIRepo(t, r)
	if result := importCI(t, r, r.Git("rev-parse", "HEAD")); result.Code != 0 {
		t.Fatal(result.Output())
	}
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "next")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	f.record("repair", "developer", "Restore ready output.")
	f.want("verify")
	finish := []string{"commit", "--ticket", f.state.Ticket, "--message", "Restore ready output"}
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry") // Verification finished before the commit request was retried.
	r.Git("config", "core.hooksPath", r.Path(".git/local-hooks"))
	writeCommitHook(t, r, "post-commit", ": > .git/fail-receipt\n")
	failed := r.Kitsu(append([]string{"task", "commit", f.id, "--json"}, finish[1:]...)...)
	var pending state
	if err := json.Unmarshal([]byte(failed.Stdout), &pending); err != nil || failed.Code != 1 || pending.Next != "receipt" || pending.Verified ||
		!strings.Contains(failed.Stderr, "verification on commit") {
		t.Fatalf("expected a pending receipt and its failure diagnostic: %s (%v)", failed.Output(), err)
	}
	committed := r.Git("rev-parse", "HEAD")
	f.call(0, "next")
	f.want("receipt")
	if r.Git("log", "-1", "--format=%s") != "Restore ready output" {
		t.Fatal("receipt failure lost the delivered commit")
	}
	if err := os.Remove(r.Path(".git/fail-receipt")); err != nil {
		t.Fatal(err)
	}
	f.call(0, finish...)
	f.want("done")
	if r.Git("rev-parse", "HEAD") != committed {
		t.Fatal("receipt retry created another commit")
	}
}

func TestFailedPRCheckRejectsUnapprovedOrStaleInput(t *testing.T) {
	r := ciRepo(t, "broken")
	head := r.Git("rev-parse", "HEAD")
	record := ".git/kitsu-stages/task-harness-ci-repair.json"
	unapproved := importCI(t, r, head)
	if unapproved.Code == 0 || !strings.Contains(unapproved.Output(), "kitsu check trust") || r.Exists(record) {
		t.Fatal(unapproved.Output())
	}
	approveCIRepo(t, r)
	stale := importCI(t, r, strings.Repeat("a", 40))
	if stale.Code == 0 || !strings.Contains(stale.Output(), "check it out") || r.Exists(record) {
		t.Fatal(stale.Output())
	}
	r.Write("output.txt", "unrelated edit\n")
	dirty := importCI(t, r, head)
	if dirty.Code == 0 || !strings.Contains(dirty.Output(), "clean worktree") || r.Exists(record) {
		t.Fatal(dirty.Output())
	}
}

func TestFailedPRCheckWaitsForLocalReproduction(t *testing.T) {
	r := ciRepo(t, "ready")
	approveCIRepo(t, r)
	result := importCI(t, r, r.Git("rev-parse", "HEAD"))
	if result.Code != 5 || !strings.Contains(result.Stdout, "not reproduced") || !strings.Contains(result.Stdout, `"reproduced":false`) {
		t.Fatal(result.Output())
	}
	resumed := r.Kitsu("task", "next", "ci-repair", "--brief")
	if resumed.Code != 0 || !strings.Contains(resumed.Stdout, `"reproduced":false`) || !strings.Contains(resumed.Stdout, `"verification_log":`) {
		t.Fatalf("resume omitted the passing check that left reproduction pending: %s", resumed.Output())
	}
	f := flow{t: t, repo: r, id: "ci-repair"}
	f.call(0, "next")
	f.want("verify")
	first := f.state.Proof.Run
	f.call(5, "ci", "--from", ciPR, "--check", "product", "--stdin")
	if f.state.Proof.Run != first {
		t.Fatal("reopening repeated an inconclusive check")
	}
	r.Write("output.txt", "broken\n")
	f.call(0, "next")
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	f.call(1, "receipt")
}
