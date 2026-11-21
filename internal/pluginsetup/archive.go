package pluginsetup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	archiveName     = "kitsu-plugin.zip"
	marketplaceName = "kitsu-marketplace"
	pluginID        = "kitsu@kitsu"
	manifestLimit   = 1 << 20
	archiveLimit    = 32 << 20
)

type member struct {
	data []byte
	dir  bool
}

type release struct {
	digest  string
	members map[string]member
}

func loadRelease(dir, host string) (release, error) {
	sums, err := readRegular(filepath.Join(dir, "SHA256SUMS"), manifestLimit)
	if err != nil {
		return release{}, err
	}
	digest, err := checksum(sums)
	if err != nil {
		return release{}, err
	}
	data, err := readRegular(filepath.Join(dir, archiveName), archiveLimit)
	if err != nil {
		return release{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return release{}, fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	members, err := archiveMembers(data)
	if err != nil {
		return release{}, fmt.Errorf("read %s: %w", archiveName, err)
	}
	version := pluginVersion(members[pluginManifest(host)].data)
	if !isKitsuMarketplace(host, members[marketManifest(host)].data) || version == "" {
		return release{}, fmt.Errorf("%s must contain a local Kitsu marketplace and plugin manifest for %s", archiveName, host)
	}
	return release{digest: digest, members: members}, nil
}

func checksum(data []byte) (string, error) {
	var digest string
	var count int
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		count++
		hexBytes, err := hex.DecodeString(fields[0])
		if len(fields) == 2 && err == nil && len(hexBytes) == sha256.Size {
			digest = strings.ToLower(fields[0])
		}
	}
	if count != 1 || digest == "" {
		return "", fmt.Errorf("SHA256SUMS must list %s exactly once with a 64-digit SHA-256 checksum", archiveName)
	}
	return digest, nil
}

func archiveMembers(data []byte) (map[string]member, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(archive.File) > 4096 {
		return nil, errors.New("too many archive members")
	}
	members := map[string]member{".": {dir: true}}
	seen := make(map[string]string)
	listed := make(map[string]bool)
	remaining := uint64(archiveLimit)
	for _, file := range archive.File {
		name, dir, err := archiveNameFor(file)
		if err != nil {
			return nil, err
		}
		if listed[name] || file.UncompressedSize64 > remaining {
			return nil, fmt.Errorf("duplicate or oversized archive member %q", file.Name)
		}
		listed[name] = true
		if err := addArchivePaths(members, seen, name, dir); err != nil {
			return nil, err
		}
		if dir {
			continue
		}
		payload, err := readMember(file)
		if err != nil {
			return nil, fmt.Errorf("read archive member %q: %w", file.Name, err)
		}
		remaining -= uint64(len(payload))
		members[name] = member{data: payload}
	}
	return members, nil
}

func archiveNameFor(file *zip.File) (name string, dir bool, err error) {
	full := strings.TrimSuffix(file.Name, "/")
	dir = full != file.Name
	mode := file.Mode().Type()
	validType := (dir && mode == fs.ModeDir && file.UncompressedSize64 == 0) || (!dir && mode == 0)
	if !fs.ValidPath(full) || strings.ContainsAny(full, "\\:\x00") || !validType {
		return "", false, fmt.Errorf("unsafe archive member %q", file.Name)
	}
	if full == marketplaceName && dir {
		return ".", true, nil
	}
	name, ok := strings.CutPrefix(full, marketplaceName+"/")
	if !ok {
		return "", false, fmt.Errorf("archive member %q is outside %s", file.Name, marketplaceName)
	}
	if _, err := filepath.Localize(name); err != nil {
		return "", false, fmt.Errorf("unsafe archive member %q", file.Name)
	}
	return name, dir, nil
}

func addArchivePaths(members map[string]member, seen map[string]string, name string, dir bool) error {
	for current := name; current != "."; current = path.Dir(current) {
		isDir := current != name || dir
		folded := strings.ToLower(current)
		if original, ok := seen[folded]; ok && (original != current || members[current].dir != isDir) {
			return fmt.Errorf("archive paths conflict at %q", current)
		}
		seen[folded] = current
		if isDir {
			members[current] = member{dir: true}
		}
	}
	return nil
}

func readMember(file *zip.File) ([]byte, error) {
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, archiveLimit+1))
	if err == nil && uint64(len(data)) != file.UncompressedSize64 {
		err = errors.New("uncompressed size differs from archive header")
	}
	return data, err
}

func readRegular(file string, limit int64) ([]byte, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%q must be a regular file of at most %d bytes", file, limit)
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%q exceeds %d bytes", file, limit)
	}
	return data, err
}

func marketManifest(host string) string {
	if host == "codex" {
		return ".agents/plugins/marketplace.json"
	}
	return ".claude-plugin/marketplace.json"
}

func pluginManifest(host string) string { return "." + host + "-plugin/plugin.json" }

func pluginVersion(data []byte) string {
	var plugin struct{ Name, Version string }
	if json.Unmarshal(data, &plugin) != nil || plugin.Name != "kitsu" {
		return ""
	}
	return plugin.Version
}

func isKitsuMarketplace(host string, data []byte) bool {
	var market struct {
		Name    string
		Plugins []struct {
			Name   string
			Source json.RawMessage
		}
	}
	want := [][2]string{{"kitsu", "./"}, {"kitsu-team", "./plugins/kitsu-team"}} // The opt-in plugin may follow the default one.
	if json.Unmarshal(data, &market) != nil || market.Name != "kitsu" || len(market.Plugins) == 0 || len(market.Plugins) > len(want) {
		return false
	}
	for i, plugin := range market.Plugins {
		local := struct{ Source, Path string }{Source: "local"}
		// Codex gives a source object and Claude Code the path alone.
		source := map[bool]any{true: &local, false: &local.Path}[host == "codex"]
		if json.Unmarshal(plugin.Source, source) != nil || plugin.Name != want[i][0] || local.Source != "local" || local.Path != want[i][1] {
			return false
		}
	}
	return true
}
