"""Offline plain/Kitsu verify-only comparison on archived Masterminds/semver releases."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tarfile
import tempfile
import time

ARCHIVES = {
    "base": ("ebda872fa10e6cc730c5973bbdb512b9511eb65e",
             "0fbb32d1dd3014c0889a411014066661731e49f4499b79369f41cecd25bfed61"),
    "gold": ("057c901b9979a2a6b8e06adfbf96349a641ba2e1",
             "629d0559830d1bbac3c7f47d5fa5f5b4e0333a619d1d4c8e4052bea68b469bf4"),
}
REQUEST = "Check whether prerelease admission stays within the matching constraint group. Report the result and leave the source alone."
# Exercise the exported API from outside the repository. Print every observation before failing.
PROBE = '''package main
import ("fmt"; "os"; semver "github.com/Masterminds/semver/v3")
func main() {
    const groups = ">= 1.0.0 <= 2.0.0-beta || > 3"
    failed := false
    for _, c := range []struct{ version string; want bool }{
        {"1.0.1-beta", true}, {"1.0.1", true}, {"3.0.1-beta", false}, {"4.0.0", true},
    } {
        constraint, err := semver.NewConstraint(groups)
        if err != nil { panic(err) }
        got := constraint.Check(semver.MustParse(c.version))
        fmt.Printf("%s %t %t\\n", c.version, got, c.want)
        if got != c.want { failed = true }
    }
    if failed { os.Exit(1) }
}
'''


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(cwd, env, *args, code=0):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, timeout=120)
    if result.returncode != code:
        raise RuntimeError((args, result.returncode, result.stdout, result.stderr))
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kitsu", type=Path)
    parser.add_argument("archives", type=Path, help="directory with the pinned base.tar.gz and gold.tar.gz")
    parser.add_argument("--pairs", type=int, default=3)
    args = parser.parse_args()
    if args.pairs <= 0:
        parser.error("pairs must be positive")
    cli = str(args.kitsu.resolve(strict=True))
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        env = dict(os.environ, GOWORK="off", GOTOOLCHAIN="local", GOPROXY="off", GOFLAGS="-mod=readonly",
                   GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1", GIT_AUTHOR_NAME="Replay",
                   GIT_AUTHOR_EMAIL="replay@example.invalid", GIT_COMMITTER_NAME="Replay",
                   GIT_COMMITTER_EMAIL="replay@example.invalid", GIT_AUTHOR_DATE="2025-01-01T00:00:00Z",
                   GIT_COMMITTER_DATE="2025-01-01T00:00:00Z", HOME=str(root / "home"),
                   GOCACHE=subprocess.check_output(["go", "env", "GOCACHE"], text=True).strip())
        (root / "home").mkdir()
        probe = root / "check.go"
        probe.write_text(PROBE)
        sources = {}
        for key, (revision, expected) in ARCHIVES.items():
            archive = args.archives / (key + ".tar.gz")
            if digest(archive) != expected:
                raise ValueError("archive digest differs: " + str(archive))
            with tarfile.open(archive) as stream:
                stream.extractall(root / key, filter="data")
            sources[key] = root / key / ("semver-" + revision)

        rounds = []
        for kind, expected_exit in (("base", 1), ("gold", 0)):
            fixture = root / ("fixture-" + kind)
            shutil.copytree(sources[kind], fixture)
            # This suite calls unexported methods whose signatures differ between revisions.
            run(fixture, env, "go", "test", "-count=1", "./...")
            check = ["go", "run", str(probe)]
            observed = run(fixture, env, *check, code=expected_exit).splitlines()
            wanted = ["1.0.1-beta false true" if expected_exit else "1.0.1-beta true true",
                      "1.0.1 true true", "3.0.1-beta false false", "4.0.0 true true"]
            if observed != wanted:
                raise ValueError(("unexpected control behavior", observed))
            run(fixture, env, "git", "init", "-q")
            run(fixture, env, "git", "add", ".")
            run(fixture, env, "git", "commit", "-qm", "Pinned candidate and unchanged tests")
            for pair in range(args.pairs):
                arms = ("plain", "kitsu") if pair % 2 == 0 else ("kitsu", "plain")
                for arm in arms:
                    work = root / f"{kind}-{pair}-{arm}"
                    shutil.copytree(fixture, work)
                    def command(*cmd, code=0):
                        return run(work, env, *cmd, code=code)
                    head = command("git", "rev-parse", "HEAD")
                    started = time.monotonic()
                    if arm == "plain":
                        if command(*check, code=expected_exit).splitlines() != wanted:
                            raise ValueError("plain behavior changed")
                    else:
                        def task(verb, *flags, code=0):
                            return json.loads(command(cli, "task", verb, "semver-review", "--brief", *flags, code=code))
                        entry = task("open", "--request", REQUEST, "--entry", "verify", "--until", "verify")
                        ready = task("record", "intake", "--ticket", entry["ticket"], "--actor", "scripted",
                                     "--note", "Run the public constraint against prerelease and stable controls.")
                        result = task("test", "--ticket", ready["ticket"], "--expect", "Prerelease stays in its OR group",
                                      "--", *check, code=5 if expected_exit else 0)
                        expected_next = "verify" if expected_exit else "done"
                        if result["verified"] == bool(expected_exit) or result["next"] != expected_next:
                            raise ValueError(("incorrect readiness", result))
                    elapsed = time.monotonic() - started
                    if arm == "kitsu":
                        saved = json.loads(command(cli, "task", "next", "semver-review", "--json"))
                        proof = saved["behavioral_assertion"]
                        log = work / ".git" / "kitsu" / "logs" / (proof["run_id"] + ".log")
                        if proof["argv"] != check or proof["exit_status"] != expected_exit or digest(log) != proof["log_sha256"]:
                            raise ValueError("proof did not match the command, exit or log")
                        output = log.read_text().splitlines()
                        if [line for line in output if line in wanted] != wanted:
                            raise ValueError(("missing version observations", output))
                    if command("git", "status", "--porcelain") or command("git", "rev-parse", "HEAD") != head:
                        raise ValueError("verify-only changed the checkout")
                    rounds.append(dict(candidate=kind, pair=pair+1, arm=arm, seconds=round(elapsed, 6),
                                       public_cases_passed=len(wanted)-expected_exit, accepted=not bool(expected_exit)))
        print(json.dumps(dict(binary_sha256=digest(Path(cli)), grader_sha256=digest(probe),
                              model_calls=0, compact=False, go=run(root, env, "go", "version"),
                              respective_upstream_suite_passed_on=["base", "gold"], controls=ARCHIVES, runs=rounds,
                              medians={kind: {arm: statistics.median(r["seconds"] for r in rounds
                                        if r["arm"] == arm and r["candidate"] == kind) for arm in ("plain", "kitsu")}
                                       for kind in ARCHIVES}), indent=2))


if __name__ == "__main__":
    main()
