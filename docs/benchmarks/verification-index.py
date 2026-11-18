"""Compare a conditional local delivery with plain Git, PR 239, and PR 240."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
import zipfile

SCRIPT = ".github/scripts/package-plugin.py"
GUIDE = "skills/task/references/managed.md"
SKILL = "skills/task/SKILL.md"
CASES = ["stage-first", "restage-good", "restage-broken"]
ARMS = ["plain", "239", "240"]


def run(args, cwd, check=True):
    p = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=45)
    if check and p.returncode:
        raise RuntimeError((args, p.returncode, p.stdout, p.stderr))
    return p


def packaged(directory):
    with tempfile.TemporaryDirectory() as temp:
        archive = Path(temp) / "plugin.zip"
        run([sys.executable, SCRIPT, archive], directory)
        with zipfile.ZipFile(archive) as package:
            entries = set(package.namelist())
            return all("kitsu-marketplace/" + path in entries and
                       package.read("kitsu-marketplace/" + path) == (Path(directory) / path).read_bytes()
                       for path in (GUIDE, SKILL))


def check_index():
    with tempfile.TemporaryDirectory() as temp:
        run(["git", "checkout-index", "--all", "--prefix=" + temp + os.sep], Path.cwd())
        return packaged(Path(temp))


def trial(src, binaries, root, case, arm, iteration):
    checkout = root / f"{iteration}-{case}-{arm}"
    checkout.mkdir()
    for item in (".agents/plugins", ".claude-plugin", ".codex-plugin", "hooks", "docs/brand", "agents", "skills"):
        shutil.copytree(src / item, checkout / item)
    for item in ("LICENSE", "docs/verification.md", SCRIPT):
        path = checkout / item
        path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(src / item, path)

    def git(*args):
        return run(["git", *args], checkout).stdout.strip()

    def task(action, *args, check=True):
        p = run([binaries[arm], "task", action, "package", "--brief", *args], checkout, check)
        return p.returncode, json.loads(p.stdout)

    git("init", "-q", "-b", "main", "--template=")
    packager = checkout / SCRIPT
    good = packager.read_text()
    assert good.count("skills/**/*.md") == 1
    bad = good.replace("skills/**/*.md", "skills/*/SKILL.md")
    packager.write_text(bad)
    git("add", ".")
    git("commit", "-qm", "Baseline packager")
    base = git("rev-parse", "HEAD")
    packager.write_text(good)
    git("add", SCRIPT)
    if case == "restage-good":
        packager.write_text(good + "\n# Package nested references.\n")
    if case == "restage-broken":
        # Keep an actual diff to commit so an empty-commit refusal cannot mask a false pass.
        packager.write_text(bad + "\n# Package nested references.\n")
    assertion = [sys.executable, "-B", str(Path(__file__).resolve()), "--check-index"]
    start = time.perf_counter()
    if arm == "plain":
        checked = git("write-tree")
        run(assertion, checkout)
    else:
        _, state = task("open", "--entry", "verify", "--until", "deliver", "--commit", "--request",
                        "Commit the packager only if the staged plugin contains the task skill and its guide.")
        _, state = task("record", "intake", "--ticket", state["ticket"], "--actor", "developer", "--note",
                        "Build the staged marketplace. Match the task skill and managed guide to their sources.")
        _, state = task("test", "--ticket", state["ticket"], "--expect", "Staged plugin includes both skill files", "--", *assertion)
        assert state["verified"] and state["next"] == "commit"
    git("add", SCRIPT)
    if arm == "plain":
        checked_again = checked != git("write-tree")
        code = run(assertion, checkout, check=False).returncode if checked_again else 0
        if code == 0:
            git("commit", "-qm", "Deliver plugin packager")
        assertions = 1 + int(checked_again)
    else:
        code, state = task("commit", "--ticket", state["ticket"], "--include", SCRIPT, "--message", "Deliver plugin packager", check=False)
        assert code in (0, 5), state
    elapsed = time.perf_counter() - start
    if arm != "plain":
        history = json.loads(run([binaries[arm], "task", "show", "package", "--json"], checkout).stdout)
        assertions = sum(event["action"] == "test" for event in history["events"])
    head, index = git("rev-parse", "HEAD"), git("write-tree")
    delivered = head != base
    contents_pass = None
    if delivered:
        fresh = root / (checkout.name + "-committed")
        git("worktree", "add", "-q", "--detach", str(fresh), head)
        contents_pass = packaged(fresh)
        assert git("diff", "--name-only", base, head) == SCRIPT
    intended = case != "restage-broken"
    return dict(case=case, arm=arm, iteration=iteration, seconds=elapsed, checks=assertions, base=base, head=head,
                index=index, committed=delivered, committed_behavior=contents_pass,
                command_exit=code, matches_request=delivered == intended and (not delivered or contents_pass))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("src", type=Path)
    parser.add_argument("old", type=Path)
    parser.add_argument("new", type=Path)
    parser.add_argument("out", type=Path)
    args = parser.parse_args()
    binaries = {"239": args.old.resolve(), "240": args.new.resolve()}
    provenance = {}
    for arm, path in binaries.items():
        provenance[arm] = {
            "identity": run([path, "version"], args.src).stdout.splitlines(),
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        }
    # Each arm must use the disposable repository, even from a Git hook or worktree.
    for key in list(os.environ):
        if key.startswith("GIT_"):
            del os.environ[key]
    with tempfile.TemporaryDirectory(prefix="kitsu-stage-comparison-") as tmp:
        root = Path(tmp)
        os.environ.update(
            HOME=tmp, XDG_CONFIG_HOME=str(root / "config"), GIT_CONFIG_NOSYSTEM="1",
            GIT_CONFIG_GLOBAL=os.devnull, GIT_AUTHOR_NAME="Replay",
            GIT_AUTHOR_EMAIL="replay@example.invalid", GIT_COMMITTER_NAME="Replay",
            GIT_COMMITTER_EMAIL="replay@example.invalid", GIT_AUTHOR_DATE="2026-01-01T00:00:00Z",
            GIT_COMMITTER_DATE="2026-01-01T00:00:00Z",
        )
        results = []
        for i in range(len(ARMS)):
            for case in CASES:
                for arm in ARMS[i:] + ARMS[:i]:
                    results.append(trial(args.src.resolve(), binaries, root, case, arm, i + 1))

    summary = []
    for case in CASES:
        rows = [r for r in results if r["case"] == case]
        assert len({r["base"] for r in rows}) == len({r["index"] for r in rows}) == 1
        if case != "restage-broken":
            assert len({r["head"] for r in rows}) == 1
        for row in rows:
            baseline_false_pass = case == "restage-broken" and row["arm"] == "239"
            assert row["matches_request"] == (not baseline_false_pass), row
            if baseline_false_pass:
                assert row["committed"] and row["committed_behavior"] is False, row
        for arm in ARMS:
            arm_rows = [r for r in rows if r["arm"] == arm]
            summary.append(dict(
                case=case, arm=arm, accepted=sum(r["matches_request"] for r in arm_rows),
                attempts=len(arm_rows), assertions=[r["checks"] for r in arm_rows],
                median_seconds=round(statistics.median(r["seconds"] for r in arm_rows), 3),
            ))

    report = dict(host=platform.platform(), source=run(["git", "rev-parse", "HEAD"], args.src).stdout.strip(),
                  binaries=provenance, model_calls=0, compact=False, results=results, summary=summary)
    args.out.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(summary))


if __name__ == "__main__":
    if sys.argv[1:] == ["--check-index"]:
        sys.exit(0 if check_index() else 1)
    main()
