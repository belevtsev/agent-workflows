package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var backendFixtureNames = []string{"backend-security-review", "go-pki-mtls", "protobuf-contracts"}

// Keep this lifecycle fixture historical as the maintained pack grows. Its
// initial immutable snapshot has the original nine registrations.
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
		installerWrite(t, filepath.Join(f.source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
		f.first = f.commit(t)
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
		installerWrite(t, filepath.Join(f.source, root, "SKILL.md"), "---\nname: "+name+"\ndescription: Isolated additive registration lifecycle fixture.\n---\n\n# Fixture skill\n", 0o644)
	}
	installerWrite(t, filepath.Join(f.source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
	return f.newRelease(t)
}

func assertAddedRegistrations(t *testing.T, installer *Installer, state, prior Object) {
	t.Helper()
	if got := len(object(hostRecord(state, hostCodex)["registrations"])); got != 12 {
		t.Fatalf("active registrations = %d, want twelve", got)
	}
	for name, raw := range object(hostRecord(state, hostCodex)["registrations"]) {
		item := object(raw)
		if old := object(object(hostRecord(prior, hostCodex)["registrations"])[name]); old != nil {
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

// activeReleaseDir resolves the active release pointer to the immutable
// release directory it names.
func activeReleaseDir(t *testing.T, i *Installer) string {
	t.Helper()
	target, err := os.Readlink(i.current)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(i.current), target)
	}
	return target
}

// The standalone report no longer carries typed registration changes, so the
// plans are checked through the ownership the activation records.
func TestRegistrationFreshAndRepeatedInstallPlans(t *testing.T) {
	t.Parallel()
	f := historicalRegistrationFixture(t)
	second := addBackendFixtureRegistrations(t, f)
	i := f.installer(false)
	before := installerSnapshot(t, f.base)
	report, err := i.Setup(true)
	if err != nil || report["dry_run"] != true || report["release"] == nil {
		t.Fatalf("fresh install preview: %#v, %v", report, err)
	}
	// A dry installation neither stages the release nor copies the runtime.
	installerUnchanged(t, f.base, before)
	if exists(f.paths.State) {
		t.Fatal("dry installation created the state directory")
	}
	i = f.installer(true)
	applied, err := i.Setup(true)
	if err != nil || applied["release"] != report["release"] {
		t.Fatalf("fresh install activated %#v; the preview named %#v: %v", applied["release"], report["release"], err)
	}
	state := mustRegistrationState(t, i)
	if stateRevision(state) != second || len(object(hostRecord(state, hostCodex)["registrations"])) != 12 {
		t.Fatalf("fresh install did not register the twelve bundle skills: %#v", hostRecord(state, hostCodex)["registrations"])
	}
	assertAddedRegistrations(t, i, state, Object{})
	before = installerSnapshot(t, f.base)
	report, err = i.Setup(true)
	if err != nil || report["changed"] != false {
		t.Fatalf("repeated install: %#v, %v", report, err)
	}
	installerUnchanged(t, f.base, before)
	if !equal(mustRegistrationState(t, i), state) {
		t.Fatal("repeated install changed registration ownership")
	}
}

// Setup on an installation keeps its active release; update is the one
// command that adds the bundle's new registrations.
func TestRegistrationAdditiveInstallAndUpdateRollbackReadd(t *testing.T) {
	t.Parallel()
	f := historicalRegistrationFixture(t)
	original := f.seedUserFiles(t)
	i := f.installer(true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	prior, err := i.state(true)
	if err != nil {
		t.Fatal(err)
	}
	firstRelease := activeReleaseDir(t, i)
	firstSnapshot := installerSnapshot(t, firstRelease)
	second := addBackendFixtureRegistrations(t, f)
	i = f.installer(true)
	before := installerSnapshot(t, f.paths.Home)
	if report, err := i.Setup(true); err != nil || report["release"] != prior["release"] {
		t.Fatalf("setup with a newer bundle moved the release: %#v, %v", report, err)
	}
	installerUnchanged(t, f.paths.Home, before)
	if state := mustRegistrationState(t, i); stateRevision(state) != f.first || !equal(hostRecord(state, hostCodex)["registrations"], hostRecord(prior, hostCodex)["registrations"]) {
		t.Fatal("setup with a newer bundle added registrations")
	}
	before = installerSnapshot(t, f.base)
	if report, err := f.installer(false).Update(); err != nil || report["dry_run"] != true {
		t.Fatalf("additive preview: %#v, %v", report, err)
	}
	installerUnchanged(t, f.base, before)
	if _, err = i.Update(); err != nil {
		t.Fatalf("additive activation: %v", err)
	}
	state, err := i.state(true)
	if err != nil || stateRevision(state) != second {
		t.Fatalf("active state: %#v, %v", state, err)
	}
	assertAddedRegistrations(t, i, state, prior)
	secondRelease := activeReleaseDir(t, i)
	if secondRelease == firstRelease {
		t.Fatal("update kept the first release directory active")
	}
	secondSnapshot := installerSnapshot(t, secondRelease)
	before = installerSnapshot(t, f.base)
	if _, err = f.installer(false).Rollback(); err != nil {
		t.Fatalf("rollback preview: %v", err)
	}
	installerUnchanged(t, f.base, before)
	if _, err = i.Rollback(); err != nil {
		t.Fatal(err)
	}
	state, err = i.state(true)
	if err != nil || !equal(hostRecord(state, hostCodex)["registrations"], hostRecord(prior, hostCodex)["registrations"]) {
		t.Fatalf("rollback did not restore nine ownership rows: %#v, %v", state, err)
	}
	for _, name := range backendFixtureNames {
		if exists(filepath.Join(i.skills, name)) {
			t.Fatalf("rollback retained %s", name)
		}
	}
	if _, err = i.Update(); err != nil {
		t.Fatalf("forward readdition: %v", err)
	}
	state, err = i.state(true)
	if err != nil {
		t.Fatal(err)
	}
	assertAddedRegistrations(t, i, state, prior)
	for path, want := range map[string]Object{firstRelease: firstSnapshot, secondRelease: secondSnapshot} {
		installerUnchanged(t, path, want)
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
}

func TestRegistrationForwardRemovalsRenamesAndRootChangesRefuse(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"remove", "rename", "root"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := historicalRegistrationFixture(t)
			if _, err := f.installer(true).Setup(true); err != nil {
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
				path := filepath.Join(f.source, root, "SKILL.md")
				installerWrite(t, path, strings.ReplaceAll(installerRead(t, path), "name: code-review", "name: renamed-review"), 0o644)
			case "root":
				regs["code-review"] = "skills/arbitrary-review-root"
				if err := os.Rename(filepath.Join(f.source, root), filepath.Join(f.source, text(regs["code-review"]))); err != nil {
					t.Fatal(err)
				}
			}
			installerWrite(t, filepath.Join(f.source, "skills-manifest.json"), string(legacyJSON(manifest)), 0o644)
			if _, err := ValidateSuite(f.source); err != nil {
				t.Fatalf("fixture must be a valid suite before compatibility rejection: %v", err)
			}
			f.commit(t)
			// Setup on an installation activates its own base, never the
			// bundle, so only update meets the changed registrations.
			before := installerSnapshot(t, f.paths.Home)
			beforeState := installerSnapshot(t, f.paths.State)
			for _, apply := range []bool{false, true} {
				if _, err := f.installer(apply).Update(); err == nil || !strings.Contains(err.Error(), "registration names or roots changed") {
					t.Fatalf("update (apply %t) accepted %s: %v", apply, change, err)
				}
				installerUnchanged(t, f.paths.Home, before)
				installerUnchanged(t, f.paths.State, beforeState)
			}
			if got := f.statusRevision(t); got != f.first {
				t.Fatal("rejected registration transition changed active release")
			}
		})
	}
}

func TestRegistrationAdditionConflictsAndMissingTargetsRefuseBeforeMerge(t *testing.T) {
	t.Parallel()
	for _, rootName := range []string{"managed", "custom codex", "standard codex", "missing target"} {
		for _, dangling := range []bool{false, true} {
			if rootName == "missing target" && dangling {
				continue
			}
			t.Run(fmt.Sprintf("%s dangling=%t", rootName, dangling), func(t *testing.T) {
				t.Parallel()
				f := historicalRegistrationFixture(t)
				f.paths.Codex = filepath.Join(f.base, "custom codex")
				if err := os.MkdirAll(f.paths.Codex, 0o700); err != nil {
					t.Fatal(err)
				}
				f.seedUserFiles(t)
				if _, err := f.installer(true).Setup(true); err != nil {
					t.Fatal(err)
				}
				addBackendFixtureRegistrations(t, f)
				name := backendFixtureNames[0]
				conflict := ""
				if rootName == "missing target" {
					if err := os.RemoveAll(filepath.Join(f.source, "skills", name)); err != nil {
						t.Fatal(err)
					}
					// A bundle that cannot be packed cannot reach an installer;
					// either refusal keeps the missing target out of the merge.
					if _, err := f.tryCommit(t); err != nil {
						t.Logf("bundle build refused the missing registration target: %v", err)
						return
					}
				} else {
					i := f.installer(true)
					root := map[string]string{"managed": i.skills, "custom codex": filepath.Join(i.Codex, "skills"), "standard codex": filepath.Join(i.Home, ".codex", "skills")}[rootName]
					conflict = filepath.Join(root, name)
					if dangling {
						if err := os.MkdirAll(root, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(f.base, "missing unrelated target"), conflict); err != nil {
							t.Fatal(err)
						}
					} else {
						installerWrite(t, conflict, "unrelated registration bytes\n", 0o640)
					}
				}
				i := f.installer(true)
				beforeHome, beforeCodex := installerSnapshot(t, f.paths.Home), installerSnapshot(t, f.paths.Codex)
				beforeState := installerRead(t, i.statePath)
				if _, err := i.Update(); err == nil {
					t.Fatalf("update accepted %s dangling=%t", rootName, dangling)
				}
				installerUnchanged(t, f.paths.Home, beforeHome)
				installerUnchanged(t, f.paths.Codex, beforeCodex)
				if installerRead(t, i.statePath) != beforeState || exists(i.journalPath) || f.activeRevision(t) != f.first {
					t.Fatal("premerge refusal changed state, the active release, or transaction ownership")
				}
				if conflict == "" {
					return
				}
				// Without the unrelated entry the same update succeeds, so the
				// refusal came from the conflict.
				if err := os.Remove(conflict); err != nil {
					t.Fatal(err)
				}
				if _, err := f.installer(true).Update(); err != nil {
					t.Fatalf("update without the conflict: %v", err)
				}
				if got := len(object(hostRecord(f.readState(t), hostCodex)["registrations"])); got != 12 {
					t.Fatalf("update without the conflict registered %d skills", got)
				}
			})
		}
	}
}

// The removed checkout installer let this test plan an activation and then
// run its transaction. The standalone installer plans and transacts in one
// call, so the conflict appears while update runs: the manager candidate it
// inspects creates it. See the test author's notes for the timing question.
func TestRegistrationAdditionRechecksDiscoveryBeforeTransaction(t *testing.T) {
	t.Parallel()
	for _, rootName := range []string{"managed", "codex"} {
		t.Run(rootName, func(t *testing.T) {
			t.Parallel()
			f := historicalRegistrationFixture(t)
			i := f.installer(true)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			state := mustRegistrationState(t, i)
			second := addBackendFixtureRegistrations(t, f)
			root := i.skills
			if rootName == "codex" {
				root = filepath.Join(i.Codex, "skills")
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			conflict := filepath.Join(root, backendFixtureNames[0])
			// The candidate creates the conflict on its first call while armed.
			// The same candidate bytes then serve the control update, because a
			// version's runtime may already have been staged from them.
			armed := filepath.Join(f.base, "late conflict armed")
			installerWrite(t, armed, "", 0o600)
			hook := "if [ -e " + fixtureShellQuote(armed) + " ]; then rm -f " + fixtureShellQuote(armed) + "; printf 'new conflict after plan\\n' > " + fixtureShellQuote(conflict) + "; fi\n"
			hooked := filepath.Join(f.base, "manager candidates", "hooked "+rootName, "cw")
			installerWrite(t, hooked, strings.Replace(fixtureManagerScript(t, ManagerProtocol, f.release(t, second).identity), "case ", hook+"case ", 1), 0o755)
			late := f.installer(true)
			late.RuntimeCandidate = hooked
			before := installerSnapshot(t, f.paths.Home)
			if _, err := late.Update(); err == nil {
				t.Fatalf("update accepted a late %s conflict", rootName)
			}
			if exists(armed) || installerRead(t, conflict) != "new conflict after plan\n" {
				t.Fatal("the candidate did not create the conflict during update")
			}
			if err := os.Remove(conflict); err != nil {
				t.Fatal(err)
			}
			installerUnchanged(t, f.paths.Home, before)
			if exists(i.journalPath) || !equal(state, mustRegistrationState(t, i)) {
				t.Fatal("late transaction conflict changed active ownership")
			}
			// The same update succeeds once the conflict is gone.
			if _, err := late.Update(); err != nil {
				t.Fatalf("update without the late conflict: %v", err)
			}
			if got := f.activeRevision(t); got != second {
				t.Fatalf("control update activated %s, want %s", got, second)
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
	t.Parallel()
	for _, originKind := range []string{"symlink", "changed link"} {
		t.Run(originKind, func(t *testing.T) {
			t.Parallel()
			f := historicalRegistrationFixture(t)
			name := backendFixtureNames[0]
			i := f.installer(true)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			addBackendFixtureRegistrations(t, f)
			i = f.installer(true)
			if _, err := i.Update(); err != nil {
				t.Fatal(err)
			}
			state := mustRegistrationState(t, i)
			item := object(object(hostRecord(state, hostCodex)["registrations"])[name])
			switch originKind {
			case "symlink":
				item["original"] = Object{"kind": "symlink", "target": filepath.Join(f.base, "adopted prior checkout", "skills", name)}
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
