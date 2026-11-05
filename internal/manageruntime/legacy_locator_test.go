package manageruntime

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// legacyInstallationMessage is the exact R2.1 refusal text shared with workflow.
const legacyInstallationMessage = `this installation was made by an older cw that this release no longer supports; run "cw uninstall" with your existing cw, then install again`

// legacyLayout installs a fixture executable at runtime/releases/<key>/cw
// under a fresh state root and returns the root, the runtime root and the
// release directory.
func legacyLayout(t *testing.T, key string) (string, string, string) {
	t.Helper()
	root := realTemp(t)
	runtimeRoot := filepath.Join(root, "runtime")
	releaseRoot := filepath.Join(runtimeRoot, "releases", key)
	if err := os.MkdirAll(releaseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(releaseRoot, "cw"), managerScript(true), 0755)
	return root, runtimeRoot, releaseRoot
}

// legacyLocatorFixture is the version-2 locator a pre-standalone cw wrote:
// a sealed record that names its source checkout.
func legacyLocatorFixture(t *testing.T, root string) (Locator, []byte) {
	t.Helper()
	locator := Locator{Version: 2, Source: filepath.Join(root, "source"), Home: root, State: root, CommandPath: filepath.Join(root, ".local", "bin", "cw")}
	data := sealedLocatorFixture(t, &locator)
	return locator, data
}

func TestLegacyInstallationSentinelMessage(t *testing.T) {
	if ErrLegacyInstallation == nil || ErrLegacyInstallation.Error() != legacyInstallationMessage {
		t.Fatalf("legacy sentinel message = %v", ErrLegacyInstallation)
	}
	if errors.Is(ErrLegacyInstallation, ErrLocatorMissing) || errors.Is(ErrLegacyInstallation, os.ErrNotExist) {
		t.Fatal("legacy sentinel aliases another resolution outcome")
	}
}

// R2.3: any decodable version-2 locator is refused with ErrLegacyInstallation,
// before path, seal, command-path, state-root and release-key checks, and
// Resolve leaves the legacy layout byte-identical.
func TestResolveRefusesVersionTwoLocatorAsLegacyInstallation(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		primary bool
		link    bool
		mutate  func(t *testing.T, root string, locator Locator, sealed []byte) []byte
	}{
		{name: "legacy revision-only release primary", key: fixtureSHA, primary: true},
		{name: "legacy revision-only release immutable", key: fixtureSHA},
		{name: "legacy layout through native link", key: fixtureSHA, primary: true, link: true},
		{name: "standalone release key", key: fixtureReleaseKey, primary: true},
		{name: "unsealed", key: fixtureSHA, primary: true, mutate: func(t *testing.T, _ string, locator Locator, _ []byte) []byte {
			locator.Integrity = ""
			data, err := json.Marshal(locator)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}},
		{name: "wrong seal", key: fixtureSHA, primary: true, mutate: func(t *testing.T, _ string, locator Locator, _ []byte) []byte {
			locator.Integrity = strings.Repeat("0", 64)
			data, err := json.Marshal(locator)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}},
		{name: "foreign state root", key: fixtureSHA, primary: true, mutate: func(t *testing.T, root string, locator Locator, _ []byte) []byte {
			locator.State = filepath.Join(root, "elsewhere")
			return sealedLocatorFixture(t, &locator)
		}},
		{name: "foreign command path", key: fixtureSHA, primary: true, mutate: func(t *testing.T, _ string, locator Locator, _ []byte) []byte {
			locator.CommandPath = "/another/home/.local/bin/cw"
			return sealedLocatorFixture(t, &locator)
		}},
		{name: "relative roots", key: fixtureSHA, primary: true, mutate: func(t *testing.T, _ string, locator Locator, _ []byte) []byte {
			locator.Home, locator.State, locator.Source = "relative/home", "relative/state", "relative/source"
			return sealedLocatorFixture(t, &locator)
		}},
		{name: "empty source", key: fixtureSHA, primary: true, mutate: func(t *testing.T, _ string, locator Locator, _ []byte) []byte {
			locator.Source = ""
			return sealedLocatorFixture(t, &locator)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, runtimeRoot, releaseRoot := legacyLayout(t, test.key)
			locator, data := legacyLocatorFixture(t, root)
			if test.mutate != nil {
				data = test.mutate(t, root, locator, data)
			}
			descriptor := filepath.Join(releaseRoot, "locator.json")
			if test.primary {
				descriptor = filepath.Join(runtimeRoot, "locator.json")
			}
			writeFixture(t, descriptor, string(data), 0600)
			executable := filepath.Join(releaseRoot, "cw")
			if test.link {
				if err := os.Symlink(releaseRoot, filepath.Join(runtimeRoot, "current")); err != nil {
					t.Fatal(err)
				}
				executable = filepath.Join(root, "cw")
				if err := os.Symlink(filepath.Join(runtimeRoot, "current", "cw"), executable); err != nil {
					t.Fatal(err)
				}
			}
			before := bootstrapTree(t, root)
			got, err := Resolve(executable)
			if !errors.Is(err, ErrLegacyInstallation) || !strings.Contains(err.Error(), legacyInstallationMessage) {
				t.Fatalf("version-2 locator not refused as legacy: %+v %v", got, err)
			}
			if errors.Is(err, ErrLocatorMissing) || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy refusal looks like an uninstalled layout: %v", err)
			}
			if got != (Locator{}) {
				t.Fatalf("legacy refusal returned roots: %+v", got)
			}
			if after := bootstrapTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatal("resolving a legacy locator changed the installation")
			}
		})
	}
}

// R2.3 ordering: a version-2 locator that is not a safe regular file or that
// does not strictly decode keeps the earlier, non-legacy error.
func TestResolveVersionTwoLocatorStillRequiresSafeStrictDescriptor(t *testing.T) {
	t.Run("unsafe mode", func(t *testing.T) {
		root, runtimeRoot, releaseRoot := legacyLayout(t, fixtureSHA)
		_, data := legacyLocatorFixture(t, root)
		descriptor := filepath.Join(runtimeRoot, "locator.json")
		writeFixture(t, descriptor, string(data), 0644)
		if err := os.Chmod(descriptor, 0644); err != nil {
			t.Fatal(err)
		}
		_, err := Resolve(filepath.Join(releaseRoot, "cw"))
		if err == nil || errors.Is(err, ErrLegacyInstallation) || !strings.Contains(err.Error(), "unsafe installed manager locator") {
			t.Fatalf("unsafe version-2 descriptor: %v", err)
		}
	})
	t.Run("unknown member", func(t *testing.T) {
		root, runtimeRoot, releaseRoot := legacyLayout(t, fixtureSHA)
		_, data := legacyLocatorFixture(t, root)
		changed := strings.Replace(string(data), `"source":`, `"checkout":`, 1)
		if changed == string(data) {
			t.Fatal("fixture mutation did not apply")
		}
		writeFixture(t, filepath.Join(runtimeRoot, "locator.json"), changed, 0600)
		_, err := Resolve(filepath.Join(releaseRoot, "cw"))
		if err == nil || errors.Is(err, ErrLegacyInstallation) || !strings.Contains(err.Error(), "invalid installed manager locator") {
			t.Fatalf("undecodable version-2 descriptor: %v", err)
		}
	})
}

// R2.3: versions other than 2 and 3 keep the unsupported-version error and
// are never reported as a legacy installation.
func TestResolveUnknownLocatorVersionKeepsUnsupportedError(t *testing.T) {
	for _, version := range []int{0, 1, 4, 10, -2} {
		t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
			root, runtimeRoot, releaseRoot := legacyLayout(t, fixtureReleaseKey)
			locator := currentLocatorFixture(root, "")
			locator.Version = version
			data := sealedLocatorFixture(t, &locator)
			writeFixture(t, filepath.Join(runtimeRoot, "locator.json"), string(data), 0600)
			_, err := Resolve(filepath.Join(releaseRoot, "cw"))
			if err == nil || errors.Is(err, ErrLegacyInstallation) || !strings.Contains(err.Error(), "unsupported installed manager locator version") {
				t.Fatalf("locator version %d: %v", version, err)
			}
		})
	}
}

// R2.3: a version-3 locator resolves only from a <revision>-<digest> release
// directory; the revision-only key used by the legacy layout is refused.
func TestResolveVersionThreeRejectsNonStandaloneReleaseKeys(t *testing.T) {
	for _, key := range []string{
		fixtureSHA,
		fixtureSHA + "-" + fixtureDigest[:63],
		fixtureSHA + "-" + fixtureDigest + "0",
		fixtureSHA[:39] + "-" + fixtureDigest,
		strings.Repeat("A", 40) + "-" + fixtureDigest,
		fixtureSHA + "-" + strings.ToUpper(fixtureDigest),
		fixtureSHA + "_" + fixtureDigest,
		fixtureSHA + "-" + fixtureDigest + "-extra",
		fixtureDigest + "-" + fixtureSHA,
	} {
		t.Run(key, func(t *testing.T) {
			root, runtimeRoot, releaseRoot := legacyLayout(t, key)
			locator := currentLocatorFixture(root, filepath.Join(root, "source"))
			data := sealedLocatorFixture(t, &locator)
			writeFixture(t, filepath.Join(runtimeRoot, "locator.json"), string(data), 0600)
			got, err := Resolve(filepath.Join(releaseRoot, "cw"))
			if err == nil || errors.Is(err, ErrLegacyInstallation) || !strings.Contains(err.Error(), "invalid installed manager release path") {
				t.Fatalf("release key %q: %+v %v", key, got, err)
			}
		})
	}
	t.Run("standalone key resolves", func(t *testing.T) {
		root, runtimeRoot, releaseRoot := legacyLayout(t, fixtureReleaseKey)
		locator := currentLocatorFixture(root, filepath.Join(root, "source"))
		data := sealedLocatorFixture(t, &locator)
		writeFixture(t, filepath.Join(runtimeRoot, "locator.json"), string(data), 0600)
		got, err := Resolve(filepath.Join(releaseRoot, "cw"))
		if err != nil || got != locator {
			t.Fatalf("standalone release key: %+v %v", got, err)
		}
	})
}
