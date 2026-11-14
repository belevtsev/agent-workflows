---
name: protobuf-contracts
description: Evolve or review protobuf schemas and generated peers across wire, source API, and ProtoJSON compatibility.
---

# Evolving protobuf contracts

Find the authoritative schema and owning repository before editing generated files. Establish exact schema revisions, generation ownership, checked-in outputs, and actual producers/consumers, including supported languages and published dependency versions. Local replacements and copied schemas do not establish which contract a deployed peer uses.

Evaluate binary wire compatibility, generated source/API compatibility, and ProtoJSON compatibility separately. Trace tags and reservations, enum numbers and unknown values, oneof membership, field presence/defaults, validation, and unknown-field handling into actual consumers. A schema that parses or passes one compatibility category may still change application semantics. Read [compatibility decisions](references/compatibility.md) for the affected representation.

Derive generator/compiler/runtime versions, commands, Buf configuration and breaking baseline from the owning repository and its CI. Follow [generation and rollout](references/generation-rollout.md) when outputs or mixed peers change. When behavior changes without a schema change, read [behavior nobody promised](references/observable-behavior.md). Do not replace pins with latest tools, invent a baseline, or hand-edit generated bindings as a substitute for the generation workflow. A missing generator leaves generation unverified; continue source and existing CI analysis without installing or mutating dependencies unless that is authorized.

Verify the supported producer/consumer combinations and semantic round trips with representative absent/default/unknown values. Assert decoded meaning and relevant unknown-field behavior; deterministic serialization does not make protobuf bytes canonical. Respect no-tests and review-only scope, and separate local fixtures, generated consistency, existing CI, and deployed peer evidence.

Report exact schema and consumer locations, the compatibility dimensions affected, generation evidence and limits, and a rollout order or rejection behavior grounded in the supported deployment model. Use current [official protobuf guidance](https://protobuf.dev/programming-guides/) and [Buf documentation](https://buf.build/docs/) for uncertain semantics rather than copying a manual into the skill.
