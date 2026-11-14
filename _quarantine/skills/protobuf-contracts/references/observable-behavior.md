# Behavior nobody promised

With enough users, every observable behavior gets depended on by someone, whatever the contract says. This is Hyrum's Law, and it's the practical definition of a breaking change. A schema can stay fully compatible while a consumer breaks.

| Behavior | How it becomes a dependency |
| --- | --- |
| Error message text | A consumer matches on `"not found"` |
| Field or map ordering | A consumer hashes serialized output |
| Timing | A slower call fires a caller's timeout, and a faster one exposes a race |
| ID format | A consumer parses a timestamp out of an ID |
| Set and unset fields | A consumer treats an absent field and a default differently |
| Log format | An alert matches on it |

If someone can observe it, assume someone has written an `if` on it.

## Make unspecified behavior unstable

Deliberately vary what you never promised, so no dependency can form. That turns a slow production break into a fast test failure. Go randomizes map iteration for exactly this reason, and `protojson` varies its whitespace so nobody compares its output as bytes. `go test -shuffle=on` applies the same idea to test order.

## For existing systems, measure first

The dependency may already exist, so find out rather than arguing about whether it should.

1. Measure the specific behavior, not the method. Count reads of the field or the old path, by caller identity.
2. Watch a full business cycle, because monthly and quarterly jobs surface after a release.
3. Canary the change, and watch consumers' error rates as well as your own.
4. Don't rely on a changelog note. Use metrics, then deprecation with a deadline, then removal, following `expand-contract`.

## Keeping your freedom

- **Return opaque values.** Make a page token an opaque signed string, not encoded offsets. Anything readable gets decoded and built by hand.
- **Separate what's promised.** Give errors a stable machine-readable code, and treat the message as free to reword. gRPC status codes and details work this way. Reword messages occasionally, so the distinction stays credible.
- **Supply what people work around.** If everyone parses an error string, ship a structured field.
- **Prefer loud failure.** Removing a field breaks visibly, while changing its meaning silently produces wrong data.

Semantic versioning covers the documented contract, and this covers everything else, so a patch release can break real users. Sometimes the right response is to document what was accidentally promised and keep it.

## In review

1. Can a consumer observe this difference? If so, it's potentially breaking, whatever the documentation says.
2. How would anyone know if someone depended on it? If the answer is that nobody would, add the metric before shipping.

_Adapted from `hyrums-law` in `coding-skills` by auralshin._
