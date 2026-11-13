//go:build !unix

package langpack

import "errors"

// trustedFile cannot tell who can change a file outside Unix, so no compiler
// passes there and a cgo build earns no Go rule credit.
func trustedFile(string) (string, error) {
	return "", errors.New("cw cannot tell who can change a file on this system")
}
