// Package platform_test holds the scenarios of the specification
// platform-support
// (openspec/changes/reduce-cw-to-evidence-gate/specs/platform-support/spec.md).
package platform_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// portableDeclaration is the kitsu.toml of the fixture `portable`.
const portableDeclaration = `version = 1

[[check]]
id = "git"
run = ["git", "--version"]
`

// editedReadme is what the scenarios write into README.md.
const editedReadme = "# portable\n\nedited\n"

// portable creates the fixture `portable`: one commit on main that holds
// README.md and kitsu.toml, after which `kitsu init` has run. extra holds checks
// that a scenario declares in addition to `git`; they are part of the one
// commit, because the installed hooks would deny a later commit.
func portable(t *testing.T, extra string) *kitsutest.Repo {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	declaration := portableDeclaration
	if extra != "" {
		declaration += "\n" + extra
	}
	repo.Write("kitsu.toml", declaration)
	repo.Write("README.md", "# portable\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "first")
	if result := repo.Kitsu("init"); result.Code != 0 {
		t.Fatalf("setting up portable: kitsu init exit status %d\n%s", result.Code, result.Output())
	}
	return repo
}

// head is the commit that HEAD names.
func head(repo *kitsutest.Repo) string { return repo.Git("rev-parse", "HEAD") }

// traced makes git write its trace2 events to a new file and returns its
// name; the events tell how each hook exited.
func traced(t *testing.T, repo *kitsutest.Repo) string {
	t.Helper()
	trace := filepath.Join(t.TempDir(), "trace2.json")
	repo.Setenv("GIT_TRACE2_EVENT", trace)
	return trace
}

// hookExits lists the exit statuses of the runs of a hook that the trace
// records.
func hookExits(t *testing.T, trace, hook string) []int {
	t.Helper()
	file, err := os.Open(trace)
	if err != nil {
		t.Fatalf("git wrote no trace: %v", err)
	}
	defer func() { _ = file.Close() }()
	type key struct {
		sid string
		id  int
	}
	type event struct {
		Event    string `json:"event"`
		SID      string `json:"sid"`
		ChildID  int    `json:"child_id"`
		Class    string `json:"child_class"`
		HookName string `json:"hook_name"`
		Code     int    `json:"code"`
	}
	started := map[key]bool{}
	var codes []int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var e event
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}
		k := key{e.SID, e.ChildID}
		switch {
		case e.Event == "child_start" && e.Class == "hook" && e.HookName == hook:
			started[k] = true
		case e.Event == "child_exit" && started[k]:
			codes = append(codes, e.Code)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return codes
}

// assertDenied checks the meaning of "denied" for a commit: the pre-commit
// hook exited 3, git recorded nothing, and something is on standard error.
func assertDenied(t *testing.T, repo *kitsutest.Repo, before string, result kitsutest.Result, trace string) {
	t.Helper()
	if result.Code == 0 {
		t.Errorf("git commit exited 0, want a denial")
	}
	if after := head(repo); after != before {
		t.Errorf("HEAD moved from %s to %s: git recorded a commit", before, after)
	}
	codes := hookExits(t, trace, "pre-commit")
	if len(codes) == 0 {
		t.Errorf("the pre-commit hook did not run")
	}
	for _, code := range codes {
		if code != 3 {
			t.Errorf("the pre-commit hook exited %d, want 3", code)
		}
	}
	if strings.TrimSpace(result.Stderr) == "" {
		t.Errorf("nothing was printed on standard error")
	}
}

// wordPattern matches name as a word of its own, not as part of a path or a
// longer name.
func wordPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^\w./\\-])` + regexp.QuoteMeta(name) + `($|[^\w./\\-])`)
}

// lineWith says whether one line of output names name as a word and holds
// every phrase.
func lineWith(output, name string, phrases ...string) bool {
	word := wordPattern(name)
	for _, line := range strings.Split(output, "\n") {
		if !word.MatchString(line) {
			continue
		}
		all := true
		for _, phrase := range phrases {
			if !strings.Contains(line, phrase) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// outcome is the outcome of the latest run of a check that `kitsu status
// --json` reports.
func outcome(t *testing.T, repo *kitsutest.Repo, id string) string {
	t.Helper()
	result := repo.Kitsu("status", "--json")
	var status struct {
		Checks []struct {
			ID      string `json:"id"`
			Outcome string `json:"outcome"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("kitsu status --json (exit status %d) printed no JSON object: %v\n%s", result.Code, err, result.Output())
	}
	for _, check := range status.Checks {
		if check.ID == id {
			return check.Outcome
		}
	}
	t.Fatalf("kitsu status --json has no entry for %q\n%s", id, result.Output())
	return ""
}
