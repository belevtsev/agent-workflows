// Package reporting_test holds the scenarios of the specification
// gate-reporting
// (openspec/changes/reduce-cw-to-evidence-gate/specs/gate-reporting/spec.md),
// the scenarios of the requirement "Observe mode" of publish-gate, and the
// scenario "Everything works offline" of check-evidence.
package reporting_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// gated is the shared fixture of gate-reporting and publish-gate: demo with
// the hooks installed and the gate enforcing.
func gated(t *testing.T) *cwtest.Repo {
	t.Helper()
	repo := cwtest.Demo(t)
	if result := repo.CW("init"); result.Code != 0 {
		t.Fatalf("cw init exited %d:\n%s", result.Code, result.Output())
	}
	return repo
}

// checked brings a repository to the checked state: cw run has just ended
// with both checks passed.
func checked(t *testing.T, repo *cwtest.Repo) {
	t.Helper()
	if result := repo.CW("run"); result.Code != 0 {
		t.Fatalf("cw run exited %d, want 0 for the checked state:\n%s", result.Code, result.Output())
	}
}

// statusCheck is one entry of checks in the output of cw status --json.
type statusCheck struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	RunID      string `json:"run_id"`
	Tree       string `json:"tree"`
	Outcome    string `json:"outcome"`
	ExitStatus *int   `json:"exit_status"`
	EndedAt    string `json:"ended_at"`
	Log        string `json:"log"`
}

// status is the object cw status --json prints.
type status struct {
	Tree          string        `json:"tree"`
	Verified      *bool         `json:"verified"`
	IndexTree     string        `json:"index_tree"`
	IndexVerified *bool         `json:"index_verified"`
	Mode          string        `json:"mode"`
	Hooks         string        `json:"hooks"`
	Checks        []statusCheck `json:"checks"`
}

// statusJSON runs cw status --json and decodes its object.
func statusJSON(t *testing.T, repo *cwtest.Repo) (status, cwtest.Result) {
	t.Helper()
	result := repo.CW("status", "--json")
	var out status
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw status --json (exit %d) did not print one JSON object: %v\n%s", result.Code, err, result.Output())
	}
	return out, result
}

// check is the entry of a check in a status object.
func (s status) check(t *testing.T, id string) statusCheck {
	t.Helper()
	for _, c := range s.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("cw status --json reports no check %q: %+v", id, s.Checks)
	return statusCheck{}
}

// decision is one entry of decisions in the output of cw stats --json.
type decision struct {
	Hook    string                    `json:"hook"`
	Mode    string                    `json:"mode"`
	Outcome string                    `json:"outcome"`
	Count   int                       `json:"count"`
	Reasons map[string]int            `json:"reasons"`
	Checks  map[string]map[string]int `json:"checks"`
}

// stats is the object cw stats --json prints.
type stats struct {
	Since            string                    `json:"since"`
	Until            string                    `json:"until"`
	Runs             map[string]map[string]int `json:"runs"`
	PassAfterFailure map[string]int            `json:"pass_after_failure"`
	Decisions        []decision                `json:"decisions"`
	Unreadable       int                       `json:"unreadable"`
}

// statsJSON runs cw stats with the given arguments and --json and decodes
// its object.
func statsJSON(t *testing.T, repo *cwtest.Repo, args ...string) stats {
	t.Helper()
	result := repo.CW(append(append([]string{"stats"}, args...), "--json")...)
	if result.Code != 0 {
		t.Errorf("cw stats --json exited %d, want 0:\n%s", result.Code, result.Output())
	}
	var out stats
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw stats --json (exit %d) did not print one JSON object: %v\n%s", result.Code, err, result.Output())
	}
	return out
}

// decisions returns the entries of one hook, mode and outcome.
func (s stats) decisions(hook, mode, outcome string) []decision {
	var out []decision
	for _, d := range s.Decisions {
		if d.Hook == hook && d.Mode == mode && d.Outcome == outcome {
			out = append(out, d)
		}
	}
	return out
}

// count is the number of decisions of one hook, mode and outcome.
func (s stats) count(hook, mode, outcome string) int {
	n := 0
	for _, d := range s.decisions(hook, mode, outcome) {
		n += d.Count
	}
	return n
}

// lineWith returns the first line of text that holds every word, and whether
// there is one.
func lineWith(text string, words ...string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			return line, true
		}
	}
	return "", false
}

// holdsNumber says whether text holds n as a number of its own, not as part
// of a longer number.
func holdsNumber(text string, n int) bool {
	return regexp.MustCompile(`(^|[^0-9])` + strconv.Itoa(n) + `($|[^0-9])`).MatchString(text)
}

// cwProgram is the path of the cw under test.
func cwProgram(t *testing.T) string {
	t.Helper()
	name := "cw"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(cwtest.Binary(t), name)
}

// head is the commit HEAD names.
func head(repo *cwtest.Repo) string { return repo.Git("rev-parse", "HEAD") }

// hookStatuses makes the hooks that git runs in the repository reach cw
// through a wrapper that writes down the exit status of each `cw hook`, and
// returns a function that reads what was written, one "<hook> <status>" per
// call. The wrapper is a shell script, so it is used where the hooks run
// with a POSIX shell that finds a program without an extension.
func hookStatuses(t *testing.T, repo *cwtest.Repo) func() []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper that observes the exit status of a hook is a POSIX shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "statuses")
	real := cwProgram(t)
	script := fmt.Sprintf(`#!/bin/sh
%q "$@"
status=$?
if [ "$1" = hook ]; then echo "$2 $status" >> %q; fi
exit $status
`, real, log)
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProgram(t, filepath.Join(bin, "cw"), script)
	path := ""
	for _, kv := range repo.Env() {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = value
		}
	}
	repo.Setenv("PATH", bin+string(os.PathListSeparator)+path)
	return func() []string {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}
