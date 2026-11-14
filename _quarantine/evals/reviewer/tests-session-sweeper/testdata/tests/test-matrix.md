# Test matrix: package `session`

| Requirement | Category | Level | Input | Expected observable outcome | Oracle |
| --- | --- | --- | --- | --- | --- |
| R1 | failure mode | unit | `NewStore(0, clock)`, `NewStore(-1s, clock)`, `NewStore(1s, nil)` | `ErrInvalidIdle`, `ErrInvalidIdle`, `ErrNilClock` | hand-checked literals |
| R2, R3 | happy path | unit | idle 1m, `Touch("a")`, advance 59 s, `Active("a")` | true | hand-checked literal |
| R3 | boundary | unit | idle 20 ms, `Touch("a")`, wait 30 ms, `Active("a")` | false | contract R3 |
| R4, R5 | happy path | unit | 200000 sessions, advance 2m, `Sweep` with a 20 ms deadline | 200000 removed, `Len` 0 | hand-checked literals |
| R6 | idempotency | unit | `Close()` | nil | contract R6 |
| R7 | concurrency | unit | 64 concurrent `Touch` calls with distinct IDs | `Len` 64 | hand-checked literal |

Security: the store takes only session IDs from its caller and parses
nothing, so it does not apply.
