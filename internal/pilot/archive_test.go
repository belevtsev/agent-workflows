package pilot

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var archiveAssets = flag.Bool("pilot-archive-assets", false, "retain frozen non-credential assets; no model execution")

func TestPilotArchiveAssets(t *testing.T) {
	if !*archiveAssets || *pilotOutput == "" {
		t.Skip("explicit private pilot output and archive flag required")
	}
	var m manifest
	if err := readJSON(filepath.Join(*pilotOutput, "frozen-manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	assets := append(slices.Clone(m.Host.Assets), m.Grader.Assets...)
	assets = append(assets, m.CoordinatorPrompt, m.Calibration)
	for _, a := range m.ObservationPrompts {
		assets = append(assets, a)
	}
	for _, c := range m.Cases {
		assets = append(assets, c.Prompt)
	}
	// home_files deliberately remain excluded: they contain authentication.
	root := filepath.Join(*pilotOutput, "frozen-asset-content")
	index := map[string]asset{}
	for _, a := range assets {
		digest, err := hashFile(a.Path)
		if err != nil || digest != a.SHA256 {
			t.Fatalf("frozen asset changed before archival: %s: %v", a.Path, err)
		}
		destination := filepath.Join(root, digest)
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			resolved, err := filepath.EvalSymlinks(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := copyRegular(resolved, destination, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		copied, err := hashFile(destination)
		if err != nil || copied != digest {
			t.Fatalf("archived bytes differ: %s: %v", a.Path, err)
		}
		index[a.Path] = asset{destination, copied}
	}
	if err := writeJSON(filepath.Join(*pilotOutput, "frozen-asset-index.json"), index, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained %d non-credential frozen assets", len(index))
}
