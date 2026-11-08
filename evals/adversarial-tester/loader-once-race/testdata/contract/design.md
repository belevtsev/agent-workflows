# Design: package `memo`

## Shape

`Cache` holds the loader, a map of completed values and a map of in-flight
calls, both guarded by one `sync.RWMutex`. An in-flight call carries a `done`
channel that is closed when its load completes, and the load's value and
error.

## Design rules

- DR1: On a miss, `Get` takes the write lock, checks the value map and the
  in-flight map again, and when neither holds the key it registers a new
  in-flight entry for the key in the in-flight map under that same write lock,
  before releasing the lock and calling `load`. A `Get` that finds an
  in-flight entry releases the lock and waits on its `done` channel.
- DR2: `load` is always called with no lock held, so a slow load for one key
  never blocks Gets for other keys.
- DR3: When the load completes, the caller that ran it takes the write lock,
  stores a successful value in the value map, removes the in-flight entry,
  releases the lock and then closes `done`. The in-flight entry's value and
  error are written before `done` is closed and read by waiters only after it
  is closed.
- DR4: Every access to either map happens with the mutex held (read lock for
  the fast-path value lookup, write lock otherwise).
