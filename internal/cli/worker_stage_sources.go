package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Raw bytes are bound per path because Git clean filters can conceal an edit
// even when another allowed edit changes the resulting Git tree.
func stageRawSources(ctx context.Context, s *session, tree string) (map[string]string, error) {
	root, err := os.OpenRoot(s.repo.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	sources := map[string]string{}
	var readErr error
	err = s.repo.Entries(ctx, tree, func(entry []byte, path string) {
		if readErr != nil || !bytes.HasPrefix(entry, []byte("100644 ")) && !bytes.HasPrefix(entry, []byte("100755 ")) {
			return
		}
		if readErr = ctx.Err(); readErr != nil {
			return
		}
		var digest string
		digest, readErr = stageRawSource(root, path)
		if readErr == nil {
			sources[path] = digest
		}
	})
	return sources, errors.Join(err, readErr)
}

func stageRawSource(root *os.Root, path string) (string, error) {
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("cannot read regular raw source %s: %v", path, err)
	}
	f, err := root.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("raw source replaced while opening")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return "", errors.New("raw source changed while hashing")
	}
	named, err := root.Lstat(path)
	if err != nil || !os.SameFile(info, named) {
		return "", errors.New("raw source replaced while hashing")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func stageRawChanges(before, after map[string]string) []gitx.Change {
	var changes []gitx.Change
	for path, hash := range before {
		if current, found := after[path]; !found {
			changes = append(changes, gitx.Change{Path: path, Kind: "removed"})
		} else if current != hash {
			changes = append(changes, gitx.Change{Path: path, Kind: "changed"})
		}
	}
	for path := range after {
		if _, found := before[path]; !found {
			changes = append(changes, gitx.Change{Path: path, Kind: "added"})
		}
	}
	slices.SortFunc(changes, func(a, b gitx.Change) int { return bytes.Compare([]byte(a.Path), []byte(b.Path)) })
	return changes
}
