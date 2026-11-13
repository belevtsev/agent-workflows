//go:build unix

package langpack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxLinkHops bounds the symbolic links trustedFile follows, as the kernel's
// own limit does.
const maxLinkHops = 40

// trustedFile resolves the absolute path to a regular executable file and
// returns it when no step of the way can be changed by the user cw runs as:
// every directory from the root down, every link and the file itself is owned
// by root or by another user, and no directory or file is writable by its
// group or others. That holds for a compiler a package manager installed,
// such as /usr/bin/gcc or macOS's /usr/bin/clang, and not for one in a
// directory the user owns. It shows who can change the file, not what the
// file does, and it shows nothing when cw runs as root.
func trustedFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s is not an absolute path", path)
	}
	// The path is walked as written, not cleaned: the kernel takes ".."
	// from the directory a link led to, not from the link's name.
	pending := strings.Split(path, "/")
	current := "/"
	if err := trustedEntry(current); err != nil {
		return "", err
	}
	for hops := 0; len(pending) > 0; {
		name := pending[0]
		pending = pending[1:]
		switch name {
		case "", ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, name)
		if err := trustedEntry(next); err != nil {
			return "", err
		}
		info, err := os.Lstat(next)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if hops++; hops > maxLinkHops {
				return "", fmt.Errorf("%s has too many links", path)
			}
			target, err := os.Readlink(next)
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(target) {
				current = "/"
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if len(pending) > 0 && !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", next)
		}
		current = next
	}
	info, err := os.Lstat(current)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable regular file", current)
	}
	return current, nil
}

// trustedEntry fails unless path, not followed, is owned by root or by a user
// other than cw's, and, unless it is a link, is not writable by its group or
// others.
func trustedEntry(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cw cannot read its owner")
	}
	if stat.Uid != 0 && int(stat.Uid) == os.Geteuid() {
		return fmt.Errorf("%s is owned by the user cw runs as", path)
	}
	if info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by its group or others (mode %v)", path, info.Mode().Perm())
	}
	return nil
}
