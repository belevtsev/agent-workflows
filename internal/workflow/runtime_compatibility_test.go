package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These fixtures simulate the v7 on-disk contract using the unchanged receipt,
// locator and manager schemas. The executables are protocol-only shell fixtures,
// not historical v7 manager binaries; the lifecycle operations run current Go.
func rewriteRuntimeFixtureProtocol(t *testing.T, installer *Installer, revision, protocol string) {
	t.Helper()
	dir := installer.runtimeDir(revision)
	binary := "#!/bin/sh\nif [ \"${1-}\" = --manager-protocol ]; then printf '%s\\n' '" + protocol + "'; fi\n"
	installerWrite(t, filepath.Join(dir, "cw"), binary, 0o755)
	receiptPath := filepath.Join(dir, "receipt.json")
	receipt, err := readJSON(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt["manager_protocol"], receipt["sha256"] = protocol, hash([]byte(binary))
	installerWrite(t, receiptPath, string(legacyJSON(seal(receipt))), 0o600)
}

func sealedV7RuntimeFixture(t *testing.T, fixture *installerFixture) *Installer {
	t.Helper()
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	rewriteRuntimeFixtureProtocol(t, installer, fixture.first, "cw-manager-v7")
	reader := fixture.installer(true)
	reader.Context = t.Context()
	return reader
}

func assertRuntimeFixtureRemoved(t *testing.T, installer *Installer, originals map[string]string) {
	t.Helper()
	for path, want := range originals {
		if got := installerRead(t, path); got != want {
			t.Fatalf("original bytes changed at %s: %q", path, got)
		}
	}
	for _, path := range []string{installer.statePath, installer.journalPath, installer.current, installer.runtimeCurrent(), installer.commandPath(), installer.locatorPath()} {
		if exists(path) {
			t.Fatalf("owned path remains: %s", path)
		}
	}
}

func TestV7RuntimeSetupUpgradeAndRollback(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	originals := fixture.seedUserFiles(t)
	old := sealedV7RuntimeFixture(t, fixture)
	oldReceipt := installerRead(t, filepath.Join(old.runtimeDir(fixture.first), "receipt.json"))
	if report, err := old.Status(); err != nil || report["installed"] != true {
		t.Fatalf("v7 status: %v, %v", report, err)
	}
	second := fixture.newRelease(t)
	installer := nativeFixtureInstaller(t, fixture, second, false)
	before := installerSnapshot(t, fixture.base)
	if report, err := installer.Setup(true); err != nil || report["dry_run"] != true || report["release"] != second {
		t.Fatalf("v7 setup preview: %v, %v", report, err)
	}
	installerUnchanged(t, fixture.base, before)
	installer.Apply = true
	if _, err := installer.Setup(true); err != nil {
		t.Fatalf("v7 to v8 activation: %v", err)
	}
	assertManagerProtocol(t, installer, "cw-manager-v8")
	state, err := installer.state(true)
	if err != nil || state["release"] != second || object(object(state["manager"])["runtime"])["revision"] != second {
		t.Fatalf("v8 activation state: %v, %v", state, err)
	}
	manager := clone(object(state["manager"]))
	if _, err := installer.Rollback(); err != nil {
		t.Fatal(err)
	}
	state, err = installer.state(true)
	if err != nil || state["release"] != fixture.first || !equal(state["manager"], manager) {
		t.Fatalf("skill rollback changed the v8 manager: %v, %v", state, err)
	}
	assertManagerProtocol(t, installer, "cw-manager-v8")
	if got := installerRead(t, filepath.Join(old.runtimeDir(fixture.first), "receipt.json")); got != oldReceipt {
		t.Fatal("upgrade or rollback rewrote the immutable v7 receipt")
	}
	if _, err := installer.Status(); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Uninstall(); err != nil {
		t.Fatal(err)
	}
	assertRuntimeFixtureRemoved(t, installer, originals)
}

func TestV7RuntimeRollbackAndUninstall(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	originals := fixture.seedUserFiles(t)
	installer := sealedV7RuntimeFixture(t, fixture)
	fixture.newRelease(t)
	// Without a newly acquired candidate, operations retain the existing manager.
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Rollback(); err != nil {
		t.Fatalf("rollback with a v7 recovery runtime: %v", err)
	}
	assertManagerProtocol(t, installer, "cw-manager-v7")
	if _, err := installer.Uninstall(); err != nil {
		t.Fatalf("uninstall with a v7 recovery runtime: %v", err)
	}
	assertRuntimeFixtureRemoved(t, installer, originals)
}

func TestV7RuntimeInterruptedUpgradeRecovery(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"after-manager-runtime", "after-state"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			old := sealedV7RuntimeFixture(t, fixture)
			beforeState := installerRead(t, old.statePath)
			second := fixture.newRelease(t)
			installer := nativeFixtureInstaller(t, fixture, second, true)
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", boundary)
			if _, err := installer.Setup(true); err == nil || !strings.Contains(err.Error(), boundary) {
				t.Fatalf("upgrade boundary did not interrupt: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			if err := os.Rename(installer.Source, installer.Source+" unavailable"); err != nil {
				t.Fatal(err)
			}
			recovery := fixture.installer(true)
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-manager-runtime")
			if _, err := recovery.Recover(); err == nil || !strings.Contains(err.Error(), "after-recover-manager-runtime") {
				t.Fatalf("runtime reversal did not interrupt: %v", err)
			}
			fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
			assertManagerProtocol(t, recovery, "cw-manager-v7")
			if _, err := recovery.Recover(); err != nil {
				t.Fatalf("resume recovery with old v7 manager state: %v", err)
			}
			if got := installerRead(t, recovery.statePath); got != beforeState {
				t.Fatal("recovery did not restore exact v7 manager ownership state")
			}
			if _, err := recovery.Status(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV7RuntimeInterruptedJournalRecovery(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	originals := fixture.seedUserFiles(t)
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-state")
	if _, err := installer.Setup(true); err == nil {
		t.Fatal("setup boundary did not interrupt")
	}
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	// The journal identity is unchanged; its sealed runtime now simulates the v7
	// executable and receipt that an earlier manager left for recovery.
	rewriteRuntimeFixtureProtocol(t, installer, fixture.first, "cw-manager-v7")
	if err := os.Rename(installer.Source, installer.Source+" unavailable"); err != nil {
		t.Fatal(err)
	}
	recovery := fixture.installer(true)
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "after-recover-manager-runtime")
	if _, err := recovery.Recover(); err == nil || !strings.Contains(err.Error(), "after-recover-manager-runtime") {
		t.Fatalf("v7 journal recovery did not reach the runtime reversal: %v", err)
	}
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	if _, err := recovery.Recover(); err != nil {
		t.Fatalf("resume v7 journal recovery: %v", err)
	}
	assertRuntimeFixtureRemoved(t, recovery, originals)
}

func TestRuntimeCandidatesRequireV8BeforeMutation(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v7", "cw-manager-v9"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			for _, operation := range []string{"setup", "prepare", "stage"} {
				t.Run(operation, func(t *testing.T) {
					t.Parallel()
					fixture := newInstallerFixture(t)
					fixture.seedUserFiles(t)
					installer := nativeFixtureInstallerProtocol(t, fixture, fixture.first, protocol, true)
					before := installerSnapshot(t, fixture.base)
					var err error
					switch operation {
					case "setup":
						_, err = installer.Setup(true)
					case "prepare":
						err = installer.prepareRuntimeFor(fixture.first)
					case "stage":
						err = installer.stageRuntime()
					}
					if err == nil || !strings.Contains(err.Error(), "does not support the manager protocol") {
						t.Fatalf("%s accepted candidate protocol %q: %v", operation, protocol, err)
					}
					installerUnchanged(t, fixture.base, before)
				})
			}
		})
	}
}

func TestRuntimeReceiptRejectsUnsupportedProtocols(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v9"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := sealedV7RuntimeFixture(t, fixture)
			rewriteRuntimeFixtureProtocol(t, installer, fixture.first, protocol)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Status(); err == nil {
				t.Fatalf("status accepted sealed receipt protocol %q", protocol)
			}
			if _, err := installer.Uninstall(); err == nil {
				t.Fatalf("uninstall accepted sealed receipt protocol %q", protocol)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestRecoveryRuntimeRejectsUnsupportedExecutableProtocols(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v9"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := sealedV7RuntimeFixture(t, fixture)
			dir := installer.runtimeDir(fixture.first)
			binary := "#!/bin/sh\nprintf '%s\\n' '" + protocol + "'\n"
			installerWrite(t, filepath.Join(dir, "cw"), binary, 0o755)
			receiptPath := filepath.Join(dir, "receipt.json")
			receipt, err := readJSON(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve a supported, sealed receipt so the executable capability
			// check is the boundary that rejects this recovery runtime.
			receipt["sha256"] = hash([]byte(binary))
			installerWrite(t, receiptPath, string(legacyJSON(seal(receipt))), 0o600)
			identity, err := installer.runtimeRelease(fixture.first)
			if err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, fixture.base)
			if err := installer.verifyRecoveryRuntime(identity); err == nil || !strings.Contains(err.Error(), "does not support the manager protocol") {
				t.Fatalf("recovery accepted executable protocol %q: %v", protocol, err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestRecoveryRuntimeRequiresReceiptProtocolMatch(t *testing.T) {
	t.Parallel()
	for _, receiptProtocol := range []string{"cw-manager-v7", "cw-manager-v8"} {
		t.Run(receiptProtocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := sealedV7RuntimeFixture(t, fixture)
			executableProtocol := "cw-manager-v8"
			if receiptProtocol == executableProtocol {
				executableProtocol = "cw-manager-v7"
			}
			rewriteRuntimeFixtureProtocol(t, installer, fixture.first, executableProtocol)
			receiptPath := filepath.Join(installer.runtimeDir(fixture.first), "receipt.json")
			receipt, err := readJSON(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			receipt["manager_protocol"] = receiptProtocol
			installerWrite(t, receiptPath, string(legacyJSON(seal(receipt))), 0o600)
			identity, err := installer.runtimeRelease(fixture.first)
			if err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, fixture.base)
			if err := installer.verifyRecoveryRuntime(identity); err == nil || !strings.Contains(err.Error(), "does not support the manager protocol") {
				t.Fatalf("recovery accepted receipt %s for executable %s: %v", receiptProtocol, executableProtocol, err)
			}
			if _, err := installer.Uninstall(); err == nil {
				t.Fatal("uninstall accepted a mismatched recovery runtime")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestV7RuntimeReceiptRetainsIntegrityAndOwnershipChecks(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"seal", "binary", "binary-mode", "locator-seal", "locator-hash", "locator-roots", "locator-mode", "receipt-mode", "extra-file", "identity", "command-owner", "active-locator-owner"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer := sealedV7RuntimeFixture(t, fixture)
			dir := installer.runtimeDir(fixture.first)
			receiptPath, locatorPath := filepath.Join(dir, "receipt.json"), filepath.Join(dir, "locator.json")
			switch change {
			case "seal":
				receipt, err := readJSON(receiptPath)
				if err != nil {
					t.Fatal(err)
				}
				receipt["manager_protocol"] = ManagerProtocol
				installerWrite(t, receiptPath, string(legacyJSON(receipt)), 0o600)
			case "binary":
				installerAppend(t, filepath.Join(dir, "cw"), "# changed\n")
			case "binary-mode", "locator-mode", "receipt-mode":
				path := filepath.Join(dir, "cw")
				if change == "locator-mode" {
					path = locatorPath
				} else if change == "receipt-mode" {
					path = receiptPath
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "locator-seal", "locator-hash", "locator-roots", "active-locator-owner":
				path := locatorPath
				if change == "active-locator-owner" {
					path = installer.locatorPath()
				}
				locator, err := readJSON(path)
				if err != nil {
					t.Fatal(err)
				}
				locator["source"] = filepath.Join(fixture.base, "other source")
				if change == "locator-roots" {
					locator["home"] = filepath.Join(fixture.base, "other home")
				}
				if change != "locator-seal" {
					locator = seal(locator)
				}
				data := legacyJSON(locator)
				installerWrite(t, path, string(data), 0o600)
				if change == "locator-roots" {
					receipt, err := readJSON(receiptPath)
					if err != nil {
						t.Fatal(err)
					}
					receipt["locator_sha256"] = hash(data)
					installerWrite(t, receiptPath, string(legacyJSON(seal(receipt))), 0o600)
				}
			case "extra-file":
				installerWrite(t, filepath.Join(dir, "extra"), "unexpected", 0o600)
			case "identity":
				state, err := installer.state(true)
				if err != nil {
					t.Fatal(err)
				}
				object(object(state["manager"])["runtime"])["version"] = "different"
				installerWrite(t, installer.statePath, string(legacyJSON(seal(state))), 0o600)
			case "command-owner":
				if err := os.Remove(installer.commandPath()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(fixture.base, "other command"), installer.commandPath()); err != nil {
					t.Fatal(err)
				}
			}
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Status(); err == nil {
				t.Fatalf("v7 status accepted changed %s", change)
			}
			if _, err := installer.Uninstall(); err == nil {
				t.Fatalf("v7 uninstall accepted changed %s", change)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}
