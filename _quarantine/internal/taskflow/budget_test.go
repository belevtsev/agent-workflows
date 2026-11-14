package taskflow

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// budgetSample is one known sample of session at usd, priced by the
// built-in table (or, with table "b", a byte-different one).
func budgetSample(session string, usd float64) UsageSample {
	return tableSample(session, "ab", usd)
}

func tableSample(session, table string, usd float64) UsageSample {
	return UsageSample{Source: UsageSourceClaude, SessionID: session, Tokens: &UsageTokens{Output: 1}, USD: &usd, USDEstimated: true,
		DollarSource: &DollarSource{Kind: PriceTableBuiltIn, Date: "2026-11-13", SHA256: strings.Repeat(table, 64/len(table))}}
}

// unknownSample is a sample of session whose dollars are unknown.
func unknownSample(session string) UsageSample {
	return UsageSample{Source: UsageSourceClaude, SessionID: session, Tokens: &UsageTokens{Output: 1}, UnknownReason: "the price table has no rates for model \"m\""}
}

// costApproval is the owner's cost-budget approval on the record.
var costApproval = []OwnerApproval{{Text: "go on", For: ApprovalCostBudget}}

// Oracle: with a 1 USD budget that stops at 80%, the stop is judged on the
// known lower bound, each session's highest known dollars summed: at least
// 0.80 USD stops; an unknown session or a later unknown, differently priced
// or lower sample never lifts a stop, at 85% or at 120%; a total that is
// unknown below the stop is unknown, never ok or stop as if it were 0; no
// profile is always ok. After the owner's cost-budget approval the stop is
// 1 USD: 0.85 is ok, 1.00 and 1.20 stop. Final follows the task's total.
func TestBudgetStatePerSampleSet(t *testing.T) {
	budget := &Profile{Name: "lite", Budget: Budget{MaxUSD: 1, StopAt: 0.8}}
	for _, c := range []struct {
		name      string
		profile   *Profile
		samples   []UsageSample
		approvals []OwnerApproval
		want      BudgetState
		detail    string
		total     float64
	}{
		{"no sample", budget, nil, nil, BudgetUnknown, "no usage sample is recorded", 0},
		{"below 80%", budget, []UsageSample{budgetSample("s1", 0.79)}, nil, BudgetOK, "estimated 0.79 USD, 79% of its 1.00 USD budget", 0.79},
		{"at 80%", budget, []UsageSample{budgetSample("s1", 0.8)}, nil, BudgetStop, "estimated 0.80 USD, 80% of its 1.00 USD budget, which stops new work at 80%", 0.8},
		{"above 80% over two sessions", budget, []UsageSample{budgetSample("s1", 0.5), budgetSample("s2", 0.35)}, nil, BudgetStop, "estimated 0.85 USD, 85%", 0.85},
		{"unknown below the stop", budget, []UsageSample{budgetSample("s1", 0.5), unknownSample("s2")}, nil, BudgetUnknown, "the latest sample's dollars are unknown", 0.5},
		{"unknown session past the stop", budget, []UsageSample{budgetSample("s1", 0.85), unknownSample("s2")}, nil, BudgetStop, "at least an estimated 0.85 USD, 85%", 0.85},
		{"unknown after 85%", budget, []UsageSample{budgetSample("s1", 0.85), unknownSample("s1")}, nil, BudgetStop, "at least an estimated 0.85 USD", 0.85},
		{"another table after 85%", budget, []UsageSample{budgetSample("s1", 0.85), tableSample("s1", "cd", 0.86)}, nil, BudgetStop, "at least an estimated 0.86 USD", 0.86},
		{"unknown after 120%", budget, []UsageSample{budgetSample("s1", 1.2), unknownSample("s1")}, costApproval, BudgetStop, "at least an estimated 1.20 USD, 120%", 1.2},
		{"no profile", nil, []UsageSample{budgetSample("s1", 5)}, nil, BudgetOK, "no profile", 5},
		{"approved below 100%", budget, []UsageSample{budgetSample("s1", 0.85)}, costApproval, BudgetOK, "which stops new work at 100%", 0.85},
		{"approved at 100%", budget, []UsageSample{budgetSample("s1", 1)}, costApproval, BudgetStop, "100% of its 1.00 USD budget", 1},
		{"approved above 100% over two sessions", budget, []UsageSample{budgetSample("s1", 0.7), budgetSample("s2", 0.5)}, costApproval, BudgetStop, "120%", 1.2},
	} {
		t.Run(c.name, func(t *testing.T) {
			record := Record{Profile: c.profile, Usage: c.samples, OwnerApprovals: c.approvals}
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
				// Final: at 100% of the task's total no approval continues.
				if over.Final() != (c.total >= 1) {
					t.Fatalf("Final() = %v for %+v, total %v", over.Final(), over, c.total)
				}
			}
		})
	}
}

// budgetLoadTask is a lite task (15 USD, stop at 12) whose stored usage is
// usd, reloaded.
func budgetLoadTask(t *testing.T, usd float64) Record {
	t.Helper()
	record, _ := stageTask(t, false)
	appendWorkerRevision(t, record, func(r *Record) {
		r.Usage = append(r.Usage, budgetSample("s1", usd))
		r.Usage[len(r.Usage)-1].At = r.UpdatedAt
	})
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("precondition: the usage revision loads: %v", err)
	}
	return loaded
}

// Oracle: the budget holds for stored revisions too. A hand-written
// revision that adds an assignment at 13 USD (past lite's 12 USD stop)
// without a cost-budget approval on the record fails to load; with one in
// the same revision it loads. At 16 USD (past 15) it fails to load even with
// one. An approval stored below the stop fails to load.
func TestStoredWorkPastTheBudgetStopNeedsTheOwnersApproval(t *testing.T) {
	for _, c := range []struct {
		name      string
		usd       float64
		approvals []OwnerApproval
		assign    bool
		refused   string
	}{
		{"over the stop without approval", 13, nil, true, "adds an assignment or a check run at the task's budget stop"},
		{"over the stop with approval", 13, costApproval, true, ""},
		{"past the whole budget with approval", 16, costApproval, true, "cost-budget approval was given when the task was not between its budget's stop and 100%"},
		{"past the whole budget without approval", 16, nil, true, "adds an assignment or a check run at the task's budget stop"},
		{"approval below the stop", 5, costApproval, false, "cost-budget approval was given when the task was not between its budget's stop and 100%"},
	} {
		t.Run(c.name, func(t *testing.T) {
			record := budgetLoadTask(t, c.usd)
			assigned := WorkerAssignment{TaskID: record.ID, Brief: stageBrief("tx", "implementation", StageTests, "source.txt"), Snapshot: currentWorkerSnapshot(t, record), AssignedAt: time.Now().UTC()}
			var err error
			if assigned.SHA256, err = assignmentHash(assigned); err != nil {
				t.Fatal(err)
			}
			appendWorkerRevision(t, record, func(r *Record) {
				for _, approval := range c.approvals {
					approval.At = r.UpdatedAt
					r.OwnerApprovals = append(r.OwnerApprovals, approval)
				}
				if c.assign {
					r.Assignments = append(r.Assignments, assigned)
					r.EvidenceRevision++
					r.Verification = nil
				}
			})
			_, err = Load(record.Directory)
			if c.refused == "" && err != nil || c.refused != "" && (err == nil || !strings.Contains(err.Error(), c.refused)) {
				t.Fatalf("Load = %v, want refusal %q", err, c.refused)
			}
		})
	}
}
