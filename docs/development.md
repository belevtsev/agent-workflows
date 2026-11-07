# Go development and releases

The manager and its development automation use the Go version pinned in go.mod.
First-party code, including the helpers skills call, is Go. Third-party skills under
third_party/ keep their own dependencies. No Go module vendor tree is present.
The resource-only third_party module boundary keeps bundled example Go files
out of the manager's build and test package discovery.

The application has a thin command entrypoint, injectable CLI I/O, focused internal
installation/runtime/validation packages, and typed ownership/recovery boundaries.
Preserve the sealed JSON encoding, immutable receipts, and the distinction between
manager identity and the active skill snapshot.

## Verification

```sh
GOWORK=off go run ./cmd/cwdev check --race --source "$PWD"
```

The developer check verifies formatting and first-party script policy, runs native
tests/vet/source validation, builds the manager once, and runs isolated installation
smoke scenarios. Fixtures use temporary homes, synthetic Git repositories, and
mock HTTP services. They never change the user's installation, consult hosted
decision models, or require service credentials. Real Bash/zsh execution verifies PATH enrollment.
Use `go test` with a focused package or test filter while developing; the complete
developer check runs the release verification gates once.

The check needs a clean checkout at an exact commit, because it builds the smoke
binary from that commit. To verify uncommitted work, add `--snapshot`:

```sh
GOWORK=off go run ./cmd/cwdev check --race --snapshot --source "$PWD"
```

It copies the tracked and Git-unignored untracked files, which `git add -A` would
commit, into a new repository under the temporary directory. It commits them there
with a fixed identity and isolated Git configuration and runs the same gates in that
copy. The source is left untouched, unless an explicit `--binary` names a path
inside it; by default the binary is built inside the copy. The output includes a line
`Snapshot tree: <hash> (...)` before the gates run, and a passing run ends with
`Verified snapshot tree: <hash>`. After committing the same work, compare:

```sh
git rev-parse HEAD^{tree}
```

The two hashes are equal when the commit contains exactly the verified files.
A difference means the commit left out or added something, such as an ignored file
the gates needed or a file staged only in part. It can also come from the source's
own Git configuration, such as `core.fileMode=false`, a clean filter or
`core.autocrlf`, which the copy does not use. In either case, rerun the check.

The copy is removed when the gates pass, fail or are interrupted. Each copy holds a
lock for its lifetime. A run killed outright, for example when `cw task run` times
out a check, leaves its copy behind. The next `--snapshot` run removes it, or, if the
run died before taking its lock, removes it once the copy is an hour old. It prints
a warning for any leftover copy it cannot remove. Concurrent runs never touch a copy
whose owner is alive. Submodules, nested repositories, unmerged paths, sparse-checkout
(skip-worktree) and assume-unchanged entries, and tracked paths replaced by special
files are refused. Git does not list untracked special files, so the copy leaves
them out. `--snapshot` cannot prepare release assets: `--release-version`,
`--revision` and `cwdev release` still require a clean exact checkout.

First-party automation must use Go. Only install.sh is allowed as a bootstrap;
script resources supplied by skills are explicitly exempt. Do not add Python
installer tests, environment preparation, or functional release shell scripts.

Exercise interruption at every transaction boundary, changed owned content,
concurrent commands, custom paths, direct invocation outside source, and uncertain
publication outcomes, on each host.

Host coverage must keep Codex-only and Claude-only installs free of each other's
files, restore user `CLAUDE.md` and `settings.json` byte for byte, leave drifted
settings keys alone in status, update, rollback, uninstall and recovery, and keep
the remaining host working after a single-host uninstall. Additive registration
scenarios verify additions, pre-merge conflicts, rollback removal of only
absent-origin records, and source-independent recovery. Bootstrap fixtures cover
refusal of older release protocols, fresh acquisition, dry runs, and acquisition
failures that leave the owned installation unchanged. Release acquisition for
`cw update` retains its checksum and identity checks.
Backend decision fixtures live in
internal/devcheck/testdata/backend-skills; give evaluators raw inputs without their
expected decisions. Model evaluations and fresh discovery are separate from native
test assertions; CI does not call hosted models or receive service credentials.

`cw consult` lives in internal/consult. Its tests use local HTTP servers and synthetic
records to check configured participation (including `not_configured` and
`no_advisers_configured`), deadlines, partial failures, and evidence-bound
adjudication without service keys:

```sh
GOWORK=off go test ./internal/consult
```

internal/consult/testdata holds goldens for version-4 records, wire requests,
digests and validation messages. They freeze the current record contract, so a
change there is a format change. Only the current version is readable; earlier
record versions are rejected with an error that names version 4.

The multilingual corpus is a managed skill resource, and `cwdev eval multilingual`
is its runner. Offline tests check complete translations, withheld expected answers,
deterministic scoring and safe bounded execution using synthetic transports. Live
runs call only configured providers and report the others as `not_run` with reason
`not_configured`. Run the
explicit hosted evaluation separately, following the
[evaluation guide](../skills/task-orchestration/references/adviser-providers.md#multilingual-evaluation).
Keep its reports outside source and immutable releases, freeze corpus/oracle hashes
before requests, and retain incorrect or unavailable results without automatic
retries. A live baseline measures only its labeled cases; it is not a CI assertion
or production-quality qualification.

Global project diagnostics, task evidence and measured-outcome aggregation live in
native Go packages, separately from sealed installer state. Tests use isolated
multi-language repositories and owned child processes. Protect project-independent
dispatch, dirty/untracked/branch source identities, stale completion rejection,
actual failed/cancelled checks, consultation task/phase/source bindings, safe
external record storage and metric unknowns/comparability. Metadata-only doctor
checks never invoke optional tools or hosted providers. Real-project read-only
diagnostics, synthetic acceptance pilots and productivity comparisons are separate
evidence; preserve that distinction in reports.

Task records use version 3 for the adviser binding, typed worker assignments,
results and coordinator acceptance. An older task record is refused with guidance
to start a new task;
never silently upgrade an old task or permit a version change inside its chain.
Tests must cover wrong task/brief/agent bindings, ownership overlap and unsafe
paths, false check claims, incomplete or cancelled work, finding dispositions,
independent review identity, and stale source/check/worker evidence.
Substantial-change gates apply to new tasks; small and unchanged read-only tasks
keep their existing path. Use project-neutral fixtures, including a non-Go
project, for API and CLI coverage. Worker records remain separate from sealed
installer formats and adviser records.

Changes to the independent test-author workflow also need bounded Codex and
Claude smokes outside CI: give fresh authors only an approved contract and test
scaffold, retain their authored patch identity, then execute their tests against
unseen correct and defective implementations. Inspect initial context, available
tool transcripts and effective model/effort; report enforcement and telemetry
limits explicitly. Exercise red-test handoffs, ownership conflicts, missing
contracts/seams, changed interfaces, contaminated feedback and prohibited assertion
weakening. These are synthetic workflow checks, not measured project-quality
improvements, and they must not modify personal host configuration.

`cw graphify` lives in internal/graphify:

```sh
GOWORK=off go test ./internal/graphify
```

Its synthetic repositories and fake local runtime verify tracked-source scope,
cache identity, stale/incomplete refusal, fixed CLI modes, and environment
isolation without installing Graphify or using hosted models. A real local smoke
check with the pinned runtime verifies extraction and query behavior separately;
it does not establish resolved Go call paths. Keep generated graph caches outside
source and immutable releases. The [runtime setup](operations.md#optional-graphify-runtime)
is explicit per device; manager install and status must never provision it.

## Distribution

The separate cwdev tool owns release automation, not the user manager:

```sh
GOWORK=off go run ./cmd/cwdev release build --source "$PWD" --version v2.0.100 --revision COMMIT_SHA --dist "$PWD/dist"
GOWORK=off go run ./cmd/cwdev release publish --source "$PWD" --version v2.0.100 --revision COMMIT_SHA --dist "$PWD/dist"
```

Replace COMMIT_SHA and the example version with the intended exact committed
revision and unique version. Publication requires explicitly authorized GitHub
credentials; builds/checks do not. Binaries support macOS/Linux ARM64/AMD64, with
CGO disabled, trimmed paths, fixed archive metadata, dependency notices, and
SHA256SUMS. The publisher validates local assets before remote writes, verifies
exact tags/assets, reconciles uncertain outcomes, and refuses conflicting assets.
Readback allows bounded visibility delays after a write; mutations are attempted
once, and an unresolved outcome must be inspected before resuming publication.
Publishing an already exact public release performs no writes.

CI retains cached Linux/macOS tests and race checks. Release artifacts are prepared
alongside Linux validation, then reused by publication after both platform checks
pass. Every successful main push produces v2.0.<workflow run number> for its exact
SHA; main runs are not superseded by later commits. Only obsolete PR runs cancel.
Publication credentials exist only in the final job, and CI contains no shell
business logic. Publication uses GitHub's legacy latest-release policy so semantic
version/creation ordering governs overlapping releases.

Keep credentials, machine state, backups, evaluations, and audit outputs outside
source. Preserve all licenses/notices and maintain THIRD_PARTY.md. The manifest
defines 33 registrations and 78 entrypoints; model defaults come from its
validated policies. `cw-manager-v10` uses self-contained resource payloads,
per-field preferences, sealed configuration roots, state/journal4 and version3
standalone runtime receipts. Installations made before v2.0.58 (source-checkout
managers and codex-workflows v1), or migrated by v2.0.58 from one, are refused
before any write. Rollback retains the capable installed manager and derives an
effective snapshot from cached base + current preferences.

Release builds export an exact immutable source revision, create a deterministic
allowlisted payload, and inject it only into that disposable export. Generated
payload files never belong in the maintained worktree. Release preflight recomputes
the expected payload identity from the same source. CWRELEASE1 and version JSON
remain compatible; the independent CWBUNDLE1 marker binds the embedded base digest.
Archive checksums bind complete executable bytes. Developer builds without generated
payload still support offline help/version but cannot silently install from cwd.

Normal updates reject older stable versions and equal-version conflicting identities,
including after skill rollback. Prepared cache artifacts alone are not authority.

Consultation records evolve separately and have one current format, version 4,
whose providers are exactly the configured participation set. Model policies must
be version 3. Tests must distinguish `not_configured`, unavailable, partially
refused and all-refused advice, cover `no_advisers_configured` without network
access, and reject task checkpoints that differ from the task's adviser binding.
They must also enforce the adjudication rules for typed evidence, investigations
and untrusted agreement.

To try an unreleased committed revision, build its release archives with the
pinned Go toolchain, extract `cw` from the archive for your platform, and run it
with all installation roots explicitly selected:

```sh
GOWORK=off go run ./cmd/cwdev release build --source "$PWD" --version v0.0.1 --revision "$(git rev-parse HEAD)" --dist /path/to/dist
mkdir /path/to/local-cw && tar -xzf /path/to/dist/cw_darwin_arm64.tar.gz -C /path/to/local-cw cw
/path/to/local-cw/cw install --home /path/to/home --codex-home /path/to/home/.codex --claude-home /path/to/home/.claude --state-dir /path/to/home/.local/state/agent-workflows --dry-run
/path/to/local-cw/cw install --home /path/to/home --codex-home /path/to/home/.codex --claude-home /path/to/home/.claude --state-dir /path/to/home/.local/state/agent-workflows
```

Replace the example paths and platform with the intended ones. Source must be
clean and committed. A plain `go build ./cmd/cw` has no embedded workflow payload
and refuses lifecycle commands.

Personal adoption fixtures must cover original and distributed inventories that
differ, same-path Archify replacement, all directory/link boundaries, interrupted
recovery resumption, cross-operation path overlap, and recovery from a cached
runtime.

With Node.js available, run the bundled portable Archify tests from its package:

```sh
node --test third_party/archify/test/*.test.mjs
```

This includes available Chrome/Chromium browser gates. Missing browser or optional
generator packages must be reported separately; skipped gates are unverified.
Keep render output outside the immutable package. Archify's managed update helpers
must make no HTTP requests, cache writes, or installed-file changes. Hash/runtime
closure and preserved licenses are covered by the shared import provenance and
native suite checks; installed prerequisites are never provisioned by the manager.

## Isolated binary acceptance

`cwdev sandbox test --source /clean/snapshot --backend docker --output /private/report.json`
builds two exact-source fixture releases outside the container, then passes only
those artifacts and the independently authored probe into a disposable test home.
The minimal image has no Git/Go and no host repository, HOME, credentials or Docker
socket mount. Synthetic keys and a loopback test service stay inside the test.

`--backend sbx` explicitly selects standalone Docker Sandboxes and the checked-in
`testing/sandbox/sbxenv.yaml` shell environment. It uses no workspace mounts, host
hooks, shared secret injection or MCP services. Its outer shell template may contain
development tools; the probe restricts child tools/PATH separately. Missing engine,
SBX executable or authentication is unavailable coverage with a nonzero result;
there is no backend fallback. Resources have unique owned names and cleanup removes
only those created by the run. Reports distinguish availability, probe results and
cleanup failure. Live host model smokes and personal installation activation remain
separately authorized work outside CI.
