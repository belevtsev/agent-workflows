package workflow

import (
	"path/filepath"
	"strings"
	"testing"
)

// Only cw-manager-v10 remains. Receipts and executables written by the
// v7, v8 and v9 managers are unsupported, so the historical v7 fixtures and
// their readable-protocol tests were deleted with that support.

// A candidate that does not answer cw-manager-v10 lacks the capability the
// standalone manager requires.
const candidateCapabilityRefusal = "standalone manager lacks the required capability"

// A sealed receipt that records any protocol but cw-manager-v10 no longer
// names the runtime its path identifies.
const receiptIdentityRefusal = "native runtime receipt identity differs from its path"

// installedRuntimeFixture is a standalone installation of the first fixture
// release whose sealed runtime the tests below change.
func installedRuntimeFixture(t *testing.T, fixture *installerFixture) (*Installer, string) {
	t.Helper()
	installer := nativeFixtureInstaller(t, fixture, fixture.first, true)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Status(); err != nil {
		t.Fatalf("unchanged runtime: %v", err)
	}
	return installer, fixtureRuntimeDir(t, fixture.paths.State, fixture.first)
}

// setRuntimeProtocols makes the sealed runtime's executable report one
// protocol and its resealed receipt record another, keeping the receipt's
// executable digest exact so only the protocols can disagree.
func setRuntimeProtocols(t *testing.T, fixture *installerFixture, dir, receiptProtocol, executableProtocol string) {
	t.Helper()
	binary := fixtureManagerScript(t, executableProtocol, fixture.release(t, fixture.first).identity)
	fixtureRewrite(t, filepath.Join(dir, "cw"), []byte(binary))
	receiptPath := filepath.Join(dir, "receipt.json")
	receipt, err := readJSON(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt["manager_protocol"], receipt["sha256"] = receiptProtocol, hash([]byte(binary))
	fixtureRewrite(t, receiptPath, legacyJSON(seal(receipt)))
}

func TestProductionRuntimeCandidatesRequireV10BeforeMutation(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v7", "cw-manager-v8", "cw-manager-v9", "cw-manager-v11"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			// stageRuntime is not called directly: the bundle identity it needs is
			// loaded only inside setup, install and update.
			for _, operation := range []string{"setup", "update"} {
				t.Run(operation, func(t *testing.T) {
					t.Parallel()
					fixture := newInstallerFixture(t)
					fixture.seedUserFiles(t)
					revision := fixture.first
					if operation == "update" {
						if _, err := nativeFixtureInstaller(t, fixture, fixture.first, true).Setup(true); err != nil {
							t.Fatal(err)
						}
						revision = fixture.newRelease(t)
					}
					installer := nativeFixtureInstallerProtocol(t, fixture, revision, protocol, true)
					before := installerSnapshot(t, fixture.base)
					var err error
					switch operation {
					case "setup":
						_, err = installer.Setup(true)
					case "update":
						_, err = installer.Update()
					}
					if err == nil || !strings.Contains(err.Error(), candidateCapabilityRefusal) {
						t.Fatalf("%s accepted candidate protocol %q: %v", operation, protocol, err)
					}
					installerUnchanged(t, fixture.base, before)
					if operation == "update" && fixture.activeRevision(t) != fixture.first {
						t.Fatal("refused update changed the active release")
					}
				})
			}
		})
	}
}

func TestRuntimeReceiptRejectsUnsupportedProtocols(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v7", "cw-manager-v8", "cw-manager-v9", "cw-manager-v11"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer, dir := installedRuntimeFixture(t, fixture)
			setRuntimeProtocols(t, fixture, dir, protocol, protocol)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Status(); err == nil || !strings.Contains(err.Error(), receiptIdentityRefusal) {
				t.Fatalf("status accepted sealed receipt protocol %q: %v", protocol, err)
			}
			if _, err := installer.Uninstall(); err == nil || !strings.Contains(err.Error(), receiptIdentityRefusal) {
				t.Fatalf("uninstall accepted sealed receipt protocol %q: %v", protocol, err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestRecoveryRuntimeRejectsUnsupportedExecutableProtocols(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"", "unknown", "cw-manager-v6", "cw-manager-v7", "cw-manager-v8", "cw-manager-v9", "cw-manager-v10", "cw-manager-v11"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer, dir := installedRuntimeFixture(t, fixture)
			// Keep a supported, sealed v10 receipt so the executable capability
			// check is the boundary that rejects this recovery runtime.
			setRuntimeProtocols(t, fixture, dir, ManagerProtocol, protocol)
			before := installerSnapshot(t, fixture.base)
			err := installer.verifyRecoveryRuntime(installer.RuntimeIdentity)
			if protocol == ManagerProtocol {
				if err != nil {
					t.Fatalf("supported recovery executable rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "does not support the manager protocol") {
				t.Fatalf("recovery accepted executable protocol %q: %v", protocol, err)
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestRecoveryRuntimeRequiresReceiptProtocolMatch(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ receipt, executable string }{
		{ManagerProtocol, "cw-manager-v9"},
		{ManagerProtocol, "cw-manager-v8"},
		{"cw-manager-v7", ManagerProtocol},
		{"cw-manager-v8", ManagerProtocol},
		{"cw-manager-v9", ManagerProtocol},
		{"cw-manager-v11", ManagerProtocol},
		{ManagerProtocol, ManagerProtocol},
	} {
		t.Run("receipt "+test.receipt+" executable "+test.executable, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			installer, dir := installedRuntimeFixture(t, fixture)
			setRuntimeProtocols(t, fixture, dir, test.receipt, test.executable)
			before := installerSnapshot(t, fixture.base)
			err := installer.verifyRecoveryRuntime(installer.RuntimeIdentity)
			switch {
			case test.receipt == test.executable:
				if err != nil {
					t.Fatalf("matching v10 receipt and executable rejected: %v", err)
				}
				return
			case test.receipt == ManagerProtocol:
				// A supported receipt names the executable's protocol mismatch.
				if err == nil || !strings.Contains(err.Error(), "does not support the manager protocol") {
					t.Fatalf("recovery accepted receipt %s for executable %s: %v", test.receipt, test.executable, err)
				}
			default:
				// An unsupported receipt protocol is refused whatever the
				// executable reports.
				if err == nil || !strings.Contains(err.Error(), receiptIdentityRefusal) {
					t.Fatalf("recovery accepted receipt %s for executable %s: %v", test.receipt, test.executable, err)
				}
			}
			if _, err := installer.Uninstall(); err == nil {
				t.Fatal("uninstall accepted a mismatched recovery runtime")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}
