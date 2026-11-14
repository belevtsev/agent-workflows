package taskflow

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// linkFailingWith is a bundleLink that fails as link(2) does with err and
// creates nothing.
func linkFailingWith(err error) bundleLink {
	return func(_ *os.Root, oldname, newname string) error {
		return &os.LinkError{Op: "linkat", Old: oldname, New: newname, Err: err}
	}
}

// bundleDirFiles returns the names in dir and the content of name there, or
// nil when it does not exist.
func bundleDirFiles(t *testing.T, dir, name string) ([]string, []byte) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return names, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return names, data
}

// writeBundleFile falls back to writing the bundle in place only when the
// filesystem has no hard links, and still never leaves a partial or
// replaced file.
//
// Oracle: a link failing with EPERM, ENOTSUP, EOPNOTSUPP or ErrUnsupported
// (vfat, exFAT, some network mounts) still writes the bundle: the directory
// then holds exactly bundle.json, with the bytes given and mode 0600. With
// links unsupported an existing bundle.json is refused (fs.ErrExist) and kept.
// A link failing for any other reason, EEXIST or EIO, is not retried: the
// call fails and the directory stays empty.
func TestWriteBundleFileFallsBackOnlyWithoutHardLinks(t *testing.T) {
	data := []byte("{\n  \"version\": 2\n}\n")
	for name, linkErr := range map[string]error{
		"EPERM":          syscall.EPERM,
		"ENOTSUP":        syscall.ENOTSUP,
		"EOPNOTSUPP":     syscall.EOPNOTSUPP,
		"ErrUnsupported": errors.ErrUnsupported,
	} {
		t.Run("writes without links, "+name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := writeBundleFile(filepath.Join(dir, "bundle.json"), data, linkFailingWith(linkErr), syncTaskDirectory); err != nil {
				t.Fatalf("link failing with %v: %v; want the bundle written without a link", linkErr, err)
			}
			names, written := bundleDirFiles(t, dir, "bundle.json")
			if !slices.Equal(names, []string{"bundle.json"}) || !bytes.Equal(written, data) {
				t.Fatalf("directory holds %v, bundle.json %q; want only bundle.json with %q", names, written, data)
			}
			info, err := os.Stat(filepath.Join(dir, "bundle.json"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("bundle mode %v, want 0600", info.Mode().Perm())
			}
		})
	}
	t.Run("refuses an existing file without links", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bundle.json"), []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := writeBundleFile(filepath.Join(dir, "bundle.json"), data, linkFailingWith(syscall.EPERM), syncTaskDirectory)
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("existing bundle.json without links: %v; want an error wrapping fs.ErrExist", err)
		}
		if names, kept := bundleDirFiles(t, dir, "bundle.json"); !slices.Equal(names, []string{"bundle.json"}) || string(kept) != "keep\n" {
			t.Fatalf("directory holds %v, bundle.json %q; want only the existing file, unchanged", names, kept)
		}
	})
	for name, linkErr := range map[string]error{"EEXIST": syscall.EEXIST, "EIO": syscall.EIO} {
		t.Run("does not retry a link failing with "+name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			err := writeBundleFile(filepath.Join(dir, "bundle.json"), data, linkFailingWith(linkErr), syncTaskDirectory)
			if !errors.Is(err, linkErr) {
				t.Fatalf("link failing with %v: %v; want that error", linkErr, err)
			}
			if names, _ := bundleDirFiles(t, dir, "bundle.json"); len(names) != 0 {
				t.Fatalf("directory holds %v; want nothing", names)
			}
		})
	}
}

// The bundle's errors speak of the reviewer bundle, not of a task artifact.
//
// Oracle: a failed write says "write the reviewer bundle to <path>"; a
// directory sync failing after the link says the bundle was written to <path>
// but its durability is uncertain, and the bundle is there whole.
func TestWriteBundleFileErrorsNameTheBundle(t *testing.T) {
	data := []byte("{}\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeBundleFile(path, data, (*os.Root).Link, syncTaskDirectory)
	if err == nil || !strings.HasPrefix(err.Error(), "write the reviewer bundle to "+path+": ") || strings.Contains(err.Error(), "task artifact") {
		t.Fatalf("existing bundle: %v; want it to start %q and not name a task artifact", err, "write the reviewer bundle to "+path+": ")
	}
	syncFailure := errors.New("sync failed")
	path = filepath.Join(t.TempDir(), "bundle.json")
	err = writeBundleFile(path, data, (*os.Root).Link, func(*os.Root) error { return syncFailure })
	want := "the reviewer bundle was written to " + path + " but its durability is uncertain: sync failed"
	if !errors.Is(err, syncFailure) || err.Error() != want {
		t.Fatalf("failed directory sync: %v; want %q", err, want)
	}
	if written, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(written, data) {
		t.Fatalf("bundle after a failed directory sync: %q, %v; want %q", written, readErr, data)
	}
}
