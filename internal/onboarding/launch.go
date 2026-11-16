package onboarding

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// LaunchOptions is an explicit interactive handoff to a resolved native host.
type LaunchOptions struct {
	Executable, Prompt string
	In                 io.Reader
	Out, Err           io.Writer
}

// Launch opens the host with one literal prompt, inherited streams and the
// caller's environment. It neither selects a host nor grants runtime trust.
func Launch(ctx context.Context, options LaunchOptions) (int, error) {
	if !filepath.IsAbs(options.Executable) {
		return -1, errors.New("host executable must be an absolute path resolved by `kitsu agent check`")
	}
	if extension := filepath.Ext(options.Executable); strings.EqualFold(extension, ".cmd") || strings.EqualFold(extension, ".bat") {
		return -1, errors.New("host uses a shell wrapper; install a native host executable or use --print for a manual handoff")
	}
	if options.Prompt == "" || strings.ContainsRune(options.Prompt, 0) {
		return -1, errors.New("host handoff requires one nonempty prompt without NUL bytes")
	}
	result := proc.RunForeground(ctx, proc.ForegroundSpec{
		Path: options.Executable, Args: []string{options.Prompt}, Env: os.Environ(),
		Stdin: options.In, Stdout: options.Out, Stderr: options.Err,
	})
	return result.Code, result.Err
}
