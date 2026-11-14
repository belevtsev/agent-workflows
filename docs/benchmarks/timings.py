"""Times cw against the size of a repository.

Usage: python3 timings.py <cw> <work-dir> <out.json> [<files>...]

<cw> is the program to measure. <work-dir> is an empty directory for the generated repositories;
remove it afterwards. Without sizes it measures 100, 1,000, 10,000, 30,000 and 100,000 files.

For each size it generates a repository of that many files of 1 KiB, declares one trivial check
(`git --version`), records it, and takes the median of 11 calls after one warm-up of:

  git status       `git status --porcelain`, as a yardstick
  cw status        `cw status` on verified content
  commit, plain    `git commit --allow-empty` with the hooks turned off for that call
  commit, gated    `git commit --allow-empty` through the pre-commit hook of cw, which allows
  cw run, no-op    `cw run` when every check has a passing run on the content
  cw run, 1 check  `cw run` after one file changed: it runs `git --version` and records the result

It needs git and Python 3 and nothing else. docs/benchmarks.md holds the numbers of one run.
"""
import json
import os
import statistics
import subprocess
import sys
import time

cw_dir, work, out = os.path.dirname(os.path.abspath(sys.argv[1])), sys.argv[2], sys.argv[3]
sizes = [int(s) for s in sys.argv[4:]] or [100, 1000, 10000, 30000, 100000]
home = os.path.join(work, "home")
os.makedirs(home, exist_ok=True)
env = {"PATH": cw_dir + ":/usr/bin:/bin", "HOME": home, "GIT_CONFIG_NOSYSTEM": "1", "LC_ALL": "C",
       "GIT_AUTHOR_NAME": "Test User", "GIT_AUTHOR_EMAIL": "test@example.invalid",
       "GIT_COMMITTER_NAME": "Test User", "GIT_COMMITTER_EMAIL": "test@example.invalid"}
RUNS, WARMUPS = 11, 1


def run(args, cwd, check=True):
    done = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and done.returncode != 0:
        raise SystemExit("%s exited %d in %s\n%s%s" % (args, done.returncode, cwd, done.stdout, done.stderr))
    return done


def timed(args, cwd, before=None):
    values = []
    for number in range(WARMUPS + RUNS):
        if before:
            before(number)
        started = time.perf_counter()
        run(args, cwd)
        if number >= WARMUPS:
            values.append((time.perf_counter() - started) * 1000)
    return {"median_ms": round(statistics.median(values), 1), "min_ms": round(min(values), 1),
            "max_ms": round(max(values), 1)}


def generate(repo, count):
    block = ("line of generated content number %06d\n" * 26)[:1024]
    for number in range(count):
        directory = os.path.join(repo, "d%03d" % (number % 200), "s%02d" % (number // 200 % 50))
        os.makedirs(directory, exist_ok=True)
        with open(os.path.join(directory, "f%06d.txt" % number), "w") as file:
            file.write("%06d\n" % number + block[: 1024 - 7])


results = {"cw": run(["cw", "version"], work).stdout.split("\n")[:2], "git": run(["git", "--version"], work).stdout.strip(),
           "runs": RUNS, "warmups": WARMUPS, "sizes": {}}
for count in sizes:
    repo = os.path.join(work, "repo-%d" % count)
    os.makedirs(repo)
    generate(repo, count)
    run(["git", "init", "-q", "-b", "main", "."], repo)
    run(["git", "add", "-A"], repo)
    run(["git", "commit", "-q", "-m", "generated"], repo)
    run(["cw", "init"], repo)
    run(["cw", "add", "trivial", "--", "git", "--version"], repo)
    run(["cw", "run"], repo)
    run(["git", "add", "-A"], repo)
    run(["git", "commit", "-q", "-m", "declare the check"], repo)
    row = {}
    row["git status"] = timed(["git", "status", "--porcelain"], repo)
    row["cw status"] = timed(["cw", "status"], repo)
    row["commit, plain"] = timed(["git", "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "x"], repo)
    row["commit, gated"] = timed(["git", "commit", "-q", "--allow-empty", "-m", "x"], repo)
    row["cw run, no-op"] = timed(["cw", "run"], repo)

    def change(number, repo=repo):
        with open(os.path.join(repo, "d000", "s00", "f000000.txt"), "a") as file:
            file.write("change %d\n" % number)

    row["cw run, 1 check"] = timed(["cw", "run"], repo, before=change)
    results["sizes"][count] = row
    print(count, json.dumps(row), flush=True)
    json.dump(results, open(out, "w"), indent=1)
