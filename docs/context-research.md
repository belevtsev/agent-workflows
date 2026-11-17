# Context and orchestration research

Research checked on 16 November 2026 using the connected search-service tool, official
documentation, research papers, and this repository. This research preceded the
separate [24-run Codex pilot](benchmarks/codex-compact-pilot-report.md), which is now complete.
The pilot found incomplete token pairs and a workflow completion problem under its time limit;
it does not establish a task-wide saving or general quality equivalence.

**Verdict.** Kitsu should first reduce repeated discovery, deterministic coordination and verbose
observations through its opt-in Go collector, expandable evidence and complete worker briefs.
These mechanisms fit its existing role and verification boundaries. A new executable-code runtime,
automatic model router or custom host compactor needs a separate experiment. This prioritization
is an inference; the implementation has not demonstrated an aggregate token or cost saving.

The target is less total resource consumption for equally reliable, verified work. Smaller
responses, fewer commands, lower bills and better results are distinct outcomes. A worker can
protect the coordinator's context while increasing the task's total usage. Likewise, a local
artifact cache avoids collection work but repeatedly printing its payload still supplies input
to the model. Count all coordinator, worker and adviser usage, including failed attempts.

**What local evidence establishes.** The historical `cw` experiments used Claude Code 2.1.311,
one tiny repository per run and one run per arm. The prices below are host-reported list-price
estimates from those runs, not current rates. All listed results passed their hidden tests.
[Experiment and limits](benchmarks.md#what-an-agent-spends).

| Task and arm | Total tokens | Reported cost | Role starts |
| --- | ---: | ---: | ---: |
| `duration`, gated single agent | 143,383 | $0.16 | 0 |
| `duration`, full workflow | 6,950,768 | $9.84 | 16 |
| `slug`, gated single agent | 105,817 | $0.14 | 0 |
| `slug`, explicitly requested roles | 683,341 | $0.72 | 4 |

The `slug` coordinator accounted for 523,790 tokens. In `duration`, coordination accounted for
2,612,892, reviews for 1,340,770 and later writing rounds for 1,616,273. Between 87% and 93% of
the experiments' tokens were cache reads. Coordination and restarts therefore warrant
investigation, but their attribution does not prove they were unnecessary. `rework` also includes
incorporating user answers and implementation after interface scaffolding.
[Attribution](benchmarks.md#the-roles-on-new-behaviour), [accounting](tokens.md#writing-tokens-and-workflow-tokens).

Passing tests did not establish request fidelity: the workflow's non-text input behavior departed
from the requested exception type, and the acceptance suite missed it. A script approved every
question; a real developer could change both trajectory and cost. Nothing was run on Codex.
The formal comparison remains unrun. [Observed disagreement](benchmarks.md#the-roles-on-new-behaviour),
[planned comparison](evaluation.md#the-comparison-with-one-agent).

The feature also produced 1,023 record lines and twelve questions. Artifact expansion and
question-driven restarts are plausible cost drivers, but those counts do not establish that
the questions or records were unjustified. Evaluate unsupported requirements and repeated prose
alongside request adherence, rather than imposing an arbitrary documentation-size target.
[Artifacts and questions](benchmarks.md#the-roles-on-new-behaviour).

**Who controls the levers.**

| Surface | Kitsu controls | Host or provider controls |
| --- | --- | --- |
| Evidence delivery | Read/search scope, batches, output bounds, retained artifacts, cursors and stale-data checks | Whether native calls already share one inference request |
| Handoffs | Stage, task group, constraints, source references, requested specialist model and selected skills | Fresh-worker execution, native role loading, actual model availability and permissions |
| State | Task records, content identities, check-evidence rules and resumption guidance | Conversation serialization, history replacement and compaction timing |
| Caching | Stable generated text and local collection reuse | Prefix matching, cache routing, retention, eviction and prices |
| Cost | Reported token categories and known/unknown attribution | Actual usage, reasoning behavior, billing and subscription allowances |

OpenAI's current cache documentation requires matching rendered prefixes; changes to tools,
instructions, relevant settings or history can interrupt reuse. A persistent session does not
guarantee a hit. Kitsu can cooperate with caching but cannot configure these API mechanisms from
an ordinary repository CLI. [OpenAI prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching).
Claude Code currently defers MCP definitions by default and performs its own caching and
auto-compaction. An old all-schemas-upfront MCP comparison is therefore an unsuitable modern
baseline. [Claude Code cost guidance](https://code.claude.com/docs/en/costs).

Fewer tool invocations do not necessarily mean fewer model requests: native hosts already group
independent calls. The useful reductions are unnecessary inference steps and data the model
would otherwise read. OpenAI describes this append-results-and-infer loop in its
[23 January 2026 Codex engineering article](https://openai.com/index/unrolling-the-codex-agent-loop/).

**Practical improvements, in priority order.** Benefits in this table are hypotheses to measure.
The current interfaces provide mechanisms; later rows are follow-up proposals.

| Priority and status | Improvement | Expected mechanism and tradeoff | Measure |
| --- | --- | --- | --- |
| 1, current interface | Batch scoped reads/searches/Git observations in Go | Avoid repeated discovery and unnecessary output; broad batches can overfetch | Model requests, returned bytes, relevant-source recall |
| 1, current interface | Bounded responses with expandable raw evidence | Keep observations useful without flooding context; tiny pages can force extra turns | All fetched pages, omitted-evidence errors, total retrieval bytes |
| 1, current interface | Record-derived worker briefs | Preserve mandatory constraints and source identities; incomplete scope causes rediscovery | Brief plus successor usage, rework, constraint recall |
| 1, current interface | Compact check outcomes with retained logs | Avoid repeated passing logs; failures must remain discoverable | Diagnostic recall, log bytes, correct-fix latency |
| 2, later | Runner-specific diagnostic extraction | Surface failures buried in logs; parsers require fallbacks | Failure recall against full logs and recovery cost |
| 2, later | Bounded symbol/dependency navigation | Locate relevant declarations before reading bodies; ranking can miss dependencies | Localization accuracy, reads, index cost |
| 2, later | Current-state deltas and restart reasons | Avoid repeated completed-stage explanations; canonical history must stay inspectable | Repeated starts, stale handoffs, coordination usage |
| 2, workflow discipline | Batch available questions and findings before required restarts | Avoid fragmented exchanges and duplicated requirement prose; unresolved behavior still needs an answer | Questions, restarts by reason, unsupported requirements |
| 3, separate experiment | Model/effort profiles, semantic compression or executable plans | May reduce expense on suitable work; can add retries, runtime cost or fidelity loss | Total cost and quality by task class |

The collector's default response limit is 16 KiB, with a configurable 512-byte to 1-MiB range.
It accepts up to 32 operations and executes at most four independent reads concurrently.
This is a byte guarantee, not a tokenizer-independent token guarantee. Pagination bounds one
response, not the full conversation. The private artifact cache and the provider's prefix cache
must have separate metrics. [Engine](../internal/contextpack/collect.go),
[limits and evidence types](../internal/contextpack/types.go).

Briefs should retain the task/stage/round label, exact request or authoritative reference,
assigned task group, outcomes, write boundaries, selected policy, starting content identity,
unresolved questions, and observed check records. Returns should name changed paths, result
identities, unmet outcomes and remaining prerequisites. Receipts do not prove skill loading;
artifacts do not grant approval or replace check evidence.
[Brief preparation](../internal/cli/compact_brief.go).

Reviewer context remains independent of the author's opinions or completion narrative. Supply
requirements, design, changed-test reasons, source, diff and check records. Current full-path
specification/design approvals, review rounds, test-before-code requirements and the final gate
remain mandatory. Tiny clear fixes already have a direct gated exception; a general small-feature
fast path or merged author/reviewer would change policy. [Workflow](workflow.md),
[coordinator contract](../internal/roles/text/coordinator.md).

One full-path task group already requires eight role starts besides lookup; the small path uses
four without findings. Reduce rediscovery inside those boundaries before changing topology.
Reference unchanged approved requirements instead of rewriting them, preserve the required
author restart after answers, and distinguish repairs from task-group transitions in accounting.
Shared artifacts can avoid regenerating a handoff, but each worker's read still consumes input.

**What comparable approaches actually show.**

Cloudflare's 20 February 2026 Code Mode API server exposes `search()` and `execute()` over more
than 2,500 endpoints with roughly 1,000 tokens of tool surface. Its headline reduction compares
schema footprints, not complete coding-task consumption. Generated code composes calls inside
an isolate and returns selected results. The transferable idea is pre-context composition and
filtering; Kitsu's fixed operations need no general JavaScript runtime.
[Cloudflare implementation](https://blog.cloudflare.com/code-mode-mcp/).

Anthropic's current programmatic-tool-calling documentation reports about 38% fewer billed input
tokens on an internal 75-tool benchmark with unchanged accuracy, but about 8% higher cost on
sequential τ²-bench with unchanged scores, without a Kitsu comparison. Intermediate results stay outside model context.
The facility requires hosted code execution; tools from the API's MCP connector and strict tools
are incompatible. These results favor selecting by workload, not blanket enablement.
[Anthropic PTC](https://platform.claude.com/docs/en/agents-and-tools/tool-use/programmatic-tool-calling).

OpenAI's current PTC similarly supports composition, but runs JavaScript in fresh isolated V8
runtimes without Node.js, subprocesses, direct networking or a general filesystem. Applications
configure eligible tools and still execute client-owned calls. This API capability does not
establish that Kitsu controls Codex's runtime.
[OpenAI PTC](https://developers.openai.com/api/docs/guides/tools-programmatic-tool-calling).

Aider's repository map ranks symbols/signatures over file dependencies and fits relevant
portions to a nominal 1,000-token default budget, which can expand. A comparable Kitsu navigation
index could reduce exploration, but should lead to exact source reads rather than substitute
for implementation evidence. [Aider repository map](https://aider.chat/docs/repomap.html).
SWE-agent's November 2024 ablation favored a 100-line viewer over smaller windows or full files
in its setup, illustrating why the smallest response is not necessarily the best response.
[SWE-agent](https://arxiv.org/html/2405.15793v3).

LLMLingua-2 uses small encoder models for extractive token compression; its August 2024 evaluation
does not establish fidelity for source code, exact limits or approval constraints. Prefer
lossless structural reduction and selective retrieval first.
[LLMLingua-2](https://aclanthology.org/2024.findings-acl.57/).
The October 2025 *Complexity Trap* study found simple observation masking competitive with
LLM summaries in its tested coding systems, motivating a simple baseline rather than an
automatic summarizer. [Paper, v3](https://arxiv.org/abs/2508.21433v3).

Conversely, *The Physical Cost of Context* (4 November 2026 revision) found a masking configuration
that reduced logical prompt volume while increasing uncached work and missing its quality gate, without testing Kitsu.
Its local-serving experiments are not Codex or Claude bills. Together these studies show why
prefix reuse, requests and quality must accompany token totals.
[Paper, v2](https://arxiv.org/abs/2610.34763v2).

*Handoff Debt* (30 August 2026 revision) compared repository-only, trace, summary and structured
takeovers. Context-bearing handoffs reduced rediscovery, while solved-rate effects depended on
the successor. Handoff production and successor effort must both be charged; takeover evidence
does not justify sending author framing to independent reviewers.
[Paper, v2](https://arxiv.org/abs/2606.02875v2).
*Evaluating AGENTS.md* (29 September 2026 revision) found repository overviews generally unhelpful
in its settings, although non-standard practices remained useful. Extra generated instructions
therefore need evaluation, not automatic preloading.
[Paper, v3](https://arxiv.org/abs/2602.11988v3).

More agents are not a general efficiency strategy. The 12 October 2026 coding-team preprint found
better ProgramBench reward at three to seven times solo-agent cost without testing Kitsu; cheaper implementers reduced
reward in its experiments. This does not determine Kitsu's model policy, but argues for measuring
where delegation pays back. [Coding-agent teams](https://arxiv.org/abs/2610.17471v1).
CompactionBench's October 2026 preprint likewise identifies compaction/cache interactions and
explains why repeated dependent requests are needed to evaluate long-session behavior. Its
asynchronous compaction requires harness control Kitsu presently lacks.
[CompactionBench](https://www.microsoft.com/en-us/research/wp-content/uploads/2026/10/CompactionBench_preprint.pdf).

## Measurement protocol

The implemented [offline comparison](compact-context.md#observed-offline-comparison) now has
five cold samples per arm: compact noisy checks emitted about 6.4 kB versus 193.5 kB, while
complete exploration emitted about 10.1 kB versus 5.9 kB. Handoff preparation used two CLI
calls versus six but emitted extra JSON. These measurements demonstrate workload-dependent
transport behavior, not a task-wide token or monetary benefit.

Begin with free deterministic replay, then separately authorize model
experiments. Do not infer an end-to-end saving from output-size tests alone.

1. Compare baseline reads/check output with the complete compact interaction: request, metadata,
   initial response and every fetched page. Cover broad/empty searches, failures in log middles,
   Unicode, long lines, unreadable files, minimum budgets, interruptions, stale policy/content,
   and expired artifacts. Check evidence preservation, explicit omissions, exit status and
   freshness. Report bytes and local latency; label tokenizer-specific estimates.
2. Keep the published comparison intact: 12 tasks, three languages, three trials and two arms,
   totaling 72 runs. Add a separately labeled context experiment comparing the same gated
   workflow with compact mode off/on. Use identical models, efforts, permissions, review/approval
   policy and native host batching. Evaluate routing or fewer stages in separate arms.
   [Published protocol](evaluation.md#the-comparison-with-one-agent).
3. Add representative repositories and task classes: clear fix, multi-file bug, new behavior,
   interface change and dependent follow-up requests long enough to exercise compaction.
   Predetermine answers to ambiguity; do not approve every recommendation automatically.
   Repeat conditions, randomize order, and record cache warmth, host/model versions, prompts,
   skills, tools and starting Git state.
4. Keep acceptance tests outside every agent-visible tree and tool. Evaluate the resulting
   commit separately. Check request fidelity, regressions, constraint recall, review findings,
   write-policy violations and the verified terminal state, not only agent claims or test count.
5. Capture uncached input, cache writes, cache reads, output/reasoning as exposed, actual models,
   unknown usage, coordinator/workers/advisers, elapsed time and human interventions. Also record
   inference requests, local operations, worker starts, restart reasons, duplicate source spans,
   all visible bytes, page fetches, check reruns, artifact hits and provider cache reuse.
6. Report failures with their spent resources, variation across runs, cost per verified success,
   and success under fixed resource budgets. Use dated rates per actual model or provider charges;
   label list-price estimates and keep unknown amounts unknown. Preserve the existing meaning of
   workflow share: review tokens are not automatically waste. [Accounting limits](tokens.md#limits).

The immediate risks are retrieval that is too narrow, pages that amplify requests, hidden failure
diagnostics, stale handoffs and added instructions that duplicate native facilities. Preserve raw
evidence and mandatory constraints, then measure whether fewer discoveries and shorter
coordination trajectories offset retrieval overhead. No defensible aggregate Kitsu saving
percentage follows from the research or historical scratch runs alone.
