package billing

import "time"

// paymentTermDays is the number of days after issue before an unpaid invoice is overdue.
const paymentTermDays = 30

// Invoice is a bill sent to a customer.
type Invoice struct {
	ID         string
	CustomerID string
	IssuedAt   time.Time
	PaidAt     *time.Time
}

// DueAt is the instant the invoice falls due.
func (i Invoice) DueAt() time.Time {
	return i.IssuedAt.AddDate(0, 0, paymentTermDays)
}

// IsOverdue reports whether the invoice is unpaid at or after its due instant.
func (i Invoice) IsOverdue(now time.Time) bool {
	return i.PaidAt == nil && !now.Before(i.DueAt())
}
