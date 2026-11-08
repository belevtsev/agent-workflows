package audit

import "time"

// Event is one recorded action.
type Event struct {
	Time   time.Time
	Actor  string
	Action string
	Target string
}

// Log returns the events recorded between from and to, oldest first.
type Log interface {
	Between(from, to time.Time) ([]Event, error)
}

// HasRole reports whether user holds role; roles come from the identity service.
type HasRole func(user, role string) bool
