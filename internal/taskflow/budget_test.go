package taskflow

import (
	"errors"
	"strings"
	"testing"
)

// budgetSample is one known sample of session s1 at usd, priced by the
// built-in table.
func budgetSample(session string, usd float64) UsageSample {
	return UsageSample{Source: UsageSourceClaude, SessionID: session, Tokens: &UsageTokens{Output: 1}, USD: &usd, USDEstimated: true,
		DollarSource: &DollarSource{Kind: PriceTableBuiltIn, Date: "2026-11-13", SHA256: strings.Repeat("ab", 32)}}
}

// unknownSample is a sample of session whose dollars are unknown.
func unknownSample(session string) UsageSample {
	return UsageSample{Source: UsageSourceClaude, SessionID: session, Tokens: &UsageTokens{Output: 1}, UnknownReason: "the price table has no rates for model \"m\""}
}

// costApprovedAssignment is an assignment that carries the owner's
// cost-budget approval.
func costApprovedAssignment() WorkerAssignment {
	return WorkerAssignment{OwnerApprovals: []OwnerApproval{{Text: "go on", For: ApprovalCostBudget}}}
}

// Oracle: with a 1 USD budget that stops at 80%, the state is stop exactly
// when the task's estimated dollars (the sum of each session's latest
// sample) are at least 0.80 USD; unknown when any session's latest dollars
// are unknown, never ok or stop as if they were 0; ok without a profile
// whatever the samples. After the owner's cost-budget approval the stop is
// 1 USD: 0.85 is ok, 1.00 and 1.20 stop.
func TestBudgetStatePerSampleSet(t *testing.T) {
	budget := &Profile{Name: "lite", Budget: Budget{MaxUSD: 1, StopAt: 0.8}}
	approved := []WorkerAssignment{costApprovedAssignment()}
	for _, c := range []struct {
		name        string
		profile     *Profile
		samples     []UsageSample
		assignments []WorkerAssignment
		want        BudgetState
		detail      string
	}{
		{"no sample", budget, nil, nil, BudgetUnknown, "no usage sample is recorded"},
		{"below 80%", budget, []UsageSample{budgetSample("s1", 0.79)}, nil, BudgetOK, "estimated 0.79 USD, 79% of its 1.00 USD budget"},
		{"at 80%", budget, []UsageSample{budgetSample("s1", 0.8)}, nil, BudgetStop, "estimated 0.80 USD, 80% of its 1.00 USD budget, which stops new work at 80%"},
		{"above 80% over two sessions", budget, []UsageSample{budgetSample("s1", 0.5), budgetSample("s2", 0.35)}, nil, BudgetStop, "estimated 0.85 USD, 85%"},
		{"nil dollars", budget, []UsageSample{budgetSample("s1", 0.9), unknownSample("s2")}, nil, BudgetUnknown, "the latest sample's dollars are unknown"},
		{"no profile", nil, []UsageSample{budgetSample("s1", 5)}, nil, BudgetOK, "no profile"},
		{"approved below 100%", budget, []UsageSample{budgetSample("s1", 0.85)}, approved, BudgetOK, "which stops new work at 100%"},
		{"approved at 100%", budget, []UsageSample{budgetSample("s1", 1)}, approved, BudgetStop, "100% of its 1.00 USD budget"},
		{"approved above 100%", budget, []UsageSample{budgetSample("s1", 1.2)}, approved, BudgetStop, "120%"},
	} {
		t.Run(c.name, func(t *testing.T) {
			record := Record{Profile: c.profile, Usage: c.samples, Assignments: c.assignments}
			got, detail := budgetState(record)
			if got != c.want || !strings.Contains(detail, c.detail) {
				t.Fatalf("budgetState = %s, %q; want %s with %q", got, detail, c.want, c.detail)
			}
			// The refusal follows the state: only stop refuses, typed both
			// as ErrOverBudget and as ErrBudgetStop.
			err := budgetRefusal(record)
			if (err != nil) != (c.want == BudgetStop) {
				t.Fatalf("budgetRefusal = %v at state %s", err, c.want)
			}
			if err != nil {
				over, ok := errors.AsType[ErrBudgetStop](err)
				if !errors.Is(err, ErrOverBudget) || !ok || !strings.Contains(err.Error(), budgetQuestion) {
					t.Fatalf("refusal %v is not an ErrOverBudget and ErrBudgetStop asking %q", err, budgetQuestion)
				}
				// Final: at 100% no approval continues the task.
				if over.Final() != (*c.samples[len(c.samples)-1].USD >= 1) {
					t.Fatalf("Final() = %v for %+v", over.Final(), over)
				}
			}
		})
	}
}
