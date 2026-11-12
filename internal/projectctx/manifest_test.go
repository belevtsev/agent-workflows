package projectctx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func manifestTestSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// manifestTestRegular is the expected entry for a regular file, hashed from
// its bytes on disk.
func manifestTestRegular(t *testing.T, root, path, mode string) FileState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return FileState{Path: path, Mode: mode, SHA256: manifestTestSum(data)}
}

// M2: CaptureManifest returns Capture's snapshot plus one sorted entry per
// eligible file, with modes, regular-file hashes and symlink-target hashes;
// ignored and deleted files are absent, untracked files are present.
func TestCaptureManifestListsEligibleFilesWithModesAndHashes(t *testing.T) {
	root := gitFixture(t)
	writeFile(t, root, ".gitignore", "*.log\n")
	writeFile(t, root, "tool.sh", "#!/bin/sh\necho tool\n")
	writeFile(t, root, "group.sh", "#!/bin/sh\necho group\n")
	writeFile(t, root, "plain.txt", "plain bytes\n")
	writeFile(t, root, "docs/guide.md", "# Guide\n")
	writeFile(t, root, "gone.txt", "removed later\n")
	if err := os.Chmod(filepath.Join(root, "tool.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Group-executable only: not owner-executable, so a plain file.
	if err := os.Chmod(filepath.Join(root, "group.sh"), 0o654); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain.txt", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "manifest fixture")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "new.txt", "untracked and not ignored\n")
	writeFile(t, root, "debug.log", "ignored output\n")

	want := []FileState{
		manifestTestRegular(t, root, ".gitignore", "100644"),
		manifestTestRegular(t, root, "docs/guide.md", "100644"),
		manifestTestRegular(t, root, "go.mod", "100644"),
		manifestTestRegular(t, root, "group.sh", "100644"),
		{Path: "link", Mode: "120000", SHA256: manifestTestSum([]byte("plain.txt"))},
		manifestTestRegular(t, root, "main.go", "100644"),
		manifestTestRegular(t, root, "new.txt", "100644"),
		manifestTestRegular(t, root, "plain.txt", "100644"),
		manifestTestRegular(t, root, "tool.sh", "100755"),
	}
	captured, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, files, err := CaptureManifest(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != captured {
		t.Fatalf("CaptureManifest snapshot = %+v, Capture = %+v", snapshot, captured)
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("manifest entries:\n got %+v\nwant %+v", files, want)
	}
}

// M2: outside Git the manifest lists exactly the files Capture fingerprints
// (dependency trees such as node_modules are not eligible), with the same
// snapshot as Capture.
func TestCaptureManifestMatchesCaptureOutsideGit(t *testing.T) {
	root, err := CanonicalDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "b.txt", "second\n")
	writeFile(t, root, "a/z.txt", "first\n")
	writeFile(t, root, "node_modules/dep/index.js", "ignored dependency\n")
	captured, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, files, err := CaptureManifest(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != captured {
		t.Fatalf("CaptureManifest snapshot = %+v, Capture = %+v", snapshot, captured)
	}
	want := []FileState{
		manifestTestRegular(t, root, "a/z.txt", "100644"),
		manifestTestRegular(t, root, "b.txt", "100644"),
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("manifest entries:\n got %+v\nwant %+v", files, want)
	}
}

// M2: CaptureManifest fails closed exactly when Capture fails (here an
// escaping symlink).
func TestCaptureManifestFailsClosedWithCapture(t *testing.T) {
	root := gitFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Capture(t.Context(), root); err == nil {
		t.Fatal("fixture precondition: Capture accepted an escaping symlink")
	}
	if _, files, err := CaptureManifest(t.Context(), root); err == nil {
		t.Fatalf("CaptureManifest succeeded where Capture failed: %+v", files)
	}
}

// manifestTestCapture captures the manifest, requiring the same snapshot as
// Capture and valid UTF-8 paths sorted by Path.
func manifestTestCapture(t *testing.T, root string) []FileState {
	t.Helper()
	captured, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	snapshot, files, err := CaptureManifest(t.Context(), root)
	if err != nil {
		t.Fatalf("CaptureManifest failed: %v", err)
	}
	if snapshot != captured {
		t.Fatalf("CaptureManifest snapshot = %+v, Capture = %+v", snapshot, captured)
	}
	for _, file := range files {
		if !utf8.ValidString(file.Path) {
			t.Errorf("manifest path %q is not valid UTF-8", file.Path)
		}
	}
	if !slices.IsSortedFunc(files, func(a, b FileState) int { return strings.Compare(a.Path, b.Path) }) {
		t.Errorf("manifest entries are not sorted by path: %+v", files)
	}
	return files
}

// manifestTestContains requires exactly one entry per wanted path, equal to
// the wanted entry.
func manifestTestContains(t *testing.T, files []FileState, want ...FileState) {
	t.Helper()
	for _, expected := range want {
		count := 0
		for _, file := range files {
			if file.Path == expected.Path {
				count++
				if file != expected {
					t.Errorf("entry %+v, want %+v", file, expected)
				}
			}
		}
		if count != 1 {
			t.Errorf("manifest has %d entries for %q, want 1: %+v", count, expected.Path, files)
		}
	}
}

func manifestTestEntry(path, data string) FileState {
	return FileState{Path: path, Mode: "100644", SHA256: manifestTestSum([]byte(data))}
}

// M3 (revision 3 spelling): file names that are not valid UTF-8 do not make
// CaptureManifest fail; each invalid byte is spelled as a literal \xNN with
// lowercase hex, a backslash in such a name is doubled and valid characters
// are kept. Skipped where the filesystem refuses such names (macOS APFS).
func TestCaptureManifestEscapesNonUTF8Names(t *testing.T) {
	root := gitFixture(t)
	if err := os.WriteFile(filepath.Join(root, "caf\xe9.txt"), []byte("tracked latin-1 name\n"), 0o644); err != nil {
		t.Skipf("filesystem refuses a non-UTF-8 file name: %v", err)
	}
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "non-UTF-8 file name")
	writeFile(t, root, "a\xff\xfeb.txt", "untracked invalid name\n")
	writeFile(t, root, "b\\\xe9.txt", "backslash and invalid byte\n")
	writeFile(t, root, "café-ok.txt", "valid UTF-8 name\n")

	files := manifestTestCapture(t, root)
	manifestTestContains(t, files,
		manifestTestEntry(`caf\xe9.txt`, "tracked latin-1 name\n"),
		manifestTestEntry(`a\xff\xfeb.txt`, "untracked invalid name\n"),
		manifestTestEntry(`b\\\xe9.txt`, "backslash and invalid byte\n"),
		manifestTestEntry("café-ok.txt", "valid UTF-8 name\n"),
	)
}

// M3 (revision 3 spelling): a valid UTF-8 name that contains a backslash has
// each backslash doubled, whether tracked or untracked; names without a
// backslash are unchanged. Runs on macOS.
func TestCaptureManifestDoublesBackslashesInNames(t *testing.T) {
	root := gitFixture(t)
	writeFile(t, root, "caf\\xe9.txt", "tracked literal look-alike\n")
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "backslash file name")
	writeFile(t, root, "dir\\sub.txt", "untracked backslash name\n")
	writeFile(t, root, "plain.txt", "plain\n")

	files := manifestTestCapture(t, root)
	manifestTestContains(t, files,
		manifestTestEntry(`caf\\xe9.txt`, "tracked literal look-alike\n"),
		manifestTestEntry(`dir\\sub.txt`, "untracked backslash name\n"),
		manifestTestEntry("plain.txt", "plain\n"),
	)
	for _, file := range files {
		if file.Path == "caf\\xe9.txt" || file.Path == "dir\\sub.txt" {
			t.Errorf("backslash name %q kept its undoubled spelling", file.Path)
		}
	}
}

// M4: distinct files keep distinct spellings. The backslash look-alike part
// (a\x41.txt beside aA.txt) runs on macOS; the raw-byte part (caf 0xE9 beside
// the literal caf\xe9.txt) runs where the filesystem allows it.
func TestCaptureManifestKeepsLookAlikeNamesDistinct(t *testing.T) {
	t.Run("backslash escape look-alike", func(t *testing.T) {
		root := gitFixture(t)
		writeFile(t, root, "a\\x41.txt", "literal backslash name\n")
		writeFile(t, root, "aA.txt", "plain A name\n")
		files := manifestTestCapture(t, root)
		manifestTestContains(t, files,
			manifestTestEntry(`a\\x41.txt`, "literal backslash name\n"),
			manifestTestEntry("aA.txt", "plain A name\n"),
		)
		if len(files) != 4 {
			t.Fatalf("manifest has %d entries, want 4 (go.mod, main.go and two look-alikes): %+v", len(files), files)
		}
	})
	t.Run("raw byte look-alike", func(t *testing.T) {
		root := gitFixture(t)
		if err := os.WriteFile(filepath.Join(root, "caf\xe9.txt"), []byte("raw byte name\n"), 0o644); err != nil {
			t.Skipf("filesystem refuses a non-UTF-8 file name: %v", err)
		}
		writeFile(t, root, "caf\\xe9.txt", "literal look-alike\n")
		files := manifestTestCapture(t, root)
		manifestTestContains(t, files,
			manifestTestEntry(`caf\xe9.txt`, "raw byte name\n"),
			manifestTestEntry(`caf\\xe9.txt`, "literal look-alike\n"),
		)
		if len(files) != 4 {
			t.Fatalf("manifest has %d entries, want 4: %+v", len(files), files)
		}
	})
}

// manifestTestSpell is the M3 spelling rule written out from the contract: a
// valid UTF-8 path without a backslash is unchanged; otherwise each backslash
// becomes two and each byte of an invalid UTF-8 sequence becomes \xNN with
// lowercase hex, other characters kept.
func manifestTestSpell(raw string) string {
	if utf8.ValidString(raw) && !strings.Contains(raw, `\`) {
		return raw
	}
	var spelled strings.Builder
	for index := 0; index < len(raw); {
		r, width := utf8.DecodeRuneInString(raw[index:])
		switch {
		case r == utf8.RuneError && width == 1:
			fmt.Fprintf(&spelled, `\x%02x`, raw[index])
		case r == '\\':
			spelled.WriteString(`\\`)
		default:
			spelled.WriteString(raw[index : index+width])
		}
		index += width
	}
	return spelled.String()
}

// M5: ManifestRawPath reverses the spelling for the contract's examples and
// returns unchanged spellings unchanged.
func TestManifestRawPathExamples(t *testing.T) {
	for spelled, raw := range map[string]string{
		`t\\ool.sh`:       "t\\ool.sh",
		`caf\xe9.txt`:     "caf\xe9.txt",
		`caf\\xe9.txt`:    "caf\\xe9.txt",
		`a\\x41.txt`:      "a\\x41.txt",
		`b\\\xe9.txt`:     "b\\\xe9.txt",
		`a\xff\xfeb.txt`:  "a\xff\xfeb.txt",
		`dir/x\xc3.md`:    "dir/x\xc3.md",
		`\\\\two.txt`:     "\\\\two.txt",
		`trail\\`:         "trail\\",
		"plain.txt":       "plain.txt",
		"dir/sub/main.go": "dir/sub/main.go",
		"café-ok.txt":     "café-ok.txt",
		"notes\nx.md":     "notes\nx.md",
		"aA.txt":          "aA.txt",
	} {
		if got := ManifestRawPath(spelled); got != raw {
			t.Errorf("ManifestRawPath(%q) = %q, want %q", spelled, got, raw)
		}
	}
}

// M5: ManifestRawPath(spelling of p) == p over many relative paths: fixed
// look-alikes and edge cases plus deterministic pseudo-random byte strings
// mixing backslashes, "x", hex digits, slashes and invalid bytes.
func TestManifestRawPathRoundTrip(t *testing.T) {
	names := []string{
		"plain.txt", "dir/sub/file.go", "café.txt", "日本/語.md", "�.txt",
		"a\\x41.txt", "aA.txt", "caf\\xe9.txt", "caf\xe9.txt", "b\\\xe9.txt",
		"\\", "\\\\", "trail\\", "\\x", "\\x4", "\\x4g", "\\xZZ", "\\\\x41", "x\\\\\\x41",
		"\xff", "\xff\xfe", "\xe2\x82", "\xed\xa0\x80", "\xc3", "dir\\/sub", "a/\\/b",
		"%x41", "\\u0041", "x41\\", "\xe9\\xe9\\\xe9",
	}
	alphabet := []byte{'\\', 'x', 'X', '4', '1', 'e', '9', 'f', 'a', '/', '.', 'z', 0x80, 0xc3, 0xa9, 0xe9, 0xff, 0xfe, 0xe2, 0x82, 0xac}
	random := rand.New(rand.NewPCG(5, 2026))
	for range 2000 {
		name := make([]byte, 1+random.IntN(12))
		for index := range name {
			name[index] = alphabet[random.IntN(len(alphabet))]
		}
		if manifestTestRelative(string(name)) {
			names = append(names, string(name))
		}
	}
	for _, raw := range names {
		spelled := manifestTestSpell(raw)
		if !utf8.ValidString(spelled) {
			t.Fatalf("test oracle produced invalid UTF-8 for %q", raw)
		}
		if got := ManifestRawPath(spelled); got != raw {
			t.Errorf("ManifestRawPath(%q) = %q, want %q", spelled, got, raw)
		}
	}
}

// M5: through CaptureManifest, each entry's ManifestRawPath names the file on
// disk (its bytes hash to the entry's SHA256), every created file is named
// once, and each entry's path is the M3 spelling of its on-disk name. Raw-byte
// names are included where the filesystem allows them.
func TestCaptureManifestPathsMapBackToDisk(t *testing.T) {
	root := gitFixture(t)
	created := map[string]string{"go.mod": "", "main.go": ""}
	for _, name := range []string{"t\\ool.sh", "a\\x41.txt", "aA.txt", "caf\\xe9.txt", "two\\\\slashes.txt", "trail\\", "\\x4g.txt", "sub/na\\me.md", "café.txt", "plain.txt"} {
		writeFile(t, root, name, "literal "+name+"\n")
		created[name] = ""
	}
	for _, name := range []string{"caf\xe9.txt", "b\\\xe9.txt", "x\xff\xfe.txt", "sub/\xc3.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("raw "+name+"\n"), 0o644); err != nil {
			t.Logf("filesystem refuses %q; raw-byte part skipped: %v", name, err)
			continue
		}
		created[name] = ""
	}
	files := manifestTestCapture(t, root)
	seen := map[string]int{}
	for _, file := range files {
		raw := ManifestRawPath(file.Path)
		seen[raw]++
		if want := manifestTestSpell(raw); file.Path != want {
			t.Errorf("entry %q maps to %q, whose spelling is %q", file.Path, raw, want)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(raw)))
		if err != nil {
			t.Errorf("entry %q: ManifestRawPath gives %q, not a file on disk: %v", file.Path, raw, err)
			continue
		}
		if manifestTestSum(data) != file.SHA256 {
			t.Errorf("entry %q: file %q has different bytes", file.Path, raw)
		}
	}
	for name := range created {
		if seen[name] != 1 {
			t.Errorf("created file %q is named by %d manifest entries, want 1", name, seen[name])
		}
	}
	if len(files) != len(created) {
		t.Errorf("manifest has %d entries for %d files: %+v", len(files), len(created), files)
	}
}

// manifestTestRelative reports whether a generated name is a clean relative
// path: no leading or trailing slash, no empty, "." or ".." segment.
func manifestTestRelative(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
