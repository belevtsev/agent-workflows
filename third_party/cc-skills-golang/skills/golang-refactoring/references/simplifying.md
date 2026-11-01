# Simplifying code

The goal is code that's easier to read, change, and debug, not fewer lines. Every input, output, side effect, and error must stay the same.

## Understand it first

If you see a fence across a road and don't know why it's there, don't tear it down. Code that looks pointless often exists for a reason nobody wrote down. Before changing or removing anything, answer these:

- What's its responsibility, what calls it, and what does it call?
- What are its edge cases and error paths?
- Which tests define its behavior?
- Why might it have been written this way? A performance need, a platform constraint, or an old incident?

History usually answers the last one quickly:

```sh
git log -S 'the exact odd line' --oneline -- path/to/file.go
```

If you can't answer these, read more before simplifying.

## Over-simplifying

Simplification can make code worse:

- **Inlining too much.** Removing a helper that gave a concept a name makes the call site harder to read.
- **Merging unrelated logic.** Two simple functions combined into one complex function isn't simpler.
- **Removing an abstraction that earns its place.** Some exist for testability or a real extension point. Apply the deletion test in `codebase-design` first.
- **Optimizing for line count.** A dense one-liner can be harder to read than the four lines it replaced.

## Edited tests are a warning

If a simplification needs a test changed to pass, it probably changed behavior. Stop and find out which. A test legitimately changes only when it was reaching past the interface, and then the change should be deliberate and explained.

Keep refactoring separate from feature work, because mixed changes are harder to review and revert. Limit the scope to what the task touched, rather than tidying everything nearby.

_Adapted from `code-simplification` in `addyosmani/agent-skills`, and rewritten for Go._
