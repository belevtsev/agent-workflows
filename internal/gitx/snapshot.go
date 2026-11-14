package gitx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeError is a working tree that git cannot record as it is on disk.
type WorktreeError struct {
	Path  string
	Cause string
}

func (e *WorktreeError) Error() string {
	if e.Path == "" {
		return "the working tree cannot be recorded: " + e.Cause
	}
	return fmt.Sprintf("the working tree cannot be recorded: %s %s", e.Path, e.Cause)
}

// Snapshot is the tree that `git add -A` followed by `git commit` would
// record. It changes neither the index nor any file of the working tree: git
// works on a copy of the index under tmp, which must be inside the git
// directory.
func (r *Repo) Snapshot(ctx context.Context, tmp string) (string, error) {
	if r.Root == "" {
		return "", &WorktreeError{Cause: "the repository has no working tree"}
	}
	index, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return "", err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := r.git(ctx, env, "-c", "advice.addEmbeddedRepo=false", "add", "-A"); err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) {
			return "", &WorktreeError{Cause: "git add -A failed:\n" + strings.TrimSpace(gitErr.Stderr)}
		}
		return "", err
	}
	if err := r.checkEntries(ctx, env); err != nil {
		return "", err
	}
	return r.git(ctx, env, "write-tree")
}

// IndexTree is the tree of the index as it is, which is the tree a commit
// records inside a pre-commit hook. git works on a copy, because write-tree
// rewrites the index file.
func (r *Repo) IndexTree(ctx context.Context, tmp string) (string, error) {
	index, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return r.git(ctx, []string{"GIT_INDEX_FILE=" + index}, "write-tree")
}

// copyIndex copies the index git would use into tmp and gives the copy the
// modification time of the original. git re-reads a file whose time is not
// older than the index; with a newer time on the copy it would trust a file
// that was edited in the second it was staged.
func (r *Repo) copyIndex(tmp string) (path string, cleanup func(), err error) {
	source := os.Getenv("GIT_INDEX_FILE")
	if source == "" {
		source = filepath.Join(r.GitDir, "index")
	} else if !filepath.IsAbs(source) {
		source = filepath.Join(r.dir, source)
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", nil, err
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", nil, err
	}
	path = filepath.Join(tmp, fmt.Sprintf("index-%d-%s", os.Getpid(), hex.EncodeToString(suffix[:])))
	cleanup = func() {
		_ = os.Remove(path)
		_ = os.Remove(path + ".lock")
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, fs.ErrNotExist) {
		return path, cleanup, nil // no index yet: git starts from an empty one
	}
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// indexEntry is one line of `git ls-files -s -v -z`.
type indexEntry struct {
	tag, mode, object, path string
}

func (r *Repo) indexEntries(ctx context.Context, env []string) ([]indexEntry, error) {
	out, err := run(ctx, call{dir: r.dir, env: env}, "ls-files", "-s", "-v", "-z")
	if err != nil {
		return nil, err
	}
	var marked []indexEntry
	for len(out) > 0 {
		record, rest, _ := bytes.Cut(out, []byte{0})
		out = rest
		meta, path, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 4 {
			continue
		}
		entry := indexEntry{tag: fields[0], mode: fields[1], object: fields[2], path: string(path)}
		// Only entries that need a look are kept: marks and submodules.
		if entry.mark() != "" || entry.mode == "160000" {
			marked = append(marked, entry)
		}
	}
	return marked, nil
}

// mark names the flag that makes git overlook the file on disk.
func (e indexEntry) mark() string {
	switch {
	case e.tag == "S" || e.tag == "s":
		return "skip-worktree"
	case e.tag != "" && e.tag[0] >= 'a' && e.tag[0] <= 'z':
		return "assume-unchanged"
	}
	return ""
}

// checkEntries refuses a working tree whose content the index copy does not
// hold: a marked file that is absent or differs, and a submodule or nested
// repository with changes it has not committed. `git add -A` leaves marked
// entries as they were, so the copy still shows the marks.
func (r *Repo) checkEntries(ctx context.Context, env []string) error {
	entries, err := r.indexEntries(ctx, env)
	if err != nil {
		return err
	}
	var present []indexEntry
	for _, entry := range entries {
		full := filepath.Join(r.Root, filepath.FromSlash(entry.path))
		if entry.mode == "160000" {
			if err := r.checkNested(ctx, entry.path, full); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Lstat(full); err != nil {
			if r.SparseCheckout(ctx) {
				return &WorktreeError{Path: entry.path, Cause: "and other tracked files are missing from disk because of the sparse checkout"}
			}
			return &WorktreeError{Path: entry.path, Cause: "is marked " + entry.mark() + " and is missing from disk"}
		}
		present = append(present, entry)
	}
	return r.checkMarked(ctx, present)
}

// checkMarked compares marked files that are on disk with the index.
func (r *Repo) checkMarked(ctx context.Context, entries []indexEntry) error {
	for _, entry := range entries {
		out, err := r.git(ctx, nil, "hash-object", "--", entry.path)
		if err != nil {
			return err
		}
		if out != entry.object {
			return &WorktreeError{Path: entry.path, Cause: "is marked " + entry.mark() + " and differs from the index"}
		}
	}
	return nil
}

// checkNested refuses a submodule or nested repository whose own working
// tree holds changes. The snapshot records it as one commit, so a check
// would otherwise run on content that no tree records.
func (r *Repo) checkNested(ctx context.Context, path, full string) error {
	if _, err := os.Lstat(filepath.Join(full, ".git")); err != nil {
		return nil // not checked out: nothing on disk can differ
	}
	out, err := run(ctx, call{dir: full, foreign: true}, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return &WorktreeError{Path: path, Cause: "is a nested repository that git cannot read: " + err.Error()}
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return &WorktreeError{Path: path, Cause: "is a submodule or nested repository that holds uncommitted changes; commit or remove them there first"}
	}
	return nil
}
