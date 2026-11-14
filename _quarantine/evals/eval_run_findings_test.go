package evals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goTestPattern is how a grader recognises a go test command: go test, or go
// with its -C flag before the subcommand in each spelling cmd/go accepts
// (-C dir, -C=dir, --C dir, --C=dir).
const goTestPattern = `\bgo\s+(?:-{1,2}C(?:=|\s+)\S+\s+)?test\b`

// TestGoTestGradersAcceptChdir fails when a grader that looks for a go test
// command stops using goTestPattern, or when the pattern misses a spelling of
// go -C <dir> test or matches another go subcommand. A run may call only go
// commands, so an agent uses -C where a shell would cd; the graders missed
// that in the run of 2026-11-14 (docs/evidence.md).
func TestGoTestGradersAcceptChdir(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("*", "*", "graders", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, file := range files {
		switch filepath.Base(file) {
		case "probe-run.md", "red-first-run.md", "record-structure.md":
		default:
			continue
		}
		users++
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), goTestPattern) {
			t.Errorf("%s does not use the shared go test pattern %s", file, goTestPattern)
		}
	}
	if users != 8 {
		t.Errorf("found %d probe-run, red-first-run and record-structure graders, want 8", users)
	}

	re, err := compileJS(goTestPattern, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"go test ./...", true},
		{"go test -race -count=1 -run '^TestA1$' ./...", true},
		{"go -C artefacts/scratch test ./...", true},
		{"go -C=artefacts/scratch test -run TestA1 .", true},
		{"go --C artefacts/scratch test ./...", true},
		{"go --C=/abs/artefacts/scratch test ./...", true},
		{"go vet ./...", false},
		{"go -C artefacts/scratch vet ./...", false},
		{"go -C artefacts/scratch tool cover -func=cover.out", false},
		{"go build ./...", false},
		{"cat go.mod testing.md", false},
	} {
		// The runner matches the JSON-encoded tool input.
		input, err := json.Marshal(map[string]string{"command": tc.command})
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(string(input)); got != tc.want {
			t.Errorf("pattern matches %q = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestLoaderOnceRaceFixtureHasOneDefect fails when the loader-once-race
// fixture has a defect besides the seeded one on the path a waiting Get takes
// when its load fails. The probe fails on the seeded source and must pass
// after fix.patch, where both Gets return "" with the error (R2, R3). In the
// run of 2026-11-14 the fixture returned the loader's value there, the agent
// kept a test of it, and the adversarial gate failed (docs/evidence.md).
func TestLoaderOnceRaceFixtureHasOneDefect(t *testing.T) {
	t.Parallel()
	root := authoredRealRoot(t)
	cases, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name != "loader-once-race" {
			continue
		}
		r, err := runAdversarial(t.Context(), c, advAuthoredFixture(t, root, c.Name, "failed-load_test.go"), t.TempDir())
		if err != nil {
			t.Fatalf("runAdversarial: %v", err)
		}
		if err := r.err(); err != nil {
			t.Fatalf("%v\n%s", err, r.describe())
		}
		return
	}
	t.Fatal("no loader-once-race case")
}
