# Design discussion (architect only; never pass to the implementer)

Considered returning `*Limits` with a `Valid()` method so callers could
construct partial values; rejected because every caller would have to call
`Valid()` again. Decision: narrow at `Parse` and give `Count` no constructor.
