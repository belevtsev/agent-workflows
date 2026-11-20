package harness_test

import (
	_ "embed"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

//go:embed testdata/feature-export/export.go
var exportImplementation string

//go:embed testdata/feature-export/export_test.go
var exportTests string

const featureDeclaration = `version = 1
require_review = true

[[check]]
id = "unit"
run = ["go", "test", "./..."]
`

func TestVerifiedFeatureDelivery(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("go.mod", "module example.com/export\n\ngo 1.25\n")
	oldExport := "package export\nimport (\"io\"; \"fmt\"; \"strings\")\nfunc run(args []string, out io.Writer) error { _, err := fmt.Fprintln(out, strings.Join(args, \"\\n\")); return err }\n"
	r.Write("export.go", oldExport)
	r.Write("kitsu.toml", featureDeclaration)
	r.Git("add", ".")
	r.Git("commit", "-qm", "Text export")
	approval := approveCIRepo(t, r)
	f := flow{t: t, repo: r, id: "json-export"}
	f.call(0, "open", "--entry", "implement", "--until", "deliver", "--commit", "--request", "private feature request")
	brief := harness.FeatureBrief{Version: 1, Goal: "Offer JSON export", Scope: "private scope text", Criteria: []harness.Criterion{
		{ID: "json-empty", Expected: "An empty export is []", Check: "behavior"},
		{ID: "json-quotes", Expected: "Quotes round-trip", Check: "behavior"},
		{ID: "text-compatible", Expected: "Default output is byte-for-byte unchanged", Check: "check:unit"},
	}, Behavior: &harness.FeatureBehavior{Expect: "JSON output has stable empty and quoted representations",
		Argv: []string{"go", "test", "-run", "^TestJSONExport$"}}}
	path := writeFeatureFixture(t, brief)
	f.record("intake", "host", "Ready for implementation", "--feature-brief", path)
	r.Write("export.go", exportImplementation)
	r.Write("export_test.go", exportTests)
	r.Git("add", "export.go", "export_test.go")
	f.record("implement", "host", "Added JSON output and behavioral tests")
	f.review("fresh-reviewer", "Candidate implements all three criteria")
	f.want("verify")
	f.call(1, "test", "--ticket", f.state.Ticket, "--expect", "passes", "--", "go", "version")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("commit")
	oldTicket := f.state.Ticket
	if !f.state.Verified {
		t.Fatal("passing tests did not verify the feature")
	}

	if err := os.Remove(approval); err != nil {
		t.Fatal(err)
	}
	f.call(0, "next")
	if f.state.Verified {
		t.Fatal("removed check approval left the candidate verified")
	}
	f.call(1, "commit", "--ticket", oldTicket, "--message", "Export JSON")
	approveCIRepo(t, r)
	f.call(0, "next")
	f.want("commit")
	brief.Scope += " and explicitly retain default behavior"
	f.call(0, "record", "plan", "--feature-brief", writeFeatureFixture(t, brief), "--actor", "host", "--note", "Clarified compatibility scope", "--ticket", f.state.Ticket)
	f.want("implement")
	f.call(1, "commit", "--ticket", oldTicket, "--message", "Export JSON")
	if receipt := r.Kitsu("task", "receipt", f.id, "--markdown"); receipt.Code == 0 {
		t.Fatal("receipt accepted the previous brief's verification")
	}
	f.record("implement", "host", "The candidate covers the revised brief")
	f.want("review")
	f.review("second-reviewer", "Rechecked acceptance on the same candidate")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("commit")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Export JSON")
	f.want("done")

	body := readReceiptMarkdown(f)
	markdownContains(t, body, "Verified commit `"+r.Git("rev-parse", "HEAD")+"`", brief.Revision(),
		"Criterion `json-empty`", "Criterion `json-quotes`", "Criterion `text-compatible`", "Host-observed review", "| `unit` | Passed |")
	markdownOmits(t, body, "private feature request", "private scope text", r.Path(""), "^TestJSONExport$")
}

func TestFeatureDraftAndNamedCheck(t *testing.T) {
	r := kitsutest.Demo(t)
	approveCIRepo(t, r)
	f := flow{t: t, repo: r, id: "feature-draft"}
	f.call(0, "open", "--entry", "plan", "--until", "plan", "--request", "Plan an export")
	draft := harness.FeatureBrief{Version: 1, Goal: "Export entries", Scope: "Determine the desired format", Draft: true}
	f.record("intake", "host", "Format still needs clarification", "--feature-brief", writeFeatureFixture(t, draft))
	f.record("plan", "host", "Ask which format the consumer supports", "--feature-brief", writeFeatureFixture(t, draft))
	f.want("done")
	if f.state.Verified || r.Git("status", "--porcelain") != "" {
		t.Fatal("planning-only changed or verified the checkout")
	}
	f.call(0, "extend", "--until", "verify")
	f.want("plan")
	draft.Draft = false
	draft.Criteria = []harness.Criterion{{ID: "export", Expected: "Fixture exports the selected rows", Check: "check:unknown"}}
	f.call(1, "record", "plan", "--feature-brief", writeFeatureFixture(t, draft), "--actor", "host", "--note", "Unknown check", "--ticket", f.state.Ticket)
	draft.Criteria[0].Check = "check:unit"
	f.record("plan", "host", "Format confirmed", "--feature-brief", writeFeatureFixture(t, draft))
	f.record("implement", "host", "Existing behavior meets the accepted plan")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	markdownContains(t, readReceiptMarkdown(f), "Criterion `export`")
}

func writeFeatureFixture(t *testing.T, brief harness.FeatureBrief) string {
	t.Helper()
	data, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/feature.json"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFeatureBriefReplayRequiresIdenticalBrief(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "replay-feature"}
	f.call(0, "open", "--entry", "plan", "--until", "plan", "--request", "Plan export")
	ticket := f.state.Ticket
	brief := harness.FeatureBrief{Version: 1, Goal: "Export rows", Scope: "One format", Draft: true}
	args := []string{"record", "intake", "--ticket", ticket, "--actor", "host", "--note", "Draft", "--feature-brief"}
	path := writeFeatureFixture(t, brief)
	f.call(0, append(args, path)...)
	f.call(0, append(args, path)...)
	f.want("plan")
	brief.Scope = "Two formats"
	out := r.Kitsu(append(append([]string{"task", args[0], f.id}, args[1:]...), writeFeatureFixture(t, brief))...)
	if out.Code != 1 || !strings.Contains(out.Stderr, "already recorded with different results") {
		t.Fatal(out.Output())
	}
}
