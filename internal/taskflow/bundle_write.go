package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WrittenBundle is a reviewer bundle WriteBundle wrote.
type WrittenBundle struct {
	// Path is the absolute path of the new file.
	Path string
	// Data is the file's content: EncodeBundle of Bundle.
	Data   []byte
	Bundle Bundle
}

// WriteBundle generates the bundle for a review about to be assigned, as
// BuildBundle does, and writes it with mode 0600 to target, which must be a
// new file outside the project source: a file inside it would change the
// source the review is bound to. The task record is not changed.
//
// The file appears whole or not at all, as a task artifact does: writeNew
// writes and syncs a temporary file in target's directory, links it to target,
// which fails if target exists, syncs the directory and removes the temporary
// name. A failed write leaves no file at target; a crash can leave a .write-*
// file beside it, never a partial bundle.
func WriteBundle(ctx context.Context, taskPath, reviewID, target string) (WrittenBundle, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return WrittenBundle{}, err
	}
	bundle, err := BuildBundle(ctx, taskPath, reviewID)
	if err != nil {
		return WrittenBundle{}, err
	}
	// Resolve the parent so a symlinked spelling of a path inside the
	// project (such as /tmp for /private/tmp) is still recognized.
	resolved := target
	if parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target)); parentErr == nil {
		resolved = filepath.Join(parent, filepath.Base(target))
	}
	if within(bundle.Snapshot.Root, resolved) {
		return WrittenBundle{}, errors.New("write the reviewer bundle outside the project source; a file inside it changes the source the review is bound to")
	}
	data, err := EncodeBundle(bundle)
	if err != nil {
		return WrittenBundle{}, err
	}
	if err := writeNewFile(target, data); err != nil {
		return WrittenBundle{}, fmt.Errorf("write the reviewer bundle to %s: %w", target, err)
	}
	return WrittenBundle{Path: target, Data: data, Bundle: bundle}, nil
}

// writeNewFile is writeNew for a file outside the task directory, with the
// file's own directory as the root.
func writeNewFile(path string, data []byte) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return writeNew(root, filepath.Base(path), data)
}
