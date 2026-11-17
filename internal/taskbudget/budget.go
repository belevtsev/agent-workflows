// Package taskbudget records optional user-authored usage expectations.
// Recorded limits neither authorize execution nor enforce runtime caps.
package taskbudget

import (
	"errors"
	"fmt"
	"strings"
)

const (
	Tokens  = "tokens"
	Seconds = "seconds"
)

type Estimate struct {
	Min         int64    `json:"min"`
	Max         int64    `json:"max"`
	Basis       string   `json:"basis"`
	Assumptions []string `json:"assumptions,omitempty"`
}

type Limit struct {
	N int64 `json:"n"`
	// Source is the user's record of authority, not proof of authorization.
	Source string `json:"source"`
}

type Entry struct {
	Estimate *Estimate `json:"estimate,omitempty"`
	Limit    *Limit    `json:"limit,omitempty"`
}

type Record struct {
	Version  int              `json:"version"`
	Task     string           `json:"task"`
	Workflow string           `json:"-"`
	Entries  map[string]Entry `json:"entries"`
	raw      map[string][]byte
}

type Actual struct {
	N        int64  `json:"n"`
	Known    bool   `json:"known"`
	Complete bool   `json:"complete"`
	Basis    string `json:"basis"`
}

type Comparison struct {
	Actual         Actual `json:"actual"`
	EstimateStatus string `json:"estimate_status"`
	LimitStatus    string `json:"limit_status"`
}

// Compare never declares incomplete or unknown observations within a budget.
func Compare(entry Entry, actual Actual) Comparison {
	c := Comparison{Actual: actual, EstimateStatus: "absent", LimitStatus: "absent"}
	if entry.Estimate != nil {
		switch {
		case !actual.Known:
			c.EstimateStatus = "unknown"
		case actual.N > entry.Estimate.Max:
			c.EstimateStatus = "above"
		case !actual.Complete:
			c.EstimateStatus = "incomplete"
		case actual.N < entry.Estimate.Min:
			c.EstimateStatus = "below"
		default:
			c.EstimateStatus = "within"
		}
	}
	if entry.Limit != nil {
		switch {
		case !actual.Known:
			c.LimitStatus = "unknown"
		case actual.N > entry.Limit.N:
			c.LimitStatus = "exceeded"
		case !actual.Complete:
			c.LimitStatus = "incomplete"
		default:
			c.LimitStatus = "within"
		}
	}
	return c
}

func validUnit(unit string) bool { return unit == Tokens || unit == Seconds }
func validTask(task string) bool {
	if task == "" || len(task) > 128 {
		return false
	}
	for _, word := range strings.Split(task, "-") {
		if word == "" {
			return false
		}
		for _, ch := range word {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
				return false
			}
		}
	}
	return true
}
func validateEntry(unit string, e Entry) error {
	if !validUnit(unit) {
		return fmt.Errorf("unsupported usage unit %q; use tokens or seconds", unit)
	}
	if e.Estimate == nil && e.Limit == nil {
		return errors.New("at least one estimate or limit is required")
	}
	if v := e.Estimate; v != nil {
		if v.Min < 0 || v.Max < v.Min || strings.TrimSpace(v.Basis) == "" {
			return errors.New("estimate requires nonnegative min <= max and a basis")
		}
		for _, a := range v.Assumptions {
			if strings.TrimSpace(a) == "" {
				return errors.New("estimate assumptions must not be empty")
			}
		}
	}
	if v := e.Limit; v != nil && (v.N < 0 || strings.TrimSpace(v.Source) == "") {
		return errors.New("limit requires a nonnegative value and an explicit source; recording it does not authorize execution")
	}
	return nil
}
