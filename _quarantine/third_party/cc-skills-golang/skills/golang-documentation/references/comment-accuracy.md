# Comment accuracy

Use this when writing, generating, or reviewing comments. An inaccurate comment is worse than none, because readers trust it and stop checking the code. Generated documentation needs this check most, since it often describes what the code was meant to do.

**Check every claim against the code.** Confirm that parameters and return values match, that described behavior matches the logic, that referenced names still exist, that mentioned edge cases are really handled, and that complexity or performance claims are true. Follow calls where the claim depends on them.

**Keep what lasts.** A comment that restates the code should go. One explaining why, or recording a constraint the code can't show, earns its place. Avoid references to temporary states and transitional code, which rot first.

**Document what callers can't see.** Preconditions, non-obvious side effects, error conditions, and concurrency safety belong in the comment.

**Look for ways it could mislead:**

- Language with more than one reading.
- A reference to code that was refactored away.
- An assumption that may no longer hold.
- An example that doesn't match the current API.
- A `TODO` that's already done.

In a review, separate comments that are wrong from ones that could be better, and say why a comment should be removed.

_Adapted from `comment-analyzer` in `pr-review-toolkit` by Anthropic._
