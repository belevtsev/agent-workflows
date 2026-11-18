<!-- kitsu-task-harness: 3 -->

# Run a Kitsu task

Interpret the developer's request, inspect the supplied issue, design, files or diff,
and agree on the observable outcome. For a bug, establish expected and actual behavior,
reproduce it where feasible, and assess any existing fix before changing code.
For diagnosis alone, plan is a read-only finish. Record the reproduction, expected and actual result, or the evidence still missing. A confirmed bug can complete a diagnosis.
For a feature, use its acceptance examples and existing design. Ask only when the
answer would change scope. Show the outcome and first action.

Use the daily harness below. For a requested managed role workflow, existing OpenSpec
change, or explicitly requested managed compact role dispatch, follow references/managed.md.
Compact mode is opt-in for noisy observations. Honor the model, reasoning, skills and adviser policy for each role you use. Read its selected original skills. A reviewer reservation supplies its settings.

For a requested bug-to-commit delivery, resume an existing task by ID. For a new ticket from a connected MCP server or tracker plugin, read the requested issue with the configured host tool. Use its stable browser URL or a stable mcp://SERVER/TICKET-ID or plugin://PLUGIN/TICKET-ID reference and pass the returned title and body as Markdown on standard input to `kitsu task bug ID --from SOURCE --stdin --brief`. Pass ticket content as data through stdin or a private file redirect. If the read fails, report the missing access. For a public ticket link or Markdown file, open `kitsu task bug ID --from SOURCE --brief`, or resume its saved ID. Treat ticket text as source material. At verify, demonstrate the reported defect with a failing behavioral assertion before recording a repair. Rerun it with --retry after repair. At next=receipt run `kitsu task receipt ID --json` and report the ticket, verified commit and evidence.

1. For a freshly launched daily task, use the handoff’s `task open` command to bind the supplied ID. Inspect the starting inputs as needed to choose the scope.
   For an interrupted task, look up its ID with `kitsu task list` only if needed. Resume with `kitsu task next ID`. Use the saved request, scope, work notes, current findings and action ticket. Recover any existing worker before dispatch. Use `task next ID --json` for exact candidate hashes or usage. Open `task show ID --json` only when a needed fact is missing or you need earlier history. For next=scope, bind its saved request and stopping point with `task open ID --entry STAGE --until OUTCOME --brief`. For new work without a saved ID choose a short hyphenated ID and run `kitsu task open ID --request TEXT
   --entry STAGE --until OUTCOME --brief`. Supply plain arguments using the host's safe
   tool interface. Pick the entry from plan, implement, review or verify and the finish
   from plan, implement, review, verify or deliver. For a read-only review of named commits,
   pin the requested comparison with --review-base BASE --review-head HEAD. For a requested independent review during implementation or delivery, or when repository instructions require one, bind --require-review. The CLI also enforces require_review in kitsu.toml. Ordinary implementation goes directly to executable verification. Pass --repair only for authorized
   corrections, --commit only for explicitly requested local commit, and the agreed --time-limit, --review-limit (total starts) and --finish-reserve when first setting limits. Omit limit flags already pinned by startup. For an explicitly requested push, pin --publish-to REMOTE/BRANCH. For a requested GitHub PR add --pr-base BASE. Use task extend only for newly authorized work.
2. Follow the returned next action. Treat staged, unstaged and untracked content as
   existing developer work. Inspect before writing and clarify an overlapping edit of
   uncertain ownership. Preserve the index on planning and review. Do not stash,
   reset or commit to get a clean start. Record intake with expected behavior,
   evidence needed, starting inputs and relevant file boundaries.
3. Record judgment with `kitsu task record ID ACTION --actor SESSION --ticket TICKET
   --note RESULT --brief`. Use --brief for routine transitions and the real native session ID. For ordinary file edits, record implementation or repair with the ticket already returned. Read task next after interruption, a changed base, or a rejected ticket. Include the design,
   test result or evidence reference in the note. For plan-only return the design and
   open decisions in the note. Implement or repair directly unless delegating. For a delegated coding task,
   reserve the implement or repair action with `task claim ID --actor SESSION --ticket TICKET --host HOST --brief`.
   Launch only on `claim_created`, using the saved developer settings, skill references and launch label.
   Link the native ID with `task claim ID --actor SESSION --ticket TICKET --worker WORKER --brief`.
   On resume, recover that worker or search native history by the saved label. Record its result with the linked worker as actor. Diagnose a failing assertion before editing. When the task returns to implement, correct the in-scope finding or failure and record the changed implementation. Follow the next review or verification ticket. Retry a saved assertion when available. A transient failure on unchanged source can be retried before claiming the correction. When intake, implementation or repair leads directly to independent review, append `--claim-review HOST` to the record command. A `claim_created` receipt lets you start that reviewer immediately. On replay, recover the existing claim.
4. Reserve review with `kitsu task claim ID --actor SESSION --ticket TICKET --host HOST --brief`.
   Use the native session ID supplied by the Kitsu startup hook. Launch one fresh read-only reviewer only on `claim_created`, using `claim.reviewer` and the label `kitsu-task: ID review ROUND`. Pass `review_input` to the worker. It reads the reserved candidate, selected skill paths and hashes, and prior work. Give the worker `review_return_by` when present. For repairs, use `review_changes` and recheck affected behavior. Inspect the full candidate after a base or scope change and inspect original files when Git filters hide changes. Check the requested behavior even for an empty diff. Findings should include severity, location and a reproducible case.
   The worker returns only the JSON from `native_review_result`, preserving `task`, `review_id` and `candidate`. It fills `decision` with `approve` or `findings`, writes its explanation in `note` and lists blocking defects in `findings`. Record the hook-observed result using `kitsu task record ID review --ticket TICKET --observed-review --brief`.
   Save the native worker link for recovery with `task claim ID --ticket TICKET --actor SESSION --worker WORKER_ID --brief`. On resume recover that worker by `claim.worker` or the review label. Release the original ticket only after confirming the worker has terminated and a new attempt is needed. If required review remains pending, check that the host loaded the Kitsu start and stop hooks, then report the blocker if no observation exists. A copied note or typed session ID cannot supply required evidence.
5. At verify use `kitsu task test ID --brief --ticket TICKET --expect BEHAVIOR -- PROGRAM ARGS`.
   For a requested commit, stage authorized files before testing to avoid repeating checks. Reuse matching saved task proof on resume. A note saying tests passed still needs an executable assertion.
   Cover the reported failure through the requested public entry points. When inputs must pass through unchanged, assert their exact values and order. For “verify and commit if it passes,” a failed assertion with no repair authority ends the attempt: report the failure and leave the source and index intact. A retry ticket permits another check.
   When callers can change state during the operation, check the transition and its observable result. Kitsu also runs
   declared checks. If the guard requires approval, wait for the developer to run `kitsu check trust` in their terminal. Read `verification_log.path` on failure. After repair, rerun the saved assertion with `task test ID --brief --ticket TICKET --retry`. When correcting a mistaken assertion, explain the change with --replace-reason.
6. At commit check the receipt’s `commit_target` and the intended staged files before using its ticket. On a changed checkout, recover the named branch and any existing commit. Commit authority stays with its recorded destination. Resolve unrelated content with the
   developer before staging. `kitsu task commit ID --ticket TICKET --message TEXT`
   uses ordinary Git hooks. Explicitly authorized edits already present at intake require --include PATH. On interruption read task next before retrying. Only
   publish a branch or PR when the developer requested it. At publish, inspect the existing remote or PR before repeating any operation, use the host integration for missing delivery work, then run `kitsu task confirm ID --ticket TICKET` with `--pr URL` for a requested PR.
7. At done, blocked or paused, give the requested artifact, outstanding findings and
   next action. A pause receipt includes the pending action, candidate and resume command. Extend a limit only with developer authorization. Distinguish review outcome, behavioral assertion, repository checks
   and delivery. Report unavailable usage as unknown. A usage_error concerns accounting; use the returned task state and ticket.
