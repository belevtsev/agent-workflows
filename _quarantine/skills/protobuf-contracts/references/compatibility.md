# Compatibility decisions

Choose the language/syntax/edition and runtime guidance applicable to the actual schema. Current primary references are the [proto3 language guide](https://protobuf.dev/programming-guides/proto3/), [field presence note](https://protobuf.dev/programming-guides/field_presence/), [ProtoJSON format](https://protobuf.dev/programming-guides/json/), [serialization stability](https://protobuf.dev/programming-guides/serialization-not-canonical/), and [Buf breaking checks](https://buf.build/docs/breaking/). Consult the relevant section when a decision depends on its semantics.

| Boundary | Decisions to verify in schema and consumers |
| --- | --- |
| Binary wire | Field numbers and wire types; deleted number reservations; enum numbers; oneof changes; old-reader unknown fields; parse/merge behavior |
| Generated source/API | Field/message/service names, packages and language options; generated accessors and presence API; enum switch exhaustiveness; compiler/runtime compatibility |
| ProtoJSON/TextFormat | Field and enum names, `json_name`, deleted name reservations, default emission and absence, unknown-field parsing, numeric representations; actual parser options |
| Application semantics | Meaning of absent versus explicit zero, validation and rejection, unsupported variants, unknown enum behavior, persistence and relay paths |

Do not reuse removed field or enum numbers. Check reservations for deleted numbers and names to prevent later reuse; name reservation does not itself preserve acceptance of old JSON. Wire-compatible type changes may still lose values or alter interpretation, so assess supported values and callers. Changing a name can preserve binary decoding while breaking source code or JSON clients.

Presence is a contract decision: inspect whether a caller must distinguish omitted from explicitly set zero/false/empty, including patches and merges. Verify the generated API and the behavior of old peers with implicit presence. Oneof changes need special care: an old reader can observe an unknown variant as unset, and parse/relay behavior may discard or alter the intended selection. Examine unknown enum values and runtime-specific handling rather than assuming every backend language behaves identically.

Binary unknown-field preservation is not guaranteed through conversions that rebuild messages or pass through JSON. A relay may be forward compatible in binary and lose data at a JSON boundary. Test the actual transformation and supported runtime options when this matters.

Use semantic oracles: compare decoded fields, presence, selected variants, errors, and relevant retained unknowns. Stable output from one deterministic serializer is useful for a local reproducibility check; it does not establish canonical bytes across languages, versions, or equivalent encodings.
