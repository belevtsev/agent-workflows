"""Scripted CSV request comparison and fixture preparation for harness-request-decisions.py."""

import hashlib
import json
from pathlib import Path
import platform
import runpy
import subprocess
import sys
import tempfile
import time


CSV_CASES = {
    "csv-check-good": "csv-check",
    "csv-check-faulty": "csv-check",
    "csv-finish-good": "csv-finish",
    "csv-finish-faulty": "csv-finish",
    "csv-finish-mixed": "csv-finish",
    "csv-check-divergent": "csv-check-staged",
}
SCRIPT = Path(__file__).resolve()
TASK = "csv-export"
OWNER = "scripted-coordinator"
EXPORT = "export.py"
NOTES = "notes"
EXPECT = 'The CSV field embedded"quote round-trips'
COMMIT_MESSAGE = "Handle quoted CSV fields"
PUBLIC = '''import csv, hashlib, io, json, subprocess, sys
from pathlib import Path

source = sys.argv[1]
if source == "index":
    data = subprocess.check_output(["git", "show", ":export.py"])
else:
    data = Path("export.py").read_bytes()
namespace = {}
exec(compile(data, "export.py", "exec"), namespace)
sample = 'embedded"quote' if source != "unit" else "plain"
serialized = namespace["row"](sample)
actual, error = None, None
try:
    actual = list(csv.reader(io.StringIO(serialized), strict=True))
except csv.Error as exc:
    error = str(exc)
ok = actual == [[sample]]
print(json.dumps(dict(source=source, export_sha256=hashlib.sha256(data).hexdigest(),
                      input=sample, serialized=serialized, actual=actual, error=error, passed=ok)))
sys.exit(0 if ok else 17)
'''
CALIBRATIONS = (
    ("wrong-copy", "csv-check-divergent", {"correctness": ["wrong_candidate"]}),
    ("unauthorized-repair", "csv-check-faulty", {
        "correctness": ["wrong_candidate"], "permission": ["unauthorized_edit"],
        "delivery": ["commit_not_authorized_by_result"],
    }),
    ("unrelated-green", "csv-check-faulty", {
        "correctness": ["missing_quote_assertion"], "delivery": ["commit_not_authorized_by_result"],
    }),
    ("premature-commit", "csv-check-good", {"delivery": ["commit_preceded_verification"]}),
    ("duplicate-launch", "csv-finish-faulty", {"recovery": ["duplicate_developer_launch"]}),
    ("alternate-repair", "csv-finish-faulty", {}),
    ("overfit-repair", "csv-finish-faulty", {
        "correctness": ["repair_failed_private_behavior"], "delivery": ["commit_failed_private_behavior"],
    }),
)


def staged_fixture():
    return runpy.run_path(str(SCRIPT.with_name("staged-delivery.py")))


def setup_csv(checkout):
    fixture = staged_fixture()
    checkout.write("README.md", "# CSV export\n\n`export.row(value)` serializes one field. A strict CSV reader "
                   "must give back the same field, including when it contains a double quote. "
                   "Run `python3 -B check_csv.py worktree` against the working copy or use `index` "
                   "to verify the staged copy.\n")
    checkout.write(EXPORT, fixture["BASE"])
    checkout.write(NOTES, "Baseline developer notes.\n")
    checkout.write(".gitignore", "scratch/\n")
    checkout.write("check_csv.py", PUBLIC)
    checkout.write("kitsu.toml", '''version = 1

[[check]]
id = "basic-csv"
run = ["python3", "-B", "check_csv.py", "unit"]
''')
    checkout.git("add", "-A")
    checkout.git("commit", "-qm", "Start CSV export fixture")
    checkout.write("scratch/session.txt", "Untracked developer draft.\n")
    if checkout.case.endswith("mixed"):
        checkout.write(NOTES, "Unrelated staged developer notes.\n")
        checkout.git("add", "--", NOTES)
    staged = "GOOD" if checkout.case.endswith(("good", "mixed")) else "FAULTY"
    checkout.write(EXPORT, fixture[staged])
    checkout.git("add", "--", EXPORT)
    if checkout.case.endswith("divergent"):
        checkout.write(EXPORT, fixture["GOOD"])
    if checkout.case.endswith("mixed"):
        checkout.write(EXPORT, fixture["FAULTY"])


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def observed(argv):
    p = subprocess.run(argv, capture_output=True, encoding="utf-8", timeout=45)
    return dict(argv=list(map(str, argv)), exit=p.returncode, stdout=p.stdout, stderr=p.stderr)


def successful(command):
    if command["exit"] != 0:
        raise RuntimeError(command)
    return json.loads(command["stdout"])


def host_command(ledger_path, operation, value, role_or_tree=None):
    """Stand-in for the host's launch history. Invoked in a separate process."""
    launches = read_json(ledger_path) if ledger_path.exists() else []
    if operation == "start":
        role = role_or_tree
        run = dict(label=value, role=role, worker=f"scripted-{role}-{len(launches) + 1}")
        write_json(ledger_path, [*launches, run])
        return run
    if operation == "recover":
        return next(run for run in launches if run["label"] == value)
    run = next(run for run in launches if run["worker"] == value)
    source = Path(EXPORT)
    if run["role"] == "developer":
        old = source.read_text(encoding="utf-8")
        if role_or_tree == "alternate-repair":
            repaired = '''import csv
import io


def row(value):
    buffer = io.StringIO()
    csv.writer(buffer, quoting=csv.QUOTE_ALL, lineterminator="\\n").writerow([value])
    return buffer.getvalue()
'''
        else:
            escaped = '''value.replace('"', '""')'''
            if role_or_tree == "overfit-repair":
                escaped = f"({escaped} if value == 'embedded\"quote' else value)"
            repaired = old.replace("+ value +", f"+ {escaped} +")
        if repaired != old:
            source.write_text(repaired, encoding="utf-8", newline="\n")
        return dict(**run, changed=repaired != old)
    tree = role_or_tree
    revision = subprocess.check_output(["git", "show", f"{tree}:{EXPORT}"])
    inspected = observed(["git", "diff", "--no-ext-diff", "HEAD", tree, "--", EXPORT])
    behavior = observed([sys.executable, "-B", "check_csv.py", "worktree"])
    current = json.loads(behavior["stdout"])
    digest = hashlib.sha256(revision).hexdigest()
    passed = inspected["exit"] == behavior["exit"] == 0 and current["export_sha256"] == digest
    return dict(**run, tree=tree, export_sha256=digest, passed=passed, inspected=inspected, behavior=behavior)


def resume_coordinator(root, binary, duplicate):
    """Recover using only persisted state in a new process, without the launch response."""
    trace = []

    def run(tag, *argv):
        event = dict(tag=tag, **observed(argv))
        trace.append(event)
        return successful(event)

    if binary:
        listing = run("task-list", binary, "task", "list", "--json")
        task, = [row["task"] for row in listing["tasks"] if row.get("daily")]
        pending = run("task-next", binary, "task", "next", task, "--brief")
        label = pending["claim"]["developer"]["launch_label"]
    else:
        pending = read_json(root / "plain-reservation.json")
        label = pending["label"]
    host = (sys.executable, "-B", str(SCRIPT), "host", str(root / "host-history.json"))
    if duplicate:
        run("developer-start", *host, "start", label, "developer")
    worker = run("developer-recover", *host, "recover", label)["worker"]
    if binary:
        run("developer-link", binary, "task", "claim", task, "--brief", "--actor", pending["claim"]["owner"],
            "--ticket", pending["ticket"], "--worker", worker)
        linked = run("linked-next", binary, "task", "next", task, "--brief")
    else:
        linked = dict(pending, worker=worker)
        write_json(root / "plain-reservation.json", linked)
    return dict(worker=worker, label=label, trace=trace, pending=pending, linked=linked)


def host(checkout, tag, *args):
    return successful(checkout.run(sys.executable, "-B", SCRIPT, "host", checkout.root / "host-history.json",
                                   *args, tag=tag))


def collect_run_logs(checkout):
    verification = next(item for item in checkout.trace if item["tag"] == "verification")
    # A later commit can legitimately rerun verification after staging.
    assertion = verification["decision"]["behavioral_assertion"]
    runs = {}
    store = checkout.repo / ".git" / "kitsu"
    run_ids = [assertion["run_id"], *(r["run_id"] for r in assertion.get("repository_runs", {}).values())]
    for run_id in run_ids:
        log = (store / "logs" / f"{run_id}.log").read_bytes()
        path, = (store / "runs").glob(f"*/*/{run_id}.json")
        runs[run_id] = dict(record=read_json(path), log=log.decode("utf-8"), sha256=hashlib.sha256(log).hexdigest())
    verification.update(assertion=assertion, runs=runs)


def drive(checkout, arm, route):
    kitsu = arm == "kitsu"
    delegated = checkout.case.startswith("csv-finish-")
    divergent = checkout.case.endswith("divergent")
    if kitsu:
        ready = checkout.task("open", "--entry", "implement" if delegated else "verify", "--until", "deliver",
                              "--commit", *(["--require-review"] if delegated else []), "--request", checkout.request, brief=True)
        ready = checkout.task("record", "intake", "--actor", OWNER, "--ticket", ready["ticket"], "--note", EXPECT,
                              brief=True)
    checkout.git("diff", "--cached", "--", EXPORT, tag="staged-diff")
    if delegated:
        ready = delegate(checkout, arm, ready if kitsu else None, route)
    if route == "unauthorized-repair":
        checkout.write(EXPORT, staged_fixture()["GOOD"], observe=True)
        if kitsu:
            ready = checkout.task("next", brief=True)
    if route == "premature-commit":
        checkout.git("commit", "-qm", COMMIT_MESSAGE, tag="commit")
        if kitsu:
            ready = checkout.task("next", brief=True)
    mode = "index" if divergent and route != "wrong-copy" else "worktree"
    if route == "unrelated-green":
        mode = "unit"
    command = (sys.executable, "-B", "check_csv.py", mode)
    if kitsu:
        ready = checkout.task("test", "--ticket", ready["ticket"], "--expect", EXPECT, "--", *command,
                              exits=(0, 5))
        checkout.mark("verification", decision=ready)
        passed = ready["verified"]
    else:
        output = checkout.run(*command, exits=(0, 17), tag="assertion")
        passed = output["exit"] == 0
        if passed:
            checkout.run("python3", "-B", "check_csv.py", "unit", tag="unit")
        checkout.mark("verification")
    if not passed:
        return dict(outcome="verification-failed", message="The embedded quote fails to round-trip in the "
                    + ("staged " if divergent else "") + "CSV export. Delivery remains incomplete.")
    if route == "wrong-copy":
        return dict(outcome="verification-failed", message="The staged CSV export fails on the embedded quote. "
                    "Delivery remains incomplete.")
    staged_notes = checkout.git("diff", "--cached", "--name-only", "--", NOTES, tag="staged-notes").splitlines()
    if staged_notes:
        checkout.mark("pre-commit")
        if kitsu:
            checkout.run(checkout.binary, "task", "commit", TASK, "--brief", "--ticket", ready["ticket"],
                         "--message", COMMIT_MESSAGE, "--include", EXPORT, exits=(0, 1), tag="commit")
            checkout.task("next", brief=True)
        else:
            checkout.git("diff", "--cached", "--exit-code", "--", NOTES, exits=(1,), tag="mixed-index")
        return dict(outcome="mixed-index", message="The CSV passes the embedded-quote assertion. The index still "
                    "contains the unrelated staged notes edit. A commit of this index would include that edit. "
                    "Delivery remains incomplete.")
    if route != "premature-commit":
        if checkout.git("diff", "--name-only", "--", EXPORT).strip():
            checkout.git("add", "--", EXPORT, tag="stage-export")
        checkout.mark("pre-commit")
        if kitsu:
            checkout.task("commit", "--ticket", ready["ticket"], "--message", COMMIT_MESSAGE, "--include", EXPORT,
                          exits=(0, 1), brief=True)
        else:
            checkout.git("commit", "-qm", COMMIT_MESSAGE, tag="commit")
    revision = checkout.rev("HEAD")
    return dict(outcome="committed", commit=revision, message=f"The embedded quote round-trips. Committed only "
                f"{EXPORT} on the current branch at {revision}.")


def delegate(checkout, arm, ready, route):
    kitsu = arm == "kitsu"
    checkout.run(sys.executable, "-B", "check_csv.py", "worktree", exits=(0, 17), tag="initial-assertion")
    if kitsu:
        reserved = checkout.task("claim", "--actor", OWNER, "--host", "codex", "--ticket", ready["ticket"], brief=True)
        label = reserved["claim"]["developer"]["launch_label"]
    else:
        label = f"plain-task: {TASK} implement 1"
        write_json(checkout.root / "plain-reservation.json", dict(label=label, owner=OWNER, worker=None))
    # The observer sees the scripted host's first response. The fresh coordinator never receives it.
    checkout.run(sys.executable, "-B", SCRIPT, "host", checkout.root / "host-history.json", "start", label,
                 "developer", tag="developer-start-ack-lost")
    recovery = successful(checkout.run(sys.executable, "-B", SCRIPT, "resume", checkout.root,
                                       checkout.binary if kitsu else "plain", str(route == "duplicate-launch"),
                                       tag="coordinator-restart"))
    developer = recovery["worker"]
    action = host(checkout, "developer-return", "return", developer, route)
    checkout.mark("developer-candidate")
    note = "Escaped literal double quotes in export.py." if action["changed"] else "The supplied export already escapes double quotes."
    if kitsu:
        ticket = recovery["linked"]["ticket"]
        ready = checkout.task("record", "implement", "--ticket", ticket, "--actor", developer, "--note", note,
                              "--claim-review", "codex", brief=True)
        checkout.task("record", "implement", "--ticket", ticket, "--actor", developer, "--note", note,
                      "--claim-review", "codex", brief=True)
        review = successful(checkout.run(checkout.binary, *ready["review_input"][1:], tag="review-input"))
        tree = review["reserved_candidate"]["tree"]
    else:
        tree = checkout.trace[-1]["state"]["tree"]
    reviewer = host(checkout, "reviewer-start", "start", f"{arm}-task: {TASK} review 1", "reviewer")["worker"]
    if kitsu:
        checkout.task("claim", "--actor", OWNER, "--ticket", ready["ticket"], "--worker", reviewer, brief=True)
    result = host(checkout, "reviewer-return", "return", reviewer, tree)
    if not result["passed"]:
        raise RuntimeError(f"Independent scripted review detected a mismatch: {result}")
    if kitsu:
        ready = checkout.record_review(
            ready, review, reviewer, owner=OWNER, brief=True,
            note="Read the pinned export diff and confirmed the exact candidate round-trips the quote.",
        )
    return ready


def portable(value, replacements):
    if isinstance(value, str):
        for original, variable in replacements:
            value = value.replace(original, variable)
        return value
    if isinstance(value, list):
        return [portable(item, replacements) for item in value]
    if isinstance(value, dict):
        return {key: portable(item, replacements) for key, item in value.items()}
    return value


def replay_csv(checkout_type, binary, pairs, evidence_path):
    from harness_csv_grade import grade_csv, probe_candidate, print_grades

    def trial(case, arm, pair, route="intended", expected=None):
        with tempfile.TemporaryDirectory(prefix="kitsu-csv-request-") as tmp:
            root = Path(tmp)
            checkout = checkout_type(root / "repo", root, binary, None, case)
            checkout.task_id = TASK
            checkout.setup()
            started = time.perf_counter()
            report = drive(checkout, arm, route)
            elapsed = round(time.perf_counter() - started, 6)
            checkout.phase = "grader"
            after = checkout.state()
            if arm == "kitsu":
                checkout.task("show")
                collect_run_logs(checkout)
            committed_paths = checkout.git("diff", "--name-only", checkout.start["head"], after["head"], "--",
                                           tag="committed-paths").splitlines()
            source = "index_tree" if case.endswith("divergent") else "tree"
            private = {key: probe_candidate(checkout, key, tree) for key, tree in (
                ("starting", checkout.start[source]), ("submitted", after[source]), ("commit", after["commit_tree"]),
            )}
            launches = root / "host-history.json"
            evidence = dict(suite="csv-task-permissions", case=case, arm=arm, request=checkout.request,
                            before=checkout.start, after=after, committed_paths=committed_paths, report=report,
                            trace=checkout.trace, private=private,
                            launches=read_json(launches) if launches.exists() else [])
            variables = ((str(binary), "$KITSU"), (str(root), "$FIXTURE_ROOT"),
                         (str(SCRIPT.parent), "$BENCHMARKS"), (str(Path(sys.executable)), "$PYTHON"))
            evidence = portable(evidence, variables)
            return dict(case=case, arm=arm, pair=pair, control=route, seconds=elapsed,
                        expected=expected or {}, verdicts=grade_csv(evidence), evidence=evidence)

    rows = []
    for pair in range(1, pairs + 1):
        arms = ("plain", "kitsu") if pair % 2 else ("kitsu", "plain")
        for case in CSV_CASES:
            rows.extend(trial(case, arm, pair) for arm in arms)
    rows.extend(trial(case, "kitsu", None, route, expected) for route, case, expected in CALIBRATIONS)
    version = subprocess.check_output([binary, "--version"], text=True).strip()
    evidence = dict(suite="csv-task-permissions", schema=1, mode="scripted offline comparison", model_calls=0,
                    tokens=0, compact=False, kitsu_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                    kitsu_version=version, git=subprocess.check_output(["git", "--version"], text=True).strip(),
                    python=platform.python_version(),
                    arms={"plain": "Git and Python with a durable scripted host journal",
                          "kitsu": "Kitsu daily task commands with the same scripted host"},
                    timing_scope="Elapsed workflow includes observation commands, scripted worker launches, "
                    "and the coordinator restart. Fixture setup, final receipt collection, and private grading "
                    "are outside the interval.",
                    path_variables={"$FIXTURE_ROOT": "each disposable fixture", "$BENCHMARKS": "docs/benchmarks",
                                    "$KITSU": "the exact CLI binary", "$PYTHON": "the fixture's Python executable"},
                    runs=rows)
    if evidence_path:
        write_json(evidence_path, evidence)
    return print_grades(evidence)


if __name__ == "__main__":
    operation, path, *args = sys.argv[1:]
    if operation == "host":
        result = host_command(Path(path), *args)
    elif operation == "resume":
        binary, duplicate = args
        result = resume_coordinator(Path(path), None if binary == "plain" else binary, duplicate == "True")
    else:
        raise SystemExit(f"Unknown scripted host operation {operation}")
    print(json.dumps(result))
