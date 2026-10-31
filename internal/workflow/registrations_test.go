package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var backendFixtureNames = []string{"backend-security-review", "go-pki-mtls", "protobuf-contracts"}

// Keep this lifecycle fixture historical as the maintained pack grows. Its
// initial immutable snapshot has the original ten registrations.
func historicalRegistrationFixture(t *testing.T) *installerFixture {
	t.Helper()
	f := newInstallerFixture(t)
	manifest := clone(f.manifest)
	manifest["registrations"] = clone(object(f.manifest["registrations"]))
	registrations := object(manifest["registrations"])
	for _, name := range backendFixtureNames {
		delete(registrations, name)
	}
	if !equal(manifest, f.manifest) {
		installerWrite(t, filepath.Join(f.paths.Source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
		f.git(t, "add", ".")
		f.git(t, "commit", "-qm", "Historical nine registrations")
		f.first = f.git(t, "rev-parse", "HEAD")
		f.manifest = manifest
	}
	if got := len(registrations); got != 9 {
		t.Fatalf("historical fixture registrations = %d, want nine", got)
	}
	return f
}

func addBackendFixtureRegistrations(t *testing.T, f *installerFixture) string {
	t.Helper()
	manifest := clone(f.manifest)
	manifest["registrations"] = clone(object(f.manifest["registrations"]))
	for _, name := range backendFixtureNames {
		root := "skills/" + name
		object(manifest["registrations"])[name] = root
		installerWrite(t, filepath.Join(f.paths.Source, root, "SKILL.md"), "---\nname: "+name+"\ndescription: Isolated additive registration lifecycle fixture.\n---\n\n# Fixture skill\n", 0o644)
	}
	installerWrite(t, filepath.Join(f.paths.Source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
	return f.newRelease(t)
}

func registrationChanges(t *testing.T, report Object) []RegistrationChange {
	t.Helper()
	changes, ok := report["registration_changes"].([]RegistrationChange)
	if !ok {
		t.Fatalf("missing typed registration changes: %#v", report)
	}
	return changes
}

func assertAddedRegistrations(t *testing.T, installer *Installer, state, prior Object) {
	t.Helper()
	if got := len(object(state["registrations"])); got != 13 {
		t.Fatalf("active registrations = %d, want thirteen", got)
	}
	for name, raw := range object(state["registrations"]) {
		item := object(raw)
		if old := object(object(prior["registrations"])[name]); old != nil {
			if !equal(item["original"], old["original"]) {
				t.Fatalf("activation changed original ownership: %s", name)
			}
		} else if !equal(item["original"], Object{"kind": "absent"}) {
			t.Fatalf("new registration was adopted: %s: %#v", name, item)
		}
		if target, err := os.Readlink(filepath.Join(installer.skills, name)); err != nil || target != item["target"] {
			t.Fatalf("registration %s = %q, %v", name, target, err)
		}
	}
}

func TestRegistrationFreshAndRepeatedInstallPlans(t *testing.T) {
	f := historicalRegistrationFixture(t)
	second := addBackendFixtureRegistrations(t, f)
	i := f.installer(false)
	calls := 0
	i.PrepareRuntime = func(context.Context, string, string, string) (RuntimeCandidate, error) {
		calls++
		t.Fatal("dry installation prepared a runtime")
		return RuntimeCandidate{}, nil
	}
	before := installerSnapshot(t, f.base)
	report, err := i.Setup(true)
	if err != nil || report["release"] != second || len(registrationChanges(t, report)) != 13 {
		t.Fatalf("fresh install preview: %#v, %v", report, err)
	}
	if calls != 0 {
		t.Fatal("dry installation downloaded a runtime")
	}
	installerUnchanged(t, f.base, before)
	i = f.installer(true)
	if _, err = i.Setup(true); err != nil {
		t.Fatal(err)
	}
	before = installerSnapshot(t, f.base)
	report, err = i.Setup(true)
	if err != nil || report["changed"] != false || len(registrationChanges(t, report)) != 0 {
		t.Fatalf("repeated install: %#v, %v", report, err)
	}
	installerUnchanged(t, f.base, before)
}

func TestRegistrationAdditiveInstallAndUpdateRollbackReadd(t *testing.T) {
	for _, action := range []string{"setup", "update"} {
		t.Run(action, func(t *testing.T) {
			f := historicalRegistrationFixture(t)
			original := f.seedUserFiles(t)
			origin := f.bareOrigin(t)
			i := f.installer(true)
			// Preserve both forms of historical origin through addition,
			// rollback, readdition, and selective uninstall.
			legacyCheckout := filepath.Join(f.base, "prior adopted checkout")
			installerCopyTree(t, f.paths.Source, legacyCheckout)
			adoptedTarget := filepath.Join(legacyCheckout, "skills", "code-review")
			if err := os.Symlink(adoptedTarget, filepath.Join(i.skills, "code-review")); err != nil {
				t.Fatal(err)
			}
			legacySkill := filepath.Join(i.Home, ".codex", "skills", "typesafe-ai")
			installerCopyTree(t, filepath.Join(legacyCheckout, "third_party", "typesafe-ai"), legacySkill)
			legacyInventory := installerSnapshot(t, legacySkill)
			agents := filepath.Join(f.paths.Codex, "AGENTS.md")
			original[agents] = installerRead(t, filepath.Join(legacyCheckout, text(f.manifest["global_instructions"]))) + original[agents]
			installerWrite(t, agents, original[agents], 0o640)
			i.MigrateFrom, i.TypeSafeLegacy = legacyCheckout, new("")
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			prior, err := i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			firstReceipt := installerRead(t, filepath.Join(i.State, "releases", f.first)+".receipt.json")
			second := addBackendFixtureRegistrations(t, f)
			before := installerSnapshot(t, f.base)
			preview := f.installer(false)
			report, err := preview.Execute(action)
			if err != nil || len(registrationChanges(t, report)) != 3 {
				t.Fatalf("additive preview: %#v, %v", report, err)
			}
			installerUnchanged(t, f.base, before)
			if action == "update" {
				f.git(t, "push", origin, "main")
				f.git(t, "reset", "--hard", f.first)
			}
			report, err = i.Execute(action)
			if err != nil || len(registrationChanges(t, report)) != 3 {
				t.Fatalf("additive activation: %#v, %v", report, err)
			}
			state, err := i.state(true)
			if err != nil || state["release"] != second {
				t.Fatalf("active state: %#v, %v", state, err)
			}
			assertAddedRegistrations(t, i, state, prior)
			secondReceipt := installerRead(t, filepath.Join(i.State, "releases", second)+".receipt.json")
			before = installerSnapshot(t, f.base)
			report, err = f.installer(false).Rollback()
			if err != nil || len(registrationChanges(t, report)) != 3 {
				t.Fatalf("rollback preview: %#v, %v", report, err)
			}
			for _, change := range registrationChanges(t, report) {
				if change.Action != "remove" || !slices.Contains(backendFixtureNames, change.Name) {
					t.Fatalf("unexpected rollback change: %#v", change)
				}
			}
			installerUnchanged(t, f.base, before)
			if _, err = i.Rollback(); err != nil {
				t.Fatal(err)
			}
			state, err = i.state(true)
			if err != nil || !equal(state["registrations"], prior["registrations"]) {
				t.Fatalf("rollback did not restore ten ownership rows: %#v, %v", state, err)
			}
			for _, name := range backendFixtureNames {
				if exists(filepath.Join(i.skills, name)) {
					t.Fatalf("rollback retained %s", name)
				}
			}
			if _, err = i.Execute(action); err != nil {
				t.Fatalf("forward readdition: %v", err)
			}
			state, err = i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			assertAddedRegistrations(t, i, state, prior)
			for sha, want := range map[string]string{f.first: firstReceipt, second: secondReceipt} {
				if got := installerRead(t, filepath.Join(i.State, "releases", sha)+".receipt.json"); got != want {
					t.Fatalf("activation rewrote immutable receipt %s", sha)
				}
			}
			unmanaged := filepath.Join(i.skills, "personal-unmanaged", "SKILL.md")
			installerWrite(t, unmanaged, "# Keep my personal skill\n", 0o640)
			for path := range original {
				installerAppend(t, path, "\n# independent later edit\n")
			}
			if _, err = i.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if got := installerRead(t, unmanaged); got != "# Keep my personal skill\n" {
				t.Fatal("selective uninstall removed an unmanaged skill")
			}
			for path, want := range original {
				if got := installerRead(t, path); got != want+"\n# independent later edit\n" {
					t.Fatalf("selective uninstall changed %s: got %q, want %q", path, got, want+"\n# independent later edit\n")
				}
			}
			if target, err := os.Readlink(filepath.Join(i.skills, "code-review")); err != nil || target != adoptedTarget {
				t.Fatalf("selective uninstall lost adopted raw link: %q, %v", target, err)
			}
			installerUnchanged(t, legacySkill, legacyInventory)
		})
	}
}

func TestRegistrationForwardRemovalsRenamesAndRootChangesRefuse(t *testing.T) {
	for _, change := range []string{"remove", "rename", "root"} {
		t.Run(change, func(t *testing.T) {
			f := historicalRegistrationFixture(t)
			origin := f.bareOrigin(t)
			i := f.installer(true)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			manifest := clone(f.manifest)
			manifest["registrations"] = clone(object(f.manifest["registrations"]))
			regs := object(manifest["registrations"])
			root := text(regs["code-review"])
			switch change {
			case "remove":
				delete(regs, "code-review")
			case "rename":
				delete(regs, "code-review")
				regs["renamed-review"] = root
				path := filepath.Join(f.paths.Source, root, "SKILL.md")
				installerWrite(t, path, strings.ReplaceAll(installerRead(t, path), "name: code-review", "name: renamed-review"), 0o644)
			case "root":
				regs["code-review"] = "skills/arbitrary-review-root"
				if err := os.Rename(filepath.Join(f.paths.Source, root), filepath.Join(f.paths.Source, text(regs["code-review"]))); err != nil {
					t.Fatal(err)
				}
			}
			installerWrite(t, filepath.Join(f.paths.Source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
			f.git(t, "add", ".")
			f.git(t, "commit", "-qm", "Incompatible registration transition")
			if _, err := ValidateSuite(f.paths.Source); err != nil {
				t.Fatalf("fixture must be a valid suite before compatibility rejection: %v", err)
			}
			before := installerSnapshot(t, f.paths.Home)
			if _, err := f.installer(false).Setup(true); err == nil || !strings.Contains(err.Error(), "registration names or roots changed") {
				t.Fatalf("setup accepted %s: %v", change, err)
			}
			if _, err := f.installer(false).Update(); err == nil || !strings.Contains(err.Error(), "registration names or roots changed") {
				t.Fatalf("forward update preview accepted %s: %v", change, err)
			}
			f.git(t, "push", origin, "main")
			f.git(t, "reset", "--hard", f.first)
			if _, err := i.Update(); err == nil || !strings.Contains(err.Error(), "registration names or roots changed") {
				t.Fatalf("update accepted %s: %v", change, err)
			}
			installerUnchanged(t, f.paths.Home, before)
			if got := f.git(t, "rev-parse", "HEAD"); got != f.first {
				t.Fatal("rejected registration transition advanced source HEAD")
			}
			if got := f.execute(t, "status")["release"]; got != f.first {
				t.Fatal("rejected registration transition changed active release")
			}
		})
	}
}

func TestRegistrationAdditionConflictsAndMissingTargetsRefuseBeforeMerge(t *testing.T) {
	for _, rootName := range []string{"managed", "custom codex", "standard codex", "missing target"} {
		for _, dangling := range []bool{false, true} {
			if rootName == "missing target" && dangling {
				continue
			}
			t.Run(fmt.Sprintf("%s dangling=%t", rootName, dangling), func(t *testing.T) {
				f := historicalRegistrationFixture(t)
				f.paths.Codex = filepath.Join(f.base, "custom codex")
				if err := os.MkdirAll(f.paths.Codex, 0o700); err != nil {
					t.Fatal(err)
				}
				f.seedUserFiles(t)
				origin := f.bareOrigin(t)
				i := f.installer(true)
				if _, err := i.Setup(true); err != nil {
					t.Fatal(err)
				}
				addBackendFixtureRegistrations(t, f)
				name := backendFixtureNames[0]
				if rootName == "missing target" {
					if err := os.RemoveAll(filepath.Join(f.paths.Source, "skills", name)); err != nil {
						t.Fatal(err)
					}
					f.git(t, "add", "-A")
					f.git(t, "commit", "-qm", "Missing new registration target")
				} else {
					root := map[string]string{"managed": i.skills, "custom codex": filepath.Join(i.Codex, "skills"), "standard codex": filepath.Join(i.Home, ".codex", "skills")}[rootName]
					path := filepath.Join(root, name)
					if dangling {
						if err := os.MkdirAll(root, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(f.base, "missing unrelated target"), path); err != nil {
							t.Fatal(err)
						}
					} else {
						installerWrite(t, path, "unrelated registration bytes\n", 0o640)
					}
				}
				f.git(t, "push", origin, "main")
				f.git(t, "reset", "--hard", f.first)
				beforeHome, beforeCodex := installerSnapshot(t, f.paths.Home), installerSnapshot(t, f.paths.Codex)
				beforeState := installerRead(t, i.statePath)
				if _, err := i.Update(); err == nil {
					t.Fatalf("update accepted %s dangling=%t", rootName, dangling)
				}
				installerUnchanged(t, f.paths.Home, beforeHome)
				installerUnchanged(t, f.paths.Codex, beforeCodex)
				if installerRead(t, i.statePath) != beforeState || f.git(t, "rev-parse", "HEAD") != f.first || exists(i.journalPath) {
					t.Fatal("premerge refusal changed HEAD, state, or transaction ownership")
				}
			})
		}
	}
}

func TestRegistrationAdditionRechecksDiscoveryBeforeTransaction(t *testing.T) {
	for _, rootName := range []string{"managed", "codex"} {
		t.Run(rootName, func(t *testing.T) {
			f := historicalRegistrationFixture(t)
			i := f.installer(true)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			state := mustRegistrationState(t, i)
			second := addBackendFixtureRegistrations(t, f)
			release, manifest, err := i.stage(second)
			if err != nil {
				t.Fatal(err)
			}
			ops, err := i.activation(state, second, release, manifest, false, false)
			if err != nil {
				t.Fatal(err)
			}
			root := i.skills
			if rootName == "codex" {
				root = filepath.Join(i.Codex, "skills")
			}
			installerWrite(t, filepath.Join(root, backendFixtureNames[0]), "new conflict after plan\n", 0o600)
			before := installerSnapshot(t, f.paths.Home)
			if err = i.transact("update", ops); err == nil {
				t.Fatalf("transaction accepted a late %s conflict", rootName)
			}
			installerUnchanged(t, f.paths.Home, before)
			if exists(i.journalPath) || !equal(state, mustRegistrationState(t, i)) {
				t.Fatal("late transaction conflict changed active ownership")
			}
		})
	}
}

func mustRegistrationState(t *testing.T, i *Installer) Object {
	t.Helper()
	state, err := i.state(true)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRegistrationRollbackRefusesAdoptedOriginsAndChangedOwnedLinks(t *testing.T) {
	for _, originKind := range []string{"symlink", "legacy", "changed link"} {
		t.Run(originKind, func(t *testing.T) {
			f := historicalRegistrationFixture(t)
			name := backendFixtureNames[0]
			if originKind == "legacy" {
				// Frozen v1 installations can carry legacy adoption rows. Their
				// immutable prior manifest need not contain that registration.
				name = "typesafe-ai"
				manifest := clone(f.manifest)
				manifest["registrations"] = clone(object(f.manifest["registrations"]))
				delete(object(manifest["registrations"]), name)
				installerWrite(t, filepath.Join(f.paths.Source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
				f.git(t, "add", ".")
				f.git(t, "commit", "-qm", "Historical snapshot before legacy registration")
				f.first = f.git(t, "rev-parse", "HEAD")
				f.manifest = manifest
			}
			i := f.installer(true)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			addBackendFixtureRegistrations(t, f)
			if originKind == "legacy" {
				manifest, err := LoadManifest(f.paths.Source)
				if err != nil {
					t.Fatal(err)
				}
				object(manifest["registrations"])[name] = "third_party/typesafe-ai"
				installerWrite(t, filepath.Join(f.paths.Source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
				f.git(t, "add", ".")
				f.git(t, "commit", "-qm", "Restore legacy registration")
			}
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			state := mustRegistrationState(t, i)
			item := object(object(state["registrations"])[name])
			switch originKind {
			case "symlink":
				item["original"] = Object{"kind": "symlink", "target": filepath.Join(f.base, "adopted prior checkout", "skills", name)}
			case "legacy":
				backup := filepath.Join(i.State, "backups", name)
				installerCopyTree(t, filepath.Join(f.paths.Source, "third_party", name), backup)
				observation, err := observe(backup)
				if err != nil {
					t.Fatal(err)
				}
				item["original"] = Object{"kind": "legacy", "path": filepath.Join(i.Home, ".codex", "skills", name), "backup": backup, "observation": observation}
			case "changed link":
				path := filepath.Join(i.skills, name)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.base, "changed target"), path); err != nil {
					t.Fatal(err)
				}
			}
			if originKind != "changed link" {
				installerWrite(t, i.statePath, string(legacyJSON(seal(state))), 0o600)
			}
			beforeHome, beforeState := installerSnapshot(t, i.Home), installerSnapshot(t, i.State)
			for _, apply := range []bool{false, true} {
				i.Apply = apply
				if _, err := i.Rollback(); err == nil {
					t.Fatalf("rollback accepted %s", originKind)
				} else if originKind != "changed link" && !strings.Contains(err.Error(), "cannot remove adopted registration") {
					t.Fatalf("rollback rejected adopted row for unrelated reason: %v", err)
				}
				installerUnchanged(t, i.Home, beforeHome)
				installerUnchanged(t, i.State, beforeState)
			}
		})
	}
}

func TestRegistrationUpdateRechecksAfterRuntimePreparationBeforeMerge(t *testing.T) {
	f := historicalRegistrationFixture(t)
	origin := f.bareOrigin(t)
	i := nativeFixtureInstaller(t, f, f.first, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	beforeState := installerRead(t, i.statePath)
	second := addBackendFixtureRegistrations(t, f)
	f.git(t, "push", origin, "main")
	f.git(t, "reset", "--hard", f.first)
	candidate := nativeFixtureInstaller(t, f, second, true)
	i.PrepareRuntime = func(context.Context, string, string, string) (RuntimeCandidate, error) {
		installerWrite(t, filepath.Join(i.Codex, "skills", backendFixtureNames[0]), "occupied while runtime prepared\n", 0o600)
		return RuntimeCandidate{Path: candidate.RuntimeCandidate, Identity: candidate.RuntimeIdentity}, nil
	}
	if _, err := i.Update(); err == nil || !strings.Contains(err.Error(), "duplicate discovery") {
		t.Fatalf("update accepted a preparation-time collision: %v", err)
	}
	if f.git(t, "rev-parse", "HEAD") != f.first || installerRead(t, i.statePath) != beforeState || exists(i.journalPath) {
		t.Fatal("runtime preparation collision advanced source or installation")
	}
	if got := mustRegistrationState(t, i)["release"]; got != f.first {
		t.Fatal("runtime preparation collision activated skills")
	}
}

func TestRegistrationNoCheckoutUpdateDryRunWithSourceBehindActive(t *testing.T) {
	f := historicalRegistrationFixture(t)
	f.seedUserFiles(t)
	origin := f.bareOrigin(t)
	i := f.installer(true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	second := addBackendFixtureRegistrations(t, f)
	f.git(t, "push", origin, "main")
	f.git(t, "reset", "--hard", f.first)
	i.NoCheckout = true
	if _, err := i.Update(); err != nil {
		t.Fatal(err)
	}
	if state := mustRegistrationState(t, i); state["release"] != second || len(object(state["registrations"])) != 13 || f.git(t, "rev-parse", "HEAD") != f.first {
		t.Fatal("no-checkout update did not activate thirteen while retaining ten in source")
	}
	// An allowed URL with a missing rewrite destination makes a fetch fail.
	// The dry run must use only clean local ancestry and owned cached state.
	if err := os.Rename(origin, origin+" temporarily offline"); err != nil {
		t.Fatal(err)
	}
	i.Apply = false
	before := installerSnapshot(t, f.base)
	report, err := i.Update()
	if err != nil || report["source_behind_active"] != true || report["local_head"] != f.first || report["active_release"] != second || len(registrationChanges(t, report)) != 0 {
		t.Fatalf("offline update preview with source behind active: %#v, %v", report, err)
	}
	installerUnchanged(t, f.base, before)
	if err = os.Rename(origin+" temporarily offline", origin); err != nil {
		t.Fatal(err)
	}
	i.Apply = true
	beforeState, beforeHome := installerRead(t, i.statePath), installerSnapshot(t, i.Home)
	report, err = i.Update()
	if err != nil || report["changed"] != false || len(registrationChanges(t, report)) != 0 {
		t.Fatalf("repeat no-checkout update: %#v, %v", report, err)
	}
	installerUnchanged(t, i.Home, beforeHome)
	if installerRead(t, i.statePath) != beforeState || f.git(t, "rev-parse", "HEAD") != f.first {
		t.Fatal("repeat no-checkout update changed HEAD or active state")
	}
}

func nativeRegistrationTransition(t *testing.T, action string) (*installerFixture, *Installer, Object, string) {
	t.Helper()
	f := historicalRegistrationFixture(t)
	f.seedUserFiles(t)
	origin := f.bareOrigin(t)
	i := nativeFixtureInstaller(t, f, f.first, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	prior := mustRegistrationState(t, i)
	second := addBackendFixtureRegistrations(t, f)
	f.git(t, "push", origin, "main")
	f.git(t, "reset", "--hard", f.first)
	i = nativeFixtureInstaller(t, f, second, true)
	if action == "rollback" {
		if _, err := i.Update(); err != nil {
			t.Fatal(err)
		}
		prior = mustRegistrationState(t, i)
	}
	return f, i, prior, second
}

func TestRegistrationNativeTransitionRecoversEveryOperationWithoutSource(t *testing.T) {
	for _, action := range []string{"update", "rollback"} {
		t.Run(action, func(t *testing.T) {
			f, i, state, second := nativeRegistrationTransition(t, action)
			sha := second
			if action == "rollback" {
				sha = f.first
			} else if err := i.stageRuntime(); err != nil {
				t.Fatal(err)
			}
			release, manifest, err := i.stage(sha)
			if err != nil {
				t.Fatal(err)
			}
			ops, err := i.activation(state, sha, release, manifest, false, action == "rollback")
			if err != nil {
				t.Fatal(err)
			}
			labels := []string{"after-journal"}
			for _, op := range ops {
				labels = append(labels, "after-"+text(op["label"]))
			}
			for _, name := range backendFixtureNames {
				if !slices.Contains(labels, "after-registration:"+name) {
					t.Fatalf("transition omitted registration operation %s", name)
				}
			}
			for _, label := range []string{"after-pointer", "after-global", "after-model-config", "after-state"} {
				if !slices.Contains(labels, label) {
					t.Fatalf("transition omitted required boundary %s", label)
				}
			}
			if action == "update" && !slices.Contains(labels, "after-manager-runtime") {
				t.Fatal("additive update omitted runtime-switch coverage")
			}
			for _, label := range labels {
				t.Run(label, func(t *testing.T) {
					f, i, prior, _ := nativeRegistrationTransition(t, action)
					beforeHome := installerSnapshot(t, f.paths.Home)
					beforeState := installerRead(t, i.statePath)
					t.Setenv("AGENT_WORKFLOWS_FAULT", label)
					if _, err := i.Execute(action); err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
						t.Fatalf("%s did not interrupt at %s: %v", action, label, err)
					}
					t.Setenv("AGENT_WORKFLOWS_FAULT", "")
					if err := os.Rename(f.paths.Source, f.paths.Source+" unavailable"); err != nil {
						t.Fatal(err)
					}
					i.Apply = false
					beforeRecovery := installerSnapshot(t, i.State)
					if report, err := i.Recover(); err != nil || report["pending"] != true {
						t.Fatalf("source-independent recovery preview: %#v, %v", report, err)
					}
					installerUnchanged(t, i.State, beforeRecovery)
					i.Apply = true
					if _, err := i.Recover(); err != nil {
						t.Fatalf("source-independent recovery: %v", err)
					}
					installerUnchanged(t, f.paths.Home, beforeHome)
					if installerRead(t, i.statePath) != beforeState || !equal(mustRegistrationState(t, i), prior) || exists(i.journalPath) {
						t.Fatal("recovery did not restore exact prior v1 ownership")
					}
					if _, err := i.Status(); err != nil {
						t.Fatalf("recovered source-independent status: %v", err)
					}
					if _, err := i.Uninstall(); err != nil {
						t.Fatalf("recovered source-independent uninstall: %v", err)
					}
				})
			}
		})
	}
}
