package publish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// git runs git in the worktree. Read-only commands skip optional index writes
// and the file system monitor; commands that take paths read them literally.
type git struct{ tool }

var readOnly = []string{"--no-optional-locks", "-c", "core.fsmonitor=false"}

// failure is a reason of section 5 with a human-readable detail.
type failure struct{ reason, detail string }

func failf(reason, format string, args ...any) *failure {
	return &failure{reason: reason, detail: fmt.Sprintf(format, args...)}
}

// readFailure classifies a read-only command that did not succeed.
func readFailure(got result, what string) *failure {
	if got.cancelled {
		return failf("cancelled", "interrupted while %s", what)
	}
	return failf("read-failed", "%s: %s", what, got.why())
}

// read runs a read-only git command and returns its stdout, which must not
// overflow.
func (g git) read(ctx context.Context, what string, args ...string) ([]byte, *failure) {
	got := g.run(ctx, nil, nil, append(slices.Clone(readOnly), args...)...)
	if !got.succeeded() || got.overflow {
		return nil, readFailure(got, what)
	}
	return got.stdout, nil
}

// probe runs a read-only git command whose non-zero exit status is an
// answer; it fails only when git did not exit on its own.
func (g git) probe(ctx context.Context, what string, args ...string) (result, *failure) {
	got := g.run(ctx, nil, nil, append(slices.Clone(readOnly), args...)...)
	if !got.exited() || got.overflow {
		return got, readFailure(got, what)
	}
	return got, nil
}

// output is the git output function the repository resolver takes: stdout
// without trailing line ends, an error for a failure or empty output.
func (g git) output(ctx context.Context, dir string, args ...string) (string, error) {
	in := g
	in.dir = dir
	got := in.run(ctx, nil, nil, append(slices.Clone(readOnly), args...)...)
	if !got.succeeded() || got.overflow {
		return "", errors.New("git " + strings.Join(args, " ") + ": " + got.why())
	}
	value := strings.TrimRight(string(got.stdout), "\r\n")
	if value == "" {
		return "", errors.New("git printed nothing")
	}
	return value, nil
}

func firstLine(data []byte) string { return strings.TrimRight(string(data), "\r\n") }

// toplevel returns the worktree root git reports, or "" outside a worktree.
func (g git) toplevel(ctx context.Context) (string, *failure) {
	got, f := g.probe(ctx, "reading the worktree root", "rev-parse", "--show-toplevel")
	if f != nil || got.code != 0 {
		return "", f
	}
	return firstLine(got.stdout), nil
}

// head returns HEAD's commit, or "" when HEAD names none.
func (g git) head(ctx context.Context) (string, *failure) {
	got, f := g.probe(ctx, "reading HEAD", "rev-parse", "--verify", "--quiet", "HEAD")
	if f != nil || got.code != 0 {
		return "", f
	}
	return firstLine(got.stdout), nil
}

// branch returns the full ref HEAD points at, or "" when it is detached.
func (g git) branch(ctx context.Context) (string, *failure) {
	got, f := g.probe(ctx, "reading the current branch", "symbolic-ref", "--quiet", "HEAD")
	if f != nil || got.code != 0 {
		return "", f
	}
	return firstLine(got.stdout), nil
}

// commitExists reports whether sha names a local commit.
func (g git) commitExists(ctx context.Context, sha string) (bool, *failure) {
	got, f := g.probe(ctx, "looking up commit "+sha, "cat-file", "-e", sha+"^{commit}")
	return f == nil && got.code == 0, f
}

// isAncestor reports whether ancestor is descendant or one of its ancestors.
func (g git) isAncestor(ctx context.Context, ancestor, descendant string) (bool, *failure) {
	got, f := g.probe(ctx, "comparing commits", "merge-base", "--is-ancestor", ancestor, descendant)
	switch {
	case f != nil:
		return false, f
	case got.code == 0:
		return true, nil
	case got.code == 1:
		return false, nil
	}
	return false, readFailure(got, "comparing commits")
}

// treeEntry is one entry of ls-tree or ls-files.
type treeEntry struct {
	mode, kind, oid string
	stage           string
	// intentToAdd is git's own intent-to-add flag on an index entry.
	intentToAdd bool
}

// ceIntentToAdd is git's CE_INTENT_TO_ADD index-entry flag, as
// `ls-files --debug` prints it in hexadecimal.
const ceIntentToAdd = 1 << 29

// tree lists the entries of treeish at the given raw paths.
func (g git) tree(ctx context.Context, treeish string, raw []string) (map[string]treeEntry, *failure) {
	entries := map[string]treeEntry{}
	if len(raw) == 0 {
		return entries, nil
	}
	out, f := g.read(ctx, "listing "+treeish, append([]string{"--literal-pathspecs", "ls-tree", "-r", "-z", treeish, "--"}, raw...)...)
	if f != nil {
		return nil, f
	}
	for _, record := range splitZ(out) {
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, failf("read-failed", "unexpected ls-tree output")
		}
		entries[name] = treeEntry{mode: fields[0], kind: fields[1], oid: fields[2]}
	}
	return entries, nil
}

// index lists the index entries at the given raw paths; an unmerged path has
// several. It reads `ls-files -s --debug -z`, whose entries are
// "MODE OID STAGE\tPATH\0" followed by five debug lines, the last
// "  size: N\tflags: HEX\n"; the flags carry git's intent-to-add bit, which
// no other ls-files format shows.
func (g git) index(ctx context.Context, raw []string) (map[string][]treeEntry, *failure) {
	entries := map[string][]treeEntry{}
	if len(raw) == 0 {
		return entries, nil
	}
	out, f := g.read(ctx, "listing the index", append([]string{"--literal-pathspecs", "ls-files", "-s", "--debug", "-z", "--"}, raw...)...)
	if f != nil {
		return nil, f
	}
	parsed, err := parseIndexDebug(out)
	if err != nil {
		return nil, failf("read-failed", "listing the index: %v", err)
	}
	for _, entry := range parsed {
		entries[entry.path] = append(entries[entry.path], entry.treeEntry)
	}
	return entries, nil
}

type indexEntry struct {
	treeEntry
	path string
}

// parseIndexDebug parses `ls-files -s --debug -z` output.
func parseIndexDebug(out []byte) ([]indexEntry, error) {
	var entries []indexEntry
	rest := out
	for len(rest) > 0 {
		header, after, ok := bytes.Cut(rest, []byte{0})
		meta, name, tab := strings.Cut(string(header), "\t")
		fields := strings.Fields(meta)
		if !ok || !tab || len(fields) != 3 {
			return nil, errors.New("unexpected ls-files output")
		}
		var last string
		for line := 0; line < 5; line++ {
			text, next, found := bytes.Cut(after, []byte("\n"))
			if !found {
				return nil, errors.New("unexpected ls-files debug output")
			}
			last, after = string(text), next
		}
		_, hexFlags, found := strings.Cut(last, "flags: ")
		if !strings.HasPrefix(last, "  size: ") || !found {
			return nil, errors.New("unexpected ls-files debug output")
		}
		flags, err := strconv.ParseUint(strings.TrimSpace(hexFlags), 16, 64)
		if err != nil {
			return nil, errors.New("unexpected ls-files debug flags")
		}
		entries = append(entries, indexEntry{path: name, treeEntry: treeEntry{mode: fields[0], kind: "blob", oid: fields[1], stage: fields[2], intentToAdd: flags&ceIntentToAdd != 0}})
		rest = after
	}
	return entries, nil
}

func splitZ(data []byte) []string {
	var records []string
	for _, record := range strings.Split(string(data), "\x00") {
		if record != "" {
			records = append(records, record)
		}
	}
	return records
}

// blobHashes returns the SHA-256 of each object's bytes, read through
// cat-file --batch without holding the objects in memory.
func (g git) blobHashes(ctx context.Context, oids []string) (map[string]string, *failure) {
	hashes := map[string]string{}
	oids = slices.Compact(slices.Sorted(slices.Values(oids)))
	// The empty blob an intent-to-add entry records need not exist as an
	// object; its bytes are known.
	oids = slices.DeleteFunc(oids, func(oid string) bool {
		if oid == emptyBlob[len(oid)] {
			hashes[oid] = emptySHA256
			return true
		}
		return false
	})
	if len(oids) == 0 {
		return hashes, nil
	}
	hasher := &batchHasher{results: hashes}
	input := []byte(strings.Join(oids, "\n") + "\n")
	got := g.run(ctx, input, hasher, append(slices.Clone(readOnly), "cat-file", "--batch")...)
	if !got.succeeded() {
		return nil, readFailure(got, "reading objects")
	}
	if hasher.err != nil || hasher.state != 0 {
		return nil, failf("read-failed", "reading objects: unexpected cat-file output")
	}
	for _, oid := range oids {
		if _, ok := hashes[oid]; !ok {
			return nil, failf("read-failed", "reading objects: object %s is missing", oid)
		}
	}
	return hashes, nil
}

// emptyBlob is the empty blob's object ID per hash function (SHA-1, SHA-256).
var emptyBlob = map[int]string{40: "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", 64: "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813"}

// emptySHA256 is the SHA-256 of no bytes.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// batchHasher parses `cat-file --batch` output and hashes each object.
type batchHasher struct {
	results   map[string]string
	state     int // 0 header, 1 content, 2 the LF after content
	header    []byte
	oid       string
	remaining int64
	sum       hash.Hash
	err       error
}

func (b *batchHasher) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && b.err == nil {
		switch b.state {
		case 0:
			i := bytes.IndexByte(p, '\n')
			if i < 0 {
				b.header = append(b.header, p...)
				if len(b.header) > 1024 {
					b.err = errors.New("cat-file header too long")
				}
				return n, nil
			}
			b.header = append(b.header, p[:i]...)
			p = p[i+1:]
			fields := strings.Fields(string(b.header))
			b.header = b.header[:0]
			if len(fields) == 2 && fields[1] == "missing" {
				continue
			}
			size, err := strconv.ParseInt(fieldAt(fields, 2), 10, 64)
			if len(fields) != 3 || err != nil || size < 0 {
				b.err = errors.New("unexpected cat-file header")
				continue
			}
			b.oid, b.remaining, b.sum, b.state = fields[0], size, sha256.New(), 1
		case 1:
			k := int(min(int64(len(p)), b.remaining))
			b.sum.Write(p[:k])
			p = p[k:]
			b.remaining -= int64(k)
		case 2:
			if p[0] != '\n' {
				b.err = errors.New("unexpected cat-file separator")
				continue
			}
			p = p[1:]
			b.state = 0
		}
		if b.state == 1 && b.remaining == 0 {
			b.results[b.oid] = hex.EncodeToString(b.sum.Sum(nil))
			b.state = 2
		}
	}
	return n, nil
}

func fieldAt(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}
	return ""
}

// attributeNames are the attributes R10.b reads.
var attributeNames = []string{"filter", "ident", "working-tree-encoding", "text", "eol", "crlf"}

// attributes returns check-attr's values per raw path.
func (g git) attributes(ctx context.Context, raw []string) (map[string]map[string]string, *failure) {
	values := map[string]map[string]string{}
	if len(raw) == 0 {
		return values, nil
	}
	args := append(append([]string{"check-attr", "-z"}, attributeNames...), "--")
	out, f := g.read(ctx, "reading attributes", append(args, raw...)...)
	if f != nil {
		return nil, f
	}
	fields := strings.Split(string(out), "\x00")
	if len(fields)%3 != 1 || fields[len(fields)-1] != "" {
		return nil, failf("read-failed", "unexpected check-attr output")
	}
	for i := 0; i+2 < len(fields); i += 3 {
		if values[fields[i]] == nil {
			values[fields[i]] = map[string]string{}
		}
		values[fields[i]][fields[i+1]] = fields[i+2]
	}
	return values, nil
}

// config returns the effective value of the key the anchored pattern names.
func (g git) config(ctx context.Context, pattern string) (configEntry, *failure) {
	got, f := g.probe(ctx, "reading configuration", "config", "-z", "--get-regexp", pattern)
	switch {
	case f != nil:
		return configEntry{}, f
	case got.code == 1:
		return configEntry{}, nil
	case got.code != 0:
		return configEntry{}, readFailure(got, "reading configuration")
	}
	entry, err := effectiveConfig(got.stdout)
	if err != nil {
		return configEntry{}, failf("read-failed", "reading configuration: %v", err)
	}
	return entry, nil
}

// directory returns a git directory rev-parse reports, absolute and with its
// symbolic links resolved.
func (g git) directory(ctx context.Context, flag string) (string, *failure) {
	out, f := g.read(ctx, "locating the git directory", "rev-parse", flag)
	if f != nil {
		return "", f
	}
	dir := firstLine(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(g.dir, dir)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", failf("read-failed", "locating the git directory: %v", err)
	}
	return resolved, nil
}

// remotes lists the configured remote names.
func (g git) remotes(ctx context.Context) ([]string, *failure) {
	out, f := g.read(ctx, "listing remotes", "remote")
	if f != nil {
		return nil, f
	}
	return strings.Fields(string(out)), nil
}

// remoteURLs lists the fetch or push URLs of remote, after rewrites.
func (g git) remoteURLs(ctx context.Context, remote string, push bool) ([]string, *failure) {
	args := []string{"remote", "get-url"}
	if push {
		args = append(args, "--push")
	}
	out, f := g.read(ctx, "reading the URLs of "+remote, append(args, "--all", "--end-of-options", remote)...)
	if f != nil {
		return nil, f
	}
	var urls []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			urls = append(urls, line)
		}
	}
	return urls, nil
}

// remoteRef returns the commit refs/heads/branch names on remote, or
// "absent".
func (g git) remoteRef(ctx context.Context, remote, branch string) (string, *failure) {
	ref := "refs/heads/" + branch
	out, f := g.read(ctx, "reading "+ref+" of "+remote, "ls-remote", "--end-of-options", remote, ref)
	if f != nil {
		return "", f
	}
	for _, line := range strings.Split(string(out), "\n") {
		oid, name, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if ok && name == ref {
			return oid, nil
		}
	}
	return "absent", nil
}

// commitObject returns the parents and message of a commit.
func (g git) commitObject(ctx context.Context, sha string) ([]string, []byte, *failure) {
	out, f := g.read(ctx, "reading commit "+sha, "cat-file", "commit", sha)
	if f != nil {
		return nil, nil, f
	}
	headers, message, ok := bytes.Cut(out, []byte("\n\n"))
	if !ok {
		headers, message = bytes.TrimSuffix(out, []byte("\n")), nil
	}
	var parents []string
	for _, line := range strings.Split(string(headers), "\n") {
		if parent, found := strings.CutPrefix(line, "parent "); found {
			parents = append(parents, parent)
		}
	}
	return parents, message, nil
}

// treeChange is one path a diff-tree reports.
type treeChange struct {
	newMode, newOID string
	deleted         bool
}

// diff lists the paths whose entries differ between two commits' trees.
func (g git) diff(ctx context.Context, from, to string) (map[string]treeChange, *failure) {
	out, f := g.read(ctx, "comparing trees", "diff-tree", "-r", "-z", "--no-renames", "--no-commit-id", from, to)
	if f != nil {
		return nil, f
	}
	changes := map[string]treeChange{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) != 5 {
			return nil, failf("read-failed", "unexpected diff-tree output")
		}
		changes[fields[i+1]] = treeChange{newMode: meta[1], newOID: meta[3], deleted: meta[4] == "D"}
	}
	return changes, nil
}

// gitLocks are the directories and names the lock listing reads.
type gitLocks struct {
	gitDir, commonDir string
	remote, branch    string
}

// list returns the Git lock files that exist, sorted and without duplicates.
func (l gitLocks) list() ([]string, error) {
	var found []string
	for _, dir := range []string{l.gitDir, filepath.Join(l.gitDir, "reftable"), filepath.Join(l.commonDir, "reftable")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".lock") {
				found = append(found, filepath.Join(dir, entry.Name()))
			}
		}
	}
	for _, name := range []string{"packed-refs.lock", filepath.Join("refs", "heads", l.branch+".lock"), filepath.Join("refs", "remotes", l.remote, l.branch+".lock")} {
		path := filepath.Join(l.commonDir, name)
		if _, err := os.Lstat(path); err == nil {
			found = append(found, path)
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return nil, err
		}
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

// listing takes the lock listing: up to three listings 1 second apart,
// stopping at the first empty one.
func (l gitLocks) listing(ctx context.Context) ([]string, *failure) {
	for attempt := 0; ; attempt++ {
		locks, err := l.list()
		if err != nil {
			return nil, failf("read-failed", "listing Git lock files: %v", err)
		}
		if len(locks) == 0 || attempt == 2 {
			return locks, nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if interrupted(ctx) {
				return nil, failf("cancelled", "interrupted while listing Git lock files")
			}
			return nil, failf("read-failed", "listing Git lock files: out of time")
		case <-timer.C:
		}
	}
}
