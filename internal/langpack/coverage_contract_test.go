package langpack_test

// Contract tests for langpack.CreditsGoTest and langpack.AddedGoCoverage
// (interface contract R20, with R5 and R6 for the admission rules they feed).
// Expected values are hand-derived from the contract's R20 text and the shared
// fixture's definition of a qualifying item.

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

func covArgv(s string) []string { return strings.Fields(s) }

func covWork(pkgs, mods []string) langpack.GoWork {
	if pkgs == nil {
		pkgs = []string{}
	}
	if mods == nil {
		mods = []string{}
	}
	return langpack.GoWork{Packages: pkgs, ModuleFiles: mods}
}

var covEmpty = langpack.GoWork{Packages: []string{}, ModuleFiles: []string{}}

// covCheck asserts the result shape GoWork promises (one element per added
// argv, every slice non-nil and sorted) and then exact equality.
func covCheck(t *testing.T, label string, got, want []langpack.GoWork) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len(result) = %d, want %d (one element per added argv): %#v", label, len(got), len(want), got)
	}
	for i, w := range got {
		if w.Packages == nil || w.ModuleFiles == nil {
			t.Errorf("%s: element %d has a nil slice: %#v (GoWork slices are never nil)", label, i, w)
		}
		if !sort.StringsAreSorted(w.Packages) || !sort.StringsAreSorted(w.ModuleFiles) {
			t.Errorf("%s: element %d is not sorted: %#v", label, i, w)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: AddedGoCoverage mismatch\n got: %#v\nwant: %#v", label, got, want)
	}
}

// covSpecSource is R20.b's source and merged list.
func covSpecSource() (langpack.Source, []string) {
	return langpack.Source{
			Current: []string{"go.mod", "internal/a/a.go", "internal/b/b.go", "internal/c/c.go", "internal/d/d.go"},
			Changed: []string{"go.mod", "internal/b/b.go", "internal/d/d.go"},
		},
		[]string{"go.mod", "internal/b/b.go", "internal/c/c.go"}
}

var covSpecPrior = [][]string{covArgv("go test ./internal/a")}

// R20.a: the Go rule's argv credit test with empty GOFLAGS. Fails when a
// listed argv is credited or rejected the other way.
func TestContractCreditsGoTest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{"go", "test", "./..."}, true},
		{[]string{"go.exe", "test", "./a"}, true},
		{[]string{"/usr/local/go/bin/go", "test", "-v", "-count=1", "./..."}, true},
		{[]string{"go", "test", "example.com/m/..."}, true},
		{[]string{"make", "test"}, false},
		{[]string{"go", "vet", "./..."}, false},
		{[]string{"go", "test", "-run", "X", "./..."}, false},
		{[]string{"go", "test", "./...", "-args", "-x"}, false},
		{[]string{"go", "test", "./a", "-v", "./b"}, false},
		{[]string{"GO.EXE", "test", "./..."}, false},
		{[]string{"go"}, false},
	} {
		if got := langpack.CreditsGoTest(tc.argv); got != tc.want {
			t.Errorf("CreditsGoTest(%q) = %t, want %t", tc.argv, got, tc.want)
		}
	}
}

// R20 ("with empty GOFLAGS") and R5.c: GOFLAGS in the process environment
// plays no part. Fails when -run=TestX in GOFLAGS stops crediting ./... or
// changes coverage, or when GOFLAGS supplies a missing credit.
func TestContractCreditsGoTestIgnoresGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-run=TestX")
	if !langpack.CreditsGoTest(covArgv("go test ./...")) {
		t.Errorf("CreditsGoTest(go test ./...) = false with GOFLAGS=-run=TestX, want true")
	}
	if langpack.CreditsGoTest(covArgv("go test -run X ./...")) {
		t.Errorf("CreditsGoTest(go test -run X ./...) = true, want false")
	}
	source, merged := covSpecSource()
	got := langpack.AddedGoCoverage(source, merged, covSpecPrior, [][]string{covArgv("go test ./...")})
	covCheck(t, "GOFLAGS set", got, []langpack.GoWork{covWork([]string{"internal/b"}, []string{"go.mod"})})
}

// R20.b: only items changed since the baseline and holding a merged path
// qualify (internal/c is merged but unchanged, internal/d changed but not
// merged), minus prior coverage (internal/a).
func TestContractAddedGoCoverageSpecExample(t *testing.T) {
	t.Parallel()
	source, merged := covSpecSource()
	got := langpack.AddedGoCoverage(source, merged, covSpecPrior, [][]string{covArgv("go test ./...")})
	covCheck(t, "R20.b", got, []langpack.GoWork{covWork([]string{"internal/b"}, []string{"go.mod"})})
}

// R20.c: an item goes to the first added argv covering it; order decides
// attribution. Fails when later argvs are credited with items earlier ones
// covered, or when module files are credited to ./internal/b.
func TestContractAddedGoCoverageOrderDecidesAttribution(t *testing.T) {
	t.Parallel()
	source, merged := covSpecSource()
	got := langpack.AddedGoCoverage(source, merged, covSpecPrior, [][]string{
		covArgv("go test ./..."), covArgv("go test ./internal/b"),
	})
	covCheck(t, "widened then b-tests", got, []langpack.GoWork{
		covWork([]string{"internal/b"}, []string{"go.mod"}),
		covEmpty,
	})

	got = langpack.AddedGoCoverage(source, merged, covSpecPrior, [][]string{
		covArgv("go test ./internal/b"), covArgv("go test ./..."),
	})
	covCheck(t, "b-tests then widened", got, []langpack.GoWork{
		covWork([]string{"internal/b"}, nil),
		covWork(nil, []string{"go.mod"}),
	})
}

// R20.d and prior coverage in general: items covered by any credited prior
// argv are never new; an uncredited prior argv covers nothing.
func TestContractAddedGoCoveragePriorCoverage(t *testing.T) {
	t.Parallel()
	source, merged := covSpecSource()
	added := [][]string{covArgv("go test ./..."), covArgv("go test ./internal/b")}

	got := langpack.AddedGoCoverage(source, merged,
		[][]string{covArgv("go test ./internal/a"), covArgv("go test ./...")}, added)
	covCheck(t, "R20.d prior holds ./...", got, []langpack.GoWork{covEmpty, covEmpty})

	got = langpack.AddedGoCoverage(source, merged,
		[][]string{covArgv("go test ./internal/b")}, [][]string{covArgv("go test ./...")})
	covCheck(t, "prior covers internal/b", got, []langpack.GoWork{covWork(nil, []string{"go.mod"})})

	got = langpack.AddedGoCoverage(source, merged,
		[][]string{covArgv("go test -run X ./..."), covArgv("make test")}, [][]string{covArgv("go test ./...")})
	covCheck(t, "uncredited prior covers nothing", got, []langpack.GoWork{
		covWork([]string{"internal/b"}, []string{"go.mod"}),
	})
}

// R20.e: an uncredited argv, or a credited argv with no ./ or . pattern,
// covers nothing, and does not consume items for later argvs.
func TestContractAddedGoCoverageUncreditedArgv(t *testing.T) {
	t.Parallel()
	source, merged := covSpecSource()
	got := langpack.AddedGoCoverage(source, merged, covSpecPrior, [][]string{
		covArgv("make test"),
		covArgv("go test -run X ./..."),
		covArgv("go test example.com/m/..."),
		covArgv("go test ./... -args -x"),
		covArgv("go test ./..."),
	})
	covCheck(t, "R20.e", got, []langpack.GoWork{
		covEmpty, covEmpty, covEmpty, covEmpty,
		covWork([]string{"internal/b"}, []string{"go.mod"}),
	})
}

// R20.f: with no baseline manifest (Changed nil) nothing qualifies; the
// result still has one non-nil, sorted element per added argv. An empty
// Changed and an empty added list are the same boundary.
func TestContractAddedGoCoverageNoChangedFiles(t *testing.T) {
	t.Parallel()
	source, merged := covSpecSource()
	added := [][]string{covArgv("go test ./..."), covArgv("go test ./internal/b"), covArgv("make test")}

	source.Changed = nil
	got := langpack.AddedGoCoverage(source, merged, nil, added)
	covCheck(t, "Changed nil", got, []langpack.GoWork{covEmpty, covEmpty, covEmpty})

	source.Changed = []string{}
	got = langpack.AddedGoCoverage(source, merged, nil, added)
	covCheck(t, "Changed empty", got, []langpack.GoWork{covEmpty, covEmpty, covEmpty})

	source, merged = covSpecSource()
	if got := langpack.AddedGoCoverage(source, merged, covSpecPrior, nil); len(got) != 0 {
		t.Errorf("no added argv: result = %#v, want no elements", got)
	}
	got = langpack.AddedGoCoverage(source, nil, covSpecPrior, added)
	covCheck(t, "merged empty", got, []langpack.GoWork{covEmpty, covEmpty, covEmpty})
}

// covWideSource has qualifying packages internal/b, internal/b/sub,
// internal/bb and internal/c and module files go.mod and go.sum; internal/e is
// changed but not merged, so it never qualifies.
func covWideSource() (langpack.Source, []string) {
	files := []string{
		"go.mod", "go.sum",
		"internal/b/b.go", "internal/b/sub/s.go", "internal/bb/bb.go",
		"internal/c/c.go", "internal/e/e.go",
	}
	return langpack.Source{Current: files, Changed: files},
		[]string{"go.mod", "go.sum", "internal/b/b.go", "internal/b/sub/s.go", "internal/bb/bb.go", "internal/c/c.go"}
}

// R20 pattern rules: ./pkg covers only that package; ./dir/... covers the
// directory and packages below it but not a sibling sharing its prefix; only
// ./... covers module files. Fails on prefix matching (internal/bb under
// ./internal/b/...), on ./internal/b covering internal/b/sub, or on a package
// pattern covering go.mod or go.sum.
func TestContractAddedGoCoveragePatterns(t *testing.T) {
	t.Parallel()
	source, merged := covWideSource()
	got := langpack.AddedGoCoverage(source, merged, nil, [][]string{
		covArgv("go test ./internal/b"),
		covArgv("go test ./internal/b/..."),
		covArgv("go test ./internal/e"),
		covArgv("go test ./..."),
	})
	covCheck(t, "patterns", got, []langpack.GoWork{
		covWork([]string{"internal/b"}, nil),
		covWork([]string{"internal/b/sub"}, nil),
		covEmpty,
		covWork([]string{"internal/bb", "internal/c"}, []string{"go.mod", "go.sum"}),
	})

	got = langpack.AddedGoCoverage(source, merged, nil, [][]string{
		covArgv("go test ./internal/..."),
		covArgv("go test ./..."),
	})
	covCheck(t, "./internal/... then ./...", got, []langpack.GoWork{
		covWork([]string{"internal/b", "internal/b/sub", "internal/bb", "internal/c"}, nil),
		covWork(nil, []string{"go.mod", "go.sum"}),
	})
}

// R20: "." covers the root package and nothing else; a module file stays
// uncovered. The contract does not fix how the root package is spelled, so
// only its count is asserted.
func TestContractAddedGoCoverageRootPackage(t *testing.T) {
	t.Parallel()
	source := langpack.Source{
		Current: []string{"go.mod", "main.go", "internal/b/b.go"},
		Changed: []string{"go.mod", "main.go", "internal/b/b.go"},
	}
	merged := []string{"go.mod", "main.go", "internal/b/b.go"}
	got := langpack.AddedGoCoverage(source, merged, nil, [][]string{covArgv("go test .")})
	if len(got) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(got))
	}
	if len(got[0].Packages) != 1 || slices.Contains(got[0].Packages, "internal/b") {
		t.Errorf(`"go test ." Packages = %#v, want exactly the root package`, got[0].Packages)
	}
	if got[0].ModuleFiles == nil || len(got[0].ModuleFiles) != 0 {
		t.Errorf(`"go test ." ModuleFiles = %#v, want empty and non-nil`, got[0].ModuleFiles)
	}
}

type covItems struct{ pkgs, mods map[string]bool }

func covUnion(t *testing.T, label string, got []langpack.GoWork, standalone []covItems) covItems {
	t.Helper()
	u := covItems{map[string]bool{}, map[string]bool{}}
	for i, w := range got {
		for _, p := range w.Packages {
			if u.pkgs[p] {
				t.Errorf("%s: package %s attributed to more than one argv", label, p)
			}
			if !standalone[i].pkgs[p] {
				t.Errorf("%s: element %d claims package %s, which its argv does not cover", label, i, p)
			}
			u.pkgs[p] = true
		}
		for _, m := range w.ModuleFiles {
			if u.mods[m] {
				t.Errorf("%s: module file %s attributed to more than one argv", label, m)
			}
			if !standalone[i].mods[m] {
				t.Errorf("%s: element %d claims module file %s, which its argv does not cover", label, i, m)
			}
			u.mods[m] = true
		}
	}
	return u
}

func covSet(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func covPermutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, p := range covPermutations(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := append(append(append([]int{}, p[:i]...), n-1), p[i:]...)
			out = append(out, q)
		}
	}
	return out
}

// R20.g (exhaustive over every permutation): order decides attribution, not
// the union. For every order the union equals the hand-derived set of
// qualifying items the argvs cover, no item is attributed twice, and each
// element holds only items its argv covers on its own.
func TestContractAddedGoCoveragePermutationUnion(t *testing.T) {
	t.Parallel()
	source, merged := covWideSource()
	type arg struct {
		argv []string
		own  covItems
	}
	none := covItems{covSet(), covSet()}
	argvs := []arg{
		{covArgv("go test ./internal/b/..."), covItems{covSet("internal/b", "internal/b/sub"), covSet()}},
		{covArgv("go test ./internal/b"), covItems{covSet("internal/b"), covSet()}},
		{covArgv("go test ./internal/c"), covItems{covSet("internal/c"), covSet()}},
		{covArgv("go test ./internal/e"), none},
		{covArgv("go test ./..."), covItems{covSet("internal/b", "internal/b/sub", "internal/bb", "internal/c"), covSet("go.mod", "go.sum")}},
		{covArgv("go test -run X ./..."), none},
	}
	for _, set := range []struct {
		name  string
		pick  []int
		union covItems
	}{
		{"without ./...", []int{0, 1, 2, 3}, covItems{covSet("internal/b", "internal/b/sub", "internal/c"), covSet()}},
		{"with ./...", []int{0, 1, 2, 3, 4, 5}, covItems{covSet("internal/b", "internal/b/sub", "internal/bb", "internal/c"), covSet("go.mod", "go.sum")}},
	} {
		for _, perm := range covPermutations(len(set.pick)) {
			added := make([][]string, len(perm))
			own := make([]covItems, len(perm))
			order := make([]string, len(perm))
			for i, j := range perm {
				a := argvs[set.pick[j]]
				added[i], own[i] = a.argv, a.own
				order[i] = strings.Join(a.argv[2:], " ")
			}
			label := fmt.Sprintf("%s, order %q", set.name, order)
			got := langpack.AddedGoCoverage(source, merged, nil, added)
			if len(got) != len(added) {
				t.Fatalf("%s: len(result) = %d, want %d", label, len(got), len(added))
			}
			u := covUnion(t, label, got, own)
			if !reflect.DeepEqual(u, set.union) {
				t.Errorf("%s: union = %v, want %v", label, u, set.union)
			}
			if t.Failed() {
				return
			}
		}
	}
}

// R20 mapping clause ("both mapped with source.Current as the Go rule maps
// them") and section 2's definition of a qualifying item: a file belongs to
// the package of its directory, or of the directory holding its testdata/; a
// .go file only while that directory still holds a .go file; a non-Go file in
// a directory without Go files belongs to the nearest ancestor directory with
// Go files; vendor, nested modules and directories starting with _ or . hold
// no package; module files are go.mod, go.sum, go.work and go.work.sum at the
// top of the selected path, covered only by ./... .
//
// Every case keeps the root and internal/ free of Go files, so a file that
// holds no package has no ancestor package to fall back to under any reading.
// Each case fails on a naive "package = directory of the path" mapping, or on
// treating every top-level go.* file alike.
func TestContractAddedGoCoverageMappingRules(t *testing.T) {
	t.Parallel()
	base := []string{"go.mod", "internal/b/b.go", "internal/c/c.go"}
	for _, tc := range []struct {
		name string
		// extra files now present (added to base for source.Current)
		present []string
		// files changed since the baseline and changed by the merge
		changed, merged []string
		added           [][]string
		want            []langpack.GoWork
	}{{
		name:    "testdata file belongs to the package holding testdata/",
		present: []string{"internal/b/testdata/golden.txt"},
		changed: []string{"internal/b/testdata/golden.txt"},
		merged:  []string{"internal/b/testdata/golden.txt"},
		added:   [][]string{covArgv("go test ./internal/b/testdata"), covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covEmpty, covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "embedded asset in a package directory belongs to that package",
		present: []string{"internal/b/embed.txt"},
		changed: []string{"internal/b/embed.txt"},
		merged:  []string{"internal/b/embed.txt"},
		added:   [][]string{covArgv("go test ./internal/c"), covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covEmpty, covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "assembly file in a package directory belongs to that package",
		present: []string{"internal/b/asm_amd64.s"},
		changed: []string{"internal/b/asm_amd64.s"},
		merged:  []string{"internal/b/asm_amd64.s"},
		added:   [][]string{covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "C file in a package directory belongs to that package",
		present: []string{"internal/b/helper.c"},
		changed: []string{"internal/b/helper.c"},
		merged:  []string{"internal/b/helper.c"},
		added:   [][]string{covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "non-Go file in a directory without Go files belongs to the nearest ancestor package",
		present: []string{"internal/b/assets/img/logo.png"},
		changed: []string{"internal/b/assets/img/logo.png"},
		merged:  []string{"internal/b/assets/img/logo.png"},
		added: [][]string{
			covArgv("go test ./internal/b/assets/..."),
			covArgv("go test ./internal/b"),
		},
		want: []langpack.GoWork{covEmpty, covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "deleted .go file counts while its directory still holds a .go file",
		changed: []string{"internal/b/old.go"},
		merged:  []string{"internal/b/old.go"},
		added:   [][]string{covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covWork([]string{"internal/b"}, nil)},
	}, {
		name:    "deleted .go file in a directory with no .go file left is no package there",
		changed: []string{"internal/gone/gone.go"},
		merged:  []string{"internal/gone/gone.go"},
		added:   [][]string{covArgv("go test ./internal/gone"), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty},
	}, {
		name:    "vendor holds no package",
		present: []string{"vendor/example.com/x/x.go", "vendor/modules.txt"},
		changed: []string{"vendor/example.com/x/x.go", "vendor/modules.txt"},
		merged:  []string{"vendor/example.com/x/x.go", "vendor/modules.txt"},
		added:   [][]string{covArgv("go test ./vendor/example.com/x"), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty},
	}, {
		name:    "nested module holds no package and its go.mod is no module file",
		present: []string{"internal/nested/go.mod", "internal/nested/n.go"},
		changed: []string{"internal/nested/go.mod", "internal/nested/n.go"},
		merged:  []string{"internal/nested/go.mod", "internal/nested/n.go"},
		added:   [][]string{covArgv("go test ./internal/nested"), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty},
	}, {
		name:    "directory starting with _ holds no package",
		present: []string{"internal/_skip/s.go"},
		changed: []string{"internal/_skip/s.go"},
		merged:  []string{"internal/_skip/s.go"},
		added:   [][]string{covArgv("go test ./internal/_skip"), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty},
	}, {
		name:    "directory starting with . holds no package",
		present: []string{"internal/.hidden/h.go"},
		changed: []string{"internal/.hidden/h.go"},
		merged:  []string{"internal/.hidden/h.go"},
		added:   [][]string{covArgv("go test ./internal/.hidden"), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty},
	}, {
		name:    "go.work and go.work.sum are module files covered only by ./...",
		present: []string{"go.work", "go.work.sum"},
		changed: []string{"go.work", "go.work.sum"},
		merged:  []string{"go.work", "go.work.sum"},
		added:   [][]string{covArgv("go test ./internal/b"), covArgv("go test ./internal/..."), covArgv("go test ./...")},
		want:    []langpack.GoWork{covEmpty, covEmpty, covWork(nil, []string{"go.work", "go.work.sum"})},
	}, {
		name:    "changed and merged files are mapped separately to one package",
		present: []string{"internal/b/testdata/golden.txt"},
		changed: []string{"internal/b/b.go"},
		merged:  []string{"internal/b/testdata/golden.txt"},
		added:   [][]string{covArgv("go test ./internal/b")},
		want:    []langpack.GoWork{covWork([]string{"internal/b"}, nil)},
	}} {
		source := langpack.Source{
			Current: append(append([]string{}, base...), tc.present...),
			Changed: tc.changed,
		}
		sort.Strings(source.Current)
		got := langpack.AddedGoCoverage(source, tc.merged, nil, tc.added)
		covCheck(t, tc.name, got, tc.want)
	}
}
