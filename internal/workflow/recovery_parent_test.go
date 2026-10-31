package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryRefusesSymlinkedOwnedParentsBeforeAnyReversal(t *testing.T) {
	for _, affected := range []string{"skills", "codex", "legacy-source", "legacy-backup"} {
		t.Run(affected, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			installer := fixture.installer(true)
			legacy := affected == "legacy-source" || affected == "legacy-backup"
			if legacy {
				path := filepath.Join(fixture.paths.Home, ".codex", "skills", "typesafe-ai")
				installerCopyTree(t, filepath.Join(fixture.paths.Source, text(object(fixture.manifest["registrations"])["typesafe-ai"])), path)
				installer.TypeSafeLegacy = new("")
			} else {
				fixture.seedUserFiles(t)
			}
			t.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
			if _, err := installer.Setup(!legacy); err == nil {
				t.Fatal("fault did not interrupt installation")
			}
			t.Setenv("AGENT_WORKFLOWS_FAULT", "")
			parent := installer.skills
			switch affected {
			case "codex":
				parent = installer.Codex
			case "legacy-source":
				parent = filepath.Join(installer.Home, ".codex", "skills")
			case "legacy-backup":
				parent = filepath.Join(installer.State, "backups")
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
	for _, entrypoint := range []string{"perform", "write-observation"} {
		t.Run(entrypoint, func(t *testing.T) {
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
