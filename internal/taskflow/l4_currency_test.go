package taskflow_test

// Contract currency (interface contract L4, third version): R18, with I8
// and R3d.

import (
	"fmt"
	"testing"
)

// TestContractSupersession fails when, after the contract package changes,
// verify's staleness report of the earlier round A does not follow R18: in a
// record with test rules only a later covering round B with an accepted and
// no rejected acceptance supersedes A (an accepted round later closed unused
// still does); in a legacy record any later covering round does (R18, R3d,
// I8).
func TestContractSupersession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		legacy     bool
		aOwned     []string
		bOwned     []string
		judge      []string // acceptances of B, in order; nil records none
		bFirst     bool     // B is assigned before A
		superseded bool
	}{
		{"accepted covering round", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"accepted"}, false, true},
		{"covering by directory", false, []string{"p/a_test.go", "p/b_test.go"}, []string{"p"}, []string{"accepted"}, false, true},
		{"accepted then unused", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"accepted", "unused"}, false, true},
		{"never judged", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, nil, false, false},
		{"closed unused, never accepted", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"unused"}, false, false},
		{"accepted then rejected", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"accepted", "rejected"}, false, false},
		{"rejected then accepted", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"rejected", "accepted"}, false, false},
		{"rejected", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"rejected"}, false, false},
		{"partly covering", false, []string{"p/a_test.go", "p/b_test.go"}, []string{"p/a_test.go"}, []string{"accepted"}, false, false},
		{"earlier round does not supersede", false, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"accepted"}, true, false},
		{"legacy: never judged", true, []string{"p/a_test.go"}, []string{"p/a_test.go"}, nil, false, true},
		{"legacy: rejected", true, []string{"p/a_test.go"}, []string{"p/a_test.go"}, []string{"rejected"}, false, true},
		{"legacy: partly covering", true, []string{"p/a_test.go", "p/b_test.go"}, []string{"p/a_test.go"}, nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := l4New(t, l4Opts{legacy: c.legacy})
			round := func(id string, owned []string) {
				files := map[string]string{}
				for _, p := range owned {
					if p == "p" {
						p = "p/a_test.go"
					}
					files[p] = l4Test("p", "Test"+id)
				}
				f.mustAssign(f.brief(id, owned, l3Req("R1", "C1")))
				f.apply(files)
				var changed []string
				for p := range files {
					changed = append(changed, p)
				}
				f.mustSubmit(f.result(id, "completed", changed, l3Uncovered("R1", l4UncoveredReason)))
			}
			first, second := "A", "B"
			firstOwned, secondOwned := c.aOwned, c.bOwned
			if c.bFirst {
				first, second = "B", "A"
				firstOwned, secondOwned = c.bOwned, c.aOwned
			}
			round(first, firstOwned)
			if first == "A" {
				f.accept("A", "accepted")
			} else {
				for _, j := range c.judge {
					f.accept("B", j)
				}
			}
			round(second, secondOwned)
			if second == "B" {
				for _, j := range c.judge {
					f.accept("B", j)
				}
			} else {
				f.accept("A", "accepted")
			}
			// Change the contract package's declaration: every contract
			// extracted before is stale.
			f.write("p/p.go", "package p\n\n// F is the contract symbol.\nfunc F() int64 { return 1 }\n")
			v := f.verify()
			aStale := l4HasPrefix(v.Problems, fmt.Sprintf(l4StalePrefix, "A"))
			if aStale == c.superseded {
				t.Errorf("%s: verify reports A's contract stale = %v, want %v (superseded %v): problems %q (R18)", c.name, aStale, !c.superseded, c.superseded, v.Problems)
			}
			// The latest round is never superseded: its staleness is reported
			// by the existing rule.
			if !c.bFirst && !l4HasPrefix(v.Problems, fmt.Sprintf(l4StalePrefix, "B")) {
				t.Errorf("%s: verify does not report B's stale contract: %q (R18: the existing rule)", c.name, v.Problems)
			}
		})
	}
}
