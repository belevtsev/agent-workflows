# Quick start

Ten minutes, one task, by hand: you run each `cw` command that the agent runs
for you in daily use, from a starter plan to a verified commit. You need macOS
or Linux, git, Go 1.24 or newer, and `shasum`.

The test `cmd/cw/quickstart_e2e_test.go` runs every block of this page tagged
`sh quickstart`, in order and in one shell, so the page stays true.

## 1. Install `cw`

Take the release archive for your system from the
[release page](https://github.com/belevtsev/agent-workflows/releases)
([steps](operations.md#get-cw)), or build it with Go:

```sh
go install github.com/belevtsev/agent-workflows/v3/cmd/cw@latest
```

The plugin is not needed for this page; [README](../README.md#quick-start)
installs it.

## 2. Make a Go module

A small module with one function and its test, committed:

```sh quickstart
mkdir -p ~/cw-quickstart/greet
cd ~/cw-quickstart/greet
git init -q
cat > go.mod <<'EOF'
module example.com/greet

go 1.24
EOF
cat > greet.go <<'EOF'
// Package greet says hello.
package greet

// Hello greets name.
func Hello(name string) string { return "Hello, " + name }
EOF
cat > greet_test.go <<'EOF'
package greet

import "testing"

func TestHello(t *testing.T) {
	if got := Hello("Ada"); got != "Hello, Ada" {
		t.Fatalf("Hello = %q", got)
	}
}
EOF
git add go.mod greet.go greet_test.go
git commit -q -m "Add greet"
```

## 3. Turn the commit gate on

`cw gate install` writes git's `pre-commit`, `pre-push` and `pre-merge-commit`
hooks. While a task is open, a commit goes through only once `cw task verify`
passes on the source it records ([git hooks](hooks.md#git-hooks)).

```sh quickstart
cw gate install
```

## 4. Write the plan and start the task

`cw task init` writes `plan.json`: the `lite` profile, one requirement and one
check, which in a Go module is the Go pack's race command
([starter plan](operations.md#starter-plan)). `cw task start` records it with
the current source as the baseline and prints the task's ID; keep it in `TASK`.

```sh quickstart
cw task init --title "Say goodbye too"
cw task start --plan plan.json | tee ../start.json
TASK=$(sed -n 's/.*"task_id":"\([0-9a-f]*\)".*/\1/p' ../start.json)
```

The task record lives outside the repository, under
`~/.local/state/agent-workflows/workflow-tasks/$TASK`.

## 5. Change the code

```sh quickstart
cat >> greet.go <<'EOF'

// Goodbye says goodbye to name.
func Goodbye(name string) string { return "Goodbye, " + name }
EOF
```

## 6. Run the check

`cw task run` runs the plan's check and records its result, so a worker's word
is never a pass:

```sh quickstart
cw task run --task "$TASK" --check race
```

## 7. Review

A changed task needs an accepted independent review of the whole source. Under
`lite` that is the `code-review` stage. First `cw task bundle` writes the
reviewer bundle, with one reach row per changed file; fill each row's four
facets. Here `sed` writes the same reason into each:

```sh quickstart
cw task bundle --task "$TASK" --review-id review-1 --output ../bundle-empty.json
sed -e 's|"callers": ""|"callers": "n/a: a library file of this module"|' \
    -e 's|"invocation_modes": ""|"invocation_modes": "n/a: go test only"|' \
    -e 's|"platforms": ""|"platforms": "n/a: portable Go"|' \
    -e 's|"consumers": ""|"consumers": "n/a: no state or output"|' \
    ../bundle-empty.json > ../bundle.json
```

Then assign the review, naming the filled bundle by path and SHA-256:

```sh quickstart
cat > ../review-brief.json <<EOF
{"id": "review-1", "agent_id": "/root/reviewer", "role": "review", "stage": "code-review",
 "goal": "Review the change against the requirements", "owned_paths": ["."],
 "constraints": [], "check_ids": [],
 "stop_condition": "Return findings with source evidence and what was not reviewed",
 "bundle": {"path": "$HOME/cw-quickstart/bundle.json",
            "sha256": "$(shasum -a 256 ../bundle.json | cut -d ' ' -f 1)"}}
EOF
cw task assign --task "$TASK" --input ../review-brief.json
```

In daily use a reviewer agent now reads the change and returns its result. Here,
save this result as `~/cw-quickstart/review-result.json`: the reviewer read
`greet.go`, ran it the one way its reach row names and found nothing.

```json
{"assignment_id": "review-1", "status": "completed",
 "summary": "Goodbye mirrors Hello and the race check covers the package. No findings.",
 "evidence": [{"path": "greet.go", "start_line": 7, "end_line": 8}],
 "changed_files": [], "check_ids": [], "findings": [], "uncertainties": [],
 "reach_runs": [{"path": "greet.go", "mode": "go test", "status": "ran",
                 "command": "go test ./...", "observed": "ok"}]}
```

`cw task handoff` checks the result against its assignment and the current
source; `cw task accept` records your decision on it:

```sh quickstart
cw task handoff --task "$TASK" --input ../review-result.json
cat > ../accept.json <<'EOF'
{"assignment_id": "review-1", "status": "accepted",
 "reason": "The review read the change and found nothing.",
 "evidence": [{"path": "greet.go", "start_line": 7, "end_line": 8}],
 "check_ids": ["race"], "findings": []}
EOF
cw task accept --task "$TASK" --input ../accept.json
```

## 8. Assess and verify

`cw task assess` records that the requirement is met, with the source lines and
the check that prove it. `cw task verify` is the gate: it exits 0 only when
every requirement, check and review holds at the current source.

```sh quickstart
cat > ../assess.json <<'EOF'
{"requirements": [{"id": "r1", "status": "satisfied",
   "evidence": [{"path": "greet.go", "start_line": 7, "end_line": 8}], "check_ids": ["race"]}],
 "decision": {"status": "resolved", "reason": "Goodbye is added and the race check passed.",
   "evidence": [{"path": "greet.go", "start_line": 7, "end_line": 8}], "check_ids": ["race"]}}
EOF
cw task assess --task "$TASK" --input ../assess.json
cw task verify --task "$TASK"
```

Verify prints `"complete":true`, its limitations (no advisers, usage not
sampled) and the task's receipt.

## 9. Commit

The gate's `pre-commit` hook lets the commit through because verify passed on
these bytes:

```sh quickstart
git add greet.go
git commit -q -m "Add Goodbye"
git log -1 --oneline
```

Change `greet.go` again and try to commit: the hook stops the commit until
verify passes on the new source.

## Next

- In daily use you ask the agent for the change and it runs these commands:
  [Your first task](../README.md#your-first-task).
- At 80% of the profile's provisional budget (15 USD for `lite`) a task stops
  taking new work and asks you ([budget stop](operations.md#budget-stop)).
- What each limitation means: [the verify gate](../README.md#the-verify-gate).
