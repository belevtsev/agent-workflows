package installers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every invocation creates a new process, including those that resume the task.
func (r *installRun) command(t *testing.T, dir string, wantExit int, program string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir, cmd.Env = dir, r.environment()
	return commandOutput(t, cmd, wantExit)
}

func commandOutput(t *testing.T, cmd *exec.Cmd, wantExit int) []byte {
	t.Helper()
	cmd.WaitDelay = 3 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	program, args := filepath.Base(cmd.Path), cmd.Args[1:]
	gotExit := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("%s %v: %v\n%s%s", program, args, err, stdout, stderr.Bytes())
		}
		gotExit = exit.ExitCode()
	}
	if gotExit != wantExit {
		t.Fatalf("%s %v: exit %d, want %d\n%s%s", program, args, gotExit, wantExit, stdout, stderr.Bytes())
	}
	return stdout
}

func decodeReceipt(t *testing.T, data []byte, receipt any) {
	t.Helper()
	if err := json.Unmarshal(data, receipt); err != nil {
		t.Fatalf("invalid receipt: %v\n%s", err, data)
	}
}

type installedTaskState struct {
	Task, Request, Next, Ticket string
	PriorNotes                  map[string]string `json:"prior_notes"`
	Verified                    bool
	Scope                       struct {
		Entry, Until   string
		Repair, Commit bool
	}
	Candidate struct{ Head, Index string }
	Last      *struct{ Action, Note string } `json:"last_result"`
	Proof     *struct {
		Outcome    string
		ExitStatus *int `json:"exit_status"`
	} `json:"behavioral_assertion"`
}

type patchFixture struct {
	name, response, next, outcome string
	taskExit, assertionExit       int
}

// Installers run without Git. The task runs with the machine's real Git and
// a disposable config and repository. The driver supplies intake and the assertion.
func checkInstalledHarness(t *testing.T, run *installRun) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	run.env["PATH"] = strings.Join([]string{filepath.Dir(run.target), filepath.Dir(git), run.env["PATH"]}, string(os.PathListSeparator))
	run.env["GIT_CONFIG_NOSYSTEM"] = "1"
	run.env["GIT_CONFIG_GLOBAL"] = filepath.Join(run.home, ".gitconfig")
	run.env["GIT_TERMINAL_PROMPT"] = "0"
	run.env["GIT_AUTHOR_NAME"], run.env["GIT_COMMITTER_NAME"] = "Installer fixture", "Installer fixture"
	run.env["GIT_AUTHOR_EMAIL"], run.env["GIT_COMMITTER_EMAIL"] = "installer@example.invalid", "installer@example.invalid"
	run.env["GIT_AUTHOR_DATE"], run.env["GIT_COMMITTER_DATE"] = "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"
	t.Run("extend-an-approved-plan", func(t *testing.T) { checkInstalledPlan(t, run, git) })
	for _, candidate := range []patchFixture{
		{"passing-patch", "ready\n", "done", "passed", 0, 0},
		{"failing-patch", "waiting\n", "verify", "failed", 5, 1},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			checkExistingPatch(t, filepath.Join(run.dir, candidate.name), run.target, git, run.command, candidate)
		})
	}
}

// Confirm that an installed binary can also extend a completed plan and carry
// its accepted behavior across separate invocations into implementation.
func checkInstalledPlan(t *testing.T, run *installRun, git string) {
	dir := t.TempDir()
	run.command(t, dir, 0, git, "init", "-q")
	const id = "planned-response"
	const plan = "Add response.txt containing the line ready"
	step := func(verb string, args ...string) installedTaskState {
		t.Helper()
		var receipt installedTaskState
		decodeReceipt(t, run.command(t, dir, 0, run.target, append([]string{"task", verb, id, "--json"}, args...)...), &receipt)
		return receipt
	}
	state := step("open", "--until", "plan", "--request", "Plan the response")
	state = step("record", "intake", "--ticket", state.Ticket, "--actor", "offline", "--note", "Check the response")
	state = step("record", "plan", "--ticket", state.Ticket, "--actor", "offline", "--note", plan)
	if state.Next != "done" || step("extend", "--until", "implement").Next != "implement" {
		t.Fatal("installed CLI did not extend the finished plan")
	}
	state = step("next")
	if err := os.WriteFile(filepath.Join(dir, "response.txt"), []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	step("record", "implement", "--ticket", state.Ticket, "--actor", "offline", "--note", "Added response")
	state = step("next")
	if state.Next != "verify" || state.PriorNotes["plan"] != plan {
		t.Fatal("fresh process did not recover the accepted plan")
	}
	step("test", "--ticket", state.Ticket, "--expect", "the response is ready", "--", "git", "grep", "--no-index", "^ready$", "--", "response.txt")
	if final := step("next"); final.Next != "done" || !final.Verified {
		t.Fatal("installed CLI lost completed implementation evidence")
	}
}

func checkExistingPatch(t *testing.T, dir, kitsu, git string, command func(*testing.T, string, int, string, ...string) []byte, candidate patchFixture) []byte {
	t.Helper()
	writeFile(t, filepath.Join(dir, "response.txt"), []byte("queued\n"))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("original notes\n"))
	gitRun := func(args ...string) string {
		t.Helper()
		return strings.TrimRight(string(command(t, dir, 0, git, args...)), "\r\n")
	}
	gitRun("init", "-q", "-b", "main", "--template=")
	gitRun("add", "response.txt", "notes.txt")
	gitRun("commit", "-qm", "Start the response fixture")
	writeFile(t, filepath.Join(dir, "response.txt"), []byte(candidate.response))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("staged developer notes\n"))
	gitRun("add", "response.txt", "notes.txt")
	files := map[string]string{"response.txt": candidate.response, "notes.txt": "working developer notes\n", "draft.txt": "untracked developer draft\n"}
	for path, content := range files {
		writeFile(t, filepath.Join(dir, path), []byte(content))
	}
	head, index := gitRun("rev-parse", "HEAD"), gitRun("write-tree")
	status := gitRun("status", "--porcelain=v1", "--untracked-files=all")
	if head == "" || index == "" || index == gitRun("rev-parse", "HEAD^{tree}") ||
		gitRun("show", ":response.txt") != strings.TrimSuffix(candidate.response, "\n") {
		t.Fatal("fixture requires an existing staged patch")
	}

	const id = "install-smoke"
	const request = "Verify the existing response patch. Preserve my staged notes, working edits and draft."
	const intake = "The existing response file must contain the line ready."
	task := func(action string, exit int, args ...string) []byte {
		t.Helper()
		out := command(t, dir, exit, kitsu, append([]string{"task", action, id, "--json"}, args...)...)
		t.Logf("kitsu task %s %s exit=%d", action, id, exit)
		return out
	}
	var opened installedTaskState
	decodeReceipt(t, task("open", 0, "--entry", "verify", "--until", "verify", "--request", request), &opened)
	if opened.Next != "intake" || opened.Ticket == "" || opened.Candidate.Head != head || opened.Candidate.Index != index {
		t.Fatalf("opening the existing patch did not pin its candidate: %+v", opened)
	}
	var recorded installedTaskState
	decodeReceipt(t, task("record", 0, "intake", "--ticket", opened.Ticket, "--actor", "offline", "--note", intake), &recorded)
	var resumed installedTaskState
	decodeReceipt(t, task("next", 0), &resumed)
	if resumed.Task != id || resumed.Request != request || resumed.Next != "verify" || resumed.Ticket == "" ||
		resumed.Ticket != recorded.Ticket || resumed.Candidate != opened.Candidate || resumed.Verified ||
		resumed.Last == nil || resumed.Last.Action != "intake" || resumed.Last.Note != intake ||
		resumed.Scope.Entry != "verify" || resumed.Scope.Until != "verify" || resumed.Scope.Repair || resumed.Scope.Commit {
		t.Fatalf("fresh process did not recover the verification request and candidate: %+v", resumed)
	}
	var details struct {
		Task, Workflow string
		Harness        installedTaskState
		Events         []struct{ Action, Note string }
	}
	decodeReceipt(t, task("show", 0), &details)
	if details.Task != id || details.Workflow != "harness" || details.Harness.Request != request || details.Harness.Next != "verify" ||
		len(details.Events) != 1 || details.Events[0].Action != "intake" || details.Events[0].Note != intake {
		t.Fatalf("inspect did not recover the original request and recorded intake: %+v", details)
	}
	task("test", candidate.taskExit, "--ticket", resumed.Ticket, "--expect", intake,
		"--", "git", "grep", "--no-index", "-e", "^ready$", "--", "response.txt")
	var final installedTaskState
	finalReceipt := task("next", 0)
	decodeReceipt(t, finalReceipt, &final)
	if final.Next != candidate.next || final.Verified != (candidate.taskExit == 0) || final.Proof == nil || final.Proof.Outcome != candidate.outcome ||
		final.Proof.ExitStatus == nil || *final.Proof.ExitStatus != candidate.assertionExit || final.Candidate != opened.Candidate {
		t.Fatalf("fresh process did not recover the expected verification outcome: %+v", final)
	}
	if gitRun("symbolic-ref", "HEAD") != "refs/heads/main" || gitRun("rev-parse", "HEAD") != head || gitRun("write-tree") != index ||
		gitRun("status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatal("task changed HEAD, the index or working tree status")
	}
	for path, want := range files {
		if got := string(readFile(t, filepath.Join(dir, path))); got != want {
			t.Fatalf("task changed %s: got %q, want %q", path, got, want)
		}
	}
	t.Logf("scripted journey resumed=verify assertion_exit=%d final=%s HEAD/index/worktree preserved, model requests=0", candidate.assertionExit, final.Next)
	return finalReceipt
}
