# Naming coupling

"Tightly coupled" is too vague to settle a design argument, because everything is coupled to something. Connascence names what kind of coupling exists and ranks it. Two pieces of code are connascent when changing one forces a change to the other to keep the program correct.

Judge each case on three things: how strong it is, how many places are involved, and how far apart they sit. They connect through one rule:

> The further apart two things are, the weaker their connascence must be.

Strong connascence inside one function is ordinary code. The same connascence across two services is an incident waiting to happen. Concentrating strong connascence inside one module is the goal, not a failure. That's what encapsulation is for.

## From weakest to strongest

Static forms can be seen by reading the code:

| Form | Both sides agree on | Fix |
| --- | --- | --- |
| Name | A name | Unavoidable and fine |
| Type | A type | This is what type systems do |
| Meaning | What a value means, such as `-1` for missing | Named constants, or a second return value |
| Position | Order of arguments or columns | A struct with named fields |
| Algorithm | That both compute identically, such as a signature | One shared implementation |

Dynamic forms appear only at runtime and are much worse, because the compiler can't see them and violations show up as production bugs:

| Form | Both sides agree on | Fix |
| --- | --- | --- |
| Execution order | That `a()` runs before `b()` | Make the wrong order impossible |
| Timing | That something happens within a window | An explicit wait on a condition |
| Values | That several values change together | One value, or one transaction |
| Identity | That two references are the same instance | Pass the reference explicitly |

## Refactoring down the ladder

Each fix turns a stronger form into a weaker one. A constructor that returns a connected client removes the "call `Connect` first" order, because the wrong sequence can no longer be written. A struct replaces a long positional parameter list. Waiting on a condition replaces a `time.Sleep`.

## In review

Replace a vague objection with this:

> This is connascence of **form**, degree **n**, across **boundary**. Weaken it to **form** by **fix**.

Watch for these:

- A magic value in more than one file.
- Four or more positional parameters of similar types.
- Two services that must deploy together. Fix this first, because it's strong coupling across the strongest boundary present.
- "Call `Init` first" in a doc comment.
- `time.Sleep` in a test.
- Two rows that must change together outside a transaction.

No linter scores this. It's vocabulary for making coupling arguments concrete enough to settle.

_Adapted from `connascence` in `coding-skills` by auralshin, and rewritten for Go._
