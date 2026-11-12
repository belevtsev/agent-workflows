package taskflow_test

// Brief inputs: R1-R6 and R38 (inputs, input_files).

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const l3InputShape = "worker brief inputs need at most 32 clean absolute paths, each with its SHA-256"

// l3Input writes content to name and returns its InputRef.
func l3Input(t *testing.T, name, content string) taskflow.InputRef {
	t.Helper()
	l3WriteFile(t, name, content)
	return taskflow.InputRef{Path: name, SHA256: l3Sum([]byte(content))}
}

// l3InputFiles lists the task directory entries named like stored inputs.
func (f *l3Fix) l3InputFiles() []string {
	f.t.Helper()
	names, err := filepath.Glob(filepath.Join(f.dir, "input-*"))
	if err != nil {
		f.t.Fatal(err)
	}
	return names
}

// TestAssignInputsShape fails when a malformed inputs list is accepted, when a
// well-formed one at the bound is refused, or when an empty list is not the
// same as an absent one (R1, R38).
func TestAssignInputsShape(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	in := filepath.Join(f.root, "inputs")
	good := l3Input(t, filepath.Join(in, "good.md"), "good input\n")
	var refs []taskflow.InputRef
	for i := 0; i < taskflow.MaxBriefInputs+1; i++ {
		refs = append(refs, l3Input(t, filepath.Join(in, fmt.Sprintf("n%02d.md", i)), fmt.Sprintf("input %d\n", i)))
	}
	long := "/" + strings.Repeat("a", 4096)
	bad := map[string][]taskflow.InputRef{
		"33 inputs":            refs,
		"relative path":        {{Path: "inputs/good.md", SHA256: good.SHA256}},
		"empty path":           {{Path: "", SHA256: good.SHA256}},
		"unclean dot-dot":      {{Path: filepath.Join(in, "x") + "/../good.md", SHA256: good.SHA256}},
		"unclean double slash": {{Path: in + "//good.md", SHA256: good.SHA256}},
		"trailing slash":       {{Path: in + "/", SHA256: good.SHA256}},
		"control character":    {{Path: in + "/go\x01od.md", SHA256: good.SHA256}},
		"4097-byte path":       {{Path: long, SHA256: good.SHA256}},
		"uppercase sha256":     {{Path: good.Path, SHA256: strings.ToUpper(good.SHA256)}},
		"short sha256":         {{Path: good.Path, SHA256: good.SHA256[:63]}},
		"long sha256":          {{Path: good.Path, SHA256: good.SHA256 + "0"}},
		"non-hex sha256":       {{Path: good.Path, SHA256: "g" + good.SHA256[1:]}},
		"empty sha256":         {{Path: good.Path, SHA256: ""}},
	}
	n := 0
	for name, inputs := range bad {
		n++
		id := fmt.Sprintf("shape%d", n)
		b := f.plainBrief(id, []string{"p/" + id + ".go"})
		b.Goal = name
		b.Inputs = inputs
		if err := f.assignFails(b, l3InputShape); err != nil && errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: refused as a missing file, want the R1 shape error first", name)
		}
	}
	if len(f.l3InputFiles()) != 0 {
		t.Errorf("refused briefs stored input copies: %v", f.l3InputFiles())
	}

	// Exactly 4096 bytes passes the shape check, so a file check (R4) reports it.
	exact := filepath.Join(f.root, "x")
	for len(exact) < 4096 {
		seg := 4096 - len(exact) - 1
		if seg > 200 {
			seg = 200
		}
		exact += "/" + strings.Repeat("b", seg)
	}
	if len(exact) != 4096 {
		t.Fatalf("built a %d-byte path", len(exact))
	}
	b := f.plainBrief("exact", []string{"p/exact.go"})
	b.Inputs = []taskflow.InputRef{{Path: exact, SHA256: good.SHA256}}
	// The file check's own error depends on the platform's path limit and R4
	// does not state it, so only require that the shape check let it through.
	if err := f.assign(b); err == nil || strings.Contains(err.Error(), l3InputShape) {
		t.Errorf("4096-byte missing input: error %v, want an error other than the shape error (R1 bound)", err)
	}

	// 32 inputs are accepted.
	b = f.plainBrief("max", []string{"p/max.go"})
	b.Inputs = refs[:taskflow.MaxBriefInputs]
	f.mustAssign(b)
	if got := f.assignment("max").InputFiles; len(got) != taskflow.MaxBriefInputs {
		t.Errorf("32 inputs stored %d copies, want 32 (R1, R5)", len(got))
	}

	// An empty list is the same as an absent one.
	b = f.plainBrief("empty", []string{"p/empty.go"})
	b.Inputs = []taskflow.InputRef{}
	f.mustAssign(b)
	a := f.assignment("empty")
	if len(a.Brief.Inputs) != 0 || len(a.InputFiles) != 0 {
		t.Errorf("empty inputs stored as %v / %v, want none (R1)", a.Brief.Inputs, a.InputFiles)
	}
	stored := f.storedAssignment("empty")
	if l3Has(l3Members(stored), "input_files") || l3Has(l3Members(stored["brief"]), "inputs") {
		t.Errorf("assignment with empty inputs stored members %v / brief %v, want no inputs or input_files (R38)",
			l3Members(stored), l3Members(stored["brief"]))
	}
}

// TestAssignInputsDuplicates fails when one path string twice is accepted,
// when two spellings of one file are refused, or when an input naming the
// contract file is refused (R2).
func TestAssignInputsDuplicates(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	real := filepath.Join(f.root, "real")
	one := l3Input(t, filepath.Join(real, "spec.md"), "spec\n")
	b := f.plainBrief("dup", []string{"p/dup.go"})
	b.Inputs = []taskflow.InputRef{one, one}
	_ = f.assignFails(b, "worker brief inputs repeat "+one.Path)

	link := filepath.Join(f.root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	other := taskflow.InputRef{Path: filepath.Join(link, "spec.md"), SHA256: one.SHA256}
	contract := taskflow.InputRef{Path: f.contract.Path, SHA256: f.contract.SHA256}
	b = f.brief("spellings", []string{"p/spellings_test.go"}, l3Req("R1", "C1"))
	b.Inputs = []taskflow.InputRef{one, other, contract}
	f.mustAssign(b)
	if got := f.assignment("spellings").InputFiles; len(got) != 3 {
		t.Errorf("two spellings and the contract file stored %d copies, want 3 (R2, R5)", len(got))
	}
}

// TestAssignInputFileChecks fails when an input that is missing, not a
// regular file, over 8 MiB or not matching its sha256 is accepted, when
// exactly 8 MiB is refused, when the checks do not run in input order, when
// an earlier input is stored although a later one fails, or when the file
// checks run before the other assign checks (R4).
func TestAssignInputFileChecks(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	in := filepath.Join(f.root, "inputs")
	good := l3Input(t, filepath.Join(in, "good.md"), "good\n")
	missing := taskflow.InputRef{Path: filepath.Join(in, "missing.md"), SHA256: good.SHA256}
	if err := os.Symlink(good.Path, filepath.Join(in, "link.md")); err != nil {
		t.Fatal(err)
	}
	link := taskflow.InputRef{Path: filepath.Join(in, "link.md"), SHA256: good.SHA256}
	if err := os.Mkdir(filepath.Join(in, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := taskflow.InputRef{Path: filepath.Join(in, "dir.md"), SHA256: good.SHA256}
	limit := bytes.Repeat([]byte("8"), taskflow.MaxInputBytes)
	exact := taskflow.InputRef{Path: filepath.Join(in, "exact.bin"), SHA256: l3Sum(limit)}
	if err := os.WriteFile(exact.Path, limit, 0o644); err != nil {
		t.Fatal(err)
	}
	over := append(limit, '9')
	large := taskflow.InputRef{Path: filepath.Join(in, "over.bin"), SHA256: l3Sum(over)}
	if err := os.WriteFile(large.Path, over, 0o644); err != nil {
		t.Fatal(err)
	}
	wrong := taskflow.InputRef{Path: good.Path, SHA256: l3Sum([]byte("other bytes"))}
	other := l3Input(t, filepath.Join(in, "other.md"), "other\n")
	otherWrong := taskflow.InputRef{Path: other.Path, SHA256: good.SHA256}

	try := func(id string, inputs ...taskflow.InputRef) error {
		t.Helper()
		b := f.plainBrief(id, []string{"p/" + id + ".go"})
		b.Goal = id
		b.Inputs = inputs
		before := f.revision()
		err := f.assign(b)
		if err != nil && f.revision() != before {
			t.Errorf("%s: failed assign moved the record", id)
		}
		return err
	}
	if err := try("missing", missing); err == nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "input "+missing.Path) {
		t.Errorf("missing input error %v, want one naming %q that wraps fs.ErrNotExist (R4)", err, "input "+missing.Path)
	}
	for id, ref := range map[string]taskflow.InputRef{"symlink": link, "directory": dir} {
		want := "input " + ref.Path + " must be a regular file, not a symlink"
		if err := try(id, ref); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s input error %v, want %q (R4)", id, err, want)
		}
	}
	if err := try("over", large); err == nil || !strings.Contains(err.Error(), "input "+large.Path+" exceeds the 8 MiB limit") {
		t.Errorf("8 MiB + 1 input error %v, want the size limit error (R4)", err)
	}
	if err := try("wrong", wrong); err == nil || !strings.Contains(err.Error(), "input "+good.Path+" sha256 differs from the file's bytes") {
		t.Errorf("wrong sha256 error %v, want the hash error (R4)", err)
	}
	// In order: the first failing input is reported.
	if err := try("order", good, missing, otherWrong); err == nil || !strings.Contains(err.Error(), "input "+missing.Path) || strings.Contains(err.Error(), "sha256 differs") {
		t.Errorf("good, missing, wrong: error %v, want the missing input reported (R4 order)", err)
	}
	if got := f.l3InputFiles(); len(got) != 0 {
		t.Errorf("refused briefs left stored copies %v (R4: every input passes before any is stored)", got)
	}
	if err := try("exact", exact); err != nil {
		t.Errorf("exactly 8 MiB refused: %v (R4 bound)", err)
	}
}

// TestAssignInputChecksRunLast fails when an input file problem is reported
// before a shape, requirements or presence error of the same brief (R4: R1 and
// R2 run with the shape checks, the file checks after every other check, R8
// included).
func TestAssignInputChecksRunLast(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{checks: []taskflow.Check{l3Check("ok", l3JSONAll...)}})
	missing := taskflow.InputRef{Path: filepath.Join(f.root, "missing.md"), SHA256: l3Sum(nil)}

	b := f.brief("r8", []string{"p/r8_test.go"})
	b.Inputs = []taskflow.InputRef{missing}
	_ = f.assignFails(b, "a brief with a contract must declare requirements: each contract requirement with the plan requirements it implements")

	b = f.brief("r7", []string{"p/r7_test.go"}, l3Req("R1", "C9"))
	b.Inputs = []taskflow.InputRef{missing}
	_ = f.assignFails(b, "brief requirement R1 names unknown plan requirement C9")

	b = f.brief("r2", []string{"p/r2_test.go"}, l3Req("R1", "C1"))
	b.Inputs = []taskflow.InputRef{missing, missing}
	_ = f.assignFails(b, "worker brief inputs repeat "+missing.Path)
}

// TestAssignStoresInputCopies fails when assign does not store one private
// copy per input, in brief order, named input-<brief id>-<32 hex>, holding the
// input's bytes; when an input inside the project is treated differently;
// when the brief's inputs are not kept as given; or when a later change to
// an original changes the record (R3, R5).
func TestAssignStoresInputCopies(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	outside := l3Input(t, filepath.Join(f.root, "packet", "contract.md"), "outside the project\n")
	inside := l3Input(t, f.abs("docs/spec.md"), "inside the project\n")
	contents := []string{"outside the project\n", "inside the project\n"}
	b := f.plainBrief("copies", []string{"p/a.go"})
	b.Inputs = []taskflow.InputRef{outside, inside}
	f.mustAssign(b)

	a := f.assignment("copies")
	if !reflect.DeepEqual(a.Brief.Inputs, b.Inputs) {
		t.Errorf("stored brief inputs %v, want %v as given (R5)", a.Brief.Inputs, b.Inputs)
	}
	if len(a.InputFiles) != 2 {
		t.Fatalf("input_files = %v, want two names (R5)", a.InputFiles)
	}
	name := regexp.MustCompile(`^input-copies-[0-9a-f]{32}$`)
	for i, n := range a.InputFiles {
		if !name.MatchString(n) {
			t.Errorf("input_files[%d] = %q, want input-copies-<32 lowercase hex> (R5)", i, n)
		}
		full := filepath.Join(f.dir, n)
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatalf("stored copy %s: %v (R5)", n, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Errorf("stored copy %s mode %v, want a regular 0600 file (R5)", n, info.Mode())
		}
		if data, _ := os.ReadFile(full); string(data) != contents[i] {
			t.Errorf("stored copy %d holds %q, want %q in brief order (R3, R5)", i, data, contents[i])
		}
	}
	if a.InputFiles[0] == a.InputFiles[1] {
		t.Errorf("both inputs share the stored name %s (R5)", a.InputFiles[0])
	}

	l3WriteFile(t, outside.Path, "edited after assign\n")
	if err := os.Remove(inside.Path); err != nil {
		t.Fatal(err)
	}
	if got := f.assignment("copies"); !reflect.DeepEqual(got, a) {
		t.Errorf("editing the originals changed the stored assignment: %s, want %s (R5)", l3JSON(got), l3JSON(a))
	}
	for i, n := range a.InputFiles {
		if data, _ := os.ReadFile(filepath.Join(f.dir, n)); string(data) != contents[i] {
			t.Errorf("stored copy %d changed to %q after the original changed (R5)", i, data)
		}
	}
}
