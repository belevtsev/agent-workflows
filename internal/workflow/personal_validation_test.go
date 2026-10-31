package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonalBackupAncestorSyncFailurePreventsOriginalMove(t *testing.T) {
	f := newInstallerFixture(t)
	_, originals := personalFixtureUpgrade(t, f)
	i := f.installer(true)
	origin := filepath.Join(i.skills, "archify")
	inventory := seedPersonalSkill(t, originals["archify"], origin)
	backup := i.personalBackup("archify")
	failedSync := filepath.Join(i.State, "backups")
	sentinel := errors.New("fixture directory fsync failure")
	err := renamePersonalDirectoryWithSync(origin, backup, func(directory string) error {
		if directory == failedSync {
			if !exists(origin) || exists(backup) {
				t.Fatal("original moved before its new backup ancestry was durable")
			}
			return sentinel
		}
		return syncDir(directory)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("new backup ancestor was not synced before rename: %v", err)
	}
	assertPersonalDirectory(t, origin, inventory)
	if exists(backup) {
		t.Fatal("failed parent durability check moved the original")
	}
}

func TestPersonalStateNestedInsideOriginalRefusesBeforeStaging(t *testing.T) {
	f := newInstallerFixture(t)
	sha, originals := personalFixtureUpgrade(t, f)
	origin := filepath.Join(f.paths.Home, ".agents", "skills", "archify")
	inventory := seedPersonalSkill(t, originals["archify"], origin)
	f.paths.State = filepath.Join(origin, "private state")
	i := nativeFixtureInstaller(t, f, sha, true)
	i.AdoptPersonalSkills = true
	before := installerSnapshot(t, f.base)
	if _, err := i.Setup(true); err == nil || !strings.Contains(err.Error(), "mutation paths overlap") {
		t.Fatalf("nested state path was accepted before staging: %v", err)
	}
	installerUnchanged(t, f.base, before)
	assertPersonalDirectory(t, origin, inventory)
}

func personalCatalogFixture(t *testing.T) *suiteFixture {
	t.Helper()
	f := newSuiteFixture(t)
	f.manifest["version"], f.manifest["adoption_catalog"] = 3, personalCatalogName
	entries, err := personalInventory(filepath.Join(f.root, "skills", "example"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := PersonalSkillCatalog{Version: 1, Skills: map[string]PersonalSkillCatalogRecord{"example": {Inventory: entries}}}
	f.write(t, personalCatalogName, string(legacyJSON(catalog)))
	f.writeManifest(t)
	return f
}

func TestPersonalCatalogStrictSchemaAndCompleteInventories(t *testing.T) {
	for _, invalid := range []string{"version", "unknown-member", "unregistered", "unordered", "duplicate", "root", "parent", "symlink", "negative-mode", "fractional-mode", "missing-mode", "directory-hash", "missing-hash", "invalid-hash", "traversal", "absolute", "backslash", "empty-inventory", "raw-data"} {
		t.Run(invalid, func(t *testing.T) {
			f := personalCatalogFixture(t)
			catalog, err := readJSON(filepath.Join(f.root, personalCatalogName))
			if err != nil {
				t.Fatal(err)
			}
			skills := object(catalog["skills"])
			inventory := sequence(object(skills["example"])["inventory"])
			root, file := object(inventory[0]), object(inventory[1])
			switch invalid {
			case "version":
				catalog["version"] = 2
			case "unknown-member":
				catalog["extra"] = true
			case "unregistered":
				skills["unregistered"] = skills["example"]
			case "unordered":
				inventory[0], inventory[1] = inventory[1], inventory[0]
			case "duplicate":
				object(skills["example"])["inventory"] = append(inventory, clone(file))
			case "root":
				root["path"] = "root"
			case "parent":
				file["path"] = "missing/SKILL.md"
			case "symlink":
				file["kind"] = "symlink"
			case "negative-mode":
				file["mode"] = -1
			case "fractional-mode":
				file["mode"] = 420.5
			case "missing-mode":
				delete(file, "mode")
			case "directory-hash":
				root["sha256"] = ""
			case "missing-hash":
				delete(file, "sha256")
			case "invalid-hash":
				file["sha256"] = strings.Repeat("G", 64)
			case "traversal":
				file["path"] = "../SKILL.md"
			case "absolute":
				file["path"] = "/SKILL.md"
			case "backslash":
				file["path"] = `references\SKILL.md`
			case "empty-inventory":
				object(skills["example"])["inventory"] = []any{}
			case "raw-data":
				file["data"] = "private bytes"
			}
			f.write(t, personalCatalogName, string(legacyJSON(catalog)))
			if _, err := LoadManifest(f.root); err == nil {
				t.Fatalf("catalog accepted %s", invalid)
			}
		})
	}
	f := personalCatalogFixture(t)
	if _, err := ValidateSuite(f.root); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{"other-origins.json", "../personal-skill-origins.json", "/personal-skill-origins.json"} {
		f.manifest["adoption_catalog"] = wrong
		f.writeManifest(t)
		if _, err := LoadManifest(f.root); err == nil {
			t.Fatalf("catalog accepted filename %q", wrong)
		}
	}
}

func interruptedPersonalFixture(t *testing.T, label string) (*installerFixture, *Installer) {
	t.Helper()
	f := newInstallerFixture(t)
	sha, originals := personalFixtureUpgrade(t, f)
	i := nativeFixtureInstaller(t, f, sha, true)
	i.AdoptPersonalSkills = true
	seedPersonalSkill(t, originals["archify"], filepath.Join(i.skills, "archify"))
	t.Setenv("AGENT_WORKFLOWS_FAULT", label)
	if _, err := i.Setup(true); err == nil || !strings.Contains(err.Error(), label) {
		t.Fatalf("personal fixture interruption %s: %v", label, err)
	}
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	return f, i
}

func TestPersonalJournalEnumeratesCompoundPathsAndRejectsInterOperationOverlap(t *testing.T) {
	for _, field := range []string{"operations", "recovery_operations"} {
		for _, owned := range []string{"origin_path", "backup_path", "registration_path", "nested"} {
			t.Run(field+"/"+owned, func(t *testing.T) {
				f, i := interruptedPersonalFixture(t, "after-adopt-personal:archify:rename")
				if field == "recovery_operations" {
					t.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-personal:archify:rename")
					if _, err := i.Recover(); err == nil {
						t.Fatal("recovery interruption missing")
					}
					t.Setenv("AGENT_WORKFLOWS_FAULT", "")
				}
				journal, err := readJSON(i.journalPath)
				if err != nil {
					t.Fatal(err)
				}
				ops := sequence(journal[field])
				var compound Object
				for _, raw := range ops {
					if object(raw)["kind"] == "personal_skill" {
						compound = object(raw)
						break
					}
				}
				if compound == nil {
					t.Fatal("fixture missing compound operation")
				}
				target := text(object(compound["personal"])[owned])
				if owned == "nested" {
					target = filepath.Join(text(object(compound["personal"])["origin_path"]), "nested")
				}
				ops = append(ops, pathOperation(target, Object{"kind": "absent"}, Object{"kind": "absent"}, "forged-overlap"))
				journal[field] = ops
				installerWrite(t, i.journalPath, string(legacyJSON(seal(journal))), 0o600)
				before := installerSnapshot(t, f.base)
				if _, err := i.Recover(); err == nil || !strings.Contains(err.Error(), "overlapping mutation paths") {
					t.Fatalf("journal overlap accepted: %v", err)
				}
				installerUnchanged(t, f.base, before)
			})
		}
	}
}

func TestPersonalRecoveryRejectsChangedRuntimeOrOwnedDirectoryBeforeReversal(t *testing.T) {
	for _, changed := range []string{"binary", "locator", "receipt", "receipt-mode", "extra-file", "forged-old-capability", "backup", "registration"} {
		t.Run(changed, func(t *testing.T) {
			f, i := interruptedPersonalFixture(t, "after-adopt-personal:archify:link")
			dir := i.runtimeDir(i.RuntimeIdentity.Revision)
			switch changed {
			case "binary":
				installerAppend(t, filepath.Join(dir, "cw"), "changed\n")
			case "locator":
				locator, err := readJSON(filepath.Join(dir, "locator.json"))
				if err != nil {
					t.Fatal(err)
				}
				locator["source"] = filepath.Join(f.base, "other source")
				installerWrite(t, filepath.Join(dir, "locator.json"), string(legacyJSON(locator)), 0o600)
			case "receipt":
				receipt, err := readJSON(filepath.Join(dir, "receipt.json"))
				if err != nil {
					t.Fatal(err)
				}
				receipt["manager_protocol"] = "cw-manager-v5"
				installerWrite(t, filepath.Join(dir, "receipt.json"), string(legacyJSON(receipt)), 0o600)
			case "receipt-mode":
				if err := os.Chmod(filepath.Join(dir, "receipt.json"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				installerWrite(t, filepath.Join(dir, "extra"), "Changed cache.\n", 0o600)
			case "forged-old-capability":
				binary := []byte("#!/bin/sh\nprintf '%s\\n' cw-manager-v5\n")
				installerWrite(t, filepath.Join(dir, "cw"), string(binary), 0o755)
				receipt, err := readJSON(filepath.Join(dir, "receipt.json"))
				if err != nil {
					t.Fatal(err)
				}
				receipt["sha256"] = hash(binary)
				installerWrite(t, filepath.Join(dir, "receipt.json"), string(legacyJSON(seal(receipt))), 0o600)
			case "backup":
				installerAppend(t, filepath.Join(i.personalBackup("archify"), "SKILL.md"), "Changed original.\n")
			case "registration":
				if err := os.Remove(filepath.Join(i.skills, "archify")); err != nil {
					t.Fatal(err)
				}
				installerWrite(t, filepath.Join(i.skills, "archify"), "Changed registration.\n", 0o600)
			}
			before := installerSnapshot(t, f.base)
			if _, err := i.Recover(); err == nil {
				t.Fatalf("recovery overwrote changed %s", changed)
			}
			installerUnchanged(t, f.base, before)
		})
	}
}
