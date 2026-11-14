# Tasks: reduce-cw-to-evidence-gate

Requirement and scenario names refer to the six files under `specs/`;
"scenarios under X" means every scenario of requirement X. Verdicts and rows
refer to [keep-and-cut.md](keep-and-cut.md). Success measures and Open
Questions refer to [proposal.md](proposal.md). Findings C1 to L15 are in
`reports/Review findings 2026-11-14.md`.

Rules for every task:

- **Tests.** The tests for the scenarios are written from the specification by
  someone other than the task's implementer: the test engineer, in the workflow
  this repository uses today. Implementation tasks do not author them. The
  tasks named "Tests" say when, and together they cover all 154 scenarios.
- **Checks.** A scenario that needs a command from a later task is checked when
  that task is done. Until then the task's check is the command it names.
- **Systems.** Every "Tests" task passes on `ubuntu-latest`, `macos-latest` and
  `windows-latest`. The git behaviour this change relies on was observed on
  Linux only, and nothing was observed on Windows (proposal, Sources and Known
  limits). A requirement that proves wrong on a system is corrected in the
  specification first, not in the test.
- **Names.** A test is named after the behaviour it checks, not after a review
  round (L12).
- **No file is deleted.** A file leaves by `git mv` into `_quarantine/`. Only
  the owner removes that folder (task 11.1).
- **Owner.** "Owner:" marks a task that only the owner can do. "Owner approves"
  marks a step that waits for the owner's word.

Order: the cut comes first, on `main`, after a branch keeps today's workflow.
From then on `main` holds only the core, every later task adds to it, and the
size limit can be checked at once (task 10.2). No release of `main` was ever
installable (C1), so only the owner's own use depends on today's `main`; tasks
1.2 and 1.3 cover it.

## 1. Decisions and safety

- [ ] 1.1 Owner: decide the module path and the name of the first tag. Recommended: keep `github.com/belevtsev/agent-workflows/v3` and tag `v3.0.0-beta.1` for the observe-mode period of group 12. `3.0.0-rc.2` in `CHANGELOG.md` names a state of the old workflow that was never tagged. Size: S. Depends on: none. Check: the decision is written into `proposal.md` under Impact, Release. Source: keep-and-cut.md, Not decided by this list; C1; `go.mod:1`; `CHANGELOG.md:185`.
- [ ] 1.2 Create a branch at the tip of `main` that keeps today's workflow, and push it. The owner names it; `workflow-v3` is a suggestion. The workflow layer later starts from this branch. Owner approves the push. Size: S. Depends on: none. Check: `git ls-remote --heads origin <branch>` prints the commit ID that `git rev-parse origin/main` prints. Source: keep-and-cut.md, How the cut is carried out.
- [ ] 1.3 Owner: point your own plugin install at that branch, and keep the `cw` on your `PATH` built from it, until task 10.4. Today's plugin hooks end with `exit 0`, so a `cw` that has no `hook pre-publish` turns today's gate off without a message. Size: S. Depends on: 1.2. Check: `cw version` prints a revision for which `git merge-base --is-ancestor <revision> origin/<branch>` exits 0, and `cw task list` runs. Source: `hooks/hooks.json` (`cw hook pre-publish; exit 0`); keep-and-cut.md, How the cut is carried out.
- [ ] 1.4 Put this change folder under version control: branch from the current `origin/main`, commit the folder and open a pull request. Owner approves the commit and the push. Size: S. Depends on: none. Check: `git ls-files openspec/changes/reduce-cw-to-evidence-gate` lists ten files (`proposal.md`, `design.md`, `keep-and-cut.md`, `tasks.md` and six `spec.md`), and CI passes on the pull request. Source: proposal, Status (the folder is untracked on `main` today).

## 2. Cut to a skeleton

Tasks 2.1 to 2.6 land in one pull request, so that `main` builds and CI passes
after it. `<base>` is the commit of `main` before that pull request.

- [x] 2.1 Move every file to `_quarantine/<same path>` with `git mv`, except: the Keep rows; the six plugin files under `.claude-plugin/`, `.codex-plugin/`, `.agents/plugins/` and `hooks/`; `README.md`, `SECURITY.md` and `THIRD_PARTY.md`; and this change folder. The Go sources and the five `docs/` files with the verdict Rewrite move too; they are reading material from then on. A file added after `ce9b122` takes the verdict of the row its path falls under. Size: M. Depends on: 1.2, 1.3, 1.4. Check: for every file of `<base>` outside those exceptions, `git ls-tree -r HEAD` holds the same blob at `_quarantine/<path>`; `git diff -M100% -l0 --diff-filter=D --name-only <base> HEAD` prints nothing; `go list ./...` lists no package under `_quarantine/`. Source: keep-and-cut.md, all tables and How the cut is carried out.
- [x] 2.2 Add the smallest command that builds: `cw version`, with exit status 2 for a wrong command line. `cmd/cw/main.go` also traps SIGHUP. `go mod tidy` leaves no module that nothing imports. Size: S. Depends on: 2.1. Check: `go build ./... && go vet ./... && go test -race ./...` pass; `cw version` exits 0; `cw nosuch` exits 2. Source: keep-and-cut.md, Go code that stays in the core; `gate-reporting`: Exit statuses; `check-evidence`: Interrupted runs; H2.
- [x] 2.3 Reduce the plugin files to a valid minimum: the manifests name no skills, no agents and no settings, and the two hook files hold no hook until group 8. Size: S. Depends on: 2.1. Check: `claude plugin validate .` passes; no manifest names a path that does not exist. Source: keep-and-cut.md, Plugin files; `.github/workflows/ci.yml` (job "Plugin manifests").
- [ ] 2.4 Fit CI and lint to what remains: one Go test job per system in place of four shards; the job names "Native Go / ubuntu-latest", "Native Go / macos-latest" and "Plugin manifests" unchanged, because branch protection requires them; a third job "Native Go / windows-latest", which is not a required check until task 11.3; the plugin job validates only `.`; `.golangci.yml` loses every exemption that names a path or function now under `_quarantine/`. Size: M. Depends on: 2.2, 2.3. Check: every CI job passes on the pull request. Source: keep-and-cut.md, Build, CI and repository files; M22; `.github/workflows/ci.yml:18-23`.
- [x] 2.5 Copy three document tests into the core with the helpers they share, and set their document list to the files that remain: `changelog_test.go`, `markdown_anchors_test.go` and `mermaid_test.go` from `internal/skillcheck/`. Size: S. Depends on: 2.1. Check: `go test ./...` passes; a `mermaid` fence added to `README.md` makes it fail. Source: keep-and-cut.md, Go code that moves to the workflow layer; `CONTRIBUTING.md`, Diagrams.
- [x] 2.6 Replace `README.md` with a short statement of the state: what the repository is becoming, that no release of the core exists, and which branch holds the previous workflow. Add the breaking changes to `CHANGELOG.md` under Unreleased. Remove from `CONTRIBUTING.md` and `docs/development.md` the sections on parts that left. Size: S. Depends on: 2.1, 2.5. Check: `go test ./...` passes, so links and anchors resolve; `README.md` names the branch of task 1.2. Source: keep-and-cut.md, Documents; proposal, What Changes; C1.

## 3. Declaration and snapshot

- [x] 3.1 Read and validate `cw.toml` from a tree: the fields of a check, the path patterns, and every refusal with the file name, the line and the rule that is broken. Size: M. Depends on: 2.2. Check: scenarios under "Path patterns"; `go test ./...` passes. Source: `check-declaration`: Declaration file, Check definition, Path patterns, Invalid declaration; L10.
- [x] 3.2 Compute the snapshot through a temporary index, changing neither the index, `HEAD`, a ref nor a file, with no limit on size. Refuse a working tree that git cannot record as it is on disk, and name the path and the cause: a skip-worktree or assume-unchanged mark, a sparse checkout, a failing `git add -A`. Size: M. Depends on: 2.2. Check: `go test ./...` passes; scenarios under "Snapshot" and "Working tree that cannot be recorded" at 5.1. Source: `check-evidence`: Snapshot, Working tree that cannot be recorded; C2, H1, M5; proposal, Sources.
- [x] 3.3 List the paths that differ between two trees, without rename or copy detection, and decide from a check's `paths` whether two trees hold the same checked content. Size: S. Depends on: 3.1. Check: `go test ./...` passes; scenarios under "Same checked content" at 5.1. Source: `check-evidence`: Same checked content; proposal, Terms.
- [ ] 3.4 Give `internal/fslock` and `internal/proc` a Windows implementation: a lock that a killed process cannot leave behind, and stopping every process a program started. Build their tests for Windows too. Size: M. Depends on: 2.4. Check: `go test -race ./...` passes on the three systems, with the tests of both packages running on Windows. Source: `platform-support`: Supported systems; `internal/proc/group_other.go`; `internal/fslock/fslock_other.go`; keep-and-cut.md, Go code that stays in the core.
- [ ] 3.5 Turn the git observations of proposal, Sources into a test that runs on the three systems. They were made on Linux only, with two local scripts. Size: M. Depends on: 2.4. Check: the test passes on the three systems; or each difference is written into `proposal.md` under Known limits, with the requirement it touches. Source: proposal, Sources (`reports/evidence-gate-git-facts.sh`, `reports/evidence-gate-git-facts-2.sh`) and Known limits.

## 4. Records and runs

- [x] 4.1 Keep one record per run, its log, and each hook decision under `cw/` in the common git directory, with `internal/fslock` for writers. Ignore a record that cannot be read, with a warning that names the file. Write nothing outside the git directory. Size: M. Depends on: 2.2, 3.4. Check: `go test -race ./...` passes; scenarios under "Record store" and "Damaged and concurrent records" at 5.1. Source: `check-evidence`: Record store, Damaged and concurrent records, What a run records; H9, M4, L8.
- [x] 4.2 Decide from the records whether a check is satisfied and a tree verified: the same `id`, `run` and `paths`; the same checked content; the decisive run that ended last, a tie counting the run that did not pass as later; no run older than 30 days. Size: M. Depends on: 3.1, 3.3, 4.1. Check: `go test ./...` passes; scenarios under "Satisfied check", "Verified tree" and "Old runs" at 5.1. Source: `check-evidence`: Satisfied check, Verified tree, Old runs; C2, M8.
- [x] 4.3 `cw run [<id>...] [--force]`: execute each check that is not satisfied, in the order of the declaration, from the repository root, without a shell, with the caller's environment and no standard input, and show its output as it is produced. Record each run for the snapshot taken immediately before the program started, with exactly one outcome. Exit 5 when an executed check did not pass and 2 for an unknown id. Size: L. Depends on: 3.2, 4.2. Check: scenarios under "Running checks", "Outcome of a run" and "Check definition", and scenarios "A misspelt key is refused" and "A command written as one string is refused". Source: `check-evidence`: Running checks, Outcome of a run, What a run records; `check-declaration`: Check definition, Invalid declaration; M9, M13.
- [ ] 4.4 Stop every process a check started when its run ends. When a leftover process holds the output open, keep the outcome the exit status gave and print a warning. On SIGINT, SIGTERM or SIGHUP, and on Windows on Ctrl+C, Ctrl+Break or the closing of the console, stop the check and record `interrupted`. Report a run whose `cw` process died as `interrupted`, and never let an unfinished run block another. `internal/proc` is used as task 3.4 left it. Size: M. Depends on: 3.4, 4.3. Check: scenarios under "Leftover processes" and scenarios "The terminal closes" and "Ctrl+Break on Windows"; scenario "`cw` is killed during a run" at 5.1. Source: `check-evidence`: Leftover processes, Interrupted runs; `platform-support`: Supported systems; H2, L7.
- [x] 4.5 Keep at most 10 MiB of a run's output, the first 1 MiB and the last 9 MiB, with a mark where output was dropped. Let `cw run` remove the logs of runs older than 30 days and the records older than 365 days. Size: S. Depends on: 4.3. Check: scenario "A very long log"; `go test ./...` passes. Source: `check-evidence`: Log limit, Old runs; M4, M14.
- [x] 4.6 `cw run --base <rev>`: for that command only, count each check that the tree of `<rev>` declares with the same `id`, `run` and `paths` as passed on that tree, and record no run for a check that was not executed. Size: S. Depends on: 4.3. Check: scenarios under "Base revision for continuous integration". Source: `check-evidence`: Base revision for continuous integration; proposal, Non-goals.
- [ ] 4.7 End a run as `failed` and name the program when it cannot be started. On Windows find a program as the system does, and pass each argument to a batch file unchanged or refuse the check. Write what the tests showed into `proposal.md` under Open Question 9. Size: M. Depends on: 4.3. Check: scenarios under "Programs that a system lacks" and "Programs on Windows". Source: `platform-support`: Programs that a system lacks, Programs on Windows; Open Question 9.

## 5. Status and setup

- [x] 5.1 `cw status [--json]`: print each declared check with its state, exit 0 when the snapshot is verified and 5 when it is not, say whether the index holds the same tree and list up to 20 paths that differ, and print the JSON object. Change nothing. Size: M. Depends on: 3.2, 3.5, 4.2. Check: scenarios under "Status", "Staged content", "Status as JSON", "Declaration file" and "Missing declaration", and scenario "A duplicate id is refused". Source: `gate-reporting`: Status, Staged content, Status as JSON, Gate state; `check-declaration`: Declaration file, Missing declaration; H9.
- [x] 5.2 `cw init` creates `cw.toml` with `version = 1` when the root has none and leaves an existing one byte for byte unchanged. `cw add` appends one check and changes no earlier byte. The hooks come in 6.1. Size: S. Depends on: 3.1, 5.1. Check: scenarios under "Create a declaration" and "Add a check from the command line". Source: `check-declaration`: Create a declaration, Add a check from the command line; H4; Success measure 1.
- [ ] 5.3 Tests: every scenario of `check-declaration` (16). Size: M. Depends on: 4.3, 5.2. Check: `go test -race ./...` passes on the three systems. Source: `specs/check-declaration/spec.md`.
- [ ] 5.4 Tests: the scenarios of `check-evidence` from "Snapshot" to "Log limit" (24). Size: L. Depends on: 4.4, 4.5, 5.1. Check: `go test -race ./...` passes on the three systems. Source: `specs/check-evidence/spec.md`.
- [ ] 5.5 Tests: the scenarios of `check-evidence` from "Same checked content" to "Damaged and concurrent records" (22). Size: L. Depends on: 4.6, 5.1. Check: `go test -race ./...` passes on the three systems. Source: `specs/check-evidence/spec.md`.

## 6. Hooks

- [x] 6.1 `cw init` installs `pre-commit`, `pre-merge-commit` and `pre-push` in the `hooks` directory of the common git directory. Each runs `cw hook <name>` with the arguments and input git gives it, and denies when `cw` is not on `PATH`, saying where the install instruction is and naming no Go command. `cw init` never changes a hook it did not write and writes none when `core.hooksPath` is set; it then exits 4 and prints the line to add, which must come last. Running it again changes nothing. On Windows the hook files run in the shell of Git for Windows. Size: M. Depends on: 5.2. Check: scenarios "Hooks are installed once", "An existing pre-commit hook", "A configured hooks directory is left to its owner" and "`cw` is missing". Source: `publish-gate`: Hook installation, Hooks of other tools, Fail closed; `platform-support`: Hooks on Windows, Install without a toolchain; `_quarantine/internal/cli/gate.go` (the marker line and exit status 4 today).
- [ ] 6.2 `cw hook pre-commit`: allow a commit only when the tree it would record is verified, whichever way git builds that tree. Decide from the records alone, without running or waiting for a check and without changing anything. On a denial, name each check that is not satisfied with its state and its log, list up to 20 paths that differ from the working tree's snapshot, end with the command to run, and name no way to skip the gate. Size: L. Depends on: 3.2, 4.2, 6.1. Check: scenarios under "Commit rule", "Paths that differ", "Hooks decide from records" and "Denial message", scenario "A formatter that runs before `cw` in a shared hook", and the scenarios under "Supported systems" and "Hooks on Windows". Source: `publish-gate`: Commit rule, Paths that differ, Hooks decide from records, Denial message; `agent-integration`: The gate needs no plugin; `platform-support`: Supported systems, Hooks on Windows; C2, H1, H9, L1.
- [x] 6.3 Fail closed: an enforcing hook that cannot establish that the content is verified exits 3 and names the cause. The causes are a tree it cannot compute, a missing or invalid declaration, a store it cannot read, and an internal error. Size: S. Depends on: 6.2. Check: scenarios "The declaration is invalid", "The declaration is gone" and "The store cannot be read". Source: `publish-gate`: Fail closed; `check-declaration`: Invalid declaration; C2.
- [x] 6.4 `cw hook pre-merge-commit`: apply the commit rule to the merge commit, and leave the merge in progress after a denial. Size: S. Depends on: 6.2. Check: scenarios under "Merge rule". Source: `publish-gate`: Merge rule; proposal, Sources.
- [x] 6.5 `cw hook pre-push`: read the refs git passes. Allow an update that deletes a ref or adds nothing, and otherwise only one whose new tip commit has a verified tree. Judge the tip only. One denied update denies the whole push, and the denial says where to run `cw run`. Size: M. Depends on: 6.2. Check: scenarios under "Push rule" and "Updates that add nothing". Source: `publish-gate`: Push rule, Updates that add nothing, Denial message; M26; Open Question 8.
- [x] 6.6 Print a notice when the tree a commit would record lacks a check that the tree of `HEAD` declares, or declares one with a different `run`, `paths` or `timeout`. Size: S. Depends on: 6.2. Check: scenarios under "Declaration change notice". Source: `publish-gate`: Declaration change notice; Open Question 4.
- [x] 6.7 Record each hook decision with its time, hook, mode, tree, outcome and reasons. `cw init --observe` puts the gate in observe mode: each hook records and prints in one line what it would have decided, and allows the operation. `cw init` returns the gate to enforcing. Size: M. Depends on: 6.3, 6.5. Check: scenarios "A cause that prevents a decision is observed too" and "Enforcing again"; the scenarios that read `cw stats` at 7.1. Source: `gate-reporting`: Recorded decisions; `publish-gate`: Observe mode; H7.
- [x] 6.8 `cw uninstall` removes the hooks that `cw` wrote and the `cw/` store, leaves `cw.toml` and every other hook file unchanged, and exits 4 with the line to remove when such a file still calls `cw hook`. Size: S. Depends on: 6.1. Check: scenarios under "Removal". Source: `publish-gate`: Removal; L3.
- [ ] 6.9 Tests: the scenarios of `publish-gate` under "Hook installation", "Hooks of other tools", "Commit rule", "Paths that differ", "Merge rule", "Hooks decide from records", "Fail closed", "Denial message" and "Declaration change notice" (27). Size: L. Depends on: 6.3, 6.4, 6.6. Check: `go test -race ./...` passes on the three systems. Source: `specs/publish-gate/spec.md`; `_quarantine/cmd/cw/hook_git_e2e_test.go`, `gate_e2e_test.go` and `staged_gate_e2e_test.go` (they drive real git today).
- [ ] 6.10 Tests: the scenarios of `publish-gate` under "Push rule", "Updates that add nothing", "Linked worktrees" and "Removal" (17). Size: M. Depends on: 6.5, 6.8. Check: `go test -race ./...` passes on the three systems. Source: `specs/publish-gate/spec.md`.
- [ ] 6.11 Tests: the scenarios of `platform-support` under "Supported systems", "Programs that a system lacks", "Programs on Windows" and "Hooks on Windows" (8). Size: M. Depends on: 4.7, 6.2. Check: `go test -race ./...` passes on the three systems. Source: `specs/platform-support/spec.md`.

## 7. Statistics, context and exit statuses

- [x] 7.1 `cw stats [--since <period>] [--json]`: from the repository's own records, print runs per check by outcome, hook decisions by hook, mode, outcome and reason, and per check how often it passed after failing on the same checked content. Size: M. Depends on: 6.7. Check: scenarios under "Statistics" and "Recorded decisions", and scenario "A commit that would be denied goes through and is counted". Source: `gate-reporting`: Statistics, Recorded decisions; H7.
- [x] 7.2 `cw context`: print at most 1,000 characters that say commits and pushes are gated, that `cw run` produces the evidence, that `cw status` shows what is missing, and whether the hooks are installed. Print nothing where the root holds no `cw.toml`. Size: S. Depends on: 6.1. Check: in `demo` the output holds `cw run` and `cw status` in at most 1,000 characters; in a clone without hooks it names `cw init`; without `cw.toml` it is empty. Source: `agent-integration`: Session context; M24.
- [x] 7.3 Hold the exit statuses of all ten commands in one table, and keep `cw` off the network. Size: S. Depends on: 6.8, 7.1, 7.2. Check: scenario "The statuses in one session"; `go list -deps ./cmd/cw` lists no `net/http`. Source: `gate-reporting`: Exit statuses; `check-evidence`: No network; H5.
- [ ] 7.4 Tests: every scenario of `gate-reporting` (12), the scenarios under "Observe mode" (3) and scenario "Everything works offline" (1). Size: M. Depends on: 7.3. Check: `go test -race ./...` passes on the three systems; the offline scenario runs where the system offers a network namespace. Source: `specs/gate-reporting/spec.md`; `publish-gate`: Observe mode; `check-evidence`: No network.

## 8. Plugin

- [x] 8.1 Session start, for Claude Code and for Codex: in a repository whose root holds `cw.toml`, add what `cw context` prints; when `cw` is not on `PATH`, add one line that says so, says where the install instruction is and names no Go command; anywhere else add nothing. The hook files hold no copy of that text and no `cw` version. On Windows the hook commands take the form that Open Question 11 settles; write the answer into `proposal.md`. Size: S. Depends on: 2.3, 7.2. Check: scenarios under "Session context" and "Instruction text comes from `cw`". Source: `agent-integration`: Session context, Instruction text comes from `cw`; `platform-support`: Install without a toolchain; Open Question 11; C1.
- [x] 8.2 Ask before skipping: before a shell command whose text holds `--no-verify`, and before a file-editing tool changes `cw.toml`, ask the user in Claude Code. For Codex, read its hook documentation first: ask if the host can, deny with the reason if it cannot. Act on no other tool call. On Windows the hook command takes the form task 8.1 settled. Size: M. Depends on: 2.3, 8.1. Check: scenarios under "Ask before skipping"; what Codex does, and the document that settles it, are written into `proposal.md` under Open Question 6. Source: `agent-integration`: Ask before skipping, The plugin never judges a commit or push; Open Questions 4, 6 and 11; M3.
- [x] 8.3 Finish the manifests: a description of the core, no skills, no agents, no settings. The `version` in a manifest is the plugin's own, and nothing compares it with `cw`. Size: S. Depends on: 1.1, 8.1, 8.2. Check: `claude plugin validate .` passes; no hook file holds a version number or an install command. Source: keep-and-cut.md, Plugin files; `agent-integration`: Instruction text comes from `cw`; C1.
- [ ] 8.4 Tests: every scenario of `agent-integration` (21). Size: M. Depends on: 6.2, 8.3. Check: `go test -race ./...` passes on the three systems. Source: `specs/agent-integration/spec.md`; `_quarantine/cmd/cw/hooks_test.go` (it runs the hook files' own commands today); `_quarantine/internal/cli/testdata/` (samples of each host's hook input).
- [x] 8.5 Read the command as words: `cw guard` splits a shell command into simple commands and words, and asks before a git call that holds `--no-verify`, `-n` of `git commit` or a setting of `core.hooksPath`, and before an edit of a hook file or of a record of `cw`. The same text inside an argument is no reason to ask; a command that cannot be split is judged by its text. Size: M. Depends on: 8.2. Check: the scenarios "The short form of the option", "A setting that replaces the hooks", "The option as the text of an argument", "A command that cannot be read" and "An edit of a hook or of a record". Source: `agent-integration`: Ask before skipping, The plugin never judges a commit or push; `docs/other-tools.md`, row G-2.
- [x] 8.6 The usage skill: write `skills/cw/SKILL.md` and its examples, and a test that replays every example against the `cw` under test, checks the head of every skill, and allows only commands that `cw help` lists. Size: M. Depends on: 8.3. Check: the scenarios under "Usage skill"; `claude plugin validate .` passes. Source: `agent-integration`: Usage skill; `docs/other-tools.md`, Skills.

## 9. Documents

- [x] 9.1 Write `docs/quickstart.md` with the five commands, and a test that executes exactly them in a new repository on each of the three systems: `cw init`, `cw add <id> -- <command>`, `cw run`, `git add -A`, `git commit`. Size: M. Depends on: 5.2, 6.2. Check: the test passes, the commit is recorded, and no file was written by hand. Source: Success measure 1; `_quarantine/cmd/cw/quickstart_e2e_test.go`.
- [x] 9.2 Write `README.md`: the promise, the five commands, the known limits, and where the previous workflow lives. The first install instruction, for each of the three systems, downloads the release archive of the tag of 1.1, checks it against `SHA256SUMS` and needs no Go; `go install` is the second instruction. The text says "a guardrail, not a sandbox", claims no measured benefit, and states nothing about independent tests or review. Size: M. Depends on: 1.1, 9.1. Check: `go test ./...` passes; the text says that the benefit is not yet measured; scenarios under "Install without a toolchain" at 11.5. Source: C1, H6, M24, L13; Success measures 1, 4 and 5; `platform-support`: Install without a toolchain; proposal, Known limits.
- [x] 9.3 Write `docs/hooks.md` (the three hooks, sharing a hook with another tool, the known limits), `docs/operations.md` (the ten commands and the exit statuses) and `docs/README.md` (the index). Size: M. Depends on: 7.3. Check: `docs/operations.md` has a section for each of the ten commands and the table of "Exit statuses"; `docs/README.md` links every file under `docs/`; `go test ./...` passes. Source: M24; `publish-gate`; `gate-reporting`: Exit statuses; proposal, Known limits.
- [x] 9.4 Write `docs/evidence.md` (what a passing run proves and what it does not), `SECURITY.md` (a guardrail, no network, which version is supported) and `THIRD_PARTY.md` (only the Go modules the core links). Size: M. Depends on: 1.1, 7.3. Check: `THIRD_PARTY.md` names every module that `go list -m all` prints besides the main module and nothing the core does not link or ship, and `licenses/` holds a file for each; `go test ./...` passes. Source: H7, M17, M24, L13; proposal, Non-goals; `check-evidence`: No network.
- [x] 9.5 Finish `CHANGELOG.md`, `CONTRIBUTING.md` and `docs/development.md`. The diagram section keeps the rule that archify is the only diagram tool and says where to get it. Size: S. Depends on: 9.3. Check: `go test ./...` passes; no document names a command that `cw --help` does not list. Source: keep-and-cut.md, Documents and Plugin files; proposal, What Changes.

## 10. Measures and self-use

- [ ] 10.1 Add a CI test of the hook cost: a hook decision takes at most 150 ms on this repository and at most 500 ms on a generated repository of 30,000 files and 68 MiB, on Linux and macOS. On Windows the test measures and reports only; write the numbers into `proposal.md` under Open Question 10, and the owner then sets the limits. The test states how it measures. Size: M. Depends on: 6.5. Check: the CI job passes on the three systems. Source: Success measure 2; Open Question 10; M4, M5.
- [x] 10.2 Add a CI step that counts the lines of every `.go` file that is not a `_test.go` file and is outside `_quarantine/` and `testdata/`, and fails above 6,000. Size: S. Depends on: 2.4. Check: the step passes, and fails in a trial with the limit set below the current count. Source: Success measure 3.
- [x] 10.3 Leave no complexity exemption in `.golangci.yml` for code that is not a test, and set the limits to the targets the file itself names: gocognit 30, funlen 80, gocyclo 20. Size: S. Depends on: 7.3. Check: `golangci-lint run ./...` passes. Source: M22; `.golangci.yml:20-21`.
- [ ] 10.4 Let this repository use its own gate. Commit a `cw.toml` with the repository's own test, vet and lint commands as checks, and add a CI job that runs `cw run --base` against the base branch of a pull request. Owner: install the new `cw`, run `cw init` in your clone, and turn the old plugin off on that machine; task 1.3 ends here, because the old plugin and the core both call `cw`. Size: M. Depends on: 4.6, 6.5. Check: in the owner's clone a commit with a failing test is denied and names the failed check; the CI job passes. Source: proposal, Known limits (CI is the backstop); `check-evidence`: Base revision for continuous integration; `CONTRIBUTING.md`, Built with itself.

## 11. Release

- [ ] 11.1 Owner: remove `_quarantine/`. Everything in it is on the branch of task 1.2. Size: S. Depends on: 8.4, 9.5. Check: `git ls-files _quarantine` prints nothing. Source: keep-and-cut.md, How the cut is carried out.
- [ ] 11.2 Change `release.yml`: refuse a tag whose commit is not on `main` or has no passing CI run, test with `-race`, run the lint and the vulnerability check, and publish build attestations and an SBOM. Build archives for windows on amd64 and arm64 as well, and list every archive in `SHA256SUMS`. Size: M. Depends on: 2.4. Check: a test of the step that decides passes for a commit that is not on `main` and for a commit without a passing CI run; a local snapshot build makes six archives and a `SHA256SUMS` that lists each; the release of task 11.4 then shows the attestation and the SBOM. Source: M19; Success measure 4; `platform-support`: Install without a toolchain; `.goreleaser.yaml`.
- [ ] 11.3 Owner: repository settings. Require the lint job and the Windows job, turn on secret scanning, push protection and Dependabot security updates, and update the description and the topics. Nobody changes these without the owner's word. Size: S. Depends on: 2.4. Check: the repository's settings show each of them. Source: M20, L13.
- [ ] 11.4 Tag the first pre-release named in 1.1. Owner approves the push. The release notes list the breaking changes, and `README.md` and `SECURITY.md` name the tag. Size: S. Depends on: 1.1, every task of groups 2 to 10, 11.1, 11.2. Check: in a clean environment the first install instruction of `README.md` succeeds and `cw version` prints the tag; the release holds archives for darwin, linux and windows on amd64 and arm64, and `SHA256SUMS`. Source: C1, L13; Success measure 4; `.goreleaser.yaml`.
- [ ] 11.5 Tests: the scenarios under "Install without a toolchain" (3), as a CI job on the three systems. It runs the first install instruction of `README.md` for that system with no `go` on `PATH`, repeats it with one byte of the archive changed, and compares the revision `cw version` prints with the commit of the tag. Size: M. Depends on: 11.4. Check: the job passes on the three systems, and fails in a trial with a wrong tag in `README.md`. Source: `specs/platform-support/spec.md`; Success measure 4; C1.

## 12. Measured benefit

- [ ] 12.1 Owner: run `cw init --observe` in at least three repositories in at least two languages, and let two weeks of agent work pass. Size: S. Depends on: 11.4. Check: in each repository `cw stats --since 14d --json` shows decisions in observe mode. Source: Success measure 5; H7.
- [ ] 12.2 Report in `README.md`, from `cw stats`, the share of commits that would have been denied and why, with the number of repositories and their languages. Only then may the text claim a benefit. Size: S. Depends on: 12.1. Check: every number in the text equals the `cw stats --json` output it was taken from. Source: Success measure 5; H7.

## Coverage

Every requirement has a task that builds it and a task that tests it.

| Capability | Requirement | Built in | Tested in |
| --- | --- | --- | --- |
| `check-declaration` | Declaration file | 3.1, 5.1 | 5.3 |
| | Check definition | 3.1, 4.3 | 5.3 |
| | Path patterns | 3.1 | 5.3 |
| | Invalid declaration | 3.1, 4.3, 5.1, 6.3 | 5.3 |
| | Missing declaration | 5.1 | 5.3 |
| | Create a declaration | 5.2 | 5.3 |
| | Add a check from the command line | 5.2 | 5.3 |
| `check-evidence` | Snapshot | 3.2 | 5.4 |
| | Working tree that cannot be recorded | 3.2 | 5.4 |
| | Running checks | 4.3 | 5.4 |
| | Outcome of a run | 4.3 | 5.4 |
| | Leftover processes | 3.4, 4.4 | 5.4 |
| | Interrupted runs | 2.2, 3.4, 4.4 | 5.4 |
| | What a run records | 4.1, 4.3 | 5.4 |
| | Log limit | 4.5 | 5.4 |
| | Same checked content | 3.3 | 5.5 |
| | Satisfied check | 4.2 | 5.5 |
| | Verified tree | 4.2 | 5.5 |
| | Old runs | 4.2, 4.5 | 5.5 |
| | Base revision for continuous integration | 4.6 | 5.5 |
| | Record store | 4.1 | 5.5 |
| | Damaged and concurrent records | 3.4, 4.1 | 5.5 |
| | No network | 7.3 | 7.4 |
| `publish-gate` | Hook installation | 6.1 | 6.9 |
| | Hooks of other tools | 6.1, 6.2 | 6.9 |
| | Commit rule | 6.2 | 6.9 |
| | Paths that differ | 6.2 | 6.9 |
| | Merge rule | 6.4 | 6.9 |
| | Push rule | 6.5 | 6.10 |
| | Updates that add nothing | 6.5 | 6.10 |
| | Hooks decide from records | 6.2 | 6.9 |
| | Fail closed | 6.1, 6.3 | 6.9 |
| | Denial message | 6.2, 6.5 | 6.9 |
| | Declaration change notice | 6.6 | 6.9 |
| | Linked worktrees | 4.1, 6.1 | 6.10 |
| | Observe mode | 6.7 | 7.4 |
| | Removal | 6.8 | 6.10 |
| `gate-reporting` | Status | 5.1 | 7.4 |
| | Staged content | 5.1 | 7.4 |
| | Gate state | 5.1, 6.1 | 7.4 |
| | Status as JSON | 5.1 | 7.4 |
| | Recorded decisions | 6.7 | 7.4 |
| | Statistics | 7.1 | 7.4 |
| | Exit statuses | 2.2, 7.3 | 7.4 |
| `agent-integration` | The gate needs no plugin | 6.2 | 8.4 |
| | Session context | 7.2, 8.1 | 8.4 |
| | Instruction text comes from `cw` | 8.1, 8.3 | 8.4 |
| | Ask before skipping | 8.2, 8.5 | 8.4 |
| | The plugin never judges a commit or push | 2.3, 8.2 | 8.4 |
| | Usage skill | 8.6 | 8.4 |
| `platform-support` | Supported systems | 2.4, 3.4, 6.2 | 6.11 |
| | Programs that a system lacks | 4.7 | 6.11 |
| | Programs on Windows | 4.7 | 6.11 |
| | Hooks on Windows | 6.1, 6.2 | 6.11 |
| | Install without a toolchain | 6.1, 8.1, 9.2, 11.2 | 11.5 |

| Success measure | Tasks |
| --- | --- |
| 1. First gated commit | 9.1 |
| 2. Hook cost | 10.1 |
| 3. Size | 10.2 |
| 4. Install | 9.2, 11.2, 11.4, 11.5 |
| 5. Measured benefit | 12.1, 12.2 |

| Open question | Settled in |
| --- | --- |
| 6. Can Codex ask the user from a hook? | 8.2 |
| 9. Arguments of a batch file on Windows | 4.7 |
| 10. Hook cost on Windows | 10.1 |
| 11. Plugin hook commands on Windows | 8.1, 8.2 |

| Finding the proposal left for this list | Tasks |
| --- | --- |
| M19 (release job) | 11.2 |
| M20 (repository protections) | 11.3 |
| M21 (scheduled evaluation run) | 2.1: the workflow file leaves `main` with the evaluation cases; the finding stays open on the branch |
| M22 (complexity limits) | 2.4, 10.3 |
| M23 (skill byte budgets) | 2.1: the test leaves `main` with the skills; the finding stays open on the branch |
| M24 (documentation) | 9.2, 9.3, 9.4 |
| L12 (test names) | The rule "Names" above; the 33 files leave `main` in 2.1 |
| L13 (badge, supported version, description) | 9.2, 9.4, 11.3, 11.4 |

## Not in this list

- The workflow layer. It starts from the branch of task 1.2 and has its own
  task list (keep-and-cut.md, What the move costs).
- The roles and the token report. They are the change `add-role-workflow`,
  which has no task list yet and cannot start before group 8 is done.
- Fixes for findings in code that leaves `main`. They stay open on the branch.
- Sparse checkouts, package managers and other version control systems
  (proposal, Non-goals).
