package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveIsDeterministicAndIncludesNotices(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	source := filepath.Join(root, "source")
	writeTest(t, filepath.Join(stage, "cw"), []byte("native executable"))
	writeTest(t, filepath.Join(source, "LICENSE"), []byte("repository notice"))
	writeTest(t, filepath.Join(source, "THIRD_PARTY.md"), []byte("provenance"))
	writeTest(t, filepath.Join(source, "licenses", "dependency.txt"), []byte("dependency notice"))
	stamp := time.Unix(1700000000, 0).UTC()
	first, second := filepath.Join(root, "first.gz"), filepath.Join(root, "second.gz")
	if err := archive(first, stage, source, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(stage, "cw"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := archive(second, stage, source, stamp); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("archive depends on filesystem timestamps: %v", err)
	}
	zipped, err := gzip.NewReader(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	want := []string{"LICENSE", "THIRD_PARTY.md", "cw", "licenses/", "licenses/dependency.txt"}
	for _, name := range want {
		header, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != name || header.Uid != 0 || header.Gid != 0 || !header.ModTime.Equal(stamp) {
			t.Fatalf("unexpected normalized archive entry: %#v", header)
		}
		if name == "cw" && header.Mode != 0o755 {
			t.Fatal("cw executable mode was not retained")
		}
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected extra archive entry: %v", err)
	}
}

func TestSourceIdentityRejectsDifferentOrDirtyRevision(t *testing.T) {
	fixture := newPublicationFixture(t)
	if _, _, err := SourceIdentity(t.Context(), fixture.options.Source, "0000000000000000000000000000000000000000", true); err == nil {
		t.Fatal("different source revision accepted")
	}
	writeTest(t, filepath.Join(fixture.options.Source, "README.md"), []byte("uncommitted source"))
	if _, _, err := SourceIdentity(t.Context(), fixture.options.Source, fixture.options.Revision, true); err == nil {
		t.Fatal("dirty release source accepted")
	}
}

func TestBuildPinsBinariesAndNoticesWhenWorktreeChanges(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	main := `package main
import (
  "fmt"
  _ "embed"
)
var version, revision, builtAt, releaseIdentity string
//go:embed proof.txt
var proof string
func main() { fmt.Print(version, revision, builtAt, releaseIdentity, "ORIGINAL_BUILD_INPUT", proof) }
`
	inputs := map[string]string{
		"go.mod":                  "module github.com/belevtsev/agent-workflows\n\ngo 1.27.1\n",
		".gitattributes":          "LICENSE export-ignore\ncmd/cw/proof.txt export-subst\n",
		"cmd/cw/main.go":          main,
		"cmd/cw/proof.txt":        "ORIGINAL_EMBEDDED_INPUT $Format:%H$\n",
		"LICENSE":                 "ORIGINAL_LICENSE\n",
		"THIRD_PARTY.md":          "ORIGINAL_THIRD_PARTY\n",
		"licenses/dependency.txt": "ORIGINAL_DEPENDENCY_LICENSE\n",
	}
	for name, data := range inputs {
		writeTest(t, filepath.Join(source, name), []byte(data))
	}
	revision := commitSourceFixture(t, source)
	writeTest(t, filepath.Join(source, ".git", "info", "attributes"), []byte("cmd/cw/proof.txt export-ignore\nlicenses/dependency.txt export-ignore\n"))
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(root, "changed")
	for _, name := range []string{"cmd/cw/main.go", "cmd/cw/proof.txt", "LICENSE", "THIRD_PARTY.md", "licenses/dependency.txt"} {
		writeTest(t, filepath.Join(changed, name), []byte(strings.ReplaceAll(inputs[name], "ORIGINAL_", "MUTATED_")))
	}
	// All four real compilers wait until the original worktree has changed.
	// This is after clean-source validation and before any compiler reads input.
	wrapper := `#!/bin/sh
set -eu
if mkdir "$CW_RELEASE_TEST_GATE" 2>/dev/null; then
  cp "$CW_RELEASE_TEST_CHANGED/cmd/cw/main.go" "$CW_RELEASE_TEST_SOURCE/cmd/cw/main.go.tmp"
  mv "$CW_RELEASE_TEST_SOURCE/cmd/cw/main.go.tmp" "$CW_RELEASE_TEST_SOURCE/cmd/cw/main.go"
  cp "$CW_RELEASE_TEST_CHANGED/cmd/cw/proof.txt" "$CW_RELEASE_TEST_SOURCE/cmd/cw/proof.txt"
  cp "$CW_RELEASE_TEST_CHANGED/LICENSE" "$CW_RELEASE_TEST_SOURCE/LICENSE"
  cp "$CW_RELEASE_TEST_CHANGED/THIRD_PARTY.md" "$CW_RELEASE_TEST_SOURCE/THIRD_PARTY.md"
  cp "$CW_RELEASE_TEST_CHANGED/licenses/dependency.txt" "$CW_RELEASE_TEST_SOURCE/licenses/dependency.txt"
  touch "$CW_RELEASE_TEST_READY"
else
  while [ ! -e "$CW_RELEASE_TEST_READY" ]; do sleep 0.01; done
fi
exec "$CW_RELEASE_TEST_REAL_GO" "$@"
`
	tools := filepath.Join(root, "tools")
	writeTest(t, filepath.Join(tools, "go"), []byte(wrapper))
	if err := os.Chmod(filepath.Join(tools, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_RELEASE_TEST_GATE", filepath.Join(root, "gate"))
	t.Setenv("CW_RELEASE_TEST_READY", filepath.Join(root, "ready"))
	t.Setenv("CW_RELEASE_TEST_CHANGED", changed)
	t.Setenv("CW_RELEASE_TEST_SOURCE", source)
	t.Setenv("CW_RELEASE_TEST_REAL_GO", realGo)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	options := BuildOptions{Source: source, Version: "v9.8.7", Revision: revision, Dist: filepath.Join(root, "dist")}
	result, err := Build(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range ArchiveNames {
		executable, err := archiveExecutable(filepath.Join(result.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"ORIGINAL_BUILD_INPUT", inputs["cmd/cw/proof.txt"]} {
			if !bytes.Contains(executable, []byte(marker)) {
				t.Fatalf("%s does not contain exact committed compiler input %q", name, marker)
			}
		}
		for _, marker := range []string{"MUTATED_BUILD_INPUT", "MUTATED_EMBEDDED_INPUT"} {
			if bytes.Contains(executable, []byte(marker)) {
				t.Fatalf("%s compiled modified original worktree input %q", name, marker)
			}
		}
		members := readArchiveMembers(t, filepath.Join(result.Directory, name))
		for _, notice := range []string{"LICENSE", "THIRD_PARTY.md", "licenses/dependency.txt"} {
			if string(members[notice]) != inputs[notice] {
				t.Fatalf("%s notice %s = %q; want exact committed bytes %q", name, notice, members[notice], inputs[notice])
			}
		}
	}
	if dirty := gitTest(t, source, "status", "--porcelain"); dirty == "" {
		t.Fatal("mutation barrier did not change the original worktree")
	}
	for name, original := range inputs {
		current, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(original, "ORIGINAL_") && !bytes.Contains(current, []byte("MUTATED_")) {
			t.Fatalf("build restored or edited the original worktree input %s", name)
		}
		writeTest(t, filepath.Join(source, name), []byte(original))
	}
	options.Dist = filepath.Join(root, "repeated")
	repeated, err := Build(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	for name, digest := range result.Digests {
		if repeated.Digests[name] != digest {
			t.Fatalf("release asset %s depends on private export directory", name)
		}
	}
	t.Logf("All four real Go binaries, embedded inputs, and notices retain exact %s bytes across original-worktree mutation; repeated assets are identical", revision)
}

func readArchiveMembers(t *testing.T, filename string) map[string][]byte {
	t.Helper()
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	members := make(map[string][]byte)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return members
		}
		if err != nil {
			t.Fatal(err)
		}
		members[header.Name], err = io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
	}
}
