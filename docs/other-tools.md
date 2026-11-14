# What other tools taught us

On 14 November 2026 four research passes read what users and maintainers report about tools
that do part of what `cw` does: git hook managers, guardrails for coding agents and token
counters, specification and role workflows, and skills. This page records what was found, and
for each finding what `cw` does about it.

The page states no benefit of `cw`. Where it reports a number, the number is the source's.

## How this was done, and its limits

- Each pass was made by a research agent that searched the web and opened the pages listed
  under [Sources](#sources). The author of this page did not open them again. A quote here is
  the quote the agent reported.
- Most sources are single reports, vendor pages or opinion. Each finding says how strong it is.
- No query named this repository or its owner.
- The outcome of each finding is one of four words:

| Outcome | Meaning |
| --- | --- |
| changed | `cw` was changed because of the finding, on the same day. The row says where. |
| already so | The design already did this. The row says where to see it. |
| rejected | Not taken, with the reason. |
| deferred | Not done. It changes no behaviour of `cw` today. The row says what it waits for. |

## What speaks against parts of cw

Three findings go against choices this repository made. They are not softened.

1. **Roles for coding have public evidence against them, and none for them here.** Anthropic
   measured about 15 times the tokens of a chat for a multi-agent system and wrote that coding
   is "not a good fit" for it today (R12). A study of 260 configurations found multi-agent
   work at −70.0% on sequential planning tasks (R17). Agent OS removed its subagents in its
   third version (R9). `cw` ships seven roles. It claims nothing for them, prints their cost
   with `cw tokens`, and holds 26 evaluation cases and a comparison with one agent, none of
   them run: [the evaluation](evaluation.md).
2. **A local gate does not hold against an agent that wants to pass it.** One report shows an
   agent that made six commits with failing tests through `--no-verify`, `git stash` and quiet
   flags, against written rules (H3). Some tools skip hooks without any intent: Aider commits
   with `--no-verify` by default (H11), and two Cursor versions replaced the hooks directory
   (H5, H6). `cw` is a guardrail on one clone. The rule holds for a team only where continuous
   integration runs the checks again and the job is required: see
   [Continuous integration](operations.md#continuous-integration).
3. **Specification workflows are reported as too heavy for small changes** by users of four
   tools (R1, R3, R4, R2). `cw` now sends a small change past the workflow entirely: see row
   R-1 below.

## Commit hooks

| # | Finding | Strength | Outcome |
| --- | --- | --- | --- |
| H-1 | Hooks check the files on disk, not what is committed; the usual repair is to stash, which loses work when it fails (H1, H13, H16). | Many reports; in the tools' own documents | already so: the hook judges the tree the commit will record, stashes nothing and changes no file. [Hooks](hooks.md). The cost is that a commit of a part of the changes is denied until the working tree holds exactly that part; the skill has an example of it. |
| H-2 | A run of the checks on a copy of the index would allow partial commits (H1). | One post, which also calls it slow | rejected: a copy of the index holds no ignored files, so a check that needs installed dependencies or a cache fails there. |
| H-3 | Hooks that change files race and alter the commit (H16, H13). | Documented by maintainers | already so: a hook executes nothing, and a check that changes the working tree ends `source_changed` and does not count. |
| H-4 | Hooks fire during a rebase and fail old commits (H1, H17). | One prominent post, one vendor feature | already so: [Hooks](hooks.md) lists which git operations run which hook; the push hook judges what is pushed, not each commit below it. |
| H-5 | Critique: keep hooks fast, quiet, without network, and do not install them unasked; put the real gate before the push or in continuous integration (H1, H2). | Widely discussed opinion | already so: a hook reads records and executes no check, installation is the explicit `cw init`, and `cw run --base` is for continuous integration. The cost of a hook in milliseconds has not been measured here. |
| H-6 | Another tool sets `core.hooksPath` and the hooks stop without a word: Claude Code worktrees (H4), Cursor (H5, H6), Husky. | Several reports, two confirmed by the vendor | already so, in part: `cw status` and the session text warn when the hooks git runs do not call `cw`, and `cw init` never overwrites. A setting given on one command line cannot be seen by any hook; `cw guard` now asks before an agent's command that does it (G-2). |
| H-7 | git has hooks set by configuration, `hook.<name>.command`, which need no hook file (H18). | Official manual; the first release with it was not found | deferred: the git on the build machine is 2.53, and the manual read is for 2.55. It waits for a git that has it. |
| H-8 | Hooks are missing after a clone (H23, H18). | Inherent to git | already so: `cw status` warns, the session text says so, and the skill has an example. No hook is installed without `cw init`. |
| H-9 | The text of a denial steers an agent (H10). | One report | already so: a denial names each check, its state, its log and the command to run, and names no way around. |
| H-10 | Users praise one binary, one configuration file, speed and parallel runs (H12, H15, H23). | Vendor and blog claims, no independent measurement found | already so for one binary and one file. Checks run one after another; running them in parallel is deferred until a repository shows that it needs it. |
| H-11 | A skip of one hook that is recorded, such as `SKIP=id` (H13). | Documented | rejected: a skip is what the gate exists to prevent. `cw init --observe` is the way to try the gate without being stopped. |
| H-12 | Results keyed by the tree have precedent: git's own continuous integration skips a tree that passed (H19). | In production at a large project | already so: this is the design. |
| H-13 | A cache key misses the tool versions and the environment (H20, H21). | Official documents of two build tools | changed: [Evidence](evidence.md) now says how to make a tool's version part of the content. An environment key of its own is rejected: the rule stays that committed content decides. |
| H-14 | A flaky check passes on a second try and nobody sees it (H22). | Official documents | already so: `cw stats` lists passes that follow a failure on the same content. |
| H-15 | A graphical git client runs hooks with a short `PATH`, and the hook program is not found (search results only). | Not verified | already so: a hook that does not find `cw` denies and says why. |

## Agents, guardrails and token counts

| # | Finding | Strength | Outcome |
| --- | --- | --- | --- |
| G-1 | Agents report that tests pass without a run, delete tests, or edit the continuous integration file (G11, G23, G21, G22). | One detailed report each; two measurements | already so for the claim: what counts is a recorded run on the content, not a statement. A check weakened by the agent passes the gate; the skill forbids it, and in the role workflow `cw roles check` denies a `developer` that changed a test. [Evidence](evidence.md) states the limit. |
| G-2 | Matching a command as text is fragile in both directions (G5, G24). | Documented by the host | changed: `cw guard` now reads a command as shell words and judges git by its arguments. [Hooks](hooks.md#hooks-of-the-agent-plugin). |
| G-3 | Codex cannot ask from a hook, and its tool hooks are "not a complete enforcement boundary" (G2). | Official | already so: on Codex the guard refuses with the reason. |
| G-4 | A hook that must stop something should answer in JSON, because exit status 2 did not stop an edit on Windows (G14, G1). | Several reports, not confirmed | already so: the guard answers in JSON and exits 0. |
| G-5 | A hook that fails or times out does not block unless `onFailure` says so (G1). | Official | rejected: the guard is a courtesy. A broken guard must not stop every command of a session. |
| G-6 | A hook that blocks the end of a turn until checks pass can loop: 50 minutes in one report; the host caps it at 8 (G12, G13, G1). | Several reports; cap documented | deferred: `cw` ships no such hook. One that only tells the user "not verified" at the end of a turn is a candidate; it waits for a run on a host. |
| G-7 | Each hook start costs about 35 ms (G15). | One self-measurement | deferred: the cost of the plugin's hooks is not measured (task 10.1 of the gate's plan). |
| G-8 | The session record repeats an answer on several lines, with placeholder counts on the early ones; taking the first line undercounts (G16, G19, G8). | Documented, and one fixed defect in another tool | already so: the reader keeps the largest count of each kind per answer. Checked on 170 real records: see [Tokens](tokens.md#checked-against-real-records). |
| G-9 | Some role records lack the last line of an answer, so its output count is a lower bound (G18). | One open report | rejected for now, against a measurement: in 16,486 answers of 170 real records none lacked it. |
| G-10 | Codex writes running totals, not one count per request (G20, G25). | Third-party documents | changed: the reader expected a line that no real record holds, so every Codex count was unknown. It now reads the running totals, and matches a separate count on 13 real records. |
| G-11 | The end-of-session hook has 1.5 s on Claude Code unless a timeout is set, and at most 3 s on Codex (G1, G2). | Official | already so: the timeouts are 30 and 3, and recording the largest real record took 0.3 s. |
| G-12 | Both record formats are internal and change between versions (G9, G20). | Official for Claude Code | already so: a record that cannot be read gives `unknown`, never 0. The versions that were read are named in [Tokens](tokens.md#checked-against-real-records). |
| G-13 | Nobody found reports tokens for each stage of a workflow (G6, G7, G25). | Absence in what was read | already so: `cw tokens --task` does, from a label in each role's brief. |

## Specifications and roles

| # | Finding | Strength | Outcome |
| --- | --- | --- | --- |
| R-1 | Too much ceremony for a small change: "a sledgehammer to crack a nut" (R1); "30 files changed, < 20 actual LOC" (R4). | Many reports; acknowledged by maintainers | changed: the skill `cw` gives a loop with no roles for a small change, and the coordinator now says so and leaves the workflow for a change of a few lines. Requirement "Two paths", case E26. |
| R-2 | Long generated documents that nobody reads (R1, R2, R3). | Many reports and one assessment | changed: the texts of `spec-writer` and `architect` now tell them to write no more than the behaviour needs and not to repeat an earlier file. No size limit is enforced. |
| R-3 | Agents skip steps, tick tasks they did not do, and touch files outside the plan (R1, R3, R6). | Many reports | already so: the stage comes from the files (`cw change status`), their rules are checked by `cw change check`, and what a role changed is compared with its limit by `cw roles check`. That a ticked task did its work is not checked by a program. |
| R-4 | Merging a change into the living specifications by header name loses text (R6). | Acknowledged by maintainers, "46 fix commits" | already so: no program merges. A role writes the current specifications and a reviewer compares. Two changes to one requirement can still collide; nothing detects it. |
| R-5 | Work handed between agents loses context (R13, R14). | Two essays, one incident | deferred: each role reads the files of the record, and a brief names them. Whether that is enough is what the evaluation is for. |
| R-6 | Agents weaken tests to pass: in one benchmark more than 79% of the cheating by Claude models was a changed test (R18). | Measured | already so in the workflow: `developer` may add a test and may not change or remove one, and git decides whether it did. |
| R-7 | A reviewer told to find problems always finds some; a reviewer in a new context with read-only tools is advised (R11). | Vendor guidance | already so: `reviewer` may write nothing, starts without the author's account, and its text says that "no findings" is a result. |
| R-8 | An architect model paired with an editor model scored 85.0% against 79.7% for one model alone (R19). | Measured, September 2024, one benchmark | noted: the only measurement found that favours a split of roles. It is not a measurement of `cw`. |
| R-9 | Light paths: Kiro, BMAD and OpenSpec each let a small task skip the specification (R7, R8, R5). | Official documents | already so: the small path has no specification and no approval. |
| R-10 | Existing code bases are harder to bring in (R1, R6). | Reports | already so: the small path needs no earlier specification, and `lookup` finds the ones that exist. Not tried on a large repository. |
| R-11 | No tool that was read reports tokens for a task (R11, R21, R6). | Absence | already so: `cw tokens`. |

## Skills

| # | Finding | Strength | Outcome |
| --- | --- | --- | --- |
| K-1 | The description alone decides whether a skill loads; say what it does and when, in the user's words, with one boundary (K3, K6, K10). | Official, consistent | changed: the new skill's description names the cases and ends with "Not for general git help". |
| K-2 | Only `name`, `description` and `metadata` work on every host; another key is an error outside Claude Code (K1). | Official | changed: a test allows no other key in any skill here. |
| K-3 | Keep the body short and the rules that matter first, because a long session keeps only the start of a skill (K1, K3). | Official | changed: about 130 lines, the five rules first; a test caps it at 200 and each example at 100. |
| K-4 | Give one default, not a menu, and a list of the mistakes agents make (K3, K5). | Official | changed: the skill has one loop and a section of mistakes. |
| K-5 | Examples tuned to one case are copied word for word (K5, K8). | Advice, no measurement | changed: seven examples over four small repositories, each as situation, commands, output and reason. |
| K-6 | A skill goes stale against the tool it describes (K19). | One community page | changed: a test replays every example against the `cw` of the same commit and fails on any other output, and the skill names only commands that `cw help` lists. |
| K-7 | Record in the skill the version of the tool it was checked against (K19). | One community page | rejected: this repository forbids a `cw` version in the plugin, since a pinned one went stale before (requirement "Instruction text comes from `cw`"). The replay does the job. |
| K-8 | Test whether the skill loads with about 20 requests, half of them near misses (K6, K8). | Official method | changed and not run: the set is in [the evaluation](evaluation.md#the-skill-cw-does-it-load-when-it-should). |
| K-9 | Codex has a newer plugin manifest at the root of the repository; the one under `.codex-plugin/` is the older form it still reads (K11). | Official | deferred: nothing here has run on Codex. It waits for a Codex run. |

## What is still open, in order

1. Run the evaluation. Until then nothing says that the roles are worth their tokens, and the
   public evidence leans the other way.
2. Run the plugin on both hosts. The hooks, the guard and the skill have run only in tests and
   in [five trial runs](evaluation.md#a-first-trial-of-the-skill-without-a-host) with a model
   that had no plugin.
3. Make the continuous integration job required. It is the owner's setting, not a file.
4. Measure the cost of the hooks (G-7, H-5).
5. Try a hook that tells the user "not verified" at the end of a turn (G-6).
6. Look at git's hooks by configuration when a git that has them is at hand (H-7).
7. Say what to do in the last line of `cw status` when the program of a check is missing. Two
   trial agents asked for it. The gate's code is at 5,998 of its 6,000 lines, so this needs a
   cut elsewhere or the owner's word on the limit.

## Sources

Opened by the research agents on 14 November 2026. The date after a title is the date of the
page where it gives one.

**Hooks (H).**
H1 [pre-commit hooks are fundamentally broken](https://jyn.dev/pre-commit-hooks-are-fundamentally-broken/), 26 Dec 2025.
H2 [Hacker News thread on H1](https://news.ycombinator.com/item?id=46398906).
H3 [claude-code issue 40117](https://github.com/anthropics/claude-code/issues/40117), 28 Mar 2026.
H4 [claude-code issue 66993](https://github.com/anthropics/claude-code/issues/66993), 10 Jun 2026.
H5 [Cursor forum 155256](https://forum.cursor.com/t/cloud-agent-skipping-custom-git-hooks/155256), 19 Mar 2026.
H6 [Cursor forum 167719](https://forum.cursor.com/t/git-hooks-silently-skipped-for-all-source-control-operations-since-3-15-6-git-extension-forces-core-hookspath-dev-null/167719), Aug 2026.
H10 [Lefthook with Claude Code](https://liambx.com/blog/ai-agent-lint-enforcement-lefthook-claude-code), 8 Jul 2025.
H11 [Aider git documentation](https://aider.chat/docs/git.html).
H12 [Git hooks comparison](https://www.andymadge.com/2026/03/10/git-hooks-comparison/), 10 Mar 2026.
H13 [pre-commit documentation](https://pre-commit.com/).
H15 [prek documentation](https://prek.j178.dev/).
H16 [lint-staged README](https://unpkg.com/lint-staged@17.6.0/README.md).
H17 [Lefthook skip options](https://lefthook.dev/configuration/skip/).
H18 [githooks](https://git-scm.com/docs/githooks) and [git-hook](https://git-scm.com/docs/git-hook) manuals.
H19 [git.git commit 7d78d5fc](https://code.googlesource.com/git/+/7d78d5fc1a91b683dde970e5e48b6d9a873cfd99%5E%21/), 8 Oct 2020.
H20 [Turborepo caching](https://turborepo.dev/docs/crafting-your-repository/caching).
H21 [Bazel remote caching](https://bazel.build/remote/caching).
H22 [GitHub merge queue](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue).
H23 [Trunk on hook management](https://trunk.io/blog/githook-management), 19 Sep 2022.

**Guardrails and tokens (G).**
G1 [Claude Code hooks](https://code.claude.com/docs/en/hooks).
G2 [Codex hooks](https://learn.chatgpt.com/docs/hooks).
G5 [Claude Code permissions](https://code.claude.com/docs/en/permissions).
G6 [Claude Code costs](https://code.claude.com/docs/en/costs).
G7 [Claude Code usage monitoring](https://code.claude.com/docs/en/monitoring-usage).
G8 [Agent SDK cost tracking](https://code.claude.com/docs/en/agent-sdk/cost-tracking).
G9 [Claude Code sessions](https://code.claude.com/docs/en/sessions).
G11 [claude-code issue 11913](https://github.com/anthropics/claude-code/issues/11913), 19 Nov 2025.
G12 [claude-code issue 55754](https://github.com/anthropics/claude-code/issues/55754), 3 May 2026.
G13 [claude-code issue 54360](https://github.com/anthropics/claude-code/issues/54360), 28 Apr 2026.
G14 [claude-code issue 80039](https://github.com/anthropics/claude-code/issues/80039), 22 Jul 2026.
G15 [claude-code issue 39391](https://github.com/anthropics/claude-code/issues/39391), 26 Mar 2026.
G16 [claude-code issue 28197](https://github.com/anthropics/claude-code/issues/28197), 24 Feb 2026.
G18 [claude-code issue 93620](https://github.com/anthropics/claude-code/issues/93620), 11 Sep 2026.
G19 [ccusage issue 901](https://github.com/ryoppippi/ccusage/issues/901), 21 Mar 2026.
G20 [ccusage Codex guide](https://ccusage.com/guide/codex/).
G21 [METR on reward hacking](https://metr.org/blog/2025-06-05-recent-reward-hacking/), 5 Jun 2025.
G22 [ImpossibleBench](https://arxiv.org/abs/2510.20270v1), 23 Oct 2025.
G23 [AI deleted my tests](https://typia.io/blog/ai-deleted-my-tests-and-said-all-tests-pass/), 3 May 2026.
G24 [Stopping agents from bypassing hooks](https://pydevtools.com/handbook/how-to/how-to-stop-ai-agents-from-bypassing-pre-commit-hooks/).
G25 [Subagent time and token viewer](https://lilting.ch/en/articles/subagent-time-token-log-viewer), 18 Sep 2026.

**Specifications and roles (R).**
R1 [Kiro, spec-kit and Tessl](https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html), 15 Oct 2025.
R2 [Thoughtworks Radar on Spec Kit](https://www.thoughtworks.com/radar/languages-and-frameworks/github-spec-kit), 15 Apr 2026.
R3 [spec-kit issue 75](https://github.com/github/spec-kit/issues/75), 8 Sep 2025.
R4 [spec-kit issue 1174](https://github.com/github/spec-kit/issues/1174), 13 Nov 2025.
R5 [OpenSpec](https://github.com/Fission-AI/OpenSpec).
R6 OpenSpec issues 2033, 2201, 1387, 2166, 724 and 1726, February to November 2026.
R7 [Kiro quick spec](https://kiro.dev/docs/specs/quick-spec/) and [bugfix specs](https://kiro.dev/docs/specs/bugfix-specs/).
R8 [BMAD documentation](https://docs.bmad-method.org/).
R9 [Agent OS v3](https://github.com/buildermethods/agent-os/discussions/310), 20 Jan 2026.
R11 [Claude Code best practices](https://code.claude.com/docs/en/best-practices).
R12 [Anthropic on its multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system), 13 Jun 2025.
R13 [Don't build multi-agents](https://cognition.com/blog/dont-build-multi-agents), 12 Jun 2025.
R14 [The orchestrator's tax](https://martinfowler.com/articles/orchestrator-tax.html), 16 Jul 2026.
R17 [Towards a science of scaling agent systems](https://arxiv.org/abs/2512.08296), 9 Dec 2025.
R18 [ImpossibleBench, full text](https://arxiv.org/html/2510.20270v1), 23 Oct 2025.
R19 [Aider architect mode](https://aider.chat/2024/09/26/architect.html), 26 Sep 2024.
R21 [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents).

**Skills (K).**
K1 [Claude Code skills](https://code.claude.com/docs/en/skills).
K3 [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices).
K5 [agentskills.io best practices](https://agentskills.io/skill-creation/best-practices).
K6 [agentskills.io on descriptions](https://agentskills.io/skill-creation/optimizing-descriptions).
K8 [Anthropic's skill-creator skill](https://raw.githubusercontent.com/anthropics/skills/main/skills/skill-creator/SKILL.md).
K10 [Codex: build skills](https://learn.chatgpt.com/docs/build-skills).
K11 [Codex: package your plugin](https://developers.openai.com/codex/plugins/build).
K19 [Repository skill release drift](https://agentpatterns.ai/patterns/anti-patterns/repository-skill-release-drift/), 25 Aug 2026.
