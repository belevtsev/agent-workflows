package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Git normalizes both fixtures to the same blob, but the behavior under test
// depends on their actual bytes. A later check can restore the original bytes.
func TestVerificationRejectsIntermediateRawChanges(t *testing.T) {
	for _, entry := range []string{"verify", "implement"} {
		t.Run(entry, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Write(".gitattributes", "fixture text eol=lf\n")
			r.Write("fixture", "x\n")
			r.Write("unit.sh", "printf 'x\\n' > fixture\n")
			r.Write("check.sh", "if test -f .git/prepare; then printf 'x\\r\\n' > fixture; fi\ntest \"$(wc -c < fixture | tr -d ' ')\" = 3\n")
			r.Git("add", ".")
			r.Git("commit", "-qm", "Pin the LF fixture")
			if got := r.Run("", "sh", "check.sh"); got.Code != 1 {
				t.Fatal("the original fixture should fail the behavior check", got.Output())
			}
			f := flow{t: t, repo: r, id: "fixture-bytes"}
			f.call(0, "open", "--request", "Verify the reader gets the required CRLF bytes", "--entry", entry, "--until", entry)
			f.record("intake", "host", "Exercise the raw-byte reader")
			if entry == "implement" {
				f.record("implement", "host", "Run the candidate regression")
			}
			f.want("verify")
			r.Write(".git/prepare", "yes")
			trial := func() kitsutest.Result {
				return r.Kitsu("task", "test", f.id, "--ticket", f.state.Ticket, "--brief", "--expect", "the fixture contains CRLF", "--", "sh", "check.sh")
			}
			if got := trial(); got.Code != 5 || !strings.Contains(got.Stdout, "source_changed") {
				t.Fatalf("certified the LF source using an assertion run against different bytes: %s", got.Output())
			}
			// Restore the original source; an interrupted or unsuccessful attempt
			// must never leave a reusable pass on those bytes.
			r.Write("fixture", "x\n")
			r.Remove(".git/prepare")
			f.call(0, "next")
			f.want("verify")
			if got := trial(); got.Code != 5 || !strings.Contains(got.Stdout, "failed") {
				t.Fatal("recovery must execute the failing assertion", got.Code, got.Output())
			}
		})
	}
}

func TestDeclaredRunRejectsChangedRawInputs(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write(".gitattributes", "fixture text eol=lf\n")
	r.Write("fixture", "x\n")
	r.Write("unit.sh", "if test -f .git/prepare; then printf 'x\\r\\n' > fixture; fi\n")
	r.Write("lint.sh", "printf 'x\\n' > fixture\n")
	f := flow{t: t, repo: r, id: "gate-inputs"}
	f.call(0, "open", "--request", "Run all required checks on the candidate", "--until", "verify")
	f.record("intake", "host", "Run the public assertion and both declared checks")
	r.Write(".git/prepare", "yes")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "app is ok", "--", "git", "grep", "ok", "--", "src/app.txt")
	originalAssertion := f.state.Proof.Run
	if len(r.RecordFiles("lint")) != 0 || r.Read("fixture") != "x\r\n" {
		t.Fatal("later check erased an observable change from the first declared run")
	}
	r.Remove(".git/prepare")
	r.Write("fixture", "x\n")
	f.call(0, "next")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "app is ok", "--", "git", "grep", "ok", "--", "src/app.txt")
	f.want("done")
	if f.state.Proof.Run != originalAssertion || len(r.RecordFiles("unit")) != 2 {
		t.Fatal("recovery must retain the stable assertion and execute the changed check again")
	}
}

func TestLegacyVerificationGetsFreshPerRunEvidence(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "older-evidence"}
	f.call(0, "open", "--request", "Verify app", "--until", "verify")
	f.record("intake", "host", "Exercise app and lint")
	run := func() { f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "app is ok", "--", "sh", "unit.sh") }
	run()
	first := f.state.Proof.Run
	name := ".git/kitsu-stages/task-harness-older-evidence.json"
	var task map[string]json.RawMessage
	var proof map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Read(name)), &task); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(task["proof"], &proof); err != nil {
		t.Fatal(err)
	}
	delete(proof, "per_run_inputs")
	var err error
	if task["proof"], err = json.Marshal(proof); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(name, string(data))
	if got := r.Kitsu("task", "next", f.id, "--brief"); got.Code != 0 || !strings.Contains(got.Stdout, "fresh per-run evidence required") {
		t.Fatal("missing recovery explanation", got.Output())
	}
	f.call(0, "next")
	f.want("verify")
	run()
	f.want("done")
	if first == f.state.Proof.Run || len(r.RecordFiles("unit")) != 2 || len(r.RecordFiles("lint")) != 2 {
		t.Fatal("older receipts were reused without observing each command")
	}
}

func TestInputChangeDoesNotDispatchABehaviorRepair(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write(".gitattributes", "fixture text eol=lf\n")
	r.Write("fixture", "x\n")
	f := flow{t: t, repo: r, id: "restore-inputs"}
	f.call(0, "open", "--request", "Verify the original candidate and repair real failures", "--entry", "verify", "--until", "verify", "--repair")
	f.record("intake", "host", "Diagnose the check against the expected bytes")
	result := r.Kitsu("task", "test", f.id, "--brief", "--expect", "read fixture", "--ticket", f.state.Ticket, "--", "sh", "-c", "printf 'x\\r\\n' > fixture; exit 23")
	var receipt struct {
		Next string
		Log  struct {
			Outcome    string
			ExitStatus *int `json:"exit_status"`
		} `json:"verification_log"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil || result.Code != 5 || receipt.Next != "verify" || receipt.Log.Outcome != "source_changed" || receipt.Log.ExitStatus == nil || *receipt.Log.ExitStatus != 23 {
		t.Fatal("an unstable check must retain its exit status and return to verify", err, result.Output())
	}
}

func TestLosingVerificationAfterCommitKeepsIndependentReview(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "recover-ready"}
	f.call(0, "open", "--require-review", "--request", "Review the fix and deliver a verified commit", "--entry", "implement", "--until", "deliver", "--commit", "--review-limit", "1")
	f.record("intake", "host", "Verify and deliver the updated fixture")
	r.Write("change.txt", "approved fix")
	f.record("implement", "host", "Implemented the fix")
	f.review("independent-reviewer", "Inspected the fix and its behavior")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "unit behavior", "--", "sh", "unit.sh")
	r.Git("add", "change.txt")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver fix")
	f.want("done")
	head := r.Git("rev-parse", "HEAD")
	r.Remove(".git/kitsu/logs/" + f.state.Proof.Run + ".log")
	f.call(0, "next")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "unit behavior", "--", "sh", "unit.sh")
	f.want("done")
	if r.Git("rev-parse", "HEAD") != head || !f.state.Verified || f.state.Delivery != "committed" {
		t.Fatal("recovery must preserve the delivered commit", f.state)
	}
	// Another identical-tree commit has a different parent and cannot inherit
	// the original review. The one-start limit makes the pending review explicit.
	r.Git("commit", "--allow-empty", "-qm", "Change history")
	f.call(0, "next")
	f.want("paused")
}
