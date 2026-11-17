package taskbudget

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
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

const maxRecordSize = 64 << 10
const changes = "openspec/changes"
const fileName = "usage-budget.json"

type recordFS struct{ *os.Root }

func openFS(root, task string) (*recordFS, error) {
	if task != "" && !validTask(task) {
		return nil, fmt.Errorf("invalid task name %q", task)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &recordFS{r}, nil
}
func (r *recordFS) directory(rel string, create bool) error {
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
			return fmt.Errorf("budget path %s must be a real directory", part)
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
func (r *recordFS) read(rel string) ([]byte, error) {
	if err := r.directory(path.Dir(rel), false); err != nil {
		return nil, err
	}
	info, err := r.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecordSize {
		return nil, fmt.Errorf("%s must be a regular budget file of at most %d bytes", rel, maxRecordSize)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("budget file %s changed while opening", rel)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRecordSize+1))
	if err == nil && (len(data) > maxRecordSize || !utf8.Valid(data)) {
		err = errors.New("budget file is too large or invalid UTF-8")
	}
	return data, err
}
func locations(task string) (string, string) {
	return path.Join(changes, task, fileName), path.Join(changes, ".usage", task, fileName)
}
func (r *recordFS) load(task string) (*Record, string, error) {
	full, small := locations(task)
	fullData, fullErr := r.read(full)
	smallData, smallErr := r.read(small)
	if fullErr != nil && !errors.Is(fullErr, fs.ErrNotExist) {
		return nil, "", fullErr
	}
	if smallErr != nil && !errors.Is(smallErr, fs.ErrNotExist) {
		return nil, "", smallErr
	}
	if fullErr == nil && smallErr == nil {
		return nil, "", errors.New("competing full and small budget records; resolve explicitly without merging histories")
	}
	if fullErr != nil && smallErr != nil {
		return nil, "", nil
	}
	data, rel := fullData, full
	if fullErr != nil {
		data, rel = smallData, small
	}
	record, err := decode(data, task)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", rel, err)
	}
	fullExists, err := r.existsDir(path.Join(changes, task))
	if err != nil {
		return nil, "", err
	}
	record.Workflow = "small"
	if fullExists {
		record.Workflow = "full"
	}
	return record, rel, nil
}

// Load reads optional shared metadata without creating records or locks.
func Load(root, task string) (*Record, error) {
	r, err := openFS(root, task)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	record, _, err := r.load(task)
	return record, err
}

// ValidateRecords validates optional metadata independently of private logs or skills.
func ValidateRecords(root, task string) error { _, err := Load(root, task); return err }

func lockTask(root, task string) (*fslock.Lock, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(canonical))
	dir := filepath.Join(os.TempDir(), "kitsu-usage-budget-locks-"+hex.EncodeToString(digest[:16]))
	if err = os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("budget lock directory must be a private real directory")
	}
	return fslock.Acquire(context.Background(), filepath.Join(dir, task+".lock"), fslock.Options{Wait: 5 * time.Second})
}

// Set modifies only the supplied unit and preserves unspecified fields.
// A source records a user's declaration; it grants no execution permission.
func Set(root, task, workflow, unit string, update Entry) (*Record, error) {
	if !validTask(task) {
		return nil, fmt.Errorf("invalid task name %q", task)
	}
	if err := validateEntry(unit, update); err != nil {
		return nil, err
	}
	if workflow != "" && workflow != "full" && workflow != "small" {
		return nil, errors.New("workflow must be full or small")
	}
	lock, err := lockTask(root, task)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	r, err := openFS(root, task)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	record, oldPath, err := r.load(task)
	if err != nil {
		return nil, err
	}
	inferred, target, err := r.target(task, workflow)
	if err != nil {
		return nil, err
	}
	if record == nil {
		record = &Record{Version: 1, Task: task, Entries: map[string]Entry{}, raw: map[string][]byte{}}
	}
	record.Workflow = inferred
	previous := record.Entries[unit]
	if update.Estimate != nil {
		previous.Estimate = update.Estimate
	}
	if update.Limit != nil {
		previous.Limit = update.Limit
	}
	record.Entries[unit] = previous
	delete(record.raw, unit)
	if err = r.move(oldPath, target); err != nil {
		return nil, err
	}
	if err := r.write(target, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *recordFS) target(task, requested string) (string, string, error) {
	fullExists, err := r.existsDir(path.Join(changes, task))
	if err != nil {
		return "", "", err
	}
	workflow := "small"
	full, target := locations(task)
	if fullExists {
		workflow, target = "full", full
	}
	if requested != "" && requested != workflow {
		return "", "", fmt.Errorf("workflow %s conflicts with existing task structure (%s)", requested, workflow)
	}
	return workflow, target, nil
}

func (r *recordFS) move(oldPath, target string) error {
	if oldPath == "" || oldPath == target {
		return nil
	}
	if err := r.directory(path.Dir(target), true); err != nil {
		return err
	}
	return r.Rename(oldPath, target)
}

// Reset removes one unit, deleting only the owned budget file when it is empty.
func Reset(root, task, unit string) (*Record, error) {
	if !validTask(task) || !validUnit(unit) {
		return nil, errors.New("reset requires a valid task and unit (tokens or seconds)")
	}
	lock, err := lockTask(root, task)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	r, err := openFS(root, task)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	record, rel, err := r.load(task)
	if err != nil || record == nil {
		return record, err
	}
	if _, ok := record.Entries[unit]; !ok {
		return record, nil
	}
	delete(record.Entries, unit)
	delete(record.raw, unit)
	if len(record.Entries) == 0 {
		return nil, r.Remove(rel)
	}
	_, target, err := r.target(task, "")
	if err != nil {
		return nil, err
	}
	if err := r.move(rel, target); err != nil {
		return nil, err
	}
	return record, r.write(target, record)
}

func (r *recordFS) write(rel string, record *Record) error {
	var out bytes.Buffer
	task, _ := json.Marshal(record.Task)
	fmt.Fprintf(&out, "{\n  \"version\": 1,\n  \"task\": %s,\n  \"entries\": {", task)
	units := make([]string, 0, len(record.Entries))
	for unit := range record.Entries {
		units = append(units, unit)
	}
	slices.Sort(units)
	for i, unit := range units {
		if i > 0 {
			out.WriteByte(',')
		}
		data := record.raw[unit]
		if data == nil {
			var err error
			data, err = json.Marshal(record.Entries[unit])
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(&out, "\n    %q: ", unit)
		out.Write(data)
	}
	out.WriteString("\n  }\n}\n")
	if out.Len() > maxRecordSize {
		return errors.New("budget record is too large")
	}
	if err := r.directory(path.Dir(rel), true); err != nil {
		return err
	}
	if info, err := r.Lstat(rel); err == nil && !info.Mode().IsRegular() {
		return errors.New("budget destination must be a regular file")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var random [12]byte
	_, _ = rand.Read(random[:])
	tmp := path.Join(path.Dir(rel), ".usage-budget-"+hex.EncodeToString(random[:]))
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(tmp) }()
	_, err = f.Write(out.Bytes())
	if err == nil {
		err = f.Sync()
	}
	if closed := f.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	return r.Rename(tmp, rel)
}

// Tasks lists tasks with budget files, refusing malformed records and unsafe paths.
func Tasks(root string) ([]string, error) {
	r, err := openFS(root, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	found := map[string]bool{}
	for _, base := range []string{changes, path.Join(changes, ".usage")} {
		exists, err := r.existsDir(base)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		entries, err := fs.ReadDir(r.FS(), base)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !validTask(entry.Name()) {
				continue
			}
			rel := path.Join(base, entry.Name(), fileName)
			_, err := r.read(rel)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			found[entry.Name()] = true
		}
	}
	tasks := make([]string, 0, len(found))
	for task := range found {
		if _, _, err := r.load(task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	slices.Sort(tasks)
	return tasks, nil
}

// TaskExists reports a full task directory or optional small budget history.
func TaskExists(root, task string) (bool, error) {
	r, err := openFS(root, task)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()
	record, _, err := r.load(task)
	if err != nil {
		return false, err
	}
	if record != nil {
		return true, nil
	}
	return r.existsDir(path.Join(changes, task))
}
