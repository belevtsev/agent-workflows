package harness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestTicketLinkedDelivery(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("output.txt", "broken\n")
	r.Git("add", ".")
	r.Git("commit", "-qm", "baseline")
	source := filepath.Join(t.TempDir(), "Export bug.md")
	if err := os.WriteFile(source, []byte("# Export is broken\nReturn ready.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := flow{t: t, repo: r, id: "ticket-fix"}
	f.call(0, "bug", "--from", source)
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	f.call(0, "bug", "--from", source) // retry without access to the original
	f.call(1, "bug", "--from", source+".md")
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
	if receipt.Commit != delivered || receipt.Source.Kind != "markdown" || !strings.HasSuffix(receipt.Source.URL, "Export%20bug.md") || len(receipt.Source.SHA256) != 64 || receipt.Reproduction.ID != red || receipt.BeforeCommit.ID == receipt.OnCommit.ID || len(receipt.OnCommit.Digest) != 64 {
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
