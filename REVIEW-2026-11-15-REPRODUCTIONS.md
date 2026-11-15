# Agent Workflows review reproductions

These procedures accompany the [review report](REVIEW-2026-11-15.md) for revision `c557832010a1eebc3d9cd3bf9f04941855da031f`, reviewed on 15 November 2026.

They require Git, Go, and Python 3. F1 requires a Unix-like filesystem supporting executable bits and symlinks. Each procedure builds the checked-out source and writes only to a new temporary directory. It commits fixture files in those temporary repositories, uses a fixture-only Git identity, and disables fixture signing and inherited hooks. It does not install hooks or change files in the source checkout.

A successful script exit means it reproduced the **current defect**, not that the product passed a regression test. After a fix, the corresponding assertions should fail or be replaced with assertions of the intended behavior. Check out the reviewed revision when reproducing the original findings.

## F1 marked file modes and symlinks

Run this from any directory; change the source path only if your checkout is elsewhere.

```sh
python3 - /home/pavlo/Documents/agent-workflows <<'PY'
#!/usr/bin/env python3
"""Reproduce marked-file snapshot defects in scratch Git repositories.

Usage: python3 /tmp/agent-workflows-gate-repro.py [source-checkout]
This builds cw from the source, changes only scratch directories, and verifies
the reported current behavior. A final exit 0 means the defects reproduced.
"""

import os
from pathlib import Path
import subprocess
import sys
import tempfile


source = Path(sys.argv[1] if len(sys.argv) > 1 else "/home/pavlo/Documents/agent-workflows")
scratch = Path(tempfile.mkdtemp(prefix="cw-gate-review-repro-"))
binary = scratch / "cw"


def command(cwd, *args, expected=0):
    result = subprocess.run(args, cwd=cwd, text=True, capture_output=True)
    print("$", " ".join(str(arg) for arg in args))
    print(result.stdout + result.stderr, end="")
    print("exit", result.returncode)
    if result.returncode != expected:
        raise AssertionError(f"Expected exit {expected}, got {result.returncode}")
    return result


def initialize(name):
    root = scratch / name
    root.mkdir()
    command(root, "git", "init", "-q", "-b", "main")
    command(root, "git", "config", "user.name", "Code Review")
    command(root, "git", "config", "user.email", "review@example.invalid")
    command(root, "git", "config", "commit.gpgsign", "false")
    command(root, "git", "config", "core.filemode", "true")
    # Isolate fixtures from any global hook-manager configuration.
    command(root, "git", "config", "core.hooksPath", str(scratch / "empty-hooks"))
    return root


command(source, "go", "build", "-o", str(binary), "./cmd/cw")
for mark in ("assume-unchanged", "skip-worktree"):
    print("\nMODE MISMATCH:", mark)
    root = initialize("mode-" + mark)
    (root / "check.sh").write_text("#!/bin/sh\nexit 0\n")
    os.chmod(root / "check.sh", 0o644)
    (root / "cw.toml").write_text('version = 1\n[[check]]\nid = "unit"\nrun = ["./check.sh"]\n')
    command(root, "git", "add", ".")
    command(root, "git", "commit", "-qm", "fixture")
    command(root, "git", "update-index", "--" + mark, "check.sh")
    os.chmod(root / "check.sh", 0o755)
    result = command(root, str(binary), "run")
    assert "passed" in result.stdout
    command(root, str(binary), "hook", "pre-commit")
    result = command(root, "git", "ls-files", "-s", "check.sh")
    assert result.stdout.startswith("100644 ")
    print("Actual working-file mode:", oct((root / "check.sh").stat().st_mode & 0o777))
    clone = scratch / ("clone-" + mark)
    command(scratch, "git", "clone", "-q", str(root), str(clone))
    result = command(clone, str(binary), "run", expected=5)
    assert "permission denied" in result.stdout + result.stderr
    assert (clone / "check.sh").stat().st_mode & 0o111 == 0
    print("CONFIRMED: pass applies to a tree whose script is not executable in a fresh checkout.")

    print("\nUNCHANGED SYMLINK:", mark)
    root = initialize("symlink-" + mark)
    (root / "target").write_text("content\n")
    (root / "link").symlink_to("target")
    (root / "cw.toml").write_text('version = 1\n[[check]]\nid = "unit"\nrun = ["true"]\n')
    command(root, "git", "add", ".")
    command(root, "git", "commit", "-qm", "fixture")
    command(root, "git", "update-index", "--" + mark, "link")
    result = command(root, "git", "status", "--porcelain")
    assert not result.stdout
    result = command(root, str(binary), "run", expected=1)
    assert "differs from the index" in result.stdout + result.stderr
    assert (root / "link").readlink() == Path("target")
    print("CONFIRMED: an unchanged marked symlink is rejected as different.")

print("\nAll four cases reproduced. Scratch artifacts:", scratch)
PY
```

Observed for both `assume-unchanged` and `skip-worktree`:

```text
cw run
  unit: ./check.sh
  unit: passed in 0s
  every declared check has a passing run on this content
  exit 0

cw hook pre-commit
  exit 0

git ls-files -s check.sh
  100644 039e4d0069c5c26909f86c505b9de66182e6d1f3 0 check.sh

Actual working-file mode: 0o755

cw run in a fresh clone of the same committed content
  unit: failed: ... ./check.sh: permission denied
  exit 5

cw run with an unchanged marked symbolic link
  cw: the working tree cannot be recorded: link is marked ... and differs from the index
  exit 1
```

Expected: a marked executable-mode mismatch should be rejected when computing the snapshot; an unchanged supported symlink should be accepted. The two manifestations share the same hash-only comparison defect.

## F2 post-shipping permissions and F3 duplicate scenario identity

The second fixture deliberately creates valid structural review/approval records; these are test fixture data, not claims of a real human review. It supplies separate Greeting and Authorization scenarios with one common title and only a greeting test mapping.

```sh
python3 - /home/pavlo/Documents/agent-workflows <<'PY'
from pathlib import Path
import subprocess, tempfile, json, sys
source=Path(sys.argv[1]).resolve()
base=Path(tempfile.mkdtemp(prefix='cw-product-repro-'))
CW=str(base/'cw')
subprocess.run(['go','build','-o',CW,'./cmd/cw'],cwd=source,check=True)
def run(repo,*args):
    p=subprocess.run(args,cwd=repo,text=True,capture_output=True)
    if p.returncode not in (0, 5):
        raise RuntimeError(f'{args}: exit {p.returncode}\n{p.stdout}\n{p.stderr}')
    return p

def write(repo,name,text):
    p=repo/name
    p.parent.mkdir(parents=True,exist_ok=True)
    p.write_text(text)

def init(name):
    r=base/name;r.mkdir()
    run(r,'git','init','-q')
    run(r,'git','config','user.email','review@example.invalid')
    run(r,'git','config','user.name','Review')
    run(r,'git','config','commit.gpgsign','false')
    run(r,'git','config','core.hooksPath',str(base/'empty-hooks'))
    write(r,'README.md','fixture\n')
    run(r,'git','add','-A');run(r,'git','commit','-qm','fixture')
    return r

r=init('archive')
write(r,'openspec/specs/greeting/spec.md','### Requirement: Greeting\nThe app SHALL greet.\n')
p=run(r,CW,'roles','check','spec-writer','--from','HEAD','--task','add-greeting')
assert p.returncode == 5 and 'outside its limit' in p.stdout
print('ARCHIVE: exit',p.returncode);print(p.stdout,p.stderr)

r=init('duplicate-scenarios')
root='openspec/changes/add-greeting/'
write(r,root+'proposal.md','# Proposal\nTwo independent cases.\n')
for cap, req, action in [('greet','Greeting','greet'),('auth','Authorization','authorize')]:
    write(r,root+f'specs/{cap}/spec.md',f'''## ADDED Requirements

### Requirement: {req}

The app SHALL {action}.

Source: the request.

#### Scenario: Happy path

- **WHEN** the app is asked to {action}
- **THEN** it performs {action}
''')
write(r,root+'tasks.md','''- [x] 1.1 Implement Greeting and Authorization. Size: S. Depends on: none. Check: review. Source: request.
''')
write(r,'tests/greet_test.txt','test_greeting\n')
write(r,root+'tests.md','''| Scenario | Test file | Text |
| --- | --- | --- |
| Happy path | tests/greet_test.txt | test_greeting |
''')
write(r,root+'design.md','''## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| app | Greet and authorize. | none | no | |

## Rules

| Rule | Checked by |
| --- | --- |
| Greet and authorize. | review |
''')
ids=json.loads(run(r,CW,'change','status','add-greeting','--json').stdout)['content']
write(r,root+'reviews.md',f'''## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |
| spec | 1 | 2026-11-15 | {ids['spec']} |
| design | 1 | 2026-11-15 | {ids['design']} |
| code | 1 | 2026-11-15 | {ids['code']} |

## Approvals

| What | Date | Content |
| --- | --- | --- |
| spec | 2026-11-15 | {ids['spec']} |
| design | 2026-11-15 | {ids['design']} |
''')
run(r,'git','add','-A');run(r,'git','commit','-qm','two behaviors one mapping')
p=run(r,CW,'change','check','add-greeting')
assert p.returncode == 0
print('DUPLICATE SCENARIO: exit',p.returncode);print(p.stdout,p.stderr)
status=run(r,CW,'change','status','add-greeting').stdout
assert 'add-greeting: done' in status
print('DUPLICATE STATUS:',status)
print('fixtures',base)
PY
```

Observed:

```text
ARCHIVE: exit 5
`spec-writer` changed 1 of 1 paths outside its limit:
  openspec/specs/greeting/spec.md (added):
  `spec-writer` may change only openspec/changes/add-greeting/proposal.md
  and openspec/changes/add-greeting/specs/
start `spec-writer` again with these paths; do not accept the work and do not repair it yourself

DUPLICATE SCENARIO: exit 0
add-greeting: the files obey their rules

DUPLICATE STATUS: add-greeting: done
```

For F2, the low-level checker is enforcing its current rule correctly. The defect is the contradiction between that rule and the shipped-change procedure that assigns the forbidden work to this role. A fix needs an explicit authorized finalization path, rather than indiscriminately loosening the normal role.

For F3, expected behavior is rejection of ambiguous scenario identities or a requirement for separately qualified mappings. No semantic understanding of the test body is needed to detect the ambiguity.

## Validation commands used for the review

```sh
go build ./...
go vet ./...
go test -race -count=1 -json ./...
golangci-lint run ./...
govulncheck ./...
GOOS=darwin go vet ./...
GOOS=darwin golangci-lint run ./...
GOOS=darwin govulncheck ./...
GOOS=windows go vet ./...
GOOS=windows golangci-lint run ./...
GOOS=windows govulncheck ./...
CW_COST=1 go test -count=1 -v ./scenarios/cost/
```

All commands above passed in the review environment. The report identifies skipped tests and the distinction between local native execution and cross-platform static analysis.
