# Design smells

These are prompts for judgment, not violations. Label each one as a possible smell, because a documented repository standard always wins, and skip anything tooling enforces. Report one only when you can describe the cost it causes in this change.

| Smell | What it looks like | Fix |
| --- | --- | --- |
| Mysterious name | A name that doesn't reveal what something does | Rename it. If no honest name comes, the design is murky |
| Duplicated code | The same logic shape in several hunks | Extract the shared shape |
| Feature envy | A function reaching into another type's data more than its own | Move it to the data |
| Data clumps | The same few fields travelling together | Give them a type |
| Primitive obsession | A string standing in for a domain concept | Give it a type, and see `parse-dont-validate` |
| Repeated switches | The same `switch` on one type in several places | One map or interface both sites share |
| Shotgun surgery | One logical change forces edits across many files | Gather what changes together |
| Divergent change | One file edited for several unrelated reasons | Split it so each part changes for one reason |
| Speculative generality | Parameters or hooks for needs nobody has | Delete it until a real need shows up |
| Message chains | Long `a.B().C().D()` navigation | Hide the walk behind one method |
| Middle man | A type that mostly delegates | Remove it and call the target directly |

_Adapted from `code-review` in `mattpocock/skills`, whose list comes from Fowler's Refactoring._
