# Requirement map: package `session`

- R1: `session_test.go` `TestNewStoreRejectsInvalidArguments`; fails if an invalid argument is accepted or returns another error.
- R2: `session_test.go` `TestTouchMakesActive`; fails if `Touch` does not record the activity.
- R3: `session_test.go` `TestActiveExpiresAfterIdle`; fails if a session stays active past its idle timeout.
- R4: `session_test.go` `TestSweepRemovesExpired`; fails if `Sweep` leaves an expired session or miscounts.
- R5: `session_test.go` `TestSweepRemovesExpired`; fails if `Len` counts swept sessions.
- R6: `session_test.go` `TestClose`; fails if `Close` is not idempotent.
- R7: `session_test.go` `TestConcurrentTouch`; fails if concurrent `Touch` calls lose a session.
