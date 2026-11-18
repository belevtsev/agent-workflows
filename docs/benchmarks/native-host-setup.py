"""Replay Kitsu plugin discovery in Linux namespaces. Pass the matching Kitsu binary as argv[1]."""
import argparse
import asyncio
import hashlib
import json
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from zipfile import ZipFile


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


async def codex_inventory(command):
    process = await asyncio.create_subprocess_exec(
        *command, "codex", "app-server", stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL, limit=2**22,
    )

    async def request(seq, method, params):
        process.stdin.write((json.dumps({"id": seq, "method": method, "params": params}) + "\n").encode())
        await process.stdin.drain()
        while line := await process.stdout.readline():
            reply = json.loads(line)
            if reply.get("id") == seq:
                if "error" in reply:
                    raise RuntimeError(reply["error"])
                return reply["result"]
        raise RuntimeError("Codex exited before returning " + method)

    try:
        async with asyncio.timeout(40):
            await request(1, "initialize", {"clientInfo": {"name": "kitsu_audit", "version": "1"},
                                          "capabilities": {"experimentalApi": True}})
            process.stdin.write(b'{"method":"initialized"}\n')
            skills = await request(2, "skills/list", {"cwds": ["/work"]})
            hooks = await request(3, "hooks/list", {"cwds": ["/work"]})
    finally:
        if process.returncode is None:
            process.terminate()
        await process.wait()
    items = sorted(s["name"] for row in skills["data"] for s in row["skills"]
                   if s["enabled"] and s["name"].startswith("kitsu:"))
    trust = {h["eventName"]: h["trustStatus"] for row in hooks["data"] for h in row["hooks"]
             if h["pluginId"] == "kitsu@kitsu"}
    assert items == ["kitsu:kitsu", "kitsu:task"]
    assert set(trust) == {"preToolUse", "sessionStart", "subagentStop", "sessionEnd"}
    assert all(value == "untrusted" for value in trust.values()), trust
    return {"enabled_skills": items, "fresh_install_hook_trust": trust}


def run(kitsu, root, archives=None, legacy_skill=None):
    repo = Path(__file__).resolve().parents[2]
    arch = {"x86_64": "amd64", "aarch64": "arm64"}[platform.machine()]
    identity = subprocess.check_output([str(kitsu), "version"], text=True, timeout=10).splitlines()[0]
    tag = identity.removeprefix("kitsu ")
    if not re.fullmatch(r"v[0-9][A-Za-z0-9._+-]*", tag):
        raise ValueError("Build or supply a versioned Kitsu binary")
    filename = f"kitsu_{tag}_linux_{arch}.tar.gz"
    source = "GoReleaser archives"
    if archives is None:
        source = "locally assembled archives"
        archives = root / "archives"
        archives.mkdir()
        with tarfile.open(archives / filename, "w:gz") as tar:
            tar.add(kitsu, arcname="kitsu")
        subprocess.run([sys.executable, str(repo / ".github/scripts/package-plugin.py"),
                        str(archives / "kitsu-plugin.zip")], check=True)
        shutil.copy2(repo / "install.sh", archives / "install.sh")
        (archives / "SHA256SUMS").write_text("".join(
            f"{digest(archives / name)}  {name}\n" for name in [filename, "kitsu-plugin.zip", "install.sh"]))
    plugin = archives / "kitsu-plugin.zip"
    checksums = (archives / "SHA256SUMS").read_text().splitlines()
    for file in (plugin, archives / "install.sh"):
        rows = [line.split() for line in checksums if line.split()[-1] == file.name]
        assert rows == [[digest(file), file.name]]
    with ZipFile(plugin) as package:
        package.extractall(root)
    market = root / "kitsu-marketplace"
    for directory in ["home", "app"]:
        (root / directory).mkdir()
    cmd = ["bwrap", "--unshare-all", "--new-session", "--die-with-parent", "--clearenv",
           "--ro-bind", "/usr", "/usr", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
           "--symlink", "usr/bin", "/bin", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp",
           "--dir", "/etc", "--ro-bind", "/etc/passwd", "/etc/passwd", "--ro-bind", "/etc/group", "/etc/group",
           "--dir", "/tools", "--ro-bind", str(Path(shutil.which("awk")).resolve()), "/tools/awk", "--bind", str(root / "home"), "/home/auditor", "--bind", str(root / "app"), "/work",
           "--ro-bind", str(market), "/market", "--ro-bind", str(archives), "/archives",
           "--ro-bind", str(archives / "install.sh"), "/tools/install.sh"]
    for host in ["claude", "codex"]:
        program = shutil.which(host)
        if program is None:
            raise RuntimeError("Install the native " + host + " executable before running the replay")
        cmd += ["--ro-bind", str(Path(program).resolve()), "/tools/" + host]
    cmd += ["--setenv", "HOME", "/home/auditor", "--setenv", "PATH", "/home/auditor/.local/bin:/tools:/usr/bin",
            "--setenv", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1", "--setenv", "TERM", "dumb", "--chdir", "/work", "--"]

    def invoke(*args, data=None):
        result = subprocess.run([*cmd, *args], input=data, capture_output=True, text=True, timeout=45)
        if result.returncode:
            raise RuntimeError(f"{args} failed ({result.returncode}): {result.stderr.strip()}")
        return result.stdout

    report = {"os": "Linux", "kitsu_binary_sha256": digest(kitsu), "model_requests": 0}
    invoke("sh", "/tools/install.sh", "--version", tag, "--archive-dir", "/archives", "--no-modify-path")
    installed = root / "home/.local/bin/kitsu"
    assert digest(installed) == digest(kitsu)
    report["install"] = {"source": source, "plugin_zip_sha256": digest(plugin), "version": tag, "installed_binary_sha256": digest(installed)}
    invoke("git", "init", "-q")
    for host, verb in [("codex", "add"), ("claude", "install")]:
        invoke(host, "plugin", "marketplace", "add", "/market")
        invoke(host, "plugin", verb, "kitsu@kitsu")
        checks = json.loads(invoke("kitsu", "agent", "check", "--host", host, "--json"))
        preview = json.loads(invoke("kitsu", "task", "start", "Verify a staged patch", "--host", host, "--until", "verify", "--json"))
        assert checks["ready"] and "Verify a staged patch" in preview["prompt"]
        report[host] = {"version": invoke(host, "--version").strip(), "ready": checks["ready"],
                        "checks": {c["id"]: c["status"] for c in checks["checks"]}, "handoff_preview": True}

    report["codex"].update(asyncio.run(codex_inventory(cmd)))
    details = invoke("claude", "plugin", "details", "kitsu@kitsu")
    estimate = int(re.search(r"Always-on:\s*~?([\d,]+) tok", details)[1].replace(",", ""))
    request = {"type": "control_request", "request_id": "kitsu-audit", "request": {"subtype": "initialize"}}
    stream = invoke("claude", "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
                    data=json.dumps(request) + "\n")
    messages = [json.loads(line) for line in stream.splitlines()]
    init = next(m["response"]["response"] for m in messages if m.get("type") == "control_response"
                and m["response"]["request_id"] == request["request_id"] and m["response"]["subtype"] == "success")
    hook = next(m for m in messages if m.get("subtype") == "hook_response"
                and m["hook_event"] == "SessionStart" and "/kitsu:task" in m.get("stdout", ""))
    commands = sorted(c["name"] for c in init["commands"] if c["name"].startswith("kitsu:"))
    agents = sorted(a["name"] for a in init["agents"] if a["name"].startswith("kitsu:"))
    assert commands == ["kitsu:kitsu", "kitsu:task"] and len(agents) == 7
    assert hook["exit_code"] == 0 and hook["outcome"] == "success"
    report["claude"].update(commands=commands, agents=agents, session_start_exit=hook["exit_code"],
                            projected_always_on_tokens=estimate, task_skill_sha256=digest(market / "skills/task/SKILL.md"))
    invoke("git", "config", "user.name", "Kitsu audit")
    invoke("git", "config", "user.email", "audit@example.invalid")
    invoke("git", "commit", "--allow-empty", "-qm", "Starting revision")
    item = root / "app/export.txt"
    item.write_text("verified export\n")
    def task(action, *args):
        return json.loads(invoke("kitsu", "task", action, "export", "--brief", *args))
    started = task("open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit the existing export")
    ready = task("record", "intake", "--ticket", started["ticket"], "--actor", "offline-driver", "--note", "Check the exported file")
    proof = task("test", "--ticket", ready["ticket"], "--expect", "The export contains the expected line",
                 "--", "git", "grep", "--no-index", "-e", "^verified export$", "--", "export.txt")
    assert proof["next"] == "commit"
    invoke("git", "add", "export.txt")
    task("commit", "--ticket", proof["ticket"], "--message", "Deliver export", "--include", "export.txt")
    resumed = task("next")
    assert resumed["next"] == "done" and resumed["verified"] and resumed["delivery"] == "committed"
    assert invoke("git", "status", "--porcelain") == ""
    report["installed_cli_journey"] = {"entry": "verify", "resumed": resumed["next"],
                                      "delivery": resumed["delivery"], "clean_checkout": True, "host_judgment": "scripted"}
    if legacy_skill:
        # Replace only the installed task-skill bytes in these disposable homes and marketplace.
        candidates = [market / "skills/task/SKILL.md", *(root / "home").rglob("skills/task/SKILL.md")]
        original = (market / "skills/task/SKILL.md").read_bytes()
        copies = [file for file in candidates if file.is_file() and file.read_bytes() == original]
        old = legacy_skill.read_bytes()
        for file in copies:
            file.write_bytes(old)
        for host in ("codex", "claude"):
            failed = subprocess.run([*cmd, "kitsu", "agent", "check", "--host", host, "--json"],
                                    capture_output=True, text=True, timeout=45)
            checks = json.loads(failed.stdout)
            skill = next(row for row in checks["checks"] if row["id"] == "task_skill")
            assert failed.returncode != 0 and not checks["ready"] and skill["status"] == "fail", checks
            assert "does not declare harness protocol" in skill["detail"], skill
            report[host]["legacy_task_skill_control"] = {"exit": failed.returncode, "detail": skill["detail"],
                                                     "skill_sha256": digest(legacy_skill)}
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--archives", type=Path, help="Use existing platform and plugin archives with their installers and SHA256SUMS")
    parser.add_argument("--legacy-skill", type=Path, help="Overlay an old task skill in the disposable install and check the version diagnostic")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="kitsu-native-") as tmp:
        print(json.dumps(run(args.binary.resolve(strict=True), Path(tmp),
                             args.archives.resolve(strict=True) if args.archives else None, args.legacy_skill), indent=2))
