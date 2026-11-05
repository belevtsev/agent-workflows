package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// R-R1, R-R4: each native identity defect is isolated from valid archive notices.
func TestNativePublicationIdentityRejectsBeforeAnyRequest(t *testing.T) {
	for _, scenario := range []string{"stale-version", "stale-revision", "plaintext", "missing-marker", "duplicate-marker", "wrong-target", "missing-cw", "duplicate-cw", "unsafe-path", "link-member"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPublicationFixture(t)
			name := "cw_linux_amd64.tar.gz"
			executable, err := archiveExecutable(filepath.Join(fixture.options.Dist, name))
			if err != nil {
				t.Fatal(err)
			}
			entries := readTestArchive(t, filepath.Join(fixture.options.Dist, name))
			cw := testExecutableEntry(t, entries)
			switch scenario {
			case "stale-version":
				fixture.options.Version = "v1.0.9"
			case "stale-revision":
				writeTest(t, filepath.Join(fixture.options.Source, "README.md"), []byte("next exact source revision\n"))
				gitTest(t, fixture.options.Source, "add", "README.md")
				gitTest(t, fixture.options.Source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "next")
				fixture.options.Revision = gitTest(t, fixture.options.Source, "rev-parse", "HEAD")
			case "plaintext":
				entries[cw].data = []byte("not a native Go executable")
			case "missing-marker":
				entries[cw].data = bytes.ReplaceAll(executable, []byte("CWRELEASE1|"), []byte("BWRELEASE1|"))
			case "duplicate-marker":
				_, stamp, err := SourceIdentity(t.Context(), fixture.options.Source, fixture.options.Revision, true)
				if err != nil {
					t.Fatal(err)
				}
				entries[cw].data = append(bytes.Clone(executable), []byte(releaseMarker(fixture.options.Version, fixture.options.Revision, stamp, "linux", "amd64"))...)
			case "wrong-target":
				entries[cw].data, err = archiveExecutable(filepath.Join(fixture.options.Dist, "cw_darwin_arm64.tar.gz"))
				if err != nil {
					t.Fatal(err)
				}
			case "missing-cw":
				entries = append(entries[:cw], entries[cw+1:]...)
			case "duplicate-cw":
				entries = append(entries, entries[cw])
			case "unsafe-path":
				entries = append(entries, testArchiveEntry{name: "licenses/../../outside", kind: tar.TypeReg, mode: 0o644, data: []byte("unsafe")})
			case "link-member":
				entries = append(entries, testArchiveEntry{name: "licenses/linked", kind: tar.TypeSymlink, mode: 0o644})
			}
			if scenario != "stale-version" && scenario != "stale-revision" {
				writeTest(t, filepath.Join(fixture.options.Dist, name), testArchive(t, entries))
				rewriteManifest(t, fixture.options.Dist)
			}
			if _, err := ValidateAssets(fixture.options.Dist); err != nil {
				t.Fatalf("scenario must have self-consistent checksums: %v", err)
			}
			requests := 0
			fixture.options.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return nil, fmt.Errorf("unexpected publication request")
			})}
			if _, err := Publish(t.Context(), fixture.options); err == nil {
				t.Fatalf("invalid binary identity was accepted: %s", scenario)
			}
			if requests != 0 {
				t.Fatal("binary identity preflight contacted GitHub")
			}
		})
	}
}

// R-R4: corrupt only embedded payload bytes, retaining complete native Go
// metadata, both identity markers, every notice, and self-consistent checksums.
// Marker-only provenance validation must not permit any network request.
func TestNativePublicationRejectsPayloadCorruptionBeforeAnyRequest(t *testing.T) {
	fixture := newPublicationFixture(t)
	name := "cw_linux_amd64.tar.gz"
	filename := filepath.Join(fixture.options.Dist, name)
	originalMembers := readArchiveMembers(t, filename)
	for _, notice := range []string{"LICENSE", "THIRD_PARTY.md", "licenses/demo.txt"} {
		if len(originalMembers[notice]) == 0 {
			t.Fatalf("payload-corruption fixture lacks valid notice %s", notice)
		}
	}
	entries := readTestArchive(t, filename)
	cw := testExecutableEntry(t, entries)
	executable := entries[cw].data
	payload, err := bundled.Build(fixture.options.Source, fixture.options.Version, fixture.options.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) == 0 || bytes.Count(executable, payload) != 1 {
		t.Fatal("native fixture must retain its exact known standalone payload once")
	}
	beforeIdentity, err := bundled.InspectBinary(executable)
	if err != nil {
		t.Fatal(err)
	}
	bundleMarker, err := bundled.BinaryMarker(beforeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	_, stamp, err := SourceIdentity(t.Context(), fixture.options.Source, fixture.options.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	releaseIdentity := releaseMarker(fixture.options.Version, fixture.options.Revision, stamp, "linux", "amd64")
	beforeBuildInfo, err := buildinfo.Read(bytes.NewReader(executable))
	if err != nil {
		t.Fatalf("fixture has no readable native Go metadata: %v", err)
	}
	corrupted := bytes.Clone(executable)
	offset := bytes.Index(executable, payload)
	changed := offset + len(payload)/2
	corrupted[changed] ^= 0x01
	if bytes.Contains(corrupted, payload) {
		t.Fatal("payload corruption did not remove the exact original payload")
	}
	for index, value := range executable {
		if index != changed && corrupted[index] != value {
			t.Fatalf("payload mutation changed unrelated executable byte %d", index)
		}
	}
	for _, marker := range []string{releaseIdentity, bundleMarker} {
		if bytes.Count(executable, []byte(marker)) != 1 || bytes.Count(corrupted, []byte(marker)) != 1 {
			t.Fatal("payload mutation must preserve both exact native identity markers once")
		}
	}
	afterIdentity, err := bundled.InspectBinary(corrupted)
	if err != nil || afterIdentity != beforeIdentity {
		t.Fatalf("payload mutation changed bundle marker metadata: %+v, %v", afterIdentity, err)
	}
	afterBuildInfo, err := buildinfo.Read(bytes.NewReader(corrupted))
	if err != nil || !reflect.DeepEqual(beforeBuildInfo, afterBuildInfo) {
		t.Fatalf("payload mutation changed native Go metadata: %v", err)
	}
	entries[cw].data = corrupted
	writeTest(t, filename, testArchive(t, entries))
	rewriteManifest(t, fixture.options.Dist)
	if _, err := ValidateAssets(fixture.options.Dist); err != nil {
		t.Fatalf("payload corruption must retain self-consistent outer checksums: %v", err)
	}
	mutatedMembers := readArchiveMembers(t, filename)
	if len(mutatedMembers) != len(originalMembers) {
		t.Fatal("payload mutation changed the valid archive inventory")
	}
	for member, contents := range originalMembers {
		if member != "cw" && !bytes.Equal(mutatedMembers[member], contents) {
			t.Fatalf("payload mutation changed unrelated archive member %s", member)
		}
	}
	requests := 0
	fixture.options.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, fmt.Errorf("unexpected publication request")
	})}
	if _, err := Publish(t.Context(), fixture.options); err == nil {
		t.Fatal("corrupt embedded payload was accepted despite self-consistent outer checksums")
	}
	if requests != 0 {
		t.Fatal("embedded payload preflight contacted GitHub")
	}
}

// Preserve every valid entry and notice while a scenario changes only its
// named defect. Retaining the original header also avoids incidental metadata
// changes when executable data is replaced.
func readTestArchive(t *testing.T, filename string) []testArchiveEntry {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	zipped, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var entries []testArchiveEntry
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		saved := *header
		entries = append(entries, testArchiveEntry{
			name: header.Name, kind: header.Typeflag, mode: header.Mode,
			data: contents, header: &saved,
		})
	}
}

func testExecutableEntry(t *testing.T, entries []testArchiveEntry) int {
	t.Helper()
	found := -1
	for index, entry := range entries {
		if entry.name == "cw" {
			if found >= 0 {
				t.Fatal("native fixture unexpectedly contains duplicate cw entries")
			}
			found = index
		}
	}
	if found < 0 {
		t.Fatal("native fixture has no cw executable entry")
	}
	return found
}

type testArchiveEntry struct {
	name   string
	kind   byte
	mode   int64
	data   []byte
	header *tar.Header
}

func testArchive(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: entry.mode, Format: tar.FormatUSTAR}
		if entry.header != nil {
			saved := *entry.header
			header = &saved
			header.Name, header.Typeflag, header.Mode = entry.name, entry.kind, entry.mode
		}
		if entry.kind == tar.TypeReg {
			header.Size = int64(len(entry.data))
		}
		if entry.kind == tar.TypeSymlink && entry.header == nil {
			header.Linkname = "../../outside"
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.kind == tar.TypeReg {
			if _, err := writer.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func rewriteManifest(t *testing.T, directory string) {
	t.Helper()
	var manifest strings.Builder
	for _, name := range ArchiveNames {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	writeTest(t, filepath.Join(directory, "SHA256SUMS"), []byte(manifest.String()))
}
