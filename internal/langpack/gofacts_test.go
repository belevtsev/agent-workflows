package langpack_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// Independent tests for LoadGoFacts (P3a-2 interface contract, B16-B20, B24
// and J4), against real go and fake go scripts. Fake scripts are named only
// through env's PATH, except where a row sets the process PATH itself, and
// run only under langpack.TrustGoOnPath, as ResolveGo refuses them.

func gfPosix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake go scripts are POSIX shell")
	}
}

func gfRealGo(t *testing.T) {
	t.Helper()
	gfPosix(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
}

// gfEnv is the contract's test environment followed by the row's overrides.
func gfEnv(overrides ...string) []string {
	return append(append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "GOFLAGS="), overrides...)
}

// gfCtx has the 2-minute deadline that is the facts budget of most rows.
func gfCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func gfDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// gfModule writes files (slash paths) under a fresh resolved directory.
func gfModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := gfDir(t)
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
			t.Fatal("cannot write the module")
		}
	}
	return dir
}

func gfKeys(files map[string]string) []string {
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// gfFake writes an executable #!/bin/sh go into a fresh directory and returns
// the directory.
func gfFake(t *testing.T, body string) string {
	t.Helper()
	dir := gfDir(t)
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// gfRecord is a script line that appends the working directory, the
// arguments and GOPROXY, GOOS, GOARCH, GOFLAGS, GOROOT and GOTOOLCHAIN to file
// rec.
func gfRecord(rec string) string {
	return `printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$(/bin/pwd -P)" "$*" "${GOPROXY-unset}" "${GOOS-unset}" "${GOARCH-unset}" "${GOFLAGS-unset}" ` +
		`"${GOROOT-unset}" "${GOTOOLCHAIN-unset}" >> '` + rec + "'\n"
}

// gfLines returns the whitespace-separated tokens of a file (none when absent).
func gfLines(path string) []string {
	data, _ := os.ReadFile(path)
	return strings.Fields(string(data))
}

// gfPids returns a file for scripts to record the pids they leave sleeping;
// the test kills every recorded process at cleanup.
func gfPids(t *testing.T) string {
	file := filepath.Join(gfDir(t), "pids")
	t.Cleanup(func() {
		for _, f := range gfLines(file) {
			if pid, err := strconv.Atoi(f); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	return file
}

// gfSnap records every path under dir with its mode and content (or link target).
func gfSnap(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		v := info.Mode().String()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			v += " -> " + target
		case info.Mode().IsRegular():
			data, _ := os.ReadFile(p)
			v += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		snap[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// gfLoad calls LoadGoFacts and fails when the call wrote anything under dir (B19, J4).
func gfLoad(t *testing.T, ctx context.Context, dir string, env []string, src langpack.Source) langpack.GoFacts {
	t.Helper()
	before := gfSnap(t, dir)
	facts := langpack.LoadGoFacts(ctx, dir, env, src)
	if after := gfSnap(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("B19/J4 violated: LoadGoFacts changed files under dir:\nbefore %v\nafter  %v", before, after)
	}
	return facts
}

const gfGraphArgs = "-deps -test -json=ImportPath,Dir,Module,ForTest,Deps,Incomplete,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,IgnoredGoFiles,IgnoredOtherFiles ./..."

// B16: on a sound module the graph facts come from go list: test-only edges
// count, packages without tests are no key, other modules and the standard
// library never appear, and the root package of "module example.test" is ".".
func TestLoadGoFactsGraphB16(t *testing.T) {
	gfRealGo(t)
	files := map[string]string{
		"go.mod":       "module example.test\n\ngo 1.21\n",
		"root.go":      "package root\n",
		"root_test.go": "package root\n\nimport \"testing\"\n\nfunc TestRoot(t *testing.T) {}\n",
		"a/a.go":       "package a\n\nimport _ \"embed\"\n\n//go:embed hello.txt\nvar Hello string\n",
		"a/hello.txt":  "hi\n",
		"a/a_test.go": "package a\n\nimport (\n\t_ \"embed\"\n\t\"testing\"\n)\n\n//go:embed testdata/golden.txt\nvar golden string\n\n" +
			"func TestA(t *testing.T) { _ = golden + Hello }\n",
		"a/testdata/golden.txt": "g\n",
		"a/ign.go":              "//go:build ignore\n\npackage a\n",
		"a/x_windows.go":        "package a\n",
		"b/b.go":                "package b\n\nimport \"example.test/a\"\n\nvar B = a.Hello\n",
		"b/b_test.go":           "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = B }\n",
		"c/c.go":                "package c\n\nvar C = 1\n",
		"c/x.txt":               "x\n",
		"c/c_test.go": "package c_test\n\nimport (\n\t_ \"embed\"\n\t\"testing\"\n\n\t\"example.test/a\"\n\t\"example.test/c\"\n)\n\n" +
			"//go:embed x.txt\nvar x string\n\nfunc TestC(t *testing.T) { _ = x + a.Hello; _ = c.C }\n",
		"d/d.go":          "package d\n\nimport \"example.test/b\"\n\nvar D = b.B\n",
		"tools/go.mod":    "module example.test/tools\n\ngo 1.21\n",
		"tools/t.go":      "package tools\n\nimport _ \"embed\"\n\n//go:embed e.txt\nvar E string\n",
		"tools/e.txt":     "e\n",
		"tools/ign.go":    "//go:build ignore\n\npackage tools\n",
		"tools/t_test.go": "package tools\n\nimport \"testing\"\n\nfunc TestT(t *testing.T) { _ = E }\n",
	}
	dir := gfModule(t, files)
	got := gfLoad(t, gfCtx(t), dir, gfEnv(), langpack.Source{Current: gfKeys(files), Changed: []string{"a/a.go"}})
	want := langpack.GoFacts{
		Tested:   map[string][]string{".": {"."}, "a": {"a"}, "b": {"a", "b"}, "c": {"a", "c"}},
		Embedded: []string{"a/hello.txt", "a/testdata/golden.txt", "c/x.txt"},
		Excluded: []string{"a/ign.go", "a/x_windows.go"},
	}
	if got.GraphError != "" || !reflect.DeepEqual(got.Tested, want.Tested) || !slices.Equal(got.Embedded, want.Embedded) || !slices.Equal(got.Excluded, want.Excluded) {
		t.Errorf("B16 violated:\n got  Tested %v Embedded %q Excluded %q GraphError %q\n want Tested %v Embedded %q Excluded %q",
			got.Tested, got.Embedded, got.Excluded, got.GraphError, want.Tested, want.Embedded, want.Excluded)
	}

	// -mod=vendor when vendor/modules.txt is current: the vendored module is
	// readable only in vendor mode (GOPROXY=off), and never appears.
	vfiles := map[string]string{
		"go.mod":                        "module example.com/v\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n",
		"vendor/modules.txt":            "# example.com/dep v1.0.0\n## explicit\nexample.com/dep\n",
		"vendor/example.com/dep/dep.go": "package dep\n\nconst X = 1\n",
		"e/e.go":                        "package e\n\nimport \"example.com/dep\"\n\nconst E = dep.X\n",
		"e/e_test.go":                   "package e\n\nimport \"testing\"\n\nfunc TestE(t *testing.T) { _ = E }\n",
	}
	vdir := gfModule(t, vfiles)
	vgot := gfLoad(t, gfCtx(t), vdir, gfEnv(), langpack.Source{Current: gfKeys(vfiles), Changed: []string{"e/e.go"}})
	if vgot.GraphError != "" || !reflect.DeepEqual(vgot.Tested, map[string][]string{"e": {"e"}}) {
		t.Errorf("B16 violated (vendored module): Tested %v, GraphError %q; want map[e:[e]] and no error", vgot.Tested, vgot.GraphError)
	}
}

// B17: header facts list changed .go files outside testdata, read through
// dir without leaving it, and are computed even when go is not found.
func TestLoadGoFactsHeadersB17(t *testing.T) {
	gfPosix(t)
	gen := "// Code generated by gen. DO NOT EDIT.\n"
	files := map[string]string{
		"go.mod":          "module example.com/h\n\ngo 1.21\n",
		"p/both.go":       gen + "//go:build ignore\n\npackage p\n",
		"p/c1.go":         "//go:build ignore\n\npackage p\n",
		"p/c2.go":         "// +build linux\n\npackage p\n",
		"p/c3.go":         "package p\n\n//go:build ignore\n",
		"p/g1.go":         gen + "\npackage p\n",
		"p/g2.go":         "package p\n\n" + gen,
		"p/bad.go":        "//go:build ignore\n\npackage 1x\n",
		"p/testdata/t.go": "//go:build ignore\n\npackage t\n",
		"p/u.go":          "//go:build ignore\n\npackage p\n",
		"p/x.txt":         "//go:build ignore\n\npackage p\n",
	}
	dir := gfModule(t, files)
	outside := gfModule(t, map[string]string{"o.go": gen + "//go:build ignore\n\npackage o\n", "esc/e.go": gen + "//go:build ignore\n\npackage e\n"})
	if os.Symlink(filepath.Join(outside, "o.go"), filepath.Join(dir, "p", "link.go")) != nil || os.Symlink(filepath.Join(outside, "esc"), filepath.Join(dir, "esc")) != nil {
		t.Fatal("cannot create symlinks")
	}
	changed := []string{"esc/e.go", "p/bad.go", "p/both.go", "p/c1.go", "p/c2.go", "p/c3.go", "p/g1.go", "p/g2.go", "p/gone.go", "p/link.go", "p/testdata/t.go", "p/x.txt"}
	got := gfLoad(t, gfCtx(t), dir, gfEnv("PATH="+gfDir(t)), langpack.Source{Current: append(gfKeys(files), "p/link.go", "esc/e.go"), Changed: changed})
	if want := []string{"p/both.go", "p/c1.go", "p/c2.go"}; !slices.Equal(got.Constrained, want) {
		t.Errorf("B17 violated: Constrained %q, want %q", got.Constrained, want)
	}
	if want := []string{"p/both.go", "p/g1.go"}; !slices.Equal(got.Generated, want) {
		t.Errorf("B17 violated: Generated %q, want %q", got.Generated, want)
	}
	if !strings.Contains(got.GraphError, "go was not found on PATH") {
		t.Errorf("B17/B24 violated: GraphError %q, want it to contain %q", got.GraphError, "go was not found on PATH")
	}
}

// B18 with real go: verify's own GOOS, GOARCH and GOFLAGS, and those of the go
// environment file, never waive -race; CGO_ENABLED reaches the probe.
func TestLoadGoFactsRaceProbeRealGoB18(t *testing.T) {
	gfRealGo(t)
	probe := exec.Command("go", "list", "-race", "runtime")
	probe.Env = gfEnv()
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("go list -race runtime fails here: %v %s", err, out)
	}
	files := map[string]string{"go.mod": "module example.com/r\n\ngo 1.21\n", "a/a.go": "package a\n"}
	dir := gfModule(t, files)
	goenv := filepath.Join(gfDir(t), "go.env")
	if os.WriteFile(goenv, []byte("GOOS=linux\nGOARCH=386\nGOFLAGS=-race=bogus\n"), 0o644) != nil {
		t.Fatal("cannot write the go environment file")
	}
	rows := []struct {
		name      string
		overrides []string
		want      string
	}{
		{"cross GOOS/GOARCH", []string{"GOOS=linux", "GOARCH=386"}, ""},
		{"windows/arm64", []string{"GOOS=windows", "GOARCH=arm64"}, ""},
		{"GOFLAGS=-race=false", []string{"GOFLAGS=-race=false"}, ""},
		{"GOFLAGS=-race=bogus", []string{"GOFLAGS=-race=bogus"}, ""},
		{"go environment file", []string{"GOENV=" + goenv}, ""},
		{"go not found", []string{"PATH=" + gfDir(t)}, ""},
	}
	switch runtime.GOOS {
	case "darwin":
		rows = append(rows, struct {
			name      string
			overrides []string
			want      string
		}{"CGO_ENABLED=0 on darwin", []string{"CGO_ENABLED=0"}, ""})
	case "linux":
		rows = append(rows, struct {
			name      string
			overrides []string
			want      string
		}{"CGO_ENABLED=0 on linux", []string{"CGO_ENABLED=0"}, "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1"})
	}
	for _, r := range rows {
		got := gfLoad(t, gfCtx(t), dir, gfEnv(r.overrides...), langpack.Source{Current: gfKeys(files), Changed: []string{"a/a.go"}})
		if got.NoRace != r.want {
			t.Errorf("B18 violated (%s): NoRace %q, want %q", r.name, got.NoRace, r.want)
		}
	}
}

// B18 with a fake go: only exit status 2 with a known first line of standard
// error means -race is unavailable.
func TestLoadGoFactsRaceProbeFakeB18(t *testing.T) {
	gfPosix(t)
	langpack.TrustGoOnPath(t)
	dir := gfModule(t, map[string]string{"go.mod": "module example.com/r\n\ngo 1.21\n", "a/a.go": "package a\n"})
	for _, r := range []struct{ stderr, code, want string }{
		{"-race is not supported on plan9/amd64", "2", "-race is not supported on plan9/amd64"},
		{"go: -race requires cgo", "2", "go: -race requires cgo"},
		{"go: -race requires cgo; enable cgo by setting CGO_ENABLED=1", "2", "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1"},
		{`go: invalid boolean value "bogus" for flag -race (from $GOFLAGS): parse error`, "2", ""},
		{"-race is not supported on plan9/amd64", "1", ""},
		{"  -race is not supported on plan9/amd64  \nsecond line", "2", "-race is not supported on plan9/amd64"},
		{"warning: first\n-race is not supported on plan9/amd64", "2", ""},
	} {
		fake := gfFake(t, "case \" $* \" in *\" -race \"*) printf '%s\\n' '"+r.stderr+"' >&2; exit "+r.code+";; esac\nexit 0\n")
		got := gfLoad(t, gfCtx(t), dir, gfEnv("PATH="+fake), langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}})
		if got.NoRace != r.want {
			t.Errorf("B18 violated: probe stderr %q exit %s gives NoRace %q, want %q", r.stderr, r.code, got.NoRace, r.want)
		}
	}
}

// B19: a broken graph is unavailable, not partial; -mod=mod in GOFLAGS never
// rewrites the module; no file under dir changes (checked by gfLoad).
func TestLoadGoFactsFailuresB19(t *testing.T) {
	gfRealGo(t)
	mod := "module example.com/f\n\ngo 1.21\n"
	test := "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"
	for _, r := range []struct {
		name     string
		files    map[string]string
		env      []string
		graphErr string // "" none, "*" any, else a substring
	}{
		{"go not found", map[string]string{"go.mod": mod, "a/a.go": "package a\n"}, []string{"PATH=" + gfDir(t)}, "go was not found on PATH"},
		{"missing requirement", map[string]string{"go.mod": mod, "a/a.go": "package a\n\nimport _ \"example.com/missing/x\"\n"}, nil, "*"},
		{"missing requirement with GOFLAGS=-e", map[string]string{"go.mod": mod, "a/a.go": "package a\n\nimport _ \"example.com/missing/x\"\n"}, []string{"GOFLAGS=-e"}, "*"},
		{"import block does not parse", map[string]string{"go.mod": mod, "a/a.go": "package a\n\nimport (\n\t\"fmt\"\n", "a/a_test.go": test}, nil, "*"},
		{"syntax error after the imports", map[string]string{"go.mod": mod, "a/a.go": "package a\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n\nfunc F( {\n", "a/a_test.go": test}, nil, ""},
		{"GOFLAGS=-mod=mod with a missing requirement", map[string]string{"go.mod": mod, "go.sum": "", "a/a.go": "package a\n\nimport _ \"example.com/missing/x\"\n"},
			[]string{"GOFLAGS=-mod=mod"}, "*"},
		{"GOFLAGS=-mod=mod without a go directive", map[string]string{"go.mod": "module example.com/f\n", "a/a.go": "package a\n", "a/a_test.go": test}, []string{"GOFLAGS=-mod=mod"}, "-"},
	} {
		dir := gfModule(t, r.files)
		got := gfLoad(t, gfCtx(t), dir, gfEnv(r.env...), langpack.Source{Current: gfKeys(r.files), Changed: []string{"a/a.go"}})
		switch r.graphErr {
		case "":
			if got.GraphError != "" || !reflect.DeepEqual(got.Tested, map[string][]string{"a": {"a"}}) {
				t.Errorf("B19 violated (%s): GraphError %q, Tested %v; want no error and map[a:[a]]", r.name, got.GraphError, got.Tested)
			}
		case "-": // only the no-rewrite check in gfLoad applies
		default:
			if got.GraphError == "" || !strings.Contains(got.GraphError, strings.TrimPrefix(r.graphErr, "*")) || got.Tested != nil || got.Embedded != nil || got.Excluded != nil {
				t.Errorf("B19 violated (%s): GraphError %q, Tested %v, Embedded %q, Excluded %q; want an error (%q) and nil graph facts",
					r.name, got.GraphError, got.Tested, got.Embedded, got.Excluded, r.graphErr)
			}
		}
	}
}

// B16, B18, B19, B24, J4: a recording fake go sees exactly the probe and the
// graph command, in dir, from the one file env's PATH names, each with
// GOPROXY=off; the probe never sees verify's GOOS, GOARCH or GOFLAGS. G6b:
// both see the resolved go command's GoTool.Env last, its GOROOT and
// GOTOOLCHAIN=local, whatever env sets.
func TestLoadGoFactsCommandsB19(t *testing.T) {
	gfPosix(t)
	langpack.TrustGoOnPath(t)
	for _, m := range []string{"readonly", "vendor"} {
		files := map[string]string{"go.mod": "module example.com/c\n\ngo 1.21\n", "a/a.go": "package a\n"}
		if m == "vendor" {
			files["vendor/modules.txt"] = "# example.com/dep v1.0.0\n## explicit\nexample.com/dep\n"
		}
		dir := gfModule(t, files)
		rec := filepath.Join(gfDir(t), "rec")
		fake := gfFake(t, gfRecord(rec)+"exit 0\n")
		gfLoad(t, gfCtx(t), dir, gfEnv("PATH="+fake, "GOOS=linux", "GOARCH=386", "GOFLAGS=-tags=x", "GOROOT=/elsewhere", "GOTOOLCHAIN=auto"),
			langpack.Source{Current: gfKeys(files), Changed: []string{"a/a.go"}})
		data, _ := os.ReadFile(rec)
		got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		tool := "|" + langpack.TrustedGOROOT + "|local"
		want := []string{
			dir + "|list -mod=" + m + " -race runtime|off|" + runtime.GOOS + "|" + runtime.GOARCH + "|-mod=" + m + tool,
			dir + "|list -e=false -mod=" + m + " " + gfGraphArgs + "|off|linux|386|-tags=x" + tool,
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("B16/B18/B19 violated (-mod=%s): invocations\n%s\nwant\n%s", m, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// B20, J4: a hung go, a go that leaves a child holding its output pipe, a
// slow graph and an oversized graph are bounded by the facts budget and
// reported in GraphError. Upper bounds have 2 s of slack; the lower bound is exact.
func TestLoadGoFactsTimeAndSizeB20(t *testing.T) {
	gfPosix(t)
	langpack.TrustGoOnPath(t)
	src := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	probeOK := gfProbeOK
	deadline := func(t *testing.T, d time.Duration) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		t.Cleanup(cancel)
		return ctx
	}
	hang := func(p string) string { return "echo $$ >> '" + p + "'\nexec /bin/sleep 60\n" }
	for _, r := range []struct {
		name     string
		script   func(pids string) string
		ctx      func(t *testing.T) context.Context
		min, max time.Duration
		graphErr string
		noRace   bool
	}{
		{"default budget, go never exits", hang,
			func(*testing.T) context.Context { return context.Background() }, 5 * time.Second, 8 * time.Second, "did not finish", true},
		{"1 s ctx deadline", hang,
			func(t *testing.T) context.Context { return deadline(t, time.Second) }, 0, 4 * time.Second, "did not finish", true},
		{"background child holds the graph's pipes", func(p string) string {
			return probeOK + "/bin/sleep 60 &\necho $! >> '" + p + "'\n" + hang(p)
		}, func(t *testing.T) context.Context { return deadline(t, time.Second) }, 0, 4 * time.Second, "did not finish", false},
		{"6 s graph, no ctx deadline", func(string) string { return probeOK + "/bin/sleep 6\nexit 0\n" },
			func(*testing.T) context.Context { return context.Background() }, 5 * time.Second, 8 * time.Second, "did not finish", false},
		{"6 s graph, 2 min ctx deadline", func(string) string { return probeOK + "/bin/sleep 6\nexit 0\n" },
			gfCtx, 0, 2 * time.Minute, "", false},
		{"graph output over 64 MiB", func(string) string {
			return probeOK + "/usr/bin/head -c 67108865 /dev/zero | /usr/bin/tr '\\000' ' '\nexit 0\n"
		}, gfCtx, 0, 2 * time.Minute, "exceeds 64 MiB", false},
		// C2: graphErr "!" means not computed and not "did not finish" (WaitDelay ends the wait).
		{"descendant holds the output after the command exited", func(p string) string {
			return probeOK + "/bin/sleep 60 &\necho $! >> '" + p + "'\nexit 0\n"
		}, gfCtx, 0, 4 * time.Second, "!", false},
	} {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			dir := gfModule(t, map[string]string{"go.mod": "module example.com/t\n\ngo 1.21\n", "a/a.go": "package a\n"})
			env := gfEnv("PATH=" + gfFake(t, r.script(gfPids(t))))
			ctx := r.ctx(t)
			start := time.Now()
			got := gfLoad(t, ctx, dir, env, src)
			took := time.Since(start)
			if took < r.min || took > r.max {
				t.Errorf("B20/J4 violated: LoadGoFacts took %v, want between %v and %v", took, r.min, r.max)
			}
			if r.graphErr == "!" {
				gfNotComputed(t, "B20/C2", got, "")
			} else if (r.graphErr == "" && got.GraphError != "") || !strings.Contains(got.GraphError, r.graphErr) {
				t.Errorf("B20 violated: GraphError %q, want it to contain %q", got.GraphError, r.graphErr)
			}
			if r.noRace && got.NoRace != "" {
				t.Errorf("B20 violated: NoRace %q, want \"\"", got.NoRace)
			}
		})
	}
}

// gfProbeOK answers the race probe at once with exit 0.
const gfProbeOK = "case \" $* \" in *\" -race \"*) exit 0;; esac\n"

// gfNotComputed fails unless the graph is unavailable for a reason other than
// the budget: GraphError set, containing want, without "did not finish", and
// nil graph facts (B19.a-c, B20 WaitDelay row).
func gfNotComputed(t *testing.T, id string, got langpack.GoFacts, want string) {
	t.Helper()
	if got.GraphError == "" || strings.Contains(got.GraphError, "did not finish") || !strings.Contains(got.GraphError, want) ||
		got.Tested != nil || got.Embedded != nil || got.Excluded != nil {
		t.Errorf("%s violated: GraphError %q, Tested %v, Embedded %q, Excluded %q; want an error containing %q, not \"did not finish\", and nil graph facts",
			id, got.GraphError, got.Tested, got.Embedded, got.Excluded, want)
	}
}

// B19.a-c (C2): undecodable output, a silent non-zero exit and an Incomplete
// entry make the graph unavailable, neither computed nor timed out.
func TestLoadGoFactsBadGraphB19(t *testing.T) {
	gfPosix(t)
	langpack.TrustGoOnPath(t)
	dir := gfModule(t, map[string]string{"go.mod": "module example.test\n\ngo 1.21\n", "a/a.go": "package a\n"})
	src := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	incomplete := `{"ImportPath":"example.test/a","Dir":"` + dir + `/a","Module":{"Path":"example.test","Dir":"` + dir + `"},"Incomplete":true}`
	for _, r := range []struct{ name, graph, want string }{
		{"B19.a cut-off entry", `printf '%s\n' '{"ImportPath":"example.test/a",'` + "\nexit 0\n", ""},
		{"B19.a not json", "printf 'not json\\n'\nexit 0\n", ""},
		{"B19.b silent exit 3", "exit 3\n", "exit status 3"},
		{"B19.c Incomplete entry", `printf '%s\n' '` + incomplete + "'\nexit 0\n", ""},
	} {
		gfNotComputed(t, r.name, gfLoad(t, gfCtx(t), dir, gfEnv("PATH="+gfFake(t, gfProbeOK+r.graph)), src), r.want)
	}
}

// B20 (C3): a ctx that already ended starts no go command and reports the budget.
func TestLoadGoFactsEndedCtxB20(t *testing.T) {
	gfPosix(t)
	dir := gfModule(t, map[string]string{"go.mod": "module example.com/t\n\ngo 1.21\n", "a/a.go": "package a\n"})
	rec := filepath.Join(gfDir(t), "rec")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	start := time.Now()
	got := gfLoad(t, ctx, dir, gfEnv("PATH="+gfFake(t, gfRecord(rec)+"exit 0\n")), langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}})
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("B20/C3 violated: LoadGoFacts took %v, want within 1 s (+2 s slack)", took)
	}
	if !strings.Contains(got.GraphError, "did not finish") || got.NoRace != "" || len(gfLines(rec)) != 0 {
		t.Errorf("B20/C3 violated: GraphError %q, NoRace %q, go invocations %q; want \"did not finish\", \"\" and none", got.GraphError, got.NoRace, gfLines(rec))
	}
}

// B24: go is resolved from env's last PATH entry (else the process PATH),
// skipping empty, relative and unusable entries; every command runs that file.
func TestLoadGoFactsFindsGoB24(t *testing.T) {
	gfPosix(t)
	langpack.TrustGoOnPath(t)
	dir := gfModule(t, map[string]string{"go.mod": "module example.com/g\n\ngo 1.21\n", "a/a.go": "package a\n"})
	src := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	rec := filepath.Join(gfDir(t), "rec")
	labelled := func(label string) string { return gfFake(t, "printf '%s\\n' '"+label+"' >> '"+rec+"'\nexit 0\n") }
	A, B := labelled("A"), labelled("B")
	work := gfDir(t)
	if os.MkdirAll(filepath.Join(work, "rel"), 0o755) != nil ||
		os.WriteFile(filepath.Join(work, "rel", "go"), []byte("#!/bin/sh\nprintf 'R\\n' >> '"+rec+"'\n"), 0o755) != nil ||
		os.WriteFile(filepath.Join(work, "go"), []byte("#!/bin/sh\nprintf 'W\\n' >> '"+rec+"'\n"), 0o755) != nil {
		t.Fatal("cannot write the working-directory fakes")
	}
	noExec, withDir, link, target := gfDir(t), gfDir(t), gfDir(t), labelled("T")
	if os.WriteFile(filepath.Join(noExec, "go"), []byte("#!/bin/sh\nprintf 'N\\n' >> '"+rec+"'\n"), 0o644) != nil ||
		os.Mkdir(filepath.Join(withDir, "go"), 0o755) != nil || os.Symlink(filepath.Join(target, "go"), filepath.Join(link, "go")) != nil {
		t.Fatal("cannot write the unusable entries")
	}
	sep := string(os.PathListSeparator)
	t.Chdir(work)
	for _, r := range []struct {
		name    string
		env     []string // nil: LoadGoFacts gets a nil env
		process string   // the process PATH, when set
		want    string   // the fake that ran both commands; "" none
	}{
		{"PATH names the fake", gfEnv("PATH=" + A), "", "A A"},
		{"last PATH entry wins", gfEnv("PATH="+A, "PATH="+B), "", "B B"},
		{"relative entry skipped", gfEnv("PATH=rel" + sep + A), "", "A A"},
		{"empty entry skipped", gfEnv("PATH=" + sep + A), "", "A A"},
		{"non-executable file and directory skipped", gfEnv("PATH=" + noExec + sep + withDir + sep + B), "", "B B"},
		{"symlink to an executable", gfEnv("PATH=" + link), "", "T T"},
		{"nil env uses the process PATH", nil, A, "A A"},
		{"env without PATH uses the process PATH", []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "GOFLAGS="}, B, "B B"},
		{"nothing usable", gfEnv("PATH=" + noExec + sep + withDir), "", ""},
	} {
		_ = os.Remove(rec)
		if r.process != "" {
			t.Setenv("PATH", r.process)
		}
		got := gfLoad(t, gfCtx(t), dir, r.env, src)
		if ran := strings.Join(gfLines(rec), " "); ran != r.want {
			t.Errorf("B24 violated (%s): fakes run %q, want %q", r.name, ran, r.want)
		}
		if r.want == "" && (!strings.Contains(got.GraphError, "go was not found on PATH") || got.NoRace != "") {
			t.Errorf("B24 violated (%s): GraphError %q, NoRace %q; want \"go was not found on PATH\" and \"\"", r.name, got.GraphError, got.NoRace)
		}
	}
}
