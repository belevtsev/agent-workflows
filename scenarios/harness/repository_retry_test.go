package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type repositoryCheckLog struct {
	Run     string `json:"run_id"`
	Exit    int    `json:"exit_status"`
	Path    string `json:"path"`
	Digest  string `json:"log_sha256"`
	Check   string `json:"check_id"`
	Outcome string `json:"outcome"`
}

func TestResumeAndReplayRepositoryCheckRetry(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("lint.sh", "if test -f .git/lint-unavailable; then echo temporary-check-failure; exit 23; fi\necho recovered\n")
	r.Write(".git/lint-unavailable", "1")
	f := flow{t: t, repo: r, id: "verify-gate"}
	f.call(0, "open", "--request", "Verify the app output", "--until", "verify", "--repair")
	f.record("intake", "host", "The application should contain ok")
	result := r.Kitsu("task", "test", f.id, "--json", "--ticket", f.state.Ticket,
		"--expect", "app contains ok", "--", "git", "grep", "ok", "--", "src/app.txt")
	failure := readRepositoryCheckFailure(t, result, 5)
	f.call(0, "next")
	f.want("repair")
	run := f.state.Proof.Run
	ticket := f.state.Ticket
	if failure.Run == run {
		t.Fatal("repository failure points to the passing assertion")
	}
	t.Run("recover failing command output", func(t *testing.T) {
		for _, flag := range []string{"--brief", "--json"} {
			result := r.Kitsu("task", "next", f.id, flag)
			if got := readRepositoryCheckFailure(t, result, 0); got != failure {
				t.Fatalf("%s returned a different repository failure: %+v", flag, got)
			}
		}
	})
	r.Remove(".git/lint-unavailable")
	retry := []string{"test", "--ticket", ticket, "--retry"}
	f.call(0, retry...)
	f.want("done")
	if f.state.Proof.Run != run {
		t.Fatal("re-executed an unchanged passing assertion")
	}
	t.Run("lost successful response", func(t *testing.T) {
		record := ".git/kitsu-stages/task-harness-" + f.id + ".json"
		completed := r.Read(record)
		result := r.Kitsu("task", "test", f.id, "--brief", "--ticket", ticket, "--retry")
		if result.Code != 0 || !strings.Contains(result.Stdout, `"next":"done"`) {
			t.Fatal("successful retry could not be replayed", result.Output())
		}
		if len(r.RecordFiles("lint")) != 2 || len(r.RecordFiles("unit")) != 1 || r.Read(record) != completed {
			t.Fatal("replay changed the completed observation or executed a check")
		}
	})
	checkRepositoryFailureHistory(t, &f, failure)
}

func TestCompletedRepositoryRetryTicketExpiresWithEvidence(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "current ledger"
		if legacy {
			name = "earlier ledger"
		}
		t.Run(name, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Write("lint.sh", "test ! -f .git/lint-unavailable || exit 23\n")
			r.Write(".git/lint-unavailable", "1")
			f := flow{t: t, repo: r, id: "verify-again"}
			f.call(0, "open", "--request", "Verify the app output", "--until", "verify")
			f.record("intake", "host", "The application should contain ok")
			f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "app remains ok", "--", "git", "grep", "ok", "--", "src/app.txt")
			f.want("verify")
			retryTicket := f.state.Ticket
			run := f.state.Proof.Run
			r.Remove(".git/lint-unavailable")
			f.call(0, "test", "--ticket", retryTicket, "--retry")
			f.want("done")
			if legacy {
				record := r.Path(".git/kitsu-stages/task-harness-" + f.id + ".json")
				r.EditRecord(record, func(fields map[string]any) { delete(fields, "generation") })
			}
			r.Write(".git/lint-unavailable", "1")
			if result := r.Kitsu("run", "lint", "--force"); result.Code != 5 {
				t.Fatal("expected to supersede the passing gate receipt", result.Output())
			}
			r.Remove(".git/lint-unavailable")
			f.call(0, "next")
			f.want("verify")
			if f.state.Verified || f.state.Ticket == retryTicket {
				t.Fatal("stale verification reused the completed retry ticket", f.state)
			}
			f.call(1, "test", "--ticket", retryTicket, "--retry")
			if len(r.RecordFiles("lint")) != 3 {
				t.Fatal("a completed ticket reran a stale repository check")
			}
			f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
			f.want("done")
			if f.state.Proof.Run != run || len(r.RecordFiles("lint")) != 4 || len(r.RecordFiles("unit")) != 1 {
				t.Fatal("fresh retry should execute only the invalidated repository check", f.state)
			}
		})
	}
}

func TestRepositoryCheckFailureSurvivesSourceRepair(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("lint.sh", "echo temporary-check-failure\nexit 23\n")
	f := flow{t: t, repo: r, id: "repair-gate"}
	f.call(0, "open", "--require-review", "--request", "Verify behavior and repair lint", "--until", "verify", "--repair")
	f.record("intake", "host", "The application should contain ok and pass lint")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "app contains ok", "--", "git", "grep", "ok", "--", "src/app.txt")
	f.want("repair")
	run := f.state.Proof.Run
	failure := readRepositoryCheckFailure(t, r.Kitsu("task", "next", f.id, "--brief"), 0)
	r.Write("lint.sh", "echo lint recovered\n")
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	f.record("repair", "host", "Correct the failing lint script")
	f.review("independent-lint-reviewer", "Checked that the corrected script runs")
	f.want("verify")
	if got := readRepositoryCheckFailure(t, r.Kitsu("task", "next", f.id, "--brief"), 0); got != failure {
		t.Fatal("changed candidate lost the failure that led to repair", got)
	}
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	if !f.state.Verified || f.state.Proof.Run == run {
		t.Fatal("source repair needs fresh behavioral evidence", f.state)
	}
	checkRepositoryFailureHistory(t, &f, failure)
	if got := readRepositoryCheckFailureLog(t, failure); !strings.Contains(got, "temporary-check-failure") {
		t.Fatal("repair replaced the failing run's diagnostic", got)
	}
}

func readRepositoryCheckFailure(t *testing.T, result kitsutest.Result, code int) repositoryCheckLog {
	t.Helper()
	var receipt struct {
		Log repositoryCheckLog `json:"verification_log"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &receipt); result.Code != code || err != nil {
		t.Fatal("missing verification result", result.Output(), err)
	}
	log := receipt.Log
	if log.Run == "" || log.Check != "lint" || log.Outcome != "failed" || log.Exit != 23 {
		t.Fatal("missing repository failure", result.Output())
	}
	if data := readRepositoryCheckFailureLog(t, log); !strings.Contains(data, "temporary-check-failure") {
		t.Fatal("missing failure diagnostic", data)
	}
	return log
}

func readRepositoryCheckFailureLog(t *testing.T, log repositoryCheckLog) string {
	t.Helper()
	data, err := os.ReadFile(log.Path)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != log.Digest {
		t.Fatal("failed check log does not match its receipt", log, err)
	}
	return string(data)
}

func checkRepositoryFailureHistory(t *testing.T, f *flow, want repositoryCheckLog) {
	t.Helper()
	shown := f.repo.Kitsu("task", "show", f.id, "--json")
	var history struct {
		Events []struct {
			Failures []repositoryCheckLog `json:"prior_repository_failures"`
		}
	}
	if err := json.Unmarshal([]byte(shown.Stdout), &history); shown.Code != 0 || err != nil {
		t.Fatal(shown.Output())
	}
	var failures []repositoryCheckLog
	for _, event := range history.Events {
		failures = append(failures, event.Failures...)
	}
	want.Path = ""
	if !slices.Equal(failures, []repositoryCheckLog{want}) {
		t.Fatal("task history did not retain the failed check receipt", failures)
	}
}
