package langpack

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// GoTool is a go command that ResolveGo verified: a build of cmd/go, judged
// from its build information without executing it.
type GoTool struct {
	// Path is the go command, with symbolic links resolved.
	Path string `json:"path"`
	// Version is the Go version Path was built with, which for cmd/go is
	// its own release, such as go1.27.4.
	Version string `json:"version"`
	// SHA256 is the hex SHA-256 of Path's bytes as ResolveGo read them.
	SHA256 string `json:"sha256"`
	// GOROOT is the tree that `go env GOROOT` reported; Path is its bin/go.
	GOROOT string `json:"goroot"`
}

// Env returns the environment entries under which Path runs as the toolchain
// ResolveGo verified: GOROOT set to its tree, as cmd/go sets it for a
// toolchain it switches to, and GOTOOLCHAIN=local, so that Path runs itself
// instead of switching to another go command. Appended to an environment,
// they replace its entries of the same names.
func (tool GoTool) Env() []string {
	return []string{"GOROOT=" + tool.GOROOT, "GOTOOLCHAIN=local"}
}

// goRootLimit bounds the output of the GOROOT probe.
const goRootLimit = 4 << 10

// ResolveGo finds the go command that program names and verifies the go
// command it would run in dir with env. A program without a path separator is
// looked up in env's PATH as LoadGoFacts looks up go (findGo); a path is
// taken as given, relative to dir. That candidate may be a version manager's
// shim, a golang.org/dl wrapper or a cmd/go whose GOTOOLCHAIN setting or
// go.mod selects another toolchain, which cmd/go looks for on PATH before it
// downloads one. So ResolveGo runs the candidate once, as `go env GOROOT` in
// dir with env and within ctx, and then reads $GOROOT/bin/go without
// executing it: it accepts only a Go program built from cmd/go
// (debug/buildinfo). The caller runs GoTool.Path with GoTool.Env, so that the
// verified go command cannot switch to another. The error names the
// candidate.
func ResolveGo(ctx context.Context, program, dir string, env []string) (GoTool, error) {
	candidate := program
	switch {
	case !strings.ContainsRune(program, filepath.Separator) && !strings.ContainsRune(program, '/'):
		candidate = findGo(env)
		if candidate == "" {
			return GoTool{}, errors.New("go was not found on PATH; put the directory of the real go command on PATH")
		}
	case !filepath.IsAbs(program):
		candidate = filepath.Join(dir, program)
	}
	refuse := func(format string, args ...any) (GoTool, error) {
		return GoTool{}, fmt.Errorf("cannot verify the go command %s: %s; cw runs only a go command built from cmd/go, so put the real go first on PATH", candidate, fmt.Sprintf(format, args...))
	}
	got := proc.Run(ctx, proc.Spec{Path: candidate, Args: []string{"env", "GOROOT"}, Dir: dir, Env: env, StdoutLimit: goRootLimit, StderrLimit: goRootLimit})
	switch {
	case got.Stopped:
		return refuse("`go env GOROOT` did not finish (%v)", context.Cause(ctx))
	case got.Err != nil || got.Code != 0:
		line, _, _ := bytes.Cut(bytes.TrimSpace(got.Stderr), []byte("\n"))
		reason := string(bytes.TrimSpace(line))
		if reason == "" && got.Err != nil {
			reason = got.Err.Error()
		}
		return refuse("`go env GOROOT` failed: %s", quoteLimited(reason))
	}
	goroot := strings.TrimSpace(string(got.Stdout))
	if got.Overflow || goroot == "" || strings.ContainsAny(goroot, "\r\n") || !filepath.IsAbs(goroot) {
		return refuse("`go env GOROOT` printed %s, not an absolute directory", quoteLimited(string(got.Stdout)))
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	tool, err := readGoTool(filepath.Join(goroot, "bin", name))
	if err != nil {
		return refuse("%v", err)
	}
	tool.GOROOT = goroot
	return tool, nil
}

// readGoTool resolves path's links and reads the file once, for its build
// information and its hash, without executing it. It accepts only cmd/go.
func readGoTool(path string) (GoTool, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return GoTool{}, fmt.Errorf("its GOROOT has no go command: %w", err)
	}
	// Opening a FIFO would block, so only a regular file is opened, and the
	// opened file must still be one.
	notRegular := fmt.Errorf("%s is not a regular file", resolved)
	if stat, err := os.Stat(resolved); err != nil || !stat.Mode().IsRegular() {
		return GoTool{}, cmp.Or(err, notRegular)
	}
	file, err := os.Open(resolved) //nolint:gosec // G304: the go command of the GOROOT that go reported; it is read, not executed.
	if err != nil {
		return GoTool{}, err
	}
	defer file.Close()
	if stat, err := file.Stat(); err != nil || !stat.Mode().IsRegular() {
		return GoTool{}, cmp.Or(err, notRegular)
	}
	info, err := buildinfo.Read(file)
	if err != nil {
		return GoTool{}, fmt.Errorf("%s is not a Go program: %w", resolved, err)
	}
	if info.Path != "cmd/go" {
		return GoTool{}, fmt.Errorf("%s is a build of %s, not of cmd/go", resolved, cmp.Or(info.Path, "an unnamed package"))
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return GoTool{}, err
	}
	return GoTool{Path: resolved, Version: info.GoVersion, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// quoteLimited quotes at most the first 200 bytes of text.
func quoteLimited(text string) string {
	if len(text) > 200 {
		return fmt.Sprintf("%q…", text[:200])
	}
	return fmt.Sprintf("%q", text)
}
