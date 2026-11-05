package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/buildinfo"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

var releaseMarker = regexp.MustCompile(`CWRELEASE1\|([^|]+)\|([0-9a-f]{40})\|([^|]+)\|(darwin|linux)\|(amd64|arm64)\|go1\.27\.1\x7cCWEND`)

func archiveBinary(data []byte, release Release) ([]byte, bundled.Identity, error) {
	if len(data) == 0 || len(data) > maxArchiveBytes || digest(data) != release.ArchiveSHA256 {
		return nil, bundled.Identity{}, errors.New("standalone archive checksum differs from pinned release")
	}
	compressed := bytes.NewReader(data)
	zipped, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, bundled.Identity{}, err
	}
	defer zipped.Close()
	zipped.Multistream(false)
	limited := &io.LimitedReader{R: zipped, N: (256 << 20) + 1}
	reader := tar.NewReader(limited)
	seen := map[string]bool{}
	var executable []byte
	for count := 0; ; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, bundled.Identity{}, err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if count >= 1000 || limited.N <= 0 || name == "" || name == "." || path.Clean(name) != name || path.IsAbs(name) || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n:") || seen[name] || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir) {
			return nil, bundled.Identity{}, errors.New("standalone archive contains unsafe or duplicate entries")
		}
		seen[name] = true
		if name != "cw" && name != "LICENSE" && name != "THIRD_PARTY.md" && name != "licenses" && !strings.HasPrefix(name, "licenses/") {
			return nil, bundled.Identity{}, errors.New("standalone archive contains an unexpected resource")
		}
		if header.Size < 0 || header.Size > 256<<20 || header.Typeflag == tar.TypeDir && (header.Size != 0 || header.Mode != 0o755) || header.Typeflag == tar.TypeReg && name != "cw" && header.Mode != 0o644 {
			return nil, bundled.Identity{}, errors.New("standalone archive has invalid file mode or size")
		}
		if name != "cw" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Mode != 0o755 || header.Size <= 0 || header.Size > 128<<20 {
			return nil, bundled.Identity{}, errors.New("standalone archive has no bounded regular executable")
		}
		executable, err = io.ReadAll(reader)
		if err != nil || int64(len(executable)) != header.Size {
			return nil, bundled.Identity{}, errors.New("standalone executable size differs from archive header")
		}
	}
	trailing, err := io.ReadAll(limited)
	if err != nil || limited.N <= 0 || compressed.Len() != 0 || len(bytes.Trim(trailing, "\x00")) != 0 || len(executable) == 0 {
		return nil, bundled.Identity{}, errors.New("standalone archive is corrupt, incomplete or has trailing data")
	}
	identity, err := bundled.InspectBinary(executable)
	if err != nil || identity.Version != release.Version || identity.Revision != release.Revision || identity.OS != release.OS || identity.Arch != release.Arch {
		return nil, bundled.Identity{}, errors.New("standalone binary bundle identity differs from pinned release")
	}
	markers := releaseMarker.FindAllSubmatch(executable, 2)
	if len(markers) != 1 || string(markers[0][1]) != release.Version || string(markers[0][2]) != release.Revision || string(markers[0][4]) != release.OS || string(markers[0][5]) != release.Arch {
		return nil, bundled.Identity{}, errors.New("standalone binary source-release identity differs")
	}
	if _, err := time.Parse(time.RFC3339, string(markers[0][3])); err != nil {
		return nil, bundled.Identity{}, errors.New("standalone binary has an invalid build timestamp")
	}
	build, err := buildinfo.Read(bytes.NewReader(executable))
	if err != nil || build.GoVersion != "go1.27.1" || build.Path != "github.com/belevtsev/agent-workflows/cmd/cw" || build.Main.Path != "github.com/belevtsev/agent-workflows" {
		return nil, bundled.Identity{}, errors.New("standalone binary module or Go toolchain differs")
	}
	settings := map[string]string{}
	for _, setting := range build.Settings {
		if _, duplicate := settings[setting.Key]; duplicate {
			return nil, bundled.Identity{}, errors.New("standalone binary has duplicate build settings")
		}
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != release.OS || settings["GOARCH"] != release.Arch || settings["CGO_ENABLED"] != "0" || settings["-trimpath"] != "true" {
		return nil, bundled.Identity{}, errors.New("standalone binary target or build settings differ")
	}
	return executable, identity, nil
}
