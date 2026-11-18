# Grade decisions from a developer’s request

This offline replay starts from seven small Git workspaces and five [developer requests](harness-request-decisions-data/requests). The same review request gets a faulty and a clean commit. The same delivery request gets matching saved executable evidence in one workspace and a “tests passed” note in the other. The [private grader](harness_request_grade.py) evaluates the requested outcome against the observed commands, retained evidence and Git state.

## Run the controls

From the repository root, use Python 3.10 or newer, Git, the repository’s Go toolchain, and Node.js or Bun on `PATH`.

```sh
scratch=$(mktemp -d /var/tmp/kitsu-request-decisions.XXXXXX)
TMPDIR=/var/tmp go build -o "$scratch/kitsu" ./cmd/kitsu
TMPDIR=/var/tmp python3 -B docs/benchmarks/harness-request-decisions.py replay \
  --kitsu "$scratch/kitsu" --evidence "$scratch/evidence.json"
python3 -B docs/benchmarks/harness-request-decisions.py grade "$scratch/evidence.json"
```

The replay and regrade each exit 0 when every verdict matches its control. A preview that lacks the named review flags exits 2 before running the controls. The JSON evidence records the CLI digest, each original request, start and finish commit and tree, index and file hashes, commands and exits, review inputs, ledger history, check records and log digests. The `grade` command also accepts a single normalized `evidence` object from the bundle and exits 1 with its violations when the submitted outcome fails.

| Outcome | Accepted controls | Rejected controls |
| --- | --- | --- |
| Diagnosis | Reproduce `double("3")` returning 6, or report the missing CI input and distinguish the developer’s reported TypeError from an observation. Both reports remain recoverable from a read-only task. | A correct reproduction with no saved diagnosis, and an edit staged and restored during diagnosis. |
| Finish a partial fix | Inspect the supplied diff and reproduce the failure before editing. Direct implementation and a route with two review rounds both end on a verified fix. The review route observes exit 18 on the original and inadequate repairs and exit 0 on the fixed candidate. | A partial repair claimed as complete, a fix made before inspecting the supplied patch, and a verified fix that overwrites unrelated developer notes. |
| Review a committed change | Execute the reserved named range’s `review_log` and `review_diff`, compare the resolved refs with `git diff`, or read the commit’s patch and parent with `git show` and `git rev-list`. Locate and reproduce the faulty behavior or return a clean result on the good commit. Git state and file bytes stay intact. | Inspecting only the empty working diff, using a different comparison after receiving a named range, and returning a false finding on the clean commit. |
| Commit a verified fix | Recover the starting proof and commit once, or execute a candidate-specific assertion and the declared suite before committing when starting from the note. The original parent, exact candidate tree and scratch files are checked. | A commit before proof, a passing positive-number suite posing as behavioral proof, asking for approval already given, and a second commit. |

The offline run matched 27 of 27 controls, including eleven accepted and sixteen rejected. The controls also accept equivalent diagnosis wording and reject unfinished attempts at each outcome. The saved reviews use scripted actor IDs. Repair and review use independently executed public behavior checks. The replay makes zero model calls. Autonomous routing from the raw requests in a native host remains open.

## Verify-only authority and delegated repair on the same CSV patch

Use the CSV replay to compare plain Git/Python with the installed Kitsu daily CLI. Both arms use a scripted host, the same raw requests, starting bytes and public behavior check. On a delegated task, a fresh coordinator process must recover the scripted worker after losing the first launch response. Keep the evidence file outside the solver checkout.

```sh
python3 -B docs/benchmarks/harness-request-decisions.py replay-csv --kitsu "$scratch/kitsu" --pairs 2 --evidence "$scratch/csv-evidence.json" > "$scratch/csv-summary.json"
python3 -B docs/benchmarks/harness-request-decisions.py grade "$scratch/csv-evidence.json"
```

The six scenarios cover conditional commits on passing and failing patches, delegated completion on both, preservation of an unrelated staged file, and an explicit request to check the staged CSV when the working copy differs. The grader checks Git, the retained assertion and the submitted CSV behavior with a separate standard library reader. Controls include a different valid implementation and a repair that only handles the public sample. The measured time includes scripted host starts, recovery and observation commands. Fixture preparation and private grading run outside the timer. [Per-run outcomes](daily-review-data/csv-task-permissions.json) · [Driver](harness_request_csv.py) · [Grader](harness_csv_grade.py)

## Prepare a host input

```sh
TMPDIR=/var/tmp python3 -B docs/benchmarks/harness-request-decisions.py prepare \
  partial-repair "$scratch/solver" --kitsu "$scratch/kitsu" \
  --starting-evidence "$scratch/starting-evidence.json"
```

Give the host `solver/request.txt` and `solver/repo/`. The checkout includes the Git history and any task records belonging to that starting state. Keep the optional starting evidence, the replay recipes and the grader outside the host’s inputs. Use `prepare --help` for the other starting states.

To grade observed host work, collect its tool calls and results, checkpoint mutations, preserve ledger and logs, and independently run the public behavior against the submitted candidate. The replay bundle shows the normalized evidence consumed by `grade`. Native host transcript collection and normalization are separate from this offline replay.

### Changing inputs during diagnosis

From the repository root, run the command below with a local Kitsu binary and Node.js or Bun on PATH. Add `--previous /path/to/earlier/kitsu` to compare a second binary.

```sh
python3 docs/benchmarks/harness-request-decisions.py replay-plan --kitsu /path/to/kitsu
```

The fixture keeps separate staged and unstaged notes. A scripted actor reproduces the original defect. The unchanged case can finish its diagnosis. In the changed case the supplied implementation is corrected before the actor resumes. Plain shell reruns the behavior check. Kitsu uses the task's opening snapshot to decide whether it can accept the original diagnosis. The grader reruns the behavior and compares Git state and file contents in every checkout. [Recorded results](daily-review-data/plan-inputs.json)
