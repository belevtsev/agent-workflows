package harness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type committedReviewInput struct {
	Scope struct {
		Base string `json:"review_base"`
		Head string `json:"review_head"`
	}
	Candidate  stageCandidate `json:"reserved_candidate"`
	Assignment string
	Log        []string `json:"review_log"`
	Diff       []string `json:"review_diff"`
}

type committedReviewClaim struct {
	Command []string `json:"review_input"`
	Claim   struct{ Worker, Ticket string }
	Created bool `json:"claim_created"`
}

func TestCommittedReviewRange(t *testing.T) {
	const request = "Review review-base..topic for the positive connection limit. Keep the checkout read-only."
	const baseline = "{\"max_connections\":10}\n"
	const faulty = "{\"max_connections\":0}\n"
	const clean = "{\"max_connections\":25}\n"
	for _, tt := range []struct {
		name, settings string
		wantFinding    bool
	}{
		{name: "faulty", settings: faulty, wantFinding: true},
		{name: "clean", settings: clean},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newStageRepo(t)
			r.Write("settings.json", baseline)
			r.Git("add", "settings.json")
			r.Git("commit", "-qm", "Set default connection limit")
			r.Write("unrelated.txt", "Unrelated text from an earlier commit.\n")
			r.Git("add", "unrelated.txt")
			r.Git("commit", "-qm", "Add unrelated text")
			base := r.Git("rev-parse", "HEAD")
			r.Git("branch", "review-base", base)

			r.Write("settings.json", tt.settings)
			r.Git("add", "settings.json")
			r.Git("commit", "-qm", "Set deployment connection limit")
			first := r.Git("rev-parse", "HEAD")
			r.Write("release.md", "The deployment requires a positive connection limit.\n")
			r.Git("add", "release.md")
			r.Git("commit", "-qm", "Document deployment contract")
			head, tree := r.Git("rev-parse", "HEAD"), r.Git("rev-parse", "HEAD^{tree}")
			r.Git("branch", "topic", head)
			workspace := freezeStageWorkspace(r, "settings.json", "release.md", "unrelated.txt")
			if workspace.status != "" || r.Git("diff", "HEAD", "--") != "" {
				t.Fatal("fixture must start with an empty working diff")
			}

			open := []string{"open", "--entry", "review", "--until", "review", "--review-base", "review-base", "--review-head", "topic", "--request", request}
			f := flow{t: t, repo: r, id: "committed-review"}
			if tt.wantFinding {
				checkCommittedReviewOptions(t, r, tree)
			}
			f.call(0, open...)
			intakeTicket := f.state.Ticket
			f.call(0, open...)
			if f.state.Ticket != intakeTicket {
				t.Fatal("reopening an identical scope replaced the intake ticket")
			}
			f.record("intake", "coordinator", "Check the deployments in the requested commits.")
			claim := reserveCommittedReview(&f)
			ticket := f.state.Ticket
			const worker = "native-range-reviewer"
			f.call(0, "claim", "--ticket", ticket, "--actor", "coordinator", "--worker", worker)
			brief := r.Kitsu(claim.Command[1:]...)
			input := stageJSON[committedReviewInput](t, brief)
			workspace.check(t, r)

			// Move both the named base and the live checkout after reservation.
			r.Git("branch", "-f", "review-base", first)
			r.Git("checkout", "--detach", "-q", base)
			r.Write("settings.json", "{\"max_connections\":63}\n")
			r.Git("add", "settings.json")
			r.Write("settings.json", "{\"max_connections\":-2}\n")
			// Local Git replacement objects can redirect the same commit ID.
			r.Git("replace", head, first)
			workspace = freezeStageWorkspace(r, "settings.json", "unrelated.txt")

			resumed := flow{t: t, repo: r, id: f.id}
			resumed.call(0, "next")
			resumed.want("review")
			recovered := stageJSON[committedReviewClaim](t, r.Kitsu("task", "next", f.id, "--brief"))
			if recovered.Created || recovered.Claim.Worker != worker || recovered.Claim.Ticket != ticket || !slices.Equal(recovered.Command, claim.Command) {
				t.Fatalf("lost the reserved range or worker on restart: %+v", recovered)
			}
			again := r.Kitsu(recovered.Command[1:]...)
			if again.Code != 0 || again.Stdout != brief.Stdout {
				t.Fatal("moving the base changed the reserved review brief", again.Output())
			}
			if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "review-input.json"), []byte(again.Stdout), 0o644); err != nil {
				t.Fatal(err)
			}

			checkCommittedReviewInput(t, r, input, base, head, tree, []string{head, first}, []string{"release.md", "settings.json"})
			var settings struct {
				Limit int `json:"max_connections"`
			}
			blob := r.Git("show", input.Candidate.Tree+":settings.json")
			if err := json.Unmarshal([]byte(blob), &settings); err != nil {
				t.Fatal(err)
			}
			var findings []string
			if settings.Limit <= 0 {
				findings = append(findings, "major settings.json:1 — read max_connections from the reserved tree. A zero limit violates the deployment contract.")
			}
			if (len(findings) != 0) != tt.wantFinding {
				t.Fatalf("reviewed limit %d, expected finding %t", settings.Limit, tt.wantFinding)
			}
			args := []string{"--note", "Checked the committed deployment limit and documentation.", "--ticket", ticket}
			for _, finding := range findings {
				args = append(args, "--finding", finding)
			}
			resumed.call(1, append([]string{"record", "review", "--actor", "wrong-worker"}, args...)...)
			resumed.call(0, append([]string{"record", "review", "--actor", worker}, args...)...)
			resumed.want("done")
			result := stageNext(&resumed)
			if !result.ReviewCurrent || result.Verified || !slices.Equal(result.Last.Findings, findings) || result.Last.Candidate != input.Candidate || result.Last.ReviewedIndex {
				t.Fatalf("review was attributed to another candidate: %+v", result)
			}
			stageHistory(&resumed, "intake", "review")
			resumed.call(1, open...)
			resumed.call(2, "extend", "--until", "verify")
			resumed.call(0, "next")
			resumed.want("done")

			newer := flow{t: t, repo: r, id: "newer-range"}
			newer.call(0, open...)
			newer.record("intake", "coordinator", "Review the current base and topic endpoints.")
			fresh := reserveCommittedReview(&newer)
			if newer.state.Ticket == ticket {
				t.Fatal("the newer range reused the old ticket")
			}
			freshInput := stageJSON[committedReviewInput](t, r.Kitsu(fresh.Command[1:]...))
			checkCommittedReviewInput(t, r, freshInput, first, head, tree, []string{head}, []string{"release.md"})
			workspace.check(t, r)
			t.Logf("base %s, head %s, commit list %v, reviewed tree %s, findings %d, review record exit 0", base, head, []string{head, first}, tree, len(findings))
		})
	}
}

func checkCommittedReviewOptions(t *testing.T, r *kitsutest.Repo, tree string) {
	t.Helper()
	for _, tt := range []struct {
		args []string
		code int
	}{
		{args: []string{"--review-base", "review-base"}, code: 2},
		{args: []string{"--review-base=", "--review-head="}, code: 2},
		{args: []string{"--review-base", "review-base", "--review-head", "topic", "--repair"}, code: 2},
		{args: []string{"--review-base", tree, "--review-head", "topic"}, code: 1},
	} {
		args := append([]string{"task", "open", "invalid-range", "--until", "review", "--request", "Check the range."}, tt.args...)
		if result := r.Kitsu(args...); result.Code != tt.code {
			t.Errorf("%v exit %d, want %d\n%s", args, result.Code, tt.code, result.Output())
		}
	}
	if r.Exists(".git/kitsu-stages/task-harness-invalid-range.json") {
		t.Fatal("an invalid range created a saved task")
	}
}

func reserveCommittedReview(f *flow) committedReviewClaim {
	f.t.Helper()
	f.want("review")
	claim := stageJSON[committedReviewClaim](f.t, f.repo.Kitsu("task", "claim", f.id,
		"--ticket", f.state.Ticket, "--actor", "coordinator", "--host", "codex", "--brief"))
	if !claim.Created || len(claim.Command) != 6 || claim.Command[0] != "kitsu" || claim.Claim.Ticket != f.state.Ticket {
		f.t.Fatalf("expected a new independent review reservation: %+v", claim)
	}
	return claim
}

func checkCommittedReviewInput(t *testing.T, r *kitsutest.Repo, input committedReviewInput, base, head, tree string, commits, paths []string) {
	t.Helper()
	if input.Scope.Base != base || input.Scope.Head != head || input.Candidate.Head != head || input.Candidate.Tree != tree || input.Candidate.Index != "" {
		t.Fatalf("reserved input points to a different candidate: %+v", input)
	}
	if len(input.Log) < 2 || input.Log[0] != "git" || len(input.Diff) < 2 || input.Diff[0] != "git" || !strings.Contains(input.Assignment, "read-only") {
		t.Fatalf("expected read-only range commands in reviewer input: %+v", input)
	}
	if actual := strings.Fields(r.Git(input.Log[1:]...)); !slices.Equal(actual, commits) {
		t.Fatalf("reviewer sees commits %v, want %v", actual, commits)
	}
	patch := r.Git(input.Diff[1:]...)
	var changed []string
	for line := range strings.Lines(patch) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "+++ b/"); ok {
			changed = append(changed, path)
		}
	}
	if !slices.Equal(changed, paths) || strings.Contains(patch, "unrelated.txt") {
		t.Fatalf("reviewer sees files %v, want %v\n%s", changed, paths, patch)
	}
}
