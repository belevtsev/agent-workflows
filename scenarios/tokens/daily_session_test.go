package tokens_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// A session can contain several daily tasks. Its host counters are useful even
// when there is no labelled worker, but cannot be added to each task's cost.
func TestDailyTaskShowsWholeSessionEvidenceWithoutChargingItsTotal(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			repo := kitsutest.NewRepo(t)
			var s session
			var actor string
			var total int
			if host == "codex" {
				s, actor, total = buildS2(t), s2Session, s2SessionTokens.total()
			} else {
				s, actor, total = buildS1(t, variant{}), s1Session, s1SessionTokens.total()
			}
			run := func(args ...string) map[string]any {
				t.Helper()
				r := repo.Kitsu(args...)
				var data map[string]any
				if err := json.Unmarshal([]byte(r.Stdout), &data); r.Code != 0 || err != nil {
					t.Fatalf("%v: %s", args, r.Output())
				}
				return data
			}
			plan := func(id, who string) {
				t.Helper()
				state := run("task", "open", id, "--entry", "plan", "--until", "plan", "--request", "Plan a CSV export", "--json")
				for _, action := range []string{"intake", "plan"} {
					state = run("task", "record", id, action, "--actor", who, "--ticket", state["ticket"].(string), "--note", "Use the CSV writer. Test a field containing a quote.", "--json")
				}
				if state["next"] != "done" {
					t.Fatal(state)
				}
			}
			plan("export-fields", actor)
			plan("import-fields", actor)
			plan("other-work", "another-session")
			if r := recordNotice(t, repo, host, s.Notices[len(s.Notices)-1]); r.Code != 0 {
				t.Fatal(r.Output())
			}
			for _, id := range []string{"export-fields", "import-fields"} {
				for _, args := range [][]string{{"usage", "show", "--task", id, "--json"}, {"task", "next", id, "--json"}} {
					data := run(args...)
					rows := entries(t, data, "actor_session_usage")
					if len(rows) != 1 || rows[0]["host"] != host || rows[0]["session"] != actor {
						t.Fatalf("%v did not identify the solo actor: %v", args, data)
					}
					wantNumber(t, rows[0], "total", total)
					key := "total"
					if args[0] == "task" {
						key = "captured_tokens"
					}
					if value, exists := data[key]; !exists || value != nil {
						t.Fatalf("whole-session cost charged to %s: %v", id, data)
					}
				}
				out := repo.Kitsu("usage", "show", "--task", id)
				if out.Code != 0 || !strings.Contains(out.Stdout, "whole session") || !strings.Contains(out.Stdout, actor) {
					t.Fatal(out.Output())
				}
			}
			if data := run("usage", "show", "--task", "other-work", "--json"); data["actor_session_usage"] != nil || data["total"] != nil {
				t.Fatal("unmatched actor received usage", data)
			}
			if r := repo.Kitsu("usage", "show", "--task", "absent", "--json"); r.Code == 0 {
				t.Fatal("invented a missing task", r.Output())
			}
			writeFile(t, s.Paths["session"], "broken\n")
			if r := recordNotice(t, repo, host, s.Notices[len(s.Notices)-1]); r.Code != 0 {
				t.Fatal(r.Output())
			}
			partial := run("usage", "show", "--task", "export-fields", "--json")
			rows := entries(t, partial, "actor_session_usage")
			wantNumber(t, rows[0], "partial_starts", 1)
			wantNumber(t, rows[0], "total", total)
			if out := repo.Kitsu("usage", "show", "--task", "export-fields"); !strings.Contains(out.Stdout, "at least") {
				t.Fatal(out.Output())
			}
		})
	}
}
