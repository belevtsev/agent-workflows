"""Shared network-isolated local environment for the qs replays."""
import subprocess


CHECK = """set -eu
node /deps/tape/bin/tape 'test/**/*.js' >/tmp/suite
tail -3 /tmp/suite
status=0
node /grader/check.js /work || status=1
node - <<'JS' || status=1
const qs = require(process.cwd() + '/lib');
const assert = require('node:assert/strict');
const query = 'a[19]=x&a=y';
let passed = false;
try {
    assert.deepEqual(qs.parse(query, {arrayLimit: 20, throwOnLimitExceeded: true}),
                     qs.parse(query, {arrayLimit: 200, throwOnLimitExceeded: true}));
    passed = true;
} finally {
    console.log(JSON.stringify({id: 'supplementary_scalar_append', passed}));
}
JS
exit "$status"
"""


def sandbox(root, work, modules, node, binary, grader, node_wrapper=None):
    command = ["bwrap", "--unshare-all", "--die-with-parent", "--new-session", "--clearenv",
               "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin",
               "--ro-bind", "/lib", "/lib", "--ro-bind-try", "/lib64", "/lib64",
               "--proc", "/proc", "--dev", "/dev", "--bind", str(root / "tmp"), "/tmp",
               "--bind", str(root / "home"), "/home/test", "--bind", str(work), "/work",
               "--ro-bind", str(modules.resolve()), "/deps",
               "--ro-bind", str(modules.resolve()), "/work/node_modules",
               "--ro-bind", str(node.resolve()), "/opt/node",
               "--ro-bind", str(binary.resolve()), "/opt/bin/kitsu",
               "--ro-bind", str(grader.resolve()), "/grader/check.js",
               "--ro-bind", str(root / "check.sh"), "/grader/check.sh", "--chdir", "/work"]
    if node_wrapper is not None:
        command.extend(["--ro-bind", str(node_wrapper), "/opt/bin/node"])
    for key, value in {"HOME": "/home/test", "PATH": "/opt/bin:/opt/node/bin:/usr/bin:/bin",
                       "NODE_PATH": "/deps", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
                       "GIT_AUTHOR_NAME": "Replay", "GIT_COMMITTER_NAME": "Replay",
                       "GIT_AUTHOR_EMAIL": "replay@example.invalid", "GIT_COMMITTER_EMAIL": "replay@example.invalid"}.items():
        command.extend(["--setenv", key, value])

    def run(*argv, status=0):
        result = subprocess.run([*command, "--", *argv], capture_output=True, timeout=90, text=True)
        if result.returncode not in (status if isinstance(status, tuple) else (status,)):
            raise RuntimeError((argv, result.returncode, result.stdout[-1000:], result.stderr[-1000:]))
        return result
    return run
