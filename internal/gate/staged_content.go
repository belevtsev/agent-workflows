package gate

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505: git names SHA-1 repositories' blobs by SHA-1; this compares names, it does not secure anything.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// stagedJudgement is what stagedDifferences finds: the paths whose staged
// content differs from the working tree's, and the paths it cannot judge
// because a git filter, ident or working-tree-encoding that the gate does
// not run may explain the difference. ok is false when the gate cannot tell
// at all.
type stagedJudgement struct {
	differ, unsure []string
	ok             bool
}

// stagedDifferences judges the commit in worktree under rule. A path counts
// when the commit records it differently from HEAD (git diff --cached,
// against the index git commits) and its staged mode or bytes differ from
// the file on disk, or the commit removes a file that is still on disk and
// not ignored. Under -a only removals count, and the paths marked
// assume-unchanged or skip-worktree, which -a leaves as staged. The mode is
// the owner's execute bit, as git reads it; where core.fileMode is false,
// it counts only for a path whose mode the commit changes. The bytes are
// first compared raw, as source capture reads them; no filter runs. A
// regular file that differs is then explained by git's end-of-line
// conversion or a Git LFS pointer to its bytes, or is unsure when another
// filter, ident or working-tree-encoding applies (explainContent). A
// submodule's commit and an unmerged path, which git refuses to commit, are
// not compared. ok is false on a git failure, a file the gate cannot read,
// or the deadline.
func stagedDifferences(ctx context.Context, git Git, worktree string, rule stagedRule) stagedJudgement {
	if rule == stagedUnknown {
		return stagedJudgement{ok: true}
	}
	output, err := git(ctx, worktree, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--ignore-submodules=none")
	if err != nil {
		return stagedJudgement{}
	}
	if output == "" {
		return stagedJudgement{ok: true}
	}
	fields := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return stagedJudgement{}
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return stagedJudgement{}
	}
	defer root.Close()
	kept := map[string]bool{} // the paths -a leaves as staged
	if rule == stagedRemovals {
		listed, err := git(ctx, worktree, "ls-files", "-v", "-z")
		if err != nil {
			return stagedJudgement{}
		}
		for entry := range strings.SplitSeq(listed, "\x00") {
			if tag, path, ok := strings.Cut(entry, " "); ok && (tag == "S" || strings.ToLower(tag) == tag) {
				kept[path] = true
			}
		}
	}
	fileMode := ""
	var judged stagedJudgement
	var removed, content []string
	for i := 0; i < len(fields); i += 2 {
		meta, path := strings.Fields(strings.TrimPrefix(fields[i], ":")), fields[i+1]
		if len(meta) != 5 || path == "" {
			return stagedJudgement{}
		}
		source, mode, object, status := meta[0], meta[1], meta[3], meta[4]
		switch {
		case status == "U" || mode == "160000":
			continue
		case status == "D":
			present, ok := onDisk(root, path)
			if !ok {
				return stagedJudgement{}
			}
			if present {
				removed = append(removed, path)
			}
			continue
		case rule == stagedRemovals && !kept[path]:
			continue
		}
		// A mode the commit changes is judged whatever core.fileMode says.
		judgeMode := true
		if source == mode || source == "000000" {
			if fileMode == "" {
				if fileMode, err = git.output(ctx, worktree, "config", "--type=bool", "--default=true", "--get", "core.fileMode"); err != nil {
					return stagedJudgement{}
				}
			}
			judgeMode = fileMode != "false"
		}
		switch matchesDisk(ctx, root, path, mode, object, judgeMode) {
		case diskUnreadable:
			return stagedJudgement{}
		case diskOtherBytes:
			content = append(content, path, object)
		case diskDiffers:
			judged.differ = append(judged.differ, path)
		}
	}
	if len(content) > 0 && !explainContent(ctx, git, root, worktree, content, &judged) {
		return stagedJudgement{}
	}
	if len(removed) > 0 {
		// A removed path still on disk differs unless git ignores it, which
		// leaves it out of the source cw task verify digests.
		args := []string{"ls-files", "-z", "--others", "--exclude-standard", "--"}
		for _, path := range removed {
			args = append(args, ":(literal)"+path)
		}
		listed, err := git(ctx, worktree, args...)
		if err != nil {
			return stagedJudgement{}
		}
		for path := range strings.SplitSeq(listed, "\x00") {
			if path != "" {
				judged.differ = append(judged.differ, path)
			}
		}
	}
	judged.ok = true
	return judged
}

// stagedAttributes are the attributes that make git's staged blob of a file
// differ from its bytes on disk.
var stagedAttributes = []string{"text", "eol", "crlf", "filter", "ident", "working-tree-encoding"}

// explainContent sorts the regular files in pairs (path, staged object name)
// whose raw bytes are not their staged blob into judged. A path with the
// filter lfs is the same when its blob is a Git LFS pointer whose oid and
// size are those of the disk bytes; a path with another filter, ident or
// working-tree-encoding is unsure; any other path is the same when git's
// end-of-line conversion may apply to it (its text, eol or crlf attribute
// is set or auto and not unset, or else core.autocrlf is true or input)
// and its blob is the disk bytes with each CRLF turned into LF. Every other
// path differs. It runs no filter. It reports false on a git failure or a
// file it cannot read.
func explainContent(ctx context.Context, git Git, root *os.Root, worktree string, pairs []string, judged *stagedJudgement) bool {
	args := append([]string{"check-attr", "-z"}, stagedAttributes...)
	args = append(args, "--")
	for i := 0; i < len(pairs); i += 2 {
		args = append(args, pairs[i])
	}
	output, err := git(ctx, worktree, args...)
	if err != nil {
		return false
	}
	attributes := map[string]map[string]string{}
	values := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	for i := 0; i+2 < len(values); i += 3 {
		if attributes[values[i]] == nil {
			attributes[values[i]] = map[string]string{}
		}
		attributes[values[i]][values[i+1]] = values[i+2]
	}
	autocrlf := ""
	for i := 0; i < len(pairs); i += 2 {
		path, object := pairs[i], pairs[i+1]
		attribute := attributes[path]
		set := func(name string) bool {
			return attribute[name] != "" && attribute[name] != "unspecified" && attribute[name] != "unset"
		}
		switch {
		case attribute["filter"] == "lfs":
			same, ok := lfsPointerMatches(ctx, git, root, worktree, path, object)
			if !ok {
				return false
			}
			if !same {
				judged.differ = append(judged.differ, path)
			}
			continue
		case set("filter") || set("ident") || set("working-tree-encoding"):
			judged.unsure = append(judged.unsure, path)
			continue
		}
		binary := attribute["text"] == "unset" || attribute["crlf"] == "unset"
		converts := !binary && (set("text") || set("eol") || set("crlf"))
		if !binary && !converts {
			if autocrlf == "" {
				value, err := git.output(ctx, worktree, "config", "--default=false", "--get", "core.autocrlf")
				if err != nil {
					return false
				}
				autocrlf = strings.ToLower(value)
			}
			converts = autocrlf == "input" || autocrlf == "true" || autocrlf == "yes" || autocrlf == "on" || autocrlf == "1"
		}
		same := false
		if converts {
			var ok bool
			if same, ok = crlfBlobMatches(ctx, root, path, object); !ok {
				return false
			}
		}
		if !same {
			judged.differ = append(judged.differ, path)
		}
	}
	return true
}

// lfsPointerMatches reports whether the staged blob object is a Git LFS
// pointer whose oid and size are the SHA-256 and size of path's bytes.
func lfsPointerMatches(ctx context.Context, git Git, root *os.Root, worktree, path, object string) (bool, bool) {
	pointer, err := git(ctx, worktree, "cat-file", "blob", object)
	if err != nil {
		return false, false
	}
	var oid, size string
	for line := range strings.SplitSeq(pointer, "\n") {
		if value, ok := strings.CutPrefix(line, "oid sha256:"); ok {
			oid = value
		} else if value, ok := strings.CutPrefix(line, "size "); ok {
			size = value
		}
	}
	if !strings.HasPrefix(pointer, "version https://git-lfs.github.com/spec/") || oid == "" || size == "" {
		return false, true
	}
	file, err := root.Open(path)
	if err != nil {
		return false, false
	}
	defer file.Close()
	sum := sha256.New()
	read, err := io.Copy(sum, contextReader{ctx, file})
	if err != nil {
		return false, false
	}
	return oid == hex.EncodeToString(sum.Sum(nil)) && size == strconv.FormatInt(read, 10), true
}

// crlfBlobMatches reports whether object is the git blob of path's bytes
// with each CRLF turned into LF, as git's end-of-line conversion stages it.
// It reads the file twice: to size the blob, then to hash it.
func crlfBlobMatches(ctx context.Context, root *os.Root, path, object string) (bool, bool) {
	blob := blobHash(object)
	if blob == nil {
		return false, false
	}
	var size counter
	for pass, sink := range []io.Writer{&size, blob} {
		if pass == 1 {
			fmt.Fprintf(blob, "blob %d\x00", size)
		}
		file, err := root.Open(path)
		if err != nil {
			return false, false
		}
		convert := &crlfWriter{to: sink}
		_, err = io.Copy(convert, contextReader{ctx, file})
		closeErr := file.Close()
		if err != nil || closeErr != nil || convert.flush() != nil {
			return false, false
		}
	}
	return hex.EncodeToString(blob.Sum(nil)) == object, true
}

// counter counts the bytes written to it.
type counter int64

func (c *counter) Write(p []byte) (int, error) {
	*c += counter(len(p))
	return len(p), nil
}

// crlfWriter writes to to what is written to it with each CRLF turned into
// LF; flush writes a CR held back at the end.
type crlfWriter struct {
	to      io.Writer
	pending bool // the last byte written was a CR, not yet passed on
}

func (w *crlfWriter) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+1)
	for _, b := range p {
		if w.pending && b != '\n' {
			out = append(out, '\r')
		}
		w.pending = b == '\r'
		if !w.pending {
			out = append(out, b)
		}
	}
	if _, err := w.to.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *crlfWriter) flush() error {
	if !w.pending {
		return nil
	}
	w.pending = false
	_, err := w.to.Write([]byte{'\r'})
	return err
}

// blobHash returns the hash git names objects with, by the length of the
// object name: SHA-1 or SHA-256; nil for any other length.
func blobHash(object string) hash.Hash {
	switch len(object) {
	case 2 * sha1.Size:
		return sha1.New() //nolint:gosec // G401: git's SHA-1 object names.
	case 2 * sha256.Size:
		return sha256.New()
	}
	return nil
}

// onDisk reports whether path names a file or symbolic link in root; a
// directory there is not one.
func onDisk(root *os.Root, path string) (bool, bool) {
	info, err := root.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, true
	case err != nil:
		return false, false
	}
	return !info.IsDir(), true
}

// diskVerdict is how a file on disk compares with its staged entry.
type diskVerdict uint8

const (
	diskSame diskVerdict = iota
	// diskDiffers is a missing file, another kind of file, or another mode.
	diskDiffers
	// diskOtherBytes is a regular file of the staged mode whose raw bytes
	// are not the staged blob, which a conversion may explain.
	diskOtherBytes
	// diskUnreadable is a file the gate cannot read.
	diskUnreadable
)

// matchesDisk compares the file at path in root with the staged mode
// (100644, 100755 or 120000) and blob object name, and the owner's execute
// bit only when judgeMode.
func matchesDisk(ctx context.Context, root *os.Root, path, mode, object string, judgeMode bool) diskVerdict {
	info, err := root.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return diskDiffers
	case err != nil:
		return diskUnreadable
	}
	blob := blobHash(object)
	if blob == nil {
		return diskUnreadable
	}
	switch {
	case mode == "120000" && info.Mode()&os.ModeSymlink != 0:
		target, err := root.Readlink(path)
		if err != nil {
			return diskUnreadable
		}
		fmt.Fprintf(blob, "blob %d\x00%s", len(target), target)
		if hex.EncodeToString(blob.Sum(nil)) != object {
			return diskDiffers
		}
		return diskSame
	case (mode == "100644" || mode == "100755") && info.Mode().IsRegular():
		if judgeMode && (info.Mode().Perm()&0o100 != 0) != (mode == "100755") {
			return diskDiffers
		}
		file, err := root.Open(path)
		if err != nil {
			return diskUnreadable
		}
		defer file.Close()
		fmt.Fprintf(blob, "blob %d\x00", info.Size())
		read, err := io.Copy(blob, contextReader{ctx, file})
		switch {
		case err != nil:
			return diskUnreadable
		case read != info.Size() || hex.EncodeToString(blob.Sum(nil)) != object:
			return diskOtherBytes
		}
		return diskSame
	}
	return diskDiffers // another kind of file than the one staged
}

// contextReader stops reading once ctx is done.
type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// stagedNamed is how many paths a reason names.
const stagedNamed = 3

// namePaths quotes the first stagedNamed paths and counts the rest.
func namePaths(paths []string) string {
	quoted := make([]string, 0, stagedNamed)
	for _, path := range paths[:min(len(paths), stagedNamed)] {
		quoted = append(quoted, strconv.Quote(path))
	}
	named := strings.Join(quoted, ", ")
	if len(paths) > stagedNamed {
		named += fmt.Sprintf(" and %d more paths", len(paths)-stagedNamed)
	}
	return named
}

// stagedProblem says why records that passed cw task verify on the working
// tree in worktree gate a commit judged by rule, or "" when they do not.
// When nothing differs, unsure lists the paths the gate cannot judge
// because a conversion it does not run applies to them. Both are empty when
// the gate cannot tell.
func stagedProblem(ctx context.Context, git Git, worktree string, rule stagedRule) (problem string, unsure []string) {
	judged := stagedDifferences(ctx, git, worktree, rule)
	switch {
	case !judged.ok:
		return "", nil
	case len(judged.differ) > 0:
		return "this commit would record staged content that differs from the working tree cw task verify passed on, for " + namePaths(judged.differ) +
			"; stage the working tree's version with `git add`, or put the staged content in the working tree and verify it", nil
	}
	return "", judged.unsure
}

// stagedUnsureReason explains the ask for a commit over verified records
// whose staged content for some paths comes from a git filter, ident or
// working-tree-encoding, which the gate does not run.
func stagedUnsureReason(verified []taskflow.OpenRecord, unsure []string) string {
	named, rest := splitNamed(verified)
	tasks := make([]string, 0, len(named)+1)
	for _, record := range named {
		tasks = append(tasks, fmt.Sprintf("%s (%s)", record.Record.ID, record.Record.Plan.Title))
	}
	if len(rest) > 0 {
		tasks = append(tasks, "and "+countListed("task", rest))
	}
	return fmt.Sprintf("agent-workflows: %s passed cw task verify on the working tree, but this commit stages content for %s through a git filter, ident or working-tree-encoding that the gate does not run, so the gate cannot tell whether the commit records the verified bytes. Confirm only if the staged content is what that conversion makes of the working tree.",
		strings.Join(tasks, ", "), namePaths(unsure))
}
