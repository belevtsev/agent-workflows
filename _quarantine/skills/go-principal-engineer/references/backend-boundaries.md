# Backend ownership boundaries

Use this reference when a Go change crosses service, persistence, schema, or authority boundaries. Establish the current call/data flow and exact producer/consumer revisions. Distinguish transport routing from business ownership, authentication from resource authorization, and an inventory/cache projection from authoritative state.

Give each invariant the smallest necessary authority: who admits the operation, commits durable state, owns mutable data, performs the effect, and reconciles partial success. Keep adapters thin when they translate protocols; preserve an explicit boundary if it encodes different recovery or durability semantics. An interface, service, queue, or second copy of state needs a concrete benefit and a named lifecycle owner.

Trace a representative request and failure from admission through validation, state commit, external effect and acknowledgment. Identify which facts a downstream component may trust and which it must validate. Name the changed contract, configuration, storage or operator behavior and derive migration requirements from supported deployments. Load `protobuf-contracts` or `go-pki-mtls` dynamically only when their contract or identity boundary is affected.

Compare viable designs against the actual workload and failure model: consistency, recovery, coupling, operational cost and reversibility. Avoid turning a local operation into a distributed workflow without a demonstrated need. For a substantial decision, retain the chosen owners, effect/commit order, rejection behavior, compatibility evidence, and remaining uncertainty in the task's agreed artifact. Keep the entrypoint's consultation with configured advisers, if any, and independent verification.
