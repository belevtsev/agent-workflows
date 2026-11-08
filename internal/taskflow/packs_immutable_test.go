package taskflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// B25 (P3a-1 A23): Load refuses a stored task whose latest revision's packs
// member differs from revision 1's, with exactly "immutable task baseline
// changed", and cw task verify fails with exit code 1. Fails when the loader
// stops comparing packs. The control row re-encodes the revision with packs
// unchanged and must still load, so the edit itself is not what is refused.
func TestPackSetImmutableInStorageB25(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	const refused = "immutable task baseline changed"
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		edit func(t *testing.T, packs json.RawMessage) json.RawMessage
	}{
		{"control: packs unchanged", nil},
		{"default selection, nothing applied", func(*testing.T, json.RawMessage) json.RawMessage {
			return json.RawMessage(`{"selection":"default","applied":[]}`)
		}},
		{"explicit selection, applied unchanged", func(t *testing.T, packs json.RawMessage) json.RawMessage {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(packs, &m); err != nil || string(m["selection"]) != `"default"` {
				t.Fatalf("precondition: stored packs %s (%v) do not have the default selection", packs, err)
			}
			m["selection"] = json.RawMessage(`"explicit"`)
			out, _ := json.Marshal(m)
			return out
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			repo, store := spRepo(t, map[string]string{"go.mod": "module example.com/m\n"})
			rec, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, builtIn)
			if err != nil || rec.Packs == nil || len(rec.Packs.Applied) != 1 {
				t.Fatalf("precondition: start with the go pack applied: %+v, %v", rec.Packs, err)
			}
			if _, err := taskflow.Run(ctx, rec.Directory, "unit"); err != nil {
				t.Fatalf("precondition: run a planned check: %v", err)
			}
			revisions, _ := filepath.Glob(filepath.Join(rec.Directory, "record-*.json"))
			slices.Sort(revisions)
			if len(revisions) < 2 {
				t.Fatalf("precondition: want at least two revisions, have %q", revisions)
			}
			last := revisions[len(revisions)-1]
			data, err := os.ReadFile(last)
			var members map[string]json.RawMessage
			if err != nil || json.Unmarshal(data, &members) != nil || members["packs"] == nil {
				t.Fatalf("precondition: cannot decode %s with a packs member: %v", last, err)
			}
			if c.edit != nil {
				members["packs"] = c.edit(t, members["packs"])
			}
			out, err := json.Marshal(members)
			if err != nil || os.WriteFile(last, out, 0o600) != nil {
				t.Fatalf("cannot rewrite %s: %v", last, err)
			}
			if info, err := os.Lstat(last); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("precondition: %s is no longer a regular 0600 file: %v, %v", last, info, err)
			}

			_, loadErr := taskflow.Load(rec.Directory)
			base := t.TempDir()
			var stdout, stderr bytes.Buffer
			vars := map[string]string{"HOME": base, "XDG_CONFIG_HOME": filepath.Join(base, "xdg-config"), "XDG_STATE_HOME": filepath.Join(base, "xdg-state")}
			verifyErr := cli.Execute(ctx, []string{"task", "verify", "--task", rec.Directory, "--config-dir", filepath.Join(base, "cfg")}, cli.Config{
				In: strings.NewReader(""), Out: &stdout, Err: &stderr,
				HomeDir: func() (string, error) { return base, nil }, WorkingDir: func() (string, error) { return base, nil },
				Getenv: func(k string) string { return vars[k] },
			})
			if c.edit == nil {
				if loadErr != nil || (verifyErr != nil && strings.Contains(verifyErr.Error(), refused)) {
					t.Fatalf("B25 control violated: re-encoding with packs unchanged gives Load %v, verify %v", loadErr, verifyErr)
				}
				return
			}
			if loadErr == nil || loadErr.Error() != refused {
				t.Errorf("B25 violated: Load after changing packs gives %v, want exactly %q", loadErr, refused)
			}
			if verifyErr == nil || !strings.Contains(verifyErr.Error(), refused) || cli.ExitCode(verifyErr) != 1 {
				t.Errorf("B25 violated: cw task verify gives %v (exit %d), want an error containing %q with exit 1", verifyErr, cli.ExitCode(verifyErr), refused)
			}
		})
	}
}
