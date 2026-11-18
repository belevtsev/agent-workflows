package harness_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestIdentifyAndResumeDailyTask(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "task-opaque-first"}
	request := "Preserve quoted fields\n" + strings.Repeat("é", 180)
	f.call(0, "open", "--entry", "plan", "--until", "deliver", "--repair", "--request", request)
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
