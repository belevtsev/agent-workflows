package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewerCanInspectPinnedStagedAndWorkingCopies(t *testing.T) {
	for _, tc := range []struct {
		name       string
		commitBase bool
		split      bool
	}{
		{"fully staged", true, false},
		{"partly staged", true, true},
		{"first commit pending", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			if tc.commitBase {
				r.Write("export.txt", "original\n")
				r.Git("add", "export.txt")
				r.Git("commit", "-qm", "original")
			}
			r.Write("export.txt", "staged defect\n")
			r.Git("add", "export.txt")
			if tc.split {
				r.Write("export.txt", "working fix\n")
			} else if diff := r.Git("diff"); diff != "" {
				t.Fatalf("expected empty ordinary diff, got %s", diff)
			}
			f := flow{t: t, repo: r, id: "review-export"}
			f.call(0, "open", "--entry", "review", "--until", "review", "--request", "Review the staged export")
			f.record("intake", "host", "Validate staged behavior")
			f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "host")
			input := r.Kitsu("task", "show", f.id, "--review-ticket", f.state.Ticket)
			var view struct {
				Working []string `json:"review_diff"`
				Staged  []string `json:"review_staged_diff"`
			}
			if input.Code != 0 || json.Unmarshal([]byte(input.Stdout), &view) != nil || len(view.Working) < 2 || view.Working[0] != "git" {
				t.Fatalf("no command for reserved content: %s", input.Output())
			}
			r.Write("export.txt", "later edit\n")
			r.Git("add", "export.txt")
			r.Setenv("GIT_EXTERNAL_DIFF", "missing-diff-driver")
			working := r.Git(view.Working[1:]...)
			if !tc.split {
				if len(view.Staged) != 0 || !strings.Contains(working, "+staged defect") {
					t.Fatal(working, view.Staged)
				}
				return
			}
			if len(view.Staged) < 2 || view.Staged[0] != "git" {
				t.Fatal("missing reserved index comparison", input.Stdout)
			}
			index := r.Git(view.Staged[1:]...)
			if !strings.Contains(index, "+staged defect") || !strings.Contains(working, "+working fix") || strings.Contains(working+index, "later edit") {
				t.Fatal(index, working)
			}
		})
	}
}
