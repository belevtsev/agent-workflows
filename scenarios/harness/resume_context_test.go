package harness_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestIdentifyAndResumeDailyTask(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "task-opaque-first"}
	request := "Preserve quoted fields\n" + strings.Repeat("é", 180)
	f.call(0, "open", "--require-review", "--entry", "plan", "--until", "deliver", "--repair", "--request", request)
	f.record("intake", "coordinator", "A quoted comma must survive export and import")
	f.record("plan", "architect", "Use the same CSV dialect for export and import")
	f.record("implement", "developer", "Changed the export path")
	f.review("first-reviewer", "Quoted comma still splits", "--finding", "The input x,y becomes two fields")
	currentFinding := r.Kitsu("task", "next", f.id)
	if currentFinding.Code != 0 || !strings.Contains(currentFinding.Stdout, "Review finding: The input x,y becomes two fields") {
		t.Fatal(currentFinding.Output())
	}
	r.Write("export.txt", "corrected\n")
	previousFinding := r.Kitsu("task", "next", f.id)
	if previousFinding.Code != 0 || !strings.Contains(previousFinding.Stdout, "The latest review covers an earlier candidate") ||
		!strings.Contains(previousFinding.Stdout, "Earlier finding: The input x,y becomes two fields") || strings.Contains(previousFinding.Stdout, "Review finding:") || strings.Contains(previousFinding.Stdout, "review: Quoted comma still splits") {
		t.Fatal("superseded finding presented as current", previousFinding.Output())
	}
	f.record("repair", "developer", "Reused the import dialect")
	f.review("second-reviewer", "Confirmed the fix and caller compatibility")
	f.want("verify")
	other := flow{t: t, repo: r, id: "task-opaque-second"}
	other.call(0, "open", "--until", "plan", "--request", "Explain connection reuse")

	list := r.Kitsu("task", "list", "--json")
	var index struct {
		Tasks []struct {
			Task  string
			Daily struct {
				Request    string
				LastAction string `json:"last_action"`
			} `json:"daily"`
		}
	}
	if err := json.Unmarshal([]byte(list.Stdout), &index); list.Code != 0 || err != nil || len(index.Tasks) != 2 {
		t.Fatal(list.Output())
	}
	first, second := index.Tasks[0], index.Tasks[1]
	if first.Task != f.id || first.Daily.LastAction != "review" || !strings.HasPrefix(first.Daily.Request, "Preserve quoted fields ") ||
		utf8.RuneCountInString(first.Daily.Request) > 120 || !strings.HasSuffix(first.Daily.Request, "…") || second.Daily.Request != "Explain connection reuse" {
		t.Fatalf("tasks cannot be distinguished by request and last saved action: %+v", index)
	}
	human := r.Kitsu("task", "list")
	if human.Code != 0 || !strings.Contains(human.Stdout, first.Daily.Request) || !strings.Contains(human.Stdout, second.Daily.Request) {
		t.Fatal(human.Output())
	}
	recovery := r.Kitsu("task", "next", f.id, "--json")
	var view struct {
		Next, Request string
		Current       *bool                 `json:"review_current"`
		PriorNotes    map[string]string     `json:"prior_notes"`
		Review        struct{ Note string } `json:"latest_review"`
	}
	if err := json.Unmarshal([]byte(recovery.Stdout), &view); recovery.Code != 0 || err != nil || view.Request != request ||
		view.Current == nil || !*view.Current || view.Next != "verify" || view.Review.Note != "Confirmed the fix and caller compatibility" ||
		view.PriorNotes["intake"] != "A quoted comma must survive export and import" ||
		view.PriorNotes["plan"] != "Use the same CSV dialect for export and import" ||
		view.PriorNotes["repair"] != "Reused the import dialect" {
		t.Fatal("incomplete coordinator recovery", recovery.Output())
	}
	terminal := r.Kitsu("task", "next", f.id)
	if terminal.Code != 0 {
		t.Fatal(terminal.Output())
	}
	for _, want := range []string{`Request: "Preserve quoted fields\n`, view.PriorNotes["intake"], view.PriorNotes["plan"], view.PriorNotes["repair"], view.Review.Note} {
		if strings.Count(terminal.Stdout, want) != 1 {
			t.Fatalf("expected one occurrence of %q in the resume output: %s", want, terminal.Stdout)
		}
	}
	r.Write("export.txt", "changed after approval")
	changed := r.Kitsu("task", "next", f.id, "--json")
	view.Current = nil
	if err := json.Unmarshal([]byte(changed.Stdout), &view); changed.Code != 0 || err != nil || view.Current == nil || *view.Current || view.Next != "review" || view.Review.Note == "" {
		t.Fatal("a previous approval must be marked stale", changed.Output())
	}
	previousApproval := r.Kitsu("task", "next", f.id)
	if previousApproval.Code != 0 || !strings.Contains(previousApproval.Stdout, "The latest review covers an earlier candidate") ||
		strings.Contains(previousApproval.Stdout, "review: Confirmed the fix and caller compatibility") {
		t.Fatal("superseded approval presented as current", previousApproval.Output())
	}
	brief := r.Kitsu("task", "next", f.id, "--brief")
	if brief.Code != 0 || strings.Contains(brief.Stdout, "prior_notes") || strings.Contains(brief.Stdout, "Use the same CSV dialect") {
		t.Fatal("routine transition repeated recovery notes", brief.Output())
	}
}

func TestTaskListKeepsHealthyRowsWhenADailyRecordIsUnreadable(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "middle-healthy"}
	f.call(0, "open", "--request", "Plan the CSV fix", "--until", "plan")
	f.record("intake", "host", "Preserve quoted fields")
	const broken = ".git/kitsu-stages/task-harness-aa-broken.json"
	const invalid = ".git/kitsu-stages/task-harness-zz-invalid.json"
	r.Write(broken, "{")
	r.Write(invalid, `{"task":"other-id","version":1}`)
	result := r.Kitsu("task", "list", "--json")
	var index struct {
		Tasks []struct {
			Task, Stage string
			Error       string `json:"daily_error"`
			Daily       *struct{ Request string }
		}
	}
	if result.Code != 1 || json.Unmarshal([]byte(result.Stdout), &index) != nil || len(index.Tasks) != 3 {
		t.Fatal("partial inventory unavailable", result.Output())
	}
	for _, row := range index.Tasks {
		if row.Task == f.id {
			if row.Daily == nil || row.Daily.Request != "Plan the CSV fix" || row.Error != "" {
				t.Fatalf("healthy task lost its summary: %+v", row)
			}
		} else if row.Stage != "unreadable" || row.Daily != nil || row.Error == "" {
			t.Fatalf("damaged record presented as usable: %+v", row)
		}
	}
	human := r.Kitsu("task", "list")
	if human.Code != 1 || !strings.Contains(human.Stdout, "Plan the CSV fix") || !strings.Contains(human.Stdout, "aa-broken: unreadable") || !strings.Contains(human.Stdout, "zz-invalid: unreadable") {
		t.Fatal(human.Output())
	}
	f.call(0, "next")
	f.record("plan", "host", "Use the CSV writer")
	f.want("done")
	for _, name := range []string{"aa-broken", "zz-invalid"} {
		for _, verb := range []string{"next", "show"} {
			if got := r.Kitsu("task", verb, name, "--json"); got.Code == 0 {
				t.Fatal("accepted unreadable record", got.Output())
			}
		}
	}
	if r.Read(broken) != "{" || r.Read(invalid) != `{"task":"other-id","version":1}` {
		t.Fatal("inspection overwrote a damaged record")
	}
}

func TestTaskListKeepsDailyResultsOnSharedCatalogFailure(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "healthy-daily"}
	f.call(0, "open", "--request", "Explain the cache", "--until", "plan")
	r.Write("openspec/changes", "not a directory")
	res := r.Kitsu("task", "list", "--json")
	var list struct {
		Error string `json:"list_error"`
		Tasks []struct{ Task string }
	}
	if res.Code != 1 || json.Unmarshal([]byte(res.Stdout), &list) != nil || !strings.Contains(list.Error, "cannot list shared tasks") || len(list.Tasks) != 1 || list.Tasks[0].Task != f.id {
		t.Fatal(res.Output())
	}
	f.call(0, "next")
	f.want("intake")
}

func TestTaskListKeepsSharedRowWhenDailyHistoryIsUnreadable(t *testing.T) {
	r := kitsutest.Demo(t)
	if err := os.MkdirAll(r.Path("openspec/changes/.skills/topic"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Write(".git/kitsu-stages/task-harness-topic.json", "{")
	for _, brokenRoot := range []bool{false, true} {
		if brokenRoot {
			r.Remove(".git/kitsu-stages/task-harness-topic.json")
			r.Remove(".git/kitsu-stages")
			r.Write(".git/kitsu-stages", "not a directory")
		}
		res := r.Kitsu("task", "list", "--json")
		var list struct {
			Error string `json:"list_error"`
			Tasks []struct {
				Task, Workflow, Stage string
				DailyError            string `json:"daily_error"`
			}
		}
		if res.Code != 1 || json.Unmarshal([]byte(res.Stdout), &list) != nil || list.Error == "" || len(list.Tasks) != 1 {
			t.Fatal(res.Output())
		}
		row := list.Tasks[0]
		if row.Task != "topic" || row.Workflow != "small" || row.Stage != "unknown" || (row.DailyError != "") == brokenRoot {
			t.Fatal("shared row lost or conflated with failed daily history", res.Output())
		}
	}
}

func TestTerminalResumeCarriesScopeAndWorkerRecoveryWithoutUsage(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "recover-review"}
	f.call(0, "open", "--entry", "implement", "--until", "deliver", "--repair", "--commit",
		"--publish-to", "origin/topic", "--pr-base", "main", "--require-review", "--review-limit", "3",
		"--time-limit", "1h", "--finish-reserve", "30s", "--request", "Make the response ready and publish it")
	f.record("intake", "host", "Check the response bytes")
	r.Write("response.txt", "ready\n")
	f.record("implement", "host", "Implemented the response", "--claim-review", "codex")
	ticket := f.state.Ticket
	r.Write(".git/kitsu/tokens", "damaged archive")
	for _, host := range []string{"codex", "claude"} {
		handoff := stageJSON[struct{ Prompt string }](t, r.Kitsu("task", "start", "--resume", f.id, "--host", host, "--json"))
		if !strings.Contains(handoff.Prompt, "`kitsu task next "+f.id+"`") {
			t.Fatalf("%s resume handoff: %s", host, handoff.Prompt)
		}
	}
	before := r.Kitsu("task", "next", f.id)
	for _, want := range []string{
		"--entry implement", "--repair", "--commit", "--publish-to origin/topic", "--pr-base main",
		"--require-review", "Review starts: 1 of 3", "Deadline:", "Reviewer return by:",
		"kitsu-task: recover-review review 1", "kitsu task show recover-review --review-ticket " + ticket,
		"Make the response ready and publish it", "Implemented the response", "Action ticket: " + ticket,
	} {
		if before.Code != 0 || !strings.Contains(before.Stdout, want) {
			t.Errorf("resume omitted %q: %s", want, before.Output())
		}
	}
	if strings.Contains(before.Output(), "archive") {
		t.Fatal("ordinary resume diagnosed unrelated usage", before.Output())
	}
	f.call(0, "claim", "--ticket", ticket, "--actor", "host", "--worker", "linked-reviewer")
	after := r.Kitsu("task", "next", f.id)
	if after.Code != 0 || !strings.Contains(after.Stdout, "Native reviewer: linked-reviewer") {
		t.Fatal("resume lost linked worker", after.Output())
	}
	sendNativeReviewEvent(t, r, "codex", "SubagentStart", "host", "linked-reviewer", "")
	report := reservedNativeReview(t, r, f.id, ticket)
	report.Decision, report.Note = "approve", "Checked the response"
	if got := nativeReviewEvent(t, r, "codex", "SubagentStop", "host", "linked-reviewer", reviewMessage(t, report)); got.Code != 1 || !strings.Contains(got.Output(), "tokens") {
		t.Fatal("expected the damaged usage archive to fail independently", got.Output())
	}
	f.call(0, "record", "review", "--ticket", ticket, "--observed-review")
	if f.state.Next != "verify" {
		t.Fatal("failed to advance the original reservation")
	}
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "response is ready", "--", "grep", "-qx", "ready", "response.txt")
	resumed := r.Kitsu("task", "next", f.id)
	if resumed.Code != 0 || !strings.Contains(resumed.Stdout, "Current review: Checked the response") ||
		!strings.Contains(resumed.Stdout, "next commit") || !strings.Contains(resumed.Stdout, "Verified on current content: true") {
		t.Fatal("commit handoff lost its review or verification evidence", resumed.Output())
	}
}
