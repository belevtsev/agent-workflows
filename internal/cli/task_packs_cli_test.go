package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const tpIncomplete = "task completion evidence is incomplete; inspect problems"

func tpPlan(argv ...string) string {
	a, _ := json.Marshal(argv)
	return `{"title":"t","kind":"substantial","checkpoints":{"required":false,"reason":"test"},` +
		`"requirements":[{"id":"r1","description":"d","check_ids":["unit"],"allow_non_execution":false}],` +
		`"checks":[{"id":"unit","argv":` + string(a) + `,"timeout_seconds":300}]}`
}

// tpRepo writes files (slash paths) under a fresh resolved directory.
func tpRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := pkResolved(t)
	for name, body := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
			t.Fatal("cannot write the project")
		}
	}
	return repo
}

// start runs cw task start with the store <base>/store (created by cw).
func (e *pkEnv) start(t *testing.T, repo, plan string) (directory string, packs json.RawMessage, err error) {
	t.Helper()
	out, err := e.run(t, plan, "task", "start", "--plan", "-", "--repo", repo, "--store", filepath.Join(e.base, "store"))
	if err != nil {
		return "", nil, err
	}
	var rec struct {
		Directory string
		Packs     json.RawMessage
	}
	pkOneLine(t, out, &rec)
	return rec.Directory, rec.Packs, nil
}

// A18, A22: cw task start records the selection and the packs detected at the
// top of --repo; selection errors follow the plan checks and create no task.
func TestTaskStartRecordsPacks(t *testing.T) {
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil || len(builtIn) != 1 {
		t.Fatalf("built-in packs: %v", err)
	}
	gomod, plan := map[string]string{"go.mod": "module m\n"}, tpPlan("go", "version")
	binding := `{"id":"go","detected":"%s","evidence_rule":"` + builtIn[0].EvidenceRule + `"}`
	for _, c := range []struct {
		name, selection string
		files           map[string]string
		plan, want      string // want: the packs JSON, or "!" and the error (a prefix when it ends with ": ")
	}{
		{"A18 default with go.mod", "", gomod, plan, `{"selection":"default","applied":[` + fmt.Sprintf(binding, "go.mod") + `]}`},
		{"A18 default with neither file", "", map[string]string{"x.txt": "x"}, plan, `{"selection":"default","applied":[]}`},
		{"A18 default with go.mod in a subdirectory", "", map[string]string{"sub/go.mod": "module m\n"}, plan, `{"selection":"default","applied":[]}`},
		{"A18 explicit empty", `{"version":1,"packs":[]}`, gomod, plan, `{"selection":"explicit","applied":[]}`},
		{"A18 explicit go with go.work", `{"version":1,"packs":["go"]}`, map[string]string{"go.work": "go 1.21\n"}, plan,
			`{"selection":"explicit","applied":[` + fmt.Sprintf(binding, "go.work") + `]}`},
		{"A18 unreadable selection", "{", gomod, plan, "!cannot read the pack selection: "},
		{"A18 selected pack not built in", `{"version":1,"packs":["go","zz"]}`, gomod, plan, `!selected pack "zz" is not built into this cw; run 'cw pack remove zz'`},
		{"A18 plan checked first", "{", gomod, "{", "!"},
	} {
		e := pkNewEnv(t)
		if c.selection != "" {
			e.writeSelection(t, c.selection)
		}
		before, _ := e.selection()
		_, packs, err := e.start(t, tpRepo(t, c.files), c.plan)
		if after, _ := e.selection(); !bytes.Equal(before, after) {
			t.Errorf("I4 violated (%s): task start changed packs.json from %q to %q", c.name, before, after)
		}
		if msg, isErr := strings.CutPrefix(c.want, "!"); isErr {
			if msg != "" {
				pkWantExit(t, err, 1, msg)
			} else if err == nil || strings.HasPrefix(err.Error(), "cannot read the pack selection") {
				t.Errorf("%s violated: an invalid plan must fail before the selection is read, got %v", c.name, err)
			}
			entries, _ := os.ReadDir(filepath.Join(e.base, "store"))
			for _, d := range entries {
				if d.IsDir() {
					t.Errorf("%s violated: a refused start created the task directory %s", c.name, d.Name())
				}
			}
			continue
		}
		var got, want any
		_ = json.Unmarshal(packs, &got)
		_ = json.Unmarshal([]byte(c.want), &want)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s/A22 violated: packs %s, %v; want %s", c.name, packs, err, c.want)
		}
	}
}

// tpVerifyChanged starts a task with start in a Go module with package a,
// changes a/a.go, runs the check once per GOFLAGS value (in order, at the same
// source) and returns the verification.
func tpVerifyChanged(t *testing.T, start func(repo string) (dir string, err error), goflags []string) taskflow.Verification {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "off")
	repo := tpRepo(t, map[string]string{"go.mod": "module example.com/m\n\ngo 1.21\n", "a/a.go": "package a\n\nfunc F() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n"})
	dir, err := start(repo)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a", "a.go"), []byte("package a\n\nfunc F() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if goflags == nil {
		goflags = []string{""}
	}
	for _, g := range goflags {
		t.Setenv("GOFLAGS", g) // checks inherit this process's environment
		if run, err := taskflow.RunCheck(context.Background(), dir, "unit"); err != nil || run.ExitCode == nil || *run.ExitCode != 0 {
			t.Fatalf("precondition: the check did not pass with GOFLAGS=%q: %v", g, err)
		}
	}
	v, err := taskflow.Verify(context.Background(), dir)
	if (err != nil && err.Error() != tpIncomplete) || v.TaskID == "" {
		t.Fatalf("verify: %v", err)
	}
	return v
}

func tpHasPrefix(problems []string, prefix string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func tpNotBuiltIn(id string) string {
	return "evidence rule " + id + " is not built into this cw; only the fallback rule applied for it"
}

// A20, A20.a-d, I1, I6 (only as contract section 0 allows): verify applies
// the rules of the applied packs only, from the latest passed run of each
// check with its GOFLAGS. A record without a pack set is refused at load
// (internal/taskflow/legacy_record_test.go).
func TestVerifyWithPacks(t *testing.T) {
	raceErr := exec.Command("go", "list", "-race", "runtime").Run()
	pack := func(id, rule string) langpack.Manifest {
		return langpack.Manifest{Schema: 1, ID: id, Kind: "language", Detect: []string{"go.mod"}, EvidenceRule: rule}
	}
	ver, test := []string{"go", "version"}, []string{"go", "test", "./..."}
	for _, c := range []struct {
		name    string
		via     string // "cli", "cli-removed" (after cw pack remove go) or "direct" (StartWithPacks explicit)
		packs   []langpack.Manifest
		argv    []string
		goflags []string // GOFLAGS of each run of the check, in order
		wantGo  bool     // a problem starting "changed package a has no passed go test"
		limits  []string // every "not built into this cw" limitation, in order
	}{
		{"A20.a go version only", "cli", nil, ver, nil, true, nil},
		{"A20.b passed race run", "cli", nil, []string{"go", "test", "-race", "./..."}, nil, false, nil},
		{"A20.c after pack remove go", "cli-removed", nil, ver, nil, false, nil},
		{"A20.d unknown rule", "direct", []langpack.Manifest{pack("go", "go.package-test/v9")}, ver, nil, false, []string{tpNotBuiltIn("go.package-test/v9")}},
		{"A20 GOFLAGS that skip tests are forwarded", "direct", []langpack.Manifest{pack("go", langpack.GoPackageTestV1)}, test, []string{"-run=^$"}, true, nil},
		{"A20 only the latest run counts", "direct", []langpack.Manifest{pack("go", langpack.GoPackageTestV1)}, test, []string{"", "-run=^$"}, true, nil},
		{"A20 empty and unknown rules", "direct", []langpack.Manifest{pack("p1", ""), pack("p2", "example.rule/v1")}, ver, nil, false,
			[]string{tpNotBuiltIn("example.rule/v1")}},
		{"A20 two unknown rules in order", "direct", []langpack.Manifest{pack("p1", "example.rule/v1"), pack("p2", "go.package-test/v9")}, ver, nil, false,
			[]string{tpNotBuiltIn("example.rule/v1"), tpNotBuiltIn("go.package-test/v9")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.argv[len(c.argv)-2] == "-race" && raceErr != nil {
				t.Skipf("go list -race runtime fails here: %v", raceErr)
			}
			e := pkNewEnv(t)
			if c.via == "cli-removed" {
				if _, err := e.run(t, "", "pack", "remove", "go"); err != nil {
					t.Fatal(err)
				}
			}
			var plan taskflow.Plan
			if err := json.Unmarshal([]byte(tpPlan(c.argv...)), &plan); err != nil {
				t.Fatal(err)
			}
			store := filepath.Join(e.base, "store")
			start := func(repo string) (string, error) {
				var rec taskflow.Record
				var err error
				switch c.via {
				case "cli", "cli-removed":
					rec.Directory, _, err = e.start(t, repo, tpPlan(c.argv...))
				default:
					rec, err = taskflow.StartWithPacks(context.Background(), repo, store, plan, nil, taskflow.SelectionExplicit, c.packs)
				}
				return rec.Directory, err
			}
			v := tpVerifyChanged(t, start, c.goflags)
			id := strings.Fields(c.name)[0]
			if c.wantGo != tpHasPrefix(v.Problems, "changed package a has no passed go test") || (!c.wantGo && tpHasPrefix(v.Problems, "changed package ")) ||
				tpHasPrefix(v.Problems, "changed substantial task has no passed check run") {
				t.Fatalf("%s violated: problems %q", id, v.Problems)
			}
			var limits []string
			for _, l := range v.Limitations {
				if strings.Contains(l, " is not built into this cw; ") {
					limits = append(limits, l)
				}
			}
			if fmt.Sprintf("%q", limits) != fmt.Sprintf("%q", c.limits) {
				t.Fatalf("%s violated: not-built-in limitations %q, want %q", id, limits, c.limits)
			}
		})
	}
}
