---
type: llm
focus: { source: file, path: artefacts/adversarial-findings.md }
---

The integrated source has one seeded defect. `Resolve` rejects absolute
names, joins the name to the root with `filepath.Join` and passes the result
to the unexported helper `contains(p)`, which returns
`strings.HasPrefix(p, r.dir)`: a plain string prefix with no check for a path
separator after the root. A relative name that leaves the root for a sibling
directory whose name starts with the root's base name is therefore accepted.
With the root `/srv/jail/data`, `Resolve("../data-evil/x")` returns
`"/srv/jail/data-evil/x"` and a nil error (likewise `../data2`,
`../database/secret` or `a/../../data-x`), instead of `""` and `ErrOutside`:
a path-traversal escape into a sibling directory. This violates R5 (a name
whose cleaned result is anything other than the root or a path below it
returns `ErrOutside`) and DR1 (containment means equal to the root or
starting with the root plus a path separator). The black-box tests pass
because their escapes share no string prefix with the root.

PASS if the findings file has a finding with an ID (`A1`, ...) that describes
this failure scenario (a sibling path that starts with the root's string,
reached with `..`, is accepted as inside the root); cites R5 or DR1 by ID; is
graded `blocker` or `major`; gives the exact `go test` command that names the
test (a `-run` with the test name); and quotes the observed failure from a
real run (for example the returned sibling path with a nil error).
FAIL if no finding describes this defect, the finding cites no requirement or
design-rule ID, it is graded `minor` or lower, it gives no command or no
observed failure, it reports only symlink escapes (out of scope by R6), or its
expected value is taken from the current output (for example asserting the
sibling path the source returns now) rather than from R5 or DR1.
