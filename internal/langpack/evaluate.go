package langpack

import (
	"context"
	"fmt"
	"slices"
)

// GoPackageTestV2 names the Go package-test rule that GoEvidence applies:
// it also requires -race, reverse-dependency and full runs, and gives no
// credit to reruns.
const GoPackageTestV2 = "go.package-test/v2"

// KnownRule reports whether id names an evidence rule built into this cw.
func KnownRule(id string) bool { return slices.Contains(builtInRules, id) }

// builtInRules are the evidence rule ids Evaluate dispatches; each has a case
// there.
var builtInRules = []string{GoPackageTestV2}

// Evidence is what Evaluate reports: problems fail verify, limitations are
// listed.
type Evidence struct {
	Problems    []string
	Limitations []string
}

// Evaluate applies AnyPassedRun, then the rule each distinct id names, in
// order. An id this cw does not know adds a limitation and only the fallback
// applies for it. dir is the selected path and env the environment for rules
// that run toolchain commands: go.package-test/v2 runs go through
// LoadGoFacts, with ctx.
func Evaluate(ctx context.Context, dir string, env []string, ruleIDs []string, source Source, passed []Run) Evidence {
	evidence := Evidence{Problems: AnyPassedRun{}.Problems(source, passed)}
	for i, id := range ruleIDs {
		if slices.Contains(ruleIDs[:i], id) {
			continue
		}
		switch id {
		case GoPackageTestV2:
			switch {
			case !slices.Contains(source.Current, "go.mod"):
				evidence.Limitations = append(evidence.Limitations, "evidence rule "+GoPackageTestV2+" needs go.mod at the selected path; only the fallback rule applied for it")
			case len(source.Changed) > 0:
				v2 := GoEvidence(source, passed, LoadGoFacts(ctx, dir, env, source))
				evidence.Problems = append(evidence.Problems, v2.Problems...)
				evidence.Limitations = append(evidence.Limitations, v2.Limitations...)
			}
		default:
			evidence.Limitations = append(evidence.Limitations, fmt.Sprintf("evidence rule %s is not built into this cw; only the fallback rule applied for it", id))
		}
	}
	return evidence
}
