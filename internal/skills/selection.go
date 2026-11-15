package skills

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

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

const smallSelections = "openspec/changes/.skills"

type selectedSkill struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"`
	SHA256 string `json:"sha256"`
}

type selection struct {
	Version    int             `json:"version"`
	Task       string          `json:"task"`
	Workflow   string          `json:"workflow"`
	Host       string          `json:"host"`
	Role       string          `json:"role"`
	LoadStatus string          `json:"load_status"`
	Skills     []selectedSkill `json:"skills"`
}

func validTask(task string) bool {
	if task == "" || len(task) > 128 {
		return false
	}
	for _, word := range strings.Split(task, "-") {
		if word == "" || strings.ContainsFunc(word, func(c rune) bool { return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') }) {
			return false
		}
	}
	return true
}

func selectionPaths(task string) (string, string) {
	return "openspec/changes/" + task + "/skills", smallSelections + "/" + task
}

// RecordSelection stores a portable selection receipt. It records no skill
// bodies, local absolute paths, or claim that the host loaded the instructions.
func RecordSelection(root, task, workflow, host, role string, entries []Entry) (string, error) {
	doc := selection{1, task, workflow, host, role, "unverified", []selectedSkill{}}
	for _, entry := range entries {
		if entry.Disabled || entry.ManualOnly {
			return "", fmt.Errorf("skill %s is disabled or manual-only", entry.ID)
		}
		doc.Skills = append(doc.Skills, selectedSkill{entry.ID, entry.Scope, entry.SHA256})
	}
	if err := doc.validate(); err != nil {
		return "", err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	lock, err := selectionLock(root, task)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Close() }()
	dir, err := selectionDestination(r, task, workflow)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	id := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := publishSelection(r, path.Join(dir, id+".json"), data); err != nil {
		return "", err
	}
	return id, nil
}

func (s selection) validate() error {
	roles := ",coordinator,lookup,spec-writer,architect,quality-engineer,developer,reviewer,git,"
	if s.Version != 1 || !validTask(s.Task) || (s.Workflow != "full" && s.Workflow != "small") ||
		(s.Host != "codex" && s.Host != "claude") || s.Role == "" || strings.Contains(s.Role, ",") ||
		!strings.Contains(roles, ","+s.Role+",") || s.LoadStatus != "unverified" || s.Skills == nil || len(s.Skills) > 64 {
		return errors.New("invalid skill selection version, task, workflow, host, role, or loading status")
	}
	seen := map[string]bool{}
	for _, skill := range s.Skills {
		if skill.ID == "" || len(skill.ID) > 1024 || strings.ContainsAny(skill.ID, "\r\n") || skill.Scope == "" ||
			!validDigest(skill.SHA256) || seen[skill.ID] {
			return errors.New("selected skills require unique ids, a scope, and a SHA256 fingerprint")
		}
		seen[skill.ID] = true
	}
	return nil
}

func validDigest(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 64 && strings.ToLower(id) == id && err == nil
}

// realDirectory checks every component before accessing a record directory.
// os.Root additionally prevents an exchanged symlink escaping the repository.
func realDirectory(r *os.Root, dir string, create bool) (bool, error) {
	component := ""
	for _, name := range strings.Split(dir, "/") {
		component = path.Join(component, name)
		info, err := r.Lstat(component)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err = r.Mkdir(component, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
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
			return false, fmt.Errorf("%s is not a real skill-record directory", component)
		}
	}
	return true, nil
}

func selectionDestination(r *os.Root, task, workflow string) (string, error) {
	full, small := selectionPaths(task)
	f, err := realDirectory(r, full, false)
	if err != nil {
		return "", err
	}
	s, err := realDirectory(r, small, false)
	if err != nil {
		return "", err
	}
	if f && s {
		return "", errors.New("task has competing full and small skill histories")
	}
	destination := small
	if workflow == "full" {
		if exists, err := realDirectory(r, path.Dir(full), false); err != nil || !exists {
			return "", fmt.Errorf("full skill selection requires an existing task folder: %s: %w", task, errors.Join(err, fs.ErrNotExist))
		}
		destination = full
		if s {
			if err := r.Rename(small, full); err != nil {
				return "", err
			}
		}
	} else if f {
		return "", errors.New("full skill history already exists; use --workflow full")
	}
	_, err = realDirectory(r, destination, true)
	return destination, err
}

func selectionLock(root, task string) (*fslock.Lock, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(canonical))
	dir := filepath.Join(os.TempDir(), "kitsu-skill-locks-"+hex.EncodeToString(digest[:16]))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return nil, fmt.Errorf("skill lock directory is not private and real: %s", dir)
	}
	return fslock.Acquire(context.Background(), filepath.Join(dir, task+".lock"), fslock.Options{Wait: 5 * time.Second})
}

func publishSelection(r *os.Root, name string, data []byte) error {
	if info, err := r.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular receipt file", name)
		}
		previous, err := readSelection(r, name)
		if err != nil || !bytes.Equal(previous, data) {
			return fmt.Errorf("existing receipt %s differs or cannot be read", name)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var random [8]byte
	_, _ = rand.Read(random[:])
	tmp := path.Join(path.Dir(name), ".tmp-"+hex.EncodeToString(random[:]))
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
	return r.Link(tmp, name)
}

func readSelection(r *os.Root, name string) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if len(data) > 1<<20 {
		return nil, errors.New("skill receipt exceeds 1 MiB")
	}
	return data, err
}
