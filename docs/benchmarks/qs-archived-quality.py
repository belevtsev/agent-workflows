"""Apply the same 19 qs behavior checks and unchanged suite to archived paid candidates."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

from qs_replay import CHECK, sandbox


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def archived_lib(receipt, work):
    record = json.loads(receipt.read_text())
    repo = receipt.with_suffix("")
    tree = record["candidate_tree"]
    if not re.fullmatch("[0-9a-f]{40}", tree):
        raise ValueError("invalid candidate tree in " + str(receipt))
    archive = subprocess.check_output(["git", "-C", str(repo), "archive", tree, "lib"])
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        for member in source.getmembers():
            if not (member.isfile() or member.isdir()):
                raise ValueError("unexpected candidate library member " + member.name)
        source.extractall(work, filter="data")
    return tree


def evaluate(args, name, source, control):
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        work = root / "work"
        for directory in (root / "home", root / "tmp", work / "node_modules"):
            directory.mkdir(parents=True)
        shutil.copy2(args.original / "package.json", work)
        shutil.copytree(args.original / "test", work / "test")
        tree = None
        if control:
            shutil.copytree(source / "lib", work / "lib")
        else:
            tree = archived_lib(source, work)
        (root / "check.sh").write_text(CHECK.replace("tail -3", "tail -7"))
        # This arms the common offline sandbox. The test never calls Kitsu.
        run = sandbox(root, work, args.modules, args.node, Path("/usr/bin/true"), args.grader)
        checked = run("sh", "/grader/check.sh", status=(0, 1))
        rows = [json.loads(line) for line in checked.stdout.splitlines() if line.startswith('{"id":')]
        assert len(rows) == len({row["id"] for row in rows}) == 19, checked.stderr
        failed = [row["id"] for row in rows if not row["passed"]]
        assert checked.returncode == bool(failed)
        suite = re.search(r"^# pass\s+(\d+)\s*$", checked.stdout, re.M)
        assert suite and "# ok" in checked.stdout
        return {"variant": name, "candidate_tree": tree, "unchanged_suite_passed": int(suite[1]),
                "behavior_passed": 19 - len(failed), "failed_behavior_ids": failed,
                "library_sha256": {p.name: sha(p) for p in sorted((work / "lib").glob("*.js"))}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("original", "reference", "managed", "daily", "grader", "node", "modules"):
        parser.add_argument("--" + key, type=Path, required=True)
    args = parser.parse_args()
    assert sha(args.grader) == "655503ff0383478c653590850ce9440b0c40d650829689b11174964495a52c63"
    variants = [("original", args.original, True), ("upstream_fix", args.reference, True),
                ("plain_codex", args.managed / "qs-sparse-array-limit-r1-codex.json", False),
                ("managed_kitsu", args.managed / "qs-sparse-array-limit-r1-kitsu.json", False),
                ("daily_kitsu", args.daily / "qs-kitsu.json", False)]
    rows = [evaluate(args, *variant) for variant in variants]
    assert rows[0]["behavior_passed"] < 19 and rows[1]["behavior_passed"] == 19
    print(json.dumps({"model_calls": 0, "grader_sha256": sha(args.grader),
                      "suite_files_sha256": {p.name: sha(p) for p in sorted((args.original / "test").glob("*.js"))},
                      "results": rows}, indent=2))


if __name__ == "__main__":
    main()
