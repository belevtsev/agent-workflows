---
type: llm
focus: { source: file, path: artefacts/adversarial_test.go }
---

This file holds the adversarial tester's kept tests for Go package `jail`.
The role contract says every kept test must fail on the current (seeded)
source because of the violation it cites, and must name its finding ID and
cited rule; a test that passes is discarded, not kept.

How the seeded source really behaves. Only one thing is wrong: the helper
`contains` checks `strings.HasPrefix(p, root)` without a separator, so a
relative name whose cleaned result is a sibling of the root starting with the
root's string (for root `/srv/jail/data`: `../data-evil/x`, `../data2`,
`../database`) resolves to that sibling path with a nil error instead of
`ErrOutside` (R5, DR1). Everything else already matches the contract, so a
test of it passes on the seeded source: `New` rejects a relative root, an
empty root and one that cleans to `/`, and cleans a valid root; a name inside
the root resolves to `filepath.Join(root, name)`; `.`, `""` and names that
clean back to the root resolve to the root; every absolute name returns `""`
and `ErrOutside`; every `..` escape whose result does not start with the
root's string (for example `..`, `../other`, `../../etc/passwd`) returns `""`
and `ErrOutside`; `Resolve` touches no file; and concurrent `Resolve` calls
report no data race.

PASS if the file declares `package jail`; every test function in it
(`func TestXxx(t *testing.T)`) resolves at least one relative name whose
cleaned result is a sibling of the root that starts with the root's string,
and asserts, from R5 or DR1, `ErrOutside` (or that `contains` rejects it), so
it fails on the seeded source for that reason; and each test names its finding
ID (`A1`, ...) and the rule it cites (R5 or DR1) in its name or a comment.
FAIL if the file is missing or holds no test function; any test function
asserts only behaviour listed above as already present (it would pass on the
seeded source); any test relies on symlinks or the filesystem; any test lacks
a finding ID or a cited requirement or design rule, or cites a rule the
contract and design do not state; or any test takes its expected values from
the current output rather than from the contract or design.
