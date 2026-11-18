package harness_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewerReadsReservedInputAcrossRepairs(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "rounds"}
	f.call(0, "open", "--require-review", "--request", "Implement a new format", "--entry", "plan", "--until", "verify", "--repair")
	f.record("intake", "host", "Empty values must survive")
	f.record("plan", "host", "Use one escaped field per line")
	r.Write("format", "first")
	reserve := func(action, note string) []string {
		t.Helper()
		got := r.Kitsu("task", "record", f.id, action, "--actor", "host", "--ticket", f.state.Ticket, "--note", note, "--claim-review", "codex", "--brief")
		var receipt struct {
			Input []string `json:"review_input"`
		}
		if got.Code != 0 || json.Unmarshal([]byte(got.Stdout), &receipt) != nil || len(receipt.Input) != 6 || receipt.Input[0] != "kitsu" {
			t.Fatal(got.Output())
		}
		f.call(0, "next")
		return receipt.Input[1:]
	}
	first := reserve("implement", "First implementation")
	if got := r.Kitsu(first...); got.Code != 0 || !strings.Contains(got.Stdout, "First implementation") || strings.Contains(got.Stdout, "prior_findings") {
		t.Fatal(got.Output())
	}
	findings := []string{"An empty first field is discarded", "A delimiter inside a field is split"}
	f.observedReview("codex", "host", "first-reviewer", "Two failing examples", "--finding", findings[0], "--finding", findings[1])
	if r.Kitsu(first...).Code == 0 {
		t.Fatal("accepted completed reservation")
	}
	r.Write("format", "second")
	second := reserve("repair", "Fixed both cases. Example suite passed")
	got := r.Kitsu(second...)
	var input struct {
		Request, Intake, Plan, Assignment string
		Repair                            string   `json:"reported_repair"`
		Findings                          []string `json:"prior_findings"`
		Prior                             string   `json:"prior_review"`
		Round                             int      `json:"review_round"`
		Changes                           []string `json:"review_changes"`
		Reviewer                          struct{ Host string }
	}
	if err := json.Unmarshal([]byte(got.Stdout), &input); got.Code != 0 || err != nil {
		t.Fatal(got.Output(), err)
	}
	if !strings.Contains(input.Assignment, "read-only") || input.Request != "Implement a new format" || input.Intake != "Empty values must survive" || input.Plan != "Use one escaped field per line" || input.Repair != "Fixed both cases. Example suite passed" || input.Round != 2 || input.Reviewer.Host != "codex" || input.Prior != "Two failing examples" || strings.Join(input.Findings, "|") != strings.Join(findings, "|") || strings.Contains(got.Stdout, "First implementation") {
		t.Fatal(got.Stdout)
	}
	if r.Kitsu(append(append([]string{}, second...), "--compact")...).Code != 2 {
		t.Fatal("a reviewer lookup accepted an unrelated compact view")
	}
	r.Setenv("GIT_EXTERNAL_DIFF", "missing-diff-driver")
	if len(input.Changes) < 2 || !strings.Contains(r.Git(input.Changes[1:]...), "+second") {
		t.Fatal(input.Changes)
	}
	// Work can move while a reviewer reads. It may still inspect the reserved
	// objects, but cannot return approval for the changed candidate.
	ticket := f.state.Ticket
	r.Write("format", "different")
	if again := r.Kitsu(second...); again.Stdout != got.Stdout || again.Code != 0 {
		t.Fatal("reservation inputs drifted", again.Output())
	}
	f.call(1, "record", "review", "--ticket", ticket, "--actor", "second-reviewer", "--note", "Clean")
	f.call(0, "release", "--ticket", ticket)
	if r.Kitsu(second...).Code == 0 {
		t.Fatal("accepted released reservation")
	}
}

func TestLegacyReviewReservationRemainsUsable(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "legacy-review"}
	f.call(0, "open", "--request", "Review the candidate", "--until", "review")
	f.record("intake", "host", "Inspect the diff")
	f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "host")
	path := r.Path(".git/kitsu-stages/task-harness-legacy-review.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	var claim map[string]json.RawMessage
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(record["claim"], &claim); err != nil {
		t.Fatal(err)
	}
	delete(claim, "candidate")
	record["claim"], err = json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got := r.Kitsu("task", "next", f.id, "--brief")
	if got.Code != 0 || strings.Contains(got.Stdout, "review_input") {
		t.Fatal(got.Output())
	}
	if r.Kitsu("task", "show", f.id, "--review-ticket", f.state.Ticket).Code == 0 {
		t.Fatal("accepted unpinned legacy claim")
	}
	f.record("review", "legacy-worker", "Checked original candidate")
	f.want("done")
}

func TestReviewerInputShowsGitNormalizedChanges(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write(".gitattributes", "fixture text eol=lf\n")
	r.Write("fixture", "one\n")
	r.Git("add", ".gitattributes", "fixture")
	r.Git("commit", "-qm", "Fixture stored with normalized line endings")
	f := flow{t: t, repo: r, id: "normalized"}
	f.call(0, "open", "--request", "Review actual file bytes", "--until", "review")
	f.record("intake", "host", "Check the exact line ending")
	f.review("first-reviewer", "Read the original bytes")
	r.Write("fixture", "one\r\n")
	f.call(0, "next")
	f.want("review")
	claim := r.Kitsu("task", "claim", f.id, "--actor", "host", "--ticket", f.state.Ticket, "--brief")
	var receipt struct {
		Created bool     `json:"claim_created"`
		Input   []string `json:"review_input"`
	}
	if err := json.Unmarshal([]byte(claim.Stdout), &receipt); claim.Code != 0 || err != nil || !receipt.Created || len(receipt.Input) != 6 {
		t.Fatal(claim.Output())
	}
	input := r.Kitsu(receipt.Input[1:]...)
	var view struct {
		RawOnly bool     `json:"review_raw_only_changes"`
		Changes []string `json:"review_changes"`
	}
	if err := json.Unmarshal([]byte(input.Stdout), &view); input.Code != 0 || err != nil || !view.RawOnly || len(view.Changes) < 2 || r.Git(view.Changes[1:]...) != "" {
		t.Fatal("empty Git diff hid changed original bytes", input.Output())
	}
}
