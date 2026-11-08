# Architect session transcript (the architect's own conversation, kept for the record)

> user: do we need to keep timeout_seconds in responses?

architect: Probably not. Most customers upgrade the SDK within a sprint, so
a clean rename is worth the short breakage. I'll leave the conflicting-values
case to the implementer; the handler can just prefer timeout_ms.
