//go:build !unix

package langpack

import "errors"

// trustedFile cannot tell who can change a file outside Unix, so no compiler
// passes there and a cgo build earns no Go rule credit.
func trustedFile(string) (string, error) {
	return "", errors.New("cw cannot tell who can change a file on this system")
}

// trustedDir cannot tell who can change a directory outside Unix either.
func trustedDir(string) (string, error) {
	return "", errors.New("cw cannot tell who can change a directory on this system")
}
