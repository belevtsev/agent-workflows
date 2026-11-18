# What did the extra coordination buy on the archived coding tasks?

The strongest comparison for the daily harness is the pflag implementation pair. Both developers’ patches reached a clean commit. Both submitted suites detected all seven defects in the offline replay. Daily Kitsu used 841,548 captured tokens versus 394,607 for plain Codex and took 33 seconds longer. The daily build in that trial launched an independent reviewer. The current default requests one only when the developer or repository requires it. Its live cost and quality on this task are still unknown.

## What the paid runs actually delivered

Each row uses a frozen run manifest and the candidate exported when the solver stopped. The token column sums captured input and output across the coordinator and every recorded worker. Cached input is included once. A ≥ marks incomplete usage coverage. Seconds measure the solver process, excluding external grading and setup.

| Trial and task | Arm | Captured tokens | Seconds | Frozen acceptance | Completed delivery |
| --- | --- | ---: | ---: | ---: | --- |
| Sol 6.1/high, pflag implementation | Plain Codex | 309,806 | 184 | 2/2 | Yes |
| Same trial | Managed Kitsu | 1,987,618 | 527 | 2/2 | Yes |
| Sol 6.1/high, qs implementation | Plain Codex | 744,020 | 254 | 18/18 | Yes |
| Same trial | Managed Kitsu | ≥4,258,254 | 720 | 18/18 | No |
| Sol 6.2/high, pflag three-arm implementation | Plain Codex | 300,723 | 113 | 2/2 | Yes |
| Same trial | Earlier managed Kitsu | 2,053,717 | 369 | 2/2 | Yes, confirmed by corrected probe |
| Same trial | Revised managed Kitsu | ≥4,193,946 | 720 | 2/2 | No |
| Sol 6.2/high, pflag daily implementation | Plain Codex | 394,607 | 154 | 2/2 | Yes |
| Same trial | Daily Kitsu | 841,548 | 187 | 2/2 | Yes |
| Sol 6.2/high, qs review and repair | Daily Kitsu | ≥5,063,787 | 659 | 19/19 | No |

All ten exported candidates passed their original suite and frozen patch-integrity checks. Three Kitsu attempts stopped with delivery unfinished. Four planned repeats in the first experiment and the plain arm of the daily qs repair never started. The daily qs task began with a partially fixed patch and used its own grader. The plain qs implementation row answers a different question.

The three pflag experiments used the same application snapshot. Within each trial the arms shared the model, reasoning effort, task and time limit. Workflow prompts, Kitsu configuration and worker execution differed by arm. The three-arm trial reused one plain baseline for both Kitsu builds. Pooling these runs into a single speedup or defect rate would give repeated pflag observations undue weight.

**One completion result needed correction.** The historical three-arm probe rejected the label `before` before examining Git or Kitsu state. The saved offline recheck changed that one argument to the recognized managed label. The probe then confirmed the same exported tree in a clean commit, a passing gate and no active interval. The report retains both observations and checks the two argument vectors against one another. Completion measures the recorded finish. The original runner also tracked workflow adherence separately, and several Kitsu runs had unknown adherence. [Per-attempt evidence](daily-review-data/archived-delivery-comparison.json)

## Would the regression tests catch a future defect?

We took the complete submitted pflag package test suite from each of the seven archived patches. Each suite first passed on its own archived library. We then ran them against one common pflag implementation and seven isolated faults. The original upstream test suite served as a baseline. This measures the protection that remained in the repository after each attempt, including tests inherited from upstream.

| Tests under evaluation | Seeded faults detected | Dropped value in `--unknown=value` | Skipped known-flag callback in `ParseAll` |
| --- | ---: | --- | --- |
| Original pflag suite | 2/7 | Missed | Missed |
| Sol 6.1 plain | 7/7 | Caught | Caught |
| Sol 6.1 managed Kitsu | 7/7 | Caught | Caught |
| Sol 6.2 three-arm plain | 7/7 | Caught | Caught |
| Sol 6.2 three-arm earlier managed Kitsu | 6/7 | Missed | Caught |
| Sol 6.2 three-arm revised managed Kitsu | 6/7 | Missed | Caught |
| Sol 6.2 daily trial plain | 7/7 | Caught | Caught |
| Sol 6.2 daily Kitsu | 7/7 | Caught | Caught |

The first five faults exercise dropping trailing arguments, continuing after the first unknown, treating a shorthand suffix as known, forwarding a known suffix in interspersed mode, and stopping early in interspersed mode. An independent public-API test rejects each fault. The two added faults exercise cases already in the frozen acceptance test. That test rejects both altered implementations. In particular, the two 6/7 suites originally passed external acceptance on their submitted code. Their regression gap appears when a later edit removes the attached value.

One revised managed suite already failed two subcases on the common implementation. Its tests expect a particular behavior when a callback changes parsing mode inside a shorthand cluster. Its own implementation passes those cases, and the upstream reference fix also disagrees with the supplementary callback probe. The score credits a seeded fault only when it introduces a newly failing named test or subtest beyond the common implementation’s existing failures. Those two pre-existing disagreements earn no credit and need maintainer adjudication before becoming release requirements. These seven retrospective faults cover one issue.

## What to change in daily work

When a task promises exact data preservation, the developer should leave a regression that checks the complete returned value and order. A parse that succeeds and forwards the right number of flags can still corrupt a value. Both daily arms demonstrated that the regression fits in an ordinary submitted suite. A reviewer is useful when independent scrutiny is requested or required. These trials give no basis for making a reviewer mandatory for this class of change.

For repeatable comparisons, keep the completion probe's arm identity separate from its validation rules. Grade a managed build with the same Git and interval checks across variants. Preserve the raw probe result and any later correction. Report code acceptance, workflow finish, and usage coverage independently.

## Reproduce offline

Use the four retained experiment directories beneath the archive root and the frozen pflag acceptance file. The driver reads the archived worktrees, reconciles session counters, extracts Git objects to temporary directories, disables Go module lookup and runs the local Go toolchain. It issues zero model requests.

```sh
python3 docs/benchmarks/archived-delivery-evidence.py \
  --archive /path/to/kitsu-experiments \
  --grader /path/to/kitsu_pilot_acceptance_test.go
```

The result records the manifest and application hashes, exported trees, model settings, completion corrections, usage coverage and failing test names for each control. [Replay driver](archived-delivery-evidence.py) · [Machine-readable results](daily-review-data/archived-delivery-comparison.json)
