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
	installerWrite(t, candidate, "#!/bin/sh\nexit 0\n", 0o755)
	installer := fixture.installer(apply)
	installer.Context = t.Context()
	installer.RuntimeCandidate = candidate
	installer.RuntimeIdentity = RuntimeIdentity{Revision: revision, Version: "test", OS: runtime.GOOS, Arch: runtime.GOARCH}
	return installer
}

func TestNativeManagerLocatorPreservesLegacyImmutableReceipt(t *testing.T) {
	fixture := newInstallerFixture(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	releaseLocator := filepath.Join(installer.runtimeDir(fixture.first), "locator.json")
	receiptPath := filepath.Join(installer.runtimeDir(fixture.first), "receipt.json")
	for _, path := range []string{releaseLocator, installer.locatorPath()} {
		record, err := readJSON(path)
		if err != nil {
			t.Fatal(err)
		}
		if record["command_path"] != installer.commandPath() {
			t.Fatalf("new locator omitted the selected command path: %s", path)
		}
	}
	legacyLocator := legacyJSON(RuntimeLocator{Source: installer.Source, Home: installer.Home, Codex: installer.Codex, State: installer.State})
	if strings.Contains(string(legacyLocator), "command_path") {
		t.Fatal("legacy locator encoding added an absent field")
	}
	installerWrite(t, releaseLocator, string(legacyLocator), 0o600)
	installerWrite(t, installer.locatorPath(), string(legacyLocator), 0o600)
	receipt, err := readJSON(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt["locator_sha256"] = hash(legacyLocator)
	legacyReceipt := string(legacyJSON(receipt))
	installerWrite(t, receiptPath, legacyReceipt, 0o600)
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if err = installer.verifyManager(object(state["manager"])); err != nil {
		t.Fatalf("legacy locator rejected: %v", err)
	}
	if _, err = installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	if got := installerRead(t, releaseLocator); got != string(legacyLocator) {
		t.Fatal("immutable legacy descriptor was rewritten")
	}
	if got := installerRead(t, receiptPath); got != legacyReceipt {
		t.Fatal("legacy receipt checksum was rewritten")
	}
	active, err := readJSON(installer.locatorPath())
	if err != nil {
		t.Fatal(err)
	}
	if active["command_path"] != installer.commandPath() {
		t.Fatal("active locator did not acquire the command path")
	}
}

func TestNativeManagerLocatorCommandPathValidation(t *testing.T) {
	fixture := newInstallerFixture(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	legacy := strings.TrimSuffix(strings.TrimSpace(string(legacyJSON(RuntimeLocator{Source: installer.Source, Home: installer.Home, Codex: installer.Codex, State: installer.State}))), "}")
	for _, test := range []struct {
		name  string
		field string
		valid bool
	}{
		{name: "legacy", valid: true},
		{name: "selected command", field: fmt.Sprintf(`,"command_path":%q`, installer.commandPath()), valid: true},
		{name: "other home", field: `,"command_path":"/another/home/.local/bin/cw"`},
		{name: "empty", field: `,"command_path":""`},
		{name: "null", field: `,"command_path":null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := installer.decodeRuntimeLocator([]byte(legacy + test.field + "}"))
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

func TestNativeManagerLegacyShellOverridePreservesBothProfiles(t *testing.T) {
	for _, shell := range []string{"none", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			original := fixture.seedUserFiles(t)
			zsh := filepath.Join(fixture.paths.Home, ".zshrc")
			installerWrite(t, zsh, "# personal zsh bytes\n", 0o604)
			fixture.execute(t, "setup") // Legacy Bash wrapper.
			bash := filepath.Join(fixture.paths.Home, ".bashrc")
			installerAppend(t, bash, "# independent bash edit\n")
			installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
			installer.Shell = shell
			t.Setenv("ZDOTDIR", "")
			if _, err := installer.Setup(true); err != nil {
				t.Fatal(err)
			}
			if got := installerRead(t, bash); got != original[bash]+"# independent bash edit\n" {
				t.Fatalf("legacy Bash block was retained or unrelated bytes lost: %q", got)
			}
			state, err := installer.state(true)
			if err != nil {
				t.Fatal(err)
			}
			profile := object(object(state["manager"])["profile"])
			if shell == "none" {
				if profile != nil || installerRead(t, zsh) != "# personal zsh bytes\n" {
					t.Fatal("shell none enrolled a profile")
				}
			} else if profile["shell"] != "zsh" || !strings.Contains(installerRead(t, zsh), profileStart) {
				t.Fatal("explicit Zsh profile was not enrolled")
			}
			installerAppend(t, zsh, "# independent zsh edit\n")
			if _, err = installer.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if got := installerRead(t, bash); got != original[bash]+"# independent bash edit\n" {
				t.Fatalf("uninstall changed Bash bytes: %q", got)
			}
			if got := installerRead(t, zsh); got != "# personal zsh bytes\n# independent zsh edit\n" {
				t.Fatalf("uninstall changed Zsh bytes: %q", got)
			}
			info, err := os.Stat(zsh)
			if err != nil || info.Mode().Perm() != 0o604 {
				t.Fatalf("Zsh mode changed: %v", err)
			}
		})
	}
}

func TestNativeManagerInterruptedShellMigrationRestoresLegacy(t *testing.T) {
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	zsh := filepath.Join(fixture.paths.Home, ".zshrc")
	installerWrite(t, zsh, "# zsh original\n", 0o600)
	fixture.execute(t, "setup")
	legacyState, err := fixture.installer(true).state(true)
	if err != nil {
		t.Fatal(err)
	}
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	installer.Shell = "zsh"
	t.Setenv("ZDOTDIR", "")
	t.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
	if _, err = installer.Setup(true); err == nil {
		t.Fatal("fault did not interrupt shell migration")
	}
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	bash := filepath.Join(fixture.paths.Home, ".bashrc")
	installerAppend(t, bash, "# later bash bytes\n")
	installerAppend(t, zsh, "# later zsh bytes\n")
	if _, err = installer.Recover(); err != nil {
		t.Fatal(err)
	}
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(state, legacyState) {
		t.Fatal("recovery did not restore exact legacy ownership")
	}
	got := installerRead(t, bash)
	start, end, err := aliasLocate([]byte(got), text(object(state["command_alias"])["segment"]))
	if err != nil {
		t.Fatal(err)
	}
	if unowned := got[:start] + got[end:]; unowned != original[bash]+"# later bash bytes\n" {
		t.Fatalf("Bash restoration lost independent bytes: %q", unowned)
	}
	if got := installerRead(t, zsh); got != "# zsh original\n# later zsh bytes\n" {
		t.Fatalf("Zsh reversal lost independent bytes: %q", got)
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

func TestNativeManagerMigratesLegacyAndMaintainsWithoutSource(t *testing.T) {
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	fixture.execute(t, "setup")
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	legacy, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	delete(legacy, "source") // A historical v1 record has source only in its wrapper.
	installerWrite(t, installer.statePath, string(legacyJSON(seal(legacy))), 0o600)
	if _, err = installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if state["command_alias"] != nil || object(state["manager"]) == nil {
		t.Fatal("legacy wrapper ownership was not migrated")
	}
	for path, want := range map[string]string{installer.commandPath(): filepath.Join(installer.runtimeCurrent(), "cw"), installer.runtimeCurrent(): installer.runtimeDir(fixture.first)} {
		got, err := os.Readlink(path)
		if err != nil || got != want {
			t.Fatalf("native link %s = %q, %v", path, got, err)
		}
	}
	rc := filepath.Join(fixture.paths.Home, ".bashrc")
	contents := installerRead(t, rc)
	if strings.Contains(contents, "function cw") || !strings.Contains(contents, profileStart) {
		t.Fatalf("wrapper remains: %s", contents)
	}
	before := installerSnapshot(t, fixture.base)
	if report, err := installer.Setup(true); err != nil || report["changed"] != false {
		t.Fatalf("repeat native setup: %#v, %v", report, err)
	}
	installerUnchanged(t, fixture.base, before)
	installerAppend(t, rc, "# unrelated after native enrollment\n")
	if err = os.Rename(fixture.paths.Source, fixture.paths.Source+" unavailable"); err != nil {
		t.Fatal(err)
	}
	installer.Source = filepath.Join(fixture.base, "a missing source")
	if _, err = installer.Status(); err != nil {
		t.Fatalf("source-independent status: %v", err)
	}
	if _, err = installer.Uninstall(); err != nil {
		t.Fatalf("source-independent uninstall: %v", err)
	}
	if contents = installerRead(t, rc); contents != original[rc]+"# unrelated after native enrollment\n" {
		t.Fatalf("uninstall lost rc bytes: %q", contents)
	}
	for _, path := range []string{installer.commandPath(), installer.runtimeCurrent(), installer.locatorPath(), installer.statePath} {
		if exists(path) {
			t.Fatalf("uninstall retained owned path %s", path)
		}
	}
	if _, err = installer.runtimeRelease(fixture.first); err != nil {
		t.Fatalf("uninstall removed runtime cache: %v", err)
	}
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

func TestNativeRootMigrationRollbackRetainsRuntimeAndOriginalOwnership(t *testing.T) {
	fixture := newInstallerFixture(t)
	manifestPath := filepath.Join(fixture.paths.Source, "skills-manifest.json")
	manifest := strings.ReplaceAll(installerRead(t, manifestPath), "third_party/", "vendor/")
	if err := os.Rename(filepath.Join(fixture.paths.Source, "third_party"), filepath.Join(fixture.paths.Source, "vendor")); err != nil {
		t.Fatal(err)
	}
	installerWrite(t, manifestPath, manifest, 0o644)
	fixture.git(t, "add", "-A")
	fixture.git(t, "commit", "-qm", "legacy roots")
	fixture.first = fixture.git(t, "rev-parse", "HEAD")
	var err error
	fixture.manifest, err = LoadManifest(fixture.paths.Source)
	if err != nil {
		t.Fatal(err)
	}
	fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err = installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	before, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(fixture.paths.Source, "vendor"), filepath.Join(fixture.paths.Source, "third_party")); err != nil {
		t.Fatal(err)
	}
	installerWrite(t, manifestPath, strings.ReplaceAll(manifest, "vendor/", "third_party/"), 0o644)
	fixture.git(t, "add", "-A")
	fixture.git(t, "commit", "-qm", "modern third-party roots")
	second := fixture.git(t, "rev-parse", "HEAD")
	installer = nativeFixtureInstaller(t, fixture, second, true)
	if _, err = installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range object(state["registrations"]) {
		if !equal(object(raw)["original"], object(object(before["registrations"])[name])["original"]) {
			t.Fatalf("root migration lost original ownership: %s", name)
		}
		target, err := os.Readlink(filepath.Join(installer.skills, name))
		if err != nil || target != installer.target(text(object(state["manifest_registrations"])[name])) {
			t.Fatalf("registration target %s: %q, %v", name, target, err)
		}
	}
	if err = os.Rename(fixture.paths.Source, fixture.paths.Source+" unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err = installer.Rollback(); err != nil {
		t.Fatalf("source-independent root rollback: %v", err)
	}
	state, err = installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if state["release"] != fixture.first || !equal(state["manifest_registrations"], before["manifest_registrations"]) {
		t.Fatal("rollback did not restore legacy roots")
	}
	if object(object(state["manager"])["runtime"])["revision"] != second {
		t.Fatal("skill rollback downgraded native manager")
	}
	if _, err = installer.Status(); err != nil {
		t.Fatal(err)
	}
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

func sharedConfigRuntimeFixture(t *testing.T, fixture *installerFixture, capability string) *Installer {
	t.Helper()
	return sharedConfigRuntimeFixtureFor(t, fixture, fixture.first, capability)
}

func sharedConfigRuntimeFixtureFor(t *testing.T, fixture *installerFixture, revision, capability string) *Installer {
	t.Helper()
	i := nativeFixtureInstaller(t, fixture, revision, true)
	script := "#!/bin/sh\ncase \"${1-}\" in\n" +
		"--manager-protocol) printf '%s\\n' cw-manager-v6 ;;\n" +
		"--codex-defaults-capability) printf '%s\\n' '" + capability + "' ;;\n" +
		"*) exit 2 ;;\nesac\n"
	installerWrite(t, i.RuntimeCandidate, script, 0o755)
	return i
}

func optionalAdviserPolicy(policy Object) {
	consultation := policy["consultation"].(Object)
	consultation["providers"] = Object{"jev": consultation["model"], "subq_snap": "subq-snap-preview"}
	consultation["comparison"] = "codex_evidence_and_verification"
	consultation["optional_providers"] = Object{"openai_decisions": "gpt-6-luna"}
}

func optionalAdviserRuntimeFixture(t *testing.T, fixture *installerFixture, revision, protocol, capability string, apply bool) *Installer {
	t.Helper()
	i := nativeFixtureInstaller(t, fixture, revision, apply)
	script := "#!/bin/sh\ncase \"${1-}\" in\n" +
		"--manager-protocol) printf '%s\\n' '" + protocol + "' ;;\n" +
		"--openai-decisions-capability) printf '%s\\n' '" + capability + "' ;;\n" +
		"*) exit 2 ;;\nesac\n"
	installerWrite(t, i.RuntimeCandidate, script, 0o755)
	return i
}

func TestOptionalAdviserRuntimeCapabilityFollowsSelectedPolicy(t *testing.T) {
	for _, test := range []struct {
		name, protocol, capability, message string
		optional                            bool
	}{
		{name: "historical v4", protocol: "cw-manager-v4"},
		{name: "paired v6", protocol: "cw-manager-v6"},
		{name: "optional missing capability", protocol: "cw-manager-v6", optional: true, message: OpenAIDecisionsCapability},
		{name: "optional older capability", protocol: "cw-manager-v6", capability: "cw-openai-decisions-v0", optional: true, message: OpenAIDecisionsCapability},
		{name: "optional unbounded output", protocol: "cw-manager-v6", capability: strings.Repeat("x", 4097), optional: true, message: OpenAIDecisionsCapability},
		{name: "optional legacy protocol", protocol: "cw-manager-v5", capability: OpenAIDecisionsCapability, optional: true, message: "native v6 runtime"},
		{name: "optional supported", protocol: "cw-manager-v6", capability: OpenAIDecisionsCapability, optional: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			if test.optional {
				editModelPolicy(t, filepath.Join(fixture.paths.Source, text(fixture.manifest["model_policy"])), optionalAdviserPolicy)
			}
			i := optionalAdviserRuntimeFixture(t, fixture, fixture.first, test.protocol, test.capability, true)
			before := installerSnapshot(t, fixture.base)
			err := i.verifyPolicyRuntime(i.RuntimeCandidate, fixture.paths.Source, fixture.manifest)
			if test.message == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("selected policy accepted an incompatible runtime: %v", err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestOptionalAdviserSetupPreservesOwnershipUntilCapableRuntimeIsAvailable(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply=%t", apply), func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			original := nativeFixtureInstaller(t, fixture, fixture.first, true)
			if _, err := original.Setup(true); err != nil {
				t.Fatal(err)
			}
			revision := commitModelPolicy(t, fixture, optionalAdviserPolicy, "Optional adviser policy")
			i := optionalAdviserRuntimeFixture(t, fixture, revision, "cw-manager-v6", "", apply)
			before := installerSnapshot(t, fixture.base)
			report, err := i.Setup(true)
			if apply {
				if err == nil || !strings.Contains(err.Error(), OpenAIDecisionsCapability) {
					t.Fatalf("optional policy activated an incapable exact runtime: %v", err)
				}
			} else if err != nil || report["dry_run"] != true || object(report["consultation_runtime"])["changed"] != false {
				t.Fatalf("optional policy preview: %#v %v", report, err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestOptionalAdviserPolicyActivatesWithUnchangedNativeFormatsAndRollsBack(t *testing.T) {
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	i := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	revision := commitModelPolicy(t, fixture, optionalAdviserPolicy, "Optional adviser policy")
	i = optionalAdviserRuntimeFixture(t, fixture, revision, "cw-manager-v6", OpenAIDecisionsCapability, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := i.state(true)
	if err != nil || state["release"] != revision {
		t.Fatalf("optional policy did not activate: %#v %v", state, err)
	}
	for _, field := range []string{"optional_providers", "openai_decisions", "openai_decisions_capability"} {
		if _, present := state[field]; present {
			t.Fatalf("optional adviser leaked into ownership field %s", field)
		}
	}
	receipt, err := readJSON(filepath.Join(i.runtimeDir(revision), "receipt.json"))
	if err != nil || integer(receipt["version"]) != 2 || receipt["manager_protocol"] != "cw-manager-v6" || len(receipt) != 6 {
		t.Fatalf("optional adviser changed v6 receipt format: %#v %v", receipt, err)
	}
	if _, err := i.Status(); err != nil {
		t.Fatalf("optional policy status: %v", err)
	}
	if _, err := i.Rollback(); err != nil {
		t.Fatalf("optional policy rollback: %v", err)
	}
	state, err = i.state(true)
	if err != nil || state["release"] != fixture.first || object(object(state["manager"])["runtime"])["revision"] != revision {
		t.Fatalf("rollback changed manager or failed to restore historical policy: %#v %v", state, err)
	}
}

func requiredThreeAdviserPolicy(policy Object) {
	optionalAdviserPolicy(policy)
	consultation := policy["consultation"].(Object)
	delete(consultation, "optional_providers")
	consultation["providers"].(Object)["openai_decisions"] = "gpt-6-luna"
}

func threeAdviserRuntimeFixture(t *testing.T, fixture *installerFixture, revision, protocol, capability string, apply bool) *Installer {
	t.Helper()
	i := nativeFixtureInstaller(t, fixture, revision, apply)
	script := "#!/bin/sh\ncase \"${1-}\" in\n" +
		"--manager-protocol) printf '%s\\n' '" + protocol + "' ;;\n" +
		"--openai-decisions-capability) printf '%s\\n' '" + OpenAIDecisionsCapability + "' ;;\n" +
		"--three-adviser-capability) printf '%s\\n' '" + capability + "' ;;\n" +
		"*) exit 2 ;;\nesac\n"
	installerWrite(t, i.RuntimeCandidate, script, 0o755)
	return i
}

func TestThreeAdviserRuntimeRequiresDistinctCapability(t *testing.T) {
	for _, test := range []struct{ name, protocol, capability, message string }{
		{name: "optional-only manager", protocol: "cw-manager-v6", message: ThreeAdviserCapability},
		{name: "older capability", protocol: "cw-manager-v6", capability: "cw-three-adviser-v0", message: ThreeAdviserCapability},
		{name: "optional capability value", protocol: "cw-manager-v6", capability: OpenAIDecisionsCapability, message: ThreeAdviserCapability},
		{name: "unbounded output", protocol: "cw-manager-v6", capability: strings.Repeat("x", 4097), message: ThreeAdviserCapability},
		{name: "legacy protocol", protocol: "cw-manager-v5", capability: ThreeAdviserCapability, message: "native v6 runtime"},
		{name: "supported", protocol: "cw-manager-v6", capability: ThreeAdviserCapability},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			editModelPolicy(t, filepath.Join(fixture.paths.Source, text(fixture.manifest["model_policy"])), requiredThreeAdviserPolicy)
			i := threeAdviserRuntimeFixture(t, fixture, fixture.first, test.protocol, test.capability, true)
			if test.name == "optional-only manager" {
				i = optionalAdviserRuntimeFixture(t, fixture, fixture.first, test.protocol, OpenAIDecisionsCapability, true)
			}
			before := installerSnapshot(t, fixture.base)
			err := i.verifyPolicyRuntime(i.RuntimeCandidate, fixture.paths.Source, fixture.manifest)
			if test.message == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("required triple accepted an incompatible runtime: %v", err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestThreeAdviserSetupPreservesOwnershipUntilCapableRuntimeIsAvailable(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply=%t", apply), func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			original := nativeFixtureInstaller(t, fixture, fixture.first, true)
			if _, err := original.Setup(true); err != nil {
				t.Fatal(err)
			}
			revision := commitModelPolicy(t, fixture, requiredThreeAdviserPolicy, "Required three-adviser policy")
			i := optionalAdviserRuntimeFixture(t, fixture, revision, "cw-manager-v6", OpenAIDecisionsCapability, apply)
			before := installerSnapshot(t, fixture.base)
			report, err := i.Setup(true)
			if apply {
				if err == nil || !strings.Contains(err.Error(), ThreeAdviserCapability) {
					t.Fatalf("required triple activated an optional-only exact runtime: %v", err)
				}
			} else if err != nil || report["dry_run"] != true || object(report["consultation_runtime"])["changed"] != false {
				t.Fatalf("required triple preview: %#v %v", report, err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestThreeAdviserPolicyKeepsNativeFormatsAndHistoricalRollback(t *testing.T) {
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	i := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	optionalRevision := commitModelPolicy(t, fixture, optionalAdviserPolicy, "Optional OpenAI adviser policy")
	i = optionalAdviserRuntimeFixture(t, fixture, optionalRevision, "cw-manager-v6", OpenAIDecisionsCapability, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	revision := commitModelPolicy(t, fixture, requiredThreeAdviserPolicy, "Required three-adviser policy")
	i = threeAdviserRuntimeFixture(t, fixture, revision, "cw-manager-v6", ThreeAdviserCapability, true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	receipt, err := readJSON(filepath.Join(i.runtimeDir(revision), "receipt.json"))
	if err != nil || integer(receipt["version"]) != 2 || receipt["manager_protocol"] != "cw-manager-v6" || len(receipt) != 6 {
		t.Fatalf("required triple changed v6 receipt format: %#v %v", receipt, err)
	}
	if err := os.Rename(fixture.paths.Source, fixture.paths.Source+" unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Status(); err != nil {
		t.Fatalf("required triple status without source: %v", err)
	}
	for _, expected := range []string{optionalRevision, fixture.first} {
		if _, err := i.Rollback(); err != nil {
			t.Fatalf("historical policy rollback without source: %v", err)
		}
		state, err := i.state(true)
		if err != nil || state["release"] != expected || object(object(state["manager"])["runtime"])["revision"] != revision {
			t.Fatalf("historical rollback changed manager or policy: %#v %v", state, err)
		}
		for _, field := range []string{"providers", "optional_providers", "three_adviser_capability"} {
			if _, present := state[field]; present {
				t.Fatalf("required triple leaked into ownership field %s", field)
			}
		}
	}
}

func TestSharedConfigRuntimeRejectsIncapableCandidates(t *testing.T) {
	for _, capability := range []string{"", "cw-codex-defaults-v0", strings.Repeat("x", 4097), CodexDefaultsCapability} {
		name := capability
		if name == "" {
			name = "missing"
		} else if len(name) > 64 {
			name = "unbounded output"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			i := sharedConfigRuntimeFixture(t, fixture, capability)
			before := installerSnapshot(t, fixture.base)
			err := i.verifySharedConfigRuntime(i.RuntimeCandidate)
			if capability == CodexDefaultsCapability {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
				t.Fatalf("incapable candidate accepted: %v", err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
	fixture := newInstallerFixture(t)
	i := sharedConfigRuntimeFixture(t, fixture, CodexDefaultsCapability)
	installerWrite(t, i.RuntimeCandidate, strings.ReplaceAll(installerRead(t, i.RuntimeCandidate), "cw-manager-v6", "cw-manager-v5"), 0o755)
	if err := i.verifySharedConfigRuntime(i.RuntimeCandidate); err == nil || !strings.Contains(err.Error(), "native v6 runtime") {
		t.Fatalf("shared config accepted a legacy manager: %v", err)
	}
}

func TestSharedConfigManagerPlanRejectsIncapableReplacement(t *testing.T) {
	for _, marker := range []struct {
		name, key string
		value     any
	}{
		{name: "shared metadata", key: "shared_config", value: Object{}},
		{name: "existing original config", key: "config_original_exists", value: true},
		{name: "absent original config", key: "config_original_exists", value: false},
	} {
		t.Run(marker.name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			i := sharedConfigRuntimeFixture(t, fixture, CodexDefaultsCapability)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			state, err := i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			state[marker.key] = marker.value
			i = sharedConfigRuntimeFixture(t, fixture, "")
			before := installerSnapshot(t, fixture.base)
			if _, _, err := i.managerPlan(state); err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
				t.Fatalf("shared config ownership accepted an incapable replacement: %v", err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestSharedConfigRecoveryRequiresCapableStagedBinary(t *testing.T) {
	for _, capability := range []string{"", CodexDefaultsCapability} {
		name := capability
		if name == "" {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			i := sharedConfigRuntimeFixture(t, fixture, capability)
			if err := i.prepareRuntimeFor(fixture.first); err != nil {
				t.Fatal(err)
			}
			if err := i.stageRuntime(); err != nil {
				t.Fatal(err)
			}
			receipt, err := readJSON(filepath.Join(i.runtimeDir(fixture.first), "receipt.json"))
			if err != nil {
				t.Fatal(err)
			}
			if integer(receipt["version"]) != 2 || receipt["manager_protocol"] != "cw-manager-v6" || len(receipt) != 6 {
				t.Fatalf("shared config changed the v6 receipt schema: %v", receipt)
			}
			beforeKeys, afterKeys := Object{}, Object{}
			for _, key := range sharedConfigKeys {
				beforeKeys[key] = Object{"present": false}
				representation := "true"
				if strings.HasSuffix(key, "_model") {
					representation = `"gpt-6.1-sol"`
				}
				afterKeys[key] = Object{"present": true, "representation": representation}
			}
			operation := configOperation(i.config, beforeKeys, afterKeys, false, true, 0o600, 0o600, "codex-config")
			if err := ConfigValidateOperation(operation); err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, fixture.base)
			_, err = i.recoveryRuntimeFor([]Object{operation})
			if capability == CodexDefaultsCapability {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
					t.Fatalf("incapable staged recovery binary accepted: %v", err)
				}
				if err := i.transact("setup", []Object{operation}); err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
					t.Fatalf("incapable runtime published a shared config transaction: %v", err)
				}
				if exists(i.journalPath) || exists(i.current) {
					t.Fatal("unrecoverable shared config transaction was published")
				}
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestSharedConfigRecoveryInspectsBothStateSidesAndSavedReversals(t *testing.T) {
	fixture := newInstallerFixture(t)
	i := sharedConfigRuntimeFixture(t, fixture, "")
	if err := i.prepareRuntimeFor(fixture.first); err != nil {
		t.Fatal(err)
	}
	if err := i.stageRuntime(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"operations", "recovery_operations"} {
		for _, marker := range []struct {
			name, key string
			value     any
		}{
			{name: "shared metadata", key: "shared_config", value: Object{}},
			{name: "existing original config", key: "config_original_exists", value: true},
			{name: "absent original config", key: "config_original_exists", value: false},
		} {
			for _, side := range []string{"before", "after"} {
				t.Run(field+"/"+marker.name+"/"+side, func(t *testing.T) {
					state := Object{marker.key: marker.value}
					op := pathOperation(i.statePath, Object{"kind": "absent"}, Object{"kind": "absent"}, "state")
					op[side] = Object{"kind": "file", "data": encode(legacyJSON(state)), "mode": 0o600}
					journal := Object{"version": 2, "recovery_runtime": asObject(i.RuntimeIdentity), field: []any{op}}
					before := installerSnapshot(t, fixture.base)
					if err := i.verifySharedConfigJournalRuntime(journal); err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
						t.Fatalf("incapable runtime accepted %s shared state in %s: %v", side, field, err)
					}
					installerUnchanged(t, fixture.base, before)
				})
			}
		}
		t.Run(field+"/config operation", func(t *testing.T) {
			journal := Object{"version": 2, "recovery_runtime": asObject(i.RuntimeIdentity), field: []any{Object{"kind": sharedConfigOperationKind, "path": i.config}}}
			if err := i.verifySharedConfigJournalRuntime(journal); err == nil || !strings.Contains(err.Error(), CodexDefaultsCapability) {
				t.Fatalf("incapable runtime accepted a config operation in %s: %v", field, err)
			}
		})
	}
	if err := i.verifySharedConfigJournalRuntime(Object{"version": 1, "operations": []any{Object{"kind": "model_config", "path": i.config}}}); err != nil {
		t.Fatalf("historical model-only journal gained a config capability requirement: %v", err)
	}
}
