---
name: domain-modeling
description: Sharpen a project's domain language with a glossary and scenario probing, and record only the decisions that are costly to reverse.
license: MIT
---

# Domain modeling

Use this when the design depends on what business terms mean: when terms are vague or overloaded, when a glossary or ADR is being written or changed, or when stated rules and code may disagree. Reading an existing glossary for vocabulary doesn't need this skill. It's for changing the model.

Vague language becomes vague code. When "account" means both a customer and a login, the confusion ends up in types, tables, and APIs, where it's expensive to fix. Resolve it while it's still a conversation.

## While designing

- **Challenge against the glossary.** When a term conflicts with `GLOSSARY.md`, say so immediately: "Your glossary defines cancellation as X, but you seem to mean Y. Which is it?"
- **Sharpen fuzzy language.** Propose one precise term for a vague or overloaded one: "When you say account, do you mean the Customer or the User? Those are different."
- **Probe with scenarios.** Invent concrete cases that test the boundaries between concepts. What happens to an invoice when part of an order is cancelled after shipping? Scenarios expose rules nobody has stated.
- **Check claims against code.** When someone says how something works, verify it. If the code cancels whole orders but the stated rule allows partial cancellation, surface the contradiction instead of designing on top of it.

Create or edit files only when the task allows repository changes. Otherwise propose the text.

## Glossary

Capture a term as soon as it's resolved, rather than batching. Follow the [glossary format](references/glossary-format.md). A glossary holds domain language only, with no implementation details, so it stays true as the code changes. Create `GLOSSARY.md` when the first term is resolved, not before.

Be opinionated. When several words exist for one concept, pick the best and list the others under `_Avoid_`, because a glossary that lists synonyms settles nothing.

## Architecture decision records

Offer an ADR only when all three are true:

1. **Hard to reverse.** Changing your mind later has a meaningful cost.
2. **Surprising without context.** A future reader will wonder why it was done this way.
3. **A real trade-off.** There were genuine alternatives and you chose for specific reasons.

If any of these is missing, skip it. An easily reversed decision can simply be reversed, and an obvious one explains itself. Too many ADRs bury the few that matter. Follow the [ADR format](references/adr-format.md). A record can be a single paragraph.

ADRs are durable records in the repository. For working through a substantial design during a task, use the decision record in `software-architecture`.
