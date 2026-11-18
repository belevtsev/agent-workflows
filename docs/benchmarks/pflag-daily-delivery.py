"""Replay the archived pflag patch through plain Git and daily Kitsu without model calls."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import tempfile
import time


BASE = "4672a8345aca54a159fe9c95dc89d9e306a275ac"
TREE = "6c602f6ee830267ed161d15387e299e932b02950"
ORACLE = "32ddc28eda2ce8d734260ab8aedd2321adde337a1e8aed06c23cbc939e924526"
ACCEPTANCE = {"TestPilotUnknownFlagStopsNonInterspersed", "TestPilotInterspersedPassthroughStillParsesKnown"}
TEST = "TestUnknownFlagPassthrough"
ENV = dict(os.environ, GOWORK="off", GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", GOFLAGS="-count=1",
           GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull, GIT_AUTHOR_NAME="Offline replay",
           GIT_COMMITTER_NAME="Offline replay", GIT_AUTHOR_EMAIL="replay@example.invalid",
           GIT_COMMITTER_EMAIL="replay@example.invalid")


def execute(cwd, argv, status=0):
    result = subprocess.run(argv, cwd=cwd, env=ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
    if result.returncode != status:
        raise RuntimeError((argv, result.returncode, result.stderr[-2000:], result.stdout[-500:]))
    return result.stdout


def grade(work, grader, expected_failures):
    overlay = work.parent / "grade.json"
    overlay.write_text(json.dumps({"Replace": {str(work / "kitsu_pilot_acceptance_test.go"): str(grader)}}))
    raw = execute(work, ["go", "test", "-overlay", str(overlay), "-tags", "pilot_acceptance", "-run", "^TestPilot", "-json", "."],
                  status=int(bool(expected_failures)))
    events = [json.loads(line) for line in raw.splitlines()]
    results = {e["Test"]: e["Action"] for e in events if e.get("Test") in ACCEPTANCE and e["Action"] in ("fail", "pass")}
    expected = {name: "fail" if name in expected_failures else "pass" for name in ACCEPTANCE}
    if results != expected:
        raise ValueError(f"Unexpected acceptance results: {results}")
    return results


def replay(root, arm, source, binary, grader, blobs, newline_git_dir=False):
    work = root / "repo"
    separate = ["--separate-git-dir", str(root / "two\nlines.git")] if newline_git_dir else []
    execute(root, ["git", "clone", "--local", "--no-checkout", "-q", *separate, str(source), str(work)])
    execute(work, ["git", "checkout", "-q", "--detach", BASE])
    actions = []
    ticket = None

    def run(label, argv, status=0):
        before = time.monotonic()
        out = execute(work, argv, status)
        actions.append({"action": label, "seconds": round(time.monotonic() - before, 6)})
        return out

    def task(verb, args, next_action):
        nonlocal ticket
        argv = [str(binary), "task", verb, "passthrough", "--brief"]
        if ticket:
            argv.extend(["--ticket", ticket])
        state = json.loads(run("task " + verb, argv + args))
        if state["next"] != next_action or state.get("claim_created") or state.get("claim"):
            raise ValueError(f"Unexpected {verb} result: {state}")
        ticket = state.get("ticket")
        return state

    start = time.monotonic()
    if arm == "kitsu":
        task("open", ["--entry", "implement", "--until", "deliver", "--commit", "--request",
                      "Fix unknown flag passthrough, verify and commit."], "intake")
        task("record", ["intake", "--actor", "offline", "--note", "Preserve trailing arguments and existing interspersed behavior."], "implement")
    (work / "flag_test.go").write_bytes(blobs["flag_test.go"])
    red = run("regression on baseline", ["go", "test", "-json", "-run", "^" + TEST + "$", "."], 1)
    if not any(e.get("Test") == TEST and e["Action"] == "fail" for e in map(json.loads, red.splitlines())):
        raise ValueError("The named regression did not fail")
    (work / "flag.go").write_bytes(blobs["flag.go"])
    run("check diff whitespace", ["git", "diff", "--check"])
    if arm == "kitsu":
        task("record", ["implement", "--actor", "offline", "--note", "Applied the archived candidate and its regression."], "verify")
        verified = task("test", ["--expect", "Unknown flags preserve trailing arguments; full suite passes", "--", "go", "test", "./..."], "commit")
        if not verified["verified"] or verified["repository_checks"] != "passed":
            raise ValueError("Missing current verification")
    else:
        run("verify", ["go", "test", "./..."])
    run("stage", ["git", "add", "--", "flag.go", "flag_test.go"])
    if arm == "kitsu":
        delivered = task("commit", ["--message", "Fix unknown flag passthrough"], "done")
        if delivered["delivery"] != "committed":
            raise ValueError("Task ended without the requested commit")
    else:
        run("commit", ["git", "commit", "-qm", "Fix unknown flag passthrough"])
    seconds = time.monotonic() - start
    actual = execute(work, ["git", "show", "-s", "--format=%T %P", "HEAD"]).decode().split()
    if actual != [TREE, BASE] or execute(work, ["git", "status", "--porcelain"]):
        raise ValueError("Delivery produced a different tree, parent or dirty checkout")
    grade(work, grader, set())
    if arm == "kitsu":
        ledger = json.loads(execute(work, [str(binary), "task", "show", "passthrough", "--json"]))
        if ledger.get("review_starts", 0) or [event["action"] for event in ledger["events"]] != ["intake", "implement", "test", "commit"]:
            raise ValueError("Unexpected Kitsu workflow history")
    return {"arm": arm, "seconds": round(seconds, 6), "commands": len(actions), "actions": actions,
            "clean_commit": True, "acceptance_passed": len(ACCEPTANCE), "regression_failed_on_base": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("source", "kitsu", "grader"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--newline-git-dir", action="store_true", help="Keep the Go source in an ordinary path and put Git metadata in a path containing a newline")
    args = parser.parse_args()
    source, binary, grader = (v.resolve(strict=True) for v in (args.source, args.kitsu, args.grader))
    if hashlib.sha256(grader.read_bytes()).hexdigest() != ORACLE:
        raise ValueError("Grader differs from the frozen pflag pair")
    blobs = {f: execute(source, ["git", "show", TREE + ":" + f]) for f in ("flag.go", "flag_test.go")}
    with tempfile.TemporaryDirectory() as dirname:
        root = Path(dirname)
        calibration = root / "baseline"
        execute(root, ["git", "clone", "--local", "--no-checkout", "-q", str(source), str(calibration)])
        execute(calibration, ["git", "checkout", "-q", "--detach", BASE])
        baseline_results = grade(calibration, grader, {"TestPilotUnknownFlagStopsNonInterspersed"})
    rows = []
    for pair in range(5):
        for arm in (("plain", "kitsu") if pair % 2 == 0 else ("kitsu", "plain")):
            with tempfile.TemporaryDirectory() as dirname:
                result = replay(Path(dirname), arm, source, binary, grader, blobs, args.newline_git_dir)
                if pair:
                    rows.append(dict(pair=pair, **result))
    medians = {arm: statistics.median(r["seconds"] for r in rows if r["arm"] == arm) for arm in ("plain", "kitsu")}
    print(json.dumps(dict(base=BASE, tree=TREE, grader_sha256=ORACLE, binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                          go=execute(source, ["go", "version"]).decode().strip(), model_calls=0, compact=False,
                          baseline_acceptance=baseline_results, newline_git_dir=args.newline_git_dir, median_seconds=medians, runs=rows), indent=2))


if __name__ == "__main__":
    main()
