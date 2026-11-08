# Test matrix: package `ratelimit`

| Requirement | Category | Level | Input | Expected observable outcome | Oracle |
| --- | --- | --- | --- | --- | --- |
| R1 | failure mode | unit | `New(0, 1s, clock)`, `New(1, 0, clock)`, `New(1, -1s, clock)`, `New(1, 1s, nil)` | `ErrInvalidLimit`, `ErrInvalidWindow`, `ErrInvalidWindow`, `ErrNilClock` | hand-checked literals |
| R2 | happy path | unit | limit 3, five `Allow("a")` calls | true, true, true, false, false | hand-checked literals |
| R3 | happy path | unit | limit 1, `Allow("a")` then `Allow("b")` | both true | hand-checked literals |
| R4 | boundary | unit | limit 3, exhaust `a`, advance the clock by exactly the window, `Allow("a")` | true, and a new window starts | contract R4 |
| R5 | happy path | unit | two allowed calls, then `Count("a")` | 2 | hand-checked literal |
| R6 | concurrency | unit | limit 10, 50 concurrent `Allow("k")` | at most 10 true results | contract R6 |

Idempotency: no operation is meant to be repeated without effect, so it does
not apply. Security: the package takes no external input beyond keys, so it
does not apply.
