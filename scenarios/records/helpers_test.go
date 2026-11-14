// Package records_test holds the scenario tests of the specification
// check-evidence, from "Same checked content" to "Damaged and concurrent
// records".
package records_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// checkedState is the demo repository after `cw run` has ended with both
// checks passed.
func checkedState(t *testing.T) *cwtest.Repo {
	t.Helper()
	repo := cwtest.Demo(t)
	mustRun(t, repo, 0, "run")
	return repo
}

// mustRun runs cw and fails the test when it does not exit with code.
func mustRun(t *testing.T, repo *cwtest.Repo, code int, args ...string) cwtest.Result {
	t.Helper()
	result := repo.CW(args...)
	if result.Code != code {
		t.Fatalf("cw %s: exit status %d, want %d\n%s", strings.Join(args, " "), result.Code, code, result.Output())
	}
	return result
}

// lineNaming is the first line of output in which id stands as a word of its
// own.
func lineNaming(output, id string) (string, bool) {
	word := regexp.MustCompile(`(^|[^\w./-])` + regexp.QuoteMeta(id) + `($|[^\w./-])`)
	for _, line := range strings.Split(output, "\n") {
		if word.MatchString(line) {
			return line, true
		}
	}
	return "", false
}

// statePattern finds a state in a line; "satisfied" does not match "not
// satisfied".
var statePattern = map[string]*regexp.Regexp{
	"satisfied": regexp.MustCompile(`(^|[^\w])satisfied`),
	"not run":   regexp.MustCompile(`not run`),
	"failed":    regexp.MustCompile(`(^|[^\w])failed`),
	"timed out": regexp.MustCompile(`timed out`),
}

// assertState checks that the line of the output that names id holds state.
func assertState(t *testing.T, result cwtest.Result, id, state string) {
	t.Helper()
	line, ok := lineNaming(result.Stdout, id)
	if !ok {
		t.Errorf("no line of the output names %q\n%s", id, result.Output())
		return
	}
	pattern := statePattern[state]
	if pattern == nil {
		t.Fatalf("unknown state %q", state)
	}
	if !pattern.MatchString(line) || (state == "satisfied" && strings.Contains(line, "not satisfied")) {
		t.Errorf("the line that names %q does not report it as %s: %q\n%s", id, state, line, result.Output())
	}
}

// statusJSON runs `cw status --json` and decodes its object.
func statusJSON(t *testing.T, repo *cwtest.Repo) map[string]any {
	t.Helper()
	result := repo.CW("status", "--json")
	object := map[string]any{}
	if err := json.Unmarshal([]byte(result.Stdout), &object); err != nil {
		t.Fatalf("cw status --json: exit status %d, output is not one JSON object: %v\n%s", result.Code, err, result.Output())
	}
	return object
}

// addEnvCheck declares the check `env` of the scenarios, whose program is
// run, and returns nothing; the declaration is not committed.
func addEnvCheck(repo *cwtest.Repo, run ...string) {
	quoted := make([]string, len(run))
	for i, arg := range run {
		quoted[i] = fmt.Sprintf("%q", arg)
	}
	repo.Write("cw.toml", cwtest.DemoDeclaration+"\n[[check]]\nid = \"env\"\nrun = ["+strings.Join(quoted, ", ")+"]\n")
}

// envFlag is the file whose existence decides the check `env`; it plays the
// role of /tmp/demo-flag. The file does not exist yet.
func envFlag(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "demo-flag")
}

func createFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("flag\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// records reads every run record of a check.
func records(t *testing.T, repo *cwtest.Repo, id string) map[string]map[string]any {
	t.Helper()
	found := map[string]map[string]any{}
	for _, path := range repo.RecordFiles(id) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]any{}
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		found[path] = fields
	}
	return found
}

// recordWithOutcome is the only record of a check that ended with outcome.
func recordWithOutcome(t *testing.T, repo *cwtest.Repo, id, outcome string) string {
	t.Helper()
	var paths []string
	for path, fields := range records(t, repo, id) {
		if fields["outcome"] == outcome {
			paths = append(paths, path)
		}
	}
	if len(paths) != 1 {
		t.Fatalf("want one record of %q with outcome %q, found %d: %v", id, outcome, len(paths), paths)
	}
	return paths[0]
}

// limit is how long a test waits for a command it started itself.
const limit = 2 * time.Minute

// background is a cw command that runs while the test goes on.
type background struct {
	command *exec.Cmd
	output  *bytes.Buffer
	done    chan struct{}
}

// startCW starts cw in a repository and returns at once.
func startCW(t *testing.T, repo *cwtest.Repo, args ...string) *background {
	t.Helper()
	run := &background{
		command: repo.Command(filepath.Join(cwtest.Binary(t), "cw"), args...),
		output:  &bytes.Buffer{},
		done:    make(chan struct{}),
	}
	run.command.Stdout, run.command.Stderr = run.output, run.output
	if err := run.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = run.command.Wait()
		close(run.done)
	}()
	t.Cleanup(func() {
		select {
		case <-run.done:
		default:
			_ = run.command.Process.Kill()
			<-run.done
		}
	})
	return run
}

// wait waits for the command to end and returns its exit status and output.
func (b *background) wait(t *testing.T) (int, string) {
	t.Helper()
	select {
	case <-b.done:
		return b.command.ProcessState.ExitCode(), b.output.String()
	case <-time.After(limit):
		_ = b.command.Process.Kill()
		<-b.done
		t.Fatalf("cw %s did not end within %s:\n%s", strings.Join(b.command.Args[1:], " "), limit, b.output.String())
		return 0, ""
	}
}

// waitFor waits until a file exists.
func waitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear within %s", path, limit)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// truncateToHalf cuts a file to half its length.
func truncateToHalf(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()/2); err != nil {
		t.Fatal(err)
	}
}
