"""Offline paired replay. Usage: python3 daily-review-overhead.py /path/to/kitsu > runs.csv"""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binary", type=Path)
parser.add_argument("--review-settings", action="store_true", help="compare separate host lookups with a reviewer reservation")
parser.add_argument("--claim-with-result", action="store_true", help="compare a separate reservation with one made while recording implementation")
parser.add_argument("--review-policy", action="store_true", help="compare plain shell, default daily implementation, and explicitly reviewed implementation")
options = parser.parse_args()
if options.review_policy and (options.review_settings or options.claim_with_result):
    parser.error("choose one comparison")
if options.claim_with_result:
    options.review_settings = True
BINARY = str(options.binary.resolve(strict=True))
BASE = 'package label\nimport "strings"\nfunc Clean(s string) string { return strings.ToLower(s) }\n'
FIX = BASE.replace('strings.ToLower(s)', 'strings.ToLower(strings.TrimSpace(s))')
TEST = '''package label
import "testing"
func TestClean(t *testing.T) {
    if Clean(" ÉCOLE ") != "école" { t.Fatal("spaces and upper case") }
}
'''
ENV = dict(os.environ, GOWORK="off", GOPROXY="off", GOSUMDB="off",
           GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull, GIT_AUTHOR_NAME="Benchmark",
           GIT_COMMITTER_NAME="Benchmark", GIT_AUTHOR_EMAIL="benchmark@example.invalid",
           GIT_COMMITTER_EMAIL="benchmark@example.invalid")


def replay(root, mode):
    root.mkdir()
    env = ENV.copy()
    if options.review_settings:
        # Share the Go build cache across arms but isolate personal role and skill policy.
        env["GOCACHE"] = subprocess.check_output(["go", "env", "GOCACHE"], text=True).strip()
        home = root.parent / "user"
        home.mkdir(exist_ok=True)
        env.update(HOME=str(home), USERPROFILE=str(home), CODEX_HOME=str(home / ".codex"),
                   XDG_CONFIG_HOME=str(home / ".config"), CLAUDE_CONFIG_DIR=str(home / ".claude"))
    calls = 0
    output_bytes = 0
    ticket = ""

    def run(*argv, expected=0):
        nonlocal calls, output_bytes
        result = subprocess.run(argv, cwd=root, env=env, capture_output=True, timeout=90)
        calls += 1
        output_bytes += len(result.stdout) + len(result.stderr)
        if result.returncode != expected:
            raise RuntimeError((argv, result.returncode, result.stderr.decode(), result.stdout.decode()))
        return result.stdout

    def task(verb, *args, next_action):
        nonlocal ticket
        output = run(BINARY, "task", verb, "clean", "--brief" if mode in ("direct", "brief", "separate", "bundled", "combined") else "--json",
                     *(["--ticket", ticket] if verb != "open" else []), *args)
        state = json.loads(output)
        assert state["next"] == next_action, state
        ticket = state.get("ticket", "")
        return state

    (root / "go.mod").write_text("module example.test/label\n\ngo 1.22\n")
    (root / "label.go").write_text(BASE)
    run("git", "init", "-q")
    if options.review_settings:
        skill = root / ".agents/skills/verify/SKILL.md"
        skill.parent.mkdir(parents=True)
        skill.write_text("---\nname: verify\ndescription: Check label normalization.\n---\nRead the regression and verify its output.\n")
        run(BINARY, "role", "set", "reviewer", "--host", "codex", "--model", "offline-reviewer",
            "--reasoning", "high", "--skills", "verify")
    run("git", "add", ".")
    run("git", "commit", "-qm", "Baseline")
    calls = output_bytes = 0
    start = time.perf_counter()
    if mode != "plain":
        task("open", "--entry", "implement", "--until", "deliver", "--commit",
             *([] if mode == "direct" else ["--require-review"]),
             "--request", "Normalize spaces and Unicode case in labels", next_action="intake")
        task("record", "intake", "--actor", "host", "--note", "Trim and lower-case Unicode", next_action="implement")
    (root / "label_test.go").write_text(TEST)
    assert b"spaces and upper case" in run("go", "test", "-count=1", "./...", expected=1)
    (root / "label.go").write_text(FIX)
    # Both replay arms inspect the same diff and run the same test and commit.
    run("git", "diff", "--", "label.go")
    if mode == "plain":
        run("go", "test", "-count=1", "./...")
        run("git", "add", "label.go", "label_test.go")
        run("git", "commit", "-qm", "Normalize labels")
    else:
        result = task("record", "implement", "--actor", "host", "--note", "Apply requested normalization",
                      *(["--claim-review", "codex"] if mode == "combined" else []), next_action="verify" if mode == "direct" else "review")
        if mode == "separate":
            selected = json.loads(run(BINARY, "role", "show", "reviewer", "--host", "codex", "--json"))
            resolved = json.loads(run(BINARY, "skill", "resolve", "reviewer", "--host", "codex", "--json"))
            model, skills = selected["models"]["codex"], resolved["skills"]
        if mode == "combined":
            assert result.get("claim_created")
            reservation = result
        elif mode != "direct":
            reservation = task("claim", "--actor", "host", *(["--host", "codex"] if mode == "bundled" else []),
                               next_action="review")
        if mode in ("combined", "bundled"):
            settings = reservation["claim"]["reviewer"]
            model, skills = settings["model"], settings["skills"]
        if options.review_settings:
            assert model == {"model": "offline-reviewer", "effort": "high"}
            assert len(skills) == 1 and Path(skills[0]["path"]) == skill
            assert hashlib.sha256(skill.read_bytes()).hexdigest() == skills[0]["sha256"]

        if mode != "direct":
            task("record", "review", "--actor", "reviewer", "--note", "Inspected diff and regression case", next_action="verify")
        task("test", "--expect", "Trim spaces and preserve Unicode lower-case", "--",
             "go", "test", "-count=1", "./...", next_action="commit")
        run("git", "add", "label.go", "label_test.go")
        assert task("commit", "--message", "Normalize labels", next_action="done")["verified"]
    elapsed = time.perf_counter() - start
    measured = (round(elapsed, 6), calls, output_bytes)
    assert not run("git", "status", "--porcelain")
    return (*measured, run("git", "rev-parse", "HEAD^{tree}").decode().strip())


if __name__ == "__main__":
    writer = csv.writer(sys.stdout)
    writer.writerow(["repeat", "arm", "seconds", "commands", "response_bytes", "tree"])
    modes = ["plain", "separate", "bundled"] if options.review_settings else ["plain", "full", "brief"]
    if options.review_policy:
        modes = ["plain", "direct", "brief"]
    if options.claim_with_result:
        modes = ["plain", "bundled", "combined"]
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory)
        trees = set()
        for repeat in range(11):
            for index in range(3):
                mode = modes[(repeat + index) % 3]
                result = replay(path / f"{repeat}-{mode}", mode)
                trees.add(result[-1])
                if repeat:
                    writer.writerow([repeat, mode, *result])
        assert len(trees) == 1, trees
