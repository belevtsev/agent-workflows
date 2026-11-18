"""Replay saved qs review rounds with a full history and a ticketed reviewer input (Linux)."""
import argparse
import hashlib
from itertools import product
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
for flag in ("previous", "kitsu", "worktree", "node", "modules"):
    parser.add_argument("--" + flag, type=Path, required=True)
parser.add_argument("--repetitions", type=int, default=1, help="Repeat with fresh task tickets to measure payload variation")
parser.add_argument("--tokenizer-dir", type=Path, help="Optional trusted Linux x64 Copilot distribution containing the local o200k_base encoder")
args = parser.parse_args()
if args.repetitions < 1:
    parser.error("--repetitions must be positive")
encoder = {}
if args.tokenizer_dir:
    binding = (args.tokenizer_dir / "compiled/linux/x64/copilot-tokenizer.node").resolve(strict=True)
    vocab = (args.tokenizer_dir / "resources/o200k_base.tiktoken.noindex").resolve(strict=True)
    encoder = {"encoding": "o200k_base", "binding_sha256": hashlib.sha256(binding.read_bytes()).hexdigest(),
               "vocabulary_sha256": hashlib.sha256(vocab.read_bytes()).hexdigest()}

def count_payloads(items):
    code = """
const fs = require("node:fs");
const t = require(process.argv[1]);
t.tokenInitEncoder("o200k_base", fs.readFileSync(process.argv[2]));
const values = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(values.map(x => t.tokenCount(x, "o200k_base"))));
"""
    proc = subprocess.run([str(args.node / "bin/node"), "-e", code, str(binding), str(vocab)],
                          input=json.dumps(items), text=True, capture_output=True, check=True, timeout=30)
    return json.loads(proc.stdout)

task = "qs-sparse-array-limit"
record = json.loads((args.worktree / ".git/kitsu-stages" / f"task-harness-{task}.json").read_text())
reviews = [e for e in record["events"] if e["action"] == "review"]
trees = [r["candidate"]["tree"] for r in reviews]
assert trees == [r["tree"] for r in json.loads((Path(__file__).parent / "daily-review-data/qs-paid.json").read_text())["repair_snapshot_replay"]]
assert len(reviews) == 4
rows = []
arms = (("full_history", args.previous), ("reserved_input", args.kitsu))
for repeat, (mode, binary) in product(range(1, args.repetitions + 1), arms):
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        work = root / "work"
        work.mkdir()
        (root / "final-tests").mkdir()

        def run(cmd, *, readonly=False, mounts=(), expected=0):
            argv = ["bwrap", "--unshare-all", "--new-session", "--die-with-parent", "--clearenv",
                    "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--ro-bind", "/lib", "/lib",
                    "--ro-bind-try", "/lib64", "/lib64", "--proc", "/proc", "--dev", "/dev",
                    "--tmpfs", "/tmp", "--dir", "/home/replay", "--ro-bind", str(binary.resolve()), "/opt/kitsu/bin/kitsu",
                    "--ro-bind" if readonly else "--bind", str(work), "/work", "--chdir", "/work",
                    "--setenv", "PATH", "/opt/kitsu/bin:/opt/node/bin:/usr/bin:/bin", "--setenv", "HOME", "/home/replay",
                    "--setenv", "GIT_CONFIG_NOSYSTEM", "1", "--setenv", "GIT_CONFIG_GLOBAL", "/dev/null",
                    "--setenv", "GIT_OPTIONAL_LOCKS", "0", *mounts, "--", *cmd]
            result = subprocess.run(argv, capture_output=True, timeout=60)
            if result.returncode != expected:
                raise RuntimeError((mode, cmd[:3], result.returncode, result.stderr[-1500:]))
            return result.stdout

        run(["git", "init", "-q"])
        shutil.copytree(args.worktree / ".git/objects", work / ".git/objects", dirs_exist_ok=True)
        (work / "node_modules").mkdir(exist_ok=True)
        start = record["starting_snapshot"]
        run(["git", "reset", "--hard", start["head"]])
        run(["git", "config", "core.autocrlf", "false"])
        def task_cmd(verb, *opts):
            return ["kitsu", "task", verb, task, *opts]

        opened = run(task_cmd("open", "--request", record["request"], "--entry", "review",
                                          "--until", "deliver", "--commit", "--repair", "--brief"))
        receipt = json.loads(opened)
        node_mounts = ["--ro-bind", str(args.node.resolve()), "/opt/node",
                       "--ro-bind", str(args.modules.resolve()), "/work/node_modules"]
        final_mounts = []
        for name in ("utils.js", "parse.js"):
            saved = root / "final-tests" / name
            saved.write_bytes(run(["git", "show", f"{trees[-1]}:test/{name}"]))
            final_mounts += ["--ro-bind", str(saved), f"/work/test/{name}"]
        for n, (work_event, review) in enumerate(zip(record["events"][::2], reviews, strict=True)):
            tree = review["candidate"]["tree"]
            # The source changes between rounds, while the original index stays in place.
            run(["git", "read-tree", "--reset", "-u", tree])
            run(["git", "read-tree", start["index"]])
            data = run(task_cmd("record", work_event["action"], "--actor", "host", "--ticket", receipt["ticket"],
                                "--note", work_event["note"], "--claim-review", "codex", "--brief"))
            receipt = json.loads(data)
            assert receipt["claim_created"]
            lookup = receipt.get("review_input", task_cmd("show", "--json"))
            result = run(lookup, readonly=mode == "reserved_input")
            view = json.loads(result)
            if mode == "reserved_input":
                assert view["reserved_candidate"] == review["candidate"]
                assert view["request"] == record["request"] and view["scope"] == record["scope"]
                assert view["intake"] == record["events"][0]["note"] and view["review_round"] == n + 1
                assert view["reviewer"] == receipt["claim"]["reviewer"]
                if n:
                    assert view["prior_findings"] == reviews[n-1]["findings"]
                    assert view["prior_review"] == reviews[n-1]["note"]
                    assert view["reported_repair"] == work_event["note"]
                    assert view["reported_work_tree"] == tree
                    assert view["review_changes"] in (
                        ["git", "diff", trees[n-1], tree, "--"],
                        ["git", "--no-replace-objects", "diff", "--no-ext-diff", "--no-textconv", trees[n-1], tree, "--"],
                    )
            else:
                assert view["harness"]["candidate"] == review["candidate"]
                assert view["events"][-1]["note"] == work_event["note"]
                if n:
                    assert view["harness"]["latest_review"]["findings"] == reviews[n-1]["findings"]
            row = dict(repetition=repeat, round=n+1, mode=mode, candidate=tree, previous_findings=len(reviews[n-1].get("findings", [])) if n else 0,
                       coordinator_claim_bytes=len(data), lookup_response_bytes=len(result),
                       forwarded_command_bytes=len(json.dumps(lookup, separators=(",", ":")).encode()))
            if encoder:
                fields = ["coordinator_claim", "lookup_response", "forwarded_command"]
                tokens = count_payloads([data.decode(), result.decode(), json.dumps(lookup, separators=(",", ":"))])
                row.update({field + "_payload_tokens": count for field, count in zip(fields, tokens, strict=True)})
            if mode == "reserved_input" and repeat == 1:
                suite = ["node", "/work/node_modules/tape/bin/tape", "test/**/*.js"]
                own = run(suite, readonly=True, mounts=node_mounts)
                final = run(suite, readonly=True, mounts=node_mounts+final_mounts, expected=0 if n == 3 else 1)
                row["candidate_suite_passed"] = len(re.findall(rb"^ok ", own, re.M))
                row["final_suite_failed_assertions"] = len(re.findall(rb"^not ok ", final, re.M))
                row["final_suite_completed_tap_plan"] = bool(re.search(rb"^1\.\.[0-9]+", final, re.M))
                assert b"# ok" in own and row["final_suite_failed_assertions"] == [9, 1, 2, 0][n]
            rows.append(row)
            findings = [arg for finding in review.get("findings", []) for arg in ("--finding", finding)]
            recorded = run(task_cmd("record", "review", "--actor", f"reviewer-{n+1}", "--ticket",
                                               receipt["ticket"], "--note", review["note"], *findings, "--brief"))
            row["coordinator_other_bytes"] = len(recorded) + (len(opened) if n == 0 else 0)
            if encoder:
                messages = [recorded.decode()] + ([opened.decode()] if n == 0 else [])
                row["coordinator_other_payload_tokens"] = sum(count_payloads(messages))
            receipt = json.loads(recorded)
            assert receipt["next"] == ("verify" if n == 3 else "repair")
print(json.dumps({"task": task, "model_calls": 0, "local_tokenizer": encoder or None, "previous_binary_sha256": hashlib.sha256(args.previous.read_bytes()).hexdigest(),
                  "binary_sha256": hashlib.sha256(args.kitsu.read_bytes()).hexdigest(), "rounds": rows}, indent=2))
