package devcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Snapshot is a disposable Git repository holding one commit of a source
// working tree: its tracked files as they are on disk (deletions included) and
// its untracked files that Git does not ignore, which is the set `git add -A`
// would commit. The developer gates need a clean exact commit, so they run in
// the copy while the source stays untouched.
type Snapshot struct {
	// Dir is the root of the copied repository.
	Dir string
	// Tree is the committed tree hash. A later commit of the same working
	// tree records the same tree unless the source's own Git configuration
	// (core.fileMode=false, filters, autocrlf) transforms the bytes.
	Tree string
	// Commit is the snapshot commit, made with a fixed identity.
	Commit string
	// Files counts the copied files and symbolic links.
	Files int
	// Leftovers reports copies of dead runs that could not be reclaimed. It
	// never fails the snapshot; the caller shows it so they do not pile up
	// unnoticed.
	Leftovers error

	root    string
	paths   []string
	release func()
}

const (
	snapshotPrefix = "cwdev-snapshot-"
	snapshotLock   = "owner.lock"
	// An owner writes this marker only after it holds the lock, so a lockable
	// marked directory belongs to a process that has died.
	snapshotMarker = "cwdev snapshot owner\n"
	// A directory without a marked lock is reclaimed only once it is this old:
	// its owner died between creating the directory and locking it.
	snapshotUnmarkedAge = time.Hour
)

// NewSnapshot copies source into a new directory under os.TempDir and commits
// it. It first reclaims copies whose owning process died without removing them
// (cw task run kills a timed-out check's whole process group). The caller must
// call Remove.
func NewSnapshot(ctx context.Context, source string) (_ *Snapshot, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	top, err := sourceGit(ctx, source, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("snapshot source must be a Git working tree: %w", err)
	}
	top = strings.TrimSpace(top)
	entries, err := snapshotEntries(ctx, top)
	if err != nil {
		return nil, err
	}
	// A copy that cannot be reclaimed now is retried by the next run and never
	// blocks this one; it is reported through Leftovers.
	leftovers := reclaimSnapshots(os.TempDir())
	root, err := os.MkdirTemp("", snapshotPrefix+"*")
	if err != nil {
		return nil, err
	}
	snapshot := &Snapshot{root: root, Leftovers: leftovers}
	defer func() {
		if err != nil {
			err = errors.Join(err, snapshot.Remove())
		}
	}()
	snapshot.release, err = lockSnapshot(filepath.Join(root, snapshotLock))
	if err != nil {
		return nil, err
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	snapshot.Dir = filepath.Join(physical, "source")
	if err := os.Mkdir(snapshot.Dir, 0o755); err != nil {
		return nil, err
	}
	parents := map[string]bool{}
	for _, rel := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copied, err := copySnapshotEntry(top, snapshot.Dir, rel, parents)
		if err != nil {
			return nil, err
		}
		if copied {
			snapshot.paths = append(snapshot.paths, rel)
		}
	}
	snapshot.Files = len(snapshot.paths)
	if err := commitSnapshot(ctx, top, snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// Remove deletes the copy and releases its lock. It is safe to call twice.
func (snapshot *Snapshot) Remove() error {
	if snapshot == nil || snapshot.root == "" {
		return nil
	}
	err := removeTree(snapshot.root)
	if snapshot.release != nil {
		snapshot.release()
		snapshot.release = nil
	}
	if err != nil {
		return fmt.Errorf("remove snapshot %s: %w", snapshot.root, err)
	}
	snapshot.root = ""
	return nil
}

// snapshotEntries lists what `git add -A` would commit: tracked paths (a path
// deleted on disk is skipped later) and untracked paths Git does not ignore.
func snapshotEntries(ctx context.Context, top string) ([]string, error) {
	unmerged, err := sourceGit(ctx, top, "ls-files", "-z", "--unmerged")
	if err != nil {
		return nil, err
	}
	if unmerged != "" {
		return nil, errors.New("snapshot source has unmerged paths; resolve them first")
	}
	// With -v, Git tags skip-worktree entries (sparse checkout) S and
	// assume-unchanged entries in lower case. `git add -A` keeps their index
	// version whatever is on disk, which a copy of the disk cannot reproduce.
	tagged, err := sourceGit(ctx, top, "ls-files", "-z", "-v")
	if err != nil {
		return nil, err
	}
	for record := range strings.SplitSeq(tagged, "\x00") {
		if len(record) < 3 {
			continue
		}
		if tag := record[0]; tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return nil, fmt.Errorf("snapshot does not support skip-worktree (sparse checkout) or assume-unchanged entries: %s", record[2:])
		}
	}
	staged, err := sourceGit(ctx, top, "ls-files", "-z", "--stage")
	if err != nil {
		return nil, err
	}
	var entries []string
	for record := range strings.SplitSeq(staged, "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("unexpected git ls-files record %q", record)
		}
		if strings.HasPrefix(meta, "160000 ") {
			return nil, fmt.Errorf("snapshot does not support submodules: %s", path)
		}
		entries = append(entries, path)
	}
	untracked, err := sourceGit(ctx, top, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for path := range strings.SplitSeq(untracked, "\x00") {
		if path == "" {
			continue
		}
		if strings.HasSuffix(path, "/") {
			return nil, fmt.Errorf("snapshot does not support nested repositories: %s", path)
		}
		entries = append(entries, path)
	}
	return entries, nil
}

// copySnapshotEntry copies one path as Git records it: a regular file with or
// without the owner executable bit, or a symbolic link verbatim. A tracked
// path missing on disk, replaced by a directory whose files are listed on
// their own, or behind a leading component that is no longer a real directory
// (a symlink or a file, as Git's has_symlink_leading_path sees it) is a
// deletion and is not copied. parents caches leading-directory checks.
func copySnapshotEntry(top, destination, rel string, parents map[string]bool) (bool, error) {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false, fmt.Errorf("snapshot path escapes the source: %s", rel)
	}
	if real, err := realParents(top, rel, parents); err != nil || !real {
		return false, err
	}
	from := filepath.Join(top, filepath.FromSlash(rel))
	to := filepath.Join(destination, filepath.FromSlash(rel))
	info, err := os.Lstat(from)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch mode := info.Mode(); {
	case mode.IsDir():
		if _, err := os.Lstat(filepath.Join(from, ".git")); err == nil {
			return false, fmt.Errorf("snapshot does not support nested repositories: %s", rel)
		}
		return false, nil
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(from)
		if err != nil {
			return false, err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return false, err
		}
		return true, os.Symlink(target, to)
	case mode.IsRegular():
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return false, err
		}
		// Git records 100755 exactly when the owner may execute the file.
		perm := fs.FileMode(0o644)
		if mode&0o100 != 0 {
			perm = 0o755
		}
		return true, copyFile(from, to, perm)
	default:
		return false, fmt.Errorf("snapshot does not support special file %s (%s)", rel, info.Mode().Type())
	}
}

// realParents reports whether every leading component of rel is a real
// directory on disk, not a symlink, file or missing path. Any other failure,
// such as an unreadable parent, is returned rather than taken as a deletion.
func realParents(top, rel string, parents map[string]bool) (bool, error) {
	dir := path.Dir(rel)
	if dir == "." {
		return true, nil
	}
	if isDir, ok := parents[dir]; ok {
		return isDir, nil
	}
	isDir, err := realParents(top, dir, parents)
	if err != nil {
		return false, err
	}
	if isDir {
		info, err := os.Lstat(filepath.Join(top, filepath.FromSlash(dir)))
		switch {
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			isDir = false
		case err != nil:
			return false, err
		default:
			isDir = info.IsDir()
		}
	}
	parents[dir] = isDir
	return isDir, nil
}

func copyFile(from, to string, perm fs.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close(), os.Chmod(to, perm))
}

// commitSnapshot commits every copied path under an isolated Git configuration:
// no global or system settings, hooks, signing, filters or excludes apply, and
// -f with literal pathspecs adds exactly the listed paths even when the copied
// .gitignore matches a tracked file. The committer date follows the source HEAD
// so build stamps match a run on the real commit.
func commitSnapshot(ctx context.Context, top string, snapshot *Snapshot) error {
	date := "2000-01-01T00:00:00Z"
	if stamp, err := sourceGit(ctx, top, "show", "-s", "--format=%cI", "HEAD"); err == nil {
		date = strings.TrimSpace(stamp)
	}
	run := func(stdin []byte, arguments ...string) (string, error) {
		command := exec.CommandContext(ctx, "git", append([]string{"-C", snapshot.Dir}, arguments...)...)
		command.Env = append(cleanGitEnvironment(),
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_LITERAL_PATHSPECS=1",
			"GIT_AUTHOR_NAME=cwdev snapshot", "GIT_AUTHOR_EMAIL=cwdev-snapshot@invalid", "GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_NAME=cwdev snapshot", "GIT_COMMITTER_EMAIL=cwdev-snapshot@invalid", "GIT_COMMITTER_DATE="+date)
		command.Stdin = bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil {
			return "", fmt.Errorf("snapshot git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(stdout.String()), nil
	}
	if _, err := run(nil, "init", "-q", "--template=", "--initial-branch=snapshot"); err != nil {
		return err
	}
	var paths bytes.Buffer
	for _, rel := range snapshot.paths {
		paths.WriteString(rel)
		paths.WriteByte(0)
	}
	if paths.Len() != 0 {
		if _, err := run(paths.Bytes(), "add", "-f", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return err
		}
	}
	head := "none"
	if value, err := sourceGit(ctx, top, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		head = strings.TrimSpace(value)
	}
	if _, err := run(nil, "commit", "-q", "--allow-empty", "--no-verify", "--no-gpg-sign", "-m", "cwdev snapshot of "+top+" at "+head); err != nil {
		return err
	}
	var err error
	if snapshot.Commit, err = run(nil, "rev-parse", "--verify", "HEAD"); err != nil {
		return err
	}
	snapshot.Tree, err = run(nil, "rev-parse", "--verify", "HEAD^{tree}")
	return err
}

// sourceGit reads the source repository with the user's own Git configuration,
// including configuration supplied through the environment (GIT_CONFIG_GLOBAL,
// GIT_CONFIG_PARAMETERS), so excludes match what their real commit would
// include. Inherited variables that select another repository, index or
// object store (from a hook, say) are dropped.
func sourceGit(ctx context.Context, dir string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, arguments...)...)
	command.Env = sourceGitEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// repositoryVariables select a repository, index or object store instead of
// the one at -C.
var repositoryVariables = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_OBJECT_DIRECTORY": true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_COMMON_DIR": true, "GIT_NAMESPACE": true,
	"GIT_PREFIX": true, "GIT_CEILING_DIRECTORIES": true, "GIT_DISCOVERY_ACROSS_FILESYSTEM": true,
	"GIT_QUARANTINE_PATH": true,
}

func sourceGitEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !repositoryVariables[strings.ToUpper(name)] {
			environment = append(environment, entry)
		}
	}
	return environment
}

// cleanGitEnvironment drops every GIT_* variable for the isolated copy.
func cleanGitEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			environment = append(environment, entry)
		}
	}
	return environment
}

// removeTree removes a directory tree, first making directories writable when
// a gate left a read-only one behind.
func removeTree(root string) error {
	if err := os.RemoveAll(root); err == nil {
		return nil
	}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

// reclaimSnapshots removes copies under temp whose owner died: their lock can
// be taken and either carries the owner marker or is older than
// snapshotUnmarkedAge. A live owner holds its lock for the copy's lifetime, so
// concurrent runs never remove each other's copies.
func reclaimSnapshots(temp string) error {
	entries, err := os.ReadDir(temp)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), snapshotPrefix) || !entry.IsDir() {
			continue
		}
		root := filepath.Join(temp, entry.Name())
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || !ownedByCurrentUser(info) {
			continue
		}
		release, marked, ok := tryLockSnapshot(filepath.Join(root, snapshotLock))
		if !ok {
			continue
		}
		if marked || time.Since(info.ModTime()) > snapshotUnmarkedAge {
			errs = append(errs, removeTree(root))
		}
		release()
	}
	return errors.Join(errs...)
}
