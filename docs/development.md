# Go development and releases

The manager and its development automation use the Go version pinned in go.mod.
Skills are separate resources under skills/ and third_party/; their helper scripts
retain their own dependencies. No Go module vendor tree is present.
The resource-only third_party module boundary keeps bundled example Go files
out of the manager's build and test package discovery.

The application has a thin command entrypoint, injectable CLI I/O, focused internal
installation/runtime/validation packages, and typed ownership/recovery boundaries.
Preserve exact historical JSON sealing, immutable receipts, and the distinction
between manager identity and the active skill snapshot.

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

First-party automation must use Go. Only install.sh is allowed as a bootstrap;
script resources supplied by skills are explicitly exempt. Do not add Python
installer tests, environment preparation, or functional release shell scripts.

Frozen historical state/journal fixtures must remain readable after refactors.
Exercise interruption at every transaction boundary, changed owned content,
concurrent commands, custom paths, direct invocation outside source, resource-root
migration in both directions, and uncertain publication outcomes.

Additive registration scenarios must preserve historical ten-registration snapshots
and v1 checksums. Verify additions, pre-merge conflicts, rollback removal of only
absent-origin records, and source-independent recovery while the enrolled manager
still points at its older runtime. Bootstrap fixtures cover v3/v4/v5 refusal, v6
reuse and fresh acquisition, dry runs, and acquisition failures that preserve
the cached manager and owned installation. Exact-revision acquisition retains
its checksum and identity checks. Backend decision fixtures live in
internal/devcheck/testdata/backend-skills; give evaluators raw inputs without their
expected decisions. Model evaluations and fresh discovery are separate from native
test assertions; CI does not call hosted models or receive service credentials.

`cw consult` lives in internal/consult. Its tests use local HTTP servers and synthetic
records to check three-adviser requests, deadlines, partial failures, and
evidence-bound finalization without service keys:

```sh
GOWORK=off go test ./internal/consult
```

internal/consult/testdata holds goldens captured from the original helpers, covering
records, wire requests, digests and validation messages. They freeze the
retained-record contract, so a change there is a compatibility change: new behavior
needs new goldens rather than edits to these. Keep historical record versions readable.

The multilingual corpus is a managed skill resource, and `cwdev eval multilingual`
is its runner. Offline tests check complete translations, withheld expected answers,
deterministic scoring and safe bounded execution using synthetic transports. Run the
explicit hosted evaluation separately, following the
[evaluation guide](../skills/task-orchestration/references/consultation-helper.md#multilingual-evaluation).
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

New task records use version two for typed worker assignments, results and
coordinator acceptance. Preserve version-one revision chains, plans, historical
evidence hashes and completion semantics; never silently upgrade an old task or
permit a version change inside its chain. Worker APIs require a new task. Tests
must cover wrong task/brief/agent bindings, ownership overlap and unsafe paths,
false check claims, incomplete or cancelled work, finding dispositions, independent
review identity, and stale source/check/worker evidence. Substantial-change gates
apply to new tasks; small and unchanged read-only tasks keep their existing path.
Use project-neutral fixtures, including a non-Go project, for API and CLI coverage.
Worker records remain separate from sealed installer formats and adviser records.

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
GOWORK=off go run ./cmd/cwdev release build --source "$PWD" --version v1.0.100 --revision COMMIT_SHA --dist "$PWD/dist"
GOWORK=off go run ./cmd/cwdev release publish --source "$PWD" --version v1.0.100 --revision COMMIT_SHA --dist "$PWD/dist"
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
pass. Every successful main push produces v1.0.<workflow run number> for its exact
SHA; main runs are not superseded by later commits. Only obsolete PR runs cancel.
Publication credentials exist only in the final job, and CI contains no shell
business logic. Publication uses GitHub's legacy latest-release policy so semantic
version/creation ordering governs overlapping releases.

Keep credentials, machine state, backups, evaluations, and audit outputs outside
source. Preserve all licenses/notices and maintain THIRD_PARTY.md. The manifest
defines 26 registrations and 71 entrypoints; model defaults come from its
validated policy. The v6 bootstrap capability supports manifest v2 and verified
personal directory adoption, retaining the max/xhigh policy. The separate
`cw-model-policy-v2-dual-choice` capability gates the paired policy extension
without changing sealed runtime or recovery formats. Bootstrap must replace an
older v6 cache only after acquiring a manager with both capabilities, and preserve
owned state when latest is incompatible; older managers require the one-time bootstrap
[migration](operations.md#migration). Frozen historical ultra policies remain unchanged and
must still validate for rollback and source-independent recovery.

The separate `cw-three-adviser-v1` capability gates the required Jev/Snap/OpenAI
provider policy. Bootstrap checks it alongside every previous capability, including
`cw-openai-decisions-v1` for historical optional policies. Native activation checks
the capability matching the selected snapshot's provider requirements. Ownership,
runtime receipts and recovery journals keep their existing sealed formats.
Consultation records evolve separately: version three requires all three providers;
historical version-one pair and version-two optional records remain readable offline.
Tests must reject disabled required coverage and opt-outs, preserve historical
adjudication, and distinguish unavailable, partially refused and all-refused advice.

For an unreleased committed local revision, use the pinned Go toolchain and the
normal native CLI with all installation roots explicitly selected:

```sh
GOWORK=off go run ./cmd/cw install --source /path/to/agent-workflows --home /path/to/home --codex-home /path/to/home/.codex --state-dir /path/to/home/.local/state/agent-workflows --dry-run
GOWORK=off go run ./cmd/cw install --source /path/to/agent-workflows --home /path/to/home --codex-home /path/to/home/.codex --state-dir /path/to/home/.local/state/agent-workflows
```

Replace the example paths with the intended real roots. Source must be clean and
committed. This uses normal exact-revision acquisition/build and managed activation;
it does not edit installed snapshots or bypass published-manager capability checks.

Personal adoption fixtures must cover original and distributed inventories that
differ, same-path Archify replacement, all directory/link boundaries, interrupted
recovery resumption, cross-operation path overlap, and checkout-free cached v6
recovery while the enrolled command remains old. V2 ownership/journal/runtime
records use the same historical checksum encoding; never rewrite frozen fixtures.

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
