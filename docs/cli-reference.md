# CLI command reference

Generated from command metadata. For setup and worked examples, see the [CLI guide](cli.md).
Regenerate with `GOWORK=off go test ./internal/cli -run TestUXCommandReferenceIsGenerated -update-cli-reference`.

## agent

### kitsu agent setup

```text
usage: kitsu agent setup [options]

Install the verified Kitsu plugin from a local release bundle.

Options:
  --host <codex|claude>    override the saved CLI host preference
  --archive-dir <directory> required directory containing kitsu-plugin.zip and
                            SHA256SUMS
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Examples:
  kitsu agent setup --host codex --archive-dir /path/to/kitsu-archives

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu agent check

```text
usage: kitsu agent check [options]

Check local CLI, plugin, task skill and hooks without executing the host.

Options:
  --host <codex|claude>    override the saved CLI host preference
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Setup: local inspection does not prove host authentication, model access, hook
trust or runtime execution. Follow the reported manual host checks.

Examples:
  kitsu agent check --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## task

### kitsu task start

```text
usage: kitsu task start [<request>] [options]

Open an interactive agent host with the Kitsu task skill.

Arguments:
  <request>          one nonempty request; quote the complete request

Options:
  --host <codex|claude>    override the saved CLI host preference
  --print                  print diagnostics and the handoff prompt without
                           opening the host
  --json                   print machine-readable JSON without prompts or
                           styling
  --resume <task>          launch a saved local task instead of creating another
  --until <outcome>        finish at plan, implement, review, verify, deliver
  --compact                opt in to Go context collection, compact briefs and
                           check results
  --require-review         require an independent review of the candidate
  --time-limit <duration>  time remaining before the task pauses
  --review-limit <count>   maximum total independent review starts, including
                           released reservations
  --finish-reserve <duration> time before the deadline reserved from new reviews
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Setup: local inspection does not prove host authentication, model access, hook
trust or runtime execution. Follow the reported manual host checks.

Start: requires terminal input, stdout and stderr and no known local blockers.
Daily startup pins a task before the host opens in a repository. Use --resume
to reopen it. The host may request permissions. --print and
--json prepare a handoff even when setup is incomplete; they execute nothing
and create no task records. The two output options are mutually exclusive.
A launched host's exit status is preserved; interruption exits 130.

Examples:
  kitsu task start "Plan a login screen" --host codex --until plan
  kitsu task start "Fix the failing test" --host claude --print
  kitsu task start --resume fix-login --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task list

```text
usage: kitsu task list [options]

List daily requests, managed tasks and saved small-task histories.

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu task list

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task show

```text
usage: kitsu task show <task> [options]

Show task records, or an explicit bounded coordinator snapshot/delta.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --review-ticket <id>     read the reserved candidate, findings and work notes
                           for the independent reviewer as JSON
  --review-brief           read the shorter handoff for the reserved reviewer
  --compact                return a full coordinator snapshot with references;
                           always JSON
  --since <snapshot-id>    return only sections changed since a retained
                           snapshot; implies --compact
  --max-bytes <bytes>      compact response byte limit; default 16384, range 512
                           to 1048576
  --help, -h               show this command's help without executing it

Examples:
  kitsu task show add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task check

```text
usage: kitsu task check [<task>]

Check saved task records offline.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu task check add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task verify

```text
usage: kitsu task verify <task> [options]

Run prioritized regressions, record historical evidence, then finish required checks.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --from <rev>             starting revision; required initially, then reuse the
                           pinned base
  --workflow <full|small>  select full or small task history
  --json                   print machine-readable JSON without prompts or
                           styling
  --compact                return an explicit bounded JSON report with retained
                           log references
  --max-bytes <bytes>      compact response limit; default 16384, range 512 to
                           1048576
  --help, -h               show this command's help without executing it

Examples:
  kitsu task verify fix-login --from HEAD
  kitsu task verify fix-login --compact

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task do

```text
usage: kitsu task do <task> [options] [-- <program> [<argument>...]]

Open, record, verify and commit a task in one command, stopping where another session or a decision is needed.

Arguments:
  <task>             task name: lowercase words joined by hyphens
  <program>          executable run directly without a shell
  <argument>         argument passed unchanged to the check program

Options:
  --request <text>         original developer request
  --from <source>          bug ticket source URL or Markdown file
  --stdin                  read bug ticket Markdown from standard input and
                           retain its source link
  --entry <stage>          implement for your own change, verify for a supplied
                           one
  --expect <text>          observable behavior asserted by the command after --
  --replace-reason <text>  explain why a failing assertion needs a different
                           command
  --commit                 delivery includes a local commit
  --message <text>         commit message; the verified candidate is committed
                           with it
  --include <path>         confirm a changed path that you are delivering;
                           repeatable
  --require-review         require an independent review of the candidate
  --actor <id>             native session identity
  --claim-review <host>    reserve the required independent review
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task do export-json --request "Add JSON export" --expect "An empty export is []" -- go test ./export
  kitsu task do export-json --message "Add JSON export" --include export.go

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task bug

```text
usage: kitsu task bug <task> --from SOURCE [options]

Open a ticket for reproduction, repair and verified local commit delivery.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --from <source>          ticket source URL or Markdown file
  --stdin                  read ticket Markdown from standard input and retain
                           its source link
  --require-review         require an independent review of the candidate
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task bug csv-fix --from tickets/csv.md

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task ci

```text
usage: kitsu task ci <task> --from PR --check ID [options]

Reproduce a failed GitHub PR check with an approved local check.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --from <url>             open GitHub pull request URL
  --check <id>             declared or privately approved local check
  --job <name>             select a failed check when the PR has several
  --stdin                  read GitHub pull request metadata from standard input
  --require-review         require an independent review of the candidate
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task ci csv-fix --from https://github.com/example/repo/pull/17 --check test

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task receipt

```text
usage: kitsu task receipt <task> [options]

Print a ticket-linked bug receipt or export saved verification as shareable Markdown.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --json                   return the local ticket-linked bug receipt as JSON
  --markdown               export saved verification without running checks
  --help, -h               show this command's help without executing it

Examples:
  kitsu task receipt export-quoting --markdown
  kitsu task receipt csv-fix --json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task open

```text
usage: kitsu task open <task> --until <outcome> [options]

Pin a candidate and the developer's requested outcome.

Arguments:
  <task>             task name: lowercase words joined by hyphens
  <outcome>          requested finish for the task

Options:
  --request <text>         original developer request
  --entry <stage>          start at plan, implement, review, verify
  --until <outcome>        finish at plan, implement, review, verify, deliver
  --repair                 authorize source repairs after findings or failing
                           assertions
  --commit                 delivery includes a local commit
  --publish-to <remote/branch> authorized remote branch whose commit must be
                               confirmed
  --pr-base <[remote:]branch> require confirmation of an open GitHub PR into
                              this branch of the pushed repository, or of the
                              named remote's
  --review-base <revision> start commit of a read-only review range; requires
                           --review-head
  --review-head <revision> end commit of a read-only review range; requires
                           --review-base
  --require-review         require an independent review of the candidate
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --time-limit <duration>  time remaining before the task pauses
  --review-limit <count>   maximum total independent review starts, including
                           released reservations
  --finish-reserve <duration> time before the deadline reserved from new reviews
  --help, -h               show this command's help without executing it

Examples:
  kitsu task open export-quoting --entry review --until verify --repair --request "Verify CSV quoting"
  kitsu task open review-pr --until review --review-base main --review-head topic --request "Review the committed change"

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task extend

```text
usage: kitsu task extend <task> [options]

Explicitly extend a saved outcome or renew its time limit.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --until <outcome>        finish at plan, implement, review, verify, deliver
  --repair                 authorize source repairs after findings or failing
                           assertions
  --commit                 delivery includes a local commit
  --publish-to <remote/branch> authorized remote branch whose commit must be
                               confirmed
  --pr-base <[remote:]branch> require confirmation of an open GitHub PR into
                              this branch of the pushed repository, or of the
                              named remote's
  --require-review         require an independent review of the candidate
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --time-limit <duration>  time remaining before the task pauses
  --review-limit <count>   maximum total independent review starts, including
                           released reservations
  --finish-reserve <duration> time before the deadline reserved from new reviews
  --help, -h               show this command's help without executing it

Examples:
  kitsu task extend add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task claim

```text
usage: kitsu task claim <task> [options]

Reserve an action before launching a host worker.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --ticket <id>            action ticket returned by task next
  --actor <id>             session reserving this action
  --worker <id>            link the launched native reviewer or developer to an
                           existing reservation
  --host <host>            resolve specialist settings for a review,
                           implementation or repair reservation
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task claim add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task release

```text
usage: kitsu task release <task> [options]

Release a claim after reconciling or cancelling its worker.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --ticket <id>            action ticket returned by task next
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task release add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task confirm

```text
usage: kitsu task confirm <task> [options]

Check every push destination and requested PR, or recheck a completed delivery.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --ticket <id>            action ticket returned by task next
  --pr <url>               published pull request URL
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task confirm export-quoting --ticket TICKET --pr https://github.com/example/repo/pull/17
  kitsu task confirm export-quoting --brief

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task commit

```text
usage: kitsu task commit <task> [options]

Commit a verified staged candidate or finish a failed PR check repair.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --ticket <id>            action ticket returned by task next
  --message <text>         commit message
  --include <path>         explicitly include a pre-existing changed path;
                           repeatable
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task commit add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task next

```text
usage: kitsu task next <task> [options]

Resume from current evidence and show the next action.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task next add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task record

```text
usage: kitsu task record <task> <action> [options]

Record intake, a plan, implementation, repair or review.

Arguments:
  <task>             task name: lowercase words joined by hyphens
  <action>           next judgment action returned by task next

Options:
  --ticket <id>            action ticket returned by task next
  --claim-review <host>    reserve the next independent review with this result
  --actor <id>             native session identity
  --note <text>            result and evidence or artifact reference
  --finding <text>         unresolved review finding; repeatable
  --feature-brief <file>   save a feature brief at intake or planning; use - for
                           stdin
  --observed-review        use the matching native review result captured by the
                           host hooks
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task record export-quoting intake --actor SESSION --ticket TICKET --note "Quoted fields should round-trip"
  kitsu task record export-quoting review --ticket TICKET --observed-review --brief

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task test

```text
usage: kitsu task test <task> [options] [-- <program> [<argument>...]]

Execute the requested behavioral assertion and required repository checks.

Arguments:
  <task>             task name: lowercase words joined by hyphens
  <program>          executable run directly without a shell
  <argument>         argument passed unchanged to the check program

Options:
  --ticket <id>            action ticket returned by task next
  --expect <text>          observable behavior asserted by this command
  --retry                  use the saved behavioral assertion on the current
                           candidate
  --replace-reason <text>  explain why a failing assertion needs a different
                           command
  --brief                  return next action, ticket and evidence references as
                           JSON
  --json                   return structured task state
  --help, -h               show this command's help without executing it

Examples:
  kitsu task test export-quoting --ticket TICKET --expect "Quoted fields round-trip" -- go test ./csv

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task run

```text
usage: kitsu task run <request> | --resume <task> [options]

Run or resume the repository's configured role workflow.

Arguments:
  <request>          one complete developer request
  <task>             task name: lowercase words joined by hyphens

Options:
  --id <task>              save a new task under this ID
  --resume <task>          resume the saved role assignments and evidence
  --until <outcome>        finish at plan, implement, review, verify or deliver
  --entry <stage>          start at plan, implement, tests, review or verify
  --repair                 authorize source repairs on an existing candidate
  --commit                 deliver one verified local commit
  --include <path>         include a pre-existing change in the authorized
                           commit
  --expect <text>          require a reviewed assertion with -- PROGRAM ARGS
  --require-review         require an independent review of the candidate
  --retry                  retry the current stage after resolving a reported
                           problem
  --release <attempt>      reconcile an outstanding attempt with the runner
                           operator’s release authorization
  --rework-limit <count>   maximum automatic test and code rework starts
  --json                   return a bounded task status
  --time-limit <duration>  time remaining before the task pauses
  --review-limit <count>   maximum total independent review starts, including
                           released reservations
  --finish-reserve <duration> time before the deadline reserved from new reviews
  --help, -h               show this command's help without executing it

Examples:
  kitsu task run "Fix CSV quoting" --until verify
  kitsu task run --resume export-quoting

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task brief

```text
usage: kitsu task brief <task> [options]

Prepare a fresh worker brief and record skill selection before its baseline.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --host <codex|claude>    host if the repository does not configure this role
  --role <role>            required specialist role
  --stage <stage>          required explicit worker stage
  --round <number>         required positive worker round
  --workflow <full|small>  full requires a task folder; small cannot infer saved
                           workflow state
  --assignment <file|->    UTF-8 assignment file or standard input; required
                           with --save
  --save                   save complete instructions once and return only
                           dispatch metadata; requires --assignment
  --max-bytes <bytes>      maximum JSON response bytes including metadata;
                           default 16384, range 512 to 1048576
  --json                   print JSON; collection, show and brief always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu task brief fix-login --role developer --stage code --round 1 --host codex --workflow small --assignment assignment.txt --save

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task prepare

```text
usage: kitsu task prepare <task> [options]

Save a worker assignment and start one private write-check interval.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --host <codex|claude>    host if the repository does not configure this role
  --role <role>            required specialist role
  --stage <stage>          required explicit stage
  --round <number>         required positive worker round
  --workflow <full|small>  default full requires a task folder; small needs
                           explicit stage and round
  --assignment <file|->    required UTF-8 task contract and role work; packet
                           supplies role/policy; - reads standard input
  --dispatch               emit version 2 with a generated fresh-worker native
                           prompt and host settings
  --view <full|launch>     default full; launch requires --dispatch and retains
                           native launch fields with a full response reference
  --max-bytes <bytes>      maximum response bytes; default 16384, range 512 to
                           1048576
  --json                   print JSON; these explicit compact helpers always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu task prepare fix-login --role developer --stage code --round 1 --host codex --workflow small --assignment assignment.txt

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task dispatch

```text
usage: kitsu task dispatch <dispatch-id> [options]

Read the original native dispatch.

Arguments:
  <dispatch-id>      immutable private interval id returned by task prepare in this worktree

Options:
  --max-bytes <bytes>      maximum response bytes; default 16384, range 512 to
                           1048576
  --json                   print JSON; these explicit compact helpers always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu task dispatch <dispatch-id>

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task return

```text
usage: kitsu task return <dispatch-id> [options]

Bind bounded untrusted worker claims to the active interval without finishing it.

Arguments:
  <dispatch-id>      private interval id returned by task prepare in this worktree

Options:
  --request <file|->       required version 1 JSON handback, at most 65536
                           bytes; optional claims may be omitted; - reads
                           standard input
  --max-bytes <bytes>      maximum response bytes; default 16384, range 512 to
                           1048576
  --json                   print JSON; these explicit compact helpers always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu task return <dispatch-id> --request -

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu task finish

```text
usage: kitsu task finish <dispatch-id> [options]

Validate a prepared role's writes and record current checks without rerunning them.

Arguments:
  <dispatch-id>      private interval id returned by task prepare in this worktree

Options:
  --expect-checks <pass|fail|any> default any records the actual verdict; pass
                                  requires current passes; fail requires current
                                  failures
  --abandon                record abandonment and release the interval; gives no
                           write or quality approval
  --reason <text>          required explanation with --abandon; cannot combine
                           abandonment with --expect-checks
  --max-bytes <bytes>      maximum response bytes; default 16384, range 512 to
                           1048576
  --json                   print JSON; these explicit compact helpers always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu task finish <dispatch-id> --expect-checks pass
  kitsu task finish <dispatch-id> --abandon --reason 'Role settings changed; prepare a fresh worker'

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## context

### kitsu context read

```text
usage: kitsu context read <path> [options]

Read bounded original source without writing a JSON request.

Arguments:
  <path>             repository-relative source file; use -- before a path beginning with -

Options:
  --start <line>           first inclusive line; omitted starts at 1
  --end <line>             last inclusive line; omitted reads through the end
  --task <task>            include the saved task state
  --max-bytes <bytes>      maximum JSON response bytes including metadata;
                           default 16384, range 512 to 1048576
  --json                   print JSON; collection, show and brief always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu context read internal/server.go --start 10 --end 60

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu context search

```text
usage: kitsu context search <pattern> [<path>] [options]

Search eligible source literally, or with an explicit Go regular expression.

Arguments:
  <pattern>          literal text by default; use -- before a pattern beginning with -
  <path>             optional repository-relative file or directory; omitted searches all eligible sources

Options:
  --regex                  interpret the pattern as a Go regular expression
  --task <task>            include the saved task state
  --max-bytes <bytes>      maximum JSON response bytes including metadata;
                           default 16384, range 512 to 1048576
  --json                   print JSON; collection, show and brief always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu context search register internal
  kitsu context search -- -flag

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu context collect

```text
usage: kitsu context collect --request <file|-> [options]

Batch repository reads, searches and Git queries in the built-in Go engine.

Options:
  --request <file|->       required version 1 JSON request; - reads standard
                           input
  --task <task>            include the saved task state
  --max-bytes <bytes>      maximum JSON response bytes including metadata;
                           default 16384, range 512 to 1048576
  --json                   print JSON; collection, show and brief always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu context collect --request context.json --json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu context show

```text
usage: kitsu context show <artifact-id> [options]

Inspect retained evidence without rerunning operations or checks.

Arguments:
  <artifact-id>      private artifact id returned by collect, brief or compact checks

Options:
  --item <item-id>         show one named operation or check log
  --cursor <cursor>        next_cursor from this artifact and item
  --max-bytes <bytes>      maximum JSON response bytes including metadata;
                           default 16384, range 512 to 1048576
  --json                   print JSON; collection, show and brief always use
                           JSON
  --help, -h               show this command's help without executing it

Examples:
  kitsu context show <artifact-id> --item source --json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## role

### kitsu role list

```text
usage: kitsu role list [options]

List role models, reasoning, skills and adviser access.

Options:
  --host <codex|claude>    override the saved CLI host preference
  --task <task>            task whose saved records or adviser access to use
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role list

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role show

```text
usage: kitsu role show <role> [options]

Show one role's effective settings and write limits.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --task <task>            task whose saved records or adviser access to use
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role show developer

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role init

```text
usage: kitsu role init [options]

Create repository role assignments from your installed host or --host.

Options:
  --host <codex|claude>    override the saved CLI host preference
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role init

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role set

```text
usage: kitsu role set <role> [options]

Change only the supplied role settings.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --model <model>          set the exact host model id; availability depends on
                           your account
  --reasoning <effort>     set reasoning effort; omitted fields stay unchanged
  --skills <references>    replace skill selections; an empty value suppresses
                           inheritance
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role set architect --host codex --model gpt-6.1-sol --reasoning max

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role edit

```text
usage: kitsu role edit <role> [options]

Interactively edit a role with a preview before saving.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --accessible             use descriptive accessible prompts instead of the
                           full-screen form
  --help, -h               show this command's help without executing it

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Editor: F1 opens field help; Save applies the preview; Cancel writes nothing.
Ctrl+C exits 130 without saving. Requires a terminal; scripts use set with --json.

Examples:
  kitsu role edit developer

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role reset

```text
usage: kitsu role reset <role> [options]

Remove role overrides in this scope; restore model defaults and inherited skills.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role reset developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role check

```text
usage: kitsu role check <role> --from <tree> [options]

Check that a role stayed within its write limits.

Arguments:
  <role>             specialist role id, or coordinator where supported
  <tree>             starting Git tree or revision

Options:
  --from <tree>            required starting Git tree
  --task <task>            task whose saved records or adviser access to use
  --finalize               permit spec-writer finalization for the named task
  --help, -h               show this command's help without executing it

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role check reviewer --from HEAD

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu role text

```text
usage: kitsu role text <role>

Print a role's instructions, including coordinator.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --help, -h               show this command's help without executing it

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu role text developer

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## skill

### kitsu skill list

```text
usage: kitsu skill list [options]

Discover original host skills without executing them.

Options:
  --host <codex|claude>    override the saved CLI host preference
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill list --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill show

```text
usage: kitsu skill show <skill> [options]

Inspect skill metadata, including unavailable sources.

Arguments:
  <skill>            catalog id, unique skill name, or original absolute path

Options:
  --host <codex|claude>    override the saved CLI host preference
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill show project-review --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill resolve

```text
usage: kitsu skill resolve <role> [options]

Resolve a role's current original sources; loading remains unverified.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --task <task>            task whose saved records or adviser access to use
  --workflow <full|small>  select full or small task history
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill resolve developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill attach

```text
usage: kitsu skill attach <skill> --role <role> [options]

Add a skill without replacing existing selections.

Arguments:
  <skill>            catalog id, unique skill name, or original absolute path
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --role <role>            role requesting advice or receiving skill selections
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill attach project-review --role developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill detach

```text
usage: kitsu skill detach <skill> --role <role> [options]

Remove a selected skill, even when its original source is gone.

Arguments:
  <skill>            catalog id, unique skill name, or original absolute path
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --role <role>            role requesting advice or receiving skill selections
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill detach project-review --role developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill clear

```text
usage: kitsu skill clear  --role <role> [options]

Save an empty skill list, suppressing inherited personal choices.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --role <role>            role requesting advice or receiving skill selections
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill clear --role developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu skill reset

```text
usage: kitsu skill reset  --role <role> [options]

Remove only the skill override and restore inheritance.

Arguments:
  <role>             specialist role id, or coordinator where supported

Options:
  --host <codex|claude>    override the saved CLI host preference
  --role <role>            role requesting advice or receiving skill selections
  --root <directory>       include an original skill directory or collection;
                           repeatable
  --global                 edit personal settings instead of repository
                           settings; role settings permit skills only
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Host: --host > KITSU_HOST > private checkout preference > personal preference.
Set a preference with `kitsu config set host codex` (or add --global).

Role settings: repository skills replace personal lists; clear pins none; reset
restores inheritance. Personal settings and coordinator accept only skills.
Selections grant no extra permissions and do not prove host loading.

Examples:
  kitsu skill reset --role developer --host codex

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## adviser

### kitsu adviser list

```text
usage: kitsu adviser list [options]

List configured experimental advisers or a task's saved binding.

Options:
  --role <role>            role requesting advice or receiving skill selections
  --task <task>            task whose saved records or adviser access to use
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser list

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser show

```text
usage: kitsu adviser show <adviser> [options]

Inspect one configured or task-bound experimental adviser.

Arguments:
  <adviser>          name of an explicitly configured experimental adviser

Options:
  --task <task>            task whose saved records or adviser access to use
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser show design

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser story-review enable

```text
usage: kitsu adviser story-review enable --adviser NAME [options]

Approve automatic Jev review for new feature stories in this checkout.

Options:
  --adviser <name>         configured TypeSafe Jev adviser
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser story-review enable

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser story-review disable

```text
usage: kitsu adviser story-review disable

Disable automatic feature story review in this checkout.

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser story-review disable

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser providers

```text
usage: kitsu adviser providers [options]

List experimental provider capabilities and required credential variables offline.

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser providers

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser bind

```text
usage: kitsu adviser bind <task> --workflow <workflow> [options]

Save the explicitly enabled adviser policy for a task.

Arguments:
  <task>             task name: lowercase words joined by hyphens
  <workflow>         full for an existing task folder, or small for lightweight history

Options:
  --workflow <full|small>  select full or small task history
  --refresh                explicitly adopt policy changes or promote a small
                           history
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser bind add-login --workflow full

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser ask

```text
usage: kitsu adviser ask <adviser> --task <task> --role <role> --stage <stage> --request <file> [options]

Consult an explicitly enabled experimental adviser and record its answer.

Arguments:
  <adviser>          name of an explicitly configured experimental adviser
  <task>             task name: lowercase words joined by hyphens
  <role>             specialist role id, or coordinator where supported
  <stage>            stage of the role making this request
  <file>             JSON request file, or - to read standard input

Options:
  --task <task>            task whose saved records or adviser access to use
  --role <role>            role requesting advice or receiving skill selections
  --stage <stage>          required workflow stage of this request
  --request <file|->       required version 1 or 2 request JSON; use - for
                           standard input
  --checkpoint <id>        required checkpoint this request addresses
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Request: CLI advisers accept version 1 advice/decision JSON; native advisers
accept decision, score and boolean requests (version 2 for score/boolean).
The call may execute a configured CLI or contact an enabled experimental API.
The complete exchange is recorded. Answers provide no automatic approval.

Examples:
  kitsu adviser ask design --task add-login --role architect --stage design --request request.json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser status

```text
usage: kitsu adviser status <task> [options]

Inspect status readiness of saved experimental adviser checkpoints.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --checkpoint <id>        consider only this checkpoint
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser status add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser check

```text
usage: kitsu adviser check <task> [options]

Inspect check readiness of saved experimental adviser checkpoints.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --checkpoint <id>        consider only this checkpoint
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser check add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser accept

```text
usage: kitsu adviser accept <id> --task <task> --reason <text> [options]

Record an explicit accept disposition without replacing reviews or approvals.

Arguments:
  <id>               check or consultation id, as appropriate
  <task>             task name: lowercase words joined by hyphens
  <text>             explicit explanation of this recorded decision

Options:
  --task <task>            task whose saved records or adviser access to use
  --reason <text>          required explanation; keep the original answer
  --choice <id>            selected choice from a decision request
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser accept <consultation-id> --task add-login --reason 'Reviewed the answer'

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser reject

```text
usage: kitsu adviser reject <id> --task <task> --reason <text> [options]

Record an explicit reject disposition without replacing reviews or approvals.

Arguments:
  <id>               check or consultation id, as appropriate
  <task>             task name: lowercase words joined by hyphens
  <text>             explicit explanation of this recorded decision

Options:
  --task <task>            task whose saved records or adviser access to use
  --reason <text>          required explanation; keep the original answer
  --choice <id>            selected choice from a decision request
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser reject <consultation-id> --task add-login --reason 'Reviewed the answer'

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu adviser defer

```text
usage: kitsu adviser defer <id> --task <task> --reason <text> [options]

Record an explicit defer disposition without replacing reviews or approvals.

Arguments:
  <id>               check or consultation id, as appropriate
  <task>             task name: lowercase words joined by hyphens
  <text>             explicit explanation of this recorded decision

Options:
  --task <task>            task whose saved records or adviser access to use
  --reason <text>          required explanation; keep the original answer
  --choice <id>            selected choice from a decision request
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu adviser defer <consultation-id> --task add-login --reason 'Reviewed the answer'

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## check

### kitsu check list

```text
usage: kitsu check list [options]

List this repository's declared checks.

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu check list

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu check trust

```text
usage: kitsu check trust [--verify-order before-review|review-first] [--ci ID [options] -- PROGRAM ARGS...]

Approve checks, verification order, or a private local CI reproduction.

Options:
  --ci <id>                approve a named local reproduction command
  --verify-order <order>   approve or revoke early verification in this checkout
  --help, -h               show this command's help without executing it

Examples:
  kitsu check trust

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu check add

```text
usage: kitsu check add <id> [options] -- <program> [<argument>...]

Declare a repository check without invoking a shell.

Arguments:
  <id>               check or consultation id, as appropriate
  <program>          executable run directly without a shell
  <argument>         argument passed unchanged to the check program

Options:
  --paths <pattern>        dependent paths; repeatable; omitted means all
                           content
  --timeout <duration>     check timeout from 1s to 6h; default 10m
  --when <pattern>         prioritize on changed paths; repeatable; never
                           changes dependencies
  --expect <text>          behavior asserted by the declared command
  --verify                 require applicable behavioral verification; requires
                           --expect
  --help, -h               show this command's help without executing it

Examples:
  kitsu check add test -- go test ./...
  kitsu check add readme --when README.md --expect "README.md is tracked" --verify -- git ls-files --error-unmatch -- README.md

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu check plan

```text
usage: kitsu check plan [options]

Explain changed-path priorities while retaining every declared gate check.

Options:
  --from <rev>             compare the whole working tree with this revision;
                           default HEAD
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu check plan --from HEAD
  kitsu check plan --from origin/main --json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu check run

```text
usage: kitsu check run [<id>...] [options]

Run declared checks that lack passing evidence for current content.

Arguments:
  <id>               check or consultation id, as appropriate

Options:
  --force                  run even when passing evidence exists
  --base <rev>             reuse evidence only when checked content matches this
                           revision
  --compact                retain logs privately and return bounded diagnostics
  --json                   select compact JSON output automatically
  --max-bytes <bytes>      compact JSON byte limit; default 16384
  --help, -h               show this command's help without executing it

Examples:
  kitsu check run

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu check stats

```text
usage: kitsu check stats [options]

Report recorded check runs and gate decisions.

Options:
  --since <period>         reporting period such as 90m, 7d or 2w; default 30d
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu check stats

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## gate

### kitsu gate init

```text
usage: kitsu gate init [options]

Declare checks when needed and install Git hooks.

Options:
  --observe                record decisions while allowing commits and pushes
  --help, -h               show this command's help without executing it

Examples:
  kitsu gate init

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu gate status

```text
usage: kitsu gate status [options]

Report whether current content has evidence to permit a commit.

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu gate status

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu gate uninstall

```text
usage: kitsu gate uninstall [options]

Detach Kitsu hooks and preserve local history.

Options:
  --purge                  also delete shared check and token evidence after
                           successful detach
  --help, -h               show this command's help without executing it

Removal: records are retained. --purge deletes shared check and token records
only after all Kitsu hook calls are detached. Task history and configuration stay.

Examples:
  kitsu gate uninstall

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## usage

### kitsu usage show

```text
usage: kitsu usage show [options]

Report task tokens and workflow overhead; unknown usage stays unknown.

Options:
  --task <task>            task whose saved records or adviser access to use
  --json                   print machine-readable JSON without prompts or
                           styling
  --diagnostics            inspect captured usage, managed spans, check evidence
                           and optional task notes offline
  --help, -h               show this command's help without executing it

Examples:
  kitsu usage show

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu usage record

```text
usage: kitsu usage record [options]

Record a host token notice from standard input.

Options:
  --host <codex|claude>    required notice format; explicit host avoids protocol
                           guessing
  --help, -h               show this command's help without executing it

Examples:
  kitsu usage record --host codex < notice.json

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu usage budget set

```text
usage: kitsu usage budget set <task> [options]

Record a sourced estimate range or declared limit; authorizes no execution.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --unit <tokens|seconds>  unit of the estimate or declared limit
  --min <number>           estimated lower bound; requires --max and --basis
  --max <number>           estimated upper bound; requires --min and --basis
  --basis <text>           basis for the estimate range
  --assumption <text>      estimate assumption; repeatable
  --limit <number>         declared nonnegative limit; requires --source; no
                           runtime enforcement
  --source <text>          explicit source of the declared limit; not proof of
                           approval
  --workflow <full|small>  match the existing full task or small budget location
  --help, -h               show this command's help without executing it

Examples:
  kitsu usage budget set fix-login --unit tokens --min 100000 --max 300000 --basis 'Earlier captured tasks'
  kitsu usage budget set fix-login --unit tokens --limit 400000 --source 'Owner instruction in task request'

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu usage budget show

```text
usage: kitsu usage budget show <task> [options]

Inspect optional task estimates and declared limits separately from actuals.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu usage budget show add-login

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu usage budget reset

```text
usage: kitsu usage budget reset <task> [options]

Remove only one unit's optional estimate and declared limit.

Arguments:
  <task>             task name: lowercase words joined by hyphens

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --unit <tokens|seconds>  unit of the estimate or declared limit
  --help, -h               show this command's help without executing it

Examples:
  kitsu usage budget reset fix-login --unit tokens

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## config

### kitsu config show

```text
usage: kitsu config show [options]

Manage CLI host preferences: show.

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu config show

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu config get

```text
usage: kitsu config get <key> [options]

Manage CLI host preferences: get.

Arguments:
  <key>              CLI preference key; currently host

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --help, -h               show this command's help without executing it

Examples:
  kitsu config get host

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu config set

```text
usage: kitsu config set <key> <value> [options]

Manage CLI host preferences: set.

Arguments:
  <key>              CLI preference key; currently host
  <value>            preference value; host accepts codex or claude

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --global                 edit personal CLI preferences instead of this private
                           checkout
  --if-unset               with --global, only set a missing personal preference
  --help, -h               show this command's help without executing it

Examples:
  kitsu config set host codex --global

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu config reset

```text
usage: kitsu config reset <key> [options]

Manage CLI host preferences: reset.

Arguments:
  <key>              CLI preference key; currently host

Options:
  --json                   print machine-readable JSON without prompts or
                           styling
  --global                 edit personal CLI preferences instead of this private
                           checkout
  --help, -h               show this command's help without executing it

Examples:
  kitsu config reset host

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu config edit

```text
usage: kitsu config edit [options]

Manage CLI host preferences: edit.

Options:
  --global                 edit personal CLI preferences instead of this private
                           checkout
  --accessible             use descriptive accessible prompts instead of the
                           full-screen form
  --help, -h               show this command's help without executing it

Editor: F1 opens field help; Save applies the preview; Cancel writes nothing.
Ctrl+C exits 130 without saving. Requires a terminal; scripts use set with --json.

Examples:
  kitsu config edit

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## completion

### kitsu completion bash

```text
usage: kitsu completion bash

Print a bash completion script for this CLI.

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu completion bash

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu completion zsh

```text
usage: kitsu completion zsh

Print a zsh completion script for this CLI.

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu completion zsh

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu completion fish

```text
usage: kitsu completion fish

Print a fish completion script for this CLI.

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu completion fish

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

### kitsu completion powershell

```text
usage: kitsu completion powershell

Print a powershell completion script for this CLI.

Options:
  --help, -h               show this command's help without executing it

Examples:
  kitsu completion powershell

Exit statuses: 0 success; 1 operation failed; 2 invalid arguments;
3 gate denial; 4 foreign hook needs attention; 5 not verified.
```

## Compatibility aliases and integrations

Existing spellings retain their original argument rules. Nested help names their preferred replacement.

### kitsu advisers

```text
usage: kitsu advisers providers [--json] | list [--role <role>] [--task <task>] [--json] | bind | resolve | status | check

Configure optional, experimental advisers; none are enabled by default.

  Opt-in                 configure a definition and repository role access; no advisers or checkpoints are enabled by default
  providers              list built-in native presets and credentials; offline, works outside a repository
  list                   print configured advisers; --task selects the saved binding
  bind                   snapshot configuration with --task and --workflow
  resolve <id>           append a disposition to a consultation
  status|check           inspect readiness or require satisfied checkpoints; never invoke models
  --task <task>          task name; required except with providers or list
  --role <role>          with list: show only advisers this requester may consult
  --workflow full|small  with bind: use a change folder or lightweight task record
  --refresh              with bind: explicitly update the saved policy or promote small to full
  --disposition <value>  with resolve: accept, reject or defer
  --reason <text>        with resolve: required explanation of the disposition
  --choice <id>          with resolve: optional selected option from a decision request
  --checkpoint <id>      with status/check: consider this required checkpoint only
  --json                 print one JSON object
```

### kitsu consult

```text
usage: kitsu consult --task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]

Ask an explicitly enabled experimental adviser and record the exchange.

  --task <task>       task with a saved adviser binding
  --role <role>       original requester, including coordinator
  --stage <stage>     workflow stage of this request
  --adviser <name>    allowed adviser whose definition still matches the binding
  --request <file|->  version 1 or 2 JSON file, or standard input; one question
  --checkpoint <id>   required checkpoint this consultation addresses
  --json              print the saved consultation, including its ID and outcome
  Advice JSON         {"version":1,"kind":"advice","question":"What could fail?","context":["src/app.txt"]}
  Decision JSON       set kind to decision and add choices: [{"id":"a","text":"First option"},{"id":"b","text":"Second option"}]
  Score JSON          {"version":2,"kind":"score","question":"How complete is the design?","levels":["Incomplete","Partial","Complete"]}
  Boolean JSON        {"version":2,"kind":"boolean","question":"Does the design handle cancellation?","criteria":{"true":"Explicit cancellation","false":"No cancellation"}}
  Capabilities        CLI: advice, decision. Native: decision, score, boolean. Results never automatically approve work.
```

### kitsu roles

```text
usage: kitsu roles [--host claude|codex] [--task <task>] [--json] | set <role> --host claude|codex [--model <model>] [--reasoning <effort>] [--skills <references>] [--global] [--root <directory>] [--json] | reset <role> --host claude|codex [--global] [--json] | check <role> --from <tree> [--task <task>] [--finalize] | text <role>

Print or configure role models, reasoning and skills, list advisers, check writes, or print instructions.

  --host claude|codex         select a host; required with set and reset
  --json                      print the roles as one JSON object
  set <role>                  save model, reasoning, and/or skill overrides in kitsu-roles.toml
  --model <model>             with set: change only this role's model for the selected host
  --reasoning <effort>        with set: change reasoning effort; omit either option to keep its current setting
  --skills <references>       with set: comma-separated skill IDs or unique names; an empty value clears selections
  --root <directory>          with set and --skills: add an explicit skill root; repeat for more roots
  --global                    save or reset personal skills in the user config directory's kitsu/roles.toml; skills only
  reset <role>                remove role overrides in the selected scope; repository skills then inherit personal choices
  --task <task>               with the table: read bound adviser access; with check: set the task write scope
  check <role> --from <tree>  exit 5 when writes since this tree exceed the role's scope
  --finalize                  with spec-writer and --task: allow current specs for capabilities in --from
  text <role>                 print one role's instructions, or coordinator
```

### kitsu skills

```text
usage: kitsu skills list --host claude|codex [--root <directory>] [--json] | resolve <role> --host claude|codex [--task <task>] [--workflow full|small] [--root <directory>] [--json]

List existing host skills or resolve a role's selection without executing skills.

  list                   print existing host skill metadata and discovery warnings
  resolve <role>         resolve configured skill references for this role, including coordinator
  --host claude|codex    select the native host inventory; required
  --root <directory>     add an explicit skill collection or directory with SKILL.md; repeat for more
  --task <task>          with resolve: record the selection for this task; without it, read only
  --workflow full|small  with resolve and --task: select the receipt location; defaults to full
  --json                 print metadata or resolved sources as one JSON object; loading remains unverified
```

### kitsu change

```text
usage: kitsu change status [<task>] [--json] | check [<task>]

Print the stage of a task's change folder, or check the rules its files must obey.

  status [<task>]  print the stage of one task, or of every task
  --json           with status: print one JSON object instead of text
  check [<task>]   exit 5 when the files of one task, or of any task, break a rule
```

### kitsu tokens

```text
usage: kitsu tokens [--task <task>] [--json] | record [--host claude|codex]

Report the tokens a task used and the share the workflow itself took, or record them from a host's notice.

  --task <task>               print the roles and the stages of one task
  --json                      print one JSON object instead of text
  record --host claude|codex  record the tokens from the host's notice on standard input; without --host, claude
```

### kitsu init

```text
usage: kitsu init [--observe]

Create kitsu.toml if it is missing and install the git hooks.

  --observe  record what the hooks would decide, and allow every commit and push
```

### kitsu add

```text
usage: kitsu add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]

Append one check to kitsu.toml.

  <id>                          the name of the check
  --paths <pattern>             a path pattern the check depends on; give it once for each pattern; without it, every path
  --timeout <duration>          the time the check may take, from 1s to 6h; without it, 10m
  -- <program> [<argument>...]  the program of the check and its arguments, started without a shell
```

### kitsu run

```text
usage: kitsu run [<id>...] [--force] [--base <rev>] [--compact|--json] [--max-bytes <bytes>]

Execute the declared checks that have no passing run on this content.

  <id>...              consider only these checks
  --force              execute even when passing evidence exists
  --base <rev>         count unchanged checked content as passed on this revision
  --compact            retain logs privately and return bounded JSON diagnostics
  --json               select compact JSON output automatically
  --max-bytes <bytes>  compact JSON limit including metadata; default 16384, range 512 to 1048576
```

### kitsu status

```text
usage: kitsu status [--json]

Say whether a commit of the working tree would be allowed, and what is missing.

  --json  print one JSON object instead of text
```

### kitsu stats

```text
usage: kitsu stats [--since <period>] [--json]

Count the runs and the hook decisions this repository recorded.

  --since <period>  count the records of this period, such as 90m, 36h, 7d or 2w; without it, 30d
  --json            print one JSON object instead of text
```

### kitsu context

```text
usage: kitsu context [--host claude|codex]

Print what an agent should know at the start of a session here.

  --host claude|codex       read the host's notice from standard input and judge its directory
  collect --request <file>  batch source reads and searches; run `kitsu context collect --help`
  show <artifact-id>        expand retained evidence; run `kitsu context show --help`
```

### kitsu hook

```text
usage: kitsu hook <name> [<argument>...]

Decide for git: pre-commit, pre-merge-commit or pre-push.

  <name>         pre-commit, pre-merge-commit or pre-push
  <argument>...  what git gives that hook; the hook files that kitsu init writes pass them on
```

### kitsu guard

```text
usage: kitsu guard [--host claude|codex]

For an agent host's hook: enforce check approval and guard local workflow settings.

  --host claude  request permission for guarded edits; this is the default
  --host codex   deny guarded edits and explain why
```

### kitsu uninstall

```text
usage: kitsu uninstall [--purge]

Detach owned hooks; optionally purge the shared check and token store.

  --purge  Delete shared check and token evidence after complete hook removal.
```

### kitsu version

```text
usage: kitsu version

Print the version, the revision and the build time.
```
