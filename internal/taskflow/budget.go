package taskflow

import (
	"fmt"
	"strconv"
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
	// BudgetUnknown: the task's dollars are unknown. Unknown usage never
	// stops a task and never counts as 0; verify discloses it.
	BudgetUnknown BudgetState = "unknown"
)

// ApprovalCostBudget is OwnerApproval.For of an assignment the owner let past
// the budget's stop: it raises the stop to 100% of the budget, once.
const ApprovalCostBudget = "cost-budget"

// budgetState judges the task's usage (UsageOf) against its profile's budget
// and returns the state with a detail that says why. A record without a
// profile has no budget and is always ok. The stop is StopAt of MaxUSD, or
// all of MaxUSD once an assignment carries the owner's cost-budget approval;
// at 100% only a new task continues. Dollars are estimated, so only their
// amount counts; tokens never stop a task (OD-3).
func budgetState(record Record) (BudgetState, string) {
	if record.Profile == nil {
		return BudgetOK, "the task records no profile, so it has no budget"
	}
	reading := readBudget(record)
	if reading.usd == nil {
		return BudgetUnknown, reading.unknown
	}
	if *reading.usd >= reading.stop() {
		return BudgetStop, reading.String()
	}
	return BudgetOK, reading.String()
}

// budgetReading is a task's estimated dollars against its budget.
type budgetReading struct {
	usd     *float64
	unknown string
	budget  Budget
	// approved reports that an assignment carries the owner's cost-budget
	// approval, which moved the stop to all of MaxUSD.
	approved bool
}

func readBudget(record Record) budgetReading {
	usage := UsageOf(record)
	return budgetReading{usd: usage.USD, unknown: usage.UnknownReason, budget: record.Profile.Budget, approved: costApproved(record)}
}

// stop is the amount, in dollars, at which the task takes no new work.
func (r budgetReading) stop() float64 {
	if r.approved {
		return r.budget.MaxUSD
	}
	return r.budget.StopAt * r.budget.MaxUSD
}

func (r budgetReading) String() string {
	return fmt.Sprintf("task usage is an estimated %s USD, %s%% of its %s USD budget, which stops new work at %s%%",
		dollars(*r.usd), percent(*r.usd/r.budget.MaxUSD), dollars(r.budget.MaxUSD), percent(r.stop()/r.budget.MaxUSD))
}

func dollars(amount float64) string { return strconv.FormatFloat(amount, 'f', 2, 64) }

func percent(share float64) string { return strconv.FormatFloat(share*100, 'f', 0, 64) }

// costApproved reports whether an assignment carries the owner's cost-budget
// approval.
func costApproved(record Record) bool {
	for _, assigned := range record.Assignments {
		if assigned.OwnerApproval != nil && assigned.OwnerApproval.For == ApprovalCostBudget {
			return true
		}
	}
	return false
}

// ErrBudgetStop is the refusal of new work, an assignment or a check run, by
// a task whose estimated dollars reached its budget's stop. It matches
// ErrOverBudget. Below 100% of the budget, the owner's cost-budget approval
// on an assignment raises the stop to 100%, once; at 100% (Final) only a new
// task continues.
type ErrBudgetStop struct {
	// USD is the task's estimated dollars; MaxUSD the budget; StopAt the
	// share of MaxUSD the stop was at.
	USD, MaxUSD, StopAt float64
	// Approved reports that the owner already raised the stop to 100%.
	Approved bool
}

// Final reports that the task used its whole budget, so no approval lets it
// take new work.
func (e ErrBudgetStop) Final() bool { return e.USD >= e.MaxUSD }

// budgetQuestion is what the coordinator asks the owner at the stop.
const budgetQuestion = "continue with a higher budget, or stop with the partial result?"

func (e ErrBudgetStop) Error() string {
	reading := budgetReading{usd: &e.USD, budget: Budget{MaxUSD: e.MaxUSD, StopAt: e.StopAt}, approved: e.Approved}
	next := "the owner's cost-budget approval raises the stop to 100% once"
	if e.Final() {
		next = "at 100% only a new task with a higher budget continues"
	}
	return reading.String() + "; the task takes no new work (" + next + "): " + budgetQuestion
}

// overCostBudget returns the refusal of new work by the task's budget, and
// nil when the task may take it: below the stop, with unknown dollars, or
// without a profile.
func overCostBudget(record Record) *ErrBudgetStop {
	if state, _ := budgetState(record); state != BudgetStop {
		return nil
	}
	reading := readBudget(record)
	return &ErrBudgetStop{USD: *reading.usd, MaxUSD: reading.budget.MaxUSD, StopAt: reading.budget.StopAt, Approved: reading.approved}
}

// budgetRefusal is overCostBudget as an error that matches ErrOverBudget and
// ErrBudgetStop, or nil.
func budgetRefusal(record Record) error {
	if over := overCostBudget(record); over != nil {
		return kindError{kind: ErrOverBudget, msg: over.Error(), cause: *over}
	}
	return nil
}
