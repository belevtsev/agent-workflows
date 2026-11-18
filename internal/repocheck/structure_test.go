package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/belevtsev/kitsu/v3/"

// imports lists, for each package of the module, the packages of the module
// it imports. With tests it also lists what its tests import.
func imports(t *testing.T, withTests bool) map[string][]string {
	t.Helper()
	format := `{{.ImportPath}}:{{join .Imports " "}}`
	if withTests {
		format += ` {{join .TestImports " "}} {{join .XTestImports " "}}`
	}
	list := exec.CommandContext(t.Context(), "go", "list", "-f", format, "./...")
	list.Dir = repoRoot
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list ./...: %v", err)
	}
	packages := map[string][]string{}
	for line := range strings.Lines(string(out)) {
		name, imported, _ := strings.Cut(strings.TrimSpace(line), ":")
		name = strings.TrimPrefix(name, module)
		packages[name] = nil
		for _, path := range strings.Fields(imported) {
			if own, found := strings.CutPrefix(path, module); found {
				packages[name] = append(packages[name], own)
			}
		}
	}
	if len(packages) < 5 {
		t.Fatalf("go list found only %d packages", len(packages))
	}
	return packages
}

// The commands are the outermost layer: nothing below may reach up to them.
func TestOnlyTheCommandImportsTheCLI(t *testing.T) {
	t.Parallel()
	const cli = "internal/cli"
	for name, imported := range imports(t, true) {
		if name != "cmd/kitsu" && name != cli && slices.Contains(imported, cli) {
			t.Errorf("%s imports %s; only cmd/kitsu may", name, cli)
		}
	}
}

var startsProgram = regexp.MustCompile(`\bexec\.Command(Context)?\(`)

// Programs start in internal/proc. Git access, declared checks, explicitly
// requested advisers, interactive handoff, native plugin setup and protected release
// publication are the execution boundaries.
func TestProgramsAreStartedInOnePlace(t *testing.T) {
	t.Parallel()
	const proc = "internal/proc"
	allowed := []string{"internal/gitx", "internal/runner", "internal/advisers", "internal/onboarding", "internal/pluginsetup", "internal/releasecmd"}
	found := false
	for name, imported := range imports(t, false) {
		if !slices.Contains(imported, proc) {
			continue
		}
		found = true
		if !slices.Contains(allowed, name) {
			t.Errorf("%s imports %s; only %s may", name, proc, strings.Join(allowed, " and "))
		}
	}
	if !found {
		t.Fatalf("no package imports %s: the rule checks nothing", proc)
	}
	for _, file := range repoFiles(t) {
		// internal/kitsutest builds and drives kitsu for the tests.
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") ||
			strings.HasPrefix(file, proc+"/") || strings.HasPrefix(file, "internal/kitsutest/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			t.Fatal(err)
		}
		if startsProgram.Match(data) {
			t.Errorf("%s starts a program itself; only %s may", file, proc)
		}
	}
}
