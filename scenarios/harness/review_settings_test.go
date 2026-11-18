package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewReservationResolvesHostSettingsOnce(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			r := kitsutest.Demo(t)
			path := map[string]string{"codex": ".agents", "claude": ".claude"}[host] + "/skills/verify/SKILL.md"
			const source = "---\nname: verify\ndescription: Review the exported data.\n---\nRead a real export.\n"
			r.Write(path, source)
			if result := r.Kitsu("role", "set", "reviewer", "--host", host, "--model", "custom-reviewer", "--reasoning", "high", "--skills", "verify"); result.Code != 0 {
				t.Fatal(result.Output())
			}
			f := flow{t: t, repo: r, id: "review-settings"}
			f.call(0, "open", "--request", "Review an existing patch", "--entry", "review", "--until", "review")
			f.record("intake", "coordinator", "Review the pending changes")
			if err := os.Remove(r.Path(path)); err != nil {
				t.Fatal(err)
			}
			f.call(0, "next")
			claim := []string{"task", "claim", f.id, "--host", host, "--actor", "coordinator", "--brief", "--ticket", f.state.Ticket}
			if result := r.Kitsu(claim...); result.Code == 0 {
				t.Fatal("reserved a reviewer with a missing selected skill")
			}
			r.Write(path, source)
			f.call(0, "next")
			claim[len(claim)-1] = f.state.Ticket
			result := r.Kitsu(claim...)
			var reply struct {
				Claim struct {
					Reviewer struct {
						Host   string
						Model  struct{ Model, Effort string }
						Skills []struct{ ID, Path, SHA256 string }
					}
				}
			}
			if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &reply) != nil {
				t.Fatal(result.Output())
			}
			settings := reply.Claim.Reviewer
			if settings.Host != host || settings.Model.Model != "custom-reviewer" || settings.Model.Effort != "high" || len(settings.Skills) != 1 {
				t.Fatalf("unexpected settings %+v", settings)
			}
			skill := settings.Skills[0]
			if skill.ID == "" || skill.Path != r.Path(path) || skill.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
				t.Fatalf("wrong skill source %+v", skill)
			}
			f.call(1, "claim", "--actor", "duplicate", "--ticket", f.state.Ticket, "--host", host)
			f.record("review", "worker", "Inspected the requested behavior")
			f.want("done")
			show := r.Kitsu("task", "show", f.id, "--json")
			var record struct {
				History []struct {
					Action   string
					Reviewer json.RawMessage
				} `json:"events"`
			}
			if show.Code != 0 || json.Unmarshal([]byte(show.Stdout), &record) != nil || len(record.History) != 2 || len(record.History[1].Reviewer) == 0 {
				t.Fatalf("missing review settings in history: %s", show.Output())
			}
		})
	}
}

func TestBriefReviewCanProceedWithoutReadableUsageHistory(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "brief-usage"}
	f.call(0, "open", "--entry", "review", "--until", "review", "--request", "Review the change")
	f.record("intake", "coordinator", "Inspect changes")
	// A malformed optional usage archive should only affect views that request it.
	r.Write(".git/kitsu/tokens", "not a directory")
	if got := r.Kitsu("task", "next", f.id, "--json"); got.Code != 1 || !strings.Contains(got.Output(), "is not a directory") {
		t.Fatalf("full usage view did not identify the malformed archive: exit %d\n%s", got.Code, got.Output())
	}
	result := r.Kitsu("task", "claim", f.id, "--ticket", f.state.Ticket, "--actor", "coordinator", "--host", "codex", "--brief")
	if result.Code != 0 {
		t.Fatal(result.Output())
	}
	var reply struct {
		Claim struct{ Reviewer struct{ Host string } }
	}
	if err := json.Unmarshal([]byte(result.Stdout), &reply); err != nil || reply.Claim.Reviewer.Host != "codex" {
		t.Fatalf("missing reviewer defaults: %s", result.Output())
	}
}
