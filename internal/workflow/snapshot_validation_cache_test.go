package workflow

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// suiteValidationRefusal is the text every command reports when an installed
// base snapshot no longer holds a valid skill suite.
const suiteValidationRefusal = "suite validation failed"

// TestResealedInvalidBaseSnapshotIsValidatedAgainInProcess accepts a base
// snapshot first, then turns its bytes into an invalid suite and reseals its
// receipt so the recorded inventory matches. A validation result remembered by
// the snapshot path alone would let the later commands pass; each must instead
// validate the changed snapshot again and refuse without changing any tree.
func TestResealedInvalidBaseSnapshotIsValidatedAgainInProcess(t *testing.T) {
	t.Parallel()
	f := newInstallerFixture(t)
	f.seedUserFiles(t)
	if _, err := f.installer(true).Setup(true); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := f.installer(true).Status(); err != nil {
		t.Fatalf("status of the unchanged installation: %v", err)
	}

	digest := f.release(t, f.first).identity.Digest
	bases := filepath.Join(f.paths.State, "bases")
	snapshot := filepath.Join(bases, digest)
	receiptPath := filepath.Join(bases, digest+".receipt.json")
	if info, err := os.Stat(snapshot); err != nil || !info.IsDir() {
		t.Fatalf("installed base snapshot %s: %v", snapshot, err)
	}

	// A registered skill whose front matter has no name makes the suite invalid.
	registrations := object(f.manifest["registrations"])
	names := slices.Sorted(maps.Keys(registrations))
	if len(names) == 0 {
		t.Fatal("fixture manifest registers no skills")
	}
	skill := filepath.Join(snapshot, filepath.FromSlash(text(registrations[names[0]])), "SKILL.md")
	invalid := []byte("---\ndescription: No name.\n---\n\n# Fixture skill\n")
	fixtureRewrite(t, skill, invalid)
	if data, err := os.ReadFile(skill); err != nil || !bytes.Equal(data, invalid) {
		t.Fatalf("snapshot skill %s was not changed: %q, %v", skill, data, err)
	}

	// Reseal the receipt so its inventory matches the changed bytes exactly.
	original, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := readJSON(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"version", "base", "inventory", "integrity_sha256"} {
		if _, present := receipt[member]; !present {
			t.Fatalf("base receipt has no %s member: %#v", member, receipt)
		}
	}
	current, err := inventory(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt["inventory"] = current
	fixtureRewrite(t, receiptPath, legacyJSON(seal(receipt)))
	if resealed, err := os.ReadFile(receiptPath); err != nil || bytes.Equal(resealed, original) {
		t.Fatalf("resealing did not record the changed inventory: %v", err)
	}

	before := installerSnapshot(t, f.base)
	for _, command := range []struct {
		name string
		run  func(*Installer) (Object, error)
	}{
		{"status", (*Installer).Status},
		{"uninstall", (*Installer).Uninstall},
		{"update", (*Installer).Update},
	} {
		if _, err := command.run(f.installer(true)); err == nil || !strings.Contains(err.Error(), suiteValidationRefusal) {
			t.Fatalf("%s accepted a resealed invalid base snapshot: %v", command.name, err)
		}
		installerUnchanged(t, f.base, before)
	}
}
