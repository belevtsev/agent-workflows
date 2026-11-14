package taskflow_test

// A contract needs a plan that can prove tests (interface contract L4, third
// version): R17, and L3 R8 for small records.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestAssignContractNeedsProvingPlan fails when a gated record whose plan
// cannot prove tests accepts a brief with a contract, refuses it with another
// message, lets L3 R8's refusal of requirements or input verification come
// first, or refuses it before the shape, ownership and contract-extract
// checks; or when it refuses a brief without a contract (R17).
func TestAssignContractNeedsProvingPlan(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{l3PlainP, {"go", "test", "-json", "-run", "TestPass", "./p/..."}, {"go", "test", "-json", "./p/...", "-args", "-x"}} {
		f := l4New(t, l4Opts{checks: []taskflow.Check{l3Check("unit", argv...)}})
		_ = f.assignFails(f.brief("te1", []string{"p/te_test.go"}), l4R17)
		// R17 takes precedence over L3 R8's refusal of requirements.
		_ = f.assignFails(f.brief("te1b", []string{"p/te1b_test.go"}, l3Req("R1", "C1")), l4R17)
		// R17 runs before input verification.
		withInput := f.brief("te1c", []string{"p/te1c_test.go"})
		in := filepath.Join(f.root, "input.txt")
		if err := os.WriteFile(in, []byte("input\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		withInput.Inputs = []taskflow.InputRef{{Path: in, SHA256: strings.Repeat("0", 64)}}
		_ = f.assignFails(withInput, l4R17)
		for _, a := range f.load().Assignments {
			if a.Brief.Contract != nil {
				t.Errorf("argv %q: a refused brief left assignment %s with contract file %q (R17)", argv, a.Brief.ID, a.ContractFile)
			}
		}
		// Checks that run before R17 keep their own refusals.
		unclean := f.brief("te2", []string{"p/../p/te_test.go"})
		if err := f.assignFails(unclean, ""); err != nil && strings.Contains(err.Error(), l4R17) {
			t.Errorf("argv %q: an unclean owned path was refused by R17, want the ownership refusal first: %v", argv, err)
		}
		data, err := os.ReadFile(f.contract.Path)
		if err != nil {
			t.Fatal(err)
		}
		stale := filepath.Join(f.root, "stale-contract.json")
		changed := append(append([]byte{}, data...), '\n')
		if err := os.WriteFile(stale, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		staleBrief := f.brief("te3", []string{"p/te3_test.go"})
		staleBrief.Contract = &taskflow.ContractRef{Path: stale, SHA256: l3Sum(changed)}
		if err := f.assignFails(staleBrief, ""); err != nil && strings.Contains(err.Error(), l4R17) {
			t.Errorf("argv %q: an extract that differs from the source was refused by R17, want the contract-extract refusal first: %v", argv, err)
		}
		f.mustAssign(f.plainBrief("impl", []string{"p/x.go"}))
		overlap := f.brief("te4", []string{"p/x.go"})
		if err := f.assignFails(overlap, ""); err != nil && strings.Contains(err.Error(), l4R17) {
			t.Errorf("argv %q: an overlapping owned path was refused by R17, want the ownership refusal first: %v", argv, err)
		}
	}
}

// TestAssignContractWhenR17DoesNotApply fails when a contract-bearing brief
// is refused by R17 in a gated record whose plan can prove tests or in a
// record whose plan is small, or when L3 R8 stops applying there (R17).
func TestAssignContractWhenR17DoesNotApply(t *testing.T) {
	t.Parallel()
	g := l4New(t, l4Opts{})
	g.mustAssign(g.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))

	f := l4New(t, l4Opts{small: true, checks: []taskflow.Check{l3Check("unit", l3PlainP...)}})
	// L3 R8: requirements are refused when the plan cannot prove tests, but
	// not by R17's message.
	if err := f.assignFails(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")), ""); err != nil && strings.Contains(err.Error(), l4R17) {
		t.Errorf("small: requirements refused with R17's message: %v (R17 applies to gated records)", err)
	}
	if err := f.assign(f.brief("te1", []string{"p/te_test.go"})); err != nil {
		t.Errorf("small: a contract without requirements was refused: %v (L3 R8 applies unchanged)", err)
	}
}
