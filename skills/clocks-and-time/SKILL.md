---
name: clocks-and-time
description: Handle durations, timestamps, time zones, DST, and event ordering in Go without wall-clock, skew, or calendar bugs.
license: MIT
---

# Clocks and time

Use this when code measures durations, stores or compares timestamps, handles time zones or DST, orders events across machines, or schedules jobs.

## Two clocks

| Clock | Property | Use for |
| --- | --- | --- |
| Wall clock | Can jump either way, through NTP, manual changes, or VM migration | Timestamps meant for people or storage |
| Monotonic | Never goes backward, and means nothing as an absolute value | Durations, timeouts, rate limits, benchmarks |

`time.Now()` carries both readings, and `Sub` and `Since` use the monotonic one, so normal Go code gets this right. The trap is that serialization strips the monotonic reading. A value that went through JSON or a database measures on the wall clock from then on, so don't compute durations across that boundary.

Compare instants with `Equal`, not `==`. A value decoded from JSON, or the same instant in another zone, is `Equal` but not `==`, because `==` also compares the monotonic reading and the location. This bites in map keys and in tests.

## Never order by timestamps from two machines

NTP keeps machines within a few milliseconds at best, and 100ms of skew is normal under load or on VMs. Two events a millisecond apart on different hosts can carry timestamps in either order, so anything built on "later timestamp wins" silently loses writes.

When order matters, use something other than wall clocks:

- Route all writes for one key through a single owner, or let the database assign the order.
- Use a Lamport counter: increment on each local event, send it, and on receive set it to `max(local, received) + 1`. If A caused B, A's counter is lower.
- Use hybrid logical clocks when you also want values close to real time.
- Make order irrelevant with idempotent operations.

## Storing time

| Do | Don't |
| --- | --- |
| `TIMESTAMPTZ` in Postgres | `TIMESTAMP`, which ignores the offset you give it |
| UTC in storage and logs | Local time in storage |
| RFC 3339 with an offset | A timestamp with no offset |
| An IANA zone name for future local events | A fixed UTC offset for a future event |

The last row is often missed. A meeting at 09:00 Berlin time next March has no fixed UTC value today, because DST rules can change. Store past events as UTC instants. Store future local commitments as wall time plus the zone name, and resolve them at use.

`time.LoadLocation` needs zone data at runtime, so it fails in a minimal container. Import `_ "time/tzdata"` or install `tzdata`. Zone rules change by government decision, so keep the data current. Convert to local time only at display, and set the database session zone to UTC, so behavior doesn't depend on the host.

## Calendar arithmetic

One day isn't always 24 hours. `Add` works on elapsed time and `AddDate` on the calendar:

```go
func nextDayNoon(ny *time.Location) (elapsed, calendar time.Time) {
	saturday := time.Date(2026, 3, 7, 12, 0, 0, 0, ny) // the day before DST starts
	return saturday.Add(24 * time.Hour), saturday.AddDate(0, 0, 1)
}
```

Here `elapsed` is 13:00 on Sunday and `calendar` is 12:00. Use `AddDate` when the answer must be a local wall time.

Some local times don't exist and some happen twice. For those, `time.Date` picks one side of the transition and doesn't guarantee which. In America/New_York, asking for the missing 02:30 on 8 March 2026 returns 01:30, not 03:30. If a stored local time can fall in a transition, decide the rule explicitly and test it.

## Scheduled jobs

- Cron in local time fires twice or not at all at a DST change, so run schedulers in UTC.
- Don't assume a job runs exactly once at exactly its time. Make handlers idempotent.
- A job processing "the last hour" from `time.Now()` skips or repeats work after a clock step or a delayed start. Persist a watermark and derive the window from data.
- Add jitter across hosts, or every machine fires at `:00` together.

## Testing

Inject the clock rather than calling `time.Now()` deep inside logic, because a test can't otherwise reach a DST boundary or a timeout. Go's `testing/synctest` runs timers on a fake clock for concurrent code. Test around DST transitions, month ends, and leap days.

_Adapted from `coding-skills` by auralshin, and rewritten for Go after running each behavior described here._
