#!/usr/bin/env python3
"""Validate a frozen multilingual corpus, or explicitly run its synthetic baseline."""

import sys

# Managed snapshots are immutable, including when this module imports helpers.
sys.dont_write_bytecode = True

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import time

import consult_decisions as consultation
from validate_policy import DEFAULT_POLICY, PolicyError, load_policy, require, unique_mapping


SKILL_ROOT = Path(__file__).resolve().parents[1]
CORPUS_ROOT = SKILL_ROOT / "evals" / "multilingual"
LANGUAGES = ("en", "uk", "de")
CHOICES = ("supported", "contradicted", "insufficient_evidence")
FAMILIES = ("durability", "tenant_isolation", "cancellation", "release_identity")
CASE_IDS = tuple("c{:02d}".format(number) for number in range(1, 13))
PROVIDERS = ("jev", "subq_snap", "openai_decisions")
HELPERS = ("consult_decisions.py", "consult_jev.py", "consult_openai.py", "validate_policy.py")


def nonempty(value):
    return isinstance(value, str) and bool(value.strip())


def validate_inputs(value):
    require(isinstance(value, dict) and set(value) == {
        "version", "languages", "instructions", "options", "cases",
    }, "invalid corpus fields")
    require(type(value["version"]) is int and value["version"] == 1, "invalid corpus version")
    require(value["languages"] == list(LANGUAGES), "invalid corpus languages")
    for field in ("instructions", "options"):
        require(isinstance(value[field], dict) and set(value[field]) == set(LANGUAGES),
                "incomplete localized corpus fields")
    for language in LANGUAGES:
        instructions = value["instructions"][language]
        require(nonempty(instructions) and
                "state.cases.{case_id}.evidence" in instructions and
                "state.cases.{case_id}.claim" in instructions,
                "instructions must refer to the question's own evidence and claim")
        options = value["options"][language]
        require(isinstance(options, dict) and set(options) == set(CHOICES) and
                all(nonempty(text) for text in options.values()), "incomplete localized options")
    require(isinstance(value["cases"], list) and len(value["cases"]) == 12,
            "corpus requires twelve cases")
    seen, families = set(), {family: 0 for family in FAMILIES}
    for case in value["cases"]:
        require(isinstance(case, dict) and set(case) == {"id", "family", "variants"},
                "invalid case fields")
        case_id, family = case["id"], case["family"]
        require(isinstance(case_id, str) and case_id in CASE_IDS and case_id not in seen,
                "invalid or repeated case ID")
        require(isinstance(family, str) and family in FAMILIES, "invalid case family")
        seen.add(case_id)
        families[family] += 1
        variants = case["variants"]
        require(isinstance(variants, dict) and set(variants) == set(LANGUAGES),
                "incomplete case translations")
        for variant in variants.values():
            require(isinstance(variant, dict) and set(variant) == {"evidence", "claim"} and
                    all(nonempty(text) for text in variant.values()), "invalid case translation")
    require(seen == set(CASE_IDS) and all(count == 3 for count in families.values()),
            "invalid case or family coverage")
    return value


def validate_oracle(value, inputs):
    require(isinstance(value, dict) and set(value) == {"version", "labels"} and
            type(value["version"]) is int and value["version"] == 1, "invalid oracle fields")
    labels = value["labels"]
    require(isinstance(labels, dict) and set(labels) == set(CASE_IDS), "incomplete oracle labels")
    for label in labels.values():
        require(isinstance(label, dict) and set(label) == {"expected", "rationale"} and
                isinstance(label["expected"], str) and label["expected"] in CHOICES and
                nonempty(label["rationale"]), "invalid oracle label")
    for family in FAMILIES:
        require({labels[case["id"]]["expected"] for case in inputs["cases"]
                 if case["family"] == family} == set(CHOICES), "oracle family labels must be balanced")
    return value


def build_request(inputs, language):
    """Build provider evidence from raw inputs only; no oracle enters this API."""
    validate_inputs(inputs)
    require(language in LANGUAGES, "unsupported corpus language")
    cases = sorted(inputs["cases"], key=lambda case: case["id"])
    return consultation.shared_request({
        "state": {"cases": {case["id"]: dict(case["variants"][language]) for case in cases}},
        "questions": {case["id"]: {
            "type": "choice",
            "instructions": inputs["instructions"][language].replace("{case_id}", case["id"]),
            "criteria": dict(inputs["options"][language]),
        } for case in cases},
    })


def prepare_requests(shared, policy):
    selected = policy["consultation"]
    require(set(selected.get("providers", {})) == set(PROVIDERS) and
            not selected.get("optional_providers"), "evaluation requires all three configured advisers")
    return {name: (consultation.openai.prepare_request(shared, model) if name == "openai_decisions"
                   else consultation.jev.prepare_request(shared, model))
            for name, model in selected["providers"].items()}


def strict_json(raw):
    try:
        return json.loads(raw.decode("utf-8"), object_pairs_hook=unique_mapping,
                          parse_constant=lambda _value: (_ for _ in ()).throw(
                              PolicyError("nonfinite JSON number")))
    except (UnicodeError, ValueError, RecursionError) as failure:
        raise PolicyError("cannot read valid evaluation JSON") from failure


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def load_resources():
    """Freeze bytes before dispatch; their hashes describe the exact loaded inputs."""
    raw = {"inputs.json": (CORPUS_ROOT / "inputs.json").read_bytes(),
           "oracle.json": (CORPUS_ROOT / "oracle.json").read_bytes(),
           "model-policy.yaml": DEFAULT_POLICY.read_bytes()}
    inputs = validate_inputs(strict_json(raw["inputs.json"]))
    oracle = validate_oracle(strict_json(raw["oracle.json"]), inputs)
    policy = load_policy(DEFAULT_POLICY)
    require(DEFAULT_POLICY.read_bytes() == raw["model-policy.yaml"], "policy changed during validation")
    for language in LANGUAGES:
        prepare_requests(build_request(inputs, language), policy)
    return inputs, oracle, policy, raw


def ratio(numerator, denominator):
    return round(numerator / denominator, 6) if denominator else None


def metrics(outcomes):
    total = len(outcomes)
    correct = sum(item["outcome"] == "correct" for item in outcomes)
    incorrect = sum(item["outcome"] == "incorrect" for item in outcomes)
    valid = correct + incorrect
    return {"total": total, "correct": correct, "incorrect": incorrect,
            "missing": total - valid, "valid_answers": valid,
            "accuracy": ratio(correct, valid), "coverage": ratio(valid, total),
            "correct_over_total": ratio(correct, total)}


def score_records(inputs, oracle, records):
    """Score synthetic labels without adjudicating a workflow consultation."""
    validate_inputs(inputs)
    validate_oracle(oracle, inputs)
    scored = {}
    for provider in PROVIDERS:
        languages, all_outcomes, triplets = {}, [], {case_id: [] for case_id in CASE_IDS}
        for language in LANGUAGES:
            stored = records.get(language, {}).get("providers", {}).get(provider, {})
            answers = stored.get("result", {}).get("answers", {})
            outcomes = []
            for case_id in CASE_IDS:
                answer, choice = answers.get(case_id, {}), None
                reason = stored.get("error", {}).get("category", stored.get("status", "not_attempted"))
                if stored.get("status") in ("success", "partial", "refusal"):
                    if answer.get("type") == "choice" and answer.get("choice") in CHOICES:
                        choice = answer["choice"]
                    elif answer.get("type") == "refusal":
                        reason = "refusal"
                expected = oracle["labels"][case_id]["expected"]
                outcome = {"id": case_id, "expected": expected, "choice": choice,
                           "outcome": "missing" if choice is None else
                                      "correct" if choice == expected else "incorrect"}
                if choice is None:
                    outcome["missing_reason"] = reason
                else:
                    triplets[case_id].append(choice)
                outcomes.append(outcome)
            languages[language] = dict(metrics(outcomes), cases=outcomes)
            all_outcomes.extend(outcomes)
        complete = [choices for choices in triplets.values() if len(choices) == len(LANGUAGES)]
        consistent = sum(len(set(choices)) == 1 for choices in complete)
        scored[provider] = {"languages": languages, "totals": metrics(all_outcomes),
                            "consistency": {"possible_triplets": len(CASE_IDS),
                                            "complete_triplets": len(complete),
                                            "consistent_triplets": consistent,
                                            "incomplete_triplets": len(CASE_IDS) - len(complete),
                                            "ratio": ratio(consistent, len(complete))}}
    return scored


def inside(path, root):
    return path == root or root in path.parents


def protected_roots():
    roots = [SKILL_ROOT.resolve()]
    # A source-run evaluator also protects the default/XDG manager store. An
    # installed evaluator additionally discovers its custom immutable release root.
    roots.append((Path.home() / ".local" / "state" / "agent-workflows" / "releases").resolve())
    if os.environ.get("XDG_STATE_HOME"):
        roots.append((Path(os.environ["XDG_STATE_HOME"]) / "agent-workflows" / "releases").resolve())
    for ancestor in SKILL_ROOT.resolve().parents:
        if ancestor.name == "releases":
            roots.append(ancestor)
    return roots


def output_path(path):
    requested = Path(path).expanduser()
    require(not os.path.lexists(str(requested)), "output directory must not already exist")
    resolved = requested.resolve()
    require(resolved.parent.is_dir(), "output parent directory must already exist")
    require(not any(inside(resolved, root) for root in protected_roots()),
            "output must be outside managed resources and releases")
    require(not any((ancestor / ".git").exists() for ancestor in (resolved.parent,) + tuple(resolved.parents)),
            "output must be outside source repositories")
    require(not any((ancestor / "skills-manifest.json").exists() for ancestor in resolved.parents),
            "output must be outside managed snapshots")
    return resolved


def write_private(directory, name, raw):
    """Publish a complete file exclusively, retaining earlier evidence on errors."""
    temporary = directory / ("." + name + ".partial")
    descriptor = os.open(str(temporary), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(str(temporary), str(directory / name))
    finally:
        temporary.unlink()


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2, allow_nan=False) + "\n").encode("utf-8")


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def provenance(raw, policy):
    scripts = Path(__file__).resolve().parent
    artifacts = {name: {"sha256": digest(body)} for name, body in raw.items()}
    artifacts["evaluate_multilingual.py"] = {"sha256": digest(Path(__file__).resolve().read_bytes())}
    for helper in HELPERS:
        artifacts[helper] = {"sha256": digest((scripts / helper).read_bytes())}
    return {"artifacts": artifacts,
            "runtime": {"implementation": platform.python_implementation(), "version": platform.python_version(),
                        "executable": str(Path(sys.executable).resolve()), "platform": platform.platform()},
            "requested_models": dict(policy["consultation"]["providers"])}


def summary_text(report):
    lines = ["# Synthetic multilingual evaluation", "", "Status: " + report["status"] + ".", "",
             "These labeled synthetic results measure this corpus only. They do not select an adviser,",
             "prove production quality, or resolve real workflow decisions. Consultation decisions remain pending.",
             "", "| Adviser | Correct | Incorrect | Missing | Accuracy (valid answers) | Coverage | Correct / total |",
             "|---|---:|---:|---:|---:|---:|---:|"]

    def formatted(value):
        return "unknown" if value is None else "{:.1%}".format(value)

    for provider in PROVIDERS:
        item = report["scoring"][provider]["totals"]
        lines.append("| {} | {} | {} | {} | {} | {} | {} |".format(
            provider, item["correct"], item["incorrect"], item["missing"], formatted(item["accuracy"]),
            formatted(item["coverage"]), formatted(item["correct_over_total"])))
    lines.extend(["", "| Adviser | Language | Correct | Incorrect | Missing | Accuracy | Coverage | Correct / total |",
                  "|---|---|---:|---:|---:|---:|---:|---:|"])
    for provider in PROVIDERS:
        for language in LANGUAGES:
            item = report["scoring"][provider]["languages"][language]
            lines.append("| {} | {} | {} | {} | {} | {} | {} | {} |".format(
                provider, language, item["correct"], item["incorrect"], item["missing"],
                formatted(item["accuracy"]), formatted(item["coverage"]), formatted(item["correct_over_total"])))
    lines.extend(["", "| Adviser | Consistent / complete triplets | Incomplete triplets | Consistency |",
                  "|---|---:|---:|---:|"])
    for provider in PROVIDERS:
        item = report["scoring"][provider]["consistency"]
        lines.append("| {} | {} / {} | {} | {} |".format(
            provider, item["consistent_triplets"], item["complete_triplets"], item["incomplete_triplets"],
            formatted(item["ratio"])))
    lines.extend(["", "Case outcomes, requested/returned model identities, whole-batch timing,",
                  "provider timing and token usage are retained in report.json.",
                  "Consistency is agreement across translations, not correctness. Missing answers are not scored as labels.",
                  "Token usage is measured per provider batch; no per-case allocation is inferred.", ""])
    return "\n".join(lines)


def run_live(inputs, oracle, policy, raw, directory):
    directory = output_path(directory)
    # Reserve private ownership before reading credentials or dispatching requests.
    directory.mkdir(mode=0o700)
    records, batches = {}, {}
    started_at, started = utc_now(), time.monotonic()
    state, exit_code = "incomplete", 2
    metadata = provenance(raw, policy)
    try:
        for name, body in raw.items():
            write_private(directory, name, body)
        write_private(directory, "provenance.json", json_bytes(metadata))
        credentials = {name: os.environ.get(consultation.PROVIDERS[name][1]) for name in PROVIDERS}
        for language in LANGUAGES:
            shared = build_request(inputs, language)
            prepared = prepare_requests(shared, policy)
            batch_started = time.monotonic()
            raw_records = consultation.collect(prepared, credentials, policy["consultation"]["deadline_seconds"],
                                               required_providers=set(PROVIDERS))
            duration = round(time.monotonic() - batch_started, 6)
            record = consultation.make_record(shared, prepared, raw_records, version=3)
            write_private(directory, language + ".record.json", json_bytes(record))
            records[language] = record
            batches[language] = {"record_file": language + ".record.json", "duration_seconds": duration,
                                 "providers": {name: {
                                     key: value for key, value in item.items()
                                     if key in ("status", "requested_model", "resolved_model", "duration_seconds",
                                                "usage", "error")}
                                     for name, item in record["providers"].items()}}
        state, exit_code = "completed", 0
    except KeyboardInterrupt:
        state, exit_code = "interrupted", 130
    except Exception:
        # Filesystem errors and provider internals can contain credentials. Retain
        # completed safe batches, never raw exception text or a traceback.
        state, exit_code = "error", 1
    scoring = score_records(inputs, oracle, records)
    if state == "completed" and any(item["totals"]["missing"] for item in scoring.values()):
        state, exit_code = "incomplete", 2
    report = {"version": 1, "kind": "synthetic_multilingual_evaluation", "status": state,
              "started_at": started_at, "finished_at": utc_now(),
              "duration_seconds": round(time.monotonic() - started, 6), "provenance": metadata,
              "execution": {"languages": list(LANGUAGES), "cases_per_language": len(CASE_IDS),
                            "judgments_per_provider": len(CASE_IDS) * len(LANGUAGES),
                            "max_network_requests": len(LANGUAGES) * len(PROVIDERS),
                            "deadline_seconds_per_batch": policy["consultation"]["deadline_seconds"],
                            "automatic_retries": False, "completed_batches": list(records)},
              "batches": batches, "scoring": scoring}
    try:
        write_private(directory, "report.json", json_bytes(report))
        write_private(directory, "summary.md", summary_text(report).encode("utf-8"))
    except OSError:
        print("Evaluation report could not be written; existing evidence is preserved.", file=sys.stderr)
        return 130 if exit_code == 130 else 1
    print("Synthetic evaluation " + state + "; private report: " + str(directory / "report.json"))
    return exit_code


class Arguments(argparse.ArgumentParser):
    def error(self, _message):
        raise PolicyError("invalid evaluation command arguments")


def main(argv=None):
    parser = Arguments(description=__doc__)
    parser.add_argument("--live", action="store_true", help="explicitly allow one hosted baseline, without retries")
    parser.add_argument("--output-dir", type=Path, help="new private directory outside source and managed releases")
    try:
        args = parser.parse_args(argv)
        require(args.live == bool(args.output_dir), "--live and --output-dir must be supplied together")
        inputs, oracle, policy, raw = load_resources()
        if not args.live:
            print(json.dumps({"status": "offline_validated", "languages": list(LANGUAGES),
                              "cases_per_language": len(CASE_IDS), "judgments_per_provider": 36,
                              "max_network_requests": 9, "network_requests_made": 0,
                              "deadline_seconds_per_batch": policy["consultation"]["deadline_seconds"],
                              "requested_models": policy["consultation"]["providers"],
                              "fixture_sha256": {name: digest(body) for name, body in raw.items()}},
                             indent=2, sort_keys=True))
            return 0
        return run_live(inputs, oracle, policy, raw, args.output_dir)
    except KeyboardInterrupt:
        print("Evaluation interrupted before a batch completed.", file=sys.stderr)
        return 130
    except (PolicyError, OSError, ValueError, TypeError):
        print("Evaluation validation or output preflight failed; no exception details retained.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
