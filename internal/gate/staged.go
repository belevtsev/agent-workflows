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
)

// stagedRule is what the gate can tell, before a git commit runs, of the
// content it records for each path (stagedDifferences).
type stagedRule uint8

const (
	// stagedIndex is a commit that records the index as it stands: plain
	// git commit, and every commit git's own hooks judge, whose index git
	// names in GIT_INDEX_FILE.
	stagedIndex stagedRule = iota
	// stagedRemovals is git commit -a, which records the working tree's
	// bytes of every path in the index, so only a path the index no longer
	// holds can differ.
	stagedRemovals
	// stagedUnknown is a commit whose recorded content the plugin hook cannot
	// tell from the index as it stands: one with pathspecs, --include,
	// --only or --pathspec-from-file, which take the named paths from the
	// working tree, or one after another command in the same line that may
	// change the index first, such as git add. It is not judged.
	stagedUnknown
)

// commitValueOptions are the git commit options that take a separate value.
var commitValueOptions = map[string]bool{
	"-m": true, "-F": true, "-c": true, "-C": true, "-t": true,
	"--message": true, "--file": true, "--reedit-message": true, "--reuse-message": true, "--template": true,
	"--author": true, "--date": true, "--fixup": true, "--squash": true, "--cleanup": true, "--trailer": true,
}

// commitStaged returns the stagedRule of git commit arguments. A word it
// cannot place, such as the value of an abbreviated long option, reads as a
// pathspec, so it leaves the commit unjudged rather than judge content the
// commit may not record.
func commitStaged(arguments []string) stagedRule {
	rule := stagedIndex
	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		switch {
		case argument == "--":
			if i+1 < len(arguments) {
				return stagedUnknown
			}
			return rule
		case argument == "--all":
			rule = stagedRemovals
		case argument == "--only" || argument == "--include" || strings.HasPrefix(argument, "--pathspec-from-file"):
			return stagedUnknown
		case commitValueOptions[argument]:
			i++
		case strings.HasPrefix(argument, "--"):
		case !strings.HasPrefix(argument, "-") || argument == "-":
			return stagedUnknown // a pathspec
		default:
		cluster:
			for j, option := range argument[1:] {
				switch {
				case option == 'a':
					rule = stagedRemovals
				case option == 'i' || option == 'o':
					return stagedUnknown
				case strings.ContainsRune("mFcCt", option):
					if j == len(argument)-2 {
						i++ // the value is the next word
					}
					break cluster // the rest of the cluster is this option's value
				}
			}
		}
	}
	return rule
}

// indexKeepers are the programs that leave git's index as it is, so a git
// commit after them in the same command line records the index the plugin
// hook reads: cd and the gate's trusted read-only commands, and the shell
// words that end a compound command.
var indexKeepers = map[string]bool{
	"cd": true, "pushd": true, "popd": true, "echo": true, "printf": true, "cat": true, "rg": true, "grep": true, "jq": true,
	"true": true, "false": true, ":": true, "pwd": true, "ls": true, "test": true, "[": true,
	"fi": true, "done": true, "esac": true, "}": true, "then": true, "else": true, "do": true,
}

// keepsIndex reports whether a simple command, from its program on, leaves
// git's index as it is: no command, one of indexKeepers, or git push.
func keepsIndex(words []string) bool {
	if len(words) == 0 || indexKeepers[words[0]] {
		return true
	}
	subcommand, _ := gitInvocation(words)
	return subcommand > 0 && words[subcommand] == "push"
}

// stagedDifferences returns the paths, at most limit of them, for which the
// commit in worktree would record content that differs from the working
// tree's, and how many there are. A path counts when the commit records it
// differently from HEAD (git diff --cached, against the index git commits)
// and either its staged mode or bytes differ from the file on disk, or the
// commit removes a file that is still on disk and not ignored. The bytes are
// compared raw, as source capture reads them: no clean filter or end-of-line
// conversion runs. A submodule's commit and an unmerged path, which git
// refuses to commit, are not compared. ok is false when the gate cannot
// tell: a git failure, a file it cannot read, or the deadline.
func stagedDifferences(ctx context.Context, git Git, worktree string, rule stagedRule, limit int) ([]string, int, bool) {
	if rule == stagedUnknown {
		return nil, 0, true
	}
	output, err := git(ctx, worktree, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--ignore-submodules=none")
	if err != nil {
		return nil, 0, false
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return nil, 0, false
	}
	defer root.Close()
	var differ, removed []string
	fields := strings.Split(output, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%2 != 0 {
		return nil, 0, false
	}
	for i := 0; i < len(fields); i += 2 {
		meta, path := strings.Fields(strings.TrimPrefix(fields[i], ":")), fields[i+1]
		if len(meta) != 5 || path == "" {
			return nil, 0, false
		}
		mode, object, status := meta[1], meta[3], meta[4]
		switch {
		case status == "U" || mode == "160000":
			continue
		case status == "D":
			present, ok := onDisk(root, path)
			if !ok {
				return nil, 0, false
			}
			if present {
				removed = append(removed, path)
			}
			continue
		case rule == stagedRemovals:
			continue
		}
		same, ok := matchesDisk(ctx, root, path, mode, object)
		if !ok {
			return nil, 0, false
		}
		if !same {
			differ = append(differ, path)
		}
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
			return nil, 0, false
		}
		for _, path := range strings.Split(listed, "\x00") {
			if path != "" {
				differ = append(differ, path)
			}
		}
	}
	return differ[:min(len(differ), limit)], len(differ), true
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

// matchesDisk reports whether the file at path in root has the staged mode
// (100644, 100755 or 120000) and blob object name. The execute bit is the
// owner's, as git reads it. ok is false when the file cannot be read or the
// object name is of no known format.
func matchesDisk(ctx context.Context, root *os.Root, path, mode, object string) (same, ok bool) {
	info, err := root.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, true
	case err != nil:
		return false, false
	}
	var content io.Reader
	var size int64
	switch {
	case mode == "120000" && info.Mode()&os.ModeSymlink != 0:
		target, err := root.Readlink(path)
		if err != nil {
			return false, false
		}
		content, size = strings.NewReader(target), int64(len(target))
	case (mode == "100644" || mode == "100755") && info.Mode().IsRegular():
		if (info.Mode().Perm()&0o100 != 0) != (mode == "100755") {
			return false, true
		}
		file, err := root.Open(path)
		if err != nil {
			return false, false
		}
		defer file.Close()
		content, size = file, info.Size()
	default:
		return false, true // another kind of file than the one staged
	}
	var blob hash.Hash
	switch len(object) {
	case 2 * sha1.Size:
		blob = sha1.New() //nolint:gosec // G401: git's SHA-1 object names.
	case 2 * sha256.Size:
		blob = sha256.New()
	default:
		return false, false
	}
	fmt.Fprintf(blob, "blob %d\x00", size)
	read, err := io.Copy(blob, contextReader{ctx, content})
	if err != nil {
		return false, false
	}
	return read == size && hex.EncodeToString(blob.Sum(nil)) == object, true
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

// stagedNamed is how many differing paths a deny reason names.
const stagedNamed = 3

// stagedProblem says why records that passed cw task verify on the working
// tree in worktree gate a commit judged by rule, or "" when the commit
// records the working tree's content for every path it changes, or when
// the gate cannot tell.
func stagedProblem(ctx context.Context, git Git, worktree string, rule stagedRule) string {
	paths, count, ok := stagedDifferences(ctx, git, worktree, rule, stagedNamed)
	if !ok || count == 0 {
		return ""
	}
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, strconv.Quote(path))
	}
	named := strings.Join(quoted, ", ")
	if count > len(paths) {
		named += fmt.Sprintf(" and %d more paths", count-len(paths))
	}
	return "this commit would record staged content that differs from the working tree cw task verify passed on, for " + named +
		"; stage the working tree's version with `git add`, or put the staged content in the working tree and verify it"
}
