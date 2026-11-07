// Declaration extract for package retry (bodies stripped).
package retry

import (
	"errors"
	"time"
)

var ErrInvalidRatio = errors.New("retry: ratio must be in (0, 1]")
var ErrInvalidMin = errors.New("retry: minRetries must not be negative")
var ErrNilClock = errors.New("retry: clock must not be nil")

type Stats struct {
	Requests int
	Retries  int
}

type Budget struct{}

func NewBudget(ratio float64, minRetries int, clock func() time.Time) (*Budget, error)
func (b *Budget) RecordRequest()
func (b *Budget) Allow() bool
func (b *Budget) Stats() Stats
