// Package gitx holds every call kitsu makes to git. Nothing else starts git.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// ErrNotRepo is returned when the directory is not inside a git repository.
var ErrNotRepo = errors.New("not a git repository")

// Repo is one working tree of a git repository.
type Repo struct {
	// Root is the top of the working tree. It is empty in a bare repository.
	Root string
	// GitDir is the git directory of this working tree.
	GitDir string
	// CommonDir is the git directory that all linked working trees share.
	CommonDir string
	// dir is where git runs: Root, or GitDir in a bare repository.
	dir string
}

// Error is a git command that did not exit 0.
type Error struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *Error) Error() string {
	text := strings.TrimSpace(e.Stderr)
	if text == "" {
		text = fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("git %s: %s", e.Args[0], text)
}

// localVars are the variables git sets for one repository. A git command
// that runs inside a submodule must not inherit them.
var localVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_CEILING_DIRECTORIES",
	"GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
	"GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE", "GIT_INTERNAL_SUPER_PREFIX",
}

// environment is the caller's environment plus extra, with git told to take
// no lock it does not need, so that reading never rewrites the index, and to
// fetch no object a partial clone lacks, so that kitsu starts no connection.
func environment(foreign bool, extra ...string) []string {
	env := os.Environ()
	if foreign {
		kept := env[:0:0]
		for _, kv := range env {
			if name, _, _ := strings.Cut(kv, "="); !slices.Contains(localVars, name) {
				kept = append(kept, kv)
			}
		}
		env = kept
	}
	env = append(env, "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1")
	return append(env, extra...)
}

// call is one git command.
type call struct {
	dir     string
	env     []string
	stdout  io.Writer // nil keeps the output for the caller
	foreign bool      // runs in another repository, such as a submodule
}

// run starts git without its file system monitor: a daemon that git started
// for kitsu would end with the call, and the next call would start it again.
func run(ctx context.Context, c call, args ...string) ([]byte, error) {
	result := proc.Run(ctx, proc.Spec{
		Path: "git", Args: append([]string{"-c", "core.fsmonitor=false"}, args...), Dir: c.dir, Env: environment(c.foreign, c.env...),
		Stdout: c.stdout, StdoutLimit: 1 << 30, StderrLimit: 1 << 16,
	})
	if result.Stopped {
		return nil, ctx.Err()
	}
	if result.Overflow {
		return nil, fmt.Errorf("git %s printed more than kitsu reads", args[0])
	}
	if result.Code != 0 {
		stderr := string(result.Stderr)
		if result.Code < 0 && result.Err != nil {
			stderr = result.Err.Error()
		}
		return result.Stdout, &Error{Args: args, Code: result.Code, Stderr: stderr}
	}
	return result.Stdout, nil
}

func (r *Repo) git(ctx context.Context, env []string, args ...string) (string, error) {
	out, err := run(ctx, call{dir: r.dir, env: env}, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// Open finds the repository that holds dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	paths, err := repositoryPaths(ctx, abs)
	if err != nil {
		return nil, err
	}
	repo := &Repo{GitDir: filepath.Clean(paths[0]), CommonDir: paths[1], dir: filepath.Clean(paths[0])}
	if !filepath.IsAbs(repo.CommonDir) {
		repo.CommonDir = filepath.Join(abs, repo.CommonDir)
	}
	repo.CommonDir = filepath.Clean(repo.CommonDir)
	if len(paths) == 3 {
		repo.Root = filepath.FromSlash(paths[2])
		repo.dir = repo.Root
	}
	return repo, nil
}

func repositoryPaths(ctx context.Context, dir string) ([]string, error) {
	fields := []string{"--absolute-git-dir", "--git-common-dir", "--show-toplevel"}
	c := call{dir: dir, env: []string{"LC_ALL=C"}}
	out, err := run(ctx, c, append([]string{"rev-parse"}, fields...)...)
	if err != nil {
		// Git uses exit 128 for both ordinary discovery misses and broken repositories.
		var gitErr *Error
		if !errors.As(err, &gitErr) || gitErr.Code != 128 {
			return nil, err
		}
		if strings.HasPrefix(gitErr.Stderr, "fatal: not a git repository (or any ") {
			return nil, fmt.Errorf("%w: %s", ErrNotRepo, dir)
		}
		if strings.TrimSpace(gitErr.Stderr) != "fatal: this operation must be run in a work tree" {
			return nil, err
		}
		fields = fields[:2] // Git printed the Git and common directories before the failing option.
	}
	paths := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(paths) > len(fields) {
		// rev-parse emits raw path bytes. A path contains a newline; get each
		// field separately so an embedded newline can never select another checkout.
		paths = make([]string, len(fields))
		for i, field := range fields {
			data, err := run(ctx, c, "rev-parse", field)
			if err != nil {
				return nil, err
			}
			paths[i] = strings.TrimSuffix(string(data), "\n")
		}
	}
	if len(paths) != len(fields) || slices.Contains(paths, "") {
		return nil, errors.New("git rev-parse returned incomplete repository paths")
	}
	return paths, nil
}

// TreeOf is the tree a revision names, and false when it names none.
func (r *Repo) TreeOf(ctx context.Context, rev string) (string, bool, error) {
	return r.resolve(ctx, rev+"^{tree}")
}

// CommitOf peels a revision to a commit, and false when it names none.
func (r *Repo) CommitOf(ctx context.Context, rev string) (string, bool, error) {
	return r.resolve(ctx, rev+"^{commit}")
}

// resolve reads exit status 1 as "names nothing"; any other failure of git,
// such as an object it cannot read, is an error.
func (r *Repo) resolve(ctx context.Context, spec string) (string, bool, error) {
	out, err := r.git(ctx, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", spec)
	var gitErr *Error
	if errors.As(err, &gitErr) && gitErr.Code == 1 {
		return "", false, nil
	}
	return out, err == nil && out != "", err
}

// Object is the id of the entry at a path of a tree, and false when the
// tree has no such entry.
func (r *Repo) Object(ctx context.Context, tree, path string) (string, bool, error) {
	return r.resolve(ctx, tree+":"+path)
}

// LastCommit is the newest commit in the history of HEAD that changed a
// path, and false when there is none.
func (r *Repo) LastCommit(ctx context.Context, path string) (string, bool, error) {
	out, err := r.git(ctx, nil, "log", "-1", "--format=%H", "--end-of-options", "HEAD", "--", path)
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return "", false, nil
	}
	return out, err == nil && out != "", err
}

// Contains says whether commit descendant holds commit ancestor in its
// history. A commit this repository does not have is not contained.
func (r *Repo) Contains(ctx context.Context, ancestor, descendant string) (bool, error) {
	_, err := r.git(ctx, nil, "merge-base", "--is-ancestor", ancestor, descendant)
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return false, nil
	}
	return err == nil, err
}

// RemoteHolds says whether a remote-tracking branch of the remote contains
// the commit.
func (r *Repo) RemoteHolds(ctx context.Context, remote, commit string) (bool, error) {
	if remote == "" || strings.ContainsAny(remote, "*?[\\") {
		return false, nil
	}
	out, err := r.git(ctx, nil, "for-each-ref", "--count=1", "--format=%(refname)", "--contains", commit, "refs/remotes/"+remote+"/")
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return false, nil
	}
	return out != "", err
}

// HooksDir is the directory whose hooks git runs for this working tree. It
// follows core.hooksPath. configured says whether that setting is in force.
func (r *Repo) HooksDir(ctx context.Context) (dir string, configured bool, err error) {
	_, cfgErr := r.git(ctx, nil, "config", "--get", "core.hooksPath")
	var gitErr *Error
	if cfgErr != nil && !errors.As(cfgErr, &gitErr) {
		return "", false, cfgErr
	}
	out, err := r.git(ctx, nil, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", false, err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(r.dir, out)
	}
	return filepath.Clean(out), cfgErr == nil, nil
}

// SparseCheckout says whether the working tree is a sparse checkout.
func (r *Repo) SparseCheckout(ctx context.Context) bool {
	out, err := r.git(ctx, nil, "config", "--bool", "--get", "core.sparseCheckout")
	return err == nil && out == "true"
}

// Declaration returns the bytes of the regular file at path in the root of
// a tree. found is false when the tree has no such entry. An entry that is
// not a regular file is an error.
func (r *Repo) Declaration(ctx context.Context, tree, path string) (data []byte, found bool, err error) {
	out, err := run(ctx, call{dir: r.dir}, "ls-tree", "-z", tree, "--", path)
	if err != nil {
		return nil, false, err
	}
	entry, _, ok := bytes.Cut(out, []byte{0})
	if !ok || len(entry) == 0 {
		return nil, false, nil
	}
	meta, _, _ := bytes.Cut(entry, []byte{'\t'})
	fields := strings.Fields(string(meta))
	if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return nil, true, fmt.Errorf("%s is not a regular file in the tree", path)
	}
	data, err = run(ctx, call{dir: r.dir}, "cat-file", "blob", fields[2])
	return data, true, err
}

// Entries calls visit for every entry of a tree, in git's order. raw is the
// entry as git prints it, ended by a NUL byte; it is valid only during the
// call.
func (r *Repo) Entries(ctx context.Context, tree string, visit func(raw []byte, path string)) error {
	splitter := &nulSplitter{emit: func(record []byte) {
		if _, path, ok := bytes.Cut(record[:len(record)-1], []byte{'\t'}); ok {
			visit(record, string(path))
		}
	}}
	_, err := run(ctx, call{dir: r.dir, stdout: splitter}, "ls-tree", "-r", "-z", tree)
	return err
}

// nulSplitter cuts a stream into records that each end with a NUL byte.
type nulSplitter struct {
	rest []byte
	emit func(record []byte)
}

func (s *nulSplitter) Write(p []byte) (int, error) {
	n := len(p)
	for {
		i := bytes.IndexByte(p, 0)
		if i < 0 {
			s.rest = append(s.rest, p...)
			return n, nil
		}
		if len(s.rest) > 0 {
			s.rest = append(s.rest, p[:i+1]...)
			s.emit(s.rest)
			s.rest = s.rest[:0]
		} else {
			s.emit(p[:i+1])
		}
		p = p[i+1:]
	}
}

// Change is one path that differs between two trees.
type Change struct {
	Path string
	Kind string // "added", "changed" or "removed"
}

// MaxPaths is how many differing paths a message and a record list.
const MaxPaths = 20

// DiffPaths lists the first MaxPaths paths that differ between two trees,
// and says how many more differ.
func (r *Repo) DiffPaths(ctx context.Context, from, to string) (paths []string, more int, err error) {
	changes, err := r.Diff(ctx, from, to)
	for i, change := range changes {
		if i == MaxPaths {
			return paths, len(changes) - MaxPaths, err
		}
		paths = append(paths, change.Path)
	}
	return paths, 0, err
}

// Diff lists the paths that differ between two trees, without rename or copy
// detection: a moved file differs at its old path and at its new one.
func (r *Repo) Diff(ctx context.Context, from, to string) ([]Change, error) {
	out, err := run(ctx, call{dir: r.dir}, "diff-tree", "-r", "--no-renames", "--name-status", "-z", from, to)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var changes []Change
	for i := 0; i+1 < len(fields); i += 2 {
		kind := "changed"
		switch fields[i] {
		case "A":
			kind = "added"
		case "D":
			kind = "removed"
		}
		changes = append(changes, Change{Path: fields[i+1], Kind: kind})
	}
	return changes, nil
}
