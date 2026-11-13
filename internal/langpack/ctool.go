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
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// CToolchain is how ReadGoEnv judged the C toolchain of a go command with cgo
// on. go test -race and every cgo build compile and link C code with the CC
// that go env reports (the environment's, a go env file's or the default,
// gcc or clang, found on PATH), C++ and Fortran files with its CXX and FC,
// and .swig files with the swig on PATH, with the CGO_*FLAGS it reports; the
// compiler runs an assembler and a linker of its own choosing. Any of them,
// their flags or the variables that steer them can put code into a test
// binary that reports a pass without running a test, so the Go rule credits
// a run with cgo on only when Unverified is empty (GoEnvWithheld).
type CToolchain struct {
	// CGO is whether go env reported CGO_ENABLED=1. With cgo off, go builds
	// no C code, and nothing else is judged.
	CGO bool `json:"cgo"`
	// CC, CXX and FC are the C, C++ and Fortran compilers the run was given,
	// by the path found for go env's CC, CXX and FC (gfortran when empty),
	// and SWIG the swig on PATH; nil when not found. Only CC must be found:
	// a go command that cannot find the others builds no C++, Fortran or
	// SWIG, and the run is given a path for CXX and FC that does not exist.
	// An FC that does not verify is not recorded and is replaced by that
	// path too, so a package with Fortran files fails to build instead of
	// every cgo run losing credit to a Fortran compiler it does not use.
	CC   *CTool `json:"cc,omitempty"`
	CXX  *CTool `json:"cxx,omitempty"`
	FC   *CTool `json:"fc,omitempty"`
	SWIG *CTool `json:"swig,omitempty"`
	// Unverified says why cw could not verify the toolchain; empty when it
	// did or cgo was off.
	Unverified string `json:"unverified,omitempty"`
}

// CTool is a C toolchain program a check run was given.
type CTool struct {
	// Path is the program as found on PATH or named by an absolute path,
	// before links are resolved, since a compiler driver such as clang++
	// reads the name it was started by. trustedFile judged this path as the
	// kernel resolves it.
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

// compilerLaunchers are programs that stand in for a compiler, often from a
// root-owned directory of links named gcc or cc, and run a compiler that
// their configuration chooses (ccache.conf, CCACHE_PREFIX, ~/.distcc/hosts)
// or serve objects from a cache the user can write.
var compilerLaunchers = []string{"ccache", "sccache", "distcc", "distcc-pump", "icecc", "buildcache"}

// missingTool is the start of the path a run is given for a compiler that
// was not found: no such file exists, and only root could create one.
const missingTool = "/nonexistent/cw-found-no-"

var (
	// hostOS is runtime.GOOS; a test replaces it to take another system's
	// branch.
	hostOS = runtime.GOOS
	// uidMapFile is the user namespace's ID map on Linux.
	uidMapFile = "/proc/self/uid_map"
	// runHelpers is judgeHelpers; a test replaces it to see whether judgeC
	// asks the compiler for its assembler and linker.
	runHelpers = judgeHelpers
)

// judgeC judges the C toolchain that the go command which printed values runs
// in dir with env (CToolchain), and returns pin: CGO_ENABLED as read and,
// with cgo on, the CGO_*FLAGS as read (a space for none), the compilers by
// the paths judged (a path that does not exist for CXX and FC not found) and,
// when the compiler finds its assembler or linker on PATH, COMPILER_PATH
// naming the directories judged. Appended to the run's environment, they win
// over a go env file read again by the run and over a later PATH lookup.
// judgeC runs a compiler, to ask which assembler and linker it uses, only
// once that compiler and its environment passed.
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
		if name, value, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "DYLD_") && value != "" && !slices.Contains(reasons, name+" is set") {
			reasons = append(reasons, name+" is set")
		}
	}
	if reason := judgeDeveloperDir(hostOS, lastValue(env, "DEVELOPER_DIR"), xcodeSelectLink); reason != "" {
		reasons = append(reasons, reason)
	}
	if hostOS == "linux" {
		if reason := judgeUserNamespace(uidMapFile); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	var reason string
	if judged.CC, reason = findCompiler("CC", printed["CC"], env); judged.CC == nil || reason != "" {
		reasons = append(reasons, cmp.Or(reason, fmt.Sprintf("CC %s was not found on PATH", quoteLimited(printed["CC"]))))
	}
	if judged.CXX, reason = findCompiler("CXX", printed["CXX"], env); reason != "" {
		reasons = append(reasons, reason)
	}
	if judged.FC, reason = findCompiler("FC", cmp.Or(printed["FC"], "gfortran"), env); reason != "" {
		judged.FC = nil
	}
	if path := findOnPath(env, "swig"); path != "" {
		if judged.SWIG, reason = judgeProgram("the swig", path); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	for _, tool := range []struct {
		name  string
		found *CTool
	}{{"CC", judged.CC}, {"CXX", judged.CXX}, {"FC", judged.FC}} {
		switch {
		case tool.found != nil:
			pin = append(pin, tool.name+"="+tool.found.Path)
		case tool.name != "CC":
			pin = append(pin, tool.name+"="+missingTool+tool.name)
		}
	}
	if len(reasons) == 0 && hostOS != "darwin" {
		helperReasons, dirs := runHelpers(ctx, judged.CC.Path, dir, env)
		reasons = append(reasons, helperReasons...)
		if len(dirs) > 0 {
			pin = append(pin, "COMPILER_PATH="+strings.Join(dirs, string(filepath.ListSeparator)))
		}
	}
	if len(reasons) > 0 {
		judged.Unverified = "cw could not verify its C toolchain: " + strings.Join(reasons, "; ")
	}
	return judged, pin
}

// findCompiler finds the compiler that go env's value of name (CC, CXX or
// FC) names, as cmd/go would, and judges it (judgeProgram). value must be
// one program without arguments or quotes, named bare, which is looked up in
// env's PATH, or by an absolute path: cmd/go runs a compiler from each
// package's directory, so a relative path names a different file for each.
// It returns nil and no reason when a bare name is not on PATH, and a reason
// when value or the program found does not pass.
func findCompiler(name, value string, env []string) (*CTool, string) {
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
		return nil, fmt.Sprintf("%s is %s, a relative path, which cmd/go takes in each package's directory", name, quoteLimited(value))
	}
	return judgeProgram("the "+name+" compiler", path)
}

// judgeProgram judges the toolchain program at the absolute path, which the
// reasons call what: it must not be a compiler launcher, by the name it is
// started by or the file it resolves to, and must pass trustedFile.
func judgeProgram(what, path string) (*CTool, string) {
	tool := &CTool{Path: path}
	resolved, _ := filepath.EvalSymlinks(path)
	for _, launcher := range compilerLaunchers {
		if filepath.Base(path) == launcher || resolved != "" && filepath.Base(resolved) == launcher {
			return tool, fmt.Sprintf("%s %s is the compiler launcher %s, which runs a compiler its configuration chooses or serves objects from a cache the user can write", what, path, launcher)
		}
	}
	resolved, err := trustedFile(path)
	if err != nil {
		return tool, fmt.Sprintf("%s %s is not one that only root or another user can change: %v", what, path, err)
	}
	sum, err := fileSHA256(resolved)
	if err != nil {
		return tool, fmt.Sprintf("%s %s cannot be read: %v", what, path, err)
	}
	tool.SHA256 = sum
	return tool, ""
}

// judgeHelpers asks the compiler at cc, already judged, which assembler and
// linker it runs (`-print-prog-name`, which gcc and clang answer) and judges
// each with trustedFile. A compiler that does not find one in its own
// directories runs the first on PATH, as gcc searches it: an empty entry is
// the directory it runs in and a relative entry is taken there (gccLookup).
// dirs are the directories of the helpers found that way, which the run is
// given as COMPILER_PATH, so that a later PATH search cannot pick another.
func judgeHelpers(ctx context.Context, cc, dir string, env []string) (reasons, dirs []string) {
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
			if path = gccLookup(env, dir, path); path == "" {
				continue // The compiler cannot run what it cannot find.
			}
			if !slices.Contains(dirs, filepath.Dir(path)) {
				dirs = append(dirs, filepath.Dir(path))
			}
		case !filepath.IsAbs(path):
			path = filepath.Join(dir, path)
		}
		if _, err := trustedFile(path); err != nil {
			reasons = append(reasons, fmt.Sprintf("the %s that %s runs, %s, is not one that only root or another user can change: %v", helper, cc, path, err))
		}
	}
	return reasons, dirs
}

// gccLookup returns the first executable regular file named name in env's
// PATH as gcc searches it from dir: an empty entry is dir itself and a
// relative entry is taken in dir. "" when there is none.
func gccLookup(env []string, dir, name string) string {
	list, found := "", false
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			list, found = value, true
		}
	}
	if !found {
		list = os.Getenv("PATH")
	}
	for _, entry := range filepath.SplitList(list) {
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(dir, entry)
		}
		candidate := filepath.Join(entry, name)
		info, err := os.Stat(candidate) //nolint:gosec // G703: a PATH lookup stats PATH entries by design.
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// judgeUserNamespace returns why file owners do not show who can change a
// file when the user namespace's ID map, read from file, is not the
// identity map of the initial namespace: in a namespace such as
// `bwrap --unshare-user --uid 0` the user's own files appear owned by root.
// A real root container keeps the identity map.
func judgeUserNamespace(file string) string {
	data, err := os.ReadFile(file) //nolint:gosec // G304: /proc/self/uid_map, or a test's stand-in.
	if err != nil {
		return fmt.Sprintf("cw cannot read its user namespace's ID map %s: %v", file, err)
	}
	if fields := strings.Fields(string(data)); !slices.Equal(fields, []string{"0", "0", "4294967295"}) {
		return fmt.Sprintf("cw runs in a user namespace that maps user IDs (%s %s), where a file's owner does not show who can change it", file, quoteLimited(strings.TrimSpace(string(data))))
	}
	return ""
}

// xcodeSelectLink is where xcode-select, which only root can run with -s,
// records the developer directory that /usr/bin/clang and the other xcrun
// shims run from.
const xcodeSelectLink = "/var/db/xcode_select_link"

// judgeDeveloperDir returns why DEVELOPER_DIR (value), which makes macOS's
// xcrun shims run the toolchain of another developer directory, does not
// pass, or "" when it is empty, goos is not darwin, or it resolves to the
// directory that xcode-select chose, recorded at selectLink.
func judgeDeveloperDir(goos, value, selectLink string) string {
	if value == "" || goos != "darwin" {
		return ""
	}
	selected, err := filepath.EvalSymlinks(selectLink)
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
