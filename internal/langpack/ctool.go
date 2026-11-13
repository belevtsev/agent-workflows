package langpack

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// CToolchain is how ReadGoEnv judged the C toolchain of a go command with cgo
// on. go test -race and every cgo build compile and link C code with the CC
// that go env reports (the environment's, a go env file's or the default,
// gcc or clang, found on PATH), with the CGO_*FLAGS it reports, and that
// compiler runs an assembler and a linker of its own choosing. Any of them,
// its flags or the variables that steer it can put code into a test binary
// that reports a pass without running a test, so the Go rule credits a run
// with cgo on only when Unverified is empty (GoEnvWithheld).
type CToolchain struct {
	// CGO is whether go env reported CGO_ENABLED=1. With cgo off, go builds
	// no C code, and nothing else is judged.
	CGO bool `json:"cgo"`
	// CC and CXX are the C and C++ compilers the run was given, by the path
	// found for go env's CC and CXX; nil when not found. CXX is judged only
	// when found: a go command that cannot find it builds no C++.
	CC  *CTool `json:"cc,omitempty"`
	CXX *CTool `json:"cxx,omitempty"`
	// Unverified says why cw could not verify the toolchain; empty when it
	// did or cgo was off.
	Unverified string `json:"unverified,omitempty"`
}

// CTool is a C or C++ compiler a check run was given.
type CTool struct {
	// Path is the compiler as found on PATH or named by an absolute path,
	// before links are resolved, since a compiler driver such as clang++
	// reads the name it was started by.
	Path string `json:"path"`
	// SHA256 is the hex SHA-256 of the file Path resolves to, empty when it
	// could not be read.
	SHA256 string `json:"sha256,omitempty"`
}

// cgoFlagNames are the flag variables cmd/go passes to the C toolchain for
// every cgo build, ahead of a package's own #cgo flags (which cmd/go checks
// against its own allowlist).
var cgoFlagNames = []string{"CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS"}

// cgoFlagAllowed matches the CGO_*FLAGS words that only choose optimisation
// and debug information, such as cmd/go's default "-O2 -g". Any other word,
// such as -B, -wrapper, -specs, -include, -I, -L or -Wl, can choose the
// programs, headers or libraries that build a test binary.
var cgoFlagAllowed = regexp.MustCompile(`^-(O[0-3sgz]?|g[0-3]?)$`)

// cSteering are variables that choose the programs, headers or libraries the
// C toolchain uses, or code loaded into it or into a cgo test binary.
// DEVELOPER_DIR, also one, is judged on its own (judgeDeveloperDir).
var cSteering = []string{
	"COMPILER_PATH", "GCC_EXEC_PREFIX", "LIBRARY_PATH", "CPATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH", "OBJC_INCLUDE_PATH",
	"CCC_OVERRIDE_OPTIONS", "GCONV_PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "LD_RUN_PATH", "SDKROOT", "TOOLCHAINS",
}

// judgeC judges the C toolchain that the go command which printed values runs
// in dir with env (CToolchain), and returns pin: CGO_ENABLED as read and,
// with cgo on, the CGO_*FLAGS as read (a space for none) and the compilers by
// the paths judged. Appended to the run's environment, they win over a go
// env file read again by the run and over a later PATH lookup. judgeC runs a
// compiler, to ask which assembler and linker it uses, only once that
// compiler and its environment passed.
func judgeC(ctx context.Context, printed map[string]string, dir string, env []string) (*CToolchain, []string) {
	judged := &CToolchain{CGO: printed["CGO_ENABLED"] == "1"}
	if !judged.CGO {
		return judged, []string{"CGO_ENABLED=0"}
	}
	pin := []string{"CGO_ENABLED=1"}
	reasons := []string{}
	for _, name := range cgoFlagNames {
		value := printed[name]
		pin = append(pin, name+"="+cmp.Or(value, " "))
		for _, word := range strings.Fields(value) {
			if !cgoFlagAllowed.MatchString(word) {
				reasons = append(reasons, fmt.Sprintf("%s holds %s; cw credits a cgo build only with -O and -g flags there", name, quoteLimited(word)))
				break
			}
		}
	}
	for _, name := range cSteering {
		if lastValue(env, name) != "" {
			reasons = append(reasons, name+" is set")
		}
	}
	for _, entry := range env {
		if name, value, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "DYLD_") && value != "" {
			reasons = append(reasons, name+" is set")
		}
	}
	if reason := judgeDeveloperDir(lastValue(env, "DEVELOPER_DIR")); reason != "" {
		reasons = append(reasons, reason)
	}
	var reason string
	if judged.CC, reason = findCompiler("CC", printed["CC"], dir, env); judged.CC == nil || reason != "" {
		reasons = append(reasons, cmp.Or(reason, fmt.Sprintf("CC %s was not found on PATH", quoteLimited(printed["CC"]))))
	}
	if judged.CXX, reason = findCompiler("CXX", printed["CXX"], dir, env); reason != "" {
		reasons = append(reasons, reason)
	}
	for _, tool := range []struct {
		name  string
		found *CTool
	}{{"CC", judged.CC}, {"CXX", judged.CXX}} {
		if tool.found != nil {
			pin = append(pin, tool.name+"="+tool.found.Path)
		}
	}
	if len(reasons) == 0 && runtime.GOOS != "darwin" {
		reasons = append(reasons, judgeHelpers(ctx, judged.CC.Path, dir, env)...)
	}
	if len(reasons) > 0 {
		judged.Unverified = "cw could not verify its C toolchain: " + strings.Join(reasons, "; ")
	}
	return judged, pin
}

// findCompiler finds the compiler that go env's value of name (CC or CXX)
// names, as cmd/go would, and judges it with trustedFile. value must be one
// program without arguments or quotes: a bare name is looked up in env's
// PATH, a relative path is taken in dir. It returns nil and no reason when a
// bare name is not on PATH, and a reason when value or the compiler found
// does not pass.
func findCompiler(name, value, dir string, env []string) (*CTool, string) {
	words := strings.Fields(value)
	if len(words) != 1 || strings.ContainsAny(value, `'"\`) {
		return nil, fmt.Sprintf("%s is %s, not one program without arguments", name, quoteLimited(value))
	}
	path := words[0]
	switch {
	case !strings.ContainsRune(path, '/') && !strings.ContainsRune(path, filepath.Separator):
		if path = findOnPath(env, path); path == "" {
			return nil, ""
		}
	case !filepath.IsAbs(path):
		path = filepath.Join(dir, path)
	}
	tool := &CTool{Path: path}
	resolved, err := trustedFile(path)
	if err != nil {
		return tool, fmt.Sprintf("the %s compiler %s is not one that only root or another user can change: %v", name, path, err)
	}
	sum, err := fileSHA256(resolved)
	if err != nil {
		return tool, fmt.Sprintf("the %s compiler %s cannot be read: %v", name, path, err)
	}
	tool.SHA256 = sum
	return tool, ""
}

// judgeHelpers asks the compiler at cc, already judged, which assembler and
// linker it runs (`-print-prog-name`, which gcc and clang answer) and judges
// each with trustedFile: a compiler that does not find one in its own
// directories runs the first on PATH.
func judgeHelpers(ctx context.Context, cc, dir string, env []string) []string {
	reasons := []string{}
	for _, helper := range []string{"as", "ld"} {
		got := proc.Run(ctx, proc.Spec{Path: cc, Args: []string{"-print-prog-name=" + helper}, Dir: dir, Env: env, StdoutLimit: goRootLimit, StderrLimit: goRootLimit})
		printed := string(bytes.TrimSpace(got.Stdout))
		if got.Stopped || got.Err != nil || got.Code != 0 || got.Overflow || printed == "" || strings.ContainsAny(printed, "\r\n") {
			reasons = append(reasons, fmt.Sprintf("`%s -print-prog-name=%s` did not name one program", cc, helper))
			continue
		}
		path := printed
		switch {
		case !strings.ContainsRune(path, '/'):
			if path = findOnPath(env, path); path == "" {
				continue // The compiler cannot run what it cannot find.
			}
		case !filepath.IsAbs(path):
			path = filepath.Join(dir, path)
		}
		if _, err := trustedFile(path); err != nil {
			reasons = append(reasons, fmt.Sprintf("the %s that %s runs, %s, is not one that only root or another user can change: %v", helper, cc, path, err))
		}
	}
	return reasons
}

// xcodeSelectLink is where xcode-select, which only root can run with -s,
// records the developer directory that /usr/bin/clang and the other xcrun
// shims run from.
const xcodeSelectLink = "/var/db/xcode_select_link"

// judgeDeveloperDir returns why DEVELOPER_DIR, which makes macOS's xcrun
// shims run the toolchain of another developer directory, does not pass, or
// "" when it is empty, cw is not on macOS, or it names the directory that
// xcode-select chose.
func judgeDeveloperDir(value string) string {
	if value == "" || runtime.GOOS != "darwin" {
		return ""
	}
	selected, err := filepath.EvalSymlinks(xcodeSelectLink)
	if named, namedErr := filepath.EvalSymlinks(value); err != nil || namedErr != nil || named != selected {
		return "DEVELOPER_DIR names a developer directory other than the one xcode-select chose"
	}
	return ""
}

// lastValue returns the value of the last entry for name in env, as a
// process started with env sees it, or "".
func lastValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		if variable, rest, ok := strings.Cut(entry, "="); ok && variable == name {
			value = rest
		}
	}
	return value
}

// fileSHA256 returns the hex SHA-256 of the regular file at path.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // G304: a compiler that trustedFile judged; it is read, not executed.
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
