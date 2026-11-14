package evals

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Authored test for the adversarial-tester cases' scaffold.sh
// (adversarial-evals revision 3, G11; the scaffold's job comes from revision
// 1's format and revision 2's E2): run in an empty workspace, it copies the
// case's testdata/, writes testdata/work/go.mod for the case's module with go
// 1.22, and copies that module to artefacts/scratch/, without bringing the
// case's fix.patch, prompt.md or graders/ along.

// scaffoldCases are the three cases and the module each interface contract
// names (adversarial-evals revision 1).
var scaffoldCases = []struct{ name, module string }{
	{"frame-length-limit", "example.com/frame"},
	{"loader-once-race", "example.com/memo"},
	{"path-prefix-escape", "example.com/jail"},
}

// scaffoldGoMod reports whether a go.mod declares module mod and go 1.22.
func scaffoldGoMod(data []byte, mod string) bool {
	var module, goLine bool
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "module" && strings.Trim(f[1], `"`) == mod:
			module = true
		case len(f) == 2 && f[0] == "go" && f[1] == "1.22":
			goLine = true
		}
	}
	return module && goLine
}

// G11: fails when a scaffold does not run in an empty directory, leaves out
// the interface contract or the module's go.mod, names another module, makes a
// scratch copy that differs from testdata/work in any file, copies fix.patch,
// prompt.md or graders/ into the workspace, or (without -short) leaves a
// scratch module whose tests do not pass.
func TestAuthoredAdversarialScaffolds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scaffold.sh is a POSIX sh script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	root := authoredRealRoot(t)
	for _, c := range scaffoldCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			caseDir := filepath.Join(root, "evals", "adversarial-tester", c.name)
			ws := t.TempDir()
			cmd := exec.CommandContext(t.Context(), sh, filepath.Join(caseDir, "scaffold.sh"))
			cmd.Dir = ws
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("sh scaffold.sh in an empty directory: %v\n%s", err, out)
			}

			want, err := os.ReadFile(filepath.Join(caseDir, "testdata", "contract", "interface-contract.md"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(ws, "testdata", "contract", "interface-contract.md"))
			if err != nil {
				t.Fatalf("workspace has no testdata/contract/interface-contract.md: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Error("workspace testdata/contract/interface-contract.md differs from the case's")
			}

			gomod, err := os.ReadFile(filepath.Join(ws, "testdata", "work", "go.mod"))
			if err != nil {
				t.Fatalf("workspace has no testdata/work/go.mod: %v", err)
			}
			if !scaffoldGoMod(gomod, c.module) {
				t.Errorf("testdata/work/go.mod does not declare module %s and go 1.22:\n%s", c.module, gomod)
			}

			caseWork := advAuthoredSnapshot(t, filepath.Join(caseDir, "testdata", "work"))
			work := advAuthoredSnapshot(t, filepath.Join(ws, "testdata", "work"))
			for p, body := range caseWork {
				if work[p] != body {
					t.Errorf("workspace testdata/work/%s is missing or differs from the case's", p)
				}
			}
			scratchDir := filepath.Join(ws, "artefacts", "scratch")
			if fi, err := os.Stat(scratchDir); err != nil || !fi.IsDir() {
				t.Fatalf("workspace has no artefacts/scratch/ directory: %v", err)
			}
			scratch := advAuthoredSnapshot(t, scratchDir)
			if !maps.Equal(scratch, work) {
				t.Errorf("artefacts/scratch differs from testdata/work: scratch has %q, work has %q",
					slices.Sorted(maps.Keys(scratch)), slices.Sorted(maps.Keys(work)))
				for p, body := range work {
					if s, ok := scratch[p]; ok && s != body {
						t.Errorf("artefacts/scratch/%s differs from testdata/work/%s", p, p)
					}
				}
			}

			err = filepath.WalkDir(ws, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				switch name := d.Name(); {
				case name == "fix.patch" || name == "prompt.md":
					t.Errorf("the case's %s reached the workspace at %s", name, p)
				case name == "graders" && d.IsDir():
					t.Errorf("the case's graders/ reached the workspace at %s", p)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			if testing.Short() {
				return
			}
			test := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./...")
			test.Dir = scratchDir
			test.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
			if out, err := test.CombinedOutput(); err != nil {
				t.Errorf("go test ./... in artefacts/scratch: %v\n%s", err, out)
			}
		})
	}
}
