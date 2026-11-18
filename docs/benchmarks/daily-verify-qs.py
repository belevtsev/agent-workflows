"""Replay a verify-only handoff on pinned qs source using prepared offline inputs (Linux)."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import time

from qs_replay import CHECK, sandbox

parser = argparse.ArgumentParser(description=__doc__)
for flag in ("kitsu", "original", "reference", "node", "modules", "grader"):
    parser.add_argument("--" + flag, required=True, type=Path)
parser.add_argument("--full-suite-output", action="store_true", help="Emit the full TAP transcript from the assertion")
parser.add_argument("--check-failed-retry", action="store_true",
                    help="Try substituting the green original suite for the failing partial candidate assertion, then check the upstream fix")
args = parser.parse_args()
assert hashlib.sha256(args.grader.read_bytes()).hexdigest() == "655503ff0383478c653590850ce9440b0c40d650829689b11174964495a52c63"
base_js = (args.original / "lib/utils.js").read_bytes()
fixed_js = (args.reference / "lib/utils.js").read_bytes()
assert hashlib.sha256(base_js).hexdigest() == "0cc35581f38d05554e412a7645ac9fc083263622386ba7f04c999161206a1b70"
assert hashlib.sha256(fixed_js).hexdigest() == "ace1c643b256be181aba29f01612283fc48d50f076a681bf3931316bf712756a"
change = b"countElements(target) >= options.arrayLimit"
assert fixed_js.count(change) == 1 and change not in base_js
partial_js = fixed_js.replace(change, b"nextIndex >= options.arrayLimit")

# Run the unchanged original suite and the external behavior assertions in both arms.
check = CHECK
if args.full_suite_output:
    check = check.replace("tail -3 /tmp/suite", "cat /tmp/suite")
def check_failed_retry(task, ticket, work):
    # The original TAP suite passes on this input. It omits the scalar-append regression.
    substitute = task("test", "--ticket", ticket, "--expect", "Original suite passes",
                      "--", "node", "/deps/tape/bin/tape", "test/**/*.js", status=(0, 1))
    accepted = substitute.returncode == 0
    if accepted:
        assert json.loads(substitute.stdout)["verified"]
    else:
        assert "--retry" in substitute.stderr
    (work / "lib/utils.js").write_bytes(fixed_js)
    ticket = json.loads(task("next").stdout)["ticket"]
    assertion = ["--expect", "Sparse arrays count defined elements", "--", "sh", "/grader/check.sh"]
    repaired = task("test", "--ticket", ticket, *(assertion if accepted else ["--retry"]))
    receipt = json.loads(repaired.stdout)
    assert receipt["verified"] and receipt["next"] == "done"
    return {"accepted_failing_candidate_after_suite_substitution": accepted,
            "upstream_fix_verified": True, "recovery_arguments": assertion if accepted else ["--retry"]}


rows = []
recovery = None
for i, (candidate, source) in enumerate((("original", base_js), ("partial", partial_js), ("reference", fixed_js))):
    for arm in (("plain", "kitsu") if i % 2 == 0 else ("kitsu", "plain")):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            work = root / "work"
            shutil.copytree(args.original, work, ignore=shutil.ignore_patterns(".git", "node_modules"))
            (work / "lib/utils.js").write_bytes(source)
            (root / "check.sh").write_text(check)
            (root / "home").mkdir()
            (root / "tmp").mkdir()
            (work / "node_modules").mkdir()
            run = sandbox(root, work, args.modules, args.node, args.kitsu, args.grader)

            run("git", "init", "-q")
            run("git", "add", ".")
            run("git", "commit", "-qm", "Frozen candidate")
            start = time.perf_counter()
            passed = candidate == "reference"
            final_state = None
            if arm == "plain":
                result = run("sh", "/grader/check.sh", status=0 if passed else 1)
                output = result.stdout
            else:
                def task(verb, *argv, status=0):
                    return run("kitsu", "task", verb, "verify-qs", "--brief", *argv, status=status)
                opened = json.loads(task("open", "--request", "Verify sparse array limits", "--entry",
                                         "verify", "--until", "verify").stdout)
                intake = json.loads(task("record", "intake", "--actor", "scripted-host", "--ticket", opened["ticket"],
                                         "--note", "Check original suite and public parse behavior").stdout)
                result = task("test", "--ticket", intake["ticket"], "--expect", "Sparse arrays count defined elements",
                              "--", "sh", "/grader/check.sh", status=0 if passed else 5)
                receipt = json.loads(result.stdout)
                final_state = receipt["next"]
                assert receipt["verified"] == passed and final_state == ("done" if passed else "verify")
            elapsed = time.perf_counter() - start
            response_bytes = len(result.stdout.encode()) + len(result.stderr.encode())
            if arm == "kitsu":
                log = receipt.get("verification_log")
                if log:
                    raw = run("cat", log["path"]).stdout
                    assert hashlib.sha256(raw.encode()).hexdigest() == log["log_sha256"]
                    output = raw
                else:
                    output = result.stderr  # Earlier Kitsu versions stream the assertion.
            cases = [json.loads(line) for line in output.splitlines() if line.startswith('{"id":')]
            assert len({c["id"] for c in cases}) == len(cases) == 19
            assert "# ok" in output and all(c["passed"] for c in cases) == passed
            assert not run("git", "status", "--porcelain").stdout
            rows.append(dict(arm=arm, candidate=candidate, seconds=round(elapsed, 6), original_suite=True,
                             verification_response_bytes=response_bytes,
                             next=final_state, source_sha256=hashlib.sha256(source).hexdigest(),
                             criteria={c["id"]: c["passed"] for c in cases}))
            if args.check_failed_retry and candidate == "partial" and arm == "kitsu":
                recovery = check_failed_retry(task, receipt["ticket"], work)
for candidate in ("original", "partial", "reference"):
    left, right = [r["criteria"] for r in rows if r["candidate"] == candidate]
    assert left == right
for row in rows:
    cases = row.pop("criteria")
    row["criteria_sha256"] = hashlib.sha256(json.dumps(cases, sort_keys=True).encode()).hexdigest()
    row["supplementary_scalar_append"] = cases.pop("supplementary_scalar_append")
    row["frozen_assertions_passed"] = sum(cases.values())
    row["frozen_assertions_total"] = len(cases)
print(json.dumps({"upstream": "ljharb/qs", "base": "42eccf0f6292c502fb4bc175191f0812d5476769",
                  "reference": "101492a5c4d9cb72f6510ecc15483759ec8e45fa", "model_calls": 0, "full_suite_output": args.full_suite_output,
                  **({"failed_retry": recovery} if args.check_failed_retry else {}),
                  "grader_sha256": hashlib.sha256(args.grader.read_bytes()).hexdigest(), "runs": rows}, indent=2))
