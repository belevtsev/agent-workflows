package taskflow_test

// Round 4 (assignment l3-tests-4): requirement R5a, added to R5 of interface
// contract L3 revision 3. An assign that passes every brief check but whose
// record update is refused because the encoded revision would exceed the
// revision size limit (8 MiB, "task revision exceeds size limit") returns that
// error and leaves the task as it was: same latest revision, no assignment
// with the brief's ID, and no file in the task directory that was not there
// before, in particular no input-<brief id>-* copy and no stored contract copy.
//
// The refusal is reached through public calls only. The plan declares
// r4PlanReqs requirements with long IDs; every brief is a contract-bearing
// (test-engineer) brief that declares the R7 maximum of 256 requirements, each
// linking every plan requirement, so each accepted assignment adds megabytes
// to the record. The test assigns briefs of one shape until one is refused.
// Each brief has its own contract extract (symbols F and G<nn>) and its own
// input bytes, so a stored copy of the refused brief's contract or inputs is
// recognisable by content whatever its file name.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	// r4SizeErr is the size-limit refusal R5a names.
	r4SizeErr = "task revision exceeds size limit"
	// r4PlanReqs is the number of plan requirements; every brief requirement
	// links all of them (R7 allows 1..256 distinct criteria per entry).
	r4PlanReqs = 256
	// r4BriefReqs is the R7 maximum of requirements per brief.
	r4BriefReqs = 256
	// r4MaxBriefs bounds the growth loop. With the sizes above each accepted
	// assignment adds several MiB, so the limit is reached within a handful
	// of assigns; the bound only stops a record that never grows.
	r4MaxBriefs = 48
)

// r4PlanIDs are the plan requirement IDs: 48 characters each.
func r4PlanIDs() []string {
	ids := make([]string, r4PlanReqs)
	for i := range ids {
		ids[i] = fmt.Sprintf("C%03d", i) + strings.Repeat("x", 44)
	}
	return ids
}

// r4ReqID is a 64-character brief requirement ID, the longest R7 allows
// (^[A-Za-z][A-Za-z0-9_.-]{0,63}$).
func r4ReqID(j int) string {
	return fmt.Sprintf("R%03d", j) + strings.Repeat("y", 60)
}

// r4ExtraGo declares G00..G<r4MaxBriefs-1> in package p so each brief can
// carry a contract extract of its own.
func r4ExtraGo() string {
	var b strings.Builder
	b.WriteString("package p\n\n")
	for i := 0; i < r4MaxBriefs; i++ {
		fmt.Fprintf(&b, "// G%02d is a contract symbol of brief %02d.\nfunc G%02d() int { return %d }\n\n", i, i, i, i)
	}
	return b.String()
}

// r4Fixture is a task whose plan can prove tests (the default check is
// go test -json ./p/...), so a contract-bearing brief must declare
// requirements (R8).
func r4Fixture(t *testing.T) *l3Fix {
	t.Helper()
	return l3New(t, l3Opts{
		files: map[string]string{"p/extra.go": r4ExtraGo()},
		reqs:  r4PlanIDs(),
	})
}

// r4Brief is the brief of index i together with the bytes handed with it.
type r4Brief struct {
	brief    taskflow.WorkerBrief
	contract []byte   // bytes of the brief's contract extract
	inputs   [][]byte // bytes of each input, in brief order
}

// briefR4 builds the contract-bearing brief of index i: its own contract
// extract (symbols F and G<ii>), two inputs outside the project with bytes
// unique to the brief, and 256 requirements each linking every plan
// requirement.
func (f *l3Fix) briefR4(i int, planIDs []string) r4Brief {
	f.t.Helper()
	id := fmt.Sprintf("r4a%02d", i)
	dir := filepath.Join(f.root, "r4", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	extract, err := taskflow.ExtractContract(filepath.Join(f.sel, "p"), []string{"F", fmt.Sprintf("G%02d", i)})
	if err != nil {
		f.t.Fatalf("ExtractContract for %s: %v", id, err)
	}
	cdata, err := taskflow.EncodeContract(extract)
	if err != nil {
		f.t.Fatalf("EncodeContract for %s: %v", id, err)
	}
	cpath := filepath.Join(dir, "contract.json")
	if err := os.WriteFile(cpath, cdata, 0o600); err != nil {
		f.t.Fatal(err)
	}
	b := f.plainBrief(id, []string{f.p(fmt.Sprintf("p/r4_%02d_test.go", i))})
	b.Contract = &taskflow.ContractRef{Path: cpath, SHA256: l3Sum(cdata)}
	out := r4Brief{contract: cdata}
	for k, name := range []string{"notes.md", "data.json"} {
		data := []byte(fmt.Sprintf("input %d of brief %s\n", k, id))
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			f.t.Fatal(err)
		}
		b.Inputs = append(b.Inputs, taskflow.InputRef{Path: p, SHA256: l3Sum(data)})
		out.inputs = append(out.inputs, data)
	}
	for j := 0; j < r4BriefReqs; j++ {
		b.Requirements = append(b.Requirements, l3Req(r4ReqID(j), planIDs...))
	}
	out.brief = b
	return out
}

// r4AssignErr calls AssignWorker, turning a panic of an unimplemented stub
// into an error so the other tests of the binary still run.
func r4AssignErr(ctx context.Context, dir string, b taskflow.WorkerBrief) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("AssignWorker panicked: %v", r)
		}
	}()
	_, err = taskflow.AssignWorker(ctx, dir, b)
	return err
}

// r4Added lists the entries of after that before does not hold.
func r4Added(before, after map[string]string) []string {
	var added []string
	for k := range after {
		if _, ok := before[k]; !ok {
			added = append(added, k)
		}
	}
	return added
}

// r4FilesWithSum lists the regular files of tree whose bytes hash to sum.
func r4FilesWithSum(tree map[string]string, sum string) []string {
	var out []string
	for k, v := range tree {
		if strings.HasPrefix(v, "-") && strings.HasSuffix(v, " "+sum) {
			out = append(out, k)
		}
	}
	return out
}

// checkCopiesR5 asserts R5 for the accepted assignment of rb: input_files
// names one input-<id>-<32 hex> copy per input, in brief order, each a 0600
// regular file in the task directory holding the input's bytes, added by this
// assign; and contract_file names a copy holding the contract's bytes, also
// added by this assign (the contract is unique to the brief).
func (f *l3Fix) checkCopiesR5(rb r4Brief, before, after map[string]string) {
	f.t.Helper()
	id := rb.brief.ID
	var a *taskflow.WorkerAssignment
	for _, x := range f.load().Assignments {
		if x.Brief.ID == id {
			x := x
			a = &x
		}
	}
	if a == nil {
		f.t.Fatalf("R5: accepted assign %s is not in the loaded record", id)
	}
	if len(a.InputFiles) != len(rb.inputs) {
		f.t.Fatalf("R5: assignment %s input_files = %q, want one name per input (%d)", id, a.InputFiles, len(rb.inputs))
	}
	name := regexp.MustCompile(`^input-` + regexp.QuoteMeta(id) + `-[0-9a-f]{32}$`)
	for k, n := range a.InputFiles {
		if !name.MatchString(n) {
			f.t.Errorf("R5: assignment %s input_files[%d] = %q, want input-%s-<32 lowercase hex>", id, k, n, id)
			continue
		}
		p := filepath.Join(f.dir, n)
		info, err := os.Lstat(p)
		if err != nil {
			f.t.Errorf("R5: assignment %s input copy %s: %v", id, n, err)
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			f.t.Errorf("R5: assignment %s input copy %s has mode %v, want a 0600 regular file", id, n, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil || string(data) != string(rb.inputs[k]) {
			f.t.Errorf("R5: assignment %s input copy %s holds %q (%v), want the input's bytes %q", id, n, data, err, rb.inputs[k])
		}
		if _, ok := after[p]; ok {
			if _, old := before[p]; old {
				f.t.Errorf("R5: assignment %s input copy %s existed before its assign", id, n)
			}
		}
	}
	if a.ContractFile == "" {
		f.t.Errorf("R5 control: assignment %s has no contract_file, so the refused brief's absent contract copy would prove nothing", id)
		return
	}
	cp := filepath.Join(f.dir, filepath.FromSlash(a.ContractFile))
	data, err := os.ReadFile(cp)
	if err != nil || string(data) != string(rb.contract) {
		f.t.Errorf("R5 control: assignment %s contract copy %s does not hold the contract's bytes (%v)", id, a.ContractFile, err)
	}
	if _, old := before[cp]; old {
		f.t.Errorf("R5 control: assignment %s contract copy %s existed before its assign", id, a.ContractFile)
	}
}

// TestR5aAssignOverRevisionSizeLimit fails when an assign refused because its
// revision would exceed the size limit returns no error or another one,
// writes a revision, records the assignment, or leaves a stored input or
// contract copy (or any other new file) in the task directory; and, as the
// control, when an accepted assign of the same shape stores no copies (then
// the absent copies would prove nothing).
func TestR5aAssignOverRevisionSizeLimit(t *testing.T) {
	t.Parallel()
	planIDs := r4PlanIDs()
	f := r4Fixture(t)

	// Grow the record with briefs of one shape until one is refused. Every
	// accepted one, the first on a task far from the limit included, must
	// store and name its copies (R5).
	var refused r4Brief
	var refusedIndex = -1
	var refusedErr error
	var before map[string]string
	var revBefore int
	for i := 0; i < r4MaxBriefs; i++ {
		rb := f.briefR4(i, planIDs)
		before = l3Tree(t, f.dir)
		revBefore = f.revision()
		err := r4AssignErr(f.ctx, f.dir, rb.brief)
		if err == nil {
			after := l3Tree(t, f.dir)
			f.checkCopiesR5(rb, before, after)
			if t.Failed() {
				t.FailNow()
			}
			continue
		}
		if i == 0 {
			t.Fatalf("control: the first assign, on a task far from the size limit, was refused: %v", err)
		}
		if !strings.Contains(err.Error(), r4SizeErr) {
			t.Fatalf("R5a: assign %s after %d accepted assigns of the same shape failed with %q, want an error containing %q", rb.brief.ID, i, err, r4SizeErr)
		}
		refused, refusedIndex, refusedErr = rb, i, err
		break
	}
	if refusedIndex < 0 {
		t.Fatalf("R5a: %d assigns of a brief with 256 requirements x %d criteria were all accepted; the record never reached the revision size limit", r4MaxBriefs, r4PlanReqs)
	}
	t.Logf("assign %d refused after %d accepted: %v", refusedIndex, refusedIndex, refusedErr)

	t.Run("refused_assign_leaves_task_unchanged", func(t *testing.T) {
		id := refused.brief.ID
		rec, err := taskflow.Load(f.dir)
		if err != nil {
			t.Fatalf("R5a: Load after the refused assign %s: %v", id, err)
		}
		if rec.Revision != revBefore {
			t.Errorf("R5a: refused assign %s moved the record from revision %d to %d", id, revBefore, rec.Revision)
		}
		for _, a := range rec.Assignments {
			if a.Brief.ID == id {
				t.Errorf("R5a: refused assign %s is recorded as an assignment (input_files %q, contract_file %q)", id, a.InputFiles, a.ContractFile)
			}
		}
		after := l3Tree(t, f.dir)
		if added := r4Added(before, after); len(added) > 0 {
			t.Errorf("R5a: refused assign %s left new entries in the task directory: %q", id, added)
		}
		glob, err := filepath.Glob(filepath.Join(f.dir, "input-"+id+"-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(glob) > 0 {
			t.Errorf("R5a: refused assign %s left input copies %q", id, glob)
		}
		if got := r4FilesWithSum(after, l3Sum(refused.contract)); len(got) > 0 {
			t.Errorf("R5a: refused assign %s left a stored copy of its contract extract: %q", id, got)
		}
		for k, in := range refused.inputs {
			if got := r4FilesWithSum(after, l3Sum(in)); len(got) > 0 {
				t.Errorf("R5a: refused assign %s left a stored copy of input %d: %q", id, k, got)
			}
		}
		// I4: copies stored by earlier assigns are never rewritten.
		for _, a := range rec.Assignments {
			names := append([]string{}, a.InputFiles...)
			if a.ContractFile != "" {
				names = append(names, a.ContractFile)
			}
			for _, n := range names {
				p := filepath.Join(f.dir, filepath.FromSlash(n))
				if before[p] == "" || after[p] != before[p] {
					t.Errorf("I4: refused assign %s changed or removed %s of assignment %s", id, n, a.Brief.ID)
				}
			}
		}
	})

	t.Run("same_brief_on_small_task_stores_copies", func(t *testing.T) {
		// Control: the refused brief's own shape (its ID, owned path,
		// requirements and inputs, with this task's contract extract of the
		// same symbols) is accepted on a fresh task and stores its copies.
		g := r4Fixture(t)
		rb := g.briefR4(refusedIndex, planIDs)
		before := l3Tree(t, g.dir)
		if err := r4AssignErr(g.ctx, g.dir, rb.brief); err != nil {
			t.Fatalf("control: brief %s on a fresh task was refused: %v", rb.brief.ID, err)
		}
		g.checkCopiesR5(rb, before, l3Tree(t, g.dir))
	})
}
