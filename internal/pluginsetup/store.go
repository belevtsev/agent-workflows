package pluginsetup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func marketplaceStore() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return "", errors.New("cannot locate the user's Kitsu data directory")
		}
		base = filepath.Join(home, ".local", "share")
		switch runtime.GOOS {
		case "darwin":
			base = filepath.Join(home, "Library", "Application Support")
		case "windows":
			base = filepath.Join(home, "AppData", "Local")
		}
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	for _, part := range []string{"kitsu", "marketplaces"} {
		base = filepath.Join(base, part)
		if err := os.Mkdir(base, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if info, err := os.Lstat(base); err != nil || !info.IsDir() {
			return "", fmt.Errorf("%q must be a real Kitsu data directory", base)
		}
	}
	return filepath.EvalSymlinks(base)
}

func (r release) save(store string) (string, error) {
	dir := filepath.Join(store, r.digest)
	marketplace := filepath.Join(dir, marketplaceName)
	if info, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("%q must be a real marketplace directory", dir)
		}
		if err := r.verify(marketplace); err != nil {
			return "", fmt.Errorf("saved marketplace %q differs from %s: %w", marketplace, archiveName, err)
		}
		return marketplace, nil
	}
	stage, err := os.MkdirTemp(store, ".extract-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	stagedMarketplace := filepath.Join(stage, marketplaceName)
	if err := r.extract(stagedMarketplace); err != nil {
		return "", err
	}
	if err := r.verify(stagedMarketplace); err != nil {
		return "", err
	}
	if err := os.Rename(stage, dir); err != nil {
		return "", err
	}
	return marketplace, nil
}

func (r release) extract(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for name, entry := range r.members {
		local, err := filepath.Localize(name)
		if err != nil {
			return err
		}
		parent := local
		if !entry.dir {
			parent = filepath.Dir(local)
		}
		if err := root.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		if !entry.dir {
			if err := root.WriteFile(local, entry.data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r release) verify(dir string) error {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%q must be a real marketplace directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	files := root.FS()
	matched := 0
	err = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		want, ok := r.members[name]
		if !ok || (want.dir && !entry.IsDir()) || (!want.dir && !entry.Type().IsRegular()) {
			return fmt.Errorf("unexpected marketplace entry %q", name)
		}
		if !want.dir && !matchFile(files, name, want.data) {
			return fmt.Errorf("marketplace file %q differs from the archive", name)
		}
		matched++
		return nil
	})
	if err == nil && matched != len(r.members) {
		err = errors.New("marketplace files are missing")
	}
	return err
}

func matchFile(files fs.FS, name string, want []byte) bool {
	f, err := files.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(len(want))+1))
	return err == nil && bytes.Equal(data, want)
}
