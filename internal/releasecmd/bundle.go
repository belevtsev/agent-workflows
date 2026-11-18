package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const versionNumber = `(0|[1-9][0-9]*)`
const prereleaseID = `(` + versionNumber + `|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`

var (
	semverTag = regexp.MustCompile(`^v` + versionNumber + `\.` + versionNumber + `\.` + versionNumber + `(-` + prereleaseID + `(\.` + prereleaseID + `)*)?$`)
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

const (
	manifestName = "SHA256SUMS"
	notesName    = "release-notes.md"
)

// bundleFiles maps the published names to their build paths. It follows the
// archive names in .goreleaser.yaml and also covers the installers and SBOMs.
func bundleFiles(tag string) []string {
	files := []string{"install.sh", "install.ps1", ".kitsu-release-plugin/kitsu-plugin.zip"}
	for _, platform := range []struct{ os, suffix string }{
		{"darwin", "tar.gz"},
		{"linux", "tar.gz"},
		{"windows", "zip"},
	} {
		for _, arch := range []string{"amd64", "arm64"} {
			archive := fmt.Sprintf("dist/kitsu_%s_%s_%s.%s", tag, platform.os, arch, platform.suffix)
			files = append(files, archive, archive+".sbom.json")
		}
	}
	return files
}

func prepareBundle(tag, directory, notes string) error {
	if err := os.Mkdir(directory, 0o755); err != nil {
		return err
	}
	for _, source := range append(bundleFiles(tag), notes) {
		if err := copyFile(source, filepath.Join(directory, filepath.Base(source))); err != nil {
			return err
		}
	}
	manifest, _, err := bundleManifest(tag, directory)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, manifestName), manifest, 0o644)
}

func copyFile(source, destination string) (err error) {
	input, err := openBundleFile(source)
	if err != nil {
		return err
	}
	defer checkErrClose(&err, input)
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer checkErrClose(&err, output)
	_, err = io.Copy(output, input)
	return err
}

func openBundleFile(name string) (*os.File, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, fmt.Errorf("missing or invalid release file %s", name)
	}
	return os.Open(name)
}

func checksum(directory, asset string) (digest string, err error) {
	file, err := openBundleFile(filepath.Join(directory, asset))
	if err != nil {
		return "", err
	}
	defer checkErrClose(&err, file)
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func bundleManifest(tag, directory string) ([]byte, map[string]string, error) {
	var manifest bytes.Buffer
	assets := make(map[string]string)
	for _, file := range bundleFiles(tag) {
		name := filepath.Base(file)
		digest, err := checksum(directory, name)
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", digest, name)
		assets[name] = "sha256:" + digest
	}
	return manifest.Bytes(), assets, nil
}

func checkBundle(tag, directory string) (map[string]string, error) {
	manifest, assets, err := bundleManifest(tag, directory)
	if err != nil {
		return nil, err
	}
	actual, err := os.ReadFile(filepath.Join(directory, manifestName))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(manifest, actual) {
		return nil, fmt.Errorf("%s does not match the complete release bundle", manifestName)
	}
	notes, err := openBundleFile(filepath.Join(directory, notesName))
	if err != nil {
		return nil, err
	}
	if err := notes.Close(); err != nil {
		return nil, err
	}
	digest, err := checksum(directory, manifestName)
	if err != nil {
		return nil, err
	}
	assets[manifestName] = "sha256:" + digest
	return assets, nil
}
