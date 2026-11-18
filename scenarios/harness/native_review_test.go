package harness_test

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type nativeReport struct {
	Task      string   `json:"task"`
	ReviewID  string   `json:"review_id"`
	Candidate string   `json:"candidate"`
	Decision  string   `json:"decision"`
	Note      string   `json:"note"`
	Findings  []string `json:"findings"`
}

func reservedNativeReview(t *testing.T, repo *kitsutest.Repo, id, ticket string) nativeReport {
	t.Helper()
	got := repo.Kitsu("task", "show", id, "--review-ticket", ticket)
	var input struct {
		Report nativeReport `json:"native_review_result"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &input); got.Code != 0 || err != nil || input.Report.ReviewID == "" {
		t.Fatal(got.Output(), err)
	}
	return input.Report
}

func reviewMessage(t *testing.T, report nativeReport) string {
	t.Helper()
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func finishNativeFixture(t *testing.T, repo *kitsutest.Repo, id, ticket, host, parent, worker, note string, findings ...string) {
	t.Helper()
	report := reservedNativeReview(t, repo, id, ticket)
	report.Decision, report.Note = "approve", note
	if len(findings) > 0 {
		report.Decision, report.Findings = "findings", findings
	}
	sendNativeReviewEvent(t, repo, host, "SubagentStop", parent, worker, reviewMessage(t, report))
}

func sendNativeReviewEvent(t *testing.T, repo *kitsutest.Repo, host, event, parent, worker, message string) {
	t.Helper()
	if got := nativeReviewEvent(t, repo, host, event, parent, worker, message); got.Code != 0 {
		t.Fatal(got.Output())
	}
}

func nativeReviewEvent(t *testing.T, repo *kitsutest.Repo, host, event, parent, worker, message string) kitsutest.Result {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"hook_event_name": event, "session_id": parent, "agent_id": worker, "cwd": repo.Dir,
		"last_assistant_message": message,
	})
	if err != nil {
		t.Fatal(err)
	}
	program := "kitsu"
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	return repo.Run(string(payload), filepath.Join(kitsutest.Binary(t), program), "tokens", "record", "--host", host)
}

func TestRequiredReviewNeedsNativeWorkerOnCurrentCandidate(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			r.Git("commit", "--allow-empty", "-qm", "Base")
			f := flow{t: t, repo: r, id: "native-review"}
			f.call(0, "open", "--request", "Track the README", "--entry", "implement", "--until", "deliver", "--commit", "--require-review")
			f.record("intake", "coordinator", "The README is tracked")
			r.Write("README.md", "# Project\n")
			r.Git("add", "README.md")
			f.record("implement", "coordinator", "Added README")
			f.want("review")
			ticket := f.state.Ticket
			f.call(0, "claim", "--ticket", ticket, "--actor", "coordinator", "--host", host)
			report := reservedNativeReview(t, r, f.id, ticket)
			report.Decision, report.Note = "approve", "Verified the staged documentation."
			f.call(1, "record", "review", "--ticket", ticket, "--actor", "reported-worker", "--note", report.Note)
			consume := []string{"record", "review", "--ticket", ticket, "--observed-review"}

			sendNativeReviewEvent(t, r, host, "SubagentStop", "coordinator", "worker", reviewMessage(t, report))
			f.call(1, consume...)
			sendNativeReviewEvent(t, r, host, "SubagentStart", "coordinator", "coordinator", "")
			sendNativeReviewEvent(t, r, host, "SubagentStop", "coordinator", "coordinator", reviewMessage(t, report))
			f.call(1, consume...)
			sendNativeReviewEvent(t, r, host, "SubagentStart", "coordinator", "worker", "")
			wrong := report
			wrong.Candidate = "stale"
			sendNativeReviewEvent(t, r, host, "SubagentStop", "coordinator", "worker", reviewMessage(t, wrong))
			sendNativeReviewEvent(t, r, host, "SubagentStop", "different-coordinator", "worker", reviewMessage(t, report))
			f.call(1, consume...)

			sendNativeReviewEvent(t, r, host, "SubagentStop", "coordinator", "worker", reviewMessage(t, report))
			f.call(1, "record", "review", "--ticket", ticket, "--actor", "worker", "--note", "Changed the worker's response")
			f.call(0, consume...)
			f.call(0, consume...) // A lost response can recover the task without a second review.
			f.want("verify")
			history := r.Kitsu("task", "show", f.id, "--json")
			var ledger struct {
				Events []struct {
					Native *struct{ Host string } `json:"native_review"`
				}
			}
			if err := json.Unmarshal([]byte(history.Stdout), &ledger); history.Code != 0 || err != nil {
				t.Fatal(history.Output(), err)
			}
			count := 0
			for _, event := range ledger.Events {
				if event.Native != nil && event.Native.Host == host {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("saved %d native reviews. Want one", count)
			}
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "README is tracked", "--", "git", "ls-files", "--error-unmatch", "--", "README.md")
			f.want("commit")
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Add README")
			f.want("done")
			receipt := readReceiptMarkdown(f)
			provider := map[string]string{"codex": "Codex", "claude": "Claude Code"}[host]
			markdownContains(t, receipt, "Host-observed review ("+provider+")", "Undeclared on the verified tree, as pinned at intake")
			markdownOmits(t, receipt, report.Note, "coordinator", "worker")
		})
	}
}
