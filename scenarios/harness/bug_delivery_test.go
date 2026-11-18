package harness_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestTicketLinkedDelivery(t *testing.T) {
	for _, kind := range []string{"markdown", "supplied", "url"} {
		t.Run(kind, func(t *testing.T) { checkTicketDelivery(t, kind) })
	}
}

func checkTicketDelivery(t *testing.T, kind string) {
	r := kitsutest.NewRepo(t)
	r.Write("output.txt", "broken\n")
	r.Git("add", ".")
	r.Git("commit", "-qm", "baseline")
	const body = "# Export is broken\nReturn ready.\n"
	source := filepath.Join(t.TempDir(), "Export bug.md")
	refSuffix := "Export%20bug.md"
	f := flow{t: t, repo: r, id: "ticket-fix"}
	var args []string
	switch kind {
	case "supplied":
		source = "mcp://company-tracker/EXP-14"
		refSuffix = source
		args = []string{"bug", "--stdin", "--from", source}
		result := r.Run(body, filepath.Join(kitsutest.Binary(t), "kitsu"), "task", "bug", f.id, "--stdin", "--from", source)
		if result.Code != 0 {
			t.Fatal(result.Output())
		}
	case "url":
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(server.Close)
		source = server.URL + "/EXP-14"
		refSuffix = source
		args = []string{"bug", "--from", source}
		f.call(0, args...)
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
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Fix export")
	f.want("receipt")
	delivered := r.Git("rev-parse", "HEAD")
	r.Write(".git/refuse-receipt", "injected post-commit failure")
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
	if receipt.Commit != delivered || receipt.Source.Kind != kind || !strings.HasSuffix(receipt.Source.URL, refSuffix) || len(receipt.Source.SHA256) != 64 || receipt.Reproduction.ID != red || receipt.BeforeCommit.ID == receipt.OnCommit.ID || len(receipt.OnCommit.Digest) != 64 {
		t.Fatalf("incorrect receipt: %+v", receipt)
	}
	f.call(0, "next")
	f.want("done")
	r.Git("commit", "--allow-empty", "-qm", "unrelated later work")
	again := r.Kitsu("task", "receipt", f.id, "--json")
	if again.Code != 0 || again.Stdout != result.Stdout {
		t.Fatalf("historical receipt changed: %s", again.Output())
	}
}
