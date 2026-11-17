package contextpack

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Engine owns private artifacts for one worktree, separately from the Git gate's store.
type Engine struct {
	repo *gitx.Repo
	dir  string
}

// Open creates no tracked files and accepts no linked cache directory.
func Open(repo *gitx.Repo) (*Engine, error) {
	if repo.Root == "" {
		return nil, errors.New("context requires a Git working tree")
	}
	dir := filepath.Join(repo.GitDir, "kitsu-context")
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("context cache must be a real private directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if err := root.Chmod(".", 0o700); err != nil {
		return nil, err
	}
	if info, err := root.Lstat("cache.lock"); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("unsafe context cache lock")
	}
	f, err := root.OpenFile("cache.lock", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &Engine{repo: repo, dir: dir}, nil
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Key hashes explicit effective dependencies, including external skill hashes.
func Key(value any) string { return digest(value) }

func artifactName(id string) (string, error) {
	if len(id) != 64 || strings.ToLower(id) != id {
		return "", errors.New("invalid artifact id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", errors.New("invalid artifact id")
	}
	return id + ".json", nil
}

// Save serializes writers and eviction. Artifacts larger than the limit are refused.
func (e *Engine) Save(ctx context.Context, a *Artifact) error {
	a.Version, a.Created = 1, time.Now().UTC()
	if a.ID == "" {
		a.ID = digest(struct {
			Nonce    string
			Artifact *Artifact
		}{rand.Text(), a})
	}
	name, err := artifactName(a.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(data) > MaxArtifact {
		return errors.New("context artifact exceeds 32 MiB; narrow the request")
	}
	lock, err := fslock.Acquire(ctx, filepath.Join(e.dir, "cache.lock"), fslock.Options{Wait: 30 * time.Second})
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	root, err := os.OpenRoot(e.dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := e.evict(root, int64(len(data)), name); err != nil {
		return err
	}
	tmp := ".artifact-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
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
	state, err := e.Snapshot(ctx)
	if err != nil || state != a.State {
		return fmt.Errorf("repository changed while preparing artifact: %v", err)
	}
	return root.Rename(tmp, name)
}

func (e *Engine) evict(root *os.Root, incoming int64, replacing string) error {
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		return err
	}
	type entry struct {
		name     string
		size     int64
		modified time.Time
	}
	var files []entry
	total := incoming
	for _, file := range entries {
		// Save holds cache.lock and has not created its own temporary yet.
		// A killed writer's temporary is therefore safe to remove now.
		if strings.HasPrefix(file.Name(), ".artifact-") {
			if err := root.Remove(file.Name()); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("context cache contains a nonregular artifact")
		}
		if file.Name() == replacing || time.Since(info.ModTime()) > Lifetime {
			if err := root.Remove(file.Name()); err != nil {
				return err
			}
			continue
		}
		total += info.Size()
		files = append(files, entry{file.Name(), info.Size(), info.ModTime()})
	}
	slices.SortFunc(files, func(a, b entry) int {
		if n := a.modified.Compare(b.modified); n != 0 {
			return n
		}
		return strings.Compare(a.name, b.name)
	})
	for _, file := range files {
		if total <= CacheBytes {
			break
		}
		if err := root.Remove(file.name); err != nil {
			return err
		}
		total -= file.size
	}
	return nil
}

// Load rejects expired, missing and stale artifacts rather than presenting them as current facts.
func (e *Engine) Load(ctx context.Context, id string) (Artifact, error) {
	var a Artifact
	name, err := artifactName(id)
	if err != nil {
		return a, err
	}
	root, err := os.OpenRoot(e.dir)
	if err != nil {
		return a, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return a, fmt.Errorf("artifact unavailable (possibly evicted): %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxArtifact {
		return a, errors.New("artifact is oversized or not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxArtifact+1))
	if err != nil || len(data) > MaxArtifact {
		return a, errors.New("artifact cannot be read within its size limit")
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return a, err
	}
	if a.ID != id || a.Version != 1 {
		return a, errors.New("invalid artifact metadata")
	}
	if time.Since(a.Created) > Lifetime || a.Created.After(time.Now().Add(time.Minute)) {
		return a, errors.New("artifact expired; collect current evidence again")
	}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return a, err
	}
	if a.State != state {
		return a, errors.New("artifact stale: repository content or HEAD changed")
	}
	return a, nil
}
