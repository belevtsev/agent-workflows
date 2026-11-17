# Reviews

Implementation is split among three Sol/high subagents: typed context adapters,
saved briefs and ordered local stage records. The coordinator owns integration,
documentation, regression review and final checks. This file supplies no human
approval or semantic completion certificate.

| Finding | Correction | Regression |
| --- | --- | --- |
| Interrupted writes could poison the immutable canonical filename | Stage private temporary content, sync/close, then atomically publish without overwriting | TestSavedWorkerBriefInterruptedStagingCanRetry |
| Clean filters could hide unauthorized raw edits beside allowed Git-visible edits | Bind original per-path raw hashes and check raw changes against original role limits | TestStageRejectsHiddenRawWriteAlongsideAllowedTreeEdit |
| A rehashed mutable finish receipt could claim unchecked write approval | Revalidate original write bounds, fixed receipt semantics, current policy and evidence before repeat completion or cleanup | TestStageRepeatRejectsRehashedUnauthorizedReceipt |
| A replaced ledger directory could redirect locks or records | Pin Git directory, use a sibling inode-checked lock and contained ledger roots; refuse replacement | TestStageLedgerRejectsSymlinkAndReplacementWithoutExternalWrites |
| Another brief or undersized budget could mutate receipts too early | Hold ledger through public brief/prepare and preflight output before selection writes | TestStageActiveIntervalBlocksAllBriefReceiptMutations and TestStageInsufficientBudgetDoesNotCreateReceiptOrActiveStage |

All findings above are resolved in local regressions. Independent re-review
confirmed the raw-source, receipt and ledger corrections. Interrupted historical
completion with later unauthorized edits requires explicit abandonment and
preserves the old receipt; it never silently approves current content.

The host still launches workers. These helpers do not prove skills loaded,
semantic TDD, reviewer independence or task completion. Source/check freshness,
approvals, finding dispositions, role limits and the hooked commit remain
authoritative. No model CLI or paid pilot was run.
