package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type document struct {
	Version *int              `json:"version"`
	Keys    map[string]string `json:"keys"`
}

func missingSnapshot() Snapshot { return Snapshot{Keys: map[string]string{}} }

func generation(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeDirectoryStat(stat *unix.Stat_t) bool {
	return uint32(stat.Mode)&unix.S_IFMT == unix.S_IFDIR && uint32(stat.Mode)&0o7777 == 0o700 && int(stat.Uid) == os.Getuid()
}

func safeFileStat(stat *unix.Stat_t) bool {
	return uint32(stat.Mode)&unix.S_IFMT == unix.S_IFREG && uint32(stat.Mode)&0o7777 == 0o600 && int(stat.Uid) == os.Getuid() && stat.Nlink == 1
}

func sameInode(first, second *unix.Stat_t) bool {
	return first.Dev == second.Dev && first.Ino == second.Ino
}

// openDirectory walks from a physical filesystem root using only nofollow
// directory descriptors. A missing path is a read-only absence, not a default
// selected from cwd or an instruction to create state during Read.
func (store Store) openDirectory(create bool) (*os.File, bool, error) {
	if !filepath.IsAbs(store.Directory) || filepath.Clean(store.Directory) != store.Directory || strings.ContainsAny(store.Directory, "\x00\r\n") {
		return nil, false, ErrUnsafeStore
	}
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, ErrRead
	}
	keep := false
	defer func() {
		if !keep {
			unix.Close(descriptor)
		}
	}()
	for _, component := range strings.Split(strings.TrimPrefix(store.Directory, "/"), "/") {
		if component == "" {
			continue
		}
		next, err := unix.Openat(descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) && create {
			if err := unix.Mkdirat(descriptor, component, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				return nil, false, ErrSave
			}
			if err := unix.Fsync(descriptor); err != nil {
				return nil, false, ErrSave
			}
			next, err = unix.Openat(descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if errors.Is(err, unix.ENOENT) && !create {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, ErrUnsafeStore
		}
		unix.Close(descriptor)
		descriptor = next
	}
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || !safeDirectoryStat(&stat) {
		return nil, false, ErrUnsafeStore
	}
	keep = true
	return os.NewFile(uintptr(descriptor), "credential-directory"), false, nil
}

func readAt(directory *os.File) (Snapshot, error) {
	descriptor, err := unix.Openat(int(directory.Fd()), documentName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return missingSnapshot(), nil
	}
	if err != nil {
		return Snapshot{}, ErrUnsafeStore
	}
	file := os.NewFile(uintptr(descriptor), "credential-document")
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || !safeFileStat(&stat) {
		return Snapshot{}, ErrUnsafeStore
	}
	if stat.Size <= 0 || stat.Size > maxDocumentBytes {
		return Snapshot{}, ErrInvalidStore
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return Snapshot{}, ErrRead
	}
	if len(data) > maxDocumentBytes || int64(len(data)) != stat.Size {
		return Snapshot{}, ErrInvalidStore
	}
	var contents document
	if err := json.Unmarshal(data, &contents, json.RejectUnknownMembers(true)); err != nil || contents.Version == nil || *contents.Version != 1 || contents.Keys == nil {
		return Snapshot{}, ErrInvalidStore
	}
	for name, value := range contents.Keys {
		if !knownName(name) || !validValue(value) {
			return Snapshot{}, ErrInvalidStore
		}
	}
	return Snapshot{Keys: contents.Keys, Generation: generation(data)}, nil
}

// Read performs no directory, lock, temporary-file or environment mutations.
func (store Store) Read() (Snapshot, error) {
	directory, missing, err := store.openDirectory(false)
	if err != nil {
		return Snapshot{}, err
	}
	if missing {
		return missingSnapshot(), nil
	}
	defer directory.Close()
	return readAt(directory)
}

func verifyLock(directory, lock *os.File) error {
	var held, named unix.Stat_t
	if unix.Fstat(int(lock.Fd()), &held) != nil || !safeFileStat(&held) || unix.Fstatat(int(directory.Fd()), lockName, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !safeFileStat(&named) || !sameInode(&held, &named) {
		return ErrUnsafeStore
	}
	return nil
}

func lockDirectory(ctx context.Context, directory *os.File) (*os.File, error) {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	descriptor := -1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-deadline.C:
			return nil, ErrBusy
		default:
		}
		flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		var err error
		descriptor, err = unix.Openat(int(directory.Fd()), lockName, flags, 0)
		if errors.Is(err, unix.ENOENT) {
			descriptor, err = unix.Openat(int(directory.Fd()), lockName, flags|unix.O_CREAT|unix.O_EXCL, 0600)
		}
		if err == nil {
			break
		}
		// APFS can return ENOENT to simultaneous nonexclusive O_CREAT opens. An
		// exclusive creator arbitrates absence; every contender then opens the one
		// existing inode. Never replace or unlink the stable lock.
		if !errors.Is(err, unix.EEXIST) && !errors.Is(err, unix.EINTR) {
			return nil, ErrUnsafeStore
		}
		pause := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			pause.Stop()
			return nil, ctx.Err()
		case <-deadline.C:
			pause.Stop()
			return nil, ErrBusy
		case <-pause.C:
		}
	}
	lock := os.NewFile(uintptr(descriptor), "credential-lock")
	acquired := false
	defer func() {
		if !acquired {
			lock.Close()
		}
	}()
	if err := verifyLock(directory, lock); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if err := verifyLock(directory, lock); err != nil {
				return nil, err
			}
			acquired = true
			return lock, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return nil, ErrUnsafeStore
		}
		pause := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			pause.Stop()
			return nil, ctx.Err()
		case <-deadline.C:
			pause.Stop()
			return nil, ErrBusy
		case <-pause.C:
		}
	}
}

func (store Store) verifyAnchor(directory *os.File) error {
	current, missing, err := store.openDirectory(false)
	if err != nil || missing {
		return ErrUnsafeStore
	}
	defer current.Close()
	var anchored, named unix.Stat_t
	if unix.Fstat(int(directory.Fd()), &anchored) != nil || !safeDirectoryStat(&anchored) || unix.Fstat(int(current.Fd()), &named) != nil || !sameInode(&anchored, &named) {
		return ErrUnsafeStore
	}
	return nil
}

func applyChanges(snapshot Snapshot, changes map[string]*string) (map[string]string, bool) {
	updated := maps.Clone(snapshot.Keys)
	for name, value := range changes {
		if value == nil {
			delete(updated, name)
		} else {
			updated[name] = *value
		}
	}
	return updated, !maps.Equal(updated, snapshot.Keys)
}

func temporaryAt(directory *os.File) (*os.File, string, error) {
	for range 4 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", ErrSave
		}
		name := ".credentials-" + hex.EncodeToString(random[:])
		descriptor, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", ErrSave
		}
		file := os.NewFile(uintptr(descriptor), "credential-temporary")
		if err := file.Chmod(0o600); err != nil {
			file.Close()
			unix.Unlinkat(int(directory.Fd()), name, 0)
			return nil, "", ErrSave
		}
		return file, name, nil
	}
	return nil, "", ErrSave
}

// Save applies only explicit changes. A nil value removes one saved key; a nil
// expected generation is unchecked, while an empty generation requires absence.
// The stable lock and the final generation recheck protect concurrent writers.
func (store Store) Save(ctx context.Context, changes map[string]*string, expectedGeneration *string) (SaveResult, error) {
	for name, value := range changes {
		if !knownName(name) || value != nil && !validValue(*value) {
			return SaveResult{}, ErrInvalidChanges
		}
	}
	if err := ctx.Err(); err != nil {
		return SaveResult{}, err
	}
	directory, _, err := store.openDirectory(true)
	if err != nil {
		return SaveResult{}, err
	}
	defer directory.Close()
	lock, err := lockDirectory(ctx, directory)
	if err != nil {
		return SaveResult{}, err
	}
	defer lock.Close()
	if err := store.verifyAnchor(directory); err != nil {
		return SaveResult{}, err
	}
	current, err := readAt(directory)
	if err != nil {
		return SaveResult{}, err
	}
	if expectedGeneration != nil && current.Generation != *expectedGeneration {
		return SaveResult{}, ErrConflict
	}
	updated, changed := applyChanges(current, changes)
	if err := ctx.Err(); err != nil {
		return SaveResult{}, err
	}
	if !changed {
		return SaveResult{DurabilityVerified: true}, nil
	}
	result := SaveResult{Changed: true}
	version := 1
	encoded, err := json.Marshal(document{Version: &version, Keys: updated}, json.Deterministic(true))
	if err != nil || len(encoded) > maxDocumentBytes {
		return result, ErrSave
	}
	encoded = append(encoded, '\n')
	file, temporary, err := temporaryAt(directory)
	if err != nil {
		return result, err
	}
	defer unix.Unlinkat(int(directory.Fd()), temporary, 0)
	written, writeErr := file.Write(encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if written != len(encoded) || writeErr != nil || syncErr != nil || closeErr != nil {
		return result, ErrSave
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := store.verifyAnchor(directory); err != nil {
		return result, err
	}
	if err := verifyLock(directory, lock); err != nil {
		return result, err
	}
	latest, err := readAt(directory)
	if err != nil {
		return result, err
	}
	if latest.Generation != current.Generation {
		return result, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := unix.Renameat(int(directory.Fd()), temporary, int(directory.Fd()), documentName); err != nil {
		return result, ErrSave
	}
	result.Committed = true
	syncDirectory := store.SyncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	if err := syncDirectory(directory); err != nil {
		return result, ErrPersistenceUnconfirmed
	}
	if err := store.verifyAnchor(directory); err != nil {
		return result, ErrPersistenceUnconfirmed
	}
	result.DurabilityVerified = true
	return result, nil
}
