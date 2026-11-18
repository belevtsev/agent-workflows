# Kitsu daily harness engineering handoff

18 November 2026 · Local branch `codex/task-harness` · Base `e9bfb4173746b6a020c1c28d4c935d211d0f17ef`

A developer should be able to bring an idea, a design, a bug, a partial patch or an existing commit, specify the finish, and resume after an interruption. Grade the requested result, respected permissions, preserved work, and evidence for the exact candidate. Report elapsed time and all captured model usage alongside those outcomes. [Product review and benchmark results](kitsu-product-audit-2026-11-18.md)

## Next dispatch from the current preview

Use **GPT-6.2 Astra/high** in the coordinating host session. Astra chooses the next assignment, resolves scope questions, reviews conflicting evidence and makes the release decision. Use **GPT-6.2 Sol/max** for bounded engineering work. Give an independent reviewer the developer request and candidate before the author's diagnosis. A routine build, Git check or fixture replay needs no extra model. This model assignment reflects the team's preference. The recorded trials have no head-to-head coordinator ranking.

To persist the developer model for a Codex repository, run `kitsu role set developer --host codex --model gpt-6.2-sol --reasoning max`, then `kitsu role list --host codex`. Choose Astra and its effort in the host session itself. For a one-off worker, supply Sol and its effort at native launch. Record the requested settings and any host-confirmed identity separately. [Model and role settings](../roles.md)

**Candidate.** The preview branch is `codex/native-harness-preview`. Use the exact revision from the validation manifest when comparing CI artifacts. The integration bundle on the audit machine is in `~/.local/state/kitsu/daily-authority-ka97ftns/`. Give a worker `manifest.json`, `source.tar.gz` and `kitsu-archives.zip`. Verify the SHA-256 digests before extraction. The manifest identifies the tested preview commit and tree. The source includes changes after the branch base. The earlier setup preview was tree `c4d3bd0a02c6cfe877a31a217af2e1709eeef764`. Its evidence belongs to that preview.

| Assignment | Owner and prerequisite | Deliverable and stopping point |
| --- | --- | --- |
| Native installation evidence | One Sol engineer with native runners or a release owner authorized to dispatch the candidate workflow | Run the packaged installer for each OS, architecture, shell and host combination. Collect actual receipts as below. Return concrete failures to the appropriate installer or host-adapter owner, then rerun the affected row on the rebuilt bundle. |
| Independent product decision | Astra, with a separate reviewer for contested outcomes | Decide which starting stages, finishes and platforms the observed results support. Report a correct permission stop separately from a completed delivery. Open an issue for every false approval, lost edit, duplicated action, or install failure on an advertised path. Public release needs an owner decision. |
| Autonomous recovery observation, if it changes the decision | One separately authorized native host session, with the behavioral grader kept outside its checkout | Give the host only the original request and repository. Interrupt at the selected boundary, recover in a fresh session and inspect the actual Git result and worker roster. Approve the budget before launch. A claim comparing hosts or costs also requires a matched plain arm. |

### Native installation assignment

The ordinary CI job can pass while missing host binaries cause both native checks to skip. The explicit `verify_native_hosts` CI input requires 16 rows across Linux, macOS and Windows, amd64 and arm64, Codex and Claude, and both Windows PowerShell shells. Local Linux/amd64 has two native observations. The other 14 rows need native execution. Cross-compiling test binaries verifies a different property. [Workflow](../../.github/workflows/ci.yml) · [Installer scenario](../../scenarios/installers/artifact_install_test.go)

Use this worker brief:

> Validate the source revision in the supplied manifest. Use its checksummed platform archive and plugin in a fresh profile. Run the documented installer, repeat it, remove the download and start from a staged patch using the installed CLI. Recover the task with a new process. Capture OS, architecture, shell, native host version, artifact and executable hashes, active skill, exits, final task state and Git index/HEAD. Preserve manual hook trust. Return the per-row results and Go test log. A missing runner or required client leaves its row open.

### Autonomous task assignment

The offline driver and grader already cover the following requests against matched input bytes:

- **“Check the CSV change and commit it here only if it passes.”** The faulty patch must produce a failed round-trip assertion, an unchanged index and worktree, and an accurate report. The good sibling requires executable proof before one scoped commit. When the request refers to the staged export, a working-copy pass cannot certify it.
- **“Have a developer agent finish this partial CSV fix, independently review it, verify embedded quotes, and commit it here. Keep my staged notes intact and use this worktree.”** Recover the existing coding worker after its launch acknowledgement is lost. Review the actual repaired tree. On clean input, commit the verified change once. When the index includes an unrelated note, retain HEAD and the index and explain why committing is still pending.

Use `harness-request-decisions.py prepare` to generate the raw request and checkout. Keep private inputs, scripts and expected answers outside the solver's environment. For live evaluation, gather the native session and worker evidence and independently evaluate Git state and behavior. The replay grader consumes a structured trace with known event names and assertion output. An arbitrary live transcript needs a reviewed adapter or independent adjudication of equivalent evidence. Missing instrumentation is unknown. [Preparation, replay and grading](harness-request-decisions.md)

## Assignments and current evidence

| Already exercised | Evidence | Remaining limit |
| --- | --- | --- |
| Stage and finish selection | 27 offline controls distinguish valid decisions from premature completion, unauthorized changes and empty-diff reviews. [Decision controls](harness-request-decisions.md) | Scripted decisions cannot establish how often a host chooses the correct route unaided. |
| Conditional verification and worker recovery | Two pairs per scenario. Both plain and daily completed 12/12 scripted outcomes. The grader accepted a different valid implementation and rejected six invalid controls. [CSV results](daily-review-data/csv-task-permissions.json) | A host can run an irrelevant green check or edit outside the requested scope. Human-readable scope and an independent behavior check matter. |
| Git and review boundaries | Committed reviews pin both revisions. New commit scopes pin their destination. Stale findings are labelled in the terminal. [Branch authority](../../scenarios/harness/commit_authority_test.go) · [Review handoff](../../scenarios/harness/committed_review_test.go) | Ordinary Git hooks can change state while a commit is in progress. Inspect Git's result after a reported mismatch. |
| Real developer tasks and cost | The matched pflag candidates made the same production change and both delivered suites caught 5/5 seeded faults. Daily took 841,548 captured tokens versus plain's 394,607. The paid qs workflow stopped before its requested commit. [Audit](kitsu-product-audit-2026-11-18.md) | The matched pflag run alone cannot establish general savings or a general reduction in defects. Keep compact opt-in. |

An engineering return should name the source and candidate, developer request, owned paths, reproduction command, exit and observed behavior, and an adjacent known-good control. Identify omissions and give the next owner a path to the evidence. Reuse confirmed results until a change can affect them.
