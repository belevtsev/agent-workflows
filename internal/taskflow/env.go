package taskflow

import "time"

// Clock returns the current time. Every time a task record stores comes from
// it: created_at and updated_at, a check run's start and finish, an
// assignment, handoff, acceptance, amendment, verification or abandonment.
type Clock func() time.Time

// Env carries the ports a task use case reads instead of reaching the machine
// directly, so a rule can run in a unit test with a fake. The ports are
// declared here, at their consumer. A nil port selects its production
// implementation, so the zero Env is production: time.Now is the clock.
//
// Each use case that writes a task revision is a method on Env and passes the
// Env to update, which stamps the revision.
type Env struct {
	Clock Clock
}

// now returns the clock's time in UTC, as a record stores it.
func (env Env) now() time.Time {
	if env.Clock == nil {
		return time.Now().UTC()
	}
	return env.Clock().UTC()
}
