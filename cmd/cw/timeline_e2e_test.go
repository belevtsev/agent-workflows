package main_test

// Independent end-to-end tests for `cw task timeline` (TL-1 to TL-9), authored
// from interface contract revision 2 and the approved e2e recipe. The real
// binary builds a real task directory through public commands; the expected
// output is computed independently from the timestamps `cw task show` prints,
// following D1 to D3 and TL-4.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

type tle2xEnv struct {
	cw, root, repo, state, home, work, gitDir string
}

func tle2xSetup(t *testing.T) tle2xEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end test builds and runs cw")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not found: %v", err)
	}
	cw := buildPlainCW(t)
	root := filepath.Join(t.TempDir(), "timeline e2e root")
	env := tle2xEnv{
		cw: cw, root: root,
		repo: filepath.Join(root, "repo"), state: filepath.Join(root, "state"),
		home: filepath.Join(root, "home"), work: filepath.Join(root, "work"),
		gitDir: filepath.Dir(git),
	}
	for _, dir := range []string{env.repo, env.state, env.home, env.work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(env.repo, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitEnv := []string{"PATH=" + env.gitDir + ":/usr/bin:/bin", "HOME=" + env.home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"add", "a.txt"},
		{"-c", "user.name=timeline", "-c", "user.email=timeline@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init"},
	} {
		command := exec.CommandContext(t.Context(), git, args...)
		command.Dir = env.repo
		command.Env = gitEnv
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	return env
}

type tle2xResult struct {
	stdout, stderr string
	code           int
}

// run starts the built cw with the recipe's minimal environment plus extra,
// bounded by a deadline so a hang fails instead of blocking.
func (env tle2xEnv) run(t *testing.T, extra []string, args ...string) tle2xResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, env.cw, append([]string{"--state-dir", env.state}, args...)...)
	command.Dir = env.work
	command.Env = append([]string{"PATH=" + env.gitDir + ":/usr/bin:/bin", "HOME=" + env.home}, extra...)
	command.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("cw %v timed out", args)
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("cw %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return tle2xResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func (env tle2xEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	result := env.run(t, nil, args...)
	if result.code != 0 {
		t.Fatalf("cw %v: exit %d\nstdout: %s\nstderr: %s", args, result.code, result.stdout, result.stderr)
	}
	return result.stdout
}

func (env tle2xEnv) writeInput(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(env.work, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const tle2xPlan = `{"title":"t","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30},{"id":"bad","argv":["false"],"timeout_seconds":30}]}`

// startTask runs `cw task start` and returns the task directory it reports.
func (env tle2xEnv) startTask(t *testing.T, planName string) string {
	t.Helper()
	return env.startTaskWithPlan(t, planName, tle2xPlan)
}

func (env tle2xEnv) startTaskWithPlan(t *testing.T, planName, planJSON string) string {
	t.Helper()
	plan := env.writeInput(t, planName, planJSON)
	var started struct {
		Task string `json:"task"`
	}
	output := env.mustRun(t, "task", "start", "--repo", env.repo, "--plan", plan, "--summary")
	if err := json.Unmarshal([]byte(output), &started); err != nil || started.Task == "" {
		t.Fatalf("task start summary %q: %v", output, err)
	}
	return started.Task
}

func (env tle2xEnv) assign(t *testing.T, dir, id, agent, role, owned string) {
	t.Helper()
	input := env.writeInput(t, "brief-"+id+".json", fmt.Sprintf(
		`{"id":%q,"agent_id":%q,"role":%q,"goal":"g","owned_paths":[%q],"constraints":[],"check_ids":[],"stop_condition":"s"}`,
		id, agent, role, owned))
	env.mustRun(t, "task", "assign", "--task", dir, "--input", input, "--summary")
}

func (env tle2xEnv) handoff(t *testing.T, dir, id, status string) {
	t.Helper()
	input := env.writeInput(t, "result-"+id+".json", fmt.Sprintf(
		`{"assignment_id":%q,"status":%q,"summary":"done","evidence":[{"path":"a.txt","start_line":1,"end_line":1}],"changed_files":[],"check_ids":[],"findings":[],"uncertainties":[]}`,
		id, status))
	env.mustRun(t, "task", "handoff", "--task", dir, "--input", input, "--summary")
}

func (env tle2xEnv) accept(t *testing.T, dir, id, status string) {
	t.Helper()
	input := env.writeInput(t, "accept-"+id+"-"+status+".json", fmt.Sprintf(
		`{"assignment_id":%q,"status":%q,"reason":"ok","evidence":[{"path":"a.txt","start_line":1,"end_line":1}],"check_ids":[],"findings":[]}`,
		id, status))
	env.mustRun(t, "task", "accept", "--task", dir, "--input", input, "--summary")
}

// tle2xRecord holds the parts of `cw task show` output the oracle needs.
type tle2xRecord struct {
	ID          string    `json:"id"`
	Directory   string    `json:"directory"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Assignments []struct {
		Brief struct {
			ID      string `json:"id"`
			AgentID string `json:"agent_id"`
			Role    string `json:"role"`
		} `json:"brief"`
		AssignedAt time.Time `json:"assigned_at"`
	} `json:"assignments"`
	WorkerResults []struct {
		Result struct {
			AssignmentID string `json:"assignment_id"`
		} `json:"result"`
		SubmittedAt time.Time `json:"submitted_at"`
	} `json:"worker_results"`
	WorkerAcceptances []struct {
		Acceptance struct {
			AssignmentID string `json:"assignment_id"`
		} `json:"acceptance"`
		AcceptedAt time.Time `json:"accepted_at"`
	} `json:"worker_acceptances"`
	Checks []struct {
		ID         string     `json:"id"`
		CheckID    string     `json:"check_id"`
		Status     string     `json:"status"`
		StartedAt  time.Time  `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
	} `json:"checks"`
}

func (env tle2xEnv) show(t *testing.T, dir string) (string, tle2xRecord) {
	t.Helper()
	output := env.mustRun(t, "task", "show", "--task", dir)
	var record tle2xRecord
	if err := json.Unmarshal([]byte(output), &record); err != nil {
		t.Fatalf("task show output: %v", err)
	}
	return output, record
}

// D1, D2 and D3, computed by the test from the stored timestamps.
func tle2xTime(at time.Time) string {
	if at.IsZero() {
		return "n/a"
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func tle2xDuration(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return "n/a"
	}
	return to.Sub(from).Round(time.Millisecond).String()
}

// tle2xObject writes a compact JSON object with keys in the given order.
// Strings are written as the contract's encoder (encoding/json/v2 with
// json.Deterministic(true), default options) writes them: every character
// raw, including `<`, `>`, `&`, U+2028 and non-ASCII text, except `"` and `\`,
// which are backslash-escaped (pinned byte for byte in the unit test
// TestTimelineTL6EncoderEscapingBytes). Control characters and invalid UTF-8,
// whose escaping this oracle does not model, make the test fail.
func tle2xObject(t *testing.T, pairs ...string) string {
	t.Helper()
	var parts []string
	for i := 0; i < len(pairs); i += 2 {
		parts = append(parts, tle2xString(t, pairs[i])+":"+pairs[i+1])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func tle2xString(t *testing.T, value string) string {
	t.Helper()
	if !utf8.ValidString(value) {
		t.Fatalf("oracle value %q is not valid UTF-8", value)
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			t.Fatalf("oracle value %q holds a control character, whose escaping the oracle does not model", value)
		case r == '"' || r == '\\':
			quoted.WriteByte('\\')
			quoted.WriteRune(r)
		default:
			quoted.WriteRune(r)
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}

type tle2xEntry struct {
	start      time.Time
	kind, id   string
	full, line string
}

// tle2xExpect computes the full JSON line (TL-1 to TL-4, TL-7) and the
// --summary lines (TL-6) for a record, with the states the test drove.
func tle2xExpect(t *testing.T, record tle2xRecord, states map[string]string) (string, string) {
	t.Helper()
	q := func(value string) string { return tle2xString(t, value) }
	var entries []tle2xEntry
	for _, assignment := range record.Assignments {
		id := assignment.Brief.ID
		var submitted, accepted time.Time
		for _, result := range record.WorkerResults {
			if result.Result.AssignmentID == id {
				submitted = result.SubmittedAt
			}
		}
		for _, acceptance := range record.WorkerAcceptances {
			if acceptance.Acceptance.AssignmentID == id {
				accepted = acceptance.AcceptedAt
			}
		}
		state, ok := states[id]
		if !ok {
			t.Fatalf("no expected state for assignment %q", id)
		}
		assigned := assignment.AssignedAt
		entries = append(entries, tle2xEntry{
			start: assigned, kind: "assignment", id: id,
			full: tle2xObject(t,
				"kind", q("assignment"), "id", q(id), "stage", q(assignment.Brief.Role), "agent", q(assignment.Brief.AgentID),
				"state", q(state), "assigned_at", q(tle2xTime(assigned)), "submitted_at", q(tle2xTime(submitted)),
				"accepted_at", q(tle2xTime(accepted)), "assign_to_handoff", q(tle2xDuration(assigned, submitted)),
				"handoff_to_accept", q(tle2xDuration(submitted, accepted))),
			line: tle2xObject(t,
				"kind", q("assignment"), "id", q(id), "stage", q(assignment.Brief.Role), "agent", q(assignment.Brief.AgentID),
				"state", q(state), "start", q(tle2xTime(assigned)),
				"assign_to_handoff", q(tle2xDuration(assigned, submitted)), "handoff_to_accept", q(tle2xDuration(submitted, accepted))),
		})
	}
	for _, run := range record.Checks {
		var finished time.Time
		if run.FinishedAt != nil {
			finished = *run.FinishedAt
		}
		state, ok := states[run.CheckID]
		if !ok {
			t.Fatalf("no expected state for check %q", run.CheckID)
		}
		entries = append(entries, tle2xEntry{
			start: run.StartedAt, kind: "check", id: run.ID,
			full: tle2xObject(t,
				"kind", q("check"), "id", q(run.ID), "stage", q(run.CheckID), "state", q(state),
				"started_at", q(tle2xTime(run.StartedAt)), "finished_at", q(tle2xTime(finished)),
				"duration", q(tle2xDuration(run.StartedAt, finished))),
			line: tle2xObject(t,
				"kind", q("check"), "id", q(run.ID), "stage", q(run.CheckID), "state", q(state),
				"start", q(tle2xTime(run.StartedAt)), "duration", q(tle2xDuration(run.StartedAt, finished))),
		})
	}
	slices.SortFunc(entries, func(a, b tle2xEntry) int {
		aMissing, bMissing := a.start.IsZero(), b.start.IsZero()
		switch {
		case !aMissing && bMissing:
			return -1
		case aMissing && !bMissing:
			return 1
		case !aMissing:
			if c := a.start.Compare(b.start); c != 0 {
				return c
			}
		}
		if a.kind != b.kind {
			if a.kind == "assignment" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.id, b.id)
	})
	var full, lines []string
	for _, entry := range entries {
		full = append(full, entry.full)
		lines = append(lines, entry.line)
	}
	fullLine := tle2xObject(t,
		"task", q(record.Directory), "task_id", q(record.ID),
		"started_at", q(tle2xTime(record.CreatedAt)), "updated_at", q(tle2xTime(record.UpdatedAt)),
		"elapsed", q(tle2xDuration(record.CreatedAt, record.UpdatedAt)),
		"entries", "["+strings.Join(full, ",")+"]") + "\n"
	summary := tle2xObject(t,
		"task_id", q(record.ID), "started_at", q(tle2xTime(record.CreatedAt)),
		"updated_at", q(tle2xTime(record.UpdatedAt)), "elapsed", q(tle2xDuration(record.CreatedAt, record.UpdatedAt))) + "\n"
	for _, line := range lines {
		summary += line + "\n"
	}
	return fullLine, summary
}

// tle2xSnapshot lists every entry under root with its mode, its bytes for a
// regular file and its target for a symlink.
func tle2xSnapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		line := fmt.Sprintf("%s %v", rel, info.Mode())
		switch {
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += fmt.Sprintf(" %x", data)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			line += " -> " + target
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func tle2xExpectSuccess(t *testing.T, label string, result tle2xResult, want string) {
	t.Helper()
	if result.code != 0 {
		t.Errorf("%s: exit %d, want 0; stderr: %s", label, result.code, result.stderr)
		return
	}
	if result.stderr != "" {
		t.Errorf("%s: stderr = %q, want nothing (TL-7)", label, result.stderr)
	}
	if result.stdout != want {
		t.Errorf("%s: stdout\n got: %s\nwant: %s", label, result.stdout, want)
	}
}

func tle2xExpectError(t *testing.T, label string, result tle2xResult, code int, mention string) {
	t.Helper()
	if result.code != code {
		t.Errorf("%s: exit %d, want %d; stdout %q stderr %q", label, result.code, code, result.stdout, result.stderr)
	}
	if result.stdout != "" {
		t.Errorf("%s: stdout = %q, want nothing on error (TL-8)", label, result.stdout)
	}
	var message string
	for _, line := range strings.Split(result.stderr, "\n") {
		if strings.HasPrefix(line, "agent-workflows: ") {
			message = line
			break
		}
	}
	if message == "" {
		t.Errorf("%s: stderr %q has no \"agent-workflows: <message>\" line", label, result.stderr)
	} else if mention != "" && !strings.Contains(message, mention) {
		t.Errorf("%s: error %q does not name %s", label, message, mention)
	}
}

// TL-1 to TL-9 on a real task: three assignments (accepted; assigned without
// a result; rejected then unused), a passed and a failed check, then the same
// task abandoned. The output must equal, byte for byte, the line computed from
// `cw task show` timestamps; --summary must equal the JSON Lines form; runs
// are repeatable and independent of TZ and --dry-run; usage and runtime errors
// print nothing on stdout; and nothing on disk changes.
func TestTimelineE2ERealTask(t *testing.T) {
	env := tle2xSetup(t)
	dir := env.startTask(t, "plan.json")
	env.assign(t, dir, "a1", "/root/worker", "evidence", ".")
	env.assign(t, dir, "a2", "/root/other worker", "implementation", "a.txt")
	env.assign(t, dir, "a3", "/root/third", "evidence", ".")
	env.handoff(t, dir, "a1", "completed")
	env.accept(t, dir, "a1", "accepted")
	env.handoff(t, dir, "a3", "completed")
	env.accept(t, dir, "a3", "rejected")
	env.accept(t, dir, "a3", "unused")
	env.mustRun(t, "task", "run", "--task", dir, "--check", "ok", "--summary")
	if result := env.run(t, nil, "task", "run", "--task", dir, "--check", "bad", "--summary"); result.code != 1 {
		t.Fatalf("task run --check bad: exit %d, want 1; stderr %s", result.code, result.stderr)
	}
	states := map[string]string{"a1": "accepted", "a2": "assigned", "a3": "unused", "ok": "passed", "bad": "failed"}

	showBefore, record := env.show(t, dir)
	if len(record.Assignments) != 3 || len(record.Checks) != 2 || len(record.WorkerResults) != 2 || len(record.WorkerAcceptances) != 3 {
		t.Fatalf("setup produced %d assignments, %d checks, %d results, %d acceptances; want 3, 2, 2, 3",
			len(record.Assignments), len(record.Checks), len(record.WorkerResults), len(record.WorkerAcceptances))
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if record.Directory != canonical {
		t.Fatalf("show directory %q differs from EvalSymlinks(%q) = %q", record.Directory, dir, canonical)
	}
	wantFull, wantSummary := tle2xExpect(t, record, states)

	// A symlink to the real task directory, made before the snapshot.
	link := filepath.Join(env.root, "task link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	// TL-1 (test review T6): a non-canonical spelling of the same directory
	// through a symlinked parent, which Load accepts.
	stateLink := filepath.Join(env.root, "state link")
	if err := os.Symlink(env.state, stateLink); err != nil {
		t.Fatal(err)
	}
	// The recipe creates tasks under <state>/workflow-tasks/<task_id>.
	viaLink := filepath.Join(stateLink, "workflow-tasks", record.ID)
	if resolved, err := filepath.EvalSymlinks(viaLink); err != nil || resolved != record.Directory {
		t.Fatalf("EvalSymlinks(%q) = %q, %v; want show directory %q", viaLink, resolved, err, record.Directory)
	}
	before := tle2xSnapshot(t, env.root)
	viaLinkResult := env.run(t, nil, "task", "timeline", "--task", viaLink)
	tle2xExpectSuccess(t, "timeline through a symlinked parent", viaLinkResult, wantFull)
	tle2xExpectSuccess(t, "timeline --summary through a symlinked parent", env.run(t, nil, "task", "timeline", "--task", viaLink, "--summary"), wantSummary)
	if viaLinkResult.code == 0 {
		var printed struct {
			Task string `json:"task"`
		}
		if err := json.Unmarshal([]byte(viaLinkResult.stdout), &printed); err != nil || printed.Task != record.Directory {
			t.Errorf("task through a symlinked parent = %q (%v), want show directory %q, not the argument %q", printed.Task, err, record.Directory, viaLink)
		}
	}

	// TL-7, TL-9: repeated runs print the same bytes, which equal the oracle.
	first := env.run(t, nil, "task", "timeline", "--task", dir)
	tle2xExpectSuccess(t, "timeline", first, wantFull)
	for i := 0; i < 2; i++ {
		again := env.run(t, nil, "task", "timeline", "--task", dir)
		if again.stdout != first.stdout || again.code != first.code {
			t.Errorf("run %d differs from the first run (TL-9):\n%q\n%q", i+2, again.stdout, first.stdout)
		}
	}
	// TL-6, TL-7: --summary prints SummaryLines as JSON Lines and nothing else.
	summary := env.run(t, nil, "task", "timeline", "--task", dir, "--summary")
	tle2xExpectSuccess(t, "timeline --summary", summary, wantSummary)
	if again := env.run(t, nil, "task", "timeline", "--summary", "--task", dir); again.stdout != summary.stdout {
		t.Errorf("repeated --summary differs (TL-9):\n%q\n%q", again.stdout, summary.stdout)
	}
	// TL-8: the global --dry-run changes nothing.
	tle2xExpectSuccess(t, "--dry-run timeline", env.run(t, nil, "--dry-run", "task", "timeline", "--task", dir), wantFull)
	tle2xExpectSuccess(t, "--dry-run timeline --summary", env.run(t, nil, "--dry-run", "task", "timeline", "--task", dir, "--summary"), wantSummary)
	// TL-5: the TZ environment changes nothing.
	for _, zone := range []string{"UTC", "Asia/Kolkata", "America/St_Johns", "Pacific/Chatham"} {
		tle2xExpectSuccess(t, "TZ="+zone, env.run(t, []string{"TZ=" + zone}, "task", "timeline", "--task", dir), wantFull)
		tle2xExpectSuccess(t, "TZ="+zone+" --summary", env.run(t, []string{"TZ=" + zone}, "task", "timeline", "--task", dir, "--summary"), wantSummary)
	}
	// TL-8 through the binary: usage errors exit 2, runtime errors exit 1.
	tle2xExpectError(t, "no --task", env.run(t, nil, "task", "timeline"), 2, "--task")
	tle2xExpectError(t, "empty --task", env.run(t, nil, "task", "timeline", "--task", ""), 2, "--task")
	tle2xExpectError(t, "positional argument", env.run(t, nil, "task", "timeline", "--task", dir, "extra"), 2, "")
	tle2xExpectError(t, "unknown flag", env.run(t, nil, "task", "timeline", "--task", dir, "--no-such-flag"), 2, "")
	tle2xExpectError(t, "symlink", env.run(t, nil, "task", "timeline", "--task", link), 1, "")
	tle2xExpectError(t, "missing directory", env.run(t, nil, "task", "timeline", "--task", filepath.Join(env.root, "no such task")), 1, "")
	tle2xExpectError(t, "regular file", env.run(t, nil, "task", "timeline", "--task", filepath.Join(env.repo, "a.txt")), 1, "")

	// TL-9: nothing under the root changed, and show prints the same record.
	if after := tle2xSnapshot(t, env.root); after != before {
		t.Errorf("timeline changed files under the test root:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if showAfter, _ := env.show(t, dir); showAfter != showBefore {
		t.Errorf("task show differs after timeline runs (TL-9)")
	}

	// TL-8: a directory with any group or other permission bit is refused;
	// an owner-only one such as 0o500 is accepted. The mode is restored after.
	for _, mode := range []fs.FileMode{0o750, 0o701, 0o707} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		modeBefore := tle2xSnapshot(t, dir)
		tle2xExpectError(t, fmt.Sprintf("mode %o", mode), env.run(t, nil, "task", "timeline", "--task", dir), 1, "")
		if modeAfter := tle2xSnapshot(t, dir); modeAfter != modeBefore {
			t.Errorf("mode %o: failing timeline changed the task directory", mode)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	tle2xExpectSuccess(t, "mode 500", env.run(t, nil, "task", "timeline", "--task", dir), wantFull)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// TL-3, TL-7: an abandoned record is accepted and yields the same entries;
	// updated_at and elapsed follow the abandonment revision.
	env.mustRun(t, "task", "abandon", "--task", dir, "--reason", "stop here", "--summary")
	_, abandoned := env.show(t, dir)
	if !abandoned.UpdatedAt.After(record.UpdatedAt) {
		t.Fatalf("abandon did not advance updated_at: %v then %v", record.UpdatedAt, abandoned.UpdatedAt)
	}
	wantAbandoned, wantAbandonedSummary := tle2xExpect(t, abandoned, states)
	abandonedBefore := tle2xSnapshot(t, env.root)
	tle2xExpectSuccess(t, "abandoned timeline", env.run(t, nil, "task", "timeline", "--task", dir), wantAbandoned)
	tle2xExpectSuccess(t, "abandoned timeline --summary", env.run(t, nil, "task", "timeline", "--task", dir, "--summary"), wantAbandonedSummary)
	if after := tle2xSnapshot(t, env.root); after != abandonedBefore {
		t.Errorf("timeline on an abandoned record changed files (TL-9)")
	}

	// TL-8, TL-9: a tampered record is a runtime error that prints nothing
	// and leaves the tampered directory as it was.
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		if _, err := file.WriteString("tampered"); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	tamperedBefore := tle2xSnapshot(t, env.root)
	tle2xExpectError(t, "tampered record", env.run(t, nil, "task", "timeline", "--task", dir), 1, "")
	tle2xExpectError(t, "tampered record --summary", env.run(t, nil, "task", "timeline", "--task", dir, "--summary"), 1, "")
	if after := tle2xSnapshot(t, env.root); after != tamperedBefore {
		t.Errorf("failing timeline changed files (TL-9)")
	}
}

// TL-1, TL-6, TL-7: a fresh task with no assignments and no check runs prints
// "entries":[] and a one-line summary.
func TestTimelineE2EEmptyTask(t *testing.T) {
	env := tle2xSetup(t)
	dir := env.startTask(t, "plan.json")
	_, record := env.show(t, dir)
	if len(record.Assignments) != 0 || len(record.Checks) != 0 {
		t.Fatalf("fresh task has %d assignments and %d checks, want none", len(record.Assignments), len(record.Checks))
	}
	wantFull, wantSummary := tle2xExpect(t, record, nil)
	if !strings.HasSuffix(wantFull, `"entries":[]}`+"\n") {
		t.Fatalf("oracle %q does not end with an empty entries array", wantFull)
	}
	before := tle2xSnapshot(t, env.root)
	tle2xExpectSuccess(t, "empty timeline", env.run(t, nil, "task", "timeline", "--task", dir), wantFull)
	tle2xExpectSuccess(t, "empty timeline --summary", env.run(t, nil, "task", "timeline", "--task", dir, "--summary"), wantSummary)
	if after := tle2xSnapshot(t, env.root); after != before {
		t.Errorf("timeline changed files under the test root (TL-9)")
	}

	// TL-7 (revision 3, test review T1): the binary's help for
	// `task timeline` describes --summary as JSON Lines, not with the shared
	// "one-line summary" text.
	help := env.run(t, nil, "task", "timeline", "--help")
	usage := tle2xSummaryUsage(help.stdout + help.stderr)
	if usage == "" {
		t.Errorf("task timeline --help has no --summary flag:\n%s%s", help.stdout, help.stderr)
	} else {
		if !strings.Contains(strings.ToLower(usage), "json lines") {
			t.Errorf("--summary help %q does not say it prints JSON Lines", usage)
		}
		if strings.Contains(strings.ToLower(usage), "one-line summary") {
			t.Errorf("--summary help %q uses the shared \"one-line summary\" text", usage)
		}
	}
}

// tle2xSummaryUsage returns the --summary flag's help: its flag line plus
// continuation lines up to the next flag line or blank line.
func tle2xSummaryUsage(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") || !strings.Contains(trimmed, "--summary") {
			continue
		}
		usage := []string{trimmed}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" || strings.HasPrefix(next, "-") {
				break
			}
			usage = append(usage, next)
		}
		return strings.Join(usage, " ")
	}
	return ""
}

// TL-7 (test review T2): a verified record (assess then verify, per the
// recipe addendum) loads and prints the computed timeline; the command
// stays read-only (TL-9). Fails if the command refuses or mishandles a record
// that holds a verification.
func TestTimelineE2EVerifiedTask(t *testing.T) {
	env := tle2xSetup(t)
	dir := env.startTask(t, "plan.json")
	env.mustRun(t, "task", "run", "--task", dir, "--check", "ok", "--summary")
	assess := env.writeInput(t, "assess.json", `{"requirements":[{"id":"r1","status":"satisfied","evidence":[{"path":"a.txt","start_line":1,"end_line":2}],"check_ids":["ok"]}],"decision":{"status":"resolved","reason":"checked","evidence":[{"path":"a.txt","start_line":1,"end_line":1}],"check_ids":["ok"]}}`)
	env.mustRun(t, "task", "assess", "--task", dir, "--input", assess, "--summary")
	if verify := env.mustRun(t, "task", "verify", "--task", dir, "--summary"); !strings.Contains(verify, `"complete":true`) {
		t.Fatalf("task verify summary %q is not complete", verify)
	}
	if complete := env.mustRun(t, "task", "show", "--task", dir, "--field", "verification.complete"); !strings.Contains(complete, "true") {
		t.Fatalf("record holds no complete verification: %q", complete)
	}
	showBefore, record := env.show(t, dir)
	if len(record.Checks) != 1 || len(record.Assignments) != 0 {
		t.Fatalf("verified task has %d checks and %d assignments, want 1 and 0", len(record.Checks), len(record.Assignments))
	}
	wantFull, wantSummary := tle2xExpect(t, record, map[string]string{"ok": "passed"})
	before := tle2xSnapshot(t, env.root)
	tle2xExpectSuccess(t, "verified timeline", env.run(t, nil, "task", "timeline", "--task", dir), wantFull)
	tle2xExpectSuccess(t, "verified timeline --summary", env.run(t, nil, "task", "timeline", "--task", dir, "--summary"), wantSummary)
	if after := tle2xSnapshot(t, env.root); after != before {
		t.Errorf("timeline on a verified record changed files (TL-9)")
	}
	if showAfter, _ := env.show(t, dir); showAfter != showBefore {
		t.Errorf("task show differs after timeline runs on a verified record (TL-9)")
	}
}

// TL-8, TL-9 (code review F1): a real task whose record-000001.json, written
// by `cw task start`, is valid, followed by a corrupt next revision
// record-000002.json (mode 0600). A record is valid only if every revision
// file is valid, so the directory holds no valid task record: the command must
// fail with exit status 1, print nothing on standard output, write an
// "agent-workflows: <message>" line to standard error, and leave the task
// directory's entries, modes and bytes unchanged, with and without --summary
// and --dry-run. Fails if the loader drops or skips an undecodable revision
// file when it lists the revisions and so loads revision 1 alone (printing a
// timeline and exiting 0), or rewrites, repairs or removes the corrupt file.
func TestTimelineE2ECorruptRevisionAfterValid(t *testing.T) {
	env := tle2xSetup(t)
	corruptions := []struct{ name, content string }{
		{"invalid JSON", "{not json"},
		{"JSON array, not an object", "[]\n"},
	}
	for i, corruption := range corruptions {
		t.Run(corruption.name, func(t *testing.T) {
			dir := env.startTask(t, fmt.Sprintf("plan-corrupt-%d.json", i))
			first := filepath.Join(dir, "record-000001.json")
			second := filepath.Join(dir, "record-000002.json")
			// Preconditions from the storage format: start wrote revision 1
			// and no revision 2, and the record loads before the corruption.
			if info, err := os.Lstat(first); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("task start left no regular revision 1 at %s: %v", first, err)
			}
			if _, err := os.Lstat(second); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("revision 2 already exists after task start (%v); the case needs start to write revision 1 only", err)
			}
			env.show(t, dir)

			if err := os.WriteFile(second, []byte(corruption.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(second, 0o600); err != nil {
				t.Fatal(err)
			}
			before := tle2xSnapshot(t, env.root)
			dirBefore := tle2xSnapshot(t, dir)
			for _, args := range [][]string{
				{"task", "timeline", "--task", dir},
				{"task", "timeline", "--task", dir, "--summary"},
				{"--dry-run", "task", "timeline", "--task", dir},
				{"--dry-run", "task", "timeline", "--task", dir, "--summary"},
			} {
				label := fmt.Sprintf("corrupt revision 2 (%s): cw %v", corruption.name, args)
				tle2xExpectError(t, label, env.run(t, nil, args...), 1, "")
				if after := tle2xSnapshot(t, dir); after != dirBefore {
					t.Errorf("%s changed the task directory (TL-9):\nbefore:\n%s\nafter:\n%s", label, dirBefore, after)
				}
			}
			if after := tle2xSnapshot(t, env.root); after != before {
				t.Errorf("failing timeline changed files under the test root (TL-9):\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// TL-6, TL-7 (test review U2): an agent ID with `<`, `>`, `&`, non-ASCII
// text, quotes and a backslash, assigned through `cw task assign`, is written
// by the contract's encoder (encoding/json/v2, json.Deterministic(true)) in
// both the full output and the --summary line: raw, except `"` and `\`. Fails
// if either form HTML-escapes `<>&` (as encoding/json v1 does), escapes
// non-ASCII text, or the two forms use different encoders.
func TestTimelineE2EEscapedAgent(t *testing.T) {
	env := tle2xSetup(t)
	dir := env.startTask(t, "plan.json")
	const agent = `/root/<a> & b é "q" \ x`
	env.assign(t, dir, "a1", agent, "evidence", ".")
	_, record := env.show(t, dir)
	if len(record.Assignments) != 1 || record.Assignments[0].Brief.AgentID != agent {
		t.Fatalf("assignments after assign: %+v, want one with agent %q", record.Assignments, agent)
	}
	wantFull, wantSummary := tle2xExpect(t, record, map[string]string{"a1": "assigned"})
	// The agent's bytes, written out by hand.
	const agentBytes = `"agent":"/root/<a> & b é \"q\" \\ x"`
	if !strings.Contains(wantFull, agentBytes) || !strings.Contains(wantSummary, agentBytes) {
		t.Fatalf("oracle disagrees with the hand-written bytes %s:\n%s%s", agentBytes, wantFull, wantSummary)
	}
	full := env.run(t, nil, "task", "timeline", "--task", dir)
	tle2xExpectSuccess(t, "timeline with an escaped agent", full, wantFull)
	if !strings.Contains(full.stdout, agentBytes) {
		t.Errorf("full output does not hold %s: %s", agentBytes, full.stdout)
	}
	summary := env.run(t, nil, "task", "timeline", "--task", dir, "--summary")
	tle2xExpectSuccess(t, "timeline --summary with an escaped agent", summary, wantSummary)
	if !strings.Contains(summary.stdout, agentBytes) {
		t.Errorf("summary does not hold %s: %s", agentBytes, summary.stdout)
	}
}

// TL-3, TL-7 (test review T2): a check run that is still running, read
// through Load and the command, shows state running and finished_at and
// duration n/a. cw refuses shell checks, so the check is `cat` on a FIFO: it
// blocks until the test releases it (see releaseAndWait), then exits 0
// whenever it opened the FIFO. Deadlines bound only the wait for the running
// revision and the wait after the release, never the work, and no process
// outlives the test.
func TestTimelineE2ERunningCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the waiting check reads a FIFO")
	}
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skipf("mkfifo not found: %v", err)
	}
	env := tle2xSetup(t)
	fifo := filepath.Join(env.root, "slow check fifo")
	if output, err := exec.CommandContext(t.Context(), mkfifo, "-m", "600", fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, output)
	}
	type check struct {
		ID             string   `json:"id"`
		Argv           []string `json:"argv"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	plan, err := json.Marshal(map[string]any{
		"title": "t", "kind": "small",
		"requirements": []map[string]any{{"id": "r1", "description": "d", "check_ids": []string{"ok"}, "allow_non_execution": false}},
		"checks": []check{
			{ID: "ok", Argv: []string{"true"}, TimeoutSeconds: 30},
			{ID: "bad", Argv: []string{"false"}, TimeoutSeconds: 30},
			{ID: "slow", Argv: []string{"cat", fifo}, TimeoutSeconds: 600},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := env.startTaskWithPlan(t, "plan-slow.json", string(plan))
	env.mustRun(t, "task", "run", "--task", dir, "--check", "ok", "--summary")

	holder, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open FIFO: %v", err)
	}
	// Not derived from t.Context(): that is canceled before cleanups run, and
	// killing cw there would skip the release below and could orphan cat.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	slow := exec.CommandContext(ctx, env.cw, "--state-dir", env.state, "task", "run", "--task", dir, "--check", "slow", "--summary")
	slow.Dir = env.work
	slow.Env = []string{"PATH=" + env.gitDir + ":/usr/bin:/bin", "HOME=" + env.home}
	slow.Stdin = strings.NewReader("")
	var slowOut, slowErr bytes.Buffer
	slow.Stdout, slow.Stderr = &slowOut, &slowErr
	if err := slow.Start(); err != nil {
		cancel()
		_ = holder.Close()
		t.Fatal(err)
	}
	waited := false
	// The test holds the FIFO open read-write (which does not block on Linux
	// or macOS) from before the check starts, so a cat that opens it while
	// the holder is open gets its open at once and blocks in read. A
	// `running` record does not prove cat has opened the FIFO yet, so the
	// release does not rely on that:
	//   1. close the holder: a cat that already has the FIFO open sees end of
	//      input once no writer is left;
	//   2. open the FIFO for writing in a goroutine (a blocking open, which
	//      completes as soon as cat has it open for reading, however late cat
	//      opens it) and close it at once, so a late cat also sees end of
	//      input;
	//   3. wait for cw, with a deadline on this wait only; on expiry kill cw
	//      and fail;
	//   4. if the writer's open is still pending (cat never opened the FIFO),
	//      complete it with a non-blocking reader so no goroutine is left.
	releaseAndWait := func() error {
		waited = true
		if err := holder.Close(); err != nil {
			t.Errorf("close FIFO: %v", err)
		}
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			if writer, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
				_ = writer.Close()
			}
		}()
		waitDone := make(chan error, 1)
		go func() { waitDone <- slow.Wait() }()
		var err error
		select {
		case err = <-waitDone:
		case <-time.After(time.Minute):
			t.Errorf("slow check did not end within a minute of its release")
			_ = slow.Process.Kill()
			err = <-waitDone
		}
		select {
		case <-writerDone:
		default:
			reader, openErr := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if openErr != nil {
				t.Errorf("open FIFO to finish the release: %v", openErr)
				break
			}
			<-writerDone
			_ = reader.Close()
		}
		return err
	}
	t.Cleanup(func() {
		if !waited {
			_ = releaseAndWait()
		}
		cancel()
	})

	// Wait, bounded, for the revision that records the run as running.
	deadline := time.Now().Add(30 * time.Second)
	var record tle2xRecord
	for {
		_, record = env.show(t, dir)
		running := false
		for _, run := range record.Checks {
			if run.CheckID == "slow" && run.Status == "running" {
				running = true
			}
		}
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no running slow check within 30s; checks: %+v", record.Checks)
		}
	}
	for _, run := range record.Checks {
		if run.CheckID == "slow" && run.FinishedAt != nil && !run.FinishedAt.IsZero() {
			t.Fatalf("running check already has finished_at %v", run.FinishedAt)
		}
	}
	wantFull, wantSummary := tle2xExpect(t, record, map[string]string{"ok": "passed", "slow": "running"})
	first := env.run(t, nil, "task", "timeline", "--task", dir)
	tle2xExpectSuccess(t, "timeline with a running check", first, wantFull)
	if !strings.Contains(first.stdout, `"stage":"slow","state":"running","started_at":`) || !strings.Contains(first.stdout, `"finished_at":"n/a","duration":"n/a"}`) {
		t.Errorf("running check is not shown as running with finished_at and duration n/a: %s", first.stdout)
	}
	summary := env.run(t, nil, "task", "timeline", "--task", dir, "--summary")
	tle2xExpectSuccess(t, "timeline --summary with a running check", summary, wantSummary)
	if !strings.Contains(summary.stdout, `"stage":"slow","state":"running","start":`) || !strings.Contains(summary.stdout, `"duration":"n/a"}`) {
		t.Errorf("summary does not show the running check with duration n/a: %s", summary.stdout)
	}

	// Release the check; once it has finished, the timeline follows the new
	// revision (state passed, a measured duration).
	if err := releaseAndWait(); err != nil {
		t.Fatalf("slow check run: %v\nstdout: %s\nstderr: %s", err, slowOut.String(), slowErr.String())
	}
	_, finished := env.show(t, dir)
	wantFinished, _ := tle2xExpect(t, finished, map[string]string{"ok": "passed", "slow": "passed"})
	tle2xExpectSuccess(t, "timeline after the check finished", env.run(t, nil, "task", "timeline", "--task", dir), wantFinished)
}
