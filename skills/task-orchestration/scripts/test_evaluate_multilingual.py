"""Offline tests of evaluation transport, scoring and evidence preservation."""

import copy
import io
import json
import multiprocessing
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

import consult_decisions as consultation
import evaluate_multilingual as evaluation
from validate_policy import DEFAULT_POLICY, PolicyError, load_policy


def fixtures():
    inputs = {"version": 1, "languages": list(evaluation.LANGUAGES),
              "instructions": {language: language + ": state.cases.{case_id}.evidence; "
                               "state.cases.{case_id}.claim" for language in evaluation.LANGUAGES},
              "options": {language: {choice: language + " " + choice for choice in evaluation.CHOICES}
                          for language in evaluation.LANGUAGES}, "cases": []}
    labels = {}
    for index, case_id in enumerate(evaluation.CASE_IDS):
        inputs["cases"].append({"id": case_id, "family": evaluation.FAMILIES[index // 3],
                                "variants": {language: {"evidence": language + " observation " + case_id,
                                                        "claim": language + " assertion " + case_id}
                                             for language in evaluation.LANGUAGES}})
        labels[case_id] = {"expected": evaluation.CHOICES[index % 3],
                           "rationale": "ORACLE_CANARY_DO_NOT_SEND " + case_id}
    return inputs, {"version": 1, "labels": labels}


def success(prepared, choices=None, refusal=()):
    choices = choices or {}
    answers = {}
    for case_id, question in prepared["questions"].items():
        if case_id in refusal:
            answers[case_id] = {"type": "refusal"}
            continue
        choice = choices.get(case_id, "supported")
        answers[case_id] = {"type": "choice", "choice": choice,
                            "probabilities": {option: float(option == choice) for option in question["criteria"]},
                            "confidence": 1.0}
    usage = {"input_tokens": 24, "output_tokens": 2}
    if prepared["model"] == consultation.openai.MODEL:
        usage.update(total_tokens=26, input_tokens_details={"cached_tokens": 0, "cache_write_tokens": 0},
                     output_tokens_details={"reasoning_tokens": 0})
    return {"request": prepared, "requested_model": prepared["model"], "resolved_model": prepared["model"],
            "status": "partial" if refusal else "success", "duration_seconds": 0.012,
            "result": {"model": prepared["model"], "answers": answers, "usage": usage}}


def successful_collect(prepared, _credentials, deadline, **kwargs):
    if deadline != 30 or kwargs.get("required_providers") != set(evaluation.PROVIDERS):
        raise AssertionError("incorrect required provider set or deadline")
    return {name: success(value) for name, value in prepared.items()}


class Response(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *_args):
        self.close()


def slow_worker(sender, provider, prepared, deadline_at, credential):
    """Ignore worker deadlines so the actual bounded collector must terminate it."""
    signal.signal(signal.SIGALRM, signal.SIG_IGN)
    time.sleep(3)


class EvaluationTests(unittest.TestCase):
    def setUp(self):
        self.inputs, self.oracle = fixtures()
        self.policy = load_policy(DEFAULT_POLICY)
        self.raw = {"inputs.json": evaluation.json_bytes(self.inputs),
                    "oracle.json": evaluation.json_bytes(self.oracle),
                    "model-policy.yaml": DEFAULT_POLICY.read_bytes()}

    def shared_and_prepared(self, language="en"):
        shared = evaluation.build_request(self.inputs, language)
        return shared, evaluation.prepare_requests(shared, self.policy)

    def record(self, language="en", **changes):
        shared, prepared = self.shared_and_prepared(language)
        raw = {name: success(value) for name, value in prepared.items()}
        raw.update(changes)
        return consultation.make_record(shared, prepared, raw, version=3)

    def run_at(self, directory):
        with mock.patch("sys.stdout", new_callable=io.StringIO):
            return evaluation.run_live(self.inputs, self.oracle, self.policy, self.raw, directory)

    def test_complete_fixture_and_localized_question_references(self):
        evaluation.validate_inputs(self.inputs)
        evaluation.validate_oracle(self.oracle, self.inputs)
        for language in evaluation.LANGUAGES:
            request = evaluation.build_request(self.inputs, language)
            self.assertEqual(len(request["questions"]), 12)
            for case_id, question in request["questions"].items():
                self.assertIn("state.cases." + case_id + ".evidence", question["instructions"])
                self.assertIn("state.cases." + case_id + ".claim", question["instructions"])
                self.assertEqual(question["criteria"], self.inputs["options"][language])
                self.assertEqual(request["state"]["cases"][case_id],
                                 self.inputs["cases"][evaluation.CASE_IDS.index(case_id)]["variants"][language])
                self.assertNotIn("family", request["state"]["cases"][case_id])

    def test_missing_translation_empty_text_and_incorrect_scope_are_rejected(self):
        for alter in (lambda value: value["cases"][0]["variants"].pop("uk"),
                      lambda value: value["cases"][0]["variants"]["de"].update(evidence=" "),
                      lambda value: value["options"]["uk"].pop("supported"),
                      lambda value: value["instructions"].update(en="Read all evidence"),
                      lambda value: value["cases"][1].update(id="c01"),
                      lambda value: value["cases"][0].update(family="cancellation")):
            with self.subTest(alter=alter):
                invalid = copy.deepcopy(self.inputs)
                alter(invalid)
                with self.assertRaises(PolicyError):
                    evaluation.validate_inputs(invalid)

    def test_separate_oracle_requires_all_balanced_labels(self):
        for alter in (lambda value: value["labels"].pop("c01"),
                      lambda value: value["labels"]["c01"].update(expected="no"),
                      lambda value: value["labels"]["c01"].update(expected="contradicted")):
            invalid = copy.deepcopy(self.oracle)
            alter(invalid)
            with self.assertRaises(PolicyError):
                evaluation.validate_oracle(invalid, self.inputs)

    def test_oracle_canary_never_enters_actual_transport_payloads(self):
        shared, prepared = self.shared_and_prepared()
        sent = []

        def fake_transport(outgoing, timeout):
            sent.append(json.loads(outgoing.data))
            self.assertEqual(timeout, 1)
            model = sent[-1]["model"]
            value = prepared["openai_decisions"] if model == consultation.openai.MODEL else next(
                value for value in prepared.values() if value["model"] == model)
            result = success(value)["result"]
            if model == consultation.openai.MODEL:
                result["answers"] = [dict(answer, name=case_id, probabilities=[
                    {"value": option, "probability": probability}
                    for option, probability in answer["probabilities"].items()])
                    for case_id, answer in result["answers"].items()]
            return Response(json.dumps(result).encode("utf-8"))

        with mock.patch.object(consultation.jev, "open_once", side_effect=fake_transport):
            for provider, value in prepared.items():
                adapter = consultation.openai if provider == "openai_decisions" else consultation.jev
                result, code = adapter.consult(value, 1, "credential-canary")
                self.assertEqual(code, 0)
                self.assertEqual(result["status"], "success")
        self.assertEqual(len(sent), 3)
        rendered = json.dumps(sent)
        self.assertNotIn("ORACLE_CANARY_DO_NOT_SEND", rendered)
        self.assertNotIn("rationale", rendered)
        self.assertNotIn('"expected"', rendered)
        self.assertNotIn("credential-canary", rendered)
        for provider in ("jev", "subq_snap"):
            self.assertEqual(prepared[provider]["state"], shared["state"])
            self.assertEqual(prepared[provider]["questions"], shared["questions"])

    def test_scoring_wrong_missing_and_valid_insufficient_evidence(self):
        shared, prepared = self.shared_and_prepared("de")
        exact = {case_id: label["expected"] for case_id, label in self.oracle["labels"].items()}
        raw = {provider: success(value, exact) for provider, value in prepared.items()}
        raw["jev"] = success(prepared["jev"], dict(exact, c01="contradicted"))
        raw["subq_snap"] = consultation.unavailable(prepared["subq_snap"], "timeout", 30)
        raw["openai_decisions"] = success(prepared["openai_decisions"], exact, refusal=("c01",))
        record = consultation.make_record(shared, prepared, raw, version=3)
        scored = evaluation.score_records(self.inputs, self.oracle, {"de": record})
        jev = scored["jev"]["languages"]["de"]
        self.assertEqual((jev["correct"], jev["incorrect"], jev["missing"]), (11, 1, 0))
        self.assertEqual(jev["accuracy"], round(11 / 12, 6))
        self.assertEqual(jev["coverage"], 1)
        self.assertEqual(jev["cases"][2]["choice"], "insufficient_evidence")
        self.assertEqual(jev["cases"][2]["outcome"], "correct")
        snap = scored["subq_snap"]["languages"]["de"]
        self.assertEqual((snap["missing"], snap["accuracy"], snap["coverage"]), (12, None, 0))
        openai = scored["openai_decisions"]["languages"]["de"]
        self.assertEqual((openai["correct"], openai["incorrect"], openai["missing"]), (11, 0, 1))
        self.assertEqual(openai["cases"][0]["missing_reason"], "refusal")
        self.assertEqual(record["final_decision"], {"status": "pending", "questions": {}})

    def test_consistency_counts_only_complete_triplets_and_is_not_accuracy(self):
        records = {language: self.record(language) for language in evaluation.LANGUAGES}
        shared, prepared = self.shared_and_prepared("de")
        records["de"] = consultation.make_record(shared, prepared, {
            name: success(value, {"c01": "contradicted"}, refusal=("c02",) if name == "openai_decisions" else ())
            for name, value in prepared.items()}, version=3)
        scored = evaluation.score_records(self.inputs, self.oracle, records)
        self.assertEqual(scored["jev"]["consistency"]["consistent_triplets"], 11)
        self.assertEqual(scored["jev"]["consistency"]["complete_triplets"], 12)
        self.assertEqual(scored["openai_decisions"]["consistency"]["complete_triplets"], 11)
        self.assertEqual(scored["openai_decisions"]["consistency"]["consistent_triplets"], 10)
        self.assertNotEqual(scored["jev"]["consistency"]["ratio"], scored["jev"]["totals"]["accuracy"])
        empty = evaluation.score_records(self.inputs, self.oracle, {})
        self.assertIsNone(empty["jev"]["consistency"]["ratio"])
        self.assertIsNone(evaluation.metrics([])["correct_over_total"])

    def test_model_mismatch_is_missing_coverage(self):
        shared, prepared = self.shared_and_prepared()
        raw = {name: success(value) for name, value in prepared.items()}
        raw["jev"]["result"]["model"] = "jev-1.13.0"
        record = consultation.make_record(shared, prepared, raw, version=3)
        self.assertEqual(record["providers"]["jev"]["error"]["category"], "invalid_response")
        score = evaluation.score_records(self.inputs, self.oracle, {"en": record})
        self.assertEqual(score["jev"]["languages"]["en"]["missing"], 12)

    def test_offline_main_never_looks_up_credentials_dispatches_or_writes(self):
        environ_get = os.environ.get

        def guard_credentials(name, default=None):
            if name in {item[1] for item in consultation.PROVIDERS.values()}:
                raise AssertionError("credential lookup")
            return environ_get(name, default)

        with tempfile.TemporaryDirectory() as temporary:
            corpus = Path(temporary)
            for name in ("inputs.json", "oracle.json"):
                (corpus / name).write_bytes(self.raw[name])
            with mock.patch.object(evaluation, "CORPUS_ROOT", corpus), \
                mock.patch.object(evaluation.os.environ, "get", side_effect=guard_credentials), \
                mock.patch.object(consultation, "collect", side_effect=AssertionError("network call")), \
                mock.patch.object(evaluation.os, "open", side_effect=AssertionError("file write")), \
                mock.patch("sys.stdout", new_callable=io.StringIO) as output:
                self.assertEqual(evaluation.main([]), 0)
        plan = json.loads(output.getvalue())
        self.assertEqual(plan["network_requests_made"], 0)
        self.assertEqual(plan["max_network_requests"], 9)

    def test_live_flag_and_output_are_required_together(self):
        for args in (["--live"], ["--output-dir", "/unused"], ["--policy", "/unused"]):
            with self.subTest(args=args), mock.patch("sys.stderr", new_callable=io.StringIO):
                self.assertEqual(evaluation.main(args), 1)

    def test_live_completed_with_wrong_answers_is_success_and_has_private_reports(self):
        with tempfile.TemporaryDirectory(prefix="multilingual eval ") as temporary:
            destination = Path(temporary) / "baseline with spaces"
            with mock.patch.object(consultation, "collect", side_effect=successful_collect) as dispatch:
                self.assertEqual(self.run_at(destination), 0)
            self.assertEqual(dispatch.call_count, 3)
            report = json.loads((destination / "report.json").read_text())
            self.assertEqual(report["status"], "completed")
            self.assertEqual(report["scoring"]["jev"]["totals"]["incorrect"], 24)
            self.assertEqual(report["execution"]["max_network_requests"], 9)
            self.assertEqual(report["execution"]["completed_batches"], list(evaluation.LANGUAGES))
            self.assertEqual(report["provenance"]["artifacts"]["oracle.json"]["sha256"],
                             evaluation.digest(self.raw["oracle.json"]))
            self.assertEqual(report["batches"]["en"]["providers"]["jev"]["usage"]["input_tokens"], 24)
            self.assertNotIn("per_case_tokens", json.dumps(report))
            self.assertEqual(destination.stat().st_mode & 0o777, 0o700)
            for path in destination.iterdir():
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            summary = (destination / "summary.md").read_text()
            self.assertIn("synthetic", summary)
            for provider in evaluation.PROVIDERS:
                for language in evaluation.LANGUAGES:
                    self.assertIn("| " + provider + " | " + language + " |", summary)
            self.assertIn("Consistent / complete triplets", summary)

    def test_existing_output_and_files_are_never_overwritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / "existing"
            destination.mkdir()
            sentinel = destination / "report.json"
            sentinel.write_text("original")
            with self.assertRaises(PolicyError), mock.patch.object(consultation, "collect") as dispatch:
                self.run_at(destination)
            dispatch.assert_not_called()
            self.assertEqual(sentinel.read_text(), "original")
            with self.assertRaises(FileExistsError):
                evaluation.write_private(destination, "report.json", b"replacement")
            self.assertEqual(sentinel.read_text(), "original")

    def test_source_and_snapshot_exclusions_follow_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            source = base / "source tree"
            source.mkdir()
            (source / ".git").write_text("gitdir: /other/place")
            alias = base / "alias"
            alias.symlink_to(source, target_is_directory=True)
            snapshot = base / "unrelated snapshot"
            snapshot.mkdir()
            (snapshot / "skills-manifest.json").write_text("{}")
            snapshot_alias = base / "snapshot alias"
            snapshot_alias.symlink_to(snapshot, target_is_directory=True)
            for target in (source / "output", alias / "output", snapshot / "output", snapshot_alias / "output",
                           evaluation.SKILL_ROOT / "output"):
                with self.subTest(target=target), self.assertRaises(PolicyError):
                    evaluation.output_path(target)
            allowed = evaluation.output_path(base / "separate output")
            self.assertEqual(allowed, (base / "separate output").resolve())

    def test_default_xdg_and_current_snapshot_release_roots_are_protected(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            default = base / ".local" / "state" / "agent-workflows" / "releases"
            custom = base / "custom state" / "agent-workflows" / "releases"
            current = base / "other state" / "releases" / "revision" / "skills" / "task-orchestration"
            for directory in (default, custom, current):
                directory.mkdir(parents=True)
            with mock.patch.object(evaluation.Path, "home", return_value=base), \
                    mock.patch.dict(os.environ, {"XDG_STATE_HOME": str(base / "custom state")}), \
                    mock.patch.object(evaluation, "SKILL_ROOT", current):
                for target in (default / "output", custom / "output", current.parents[2] / "output"):
                    with self.subTest(target=target), self.assertRaises(PolicyError):
                        evaluation.output_path(target)

    def test_interrupt_retains_completed_batch_and_marks_remaining_missing(self):
        calls = []

        def interrupt_after_first(prepared, credentials, deadline, **kwargs):
            calls.append(prepared)
            if len(calls) == 2:
                raise KeyboardInterrupt()
            return successful_collect(prepared, credentials, deadline, **kwargs)

        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / "interrupted"
            with mock.patch.object(consultation, "collect", side_effect=interrupt_after_first):
                self.assertEqual(self.run_at(destination), 130)
            first = (destination / "en.record.json").read_bytes()
            report = json.loads((destination / "report.json").read_text())
            self.assertEqual(report["status"], "interrupted")
            self.assertEqual(report["execution"]["completed_batches"], ["en"])
            self.assertEqual(report["scoring"]["jev"]["totals"]["missing"], 24)
            self.assertFalse((destination / "uk.record.json").exists())
            self.assertEqual((destination / "en.record.json").read_bytes(), first)

    def test_actual_helper_missing_credentials_preserves_missing_coverage(self):
        with tempfile.TemporaryDirectory() as temporary, mock.patch.dict(os.environ, {}, clear=True):
            destination = Path(temporary) / "no credentials"
            self.assertEqual(self.run_at(destination), 2)
            report = json.loads((destination / "report.json").read_text())
            for provider in evaluation.PROVIDERS:
                self.assertEqual(report["scoring"][provider]["totals"]["missing"], 36)
                self.assertIsNone(report["scoring"][provider]["totals"]["accuracy"])
                self.assertEqual(report["batches"]["en"]["providers"][provider]["error"],
                                 {"category": "missing_credential"})

    @unittest.skipUnless(hasattr(signal, "setitimer") and "fork" in multiprocessing.get_all_start_methods(),
                         "bounded native worker test requires POSIX fork")
    def test_actual_collector_deadline_is_bounded_without_retries(self):
        shared, prepared = self.shared_and_prepared()
        started = time.monotonic()
        raw = consultation.collect(prepared, {provider: "test" for provider in prepared}, 0.15,
                                   context=multiprocessing.get_context("fork"), worker=slow_worker,
                                   required_providers=set(evaluation.PROVIDERS))
        self.assertLess(time.monotonic() - started, 2)
        record = consultation.make_record(shared, prepared, raw, version=3)
        for value in record["providers"].values():
            self.assertEqual(value["status"], "unavailable")
            self.assertEqual(value["error"]["category"], "timeout")

    def test_credential_and_raw_exception_values_are_excluded(self):
        def transport_failure(*_args, **_kwargs):
            raise RuntimeError("SECRET_CANARY_DO_NOT_RETAIN")

        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / "error"
            with mock.patch.object(consultation, "collect", side_effect=transport_failure), \
                    mock.patch.dict(os.environ, {"TYPESAFE_API_KEY": "SECRET_CANARY_DO_NOT_RETAIN"}):
                self.assertEqual(self.run_at(destination), 1)
            self.assertNotIn("SECRET_CANARY_DO_NOT_RETAIN", "\n".join(
                path.read_text() for path in destination.iterdir()))
            self.assertEqual(json.loads((destination / "report.json").read_text())["status"], "error")

    def test_unknown_provider_fields_are_not_retained(self):
        def with_unsafe_extras(prepared, credentials, deadline, **kwargs):
            raw = successful_collect(prepared, credentials, deadline, **kwargs)
            for value in raw.values():
                value.update(headers={"Authorization": "SECRET_CANARY"}, internal_trace="SECRET_CANARY")
                value["result"]["diagnostic"] = "SECRET_CANARY"
            return raw

        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / "sanitized"
            with mock.patch.object(consultation, "collect", side_effect=with_unsafe_extras):
                self.assertEqual(self.run_at(destination), 0)
            self.assertNotIn("SECRET_CANARY", "\n".join(path.read_text() for path in destination.iterdir()))

    def test_bundled_resources_work_from_unrelated_cwd_and_paths_with_spaces(self):
        with tempfile.TemporaryDirectory(prefix="isolated eval ") as temporary:
            base = Path(temporary)
            skill = base / "managed skill with spaces"
            shutil.copytree(evaluation.SKILL_ROOT, skill)
            corpus = skill / "evals" / "multilingual"
            corpus.mkdir(parents=True, exist_ok=True)
            for name in ("inputs.json", "oracle.json"):
                (corpus / name).write_bytes(self.raw[name])
            unrelated = base / "unrelated project"
            unrelated.mkdir()
            (unrelated / ".git").mkdir()
            before = sorted(str(path.relative_to(skill)) for path in skill.rglob("*"))
            completed = subprocess.run([sys.executable, "-B", str(skill / "scripts" / "evaluate_multilingual.py")],
                                       cwd=str(unrelated), stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       timeout=10, check=False)
            self.assertEqual(completed.returncode, 0, completed.stderr.decode())
            self.assertEqual(json.loads(completed.stdout)["status"], "offline_validated")
            self.assertEqual(before, sorted(str(path.relative_to(skill)) for path in skill.rglob("*")))
            self.assertFalse(any(path.name == "__pycache__" for path in skill.rglob("*")))


if __name__ == "__main__":
    unittest.main()
