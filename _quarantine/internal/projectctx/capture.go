package projectctx

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505: Git's sha1 object format requires its native blob identity.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	hashpkg "hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxFiles       = 30000
	maxEntries     = 100000
	maxSourceBytes = 64 << 20
	maxFileBytes   = maxSourceBytes
	maxGitOutput   = 8 << 20

	// gitProbeBudget bounds one Git inspection command, including the time
	// its output takes to arrive after Git exits. Git exiting with a failure
	// says the path is not a repository; Git running out of time says
	// nothing about it, so capture stops with an error instead. Five seconds
	// was exceeded on loaded machines (macOS CI runners, hosts at load
	// averages far above their core count), so the budget only stops a hung
	// Git. The caller's context bounds every probe as well, so a caller with
	// a shorter deadline, such as a hook or cw doctor, keeps it.
	gitProbeBudget = 60 * time.Second

	// gitProbeWaitDelay is how long a Git probe's output may still be
	// copied after Git exits. Capture stops when a probe does not finish
	// cleanly, and on a loaded host copying a large listing such as
	// ls-tree -r can take far longer than the 100ms default, which made a
	// complete probe fail with exec.ErrWaitDelay. The grace never outlasts
	// the probe budget or the caller's context: when either ends, the
	// probe's process group is killed, which normally closes its pipes at
	// once.
	gitProbeWaitDelay = 10 * time.Second
)

// DirtyComparisonWarning is the fixed caveat that every Git snapshot's
// warnings carry, clean or dirty. A caller that drops it compares against
// this value, so rewording it here changes no caller.
const DirtyComparisonWarning = "Dirty compares raw on-disk bytes and index/tree metadata without executing Git filters; filtered or normalized files may be conservatively marked dirty"

// gitProbeAborted is a Git inspection command that did not finish cleanly:
// it ran past its budget, was killed by a signal, was stopped by the caller,
// or could not be run or waited for. Unlike Git exiting with a failure it
// says nothing about the repository, so capture returns it rather than
// treating a repository as a plain folder or dropping HEAD or the branch. It
// wraps its cause, so a caller's cancellation stays visible to errors.Is.
type gitProbeAborted struct{ err error }

func (aborted gitProbeAborted) Error() string { return aborted.err.Error() }

func (aborted gitProbeAborted) Unwrap() error { return aborted.err }

// stopsCapture reports whether a Git probe error ends capture.
func stopsCapture(err error) bool {
	_, aborted := errors.AsType[gitProbeAborted](err)
	return aborted
}

type captured struct {
	snapshot Snapshot
	paths    []string
	files    []FileState
	warnings []string
	partial  bool
}

// FileState is one eligible source file at capture time.
type FileState struct {
	Path   string `json:"path"`   // slash-separated, relative to Snapshot.Root; see manifestPath for \\ and \xNN
	Mode   string `json:"mode"`   // "100644", "100755" (owner-executable) or "120000" (symlink)
	SHA256 string `json:"sha256"` // SHA-256 of the file bytes, or of the symlink target text
}

type gitEntry struct {
	mode, oid, stage string
}

// Capture fails closed when bounded inspection cannot identify every eligible
// source byte. No complete fingerprint is returned for an escaping symlink.
func Capture(ctx context.Context, repo string) (Snapshot, error) {
	got, err := capture(ctx, repo, gitProbeBudget)
	if err != nil {
		return Snapshot{}, err
	}
	if got.partial {
		return Snapshot{}, errors.New("project source identity is incomplete: " + strings.Join(got.warnings, "; "))
	}
	return got.snapshot, nil
}

// manifestPath spells a relative path as valid UTF-8 so it can be stored as
// JSON. A valid path without a backslash, the usual case, is unchanged.
// Otherwise each backslash becomes \\ and each byte of an invalid sequence
// \xNN, so distinct paths keep distinct spellings. A spelling only names the
// file; it is not opened.
func manifestPath(rel string) string {
	rel = filepath.ToSlash(rel)
	if utf8.ValidString(rel) && !strings.Contains(rel, `\`) {
		return rel
	}
	var out strings.Builder
	for i := 0; i < len(rel); {
		r, size := utf8.DecodeRuneInString(rel[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&out, `\x%02x`, rel[i])
		case r == '\\':
			out.WriteString(`\\`)
		default:
			out.WriteString(rel[i : i+size])
		}
		i += size
	}
	return out.String()
}

// ManifestRawPath reverses manifestPath: it returns the on-disk relative path
// (slash-separated) that a manifest spelling names, so the file can be opened
// or searched for. A spelling that manifestPath left unchanged is returned as is.
func ManifestRawPath(spelled string) string {
	if !strings.Contains(spelled, `\`) {
		return spelled
	}
	var out []byte
	for i := 0; i < len(spelled); i++ {
		if spelled[i] == '\\' && i+1 < len(spelled) {
			if spelled[i+1] == '\\' {
				out = append(out, '\\')
				i++
				continue
			}
			if spelled[i+1] == 'x' && i+3 < len(spelled) {
				if value, err := strconv.ParseUint(spelled[i+2:i+4], 16, 8); err == nil {
					out = append(out, byte(value))
					i += 3
					continue
				}
			}
		}
		out = append(out, spelled[i])
	}
	return string(out)
}

// CaptureManifest returns the snapshot Capture returns for the same source and
// one FileState per eligible file, sorted by path, from the same single read.
// A tracked file missing from disk is omitted. It fails exactly when Capture
// fails.
func CaptureManifest(ctx context.Context, repo string) (Snapshot, []FileState, error) {
	got, err := capture(ctx, repo, gitProbeBudget)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if got.partial {
		return Snapshot{}, nil, errors.New("project source identity is incomplete: " + strings.Join(got.warnings, "; "))
	}
	// Capture walks raw paths in byte order; an escaped spelling can sort
	// differently, so order by the spelling the manifest stores.
	slices.SortFunc(got.files, func(a, b FileState) int { return strings.Compare(a.Path, b.Path) })
	return got.snapshot, got.files, nil
}

// CanonicalDirectory returns the absolute, symlink-free, clean form of an
// existing directory, the form Snapshot.Root and SelectedPath take.
func CanonicalDirectory(path string) (string, error) {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("resolve selected project path")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", errors.New("selected project path is unavailable")
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", errors.New("selected project path must be a directory")
	}
	return filepath.Clean(abs), nil
}

// capture inspects repo, giving each Git command at most probeBudget.
func capture(ctx context.Context, repo string, probeBudget time.Duration) (captured, error) {
	selected, err := CanonicalDirectory(repo)
	if err != nil {
		return captured{}, err
	}
	got := captured{snapshot: Snapshot{SelectedPath: selected, Root: selected}}
	workingState := ""
	tracked := map[string]gitEntry{}
	git := func(dir string, args ...string) (string, error) {
		probeCtx, cancel := context.WithTimeout(ctx, probeBudget)
		defer cancel()
		// Read-only Git inspection must not invoke a configured filesystem
		// monitor hook or update the untracked cache as a side effect.
		gitArgs := []string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", dir}
		result, runErr := executeBounded(probeCtx, Command{Executable: "git", Args: append(gitArgs, args...), Dir: dir, Env: MinimalEnvironment(os.Getenv), WaitDelay: gitProbeWaitDelay}, maxGitOutput)
		if runErr != nil {
			probe := strings.Join(args, " ")
			// The caller's context is checked first: its deadline can end
			// the probe's context too.
			if ctx.Err() != nil {
				return "", gitProbeAborted{fmt.Errorf("Git inspection stopped by the caller (git %s): %w", probe, ctx.Err())}
			}
			if probeCtx.Err() != nil {
				return "", gitProbeAborted{fmt.Errorf("Git inspection timed out after %s (git %s)", probeBudget, probe)}
			}
			// Only Git exiting with a status, or no Git at all, is positive
			// evidence about the repository. Anything else, such as a start
			// failure or a child holding Git's output open past WaitDelay
			// after Git exited, stops capture.
			exit, isExit := errors.AsType[*exec.ExitError](runErr)
			switch {
			case isExit && exit.Exited(), errors.Is(runErr, exec.ErrNotFound):
				return "", errors.New("Git inspection failed")
			case isExit:
				return "", gitProbeAborted{fmt.Errorf("Git inspection was killed before it finished (git %s)", probe)}
			default:
				return "", gitProbeAborted{fmt.Errorf("Git inspection could not complete (git %s): %w", probe, runErr)}
			}
		}
		if result.Truncated {
			return string(result.Stdout), errors.New("Git metadata exceeded the inspection output limit")
		}
		return string(result.Stdout), nil
	}
	// Only Git exiting with a failure status, or no git on PATH, means the
	// selected path is not in a Git repository; every unclean finish of this
	// or a later probe stops capture.
	root, gitErr := git(selected, "rev-parse", "--show-toplevel")
	if stopsCapture(gitErr) {
		return captured{}, gitErr
	}
	gitRepo := gitErr == nil
	if gitRepo {
		got.snapshot.Root, err = CanonicalDirectory(strings.TrimSuffix(strings.TrimSuffix(root, "\n"), "\r"))
		if err != nil {
			return captured{}, errors.New("Git worktree root is unavailable")
		}
		if !within(got.snapshot.Root, selected) {
			return captured{}, errors.New("selected path is outside its Git worktree root")
		}
		head, headErr := git(got.snapshot.Root, "rev-parse", "--verify", "HEAD")
		if stopsCapture(headErr) {
			return captured{}, headErr
		}
		if headErr == nil {
			got.snapshot.Head = strings.TrimSpace(head)
		}
		branch, branchErr := git(got.snapshot.Root, "symbolic-ref", "--short", "-q", "HEAD")
		if stopsCapture(branchErr) {
			return captured{}, branchErr
		}
		if branchErr == nil {
			got.snapshot.Branch = strings.TrimSpace(branch)
		}
		// Only index/tree plumbing is used. Worktree commands such as status
		// can run repository-configured clean filters while refreshing files.
		index, indexErr := git(got.snapshot.Root, "ls-files", "--stage", "-z", "--cached")
		if stopsCapture(indexErr) {
			return captured{}, indexErr
		}
		var validIndex bool
		tracked, validIndex = parseGitEntries(index, true)
		if indexErr != nil || !validIndex {
			got.incomplete("Git index listing exceeded bounds or was unavailable")
		}
		workingState = index
		headEntries := map[string]gitEntry{}
		if got.snapshot.Head != "" {
			tree, treeErr := git(got.snapshot.Root, "ls-tree", "-r", "-z", "HEAD")
			if stopsCapture(treeErr) {
				return captured{}, treeErr
			}
			var validTree bool
			headEntries, validTree = parseGitEntries(tree, false)
			if treeErr != nil || !validTree {
				got.incomplete("Git HEAD tree listing exceeded bounds or was unavailable")
			}
		}
		got.snapshot.Dirty = !sameGitEntries(tracked, headEntries)
		for path := range tracked {
			// Tracked source is always included, even under names such as
			// internal/log or build.
			got.paths = append(got.paths, path)
		}
		listed, listErr := git(got.snapshot.Root, "ls-files", "-z", "--others", "--exclude-standard")
		if stopsCapture(listErr) {
			return captured{}, listErr
		}
		if listErr != nil {
			got.incomplete("Git file listing exceeded bounds or was unavailable")
		}
		parts := strings.Split(listed, "\x00")
		// A truncated final path is never interpreted as a real filename.
		if len(parts) > 0 {
			parts = parts[:len(parts)-1]
		}
		for _, path := range parts {
			if len(got.paths) >= maxEntries {
				got.incomplete("Project path scan reached its entry limit")
				break
			}
			// Git's own exclude decisions define untracked eligibility. A
			// basename heuristic cannot distinguish new source from output.
			got.paths = append(got.paths, path)
			got.snapshot.Dirty = true
		}
		got.warnings = append(got.warnings, DirtyComparisonWarning)
	} else {
		if ctx.Err() != nil {
			return captured{}, ctx.Err()
		}
		got.warnings = append(got.warnings, "Git identity is unavailable; content identity covers the selected folder only")
		for ancestor := selected; ; ancestor = filepath.Dir(ancestor) {
			if _, markerErr := os.Lstat(filepath.Join(ancestor, ".git")); markerErr == nil {
				got.incomplete("A Git marker exists but Git worktree identity could not be read")
				break
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
		entries := 0
		err = filepath.WalkDir(selected, func(path string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				got.incomplete("Some project paths were unreadable")
				return nil
			}
			rel, _ := filepath.Rel(selected, path)
			if rel == "." {
				return nil
			}
			if ignored(rel) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			entries++
			if entries > maxEntries {
				got.incomplete("Project path scan reached its entry limit")
				return fs.SkipAll
			}
			if !entry.IsDir() {
				got.paths = append(got.paths, rel)
			}
			return nil
		})
		if err != nil {
			return captured{}, err
		}
	}
	slices.Sort(got.paths)
	got.paths = slices.Compact(got.paths)
	sourcePaths := got.paths
	if len(sourcePaths) > maxFiles {
		sourcePaths = sourcePaths[:maxFiles]
		got.incomplete("Project source scan reached its file limit")
	}
	hash := sha256.New()
	for _, value := range []string{"cw-project-v2", got.snapshot.Root, selected, got.snapshot.Head, got.snapshot.Branch, workingState} {
		fmt.Fprintf(hash, "%d:%s\x00", len(value), value)
	}
	var total int64
	sourceRoot, openRootErr := os.OpenRoot(got.snapshot.Root)
	if openRootErr != nil {
		return captured{}, errors.New("open project source root")
	}
	defer sourceRoot.Close()
	for _, rel := range sourcePaths {
		if ctx.Err() != nil {
			return captured{}, ctx.Err()
		}
		path := filepath.Join(got.snapshot.Root, rel)
		if !within(got.snapshot.Root, path) {
			got.incomplete("A project path escaped the selected root")
			continue
		}
		fmt.Fprintf(hash, "path:%d:%s\x00", len(rel), rel)
		info, statErr := sourceRoot.Lstat(rel)
		if errors.Is(statErr, fs.ErrNotExist) {
			_, _ = io.WriteString(hash, "missing\x00")
			if _, ok := tracked[rel]; ok {
				got.snapshot.Dirty = true
			}
			continue
		}
		if statErr != nil {
			got.incomplete("Some project source files were unreadable")
			continue
		}
		fmt.Fprintf(hash, "mode:%s\x00", info.Mode().String())
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := sourceRoot.Readlink(rel)
			resolved, resolveErr := filepath.EvalSymlinks(path)
			if linkErr != nil || resolveErr != nil || !within(got.snapshot.Root, resolved) {
				got.incomplete("A symlink escapes the project root or cannot be resolved; it was not followed")
				continue
			}
			fmt.Fprintf(hash, "symlink:%d:%s\x00", len(target), target)
			targetHash := sha256.Sum256([]byte(target))
			got.files = append(got.files, FileState{Path: manifestPath(rel), Mode: "120000", SHA256: hex.EncodeToString(targetHash[:])})
			if indexed, ok := tracked[rel]; ok {
				object := gitBlobHash(indexed.oid, int64(len(target)))
				_, _ = io.WriteString(object, target)
				if indexed.mode != "120000" || indexed.stage != "0" || hex.EncodeToString(object.Sum(nil)) != indexed.oid {
					got.snapshot.Dirty = true
				}
			}
			// The link itself is source. Its in-root target is scanned under
			// its own path; directory links are deliberately not traversed.
			got.snapshot.FileCount++
			continue
		}
		if !info.Mode().IsRegular() {
			got.incomplete("A non-regular project file was not read")
			continue
		}
		if info.Size() > maxFileBytes || total+info.Size() > maxSourceBytes {
			got.incomplete("Project source hashing reached its per-file or total byte limit")
			continue
		}
		file, openErr := sourceRoot.Open(rel)
		if openErr != nil {
			got.incomplete("Some project source files were unreadable")
			continue
		}
		fileHash := sha256.New()
		var writer io.Writer = fileHash
		indexed, isTracked := tracked[rel]
		var object hashpkg.Hash
		if isTracked {
			object = gitBlobHash(indexed.oid, info.Size())
			writer = io.MultiWriter(fileHash, object)
		}
		read, readErr := io.Copy(writer, io.LimitReader(sourceReader{ctx, file}, maxFileBytes+1))
		after, afterErr := file.Stat()
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || afterErr != nil || read > maxFileBytes || total+read > maxSourceBytes || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			got.incomplete("Project source bytes changed during inspection or exceeded bounds")
			continue
		}
		total += read
		if isTracked {
			mode := "100644"
			if info.Mode().Perm()&0111 != 0 {
				mode = "100755"
			}
			if indexed.mode != mode || indexed.stage != "0" || hex.EncodeToString(object.Sum(nil)) != indexed.oid {
				got.snapshot.Dirty = true
			}
		}
		fileSum := fileHash.Sum(nil)
		fmt.Fprintf(hash, "bytes:%d:%x\x00", read, fileSum)
		fileMode := "100644"
		if info.Mode().Perm()&0100 != 0 {
			fileMode = "100755"
		}
		got.files = append(got.files, FileState{Path: manifestPath(rel), Mode: fileMode, SHA256: hex.EncodeToString(fileSum)})
		got.snapshot.FileCount++
	}
	fmt.Fprintf(hash, "dirty:%t\x00", got.snapshot.Dirty)
	prefix := "sha256:"
	if got.partial {
		prefix = "partial-sha256:"
	}
	got.snapshot.Fingerprint = prefix + hex.EncodeToString(hash.Sum(nil))
	if ctx.Err() != nil {
		return captured{}, ctx.Err()
	}
	return got, nil
}

func parseGitEntries(output string, index bool) (map[string]gitEntry, bool) {
	result := map[string]gitEntry{}
	valid := true
	for _, line := range strings.Split(output, "\x00") {
		if line == "" {
			continue
		}
		metadata, path, ok := strings.Cut(line, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || path == "" || len(result) >= maxEntries {
			valid = false
			continue
		}
		entry := gitEntry{mode: fields[0], oid: fields[2], stage: "0"}
		if index {
			entry.oid, entry.stage = fields[1], fields[2]
		}
		if (len(entry.oid) != 40 && len(entry.oid) != 64) || !within(".", path) || filepath.IsAbs(path) {
			valid = false
			continue
		}
		if _, err := hex.DecodeString(entry.oid); err != nil {
			valid = false
			continue
		}
		result[path] = entry
	}
	if output != "" && !strings.HasSuffix(output, "\x00") {
		valid = false
	}
	return result, valid
}

func sameGitEntries(index, tree map[string]gitEntry) bool {
	if len(index) != len(tree) {
		return false
	}
	for path, entry := range index {
		if entry != tree[path] || entry.stage != "0" {
			return false
		}
	}
	return true
}

func gitBlobHash(oid string, size int64) hashpkg.Hash {
	var result hashpkg.Hash = sha1.New() //nolint:gosec // G401: a sha1 repository names blobs by their sha1 object ID.
	if len(oid) == 64 {
		result = sha256.New()
	}
	fmt.Fprintf(result, "blob %d\x00", size)
	return result
}

type sourceReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader sourceReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func (got *captured) incomplete(warning string) {
	got.partial = true
	if !slices.Contains(got.warnings, warning) {
		got.warnings = append(got.warnings, warning)
	}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Dedicated dependency/cache and reserved operational trees do not establish
// project capabilities. Generic names such as log, build and target can contain
// first-party source and are deliberately not excluded. Git source identity
// does not use these heuristics: it honors Git's own exclude decisions instead.
func ignored(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index, part := range parts {
		switch strings.ToLower(part) {
		case ".git", ".hg", ".svn", "node_modules", "vendor", ".terraform", ".venv", "venv", "__pycache__", ".mypy_cache", ".pytest_cache", ".ruff_cache", ".tox", ".nox", ".cache", ".next", ".nuxt", ".yarn", ".pnpm-store", ".parcel-cache", ".svelte-kit", ".turbo", ".gradle", ".idea", ".vscode", ".graphify", "graphify-out", ".cw", ".cw-evidence":
			return true
		}
		if index > 0 && parts[index-1] == ".codex" {
			switch part {
			case "sessions", "worktrees", "automations", "evidence", "log":
				return true
			}
		}
		if index > 0 && parts[index-1] == ".claude" {
			switch part {
			case "worktrees", "projects", "sessions", "settings.local.json":
				return true
			}
		}
	}
	return false
}
