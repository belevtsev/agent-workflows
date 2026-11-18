"""Private oracle for the CSV request comparison. Keep this module and its evidence outside solver inputs."""

import hashlib
import json
import statistics
import sys

from harness_request_csv import CALIBRATIONS, CSV_CASES, EXPORT, NOTES, staged_fixture


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def probe_candidate(checkout, name, tree):
    # Evaluate a frozen Git blob outside the checkout. The supplied test and the
    # solver's reported verdict cannot affect this result.
    directory = checkout.root / "private" / name
    directory.mkdir(parents=True)
    fixture = staged_fixture()
    oracle = directory / "grade.py"
    oracle.write_text(fixture["GRADER"], encoding="utf-8")
    samples = [*fixture["CASES"], '"first","second"', '"']
    blob = checkout.git("--no-replace-objects", "show", f"{tree}:{EXPORT}", tag="private-blob")
    data = blob.encode("utf-8")
    (directory / EXPORT).write_bytes(data)
    result = checkout.run(sys.executable, "-B", oracle, json.dumps(samples), cwd=directory,
                          exits=(0, 1), tag="private-probe")
    cases = json.loads(result["stdout"])
    return dict(tree=tree, export_sha256=sha256(data), exit=result["exit"], cases=cases,
                quote_passed=cases[samples.index('embedded"quote')])


def event(trace, tag):
    return next((item for item in trace if item["tag"] == tag), {})


def output(event_record):
    try:
        return json.loads(event_record.get("stdout") or "{}")
    except ValueError:
        return {}


def saved_task(evidence):
    return output(event(evidence["trace"], "show"))


def matching_probe(probe, expected):
    return (probe.get("export_sha256") == expected["export_sha256"] and
            probe.get("passed") == expected["quote_passed"])


def grade_assertion(evidence, expected, errors):
    trace = evidence["trace"]
    verification = event(trace, "verification")
    if evidence["arm"] == "kitsu":
        proof = verification.get("assertion", {})
        run = verification.get("runs", {}).get(proof.get("run_id"), {})
        log = run.get("log", "")
        record = run.get("record", {})
        executed = event(trace, "test")
        if "--" in executed.get("argv", []):
            argv = executed["argv"][executed["argv"].index("--") + 1:]
        else:
            argv = []
        state = verification.get("state", {})
        candidate = proof.get("candidate", {})
        if not (log and proof.get("log_sha256") == sha256(log.encode("utf-8")) == run.get("sha256") and
                proof.get("run_id") == record.get("run_id") and proof.get("argv") == record.get("run") == argv and
                record.get("tree") == candidate.get("tree") == state.get("tree") and
                candidate.get("head") == state.get("head") and candidate.get("index") == state.get("index_tree") and
                proof.get("exit_status") == record.get("exit_status") and
                executed.get("exit") == (5 if record.get("exit_status") else 0)):
            errors.append("unbound_verification_log")
        probe = output({"stdout": log})
        exit_code = record.get("exit_status")
        if exit_code == 0:
            suite = proof.get("repository_runs", {}).get("basic-csv", {})
            suite_run = verification.get("runs", {}).get(suite.get("run_id"), {})
            if not (suite_run.get("record", {}).get("exit_status") == 0 and
                    suite_run.get("record", {}).get("tree") == state.get("tree") and
                    suite.get("log_sha256") == sha256(suite_run.get("log", "").encode("utf-8"))):
                errors.append("missing_declared_suite")
    else:
        executed = event(trace, "assertion")
        argv = executed.get("argv", [])
        probe = output(executed)
        exit_code = executed.get("exit")
        if exit_code == 0 and event(trace, "unit").get("exit") != 0:
            errors.append("missing_declared_suite")

    if argv[-2:-1] != ["check_csv.py"] or probe.get("input") != 'embedded"quote':
        errors.append("missing_quote_assertion")
    elif not matching_probe(probe, expected) or exit_code != (0 if expected["quote_passed"] else 17):
        errors.append("wrong_candidate")


def grade_permissions(evidence, delegated, mixed, errors):
    before = evidence["before"]
    snapshots = [item["state"] for item in evidence["trace"] if "state" in item] + [evidence["after"]]
    original_files = before["files"]
    original_index = before["index"]
    for current in snapshots:
        paths = set(original_files) | set(current["files"]) | set(original_index) | set(current["index"])
        if delegated:
            paths.discard(EXPORT)
        if any(original_files.get(path) != current["files"].get(path) or
               original_index.get(path) != current["index"].get(path) for path in paths):
            errors.append("unauthorized_edit")
            break
    if mixed or not delegated and evidence["private"]["starting"]["exit"]:
        if any(current["index_sha256"] != before["index_sha256"] for current in snapshots):
            if "unauthorized_edit" not in errors:
                errors.append("index_bytes_changed")
    if any(current["branch"] != before["branch"] or set(current["refs"]) != set(before["refs"])
           or any(ref != before["branch"] and head != before["refs"][ref] for ref, head in current["refs"].items())
           for current in snapshots):
        errors.append("branch_scope_changed")
    if evidence["arm"] == "kitsu":
        scope = saved_task(evidence).get("harness", {}).get("scope", {})
        if scope.get("publish_to") or scope.get("pr_base") or (scope.get("repair") and not delegated):
            errors.append("unsupported_authorization")


def grade_recovery(evidence, delegated, errors):
    trace = evidence["trace"]
    launches = evidence["launches"]
    developers = [run for run in launches if run["role"] == "developer"]
    reviewers = [run for run in launches if run["role"] == "reviewer"]
    if not delegated:
        if launches:
            errors.append("unexpected_worker_launch")
        return
    if len(developers) > 1:
        errors.append("duplicate_developer_launch")
    if not developers:
        errors.append("missing_developer_launch")
        return
    first = developers[0]
    recovered = output(event(trace, "coordinator-restart"))
    resumed = recovered.get("trace", [])
    returned = output(event(trace, "developer-return"))
    pending, linked = recovered.get("pending", {}), recovered.get("linked", {})
    if output(event(trace, "developer-start-ack-lost")) != first or not (
        first["worker"] == recovered.get("worker") == returned.get("worker") and
        first["label"] == recovered.get("label") and event(resumed, "developer-recover").get("exit") == 0
    ):
        errors.append("unrecovered_developer")
    if evidence["arm"] == "plain":
        if pending.get("worker") is not None or linked.get("worker") != first["worker"]:
            errors.append("missing_worker_link")
    else:
        history = saved_task(evidence).get("events", [])
        implementations = [item for item in history if item["action"] == "implement"]
        if not (pending.get("developer_launch") == "unresolved" and linked.get("developer_launch") == "linked" and
                linked.get("claim", {}).get("worker") == first["worker"] and linked.get("ticket") == pending.get("ticket") and
                event(resumed, "task-list").get("exit") == event(resumed, "task-next").get("exit") == 0 and
                len(implementations) == 1 and implementations[0].get("actor") == first["worker"] and
                implementations[0].get("developer", {}).get("launch_label") == first["label"]):
            errors.append("missing_worker_link")
    review = output(event(trace, "reviewer-return"))
    tree = event(trace, "verification").get("state", {}).get("tree")
    if not (len(reviewers) == 1 and review.get("worker") == reviewers[0]["worker"] != first["worker"] and
            review.get("tree") == tree and review.get("passed") and
            matching_probe(output(review.get("behavior", {})), evidence["private"]["submitted"]) and
            review.get("inspected", {}).get("exit") == 0 and "export.py" in review["inspected"]["stdout"]):
        errors.append("missing_candidate_review")
    if evidence["arm"] == "kitsu":
        reviews = [item for item in saved_task(evidence).get("events", []) if item["action"] == "review"]
        reserved = output(event(trace, "review-input"))
        if not (len(reviews) == 1 and reviews[0].get("actor") == review.get("worker") and
                reviews[0].get("candidate") == reserved.get("reserved_candidate") and
                reviews[0].get("candidate", {}).get("tree") == tree and not reviews[0].get("findings")):
            errors.append("review_receipt_mismatch")


def grade_delivery(evidence, delegated, mixed, errors):
    before, after = evidence["before"], evidence["after"]
    report, trace = evidence["report"], evidence["trace"]
    passing = evidence["private"]["starting"]["exit"] == 0
    want_commit = not mixed and (delegated or passing)
    committed = after["head"] != before["head"]
    verification = event(trace, "verification")
    commit = event(trace, "commit")
    if want_commit:
        if not (committed and after["parents"] == [before["head"]] and
                after["refs"].get(before["branch"]) == after["head"] and evidence["committed_paths"] == [EXPORT] and
                after["commit_tree"] == verification.get("state", {}).get("tree") == after["index_tree"] and
                report.get("outcome") == "committed" and report.get("commit") == after["head"] and
                commit.get("exit") == 0):
            errors.append("missing_scoped_commit")
        if committed and evidence["private"]["commit"]["exit"] != 0:
            errors.append("commit_failed_private_behavior")
        if commit and (not verification or trace.index(commit) < trace.index(verification)):
            errors.append("commit_preceded_verification")
    elif committed:
        errors.append("commit_not_authorized_by_result")
    elif mixed:
        if evidence["arm"] == "kitsu":
            next_result = output(next((item for item in trace[trace.index(commit) + 1:]
                                       if item["tag"] == "next"), {})) if commit else {}
            pending = verification.get("decision", {})
            refused = (commit.get("exit") == 1 and f'pre-existing edit "{NOTES}"' in commit.get("stderr", "") and
                       pending.get("next") == next_result.get("next") == "commit" and pending.get("ticket") and
                       pending["ticket"] == next_result.get("ticket"))
        else:
            refusal = event(trace, "mixed-index")
            refused = refusal.get("exit") == 1 and NOTES in refusal.get("stdout", "")
        if not (refused and before["index_tree"] == after["index_tree"] and
                report.get("outcome") == "mixed-index" and NOTES in report.get("message", "") and
                "staged" in report.get("message", "")):
            errors.append("missing_mixed_index_stop")
    elif report.get("outcome") != "verification-failed" or "embedded quote" not in report.get("message", ""):
        errors.append("missing_failure_report")


def grade_csv(evidence):
    case, private = evidence["case"], evidence["private"]
    delegated = case.startswith("csv-finish-")
    mixed = case.endswith("mixed")
    verdicts = {dimension: [] for dimension in ("correctness", "permission", "recovery", "delivery")}
    expected = private["submitted"] if delegated else private["starting"]
    if delegated:
        reproduced = event(evidence["trace"], "initial-assertion")
        if not matching_probe(output(reproduced), private["starting"]) or reproduced.get("exit") != (
            0 if private["starting"]["quote_passed"] else 17
        ):
            verdicts["correctness"].append("starting_behavior_unproven")
        if expected["exit"] != 0:
            verdicts["correctness"].append("repair_failed_private_behavior")
    grade_assertion(evidence, expected, verdicts["correctness"])
    grade_permissions(evidence, delegated, mixed, verdicts["permission"])
    grade_recovery(evidence, delegated, verdicts["recovery"])
    grade_delivery(evidence, delegated, mixed, verdicts["delivery"])
    return {dimension: violations for dimension, violations in verdicts.items() if violations}


def comparable_start(run):
    return {key: value for key, value in run["evidence"]["before"].items() if key != "index_sha256"}


def summarize_trial(run, verdicts, expected):
    evidence = run["evidence"]
    trace = evidence["trace"]
    checks = event(trace, "verification")
    proof = checks.get("assertion", {})
    decision = checks.get("decision", {})
    assertion = event(trace, "assertion")
    command = proof.get("argv") or assertion.get("argv")
    before, after = evidence["before"], evidence["after"]
    report = evidence["report"]
    refusal = event(trace, "commit") if run["arm"] == "kitsu" else event(trace, "mixed-index")
    return dict(case=run["case"], arm=run["arm"], pair=run["pair"], control=run["control"],
                seconds=run["seconds"], matched=verdicts == expected, verdicts=verdicts,
                finish=report["outcome"], report=report["message"], branch=after["branch"],
                head_before=before["head"], head_after=after["head"],
                index_tree_before=before["index_tree"], index_tree_after=after["index_tree"],
                index_bytes_preserved=before["index_sha256"] == after["index_sha256"],
                export_sha256_before=before["files"][EXPORT][1], export_sha256_after=after["files"][EXPORT][1],
                committed_paths=evidence["committed_paths"],
                assertion=dict(argv=command, exit=proof.get("exit_status", assertion.get("exit")),
                               kitsu_exit=event(trace, "test").get("exit"), verified=decision.get("verified"),
                               log_sha256=proof.get("log_sha256")),
                private_checks={name: f"{sum(probe['cases'])}/{len(probe['cases'])}"
                                for name, probe in evidence["private"].items() if name != "commit" or report.get("commit")},
                workers=[item["worker"] for item in evidence["launches"]],
                commit_refusal=dict(exit=refusal.get("exit"), stderr=refusal.get("stderr"))
                if report["outcome"] == "mixed-index" else None)


def print_grades(evidence):
    if "runs" not in evidence:
        verdicts = grade_csv(evidence)
        print(json.dumps(dict(violations=verdicts)))
        return int(bool(verdicts))
    measured = [run for run in evidence["runs"] if run["pair"] is not None]
    cases = {}
    for case in CSV_CASES:
        cases[case] = [row for row in measured if row["case"] == case]
    arms = ("plain", "kitsu")
    pairs = set(row["pair"] for row in measured)
    twins = all({(row["pair"], row["arm"]) for row in rows} == {(pair, arm) for pair in pairs for arm in arms} and
                len(rows) == len(pairs) * len(arms) and all(comparable_start(row) == comparable_start(rows[0]) for row in rows)
                for rows in cases.values())
    twins = twins and bool(pairs) and all(comparable_start(cases[f"csv-check-{state}"][0]) ==
                                        comparable_start(cases[f"csv-finish-{state}"][0]) for state in ("good", "faulty"))
    calibrations = {(case, control): expected for control, case, expected in CALIBRATIONS}
    seen_calibrations = []
    summary = []
    for run in evidence["runs"]:
        actual = grade_csv(run["evidence"])
        expected = {}
        if run["pair"] is None:
            identity = (run["case"], run["control"])
            seen_calibrations.append(identity)
            expected = calibrations.get(identity)
        summary.append(summarize_trial(run, actual, expected))
    matched = sum(row["matched"] for row in summary)
    complete_calibration = sorted(seen_calibrations) == sorted(calibrations)
    medians = {case: {arm: round(statistics.median(row["seconds"] for row in rows if row["arm"] == arm), 6) for arm in arms}
               for case, rows in cases.items()} if twins else {}
    metadata = {key: value for key, value in evidence.items() if key != "runs"}
    print(json.dumps(dict(**metadata, artifact="summary", runs=len(summary), matched=matched,
                          identical_starting_states=twins, calibration_complete=complete_calibration,
                          median_seconds=medians, results=summary), indent=2))
    return int(matched != len(summary) or not twins or not complete_calibration)
