"""Finish copies of the budget-stopped qs task offline, using plain shell and the saved Kitsu task."""
import argparse
import hashlib
import json
import re
from pathlib import Path
import shutil
import statistics
import tempfile
import time

from qs_replay import CHECK, sandbox

parser = argparse.ArgumentParser(description=__doc__)
for name in ("worktree", "kitsu", "grader", "node", "modules"):
    parser.add_argument("--" + name, type=Path, required=True)
parser.add_argument("--pairs", type=int, default=3)
parser.add_argument("--local-publication", action="store_true",
                    help="After the requested commit, exercise a new publication request against two disposable local remotes")
parser.add_argument("--switch-at-commit", action="store_true",
                    help="Switch branches at the same revision after verification and recover the intended commit")
parser.add_argument("--coordinator-recovery", action="store_true",
                    help="Check whether task list and next recover the saved coordinator context before resuming")
parser.add_argument("--retry-repository-check", action="store_true",
                    help="Inject a single declared-suite failure, resume, and replay a lost Kitsu success response")
parser.add_argument("--previous-cli", type=Path, help="Compare coordinator reads with an earlier Kitsu executable in the same frozen checkout")
args = parser.parse_args()
if args.previous_cli and not args.coordinator_recovery:
    parser.error("--previous-cli requires --coordinator-recovery")
if args.pairs < 1:
    parser.error("--pairs must be positive")

paid = json.loads((Path(__file__).parent / "daily-review-data/qs-paid.json").read_text())
task = paid["case_id"]
target = paid["runs"][0]["candidate_tree"]
ledger_path = Path(".git/kitsu-stages") / f"task-harness-{task}.json"
ledger = (args.worktree / ledger_path).read_bytes()
record = json.loads(ledger)
reviews = [event for event in record["events"] if event["action"] == "review"]
assert len(reviews) == 4 and reviews[-1]["candidate"]["tree"] == target and not reviews[-1].get("findings")
assert record["scope"]["commit"] and record["scope"]["until"] == "deliver"
assert hashlib.sha256(args.grader.read_bytes()).hexdigest() == "655503ff0383478c653590850ce9440b0c40d650829689b11174964495a52c63"
paths = ["lib/parse.js", "lib/utils.js", "test/parse.js", "test/utils.js"]
declared = ["node", "node_modules/tape/bin/tape", "test/parse.js", "test/stringify.js", "test/utils.js"]


def intermittent_node(root):
    wrapper = root / "node"
    wrapper.write_text("""#!/bin/sh
set -eu
if [ "$#" -eq 4 ] && [ "$1" = node_modules/tape/bin/tape ] && [ "$2" = test/parse.js ] && [ "$3" = test/stringify.js ] && [ "$4" = test/utils.js ]; then
    echo launch >> /tmp/declared-launches
    if [ -f /tmp/fail-once ]; then
        rm /tmp/fail-once
        echo 'Replay injected a temporary declared check failure' >&2
        exit 23
    fi
fi
exec /opt/node/bin/node "$@"
""")
    wrapper.chmod(0o755)
    (root / "tmp/fail-once").touch()
    return wrapper


def publish_and_recover(run, command, arm):
    destinations = ("/tmp/team.git", "/tmp/mirror.git")
    ref = "refs/heads/delivery"
    head = run("git", "rev-parse", "HEAD").stdout.strip()
    for path in destinations:
        run("git", "init", "--bare", "--quiet", path)
    run("git", "remote", "add", "team", destinations[0])
    for path in destinations:
        run("git", "config", "--add", "remote.team.pushurl", path)

    def observed():
        refs = [run("git", "ls-remote", "--heads", "--", path, ref).stdout.strip() for path in destinations]
        return all(line == f"{head}\t{ref}" for line in refs)

    started = time.monotonic()
    if arm == "kitsu":
        pending = command("extend", "--publish-to", "team/delivery")
        assert pending["next"] == "publish" and pending["verified"]
    run("git", "push", "team", f"HEAD:{ref}")
    if arm == "kitsu":
        initial = command("confirm", "--ticket", pending["ticket"])
        assert initial["next"] == "done" and initial["last_publication"]["commit"] == head
    else:
        assert observed()
    initial_seconds = time.monotonic() - started

    # The remote change and the independent readback are outside both timed intervals.
    run("git", "--git-dir", destinations[1], "update-ref", "-d", ref)
    assert not observed()
    started = time.monotonic()
    if arm == "kitsu":
        pending = json.loads(run("kitsu", "task", "confirm", task, "--brief", status=1).stdout)
        assert pending["next"] == "publish" and pending["last_publication"]["outcome"] == "unconfirmed"
    else:
        assert not observed()
    run("git", "push", destinations[1], f"HEAD:{ref}")
    if arm == "kitsu":
        final = command("confirm", "--ticket", pending["ticket"])
        assert final["next"] == "done" and final["last_publication"]["outcome"] == "confirmed"
    else:
        assert observed()
    recovery_seconds = time.monotonic() - started
    assert observed() and not run("git", "status", "--porcelain").stdout
    return dict(initial_seconds=round(initial_seconds, 6), recovery_seconds=round(recovery_seconds, 6),
                missing_mirror_detected=True, destinations_at_verified_commit=2)


def check_coordinator_reads(run, previous):
    def read(*args):
        out = run("kitsu", "task", *args, "--json").stdout
        return json.loads(out), len(out.encode())

    index, _ = read("list")
    saved = next(row["daily"] for row in index["tasks"] if row["task"] == task)
    assert saved["last_action"] == "review" and record["request"].startswith(saved["request"].rstrip("…"))
    view, next_bytes = read("next", task)
    full, show_bytes = read("show", task)
    history = full["events"]
    assert history == record["events"]
    for action in ("intake", "repair"):
        expected = next(e["note"] for e in reversed(history) if e["action"] == action)
        assert view["prior_notes"][action] == expected
    assert view["review_current"] and view["latest_review"] == reviews[-1] and view["request"] == record["request"]
    assert view["scope"] == record["scope"] and view["pause"]["pending_action"] == "verify"
    result = {"next_bytes": next_bytes, "show_bytes": show_bytes,
              "request_intake_repair_review_scope_and_pause_matched": True}
    if previous:
        prev_next = previous("kitsu", "task", "next", task, "--json").stdout
        prev_show = previous("kitsu", "task", "show", task, "--json").stdout
        assert json.loads(prev_show)["events"] == history
        older = json.loads(prev_next)
        assert older["request"] == record["request"] and older["latest_review"] == reviews[-1]
        assert all(json.dumps(note) not in json.dumps(older) for note in view["prior_notes"].values())
        result.update(previous_next_bytes=len(prev_next.encode()), previous_show_bytes=len(prev_show.encode()),
                      previous_show_had_full_history=True, previous_next_omitted_prior_notes=True)
    return result


def finish(root, arm):
    work = root / "work"
    # Reconstruct the candidate from pinned Git objects. The paid checkout is read-only input.
    work.mkdir()
    (work / "node_modules").mkdir()
    (root / "home").mkdir()
    (root / "tmp").mkdir()
    (root / "check.sh").write_text(CHECK.replace("tail -3", "tail -7"))
    wrapper = intermittent_node(root) if args.retry_repository_check else None
    run = sandbox(root, work, args.modules, args.node, args.kitsu, args.grader, node_wrapper=wrapper)
    run("git", "init", "-q")
    shutil.copytree(args.worktree / ".git/objects", work / ".git/objects", dirs_exist_ok=True)
    base = record["starting_snapshot"]
    run("git", "update-ref", "HEAD", base["head"])
    run("git", "read-tree", "--reset", "-u", target)
    run("git", "read-tree", base["index"])
    assert sorted(run("git", "diff", "--name-only", "HEAD").stdout.splitlines()) == paths
    if arm == "kitsu":
        (work / ledger_path).parent.mkdir()
        (work / ledger_path).write_bytes(ledger)

    def command(*argv, status=0):
        return json.loads(run("kitsu", "task", argv[0], task, "--brief", *argv[1:], status=status).stdout)

    coordinator_reads = None
    if arm == "kitsu" and args.coordinator_recovery:
        previous = sandbox(root, work, args.modules, args.node, args.previous_cli, args.grader) if args.previous_cli else None
        coordinator_reads = check_coordinator_reads(run, previous)
    if args.switch_at_commit:
        run("git", "checkout", "-qb", "delivery")
    started = time.monotonic()
    if arm == "kitsu":
        paused = command("next")
        assert paused["pause"] == {"reason": "deadline", "pending_action": "verify"}
        ready = command("extend", "--time-limit", "2m")
        assert ready["next"] == "verify" and ready["review_current"]
        verify_args = ("test", "--ticket", ready["ticket"], "--expect", "Sparse arrays count defined elements", "--", "sh", "/grader/check.sh")
        if args.retry_repository_check:
            first = command(*verify_args, status=5)
            resumed = command("next")
            failure = resumed["verification_log"]
            assert first["verification_log"] == failure and failure["check_id"] == "pilot-test" and failure["exit_status"] == 23
            diagnostic = run("cat", failure["path"]).stdout
            assert "Replay injected" in diagnostic and hashlib.sha256(diagnostic.encode()).hexdigest() == failure["log_sha256"]
            retry_args = ("test", "--ticket", resumed["ticket"], "--retry")
            verified = command(*retry_args)
            replayed = command(*retry_args)
            assert first["run_id"] == verified["run_id"] == replayed["run_id"] and replayed["verified"]
        else:
            verified = command(*verify_args)
        assert verified["verified"] and verified["next"] == "commit" and verified["repository_checks"] == "passed"
    else:
        output = run("sh", "/grader/check.sh").stdout
        if args.retry_repository_check:
            assert "Replay injected" in run(*declared, status=23).stderr
        assert "# ok" in run(*declared).stdout
    run("git", "add", "--", *paths)
    if args.switch_at_commit:
        run("git", "checkout", "-qb", "other")
        if arm == "kitsu":
            assert verified["commit_target"] == "refs/heads/delivery"
            denied = run("kitsu", "task", "commit", task, "--ticket", verified["ticket"],
                         "--message", "Repair sparse array limits", status=1)
            assert "refs/heads/other" in denied.stderr and "refs/heads/delivery" in denied.stderr, denied.stderr
            changed = command("next")
            assert changed["next"] == "blocked" and not changed.get("ticket")
            assert changed["commit_target"] == "refs/heads/delivery" and changed["current_commit_target"] == "refs/heads/other"
            assert changed["run_id"] == verified["run_id"]
        else:
            assert run("git", "symbolic-ref", "HEAD").stdout.strip() != "refs/heads/delivery"
        assert run("git", "rev-parse", "HEAD").stdout.strip() == base["head"]
        run("git", "checkout", "-q", "delivery")
    if arm == "kitsu":
        result = command("commit", "--ticket", verified["ticket"], "--message", "Repair sparse array limits")
        assert result["next"] == "done" and result["delivery"] == "committed"
    else:
        run("git", "commit", "-qm", "Repair sparse array limits")
    elapsed = time.monotonic() - started
    if arm == "kitsu":
        log = verified["verification_log"]
        output = run("cat", log["path"]).stdout
        assert hashlib.sha256(output.encode()).hexdigest() == log["log_sha256"]
        history = json.loads((work / ledger_path).read_bytes())
        assert history["events"][:len(record["events"])] == record["events"]
        assert [e["action"] for e in history["events"][len(record["events"]):]] == ["test", "commit"]
        if args.retry_repository_check:
            receipts = history["events"][-2]["prior_repository_failures"]
            assert len(receipts) == 1 and all(receipts[0][k] == failure[k] for k in ("run_id", "check_id", "log_sha256", "exit_status"))
    cases = [json.loads(line) for line in output.splitlines() if line.startswith('{"id":')]
    assert len(set(c["id"] for c in cases)) == len(cases) == 19 and all(c["passed"] for c in cases)
    assert re.search(r"^# pass\s+1726$", output, re.M), output[:400]
    assert not run("git", "status", "--porcelain").stdout
    assert run("git", "show", "-s", "--format=%T %P", "HEAD").stdout.strip().split() == [target, base["head"]]
    if args.switch_at_commit:
        assert run("git", "rev-parse", "other").stdout.strip() == base["head"]
        assert run("git", "symbolic-ref", "HEAD").stdout.strip() == "refs/heads/delivery"
    publication = {"publication": publish_and_recover(run, command, arm)} if args.local_publication else {}
    if coordinator_reads is not None:
        publication["coordinator_reads"] = coordinator_reads
    if args.retry_repository_check:
        launches = (root / "tmp/declared-launches").read_text().splitlines()
        assert launches == ["launch", "launch"]
        publication["repository_retry"] = dict(injected_exit=23, declared_suite_launches=len(launches))
        if arm == "kitsu":
            publication["repository_retry"].update(failure_receipt_retained=True, lost_success_replayed=True)
    return dict(**publication, arm=arm, seconds=round(elapsed, 6), behavior_passed=len(cases), submitted_suite_passed=1726,
                declared_suite_passed=True, clean_verified_commit=True)


rows = []
for pair in range(args.pairs):
    for arm in (("plain", "kitsu") if pair % 2 == 0 else ("kitsu", "plain")):
        with tempfile.TemporaryDirectory() as tmp:
            rows.append({"pair": pair + 1, **finish(Path(tmp), arm)})
report = dict(task=task, candidate=target, ledger_sha256=hashlib.sha256(ledger).hexdigest(),
                      kitsu_sha256=hashlib.sha256(args.kitsu.read_bytes()).hexdigest(),
                      model_calls=0, compact=False, saved_reviews=len(reviews), additional_reviews=0,
                      median_wall_seconds={a: statistics.median(r["seconds"] for r in rows if r["arm"] == a)
                                           for a in ("plain", "kitsu")}, runs=rows)
if args.previous_cli:
    report["previous_read_cli_sha256"] = hashlib.sha256(args.previous_cli.read_bytes()).hexdigest()
if args.switch_at_commit:
    report["same_commit_branch_switch"] = "Recovered delivery branch; other branch stayed at parent"
if args.local_publication:
    report["local_publication_medians"] = {
        arm: {key: statistics.median(row["publication"][key] for row in rows if row["arm"] == arm)
              for key in ("initial_seconds", "recovery_seconds")}
        for arm in ("plain", "kitsu")
    }
print(json.dumps(report, indent=2))
