package distribution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/manageruntime"
)

func (client Client) validateRelease(release Release) error {
	_, download, err := client.bases()
	if err != nil {
		return err
	}
	if _, err := compareVersion(release.Version, release.Version); err != nil {
		return err
	}
	name := "cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if release.ID <= 0 || !sourceRevision.MatchString(release.Revision) || release.OS != runtime.GOOS || release.Arch != runtime.GOARCH || release.ArchiveName != name || release.ArchiveURL != download+"/"+release.Version+"/"+name || !contentDigest.MatchString(release.ArchiveSHA256) || !contentDigest.MatchString(release.ChecksumsSHA256) {
		return errors.New("invalid or conflicting pinned standalone release")
	}
	return nil
}

func secureDirectory(directory string, create bool) error {
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			// macOS's system aliases are canonicalized by the installer, but
			// also occur in Go's temporary directory paths during local checks.
			if (current == "/var" || current == "/tmp") && info.Mode()&os.ModeSymlink != 0 {
				target, readErr := os.Readlink(current)
				if readErr == nil && target == "/private"+current {
					continue
				}
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe standalone cache directory: %s", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if create {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm() != 0o700 {
		return errors.New("standalone candidate cache must be private and owned by the current user")
	}
	return nil
}

func regularBytes(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("standalone cache file is missing, unsafe or oversized")
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil || int64(len(data)) > limit || int64(len(data)) != info.Size() {
		return nil, errors.New("standalone cache file changed during read")
	}
	return data, nil
}

func (client Client) inspect(ctx context.Context, path string, expected bundled.Identity, expectedBytes []byte) (manageruntime.Candidate, error) {
	inspect := client.Inspect
	if inspect == nil {
		inspect = manageruntime.InspectStandalone
	}
	identity, err := inspect(ctx, path)
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	if identity.Protocol != "cw-manager-v10" || identity.Bundle == nil || *identity.Bundle != expected || identity.Version != expected.Version || identity.Revision != expected.Revision || identity.OS != expected.OS || identity.Arch != expected.Arch || identity.SHA256 != digest(expectedBytes) {
		return manageruntime.Candidate{}, errors.New("prepared standalone candidate differs from pinned archive identity")
	}
	identity.Path = path
	return identity, nil
}

func (client Client) cached(ctx context.Context, directory string, release Release) (manageruntime.Candidate, error) {
	if err := secureDirectory(directory, false); err != nil {
		return manageruntime.Candidate{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		return manageruntime.Candidate{}, errors.New("standalone candidate cache contains incomplete or unexpected files")
	}
	for _, entry := range entries {
		if entry.Name() != "cw" && entry.Name() != "archive.tar.gz" {
			return manageruntime.Candidate{}, errors.New("standalone candidate cache contains unexpected files")
		}
	}
	archive, err := regularBytes(filepath.Join(directory, "archive.tar.gz"), maxArchiveBytes)
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	executable, expected, err := archiveBinary(archive, release)
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	path := filepath.Join(directory, "cw")
	cached, err := regularBytes(path, 128<<20)
	if err != nil || !bytes.Equal(cached, executable) {
		return manageruntime.Candidate{}, errors.New("cached standalone executable differs from pinned archive")
	}
	return client.inspect(ctx, path, expected, executable)
}

// Prepare stages a pinned candidate and its checksum-verifiable archive. It
// never replaces the active runtime, follows source fallbacks or invokes tools.
func (client Client) Prepare(ctx context.Context, release Release, stateRoot string) (manageruntime.Candidate, error) {
	if err := client.validateRelease(release); err != nil {
		return manageruntime.Candidate{}, err
	}
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot || strings.ContainsAny(stateRoot, "\x00\r\n") {
		return manageruntime.Candidate{}, errors.New("standalone acquisition requires an absolute clean state root")
	}
	if err := ctx.Err(); err != nil {
		return manageruntime.Candidate{}, err
	}
	cache := filepath.Join(stateRoot, "runtime", "candidates")
	key := release.Version + "-" + release.Revision + "-" + release.ArchiveSHA256
	destination := filepath.Join(cache, key)
	if _, err := os.Lstat(destination); err == nil {
		return client.cached(ctx, destination, release)
	} else if !errors.Is(err, os.ErrNotExist) {
		return manageruntime.Candidate{}, err
	}
	if err := secureDirectory(cache, true); err != nil {
		return manageruntime.Candidate{}, err
	}
	archive, err := client.fetch(ctx, release.ArchiveURL, maxArchiveBytes)
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	executable, expected, err := archiveBinary(archive, release)
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	stage, err := os.MkdirTemp(cache, ".standalone-")
	if err != nil {
		return manageruntime.Candidate{}, err
	}
	defer os.RemoveAll(stage)
	write := func(name string, data []byte, mode os.FileMode) error {
		file, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		return errors.Join(writeErr, file.Sync(), file.Close())
	}
	if err := write("archive.tar.gz", archive, 0o600); err != nil {
		return manageruntime.Candidate{}, err
	}
	if err := write("cw", executable, 0o700); err != nil {
		return manageruntime.Candidate{}, err
	}
	if _, err := client.inspect(ctx, filepath.Join(stage, "cw"), expected, executable); err != nil {
		return manageruntime.Candidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return manageruntime.Candidate{}, err
	}
	// A directory reservation never replaces even an empty occupied path.
	// Publish cw last so its appearance is the cache completion point.
	if err := os.Mkdir(destination, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return client.cached(ctx, destination, release)
		}
		return manageruntime.Candidate{}, err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(destination)
		}
	}()
	for _, name := range []string{"archive.tar.gz", "cw"} {
		if err := os.Link(filepath.Join(stage, name), filepath.Join(destination, name)); err != nil {
			return manageruntime.Candidate{}, err
		}
	}
	for _, name := range []string{destination, cache} {
		directory, err := os.Open(name)
		if err != nil {
			return manageruntime.Candidate{}, err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return manageruntime.Candidate{}, err
		}
	}
	complete = true
	return client.inspect(ctx, filepath.Join(destination, "cw"), expected, executable)
}
