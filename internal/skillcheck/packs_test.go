package skillcheck_test

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// A10: the checked-in packs load, name known rules and match the embedded
// copies.
func TestRepositoryPacks(t *testing.T) {
	fsys := os.DirFS("../..")
	manifests, err := langpack.LoadManifests(fsys)
	if err != nil {
		t.Fatalf("A10 violated: packs do not load: %v", err)
	}
	hasGo := false
	for _, m := range manifests {
		hasGo = hasGo || m.ID == "go"
	}
	if !hasGo {
		t.Fatalf("A10 violated: the repository load holds no go pack: %v", manifests)
	}
	for _, m := range manifests {
		if m.EvidenceRule != "" && !langpack.KnownRule(m.EvidenceRule) {
			t.Errorf("A10 violated: pack %s names unknown evidence_rule %q", m.ID, m.EvidenceRule)
		}
	}
	embedded, err := fs.Glob(agentworkflows.Packs, "packs/*/pack.yaml")
	if err != nil || len(embedded) == 0 {
		t.Fatalf("A10 violated: no embedded pack files: %v", err)
	}
	for _, name := range embedded {
		want, err1 := fs.ReadFile(agentworkflows.Packs, name)
		got, err2 := fs.ReadFile(fsys, name)
		if err1 != nil || err2 != nil || !bytes.Equal(got, want) {
			t.Errorf("A10 violated: embedded %s differs from the file on disk (%v, %v)", name, err1, err2)
		}
	}
}
