package langpack

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	// GOROOT is Path's own tree, the directory above its bin, to which the
	// GOROOT that `go env GOROOT` printed resolves.
	GOROOT string `json:"goroot"`
}

// Env returns the environment entries under which Path runs as the toolchain
// ResolveGo verified: GOROOT set to its own tree, as cmd/go sets it for a
// toolchain it switches to, so that neither a GOROOT variable nor a go env
// file selects another tree and its go.env, and GOTOOLCHAIN=local, so that
// Path runs itself instead of switching to another go command. Appended to an
// environment, they replace its entries of the same names.
func (tool GoTool) Env() []string {
	return []string{"GOROOT=" + tool.GOROOT, "GOTOOLCHAIN=local"}
}

// goRootLimit bounds the output of the GOROOT probe.
const goRootLimit = 4 << 10

// ResolveGo finds the go command that program names and verifies the go
// command it would run in dir with env. A program without a path separator is
// looked up in env's PATH (findGo), as LoadGoFacts does for go; a path is
// taken as given, relative to dir. That candidate may be a version manager's
// shim, a golang.org/dl wrapper or a cmd/go whose GOTOOLCHAIN setting or
// go.mod selects another toolchain, which cmd/go looks for on PATH before it
// downloads one. So ResolveGo runs the candidate once, as `go env GOROOT` in
// dir with env and within ctx, and then reads $GOROOT/bin/go without
// executing it: it accepts only a Go program built from cmd/go
// (debug/buildinfo), and only when the printed GOROOT resolves to that
// program's own tree. The caller runs GoTool.Path with GoTool.Env, so that the
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
	// A GOROOT elsewhere, such as a directory of links to the real tree with
	// a go.env of its own, would run the real go under another configuration.
	tree := filepath.Dir(filepath.Dir(tool.Path))
	if resolved, err := filepath.EvalSymlinks(goroot); err != nil || resolved != tree {
		return refuse("`go env GOROOT` printed %s, which is not %s, the tree of its go command %s", goroot, tree, tool.Path)
	}
	tool.GOROOT = tree
	return tool, nil
}

// GoEnv is the configuration that a verified go command reported, with
// `go env`, for the environment and directory of a check run (ReadGoEnv).
type GoEnv struct {
	// GOFLAGS is the GOFLAGS the go command applies: the environment's when
	// it is not empty, otherwise a go env file's (GOENV, `go env -w`) or its
	// GOROOT's go.env. It is stored with compiler and linker flag values
	// redacted (RedactGOFLAGS).
	GOFLAGS string `json:"goflags"`
	// GOOS and GOARCH are the platform the go command builds for.
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	// GOHOSTOS and GOHOSTARCH are the platform the go command itself was
	// built for, which go test compares with GOOS and GOARCH.
	GOHOSTOS   string `json:"gohostos"`
	GOHOSTARCH string `json:"gohostarch"`
}

// goEnvLimit bounds the output of the go env probe.
const goEnvLimit = 64 << 10

// ReadGoEnv runs tool.Path once, within ctx, as
// `go env -json GOFLAGS GOOS GOARCH GOHOSTOS GOHOSTARCH` in dir with env
// followed by tool.Env, which is the environment that a check runs tool with,
// and returns what it printed, GOFLAGS redacted. The values are those that go
// command applies to a run started with the same environment and directory,
// from the environment or from a go env file. It fails when the command fails,
// stops, prints more than goEnvLimit bytes or prints anything but the five
// values, with GOOS, GOARCH, GOHOSTOS and GOHOSTARCH not empty.
func ReadGoEnv(ctx context.Context, tool GoTool, dir string, env []string) (GoEnv, error) {
	refuse := func(format string, args ...any) (GoEnv, error) {
		return GoEnv{}, fmt.Errorf("cannot read the configuration of the go command %s: %s", tool.Path, fmt.Sprintf(format, args...))
	}
	got := proc.Run(ctx, proc.Spec{Path: tool.Path, Args: []string{"env", "-json", "GOFLAGS", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH"}, Dir: dir, Env: append(slices.Clip(env), tool.Env()...), StdoutLimit: goEnvLimit, StderrLimit: goRootLimit})
	switch {
	case got.Stopped:
		return refuse("`go env` did not finish (%v)", context.Cause(ctx))
	case got.Err != nil || got.Code != 0:
		line, _, _ := bytes.Cut(bytes.TrimSpace(got.Stderr), []byte("\n"))
		reason := string(bytes.TrimSpace(line))
		if reason == "" && got.Err != nil {
			reason = got.Err.Error()
		}
		return refuse("`go env` failed: %s", quoteLimited(reason))
	case got.Overflow:
		return refuse("`go env` printed more than %d bytes", goEnvLimit)
	}
	var printed map[string]string
	if err := json.Unmarshal(got.Stdout, &printed); err != nil {
		return refuse("`go env -json` printed %s, not a JSON object of strings", quoteLimited(string(got.Stdout)))
	}
	read := GoEnv{GOFLAGS: RedactGOFLAGS(printed["GOFLAGS"]), GOOS: printed["GOOS"], GOARCH: printed["GOARCH"], GOHOSTOS: printed["GOHOSTOS"], GOHOSTARCH: printed["GOHOSTARCH"]}
	if _, ok := printed["GOFLAGS"]; !ok || len(printed) != 5 || read.GOOS == "" || read.GOARCH == "" || read.GOHOSTOS == "" || read.GOHOSTARCH == "" {
		return refuse("`go env -json` printed %s, not GOFLAGS, GOOS, GOARCH, GOHOSTOS and GOHOSTARCH", quoteLimited(string(got.Stdout)))
	}
	return read, nil
}

// GoEnvWithheld returns why the Go rule credits no go test run made under env,
// whatever its argv and GOFLAGS, or "" when env allows credit. A run without
// env does not show what its go command applied. A run whose GOOS or GOARCH
// is not its go command's host's earns none either: go test hands each such
// test binary to a go_<goos>_<goarch>_exec program it finds on PATH, which
// need not run it.
func GoEnvWithheld(env *GoEnv) string {
	switch {
	case env == nil:
		return "cw did not record the configuration its go command applied (go env); run it again"
	case env.GOOS != env.GOHOSTOS || env.GOARCH != env.GOHOSTARCH:
		return fmt.Sprintf("it built its tests for %s/%s, not for its go command's host %s/%s, so go test may have handed them to a go_%s_%s_exec program instead of running them", env.GOOS, env.GOARCH, env.GOHOSTOS, env.GOHOSTARCH, env.GOOS, env.GOARCH)
	}
	return ""
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
