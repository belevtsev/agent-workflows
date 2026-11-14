# Requirement map: package `ratelimit`

- R1: `limiter_test.go` `TestNewRejectsInvalidArguments`; fails if any invalid argument is accepted or returns another error.
- R2: `limiter_test.go` `TestLimitPerKey`; fails if a call past the limit is allowed or an early call is refused.
- R3: `limiter_test.go` `TestKeysIndependent`; fails if one key's calls refuse another key.
- R4: `limiter_test.go` `TestLimitPerKey`; fails if the window never resets.
- R5: `limiter_test.go` `TestCountMatchesAllowed`; fails if `Count` disagrees with the allowed calls.
- R6: `limiter_test.go` `TestConcurrentAllow`; fails if concurrent calls exceed the limit.
