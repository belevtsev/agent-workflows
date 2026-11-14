package retry_test

import (
	"errors"
	"testing"
	"time"

	"example.com/retry"
)

// Deliberately weak: covers only the constructor errors of R1.
func TestNewBudgetErrors(t *testing.T) {
	now := func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := retry.NewBudget(0, 0, now); !errors.Is(err, retry.ErrInvalidRatio) {
		t.Fatalf("ratio 0: error = %v, want ErrInvalidRatio", err)
	}
	if _, err := retry.NewBudget(0.5, -1, now); !errors.Is(err, retry.ErrInvalidMin) {
		t.Fatalf("minRetries -1: error = %v, want ErrInvalidMin", err)
	}
	if _, err := retry.NewBudget(0.5, 0, nil); !errors.Is(err, retry.ErrNilClock) {
		t.Fatalf("nil clock: error = %v, want ErrNilClock", err)
	}
	if _, err := retry.NewBudget(0.5, 0, now); err != nil {
		t.Fatalf("valid arguments: error = %v, want nil", err)
	}
}
