package harness_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestFeatureReviewBrief(t *testing.T) {
	for _, tc := range []struct {
		host  string
		stale bool
	}{{"codex", false}, {"claude", true}} {
		t.Run(tc.host, func(t *testing.T) {
			f, approval := earlyFeature(t, earlyPolicy)
			order := grantVerifyOrder(t, approval)
			r := f.repo
			r.Write("output.txt", "json\n")
			r.Git("add", "output.txt")
			workNote := strings.Repeat("Earlier implementation detail. ", 25)
			f.record("implement", "host", workNote)
			f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
			if tc.stale {
				r.Write("output.txt", "different candidate\n")
				r.Git("add", "output.txt")
				if err := os.Remove(order); err != nil {
					t.Fatal(err)
				}
				f.call(0, "next")
			}
			f.want("review")
			checks := len(r.RecordFiles("acceptance"))
			claim := r.Kitsu("task", "claim", f.id, "--ticket", f.state.Ticket, "--actor", "host", "--host", tc.host, "--brief")
			var reserved struct {
				Input []string `json:"review_input"`
			}
			if claim.Code != 0 || json.Unmarshal([]byte(claim.Stdout), &reserved) != nil ||
				!slices.Contains(reserved.Input, "--review-brief") {
				t.Fatal(claim.Output())
			}
			result := r.Kitsu(reserved.Input[1:]...)
			var brief struct {
				Request         string
				Details         []string
				Diff            []string              `json:"review_diff"`
				FeatureRevision string                `json:"feature_revision"`
				Criteria        []struct{ ID string } `json:"acceptance_criteria"`
				Verification    struct {
					Status string
					Runs   map[string]string `json:"repository_runs"`
				}
			}
			if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &brief) != nil || brief.FeatureRevision == "" ||
				!strings.Contains(brief.Request, "keep default text") || len(brief.Criteria) != 1 ||
				brief.Criteria[0].ID != "json-output" || len(brief.Details) < 2 ||
				!slices.Contains(brief.Diff, "--no-ext-diff") || strings.Contains(result.Stdout, workNote) {
				t.Fatal(result.Output())
			}
			wantStatus := "passed"
			if tc.stale {
				wantStatus = "unverified"
			}
			if brief.Verification.Status != wantStatus || (brief.Verification.Runs["acceptance"] != "") == tc.stale {
				t.Fatalf("verification status: %s", result.Output())
			}
			full := r.Kitsu(brief.Details[1:]...)
			if full.Code != 0 || !strings.Contains(full.Stdout, workNote) ||
				!strings.Contains(full.Stdout, "json-output") || len(result.Stdout) >= len(full.Stdout) {
				t.Fatal(full.Output())
			}
			if len(r.RecordFiles("acceptance")) != checks {
				t.Fatal("reading the review assignment ran a check")
			}
			t.Logf("review input bytes full=%d brief=%d", len(full.Stdout), len(result.Stdout))
			for _, args := range [][]string{
				{"--review-brief"},
				{"--review-ticket", f.state.Ticket, "--review-brief", "--compact"},
				{"--review-ticket", strings.Repeat("0", 64), "--review-brief"},
			} {
				if got := r.Kitsu(append([]string{"task", "show", f.id}, args...)...); got.Code == 0 {
					t.Fatalf("accepted invalid review read %v: %s", args, got.Output())
				}
			}
		})
	}
}
