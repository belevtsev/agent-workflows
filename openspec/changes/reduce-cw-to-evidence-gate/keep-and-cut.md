# Keep-and-cut list: reduce-cw-to-evidence-gate

This is the historical `cw` implementation record. Current interfaces are named Gatecub; see [migration](../../../docs/migration.md).

Status: a list for the owner, written on 14 November 2026. Nothing in it had
been carried out then: no file was moved, removed or changed. It has been
carried out since; [tasks.md](tasks.md) says how. It applies the
accepted [proposal](proposal.md) to every file tracked at commit `ce9b122`, the
commit the review read.

Updated the same day for the owner's later decisions: Windows is in the first
version, and installing must not need Go. No verdict changed. The rows for
`internal/proc`, `internal/fslock`, the release files, CI and the README say
what the two decisions add.

Still the same day the owner asked for roles and a token count. The change
[add-role-workflow](../add-role-workflow/proposal.md) proposes them, written
anew on top of the core. It changes no verdict here: what this list moves
still leaves `main` and stays on its branch. See "Yours to overrule", 1 and 3.

## Result

About 91% of the Go code leaves the core. 850 lines stay as they are.

| Verdict | Files | Go lines | Meaning |
| --- | ---: | ---: | --- |
| Keep | 41 | 850 | Stays in the core as it is, or with the small edit its row names. |
| Rewrite | 26 | 3,377 | The purpose stays in the core. The content is written anew to the specification. Today's file is something to read first, not something to edit. |
| Move | 1,820 | 18,163 | Leaves the core and goes to the workflow layer. |
| Cut | 288 | 23,993 | Leaves the core and is not carried into the layer. |
| Tests with no verdict of their own | 85 | 0 | Tests in the four packages that are split. Each follows the code it tests. |
| Total | 2,260 | 46,383 | |

"Go lines" counts every `.go` file that is not a `_test.go` file. That includes
536 lines of example code inside `third_party/`; the proposal's "about 45,000"
is the 45,847 lines outside it. Test code is counted apart, under
[Tests](#tests).

Three things follow from the table:

- **The core is mostly new code.** It starts with 850 kept lines. The rest of
  its 6,000-line limit (proposal, Success measure 3) is written to the
  specification, with 3,377 lines of today's code as reading material.
- **Nothing kept depends on anything that leaves.** `internal/proc` and
  `internal/fslock` import only the Go standard library and `golang.org/x/sys`.
- **Moving is not copying.** The task record imports four packages that do not
  go with it. See [What the move costs](#what-the-move-costs).

## How the verdicts were chosen

The accepted proposal fixes what leaves the core: its Non-goals name every
feature that goes. The answer to Open Question 1 fixes three things that move:
the roles, the orchestration skill and the task record.

For everything else that leaves, this list chooses between Move and Cut by one
rule:

> A part moves when the workflow layer cannot do its job without it. A part is
> cut when the core now does that job, when the workflow works without it, or
> when it is the owner's personal tooling.

That choice is a recommendation, not a decision already made. Each Cut row can
become a Move row; [Yours to overrule](#yours-to-overrule) names the price.

The basis column cites review findings by number (C1 to L15, in
`reports/Review findings 2026-11-14.md`) and requirements by specification and
name, for example `check-evidence`: Snapshot.

## Go code that stays in the core

| Path | Lines | Verdict | What happens | Basis |
| --- | ---: | --- | --- | --- |
| `cmd/cw/main.go` | 55 | Keep | Add SIGHUP to the two signals it traps today. | H2; `check-evidence`: Interrupted runs |
| `internal/proc/` | 531 | Keep | Unchanged for Linux and macOS, and Windows code is added: on other systems it stops only the program itself today (`group_other.go`). It runs a program without a shell, in a process group of its own, and stops the whole group. It already reports the exit status when a leftover process holds the output open (L7); the core's runner has to use that status and print the warning. | `check-evidence`: Running checks, Leftover processes; `platform-support`: Supported systems |
| `internal/fslock/` | 264 | Keep | Unchanged for Linux and macOS, and Windows code is added: on other systems it can take no lock today (`fslock_other.go`). File locks that a killed process cannot leave behind. | `check-evidence`: Damaged and concurrent records; `platform-support`: Supported systems |
| `internal/cli/cli.go`, `exit.go`, `help.go` | 398 | Rewrite | The command root, for ten commands in place of twelve groups. The exit status numbers 0 to 5 keep their meaning. | `gate-reporting`: Exit statuses |
| `internal/cli/gate.go`, `hook_git.go`, `hook.go` | 1,112 | Rewrite | Become `cw init`, `cw uninstall`, `cw hook <name>` and `cw context`. Worth carrying over: the marker line that shows `cw` wrote a hook, the refusal to replace another tool's hook, exit status 4, and the reading of each host's hook input. New: hooks are written only in the common git directory, never when `core.hooksPath` is set, and `pre-push` reads the refs git passes. | `publish-gate`: Hook installation, Hooks of other tools, Push rule, Removal; M26 |
| `internal/gate/gate.go`, `git.go`, `staged.go`, `staged_content.go` | 1,204 | Rewrite | The three hook decisions. Little of this code survives: it decides from task records and compares staged files one by one. The core compares two git trees. | `publish-gate`: Commit rule, Paths that differ, Merge rule, Fail closed, Denial message; C2, H1, L1 |
| `internal/projectctx/capture.go`, `types.go` | 663 | Rewrite | The snapshot. Today `cw` reads and hashes every file itself, with a ceiling of 30,000 files and 64 MiB. The core asks git for the tree through a temporary index, with no ceiling. | `check-evidence`: Snapshot; C2, M5 |

New code with nothing to start from: the `cw.toml` reader and `cw add`, the
record store, `cw run`, `cw status`, `cw stats`, observe mode, and everything
Windows needs. The tests of `internal/proc` and `internal/fslock` are built for
Linux and macOS only today.

## Go code that moves to the workflow layer

| Path | Lines | What it is | Basis |
| --- | ---: | --- | --- |
| `internal/taskflow/` | 12,907 | The task record: briefs, bundles, handoffs, acceptances, assessments, rounds, stages, timeline. | Open Question 1 |
| `internal/cli/task_*.go` (not usage, budget or consult), `compact.go`, `verify_problems.go`, `help/`, `schemas/` | 2,162 | The `cw task` subcommands, their help texts and input schemas. | Open Question 1 |
| `internal/projectctx/merge.go`, `references.go` | 328 | Git questions that only the task record asks (`taskflow/amend.go`, `bundle.go`). | Follows the task record |
| `internal/skillcheck/`, `catalog_test.go` | 441 | Content checks for skills, agents and routing. The 441 lines are fixtures; the checks are 5,057 lines of tests. | Follows the skills |
| `evals/`, `cmd/cwdev/` | 1,789 | The evaluation cases for the role prompts and the tool that runs them. Open findings go with them: H7, M21, L12. | Follows the roles |

Three tests in `internal/skillcheck` check this repository's own documents, not
skills: `changelog_test.go`, `markdown_anchors_test.go` and `mermaid_test.go`.
They are worth copying into the core together with the helpers they share
(`repoRoot`, `writeFixture`, `linkCheckedDocuments`). The last one keeps the
rule "archify only, no Mermaid" under test.

## Go code that is cut

| Path | Lines | What it is | Why it is cut | Basis |
| --- | ---: | --- | --- | --- |
| `internal/usagelog/`, `internal/outcomes/`, `internal/cli/` `hook_usage.go`, `metrics.go`, `task_usage.go`, `task_budget.go` | 6,259 | Token usage, budgets and `cw metrics`. | The workflow works without it. The code's own comment calls the `cw metrics` commands experimental, and the budget stop does nothing under Codex. `cw stats` is new code over the gate's own records. | Non-goal; M11 |
| `internal/consult/`, `internal/credentials/`, `internal/modelpolicy/`, `internal/userconfig/userconfig.go`, `internal/cli/` `consult.go`, `keys.go`, `credentials.go`, `adviser_opt_in.go`, `policy.go`, `task_consult.go` | 5,566 | Adviser consultation, adviser keys and the adviser part of the model policy. | The workflow works without it: an adviser is contacted only after the user opts in. It is the code that sends repository details to adviser services. | Non-goal; H5, M10, M12, L9, L10 |
| `internal/publish/`, `internal/cli/publish.go`, `cmd/cw/testdata/publish/` | 3,356 | `cw publish`. | `git commit`, `git push` and `gh pr create` do this job, and the gate judges them. | Non-goal; M13, L5 |
| `internal/langpack/`, `packs/`, `internal/userconfig/packs.go`, `internal/cli/pack.go`, `embed.go` | 3,056 | The Go evidence rule, the C-toolchain layer and the packs. | A check passes when its program exits 0. A Go team declares `go test -race ./...` as a check. | Non-goal; H3, H4, M6, M7, L6, L14 |
| `internal/gate/command.go`, `scan.go`, `internal/shellwords/` | 2,387 | Reading shell commands to find a commit or a push. | The git hooks replace it. No shell parsing remains. | H8, M1, M2, M3; `agent-integration`: The plugin never judges a commit or push |
| `internal/graphify/`, `skills/graphify/` | 1,284 | Code discovery with the Graphify tool. | The owner's personal tooling inside the product. | Non-goal; M16 |
| `internal/doctor/`, `internal/projectctx/command.go`, `inspect.go`, `internal/cli/doctor.go`, `hook_version.go` | 1,093 | `cw doctor` and the comparison of plugin and `cw` versions. | `cw status` and `cw context` say what is missing. The plugin holds no `cw` version. | Non-goal; C1, M16; `agent-integration`: Instruction text comes from `cw` |
| `internal/codexinstall/`, `internal/cli/hostfiles.go`, `global/codex-defaults.toml` | 572 | The Codex installer and personal Codex defaults. | Codex takes the plugin from `.codex-plugin/`. | Non-goal; L3, L4, M16 |
| `internal/gate/change.go`, `internal/cli/gate_suggest.go` | 420 | The signs of a "substantial" change, and the hint to install the hooks. | The core asks nothing about the size of a change, and `cw init` installs the hooks. | M15 |

## Plugin files

| Path | Files | Verdict | What happens | Basis |
| --- | ---: | --- | --- | --- |
| `.claude-plugin/`, `.codex-plugin/`, `.agents/plugins/` | 4 | Rewrite | The core's plugin lists no skills, no agents and no settings. Today's three settings (`enforce_verify`, `small_task_prompt`, `session_doctor`) go, because the gate is in git and not in the plugin. | `agent-integration`: The gate needs no plugin |
| `hooks/hooks.json`, `hooks/codex-hooks.json` | 2 | Rewrite | Two jobs remain: add the text of `cw context` at the start of a session, and ask before a git call that skips the hooks or an edit of `cw.toml`. The pinned `cw@v3.0.0-rc.2` goes, and so does the silent `exit 0` when `cw` is missing. | `agent-integration`: Session context, Instruction text comes from `cw`, Ask before skipping; C1 |
| `agents/` | 7 | Move | The seven roles. | Open Question 1 |
| `skills/task-orchestration/` | 29 | Move | The orchestration skill. It has to be rewritten there to use `cw run` and `cw status`. Open findings go with it: M15, M25. | Open Question 1 |
| `skills/` (20 other skills, `README.md`), `skills-manifest.json` | 80 | Move | Engineering knowledge that the roles load. Not part of a gate. | L15 |
| `third_party/` | 649 | Move | Imported skills: archify, cc-skills-golang, docker-skills, typesafe-ai. Open findings go with them: M17, M18. | Follows the skills |
| `global/AGENTS.md` | 1 | Move | The working conventions and review rules of the workflow. The core's session text comes from `cw context`. | M15, L11 |

The core then vendors no diagram tool. A diagram in the core's documents is
still an archify specification with its exported SVG, drawn with archify from
the layer or from upstream.

## Documents

| Path | Verdict | What happens | Basis |
| --- | --- | --- | --- |
| `README.md` | Rewrite | Lead with the promise and the five commands. The first install instruction uses a release archive and needs no Go. State the limits. Claim no benefit until the observe-mode numbers exist. | C1, H6, M24, L13; Success measures 1, 4, 5; `platform-support`: Install without a toolchain |
| `docs/quickstart.md` | Rewrite | The five-command start, executed by a test as today. | Success measure 1 |
| `docs/hooks.md` (904 lines) | Rewrite | The three hooks, sharing a hook with another tool, the known limits. | `publish-gate`; proposal, Known limits |
| `docs/operations.md` (1,946 lines) | Rewrite | The ten commands and the exit statuses. | M24; `gate-reporting` |
| `docs/evidence.md` | Rewrite | What a passing run proves and what it does not, then the measured numbers once they exist. | H7; Success measure 5 |
| `docs/README.md` | Rewrite | The index. Today it omits the quick start and `hooks.md`. | M24 |
| `SECURITY.md` | Rewrite | A guardrail, not a sandbox. No network. Which version is supported. | M24, L13; `check-evidence`: No network |
| `THIRD_PARTY.md` | Rewrite | Only the Go modules the core links. The skill sources move. | M17 |
| `CHANGELOG.md` | Keep | A new entry lists the breaking changes. | Proposal, What Changes |
| `CONTRIBUTING.md` | Keep | The sections on skills and language packs go. The diagram section keeps the archify-only rule and points outside the repository. | |
| `docs/development.md` (481 lines) | Keep | The sections on consultation, multilingual evaluation, tasks and metrics, the task record format, Graphify and Archify go. | |
| `LICENSE`, `CODE_OF_CONDUCT.md` | Keep | Unchanged. | |
| `licenses/` (6 of 8) | Keep | cobra, pflag, mousetrap, go-toml, Go and `golang.org/x/sys`. | |
| `licenses/yaml-LICENSE`, `licenses/adapted-skill-sources-LICENSE` | Move | They cover what the layer holds: the YAML module and the adapted skills. | |
| `docs/workflow.md`, `agents.md`, `decision-flow.md`, `skill-evaluation.md`, `docs/diagrams/` | Move | They describe the workflow. | Open Question 1 |
| `openspec/changes/add-post-merge-check-amendment/` | Move | It specifies `cw task add-checks`. It was implemented and never archived. | L15 |
| `docs/advisers.md` | Cut | Goes with consultation. | H5 |
| `docs/packs.md`, `docs/design/language-packs.md` | Cut | Go with the Go rule. | |
| `openspec/changes/reduce-cw-to-evidence-gate/` | Keep | This change. It is not part of `ce9b122`. | |

## Build, CI and repository files

| Path | Verdict | What happens | Basis |
| --- | --- | --- | --- |
| `go.mod`, `go.sum` | Keep | `gopkg.in/yaml.v3` leaves with the model policy and the packs. `go-toml` stays for `cw.toml`; today only the doctor and the content checks use it. cobra and `golang.org/x/sys` stay. | |
| `.goreleaser.yaml`, `.github/workflows/release.yml` | Keep | Release only from a commit on `main` whose CI passed. Add archives for Windows; today the builds are for darwin and linux. | M19; Success measure 4; `platform-support`: Install without a toolchain |
| `.github/workflows/ci.yml`, `.github/actions/go-test/action.yml` | Keep | Add a Windows job, the hook-cost measurement and the install check. The four shards per system can go once the test suite is the core's; the test that checks them, `internal/skillcheck/ci_shards_test.go`, goes with them. | Success measures 2, 4 |
| `.golangci.yml` (533 lines) | Keep | Its exemptions name code that leaves: `internal/taskflow` 28 times, `internal/cli` 17, `cmd/cw` 16. They go with that code, and the limits can then come down. | M22 |
| `.github/CODEOWNERS`, `dependabot.yml`, `PULL_REQUEST_TEMPLATE.md`, `ISSUE_TEMPLATE/bug_report.md` | Keep | The two templates name skills; those lines go. | |
| `.editorconfig`, `.gitattributes`, `.gitignore` | Keep | Unchanged. | |
| `.github/workflows/evals.yml`, `ISSUE_TEMPLATE/skill_proposal.md` | Move | Go with the evaluation cases and the skills. | M21 |
| `.github/ISSUE_TEMPLATE/pack_proposal.md` | Cut | Goes with the packs. | |

Repository settings (M20) and the repository description (L13) are not files.
They need your decision before anyone changes them.

## Tests

Test code is 109,209 lines, more than twice the code it tests.

| Group | Test lines | What happens |
| --- | ---: | --- |
| Tests of kept code (`internal/proc`, `internal/fslock`, `cmd/cw/main_test.go`, `buildidentity_test.go`) | 1,147 | Keep. |
| Tests of moved code | 52,902 | Move with their subject. |
| Tests of cut code | 23,083 | Cut with their subject. |
| Tests in the four split packages (`internal/cli`, `internal/gate`, `internal/projectctx`, `cmd/cw`) that their file name does not assign: 85 files | 32,077 | Each follows the code it tests. A test of rewritten code is rewritten. |

The core's tests are written from the 206 scenarios of the six specifications.
Four of today's tests are worth reading first, because they drive real git:

- `cmd/cw/quickstart_e2e_test.go` executes the quick start. Success measure 1
  asks for the same.
- `cmd/cw/hook_git_e2e_test.go`, `gate_e2e_test.go` and
  `staged_gate_e2e_test.go` commit, merge and push with the hooks installed.

## What the move costs

The task record does not build outside this repository as it is.
`internal/taskflow` imports five other packages, and none of them goes with it
whole:

| Imported package | Verdict here | What the layer does instead |
| --- | --- | --- |
| `internal/projectctx` | The snapshot is rewritten in the core; `merge.go` and `references.go` move | Ask the core for the snapshot. `cw status --json` gives the tree and whether it is verified. |
| `internal/langpack` | Cut | Nothing. What the Go rule required becomes checks in `cw.toml`. |
| `internal/consult` | Cut | Drop `consultation.go`, `consultation_records.go` and `adviser_claim.go` (483 lines), or move consultation too. |
| `internal/proc`, `internal/fslock` | Keep, in the core | Hold its own copy. Go does not let another module import an `internal` package. |

The record also still does three jobs the core now does: it runs the checks,
binds their results to the source, and tells the gate what to deny. The files
that do this, judged by their names and by the lines the review cites, are
`checks.go`, `executed.go`, `reuse.go`, `find.go`, `requirements.go`,
`packs.go`, `publication.go`, `budget.go` and `usage.go`: about 2,900 lines.
The findings the review placed in those files (C2, H2, H4, H9, M8, M9, M11,
M13, M14, L2, L7) end with them. `storage.go`
(760 lines) keeps a full copy of the record for every revision (M4, L8) and
needs a new design in the layer.

So the layer is its own piece of work, with its own task list. The core does
not wait for it.

## Yours to overrule

1. **Where the layer lives.** This list assumes a repository of its own. The
   answer to Open Question 1 keeps this repository for the core and does not
   name the layer's home. The other option is a folder here with its own Go
   module and plugin manifest; it keeps 1,820 files beside a core of about 70.
   The change `add-role-workflow` proposes a third option: seven role texts
   and three small commands, written anew in this repository, while the 1,820
   files stay on the branch.
2. **Consultation is cut (5,566 lines).** If you use advisers, it moves. The
   price is five open findings, and H5 and M12 need fixing before anyone else
   is offered the layer.
3. **Usage and budgets are cut (6,259 lines).** If they move, `shellwords`
   (1,048 lines) moves with them, because `usagelog` imports it, and M11 stays
   open. The change `add-role-workflow` proposes a token report that starts
   from four files of `usagelog` (1,223 lines) and leaves prices, budgets and
   history cut.
4. **Graphify, the doctor and the Codex installer are cut (2,949 lines).** They
   are closer to personal configuration than to a product (M16).
5. **`cw publish` and the Go rule are cut (6,412 lines).** In the layer,
   verification is `cw run`, so the Go rule has nothing left to attach to.

## Not decided by this list

- The layer's name.
- The version number and module path of the first core release. `go.mod` says
  `/v3`, and no `v3` tag had been published when this list was written (C1).
- The order of the work. That is `tasks.md`.

## How the cut is carried out

Two of your rules shape the work, and both belong in `tasks.md`:

- **No file is deleted.** A cut or moved file goes to a quarantine folder, and
  you remove the folder. The Go tool's `./...` pattern skips folders whose
  names begin with `_`, so a folder such as `_quarantine/` keeps that code out
  of the build and the tests.
- **Your own workflow keeps working.** `cw task` is the flow you use today. A
  branch at today's `main`, made before the first cut, keeps it usable until
  the layer exists.

## How this list was checked

- **Coverage.** A script matches every tracked file of `ce9b122` against the
  rows above: 2,260 files, none unmatched, no rule without a match. It also
  produced the file and line counts in this document. It is
  `reports/keep-and-cut-check.py`, run locally on 14 November 2026. `reports/`
  is local and git-ignored.
- **Dependencies.** The imports between the packages were read from the source
  with a search, not from a build.
- **Limits.** The verdicts rest on package comments, file names, imports and
  the review, not on a line-by-line reading of all 46,383 lines. The 85 test
  files in the split packages were not assigned one by one. The estimate of
  2,900 lines inside `internal/taskflow` is by file name. Nothing was built or
  run to test a verdict.
