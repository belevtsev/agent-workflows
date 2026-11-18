package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckBundleRequiresEveryFileAndDigest(t *testing.T) {
	const tag = "v0.1.0"
	directory := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range bundleFiles(tag) {
		write(filepath.Base(file), []byte(file))
	}
	write(notesName, []byte("release notes"))
	manifest, _, err := bundleManifest(tag, directory)
	if err != nil {
		t.Fatal(err)
	}
	write(manifestName, manifest)
	assets, err := checkBundle(tag, directory)
	if err != nil || len(assets) != 16 {
		t.Fatalf("verified %d assets, want 16: %v", len(assets), err)
	}
	write(manifestName, manifest[:bytes.LastIndexByte(manifest[:len(manifest)-1], '\n')+1])
	if _, err := checkBundle(tag, directory); err == nil {
		t.Fatal("accepted a manifest that omits an SBOM")
	}
	write(manifestName, manifest)
	write("install.sh", []byte("changed installer"))
	if _, err := checkBundle(tag, directory); err == nil {
		t.Fatal("accepted a changed installer")
	}
}
