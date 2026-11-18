package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestImplementationReviewPolicy(t *testing.T) {
	for _, policy := range []string{"ordinary", "requested", "project", "introduced", "invalid", "legacy"} {
		t.Run(policy, func(t *testing.T) {
			r := kitsutest.Demo(t)
			original := r.Read("kitsu.toml")
			required := "require_review = true\n" + original
			if policy == "project" {
				r.Write("kitsu.toml", required)
				r.Git("commit", "-qam", "Require independent review")
				r.Write("kitsu.toml", original)
			}
			f := flow{t: t, repo: r, id: "policy"}
			open := []string{"open", "--request", "Implement, test and commit the ready response", "--entry", "implement", "--until", "deliver", "--commit"}
			if policy == "requested" {
				open = append(open, "--require-review")
			}
			f.call(0, open...)
			if policy == "legacy" {
				path := ".git/kitsu-stages/task-harness-policy.json"
				data := map[string]any{}
				if err := json.Unmarshal([]byte(r.Read(path)), &data); err != nil {
					t.Fatal(err)
				}
				data["version"] = 1
				body, err := json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
				r.Write(path, string(body))
				f.call(0, "next")
			}
			f.record("intake", "host", "The command must print ready")
			if policy == "invalid" {
				r.Write("kitsu.toml", "require_review = [")
			}
			if policy == "introduced" {
				r.Write("kitsu.toml", required)
			}
			r.Write("response.txt", "pending\n")
			f.record("implement", "host", "Wrote the response")
			if policy == "invalid" {
				f.want("blocked")
				receipt := r.Kitsu("task", "next", f.id, "--brief")
				if receipt.Code != 0 || !strings.Contains(receipt.Stdout, "review_policy_error") || f.state.Ticket != "" {
					t.Fatal(receipt.Output())
				}
				r.Write("kitsu.toml", original)
				f.call(0, "next")
				f.want("verify")
				return
			}
			if policy != "ordinary" {
				f.want("review")
				f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Premature commit")
				return
			}
			f.want("verify")
			f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "response is ready", "--", "grep", "-qx", "ready", "response.txt")
			f.want("implement")
			r.Write("response.txt", "ready\n")
			f.record("implement", "host", "Corrected the response")
			f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
			f.want("commit")
			r.Git("add", "response.txt")
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Return ready")
			f.want("done")
			history := r.Kitsu("task", "show", f.id, "--json")
			var record struct {
				Events       []struct{ Action string }
				ReviewStarts int `json:"review_starts"`
			}
			if err := json.Unmarshal([]byte(history.Stdout), &record); err != nil || history.Code != 0 || record.ReviewStarts != 0 {
				t.Fatal(history.Output())
			}
			for _, event := range record.Events {
				if event.Action == "review" {
					t.Fatal("recorded an unrequested review")
				}
			}
			if r.Git("status", "--porcelain") != "" || strings.TrimSpace(r.Git("show", "HEAD:response.txt")) != "ready" {
				t.Fatal("requested delivery missing")
			}
		})
	}
}

func TestReviewedImplementationRecoversAfterFailedBehavior(t *testing.T) {
	for _, policy := range []string{"requested", "repository"} {
		t.Run(policy, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "reviewed-implementation"}
			open := []string{"open", "--request", "Implement the ready response and commit it", "--entry", "implement", "--until", "deliver", "--commit"}
			if policy == "repository" {
				r.Write("kitsu.toml", "require_review = true\n"+r.Read("kitsu.toml"))
				r.Git("commit", "-qam", "Require review")
			} else {
				open = append(open, "--require-review")
			}
			f.call(0, open...)
			f.record("intake", "host", "The response must be ready")
			r.Write("response.txt", "pending\n")
			f.record("implement", "host", "Added the response")
			f.review("first-reviewer", "Accepted the first candidate")
			f.want("verify")
			f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "response is ready", "--", "grep", "-qx", "ready", "response.txt")
			f.call(0, "next")
			f.want("implement")
			r.Write("response.txt", "ready\n")
			f.record("implement", "host", "Corrected the tested response")
			f.want("review")
			f.review("second-reviewer", "Checked the corrected response")
			f.want("verify")
			f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
			f.want("commit")
			r.Git("add", "response.txt")
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Return ready")
			f.want("done")
			if r.Git("show", "HEAD:response.txt") != "ready" {
				t.Fatal("the verified response was not delivered")
			}
		})
	}
}

func TestVerifyFirstReportsCheckedTreeUntilReviewClearsDelivery(t *testing.T) {
	for _, finish := range []string{"verify", "deliver"} {
		for _, finding := range []bool{false, true} {
			name := "approved"
			if finding {
				name = "finding"
			}
			t.Run(finish+"/"+name, func(t *testing.T) {
				r := kitsutest.Demo(t)
				r.Write("kitsu.toml", "require_review = true\n"+r.Read("kitsu.toml"))
				r.Git("commit", "-qam", "Require independent review")
				r.Write("example.csv", "normal,field\n")
				r.Git("add", "example.csv")
				before := freezeStageWorkspace(r, "example.csv")
				f := flow{t: t, repo: r, id: "csv-check"}
				open := []string{"open", "--request", "Check the supplied CSV patch under the repository review policy", "--entry", "verify", "--until", finish}
				if finish == "deliver" {
					open = append(open, "--commit")
				}
				f.call(0, open...)
				f.record("intake", "coordinator", "Check the ordinary field and review CSV compatibility")
				f.want("verify")
				f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "ordinary field is present", "--", "git", "grep", "-q", "^normal,field$", "--", "example.csv")
				f.want("review")
				checkTree := func() {
					t.Helper()
					for _, flag := range []string{"--json", "--brief"} {
						view := stageJSON[stageReceipt](t, r.Kitsu("task", "next", f.id, flag))
						if !view.Verified || view.Delivery != "verified working tree" {
							t.Fatalf("%s returned verification=%t delivery=%q next=%q", flag, view.Verified, view.Delivery, view.Next)
						}
					}
					saved := stageJSON[struct{ Harness stageReceipt }](t, r.Kitsu("task", "show", f.id, "--json"))
					if !saved.Harness.Verified || saved.Harness.Delivery != "verified working tree" {
						t.Fatalf("task show disagrees with task next: %+v", saved.Harness)
					}
					out := r.Kitsu("task", "next", f.id)
					if out.Code != 0 || !strings.Contains(out.Stdout, "verified working tree") {
						t.Fatal(out.Output())
					}
				}
				checkTree()
				// Even a fully staged and tested candidate still needs its required review.
				f.call(1, "commit", "--message", "Too early", "--ticket", f.state.Ticket)
				if finding {
					f.review("reviewer", "Required escaping case missing", "--finding", "CSV regression omits embedded quotes")
					f.want("blocked")
					checkTree()
					stageHistory(&f, "intake", "test", "review")
					before.check(t, r)
					return
				}
				f.review("reviewer", "Checked the supplied candidate")
				if finish == "verify" {
					f.want("done")
					checkTree()
					before.check(t, r)
					return
				}
				f.want("commit")
				if f.state.Delivery != "ready to commit" {
					t.Fatal(f.state.Delivery)
				}
				ticket := f.state.Ticket
				r.Git("reset", "-q", "HEAD", "--", "example.csv")
				f.call(0, "next")
				f.want("verify")
				if f.state.Verified {
					t.Fatal("unstaged patch retained its prior index verification")
				}
				r.Git("add", "example.csv")
				f.call(0, "commit", "--ticket", ticket, "--message", "Add verified example", "--include", "example.csv")
				f.want("done")
				if f.state.Delivery != "committed" || r.Git("show", "HEAD:example.csv") != "normal,field" {
					t.Fatal(f.state)
				}
			})
		}
	}
}
