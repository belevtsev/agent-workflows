# Register a trusted workflow runner

Supervised tasks use a service operated by your team to run Codex, Claude Code,
and repository checks in disposable environments. The service needs its own host
setup, approved check policy, and native acceptance evidence. The CLI and
plugins install independently.

## 1. Prepare the execution profile

- Provision a fresh job for each assignment. Materialize only the file bundle
  Kitsu supplies. Bind worker inputs and executable paths to the assigned
  profile. Keep the live worktree, Git directory, user configuration, signing
  key, and other jobs outside the job.
- Start the assigned coding host with an operator-owned configuration. For
  Claude Code, load user-only settings from the isolated profile. Load selected
  skills by their registered ID and SHA-256. Give architects and reviewers a
  read-only candidate. Permit changes only in write roles and report the full
  returned file delta.
- Run checks in a separate fresh checkout of the supplied bundle. Use reviewed
  executables and a minimal environment. Keep verification jobs offline. Make
  dependencies available in the profile. Enable only the connections workers
  need to reach their coding host.
- Supervise the whole job independently of the CLI. Use a renewable lease and
  the job deadline. On cancellation, request graceful shutdown and then tear
  down the entire job. Preserve the result by attempt ID. Report the job as
  stopped only after its descendants have exited.
- Validate the setup on Linux, macOS, and Windows. Exercise baseline and final
  verification, blocked network access and host files, fresh reviewer
  settings, cancellation, job teardown after launcher loss, and result replay.
  Test the signed-in Codex and Claude Code profiles before offering them to
  developers.

## 2. Review the project policy

The policy names the opening `kitsu.toml` SHA-256, or `undeclared` when the
repository has no declaration. Use explicit source and test paths in the
repository's own conventions. For example, a project with `src/` and
`checks/` can register this policy.

```json
{
  "declaration": "undeclared",
  "code_paths": ["src/**"],
  "test_paths": ["checks/**"],
  "checks": [
    {
      "id": "csv-case",
      "argv": ["./scripts/check-csv"],
      "inputs": {"scripts/check-csv": "<64-digit SHA-256>"},
      "test_id": "csv-quoting"
    }
  ]
}
```

Include every repository check by its declared ID and exact argv. List
repository executable inputs and their hashes. The profile supplies trusted
tools and interpreters. The reviewed reporter distinguishes a named failing test
from a build, syntax, or command error. The intake assertion matches an
approved argv exactly. A worker assertion can add evidence from another
reviewed check.

## 3. Register the worktree

Use `/etc/kitsu/workflow-runners.json` on Linux and macOS. Its location and
file must be controlled by the administrator. On Windows, save the same JSON
as the `WorkflowRunners` string value under `HKLM\SOFTWARE\Kitsu` with a
protected registry ACL. Use the absolute path of each checkout authorized to
send candidate source files to the service.

```json
[
  {
    "worktree": "/absolute/path/to/worktree",
    "project": "csv-export",
    "endpoint": "https://runner.example.org",
    "receipt_key": "<base64 Ed25519 public key>",
    "root_ca": "<optional PEM certificate>",
    "client_certificate": "<PEM client certificate>",
    "client_key": "<PEM client private key>"
  }
]
```

Restrict registration reads to the developers who can use the runner.
Authenticate the CLI with mutual TLS or an equivalent deployment boundary.
Keep service credentials and operator release authority outside the worker
environment. Rotate a profile by registering the new policy and opening a
new task.

## Protocol version 1

The [protocol definitions](../internal/workflowrunner/protocol.go) describe
the JSON body for `POST /v1/workflow`.

| Action | Runner contract |
| --- | --- |
| `probe` | Return signed `ready` capabilities with platform, profile, reviewed policy, and native acceptance platforms. The platform is Go’s `GOOS/GOARCH`. |
| `start` | Persist the task’s intake binding and reservation before execution. Require the pinned profile, policy and check inputs. Reject a changed binding or job for an existing attempt. Start one disposable job and retain its terminal result. |
| `status` | Return the current state or the retained result for the same attempt and binding. Renew a live job’s lease. |
| `cancel` | Stop the whole job and report its observed state. The operator can authorize release of a settled attempt independently. |

Sign the serialized receipt bytes with Ed25519, then base64-encode the bytes
and signature in the response envelope. Include the SHA-256 of the exact
request bytes, including its nonce. A started job’s receipt contains the
attempt, binding, and SHA-256 of the job JSON as encoded by Kitsu. Reports use
`running`, `returned`, `stopped`, or `failed`. Only a settled terminal job has
`stopped: true`. A worker result names the host, role, actual model and
reasoning effort, and fresh session. A reviewer identifies the
`kitsu-reviewer` profile. Check results name every requested check in order
with `passed`, `test_failed`, `failed`, or `error` and the observed status.

The current CLI accepts portable regular files, up to 8192 files and 16 MiB
of source bytes per job. It excludes repository agent settings and ignored
files outside the accepted Git candidate. A snapshot containing a symlink or
submodule requires a different checkout before a configured run. Exact
staged content is checked for local delivery. Only those staged check runs enter
the Git gate's evidence store. The runner signs the results
it actually observes. A passing check verifies the behaviors that the
reviewed assertions exercise.

[Start or resume a task](configured-workflow.md)
