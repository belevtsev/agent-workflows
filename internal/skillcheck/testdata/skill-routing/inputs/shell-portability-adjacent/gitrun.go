package fixture

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

var ErrNotRepository = errors.New("not a git repository")

// Git runs git with args in dir and returns its standard output.
func Git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if bytes.Contains(stderr.Bytes(), []byte("not a git repository")) {
			return nil, ErrNotRepository
		}
		return nil, errors.New("git failed: " + err.Error())
	}
	return output, nil
}
