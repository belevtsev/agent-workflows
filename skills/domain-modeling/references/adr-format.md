# ADR format

ADRs live in `docs/adr/` with sequential numbers: `0001-slug.md`, `0002-slug.md`. Scan for the highest existing number and add one. Create the directory when the first ADR is needed.

```md
# {Short title of the decision}

{One to three sentences: the context, what was decided, and why.}
```

An ADR can be a single paragraph. Its value is recording that a decision was made and why, not filling in sections. Add these only when they earn their place:

- **Status:** `proposed`, `accepted`, `deprecated`, or `superseded by ADR-NNNN`, when decisions get revisited.
- **Considered options:** when the rejected alternatives are worth remembering.
- **Consequences:** when downstream effects aren't obvious.

## What qualifies

- **Architectural shape.** "The write model is event-sourced, and the read model is projected into Postgres."
- **Integration between contexts.** "Ordering and Billing communicate through events, not synchronous calls."
- **Technology with lock-in.** Database, message bus, auth provider, or deployment target. Not every library, only those that would take a quarter to replace.
- **Boundaries and scope.** "Customer data is owned by the Customer context, and others reference it by ID only." Explicit refusals are as valuable as commitments.
- **Deliberate deviations.** "We use hand-written SQL rather than an ORM because of X." These stop the next engineer from fixing something that was deliberate.
- **Constraints the code can't show.** "Responses must stay under 200ms because of the partner contract."
- **Non-obvious rejections.** If an alternative was rejected for subtle reasons, record it, or someone will propose it again in six months.
