package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
// source the review is bound to. A failed write leaves no file at target
// (writeBundleFile). The task record is not changed.
func (env Env) WriteBundle(ctx context.Context, taskPath, reviewID, target string) (WrittenBundle, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return WrittenBundle{}, err
	}
	bundle, err := env.BuildBundle(ctx, taskPath, reviewID)
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
	if err := writeBundleFile(target, data, (*os.Root).Link, syncTaskDirectory); err != nil {
		return WrittenBundle{}, err
	}
	return WrittenBundle{Path: target, Data: data, Bundle: bundle}, nil
}

// bundleLink gives the file oldname in root the new name newname, failing if
// newname exists. Production passes (*os.Root).Link; tests pass a link that
// fails as a filesystem without hard links does.
type bundleLink func(root *os.Root, oldname, newname string) error

// writeBundleFile writes data to path, which must not exist, so that a failed
// write leaves no file at path. data goes to a temporary file in path's
// directory, which is written, synced and closed, then linked to path: the
// bundle appears whole or not at all, and the link refuses an existing file.
// On a filesystem without hard links (vfat, exFAT, some network mounts) it
// instead creates path exclusively, writes and syncs it, and removes it again
// if a step fails; there a crash during the write can leave a partial file.
// The directory is synced last with syncDir (syncTaskDirectory in production).
func writeBundleFile(path string, data []byte, link bundleLink, syncDir func(*os.Root) error) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("write the reviewer bundle to %s: %w", path, err)
	}
	defer root.Close()
	name := filepath.Base(path)
	id, err := randomID()
	if err != nil {
		return fmt.Errorf("write the reviewer bundle to %s: %w", path, err)
	}
	temporary := ".cw-bundle-" + id
	if err := createSynced(root, temporary, data); err != nil {
		return fmt.Errorf("write the reviewer bundle to %s: %w", path, err)
	}
	err = link(root, temporary, name)
	root.Remove(temporary)
	if linksUnsupported(err) {
		err = createSynced(root, name, data)
	}
	if err != nil {
		return fmt.Errorf("write the reviewer bundle to %s: %w", path, err)
	}
	if err := syncDir(root); err != nil {
		return fmt.Errorf("the reviewer bundle was written to %s but its durability is uncertain: %w", path, err)
	}
	return nil
}

// createSynced creates name in root with mode 0600, failing if it exists,
// then writes data, syncs and closes it. When a step after the create fails it
// removes the file, so it never leaves a partial one.
func createSynced(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		root.Remove(name)
	}
	return err
}

// linksUnsupported reports whether a link failed because the filesystem has
// no hard links. An existing target (EEXIST) and every other error are not.
func linksUnsupported(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, errors.ErrUnsupported)
}
