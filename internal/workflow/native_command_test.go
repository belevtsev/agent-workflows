package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func nativeFixtureInstaller(t *testing.T, fixture *installerFixture, revision string, apply bool) *Installer {
	t.Helper()
	t.Setenv("PATH", "/usr/bin:/bin")
	candidate := filepath.Join(fixture.base, "native candidate "+revision)
	installerWrite(t, candidate, "#!/bin/sh\nif [ \"${1-}\" = --manager-protocol ]; then printf '%s\\n' "+ManagerProtocol+"; fi\n", 0o755)
	installer := fixture.installer(apply)
	installer.Context = t.Context()
	installer.RuntimeCandidate = candidate
	installer.RuntimeIdentity = RuntimeIdentity{Revision: revision, Version: "test", OS: runtime.GOOS, Arch: runtime.GOARCH}
	return installer
}

func TestNativeManagerLocatorCommandPathValidation(t *testing.T) {
	fixture := newInstallerFixture(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
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
			locator := RuntimeLocator{Version: 2, Source: installer.Source, Home: installer.Home, Codex: installer.Codex, State: installer.State, CommandPath: test.path}
			_, err := installer.decodeRuntimeLocator(legacyJSON(seal(asObject(locator))))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t: %v", test.valid, err)
			}
		})
	}
}

func TestNativeManagerAlreadyOnPATHDoesNotAddProfile(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy %t", legacy), func(t *testing.T) {
			fixture := newInstallerFixture(t)
			original := fixture.seedUserFiles(t)
			if legacy {
				fixture.execute(t, "setup")
			}
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			t.Setenv("PATH", filepath.Join(fixture.paths.Home, ".local", "bin")+":/usr/bin:/bin")
			if _, err := installer.Setup(true); err != nil {
				t.Fatal(err)
			}
			state, err := installer.state(true)
			if err != nil {
				t.Fatal(err)
			}
			if object(state["manager"])["profile"] != nil {
				t.Fatal("unnecessary PATH profile was added")
			}
			rc := filepath.Join(fixture.paths.Home, ".bashrc")
			if got := installerRead(t, rc); got != original[rc] {
				t.Fatalf("startup bytes changed: %q", got)
			}
		})
	}
}

func TestNativeManagerRejectsExternalPATHCommand(t *testing.T) {
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	otherBin := filepath.Join(fixture.base, "other bin")
	installerWrite(t, filepath.Join(otherBin, "cw"), "#!/bin/sh\nexit 0\n", 0o755)
	t.Setenv("PATH", otherBin+":/usr/bin:/bin")
	before := installerSnapshot(t, fixture.base)
	if _, err := installer.Setup(true); err == nil || !strings.Contains(err.Error(), "another cw command") {
		t.Fatalf("external cw accepted: %v", err)
	}
	installerUnchanged(t, fixture.base, before)
}

func TestNativeManagerRecoversEveryNativeBoundary(t *testing.T) {
	for _, label := range []string{"after-manager-runtime", "after-manager-command", "after-manager-locator", "after-command-profile", "after-state"} {
		t.Run(label, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			original := fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			t.Setenv("AGENT_WORKFLOWS_FAULT", label)
			if _, err := installer.Setup(true); err == nil {
				t.Fatal("fault did not interrupt native enrollment")
			}
			t.Setenv("AGENT_WORKFLOWS_FAULT", "")
			installer.Source = filepath.Join(fixture.base, "unavailable")
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
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	t.Setenv("AGENT_WORKFLOWS_FAULT", "after-command-profile")
	if _, err := installer.Setup(true); err == nil {
		t.Fatal("fault did not interrupt profile installation")
	}
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	rc := filepath.Join(fixture.paths.Home, ".bashrc")
	installerAppend(t, rc, "# independent after failure\n")
	t.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-command-profile")
	if _, err := installer.Recover(); err == nil {
		t.Fatal("fault did not interrupt recovery")
	}
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	installerAppend(t, rc, "# independent during recovery\n")
	if _, err := installer.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := installerRead(t, rc); got != original[rc]+"# independent after failure\n# independent during recovery\n" {
		t.Fatalf("lost independent profile bytes: %q", got)
	}
}

func TestNativeManagerConflictAndPreparationFailureDoNotActivate(t *testing.T) {
	for _, conflict := range []string{"command", "candidate"} {
		t.Run(conflict, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			if conflict == "command" {
				installerWrite(t, installer.commandPath(), "user command", 0o755)
			} else {
				installer.RuntimeIdentity.Revision = strings.Repeat("b", 40)
				installer.PrepareRuntime = func(context.Context, string, string, string) (RuntimeCandidate, error) {
					return RuntimeCandidate{}, errors.New("download failed")
				}
			}
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Setup(true); err == nil {
				t.Fatal("invalid native preparation succeeded")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestNativeManagerRejectsTamperedRuntime(t *testing.T) {
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(installer.runtimeDir(fixture.first), "cw")
	installerAppend(t, runtimePath, "# tampered\n")
	if _, err := installer.Status(); err == nil {
		t.Fatal("modified executable accepted")
	}
}

func TestNativeManagerUninstallRecoversEveryNativeBoundary(t *testing.T) {
	for _, label := range []string{"after-command-profile", "after-manager-command", "after-manager-runtime", "after-manager-locator", "after-state"} {
		t.Run(label, func(t *testing.T) {
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
			t.Setenv("AGENT_WORKFLOWS_FAULT", label)
			if _, err = installer.Uninstall(); err == nil {
				t.Fatal("fault did not interrupt native uninstall")
			}
			t.Setenv("AGENT_WORKFLOWS_FAULT", "")
			if _, err = installer.runtimeRelease(fixture.first); err != nil {
				t.Fatalf("immutable recovery metadata unavailable: %v", err)
			}
			installer.Source = filepath.Join(fixture.base, "missing source during recovery")
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
	for _, corruption := range []string{"command-path", "runtime-target", "locator-roots", "profile-path", "profile-content"} {
		t.Run(corruption, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			t.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
			if _, err := installer.Setup(true); err == nil {
				t.Fatal("fault did not interrupt native setup")
			}
			t.Setenv("AGENT_WORKFLOWS_FAULT", "")
			journal, err := readJSON(installer.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range sequence(journal["operations"]) {
				op := object(raw)
				switch {
				case corruption == "command-path" && op["path"] == installer.commandPath():
					op["path"] = filepath.Join(fixture.base, "foreign command")
				case corruption == "runtime-target" && op["path"] == installer.runtimeCurrent():
					object(op["after"])["target"] = fixture.paths.Source
				case corruption == "locator-roots" && op["path"] == installer.locatorPath():
					object(op["after"])["data"] = encode(legacyJSON(RuntimeLocator{Source: fixture.paths.Source, Home: fixture.base, Codex: fixture.paths.Codex, State: fixture.paths.State}))
				case corruption == "profile-path" && op["kind"] == "command_profile":
					op["path"] = filepath.Join(fixture.base, "foreign rc")
				case corruption == "profile-content" && op["kind"] == "command_profile":
					op["after_segment"] = "unowned content"
				}
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

func TestNativeUpdateNoCheckoutPreparesFetchedRuntime(t *testing.T) {
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	origin := fixture.bareOrigin(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	second := fixture.newRelease(t)
	fixture.git(t, "push", origin, "main")
	fixture.git(t, "reset", "--hard", fixture.first)
	candidate := nativeFixtureInstaller(t, fixture, second, true)
	calls := 0
	installer.NoCheckout = true
	installer.PrepareRuntime = func(ctx context.Context, source, sha, root string) (RuntimeCandidate, error) {
		calls++
		if ctx != installer.Context || source != installer.Source || sha != second || root != installer.State {
			t.Fatal("runtime preparation did not receive exact update inputs")
		}
		return RuntimeCandidate{Path: candidate.RuntimeCandidate, Identity: candidate.RuntimeIdentity}, nil
	}
	if _, err := installer.Update(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("runtime prepare calls = %d", calls)
	}
	if got := fixture.git(t, "rev-parse", "HEAD"); got != fixture.first {
		t.Fatalf("no-checkout advanced source: %s", got)
	}
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if state["release"] != second || object(object(state["manager"])["runtime"])["revision"] != second {
		t.Fatal("no-checkout failed to update manager and skills")
	}
}

func sharedConfigRuntimeFixture(t *testing.T, fixture *installerFixture) *Installer {
	t.Helper()
	return sharedConfigRuntimeFixtureFor(t, fixture, fixture.first)
}

func sharedConfigRuntimeFixtureFor(t *testing.T, fixture *installerFixture, revision string) *Installer {
	t.Helper()
	return nativeFixtureInstaller(t, fixture, revision, true)
}
