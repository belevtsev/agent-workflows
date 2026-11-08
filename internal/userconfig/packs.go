package userconfig

import (
	"crypto/rand"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"golang.org/x/sys/unix"
)

const (
	packsName     = "packs.json"
	maxPacksBytes = 64 << 10
)

// packSelection is the shape of packs.json. A nil Version is an absent or
// null version; a nil Packs lists no packs.
type packSelection struct {
	Version *int     `json:"version"`
	Packs   []string `json:"packs"`
}

var errPrivateDirectory = errors.New("private configuration directory must be owned by the current user with mode 0700")

// SelectedPacks reads <dir>/packs.json, the user's pack selection. found is
// false when the file is absent; an existing file, even one listing no packs,
// is an explicit selection. ids are sorted, or nil when none are listed.
func SelectedPacks(dir string) (ids []string, found bool, err error) {
	if !safeAbsolute(dir) {
		return nil, false, errors.New("private configuration requires an absolute clean path")
	}
	name := filepath.Join(dir, packsName)
	if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("unsafe directory: %s: %w", dir, err)
	}
	if err := privateDirectory(dir); err != nil {
		return nil, false, err
	}
	data, err := readPrivateFile(name)
	if err != nil {
		return nil, false, err
	}
	ids, err = decodePacks(data)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

// readPrivateFile reads name without following a final symbolic link, after
// checking the open file is a current-user regular file with mode 0600 of at
// most maxPacksBytes.
func readPrivateFile(name string) ([]byte, error) {
	refused := errors.New("pack selection must be a current-user regular file with mode 0600")
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, refused
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCaller(info) || info.Size() > maxPacksBytes {
		return nil, refused
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPacksBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPacksBytes {
		return nil, refused
	}
	return data, nil
}

func decodePacks(data []byte) ([]string, error) {
	var selection packSelection
	if err := json.Unmarshal(data, &selection, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid pack selection: %w", err)
	}
	if selection.Version == nil {
		return nil, errors.New("pack selection requires version 1")
	}
	if *selection.Version != 1 {
		return nil, fmt.Errorf("pack selection version %d is not supported; this cw reads version 1", *selection.Version)
	}
	for i, id := range selection.Packs {
		if !langpack.ValidID(id) {
			return nil, fmt.Errorf("invalid pack id %q in pack selection", id)
		}
		if slices.Contains(selection.Packs[:i], id) {
			return nil, fmt.Errorf("pack %q is listed twice in pack selection", id)
		}
	}
	if len(selection.Packs) == 0 {
		return nil, nil
	}
	return slices.Sorted(slices.Values(selection.Packs)), nil
}

// SavePacks atomically writes ids, deduplicated and sorted, as
// {"version":1,"packs":[...]} to <dir>/packs.json with mode 0600. On any
// error the previous file is unchanged.
func SavePacks(dir string, ids []string) error {
	for _, id := range ids {
		if !langpack.ValidID(id) {
			return fmt.Errorf("invalid pack id %q", id)
		}
	}
	if !safeAbsolute(dir) {
		return errors.New("private configuration requires an absolute clean path")
	}
	version := 1
	packs := slices.Compact(slices.Sorted(slices.Values(ids)))
	if packs == nil {
		packs = []string{}
	}
	encoded, err := json.Marshal(packSelection{Version: &version, Packs: packs}, json.Deterministic(true))
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	directory, err := openPrivateDirectory(dir)
	if err != nil {
		return err
	}
	defer unix.Close(directory)
	temporary, err := createTemporary(directory)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(directory, temporary.Name(), 0)
	_, writeErr := temporary.Write(encoded)
	syncErr := temporary.Sync()
	if err := errors.Join(writeErr, syncErr, temporary.Close()); err != nil {
		return fmt.Errorf("write the pack selection: %w", err)
	}
	if err := unix.Renameat(directory, temporary.Name(), directory, packsName); err != nil {
		return fmt.Errorf("replace %s: %w", packsName, err)
	}
	if err := unix.Fsync(directory); err != nil {
		return fmt.Errorf("pack selection was replaced but its directory durability is uncertain: %w", err)
	}
	return nil
}

// openPrivateDirectory walks dir from / without following symbolic links,
// creating each missing component with mode 0700, and returns a descriptor
// for dir once it is a current-user directory with mode 0700.
func openPrivateDirectory(dir string) (int, error) {
	const flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	descriptor, err := unix.Open("/", flags, 0)
	if err != nil {
		return -1, fmt.Errorf("open /: %w", err)
	}
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(dir, "/"), "/") {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		next, err := unix.Openat(descriptor, component, flags, 0)
		if errors.Is(err, unix.ENOENT) {
			if err := unix.Mkdirat(descriptor, component, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(descriptor)
				return -1, fmt.Errorf("create %s: %w", current, err)
			}
			if err := unix.Fsync(descriptor); err != nil {
				unix.Close(descriptor)
				return -1, fmt.Errorf("sync the parent of %s: %w", current, err)
			}
			next, err = unix.Openat(descriptor, component, flags, 0)
		}
		unix.Close(descriptor)
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return -1, fmt.Errorf("unsafe directory: %s", current)
		}
		if err != nil {
			return -1, fmt.Errorf("open %s: %w", current, err)
		}
		descriptor = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o7777 != 0o700 || int(stat.Uid) != os.Geteuid() {
		unix.Close(descriptor)
		return -1, errPrivateDirectory
	}
	return descriptor, nil
}

// createTemporary creates a new .packs-<random> file with mode 0600 in the
// directory.
func createTemporary(directory int) (*os.File, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	name := ".packs-" + hex.EncodeToString(random[:])
	descriptor, err := unix.Openat(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create a temporary pack selection: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		unix.Unlinkat(directory, name, 0)
		return nil, err
	}
	return file, nil
}
