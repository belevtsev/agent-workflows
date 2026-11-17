package verification

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

const maxHistorySize = 1 << 20
const maxHistoryEntries = 4096

// os.Root contains every operation; explicit component checks also reject
// symlinks whose destinations happen to be inside the repository.
type historyFS struct{ *os.Root }

func openHistory(root string) (*historyFS, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &historyFS{Root: r}, nil
}

func (r *historyFS) directory(rel string, create bool) (bool, error) {
	if !fs.ValidPath(rel) {
		return false, fmt.Errorf("invalid verification path %q", rel)
	}
	component := ""
	for _, name := range strings.Split(rel, "/") {
		component = path.Join(component, name)
		info, err := r.Lstat(component)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err := r.Mkdir(component, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return false, err
			}
			info, err = r.Lstat(component)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("verification path %s is not a real directory", component)
		}
	}
	return true, nil
}

func (r *historyFS) entries(rel string) ([]fs.DirEntry, error) {
	exists, err := r.directory(rel, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("verification directory %s is missing", rel)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(maxHistoryEntries + 1)
	if len(entries) > maxHistoryEntries {
		return nil, errors.New("verification directory contains too many entries")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return entries, nil
}

func (r *historyFS) read(rel string, out any) ([]byte, error) {
	if exists, err := r.directory(path.Dir(rel), false); err != nil || !exists {
		return nil, fmt.Errorf("verification record parent missing or unsafe: %w", errors.Join(err, fs.ErrNotExist))
	}
	info, err := r.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxHistorySize {
		return nil, fmt.Errorf("verification record %s must be a regular file of at most 1 MiB", rel)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("verification record %s changed while opening", rel)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxHistorySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxHistorySize || !utf8.Valid(data) {
		return nil, errors.New("verification record is oversized or invalid UTF-8")
	}
	if err := strictHistoryJSON(data, out); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	return data, nil
}

func historyJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) >= maxHistorySize {
		return nil, errors.New("verification record exceeds 1 MiB")
	}
	return append(data, '\n'), nil
}

func strictHistoryJSON(data []byte, out any) error {
	if err := historyJSONKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("verification file must hold exactly one JSON record")
	}
	canonical, err := historyJSON(out)
	if err != nil {
		return err
	}
	// Requiring complete canonical records catches missing zero-valued fields,
	// null scalars, and noncanonical spellings as well as missing arrays.
	if !bytes.Equal(data, canonical) {
		return errors.New("verification record is not complete canonical JSON")
	}
	return nil
}

func historyJSONKeys(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return errors.New("verification JSON is nested too deeply")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] || name != strings.ToLower(name) {
				return errors.New("verification JSON contains duplicate or non-lowercase fields")
			}
			seen[name] = true
		}
		if err := historyJSONKeys(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (r *historyFS) write(rel string, data []byte, replace bool) error {
	if _, err := r.directory(path.Dir(rel), true); err != nil {
		return err
	}
	info, err := r.Lstat(rel)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("verification record %s is not a regular file", rel)
		}
		if !replace {
			var existing Attempt
			previous, err := r.read(rel, &existing)
			if err != nil || !bytes.Equal(previous, data) {
				return fmt.Errorf("immutable verification record %s differs or cannot be read", rel)
			}
			return nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	tmp := path.Join(path.Dir(rel), ".tmp-"+hex.EncodeToString(nonce[:]))
	f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(tmp) }()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if replace {
		err = r.Rename(tmp, rel)
	} else {
		err = r.Link(tmp, rel)
	}
	if err == nil {
		if directory, openErr := r.Open(path.Dir(rel)); openErr == nil {
			_ = directory.Sync() // Directory sync is unavailable on some platforms.
			_ = directory.Close()
		}
	}
	return err
}

func historyLock(root, task string) (*fslock.Lock, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(os.TempDir(), "kitsu-verification-locks-"+historyDigest([]byte(canonical))[:32])
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("verification lock directory must be real")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("verification lock directory must be private")
	}
	lockRoot, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lockRoot.Close() }()
	name := task + ".lock"
	f, err := lockRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	return fslock.Acquire(context.Background(), filepath.Join(dir, name), fslock.Options{
		Wait: 5 * time.Second, NoCreate: true,
		Check: func(file *os.File) error {
			named, err := lockRoot.Lstat(name)
			if err != nil {
				return err
			}
			held, err := file.Stat()
			if err != nil || !named.Mode().IsRegular() || !os.SameFile(named, held) {
				return fslock.ErrReplaced
			}
			return nil
		},
	})
}
