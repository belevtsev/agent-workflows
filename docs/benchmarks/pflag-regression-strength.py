"""Compare the regression tests written by the two archived pflag solvers, offline."""
import argparse
import difflib
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile


DATA = Path(__file__).with_name("daily-review-data")
ORIGINAL_TEST_BLOB = "6d23feb6c5e992976d9b9aacc805ad9f298789eb"
# Each replacement deliberately breaks a distinct part of the original developer's request.
SUFFIX = "if !f.interspersed || (len(shorthands) > 2 && shorthands[1] == '=') {"
STOP = "if !f.interspersed {\n\t\t\t\t\tf.args = append(f.args, args...)\n\t\t\t\t\treturn nil"
MUTATIONS = {
    "discard_trailing_arguments": (STOP, "if !f.interspersed {\n\t\t\t\t\treturn nil"),
    "continue_after_unknown": (STOP, "if false {\n\t\t\t\t\tf.args = append(f.args, args...)\n\t\t\t\t\treturn nil"),
    "parse_known_suffix_after_unknown": (SUFFIX, "if len(shorthands) > 2 && shorthands[1] == '=' {"),
    "forward_known_suffix_in_interspersed_mode": (SUFFIX, "if len(shorthands) > 0 {"),
    "stop_in_interspersed_mode": (STOP, "if true {\n\t\t\t\t\tf.args = append(f.args, args...)\n\t\t\t\t\treturn nil"),
}
ORACLE = '''package pflag
import ("reflect"; "testing")
func TestAuditPassthroughControls(t *testing.T) {
    for _, x := range []struct { name string; interspersed, wantKnown bool; input, want []string }{
        {"long and trailing", false, false, []string{"--unknown","--known","file"}, []string{"--unknown","--known","file"}},
        {"cluster and trailing", false, false, []string{"-xk","file"}, []string{"-xk","file"}},
        {"interspersed cluster", true, true, []string{"-xk","file"}, []string{"-x","file"}},
        {"interspersed long", true, true, []string{"--unknown","--known"}, []string{"--unknown"}},
    } {
        t.Run(x.name,func(t *testing.T){
            f:=NewFlagSet("audit", ContinueOnError)
            known:=f.BoolP("known","k",false,"")
            f.SetInterspersed(x.interspersed)
            f.ParseErrorsAllowlist.UnknownFlagsHandling=PassUnknownFlagToArgs
            err:=f.Parse(x.input)
            if err!=nil || *known!=x.wantKnown || !reflect.DeepEqual(f.Args(),x.want) { t.Errorf("input %q -> %q, known %t, err %v",x.input,f.Args(),*known,err) }
        })
    }
}
'''


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args])


def test(directory, name, env):
    selection = ["-run", "^" + name + "$"] if name else ["-skip", "^TestAuditPassthroughControls$"]
    run = subprocess.run(["go", "test", "-vet=off", "-count=1", "-json", *selection, "."],
                         cwd=directory, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
    events = [json.loads(line) for line in run.stdout.splitlines()]
    finished = [e for e in events if e.get("Test") == name and e["Action"] in ("pass", "fail")]
    observed = [e["Test"] for e in events if e.get("Test") and e["Action"] == "run"]
    failures = [e["Test"] for e in events if e.get("Test") and e["Action"] == "fail"]
    if (len(finished) != 1 or not observed or run.returncode != int(finished[0]["Action"] == "fail")
            or (run.returncode == 1 and not failures)):
        raise RuntimeError(f"{name}: unusable test result\n{run.stderr.decode()}\n{run.stdout[-1600:].decode()}")
    return finished[0]["Action"]


def entries(repo, tree):
    paths = {}
    for row in git(repo, "ls-tree", "-rz", tree).split(b"\0"):
        if row:
            metadata, path = row.split(b"\t", 1)
            paths[path] = metadata
    return paths


def compare(repos):
    evidence = json.loads((DATA / "pflag-pair.json").read_text())
    trees = {r["arm"]: r["candidate_tree"] for r in evidence["attempts"]}
    tests = {r["arm"]: next(iter(r["added_test_results"]["candidate"])) for r in evidence["attempts"]}
    files = {arm: entries(repo, trees[arm]) for arm, repo in repos.items()}
    a, b = files["codex"], files["kitsu"]
    differing = sorted(p.decode() for p in a.keys() | b.keys() if a.get(p) != b.get(p))
    if set(differing) - {"flag.go", "flag_test.go", "kitsu.toml", "kitsu-roles.toml"}:
        raise ValueError(f"unexpected differing paths: {differing}")
    source = {arm: git(repo, "show", trees[arm]+":flag.go").decode() for arm, repo in repos.items()}
    edits = [s[2:] for s in difflib.ndiff(source["codex"].splitlines(), source["kitsu"].splitlines())
             if s.startswith(("- ", "+ "))]
    if not edits or not all(s.lstrip().startswith("// ") for s in edits):
        raise ValueError("candidate source differences include more than prose comments")
    original_tests = git(repos["codex"], "cat-file", "blob", ORIGINAL_TEST_BLOB)
    env = dict(os.environ, GOENV="off", GOFLAGS="", GOWORK="off", GOTOOLCHAIN="local", GOPROXY="off", CGO_ENABLED="0")
    with tempfile.TemporaryDirectory() as tmp:
        checkout = Path(tmp)
        with tarfile.open(fileobj=io.BytesIO(git(repos["codex"], "archive", trees["codex"]))) as archive:
            archive.extractall(checkout, filter="data")
        (checkout/"kitsu_audit_test.go").write_text(ORACLE)
        rows = []
        for case in ["correct", *MUTATIONS]:
            contents = source["codex"]
            if case in MUTATIONS:
                before, after = MUTATIONS[case]
                if contents.count(before) != 1:
                    raise ValueError(f"{case}: expected exactly one mutation site")
                contents = contents.replace(before, after)
            (checkout/"flag.go").write_text(contents)
            row = {"case": case, "source_sha256": hashlib.sha256(contents.encode()).hexdigest()}
            row["oracle"] = test(checkout, "TestAuditPassthroughControls", env)
            if row["oracle"] != ("pass" if case == "correct" else "fail"):
                raise ValueError(f"uncalibrated control: {case}")
            (checkout / "flag_test.go").write_bytes(original_tests)
            row["original_suite"] = test(checkout, None, env)
            if case == "correct" and row["original_suite"] != "pass":
                raise ValueError("original suite failed the correct control")
            for arm, repo in repos.items():
                (checkout/"flag_test.go").write_bytes(git(repo, "show", trees[arm]+":flag_test.go"))
                row[arm] = test(checkout, tests[arm], env)
                if case == "correct" and row[arm] != "pass":
                    raise ValueError(f"{arm}: false alarm on correct control")
            rows.append(row)
    return {"task": evidence["task"], "candidate_trees": trees, "test_names": tests,
            "original_test_blob": ORIGINAL_TEST_BLOB, "different_paths": differing, "production_source_differences": "prose line comments only",
            "go": subprocess.check_output(["go","version"],env=env,text=True).strip(), "model_calls": 0, "controls": rows}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", type=Path, required=True, help="Git repository containing the archived plain candidate tree")
    parser.add_argument("--kitsu", type=Path, required=True, help="Git repository containing the archived Kitsu candidate tree")
    args = parser.parse_args()
    print(json.dumps(compare(vars(args)), indent=2))
