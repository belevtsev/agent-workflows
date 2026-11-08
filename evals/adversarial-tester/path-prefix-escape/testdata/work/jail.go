// Package jail confines untrusted relative names to a root directory.
package jail

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrOutside reports a name that resolves outside the root.
var ErrOutside = errors.New("jail: name resolves outside the root")

// Root is a directory that names are resolved against.
type Root struct {
	dir string
}

// New returns a Root for the absolute directory dir.
func New(dir string) (*Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("jail: root %q is not absolute", dir)
	}
	clean := filepath.Clean(dir)
	if clean == string(filepath.Separator) {
		return nil, fmt.Errorf("jail: root %q is the filesystem root", dir)
	}
	return &Root{dir: clean}, nil
}

// Resolve returns the cleaned absolute path of name within the root, or
// ErrOutside when name leaves it.
func (r *Root) Resolve(name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", ErrOutside
	}
	p := filepath.Join(r.dir, name)
	if !r.contains(p) {
		return "", ErrOutside
	}
	return p, nil
}

// contains reports whether the cleaned path p lies within the root.
func (r *Root) contains(p string) bool {
	return strings.HasPrefix(p, r.dir)
}
