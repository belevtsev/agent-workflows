package taskflow

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

// BudgetState is where a task stands against its cost budget.
type BudgetState string

// The budget states budgetState returns.
const (
	// BudgetOK: the task may take new work.
	BudgetOK BudgetState = "ok"
	// BudgetStop: the task's estimated dollars reached the stop, so it
	// takes no new work until the owner decides.
	BudgetStop BudgetState = "stop"
	// BudgetUnknown: the task's dollars are unknown and their known part is
	// below the stop. Unknown usage never starts a stop and never counts as
	// 0; verify discloses it.
	BudgetUnknown BudgetState = "unknown"
)

// ApprovalCostBudget is OwnerApproval.For of the owner's approval to go past
// the budget's stop. Cost is task-level, so the approval is stored on the
// record (Record.OwnerApprovals), not on an assignment; it raises the stop to
// 100% of the budget, once.
const ApprovalCostBudget = "cost-budget"

// budgetState judges the task's usage against its profile's budget and
// returns the state with a detail that says why. A record without a profile
// has no budget and is always ok. The stop is StopAt of MaxUSD, or all of
// MaxUSD once the record holds the owner's cost-budget approval; at 100% only
// a new task continues.
//
// The stop is judged on a known lower bound (knownFloor): each session's
// highest known dollars, summed. Unknown dollars therefore never start a
// stop, and a later unknown or differently priced sample never lifts one.
// Dollars are estimated, so only their amount counts; tokens never stop a
// task (OD-3).
func budgetState(record Record) (BudgetState, string) {
	if record.Profile == nil {
		return BudgetOK, "the task records no profile, so it has no budget"
	}
	reading := readBudget(record)
	switch {
	case reading.floor >= reading.stop():
		return BudgetStop, reading.String()
	case reading.unknown != "":
		return BudgetUnknown, reading.unknown
	}
	return BudgetOK, reading.String()
}

// budgetReading is a task's estimated dollars against its budget.
type budgetReading struct {
	// floor is the known lower bound of the task's dollars; unknown, when
	// not empty, says why the whole amount is unknown.
	floor   float64
	unknown string
	budget  Budget
	// approved reports that the record holds the owner's cost-budget
	// approval, which moved the stop to all of MaxUSD.
	approved bool
}

func readBudget(record Record) budgetReading {
	reading := budgetReading{floor: knownFloor(record), budget: record.Profile.Budget, approved: costApproved(record.OwnerApprovals)}
	if usage := UsageOf(record); usage.USD == nil {
		reading.unknown = usage.UnknownReason
	}
	return reading
}

// knownFloor is a lower bound of the task's estimated dollars: per session,
// the highest dollars any of its samples knew, summed over the sessions. A
// session's dollars only grow, so a later sample that is unknown, priced by
// another table or lower (a stale copy) cannot lower it.
func knownFloor(record Record) float64 {
	type key struct{ source, session string }
	highest := map[key]float64{}
	for _, sample := range record.Usage {
		if sample.USD != nil {
			k := key{sample.Source, sample.SessionID}
			highest[k] = max(highest[k], *sample.USD)
		}
	}
	total := 0.0
	for _, amount := range highest {
		total += amount
	}
	return total
}

// stop is the amount, in dollars, at which the task takes no new work.
func (r budgetReading) stop() float64 {
	if r.approved {
		return r.budget.MaxUSD
	}
	return r.budget.StopAt * r.budget.MaxUSD
}

func (r budgetReading) String() string {
	amount := "an estimated " + dollars(r.floor)
	if r.unknown != "" {
		amount = "at least an estimated " + dollars(r.floor)
	}
	return fmt.Sprintf("task usage is %s USD, %s%% of its %s USD budget, which stops new work at %s%%",
		amount, percent(r.floor/r.budget.MaxUSD), dollars(r.budget.MaxUSD), percent(r.stop()/r.budget.MaxUSD))
}

func dollars(amount float64) string { return strconv.FormatFloat(amount, 'f', 2, 64) }

func percent(share float64) string { return strconv.FormatFloat(share*100, 'f', 0, 64) }

// costApproved reports whether approvals hold the owner's cost-budget
// approval.
func costApproved(approvals []OwnerApproval) bool {
	return slices.ContainsFunc(approvals, func(a OwnerApproval) bool { return a.For == ApprovalCostBudget })
}

// budgetBand places the task's known dollars against the budget: 0 below
// StopAt of MaxUSD, 1 from there, 2 from MaxUSD. A sample that changes the
// band is always stored (usageWorthStoring), so the stop never waits for the
// 5% rule.
func budgetBand(record Record) int {
	if record.Profile == nil {
		return 0
	}
	floor, budget := knownFloor(record), record.Profile.Budget
	switch {
	case floor >= budget.MaxUSD:
		return 2
	case floor >= budget.StopAt*budget.MaxUSD:
		return 1
	}
	return 0
}

// ErrBudgetStop is the refusal of new work, an assignment or a check run, by
// a task whose estimated dollars reached its budget's stop. It matches
// ErrOverBudget. Below 100% of the budget, the owner's cost-budget approval
// raises the stop to 100%, once; at 100% (Final) only a new task continues.
type ErrBudgetStop struct {
	// USD is the known lower bound of the task's estimated dollars; AtLeast
	// reports that the rest is unknown. MaxUSD is the budget and StopAt the
	// profile's share of it at which the task stops.
	USD, MaxUSD, StopAt float64
	AtLeast             bool
	// Approved reports that the owner already raised the stop to 100%.
	Approved bool
}

// Final reports that the task used its whole budget, so no approval lets it
// take new work.
func (e ErrBudgetStop) Final() bool { return e.USD >= e.MaxUSD }

// budgetQuestion is what the coordinator asks the owner at the stop.
const budgetQuestion = "continue with a higher budget, or stop with the partial result?"

func (e ErrBudgetStop) Error() string {
	reading := budgetReading{floor: e.USD, budget: Budget{MaxUSD: e.MaxUSD, StopAt: e.StopAt}, approved: e.Approved}
	if e.AtLeast {
		reading.unknown = "unknown"
	}
	next := "the owner's cost-budget approval raises the stop to 100% once"
	if e.Final() {
		next = "at 100% only a new task with a higher budget continues"
	}
	return reading.String() + "; the task takes no new work (" + next + "): " + budgetQuestion
}

// overCostBudget returns the refusal of new work by the task's budget, and
// nil when the task may take it: below the stop, or without a profile.
func overCostBudget(record Record) *ErrBudgetStop {
	if state, _ := budgetState(record); state != BudgetStop {
		return nil
	}
	reading := readBudget(record)
	return &ErrBudgetStop{USD: reading.floor, MaxUSD: reading.budget.MaxUSD, StopAt: reading.budget.StopAt, AtLeast: reading.unknown != "", Approved: reading.approved}
}

// budgetRefusal is overCostBudget as an error that matches ErrOverBudget and
// ErrBudgetStop, or nil.
func budgetRefusal(record Record) error {
	if over := overCostBudget(record); over != nil {
		return kindError{kind: ErrOverBudget, msg: over.Error(), cause: *over}
	}
	return nil
}

// finalCostRefusal is the refusal of any new work once the task's known
// dollars reach all of its budget, whatever the owner approved, and nil
// before.
func finalCostRefusal(record Record) error {
	if over := overCostBudget(record); over != nil && over.Final() {
		return budgetRefusal(record)
	}
	return nil
}

// costLimit is the cost budget as a limit the owner may approve: at the stop
// and below 100% of the budget, and nil otherwise.
func costLimit(record Record) *overLimit {
	if over := overCostBudget(record); over != nil && !over.Final() {
		return &overLimit{For: ApprovalCostBudget, refusal: budgetRefusal(record)}
	}
	return nil
}

// approveNewWork admits new work, an assignment or a check run, against the
// budget. It refuses all of it at 100%; it reads words, the
// --owner-approval values, against over, the other limits the work goes
// past, plus the cost limit at the stop; and it stores a cost-budget
// approval on the record, stamped at, returning the other approvals for the
// caller to store.
func approveNewWork(record *Record, over []overLimit, subject string, words []string, at time.Time) ([]OwnerApproval, error) {
	if err := finalCostRefusal(*record); err != nil {
		return nil, err
	}
	if cost := costLimit(*record); cost != nil {
		over = append(over, *cost)
	}
	approvals, err := ownerApprovals(over, subject, words)
	if err != nil {
		return nil, err
	}
	var rest []OwnerApproval
	for _, approval := range approvals {
		if approval.For == ApprovalCostBudget {
			approval.At = at
			record.OwnerApprovals = append(record.OwnerApprovals, approval)
			continue
		}
		rest = append(rest, approval)
	}
	return rest, nil
}

// validateCostApprovals checks the record's approvals: only a task with a
// profile has any; each is a cost-budget approval with the owner's words, at
// a time within the task; and there is at most one, as it raises the stop
// once.
func validateCostApprovals(record Record) error {
	if len(record.OwnerApprovals) == 0 {
		return nil
	}
	if record.Profile == nil || len(record.OwnerApprovals) > 1 {
		return errors.New("invalid stored owner approvals: a task with a budget holds at most one cost-budget approval")
	}
	for _, approval := range record.OwnerApprovals {
		if approval.For != ApprovalCostBudget || !validApprovalText(approval.Text) || approval.At.Before(record.CreatedAt) || approval.At.After(record.UpdatedAt) {
			return errors.New("invalid stored owner approval: it needs the owner's bounded words for cost-budget and a time within the task")
		}
	}
	return nil
}

// validateBudgetTransition re-checks the budget against the previous
// revision: approvals are append-only, one a revision; one is added only at
// the stop below 100%; and a revision that adds new work, an assignment or a
// check run, does so below the stop the record's approvals then set.
func validateBudgetTransition(previous, current Record) error {
	added := len(current.OwnerApprovals) - len(previous.OwnerApprovals)
	if added < 0 || added > 1 || !sameEncoded(previous.OwnerApprovals, current.OwnerApprovals[:len(previous.OwnerApprovals)]) {
		return errors.New("stored owner approvals changed")
	}
	if added == 1 && costLimit(previous) == nil {
		return errors.New("a stored cost-budget approval was given when the task was not between its budget's stop and 100%")
	}
	if len(current.Assignments) > len(previous.Assignments) || len(current.Checks) > len(previous.Checks) {
		judged := previous
		judged.OwnerApprovals = current.OwnerApprovals
		if overCostBudget(judged) != nil {
			return errors.New("a stored revision adds an assignment or a check run at the task's budget stop without the owner's approval, or past its whole budget")
		}
	}
	return nil
}
