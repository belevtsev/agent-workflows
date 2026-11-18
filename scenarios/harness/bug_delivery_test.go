package harness_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestTicketLinkedDelivery(t *testing.T) {
	for _, kind := range []string{"markdown", "supplied", "url", "github"} {
		t.Run(kind, func(t *testing.T) { checkTicketDelivery(t, kind, false) })
	}
}

func TestTicketReceiptAfterCommitHookChangedTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable hook")
	}
	checkTicketDelivery(t, "markdown", true)
}

func checkTicketDelivery(t *testing.T, kind string, formatter bool) {
	r := kitsutest.NewRepo(t)
	r.Write("output.txt", "broken\n")
	r.Git("add", ".")
	r.Git("commit", "-qm", "baseline")
	const body = "# Export is broken\nReturn ready.\n"
	source := filepath.Join(t.TempDir(), "Export bug.md")
	refSuffix := "Export%20bug.md"
	sourceKind := kind
	f := flow{t: t, repo: r, id: "ticket-fix"}
	var args []string
	switch kind {
	case "supplied", "github":
		sourceKind = "supplied"
		source = "mcp://company-tracker/private-route/EXP-14?access_token=private-query#private-fragment"
		if kind == "github" {
			source = "https://github.com/example/project/issues/42?access_token=private-query#private-fragment"
		}
		refSuffix = source
		args = []string{"bug", "--stdin", "--from", source}
		result := r.Run(body, filepath.Join(kitsutest.Binary(t), "kitsu"), "task", "bug", f.id, "--stdin", "--from", source)
		if result.Code != 0 {
			t.Fatal(result.Output())
		}
	case "url":
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("direct ticket import contacted a loopback server")
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(server.Close)
		source = server.URL + "/EXP-14"
		refSuffix = source
		args = []string{"bug", "--from", source}
		rejected := r.Kitsu("task", "bug", f.id, "--from", source)
		if rejected.Code == 0 || !strings.Contains(rejected.Output(), "use a host connector with --stdin") ||
			r.Exists(".git/kitsu-stages/ticket-ticket-fix.md") || r.Exists(".git/kitsu-stages/task-harness-ticket-fix.json") {
			t.Fatal("blocked import created a task or omitted connector guidance", rejected.Output())
		}
		accepted := r.Run(body, filepath.Join(kitsutest.Binary(t), "kitsu"), "task", "bug", f.id, "--stdin", "--from", source)
		if accepted.Code != 0 {
			t.Fatal(accepted.Output())
		}
		// Model a direct URL snapshot already saved by an earlier Kitsu version.
		record := ".git/kitsu-stages/task-harness-ticket-fix.json"
		var saved harness.Task
		if err := json.Unmarshal([]byte(r.Read(record)), &saved); err != nil || saved.Bug == nil || saved.Bug.Source.URL != source {
			t.Fatalf("invalid host-supplied snapshot: %v, %v", saved.Bug, err)
		}
		saved.Bug.Source.Kind = "url"
		encoded, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		r.Write(record, string(encoded))
		server.Close()
	default:
		if err := os.WriteFile(source, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		args = []string{"bug", "--from", source}
		f.call(0, args...)
	}
	snapshot := r.Path(".git/kitsu-stages/ticket-ticket-fix.md")
	data, err := os.ReadFile(snapshot)
	if err != nil || !strings.Contains(string(data), refSuffix+">") || !strings.Contains(string(data), "# Export is broken") {
		t.Fatal(string(data), err)
	}
	if err := os.Remove(snapshot); err != nil {
		t.Fatal(err)
	}
	if kind == "markdown" {
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
	}
	f.call(0, args...) // retry without access to the original
	regenerated, err := os.ReadFile(snapshot)
	if err != nil || string(regenerated) != string(data) {
		t.Fatal("offline snapshot changed", err)
	}
	args[len(args)-1] = source + ".md"
	f.call(1, args...)
	f.record("intake", "coordinator", "The public output must be ready.")
	f.want("verify")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "file exists", "--", "git", "ls-files", "output.txt")
	f.want("verify")
	f.call(1, "receipt")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "output is ready", "--", "git", "-c", "alias.assert-output=!git grep -q ^ready$ -- output.txt && test ! -f .git/refuse-receipt", "assert-output")
	f.want("repair")
	red := f.state.Proof.Run
	f.call(0, "next")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	f.record("repair", "developer", "Fixed the output.")
	f.call(1, "test", "--ticket", f.state.Ticket, "--expect", "exists", "--replace-reason", "easier", "--", "git", "ls-files", "output.txt")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("commit")
	beforeRun, beforeTree := f.state.Proof.Run, r.Git("write-tree")
	if formatter {
		r.Git("config", "core.hooksPath", r.Path(".git/local-hooks"))
		writeCommitHook(t, r, "pre-commit", "printf 'normalized\\n' >> output.txt\ngit add output.txt\n")
		f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Fix export")
		f.call(1, "receipt")
		f.call(0, "next")
		f.want("verify")
		f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	} else {
		f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Fix export")
	}
	f.want("receipt")
	delivered := r.Git("rev-parse", "HEAD")
	r.Write(".git/refuse-receipt", "injected post-commit failure")
	draft := readReceiptMarkdown(f)
	markdownContains(t, draft, "Verified commit `"+delivered+"`", "Bug reproduction · Failed as expected", "Ticket delivery receipt pending")
	markdownOmits(t, draft, r.Path(""), "injected post-commit failure")
	f.call(1, "receipt")
	f.call(0, "next")
	f.want("receipt")
	if err := os.Remove(r.Path(".git/refuse-receipt")); err != nil {
		t.Fatal(err)
	}
	result := r.Kitsu("task", "receipt", f.id, "--json")
	var receipt harness.DeliveryReceipt
	if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil || result.Code != 0 {
		t.Fatal(result.Output(), err)
	}
	if receipt.Commit != delivered || receipt.Source.Kind != sourceKind || !strings.HasSuffix(receipt.Source.URL, refSuffix) || len(receipt.Source.SHA256) != 64 || receipt.Reproduction.ID != red || receipt.BeforeCommit.ID == receipt.OnCommit.ID || len(receipt.OnCommit.Digest) != 64 {
		t.Fatalf("incorrect receipt: %+v", receipt)
	}
	if formatter && (receipt.BeforeCommitTree != beforeTree || receipt.Tree == beforeTree || receipt.BeforeCommit.ID != beforeRun || receipt.OnCommit.ID == f.state.Proof.Run) {
		t.Fatalf("did not distinguish pre-hook, post-hook and receipt runs: %+v", receipt)
	}
	shareable := readReceiptMarkdown(f)
	markdownContains(t, shareable, "Behavior on the delivered commit · Passed", receipt.Source.SHA256)
	markdownOmits(t, shareable, receipt.OnCommit.ID, receipt.Reproduction.ID, "refuse-receipt", "private-query", "private-fragment")
	if kind == "markdown" {
		markdownContains(t, shareable, "Ticket came from a local Markdown file")
		markdownOmits(t, shareable, "file:", source, "Export%20bug.md")
	}
	if kind == "supplied" || kind == "url" {
		markdownOmits(t, shareable, source, "company-tracker", "private-route")
	}
	if kind == "github" {
		markdownContains(t, shareable, "<https://github.com/example/project/issues/42>")
	}
	f.call(0, "next")
	f.want("done")
	r.Git("commit", "--allow-empty", "-qm", "unrelated later work")
	again := r.Kitsu("task", "receipt", f.id, "--json")
	if again.Code != 0 || again.Stdout != result.Stdout {
		t.Fatalf("historical receipt changed: %s", again.Output())
	}
}
