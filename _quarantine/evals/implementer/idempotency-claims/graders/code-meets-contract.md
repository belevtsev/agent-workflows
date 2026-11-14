---
type: llm
focus: { source: file, path: idem/idem.go }
---

PASS if `New` rejects a non-positive TTL with `ErrInvalidTTL` and a nil clock
with `ErrNilClock`; `Claim` of an empty key returns `ErrEmptyKey`; the first
`Claim` of a key returns a `Claim` whose `ExpiresAt` is the clock's now plus
the TTL; a second `Claim` before expiry returns `ErrDuplicate`; a `Claim`
after expiry succeeds; `Release` of an unknown or expired key returns
`ErrNotFound`; concurrent `Claim` calls for one key let exactly one succeed
because the check and the insert happen under one lock; `Len` counts only
unexpired claims; `Sweep` removes expired entries and returns how many.
FAIL if the check-then-insert in `Claim` is not atomic (for example a
`RLock` check followed by a `Lock` insert), if a background goroutine is
started for expiry, if expired entries are silently treated as live, if the
clock is read more than once per operation in a way that lets an entry
expire mid-call, or if the code adds persistence hooks, interfaces or
metrics the contract did not ask for.
