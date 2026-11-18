"""Compare scripted first-commit delivery in plain Git and Kitsu. Requires Git and Python 3."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import time

BAD = 'def fields(line):\n    return line.split(",")\n'
GOOD = 'import csv\n\ndef fields(line):\n    return next(csv.reader([line]))\n'
TEST = '''import unittest
from csv_fields import fields

class FieldsTest(unittest.TestCase):
    def test_quoted_comma(self):
        self.assertEqual(fields('id,"Doe, Jane"'), ["id", "Doe, Jane"])
'''
FILES = {".gitignore", "csv_fields.py", "test_csv_fields.py"}


def check_session_receipt(binary, arm, work, run):
    # A controlled whole-session receipt spans earlier work and the CSV request.
    # All task allocation information still has to come from the shipped CLI.
    events = [{"type": "turn_context", "payload": {"model": "offline-fixture"}}]
    for request, total in [("Earlier work in this session", 1), ("Preserve quoted fields", 2)]:
        events.append({"type": "response_item", "payload": {"type": "message", "role": "user",
                       "content": [{"type": "input_text", "text": request}]}})
        last = {"input_tokens": 500, "cached_input_tokens": 250, "output_tokens": 25}
        events.append({"type": "event_msg", "payload": {"type": "token_count", "info": {
            "last_token_usage": last, "total_token_usage": {k: v * total for k, v in last.items()}}}})
    path = work / ".git" / "offline-session.jsonl"
    path.write_text("".join(json.dumps(event) + "\n" for event in events))
    counts = json.loads(path.read_text().splitlines()[-1])["payload"]["info"]["total_token_usage"]
    measured = counts["input_tokens"] + counts["output_tokens"]
    if arm == "kitsu":
        notice = {"session_id": "offline", "transcript_path": str(path), "cwd": str(work)}
        run(binary, "usage", "record", "--host", "codex", stdin=json.dumps(notice))
        for args, key in [(("usage", "show", "--task", "first-csv"), "total"),
                          (("task", "next", "first-csv"), "captured_tokens")]:
            report = json.loads(run(binary, *args, "--json").stdout)
            rows = report.get("actor_session_usage", [])
            if (report[key] is not None or len(rows) != 1 or rows[0]["session"] != "offline"
                    or rows[0]["total"] != measured):
                raise ValueError("daily task lost or misattributed its actor's whole-session receipt")
    return {"fixture_session_tokens": measured, "task_attribution": "unknown"}


def replay(binary, arm, work, session_receipt=False):
    env = {k: v for k, v in os.environ.items() if not k.startswith(("GIT_", "PYTHON"))}
    env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull, GIT_AUTHOR_NAME="Offline",
               GIT_COMMITTER_NAME="Offline", GIT_AUTHOR_EMAIL="offline@example.invalid", GIT_COMMITTER_EMAIL="offline@example.invalid")

    def run(*args, code=0, stdin=None):
        result = subprocess.run(args, cwd=work, env=env, input=stdin, capture_output=True, text=True, timeout=30)
        if result.returncode != code:
            raise ValueError(f"{args}: {result.returncode}\n{result.stderr}\n{result.stdout}")
        return result

    def task(verb, *args, code=0):
        return json.loads(run(binary, "task", verb, "first-csv", "--brief", *args, code=code).stdout)

    run("git", "init", "--quiet", "-b", "main")
    started = time.monotonic()
    if arm == "kitsu":
        opened = task("open", "--entry", "implement", "--until", "deliver", "--commit", "--request",
                      "Preserve quoted commas and commit the first version with a regression test.")
        intake = task("record", "intake", "--ticket", opened["ticket"], "--actor", "offline", "--note", "Preserve the quoted field.")
    (work / "test_csv_fields.py").write_text(TEST)
    (work / "csv_fields.py").write_text(BAD)
    if "FAIL: test_quoted_comma" not in run(sys.executable, "-B", "-m", "unittest", code=1).stderr:
        raise ValueError("expected the regression to fail on the faulty implementation")
    (work / "csv_fields.py").write_text(GOOD)
    suite = [sys.executable, "-m", "unittest"]
    if arm == "kitsu":
        implemented = task("record", "implement", "--ticket", intake["ticket"], "--actor", "offline", "--note", "Use the CSV reader")
        def verify(ticket, code=0):
            return task("test", "--ticket", ticket, "--expect", "quoted commas survive parsing", "--", *suite, code=code)
        first = verify(implemented["ticket"], code=5)
        resumed = task("next")
        receipt = first["verification_log"]
        if (first["verified"] or receipt["exit_status"] != 0 or receipt["outcome"] != "source_changed"
                or "__pycache__/" not in receipt["detail"] or resumed["verification_log"] != receipt):
            raise ValueError("passing process lost its source-change diagnostic")
    else:
        run(*suite)
        if "__pycache__/" not in run("git", "ls-files", "--others", "--exclude-standard").stdout:
            raise ValueError("expected generated output to be visible in the plain checkout")
    (work / ".gitignore").write_text("__pycache__/\n")
    if arm == "kitsu":
        verified = verify(task("next")["ticket"])
        if not verified["verified"] or verified["next"] != "commit" or "detail" in verified["verification_log"]:
            raise ValueError("verification did not recover")
    else:
        run(*suite)
    run("git", "add", "--", *sorted(FILES))
    if arm == "kitsu":
        done = task("commit", "--ticket", verified["ticket"], "--message", "Preserve quoted fields")
        if done["next"] != "done" or done["delivery"] != "committed":
            raise ValueError("missing commit receipt")
    else:
        run("git", "commit", "--quiet", "-m", "Preserve quoted fields")
    seconds = time.monotonic() - started
    if (run("git", "rev-list", "--count", "HEAD").stdout.strip() != "1" or run("git", "status", "--porcelain").stdout
            or set(run("git", "ls-tree", "--name-only", "HEAD").stdout.splitlines()) != FILES):
        raise ValueError("expected a clean root commit containing only the agreed files")
    run(sys.executable, "-B", "-c", """from csv_fields import fields
assert fields('001,"a,b",""') == ['001', 'a,b', '']""")
    receipt = check_session_receipt(binary, arm, work, run) if session_receipt else {}
    return dict(**receipt, arm=arm, seconds=round(seconds, 6), tree=run("git", "rev-parse", "HEAD^{tree}").stdout.strip(),
                baseline_regression_failed=True, clean_root_commit=True, independent_case_passed=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kitsu", type=Path, required=True)
    parser.add_argument("--session-receipt", action="store_true", help="check a controlled whole-session receipt after delivery")
    args = parser.parse_args()
    binary = args.kitsu.resolve(strict=True)
    rows = []
    for pair in range(5):
        for arm in (("plain", "kitsu") if pair % 2 == 0 else ("kitsu", "plain")):
            with tempfile.TemporaryDirectory() as root:
                result = replay(binary, arm, Path(root), args.session_receipt)
                if pair:
                    rows.append(dict(pair=pair, **result))
    if len({row["tree"] for row in rows}) != 1:
        raise ValueError("arms committed different trees")
    print(json.dumps(dict(model_calls=0, compact=False, python=sys.version.split()[0], binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                          median_seconds={a: statistics.median(r["seconds"] for r in rows if r["arm"] == a) for a in ("plain", "kitsu")},
                          runs=rows), indent=2))
