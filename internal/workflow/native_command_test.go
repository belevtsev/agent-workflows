package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nativeFixtureInstaller is the standalone installer for one packed fixture
// release: its bundle, its manager candidate and that release's identity.
func nativeFixtureInstaller(t *testing.T, fixture *installerFixture, revision string, apply bool) *Installer {
	t.Helper()
	fixture.env.Setenv("PATH", "/usr/bin:/bin")
	installer := fixture.installerFor(t, revision, apply)
	installer.Context = t.Context()
	return installer
}

// nativeFixtureInstallerProtocol offers a manager candidate that reports
// protocol for the release's exact bundle identity.
func nativeFixtureInstallerProtocol(t *testing.T, fixture *installerFixture, revision, protocol string, apply bool) *Installer {
	t.Helper()
	installer := nativeFixtureInstaller(t, fixture, revision, apply)
	if protocol == ManagerProtocol {
		return installer
	}
	release := fixture.release(t, revision)
	candidate := filepath.Join(fixture.base, "manager candidates", "protocol "+fixtureRevision(protocol, release.identity.Version)[:12], "cw")
	installerWrite(t, candidate, fixtureManagerScript(t, protocol, release.identity), 0o755)
	installer.RuntimeCandidate = candidate
	return installer
}

func TestNativeManagerLocatorCommandPathValidation(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	// The locator a standalone install writes is version 3; each case changes
	// only its command path and reseals it.
	data, err := os.ReadFile(installer.locatorPath())
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := readJSON(installer.locatorPath())
	if err != nil {
		t.Fatal(err)
	}
	if integer(recorded["version"]) != 3 || recorded["command_path"] != installer.commandPath() {
		t.Fatalf("installed locator is not the version-3 locator for this command: %#v", recorded)
	}
	if locator, err := installer.decodeRuntimeLocator(data); err != nil || locator.Version != 3 || locator.CommandPath != installer.commandPath() {
		t.Fatalf("recorded locator: %#v, %v", locator, err)
	}
	for _, test := range []struct {
		name  string
		path  string
		valid bool
	}{
		{name: "selected command", path: installer.commandPath(), valid: true},
		{name: "other home", path: "/another/home/.local/bin/cw"},
		{name: "empty", path: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			locator := clone(recorded)
			locator["command_path"] = test.path
			_, err := installer.decodeRuntimeLocator(legacyJSON(seal(locator)))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t: %v", test.valid, err)
			}
		})
	}
}

func TestCodexRootIsChosenWhenCodexEnrolls(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	installer := func(hosts ...string) *Installer {
		i := nativeFixtureInstaller(t, f, f.first, true)
		i.Hosts = hosts
		return i
	}
	if _, err := installer(hostClaude).Setup(true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first codex", "second codex"} {
		f.paths.Codex = filepath.Join(f.base, name)
		i := installer(hostCodex)
		if _, err := i.Setup(true); err != nil {
			t.Fatalf("enroll at %s: %v", name, err)
		}
		state, err := i.state(true)
		if err != nil || hostRecord(state, hostCodex)["home"] != i.Codex {
			t.Fatalf("Codex root not recorded: %#v, %v", hostRecord(state, hostCodex)["home"], err)
		}
		if codex, claude := RecordedHomes(f.paths.State); codex != i.Codex || claude != i.Claude {
			t.Fatalf("recorded roots: %q, %q", codex, claude)
		}
		if _, err := installer().Status(); err != nil {
			t.Fatalf("status at %s: %v", name, err)
		}
		f.paths.Codex = filepath.Join(f.base, "another codex")
		if _, err := installer().Status(); err == nil {
			t.Fatal("a recorded Codex root must stay bound")
		}
		f.paths.Codex = filepath.Join(f.base, name)
		if _, err := installer(hostCodex).Uninstall(); err != nil {
			t.Fatalf("remove from %s: %v", name, err)
		}
		if codex, _ := RecordedHomes(f.paths.State); codex != "" {
			t.Fatalf("removed host left a recorded root: %q", codex)
		}
	}
}

// Every standalone setup enrolls the manager, so the old variant that first
// installed without a native manager no longer exists.
func TestNativeManagerAlreadyOnPATHDoesNotAddProfile(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	fixture.env.Setenv("PATH", filepath.Join(fixture.paths.Home, ".local", "bin")+":/usr/bin:/bin")
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if object(state["manager"]) == nil || object(state["manager"])["profile"] != nil {
		t.Fatalf("unnecessary PATH profile was added: %#v", state["manager"])
	}
	rc := filepath.Join(fixture.paths.Home, ".bashrc")
	if got := installerRead(t, rc); got != original[rc] {
		t.Fatalf("startup bytes changed: %q", got)
	}
}

func TestNativeManagerRejectsExternalPATHCommand(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	otherBin := filepath.Join(fixture.base, "other bin")
	installerWrite(t, filepath.Join(otherBin, "cw"), "#!/bin/sh\nexit 0\n", 0o755)
	fixture.env.Setenv("PATH", otherBin+":/usr/bin:/bin")
	before := installerSnapshot(t, fixture.base)
	if _, err := installer.Setup(true); err == nil || !strings.Contains(err.Error(), "another cw command") {
		t.Fatalf("external cw accepted: %v", err)
	}
	installerUnchanged(t, fixture.base, before)
}

func TestNativeManagerRecoversEveryNativeBoundary(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"after-manager-runtime", "after-manager-command", "after-manager-locator", "after-command-profile", "after-state"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			original := fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
			if _, err := installer.Setup(true); err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
				t.Fatalf("fault did not interrupt native enrollment: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			if _, err := installer.Recover(); err != nil {
				t.Fatal(err)
			}
			for path, want := range original {
				if got := installerRead(t, path); got != want {
					t.Fatalf("recovery changed %s: %q", path, got)
				}
			}
			for _, path := range []string{installer.commandPath(), installer.runtimeCurrent(), installer.locatorPath(), installer.statePath, installer.journalPath} {
				if exists(path) {
					t.Fatalf("recovery retained %s", path)
				}
			}
		})
	}
}

func TestNativeManagerRecoveryPreservesIndependentProfileEdits(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-command-profile")
	if _, err := installer.Setup(true); err == nil {
		t.Fatal("fault did not interrupt profile installation")
	}
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	rc := filepath.Join(fixture.paths.Home, ".bashrc")
	installerAppend(t, rc, "# independent after failure\n")
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-command-profile")
	if _, err := installer.Recover(); err == nil {
		t.Fatal("fault did not interrupt recovery")
	}
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	installerAppend(t, rc, "# independent during recovery\n")
	if _, err := installer.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := installerRead(t, rc); got != original[rc]+"# independent after failure\n# independent during recovery\n" {
		t.Fatalf("lost independent profile bytes: %q", got)
	}
}

func TestNativeManagerConflictAndPreparationFailureDoNotActivate(t *testing.T) {
	t.Parallel()
	for _, conflict := range []string{"command", "candidate"} {
		t.Run(conflict, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			if conflict == "command" {
				installerWrite(t, installer.commandPath(), "user command", 0o755)
			} else {
				// The candidate cannot be prepared for the identity asked of it:
				// it reports the bundle's revision, not this one.
				installer.RuntimeIdentity.Revision = strings.Repeat("b", 40)
			}
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Setup(true); err == nil {
				t.Fatal("invalid native preparation succeeded")
			}
			installerUnchanged(t, fixture.base, before)
			// The same installation succeeds once the conflict is gone, so the
			// refusal above came from the conflict.
			if conflict == "command" {
				if err := os.Remove(installer.commandPath()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := nativeFixtureInstaller(t, fixture, fixture.first, true).Setup(true); err != nil {
				t.Fatalf("control installation: %v", err)
			}
		})
	}
}

func TestNativeManagerRejectsTamperedRuntime(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Status(); err != nil {
		t.Fatalf("untampered runtime: %v", err)
	}
	runtimePath := filepath.Join(fixtureRuntimeDir(t, fixture.paths.State, fixture.first), "cw")
	fixtureRewrite(t, runtimePath, []byte(installerRead(t, runtimePath)+"# tampered\n"))
	if _, err := installer.Status(); err == nil {
		t.Fatal("modified executable accepted")
	}
}

func TestNativeManagerUninstallRecoversEveryNativeBoundary(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"after-command-profile", "after-manager-command", "after-manager-runtime", "after-manager-locator", "after-state"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			if _, err := installer.Setup(true); err != nil {
				t.Fatal(err)
			}
			stateBefore, err := installer.state(true)
			if err != nil {
				t.Fatal(err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
			if _, err = installer.Uninstall(); err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
				t.Fatalf("fault did not interrupt native uninstall: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			if err = installer.verifyRecoveryRuntime(installer.RuntimeIdentity); err != nil {
				t.Fatalf("immutable recovery metadata unavailable: %v", err)
			}
			if _, err = installer.Recover(); err != nil {
				t.Fatalf("native uninstall recovery: %v", err)
			}
			stateAfter, err := installer.state(true)
			if err != nil {
				t.Fatal(err)
			}
			if !equal(stateBefore, stateAfter) {
				t.Fatal("native recovery lost original ownership")
			}
			if _, err = installer.Status(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeManagerUnsafeRecoveryJournalRefusesAllReversals(t *testing.T) {
	t.Parallel()
	for _, corruption := range []string{"command-path", "runtime-target", "locator-roots", "profile-path", "profile-content"} {
		t.Run(corruption, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
			if _, err := installer.Setup(true); err == nil || !strings.Contains(err.Error(), "injected failure at after-state") {
				t.Fatalf("fault did not interrupt native setup: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			journal, err := readJSON(installer.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			corrupted := false
			for _, raw := range sequence(journal["operations"]) {
				op := object(raw)
				switch {
				case corruption == "command-path" && op["path"] == installer.commandPath():
					op["path"] = filepath.Join(fixture.base, "foreign command")
				case corruption == "runtime-target" && op["path"] == installer.runtimeCurrent():
					object(op["after"])["target"] = fixture.source
				case corruption == "locator-roots" && op["path"] == installer.locatorPath():
					object(op["after"])["data"] = encode(legacyJSON(seal(asObject(RuntimeLocator{Version: 3, Home: fixture.base, State: fixture.paths.State, CommandPath: installer.commandPath()}))))
				case corruption == "profile-path" && op["kind"] == "command_profile":
					op["path"] = filepath.Join(fixture.base, "foreign rc")
				case corruption == "profile-content" && op["kind"] == "command_profile":
					op["after_segment"] = "unowned content"
				default:
					continue
				}
				corrupted = true
			}
			if !corrupted {
				t.Fatalf("journal has no operation to corrupt for %s", corruption)
			}
			installerWrite(t, installer.journalPath, string(legacyJSON(seal(journal))), 0o600)
			before := installerSnapshot(t, fixture.base)
			if _, err = installer.Recover(); err == nil {
				t.Fatal("unsafe native recovery journal accepted")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestNativeManagerSymlinkedCommandParentRefusesUninstall(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Dir(installer.commandPath())
	foreign := filepath.Join(fixture.base, "foreign bin")
	if err := os.Rename(bin, foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, bin); err != nil {
		t.Fatal(err)
	}
	before := installerSnapshot(t, fixture.base)
	if _, err := installer.Uninstall(); err == nil {
		t.Fatal("symlinked command parent was accepted")
	}
	installerUnchanged(t, fixture.base, before)
}

func TestInstallerContextCancellationLeavesInstallationUntouched(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := fixture.installer(true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	installer.Context = ctx
	before := installerSnapshot(t, fixture.base)
	if _, err := installer.Setup(true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled setup: %v", err)
	}
	installerUnchanged(t, fixture.base, before)
}

func sharedConfigRuntimeFixture(t *testing.T, fixture *installerFixture) *Installer {
	t.Helper()
	return sharedConfigRuntimeFixtureFor(t, fixture, fixture.first)
}

func sharedConfigRuntimeFixtureFor(t *testing.T, fixture *installerFixture, revision string) *Installer {
	t.Helper()
	return nativeFixtureInstaller(t, fixture, revision, true)
}
