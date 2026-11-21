package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

const earlyPolicy = `version = 1
require_review = true
[[check]]
id = "acceptance"
run = ["git", "grep", "--no-index", "-q", "-e", "^json$", "--", "output.txt"]
`

func earlyFeature(t *testing.T, policy string) (flow, string) {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Write("kitsu.toml", policy)
	r.Write("output.txt", "text\n")
	r.Git("add", ".")
	r.Git("commit", "-qm", "Text export")
	approval := approveCIRepo(t, r)
	f := flow{t: t, repo: r, id: "export-json"}
	f.call(0, "open", "--entry", "implement", "--until", "deliver", "--commit", "--request", "Add JSON export and keep default text")
	f.record("intake", "host", "JSON mode returns the requested representation",
		"--feature-brief", writeFeatureFixture(t, harness.FeatureBrief{Version: 1, Goal: "Add JSON export", Scope: "Keep the default",
			Criteria: []harness.Criterion{{ID: "json-output", Expected: "Output is json in the fixture", Check: "check:acceptance"}}}))
	return f, approval
}

func grantVerifyOrder(t *testing.T, approval string) string {
	t.Helper()
	data, err := os.ReadFile(approval)
	if err != nil {
		t.Fatal(err)
	}
	path := approval + ".verify-order"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyFeatureBeforeFirstReview(t *testing.T) {
	f, approval := earlyFeature(t, earlyPolicy)
	grantVerifyOrder(t, approval)
	r := f.repo
	r.Write("output.txt", "broken\n")
	r.Git("add", "output.txt")
	implement := f.state.Ticket
	args := []string{"record", "implement", "--claim-review", "codex", "--actor", "host",
		"--note", "Candidate for delivery", "--ticket", implement}
	f.call(0, args...)
	f.want("verify")
	f.call(0, args...)
	f.want("verify")
	if len(r.RecordFiles("acceptance")) > 0 {
		t.Fatal("recording implementation ran a check")
	}

	f.call(5, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("implement")
	current := r.Read(".git/kitsu-stages/task-harness-export-json.json")
	if strings.Contains(current, `"review_starts"`) || strings.Contains(current, `"claim"`) {
		t.Fatal("failed candidate consumed a review reservation")
	}
	r.Write("output.txt", "json\n")
	r.Git("add", "output.txt")
	f.record("implement", "host", "Corrected output")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("review")
	f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Too early")
	r.Write("output.txt", "stale\n")
	r.Git("add", "output.txt")
	f.call(0, "next")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("changed candidate retained proof")
	}
	r.Write("output.txt", "json\n")
	r.Git("add", "output.txt")
	f.call(0, "next")
	f.review("independent", "Verified the behavior and implementation")
	f.want("commit")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Add JSON export")
	f.want("done")
}

func TestVerifyBeforeReviewConsentChanges(t *testing.T) {
	for _, mode := range []string{"disabled", "review already reserved", "revoked before test", "revoked before correction retry", "ordinary trust removed", "candidate policy changed"} {
		t.Run(mode, func(t *testing.T) {
			f, approval := earlyFeature(t, earlyPolicy)
			if mode == "disabled" || mode == "review already reserved" {
				f.record("implement", "host", "Candidate for delivery")
				f.want("review")
				if mode == "review already reserved" {
					f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "host", "--host", "codex")
					grantVerifyOrder(t, approval)
					f.call(0, "next")
					f.want("review")
				}
				return
			}
			order := grantVerifyOrder(t, approval)
			f.record("implement", "host", "Candidate for delivery")
			f.want("verify")
			runs := 0
			switch mode {
			case "revoked before correction retry":
				f.call(5, "test", "--ticket", f.state.Ticket, "--retry")
				f.want("implement")
				runs = len(f.repo.RecordFiles("acceptance"))
				fallthrough
			case "revoked before test":
				if err := os.Remove(order); err != nil {
					t.Fatal(err)
				}
			case "ordinary trust removed":
				if err := os.Remove(approval); err != nil {
					t.Fatal(err)
				}
			case "candidate policy changed":
				f.repo.Write("kitsu.toml", earlyPolicy+"# changed\n")
			}
			f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
			if len(f.repo.RecordFiles("acceptance")) != runs {
				t.Fatal("started a check using stale consent")
			}
		})
	}
}

func TestVerifyBeforeReviewRevokedBetweenChecks(t *testing.T) {
	f, approval := earlyFeature(t, `version = 1
require_review = true
[[check]]
id = "acceptance"
run = ["sh", "-c", "touch .git/first-check; until test -f .git/continue-check; do sleep 0.05; done"]
timeout = "8s"
[[check]]
id = "remaining"
run = ["git", "status", "--short"]
`)
	order := grantVerifyOrder(t, approval)
	r := f.repo
	f.record("implement", "host", "Candidate for delivery")
	f.want("verify")
	cmd := r.Command(filepath.Join(kitsutest.Binary(t), "kitsu"), "task", "test", f.id, "--ticket", f.state.Ticket, "--retry", "--brief")
	ready := make(chan string, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) == 0 {
			out = []byte(err.Error())
		}
		ready <- string(out)
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for !r.Exists(".git/first-check") {
		select {
		case out := <-ready:
			t.Fatal(out)
		case <-deadline.C:
			t.Fatal("assertion never started")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := os.Remove(order); err != nil {
		t.Fatal(err)
	}
	r.Write(".git/continue-check", "")
	if out := <-ready; !strings.Contains(out, "early verification needs current checkout approval") {
		t.Fatal(out)
	}
	if len(r.RecordFiles("remaining")) != 0 {
		t.Fatal("launched another check after revocation")
	}
}
