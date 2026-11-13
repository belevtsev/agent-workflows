//go:build unix

package langpack_test

import (
	"os"
	"path/filepath"
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
		{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES is set", []string{"DYLD_INSERT_LIBRARIES=" + filepath.Join(users, "x.dylib")}},
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
// system's own, or none found, leave none.
func TestJudgeHelpersRefusesAnAssemblerOrLinkerTheUserCanChange(t *testing.T) {
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
		{"an assembler and a linker first on PATH", bare, users + ":/usr/bin:/bin", []string{"the as that " + bare + " runs, " + filepath.Join(users, "as"), "the ld that " + bare + " runs, " + ld}},
		{"a linker named by its path", named, "/usr/bin:/bin", []string{"the ld that " + named + " runs, " + ld}},
		{"a compiler that does not answer", failing, "/usr/bin:/bin", []string{"`" + failing + " -print-prog-name=as` did not name one program", "`" + failing + " -print-prog-name=ld` did not name one program"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := langpack.JudgeHelpers(t.Context(), c.cc, t.TempDir(), []string{"PATH=" + c.path})
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
