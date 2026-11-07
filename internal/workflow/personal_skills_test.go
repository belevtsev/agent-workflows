package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func personalFixtureUpgrade(t *testing.T, fixture *installerFixture) (string, map[string]string) {
	t.Helper()
	maintained, err := LoadManifest(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest := clone(fixture.manifest)
	manifest["registrations"] = clone(object(manifest["registrations"]))
	catalog := PersonalSkillCatalog{Version: 1, Skills: map[string]PersonalSkillCatalogRecord{}}
	originals := map[string]string{}
	for _, name := range keys(object(maintained["registrations"])) {
		if object(manifest["registrations"])[name] != nil {
			continue
		}
		root := text(object(maintained["registrations"])[name])
		object(manifest["registrations"])[name] = root
		original := filepath.Join(fixture.base, "trusted original skills", name)
		installerWrite(t, filepath.Join(original, "SKILL.md"), "---\nname: "+name+"\ndescription: Original personal fixture.\n---\n\n# Original skill\n\n[Guide](references/guide.md)\n", 0o644)
		installerWrite(t, filepath.Join(original, "references", "guide.md"), "# Original guide\n\nPrivate original marker for "+name+".\n", 0o640)
		installerWrite(t, filepath.Join(original, "scripts", "helper.sh"), "#!/bin/sh\nprintf '%s\\n' original\n", 0o750)
		for relative, mode := range map[string]os.FileMode{".": 0o750, "references": 0o710, "scripts": 0o700} {
			if err := os.Chmod(filepath.Join(original, relative), mode); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := personalInventory(original)
		if err != nil {
			t.Fatal(err)
		}
		catalog.Skills[name] = PersonalSkillCatalogRecord{Inventory: entries}
		originals[name] = original
		// The managed source intentionally differs from the adoption input.
		// It must be the catalog, never the adapted source, that qualifies it.
		installerWrite(t, filepath.Join(fixture.source, root, "SKILL.md"), "---\nname: "+name+"\ndescription: Managed personal fixture adaptation.\n---\n\n# Managed skill\n", 0o644)
	}
	installerWrite(t, filepath.Join(fixture.source, personalCatalogName), string(legacyJSON(catalog)), 0o644)
	installerWrite(t, filepath.Join(fixture.source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
	sha := fixture.newRelease(t)
	fixture.manifest = manifest
	return sha, originals
}

func seedPersonalSkill(t *testing.T, original, destination string) []PersonalInventoryEntry {
	t.Helper()
	installerCopyTree(t, original, destination)
	entries, err := personalInventory(destination)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func assertPersonalDirectory(t *testing.T, target string, inventory []PersonalInventoryEntry) {
	t.Helper()
	actual, err := personalInventory(target)
	if err != nil || !slices.Equal(actual, inventory) {
		t.Fatalf("original directory was not restored exactly at %s: %v", target, err)
	}
}

func TestPersonalSkillsFreshRepeatedRestoreCustomRootsAndDryRun(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.paths.Codex = filepath.Join(fixture.base, "selected Codex home with spaces")
	if err := os.MkdirAll(fixture.paths.Codex, 0o700); err != nil {
		t.Fatal(err)
	}
	originalUser := fixture.seedUserFiles(t)
	sha, originals := personalFixtureUpgrade(t, fixture)
	i := nativeFixtureInstaller(t, fixture, sha, true)
	i.AdoptPersonalSkills = true
	roots := []string{i.skills, filepath.Join(i.Codex, "skills"), filepath.Join(i.Home, ".codex", "skills")}
	locations := map[string]string{}
	inventories := map[string][]PersonalInventoryEntry{}
	for index, name := range keys(object(fixture.manifest["registrations"])) {
		if original := originals[name]; original != "" {
			root := roots[index%len(roots)]
			if name == "archify" {
				root = i.skills
			}
			locations[name] = filepath.Join(root, name)
			inventories[name] = seedPersonalSkill(t, original, locations[name])
		}
	}
	before := installerSnapshot(t, fixture.base)
	ordinary := nativeFixtureInstaller(t, fixture, sha, false)
	if _, err := ordinary.Setup(true); err == nil {
		t.Fatal("ordinary setup adopted occupied personal names")
	}
	// The adoption preview neither stages the release nor copies the runtime;
	// the standalone report no longer lists typed registration changes.
	preview := nativeFixtureInstaller(t, fixture, sha, false)
	preview.AdoptPersonalSkills = true
	report, err := preview.Setup(true)
	if err != nil || report["dry_run"] != true || report["release"] == nil {
		t.Fatalf("adoption preview: %#v, %v", report, err)
	}
	installerUnchanged(t, fixture.base, before)
	if _, err = i.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := i.state(true)
	if err != nil || integer(state["version"]) != 4 || state["release"] != report["release"] || len(object(hostRecord(state, hostCodex)["registrations"])) != 36 {
		t.Fatalf("version-four installed state: %#v, %v", state, err)
	}
	for name := range originals {
		origin, err := i.validatePersonalOrigin(name, object(object(object(hostRecord(state, hostCodex)["registrations"])[name])["original"]))
		if err != nil || origin.Path != locations[name] || origin.Backup != i.personalBackup(name) {
			t.Fatalf("personal origin %s: %#v, %v", name, origin, err)
		}
		assertPersonalDirectory(t, origin.Backup, inventories[name])
		if strings.Contains(installerRead(t, i.statePath), "Private original marker") {
			t.Fatal("personal state stores raw original file bytes")
		}
	}
	before = installerSnapshot(t, fixture.base)
	if report, err = i.Setup(true); err != nil || report["changed"] != false {
		t.Fatalf("repeated adoption: %#v, %v", report, err)
	}
	installerUnchanged(t, fixture.base, before)
	for target := range originalUser {
		installerAppend(t, target, "\n# independent later bytes\n")
	}
	// Uninstall needs neither the bundle nor the candidate.
	if _, err = NewInstaller(Options{Paths: fixture.paths, Getenv: fixture.env.Getenv, Apply: true, Context: t.Context()}).Uninstall(); err != nil {
		t.Fatal(err)
	}
	for name := range originals {
		assertPersonalDirectory(t, locations[name], inventories[name])
		if exists(i.personalBackup(name)) {
			t.Fatalf("uninstall retained backup for %s", name)
		}
	}
	for target, original := range originalUser {
		if got := installerRead(t, target); got != original+"\n# independent later bytes\n" {
			t.Fatalf("uninstall changed unrelated bytes in %s", target)
		}
	}
}

func TestPersonalSkillsCompoundBoundariesAndResumedRecovery(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"archify", "production-plan"} {
		for _, action := range []string{"setup", "uninstall"} {
			prefix := "adopt-personal:"
			boundaries := []string{"rename", "link", "complete"}
			if action == "uninstall" {
				prefix, boundaries = "restore-personal:", []string{"unlink", "rename", "complete"}
			}
			for _, boundary := range boundaries {
				t.Run(name+"/"+action+"/"+boundary, func(t *testing.T) {
					t.Parallel()
					fixture := newInstallerFixture(t)
					sha, originals := personalFixtureUpgrade(t, fixture)
					i := nativeFixtureInstaller(t, fixture, sha, true)
					i.AdoptPersonalSkills = true
					location := filepath.Join(i.Codex, "skills", name)
					if name == "archify" {
						location = filepath.Join(i.skills, name)
					}
					original := seedPersonalSkill(t, originals[name], location)
					if action == "uninstall" {
						if _, err := i.Setup(true); err != nil {
							t.Fatal(err)
						}
					}
					beforeHome := installerSnapshot(t, i.Home)
					beforeState, _ := observe(i.statePath)
					label := "after-" + prefix + name
					if boundary != "complete" {
						label += ":" + boundary
					}
					fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
					if _, err := i.Execute(action); err == nil || !strings.Contains(err.Error(), label) {
						t.Fatalf("missing compound fault at %s: %v", label, err)
					}
					fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
					journal, err := readJSON(i.journalPath)
					if err != nil || integer(journal["version"]) != 4 {
						t.Fatalf("missing version-four journal: %v", err)
					}
					// Interrupt recovery at its own first compound boundary too.
					recoveryBoundary := "rename"
					if action == "setup" && boundary != "rename" {
						recoveryBoundary = "unlink"
					}
					if action == "uninstall" && boundary == "unlink" {
						recoveryBoundary = "link"
					}
					fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-personal:"+name+":"+recoveryBoundary)
					recovery := NewInstaller(Options{Paths: fixture.paths, Getenv: fixture.env.Getenv, Apply: true, Context: t.Context()})
					if _, err := recovery.Recover(); err == nil {
						t.Fatal("recovery compound fault did not interrupt")
					}
					fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
					recovery.Apply = false
					beforeRecovery := installerSnapshot(t, i.State)
					if _, err := recovery.Recover(); err != nil {
						t.Fatalf("resumed recovery preview: %v", err)
					}
					installerUnchanged(t, i.State, beforeRecovery)
					recovery.Apply = true
					if _, err := recovery.Recover(); err != nil {
						t.Fatalf("resumed recovery: %v", err)
					}
					installerUnchanged(t, i.Home, beforeHome)
					afterState, _ := observe(i.statePath)
					if !equal(beforeState, afterState) || exists(i.journalPath) {
						t.Fatal("compound recovery did not restore prior ownership")
					}
					if action == "setup" {
						assertPersonalDirectory(t, location, original)
					} else if _, err := recovery.Status(); err != nil {
						t.Fatalf("recovered installation: %v", err)
					}
				})
			}
		}
	}
}

func TestPersonalSkillsModifiedOccupiedAndDuplicateInputsRefuse(t *testing.T) {
	t.Parallel()
	for _, corruption := range []string{"valid", "contents", "extra", "mode", "symlink", "dangling", "duplicates", "occupied-backup", "unsafe-parent", "cancelled", "old-runtime"} {
		t.Run(corruption, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			sha, originals := personalFixtureUpgrade(t, fixture)
			i := nativeFixtureInstaller(t, fixture, sha, true)
			i.AdoptPersonalSkills = true
			location := filepath.Join(i.Codex, "skills", "production-plan")
			seedPersonalSkill(t, originals["production-plan"], location)
			switch corruption {
			case "contents":
				installerAppend(t, filepath.Join(location, "SKILL.md"), "Changed.\n")
			case "extra":
				installerWrite(t, filepath.Join(location, "extra.md"), "Uncataloged.\n", 0o644)
			case "mode":
				if err := os.Chmod(location, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("SKILL.md", filepath.Join(location, "linked")); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				if err := os.Rename(location, location+" real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing", location); err != nil {
					t.Fatal(err)
				}
			case "duplicates":
				seedPersonalSkill(t, originals["production-plan"], filepath.Join(i.skills, "production-plan"))
			case "occupied-backup":
				installerWrite(t, i.personalBackup("production-plan"), "Occupied.\n", 0o600)
			case "unsafe-parent":
				parent := filepath.Dir(location)
				if err := os.Rename(parent, parent+" real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent+" real", parent); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				i.Context = ctx
			case "old-runtime":
				installerWrite(t, i.RuntimeCandidate, fixtureManagerScript(t, "cw-manager-v5", fixture.release(t, sha).identity), 0o755)
			}
			before := installerSnapshot(t, fixture.base)
			_, err := i.Setup(true)
			if corruption == "valid" {
				// The unchanged catalogued skill is adopted, so each refusal
				// below comes from its own corruption.
				if err != nil {
					t.Fatalf("valid adoption: %v", err)
				}
				state := mustRegistrationState(t, i)
				if _, err := i.validatePersonalOrigin("production-plan", object(object(object(hostRecord(state, hostCodex)["registrations"])["production-plan"])["original"])); err != nil {
					t.Fatalf("valid adoption recorded no personal origin: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("adoption accepted %s", corruption)
			}
			switch corruption {
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled adoption: %v", err)
				}
			case "old-runtime":
				if !strings.Contains(err.Error(), "standalone manager lacks the required capability") {
					t.Fatalf("old runtime refused for another reason: %v", err)
				}
			default:
				// Each input refusal names the skill or the directory it is in.
				if !strings.Contains(err.Error(), "production-plan") && !strings.Contains(err.Error(), filepath.Dir(location)) {
					t.Fatalf("%s refused for another reason: %v", corruption, err)
				}
			}
			installerUnchanged(t, fixture.base, before)
			if exists(i.journalPath) || exists(i.current) {
				t.Fatal("invalid adoption reached activation")
			}
		})
	}
}

func TestPersonalSkillsAdditiveAbsentOriginRollbackAndAdoptedRemovalRefusal(t *testing.T) {
	t.Parallel()
	for _, historical := range []int{9, 12} {
		for _, adoption := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/adoption=%t", historical, adoption), func(t *testing.T) {
				t.Parallel()
				fixture := newInstallerFixture(t)
				if historical == 9 {
					fixture = historicalRegistrationFixture(t)
				}
				old := nativeFixtureInstaller(t, fixture, fixture.first, true)
				if _, err := old.Setup(true); err != nil {
					t.Fatal(err)
				}
				prior := mustRegistrationState(t, old)
				sha, originals := personalFixtureUpgrade(t, fixture)
				i := nativeFixtureInstaller(t, fixture, sha, true)
				i.AdoptPersonalSkills = adoption
				if adoption {
					seedPersonalSkill(t, originals["production-plan"], filepath.Join(i.Codex, "skills", "production-plan"))
				}
				// Update is the command that activates the newer bundle.
				if _, err := i.Update(); err != nil {
					t.Fatal(err)
				}
				state := mustRegistrationState(t, i)
				if len(object(hostRecord(state, hostCodex)["registrations"])) != 36 || integer(state["version"]) != 4 || stateRevision(state) != sha {
					t.Fatal("additive update did not activate 36 v4 registrations")
				}
				for name, raw := range object(hostRecord(prior, hostCodex)["registrations"]) {
					if !equal(object(raw)["original"], object(object(hostRecord(state, hostCodex)["registrations"])[name])["original"]) {
						t.Fatalf("changed historical origin %s", name)
					}
				}
				if adoption {
					before := installerSnapshot(t, fixture.base)
					if _, err := i.Rollback(); err == nil || !strings.Contains(err.Error(), "rollback cannot remove adopted registration") {
						t.Fatalf("adopted removal did not refuse: %v", err)
					}
					installerUnchanged(t, fixture.base, before)
				} else {
					if _, err := i.Rollback(); err != nil {
						t.Fatal(err)
					}
					state = mustRegistrationState(t, i)
					if len(object(hostRecord(state, hostCodex)["registrations"])) != historical || object(object(state["manager"])["runtime"])["revision"] != sha || integer(state["version"]) != 4 || stateRevision(state) != fixture.first {
						t.Fatal("rollback changed manager identity or historical registration subset")
					}
				}
			})
		}
	}
}

func TestPersonalSkillsRecoveryAfterRuntimeReversalWithoutSource(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{ManagerProtocol} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			old := nativeFixtureInstallerProtocol(t, fixture, fixture.first, protocol, true)
			if _, err := old.Setup(true); err != nil {
				t.Fatal(err)
			}
			oldRuntime := fixtureRuntimeDir(t, fixture.paths.State, fixture.first)
			beforeState := installerRead(t, old.statePath)
			sha, originals := personalFixtureUpgrade(t, fixture)
			i := nativeFixtureInstaller(t, fixture, sha, true)
			i.AdoptPersonalSkills = true
			location := filepath.Join(i.skills, "archify")
			original := seedPersonalSkill(t, originals["archify"], location)
			// Update is the command that switches the runtime to the newer one.
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
			if _, err := i.Update(); err == nil || !strings.Contains(err.Error(), "injected failure at after-state") {
				t.Fatalf("update fault did not interrupt: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			recovery := NewInstaller(Options{Paths: fixture.paths, Getenv: fixture.env.Getenv, Apply: true, Context: t.Context()})
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-manager-runtime")
			if _, err := recovery.Recover(); err == nil || !strings.Contains(err.Error(), "after-recover-manager-runtime") {
				t.Fatalf("runtime reversal fault: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			target, err := os.Readlink(i.runtimeCurrent())
			if err == nil && !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(i.runtimeCurrent()), target)
			}
			if err != nil || filepath.Clean(target) != oldRuntime {
				t.Fatalf("enrolled manager was not reverted to old runtime: %q, %v", target, err)
			}
			if _, err := recovery.Recover(); err != nil {
				t.Fatal(err)
			}
			if got := installerRead(t, i.statePath); got != beforeState {
				t.Fatal("resumed recovery changed old state bytes")
			}
			assertPersonalDirectory(t, location, original)
			if _, err := recovery.Status(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPersonalSkillsUninstallChangedDestinationOrBackupRefuses(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"destination", "backup"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			sha, originals := personalFixtureUpgrade(t, fixture)
			i := nativeFixtureInstaller(t, fixture, sha, true)
			i.AdoptPersonalSkills = true
			location := filepath.Join(i.Codex, "skills", "production-plan")
			seedPersonalSkill(t, originals["production-plan"], location)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			if change == "destination" {
				installerWrite(t, location, "New personal contents.\n", 0o600)
			} else {
				installerAppend(t, filepath.Join(i.personalBackup("production-plan"), "SKILL.md"), "Changed backup.\n")
			}
			before := installerSnapshot(t, fixture.base)
			if _, err := i.Uninstall(); err == nil {
				t.Fatal("uninstall overwrote changed personal ownership")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestPersonalSkillCrossFilesystemRefuses(t *testing.T) {
	t.Parallel()
	adopt := func(t *testing.T, state string) (*Installer, string, []PersonalInventoryEntry, error) {
		t.Helper()
		fixture := newInstallerFixture(t)
		sha, originals := personalFixtureUpgrade(t, fixture)
		if state != "" {
			fixture.paths.State = state
		}
		i := nativeFixtureInstaller(t, fixture, sha, true)
		i.AdoptPersonalSkills = true
		location := filepath.Join(i.skills, "archify")
		original := seedPersonalSkill(t, originals["archify"], location)
		_, err := i.Setup(true)
		return i, location, original, err
	}
	// On one filesystem the same adoption succeeds, so the refusal below
	// comes from the second filesystem.
	t.Run("same filesystem", func(t *testing.T) {
		t.Parallel()
		i, _, original, err := adopt(t, "")
		if err != nil {
			t.Fatalf("same-filesystem adoption: %v", err)
		}
		assertPersonalDirectory(t, i.personalBackup("archify"), original)
	})
	t.Run("second filesystem", func(t *testing.T) {
		t.Parallel()
		if !exists("/dev/shm") {
			t.Skip("second filesystem unavailable on this platform")
		}
		state, err := os.MkdirTemp("/dev/shm", "cw-personal-fixture-")
		if err != nil {
			t.Skipf("second filesystem unavailable: %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(state) })
		i, location, original, err := adopt(t, state)
		if err == nil || !strings.Contains(err.Error(), "same filesystem") {
			t.Fatalf("cross-filesystem adoption did not refuse: %v", err)
		}
		assertPersonalDirectory(t, location, original)
		if exists(i.journalPath) || exists(i.current) {
			t.Fatal("cross-filesystem adoption reached activation")
		}
	})
}

func TestPersonalSkillCancellationBetweenCompoundStepsRecovers(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	sha, originals := personalFixtureUpgrade(t, fixture)
	i := nativeFixtureInstaller(t, fixture, sha, true)
	i.AdoptPersonalSkills = true
	location := filepath.Join(i.skills, "archify")
	original := seedPersonalSkill(t, originals["archify"], location)
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-adopt-personal:archify:rename")
	if _, err := i.Setup(true); err == nil || !strings.Contains(err.Error(), "injected failure at after-adopt-personal:archify:rename") {
		t.Fatalf("compound pause did not interrupt: %v", err)
	}
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	i.Context = ctx
	if _, err := i.Recover(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery: %v", err)
	}
	i.Context = t.Context()
	if _, err := i.Recover(); err != nil {
		t.Fatal(err)
	}
	assertPersonalDirectory(t, location, original)
}
