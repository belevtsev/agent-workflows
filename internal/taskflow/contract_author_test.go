package taskflow_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Independent contract tests for WP9 (packet revisions 1 and 2). Every test names the
// requirement IDs it covers.

// contractAuthorGoMod is written at the root of every fixture module.
const contractAuthorGoMod = "module example.com/m\n\ngo 1.27\n"

// contractAuthorSmallSource declares only Kind.
const contractAuthorSmallSource = "package pkg\n\n// Kind is a kind.\ntype Kind int\n"

// contractAuthorPkgSource holds every agreed rendering of E6 plus bodies and
// literals E5 must strip.
const contractAuthorPkgSource = `package pkg

// Kind is a kind.
type Kind int

const (
	// First is first.
	First Kind = iota + 1 // trailing
	Second
)

const (
	A, B = iota, iota * 10
	C, D
)

// Limit is the limit.
const Limit = 7

// Stack holds items.
type Stack[T any] struct {
	// items are the items.
	items []T // line
	Name string ` + "`json:\"name\"`" + `
}

// Push adds.
func (s *Stack[T]) Push(v T) error {
	// body comment must vanish
	_ = func() int {
		// literal comment must vanish
		return 99
	}
	return nil
}

// Doer does.
type Doer interface {
	// Do it.
	Do(ctx int) (string, error)
}

// Handler does things.
var Handler = func(x int) int {
	// secret
	return x * 42
}

// Errors is the group doc and must vanish from spec texts.
var (
	ErrX = errorsNew("x")
	// ErrY is y.
	ErrY = errorsNew("y")
)

var y, z int

var a, b = 1, 2

var m, n = pair()

// Split splits.
func Split(s string) (head, tail string) { // opening comment must vanish
	return s, ""
}

// Map maps.
func Map[T, U any](xs []T, f func(T) U) []U {
	return nil
}

func errorsNew(s string) error { return nil }

func pair() (int, int) { return 1, 2 }
`

// contractAuthorBodySource holds odd spacing, nested literals and body
// comments for E5.
const contractAuthorBodySource = `package pkg

// Odd is odd.
func   Odd( a int,b  string )(int,error){ // opening must vanish
	// body comment must vanish
	f := func() {
		// nested literal must vanish
	}
	f()
	return a, nil
}

// Table holds funcs.
var Table = map[string]func() int{
	"a": func() int {
		// table literal must vanish
		return 7007
	},
}

type T struct{}

// Get gets.
func (t T) Get() (v int) {
	/* block comment must vanish */
	return 8008
}
`

// contractAuthorTaskSource is the project package used by A1-A2 and V1-V4.
const contractAuthorTaskSource = `package pkg

// Kind is a kind.
type Kind int

// Limit is the limit.
const Limit = 7

// Run runs.
func Run(x int) int {
	return x + 1
}
`

// contractAuthorTaskSymbols returns a fresh copy of the symbols extracted
// from contractAuthorTaskSource.
func contractAuthorTaskSymbols() []string {
	return []string{"Kind", "Limit", "Run"}
}

// contractAuthorModule writes go.mod and files into a new resolved directory.
func contractAuthorModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := scaffoldDir(t)
	scaffoldWrite(t, root, "go.mod", contractAuthorGoMod)
	for path, data := range files {
		scaffoldWrite(t, root, path, data)
	}
	return root
}

// contractAuthorExtract extracts symbols from dir and fails the test on error.
func contractAuthorExtract(t *testing.T, dir string, symbols ...string) taskflow.Contract {
	t.Helper()
	contract, err := taskflow.ExtractContract(dir, symbols)
	if err != nil {
		t.Fatalf("ExtractContract(%q, %q): %v", dir, symbols, err)
	}
	return contract
}

// contractAuthorLine returns the 1-based number of the unique line of src
// that starts with prefix.
func contractAuthorLine(t *testing.T, src, prefix string) int {
	t.Helper()
	found := 0
	for i, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if found != 0 {
			t.Fatalf("fixture prefix %q matches lines %d and %d", prefix, found, i+1)
		}
		found = i + 1
	}
	if found == 0 {
		t.Fatalf("fixture prefix %q matches no line", prefix)
	}
	return found
}

// contractAuthorSave encodes contract into a new file outside every project
// and returns its path and bytes.
func contractAuthorSave(t *testing.T, contract taskflow.Contract) (string, []byte) {
	t.Helper()
	data, err := taskflow.EncodeContract(contract)
	if err != nil {
		t.Fatalf("EncodeContract: %v", err)
	}
	file := filepath.Join(scaffoldDir(t), "contract.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file, data
}

// contractAuthorContractBrief extracts repo/pkg as it is now, saves the
// extract outside the project and returns an implementation brief carrying it.
func contractAuthorContractBrief(t *testing.T, repo, id string, owned ...string) taskflow.WorkerBrief {
	t.Helper()
	contract := contractAuthorExtract(t, filepath.Join(repo, "pkg"), contractAuthorTaskSymbols()...)
	file, data := contractAuthorSave(t, contract)
	brief := scaffoldBrief(id, owned...)
	brief.Contract = &taskflow.ContractRef{Path: file, SHA256: scaffoldSHA256(data)}
	return brief
}

// contractAuthorAssign assigns brief and fails the test on error.
func contractAuthorAssign(t *testing.T, task string, brief taskflow.WorkerBrief) taskflow.Record {
	t.Helper()
	record, err := taskflow.AssignWorker(t.Context(), task, brief)
	if err != nil {
		t.Fatalf("AssignWorker(%s): %v", brief.ID, err)
	}
	return record
}

// contractAuthorAssignments returns the assignments of record with brief ID id.
func contractAuthorAssignments(record taskflow.Record, id string) []taskflow.WorkerAssignment {
	var matches []taskflow.WorkerAssignment
	for _, assignment := range record.Assignments {
		if assignment.Brief.ID == id {
			matches = append(matches, assignment)
		}
	}
	return matches
}

// contractAuthorReplace replaces the first occurrence of old in the project
// file at the slash-separated path.
func contractAuthorReplace(t *testing.T, repo, path, old, replacement string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("fixture %s lacks %q", path, old)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if err := os.WriteFile(full, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

// contractAuthorContractOnly keeps entries containing "contract" in any case.
func contractAuthorContractOnly(entries []string) []string {
	var kept []string
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry), "contract") {
			kept = append(kept, entry)
		}
	}
	return kept
}

// contractAuthorContractEntries runs Verify and returns its contract problems
// and limitations. Verify's error is ignored: the scaffold task always has
// unrelated problems.
func contractAuthorContractEntries(t *testing.T, task string) (problems, limitations []string) {
	t.Helper()
	verification, _ := taskflow.Verify(t.Context(), task)
	return contractAuthorContractOnly(verification.Problems), contractAuthorContractOnly(verification.Limitations)
}

// contractAuthorWith keeps entries containing every part; the words "contract"
// and "stale" are matched case-insensitively, other parts exactly.
func contractAuthorWith(entries []string, parts ...string) []string {
	var kept []string
	for _, entry := range entries {
		ok := true
		for _, part := range parts {
			switch part {
			case "contract", "stale":
				ok = ok && strings.Contains(strings.ToLower(entry), part)
			default:
				ok = ok && strings.Contains(entry, part)
			}
		}
		if ok {
			kept = append(kept, entry)
		}
	}
	return kept
}

// contractAuthorAssignedTask starts a scaffold task over the task source and
// assigns the contract brief "alpha" owning owned.
func contractAuthorAssignedTask(t *testing.T, owned ...string) (repo, task string) {
	t.Helper()
	repo, task = scaffoldTask(t, map[string]string{"go.mod": contractAuthorGoMod, "pkg/a.go": contractAuthorTaskSource})
	contractAuthorAssign(t, task, contractAuthorContractBrief(t, repo, "alpha", owned...))
	return repo, task
}

// TestContractAuthorExtractShape covers E1: language, root, dir, symbols,
// grouping by requested-symbol order, slash paths and line numbers.
func TestContractAuthorExtractShape(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorPkgSource})
	symbols := []string{"Stack.Push", "Kind", "Second"}
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), slices.Clone(symbols)...)
	if got.Language != "go" {
		t.Errorf("Language = %q, want go", got.Language)
	}
	if got.Root != root || !filepath.IsAbs(got.Root) {
		t.Errorf("Root = %q, want %q", got.Root, root)
	}
	if got.Dir != "pkg" {
		t.Errorf("Dir = %q, want pkg", got.Dir)
	}
	if !reflect.DeepEqual(got.Symbols, symbols) {
		t.Errorf("Symbols = %q, want %q", got.Symbols, symbols)
	}
	wantLines := []int{
		contractAuthorLine(t, contractAuthorPkgSource, "func (s *Stack[T]) Push("),
		contractAuthorLine(t, contractAuthorPkgSource, "type Kind int"),
		contractAuthorLine(t, contractAuthorPkgSource, "\tSecond"),
	}
	if len(got.Declarations) != len(symbols) {
		t.Fatalf("got %d declarations, want %d: %+v", len(got.Declarations), len(symbols), got.Declarations)
	}
	for i, decl := range got.Declarations {
		if decl.Symbol != symbols[i] || decl.Path != "pkg/a.go" || decl.Line != wantLines[i] {
			t.Errorf("declaration %d = {%s %s line %d}, want {%s pkg/a.go line %d}", i, decl.Symbol, decl.Path, decl.Line, symbols[i], wantLines[i])
		}
	}
}

// TestContractAuthorExtractRoot covers E1: Root is the resolved directory of
// the nearest go.mod (or dir without one) and Dir and Path are slash-separated
// relative to it.
func TestContractAuthorExtractRoot(t *testing.T) {
	t.Parallel()
	type result struct {
		dir, wantRoot, wantDir, wantPath string
	}
	cases := []struct {
		name  string
		setup func(t *testing.T) result
	}{
		{"dir is module root", func(t *testing.T) result {
			root := contractAuthorModule(t, map[string]string{"a.go": contractAuthorSmallSource})
			return result{root, root, ".", "a.go"}
		}},
		{"nearest go.mod wins", func(t *testing.T) result {
			root := contractAuthorModule(t, map[string]string{"sub/go.mod": contractAuthorGoMod, "sub/pkg/a.go": contractAuthorSmallSource})
			sub := filepath.Join(root, "sub")
			return result{filepath.Join(sub, "pkg"), sub, "pkg", "pkg/a.go"}
		}},
		{"deep dir is slash separated", func(t *testing.T) result {
			root := contractAuthorModule(t, map[string]string{"x/y/a.go": contractAuthorSmallSource})
			return result{filepath.Join(root, "x", "y"), root, "x/y", "x/y/a.go"}
		}},
		{"no go.mod up to filesystem root", func(t *testing.T) result {
			dir := scaffoldDir(t)
			for parent := dir; ; {
				if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
					t.Skipf("temporary directory has an ancestor go.mod in %s", parent)
				}
				next := filepath.Dir(parent)
				if next == parent {
					break
				}
				parent = next
			}
			scaffoldWrite(t, dir, "a.go", contractAuthorSmallSource)
			return result{dir, dir, ".", "a.go"}
		}},
		{"symlinked path is resolved", func(t *testing.T) result {
			root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorSmallSource})
			link := filepath.Join(scaffoldDir(t), "link")
			if err := os.Symlink(root, link); err != nil {
				t.Fatal(err)
			}
			return result{filepath.Join(link, "pkg"), root, "pkg", "pkg/a.go"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := tc.setup(t)
			got := contractAuthorExtract(t, want.dir, "Kind")
			if got.Root != want.wantRoot {
				t.Errorf("Root = %q, want %q", got.Root, want.wantRoot)
			}
			if got.Dir != want.wantDir {
				t.Errorf("Dir = %q, want %q", got.Dir, want.wantDir)
			}
			if len(got.Declarations) != 1 || got.Declarations[0].Path != want.wantPath {
				t.Errorf("Declarations = %+v, want one with Path %q", got.Declarations, want.wantPath)
			}
			if len(got.Files) != 1 || got.Files[0].Path != want.wantPath {
				t.Errorf("Files = %+v, want one with Path %q", got.Files, want.wantPath)
			}
		})
	}
}

// TestContractAuthorExtractRelativeDir covers E1: a relative dir still gives
// an absolute Root. It changes the working directory, so it is not parallel.
func TestContractAuthorExtractRelativeDir(t *testing.T) {
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorSmallSource})
	t.Chdir(root)
	got := contractAuthorExtract(t, "pkg", "Kind")
	if got.Root != root || !filepath.IsAbs(got.Root) {
		t.Errorf("Root = %q, want absolute %q", got.Root, root)
	}
	if got.Dir != "pkg" {
		t.Errorf("Dir = %q, want pkg", got.Dir)
	}
}

// TestContractAuthorExtractFiles covers E2 (and E1 grouping): Files lists only
// the files declarations come from, by file name, with exact-byte hashes.
func TestContractAuthorExtractFiles(t *testing.T) {
	t.Parallel()
	aSource := "package pkg\n\n// Kind is a kind.\ntype Kind int\n\n\n\n"
	bSource := "package pkg\n\n// Other is other.\ntype Other int\n"
	cSource := "package pkg\n\n// Run runs.\nfunc Run() {}\n"
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": aSource, "pkg/b.go": bSource, "pkg/c.go": cSource})
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), "Run", "Kind")
	wantFiles := []taskflow.ContractFile{
		{Path: "pkg/a.go", SHA256: scaffoldSHA256([]byte(aSource))},
		{Path: "pkg/c.go", SHA256: scaffoldSHA256([]byte(cSource))},
	}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
	}
	if len(got.Declarations) != 2 || got.Declarations[0].Path != "pkg/c.go" || got.Declarations[1].Path != "pkg/a.go" {
		t.Errorf("Declarations = %+v, want Run from pkg/c.go then Kind from pkg/a.go", got.Declarations)
	}
}

// TestContractAuthorExtractKindsAndMatching covers E3: kinds, method matching
// for every receiver spelling, and plain names never matching methods.
func TestContractAuthorExtractKindsAndMatching(t *testing.T) {
	t.Parallel()
	source := `package pkg

type R1 int

// M on R1.
func (R1) M() {}

type R2 int

// M on R2.
func (r *R2) M() {}

type R3[K any] struct{}

// M on R3.
func (r R3[K]) M() {}

type R4[K comparable, V any] struct{}

// M on R4.
func (r *R4[K, V]) M() {}

type S struct{}

// Run on S.
func (S) Run() {}

// Run is a func.
func Run() {}

// Only is only a method.
func (S) Only() {}

// Helper is a func.
func Helper() {}

// Inner is a type.
type Inner int

// Count is a const.
const Count = 3

// Total is a var.
var Total = 4
`
	root := contractAuthorModule(t, map[string]string{"pkg/r.go": source})
	dir := filepath.Join(root, "pkg")
	matches := []struct{ symbol, kind, text string }{
		{"R1.M", "method", "// M on R1.\nfunc (R1) M()"},
		{"R2.M", "method", "// M on R2.\nfunc (r *R2) M()"},
		{"R3.M", "method", "// M on R3.\nfunc (r R3[K]) M()"},
		{"R4.M", "method", "// M on R4.\nfunc (r *R4[K, V]) M()"},
		{"S.Run", "method", "// Run on S.\nfunc (S) Run()"},
		{"Run", "func", "// Run is a func.\nfunc Run()"},
		{"Inner", "type", "// Inner is a type.\ntype Inner int"},
		{"Count", "const", "// Count is a const.\nconst Count = 3"},
		{"Total", "var", "// Total is a var.\nvar Total = 4"},
	}
	for _, tc := range matches {
		t.Run("match "+tc.symbol, func(t *testing.T) {
			t.Parallel()
			got := contractAuthorExtract(t, dir, tc.symbol)
			if len(got.Declarations) != 1 {
				t.Fatalf("got %d declarations, want 1: %+v", len(got.Declarations), got.Declarations)
			}
			decl := got.Declarations[0]
			if decl.Symbol != tc.symbol || decl.Kind != tc.kind || decl.Text != tc.text {
				t.Errorf("declaration = {%s %s %q}, want {%s %s %q}", decl.Symbol, decl.Kind, decl.Text, tc.symbol, tc.kind, tc.text)
			}
		})
	}
	// Plain names never match methods; T.M never matches a func, a type or
	// another type's method.
	for _, symbol := range []string{"Only", "M", "S.Helper", "S.Inner", "R1.Run", "S.M"} {
		t.Run("no match "+symbol, func(t *testing.T) {
			t.Parallel()
			if _, err := taskflow.ExtractContract(dir, []string{symbol}); err == nil {
				t.Errorf("ExtractContract(%q) succeeded, want an unknown-symbol error", symbol)
			}
		})
	}
}

// TestContractAuthorExtractPlatformFiles covers E3 (one declaration per file,
// build constraints ignored), E1 ordering and E2 file order.
func TestContractAuthorExtractPlatformFiles(t *testing.T) {
	t.Parallel()
	darwin := "//go:build darwin\n\npackage pkg\n\n// Plat names darwin.\nfunc Plat() string {\n\treturn \"darwin\"\n}\n"
	linux := "//go:build linux\n\npackage pkg\n\n// Plat names linux.\nfunc Plat() string {\n\treturn \"linux\"\n}\n"
	root := contractAuthorModule(t, map[string]string{"pkg/a_linux.go": linux, "pkg/a_darwin.go": darwin, "pkg/a.go": contractAuthorSmallSource})
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), "Plat", "Kind")
	want := []taskflow.ContractDeclaration{
		{Symbol: "Plat", Kind: "func", Path: "pkg/a_darwin.go", Line: 6, Text: "// Plat names darwin.\nfunc Plat() string"},
		{Symbol: "Plat", Kind: "func", Path: "pkg/a_linux.go", Line: 6, Text: "// Plat names linux.\nfunc Plat() string"},
		{Symbol: "Kind", Kind: "type", Path: "pkg/a.go", Line: 4, Text: "// Kind is a kind.\ntype Kind int"},
	}
	if !reflect.DeepEqual(got.Declarations, want) {
		t.Errorf("Declarations = %+v, want %+v", got.Declarations, want)
	}
	wantFiles := []taskflow.ContractFile{
		{Path: "pkg/a.go", SHA256: scaffoldSHA256([]byte(contractAuthorSmallSource))},
		{Path: "pkg/a_darwin.go", SHA256: scaffoldSHA256([]byte(darwin))},
		{Path: "pkg/a_linux.go", SHA256: scaffoldSHA256([]byte(linux))},
	}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
	}
}

// TestContractAuthorExtractScope covers E4: only non-test .go files directly
// in dir are read.
func TestContractAuthorExtractScope(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{
		"pkg/a.go":           contractAuthorSmallSource,
		"pkg/x_test.go":      "package pkg\n\n// OnlyTest is only in a test.\nfunc OnlyTest() {}\n",
		"pkg/broken_test.go": "package pkg\n\nfunc broken( {\n",
		"pkg/k_test.go":      "package pkg_test\n\n// Kind is a test kind.\ntype Kind string\n",
		"pkg/notes.txt":      "package pkg\n\nfunc Txt() {}\n",
		"pkg/sub/s.go":       "package sub\n\n// Deep is nested.\nfunc Deep() {}\n",
	})
	dir := filepath.Join(root, "pkg")
	got := contractAuthorExtract(t, dir, "Kind")
	wantDecls := []taskflow.ContractDeclaration{{Symbol: "Kind", Kind: "type", Path: "pkg/a.go", Line: 4, Text: "// Kind is a kind.\ntype Kind int"}}
	if !reflect.DeepEqual(got.Declarations, wantDecls) {
		t.Errorf("Declarations = %+v, want %+v", got.Declarations, wantDecls)
	}
	wantFiles := []taskflow.ContractFile{{Path: "pkg/a.go", SHA256: scaffoldSHA256([]byte(contractAuthorSmallSource))}}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
	}
	for _, symbol := range []string{"OnlyTest", "Deep", "Txt"} {
		if _, err := taskflow.ExtractContract(dir, []string{symbol}); err == nil {
			t.Errorf("ExtractContract(%q) succeeded, want unknown symbol", symbol)
		}
	}
}

// TestContractAuthorExtractBodiesStripped covers E5: no function, method or
// literal body nor any comment inside one appears; func texts end at the
// gofmt signature.
func TestContractAuthorExtractBodiesStripped(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorPkgSource, "pkg/b.go": contractAuthorBodySource})
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), "Stack.Push", "Handler", "Split", "Map", "Odd", "Table", "T.Get")
	if len(got.Declarations) != 7 {
		t.Fatalf("got %d declarations, want 7: %+v", len(got.Declarations), got.Declarations)
	}
	forbidden := []string{"must vanish", "secret", "42", "99", "7007", "8008", "return"}
	for _, decl := range got.Declarations {
		for _, word := range forbidden {
			if strings.Contains(decl.Text, word) {
				t.Errorf("%s Text contains %q: %q", decl.Symbol, word, decl.Text)
			}
		}
		if (decl.Kind == "func" || decl.Kind == "method") && strings.Contains(decl.Text, "{") {
			t.Errorf("%s Text contains a body brace: %q", decl.Symbol, decl.Text)
		}
	}
	exact := map[string]string{
		"Stack.Push": "// Push adds.\nfunc (s *Stack[T]) Push(v T) error",
		"Handler":    "// Handler does things.\nvar Handler = func(x int) int {}",
		"Split":      "// Split splits.\nfunc Split(s string) (head, tail string)",
		"Map":        "// Map maps.\nfunc Map[T, U any](xs []T, f func(T) U) []U",
		"Odd":        "// Odd is odd.\nfunc Odd(a int, b string) (int, error)",
		"T.Get":      "// Get gets.\nfunc (t T) Get() (v int)",
	}
	for _, decl := range got.Declarations {
		if want, ok := exact[decl.Symbol]; ok && decl.Text != want {
			t.Errorf("%s Text = %q, want %q", decl.Symbol, decl.Text, want)
		}
		if decl.Symbol == "Table" && !strings.HasPrefix(decl.Text, "// Table holds funcs.\nvar Table = map[string]func() int{") {
			t.Errorf("Table Text = %q, want the doc and var prefix", decl.Text)
		}
	}
}

// TestContractAuthorExtractTextFormat covers E6 (every agreed rendering), E3
// kinds and E1 line numbers, including specs inside groups.
func TestContractAuthorExtractTextFormat(t *testing.T) {
	t.Parallel()
	src := contractAuthorPkgSource
	cases := []struct{ symbol, kind, linePrefix, text string }{
		{"Kind", "type", "type Kind int", "// Kind is a kind.\ntype Kind int"},
		{"First", "const", "\tFirst Kind", "// First is first.\nconst First Kind = iota + 1 // trailing\n// iota = 0"},
		{"Second", "const", "\tSecond", "const Second Kind = iota + 1\n// iota = 1"},
		{"A", "const", "\tA, B =", "const A = iota\n// iota = 0"},
		{"B", "const", "\tA, B =", "const B = iota * 10\n// iota = 0"},
		{"C", "const", "\tC, D", "const C = iota\n// iota = 1"},
		{"D", "const", "\tC, D", "const D = iota * 10\n// iota = 1"},
		{"Limit", "const", "const Limit = 7", "// Limit is the limit.\nconst Limit = 7"},
		{"Stack", "type", "type Stack[T any] struct", "// Stack holds items.\ntype Stack[T any] struct {\n\t// items are the items.\n\titems []T    // line\n\tName  string `json:\"name\"`\n}"},
		{"Stack.Push", "method", "func (s *Stack[T]) Push(", "// Push adds.\nfunc (s *Stack[T]) Push(v T) error"},
		{"Doer", "type", "type Doer interface", "// Doer does.\ntype Doer interface {\n\t// Do it.\n\tDo(ctx int) (string, error)\n}"},
		{"Handler", "var", "var Handler = func", "// Handler does things.\nvar Handler = func(x int) int {}"},
		{"ErrX", "var", "\tErrX =", "var ErrX = errorsNew(\"x\")"},
		{"ErrY", "var", "\tErrY =", "// ErrY is y.\nvar ErrY = errorsNew(\"y\")"},
		{"z", "var", "var y, z int", "var z int"},
		{"b", "var", "var a, b = 1, 2", "var b = 2"},
		{"n", "var", "var m, n = pair()", "var _, n = pair()"},
		{"Split", "func", "func Split(", "// Split splits.\nfunc Split(s string) (head, tail string)"},
		{"Map", "func", "func Map[", "// Map maps.\nfunc Map[T, U any](xs []T, f func(T) U) []U"},
	}
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": src})
	// Request in reverse source order so grouping follows the request (E1).
	symbols := make([]string, 0, len(cases))
	for i := len(cases) - 1; i >= 0; i-- {
		symbols = append(symbols, cases[i].symbol)
	}
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), symbols...)
	if len(got.Declarations) != len(cases) {
		t.Fatalf("got %d declarations, want %d", len(got.Declarations), len(cases))
	}
	for i, decl := range got.Declarations {
		tc := cases[len(cases)-1-i]
		t.Run(tc.symbol, func(t *testing.T) {
			want := taskflow.ContractDeclaration{Symbol: tc.symbol, Kind: tc.kind, Path: "pkg/a.go", Line: contractAuthorLine(t, src, tc.linePrefix), Text: tc.text}
			if decl != want {
				t.Errorf("declaration\n got  %+v\n want %+v", decl, want)
			}
			if strings.HasSuffix(decl.Text, "\n") {
				t.Errorf("Text ends with a newline: %q", decl.Text)
			}
		})
	}
}

// TestContractAuthorExtractErrors covers E7.
func TestContractAuthorExtractErrors(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{
		"pkg/a.go":        contractAuthorPkgSource,
		"tests/x_test.go": "package tests\n\nfunc Only() {}\n",
		"empty/README.md": "no Go here\n",
		"broken/a.go":     contractAuthorSmallSource,
		"broken/b.go":     "package pkg\n\nfunc broken( {\n",
	})
	if err := os.Mkdir(filepath.Join(root, "none"), 0o700); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "pkg")
	cases := []struct {
		name    string
		dir     string
		symbols []string
		check   func(t *testing.T, msg string)
	}{
		{name: "nil symbols", dir: pkg, symbols: nil},
		{name: "empty symbols", dir: pkg, symbols: []string{}},
		{name: "symbol starting with a digit", dir: pkg, symbols: []string{"Kind", "1A"}},
		{name: "symbol with two dots", dir: pkg, symbols: []string{"A.B.C"}},
		{name: "empty symbol", dir: pkg, symbols: []string{""}},
		{name: "symbol with a dash", dir: pkg, symbols: []string{"a-b"}},
		{name: "method without a name", dir: pkg, symbols: []string{"Stack."}},
		{name: "unknown symbols are all named", dir: pkg, symbols: []string{"Kind", "Nope", "Stack.Nope"}, check: func(t *testing.T, msg string) {
			if !strings.Contains(msg, "Stack.Nope") || strings.Count(msg, "Nope") < 2 {
				t.Errorf("error %q does not name both Nope and Stack.Nope", msg)
			}
		}},
		{name: "distinct unknown symbols are all named", dir: pkg, symbols: []string{"Absent", "Kind", "Stack.Missing"}, check: func(t *testing.T, msg string) {
			if !strings.Contains(msg, "Absent") || !strings.Contains(msg, "Stack.Missing") {
				t.Errorf("error %q does not name Absent and Stack.Missing", msg)
			}
		}},
		{name: "missing path", dir: filepath.Join(root, "missing"), symbols: []string{"Kind"}},
		{name: "regular file", dir: filepath.Join(pkg, "a.go"), symbols: []string{"Kind"}},
		{name: "only test files", dir: filepath.Join(root, "tests"), symbols: []string{"Only"}},
		{name: "no Go files", dir: filepath.Join(root, "empty"), symbols: []string{"Kind"}},
		{name: "empty directory", dir: filepath.Join(root, "none"), symbols: []string{"Kind"}},
		{name: "syntax error in another file", dir: filepath.Join(root, "broken"), symbols: []string{"Kind"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := taskflow.ExtractContract(tc.dir, tc.symbols)
			if err == nil {
				t.Fatalf("ExtractContract(%q, %q) succeeded, want an error", tc.dir, tc.symbols)
			}
			if tc.check != nil {
				tc.check(t, err.Error())
			}
		})
	}
}

// contractAuthorNames returns n distinct symbols S0..S(n-1) joined by commas.
func contractAuthorNames(n int) ([]string, string) {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("S%d", i)
	}
	return names, strings.Join(names, ",")
}

// TestContractAuthorParseSymbols covers P1 and the 256-symbol bound of P2.
func TestContractAuthorParseSymbols(t *testing.T) {
	t.Parallel()
	limit, limitValue := contractAuthorNames(256)
	cases := []struct {
		value string
		want  []string
	}{
		{" A , T.M", []string{"A", "T.M"}},
		{"A", []string{"A"}},
		{"Kind,Stack.Push,_x", []string{"Kind", "Stack.Push", "_x"}},
		{"  B  ,A", []string{"B", "A"}},
		{limitValue, limit},
	}
	for _, tc := range cases {
		got, err := taskflow.ParseContractSymbols(tc.value)
		if err != nil {
			t.Errorf("ParseContractSymbols(%.40q): %v", tc.value, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseContractSymbols(%.40q) = %.80q, want %.80q", tc.value, got, tc.want)
		}
	}
}

// TestContractAuthorParseSymbolsErrors covers P2.
func TestContractAuthorParseSymbolsErrors(t *testing.T) {
	t.Parallel()
	_, tooMany := contractAuthorNames(257)
	for _, value := range []string{"", " ", "A,,B", "A,", ",A", "1A", "a-b", "A.B.C", "T.", ".M", "A,A", "T.M, T.M", tooMany} {
		if got, err := taskflow.ParseContractSymbols(value); err == nil {
			t.Errorf("ParseContractSymbols(%.40q) = %.80q, want an error", value, got)
		}
	}
}

// contractAuthorKeys returns the sorted keys of a JSON object value.
func contractAuthorKeys(t *testing.T, value any) []string {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("JSON value %v is not an object", value)
	}
	return slices.Sorted(maps.Keys(object))
}

// contractAuthorFirst returns the first element of a JSON array value.
func contractAuthorFirst(t *testing.T, value any) any {
	t.Helper()
	array, ok := value.([]any)
	if !ok || len(array) == 0 {
		t.Fatalf("JSON value %v is not a non-empty array", value)
	}
	return array[0]
}

// TestContractAuthorEncode covers N1.
func TestContractAuthorEncode(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorPkgSource})
	contract := contractAuthorExtract(t, filepath.Join(root, "pkg"), "Stack", "Stack.Push", "First", "ErrY")
	data, err := taskflow.EncodeContract(contract)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.HasSuffix(data, []byte("\n\n")) {
		t.Errorf("encoding does not end with exactly one newline: %q", data[max(0, len(data)-10):])
	}
	if !bytes.HasPrefix(data, []byte("{\n  \"language\": \"go\",\n")) {
		t.Errorf("encoding is not indented JSON starting with language: %.60q", data)
	}
	if !json.Valid(data) {
		t.Fatalf("encoding is not valid JSON")
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	if got, want := contractAuthorKeys(t, generic), []string{"declarations", "dir", "files", "language", "root", "symbols"}; !reflect.DeepEqual(got, want) {
		t.Errorf("contract keys = %q, want %q", got, want)
	}
	top := generic.(map[string]any)
	if got, want := contractAuthorKeys(t, contractAuthorFirst(t, top["files"])), []string{"path", "sha256"}; !reflect.DeepEqual(got, want) {
		t.Errorf("file keys = %q, want %q", got, want)
	}
	if got, want := contractAuthorKeys(t, contractAuthorFirst(t, top["declarations"])), []string{"kind", "line", "path", "symbol", "text"}; !reflect.DeepEqual(got, want) {
		t.Errorf("declaration keys = %q, want %q", got, want)
	}
	var decoded taskflow.Contract
	if err := taskflow.Decode(bytes.NewReader(data), &decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(decoded, contract) {
		t.Errorf("decoded contract differs\n got  %+v\n want %+v", decoded, contract)
	}
	again, err := taskflow.EncodeContract(contract)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, data) {
		t.Errorf("second encoding differs")
	}
}

// TestContractAuthorAssignAccepts covers A1.
func TestContractAuthorAssignAccepts(t *testing.T) {
	t.Parallel()
	repo, task := scaffoldTask(t, map[string]string{"go.mod": contractAuthorGoMod, "pkg/a.go": contractAuthorTaskSource})
	contract := contractAuthorExtract(t, filepath.Join(repo, "pkg"), contractAuthorTaskSymbols()...)
	file, data := contractAuthorSave(t, contract)
	brief := scaffoldBrief("alpha", "pkg/a_test.go")
	brief.Contract = &taskflow.ContractRef{Path: file, SHA256: scaffoldSHA256(data)}
	record := contractAuthorAssign(t, task, brief)
	matches := contractAuthorAssignments(record, "alpha")
	if len(matches) != 1 {
		t.Fatalf("record has %d assignments with ID alpha, want 1", len(matches))
	}
	if matches[0].ContractFile == "" {
		t.Fatalf("ContractFile is empty")
	}
	copied, err := os.ReadFile(filepath.Join(record.Directory, matches[0].ContractFile))
	if err != nil {
		t.Fatalf("read contract copy: %v", err)
	}
	if !bytes.Equal(copied, data) {
		t.Errorf("contract copy differs from the extract file")
	}
}

// TestContractAuthorAssignRefuses covers A2: each invalid contract brief is
// refused and leaves no assignment behind, so a later valid assign with the
// same ID succeeds.
func TestContractAuthorAssignRefuses(t *testing.T) {
	t.Parallel()
	const id = "alpha"
	withRole := func(role string) func(t *testing.T, repo string) taskflow.WorkerBrief {
		return func(t *testing.T, repo string) taskflow.WorkerBrief {
			brief := contractAuthorContractBrief(t, repo, id, "pkg/a_test.go")
			brief.Role = role
			brief.OwnedPaths = []string{"."}
			return brief
		}
	}
	withPath := func(path func(t *testing.T, file string) string) func(t *testing.T, repo string) taskflow.WorkerBrief {
		return func(t *testing.T, repo string) taskflow.WorkerBrief {
			brief := contractAuthorContractBrief(t, repo, id, "pkg/a_test.go")
			brief.Contract.Path = path(t, brief.Contract.Path)
			return brief
		}
	}
	withSHA := func(sha func(valid string) string) func(t *testing.T, repo string) taskflow.WorkerBrief {
		return func(t *testing.T, repo string) taskflow.WorkerBrief {
			brief := contractAuthorContractBrief(t, repo, id, "pkg/a_test.go")
			brief.Contract.SHA256 = sha(brief.Contract.SHA256)
			return brief
		}
	}
	afterEdit := func(old, replacement string) func(t *testing.T, repo string) taskflow.WorkerBrief {
		return func(t *testing.T, repo string) taskflow.WorkerBrief {
			brief := contractAuthorContractBrief(t, repo, id, "pkg/a_test.go")
			contractAuthorReplace(t, repo, "pkg/a.go", old, replacement)
			return brief
		}
	}
	cases := []struct {
		name string
		bad  func(t *testing.T, repo string) taskflow.WorkerBrief
	}{
		// The review case may also fail for a missing bundle; evidence and
		// verification isolate the role rule.
		{"review role", withRole("review")},
		{"evidence role", withRole("evidence")},
		{"verification role", withRole("verification")},
		{"relative path", withPath(func(t *testing.T, file string) string {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(cwd, file)
			if err != nil || filepath.IsAbs(rel) {
				t.Fatalf("relative path to %s: %q, %v", file, rel, err)
			}
			return rel
		})},
		{"path with dot-dot", withPath(func(t *testing.T, file string) string {
			if err := os.Mkdir(filepath.Join(filepath.Dir(file), "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			return filepath.Dir(file) + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(file)
		})},
		{"path with dot", withPath(func(t *testing.T, file string) string {
			return filepath.Dir(file) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(file)
		})},
		{"path with double separator", withPath(func(t *testing.T, file string) string {
			return filepath.Dir(file) + string(filepath.Separator) + string(filepath.Separator) + filepath.Base(file)
		})},
		{"uppercase sha256", withSHA(strings.ToUpper)},
		{"short sha256", withSHA(func(valid string) string { return valid[:63] })},
		{"long sha256", withSHA(func(valid string) string { return valid + "0" })},
		{"empty sha256", withSHA(func(string) string { return "" })},
		{"non-hex sha256", withSHA(func(valid string) string { return "g" + valid[1:] })},
		{"sha256 of other bytes", withSHA(func(string) string { return scaffoldSHA256([]byte("other bytes")) })},
		{"file inside the project", func(t *testing.T, repo string) taskflow.WorkerBrief {
			contract := contractAuthorExtract(t, filepath.Join(repo, "pkg"), contractAuthorTaskSymbols()...)
			data, err := taskflow.EncodeContract(contract)
			if err != nil {
				t.Fatal(err)
			}
			scaffoldWrite(t, repo, "notes/contract.json", string(data))
			brief := scaffoldBrief(id, "pkg/a_test.go")
			brief.Contract = &taskflow.ContractRef{Path: filepath.Join(repo, "notes", "contract.json"), SHA256: scaffoldSHA256(data)}
			return brief
		}},
		{"missing file", func(t *testing.T, repo string) taskflow.WorkerBrief {
			brief := scaffoldBrief(id, "pkg/a_test.go")
			brief.Contract = &taskflow.ContractRef{Path: filepath.Join(scaffoldDir(t), "missing.json"), SHA256: scaffoldSHA256([]byte("missing"))}
			return brief
		}},
		{"source comment changed after extraction", afterEdit("// Run runs.\n", "// Run runs.\n// More words.\n")},
		{"source trailing comment added after extraction", afterEdit("return x + 1\n}\n", "return x + 1\n}\n\n// trailing note\n")},
		{"source body changed after extraction", afterEdit("return x + 1", "return x + 2")},
		{"extract text edited with matching sha256", func(t *testing.T, repo string) taskflow.WorkerBrief {
			contract := contractAuthorExtract(t, filepath.Join(repo, "pkg"), contractAuthorTaskSymbols()...)
			contract.Declarations[0].Text = "// Kind is a different kind.\ntype Kind int"
			file, data := contractAuthorSave(t, contract)
			brief := scaffoldBrief(id, "pkg/a_test.go")
			brief.Contract = &taskflow.ContractRef{Path: file, SHA256: scaffoldSHA256(data)}
			return brief
		}},
		{"package outside the project", func(t *testing.T, _ string) taskflow.WorkerBrief {
			other := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorTaskSource})
			return contractAuthorContractBrief(t, other, id, "pkg/a_test.go")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, task := scaffoldTask(t, map[string]string{"go.mod": contractAuthorGoMod, "pkg/a.go": contractAuthorTaskSource})
			bad := tc.bad(t, repo)
			if _, err := taskflow.AssignWorker(t.Context(), task, bad); err == nil {
				t.Fatalf("AssignWorker accepted the invalid contract brief")
			}
			record := contractAuthorAssign(t, task, contractAuthorContractBrief(t, repo, id, "pkg/a_test.go"))
			if got := len(contractAuthorAssignments(record, id)); got != 1 {
				t.Errorf("record has %d assignments with ID %s, want 1", got, id)
			}
		})
	}
}

// TestContractAuthorVerifyFresh covers V1.
func TestContractAuthorVerifyFresh(t *testing.T) {
	t.Parallel()
	_, task := contractAuthorAssignedTask(t, "pkg/a_test.go")
	problems, limitations := contractAuthorContractEntries(t, task)
	if len(problems) != 0 || len(limitations) != 0 {
		t.Errorf("fresh contract reported problems %q and limitations %q", problems, limitations)
	}
}

// TestContractAuthorVerifyBodyDrift covers V2.
func TestContractAuthorVerifyBodyDrift(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, old, replacement string }{
		{"body changed", "return x + 1", "return x + 2"},
		{"comment added inside body", "\treturn x + 1", "\t// note inside the body\n\treturn x + 1"},
		{"declarations shifted by a detached comment", "package pkg\n", "package pkg\n\n// detached note\n"},
		{"declarations shifted by an unrelated declaration", "package pkg\n", "package pkg\n\nvar spacer = 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, task := contractAuthorAssignedTask(t, "pkg/a_test.go")
			contractAuthorReplace(t, repo, "pkg/a.go", tc.old, tc.replacement)
			problems, limitations := contractAuthorContractEntries(t, task)
			if len(problems) != 0 {
				t.Errorf("body-only drift reported contract problems %q", problems)
			}
			if len(limitations) != 1 {
				t.Fatalf("got contract limitations %q, want exactly one", limitations)
			}
			if !strings.Contains(limitations[0], "alpha") || !strings.Contains(limitations[0], "pkg/a.go") {
				t.Errorf("limitation %q does not name alpha and pkg/a.go", limitations[0])
			}
		})
	}
}

// TestContractAuthorVerifyDeclarationDrift covers V3.
func TestContractAuthorVerifyDeclarationDrift(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		drift func(t *testing.T, repo string)
		names []string
	}{
		{"signature changed", func(t *testing.T, repo string) {
			contractAuthorReplace(t, repo, "pkg/a.go", "func Run(x int) int {", "func Run(x, y int) int {")
		}, []string{"Run"}},
		{"doc comment changed", func(t *testing.T, repo string) {
			contractAuthorReplace(t, repo, "pkg/a.go", "// Run runs.", "// Run runs quickly.")
		}, nil},
		{"const value changed", func(t *testing.T, repo string) {
			contractAuthorReplace(t, repo, "pkg/a.go", "const Limit = 7", "const Limit = 8")
		}, nil},
		{"second declaration in a new file", func(t *testing.T, repo string) {
			scaffoldWrite(t, repo, "pkg/b.go", "package pkg\n\n// Kind is a kind.\ntype Kind int\n")
		}, nil},
		{"symbol removed", func(t *testing.T, repo string) {
			contractAuthorReplace(t, repo, "pkg/a.go", "// Limit is the limit.\nconst Limit = 7\n", "")
		}, nil},
		{"package directory deleted", func(t *testing.T, repo string) {
			if err := os.RemoveAll(filepath.Join(repo, "pkg")); err != nil {
				t.Fatal(err)
			}
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, task := contractAuthorAssignedTask(t, "pkg/a_test.go")
			tc.drift(t, repo)
			problems, _ := contractAuthorContractEntries(t, task)
			parts := append([]string{"alpha", "contract", "stale"}, tc.names...)
			if len(contractAuthorWith(problems, parts...)) == 0 {
				t.Errorf("no contract problem contains %q; contract problems: %q", parts, problems)
			}
		})
	}
}

// TestContractAuthorVerifySupersession covers V4 (with V3 drift): a later
// contract brief owning every path of a blocked first assignment clears its
// contract entries; partial ownership or a brief without a contract does not.
func TestContractAuthorVerifySupersession(t *testing.T) {
	t.Parallel()
	first := []string{"pkg/a_test.go", "pkg/b_test.go"}
	cases := []struct {
		name       string
		owned      []string
		contract   bool
		supersedes bool
	}{
		{"same paths with contract", first, true, true},
		{"covering directory with contract", []string{"pkg"}, true, true},
		{"some paths with contract", []string{"pkg/a_test.go"}, true, false},
		{"same paths without contract", first, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, task := contractAuthorAssignedTask(t, first...)
			contractAuthorReplace(t, repo, "pkg/a.go", "func Run(x int) int {", "func Run(x, y int) int {")
			scaffoldBlockedHandoff(t, task, "alpha")
			second := scaffoldBrief("bravo", tc.owned...)
			if tc.contract {
				second = contractAuthorContractBrief(t, repo, "bravo", tc.owned...)
			}
			contractAuthorAssign(t, task, second)
			problems, limitations := contractAuthorContractEntries(t, task)
			alpha := append(contractAuthorWith(problems, "alpha"), contractAuthorWith(limitations, "alpha")...)
			if tc.supersedes {
				if len(alpha) != 0 {
					t.Errorf("superseded assignment still has contract entries %q", alpha)
				}
				return
			}
			if len(contractAuthorWith(problems, "alpha", "contract", "stale")) == 0 {
				t.Errorf("stale contract problem for alpha disappeared; contract problems: %q", problems)
			}
		})
	}
}

// Revision 2 tests (packet r2: E4 amended, E6b, A2 amended).

// contractAuthorSymlink creates link pointing at target and fails the test on
// error.
func contractAuthorSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

// TestContractAuthorExtractSymlinkedFiles covers E4 (amended), with E1 paths
// and lines and E2 files: a .go entry linking to a regular file (outside dir,
// absolutely or relatively) is read under the link's own root-relative path
// with the target's hash; a .go entry that is or links to a directory is
// ignored.
func TestContractAuthorExtractSymlinkedFiles(t *testing.T) {
	t.Parallel()
	linked := "package pkg\n\n\n// Linked is linked.\nfunc Linked(x int) int {\n\treturn x\n}\n"
	shared := "package pkg\n\n// Shared is shared.\ntype Shared string\n"
	outside := scaffoldDir(t)
	scaffoldWrite(t, outside, "real.go", linked)
	root := contractAuthorModule(t, map[string]string{
		"pkg/a.go":          contractAuthorSmallSource,
		"shared/m.txt":      shared,
		"pkg/d.go/inner.go": "package inner\n\n// InDir is in a directory named like a Go file.\nfunc InDir() {}\n",
		"elsewhere/x.go":    "package elsewhere\n\n// ViaDirLink is reachable only through a directory link.\nfunc ViaDirLink() {}\n",
	})
	dir := filepath.Join(root, "pkg")
	contractAuthorSymlink(t, filepath.Join(outside, "real.go"), filepath.Join(dir, "l.go"))
	contractAuthorSymlink(t, filepath.Join("..", "shared", "m.txt"), filepath.Join(dir, "m.go"))
	contractAuthorSymlink(t, filepath.Join(root, "elsewhere"), filepath.Join(dir, "e.go"))

	got := contractAuthorExtract(t, dir, "Linked", "Shared", "Kind")
	wantDecls := []taskflow.ContractDeclaration{
		{Symbol: "Linked", Kind: "func", Path: "pkg/l.go", Line: contractAuthorLine(t, linked, "func Linked("), Text: "// Linked is linked.\nfunc Linked(x int) int"},
		{Symbol: "Shared", Kind: "type", Path: "pkg/m.go", Line: contractAuthorLine(t, shared, "type Shared"), Text: "// Shared is shared.\ntype Shared string"},
		{Symbol: "Kind", Kind: "type", Path: "pkg/a.go", Line: 4, Text: "// Kind is a kind.\ntype Kind int"},
	}
	if !reflect.DeepEqual(got.Declarations, wantDecls) {
		t.Errorf("Declarations = %+v, want %+v", got.Declarations, wantDecls)
	}
	wantFiles := []taskflow.ContractFile{
		{Path: "pkg/a.go", SHA256: scaffoldSHA256([]byte(contractAuthorSmallSource))},
		{Path: "pkg/l.go", SHA256: scaffoldSHA256([]byte(linked))},
		{Path: "pkg/m.go", SHA256: scaffoldSHA256([]byte(shared))},
	}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
	}
	if got.Root != root || got.Dir != "pkg" {
		t.Errorf("Root, Dir = %q, %q, want %q, pkg", got.Root, got.Dir, root)
	}
	// Directory entries named *.go, real or linked, are not read.
	for _, symbol := range []string{"InDir", "ViaDirLink"} {
		if _, err := taskflow.ExtractContract(dir, []string{symbol}); err == nil {
			t.Errorf("ExtractContract(%q) succeeded, want unknown symbol", symbol)
		}
	}
}

// TestContractAuthorExtractSymlinkedSyntaxError covers E4 (amended) with E7: a
// linked .go file is read like a regular file, so a syntax error in its target
// fails the extraction.
func TestContractAuthorExtractSymlinkedSyntaxError(t *testing.T) {
	t.Parallel()
	outside := scaffoldDir(t)
	scaffoldWrite(t, outside, "bad.go", "package pkg\n\nfunc broken( {\n")
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorSmallSource})
	dir := filepath.Join(root, "pkg")
	contractAuthorSymlink(t, filepath.Join(outside, "bad.go"), filepath.Join(dir, "b.go"))
	if _, err := taskflow.ExtractContract(dir, []string{"Kind"}); err == nil {
		t.Errorf("ExtractContract succeeded despite a syntax error behind pkg/b.go")
	}
}

// contractAuthorMultiNameSource holds multi-name specs whose comments belong
// to other names (E6b).
const contractAuthorMultiNameSource = `package pkg

var P, Q = 1 /* p note */, 2 /* Q_SECRET */

// R doc
var R, X int = 3, 4 // line

const (
	A, B = iota, iota * 10 // AB_SECRET
	C, D
)

var J /* j name note */, K = 1, 2

var S, U = f(1 /* s inner */), g(2 /* u inner */) // SU_LINE

var V, W map[string] /* vw type */ int

var M, N = pair( /* pair arg */ ) // MN_LINE

var (
	// G doc
	G, H = 5 /* g note */, 6 // GH_LINE
)

// Solo doc
var Solo = 1 // solo note

func f(int) int { return 0 }

func g(int) int { return 0 }

func pair() (int, int) { return 1, 2 }
`

// TestContractAuthorExtractMultiNameComments covers E6b (with E6 multi-name
// rendering, E1 lines and E3 kinds): a multi-name spec keeps only its doc and
// comments inside the kept name, the type and the kept or shared value; its
// trailing line comment is dropped; a single-name spec keeps its own.
func TestContractAuthorExtractMultiNameComments(t *testing.T) {
	t.Parallel()
	src := contractAuthorMultiNameSource
	cases := []struct {
		symbol, kind, linePrefix string
		exact                    string   // full Text when the packet fixes it
		prefix                   string   // Text prefix when only part is fixed
		contains, absent         []string // required and forbidden substrings
	}{
		{symbol: "P", kind: "var", linePrefix: "var P, Q", exact: "var P = 1", absent: []string{"p note", "Q_SECRET", "/*"}},
		{symbol: "Q", kind: "var", linePrefix: "var P, Q", exact: "var Q = 2", absent: []string{"p note", "Q_SECRET", "/*"}},
		{symbol: "R", kind: "var", linePrefix: "var R, X", exact: "// R doc\nvar R int = 3", absent: []string{"// line"}},
		{symbol: "X", kind: "var", linePrefix: "var R, X", exact: "// R doc\nvar X int = 4", absent: []string{"// line"}},
		{symbol: "A", kind: "const", linePrefix: "\tA, B =", exact: "const A = iota\n// iota = 0", absent: []string{"AB_SECRET"}},
		{symbol: "B", kind: "const", linePrefix: "\tA, B =", exact: "const B = iota * 10\n// iota = 0", absent: []string{"AB_SECRET"}},
		{symbol: "C", kind: "const", linePrefix: "\tC, D", exact: "const C = iota\n// iota = 1", absent: []string{"AB_SECRET"}},
		{symbol: "D", kind: "const", linePrefix: "\tC, D", exact: "const D = iota * 10\n// iota = 1", absent: []string{"AB_SECRET"}},
		{symbol: "K", kind: "var", linePrefix: "var J ", exact: "var K = 2", absent: []string{"j name note"}},
		{symbol: "S", kind: "var", linePrefix: "var S, U", prefix: "var S = f(1", contains: []string{"s inner"}, absent: []string{"u inner", "SU_LINE", "g("}},
		{symbol: "U", kind: "var", linePrefix: "var S, U", prefix: "var U = g(2", contains: []string{"u inner"}, absent: []string{"s inner", "SU_LINE", "f("}},
		{symbol: "V", kind: "var", linePrefix: "var V, W", prefix: "var V map[string]", contains: []string{"vw type"}, absent: []string{"W"}},
		{symbol: "W", kind: "var", linePrefix: "var V, W", prefix: "var W map[string]", contains: []string{"vw type"}, absent: []string{"V"}},
		{symbol: "N", kind: "var", linePrefix: "var M, N", prefix: "var _, N = pair(", contains: []string{"pair arg"}, absent: []string{"MN_LINE", "M"}},
		{symbol: "G", kind: "var", linePrefix: "\tG, H =", exact: "// G doc\nvar G = 5", absent: []string{"g note", "GH_LINE"}},
		{symbol: "H", kind: "var", linePrefix: "\tG, H =", exact: "// G doc\nvar H = 6", absent: []string{"g note", "GH_LINE"}},
		{symbol: "Solo", kind: "var", linePrefix: "var Solo", exact: "// Solo doc\nvar Solo = 1 // solo note"},
	}
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": src})
	symbols := make([]string, len(cases))
	for i, tc := range cases {
		symbols[i] = tc.symbol
	}
	got := contractAuthorExtract(t, filepath.Join(root, "pkg"), symbols...)
	if len(got.Declarations) != len(cases) {
		t.Fatalf("got %d declarations, want %d: %+v", len(got.Declarations), len(cases), got.Declarations)
	}
	for i, decl := range got.Declarations {
		tc := cases[i]
		t.Run(tc.symbol, func(t *testing.T) {
			if decl.Symbol != tc.symbol || decl.Kind != tc.kind || decl.Path != "pkg/a.go" {
				t.Errorf("declaration = {%s %s %s}, want {%s %s pkg/a.go}", decl.Symbol, decl.Kind, decl.Path, tc.symbol, tc.kind)
			}
			if want := contractAuthorLine(t, src, tc.linePrefix); decl.Line != want {
				t.Errorf("Line = %d, want %d", decl.Line, want)
			}
			if tc.exact != "" && decl.Text != tc.exact {
				t.Errorf("Text = %q, want %q", decl.Text, tc.exact)
			}
			if tc.prefix != "" && !strings.HasPrefix(decl.Text, tc.prefix) {
				t.Errorf("Text = %q, want prefix %q", decl.Text, tc.prefix)
			}
			for _, part := range tc.contains {
				if !strings.Contains(decl.Text, part) {
					t.Errorf("Text = %q, want it to contain %q", decl.Text, part)
				}
			}
			for _, part := range tc.absent {
				if strings.Contains(decl.Text, part) {
					t.Errorf("Text = %q, must not contain %q", decl.Text, part)
				}
			}
			if strings.HasSuffix(decl.Text, "\n") {
				t.Errorf("Text ends with a newline: %q", decl.Text)
			}
		})
	}
}

// TestContractAuthorAssignRefusesSymlinkedParent covers A2 (amended): an
// extract inside the project reached through a symlink outside it is refused
// and leaves no assignment, so a later valid assign with the same ID succeeds.
func TestContractAuthorAssignRefusesSymlinkedParent(t *testing.T) {
	t.Parallel()
	const id = "alpha"
	cases := []struct{ name, linkTarget, viaLink string }{
		{"link to a project subdirectory", "notes", "contract.json"},
		{"link to the project root", ".", "notes/contract.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, task := scaffoldTask(t, map[string]string{"go.mod": contractAuthorGoMod, "pkg/a.go": contractAuthorTaskSource})
			contract := contractAuthorExtract(t, filepath.Join(repo, "pkg"), contractAuthorTaskSymbols()...)
			data, err := taskflow.EncodeContract(contract)
			if err != nil {
				t.Fatal(err)
			}
			scaffoldWrite(t, repo, "notes/contract.json", string(data))
			link := filepath.Join(scaffoldDir(t), "link")
			contractAuthorSymlink(t, filepath.Join(repo, filepath.FromSlash(tc.linkTarget)), link)
			path := filepath.Join(link, filepath.FromSlash(tc.viaLink))
			if strings.HasPrefix(path, repo) || filepath.Clean(path) != path || !filepath.IsAbs(path) {
				t.Fatalf("fixture path %q must be absolute, clean and not start with %q", path, repo)
			}
			if viaLink, err := os.ReadFile(path); err != nil || !bytes.Equal(viaLink, data) {
				t.Fatalf("fixture: %s does not hold the extract (%v)", path, err)
			}
			bad := scaffoldBrief(id, "pkg/a_test.go")
			bad.Contract = &taskflow.ContractRef{Path: path, SHA256: scaffoldSHA256(data)}
			if _, err := taskflow.AssignWorker(t.Context(), task, bad); err == nil {
				t.Fatalf("AssignWorker accepted an extract inside the project through %s", path)
			}
			record := contractAuthorAssign(t, task, contractAuthorContractBrief(t, repo, id, "pkg/a_test.go"))
			if got := len(contractAuthorAssignments(record, id)); got != 1 {
				t.Errorf("record has %d assignments with ID %s, want 1", got, id)
			}
		})
	}
}

// Revision 3 tests (packet r3: E4c).

// contractAuthorDangling creates a symlink at the slash-separated path under dir
// whose target does not exist.
func contractAuthorDangling(t *testing.T, dir, path string) {
	t.Helper()
	target := filepath.Join(scaffoldDir(t), "missing.go")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("fixture target %s exists (%v)", target, err)
	}
	link := filepath.Join(dir, filepath.FromSlash(path))
	contractAuthorSymlink(t, target, link)
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("fixture link %s: %v", link, err)
	}
	if _, err := os.Stat(link); !os.IsNotExist(err) {
		t.Fatalf("fixture link %s is not dangling (%v)", link, err)
	}
}

// contractAuthorWantOnlyA asserts that got holds exactly Kind from pkg/a.go and
// lists only pkg/a.go in Files.
func contractAuthorWantOnlyA(t *testing.T, got taskflow.Contract) {
	t.Helper()
	wantDecls := []taskflow.ContractDeclaration{{Symbol: "Kind", Kind: "type", Path: "pkg/a.go", Line: 4, Text: "// Kind is a kind.\ntype Kind int"}}
	if !reflect.DeepEqual(got.Declarations, wantDecls) {
		t.Errorf("Declarations = %+v, want %+v", got.Declarations, wantDecls)
	}
	wantFiles := []taskflow.ContractFile{{Path: "pkg/a.go", SHA256: scaffoldSHA256([]byte(contractAuthorSmallSource))}}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
	}
}

// TestContractAuthorExtractIgnoredNames covers E4c (with E2 files, E3 one
// declaration per file and E7 unknown symbols): .go files whose names start
// with . or _ are not read, so their declarations are unknown symbols and a
// symbol they also declare yields only the ordinary file's declaration.
func TestContractAuthorExtractIgnoredNames(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{
		"pkg/a.go":   contractAuthorSmallSource,
		"pkg/.x.go":  "package pkg\n\n// Kind is a hidden kind.\ntype Kind int\n\n// Dot is hidden.\nfunc Dot() {}\n",
		"pkg/_x.go":  "package pkg\n\n// Kind is an underscored kind.\ntype Kind int\n\n// Under is underscored.\nfunc Under() {}\n",
		"pkg/.#y.go": "package pkg\n\n// Lock is in a lock-named file.\nfunc Lock() {}\n",
	})
	dir := filepath.Join(root, "pkg")
	contractAuthorWantOnlyA(t, contractAuthorExtract(t, dir, "Kind"))
	for _, symbol := range []string{"Dot", "Under", "Lock"} {
		_, err := taskflow.ExtractContract(dir, []string{"Kind", symbol})
		if err == nil {
			t.Errorf("ExtractContract(Kind, %s) succeeded, want an unknown-symbol error", symbol)
			continue
		}
		if !strings.Contains(err.Error(), symbol) {
			t.Errorf("error %q does not name the unknown symbol %s", err, symbol)
		}
	}
}

// TestContractAuthorExtractIgnoredNamesBeforeReading covers E4c (with E2
// files): entries starting with . or _ are ignored before anything else, so a
// dangling symlink such as the editor lock file .#a.go, or a syntax error in
// such a file, does not make extraction fail and is not listed in Files.
func TestContractAuthorExtractIgnoredNamesBeforeReading(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{"dangling editor lock .#a.go", func(t *testing.T, dir string) { contractAuthorDangling(t, dir, ".#a.go") }},
		{"dangling _#a.go", func(t *testing.T, dir string) { contractAuthorDangling(t, dir, "_#a.go") }},
		{"syntax error in .broken.go", func(t *testing.T, dir string) {
			scaffoldWrite(t, dir, ".broken.go", "package pkg\n\nfunc broken( {\n")
		}},
		{"syntax error in _broken.go", func(t *testing.T, dir string) {
			scaffoldWrite(t, dir, "_broken.go", "package pkg\n\nfunc broken( {\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorSmallSource})
			dir := filepath.Join(root, "pkg")
			tc.setup(t, dir)
			contractAuthorWantOnlyA(t, contractAuthorExtract(t, dir, "Kind"))
		})
	}
}

// TestContractAuthorExtractDanglingOrdinaryLink covers E4c (with E4 amended
// and E7): a dangling .go symlink with an ordinary name still makes extraction
// fail, even when the requested symbol is declared in another file.
func TestContractAuthorExtractDanglingOrdinaryLink(t *testing.T) {
	t.Parallel()
	root := contractAuthorModule(t, map[string]string{"pkg/a.go": contractAuthorSmallSource})
	dir := filepath.Join(root, "pkg")
	contractAuthorWantOnlyA(t, contractAuthorExtract(t, dir, "Kind"))
	contractAuthorDangling(t, dir, "b.go")
	if got, err := taskflow.ExtractContract(dir, []string{"Kind"}); err == nil {
		t.Errorf("ExtractContract succeeded despite the dangling pkg/b.go symlink: %+v", got)
	}
}
