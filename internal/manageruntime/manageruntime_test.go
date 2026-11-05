package manageruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fixtureSHA = "0123456789012345678901234567890123456789"

// fixtureDigest is the 64-hex bundle digest half of a standalone release key.
const fixtureDigest = "89abcdef89abcdef89abcdef89abcdef89abcdef89abcdef89abcdef89abcdef"

// fixtureReleaseKey is the only release directory form a version-3 locator
// resolves through: <40-hex revision>-<64-hex digest> (R2.3).
const fixtureReleaseKey = fixtureSHA + "-" + fixtureDigest

func realTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func archiveFixture(t *testing.T, headers []tar.Header, bodies []string) []byte {
	t.Helper()
	out := new(bytes.Buffer)
	zip := gzip.NewWriter(out)
	archive := tar.NewWriter(zip)
	for index, header := range headers {
		header.Size = int64(len(bodies[index]))
		if err := archive.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(bodies[index])); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// currentLocatorFixture returns version-3 roots for an installation whose
// state directory is root, so runtime/ is root/runtime.
func currentLocatorFixture(root, source string) Locator {
	return Locator{Version: 3, Source: source, Home: root, State: root, CommandPath: filepath.Join(root, ".local", "bin", "cw"), Config: filepath.Join(root, ".config", "agent-workflows")}
}

// R2.3: a version-3 locator next to a <revision>-<digest> release resolves
// through the native runtime/current link and keeps its optional source.
func TestResolveInstalledLocatorThroughNativeLink(t *testing.T) {
	root := realTemp(t)
	runtimeRoot := filepath.Join(root, "runtime")
	releaseRoot := filepath.Join(runtimeRoot, "releases", fixtureReleaseKey)
	if err := os.MkdirAll(releaseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(releaseRoot, "cw")
	if err := os.WriteFile(binary, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(releaseRoot, filepath.Join(runtimeRoot, "current")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cw")
	if err := os.Symlink(filepath.Join(runtimeRoot, "current", "cw"), link); err != nil {
		t.Fatal(err)
	}
	locator := currentLocatorFixture(root, filepath.Join(root, "source"))
	data := sealedLocatorFixture(t, &locator)
	if err := os.WriteFile(filepath.Join(runtimeRoot, "locator.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != locator || got.Source != filepath.Join(root, "source") {
		t.Fatalf("got %+v", got)
	}
	locator.State = filepath.Join(root, "different")
	data = sealedLocatorFixture(t, &locator)
	if err = os.WriteFile(filepath.Join(runtimeRoot, "locator.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(link); err == nil {
		t.Fatal("cross-installation locator accepted")
	}
}
func TestResolveNeverSearchesDeveloperAncestors(t *testing.T) {
	root := realTemp(t)
	binary := filepath.Join(root, "cw")
	if err := os.WriteFile(binary, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "locator.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected developer locator resolution: %v", err)
	}
}

func TestInspectRejectsWrongProtocolAndPreservesCancellation(t *testing.T) {
	for _, test := range []struct{ name, output string }{{"unknown member", fmt.Sprintf(`{"arch":"%s","built_at":"now","os":"%s","revision":"%s","version":"v1.2.3","extra":true}`, runtime.GOARCH, runtime.GOOS, fixtureSHA)}, {"wrong platform", fmt.Sprintf(`{"arch":"wrong","built_at":"now","os":"%s","revision":"%s","version":"v1.2.3"}`, runtime.GOOS, fixtureSHA)}, {"unbounded output", strings.Repeat("x", 8192)}} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(realTemp(t), "cw")
			writeFixture(t, path, "#!/bin/sh\nprintf '%s\\n' '"+test.output+"'\n", 0755)
			if _, err := Inspect(t.Context(), path); err == nil {
				t.Fatal("invalid executable protocol accepted")
			}
		})
	}
	path := filepath.Join(realTemp(t), "cw")
	writeFixture(t, path, managerScript(true), 0755)
	identity, err := Inspect(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Revision != fixtureSHA {
		t.Fatalf("wrong identity: %+v", identity)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Inspect(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

// R2.3: the immutable release locator of a <revision>-<digest> release still
// recovers version-3 roots after the active locator is removed.
func TestResolveImmutableReleaseLocatorAfterActiveLocatorRemoved(t *testing.T) {
	root := realTemp(t)
	runtimeRoot := filepath.Join(root, "runtime")
	releaseRoot := filepath.Join(runtimeRoot, "releases", fixtureReleaseKey)
	if err := os.MkdirAll(releaseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(releaseRoot, "cw")
	writeFixture(t, binary, managerScript(true), 0755)
	locator := currentLocatorFixture(root, "")
	data := sealedLocatorFixture(t, &locator)
	releaseLocator := filepath.Join(releaseRoot, "locator.json")
	if err := os.WriteFile(releaseLocator, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(binary)
	if err != nil {
		t.Fatal(err)
	}
	if got != locator {
		t.Fatalf("wrong recovery roots: %+v", got)
	}
	primary := filepath.Join(runtimeRoot, "locator.json")
	writeFixture(t, primary, "malformed", 0600)
	if _, err = Resolve(binary); err == nil || errors.Is(err, ErrLocatorMissing) || errors.Is(err, ErrLegacyInstallation) {
		t.Fatalf("malformed primary fell back: %v", err)
	}
	if err = os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(releaseLocator); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(binary); !errors.Is(err, ErrLocatorMissing) {
		t.Fatalf("owned layout without descriptors did not require explicit roots: %v", err)
	}
}

// sealedLocatorFixture seals a locator with the fixture encoding pinned by
// TestResolveSealedV3LocatorAndRejectChangedRoots.
func sealedLocatorFixture(t *testing.T, locator *Locator) []byte {
	t.Helper()
	locator.Integrity = ""
	unsigned, err := json.Marshal(*locator)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(unsigned, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "integrity_sha256")
	canonical, err := json.Marshal(fields, json.Deterministic(true), jsontext.WithIndent("  "), jsontext.EscapeForHTML(false), jsontext.EscapeForJS(false))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append(canonical, '\n'))
	locator.Integrity = hex.EncodeToString(sum[:])
	sealed, err := json.Marshal(*locator)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// R2.3: version-3 locators resolve as before, including a readable source,
// and every sealed field (version, roots, member names) stays bound.
func TestResolveSealedV3LocatorAndRejectChangedRoots(t *testing.T) {
	root := realTemp(t)
	runtimeRoot := filepath.Join(root, "runtime")
	releaseRoot := filepath.Join(runtimeRoot, "releases", fixtureReleaseKey)
	if err := os.MkdirAll(releaseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(releaseRoot, "cw")
	writeFixture(t, binary, managerScript(true), 0755)
	config := filepath.Join(root, ".config", "agent-workflows")
	fields := map[string]any{
		"version": 3, "source": filepath.Join(root, "source é 🔒"),
		"home": root, "state": root,
		"command_path": filepath.Join(root, ".local", "bin", "cw"),
		"config_dir":   config,
	}
	// Independent fixture encoding pins the historical Python ASCII checksum,
	// including a non-BMP path character, rather than using the production seal.
	unsigned, err := json.Marshal(fields, json.Deterministic(true), jsontext.WithIndent("  "), jsontext.EscapeForHTML(false), jsontext.EscapeForJS(false))
	if err != nil {
		t.Fatal(err)
	}
	unsigned = []byte(strings.NewReplacer("é", `\u00e9`, "🔒", `\ud83d\udd12`).Replace(string(unsigned)) + "\n")
	sum := sha256.Sum256(unsigned)
	fields["integrity_sha256"] = hex.EncodeToString(sum[:])
	valid, err := json.Marshal(fields, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []string{filepath.Join(runtimeRoot, "locator.json"), filepath.Join(releaseRoot, "locator.json")} {
		writeFixture(t, descriptor, string(valid), 0600)
		got, err := Resolve(binary)
		if err != nil || got.Version != 3 || got.Source != fields["source"] || got.Config != config || got.Integrity != fields["integrity_sha256"] {
			t.Fatalf("sealed recovery locator did not resolve: %+v %v", got, err)
		}
		for _, changed := range []string{
			strings.Replace(string(valid), `"version":3`, `"version":4`, 1),
			strings.Replace(string(valid), `"version":3`, `"version":0`, 1),
			strings.Replace(string(valid), `,"version":3`, "", 1),
			strings.Replace(string(valid), `"source":`, `"unknown":`, 1),
			strings.Replace(string(valid), "source é 🔒", "other source", 1),
			strings.Replace(string(valid), config, filepath.Join(root, "other config"), 1),
			strings.Replace(string(valid), filepath.Join(root, ".local", "bin", "cw"), "/another/home/.local/bin/cw", 1),
		} {
			if changed == string(valid) {
				t.Fatal("fixture mutation did not apply")
			}
			writeFixture(t, descriptor, changed, 0600)
			if _, err := Resolve(binary); err == nil || errors.Is(err, ErrLegacyInstallation) {
				t.Fatalf("changed v3 locator accepted or misreported as legacy: %v: %s", err, changed)
			}
		}
		if err := os.Remove(descriptor); err != nil {
			t.Fatal(err)
		}
	}
}
