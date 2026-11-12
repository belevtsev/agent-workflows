package gate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestPrePublishKnownForms holds one row per known command form, with the
// decision the gate gives it while the repository has a gating record. Later
// changes to command recognition add rows here, so a change in any known
// form shows up as a failing row.
func TestPrePublishKnownForms(t *testing.T) {
	repo := canonicalDir(t)
	// A repository whose path holds a space, reached only through a quoted
	// cd.
	spaced := filepath.Join(canonicalDir(t), "my proj")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := canonicalDir(t)
	open := func(id string) []taskflow.OpenRecord {
		return []taskflow.OpenRecord{record(id, "Open", "it has no passing cw task verify")}
	}
	store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open("task-k"), spaced: open("task-s")}}
	rows := []struct {
		name    string
		cwd     string
		command string
		want    gate.Permission
	}{
		// Oracle for the first three rows: nothing in the command commits or
		// pushes, so the gate neither denies nor asks.
		{"separator inside a quoted argument", repo, `echo "todo; git commit later"`, gate.Allow},
		{"pipe inside a quoted search pattern", repo, `rg "foo|git commit" docs`, gate.Allow},
		{"git push in a heredoc body", repo, "cat > notes.md <<'EOF'\nRelease steps:\ngit push origin main\nEOF", gate.Allow},
		// Oracle for the controls: each runs a real git commit or push in
		// the gated repository, so the gate denies.
		{"plain commit", repo, "git commit -m x", gate.Deny},
		{"commit after a quoted separator", repo, `echo "todo; later" && git commit -m "fix; git push"`, gate.Deny},
		{"push after a heredoc", repo, "cat > notes.md <<'EOF'\ngit push origin main\nEOF\ngit push origin main", gate.Deny},
		{"push after a pipeline with a quoted pattern", repo, `rg "foo|bar" docs | head -1 && git push`, gate.Deny},
		{"commit inside a quoted substitution", repo, `echo "$(git commit -m x)"`, gate.Deny},
		// Oracle: a quoted cd path with a space resolves to that directory,
		// whose record gates.
		{"cd to a quoted path with a space", elsewhere, `cd "` + spaced + `" && git commit -m x`, gate.Deny},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			git := &fakeGit{}
			git.repository(repo)
			git.repository(spaced)
			got := decide(row.command, row.cwd, true, true, git, store)
			if got.Permission != row.want {
				t.Fatalf("Decide(%q) = %q (reason %d bytes: %q), want %q", row.command, got.Permission, len(got.Reason), got.Reason, row.want)
			}
		})
	}
}
