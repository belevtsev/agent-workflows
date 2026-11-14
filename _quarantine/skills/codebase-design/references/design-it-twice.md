# Design it twice

Your first interface is unlikely to be the best one. Use this when an interface is expensive to change and the right shape isn't obvious. Small local functions don't need it.

## Process

**Frame the problem.** Write down the constraints any interface must satisfy, the dependencies and their categories from [deepening](deepening.md), and a rough sketch that makes the constraints concrete. The sketch isn't a proposal.

**Produce different designs.** Create at least three interfaces that differ in kind, not in naming. If delegation is authorized and available, give each to an independent worker with a fresh context, since a worker that has seen one design tends to vary it instead of replacing it. Otherwise write them one at a time and finish each before starting the next. Give each design a different constraint:

- Minimize the interface: one to three entry points, with maximum leverage for each.
- Maximize flexibility: support many use cases and extension.
- Optimize for the most common caller: make the default case trivial.
- Build around ports and adapters, where dependencies cross a seam.

**Describe each design** with its types and methods, its invariants, ordering rules, and error modes, a usage example, what it hides, its dependency strategy, and where its leverage is high or thin.

**Compare and choose.** Contrast the designs on depth, locality, and seam placement. Then recommend one and explain why. Propose a hybrid if elements combine well. Give a clear recommendation, not a menu.
