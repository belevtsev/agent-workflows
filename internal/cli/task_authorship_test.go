package cli_test

// cw task authorship (interface contract L4, third version): R20, and R1
// through cw task start. The fixture is a real Git repository holding a small
// Go module in t.TempDir() with a task store beside it; the commands run
// through cli.Execute with explicit streams.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// l4cNew is l3cNew with a record that has test rules (StartWithPacks).
func l4cNew(t *testing.T) *l3cFix {
	t.Helper()
	l3cNeedNoGOFLAGS(t)
	root := t.TempDir()
	f := &l3cFix{t: t, ctx: t.Context(), root: root, repo: filepath.Join(root, "repo dir"), home: filepath.Join(root, "home")}
	f.write("go.mod", "module "+l3cModule+"\n\ngo 1.22\n")
	f.write("p/p.go", "package p\n\n// F is the contract symbol.\nfunc F() int { return 1 }\n")
	if err := os.MkdirAll(f.home, 0o700); err != nil {
		t.Fatal(err)
	}
	f.git("init", "-q")
	f.git("add", ".")
	f.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "fixture")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	check := l3cCheck("ok", "go", "test", "-json", "./p/...")
	plan := taskflow.Plan{Title: "l4 cli fixture", Kind: "substantial", CoordinatorID: l3cCoordinator, Checks: []taskflow.Check{check},
		Checkpoints:  &taskflow.CheckpointPolicy{Required: false, Reason: "fixture"},
		Requirements: []taskflow.Requirement{{ID: "C1", Description: "criterion C1", CheckIDs: []string{"ok"}}}}
	rec, err := taskflow.Env{}.StartWithPacks(f.ctx, f.repo, store, plan, nil, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("StartWithPacks: %v", err)
	}
	f.dir, f.id = rec.Directory, rec.ID
	extract, err := taskflow.ExtractContract(filepath.Join(f.repo, "p"), []string{"F"})
	if err != nil {
		t.Fatalf("ExtractContract: %v", err)
	}
	data, err := taskflow.EncodeContract(extract)
	if err != nil {
		t.Fatal(err)
	}
	cpath := filepath.Join(root, "contract.json")
	if err := os.WriteFile(cpath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f.contract = &taskflow.ContractRef{Path: cpath, SHA256: l3cSum(data)}
	return f
}

// l4cAuthorshipFixture has one file an unaccepted test engineer wrote and
// one no worker result recorded.
func l4cAuthorshipFixture(t *testing.T) *l3cFix {
	f := l4cNew(t)
	f.assign("te1", []string{"p/te_test.go"}, l3cReq("R1", "C1"))
	f.write("p/te_test.go", l3cPTests)
	f.submit("te1", []string{"p/te_test.go"}, l3cUncovered("R1", "fixture"))
	f.write("p/stray_test.go", "package p\n\nimport \"testing\"\n\nfunc TestStray(t *testing.T) {}\n")
	return f
}

// TestTaskAuthorshipCLIPrintsReport fails when cw task authorship does not
// print taskflow.Authorship's report as one JSON line, exits non-zero for
// unowned files, writes to the task, behaves differently with --dry-run, or
// fails on an abandoned task (R20, I7).
func TestTaskAuthorshipCLIPrintsReport(t *testing.T) {
	t.Parallel()
	f := l4cAuthorshipFixture(t)
	before := l3cTree(t, f.dir)
	out, stderr, err := f.execute("task", "authorship", "--task", f.dir)
	if err != nil || cli.ExitCode(err) != 0 {
		t.Fatalf("cw task authorship: %v (exit %d), stderr %q; want exit 0 whatever the statuses (R20)", err, cli.ExitCode(err), stderr)
	}
	line := l3cOneLine(t, "cw task authorship", out)
	var rep taskflow.AuthorshipReport
	if err := taskflow.Decode(strings.NewReader(line), &rep); err != nil {
		t.Fatalf("stdout does not decode strictly as an AuthorshipReport: %v\n%s (R20)", err, line)
	}
	if rep.TaskID != f.id || len(rep.Files) != 2 || rep.Files[0].Path != "p/stray_test.go" || rep.Files[1].Path != "p/te_test.go" ||
		rep.Files[0].Status != "unowned" || rep.Files[1].Status != "unowned" {
		t.Errorf("printed report %s, want task %s with p/stray_test.go and p/te_test.go unowned (R19, R20)", line, f.id)
	}
	direct, err := taskflow.Env{}.Authorship(f.ctx, f.dir)
	if err != nil {
		t.Fatalf("taskflow.Authorship: %v", err)
	}
	want, _ := json.Marshal(direct)
	if !l3cSameJSON(line, string(want)) {
		t.Errorf("printed report differs from taskflow.Authorship's:\n got  %s\n want %s (R20)", line, want)
	}
	dry, _, err := f.execute("task", "authorship", "--task", f.dir, "--dry-run")
	if err != nil {
		t.Errorf("cw task authorship --dry-run: %v, want it accepted (R20)", err)
	} else if dry != out {
		t.Errorf("--dry-run changed the output:\n got  %s\n want %s (R20)", dry, out)
	}
	l3cSameTree(t, "cw task authorship", before, l3cTree(t, f.dir))

	if _, err := (taskflow.Env{}).Abandon(f.dir, "fixture abandoned"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	abandoned := l3cTree(t, f.dir)
	out, stderr, err = f.execute("task", "authorship", "--task", f.dir)
	if err != nil || cli.ExitCode(err) != 0 {
		t.Fatalf("cw task authorship of an abandoned task: %v (exit %d), stderr %q; want exit 0 (R20)", err, cli.ExitCode(err), stderr)
	}
	var after taskflow.AuthorshipReport
	if err := taskflow.Decode(strings.NewReader(l3cOneLine(t, "abandoned", out)), &after); err != nil || len(after.Files) != 2 {
		t.Errorf("abandoned task report %q (%v), want the same two files (R19, R20)", out, err)
	}
	l3cSameTree(t, "cw task authorship of an abandoned task", abandoned, l3cTree(t, f.dir))
}

// TestTaskAuthorshipCLIErrors fails when a missing --task or a positional
// argument is not a usage error (exit 2) with nothing on stdout, or when a
// task that cannot be loaded does not exit 1 with nothing on stdout (R20).
func TestTaskAuthorshipCLIErrors(t *testing.T) {
	t.Parallel()
	f := l4cNew(t)
	out, stderr, err := f.execute("task", "authorship")
	if err == nil || cli.ExitCode(err) != 2 || !strings.Contains(err.Error()+stderr, "authorship needs --task") {
		t.Errorf("cw task authorship without --task: %v (exit %d), stderr %q; want usage error %q, exit 2 (R20)", err, cli.ExitCode(err), stderr, "authorship needs --task")
	}
	if out != "" {
		t.Errorf("usage error printed %q on stdout (R20)", out)
	}
	out, _, err = f.execute("task", "authorship", "--task", f.dir, "extra")
	if err == nil || cli.ExitCode(err) != 2 || out != "" {
		t.Errorf("cw task authorship with a positional argument: %v (exit %d), stdout %q; want exit 2 and no stdout (R20)", err, cli.ExitCode(err), out)
	}
	missing := filepath.Join(f.root, "no-such-task")
	out, _, err = f.execute("task", "authorship", "--task", missing)
	if err == nil || cli.ExitCode(err) != 1 || out != "" {
		t.Errorf("cw task authorship of a missing task: %v (exit %d), stdout %q; want exit 1 and no stdout (R20)", err, cli.ExitCode(err), out)
	}
}

// l4cRewrite rewrites every stored revision of the task through edit. With
// chain set, each previous_sha256 is the SHA-256 of the rewritten previous
// revision file, so only the edit can stop the record from loading; without
// it, the stored previous_sha256 values are kept as they were.
func l4cRewrite(t *testing.T, dir string, chain bool, edit func(rev int, doc map[string]any)) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "record-*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no record files: %v", err)
	}
	sort.Strings(names)
	prev := ""
	for i, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		edit(i+1, doc)
		if chain && i > 0 {
			doc["previous_sha256"] = prev
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, out, 0o600); err != nil {
			t.Fatal(err)
		}
		prev = l3cSum(out)
	}
}

// TestTaskAuthorshipCLIRecordDoesNotLoad fails when cw task authorship of an
// existing task directory whose record does not load (an invalid stored
// value with the chain kept, or a broken chain) does not exit 1 with nothing
// on stdout, or writes to the task directory (R20, R19).
func TestTaskAuthorshipCLIRecordDoesNotLoad(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, dir string){
		"invalid stored test rules": func(t *testing.T, dir string) {
			l4cRewrite(t, dir, true, func(_ int, doc map[string]any) { doc["test_rules"] = "bogus" })
		},
		"broken revision chain": func(t *testing.T, dir string) {
			l4cRewrite(t, dir, false, func(rev int, doc map[string]any) {
				if rev > 1 {
					doc["previous_sha256"] = strings.Repeat("0", 64)
				}
			})
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := l4cAuthorshipFixture(t)
			corrupt(t, f.dir)
			if _, err := taskflow.Load(f.dir); err == nil {
				t.Fatalf("fixture: the record still loads after %s", name)
			}
			before := l3cTree(t, f.dir)
			out, stderr, err := f.execute("task", "authorship", "--task", f.dir)
			if err == nil || cli.ExitCode(err) != 1 {
				t.Errorf("cw task authorship of a record that does not load: %v (exit %d), stderr %q; want exit 1 (R20)", err, cli.ExitCode(err), stderr)
			}
			if out != "" {
				t.Errorf("cw task authorship of a record that does not load printed %q on stdout, want nothing (R20)", out)
			}
			l3cSameTree(t, "cw task authorship of a record that does not load", before, l3cTree(t, f.dir))
		})
	}
}

// TestTaskAuthorshipCLIHelp fails when the command's help does not say that
// it changes nothing, works on an abandoned task and exits 0 whatever the
// statuses (R20, the contract's CLI section).
func TestTaskAuthorshipCLIHelp(t *testing.T) {
	t.Parallel()
	f := l4cNew(t)
	out, stderr, err := f.execute("task", "authorship", "--help")
	if err != nil {
		t.Fatalf("cw task authorship --help: %v, stderr %q", err, stderr)
	}
	text := strings.ToLower(out + stderr)
	for _, want := range []string{"nothing", "abandon"} {
		if !strings.Contains(text, want) {
			t.Errorf("help %q lacks %q: it must say that it changes nothing, works on an abandoned task and exits 0 (R20)", out+stderr, want)
		}
	}
	if !regexp.MustCompile(`exit[a-z ]*0`).MatchString(text) {
		t.Errorf("help %q does not say that it exits 0 whatever the statuses (R20)", out+stderr)
	}
}

// TestTaskStartRecordsTestRules fails when cw task start does not record
// test_rules go.test-evidence/v1 (R1).
func TestTaskStartRecordsTestRules(t *testing.T) {
	t.Parallel()
	e := pkNewEnv(t)
	for _, files := range []map[string]string{{"go.mod": "module m\n"}, {"README.md": "docs\n"}} {
		repo := tpRepo(t, files)
		dir, _, err := e.start(t, repo, tpPlan("go", "version"))
		if err != nil {
			t.Fatalf("cw task start: %v", err)
		}
		rec, err := taskflow.Load(dir)
		if err != nil || rec.TestRules != "go.test-evidence/v1" {
			t.Errorf("cw task start in %v: test_rules %q (%v), want %q (R1)", files, rec.TestRules, err, "go.test-evidence/v1")
		}
	}
}
