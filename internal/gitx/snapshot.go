package gitx

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
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
//
// The cleanup removes the copy and then tmp and its parent when they are
// left empty, so that a command which only reads leaves no directory behind.
// An empty file next to the copy keeps another process from removing the
// directory while git still works in it.
func (r *Repo) copyIndex(tmp string) (path string, cleanup func(), err error) {
	source := r.indexPath()
	path = filepath.Join(tmp, fmt.Sprintf("index-%d-%s", os.Getpid(), rand.Text()))
	hold := path + ".hold"
	if err := holdDir(tmp, hold); err != nil {
		return "", nil, err
	}
	cleanup = func() {
		_ = os.Remove(path)
		_ = os.Remove(path + ".lock")
		_ = os.Remove(hold)
		// Both fail, as they should, when the directory still holds something.
		_ = os.Remove(tmp)
		_ = os.Remove(filepath.Dir(tmp))
	}
	// One handle gives the content and the time of the same index, also when git replaces the file in between.
	file, err := os.Open(source)
	if errors.Is(err, fs.ErrNotExist) {
		return path, cleanup, nil // no index yet: git starts from an empty one
	}
	var info fs.FileInfo
	var data []byte
	if err == nil {
		defer func() { _ = file.Close() }()
		if info, err = file.Stat(); err == nil {
			data, err = io.ReadAll(file)
		}
	}
	if err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err == nil {
		err = os.Chtimes(path, info.ModTime(), info.ModTime())
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func (r *Repo) indexPath() string {
	source := os.Getenv("GIT_INDEX_FILE")
	if source == "" {
		return filepath.Join(r.GitDir, "index")
	}
	if !filepath.IsAbs(source) {
		return filepath.Join(r.dir, source)
	}
	return source
}

// holdDir creates the directory and an empty file in it, again when another process removed the
// directory in between: the file is then not found, or refused while Windows still removes it.
func holdDir(dir, file string) (err error) {
	for attempt := range 8 {
		time.Sleep(time.Duration(attempt) * time.Millisecond)
		if err = os.MkdirAll(dir, 0o700); err == nil {
			err = os.WriteFile(file, nil, 0o600)
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			return err
		}
	}
	return err
}

// indexEntry is one line of `git ls-files -s -v -z`.
type indexEntry struct {
	tag, mode, object, path string
}

func indexEntries(ctx context.Context, c call) ([]indexEntry, error) {
	out, err := run(ctx, c, "ls-files", "-s", "-v", "-z")
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

// checkEntries rejects marked files that are missing or changed and nested
// repositories with uncommitted changes. `git add -A` leaves marked entries
// untouched; restaging them in the copy compares Git's file modes, symlinks
// and filtered content without relying on hashes of dereferenced paths.
func (r *Repo) checkEntries(ctx context.Context, env []string) error {
	entries, err := indexEntries(ctx, call{dir: r.dir, env: env})
	if err != nil {
		return err
	}
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
		// update-index changes only one of these flags per invocation.
		for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
			if _, err := r.git(ctx, env, "update-index", "--no-"+mark, "--", entry.path); err != nil {
				return err
			}
		}
		if _, err := r.git(ctx, env, "-c", "core.sparseCheckout=false", "--literal-pathspecs", "add", "--", entry.path); err != nil {
			return err
		}
		out, err := r.git(ctx, env, "--literal-pathspecs", "ls-files", "-s", "-z", "--", entry.path)
		if err != nil {
			return err
		}
		if out != entry.mode+" "+entry.object+" 0\t"+entry.path+"\x00" {
			return &WorktreeError{Path: entry.path, Cause: "is marked " + entry.mark() + " and differs from the index"}
		}
	}
	return nil
}

// checkNested refuses a submodule or nested repository whose folder holds
// other content than its commit: uncommitted changes, a file that git is
// told to overlook, or a missing checkout. The snapshot
// records it as one commit, so a check would otherwise run on content that
// no tree records.
func (r *Repo) checkNested(ctx context.Context, path, full string) error {
	if _, err := os.Lstat(filepath.Join(full, ".git")); err != nil {
		return &WorktreeError{Path: path, Cause: "is a submodule that is not checked out. Initialize the submodule before recording evidence"}
	}
	nested := call{dir: full, foreign: true}
	out, err := run(ctx, nested, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return &WorktreeError{Path: path, Cause: "is a nested repository that git cannot read: " + err.Error()}
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return &WorktreeError{Path: path, Cause: "is a submodule or nested repository that holds uncommitted changes; commit or remove them there first"}
	}
	entries, err := indexEntries(ctx, nested)
	for _, entry := range entries {
		if entry.mode != "160000" {
			return &WorktreeError{Path: path + "/" + entry.path, Cause: "is marked " + entry.mark() + " in a submodule, so git does not say whether it differs; clear the mark there first"}
		} else if err := r.checkNested(ctx, path+"/"+entry.path, filepath.Join(full, filepath.FromSlash(entry.path))); err != nil {
			return err
		}
	}
	return err
}
