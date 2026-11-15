package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/pelletier/go-toml/v2"
)

type uxSnapshot struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}

func uxReadFile(path string) (uxSnapshot, error) {
	file := uxSnapshot{path: path, mode: 0o600}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return file, fmt.Errorf("%s must be a regular file of at most 1 MiB, not a link", path)
	}
	file.data, err = os.ReadFile(path)
	file.mode, file.exists = info.Mode().Perm(), true
	return file, err
}

func uxReadPreference(path string) (string, uxSnapshot, error) {
	file, err := uxReadFile(path)
	if err != nil || !file.exists {
		return "", file, err
	}
	var fields map[string]any
	if err := toml.Unmarshal(file.data, &fields); err != nil {
		return "", file, fmt.Errorf("%s: invalid TOML: %w", path, err)
	}
	if version, ok := fields["version"].(int64); !ok || version != 1 {
		return "", file, fmt.Errorf("%s: version must be 1", path)
	}
	for key := range fields {
		if key != "version" && key != "host" {
			return "", file, fmt.Errorf("%s: unknown key %q; only version and host are allowed", path, key)
		}
	}
	if value, exists := fields["host"]; exists {
		host, ok := value.(string)
		if !ok || !uxValidHost(host) {
			return "", file, fmt.Errorf("%s: host must be codex or claude", path)
		}
		return host, file, nil
	}
	return "", file, nil
}

func uxSavePreference(ctx context.Context, before uxSnapshot, host string) error {
	if host != "" && !uxValidHost(host) {
		return fmt.Errorf("host must be codex or claude")
	}
	if !before.exists && host == "" {
		return nil
	}
	data := []byte("version = 1\n")
	if host != "" {
		data = fmt.Appendf(data, "host = %q\n", host)
	}
	return uxSaveFile(ctx, before, before.path+".lock", nil, data)
}

func uxSaveFile(ctx context.Context, before uxSnapshot, lockPath string, related []uxSnapshot, data []byte) error {
	release, err := uxLockSnapshots(ctx, lockPath, related)
	if err != nil {
		return err
	}
	defer release()
	for _, expected := range append(related, before) {
		current, err := uxReadFile(expected.path)
		if err != nil {
			return err
		}
		if expected.exists != current.exists || expected.mode != current.mode || !bytes.Equal(expected.data, current.data) {
			return fmt.Errorf("%s changed while editing; reopen the editor before saving", expected.path)
		}
	}
	if before.exists && bytes.Equal(before.data, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(before.path), 0o700); err != nil {
		return err
	}
	return store.WriteFile(before.path, data, before.mode)
}

func uxLockSnapshots(ctx context.Context, primary string, related []uxSnapshot) (func(), error) {
	paths := []string{primary}
	for _, file := range related {
		paths = append(paths, file.path+".lock")
	}
	slices.Sort(paths)
	var locks []*fslock.Lock
	release := func() {
		for _, lock := range locks {
			_ = lock.Close()
		}
	}
	for _, path := range slices.Compact(paths) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			release()
			return nil, err
		}
		info, err := os.Lstat(filepath.Dir(path))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			release()
			return nil, fmt.Errorf("%s must be a real configuration directory", filepath.Dir(path))
		}
		lock, err := fslock.Acquire(ctx, path, fslock.Options{Wait: 5 * time.Second})
		if err != nil {
			release()
			return nil, err
		}
		locks = append(locks, lock)
	}
	return release, nil
}
