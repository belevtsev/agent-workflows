//go:build unix

package langpack_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// ctEnv is a minimal environment with the real go and the system's
// directories on PATH, cgo on and no go env file, followed by overrides.
func ctEnv(t *testing.T, overrides ...string) []string {
	t.Helper()
	return append([]string{"HOME=" + t.TempDir(), "PATH=" + gtRealGo(t) + ":/usr/bin:/bin", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOENV=off", "CGO_ENABLED=1"}, overrides...)
}

// The C toolchain a go command with cgo on runs. Oracle (F18): cw verifies
// a C compiler only when no step of its path can be changed by the user cw
// runs as, as for the system's gcc or clang in /usr/bin; a wrapper in a
// directory the user owns, named by CC, by a go env file or first on PATH, a
// link there to the system compiler, a CC with arguments, CGO_*FLAGS beyond
// -O and -g, and variables that steer the compiler or the loader, each leave
// the toolchain unverified, so GoEnvWithheld withholds credit and names why;
// the run is pinned to the compiler judged. With cgo off nothing is judged
// and nothing is withheld.
func TestReadGoEnvVerifiesOnlyACToolchainThatOnlyRootCanChange(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, ctEnv(t))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	read := func(t *testing.T, env []string) (langpack.GoEnv, []string) {
		t.Helper()
		got, pin, _, err := langpack.ReadGoEnv(t.Context(), tool, dir, env)
		if err != nil || got.C == nil {
			t.Fatalf("ReadGoEnv = %+v, %v; want a judged C toolchain", got, err)
		}
		return got, pin
	}
	system, pin := read(t, ctEnv(t))
	if system.C.CC == nil || !system.C.CGO || system.C.Unverified != "" || len(system.C.CC.SHA256) != 64 || langpack.GoEnvWithheld(&system) != "" {
		t.Fatalf("the system C compiler: C = %+v (CC %+v), withheld %q; want cgo on, a hashed CC and nothing unverified", system.C, system.C.CC, langpack.GoEnvWithheld(&system))
	}
	cc := system.C.CC.Path
	for _, entry := range []string{"CGO_ENABLED=1", "CGO_CFLAGS=-O2 -g", "CGO_CPPFLAGS= ", "CC=" + cc} {
		if !slices.Contains(pin, entry) {
			t.Errorf("pin %q lacks %q", pin, entry)
		}
	}
	if byPath, _ := read(t, ctEnv(t, "CC="+cc)); byPath.C.Unverified != "" {
		t.Errorf("the system C compiler named by its path is unverified: %s", byPath.C.Unverified)
	}

	users := t.TempDir()
	wrapper := gtScript(t, users, filepath.Base(cc), "exec '"+cc+"' \"$@\"\n")
	link := filepath.Join(users, "link-cc")
	if err := os.Symlink(cc, link); err != nil {
		t.Fatal(err)
	}
	// dyld ends a process whose inserted library does not load, go env
	// included, so on macOS the library is a real one the user built, as an
	// attack's would be. Other systems ignore the variable.
	dylib := filepath.Join(users, "x.dylib")
	if runtime.GOOS == "darwin" {
		src := filepath.Join(users, "x.c")
		if err := os.WriteFile(src, []byte("int cw_inserted;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(t.Context(), cc, "-dynamiclib", "-o", dylib, src).CombinedOutput(); err != nil {
			t.Fatalf("precondition: building a dylib with %s: %v\n%s", cc, err, out)
		}
	}
	goEnvFile := filepath.Join(users, "env")
	if err := os.WriteFile(goEnvFile, []byte("CC="+wrapper+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, want string
		env        []string
	}{
		{"a wrapper named by CC", "the CC compiler " + wrapper + " is not one that only root or another user can change", []string{"CC=" + wrapper}},
		{"a wrapper first on PATH", "the CC compiler " + wrapper + " is not one", []string{"PATH=" + users + ":" + gtRealGo(t) + ":/usr/bin:/bin"}},
		{"a wrapper in a go env file", "the CC compiler " + wrapper + " is not one", []string{"GOENV=" + goEnvFile}},
		{"a link in a user's directory to the system compiler", "the CC compiler " + link + " is not one", []string{"CC=" + link}},
		{"a CC with an argument", "CC is \"" + cc + " -B" + users + "\", not one program without arguments", []string{"CC=" + cc + " -B" + users}},
		{"a CGO_CFLAGS that picks the compiler's programs", "CGO_CFLAGS holds \"-B" + users + "\"", []string{"CGO_CFLAGS=-O2 -g -B" + users}},
		{"a CGO_LDFLAGS that passes linker options", "CGO_LDFLAGS holds \"-Wl,--wrap=main\"", []string{"CGO_LDFLAGS=-Wl,--wrap=main"}},
		{"LD_PRELOAD", "LD_PRELOAD is set", []string{"LD_PRELOAD=" + filepath.Join(users, "x.so")}},
		{"CPATH", "CPATH is set", []string{"CPATH=" + users}},
		{"COMPILER_PATH", "COMPILER_PATH is set", []string{"COMPILER_PATH=" + users}},
		{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES is set", []string{"DYLD_INSERT_LIBRARIES=" + dylib}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, _ := read(t, ctEnv(t, c.env...))
			withheld := langpack.GoEnvWithheld(&got)
			if !got.C.CGO || !strings.Contains(got.C.Unverified, c.want) || !strings.HasPrefix(withheld, "cgo was on, and cw could not verify its C toolchain: ") || !strings.Contains(withheld, c.want) {
				t.Fatalf("C = %+v, withheld %q; want cgo on and %q", got.C, withheld, c.want)
			}
		})
	}
	off, pin := read(t, ctEnv(t, "CGO_ENABLED=0", "CC="+wrapper))
	if *off.C != (langpack.CToolchain{}) || langpack.GoEnvWithheld(&off) != "" || pin[len(pin)-1] != "CGO_ENABLED=0" {
		t.Errorf("cgo off: C = %+v, pin %q, withheld %q; want nothing judged, CGO_ENABLED=0 pinned and nothing withheld", off.C, pin, langpack.GoEnvWithheld(&off))
	}
}

// A compiler that finds no assembler or linker in its own directories runs
// the first on PATH. Oracle (F18): judgeHelpers asks the compiler which it
// runs and leaves a reason for one, found on PATH or named by its path, in a
// directory the user owns, and for a compiler that does not answer; the
// system's own, or none found, leave none. Review finding 6 on #181: gcc
// takes an empty PATH entry as the directory it runs in and a relative entry
// in it, so a helper found there is judged too, and the directories of the
// helpers found on PATH are returned for the run's COMPILER_PATH.
func TestJudgeHelpersRefusesAnAssemblerOrLinkerTheUserCanChange(t *testing.T) {
	work := t.TempDir()
	gtScript(t, work, "as", "exit 0\n")
	gtScript(t, filepath.Join(work, "sub"), "ld", "exit 0\n")
	var system []string
	for _, helper := range []string{"/usr/bin/as", "/usr/bin/ld"} {
		if _, err := os.Stat(helper); err == nil && !slices.Contains(system, filepath.Dir(helper)) {
			system = append(system, filepath.Dir(helper))
		}
	}
	users := t.TempDir()
	gtScript(t, users, "as", "exit 0\n")
	ld := gtScript(t, users, "ld", "exit 0\n")
	bare := gtScript(t, t.TempDir(), "cc", "case \"$1\" in -print-prog-name=as) echo as ;; *) echo ld ;; esac\n")
	named := gtScript(t, t.TempDir(), "cc", "case \"$1\" in -print-prog-name=as) echo as ;; *) echo '"+ld+"' ;; esac\n")
	failing := gtScript(t, t.TempDir(), "cc", "exit 1\n")
	for _, c := range []struct {
		name, cc, path string
		want           []string
	}{
		{"the system's", bare, "/usr/bin:/bin", nil},
		{"an empty PATH entry and a relative one", bare, ":sub:/usr/bin:/bin", []string{"the as that " + bare + " runs, " + filepath.Join(work, "as"), "the ld that " + bare + " runs, " + filepath.Join(work, "sub", "ld")}},
		{"an assembler and a linker first on PATH", bare, users + ":/usr/bin:/bin", []string{"the as that " + bare + " runs, " + filepath.Join(users, "as"), "the ld that " + bare + " runs, " + ld}},
		{"a linker named by its path", named, "/usr/bin:/bin", []string{"the ld that " + named + " runs, " + ld}},
		{"a compiler that does not answer", failing, "/usr/bin:/bin", []string{"`" + failing + " -print-prog-name=as` did not name one program", "`" + failing + " -print-prog-name=ld` did not name one program"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, dirs := langpack.JudgeHelpers(t.Context(), c.cc, work, []string{"PATH=" + c.path})
			if c.want == nil && c.cc == bare && !slices.Equal(dirs, system) {
				t.Errorf("JudgeHelpers directories = %q, want %q", dirs, system)
			}
			if len(got) != len(c.want) {
				t.Fatalf("JudgeHelpers = %q, want reasons starting %q", got, c.want)
			}
			for i, want := range c.want {
				if !strings.HasPrefix(got[i], want) {
					t.Errorf("reason %q does not start %q", got[i], want)
				}
			}
		})
	}
}

// Review findings 3, 7 and 9 on #181: the branches that depend on the
// system. Oracles: on Linux, a user namespace whose ID map is not the
// identity map (bwrap --unshare-user --uid 0 shows the user's files as
// root's) leaves the toolchain unverified, and the identity map passes, after
// which cw asks the compiler for its assembler and linker; on macOS the ID
// map is not read and the compiler is not asked (its toolchain lives in the
// developer directory, a documented gap), and a DEVELOPER_DIR other than the
// one xcode-select chose leaves the toolchain unverified.
func TestReadGoEnvJudgesTheSystemSpecificSteps(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, ctEnv(t))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	maps := t.TempDir()
	identity := filepath.Join(maps, "identity")
	mapped := filepath.Join(maps, "mapped")
	for file, data := range map[string]string{identity: "         0          0 4294967295\n", mapped: "         0       1000          1\n"} {
		if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unverified := func(t *testing.T, env []string) string {
		t.Helper()
		got, _, _, err := langpack.ReadGoEnv(t.Context(), tool, dir, env)
		if err != nil || got.C == nil || !got.C.CGO {
			t.Fatalf("ReadGoEnv = %+v, %v; want a judged C toolchain with cgo on", got, err)
		}
		return got.C.Unverified
	}
	asked := langpack.PretendHost(t, "linux", mapped)
	if got := unverified(t, ctEnv(t)); !strings.Contains(got, "cw runs in a user namespace that maps user IDs") || *asked {
		t.Errorf("Linux in a mapped user namespace: unverified %q, helpers asked %v; want the namespace named and no compiler run", got, *asked)
	}
	asked = langpack.PretendHost(t, "linux", identity)
	if got := unverified(t, ctEnv(t)); strings.Contains(got, "user namespace") || !*asked {
		t.Errorf("Linux with the identity map: unverified %q, helpers asked %v; want no namespace reason and the compiler asked", got, *asked)
	}
	asked = langpack.PretendHost(t, "darwin", mapped)
	langpack.PretendDeveloperDir(t, "/usr", nil)
	if got := unverified(t, ctEnv(t)); got != "" || *asked {
		t.Errorf("macOS: unverified %q, helpers asked %v; want verified without reading the ID map or asking the compiler", got, *asked)
	}
	if got := unverified(t, ctEnv(t, "DEVELOPER_DIR="+maps)); !strings.Contains(got, "DEVELOPER_DIR names a developer directory other than the one xcode-select chose") {
		t.Errorf("macOS with another DEVELOPER_DIR: unverified %q", got)
	}

	selected := t.TempDir()
	link := filepath.Join(t.TempDir(), "xcode_select_link")
	if err := os.Symlink(selected, link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, goos, value string
		refused           bool
	}{
		{"unset", "darwin", "", false},
		{"the selected directory", "darwin", selected, false},
		{"another directory", "darwin", maps, true},
		{"a directory that does not exist", "darwin", filepath.Join(maps, "none"), true},
		{"a relative path", "darwin", "Xcode.app/Contents/Developer", true},
		{"the selected directory after a space", "darwin", " " + selected, true},
		{"not macOS", "linux", maps, false},
	} {
		if got := langpack.JudgeDeveloperDir(c.goos, c.value, link); (got != "") != c.refused {
			t.Errorf("%s: JudgeDeveloperDir = %q, want refused %v", c.name, got, c.refused)
		}
	}
}

// The macOS developer directory (F23, #181 review finding 7, and the review
// of #198): the /usr/bin/clang shim runs the clang of the directory
// xcode-select -p prints, found through a per-user lookup cache. Oracles: on
// macOS the run's pin turns xcrun's cache off (xcrun_nocache=1); an active
// developer directory that only root can change, such as /usr, leaves the
// toolchain verified, is recorded as developer_dir and is pinned as
// DEVELOPER_DIR; an active one the user owns, as a dragged-in Xcode.app is,
// or one under /Applications, which the admin group can write, gives way to
// the Command Line Tools directory when that passes, which is then recorded
// and pinned instead, so the run's key differs; when neither passes the
// toolchain is unverified, the reason naming both directories and how to earn
// credit, and nothing is pinned; an xcode-select that cannot print leaves it
// unverified, naming why; a DEVELOPER_DIR that is not an absolute path, such
// as one starting with a space that a trimmed reading would take for /usr,
// is refused; other systems neither pin nor record a developer directory.
func TestReadGoEnvJudgesTheMacOSDeveloperDirectory(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, ctEnv(t))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	identity := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(identity, []byte("0 0 4294967295\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	judge := func(t *testing.T, overrides ...string) (*langpack.CToolchain, []string, string) {
		t.Helper()
		got, pin, config, err := langpack.ReadGoEnv(t.Context(), tool, dir, ctEnv(t, overrides...))
		if err != nil || got.C == nil || !got.C.CGO {
			t.Fatalf("ReadGoEnv = %+v, %v; want a judged C toolchain with cgo on", got, err)
		}
		return got.C, pin, langpack.GoEnvKey([]byte("secret"), config)
	}
	pinned := func(pin []string) []string {
		var values []string
		for _, entry := range pin {
			if value, ok := strings.CutPrefix(entry, "DEVELOPER_DIR="); ok {
				values = append(values, value)
			}
		}
		return values
	}
	owned := t.TempDir()
	langpack.PretendHost(t, "darwin", identity)

	langpack.PretendCommandLineTools(t, owned)
	langpack.PretendDeveloperDir(t, "/usr", nil)
	active, pin, activeKey := judge(t)
	if active.Unverified != "" || active.DeveloperDir != "/usr" || !slices.Contains(pin, "xcrun_nocache=1") || !slices.Equal(pinned(pin), []string{"/usr"}) {
		t.Errorf("macOS, root's active developer directory: C = %+v, pin %q; want verified, developer_dir /usr, xcrun_nocache=1 and DEVELOPER_DIR=/usr pinned", active, pin)
	}

	langpack.PretendCommandLineTools(t, "/usr/bin")
	langpack.PretendDeveloperDir(t, owned, nil)
	fallback, pin, fallbackKey := judge(t)
	if fallback.Unverified != "" || fallback.DeveloperDir != "/usr/bin" || !slices.Equal(pinned(pin), []string{"/usr/bin"}) {
		t.Errorf("macOS, an active directory the user owns and root's Command Line Tools: C = %+v, pin %q; want verified, developer_dir /usr/bin and DEVELOPER_DIR=/usr/bin pinned", fallback, pin)
	}
	if fallbackKey == activeKey {
		t.Errorf("macOS: the run key is the same for developer directories /usr and /usr/bin; want them told apart")
	}

	tools := t.TempDir()
	langpack.PretendCommandLineTools(t, tools)
	neither, pin, _ := judge(t)
	for _, want := range []string{"the active developer directory " + owned + " (", "the Command Line Tools directory " + tools + " (", "install the Command Line Tools (xcode-select --install) or select a developer directory that only root can change"} {
		if !strings.Contains(neither.Unverified, want) {
			t.Errorf("macOS, neither directory passes: unverified %q, want it to hold %q", neither.Unverified, want)
		}
	}
	if neither.DeveloperDir != "" || len(pinned(pin)) != 0 {
		t.Errorf("macOS, neither directory passes: developer_dir %q, pin %q; want none recorded or pinned", neither.DeveloperDir, pin)
	}

	langpack.PretendCommandLineTools(t, "/usr/bin")
	langpack.PretendDeveloperDir(t, "", errors.New("xcode-select failed"))
	if failed, _, _ := judge(t); !strings.Contains(failed.Unverified, "cw cannot read the active developer directory: xcode-select failed") {
		t.Errorf("macOS, xcode-select -p failing: unverified %q, want the failure named", failed.Unverified)
	}

	langpack.PretendDeveloperDir(t, "/usr", nil)
	for _, value := range []string{"Xcode.app/Contents/Developer", " /usr"} {
		if refused, _, _ := judge(t, "DEVELOPER_DIR="+value); !strings.Contains(refused.Unverified, fmt.Sprintf("DEVELOPER_DIR is %q, not an absolute path", value)) {
			t.Errorf("macOS, DEVELOPER_DIR=%q: unverified %q, want it refused as not absolute", value, refused.Unverified)
		}
	}

	langpack.PretendHost(t, "linux", identity)
	langpack.PretendDeveloperDir(t, owned, nil)
	if linux, pin, _ := judge(t); strings.Contains(linux.Unverified, "developer directory") || linux.DeveloperDir != "" || slices.Contains(pin, "xcrun_nocache=1") || len(pinned(pin)) != 0 {
		t.Errorf("Linux: C = %+v, pin %q; want no developer directory judged, recorded or pinned", linux, pin)
	}
}

// Review of #198: xcode-select -p is read as printed, less one final
// newline. Oracle: one absolute path on one line is returned as printed,
// with or without that newline; a failing run, no output, a relative path,
// a path that starts with a space (absolute only once trimmed) and two lines
// are each refused, naming the program.
func TestXcodeSelectPrintTakesOneAbsoluteLine(t *testing.T) {
	scripts := t.TempDir()
	for i, c := range []struct {
		name, body, want string
	}{
		{"one line", "printf '/Applications/Xcode.app/Contents/Developer\\n'\n", "/Applications/Xcode.app/Contents/Developer"},
		{"no final newline", "printf '/Library/Developer/CommandLineTools'\n", "/Library/Developer/CommandLineTools"},
		{"a trailing space kept", "printf '/usr \\n'\n", "/usr "},
		{"failing", "echo /usr; exit 1\n", ""},
		{"nothing printed", "exit 0\n", ""},
		{"a relative path", "echo Xcode.app/Contents/Developer\n", ""},
		{"a leading space", "echo ' /usr'\n", ""},
		{"two lines", "printf '/usr\\n/opt\\n'\n", ""},
		{"a carriage return", "printf '/usr\\r\\n'\n", ""},
	} {
		program := gtScript(t, filepath.Join(scripts, fmt.Sprint(i)), "xcode-select", c.body)
		got, err := langpack.XcodeSelectPrint(t.Context(), program, scripts, []string{"PATH=/usr/bin:/bin"})
		switch {
		case c.want != "" && (err != nil || got != c.want):
			t.Errorf("%s: XcodeSelectPrint = %q, %v; want %q", c.name, got, err, c.want)
		case c.want == "" && (err == nil || got != "" || !strings.Contains(err.Error(), "`"+program+" -p` did not print one absolute directory")):
			t.Errorf("%s: XcodeSelectPrint = %q, %v; want it refused, naming %s", c.name, got, err, program)
		}
	}
}
