#!/usr/bin/env python3
"""Guarded local Graphify discovery for tracked Go source. Standard library only."""

from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import errno
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

VERSION = "0.9.91"
SCHEMA = 1
EXCLUDED = frozenset({"third_party", "vendor", "testdata", "fixture", "fixtures", ".git", ".venv", "node_modules"})
SCOPE = {"languages": ["go"], "module_files": ["go.mod", "go.sum"], "excluded_components": sorted(EXCLUDED)}
QUALIFICATION = (
    "Undirected structural discovery only. EXTRACTED/INFERRED/AMBIGUOUS are upstream "
    "extraction labels, not verified correctness; even EXTRACTED call targets can "
    "be wrong for same-named functions. Verify call direction, interfaces, "
    "factories and dynamic dispatch against current source/gopls. Missing results do "
    "not prove absence."
)
MAX_FILE_BYTES = 16 * 1024 * 1024
MAX_SOURCE_BYTES = 128 * 1024 * 1024
MAX_GRAPH_BYTES = 128 * 1024 * 1024


class DiscoveryError(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def digest(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def inside(path, root):
    return path == root or root in path.parents


def process(argv, *, cwd, env, timeout):
    """Kill owned descendants on timeout/interruption; never invoke a shell."""
    if os.name != "posix":
        raise DiscoveryError("platform", "The guarded Graphify runner currently supports macOS and Linux")
    proc = subprocess.Popen(
        [str(arg) for arg in argv], cwd=cwd, env=env,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        start_new_session=os.name == "posix",
    )
    try:
        stdout, stderr = proc.communicate(timeout=timeout)
    except BaseException as exc:
        try:
            if os.name == "posix":
                os.killpg(proc.pid, signal.SIGKILL)
            else:
                proc.kill()
        except ProcessLookupError:
            pass
        proc.communicate()
        if isinstance(exc, subprocess.TimeoutExpired):
            raise DiscoveryError("timeout", "Graphify operation timed out; no incomplete graph was activated") from exc
        raise
    if len(stdout) + len(stderr) > MAX_GRAPH_BYTES:
        raise DiscoveryError("output_limit", "Tool output exceeded the local limit")
    return proc.returncode, stdout.decode("utf-8", errors="replace"), stderr.decode("utf-8", errors="replace")


def environment(runtime=None, output=None, child_home=None):
    # No inherited API keys, Python injection, Graphify flags, logging or backends.
    env = {key: os.environ[key] for key in ("HOME", "USERPROFILE", "SYSTEMROOT", "WINDIR", "TMPDIR") if key in os.environ}
    env.update({
        "PATH": os.pathsep.join(filter(None, [str(runtime.parent) if runtime else "", os.defpath])),
        "LANG": "C.UTF-8", "PYTHONHASHSEED": "0", "PYTHONDONTWRITEBYTECODE": "1",
        "GRAPHIFY_NO_AUTO_REFRESH": "1", "GRAPHIFY_QUERY_LOG_DISABLE": "1",
    })
    if output is not None:
        env["GRAPHIFY_OUT"] = str(output)
    if child_home is not None:
        # Graphify imports ~/.graphify/providers.json even in code-only mode.
        # Sandbox only the child process; parent runtime/cache resolution stays real.
        env.update({"HOME": str(child_home), "USERPROFILE": str(child_home), "XDG_CONFIG_HOME": str(child_home / ".config"), "XDG_CACHE_HOME": str(child_home / ".cache")})
    return env


def graphify_process(runtime, arguments, *, cwd, output=None, timeout):
    with tempfile.TemporaryDirectory(prefix="agent-graphify-home-") as directory:
        return process([runtime, *arguments], cwd=cwd, env=environment(runtime, output, Path(directory)), timeout=timeout)


def git(repo, *args, allow_missing=False):
    rc, out, err = process(["git", "-C", repo, *args], cwd=repo, env=environment(), timeout=15)
    if rc and not (allow_missing and rc == 1):
        raise DiscoveryError("git", err.strip() or "Git source inspection failed")
    return out


def source(repo):
    requested = Path(repo).expanduser().resolve()
    if not requested.is_dir():
        raise DiscoveryError("repository", "Repository directory is missing")
    root = Path(git(requested, "rev-parse", "--show-toplevel").strip()).resolve()
    revision = git(root, "rev-parse", "HEAD").strip()
    branch = git(root, "symbolic-ref", "--quiet", "--short", "HEAD", allow_missing=True).strip() or None
    files, content = [], {}
    total = 0
    for name in sorted(set(git(root, "ls-files", "--cached", "-z").split("\0"))):
        if not name:
            continue
        rel = PurePosixPath(name)
        if rel.is_absolute() or ".." in rel.parts:
            raise DiscoveryError("source_path", "Invalid tracked source path")
        if set(rel.parts) & EXCLUDED or not (rel.suffix == ".go" or rel.name in SCOPE["module_files"]):
            continue
        path = root / name
        if not path.exists() and not path.is_symlink():
            continue  # A tracked deletion is represented by its absence in this snapshot.
        if not inside(path.resolve(), root) or any((root.joinpath(*rel.parts[:i])).is_symlink() for i in range(1, len(rel.parts) + 1)):
            raise DiscoveryError("source_symlink", f"Tracked source traverses a symlink: {name}")
        info = path.stat()
        if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_FILE_BYTES:
            raise DiscoveryError("source_limit", f"Unsupported source file: {name}")
        data = path.read_bytes()
        total += len(data)
        if total > MAX_SOURCE_BYTES:
            raise DiscoveryError("source_limit", "Tracked Go snapshot exceeds the local size limit")
        content[name] = data
        files.append({"path": name, "sha256": digest(data), "size": len(data)})
    if not any(name.endswith(".go") for name in content):
        raise DiscoveryError("empty_source", "No tracked first-party Go source is available in this scope")
    state = {
        "schema": SCHEMA, "repository": str(root), "revision": revision, "branch": branch,
        "tool_version": VERSION, "scope": SCOPE, "files": files,
        "adapter_sha256": digest(Path(__file__).read_bytes()),
        "source_sha256": digest(encoded(files)),
    }
    return state, content


def cache_paths(state, requested=None):
    root = Path(requested or Path.home() / ".cache" / "agent-workflows" / "graphify").expanduser().resolve()
    protected = [Path(state["repository"]), Path(__file__).resolve().parent.parent]
    # A cache override may not enter another managed snapshot/source release either.
    if any(inside(root, p) for p in protected) or any((p / "skills-manifest.json").is_file() for p in (root, *root.parents)):
        raise DiscoveryError("cache_path", "Graphify cache must be outside repositories and managed skill releases")
    worktree = root / digest(state["repository"].encode())[:20]
    entry = worktree / digest(encoded(state))
    for component in (worktree, entry):
        if component.is_symlink() or not inside(component.resolve(), root) or (component.exists() and not component.is_dir()):
            raise DiscoveryError("cache_path", "Graphify cache components must be owned directories, never symlink redirects")
    return worktree, entry


def resolve_runtime(override=None):
    default = Path.home() / ".venvs" / "agent-graphify" / ("Scripts/graphify.exe" if os.name == "nt" else "bin/graphify")
    candidates = [Path(override).expanduser()] if override else [default, Path(shutil.which("graphify") or "/nonexistent/graphify")]
    for candidate in candidates:
        if candidate.is_file() and os.access(candidate, os.X_OK):
            runtime = candidate.absolute()
            rc, out, _ = graphify_process(runtime, ["--version"], cwd=runtime.parent, timeout=15)
            if rc or not re.fullmatch(r"graphify\s*" + re.escape(VERSION), out.strip()):
                raise DiscoveryError("runtime_version", f"Explicit use requires graphifyy=={VERSION}; install the pinned separate runtime")
            return runtime
    raise DiscoveryError("runtime_missing", f"Install graphifyy=={VERSION} in ~/.venvs/agent-graphify; normal source discovery remains available")


def write_json(path, data):
    path.write_bytes(encoded(data) + b"\n")
    path.chmod(0o600)


def mapped_source(value, snapshot, state):
    if not isinstance(value, str) or not value:
        return value
    path = Path(value)
    if path.is_absolute():
        try:
            name = path.relative_to(snapshot).as_posix()
        except ValueError:
            try:
                name = path.relative_to(state["repository"]).as_posix()
            except ValueError as exc:
                raise DiscoveryError("graph_reference", "Graph contains an out-of-scope source reference") from exc
    else:
        name = PurePosixPath(value).as_posix()
    if name not in {item["path"] for item in state["files"]}:
        raise DiscoveryError("graph_reference", f"Graph references an unindexed source: {name}")
    return str(Path(state["repository"]) / name)


def validate_graph(path, state, snapshot, *, rewrite=False):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_GRAPH_BYTES:
        raise DiscoveryError("graph_incomplete", "Graph output is missing, unsafe or too large")
    try:
        data = json.loads(path.read_bytes())
    except (ValueError, OSError) as exc:
        raise DiscoveryError("graph_incomplete", "Graph output is unreadable") from exc
    if not isinstance(data, dict) or not isinstance(data.get("nodes"), list) or not data["nodes"]:
        raise DiscoveryError("graph_incomplete", "Extraction did not produce graph nodes")
    edges = data.get("links", data.get("edges"))
    if not isinstance(edges, list) or data.get("directed") is True:
        raise DiscoveryError("graph_schema", "Expected Graphify's undirected structural graph")
    represented = set()
    indexed = {str(Path(state["repository"]) / item["path"]): item for item in state["files"]}
    line_counts = {name: max(1, len(Path(name).read_bytes().splitlines())) for name in indexed}
    for item in [*data["nodes"], *edges]:
        if not isinstance(item, dict):
            raise DiscoveryError("graph_schema", "Invalid graph record")
        value = item.get("source_file")
        if value:
            mapped = mapped_source(value, snapshot, state)
            represented.add(mapped)
            if not rewrite and value != mapped:
                raise DiscoveryError("graph_reference", "Cached graph reference is not mapped to the current workspace")
            item["source_file"] = mapped
            location = str(item.get("source_location", ""))
            match = re.fullmatch(r"L?(\d+)(?:-L?(\d+))?", location)
            if match:
                start, end = int(match[1]), int(match[2] or match[1])
                if not 1 <= start <= end <= line_counts[mapped]:
                    raise DiscoveryError("graph_reference", "Graph source location is outside the indexed file")
    required = {str(Path(state["repository"]) / item["path"]) for item in state["files"] if item["path"].endswith(".go")}
    missing = sorted(required - represented)
    if missing:
        raise DiscoveryError("graph_incomplete", f"Extraction omitted {len(missing)} Go source file(s), including {Path(missing[0]).name}")
    if rewrite:
        write_json(path, data)
    return {
        "nodes": len(data["nodes"]), "edges": len(edges), "source_files": len(required),
        "edge_labels": dict(Counter(str(edge.get("confidence", "UNSPECIFIED")) for edge in edges)),
        "directed": False,
    }


def current_entry(state, entry):
    try:
        if entry.is_symlink() or any((entry / name).is_symlink() for name in ("ready.json", "graphify-out", "source")):
            raise ValueError("unsafe cached component")
        ready = json.loads((entry / "ready.json").read_bytes())
        if ready.get("source") != state or ready.get("graph_sha256") != digest((entry / "graphify-out/graph.json").read_bytes()):
            raise ValueError("fingerprint differs")
        summary = validate_graph(entry / "graphify-out/graph.json", state, entry / "source")
    except (OSError, ValueError, DiscoveryError) as exc:
        raise DiscoveryError("stale_graph", "No complete graph matches this worktree/revision/content; run build or use normal discovery") from exc
    return ready, summary


def provenance(state):
    return {key: state[key] for key in ("repository", "revision", "branch", "tool_version", "source_sha256")}


def build(args):
    state, contents = source(args.repo)
    parent, entry = cache_paths(state, args.cache_root)
    runtime = resolve_runtime(args.runtime)
    if entry.exists():
        _, summary = current_entry(state, entry)
        return {"status": "ok", "command": "build", "reused": True, "graph": str(entry / "graphify-out/graph.json"), "provenance": provenance(state), "summary": summary}
    parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    parent.chmod(0o700)
    pending = Path(tempfile.mkdtemp(prefix=".build-", dir=parent))
    started = time.monotonic()
    try:
        snapshot, output = pending / "source", pending / "graphify-out"
        snapshot.mkdir()
        for name, content in contents.items():
            target = snapshot / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            target.chmod(0o600)
        rc, out, err = graphify_process(runtime, ["extract", snapshot, "--code-only", "--no-dedup"], cwd=pending, output=output, timeout=args.timeout)
        (pending / "extraction.stdout").write_text(out)
        (pending / "extraction.stderr").write_text(err)
        if rc or re.search(r"AST extraction failed|extraction is incomplete|walk.errors", out + err, re.IGNORECASE):
            raise DiscoveryError("extraction_failed", "Code-only extraction failed; no graph was activated")
        graph = output / "graph.json"
        summary = validate_graph(graph, state, snapshot, rewrite=True)
        if source(args.repo)[0] != state:
            raise DiscoveryError("source_changed", "Source changed during extraction; build a fresh graph")
        (output / ".graphify_root").write_text(state["repository"], encoding="utf-8")
        ready = {"source": state, "graph_sha256": digest(graph.read_bytes()), "summary": summary, "runtime": str(runtime), "built_at": datetime.now(timezone.utc).isoformat()}
        write_json(pending / "ready.json", ready)
        # A concurrent successful build wins; never overwrite or delete its cache.
        if entry.exists():
            current_entry(state, entry)
        else:
            try:
                pending.rename(entry)
            except OSError as exc:
                if exc.errno not in (errno.EEXIST, errno.ENOTEMPTY):
                    raise
                current_entry(state, entry)
        return {"status": "ok", "command": "build", "reused": False, "graph": str(entry / "graphify-out/graph.json"), "provenance": provenance(state), "summary": summary, "elapsed_seconds": round(time.monotonic() - started, 6)}
    finally:
        if pending.exists():
            shutil.rmtree(pending)


def query(args):
    state, _ = source(args.repo)
    _, entry = cache_paths(state, args.cache_root)
    ready, summary = current_entry(state, entry)
    runtime = resolve_runtime(args.runtime)
    graph = entry / "graphify-out/graph.json"
    started = time.monotonic()
    rc, out, _ = graphify_process(runtime, ["query", args.question, "--budget", args.budget, "--graph", graph], cwd=entry, output=graph.parent, timeout=args.timeout)
    if rc not in (0, 3, 4):
        raise DiscoveryError("query_failed", "Local Graphify query failed; use normal source discovery")
    if source(args.repo)[0] != state or digest(graph.read_bytes()) != ready["graph_sha256"]:
        raise DiscoveryError("source_changed", "Source or graph changed during query; discard this result and rebuild")
    # Upstream may deliberately overrun --budget when all nodes fit. Bound our
    # response honestly; truncation never establishes absence of omitted edges.
    cap = args.budget * 3
    truncated = len(out) > cap
    if truncated:
        cut = out.rfind("\n", 0, cap)
        out = out[:cut if cut > 0 else cap]
    return {
        "status": {0: "ok", 3: "ambiguous", 4: "no_match"}[rc], "command": "query",
        "graph": str(graph), "provenance": provenance(state), "qualification": QUALIFICATION,
        "context": out, "context_chars": len(out), "truncated": truncated,
        "truncation_notice": "Context cut at the wrapper budget; omitted relationships remain unknown" if truncated else None,
        "summary": summary, "elapsed_seconds": round(time.monotonic() - started, 6),
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("build", "query"):
        child = commands.add_parser(command)
        child.add_argument("--repo", type=Path, required=True)
        child.add_argument("--runtime", type=Path, help="Pinned Graphify executable; checked on explicit use")
        child.add_argument("--cache-root", type=Path, help="External cache directory")
        child.add_argument("--timeout", type=float, default=300 if command == "build" else 30)
        if command == "query":
            child.add_argument("question")
            child.add_argument("--budget", type=int, default=2000)
    args = parser.parse_args(argv)
    if not math.isfinite(args.timeout) or not 0 < args.timeout <= 600:
        parser.error("--timeout must be finite, positive and at most 600 seconds")
    if args.command == "query" and (not 100 <= args.budget <= 16000 or not args.question.strip() or len(args.question) > 8000):
        parser.error("query needs a nonempty question up to 8000 characters and --budget between 100 and 16000")
    try:
        result = build(args) if args.command == "build" else query(args)
    except (DiscoveryError, OSError) as exc:
        print(json.dumps({"status": "unavailable", "code": getattr(exc, "code", "filesystem"), "detail": str(exc), "fallback": "Continue with direct source/rg, available jbcontext and gopls"}))
        return 2
    print(json.dumps(result, ensure_ascii=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
