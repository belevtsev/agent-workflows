package langpack

import (
	"context"
	"fmt"
	"slices"
)

// GoPackageTestV1 names GoTests, the Go package-test rule, unchanged.
const GoPackageTestV1 = "go.package-test/v1"

// KnownRule reports whether id names an evidence rule built into this cw.
func KnownRule(id string) bool { return slices.Contains(builtInRules, id) }

// builtInRules are the evidence rule ids Evaluate dispatches; each has a case
// there.
var builtInRules = []string{GoPackageTestV1}

// Evidence is what Evaluate reports: problems fail verify, limitations are
// listed.
type Evidence struct {
	Problems    []string
	Limitations []string
}

// Evaluate applies AnyPassedRun, then the rule each distinct id names, in
// order. An id this cw does not know adds a limitation and only the fallback
// applies for it. dir is the selected path and env the environment for rules
// that run toolchain commands; go.package-test/v1 runs none.
func Evaluate(ctx context.Context, dir string, env []string, ruleIDs []string, source Source, passed []Run) Evidence {
	evidence := Evidence{Problems: AnyPassedRun{}.Problems(source, passed)}
	for i, id := range ruleIDs {
		if slices.Contains(ruleIDs[:i], id) {
			continue
		}
		switch id {
		case GoPackageTestV1:
			if (GoTests{}).Applies(source) {
				evidence.Problems = append(evidence.Problems, GoTests{}.Problems(source, passed)...)
			}
		default:
			evidence.Limitations = append(evidence.Limitations, fmt.Sprintf("evidence rule %s is not built into this cw; only the fallback rule applied for it", id))
		}
	}
	return evidence
}
