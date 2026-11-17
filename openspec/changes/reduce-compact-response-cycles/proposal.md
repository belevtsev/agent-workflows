# Reduce compact response cycles

## Why

The completed Codex pair captured 1,984,340 compact versus 1,638,219 ordinary
tokens. Its 71 versus 56 response-usage records identify unnecessary discovery,
wait and evidence-expansion opportunities. A five-chunk failed result serialized
to 6,556 bytes under a 16,384-byte budget but still lost a chunk to a fixed
preview. The investigation does not establish causal or general savings.

## What changes

- Deliver complete failed check evidence when the final serialized response fits;
  retain bounded preview and original-log recovery when it does not.
- Snapshot bounded QE check/log/search argv recipes and scoped optional-tool
  observations into the original saved worker packet.
- Clarify terminal/semantic handoff decisions and success-only finish/prepare
  batching using existing host/Go CLI operations.
- Add opt-in `task prepare --dispatch --view launch` and read-only
  `task dispatch <dispatch-id>` for the original full native dispatch.

## Not part of this change

No paid pilot/probe, worker spawner, wait daemon, role or model downgrade,
test-coverage reduction, broad receipt exclusion, check suppression or review/
approval relaxation. Compact remains opt-in. Existing default full JSON and
unmanaged operation remain compatible. Native token/cost or model-quality
benefits of these changes are unknown; frozen studies remain unchanged.

## Open questions

None. Local verification is recorded in tests.md.
