# Interface contract: package `clamp`

Limits an integer to a closed range. Language: Go. Module
`example.com/clamp`, package `clamp`.

## Requirements

- R1: `Int(v, lo, hi)` with `lo <= v <= hi` returns `v` and a nil error,
  including `v == lo` and `v == hi`.
- R2: `Int(v, lo, hi)` with `lo <= hi` and `v < lo` returns `lo` and a nil
  error.
- R3: `Int(v, lo, hi)` with `lo <= hi` and `v > hi` returns `hi` and a nil
  error.
- R4: `Int(v, lo, hi)` with `lo > hi` returns `0` and an error for which
  `errors.Is(err, ErrInvertedRange)` holds, for every `v`.
- R5: For every `v` and every `lo <= hi`, including `math.MinInt` and
  `math.MaxInt`, the result `r` satisfies `lo <= r <= hi`, and `r == v`
  exactly when `lo <= v <= hi`.

## Scope

- Integers of type `int` only; no generic or floating-point variant.
