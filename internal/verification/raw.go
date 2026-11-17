package verification

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // G505: Git SHA1 repositories require their existing object hash, not a security digest.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// rawDifferences conservatively treats original working bytes that differ from
// Git's normalized snapshot as changed. A raw baseline is unavailable: filters
// and line-ending normalization must not hide a required verification trigger.
func rawDifferences(ctx context.Context, repo *gitx.Repo, tree string) (map[string]bool, error) {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return nil, fmt.Errorf("read original verification sources: %w", err)
	}
	defer func() { _ = root.Close() }()
	paths := map[string]bool{}
	buffer := make([]byte, 32<<10)
	var sourceErr error
	err = repo.Entries(ctx, tree, func(raw []byte, path string) {
		if sourceErr != nil || generatedHistory(path) {
			return
		}
		header, _, _ := bytes.Cut(raw, []byte{'\t'})
		fields := strings.Fields(string(header))
		if len(fields) != 3 || fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
			return
		}
		var differs bool
		differs, sourceErr = rawBlobDiffers(ctx, root, path, fields[2], buffer)
		if differs {
			paths[path] = true
		}
	})
	if err != nil {
		return nil, err
	}
	if sourceErr != nil {
		return nil, sourceErr
	}
	return paths, nil
}

func rawBlobDiffers(ctx context.Context, root *os.Root, path, object string, buffer []byte) (bool, error) {
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("original source %s is not an available regular file: %v", decl.Plain(path), err)
	}
	f, err := root.Open(path)
	if err != nil {
		return false, fmt.Errorf("read original source %s: %w", decl.Plain(path), err)
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !sameRawFile(info, opened) {
		return false, fmt.Errorf("original source %s changed while opening", decl.Plain(path))
	}
	h, err := gitBlobHash(object)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(h, "blob %d%c", info.Size(), byte(0))
	size, err := io.CopyBuffer(h, rawContextReader{ctx: ctx, reader: f}, buffer)
	if err != nil {
		return false, fmt.Errorf("read original source %s: %w", decl.Plain(path), err)
	}
	after, err := f.Stat()
	if err != nil || size != info.Size() || !sameRawFile(info, after) {
		return false, fmt.Errorf("original source %s changed while hashing", decl.Plain(path))
	}
	named, err := root.Lstat(path)
	if err != nil || !sameRawFile(info, named) {
		return false, fmt.Errorf("original source %s changed while hashing", decl.Plain(path))
	}
	return hex.EncodeToString(h.Sum(nil)) != object, nil
}

func sameRawFile(before, after os.FileInfo) bool {
	return after != nil && after.Mode().IsRegular() && before.Mode() == after.Mode() && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func gitBlobHash(object string) (hash.Hash, error) {
	switch len(object) {
	case sha1.Size * 2:
		return sha1.New(), nil //nolint:gosec // Git's SHA1 object format is an existing content identity.
	case sha256.Size * 2:
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("unsupported Git object identity length %d", len(object))
	}
}

type rawContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r rawContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
