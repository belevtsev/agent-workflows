"""Offline paired test of the documented staged-file delivery procedure. Requires Python 3.9+, Git and a daily Kitsu CLI."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import time

BASE = 'def row(value):\n    return value + "\\n"\n'
GOOD = '''def row(value):
    return '"' + value.replace('"', '""') + '"\\n'
'''
FAULTY = GOOD.replace('value.replace(\'"\', \'""\')', 'value')
CASES = ['plain', 'two,fields', 'embedded"quote', 'line\nbreak', '']
# This grader lives outside the checkout. Read the actual public serialization
# through the standard CSV reader. -B keeps imported bytecode outside the candidate.
GRADER = """import csv, io, json, runpy, sys
row = runpy.run_path("export.py")["row"]
cases = json.loads(sys.argv[1])
def roundtrip(s):
    try:
        return list(csv.reader(io.StringIO(row(s)), strict=True)) == [[s]]
    except csv.Error:
        return False

passed = [roundtrip(s) for s in cases]
print(json.dumps(passed))
sys.exit(0 if all(passed) else 1)
"""


def trial(root, binary, arm, kind):
    original, delivery = root / 'developer', root / 'delivery'
    original.mkdir()
    home = root / 'home'
    home.mkdir()
    env = dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
               GIT_AUTHOR_NAME='Replay', GIT_AUTHOR_EMAIL='test@example.invalid',
               GIT_COMMITTER_NAME='Replay', GIT_COMMITTER_EMAIL='test@example.invalid',
               HOME=str(home), USERPROFILE=str(home), XDG_CONFIG_HOME=str(home / '.config'))

    def run(cwd, *argv, code=0):
        p = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=45)
        if p.returncode != code:
            raise RuntimeError((argv, p.returncode, p.stdout, p.stderr))
        return p.stdout

    def git(repo, *args):
        return run(repo, 'git', *args)

    def task(verb, *args, code=0):
        return json.loads(run(delivery, binary, 'task', verb, 'csv-export', '--brief', *args, code=code))

    git(original, 'init', '-q')
    git(original, 'config', 'apply.whitespace', 'fix')
    (original / 'export.py').write_text(BASE)
    (original / 'notes').write_text('baseline\n')
    git(original, 'add', '.')
    git(original, 'commit', '-qm', 'Baseline')
    base = git(original, 'rev-parse', 'HEAD').decode().strip()
    (original / 'export.py').write_text((GOOD if kind == 'good' else FAULTY) + '# retain staged bytes   \n')
    (original / 'notes').write_text('staged developer work\n')
    git(original, 'add', 'export.py', 'notes')
    # Construct a plausible false approval (or false rejection) from reading the wrong copy.
    (original / 'export.py').write_text(FAULTY if kind == 'good' else GOOD)
    (original / 'notes').write_text('different unstaged developer work\n')
    (original / 'new-notes').write_text('untracked draft\n')

    def developer_state():
        return [git(original, 'rev-parse', 'HEAD'), git(original, 'write-tree'),
                git(original, 'status', '--porcelain=v1', '-z'),
                *((original / name).read_bytes() for name in ('export.py', 'notes', 'new-notes'))]

    before = developer_state()
    grader = root / 'check.py'
    grader.write_text(GRADER)
    check = [sys.executable, '-B', str(grader), json.dumps(CASES)]
    expected = 0 if kind == 'good' else 1
    opposite = json.loads(run(original, *check, code=1 - expected))
    start = time.monotonic()
    staged = git(original, 'write-tree').decode().strip()
    git(original, 'worktree', 'add', '-qb', 'deliver-csv', str(delivery), base)
    git(delivery, 'restore', '--source', staged, '--staged', '--worktree', '--', 'export.py')
    git(delivery, 'diff', '--cached', '--quiet', staged, '--', 'export.py')
    assert git(delivery, 'diff', '--cached', '--name-only') == b'export.py\n'

    if arm == 'kitsu':
        ready = task('open', '--entry', 'verify', '--until', 'deliver', '--commit', '--request',
                     'Verify the selected staged CSV export and commit only when all fields round-trip.')
        ready = task('record', 'intake', '--actor', 'scripted-coordinator', '--ticket', ready['ticket'],
                     '--note', 'Round-trip the five CSV fields and preserve all source checkout edits.')
        result = task('test', '--ticket', ready['ticket'], '--expect', 'five CSV fields round-trip',
                      '--', *check, code=5 if expected else 0)
        assert result['verified'] == (kind == 'good')
        if expected:
            assert result['next'] == 'verify'
        else:
            assert result['next'] == 'commit'
            end = task('commit', '--ticket', result['ticket'], '--message', 'Add CSV escaping',
                       '--include', 'export.py')
            assert end['next'] == 'done' and end['delivery'] == 'committed'
    else:
        run(delivery, *check, code=expected)
        if not expected:
            git(delivery, 'commit', '-qm', 'Add CSV escaping')
    elapsed = time.monotonic() - start
    observed = json.loads(run(delivery, *check, code=expected))
    assert observed == [True, True, not expected, True, True] and observed != opposite
    assert developer_state() == before
    head = git(delivery, 'rev-parse', 'HEAD').decode().strip()
    assert (head == base) == bool(expected)
    if not expected:
        assert git(delivery, 'diff', '--name-only', base, head) == b'export.py\n'
        assert git(delivery, 'rev-parse', 'HEAD^').decode().strip() == base
        assert git(delivery, 'status', '--porcelain') == b''
    return dict(arm=arm, staged=kind, cases_passed=sum(observed), cases_total=len(CASES),
                original_preserved=True, selected_index_entry_matched=True, committed=head != base, seconds=round(elapsed, 6))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('kitsu', type=Path)
    parser.add_argument('--pairs', type=int, default=3)
    args = parser.parse_args()
    if args.pairs < 1:
        parser.error('--pairs must be positive')
    binary = args.kitsu.resolve(strict=True)
    results = []
    for i in range(args.pairs):
        arms = ['plain', 'kitsu'] if i % 2 == 0 else ['kitsu', 'plain']
        for kind in ('good', 'faulty'):
            for arm in arms:
                with tempfile.TemporaryDirectory() as tmp:
                    results.append(dict(pair=i + 1, **trial(Path(tmp), binary, arm, kind)))
    print(json.dumps(dict(model_calls=0, compact=False, kitsu_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                     median_seconds={kind: {arm: statistics.median(r['seconds'] for r in results if r['arm'] == arm and r['staged'] == kind)
                                            for arm in ('plain', 'kitsu')} for kind in ('good', 'faulty')}, runs=results), indent=2))
