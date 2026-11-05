package bundled_test

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

type bundledMarkerCase struct {
	name     string
	identity bundled.Identity
}

type bundledResource struct {
	data []byte
	mode fs.FileMode
}

type bundledSnapshotEntry struct {
	mode     fs.FileMode
	modified int64
	size     int64
	data     string
	link     string
}

func bundledMarkerIdentity() bundled.Identity {
	return bundled.Identity{
		Version:  "v1.2.3",
		Revision: strings.Repeat("a", 40),
		OS:       "linux",
		Arch:     "amd64",
		Digest:   strings.Repeat("b", 64),
	}
}

func bundledMarkerWire(identity bundled.Identity) string {
	return fmt.Sprintf("CWBUNDLE1|%s|%s|%s|%s|%s|CWBEND", identity.Version, identity.Revision, identity.OS, identity.Arch, identity.Digest)
}

func bundledInvalidMarkerCases() []bundledMarkerCase {
	changes := []struct {
		name  string
		field string
		value string
	}{
		{"empty_version", "version", ""},
		{"version_too_long", "version", strings.Repeat("a", 129)},
		{"leading_dot", "version", ".v1"},
		{"leading_underscore", "version", "_v1"},
		{"leading_plus", "version", "+v1"},
		{"leading_minus", "version", "-v1"},
		{"version_space", "version", "v1 2"},
		{"version_slash", "version", "v1/2"},
		{"version_backslash", "version", "v1\\2"},
		{"version_delimiter", "version", "v1|2"},
		{"version_newline", "version", "v1\n2"},
		{"version_tab", "version", "v1\t2"},
		{"version_nul", "version", "v1\x002"},
		{"version_del", "version", "v1\x7f2"},
		{"version_non_ascii_leading", "version", "é1"},
		{"version_non_ascii_continuation", "version", "v1é"},
		{"empty_revision", "revision", ""},
		{"short_revision", "revision", strings.Repeat("a", 39)},
		{"long_revision", "revision", strings.Repeat("a", 41)},
		{"uppercase_revision", "revision", strings.Repeat("A", 40)},
		{"non_hex_revision", "revision", "g" + strings.Repeat("a", 39)},
		{"empty_digest", "digest", ""},
		{"short_digest", "digest", strings.Repeat("b", 63)},
		{"long_digest", "digest", strings.Repeat("b", 65)},
		{"uppercase_digest", "digest", strings.Repeat("B", 64)},
		{"non_hex_digest", "digest", "g" + strings.Repeat("b", 63)},
		{"empty_os", "os", ""},
		{"unsupported_os", "os", "windows"},
		{"uppercase_os", "os", "Linux"},
		{"empty_arch", "arch", ""},
		{"unsupported_arch", "arch", "386"},
		{"uppercase_arch", "arch", "AMD64"},
	}
	cases := make([]bundledMarkerCase, 0, len(changes))
	for _, change := range changes {
		identity := bundledMarkerIdentity()
		switch change.field {
		case "version":
			identity.Version = change.value
		case "revision":
			identity.Revision = change.value
		case "digest":
			identity.Digest = change.value
		case "os":
			identity.OS = change.value
		case "arch":
			identity.Arch = change.value
		}
		cases = append(cases, bundledMarkerCase{name: change.name, identity: identity})
	}
	return cases
}

func TestR_B1_MarkerWireAndRoundTrip(test *testing.T) {
	versions := []string{"v1.2.3", "0", "A", "a._+-Z09", strings.Repeat("a", 128)}
	for _, operatingSystem := range []string{"darwin", "linux"} {
		for _, architecture := range []string{"arm64", "amd64"} {
			for versionIndex, version := range versions {
				name := fmt.Sprintf("%s_%s_version_%d", operatingSystem, architecture, versionIndex)
				test.Run(name, func(test *testing.T) {
					identity := bundledMarkerIdentity()
					identity.Version = version
					identity.OS = operatingSystem
					identity.Arch = architecture
					identity.Revision = strings.Repeat("0123456789", 4)
					identity.Digest = strings.Repeat("0123456789abcdef", 4)
					marker, err := bundled.BinaryMarker(identity)
					if err != nil {
						test.Fatalf("BinaryMarker: %v", err)
					}
					if marker != bundledMarkerWire(identity) {
						test.Fatalf("marker = %q, want public wire %q", marker, bundledMarkerWire(identity))
					}
					binary := append([]byte{0x00, 0x7f, 'E', 'L', 'F', 0xff, '\n'}, []byte(marker)...)
					binary = append(binary, 0xff, 0x00, 'x', '\n')
					actual, err := bundled.InspectBinary(binary)
					if err != nil {
						test.Fatalf("InspectBinary: %v", err)
					}
					if actual != identity {
						test.Fatalf("identity = %#v, want %#v", actual, identity)
					}
				})
			}
		}
	}
}

func TestR_B1_MarkerMetadataDoesNotRequirePayloadOrSignature(test *testing.T) {
	identity := bundledMarkerIdentity()
	identity.Revision = strings.Repeat("0", 40)
	identity.Digest = strings.Repeat("0", 64)
	marker, err := bundled.BinaryMarker(identity)
	if err != nil {
		test.Fatalf("syntactically valid metadata must be encodable: %v", err)
	}
	actual, err := bundled.InspectBinary([]byte("synthetic, unsigned bytes\x00" + marker + "\x00not a real executable"))
	if err != nil {
		test.Fatalf("metadata inspection must not require an authentic executable or payload: %v", err)
	}
	if actual != identity {
		test.Fatalf("identity = %#v, want %#v", actual, identity)
	}
}

func TestR_B1_BinaryMarkerRejectsInvalidIdentity(test *testing.T) {
	for _, scenario := range bundledInvalidMarkerCases() {
		test.Run(scenario.name, func(test *testing.T) {
			if _, err := bundled.BinaryMarker(scenario.identity); err == nil {
				test.Fatalf("BinaryMarker accepted invalid identity %#v", scenario.identity)
			}
		})
	}
}

func TestR_B1_InspectBinaryRejectsInvalidIdentity(test *testing.T) {
	for _, scenario := range bundledInvalidMarkerCases() {
		test.Run(scenario.name, func(test *testing.T) {
			binary := []byte("prefix\x00" + bundledMarkerWire(scenario.identity) + "\x00suffix")
			if _, err := bundled.InspectBinary(binary); err == nil {
				test.Fatalf("InspectBinary accepted invalid wire identity %#v", scenario.identity)
			}
		})
	}
}

func TestR_B1_InspectBinaryRejectsMissingMalformedAndDuplicateMarkers(test *testing.T) {
	identity := bundledMarkerIdentity()
	marker := bundledMarkerWire(identity)
	cases := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"unmarked_executable", []byte{0x7f, 'E', 'L', 'F', 0x00, 0xff}},
		{"unknown_wire_version", []byte(strings.Replace(marker, "CWBUNDLE1", "CWBUNDLE2", 1))},
		{"missing_field", []byte(strings.Replace(marker, "|linux|", "|", 1))},
		{"extra_field", []byte(strings.Replace(marker, "|CWBEND", "|extra|CWBEND", 1))},
		{"missing_end_separator", []byte(strings.Replace(marker, "|CWBEND", "CWBEND", 1))},
		{"duplicate_equal", []byte(marker + "\x00" + marker)},
		{"duplicate_adjacent", []byte(marker + marker)},
	}
	for _, field := range []string{"version", "revision", "digest", "os", "arch"} {
		other := identity
		switch field {
		case "version":
			other.Version = "v1.2.4"
		case "revision":
			other.Revision = strings.Repeat("c", 40)
		case "digest":
			other.Digest = strings.Repeat("d", 64)
		case "os":
			other.OS = "darwin"
		case "arch":
			other.Arch = "arm64"
		}
		cases = append(cases, struct {
			name string
			data []byte
		}{"duplicate_conflicting_" + field, []byte(marker + "\x00" + bundledMarkerWire(other))})
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			if _, err := bundled.InspectBinary(scenario.data); err == nil {
				test.Fatal("InspectBinary accepted missing, malformed, or duplicate markers")
			}
		})
	}
	for length := 1; length < len(marker); length++ {
		test.Run(fmt.Sprintf("truncated_at_%d", length), func(test *testing.T) {
			if _, err := bundled.InspectBinary([]byte(marker[:length])); err == nil {
				test.Fatalf("InspectBinary accepted marker truncated at byte %d", length)
			}
		})
	}
}

func bundledFixtureManifest(registration string) map[string]any {
	return map[string]any{
		"version":             3,
		"registrations":       map[string]string{"demo": registration},
		"global_instructions": "global/AGENTS.md",
		"model_policy":        registration + "/policy.yaml",
		"claude_model_policy": registration + "/claude.yaml",
		"adoption_catalog":    "global/adoption.json",
		"required_licenses":   []string{"licenses/demo.txt"},
	}
}

func bundledJSON(test *testing.T, value any) []byte {
	test.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		test.Fatalf("marshal synthetic fixture: %v", err)
	}
	return append(encoded, '\n')
}

func bundledFixtureFiles(test *testing.T, registration string) map[string]bundledResource {
	test.Helper()
	return map[string]bundledResource{
		"skills-manifest.json":                 {bundledJSON(test, bundledFixtureManifest(registration)), 0644},
		"global/AGENTS.md":                     {[]byte("synthetic global instructions\n"), 0644},
		"global/adoption.json":                 {[]byte("synthetic adoption text, deliberately not a schema document\n"), 0644},
		"licenses/demo.txt":                    {[]byte("synthetic required license\n"), 0644},
		registration + "/SKILL.md":             {[]byte("synthetic demo skill resource\n"), 0644},
		registration + "/policy.yaml":          {[]byte("synthetic model policy, opaque to Build\n"), 0640},
		registration + "/claude.yaml":          {[]byte("synthetic claude policy, opaque to Build\n"), 0600},
		registration + "/assets/run.sh":        {[]byte("synthetic executable asset bytes\n"), 0755},
		registration + "/assets/key.txt":       {[]byte("synthetic ordinary private-mode resource, not a credential\n"), 0600},
		registration + "/assets/read-only.txt": {[]byte("synthetic read-only resource\n"), 0400},
	}
}

func bundledCanonicalTemp(test *testing.T) string {
	test.Helper()
	canonical, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatalf("canonicalize fixture temp directory: %v", err)
	}
	return canonical
}

func bundledWriteFile(test *testing.T, root, name string, resource bundledResource) {
	test.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		test.Fatalf("create fixture parent for %s: %v", name, err)
	}
	if err := os.WriteFile(filename, resource.data, resource.mode.Perm()); err != nil {
		test.Fatalf("write fixture %s: %v", name, err)
	}
	if err := os.Chmod(filename, resource.mode.Perm()); err != nil {
		test.Fatalf("set fixture mode for %s: %v", name, err)
	}
}

func bundledWriteSource(test *testing.T, files map[string]bundledResource, reverse bool, stamp time.Time) string {
	test.Helper()
	root := bundledCanonicalTemp(test)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	if reverse {
		for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
			names[left], names[right] = names[right], names[left]
		}
	}
	for _, name := range names {
		bundledWriteFile(test, root, name, files[name])
	}
	if err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chtimes(filename, stamp, stamp)
	}); err != nil {
		test.Fatalf("set fixture timestamps: %v", err)
	}
	return root
}

func bundledBuild(test *testing.T, root string) []byte {
	test.Helper()
	payload, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40))
	if err != nil {
		test.Fatalf("Build: %v", err)
	}
	if len(payload) == 0 {
		test.Fatal("Build returned an empty payload")
	}
	return payload
}

func bundledOpen(test *testing.T, payload []byte) *bundled.Bundle {
	test.Helper()
	bundle, err := bundled.Open(payload)
	if err != nil {
		test.Fatalf("Open: %v", err)
	}
	if bundle == nil {
		test.Fatal("Open returned a nil bundle without an error")
	}
	return bundle
}

func bundledRequiredFiles(files map[string]bundledResource) map[string]bundledResource {
	required := make(map[string]bundledResource, len(files))
	for name, resource := range files {
		if name != "skills-manifest.json" {
			required[name] = resource
		}
	}
	return required
}

func bundledRequireReads(test *testing.T, bundle *bundled.Bundle, required map[string]bundledResource) {
	test.Helper()
	for name, expected := range required {
		actual, err := bundle.ReadFile(name)
		if err != nil {
			test.Fatalf("ReadFile(%q): %v", name, err)
		}
		if !bytes.Equal(actual, expected.data) {
			test.Fatalf("ReadFile(%q) = %q, want %q", name, actual, expected.data)
		}
	}
}

func bundledSnapshot(test *testing.T, root string) map[string]bundledSnapshotEntry {
	test.Helper()
	snapshot := make(map[string]bundledSnapshotEntry)
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := bundledSnapshotEntry{mode: info.Mode(), modified: info.ModTime().UnixNano(), size: info.Size()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(filename)
			if err != nil {
				return err
			}
			state.data = string(data)
		} else if info.Mode()&os.ModeSymlink != 0 {
			state.link, err = os.Readlink(filename)
			if err != nil {
				return err
			}
		}
		snapshot[filepath.ToSlash(relative)] = state
		return nil
	})
	if err != nil {
		test.Fatalf("snapshot fixture directory: %v", err)
	}
	return snapshot
}

func bundledProtectWorkspace(test *testing.T) {
	test.Helper()
	workspace := bundledCanonicalTemp(test)
	bundledWriteFile(test, workspace, "unrelated/keep.txt", bundledResource{[]byte("unrelated workspace sentinel\n"), 0640})
	test.Chdir(workspace)
	before := bundledSnapshot(test, workspace)
	test.Cleanup(func() {
		if after := bundledSnapshot(test, workspace); !reflect.DeepEqual(after, before) {
			test.Errorf("Open/ReadFile changed the protected workspace: before %#v, after %#v", before, after)
		}
	})
}

func bundledRequireExtracted(test *testing.T, destination string, allowed, required map[string]bundledResource) {
	test.Helper()
	allowedDirectories := map[string]bool{".": true}
	for name := range allowed {
		for parent := filepath.Dir(filepath.FromSlash(name)); parent != "."; parent = filepath.Dir(parent) {
			allowedDirectories[filepath.ToSlash(parent)] = true
		}
	}
	found := make(map[string]bool)
	err := filepath.WalkDir(destination, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(destination, filename)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if !allowedDirectories[name] {
				return fmt.Errorf("unexpected extracted directory %q", name)
			}
			if info.Mode().Perm() != 0755 {
				return fmt.Errorf("extracted directory mode for %q = %04o, want 0755", name, info.Mode().Perm())
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("extracted nonregular resource %q: %v", name, info.Mode())
		}
		expected, exists := allowed[name]
		if !exists {
			return fmt.Errorf("unexpected extracted file %q", name)
		}
		actual, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected.data) {
			return fmt.Errorf("extracted contents differ for %q: got %q, want %q", name, actual, expected.data)
		}
		// R-B2: public pack permissions depend only on the source executable bit.
		wantMode := fs.FileMode(0644)
		if expected.mode.Perm()&0111 != 0 {
			wantMode = 0755
		}
		if info.Mode().Perm() != wantMode {
			return fmt.Errorf("extracted mode for %q = %04o, want %04o", name, info.Mode().Perm(), wantMode)
		}
		found[name] = true
		return nil
	})
	if err != nil {
		test.Fatalf("inspect extracted inventory: %v", err)
	}
	for name := range required {
		if !found[name] {
			test.Errorf("required payload file %q was not extracted", name)
		}
	}
}

func TestR_B2_BuildIsDeterministicAcrossOrderAndMtime(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	firstRoot := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	secondRoot := bundledWriteSource(test, files, true, time.Unix(2000000000, 0))
	first := bundledBuild(test, firstRoot)
	repeated := bundledBuild(test, firstRoot)
	second := bundledBuild(test, secondRoot)
	if !bytes.Equal(first, repeated) {
		test.Fatal("repeated Build of unchanged bytes/modes produced different compressed bytes")
	}
	if !bytes.Equal(first, second) {
		test.Fatal("file creation order, root location, or mtimes changed compressed bundle bytes")
	}
	// R-B2: changing private/read-only bits without changing executability
	// cannot change the public payload or its digest.
	privateFiles := make(map[string]bundledResource, len(files))
	for name, resource := range files {
		resource.mode = 0400 | (resource.mode.Perm() & 0111)
		privateFiles[name] = resource
	}
	privateRoot := bundledWriteSource(test, privateFiles, true, time.Unix(1500000000, 0))
	privatePayload := bundledBuild(test, privateRoot)
	if !bytes.Equal(first, privatePayload) {
		test.Fatal("private source permission bits changed canonical public bundle bytes")
	}
	firstIdentity := bundledOpen(test, first).Identity()
	secondIdentity := bundledOpen(test, second).Identity()
	if privateIdentity := bundledOpen(test, privatePayload).Identity(); privateIdentity != firstIdentity {
		test.Fatalf("private permission bits changed identity: %#v and %#v", firstIdentity, privateIdentity)
	}
	if firstIdentity != secondIdentity {
		test.Fatalf("deterministic builds have different identities: %#v and %#v", firstIdentity, secondIdentity)
	}
	if firstIdentity.Version != "v1.2.3" || firstIdentity.Revision != strings.Repeat("a", 40) {
		test.Fatalf("Build lost supplied version/revision: %#v", firstIdentity)
	}
	if firstIdentity.OS != "darwin" && firstIdentity.OS != "linux" {
		test.Fatalf("Build returned unsupported OS %q", firstIdentity.OS)
	}
	if firstIdentity.Arch != "arm64" && firstIdentity.Arch != "amd64" {
		test.Fatalf("Build returned unsupported architecture %q", firstIdentity.Arch)
	}
	digest, err := hex.DecodeString(firstIdentity.Digest)
	if err != nil || len(digest) != 32 || strings.ToLower(firstIdentity.Digest) != firstIdentity.Digest {
		test.Fatalf("Build returned invalid lowercase SHA256 digest %q", firstIdentity.Digest)
	}
}

func TestR_B2_OpenIsSelfContainedReadOnlyAndReturnsDefensiveCopies(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	payload := bundledBuild(test, root)
	if err := os.RemoveAll(root); err != nil {
		test.Fatalf("remove source fixture before Open: %v", err)
	}
	bundledProtectWorkspace(test)
	bundle := bundledOpen(test, payload)
	bundledRequireReads(test, bundle, bundledRequiredFiles(files))
	first, err := bundle.ReadFile("skills/demo/SKILL.md")
	if err != nil {
		test.Fatalf("first ReadFile: %v", err)
	}
	if len(first) == 0 {
		test.Fatal("nonempty source resource became empty")
	}
	first[0] ^= 0xff
	second, err := bundle.ReadFile("skills/demo/SKILL.md")
	if err != nil {
		test.Fatalf("second ReadFile: %v", err)
	}
	if !bytes.Equal(second, files["skills/demo/SKILL.md"].data) {
		test.Fatal("mutating ReadFile's result changed the stored payload")
	}
	unsafeNames := []string{
		"", ".", "..", "../global/AGENTS.md", "/global/AGENTS.md",
		"skills/demo/../../global/AGENTS.md", "skills/demo/../../../outside.txt",
		"..\\global\\AGENTS.md", "C:\\global\\AGENTS.md", "global/AGENTS.md\x00",
		"skills/demo/missing.txt", "missing.txt",
	}
	for _, name := range unsafeNames {
		test.Run(fmt.Sprintf("reject_name_%q", name), func(test *testing.T) {
			if _, err := bundle.ReadFile(name); err == nil {
				test.Fatalf("ReadFile accepted missing or unsafe name %q", name)
			}
		})
	}
}

func bundledGzip(test *testing.T, data []byte) []byte {
	test.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		test.Fatalf("write synthetic gzip member: %v", err)
	}
	if err := writer.Close(); err != nil {
		test.Fatalf("close synthetic gzip member: %v", err)
	}
	return compressed.Bytes()
}

func TestR_B2_OpenRejectsChangedTruncatedAndTrailingData(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	payload := bundledBuild(test, root)
	badHeader := bytes.Clone(payload)
	badHeader[0] ^= 0xff
	badBody := bytes.Clone(payload)
	badBody[len(badBody)/2] ^= 0x80
	badTrailer := bytes.Clone(payload)
	badTrailer[len(badTrailer)-1] ^= 0x80
	cases := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"not_gzip", []byte("not a bundle")},
		{"changed_header", badHeader},
		{"changed_body", badBody},
		{"changed_trailer", badTrailer},
		{"truncated_one_byte", payload[:1]},
		{"truncated_half", payload[:len(payload)/2]},
		{"truncated_last_byte", payload[:len(payload)-1]},
		{"replacement_gzip_document", bundledGzip(test, []byte("unrelated synthetic document\n"))},
		{"trailing_garbage", append(bytes.Clone(payload), []byte("untrusted trailing bytes")...)},
		{"trailing_nul", append(bytes.Clone(payload), 0)},
		{"trailing_gzip_member", append(bytes.Clone(payload), bundledGzip(test, []byte("second gzip member\n"))...)},
		{"trailing_empty_gzip_member", append(bytes.Clone(payload), bundledGzip(test, nil)...)},
		{"trailing_duplicate_bundle", append(bytes.Clone(payload), payload...)},
	}
	bundledProtectWorkspace(test)
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			if _, err := bundled.Open(scenario.data); err == nil {
				test.Fatal("Open accepted malformed, changed, truncated, or trailing payload data")
			}
		})
	}
}

func TestR_B2_ExtractCreatesExactAllowedFilesAndModes(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	bundle := bundledOpen(test, bundledBuild(test, root))
	destination := filepath.Join(bundledCanonicalTemp(test), "new-destination")
	if err := bundle.Extract(destination); err != nil {
		test.Fatalf("Extract into a new destination: %v", err)
	}
	bundledRequireExtracted(test, destination, files, bundledRequiredFiles(files))
}

func TestR_B2_ExtractRefusesNonemptyAndFileDestinationsWithoutChanges(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	bundle := bundledOpen(test, bundledBuild(test, root))
	for _, scenario := range []string{"unrelated_file", "overlapping_payload_file", "regular_file_destination", "nested_symlink"} {
		test.Run(scenario, func(test *testing.T) {
			parent := bundledCanonicalTemp(test)
			destination := filepath.Join(parent, "destination")
			external := bundledCanonicalTemp(test)
			bundledWriteFile(test, external, "keep.txt", bundledResource{[]byte("outside destination sentinel\n"), 0644})
			switch scenario {
			case "regular_file_destination":
				bundledWriteFile(test, parent, "destination", bundledResource{[]byte("not a directory\n"), 0640})
			case "unrelated_file":
				bundledWriteFile(test, destination, "keep.txt", bundledResource{[]byte("must not be clobbered\n"), 0640})
			case "overlapping_payload_file":
				bundledWriteFile(test, destination, "global/AGENTS.md", bundledResource{[]byte("preexisting unrelated instructions\n"), 0600})
			case "nested_symlink":
				if err := os.Mkdir(destination, 0755); err != nil {
					test.Fatalf("create destination: %v", err)
				}
				if err := os.Symlink(external, filepath.Join(destination, "global")); err != nil {
					test.Fatalf("create nested untrusted symlink: %v", err)
				}
			}
			before := bundledSnapshot(test, parent)
			externalBefore := bundledSnapshot(test, external)
			if err := bundle.Extract(destination); err == nil {
				test.Fatal("Extract accepted a nonempty or nondirectory destination")
			}
			if after := bundledSnapshot(test, parent); !reflect.DeepEqual(after, before) {
				test.Errorf("rejected Extract changed the destination tree: before %#v, after %#v", before, after)
			}
			if after := bundledSnapshot(test, external); !reflect.DeepEqual(after, externalBefore) {
				test.Error("rejected Extract changed an external symlink target")
			}
		})
	}
}

func TestR_B2_ExtractRefusesSymlinkDestinationsAndAncestors(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	bundle := bundledOpen(test, bundledBuild(test, root))
	for _, scenario := range []string{"live_destination_symlink", "dangling_destination_symlink", "symlink_ancestor"} {
		test.Run(scenario, func(test *testing.T) {
			parent := bundledCanonicalTemp(test)
			external := bundledCanonicalTemp(test)
			bundledWriteFile(test, external, "keep.txt", bundledResource{[]byte("external sentinel\n"), 0640})
			link := filepath.Join(parent, "untrusted-link")
			target := external
			if scenario == "dangling_destination_symlink" {
				target = filepath.Join(external, "does-not-exist")
			}
			if err := os.Symlink(target, link); err != nil {
				test.Fatalf("create untrusted destination symlink: %v", err)
			}
			destination := link
			if scenario == "symlink_ancestor" {
				destination = filepath.Join(link, "new-destination")
			}
			before := bundledSnapshot(test, parent)
			externalBefore := bundledSnapshot(test, external)
			if err := bundle.Extract(destination); err == nil {
				test.Fatal("Extract trusted a symlink destination or ancestor")
			}
			if after := bundledSnapshot(test, parent); !reflect.DeepEqual(after, before) {
				test.Error("rejected Extract changed the symlink tree")
			}
			if after := bundledSnapshot(test, external); !reflect.DeepEqual(after, externalBefore) {
				test.Error("rejected Extract changed the external target tree")
			}
		})
	}
}

func TestR_B3_BuildIncludesRegisteredRootsAndLegitimateTestNamedAssets(test *testing.T) {
	for _, registration := range []string{"skills/demo", "third_party/demo"} {
		test.Run(strings.ReplaceAll(registration, "/", "_"), func(test *testing.T) {
			files := bundledFixtureFiles(test, registration)
			for _, suffix := range []string{"assets/tests/example.txt", "assets/fixtures/sample.yaml", "assets/testdata/example.txt", "assets/parser_test.go"} {
				files[registration+"/"+suffix] = bundledResource{[]byte("legitimate synthetic asset: " + suffix + "\n"), 0644}
			}
			required := bundledRequiredFiles(files)
			for _, name := range []string{"LICENSE", "THIRD_PARTY.md", "README.md", "go.mod", "docs/guide.md", "global/resources/extra.txt", "licenses/extra.txt"} {
				files[name] = bundledResource{[]byte("optional allowlisted resource: " + name + "\n"), 0644}
			}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			bundle := bundledOpen(test, bundledBuild(test, root))
			bundledRequireReads(test, bundle, required)
			destination := filepath.Join(bundledCanonicalTemp(test), "extracted")
			if err := bundle.Extract(destination); err != nil {
				test.Fatalf("Extract: %v", err)
			}
			bundledRequireExtracted(test, destination, files, required)
		})
	}
}

func TestR_B3_BuildExcludesUnregisteredSecretsCachesAndFixtureTrees(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	baseline := bundledBuild(test, root)
	excluded := []string{
		"unrelated.txt", "main.go", "skills/unregistered/SKILL.md", "third_party/unregistered/SKILL.md",
		".git/config", ".github/workflows/build.yaml", ".codex/config.toml", ".claude/settings.json", ".agents/private.txt",
		"credentials.json", "auth.json", "preferences.json", "config.toml", ".env", ".env.local",
		"credentials/token.txt", "auth/token.txt", "preferences/settings.txt", ".cache/entry.txt", "tests/root.txt", "fixtures/root.txt",
		"skills/demo/.git/config", "skills/demo/.github/workflows/build.yaml", "skills/demo/.codex/config.toml",
		"skills/demo/.claude/settings.json", "skills/demo/.agents/private.txt", "skills/demo/nested/.git/config",
		"skills/demo/credentials.json", "skills/demo/auth.json", "skills/demo/preferences.json", "skills/demo/config.toml",
		"skills/demo/.env", "skills/demo/.env.production", "skills/demo/credentials/token.txt", "skills/demo/auth/token.txt",
		"skills/demo/preferences/settings.txt", "skills/demo/cache/entry.txt", "skills/demo/.cache/entry.txt",
		"skills/demo/__pycache__/compiled.pyc", "skills/demo/test/unit.txt", "skills/demo/tests/unit.txt",
		"skills/demo/fixture/sample.txt", "skills/demo/fixtures/sample.txt", "skills/demo/testdata/sample.txt",
		"global/.env", "global/.git/config", "global/cache/entry.txt", "global/tests/unit.txt", "licenses/.env",
	}
	for _, name := range excluded {
		bundledWriteFile(test, root, name, bundledResource{[]byte("must be excluded: " + name + "\n"), 0644})
	}
	payload := bundledBuild(test, root)
	if !bytes.Equal(payload, baseline) {
		test.Error("excluded source files changed deterministic bundle bytes")
	}
	bundle := bundledOpen(test, payload)
	bundledRequireReads(test, bundle, bundledRequiredFiles(files))
	for _, name := range excluded {
		test.Run(name, func(test *testing.T) {
			if _, err := bundle.ReadFile(name); err == nil {
				test.Fatalf("excluded source %q is readable from bundle", name)
			}
		})
	}
	destination := filepath.Join(bundledCanonicalTemp(test), "extracted")
	if err := bundle.Extract(destination); err != nil {
		test.Fatalf("Extract: %v", err)
	}
	bundledRequireExtracted(test, destination, files, bundledRequiredFiles(files))
}

func TestR_B3_BuildRejectsMissingAndMalformedManifestFields(test *testing.T) {
	fields := []string{"version", "registrations", "global_instructions", "model_policy", "claude_model_policy", "adoption_catalog", "required_licenses"}
	for _, field := range fields {
		test.Run("missing_"+field, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			manifest := bundledFixtureManifest("skills/demo")
			delete(manifest, field)
			files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted manifest without %q", field)
			}
		})
	}
	changes := []struct {
		name  string
		field string
		value any
	}{
		{"version_zero", "version", 0},
		{"version_two", "version", 2},
		{"version_four", "version", 4},
		{"version_string", "version", "3"},
		{"version_fractional", "version", 3.5},
		{"registrations_array", "registrations", []string{"skills/demo"}},
		{"registrations_null", "registrations", nil},
		{"registration_value_not_string", "registrations", map[string]any{"demo": 7}},
		{"instructions_not_string", "global_instructions", true},
		{"model_policy_not_string", "model_policy", 7},
		{"claude_policy_not_string", "claude_model_policy", []string{"skills/demo/claude.yaml"}},
		{"adoption_not_string", "adoption_catalog", map[string]string{"path": "global/adoption.json"}},
		{"licenses_not_array", "required_licenses", "licenses/demo.txt"},
		{"licenses_null", "required_licenses", nil},
		{"license_element_not_string", "required_licenses", []any{7}},
	}
	for _, scenario := range changes {
		test.Run(scenario.name, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			manifest := bundledFixtureManifest("skills/demo")
			manifest[scenario.field] = scenario.value
			files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatal("Build accepted an invalid manifest field")
			}
		})
	}
	for _, malformed := range []struct {
		name string
		data []byte
	}{
		{"empty_json", []byte{}},
		{"truncated_json", []byte(`{"version":3,`)},
		{"null_document", []byte("null\n")},
		{"array_document", []byte("[]\n")},
		{"trailing_document", append(bundledJSON(test, bundledFixtureManifest("skills/demo")), []byte("{}\n")...)},
	} {
		test.Run(malformed.name, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			files["skills-manifest.json"] = bundledResource{malformed.data, 0644}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatal("Build accepted an invalid manifest document")
			}
		})
	}
}

func TestR_B3_BuildRejectsMissingRequiredInputs(test *testing.T) {
	for _, missing := range []string{
		"skills-manifest.json", "global/AGENTS.md", "global/adoption.json", "licenses/demo.txt",
		"skills/demo/SKILL.md", "skills/demo/policy.yaml", "skills/demo/claude.yaml",
	} {
		test.Run(missing, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			delete(files, missing)
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted missing required input %q", missing)
			}
		})
	}
	test.Run("required_resource_is_directory", func(test *testing.T) {
		files := bundledFixtureFiles(test, "skills/demo")
		delete(files, "global/AGENTS.md")
		root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
		if err := os.Mkdir(filepath.Join(root, "global", "AGENTS.md"), 0755); err != nil {
			test.Fatalf("create directory in place of required file: %v", err)
		}
		if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
			test.Fatal("Build accepted a directory in place of a required file")
		}
	})
}

func TestR_B3_BuildRejectsInvalidAndOverlappingRegistrationRoots(test *testing.T) {
	for _, registration := range []string{
		"", ".", "..", "./skills/demo", "skills/./demo", "skills/demo/..", "skills/demo/../demo",
		"skills/../../outside", "/skills/demo", "other/demo", "skills", "third_party",
	} {
		test.Run(fmt.Sprintf("invalid_%q", registration), func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			manifest := bundledFixtureManifest("skills/demo")
			manifest["registrations"] = map[string]string{"demo": registration}
			files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted invalid registration root %q", registration)
			}
		})
	}
	for _, scenario := range []string{"absolute_existing_root", "escaping_existing_root", "same_root_twice", "ancestor_descendant_roots"} {
		test.Run(scenario, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			external := bundledCanonicalTemp(test)
			for name, resource := range files {
				if strings.HasPrefix(name, "skills/demo/") {
					bundledWriteFile(test, external, strings.TrimPrefix(name, "skills/demo/"), resource)
				}
			}
			manifest := bundledFixtureManifest("skills/demo")
			switch scenario {
			case "absolute_existing_root":
				manifest["registrations"] = map[string]string{"demo": external}
			case "escaping_existing_root":
				relative, err := filepath.Rel(root, external)
				if err != nil {
					test.Fatalf("relative escaping fixture root: %v", err)
				}
				manifest["registrations"] = map[string]string{"demo": filepath.ToSlash(relative)}
			case "same_root_twice":
				manifest["registrations"] = map[string]string{"demo": "skills/demo", "other": "skills/demo"}
			case "ancestor_descendant_roots":
				bundledWriteFile(test, root, "skills/demo/nested/SKILL.md", bundledResource{[]byte("nested synthetic skill\n"), 0644})
				manifest["registrations"] = map[string]string{"demo": "skills/demo", "nested": "skills/demo/nested"}
			}
			bundledWriteFile(test, root, "skills-manifest.json", bundledResource{bundledJSON(test, manifest), 0644})
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatal("Build accepted an escaping, absolute, duplicate, or overlapping registration root")
			}
		})
	}
}

func TestR_B3_BuildAcceptsNonoverlappingPrefixSiblingRoots(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	files["skills/demo-extra/SKILL.md"] = bundledResource{[]byte("second synthetic skill\n"), 0644}
	files["skills/demo-extra/assets/example.txt"] = bundledResource{[]byte("second synthetic asset\n"), 0644}
	manifest := bundledFixtureManifest("skills/demo")
	manifest["registrations"] = map[string]string{"demo": "skills/demo", "demo-extra": "skills/demo-extra"}
	files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	bundle := bundledOpen(test, bundledBuild(test, root))
	bundledRequireReads(test, bundle, bundledRequiredFiles(files))
}

func TestR_B3_BuildRejectsEscapingAndAbsoluteResourceReferences(test *testing.T) {
	for _, field := range []string{"global_instructions", "model_policy", "claude_model_policy", "adoption_catalog", "required_licenses"} {
		for _, kind := range []string{"absolute", "escaping"} {
			test.Run(field+"_"+kind, func(test *testing.T) {
				files := bundledFixtureFiles(test, "skills/demo")
				root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
				external := bundledCanonicalTemp(test)
				bundledWriteFile(test, external, "resource.txt", bundledResource{[]byte("outside source fixture resource\n"), 0644})
				reference := filepath.Join(external, "resource.txt")
				if kind == "escaping" {
					var err error
					reference, err = filepath.Rel(root, reference)
					if err != nil {
						test.Fatalf("make escaping resource reference: %v", err)
					}
					reference = filepath.ToSlash(reference)
				}
				manifest := bundledFixtureManifest("skills/demo")
				if field == "required_licenses" {
					manifest[field] = []string{reference}
				} else {
					manifest[field] = reference
				}
				bundledWriteFile(test, root, "skills-manifest.json", bundledResource{bundledJSON(test, manifest), 0644})
				if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
					test.Fatal("Build accepted a resource reference outside the source root")
				}
			})
		}
	}
}

func TestR_B3_BuildRejectsSymlinksInIncludedResources(test *testing.T) {
	for _, name := range []string{
		"skills-manifest.json", "global/AGENTS.md", "licenses/demo.txt", "skills/demo/SKILL.md",
		"skills/demo/policy.yaml", "skills/demo/claude.yaml", "skills/demo/assets/linked.txt",
		"skills/demo", "skills/demo/assets/linked-directory",
	} {
		test.Run(name, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			external := bundledCanonicalTemp(test)
			target := filepath.Join(external, "resource.txt")
			if name == "skills/demo" {
				target = external
				for resourceName, resource := range files {
					if strings.HasPrefix(resourceName, "skills/demo/") {
						bundledWriteFile(test, external, strings.TrimPrefix(resourceName, "skills/demo/"), resource)
					}
				}
			} else if name == "skills/demo/assets/linked-directory" {
				target = external
				bundledWriteFile(test, external, "resource.txt", bundledResource{[]byte("synthetic symlink directory resource\n"), 0644})
			} else {
				resource, exists := files[name]
				if !exists {
					resource = bundledResource{[]byte("synthetic linked asset\n"), 0644}
				}
				bundledWriteFile(test, external, "resource.txt", resource)
			}
			link := filepath.Join(root, filepath.FromSlash(name))
			if err := os.RemoveAll(link); err != nil {
				test.Fatalf("remove fixture resource before replacing it with a symlink: %v", err)
			}
			if err := os.Symlink(target, link); err != nil {
				test.Fatalf("create included symlink: %v", err)
			}
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted included symlink %q", name)
			}
		})
	}
}

func TestR_B3_BuildRejectsSpecialFiles(test *testing.T) {
	files := bundledFixtureFiles(test, "skills/demo")
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	test.Chdir(root)
	listener, err := net.Listen("unix", "skills/demo/special.sock")
	if err != nil {
		test.Fatalf("create included Unix socket fixture: %v", err)
	}
	test.Cleanup(func() {
		if err := listener.Close(); err != nil {
			test.Errorf("close Unix socket fixture: %v", err)
		}
	})
	if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
		test.Fatal("Build accepted an included socket special file")
	}
}
