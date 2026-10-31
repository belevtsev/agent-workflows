package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxArchiveSize = 64 << 20
const maxExecutableSize = 128 << 20
const maxUnpackedSize = 256 << 20

// ValidateNativeAssets binds all verified archives to the requested cw version,
// revision, platform and pinned Go compiler before any publication network call.
// It reads Go build metadata without executing foreign-platform binaries.
func ValidateNativeAssets(directory, version, revision string, stamp time.Time) (*Assets, error) {
	assets, err := ValidateAssets(directory)
	if err != nil {
		return nil, err
	}
	for _, name := range ArchiveNames {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, "cw_"), ".tar.gz"), "_")
		executable, err := archiveExecutable(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("invalid release archive %s: %w", name, err)
		}
		info, err := buildinfo.Read(bytes.NewReader(executable))
		if err != nil {
			return nil, fmt.Errorf("release archive %s does not contain a Go cw binary", name)
		}
		if info.Path != "github.com/belevtsev/agent-workflows/cmd/cw" || info.Main.Path != "github.com/belevtsev/agent-workflows" || info.GoVersion != "go1.27.1" {
			return nil, fmt.Errorf("release binary module or Go toolchain differs: %s", name)
		}
		settings := make(map[string]string)
		for _, setting := range info.Settings {
			if _, duplicate := settings[setting.Key]; duplicate {
				return nil, fmt.Errorf("duplicate release binary build setting: %s", name)
			}
			settings[setting.Key] = setting.Value
		}
		marker := []byte(releaseMarker(version, revision, stamp, parts[0], parts[1]))
		// Go deliberately omits -ldflags build settings when -trimpath is
		// enabled. This retained linker string records release identity without
		// exposing source paths or executing a foreign-platform binary. It is
		// identity metadata, not an attestation of how the binary was built.
		if bytes.Count(executable, []byte("CWRELEASE1|")) != 1 || bytes.Count(executable, []byte("|CWEND")) != 1 || !bytes.Contains(executable, marker) ||
			settings["GOOS"] != parts[0] || settings["GOARCH"] != parts[1] || settings["CGO_ENABLED"] != "0" || settings["-trimpath"] != "true" {
			return nil, fmt.Errorf("release binary identity differs from version, source revision or target: %s", name)
		}
	}
	return assets, nil
}

func releaseMarker(version, revision string, stamp time.Time, targetOS, targetArch string) string {
	return strings.Join([]string{"CWRELEASE1", version, revision, stamp.UTC().Format(time.RFC3339), targetOS, targetArch, "go1.27.1", "CWEND"}, "|")
}

func archiveExecutable(filename string) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxArchiveSize {
		return nil, errors.New("archive must be a bounded regular file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// A buffered byte reader and Multistream(false) let us reject trailing gzip
	// members or data rather than hiding them behind the first valid tar archive.
	data, err := io.ReadAll(io.LimitReader(file, maxArchiveSize+1))
	if err != nil || len(data) > maxArchiveSize {
		return nil, errors.Join(errors.New("archive exceeds compressed input limit"), err)
	}
	compressed := bytes.NewReader(data)
	zipped, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, err
	}
	defer zipped.Close()
	zipped.Multistream(false)
	limited := &io.LimitedReader{R: zipped, N: maxUnpackedSize + 1}
	reader := tar.NewReader(limited)
	seen := make(map[string]bool)
	var executable []byte
	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if entries >= 1000 || limited.N <= 0 {
			return nil, errors.New("archive exceeds unpacked input limit")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || path.Clean(name) != name || path.IsAbs(name) || strings.Contains(name, "\\") || strings.ContainsAny(name, "\x00\r\n") || strings.HasPrefix(name, "../") || seen[name] {
			return nil, errors.New("archive contains unsafe or duplicate member names")
		}
		seen[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return nil, errors.New("archive contains a link or unsupported member")
		}
		if name != "cw" && name != "LICENSE" && name != "THIRD_PARTY.md" && name != "licenses" && !strings.HasPrefix(name, "licenses/") {
			return nil, errors.New("archive contains an unexpected member")
		}
		if header.Size < 0 || header.Size > maxUnpackedSize || (header.Typeflag == tar.TypeDir && header.Size != 0) {
			return nil, errors.New("archive member has an invalid size")
		}
		if name != "cw" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxExecutableSize || header.Mode != 0o755 {
			return nil, errors.New("archive has an invalid cw executable")
		}
		executable, err = io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(executable)) != header.Size {
			return nil, errors.Join(errors.New("archive executable size differs"), err)
		}
	}
	// Reading through the gzip trailer checks its checksum and remaining bytes;
	// tar permits its usual zero padding, but no additional tar payload.
	trailing, err := io.ReadAll(limited)
	if err != nil || limited.N <= 0 || compressed.Len() != 0 {
		return nil, errors.Join(errors.New("archive has invalid trailing data"), err)
	}
	if len(bytes.Trim(trailing, "\x00")) != 0 {
		return nil, errors.New("archive has trailing tar payload")
	}
	if executable == nil {
		return nil, errors.New("archive is missing its cw executable")
	}
	return executable, nil
}
