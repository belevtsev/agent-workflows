package advisers

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

// JSON may encode each byte of a bounded prompt or answer as six bytes.
const maxRecordSize = 16 << 20

// recordFS keeps all record access rooted in the repository. Explicit checks
// reject links even when their target happens to remain inside the repository.
type recordFS struct{ *os.Root }

func openRecordFS(root string) (*recordFS, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &recordFS{r}, nil
}

// Mutations share a stable lock outside either movable history directory.
// The paid adviser call happens between Begin and Finish, outside this lock.
func openWritableRecords(root, task string) (*recordFS, func(), error) {
	if !validTask(task) {
		return nil, nil, fmt.Errorf("invalid task name %q", task)
	}
	r, err := openRecordFS(root)
	if err != nil {
		return nil, nil, err
	}
	lock, err := lockTask(root, task)
	if err != nil {
		_ = r.Close()
		return nil, nil, err
	}
	return r, func() {
		_ = lock.Close()
		_ = r.Close()
	}, nil
}

func lockTask(root, task string) (*fslock.Lock, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(canonical))
	directory := filepath.Join(os.TempDir(), "kitsu-adviser-locks-"+hex.EncodeToString(digest[:16]))
	if err = os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(directory)
	private := err == nil && (runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0)
	if !private || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("adviser lock directory must be a private real directory")
	}
	lockRoot, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lockRoot.Close() }()
	name := task + ".lock"
	f, err := lockRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		if err = f.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	return fslock.Acquire(context.Background(), filepath.Join(directory, name), fslock.Options{
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

func newRecordID() string {
	var random [8]byte
	_, _ = rand.Read(random[:]) // crypto/rand.Read cannot fail.
	return time.Now().UTC().Format("20060102t150405.000000000") + "-" + hex.EncodeToString(random[:])
}

func validRecordID(id string) bool {
	if id == "" || len(id) > 128 || id[0] == '.' || strings.HasSuffix(id, ".") || !filepath.IsLocal(id) {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func validTask(task string) bool {
	if task == "" || len(task) > 128 {
		return false
	}
	for _, word := range strings.Split(task, "-") {
		if word == "" {
			return false
		}
		for _, c := range word {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return false
			}
		}
	}
	return true
}

func (r *recordFS) directory(rel string, create bool) error {
	if rel == "." {
		return nil
	}
	if !fs.ValidPath(rel) {
		return fmt.Errorf("invalid record path %q", rel)
	}
	part := ""
	for _, name := range strings.Split(rel, "/") {
		part = path.Join(part, name)
		info, err := r.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err = r.Mkdir(part, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, err = r.Lstat(part)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("record path %s is not a real directory", part)
		}
	}
	return nil
}

func (r *recordFS) existsDir(rel string) (bool, error) {
	err := r.directory(rel, false)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (r *recordFS) read(rel string, out any) error {
	if err := r.directory(path.Dir(rel), false); err != nil {
		return err
	}
	info, err := r.Lstat(rel)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecordSize {
		return fmt.Errorf("%s is not a regular record of at most %d bytes", rel, maxRecordSize)
	}
	f, err := r.Open(rel)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return fmt.Errorf("record %s changed while opening it", rel)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRecordSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxRecordSize || !utf8.Valid(data) {
		return fmt.Errorf("record %s is too large", rel)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	var extra any
	if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s must hold exactly one JSON record", rel)
	}
	if err = requestKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return nil
}

// publish writes a complete, synced file before publishing it. A hard link
// reserves an immutable destination atomically; only the binding pointer may
// be replaced. A failed or racing publication cannot overwrite history.
func (r *recordFS) publish(rel string, value any, replace bool) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data) >= maxRecordSize {
		return fmt.Errorf("record %s is too large", rel)
	}
	if err = r.directory(path.Dir(rel), true); err != nil {
		return err
	}
	if info, statErr := r.Lstat(rel); statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("record %s is not a regular file", rel)
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	tmp := path.Join(path.Dir(rel), ".tmp-"+newRecordID())
	f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(tmp) }()
	_, err = f.Write(append(data, '\n'))
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
		if dir, openErr := r.Open(path.Dir(rel)); openErr == nil {
			_ = dir.Sync() // Directory sync is unavailable on some platforms.
			_ = dir.Close()
		}
	}
	return err
}

func (r *recordFS) entries(rel string) ([]fs.DirEntry, error) {
	if err := r.directory(rel, false); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return fs.ReadDir(r.FS(), rel)
}
