package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"gopkg.in/yaml.v3"
)

// pkEnv is a hermetic cw environment under one resolved temporary directory.
type pkEnv struct{ base, home, cfg string }

func pkResolved(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func pkNewEnv(t *testing.T) *pkEnv {
	base := pkResolved(t)
	return &pkEnv{base: base, home: filepath.Join(base, "home"), cfg: filepath.Join(base, "cfg")}
}

func (e *pkEnv) config(stdin string, out, errOut *bytes.Buffer) cli.Config {
	vars := map[string]string{"HOME": e.home, "XDG_CONFIG_HOME": filepath.Join(e.base, "xdg-config"), "XDG_STATE_HOME": filepath.Join(e.base, "xdg-state")}
	return cli.Config{In: strings.NewReader(stdin), Out: out, Err: errOut,
		HomeDir: func() (string, error) { return e.home, nil }, WorkingDir: func() (string, error) { return e.base, nil },
		Getenv: func(k string) string { return vars[k] }}
}

// run executes cw with --config-dir appended and returns stdout and the error.
func (e *pkEnv) run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cli.Execute(context.Background(), append(append([]string{}, args...), "--config-dir", e.cfg), e.config(stdin, &out, &errOut))
	return out.String(), err
}

func (e *pkEnv) writeSelection(t *testing.T, body string) {
	p := filepath.Join(e.cfg, "packs.json")
	if os.MkdirAll(e.cfg, 0o700) != nil || os.WriteFile(p, []byte(body), 0o600) != nil || os.Chmod(p, 0o600) != nil {
		t.Fatal("cannot write packs.json")
	}
}

// selection returns the raw packs.json and its ids; raw is nil when absent.
func (e *pkEnv) selection() ([]byte, []string) {
	raw, _ := os.ReadFile(filepath.Join(e.cfg, "packs.json"))
	var doc struct{ Packs []string }
	_ = json.Unmarshal(raw, &doc)
	return raw, append([]string{}, doc.Packs...)
}

func pkOneLine(t *testing.T, out string, v any) {
	t.Helper()
	if strings.Count(strings.TrimSuffix(out, "\n"), "\n") != 0 || json.Unmarshal([]byte(out), v) != nil {
		t.Fatalf("stdout is not one JSON line: %q", out)
	}
}

// pkWantExit checks the exit code and the message: exact, a prefix when it
// ends with ": ", or unchecked when empty.
func pkWantExit(t *testing.T, err error, code int, msg string) {
	t.Helper()
	prefix := strings.HasSuffix(msg, ": ")
	if err == nil || cli.ExitCode(err) != code || (prefix && !strings.HasPrefix(err.Error(), msg)) || (!prefix && msg != "" && err.Error() != msg) {
		t.Fatalf("got %v (exit %d), want exit %d with %q", err, cli.ExitCode(err), code, msg)
	}
}

// A14, A17: list reports every built-in pack and the effective selection and
// writes nothing; lang list is the same command.
func TestPackList(t *testing.T) {
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil {
		t.Fatal(err)
	}
	e := pkNewEnv(t)
	check := func(cmd, wantSel string, selected bool) {
		t.Helper()
		out, err := e.run(t, "", cmd, "list")
		var got struct {
			Selection string
			Packs     []struct {
				ID, Kind     string
				Detect       []string
				EvidenceRule string `json:"evidence_rule"`
				Selected     *bool
			}
		}
		pkOneLine(t, out, &got)
		if err != nil || got.Selection != wantSel || len(got.Packs) != len(builtIn) {
			t.Fatalf("A14 violated: %s list gives %s, %v; want selection %q", cmd, out, err, wantSel)
		}
		for i, m := range builtIn {
			p := got.Packs[i]
			if p.ID != m.ID || p.Kind != m.Kind || !reflect.DeepEqual(p.Detect, m.Detect) || p.EvidenceRule != m.EvidenceRule ||
				p.Selected == nil || *p.Selected != (selected && m.ID == "go") {
				t.Fatalf("A14 violated: entry %+v, want %s selected=%v", p, m.ID, selected)
			}
		}
	}
	check("pack", "default", true)
	if _, err := os.Lstat(e.cfg); !os.IsNotExist(err) {
		t.Fatalf("A14 violated: list created the configuration directory")
	}
	_, err = e.run(t, "", "pack", "list", "extra")
	pkWantExit(t, err, 2, "")
	if _, err := e.run(t, "", "pack", "remove", "go"); err != nil {
		t.Fatal(err)
	}
	check("lang", "explicit", false)
	e.writeSelection(t, "{")
	_, err = e.run(t, "", "pack", "list")
	pkWantExit(t, err, 1, "cannot read the pack selection: ")
}

// cw pack list prints, for each built-in pack, exactly the fields its
// pack.yaml declares. Oracle: the embedded pack.yaml decoded as plain YAML,
// without schema, equals the list entry as JSON without selected, key for key
// and value for value.
func TestPackListPrintsOnlyWhatPackYamlDeclares(t *testing.T) {
	names, err := fs.Glob(agentworkflows.Packs, "packs/*/pack.yaml")
	if err != nil || len(names) == 0 {
		t.Fatalf("no embedded pack.yaml: %v", err)
	}
	out, err := pkNewEnv(t).run(t, "", "pack", "list")
	var got struct{ Packs []map[string]any }
	pkOneLine(t, out, &got)
	if err != nil || len(got.Packs) != len(names) {
		t.Fatalf("pack list gives %s, %v; want %d packs", out, err, len(names))
	}
	for i, name := range names {
		data, err := fs.ReadFile(agentworkflows.Packs, name)
		var declared map[string]any
		if err != nil || yaml.Unmarshal(data, &declared) != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		delete(declared, "schema")
		// Normalise the YAML values to what encoding/json decodes.
		raw, err := json.Marshal(declared)
		if err != nil || json.Unmarshal(raw, &declared) != nil {
			t.Fatalf("cannot normalise %s: %v", name, err)
		}
		printed := got.Packs[i]
		delete(printed, "selected")
		if !reflect.DeepEqual(printed, declared) {
			t.Errorf("pack list entry %v differs from what %s declares: %v", printed, name, declared)
		}
	}
}

// A15, A16, A17: add and remove edit the effective selection, save only on
// change, keep ids already in the file, honour --dry-run, and refuse bad
// arguments, ids and I/O without touching packs.json.
func TestPackAddRemove(t *testing.T) {
	type step struct {
		args    string
		want    []string // printed selection; nil when an error is expected
		written bool
		file    []string // ids in packs.json afterwards; nil: absent
		exit    int
		msg     string
	}
	unwritable := func(t *testing.T, e *pkEnv) {
		if os.MkdirAll(e.cfg, 0o700) != nil || os.Chmod(e.cfg, 0o500) != nil {
			t.Fatal("setup failed")
		}
		t.Cleanup(func() { _ = os.Chmod(e.cfg, 0o700) })
	}
	cases := []struct {
		setup string // initial packs.json; "-" for an existing 0500 directory without it
		steps []step
	}{
		{"", []step{{"pack add go", []string{"go"}, true, []string{"go"}, 0, ""}, {"lang add go", []string{"go"}, false, []string{"go"}, 0, ""}}},
		{"", []step{{"lang remove go", []string{}, true, []string{}, 0, ""}, {"pack remove go", []string{}, false, []string{}, 0, ""}}},
		{"", []step{{"pack remove zz", []string{"go"}, true, []string{"go"}, 0, ""}}},
		{`{"version":1,"packs":["zz"]}`, []step{{"pack add go", []string{"go", "zz"}, true, []string{"go", "zz"}, 0, ""}}},
		{"", []step{{"pack add go --dry-run", []string{"go"}, false, nil, 0, ""}, {"lang remove go --dry-run", []string{}, false, nil, 0, ""}}},
		{"", []step{
			{"pack add", nil, false, nil, 2, ""}, {"lang add go go", nil, false, nil, 2, ""},
			{"pack remove", nil, false, nil, 2, ""}, {"pack remove a b", nil, false, nil, 2, ""},
			{"pack add zz", nil, false, nil, 2, `unknown pack "zz"; built-in packs: go`},
			{"pack remove Bad_Id", nil, false, nil, 2, `invalid pack id "Bad_Id"`},
		}},
		{`{"version":1,"packs":["go","zz"]}`, []step{{"lang remove zz", []string{"go"}, true, []string{"go"}, 0, ""}}},
		{"{", []step{
			{"pack add go", nil, false, []string{}, 1, "cannot read the pack selection: "},
			{"pack remove go", nil, false, []string{}, 1, "cannot read the pack selection: "},
		}},
		{"-", []step{
			{"pack add go", nil, false, nil, 1, "cannot save the pack selection: "},
			{"pack remove go", nil, false, nil, 1, "cannot save the pack selection: "},
		}},
	}
	for _, c := range cases {
		e := pkNewEnv(t)
		if c.setup == "-" {
			unwritable(t, e)
		} else if c.setup != "" {
			e.writeSelection(t, c.setup)
		}
		for _, s := range c.steps {
			before, _ := e.selection()
			out, err := e.run(t, "", strings.Fields(s.args)...)
			if s.want == nil {
				pkWantExit(t, err, s.exit, s.msg)
			} else {
				var got struct {
					Selected []string
					Written  *bool
				}
				pkOneLine(t, out, &got)
				if err != nil || got.Selected == nil || !reflect.DeepEqual(got.Selected, s.want) || got.Written == nil || *got.Written != s.written {
					t.Fatalf("A15/A16 violated: %s printed %s, %v; want selected %q written %v", s.args, out, err, s.want, s.written)
				}
			}
			raw, ids := e.selection()
			if (raw == nil) != (s.file == nil) || (raw != nil && s.want != nil && !reflect.DeepEqual(ids, s.file)) || (!s.written && !bytes.Equal(before, raw)) {
				t.Fatalf("A15/A16 violated: after %s packs.json is %q, want ids %q", s.args, raw, s.file)
			}
		}
		if _, err := os.Lstat(e.cfg); c.steps[0].exit == 2 && !os.IsNotExist(err) {
			t.Fatalf("A15 violated: refused arguments created the configuration directory")
		}
	}
}

// A17: bare pack prints help; help paths touch neither Getenv nor HomeDir.
func TestPackHelp(t *testing.T) {
	e := pkNewEnv(t)
	var out, errOut bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"pack"}, e.config("", &out, &errOut)); err != nil ||
		!strings.Contains(out.String()+errOut.String(), "remove") {
		t.Fatalf("A17 violated: cw pack gives %v with output %q", err, out.String()+errOut.String())
	}
	for _, args := range [][]string{{"help", "pack"}, {"pack", "--help"}} {
		cfg := e.config("", &out, &errOut)
		cfg.Getenv = func(k string) string { t.Errorf("A17 violated: %v called Getenv(%q)", args, k); return "" }
		cfg.HomeDir = func() (string, error) { t.Errorf("A17 violated: %v called HomeDir", args); return e.home, nil }
		if err := cli.Execute(context.Background(), args, cfg); err != nil {
			t.Fatalf("A17 violated: %v: %v", args, err)
		}
	}
}

// A14, A15 (section 3.6 directories): without --config-dir the selection lives
// in $XDG_CONFIG_HOME/agent-workflows, else <home>/.config/agent-workflows.
func TestPackDefaultConfigDir(t *testing.T) {
	for _, useXDG := range []bool{true, false} {
		e := pkNewEnv(t)
		xdg, dir := filepath.Join(e.base, "xdg"), filepath.Join(e.home, ".config", "agent-workflows")
		if useXDG {
			dir = filepath.Join(xdg, "agent-workflows")
		}
		run := func(stdin string, args ...string) string {
			var out, errOut bytes.Buffer
			cfg := e.config(stdin, &out, &errOut)
			cfg.Getenv = func(k string) string {
				if k == "XDG_CONFIG_HOME" && useXDG {
					return xdg
				} else if k == "HOME" {
					return e.home
				}
				return ""
			}
			if err := cli.Execute(context.Background(), args, cfg); err != nil {
				t.Fatalf("%v (XDG %v): %v", args, useXDG, err)
			}
			return out.String()
		}
		run("", "pack", "add", "go")
		e.cfg = dir
		if _, ids := e.selection(); !reflect.DeepEqual(ids, []string{"go"}) {
			t.Fatalf("A15 violated: cw pack add go (XDG %v) did not write %s/packs.json", useXDG, dir)
		}
		var got struct{ Selection string }
		if pkOneLine(t, run("", "pack", "list"), &got); got.Selection != "explicit" {
			t.Fatalf("A14 violated: cw pack list (XDG %v) does not read %s: selection %q", useXDG, dir, got.Selection)
		}
		// A18: task start reads the same default directory.
		run("", "pack", "remove", "go")
		out := run(tpPlan("go", "version"), "task", "start", "--full", "--plan", "-", "--repo", tpRepo(t, map[string]string{"go.mod": "module m\n"}),
			"--store", filepath.Join(e.base, "store"))
		var rec struct{ Packs json.RawMessage }
		pkOneLine(t, out, &rec)
		var packs, want any
		_ = json.Unmarshal(rec.Packs, &packs)
		_ = json.Unmarshal([]byte(`{"selection":"explicit","applied":[]}`), &want)
		if !reflect.DeepEqual(packs, want) {
			t.Fatalf("A18 violated: task start (XDG %v) recorded packs %s, want explicit with no applied pack", useXDG, rec.Packs)
		}
	}
}
