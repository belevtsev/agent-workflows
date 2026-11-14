package langpack_test

// Go test files (interface contract L4, third version): R10. Expected values
// are R10's examples and cases derived by hand from its four rules.

import (
	"math/rand/v2"
	"path"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

func l4GoTestFile(t *testing.T, files []string, file string) bool {
	t.Helper()
	return l3Safe(t, func() bool { return langpack.GoTestFile(files, file) })
}

// TestGoTestFileExamples fails when GoTestFile differs from R10's examples.
func TestGoTestFileExamples(t *testing.T) {
	t.Parallel()
	files := []string{"go.mod", "p/p.go"}
	for _, file := range []string{"p/x_test.go", "x_test.go", "p/testdata/golden.txt", "p/testdata/f/seed"} {
		if !l4GoTestFile(t, files, file) {
			t.Errorf("GoTestFile(%q, %q) = false, want true (R10 example)", files, file)
		}
	}
	for _, file := range []string{"q/testdata/a", "vendor/v/x_test.go", "_tools/x_test.go", "p/_x_test.go", "p/x.go", "../x_test.go"} {
		if l4GoTestFile(t, files, file) {
			t.Errorf("GoTestFile(%q, %q) = true, want false (R10 example)", files, file)
		}
	}
	nested := append(append([]string{}, files...), "third/go.mod")
	if l4GoTestFile(t, nested, "third/a/x_test.go") {
		t.Errorf("GoTestFile(%q, third/a/x_test.go) = true, want false: third is a nested module (R10 example)", nested)
	}
	for _, file := range []string{"p/x_test.go", "x_test.go", "p/testdata/golden.txt"} {
		if l4GoTestFile(t, []string{"p/p.go", "go.work"}, file) {
			t.Errorf("GoTestFile without go.mod, %q = true, want false (R10 example, R23)", file)
		}
	}
}

// TestGoTestFileRules fails when a case derived from R10's rules 1-4 is
// judged differently: path shape, owner of testdata, vendor and _ or .
// elements, nested modules between owner and the root, and the test-name and
// testdata conditions.
func TestGoTestFileRules(t *testing.T) {
	t.Parallel()
	base := []string{"go.mod", "p/p.go", "main.go", "p/sub/s.go", "r/go.mod", "r/r.go", ".hidden/h.go", "p/.cache/c.go", "vendor/v/v.go", "_tools/t.go"}
	cases := []struct {
		name  string
		files []string
		file  string
		want  bool
	}{
		// Rule 1.
		{"no go.mod in files", []string{"p/p.go"}, "p/x_test.go", false},
		{"go.mod only nested", []string{"m/go.mod", "m/m.go"}, "m/x_test.go", false},
		{"empty files", nil, "x_test.go", false},
		// Rule 2.
		{"empty file", base, "", false},
		{"absolute file", base, "/p/x_test.go", false},
		{"dot prefix not clean", base, "./p/x_test.go", false},
		{"double slash not clean", base, "p//x_test.go", false},
		{"dot-dot inside not clean", base, "p/../x_test.go", false},
		{"trailing slash not clean", base, "p/x_test.go/", false},
		{"dot-dot", base, "..", false},
		{"leading dot-dot", base, "../p/x_test.go", false},
		{"dot", base, ".", false},
		// Rule 3: owner elements.
		{"root test file", base, "x_test.go", true},
		{"nested package", base, "p/sub/y_test.go", true},
		{"vendor directory", base, "vendor/v/v_test.go", false},
		{"vendor deeper", base, "p/vendor/x_test.go", false},
		{"underscore directory", base, "_tools/t_test.go", false},
		{"dot directory", base, ".hidden/h_test.go", false},
		{"dot directory deeper", base, "p/.cache/c_test.go", false},
		{"vendor-like name is fine", base, "vendors/x_test.go", true},
		{"underscore inside a name is fine", base, "p_q/x_test.go", true},
		// Rule 3: nested modules.
		{"nested module root", base, "r/x_test.go", false},
		{"inside a nested module", base, "r/deep/x_test.go", false},
		{"testdata of a nested module", base, "r/testdata/golden", false},
		{"go.mod of a sibling does not matter", base, "p/x_test.go", true},
		{"go.mod below the owner does not matter", append(append([]string{}, base...), "p/sub/inner/go.mod"), "p/sub/x_test.go", true},
		// Rule 4: test names.
		{"not a test name", base, "p/test.go", false},
		{"underscore test file", base, "p/_test.go", false},
		{"dot test file", base, "p/.x_test.go", false},
		{"backup of a test file", base, "p/x_test.go.bak", false},
		{"upper-case suffix", base, "p/x_TEST.go", false},
		{"file named testdata", base, "p/testdata", false},
		{"Go source not a test", base, "p/p.go", false},
		{"go.mod itself", base, "go.mod", false},
		// Rule 4: testdata.
		{"testdata at the root with a root Go file", base, "testdata/golden", true},
		{"testdata at the root without a root Go file", []string{"go.mod", "p/p.go"}, "testdata/golden", false},
		{"testdata whose owner has only test files", []string{"go.mod", "q/q_test.go"}, "q/testdata/a", true},
		{"testdata of a package below the Go file", base, "p/sub/testdata/a", true},
		{"testdata whose owner has no Go file", []string{"go.mod", "p/p.go"}, "p/sub/testdata/a", false},
		{"Go file below the owner does not count", []string{"go.mod", "p/sub/s.go"}, "p/testdata/a", false},
		{"non-Go file in the owner does not count", []string{"go.mod", "q/README.md", "q/go.sum"}, "q/testdata/a", false},
		{"test-named file in testdata is test data", base, "p/testdata/x_test.go", true},
		{"underscore file in testdata", base, "p/testdata/_x", true},
		{"dot file in testdata", base, "p/testdata/.x", true},
		{"underscore directory below testdata", base, "p/testdata/_d/x", true},
		{"vendor below testdata", base, "p/testdata/vendor/x", true},
		{"nested testdata: owner before the first", base, "p/testdata/q/testdata/z", true},
		{"go.mod below testdata does not matter", append(append([]string{}, base...), "p/testdata/m/go.mod"), "p/testdata/m/x_test.go", true},
		{"testdata under vendor", base, "vendor/v/testdata/x", false},
		{"testdata under an underscore directory", base, "_tools/testdata/x", false},
		{"testdata under a dot directory", base, "p/.cache/testdata/x", false},
		{"testdata of a nested module", base, "r/testdata/x", false},
	}
	for _, c := range cases {
		if got := l4GoTestFile(t, c.files, c.file); got != c.want {
			t.Errorf("%s: GoTestFile(%q, %q) = %v, want %v (R10)", c.name, c.files, c.file, got, c.want)
		}
	}
}

// TestGoTestFileProperties fails when, over generated paths, GoTestFile is
// true for files without go.mod, or true for a path that is not a test-side
// path (one with a testdata element or ending in _test.go) (R10 rules 1 and
// 4).
func TestGoTestFileProperties(t *testing.T) {
	t.Parallel()
	elems := []string{"p", "q", "testdata", "vendor", "_x", ".y", "x_test.go", "a.go", "go.mod", "f.txt", "..", "."}
	rng := rand.New(rand.NewPCG(10, 23))
	gen := func() string {
		n := 1 + rng.IntN(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = elems[rng.IntN(len(elems))]
		}
		return strings.Join(parts, "/")
	}
	for i := 0; i < 3000; i++ {
		var files []string
		for n := rng.IntN(6); n > 0; n-- {
			files = append(files, gen())
		}
		file := gen()
		hasGoMod := false
		for _, f := range files {
			if f == "go.mod" {
				hasGoMod = true
			}
		}
		got := l4GoTestFile(t, files, file)
		if got && !hasGoMod {
			t.Fatalf("GoTestFile(%q, %q) = true without go.mod in files (R10 rule 1)", files, file)
		}
		testSide := strings.HasSuffix(path.Base(file), "_test.go")
		for _, e := range strings.Split(path.Dir(file), "/") {
			if e == "testdata" {
				testSide = true
			}
		}
		if got && !testSide {
			t.Fatalf("GoTestFile(%q, %q) = true for a path that is neither _test.go nor under testdata (R10 rule 4)", files, file)
		}
		if got && (file != path.Clean(file) || strings.HasPrefix(file, "../") || file == "..") {
			t.Fatalf("GoTestFile(%q, %q) = true for an unclean path (R10 rule 2)", files, file)
		}
	}
}
