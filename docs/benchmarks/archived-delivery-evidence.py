"""Reconcile four archived paid comparisons and grade pflag test suites on common offline controls."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import subprocess
import tarfile
import tempfile

here = Path(__file__).parent
strength = runpy.run_path(str(here / "pflag-regression-strength.py"))
delivery = runpy.run_path(str(here / "pflag-daily-delivery.py"))
git, test = strength["git"], strength["test"]
cohorts = ("kitsu-vs-codex-20261117", "pflag-sol62-three", "daily-audit-pair-nov18", "daily-qs-review-pair-nov18")
mutations = dict(strength["MUTATIONS"],
    drop_attached_unknown_value=(
        "f.args = append(f.args, errUnknownFlag.UnknownFlags)",
        'unknown := errUnknownFlag.UnknownFlags\n\t\t\t\tif !f.interspersed { unknown = strings.SplitN(unknown, "=", 2)[0] }\n\t\t\t\tf.args = append(f.args, unknown)'),
    skip_parseall_callback_in_passthrough=(
        "return fn(flag, value)",
        "if !f.interspersed && f.ParseErrorsAllowlist.UnknownFlagsHandling == PassUnknownFlagToArgs { return nil }; return fn(flag, value)"))
env = dict(os.environ, GOENV="off", GOFLAGS="", GOWORK="off", GOTOOLCHAIN="local", GOPROXY="off", CGO_ENABLED="0")


def read(path):
    return json.loads(path.read_bytes())


def completion(run, result):
    if run.name != "pflag-before":
        return result["completion"], "original probe"
    original = result["completion_evidence"]
    correction = read(run / "completion-offline-kitsu-label.json")
    replaced = ["-workflow-namespace-arm=kitsu" if arg == "-workflow-namespace-arm=before" else arg for arg in original["argv"]]
    expected = f"committed tree {result['candidate_tree']}; clean workspace; completion evidence only\nPASS\n"
    if (original["argv"].count("-workflow-namespace-arm=before") != 1 or original["exit_code"] == 0
            or correction["argv"] != replaced or correction["exit_code"] != 0 or correction["stdout"] != expected):
        raise ValueError("unreconciled before-arm completion correction")
    return True, "same offline completion probe with recognized managed arm label"


def inventory(archive):
    rows, paths, missing = [], {}, []
    for cohort in cohorts:
        folder = archive / cohort
        manifest = folder / "frozen-workflow.json"
        key = hashlib.sha256(manifest.read_bytes()).hexdigest()
        plan = read(manifest)
        applications = {case["id"]: case["application"]["sha256"] for case in plan["cases"]}
        for spec in plan["runs"]:
            run = folder / "runs" / spec["id"]
            if not run.exists():
                missing.append([cohort, spec["id"]])
                continue
            result, grade = read(run / "result.json"), read(run / "grade.json")
            usage = result["usage"]
            threads = usage["threads"]
            if (result["manifest_sha256"] != key or result["run"] != spec or grade != result["grade"]
                    or grade["candidate_id"] != result["candidate_tree"] or grade["infrastructure_status"] != "ok"
                    or not grade["eligibility"]["passed"]):
                raise ValueError(f"unreconciled run {run}")
            if (len({t["id"] for t in threads}) != len(threads)
                    or sum(t["input_total"] + t["output"] for t in threads) != usage["total"]):
                raise ValueError(f"unreconciled usage {run}")
            criteria = grade["acceptance"]["criteria"]
            row = dict(cohort=cohort, manifest=key, model=plan["model"], effort=plan["effort"],
                       run=spec["id"], case=spec["case_id"], arm=spec["arm"], baseline=result["baseline_tree"],
                       candidate=result["candidate_tree"], status=result["status"], completion_raw=result["completion"],
                       application=applications[spec["case_id"]], common_prompt=plan["common_prompt"]["sha256"],
                       acceptance_passed=sum(c["passed"] for c in criteria), acceptance_total=len(criteria),
                       original_suite=grade["regressions"]["passed"], patch_integrity=grade["patch_integrity"]["passed"],
                       method=result["method"]["status"], tokens=usage["total"], usage_complete=usage["complete"],
                       sessions=len(threads), seconds=round(result["execution"]["elapsed_ns"]/1e9, 3))
            row["completion"], row["completion_source"] = completion(run, result)
            rows.append(row)
            if spec["case_id"] == "pflag-unknown-flags":
                paths[(cohort, spec["id"])] = run / "worktree"
    return rows, paths, missing


def suite_files(repo, tree):
    entries = strength["entries"](repo, tree)
    return {os.fsdecode(p): meta.split()[2].decode() for p, meta in entries.items()
            if b"/" not in p and p.endswith(b"_test.go")}


def suite_failures(work):
    result = subprocess.run(["go", "test", "-vet=off", "-count=1", "-json", "."], cwd=work, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
    events = [json.loads(line) for line in result.stdout.splitlines()]
    failed = {e["Test"] for e in events if e.get("Test") and e["Action"] == "fail"}
    verdicts = [e["Action"] for e in events if "Test" not in e and e["Action"] in ("pass", "fail")]
    if (not any(e.get("Test") and e["Action"] == "run" for e in events) or result.returncode != int(bool(failed))
            or verdicts != ["fail" if failed else "pass"]):
        raise ValueError(f"unusable Go test run: {result.stderr[-500:]}, {result.stdout[-500:]}")
    return sorted(name for name in failed if not any(other.startswith(name + "/") for other in failed))


def grade_suites(rows, repos, grader):
    common = repos[("daily-audit-pair-nov18", "pflag-codex")]
    reference = next(r for r in rows if r["cohort"] == "daily-audit-pair-nov18" and r["arm"] == "codex")
    if hashlib.sha256(grader.read_bytes()).hexdigest() != delivery["ORACLE"]:
        raise ValueError("frozen acceptance grader differs")
    original = suite_files(common, reference["baseline"])
    # Match the original package tests. Transplant only the submitted test files.
    suites = [("original", common, original, reference["candidate"])]
    for row in rows:
        key = row["cohort"], row["run"]
        if key not in repos:
            continue
        repo = repos[key]
        if suite_files(repo, row["baseline"]) != original:
            raise ValueError(f"pflag baseline tests differ: {key}")
        changed = git(repo, "diff", "--name-only", "-z", row["baseline"], row["candidate"]).split(b"\0")
        if any(f.endswith(b".go") and (b"/" in f or (f != b"flag.go" and not f.endswith(b"_test.go")))
               or f in (b"go.mod", b"go.sum") for f in changed):
            raise ValueError(f"candidate needs other code: {key}")
        suites.append(("/".join(key), repo, suite_files(repo, row["candidate"]), row["candidate"]))
    results = []
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        with tarfile.open(fileobj=io.BytesIO(git(common, "archive", reference["candidate"]))) as tar:
            tar.extractall(work, filter="data")
        source = (work / "flag.go").read_text()
        for name in ["correct", *mutations]:
            code = source
            if name in mutations:
                before, after = mutations[name]
                if code.count(before) != 1:
                    raise ValueError(f"ambiguous fault location {name}")
                code = code.replace(before, after)
            (work / "flag.go").write_text(code)
            for label, repo, files, own_tree in suites:
                for path in work.glob("*_test.go"):
                    path.unlink()
                for path, blob in files.items():
                    (work / path).write_bytes(git(repo, "cat-file", "blob", blob))
                if label == "original":
                    if name in strength["MUTATIONS"] or name == "correct":
                        oracle = work / "audit_controls_test.go"
                        oracle.write_text(strength["ORACLE"])
                        observed = test(work, "TestAuditPassthroughControls", env)
                    else:
                        oracle = work / "audit_acceptance_test.go"
                        oracle.write_bytes(grader.read_bytes())
                        observed = test(work, "TestPilotUnknownFlagStopsNonInterspersed",
                                        dict(env, GOFLAGS="-tags=pilot_acceptance"))
                    if observed != ("pass" if name == "correct" else "fail"):
                        raise ValueError(f"invalid independent control {name}: {observed}")
                    oracle.unlink()
                failed = suite_failures(work)
                if name == "correct":
                    (work / "flag.go").write_bytes(git(repo, "show", own_tree + ":flag.go"))
                    if suite_failures(work):
                        raise ValueError(f"suite failed on its own archived implementation: {label}")
                    (work / "flag.go").write_text(code)
                results.append(dict(suite=label, control=name, failing_cases=failed))
    return results


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--grader", required=True, type=Path)
    args = parser.parse_args()
    rows, repos, missing = inventory(args.archive)
    controls = grade_suites(rows, repos, args.grader)
    print(json.dumps(dict(go=subprocess.check_output(["go", "version"], text=True).strip(), new_model_calls=0,
                          production_control=next(r["candidate"] for r in rows if r["cohort"] == "daily-audit-pair-nov18" and r["arm"] == "codex"),
                          acceptance_grader_sha256=delivery["ORACLE"], attempts=rows, unstarted=missing, suite_controls=controls), indent=2))
