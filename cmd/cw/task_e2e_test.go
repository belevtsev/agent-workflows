package main_test

// End-to-end tests for test contract TC-2 (revision 1): cw task lifecycle.
//
// Requirement mapping (all subtests of TestTaskLifecycleE2E):
//   TL-1 happy_path (start, run ok, assess, verify complete)
//   TL-2 verify_before_assessment
//   TL-3 failed_check_blocks_completion (also the failed-run output of `task run`)
//   TL-4 source_change_makes_evidence_stale
//   TL-5 records_live_outside_repository
//
// Every identifier declared here is prefixed with tle2e; buildPlainCW is the approved scaffold.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	tle2ePlanOK = `{"title":"t","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30}]}`

	tle2ePlanOKBad = `{"title":"t","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30},{"id":"bad","argv":["false"],"timeout_seconds":30}]}`

	tle2eAssessInput = `{"requirements":[{"id":"r1","status":"satisfied","evidence":[{"path":"a.txt","start_line":1,"end_line":2}],"check_ids":["ok"]}],"decision":{"status":"resolved","reason":"checked","evidence":[{"path":"a.txt","start_line":1,"end_line":1}],"check_ids":["ok"]}}`
)

func TestTaskLifecycleE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end test builds the cw binary")
	}
	bin := buildPlainCW(t)

	t.Run("happy_path", func(t *testing.T) {
		// TL-1
		e := tle2eNewEnv(t, bin)
		e.happyPath(t)
	})

	t.Run("verify_before_assessment", func(t *testing.T) {
		// TL-2
		e := tle2eNewEnv(t, bin)
		task, _ := e.start(t, tle2ePlanOK)
		code, v := e.verify(t, task)
		if code != 5 {
			t.Errorf("verify exit %d, want 5", code)
		}
		if v.complete {
			t.Errorf("complete = true, want false")
		}
		tle2eWantProblem(t, v.problems, "requirement r1 is not satisfied")
	})

	t.Run("failed_check_blocks_completion", func(t *testing.T) {
		// TL-3
		e := tle2eNewEnv(t, bin)
		task, _ := e.start(t, tle2ePlanOKBad)
		e.runCheck(t, task, "ok", 0, "passed")
		e.runCheck(t, task, "bad", 1, "failed")
		e.assess(t, task)
		code, v := e.verify(t, task)
		if code != 5 {
			t.Errorf("verify exit %d, want 5", code)
		}
		if v.complete {
			t.Errorf("complete = true, want false")
		}
		tle2eWantProblem(t, v.problems, "latest run of bad is failed")
	})

	t.Run("source_change_makes_evidence_stale", func(t *testing.T) {
		// TL-4
		e := tle2eNewEnv(t, bin)
		task, _ := e.happyPath(t)
		tle2eWrite(t, filepath.Join(e.repo, "a.txt"), "line one\nchanged\n")
		code, v := e.verify(t, task)
		if code != 5 {
			t.Errorf("verify after source change exit %d, want 5", code)
		}
		if v.complete {
			t.Errorf("complete = true after source change, want false")
		}
		tle2eWantProblem(t, v.problems, "requirement r1 evidence is stale")
	})

	t.Run("records_live_outside_repository", func(t *testing.T) {
		// TL-5
		e := tle2eNewEnv(t, bin)
		task, _ := e.happyPath(t)
		if !tle2eInside(task, filepath.Join(e.state, "workflow-tasks")) {
			t.Errorf("task %s is not under %s", task, filepath.Join(e.state, "workflow-tasks"))
		}
		if tle2eInside(task, e.repo) {
			t.Errorf("task %s is inside the repository %s", task, e.repo)
		}
		out := e.git(t, "status", "--porcelain")
		if strings.TrimSpace(out) != "" {
			t.Errorf("git status --porcelain is not empty:\n%s", out)
		}
	})
}

type tle2eEnv struct {
	bin, gitPath, repo, state, home, work string
}

func tle2eNewEnv(t *testing.T, bin string) *tle2eEnv {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not found: %v", err)
	}
	root := t.TempDir()
	e := &tle2eEnv{
		bin:     bin,
		gitPath: gitPath,
		repo:    filepath.Join(root, "repo"),
		state:   filepath.Join(root, "state"),
		home:    filepath.Join(root, "home"),
		work:    filepath.Join(root, "work"),
	}
	for _, d := range []string{e.repo, e.state, e.home, e.work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.git(t, "-c", "init.defaultBranch=main", "init", "-q")
	tle2eWrite(t, filepath.Join(e.repo, "a.txt"), "line one\nline two\n")
	e.git(t, "add", "a.txt")
	e.git(t, "-c", "user.name=Test Author", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false",
		"commit", "-q", "-m", "initial")
	return e
}

func (e *tle2eEnv) pathEnv() string {
	return "PATH=" + filepath.Dir(e.gitPath) + ":/usr/bin:/bin"
}

func (e *tle2eEnv) git(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.gitPath, args...)
	cmd.Dir = e.repo
	cmd.Env = []string{e.pathEnv(), "HOME=" + e.home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return out.String()
}

func (e *tle2eEnv) cw(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Dir = e.work
	cmd.Env = []string{e.pathEnv(), "HOME=" + e.home}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run cw %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

// start runs `cw task start` and checks its summary; it returns the task directory and id.
func (e *tle2eEnv) start(t *testing.T, plan string) (task, taskID string) {
	t.Helper()
	planPath := filepath.Join(e.work, "plan.json")
	tle2eWrite(t, planPath, plan)
	stdout, stderr, code := e.cw(t, "task", "start", "--repo", e.repo, "--plan", planPath, "--state-dir", e.state, "--summary")
	if code != 0 {
		t.Fatalf("task start exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	obj := tle2eDecodeOne(t, stdout)
	task = tle2eString(t, obj, "task")
	taskID = tle2eString(t, obj, "task_id")
	if !filepath.IsAbs(task) {
		t.Errorf("task %q is not absolute", task)
	}
	if !tle2eInside(task, filepath.Join(e.state, "workflow-tasks")) {
		t.Errorf("task %s is not inside %s", task, filepath.Join(e.state, "workflow-tasks"))
	}
	if taskID == "" || taskID != filepath.Base(task) {
		t.Errorf("task_id = %q, want non-empty base name of %q", taskID, task)
	}
	var advisers map[string]json.RawMessage
	if raw, ok := obj["advisers"]; !ok || json.Unmarshal(raw, &advisers) != nil || advisers == nil || len(advisers) != 0 {
		t.Errorf("advisers = %s, want an empty object", obj["advisers"])
	}
	tle2eWantStrings(t, obj, "requirements", []string{"r1"})
	var wantChecks []string
	if plan == tle2ePlanOKBad {
		wantChecks = []string{"ok", "bad"}
	} else {
		wantChecks = []string{"ok"}
	}
	tle2eWantStringSet(t, obj, "checks", wantChecks)
	return task, taskID
}

// runCheck runs `cw task run` for one check and checks its summary.
func (e *tle2eEnv) runCheck(t *testing.T, task, check string, wantCode int, wantStatus string) {
	t.Helper()
	stdout, stderr, code := e.cw(t, "task", "run", "--task", task, "--check", check, "--summary")
	// cw exits 0 for a passed check and 5 (negative result) for any other
	// recorded status; wantCode is the check's own exit code.
	wantExit := 0
	if wantStatus != "passed" {
		wantExit = 5
	}
	if code != wantExit {
		t.Fatalf("task run %s exit %d, want %d\nstdout:\n%s\nstderr:\n%s", check, code, wantExit, stdout, stderr)
	}
	obj := tle2eDecodeOne(t, stdout)
	if got := tle2eString(t, obj, "check_id"); got != check {
		t.Errorf("check_id = %q, want %q", got, check)
	}
	if got := tle2eString(t, obj, "status"); got != wantStatus {
		t.Errorf("status = %q, want %q", got, wantStatus)
	}
	var exitCode json.Number
	if err := json.Unmarshal(obj["exit_code"], &exitCode); err != nil || exitCode.String() != strconv.Itoa(wantCode) {
		t.Errorf("exit_code = %s, want %d", obj["exit_code"], wantCode)
	}
	if wantCode == 0 {
		if tle2eString(t, obj, "run_id") == "" {
			t.Errorf("run_id is empty")
		}
		logPath := tle2eString(t, obj, "log")
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(task, logPath)
		}
		if fi, err := os.Stat(logPath); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("log %q is not an existing file: %v", logPath, err)
		}
		if !tle2eInside(logPath, task) {
			t.Errorf("log %q is not inside task %q", logPath, task)
		}
	}
}

// assess runs `cw task assess` with the contract's assessment input and checks its summary.
func (e *tle2eEnv) assess(t *testing.T, task string) {
	t.Helper()
	input := filepath.Join(e.work, "assess.json")
	tle2eWrite(t, input, tle2eAssessInput)
	stdout, stderr, code := e.cw(t, "task", "assess", "--task", task, "--input", input, "--summary")
	if code != 0 {
		t.Fatalf("task assess exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	obj := tle2eDecodeOne(t, stdout)
	if keys := tle2eKeys(obj); keys != "decision,requirements,task" {
		t.Errorf("assess keys = %s, want decision,requirements,task", keys)
	}
	if got := tle2eString(t, obj, "task"); got != task {
		t.Errorf("assess task = %q, want %q", got, task)
	}
	var reqs map[string]string
	if err := json.Unmarshal(obj["requirements"], &reqs); err != nil || len(reqs) != 1 || reqs["r1"] != "satisfied" {
		t.Errorf("assess requirements = %s, want {\"r1\":\"satisfied\"}", obj["requirements"])
	}
	if got := tle2eString(t, obj, "decision"); got != "resolved" {
		t.Errorf("assess decision = %q, want resolved", got)
	}
}

type tle2eVerify struct {
	taskID   string
	complete bool
	problems []string
}

// verify runs `cw task verify` and checks the shape of its summary.
func (e *tle2eEnv) verify(t *testing.T, task string) (int, tle2eVerify) {
	t.Helper()
	stdout, stderr, code := e.cw(t, "task", "verify", "--task", task, "--summary")
	obj := tle2eDecodeOne(t, stdout)
	if keys := tle2eKeys(obj); keys != "complete,limitations,omitted_fixed_limitations,problems,task_id" {
		t.Errorf("verify keys = %s, want complete,limitations,omitted_fixed_limitations,problems,task_id\nstderr:\n%s", keys, stderr)
	}
	var v tle2eVerify
	v.taskID = tle2eString(t, obj, "task_id")
	if err := json.Unmarshal(obj["complete"], &v.complete); err != nil {
		t.Errorf("complete = %s is not a bool", obj["complete"])
	}
	if raw := obj["problems"]; raw == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &v.problems) != nil {
		t.Errorf("problems = %s is not an array of strings", raw)
	}
	var limitations []json.RawMessage
	if raw := obj["limitations"]; raw == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &limitations) != nil {
		t.Errorf("limitations = %s is not an array", raw)
	}
	var omitted json.Number
	if err := json.Unmarshal(obj["omitted_fixed_limitations"], &omitted); err != nil {
		t.Errorf("omitted_fixed_limitations = %s is not a number", obj["omitted_fixed_limitations"])
	}
	if code == 1 && len(v.problems) == 0 {
		t.Errorf("verify exited 1 with no problems\nstdout:\n%s", stdout)
	}
	return code, v
}

// happyPath runs start, run ok, assess and verify, asserting TL-1.
func (e *tle2eEnv) happyPath(t *testing.T) (task, taskID string) {
	t.Helper()
	task, taskID = e.start(t, tle2ePlanOK)
	e.runCheck(t, task, "ok", 0, "passed")
	e.assess(t, task)
	code, v := e.verify(t, task)
	if code != 0 {
		t.Fatalf("verify exit %d, want 0; problems: %q", code, v.problems)
	}
	if !v.complete {
		t.Errorf("complete = false, want true")
	}
	if len(v.problems) != 0 {
		t.Errorf("problems = %q, want empty", v.problems)
	}
	if v.taskID != taskID {
		t.Errorf("verify task_id = %q, want %q", v.taskID, taskID)
	}
	return task, taskID
}

func tle2eDecodeOne(t *testing.T, stdout string) map[string]json.RawMessage {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Errorf("stdout holds more than one JSON value\n%s", stdout)
	}
	return obj
}

func tle2eString(t *testing.T, obj map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(obj[key], &s); err != nil {
		t.Errorf("%s = %s is not a string", key, obj[key])
	}
	return s
}

func tle2eWantStrings(t *testing.T, obj map[string]json.RawMessage, key string, want []string) {
	t.Helper()
	var got []string
	if err := json.Unmarshal(obj[key], &got); err != nil || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("%s = %s, want %q", key, obj[key], want)
	}
}

func tle2eWantStringSet(t *testing.T, obj map[string]json.RawMessage, key string, want []string) {
	t.Helper()
	var got []string
	if err := json.Unmarshal(obj[key], &got); err != nil {
		t.Errorf("%s = %s, want %q", key, obj[key], want)
		return
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, "\x00") != strings.Join(w, "\x00") {
		t.Errorf("%s = %q, want %q (any order)", key, got, want)
	}
}

func tle2eKeys(obj map[string]json.RawMessage) string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func tle2eWantProblem(t *testing.T, problems []string, substr string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return
		}
	}
	t.Errorf("no problem contains %q; problems: %q", substr, problems)
}

// tle2eInside reports whether p lies strictly inside dir. Both sides are compared lexically and,
// when they exist, after resolving symlinks (temp dirs on darwin live behind /var -> /private/var).
func tle2eInside(p, dir string) bool {
	in := func(p, dir string) bool {
		rel, err := filepath.Rel(dir, p)
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if in(filepath.Clean(p), filepath.Clean(dir)) {
		return true
	}
	rp, err1 := filepath.EvalSymlinks(p)
	rd, err2 := filepath.EvalSymlinks(dir)
	return err1 == nil && err2 == nil && in(rp, rd)
}

func tle2eWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
