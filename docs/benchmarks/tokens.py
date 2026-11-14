"""Runs one arm of the token benchmark: a headless Claude Code session on one small task in a new
scratch repository.

Usage: python3 tokens.py <cw> <plugin> <arm> <task> <out-dir>

  <cw>      the cw program to use; its directory is put first on PATH
  <plugin>  the root of a checkout of this repository, given to Claude Code with --plugin-dir
  <arm>     plain  one agent in a repository without cw and without the plugin
            gate   one agent in a repository with the gate and the plugin
            roles  the role workflow: the skill `task` of the plugin
            roles-asked  the same, and the request asks for the roles also for a small change
  <task>    duration (Python, new behaviour) or slug (Go, a defect)

It writes <out-dir>/<task>-<arm>/: the repository, the event stream of every turn as the host
printed it (turn-N.jsonl), and summary.json with the cost, the tokens by model, the roles that
were started and what `cw tokens` reports. When a session is resumed, the host reports the cost
and the tokens of the whole session so far, so the last turn holds the totals.

The session runs with the settings of the scratch repository only, without MCP servers, on the
model MODEL, and may edit files and run commands in that repository without asking. In the arm
`roles` the developer is not there: when the session stops before the task is committed, the
script answers with REPLY, up to seven times. It spends real tokens of the account that
`claude` is logged in to; the arm `roles` is capped at CAP dollars at list price.

It needs `claude` (Claude Code), git, Python 3, and Go for the task `slug`.
docs/benchmarks.md holds the numbers of one run and what they do and do not show.
"""
import json
import os
import subprocess
import sys
import time

CW_DIR = os.path.dirname(os.path.abspath(sys.argv[1]))
PLUGIN = os.path.abspath(sys.argv[2])
MODEL = "opus"
CAP = 30.0
REPLY = ("Yes, approved. Go on with the next stage. I cannot answer more questions today: where the workflow needs "
         "my decision, take the option you recommend, tell me which, and record it as my approval with today's date.")

PY_FILES = {
    ".gitignore": "__pycache__/\n",
    "calc/__init__.py": "def add(a, b):\n    return a + b\n",
    "tests/__init__.py": "",
    "tests/test_calc.py": (
        "import unittest\n\nfrom calc import add\n\n\nclass AddTest(unittest.TestCase):\n"
        "    def test_add(self):\n        self.assertEqual(add(2, 3), 5)\n\n\n"
        'if __name__ == "__main__":\n    unittest.main()\n'),
}
GO_FILES = {
    "go.mod": "module example.test/slug\n\ngo 1.24\n",
    "slug.go": (
        "// Package slug turns a title into a part of a URL.\npackage slug\n\nimport \"strings\"\n\n"
        "// Slugify returns title in lower case with every run of characters that are not\n"
        "// letters or digits replaced by one hyphen, and no hyphen at either end.\n"
        "func Slugify(title string) string {\n\tvar b strings.Builder\n"
        "\tfor _, r := range strings.ToLower(title) {\n"
        "\t\tif (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {\n\t\t\tb.WriteRune(r)\n\t\t} else {\n"
        "\t\t\tb.WriteByte('-')\n\t\t}\n\t}\n\treturn strings.Trim(b.String(), \"-\")\n}\n"),
    "slug_test.go": (
        "package slug\n\nimport \"testing\"\n\nfunc TestSlugify(t *testing.T) {\n"
        "\tif got := Slugify(\"Hello World\"); got != \"hello-world\" {\n"
        "\t\tt.Fatalf(\"got %q\", got)\n\t}\n}\n"),
}

TASKS = {
    # New behaviour: the coordinator should take the full path.
    "duration": {
        "files": PY_FILES,
        "checks": [["test", "python3", "-m", "unittest", "discover", "-s", "tests", "-t", "."]],
        "tests": ["tests/**"],
        "text": ("Add a function `parse_duration(text)` to the package `calc`. It turns a duration written as text, "
                 "such as `1h30m`, `45s`, `2d` or `1d2h3m4s`, into the number of seconds as an int. The units are "
                 "d, h, m and s, each at most once and in that order. Anything else, also an empty text, is a "
                 "ValueError."),
    },
    # A defect against stated behaviour: the coordinator should take the small path.
    "slug": {
        "files": GO_FILES,
        "checks": [["test", "go", "test", "./..."], ["vet", "go", "vet", "./..."]],
        "tests": ["**/*_test.go"],
        "text": ("There is a defect in `Slugify`: `Slugify(\"Hello,  World\")` returns `hello---world`. The comment "
                 "of the function says that every run of other characters becomes one hyphen, so it must return "
                 "`hello-world`. Fix it."),
    },
}


def sh(args, cwd, env=None, check=True):
    done = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and done.returncode != 0:
        raise SystemExit("%s failed in %s:\n%s%s" % (args, cwd, done.stdout, done.stderr))
    return done


def environment(with_cw):
    env = dict(os.environ)
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    if with_cw:
        env["PATH"] = CW_DIR + ":" + env["PATH"]
    return env


def make_repo(repo, task, arm):
    os.makedirs(repo)
    for name, content in task["files"].items():
        full = os.path.join(repo, name)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        open(full, "w").write(content)
    sh(["git", "init", "-q", "-b", "main", "."], repo)
    sh(["git", "config", "user.name", "Test User"], repo)
    sh(["git", "config", "user.email", "test@example.invalid"], repo)
    sh(["git", "add", "-A"], repo)
    sh(["git", "commit", "-q", "-m", "the first state"], repo)
    if arm == "plain":
        return
    env = environment(True)
    sh(["cw", "init"], repo, env)
    for check in task["checks"]:
        sh(["cw", "add", check[0], "--"] + check[1:], repo, env)
    if arm == "roles":
        patterns = ", ".join('"%s"' % p for p in task["tests"])
        open(os.path.join(repo, "cw-roles.toml"), "w").write("version = 1\n\n[paths]\ntests = [%s]\n" % patterns)
    sh(["cw", "run"], repo, env)
    sh(["git", "add", "-A"], repo)
    sh(["git", "commit", "-q", "-m", "declare the checks"], repo, env)


def turn(repo, prompt, arm, log, resume, budget):
    cmd = ["claude", "-p", prompt, "--setting-sources", "project", "--strict-mcp-config", "--model", MODEL,
           "--output-format", "stream-json", "--verbose", "--max-budget-usd", str(budget),
           "--permission-mode", "acceptEdits",
           "--allowedTools", "Bash", "Read", "Edit", "Write", "Glob", "Grep", "Task", "Agent", "Skill"]
    if arm != "plain":
        cmd += ["--plugin-dir", PLUGIN]
    if resume:
        cmd += ["--resume", resume]
    started = time.time()
    with open(log, "w") as out, open(log + ".err", "w") as err:
        code = subprocess.run(cmd, cwd=repo, env=environment(arm != "plain"), stdin=subprocess.DEVNULL,
                              stdout=out, stderr=err, timeout=3 * 3600).returncode
    info = {"exit": code, "seconds": round(time.time() - started, 1), "session": None, "result": None,
            "limits": {}, "agents": [], "denials": [], "last_text": ""}
    for line in open(log):
        try:
            event = json.loads(line)
        except ValueError:
            continue
        kind = event.get("type")
        if kind == "system" and event.get("subtype") == "init":
            info["session"] = event.get("session_id")
        elif kind == "rate_limit_event":
            rate = event.get("rate_limit_info", {})
            for name, window in (rate.get("unifiedWindows") or {}).items():
                info["limits"][name] = window.get("utilization")
        elif kind == "assistant" and not event.get("parent_tool_use_id"):
            for part in event["message"]["content"]:
                if part["type"] == "text":
                    info["last_text"] = part["text"]
                elif part["type"] == "tool_use" and part["name"] in ("Task", "Agent"):
                    given = part["input"]
                    info["agents"].append({"type": given.get("subagent_type"), "model": given.get("model"),
                                           "first_line": (given.get("prompt") or "").split("\n")[0][:120]})
        elif kind == "result":
            info["result"] = {k: event.get(k) for k in
                              ("subtype", "is_error", "num_turns", "duration_ms", "total_cost_usd", "modelUsage")}
            info["denials"] = [d.get("tool_input") for d in event.get("permission_denials") or []]
    return info


def finished(repo, first_commit, arm):
    head = sh(["git", "rev-parse", "HEAD"], repo).stdout.strip()
    clean = sh(["git", "status", "--porcelain"], repo).stdout.strip() == ""
    if arm == "plain":
        return head != first_commit and clean
    passing = sh(["cw", "status"], repo, environment(True), check=False).returncode == 0
    return head != first_commit and clean and passing


def main():
    arm, name, out = sys.argv[3], sys.argv[4], sys.argv[5]
    task = TASKS[name]
    base = os.path.join(out, "%s-%s" % (name, arm))
    asked = ""
    if arm == "roles-asked":
        # The coordinator offers to leave the workflow for a change of a few lines. This arm asks for the roles.
        arm, asked = "roles", " Take it through the roles on the path that fits; I ask for the roles, also if the change is small."
    if os.path.exists(base):
        raise SystemExit(base + " exists")
    os.makedirs(base)
    repo = os.path.join(base, "repo")
    make_repo(repo, task, arm)
    first_commit = sh(["git", "rev-parse", "HEAD"], repo).stdout.strip()
    if arm == "roles":
        prompt = "/agent-workflows:task " + task["text"] + asked
        turns, cap, budget = 8, CAP, 15
    else:
        prompt = task["text"] + " Write the tests it needs, make them pass, and commit the change."
        turns, cap, budget = 2, 6.0, 3
    summary = {"arm": arm, "task": name, "model": MODEL, "prompt": prompt, "turns": []}
    session, spent = None, 0.0
    for number in range(1, turns + 1):
        info = turn(repo, prompt if session is None else REPLY, arm, os.path.join(base, "turn-%d.jsonl" % number),
                    session, budget)
        summary["turns"].append(info)
        session = session or info["session"]
        # On a resumed session the host reports the cost and the tokens of the whole session so far,
        # not of the turn: the last turn holds the totals.
        spent = (info["result"] or {}).get("total_cost_usd") or spent
        done = finished(repo, first_commit, arm)
        summary["finished"], summary["cost_usd"] = done, round(spent, 4)
        json.dump(summary, open(os.path.join(base, "summary.json"), "w"), indent=1)
        if done or info["exit"] != 0 or info["result"] is None or spent >= cap:
            break
    env = environment(arm != "plain")
    summary["usage"] = ((summary["turns"][-1]["result"] or {}).get("modelUsage")) or {}
    summary["log"] = sh(["git", "log", "--format=%h %an %s"], repo).stdout
    summary["diffstat"] = sh(["git", "diff", "--stat", first_commit, "HEAD"], repo).stdout
    if arm != "plain":
        summary["cw_status"] = sh(["cw", "status"], repo, env, check=False).stdout
        summary["cw_tokens"] = sh(["cw", "tokens"], repo, env, check=False).stdout
        summary["cw_tokens_json"] = sh(["cw", "tokens", "--json"], repo, env, check=False).stdout
        summary["cw_stats"] = sh(["cw", "stats"], repo, env, check=False).stdout
    json.dump(summary, open(os.path.join(base, "summary.json"), "w"), indent=1)
    print(name, arm, "finished" if summary["finished"] else "NOT finished", "cost", summary["cost_usd"],
          "turns", len(summary["turns"]), "limits", summary["turns"][-1]["limits"])


main()
