package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryRefusesSymlinkedOwnedParentsBeforeAnyReversal(t *testing.T) {
	t.Parallel()
	for _, affected := range []string{"skills", "codex"} {
		t.Run(affected, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := fixture.installer(true)
			fixture.seedUserFiles(t)
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
			if _, err := installer.Setup(true); err == nil {
				t.Fatal("fault did not interrupt installation")
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			parent := installer.skills
			if affected == "codex" {
				parent = installer.Codex
			}
			foreign := filepath.Join(fixture.base, "foreign "+affected)
			if err := os.Rename(parent, foreign); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, parent); err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Recover(); err == nil {
				t.Fatal("recovery accepted a symlinked owned parent")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestMutationRechecksParentBeforeDeletion(t *testing.T) {
	t.Parallel()
	for _, entrypoint := range []string{"perform", "write-observation"} {
		t.Run(entrypoint, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := fixture.installer(true)
			parent := installer.skills
			path := filepath.Join(parent, "owned")
			installerWrite(t, path, "owned bytes", 0o600)
			observation, err := observe(path)
			if err != nil {
				t.Fatal(err)
			}
			operation := pathOperation(path, observation, Object{"kind": "absent"}, "late-delete")
			foreign := filepath.Join(fixture.base, "foreign late parent")
			if err = os.Rename(parent, foreign); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(foreign, parent); err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, fixture.base)
			if entrypoint == "perform" {
				err = installer.perform(operation)
			} else {
				err = writeObservation(path, Object{"kind": "absent"})
			}
			if err == nil {
				t.Fatal("late symlink parent allowed deletion outside the root")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}
