package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestFeatureHandoffReplaysInBothHosts(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			r := kitsutest.Demo(t)
			approveCIRepo(t, r)
			f := flow{t: t, repo: r, id: "resume"}
			f.call(0, "open", "--entry", "implement", "--until", "deliver", "--commit", "--request", "private implementation request")
			r.Write(".git/feature.json", `{"version":1,"goal":"Export data","scope":"Retain behavior","criteria":[{"id":"export","expected_behavior":"Unit checks cover exported rows","check":"check:unit"}]}`)
			f.record("intake", host, "Intake complete", "--feature-brief", r.Path(".git/feature.json"))
			r.Write("export.txt", "one row\n")
			r.Git("add", "export.txt")
			f.record("implement", host, "Added export")
			before := resumeFeatureCommand(t, r, f.id)
			f.call(0, before...)
			f.want("commit")
			run := f.state.Proof.Run
			f.call(0, before...)
			if f.state.Proof.Run != run {
				t.Fatal("replayed handoff reran the saved check")
			}

			staleCommit := f.state.Ticket
			r.Write("export.txt", "two rows\n")
			r.Git("add", "export.txt")
			f.call(1, "commit", "--ticket", staleCommit, "--message", "Deliver export")
			current := resumeFeatureCommand(t, r, f.id)
			f.call(0, current...)
			if f.state.Proof.Run == run {
				t.Fatal("changed content reused the earlier proof")
			}
			// Simulate termination after Git advanced the ref and before the
			// ledger recorded delivery. A resume observes the exact commit.
			r.Git("commit", "-qm", "Deliver export")
			head := r.Git("rev-parse", "HEAD")
			f.call(0, "next")
			f.want("done")
			f.call(0, "next")
			markdownContains(t, readReceiptMarkdown(f), "Verified commit `"+head+"`")
		})
	}
}

func resumeFeatureCommand(t *testing.T, r *kitsutest.Repo, task string) []string {
	t.Helper()
	out := r.Kitsu("task", "next", task, "--brief")
	var brief struct {
		NextCommand []string `json:"next_command"`
	}
	if out.Code != 0 || json.Unmarshal([]byte(out.Stdout), &brief) != nil ||
		len(brief.NextCommand) != 8 || strings.Contains(out.Stdout, "private implementation request") {
		t.Fatal(out.Output())
	}
	if brief.NextCommand[0] != "kitsu" || brief.NextCommand[1] != "task" || brief.NextCommand[3] != task {
		t.Fatalf("unexpected command %q", brief.NextCommand)
	}
	// flow.call supplies the task ID and --json itself.
	return append([]string{"test"}, brief.NextCommand[4:7]...)
}
