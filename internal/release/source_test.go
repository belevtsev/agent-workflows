package release

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportSourceUsesCompleteExactBlobs(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		".gitattributes": "LICENSE export-ignore\npayload export-subst\n",
		"LICENSE":        "original notice\n",
		"payload":        "$Format:%H$\n",
		"nested/tool":    "executable input\n",
	}
	for name, data := range files {
		writeTest(t, filepath.Join(source, name), []byte(data))
	}
	if err := os.Chmod(filepath.Join(source, "nested", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	revision := commitSourceFixture(t, source)
	// Local archive attributes can differ from the commit without making the
	// worktree dirty. Neither these nor committed attributes may transform inputs.
	writeTest(t, filepath.Join(source, ".git", "info", "attributes"), []byte("nested/tool export-ignore\n"))
	writeTest(t, filepath.Join(source, "LICENSE"), []byte("edited worktree notice\n"))
	writeTest(t, filepath.Join(source, "untracked"), []byte("not a source input\n"))
	destination := filepath.Join(t.TempDir(), "source")
	if err := exportSource(t.Context(), source, revision, destination); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || string(got) != want {
			t.Fatalf("exported %s = %q, %v; want exact committed bytes %q", name, got, err, want)
		}
	}
	info, err := os.Stat(filepath.Join(destination, "nested", "tool"))
	if err != nil || info.Mode().Perm()&0o111 != 0o111 {
		t.Fatalf("exported executable mode: %v, %v", info, err)
	}
	for _, name := range []string{".git", "untracked"} {
		if _, err := os.Lstat(filepath.Join(destination, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("export included non-commit input %s: %v", name, err)
		}
	}
	current, err := os.ReadFile(filepath.Join(source, "LICENSE"))
	if err != nil || string(current) != "edited worktree notice\n" {
		t.Fatalf("export changed the original worktree: %q, %v", current, err)
	}
}

func TestExportSourceRejectsLinksAndUnavailableObjects(t *testing.T) {
	for _, scenario := range []string{"symlink", "missing commit"} {
		t.Run(scenario, func(t *testing.T) {
			source := t.TempDir()
			writeTest(t, filepath.Join(source, "regular"), []byte("source"))
			if scenario == "symlink" {
				if err := os.Symlink("../outside", filepath.Join(source, "link")); err != nil {
					t.Fatal(err)
				}
			}
			revision := commitSourceFixture(t, source)
			if scenario == "missing commit" {
				revision = strings.Repeat("0", 40)
			}
			destination := filepath.Join(t.TempDir(), "source")
			if err := exportSource(t.Context(), source, revision, destination); err == nil {
				t.Fatalf("%s was accepted", scenario)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("failed export left a source directory: %v", err)
			}
		})
	}
}

func TestSourceFilesRejectsUnsafeOrUnboundedManifest(t *testing.T) {
	object := strings.Repeat("a", 40)
	entry := func(mode, kind, name string) string { return mode + " " + kind + " " + object + "\t" + name + "\x00" }
	for name, input := range map[string]string{
		"empty":         "",
		"incomplete":    strings.TrimSuffix(entry("100644", "blob", "file"), "\x00"),
		"parent":        entry("100644", "blob", "../outside"),
		"absolute":      entry("100644", "blob", "/outside"),
		"backslash":     entry("100644", "blob", `dir\file`),
		"alias":         entry("100644", "blob", "dir/../file"),
		"git metadata":  entry("100644", "blob", ".git/config"),
		"duplicate":     strings.Repeat(entry("100644", "blob", "file"), 2),
		"symlink":       entry("120000", "blob", "link"),
		"gitlink":       entry("160000", "commit", "submodule"),
		"manifest size": strings.Repeat("a", maxSourceManifest+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := sourceFiles([]byte(input)); err == nil {
				t.Fatal("invalid source manifest accepted")
			}
		})
	}
	var many strings.Builder
	for index := range maxSourceFiles + 1 {
		many.WriteString(entry("100644", "blob", fmt.Sprintf("file-%d", index)))
	}
	if _, err := sourceFiles([]byte(many.String())); err == nil {
		t.Fatal("source file count limit was not enforced")
	}
}

func TestExtractSourceBlobsRejectsMissingChangedOrUnboundedData(t *testing.T) {
	data := "original bytes"
	object := sourceBlobID(data)
	valid := fmt.Sprintf("%s blob %d\n%s\n", object, len(data), data)
	for name, input := range map[string]string{
		"missing":       object + " missing\n",
		"wrong object":  strings.Repeat("a", 40) + " blob 0\n\n",
		"wrong type":    object + " commit 0\n\n",
		"header limit":  strings.Repeat("a", 8192),
		"negative size": object + " blob -1\n",
		"size limit":    fmt.Sprintf("%s blob %d\n", object, maxSourceBytes+1),
		"truncated":     strings.TrimSuffix(valid, "bytes\n"),
		"changed bytes": strings.Replace(valid, data, "different data", 1),
		"separator":     strings.TrimSuffix(valid, "\n") + "x",
		"trailing":      valid + "unrequested bytes",
	} {
		t.Run(name, func(t *testing.T) {
			files := []sourceFile{{name: "file", object: object, mode: 0o644}}
			if err := extractSourceBlobs(t.Context(), strings.NewReader(input), files, t.TempDir()); err == nil {
				t.Fatal("invalid source blob stream accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := extractSourceBlobs(ctx, strings.NewReader(valid), []sourceFile{{name: "file", object: object}}, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}

func TestSourceManifestOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	var output sourceBuffer
	output.limit = 8
	input := struct{ io.Reader }{bytes.NewReader(make([]byte, 9))}
	if _, err := io.Copy(&output, input); err == nil || len(output.Bytes()) > output.limit {
		t.Fatalf("unbounded manifest accepted: len=%d, err=%v", len(output.Bytes()), err)
	}
}

func TestExportSourceCleansPartialFailureAndCancellation(t *testing.T) {
	for _, scenario := range []string{"incomplete", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeTest(t, filepath.Join(source, "a"), []byte("first"))
			writeTest(t, filepath.Join(source, "b"), []byte("second"))
			revision := commitSourceFixture(t, source)
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			wrapper := `#!/bin/sh
set -eu
for argument do
  if [ "$argument" = cat-file ]; then
    if [ "$CW_RELEASE_EXPORT_CASE" = canceled ]; then
      touch "$CW_RELEASE_EXPORT_READY"
      exec sleep 300
    fi
    cat "$CW_RELEASE_EXPORT_DATA"
    exit 0
  fi
done
exec "$CW_RELEASE_REAL_GIT" "$@"
`
			tools := filepath.Join(root, "tools")
			writeTest(t, filepath.Join(tools, "git"), []byte(wrapper))
			if err := os.Chmod(filepath.Join(tools, "git"), 0o755); err != nil {
				t.Fatal(err)
			}
			batch := filepath.Join(root, "batch")
			writeTest(t, batch, []byte(fmt.Sprintf("%s blob 5\nfirst\n", sourceBlobID("first"))))
			ready := filepath.Join(root, "ready")
			t.Setenv("CW_RELEASE_REAL_GIT", git)
			t.Setenv("CW_RELEASE_EXPORT_CASE", scenario)
			t.Setenv("CW_RELEASE_EXPORT_DATA", batch)
			t.Setenv("CW_RELEASE_EXPORT_READY", ready)
			t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			destination := filepath.Join(root, "export")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- exportSource(ctx, source, revision, destination) }()
			if scenario == "canceled" {
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
			waitReady:
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					select {
					case err := <-done:
						t.Fatalf("export stopped before cancellation barrier: %v", err)
					case <-deadline.C:
						t.Fatal("export did not reach the blocked Git reader")
					case <-ticker.C:
						continue waitReady
					}
				}
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || (scenario == "canceled" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("export failure was not preserved: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("export did not stop after failure or cancellation")
			}
			if _, err := os.Stat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("partial export survived failure: %v", err)
			}
		})
	}
}

func commitSourceFixture(t *testing.T, source string) string {
	t.Helper()
	gitTest(t, source, "init", "-q")
	gitTest(t, source, "add", ".")
	gitTest(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "source fixture")
	return gitTest(t, source, "rev-parse", "HEAD")
}

func sourceBlobID(data string) string {
	digest := sha1.New()
	fmt.Fprintf(digest, "blob %d\x00%s", len(data), data)
	return hex.EncodeToString(digest.Sum(nil))
}
