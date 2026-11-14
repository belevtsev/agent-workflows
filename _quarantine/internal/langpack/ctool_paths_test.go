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

// Review findings 1, 2, 5 and 6 on #181: cw judges each C toolchain program
// as the kernel and cmd/go find it. Oracles:
//   - a CC through a link the user owns and "..": the kernel takes ".." from
//     the directory the link led to, so the program that runs is the user's,
//     and cw judges that walk, not the lexically cleaned /usr/bin path;
//   - a compiler launcher (ccache, by name or by the file a gcc link resolves
//     to) runs a compiler its configuration chooses, so it does not verify;
//   - an FC that does not verify, and a CXX not found, are replaced in the run
//     by a path that does not exist, so no Fortran or C++ builds, and the
//     rest still verifies;
//   - a swig the user can change, first on PATH, does not verify;
//   - a withheld run says how to earn credit again.
func TestReadGoEnvJudgesEachToolchainProgramAsTheKernelAndCmdGoFindIt(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, ctEnv(t))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	read := func(t *testing.T, env []string) (langpack.GoEnv, []string) {
		t.Helper()
		got, pin, _, err := langpack.ReadGoEnv(t.Context(), tool, dir, env)
		if err != nil || got.C == nil || !got.C.CGO {
			t.Fatalf("ReadGoEnv = %+v, %v; want a judged C toolchain with cgo on", got, err)
		}
		return got, pin
	}
	system, _ := read(t, ctEnv(t))
	if system.C.CC == nil || system.C.Unverified != "" {
		t.Fatalf("precondition: the system C compiler does not verify: %+v", system.C)
	}
	cc := system.C.CC.Path

	// lnk -> w/d/…/d, deep enough that lnk/.. repeated climbs from it back to
	// w, where w/<cc> is a wrapper, while the cleaned path is cc itself.
	w, users := t.TempDir(), t.TempDir()
	climb := len(strings.Split(strings.Trim(users, "/"), "/")) + 1
	deep := w
	for range climb {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	gtScript(t, filepath.Join(w, filepath.Dir(cc)), filepath.Base(cc), "exec '"+cc+"' \"$@\"\n")
	if err := os.Symlink(deep, filepath.Join(users, "lnk")); err != nil {
		t.Fatal(err)
	}
	dotdot := filepath.Join(users, "lnk") + strings.Repeat("/..", climb) + cc
	if filepath.Clean(dotdot) != cc {
		t.Fatalf("precondition: %s does not clean to %s", dotdot, cc)
	}
	launchers := t.TempDir()
	gtScript(t, launchers, "ccache", "exec \"$@\"\n")
	if err := os.Symlink("ccache", filepath.Join(launchers, "gcc")); err != nil {
		t.Fatal(err)
	}
	fc := gtScript(t, t.TempDir(), "myfc", "exit 0\n")
	swig := t.TempDir()
	gtScript(t, swig, "swig", "exit 0\n")

	for _, c := range []struct {
		name, unverified, pinned string
		env                      []string
	}{
		{"a CC through a user's link and ..", "the CC compiler " + dotdot + " is not one", "", []string{"CC=" + dotdot}},
		{"a ccache named by CC", "the CC compiler " + filepath.Join(launchers, "ccache") + " is the compiler launcher ccache", "", []string{"CC=" + filepath.Join(launchers, "ccache")}},
		{"a gcc link to ccache", "the CC compiler " + filepath.Join(launchers, "gcc") + " is the compiler launcher ccache", "", []string{"CC=" + filepath.Join(launchers, "gcc")}},
		{"an FC the user can change", "", "FC=/nonexistent/cw-found-no-FC", []string{"FC=" + fc}},
		{"a CXX not found", "", "CXX=/nonexistent/cw-found-no-CXX", []string{"CXX=cw-no-such-cxx"}},
		{"a swig first on PATH", "the swig " + filepath.Join(swig, "swig") + " is not one", "", []string{"PATH=" + swig + ":" + gtRealGo(t) + ":/usr/bin:/bin"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, pin := read(t, ctEnv(t, c.env...))
			if c.unverified != "" && !strings.Contains(got.C.Unverified, c.unverified) {
				t.Fatalf("C = %+v; want unverified with %q", got.C, c.unverified)
			}
			if withheld := langpack.GoEnvWithheld(&got); c.unverified != "" && !strings.Contains(withheld, "To earn credit, run the check again") {
				t.Errorf("withheld %q does not say how to earn credit", withheld)
			}
			if c.pinned != "" && (got.C.Unverified != "" || !slices.Contains(pin, c.pinned)) {
				t.Fatalf("C = %+v, pin %q; want nothing unverified and %q pinned", got.C, pin, c.pinned)
			}
		})
	}
}
