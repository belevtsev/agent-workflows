"""Replay a scripted review finding on a partly implemented semver change with plain Git and Kitsu."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tempfile
import time


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def replay(root, env, fixture, corrected, arm, binary, compare_resume, sample_dir):
    work = root / arm
    shutil.copytree(fixture, work, ignore=shutil.ignore_patterns(".git"))

    def run(*argv, code=0):
        result = subprocess.run(argv, cwd=work, env=env, text=True, capture_output=True, timeout=120)
        if result.returncode != code:
            raise RuntimeError((argv, result.returncode, result.stdout, result.stderr))
        return result.stdout.strip()

    def task(verb, *flags):
        return json.loads(run(binary, "task", verb, "semver", "--brief", *flags))

    def record(action, state, actor, note, *flags):
        return task("record", action, "--ticket", state["ticket"], "--actor", actor, "--note", note, *flags)

    run("git", "init", "-q")
    run("git", "add", ".")
    run("git", "commit", "-qm", "Pinned semver baseline and public assertions")
    app = work / "constraints.go"
    good = (corrected / app.name).read_text()
    group = "if !cs.containsPre[i] && v.pre"
    if good.count(group) != 1:
        raise ValueError("candidate does not contain the expected group-aware Validate implementation")
    check = ["go", "test", "-count=1", "-tags", "pilot_acceptance", "-run", "^TestPilotPrereleaseGroupsPublicCheckAndValidate$", "."]
    handoff = {}
    started = time.monotonic()
    if binary:
        state = task("open", "--entry", "implement", "--until", "deliver", "--require-review", "--commit",
                     "--request", "Implement group-aware prereleases for Check and Validate, review, test and commit")
        state = record("intake", state, "author", "Both public APIs must honor prereleases in the matching group")
    app.write_text(good.replace(group, 'if c.con.pre == "" && v.pre'))
    shutil.copyfile(corrected / "constraints_test.go", work / "constraints_test.go")
    if binary:
        state = record("implement", state, "author", "Updated group-aware comparison")
        state = task("claim", "--actor", "author", "--ticket", state["ticket"])
    finding = "Check admits the matching prerelease but Validate rejects it"
    if "Check=true Validate=false" not in run(*check, code=1):
        raise ValueError("reviewer did not reproduce the seeded disagreement")
    if binary:
        record("review", state, "reviewer-one", finding, "--finding", finding)
        if compare_resume:
            flags = ["--json"] if arm == "previous" else []
            reading = time.monotonic()
            output = run(binary, "task", "next", "semver", *flags)
            handoff = dict(resume_bytes=len((output+"\n").encode()), resume_seconds=time.monotonic()-reading)
            if sample_dir is not None:
                (sample_dir / f"{root.name}-{arm}.txt").write_text(output+"\n")
            if finding not in output or "Both public APIs must honor prereleases" not in output:
                raise ValueError("missing review evidence on resume")
            if flags:
                state = json.loads(output)
            else:
                if "next implement" not in output or "--commit" not in output or "--require-review" not in output:
                    raise ValueError("resume omitted the correction or its scope")
                tickets = [line.removeprefix("Action ticket: ") for line in output.splitlines()
                           if line.startswith("Action ticket: ")]
                if len(tickets) != 1:
                    raise ValueError("resume omitted the unique action ticket")
                state = {"next": "implement", "ticket": tickets[0]}
        else:
            state = task("next")
        if state["next"] == "blocked":
            return dict(outcome="blocked on finding", seconds=time.monotonic()-started)
        if state["next"] != "implement":
            raise ValueError(state)
    app.write_text(good)
    if binary:
        state = record("implement", state, "author", "Corrected Validate for the whole group")
        state = task("claim", "--actor", "author", "--ticket", state["ticket"])
    run(*check)
    if binary:
        state = record("review", state, "reviewer-two", "Both public APIs pass the group assertions")
        state = task("test", "--ticket", state["ticket"], "--expect", "Check and Validate honor matching groups", "--", *check)
        if state["next"] != "commit":
            raise ValueError(state)
    else:
        run(*check)
        run("go", "test", "./...")
    run("git", "add", "constraints.go", "constraints_test.go")
    if binary:
        if task("commit", "--ticket", state["ticket"], "--message", "Honor prerelease groups")["next"] != "done":
            raise ValueError("missing delivery")
    else:
        run("git", "commit", "-qm", "Honor prerelease groups")
    tree = run("git", "rev-parse", "HEAD^{tree}")
    if run("git", "status", "--porcelain"):
        raise ValueError("dirty delivery")
    return dict(outcome="delivered", tree=tree, seconds=time.monotonic()-started, **handoff)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("fixtures", type=Path, help="archived semver audit-work directory with base and gold subdirectories")
    parser.add_argument("previous", type=Path)
    parser.add_argument("current", type=Path)
    parser.add_argument("--compare-resume", action="store_true",
                        help="Compare an earlier full JSON resume with the terminal resume. Both builds must support correcting findings.")
    parser.add_argument("--resume-samples", type=Path, help="save the scripted resume responses for offline tokenization")
    args = parser.parse_args()
    if args.resume_samples is not None:
        if not args.compare_resume:
            parser.error("--resume-samples requires --compare-resume")
        args.resume_samples.mkdir(parents=True, exist_ok=True)
    binaries = {"plain": None, "previous": args.previous.resolve(strict=True), "current": args.current.resolve(strict=True)}
    rows = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        env = dict(os.environ, GOWORK="off", GOPROXY="off", GOTOOLCHAIN="local",
                   GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1", GIT_AUTHOR_NAME="Replay",
                   GIT_AUTHOR_EMAIL="replay@example.invalid", GIT_COMMITTER_NAME="Replay", GIT_COMMITTER_EMAIL="replay@example.invalid")
        for round_id, first in enumerate(binaries):
            arms = list(binaries)
            start = arms.index(first)
            for arm in arms[start:] + arms[:start]:
                result = replay(root / str(round_id), env, args.fixtures / "base", args.fixtures / "gold", arm, binaries[arm], args.compare_resume, args.resume_samples)
                expected = "blocked on finding" if arm == "previous" and not args.compare_resume else "delivered"
                if result["outcome"] != expected:
                    raise ValueError((arm, result))
                rows.append(dict(round=round_id+1, arm=arm, **result))
    if len({r["tree"] for r in rows if "tree" in r}) != 1:
        raise ValueError("completed arms delivered different trees")
    print(json.dumps(dict(model_calls=0, compare_resume=args.compare_resume, go=subprocess.check_output(["go", "version"], text=True).strip(),
                          compact=False, assertion_sha256=digest(args.fixtures/"base"/"kitsu_pilot_acceptance_test.go"),
                          binaries={a: digest(b) for a, b in binaries.items() if b}, runs=rows,
                          median_seconds={a: statistics.median(r["seconds"] for r in rows if r["arm"] == a)
                                          for a in binaries}), indent=2))


if __name__ == "__main__":
    main()
