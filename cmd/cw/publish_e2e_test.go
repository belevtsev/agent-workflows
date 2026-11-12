//go:build !windows

package main_test

// End-to-end tests for the interface contract of `cw publish` (architect-3,
// revision 3; interface-contract.md sha256
// c561fc7c84541c843cff1fb20432a03d8e5ac3ce9f7fd3400dd8026c97b842ea).
//
// Authored by the independent test engineer (assignment test-engineer-1) from
// that contract alone. Every subtest of TestPublishE2E is named after the
// requirement it guards and fails when that requirement is violated;
// test-matrix.md and requirement-map.md in the artefact directory map each
// case. The tests build the real cw binary once and drive it through its
// command line only, with:
//   - a local bare repository as the remote;
//   - a fake gh built from testdata/publish/helper and placed first on PATH,
//     keeping pull-request state in a JSON file per test;
//   - Git hooks built from the same helper (no shell scripts);
//   - a PATH holding only a git wrapper, `true` and, when wanted, the fake gh,
//     and HOME, TMPDIR, GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM=1 inside the
//     test's temporary root, so the user's environment never decides an
//     outcome.
// Timing cases coordinate through marker files; deadlines only bound waiting.
//
// Every identifier declared here is prefixed with pub.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	pubPlanSmall      = `{"title":"publish fixture","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30}]}`
	pubPlanOther      = `{"title":"Other open work","kind":"substantial","checkpoints":{"required":false,"reason":"publish fixture"},"requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30}]}`
	pubPlanOtherSmall = `{"title":"Other small work","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":["true"],"timeout_seconds":30}]}`
	pubAssessInput    = `{"requirements":[{"id":"r1","status":"satisfied","evidence":[{"path":"a.txt","start_line":1,"end_line":2}],"check_ids":["ok"]}],"decision":{"status":"resolved","reason":"checked","evidence":[{"path":"a.txt","start_line":1,"end_line":1}],"check_ids":["ok"]}}`

	pubRepo     = "octo/widget"
	pubBranch   = "feature"
	pubBase     = "main"
	pubTitle    = "Publish the widget change"
	pubMessage  = "Publish the widget change\n\nThe body of the commit message.\n"
	pubBody     = "Why: the widget needed it.\r\n\r\nTrailing spaces stay in stdin.  \n"
	pubPRFields = "number,url,state,headRefName,headRefOid,baseRefName,title,body,isCrossRepository"
	pubSSHURL   = "git@github.example:octo/widget.git"

	// pubDeadline bounds waiting for one cw run or one marker file.
	pubDeadline = 3 * time.Minute

	// pubBlockedTimeout is the --timeout of runs in which one command blocks
	// until stopped: far above what any unblocked git or gh command needs,
	// so only the blocked command reaches it.
	pubBlockedTimeout = "20s"
)

// The standard change of pubEnv.standardChange: modified, deleted, new and
// new executable paths. pubStandardNew lists the paths the index does not
// list before the commit (the expected intent-to-add paths, R13.e).
var (
	pubStandardD   = []string{"a.txt", "gone.txt", "new.txt", "tool"}
	pubStandardNew = []string{"new.txt", "tool"}
	pubHex40       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	pubHex64       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	pubHex32       = regexp.MustCompile(`^[0-9a-f]{32}$`)
	pubEntryName   = regexp.MustCompile(`^publication-([0-9]{6})\.json$`)
)

func TestPublishE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end test builds the cw binary and runs git")
	}
	tl := pubBuildTools(t)
	cases := []struct {
		name string
		fn   func(*testing.T, *pubTools)
	}{
		{"R1_usage_errors_exit_2_and_change_nothing", pubTestUsage},
		{"R1d_name_rules_property", pubTestNameRules},
		{"R2_R13a_R15b_R19_R21_happy_path_without_pr", pubTestHappyPath},
		{"R13a_message_bytes_are_committed_verbatim", pubTestMessageVerbatim},
		{"R13b_R6d_unrelated_staged_and_unstaged_work_preserved", pubTestUnrelatedWork},
		{"R8_R4_path_subset_without_gh", pubTestPathSubset},
		{"R8_nothing_to_publish", pubTestNothingToPublish},
		{"R8_path_outside_verified_change_set", pubTestPathNotVerified},
		{"R6c_bytes_or_mode_changed_after_verify", pubTestSourceChanged},
		{"R6c_capture_failed", pubTestCaptureFailed},
		{"R6a_R6b_task_not_verified_or_abandoned", pubTestGateNotVerified},
		{"R7a_R7b_R7d_other_open_records", pubTestOpenRecords},
		{"R7_open_record_from_cw_v2_0_96", pubTestOpenLegacyRecord},
		{"R7_open_record_that_cannot_be_read", pubTestOpenUnreadableRecord},
		{"R7c_hook_store_and_task_store", pubTestOpenRecordStores},
		{"R7e_unreadable_store", pubTestStoreUnreadable},
		{"R7_R10c_linked_worktree", pubTestLinkedWorktree},
		{"R9a_not_a_repository", pubTestNotARepository},
		{"R9b_R9c_branch_and_head", pubTestBranchHead},
		{"R10a_hunk_scope", pubTestHunkScope},
		{"R10b_conversions_for_cr_bytes_and_attributes", pubTestConversionCR},
		{"R10b_file_mode_conversions", pubTestConversionMode},
		{"R10c_git_lock_listing", pubTestGitLocks},
		{"R11_remote_moved_and_not_fast_forward", pubTestRemoteRefs},
		{"R11g_one_repository", pubTestRemoteSplit},
		{"R12_pr_preconditions", pubTestPRPreconditions},
		{"R12c_equivalent_pr_already_present", pubTestPREquivalentExists},
		{"R16a_R16c_R22_R2_pr_create_and_summary", pubTestPRCreate},
		{"R16b_pr_edit_existing", pubTestPREdit},
		{"R16d_R17_R20b_pr_unknown_after_create", pubTestPRUnknownAfterCreate},
		{"R16d_R20b_pr_unknown_before_create", pubTestPRUnknownBeforeCreate},
		{"R16d_R17_R20b_pr_edit_unknown", pubTestPREditUnknown},
		{"R16a_R16d_R20b_pr_not_located_after_create", pubTestPRNotLocated},
		{"R16d_R20b_pr_view_fails_after_create_or_edit", pubTestPRViewFails},
		{"R16d_R18c_R20b_interrupt_during_pr_create", pubTestInterruptPR},
		{"R16c_pr_mismatch_title", pubTestPRMismatchTitle},
		{"R16c_R17_pr_readback_body_and_state", pubTestPRReadbackBodyState},
		{"R16a_R16c_readback_retries", pubTestPRReadbackRetries},
		{"R16c_pr_mismatch_after_five_views", pubTestPRReadbackStale},
		{"R16_pr_step_reread_refuses", pubTestPRReRead},
		{"R15d_R17_R20c_remote_refuses_push", pubTestPushRefused},
		{"R15d_R17_pre_push_hook_fails", pubTestPrePushFails},
		{"R15c_push_mismatch", pubTestPushMismatch},
		{"R15_push_reread_remote_moved", pubTestRemoteMovedAtPush},
		{"R15_push_reread_git_locked", pubTestLockedAtPush},
		{"R15_push_reread_remote_split", pubTestSplitAtPush},
		{"R6f_R13_commit_reread_source_changed", pubTestCommitReReadSource},
		{"R13_commit_reread_head_moved", pubTestCommitReReadHead},
		{"R13_commit_reread_git_locked", pubTestCommitReReadLock},
		{"R6f_commit_reread_task_abandoned", pubTestCommitReReadAbandoned},
		{"R6f_R15_push_reread_task_abandoned", pubTestPushReReadAbandoned},
		{"R6f_R16_pr_reread_task_abandoned", pubTestPRReReadAbandoned},
		{"R13c_R20a_nothing_to_commit", pubTestDEmpty},
		{"R14a_R17_commit_unknown_lock_left", pubTestCommitUnknown},
		{"R14b_R21b_commit_failed", pubTestCommitFailed},
		{"R14b_R13e_commit_failed_new_paths", pubTestCommitFailedNewPaths},
		{"R14d_commit_mismatch", pubTestCommitMismatch},
		{"R14d_R19_R9c_commit_mismatch_in_bytes_or_mode", pubTestCommitMismatchTree},
		{"R14e_R18a_commit_timeout", pubTestCommitTimeout},
		{"hook_background_job_outlives_the_commit", pubTestHookBackgroundJob},
		{"R18c_interrupt_during_commit", pubTestInterruptCommit},
		{"R18c_R20c_interrupt_during_push", pubTestInterruptPush},
		{"R18b_interrupt_during_read_only_command", pubTestInterruptReadOnly},
		{"R18a_read_only_timeout", pubTestReadOnlyTimeout},
		{"R19_R20d_R5_killed_run_leaves_started_entry", pubTestKilledRun},
		{"R5a_concurrent_run_is_locked", pubTestConcurrentLocked},
		{"R5b_publication_log_invalid", pubTestRecordInvalid},
		{"R5_task_unreadable", pubTestTaskUnreadable},
		{"R20a_rerun_and_push_on_top", pubTestReruns},
		{"R3_precedence_ladder", pubTestLadder},
		{"R4_git_missing", pubTestGitMissing},
		{"R22_environment_isolation", pubTestEnvIsolation},
		{"R23_checks_cannot_run_cw_publish", pubTestCheckGuard},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.fn(t, tl)
		})
	}
}

// ---------------------------------------------------------------------------
// Tools, environment and process helpers.

type pubTools struct {
	cw      string // the built cw binary
	helper  string // the built helper program
	realGit string // absolute path of the real git
	toolDir string // PATH directory holding git (wrapper) and true
	ghDir   string // PATH directory holding the fake gh
	utilDir string // pubappend and pubtouch, called by absolute path
	policy  string // model policy for substantial plans
}

func pubCanonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func pubLink(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.Link(src, dst); err == nil {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func pubWriteFile(t *testing.T, p string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func pubWriteJSON(t *testing.T, p string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pubWriteFile(t, p, data, 0o644)
}

func pubBuildTools(t *testing.T) *pubTools {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not found: %v", err)
	}
	if realGit, err = filepath.Abs(realGit); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(realGit, "version").Output()
	if err != nil {
		t.Fatalf("git version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Fatalf("git version output %q", out)
	}
	parts := strings.Split(fields[2], ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 2 || (major == 2 && minor < 24) {
		t.Skipf("git %s is older than 2.24, which the contract requires", fields[2])
	}
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := pubCanonicalTemp(t)
	tl := &pubTools{
		cw:      buildPlainCW(t),
		realGit: realGit,
		helper:  filepath.Join(root, "helper"),
		toolDir: filepath.Join(root, "pub tools"),
		ghDir:   filepath.Join(root, "pub gh"),
		utilDir: filepath.Join(root, "pub util"),
		policy:  filepath.Join(module, "skills", "task-orchestration", "model-policy.yaml"),
	}
	build := exec.CommandContext(t.Context(), goTool(t), "build", "-o", tl.helper, "./cmd/cw/testdata/publish/helper")
	build.Dir = module
	build.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build helper: %v\n%s", err, output)
	}
	for _, d := range []string{tl.toolDir, tl.ghDir, tl.utilDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pubLink(t, tl.helper, filepath.Join(tl.toolDir, "git"))
	pubWriteJSON(t, filepath.Join(tl.toolDir, "git.json"), map[string]string{"exec": realGit})
	pubLink(t, tl.helper, filepath.Join(tl.toolDir, "true"))
	pubLink(t, tl.helper, filepath.Join(tl.ghDir, "gh"))
	pubLink(t, tl.helper, filepath.Join(tl.utilDir, "pubappend"))
	pubLink(t, tl.helper, filepath.Join(tl.utilDir, "pubtouch"))
	return tl
}

// pubEnv is one isolated world: a repository on branch feature with a bare
// remote holding main, a state directory, a home, a working directory and the
// fake gh's files, all under a root whose name contains a space.
type pubEnv struct {
	t                                    *testing.T
	tl                                   *pubTools
	root, repo, gitDir, remote, state    string
	home, work, tmp, logs                string
	gitconfig, ghState, ghLog, msg, body string
	task, taskID, base, worktree         string
	gh                                   bool     // put the fake gh first on PATH
	extra                                []string // extra environment for cw runs
	seq                                  int
}

const pubGitconfig = "[user]\n\tname = Pub Tester\n\temail = pub@example.invalid\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n[advice]\n\tdetachedHead = false\n"

var pubBaseline = map[string]string{
	"a.txt":     "line one\nline two\n",
	"b.txt":     "bee\n",
	"gone.txt":  "to be deleted\n",
	"u.txt":     "unrelated\n",
	"w.txt":     "worktree\n",
	"sub/s.txt": "sub\n",
}

func pubNewEnv(t *testing.T, tl *pubTools) *pubEnv {
	t.Helper()
	root := filepath.Join(pubCanonicalTemp(t), "pub root")
	e := &pubEnv{t: t, tl: tl, root: root, gh: true}
	e.repo = filepath.Join(root, "repo")
	e.worktree = e.repo
	e.gitDir = filepath.Join(e.repo, ".git")
	e.remote = filepath.Join(root, "remote.git")
	e.state = filepath.Join(root, "state")
	e.home = filepath.Join(root, "home")
	e.work = filepath.Join(root, "work")
	e.tmp = filepath.Join(root, "tmp")
	e.logs = filepath.Join(root, "logs")
	for _, d := range []string{e.repo, e.state, e.home, e.work, e.tmp, e.logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.gitconfig = filepath.Join(root, "gitconfig")
	pubWriteFile(t, e.gitconfig, []byte(pubGitconfig), 0o644)
	e.ghState = filepath.Join(root, "gh-state.json")
	e.ghLog = filepath.Join(e.logs, "gh.jsonl")
	e.msg = filepath.Join(e.work, "message.txt")
	pubWriteFile(t, e.msg, []byte(pubMessage), 0o644)
	e.body = filepath.Join(e.work, "body.md")
	pubWriteFile(t, e.body, []byte(pubBody), 0o644)

	e.git("init", "-q")
	for _, name := range slices.Sorted(maps.Keys(pubBaseline)) {
		e.write(name, pubBaseline[name])
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", "initial")
	e.base = e.rev("HEAD")
	e.gitIn(root, "init", "-q", "--bare", e.remote)
	e.git("remote", "add", "origin", e.remote)
	e.git("push", "-q", "origin", "main")
	e.git("checkout", "-q", "-b", pubBranch)
	e.saveGH(pubGHState{Repo: pubRepo, RepoURL: e.remote, SSHURL: pubSSHURL, RemoteGitDir: e.remote, NextNumber: 1, PRs: []pubPR{}})
	return e
}

func (e *pubEnv) next() int {
	e.seq++
	return e.seq
}

func (e *pubEnv) gitEnv() []string {
	return []string{"PATH=" + e.tl.toolDir, "HOME=" + e.home, "GIT_CONFIG_GLOBAL=" + e.gitconfig, "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C", "TMPDIR=" + e.tmp}
}

func (e *pubEnv) gitRun(dir, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.tl.realGit, args...)
	cmd.Dir = dir
	cmd.Env = e.gitEnv()
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %q in %s: %v\n%s", args, dir, err, errb.String())
	}
	return out.String(), nil
}

func (e *pubEnv) gitIn(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.gitRun(dir, "", args...)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *pubEnv) git(args ...string) string {
	e.t.Helper()
	return e.gitIn(e.repo, args...)
}

func (e *pubEnv) rev(r string) string {
	e.t.Helper()
	return strings.TrimSpace(e.git("rev-parse", "--verify", "-q", r))
}

// remoteRef is R of the contract for a bare repository: the commit of
// refs/heads/BRANCH, or "absent".
func (e *pubEnv) remoteRef(gitDir, branch string) string {
	e.t.Helper()
	out, err := e.gitRun(e.root, "", "--git-dir="+gitDir, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if err != nil {
		return "absent"
	}
	return strings.TrimSpace(out)
}

func (e *pubEnv) refsOf(gitDir string) string {
	e.t.Helper()
	return e.gitIn(e.root, "--git-dir="+gitDir, "for-each-ref", "--format=%(refname) %(objectname)")
}

func (e *pubEnv) lsFiles() []string {
	e.t.Helper()
	return pubSplitNUL(e.git("ls-files", "-z"))
}

func pubSplitNUL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (e *pubEnv) write(rel, content string) { e.writeMode(rel, content, 0o644) }

func (e *pubEnv) writeMode(rel, content string, mode os.FileMode) {
	e.t.Helper()
	pubWriteFile(e.t, filepath.Join(e.repo, rel), []byte(content), mode)
}

func (e *pubEnv) remove(rel string) {
	e.t.Helper()
	if err := os.Remove(filepath.Join(e.repo, rel)); err != nil {
		e.t.Fatal(err)
	}
}

// pubFile is the expected committed state of a path; nil means deleted.
type pubFile struct {
	mode string
	data string
}

// standardChange edits the worktree after the task started: a.txt modified,
// gone.txt deleted, new.txt added, tool added with mode 0755.
func (e *pubEnv) standardChange() map[string]*pubFile {
	e.t.Helper()
	e.write("a.txt", "line one\nline two\nline three\n")
	e.write("new.txt", "new file\n")
	e.writeMode("tool", "tool\n", 0o755)
	e.remove("gone.txt")
	return map[string]*pubFile{
		"a.txt":    {"100644", "line one\nline two\nline three\n"},
		"gone.txt": nil,
		"new.txt":  {"100644", "new file\n"},
		"tool":     {"100755", "tool\n"},
	}
}

func (e *pubEnv) env(extra ...string) []string {
	path := e.tl.toolDir
	if e.gh {
		path = e.tl.ghDir + string(os.PathListSeparator) + path
	}
	env := []string{
		"PATH=" + path, "HOME=" + e.home, "GIT_CONFIG_GLOBAL=" + e.gitconfig, "GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C", "TERM=dumb", "TMPDIR=" + e.tmp,
		"PUBE2E_GH_STATE=" + e.ghState, "PUBE2E_GH_LOG=" + e.ghLog, "PUBE2E_PASSTHROUGH=kept",
	}
	env = append(env, e.extra...)
	return append(env, extra...)
}

type pubRun struct {
	args           []string
	code           int
	stdout, stderr string
	gitLog         string      // the git wrapper's log of a publish run
	git            pubGitCount // its state-changing git commands
	pubBefore      int         // publication files before the run, -1 when unknown
}

type pubProc struct {
	cmd      *exec.Cmd
	ctx      context.Context
	cancel   context.CancelFunc
	out, err bytes.Buffer
	args     []string
	done     chan struct{} // closed when the process has been waited for
	waitErr  error
	gitLog   string
	// pubBefore counts the task's publication files before the run.
	pubBefore int
}

func (e *pubEnv) startCW(extraEnv []string, args ...string) *pubProc {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), pubDeadline)
	p := &pubProc{ctx: ctx, cancel: cancel, args: args, done: make(chan struct{})}
	if len(args) > 0 && args[0] == "publish" {
		// The git wrapper records every git command of this run.
		p.gitLog = filepath.Join(e.logs, fmt.Sprintf("git-%d.jsonl", e.next()))
		extraEnv = append(slices.Clone(extraEnv), "PUBE2E_GIT_LOG="+p.gitLog)
		// It also lists the task's publication files at each git command, so
		// R19's started entry can be checked before the first state-changing
		// command.
		p.pubBefore = -1
		if task := pubFlagValue(args, "--task"); task != "" {
			if entries, err := os.ReadDir(task); err == nil {
				p.pubBefore = 0
				for _, de := range entries {
					if strings.HasPrefix(de.Name(), "publication-") {
						p.pubBefore++
					}
				}
				extraEnv = append(extraEnv, "PUBE2E_TASK_DIR="+task)
			}
		}
	}
	cmd := exec.CommandContext(ctx, e.tl.cw, args...)
	cmd.Dir = e.work
	cmd.Env = e.env(extraEnv...)
	// cw gets its own process group, so whatever group it signals is never
	// the test's; on the deadline the whole group is killed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Stdout, cmd.Stderr = &p.out, &p.err
	p.cmd = cmd
	if err := cmd.Start(); err != nil {
		cancel()
		e.t.Fatalf("start cw %q: %v", args, err)
	}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p
}

func (p *pubProc) wait(t *testing.T) pubRun {
	t.Helper()
	<-p.done
	err := p.waitErr
	defer p.cancel()
	if p.ctx.Err() != nil {
		t.Fatalf("cw %q did not finish within %s\nstdout:\n%s\nstderr:\n%s", p.args, pubDeadline, p.out.String(), p.err.String())
	}
	r := pubRun{args: p.args, stdout: p.out.String(), stderr: p.err.String(), gitLog: p.gitLog, pubBefore: p.pubBefore}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		r.code = exitErr.ExitCode()
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.code = -int(ws.Signal())
		}
	default:
		t.Fatalf("cw %q: %v", p.args, err)
	}
	return r
}

func (e *pubEnv) cw(extraEnv []string, args ...string) pubRun {
	e.t.Helper()
	return e.waitRun(e.startCW(extraEnv, args...))
}

// waitRun waits for p and, for a publish run, checks its git commands.
func (e *pubEnv) waitRun(p *pubProc) pubRun {
	e.t.Helper()
	r := p.wait(e.t)
	if r.gitLog != "" {
		r.git = e.checkGit(r)
	}
	return r
}

func (e *pubEnv) cwOK(args ...string) string {
	e.t.Helper()
	r := e.cw(nil, args...)
	if r.code != 0 {
		e.t.Fatalf("cw %q: exit %d\nstdout:\n%s\nstderr:\n%s", args, r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

// startOther runs `cw task start` for plan on repo, in store when given and
// in the state directory otherwise; it returns the task directory and id.
func (e *pubEnv) startOther(plan, repo, store string) (string, string) {
	e.t.Helper()
	planPath := filepath.Join(e.work, fmt.Sprintf("plan-%d.json", e.next()))
	pubWriteFile(e.t, planPath, []byte(plan), 0o644)
	args := []string{"task", "start", "--repo", repo, "--plan", planPath, "--summary"}
	if store != "" {
		args = append(args, "--store", store)
	} else {
		args = append(args, "--state-dir", e.state)
	}
	if strings.Contains(plan, `"kind":"substantial"`) {
		args = append(args, "--policy", e.tl.policy)
	}
	out := e.cwOK(args...)
	var s struct {
		Task   string `json:"task"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || s.Task == "" || s.TaskID == "" {
		e.t.Fatalf("task start summary %q: %v", out, err)
	}
	task, err := filepath.EvalSymlinks(s.Task)
	if err != nil {
		e.t.Fatal(err)
	}
	return task, s.TaskID
}

func (e *pubEnv) startTask(plan string) {
	e.t.Helper()
	e.task, e.taskID = e.startOther(plan, e.repo, "")
}

func (e *pubEnv) verifyTask() { e.t.Helper(); e.verifyTaskAt(e.task) }

func (e *pubEnv) verifyTaskAt(task string) {
	e.t.Helper()
	e.cwOK("task", "run", "--task", task, "--check", "ok", "--summary")
	input := filepath.Join(e.work, fmt.Sprintf("assess-%d.json", e.next()))
	pubWriteFile(e.t, input, []byte(pubAssessInput), 0o644)
	e.cwOK("task", "assess", "--task", task, "--input", input, "--summary")
	code, complete, problems := e.verifySummary(task)
	if code != 0 || !complete {
		e.t.Fatalf("verify: exit %d complete %v problems %q", code, complete, problems)
	}
}

func (e *pubEnv) verifySummary(task string) (int, bool, []string) {
	e.t.Helper()
	r := e.cw(nil, "task", "verify", "--task", task, "--summary")
	var s struct {
		Complete bool     `json:"complete"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		e.t.Fatalf("verify summary %q: %v\nstderr:\n%s", r.stdout, err, r.stderr)
	}
	return r.code, s.Complete, s.Problems
}

// pubFlags is the argv of `cw publish` after the subcommand.
type pubFlags []string

var pubValueless = map[string]bool{"--summary": true, "--dry-run": true}

func (f pubFlags) drop(name string) pubFlags {
	var out pubFlags
	for i := 0; i < len(f); i++ {
		if f[i] == name {
			if !pubValueless[name] {
				i++
			}
			continue
		}
		out = append(out, f[i])
	}
	return out
}

func (f pubFlags) set(name string, values ...string) pubFlags {
	out := f.drop(name)
	for _, v := range values {
		out = append(out, name, v)
	}
	return out
}

func (f pubFlags) add(args ...string) pubFlags { return append(slices.Clone(f), args...) }

func (f pubFlags) argv() []string { return append([]string{"publish"}, f...) }

func (e *pubEnv) flags() pubFlags {
	return pubFlags{"--task", e.task, "--message-file", e.msg, "--expect-head", e.base, "--push", "origin", "--branch", pubBranch, "--state-dir", e.state}
}

func (e *pubEnv) prFlags(mode string) pubFlags {
	return e.flags().add("--pr", mode, "--repo", pubRepo, "--base", pubBase, "--title", pubTitle, "--body-file", e.body)
}

func (e *pubEnv) publishF(f pubFlags, extraEnv ...string) pubRun {
	e.t.Helper()
	return e.cw(extraEnv, f.argv()...)
}

func (e *pubEnv) publish(extra ...string) pubRun {
	e.t.Helper()
	return e.publishF(e.flags().add(extra...))
}

// ---------------------------------------------------------------------------
// Report, Summary and publication record (contract sections 6 and 7).

type pubOpenRecord struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Problem   string `json:"problem"`
}

type pubCommitStep struct {
	Status string `json:"status"`
	Commit string `json:"commit"`
	Detail string `json:"detail"`
}

type pubPushStep struct {
	Status    string `json:"status"`
	RemoteRef string `json:"remote_ref"`
	Detail    string `json:"detail"`
}

type pubPRStep struct {
	Status     string `json:"status"`
	Number     int    `json:"number"`
	URL        string `json:"url"`
	HeadRefOID string `json:"head_ref_oid"`
	Base       string `json:"base"`
	State      string `json:"state"`
	Detail     string `json:"detail"`
}

type pubSteps struct {
	Commit pubCommitStep `json:"commit"`
	Push   pubPushStep   `json:"push"`
	PR     pubPRStep     `json:"pr"`
}

type pubReport struct {
	Status          string          `json:"status"`
	Reason          string          `json:"reason"`
	Detail          string          `json:"detail"`
	Task            string          `json:"task"`
	TaskID          string          `json:"task_id"`
	Worktree        string          `json:"worktree"`
	SourceSHA256    string          `json:"source_sha256"`
	Paths           []string        `json:"paths"`
	CommittedPaths  []string        `json:"committed_paths"`
	IntentToAdd     []string        `json:"intent_to_add"`
	LocksLeft       []string        `json:"locks_left"`
	ExpectHead      string          `json:"expect_head"`
	Branch          string          `json:"branch"`
	Remote          string          `json:"remote"`
	ExpectRemote    string          `json:"expect_remote"`
	OpenRecords     []pubOpenRecord `json:"open_records"`
	Steps           pubSteps        `json:"steps"`
	PreviousOutcome string          `json:"previous_outcome"`
	Record          string          `json:"record"`
}

type pubSummary struct {
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	TaskID    string `json:"task_id"`
	Commit    string `json:"commit"`
	RemoteRef string `json:"remote_ref"`
	PRNumber  int    `json:"pr_number"`
	PRURL     string `json:"pr_url"`
	Record    string `json:"record"`
}

type pubPublicationPR struct {
	Mode       string `json:"mode"`
	Repo       string `json:"repo"`
	Base       string `json:"base"`
	Title      string `json:"title"`
	BodySHA256 string `json:"body_sha256"`
}

type pubRequest struct {
	SourceSHA256   string           `json:"source_sha256"`
	Paths          []string         `json:"paths"`
	CommittedPaths []string         `json:"committed_paths"`
	MessageSHA256  string           `json:"message_sha256"`
	ExpectHead     string           `json:"expect_head"`
	Branch         string           `json:"branch"`
	Remote         string           `json:"remote"`
	ExpectRemote   string           `json:"expect_remote"`
	PR             pubPublicationPR `json:"pr"`
}

type pubEntry struct {
	Version        int         `json:"version"`
	TaskID         string      `json:"task_id"`
	Sequence       int         `json:"sequence"`
	PreviousSHA256 string      `json:"previous_sha256"`
	RunID          string      `json:"run_id"`
	Kind           string      `json:"kind"`
	At             string      `json:"at"`
	Request        *pubRequest `json:"request,omitempty"`
	Status         string      `json:"status,omitempty"`
	Reason         string      `json:"reason,omitempty"`
	ExitCode       *int        `json:"exit_code,omitempty"`
	Steps          *pubSteps   `json:"steps,omitempty"`

	path string
}

const (
	pubReportKeys   = "branch,committed_paths,detail,expect_head,expect_remote,intent_to_add,locks_left,open_records,paths,previous_outcome,reason,record,remote,source_sha256,status,steps,task,task_id,worktree"
	pubSummaryKeys  = "commit,pr_number,pr_url,reason,record,remote_ref,status,task_id"
	pubStepsKeys    = "commit,pr,push"
	pubCommitKeys   = "commit,detail,status"
	pubPushKeys     = "detail,remote_ref,status"
	pubPRKeys       = "base,detail,head_ref_oid,number,state,status,url"
	pubOpenKeys     = "directory,problem,task_id,title"
	pubStartedKeys  = "at,kind,previous_sha256,request,run_id,sequence,task_id,version"
	pubFinishedKeys = "at,exit_code,kind,previous_sha256,run_id,sequence,status,steps,task_id,version"
	pubRequestKeys  = "branch,committed_paths,expect_head,expect_remote,message_sha256,paths,pr,remote,source_sha256"
	pubReqPRKeys    = "base,body_sha256,mode,repo,title"
)

// pubKeys decodes raw as a JSON object and checks its exact key set.
func pubKeys(t *testing.T, label string, raw []byte, want string) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		t.Fatalf("%s is not a JSON object: %v\n%s", label, err, raw)
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range obj {
			if !yield(k) {
				return
			}
		}
	})
	if got := strings.Join(keys, ","); got != want {
		t.Errorf("%s keys = %s, want %s", label, got, want)
	}
	return obj
}

func pubArray(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		t.Errorf("%s = %s, want a JSON array (never null)", label, raw)
	}
}

func pubDecodeSteps(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	steps := pubKeys(t, label, raw, pubStepsKeys)
	pubKeys(t, label+".commit", steps["commit"], pubCommitKeys)
	pubKeys(t, label+".push", steps["push"], pubPushKeys)
	pubKeys(t, label+".pr", steps["pr"], pubPRKeys)
}

func pubDecodeReport(t *testing.T, stdout string) *pubReport {
	t.Helper()
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Errorf("stdout is not exactly one line:\n%q", stdout)
	}
	obj := pubKeys(t, "report", []byte(stdout), pubReportKeys)
	for _, k := range []string{"paths", "committed_paths", "intent_to_add", "locks_left", "open_records"} {
		pubArray(t, "report."+k, obj[k])
	}
	pubDecodeSteps(t, "report.steps", obj["steps"])
	var open []json.RawMessage
	_ = json.Unmarshal(obj["open_records"], &open)
	for i, o := range open {
		pubKeys(t, fmt.Sprintf("report.open_records[%d]", i), o, pubOpenKeys)
	}
	var rep pubReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("report does not decode: %v\n%s", err, stdout)
	}
	return &rep
}

func pubOneLine(t *testing.T, label, s, prefix string) {
	t.Helper()
	if !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 || !strings.HasPrefix(s, prefix) {
		t.Errorf("%s = %q, want exactly one line starting %q", label, s, prefix)
	}
}

// pubWant checks the exit status, the single-line output of R2 and the
// status and reason of R3, and returns the Report (nil for exit 2).
func pubWant(t *testing.T, r pubRun, code int, status, reason string) *pubReport {
	t.Helper()
	if r.code != code {
		t.Fatalf("cw %q: exit %d, want %d (%s %s)\nstdout:\n%s\nstderr:\n%s", r.args, r.code, code, status, reason, r.stdout, r.stderr)
	}
	if code == 2 {
		if r.stdout != "" {
			t.Errorf("usage error printed stdout %q, want empty", r.stdout)
		}
		pubOneLine(t, "usage stderr", r.stderr, "agent-workflows: ")
		return nil
	}
	rep := pubDecodeReport(t, r.stdout)
	if rep.Status != status || rep.Reason != reason {
		t.Fatalf("cw %q: status %q reason %q, want %q %q\nstdout:\n%s\nstderr:\n%s", r.args, rep.Status, rep.Reason, status, reason, r.stdout, r.stderr)
	}
	if code == 0 {
		if r.stderr != "" {
			t.Errorf("exit 0 wrote stderr %q, want empty", r.stderr)
		}
	} else {
		pubOneLine(t, "stderr", r.stderr, fmt.Sprintf("agent-workflows: publish %s (%s): ", status, reason))
	}
	return rep
}

func pubWantSummary(t *testing.T, r pubRun) *pubSummary {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("cw %q: exit %d, want 0\nstdout:\n%s\nstderr:\n%s", r.args, r.code, r.stdout, r.stderr)
	}
	if !strings.HasSuffix(r.stdout, "\n") || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("summary stdout is not exactly one line:\n%q", r.stdout)
	}
	pubKeys(t, "summary", []byte(r.stdout), pubSummaryKeys)
	var s pubSummary
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		t.Fatal(err)
	}
	if r.stderr != "" {
		t.Errorf("exit 0 wrote stderr %q", r.stderr)
	}
	return &s
}

func pubWantSteps(t *testing.T, rep *pubReport, commit, push, pr string) {
	t.Helper()
	if rep.Steps.Commit.Status != commit || rep.Steps.Push.Status != push || rep.Steps.PR.Status != pr {
		t.Errorf("steps = commit %q push %q pr %q, want %q %q %q", rep.Steps.Commit.Status, rep.Steps.Push.Status, rep.Steps.PR.Status, commit, push, pr)
	}
}

func pubWantStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if got == nil {
		got = []string{}
	}
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", label, got, want)
	}
}

func pubSamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func pubSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (e *pubEnv) fileSHA(p string) string {
	e.t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	return pubSHA(data)
}

// pubEntryNames lists the publication files of a task directory, sorted.
func pubEntryNames(t *testing.T, task string) []string {
	t.Helper()
	entries, err := os.ReadDir(task)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), "publication-") {
			names = append(names, de.Name())
		}
	}
	sort.Strings(names)
	return names
}

// entries reads and validates the publication record of section 7: names
// without gaps from 000001, private regular files, version 1, the task id,
// sequence, the previous-file chain, run ids, UTC RFC 3339 times, the kind
// shapes, and every finished entry directly after its own started entry.
func (e *pubEnv) entries() []pubEntry {
	t := e.t
	t.Helper()
	names := pubEntryNames(t, e.task)
	var out []pubEntry
	prev := ""
	for i, name := range names {
		m := pubEntryName.FindStringSubmatch(name)
		if m == nil {
			t.Fatalf("unexpected file %s in the task directory", name)
		}
		if n, _ := strconv.Atoi(m[1]); n != i+1 {
			t.Fatalf("publication files %q have a gap at %s", names, name)
		}
		p := filepath.Join(e.task, name)
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want a regular file with mode 0600", name, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var en pubEntry
		if err := json.Unmarshal(data, &en); err != nil {
			t.Fatalf("%s does not decode: %v\n%s", name, err, data)
		}
		en.path = p
		switch en.Kind {
		case "started":
			obj := pubKeys(t, name, data, pubStartedKeys)
			req := pubKeys(t, name+".request", obj["request"], pubRequestKeys)
			pubArray(t, name+".request.paths", req["paths"])
			pubArray(t, name+".request.committed_paths", req["committed_paths"])
			pubKeys(t, name+".request.pr", req["pr"], pubReqPRKeys)
		case "finished":
			want := pubFinishedKeys
			if en.Reason != "" {
				want = "at,exit_code,kind,previous_sha256,reason,run_id,sequence,status,steps,task_id,version"
			}
			obj := pubKeys(t, name, data, want)
			pubDecodeSteps(t, name+".steps", obj["steps"])
			if i == 0 || out[i-1].Kind != "started" || out[i-1].RunID != en.RunID {
				t.Errorf("%s is a finished entry not directly after the started entry of run %s", name, en.RunID)
			}
		default:
			t.Errorf("%s kind %q", name, en.Kind)
		}
		if en.Version != 1 || en.TaskID != e.taskID || en.Sequence != i+1 || en.PreviousSHA256 != prev || !pubHex32.MatchString(en.RunID) {
			t.Errorf("%s: version %d task %q sequence %d previous %q run %q; want 1 %q %d %q and 32 lowercase hex", name, en.Version, en.TaskID, en.Sequence, en.PreviousSHA256, en.RunID, e.taskID, i+1, prev)
		}
		at, err := time.Parse(time.RFC3339Nano, en.At)
		if err != nil || !strings.HasSuffix(en.At, "Z") || at.Location() != time.UTC {
			t.Errorf("%s at %q is not a UTC RFC 3339 time", name, en.At)
		}
		prev = pubSHA(data)
		out = append(out, en)
	}
	return out
}

// checkLastRun checks the last two entries against the run's Report and
// exit code (R19, section 7) and returns the started entry's request.
func (e *pubEnv) checkLastRun(entries []pubEntry, rep *pubReport, code int, msgPath string) *pubRequest {
	t := e.t
	t.Helper()
	if len(entries) < 2 {
		t.Fatalf("%d publication entries, want a started and a finished entry", len(entries))
	}
	st, fin := entries[len(entries)-2], entries[len(entries)-1]
	if st.Kind != "started" || fin.Kind != "finished" || st.RunID != fin.RunID {
		t.Fatalf("last entries are %s/%s runs %s/%s, want started then finished of one run", st.Kind, fin.Kind, st.RunID, fin.RunID)
	}
	if fin.Status != rep.Status || fin.Reason != rep.Reason || fin.ExitCode == nil || *fin.ExitCode != code {
		t.Errorf("finished entry status %q reason %q exit %v, want %q %q %d", fin.Status, fin.Reason, fin.ExitCode, rep.Status, rep.Reason, code)
	}
	if fin.Steps == nil || *fin.Steps != rep.Steps {
		t.Errorf("finished entry steps %+v, want the Report's %+v", fin.Steps, rep.Steps)
	}
	if !filepath.IsAbs(rep.Record) || !pubSamePath(rep.Record, fin.path) {
		t.Errorf("report record %q, want the absolute path of %s", rep.Record, fin.path)
	}
	req := st.Request
	if req == nil {
		t.Fatalf("started entry has no request")
	}
	if req.SourceSHA256 != rep.SourceSHA256 || req.ExpectHead != rep.ExpectHead || req.Branch != rep.Branch || req.Remote != rep.Remote || req.ExpectRemote != rep.ExpectRemote {
		t.Errorf("started request %+v disagrees with the Report", req)
	}
	pubWantStrings(t, "request.paths", req.Paths, rep.Paths)
	pubWantStrings(t, "request.committed_paths", req.CommittedPaths, rep.CommittedPaths)
	if req.MessageSHA256 != e.fileSHA(msgPath) {
		t.Errorf("request.message_sha256 = %q, want the SHA-256 of the message file", req.MessageSHA256)
	}
	return req
}

// wantRequestCommit checks that c is "this request's commit" (section 2):
// one parent, the message bytes, a tree that differs from the parent's at
// exactly the expected paths, each with its verified mode and bytes.
func (e *pubEnv) wantRequestCommit(c, parent, msg string, files map[string]*pubFile) {
	t := e.t
	t.Helper()
	raw := e.git("cat-file", "commit", c)
	header, message, ok := strings.Cut(raw, "\n\n")
	if !ok {
		t.Fatalf("commit %s has no message:\n%s", c, raw)
	}
	var parents []string
	for _, line := range strings.Split(header, "\n") {
		if p, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, p)
		}
	}
	pubWantStrings(t, "parents of "+c, parents, []string{parent})
	if message != msg {
		t.Errorf("commit message = %q, want the message file's bytes %q", message, msg)
	}
	changed := pubSplitNUL(e.git("diff-tree", "-r", "-z", "--no-renames", "--name-only", parent, c))
	sort.Strings(changed)
	pubWantStrings(t, "paths changed by "+c, changed, slices.Sorted(func(yield func(string) bool) {
		for k := range files {
			if !yield(k) {
				return
			}
		}
	}))
	for path, f := range files {
		out := e.git("ls-tree", "-z", c, "--", path)
		if f == nil {
			if out != "" {
				t.Errorf("%s still holds deleted %s: %q", c, path, out)
			}
			continue
		}
		meta, _, _ := strings.Cut(strings.TrimSuffix(out, "\x00"), "\t")
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			t.Errorf("%s has no entry for %s: %q", c, path, out)
			continue
		}
		if fields[0] != f.mode {
			t.Errorf("%s:%s mode %s, want %s", c, path, fields[0], f.mode)
		}
		if got := e.git("cat-file", "blob", fields[2]); got != f.data {
			t.Errorf("%s:%s bytes %q, want %q", c, path, got, f.data)
		}
	}
}

// ---------------------------------------------------------------------------
// State snapshots: a refusal or denial before the started entry changes
// neither the repository, the remote, the PR nor the task directory (I5).

type pubSnap struct {
	head, refs, index, status, remote, gh string
	tree, task                            map[string]string
}

func pubTree(t *testing.T, dir string, skipGit bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	if dir == "" {
		return out
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if skipGit && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			out[rel] = "dir " + info.Mode().Perm().String()
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link " + target
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				out[rel] = "unreadable " + info.Mode().String()
				return nil
			}
			out[rel] = info.Mode().String() + " " + pubSHA(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *pubEnv) snap() pubSnap {
	e.t.Helper()
	sym, _ := e.gitRun(e.repo, "", "symbolic-ref", "-q", "HEAD")
	gh, _ := os.ReadFile(e.ghState)
	return pubSnap{
		head:   strings.TrimSpace(sym) + " " + e.rev("HEAD"),
		refs:   e.git("for-each-ref", "--format=%(refname) %(objectname)"),
		index:  e.git("ls-files", "-s", "-z"),
		status: e.git("--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all"),
		remote: e.refsOf(e.remote),
		gh:     string(gh),
		tree:   pubTree(e.t, e.repo, true),
		task:   pubTree(e.t, e.task, false),
	}
}

func pubDiffMaps(a, b map[string]string) []string {
	var diff []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			diff = append(diff, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return diff
}

func (e *pubEnv) mustUnchanged(label string, before pubSnap) {
	e.t.Helper()
	after := e.snap()
	for _, f := range []struct{ name, a, b string }{
		{"HEAD", before.head, after.head}, {"refs", before.refs, after.refs}, {"index", before.index, after.index},
		{"status", before.status, after.status}, {"remote refs", before.remote, after.remote}, {"gh state", before.gh, after.gh},
	} {
		if f.a != f.b {
			e.t.Errorf("%s changed %s:\nbefore %q\nafter  %q", label, f.name, f.a, f.b)
		}
	}
	if d := pubDiffMaps(before.tree, after.tree); len(d) > 0 {
		e.t.Errorf("%s changed worktree paths %q", label, d)
	}
	// The first run creates the empty lock file .publish-lock, which stays (R5).
	if _, had := before.task[".publish-lock"]; !had && after.task[".publish-lock"] == "-rw------- "+pubSHA(nil) {
		delete(after.task, ".publish-lock")
	}
	if d := pubDiffMaps(before.task, after.task); len(d) > 0 {
		e.t.Errorf("%s changed task directory entries %q", label, d)
	}
}

// refuse runs cw and requires the given non-zero outcome and no change at all.
func (e *pubEnv) refuse(label string, f pubFlags, code int, status, reason string, extraEnv ...string) *pubReport {
	e.t.Helper()
	before := e.snap()
	r := e.publishF(f, extraEnv...)
	rep := pubWant(e.t, r, code, status, reason)
	e.mustUnchanged(label, before)
	if r.git != (pubGitCount{}) {
		e.t.Errorf("%s: state-changing git commands ran: %+v", label, r.git)
	}
	if rep != nil && rep.Record != "" {
		e.t.Errorf("%s: record %q, want \"\" before the started entry", label, rep.Record)
	}
	return rep
}

// taskRecordFiles are the task directory's files other than publication
// files and the publish lock file (R21.a).
func (e *pubEnv) taskRecordFiles() map[string]string {
	e.t.Helper()
	all := pubTree(e.t, e.task, false)
	for k := range all {
		if strings.HasPrefix(k, "publication-") || k == ".publish-lock" {
			delete(all, k)
		}
	}
	return all
}

// ---------------------------------------------------------------------------
// Hooks, the fake gh and markers.

type pubHookConf struct {
	Log        string      `json:"log,omitempty"`
	Probe      []string    `json:"probe,omitempty"`
	Create     []string    `json:"create,omitempty"`
	Copy       [][2]string `json:"copy,omitempty"`
	Run        [][]string  `json:"run,omitempty"`
	RunHookEnv [][]string  `json:"run_hook_env,omitempty"`
	AppendArg1 string      `json:"append_arg1,omitempty"`
	Started    string      `json:"started,omitempty"`
	Release    string      `json:"release,omitempty"`
	Exit       int         `json:"exit"`
}

type pubCall struct {
	Role     string          `json:"role"`
	Args     []string        `json:"args"`
	Stdin    string          `json:"stdin"`
	Cwd      string          `json:"cwd"`
	Pid      int             `json:"pid"`
	Env      []string        `json:"env"`
	Probe    map[string]bool `json:"probe"`
	Result   int             `json:"result"`
	PubFiles []string        `json:"pubfiles"`
}

// hook installs the helper as hooksDir/role with conf and returns its log.
func (e *pubEnv) hook(hooksDir, role string, conf pubHookConf) string {
	e.t.Helper()
	if conf.Log == "" {
		conf.Log = filepath.Join(e.logs, role+".jsonl")
	}
	dst := filepath.Join(hooksDir, role)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	if _, err := os.Lstat(dst); err != nil {
		pubLink(e.t, e.tl.helper, dst)
	}
	pubWriteJSON(e.t, dst+".json", conf)
	return conf.Log
}

func pubCalls(t *testing.T, logPath string) []pubCall {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []pubCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var c pubCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("log %s line %q: %v", logPath, line, err)
		}
		calls = append(calls, c)
	}
	return calls
}

func pubEnvValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// blocker returns a started marker and a release file for a blocking hook
// or gh call. Cleanup releases it and waits for the blocked process to exit.
func (e *pubEnv) blocker(name string) (string, string) {
	started := filepath.Join(e.logs, name+".started")
	release := filepath.Join(e.logs, name+".release")
	e.t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o644)
		data, err := os.ReadFile(started)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
			time.Sleep(20 * time.Millisecond)
		}
		pubWaitGone(filepath.Join(e.gitDir, "index.lock"), 20*time.Second)
	})
	return started, release
}

// pubWaitMarker waits for a marker file that a hook or the fake gh creates
// while cw runs; it fails at once when cw exits first. The deadline bounds
// the wait only.
func pubWaitMarker(t *testing.T, proc *pubProc, p string) {
	t.Helper()
	deadline := time.Now().Add(pubDeadline)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(p); err == nil {
			return
		}
		select {
		case <-proc.done:
			if _, err := os.Stat(p); err == nil {
				return
			}
			r := proc.wait(t)
			t.Fatalf("cw %q exited %d before marker %s appeared\nstdout:\n%s\nstderr:\n%s", r.args, r.code, p, r.stdout, r.stderr)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("marker %s did not appear within %s", p, pubDeadline)
}

func pubWaitGone(p string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

type pubPR struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	State             string `json:"state"`
	HeadRefName       string `json:"headRefName"`
	HeadRefOid        string `json:"headRefOid"`
	BaseRefName       string `json:"baseRefName"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	IsCrossRepository bool   `json:"isCrossRepository"`
}

type pubGHState struct {
	Repo              string     `json:"repo"`
	RepoURL           string     `json:"repo_url"`
	SSHURL            string     `json:"ssh_url"`
	RemoteGitDir      string     `json:"remote_git_dir"`
	NextNumber        int        `json:"next_number"`
	PRs               []pubPR    `json:"prs"`
	RepoMode          string     `json:"repo_mode"`
	ListMode          string     `json:"list_mode"`
	CreateMode        string     `json:"create_mode"`
	EditMode          string     `json:"edit_mode"`
	ViewMode          string     `json:"view_mode"`
	CreateTitleSuffix string     `json:"create_title_suffix"`
	BodyLF            bool       `json:"body_lf"`
	BodyPrefix        string     `json:"body_prefix"`
	BodySuffix        string     `json:"body_suffix"`
	ViewSetsState     string     `json:"view_sets_state"`
	HideNewLists      int        `json:"hide_new_lists"`
	HiddenNumber      int        `json:"hidden_number"`
	StaleViews        int        `json:"stale_views"`
	OnList            [][]string `json:"on_list"`
	Started           string     `json:"started"`
	Release           string     `json:"release"`
}

func (e *pubEnv) saveGH(s pubGHState) {
	e.t.Helper()
	if s.PRs == nil {
		s.PRs = []pubPR{}
	}
	pubWriteJSON(e.t, e.ghState, s)
}

func (e *pubEnv) loadGH() pubGHState {
	e.t.Helper()
	data, err := os.ReadFile(e.ghState)
	if err != nil {
		e.t.Fatal(err)
	}
	var s pubGHState
	if err := json.Unmarshal(data, &s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *pubEnv) setGH(fn func(*pubGHState)) {
	e.t.Helper()
	s := e.loadGH()
	fn(&s)
	e.saveGH(s)
}

func pubOpenPR(n int, title, base, body string, cross bool) pubPR {
	return pubPR{Number: n, URL: fmt.Sprintf("https://github.example/%s/pull/%d", pubRepo, n), State: "OPEN", HeadRefName: pubBranch, BaseRefName: base, Title: title, Body: body, IsCrossRepository: cross}
}

func pubGHRepoView() []string { return []string{"repo", "view", pubRepo, "--json=url,sshUrl"} }

func pubGHList() []string {
	return []string{"pr", "list", "--repo=" + pubRepo, "--head=" + pubBranch, "--state=open", "--limit=100", "--json=" + pubPRFields}
}

func pubGHCreate(title string) []string {
	return []string{"pr", "create", "--repo=" + pubRepo, "--base=" + pubBase, "--head=" + pubBranch, "--title=" + title, "--body-file=-"}
}

func pubGHEdit(n int, title string) []string {
	return []string{"pr", "edit", strconv.Itoa(n), "--repo=" + pubRepo, "--base=" + pubBase, "--title=" + title, "--body-file=-"}
}

func pubGHView(n int) []string {
	return []string{"pr", "view", strconv.Itoa(n), "--repo=" + pubRepo, "--json=" + pubPRFields}
}

type pubGHCount struct{ repo, list, create, edit, view int }

// ghCheck checks every recorded gh call against section 8 and R22: an exact
// argument vector, stdin holding the body file's bytes for create and edit
// and empty otherwise, the worktree as working directory, GH_PROMPT_DISABLED=1
// and pass-through of other variables. It returns the counts per use.
func (e *pubEnv) ghCheck(worktree, title string) pubGHCount {
	t := e.t
	t.Helper()
	body, err := os.ReadFile(e.body)
	if err != nil {
		t.Fatal(err)
	}
	var n pubGHCount
	for _, c := range pubCalls(t, e.ghLog) {
		wantStdin := ""
		switch {
		case slices.Equal(c.Args, pubGHRepoView()):
			n.repo++
		case slices.Equal(c.Args, pubGHList()):
			n.list++
		case slices.Equal(c.Args, pubGHCreate(title)):
			n.create++
			wantStdin = string(body)
		case len(c.Args) == 7 && c.Args[1] == "edit":
			num, _ := strconv.Atoi(c.Args[2])
			if !slices.Equal(c.Args, pubGHEdit(num, title)) {
				t.Errorf("gh argv %q, want %q", c.Args, pubGHEdit(num, title))
			}
			n.edit++
			wantStdin = string(body)
		case len(c.Args) == 5 && c.Args[1] == "view":
			num, _ := strconv.Atoi(c.Args[2])
			if !slices.Equal(c.Args, pubGHView(num)) {
				t.Errorf("gh argv %q, want %q", c.Args, pubGHView(num))
			}
			n.view++
		default:
			t.Errorf("gh run with argv %q, not one of section 8", c.Args)
		}
		if c.Stdin != wantStdin {
			t.Errorf("gh %q stdin %q, want %q", c.Args, c.Stdin, wantStdin)
		}
		if !pubSamePath(c.Cwd, worktree) {
			t.Errorf("gh %q ran in %q, want the worktree %q", c.Args, c.Cwd, worktree)
		}
		if v, _ := pubEnvValue(c.Env, "GH_PROMPT_DISABLED"); v != "1" {
			t.Errorf("gh %q GH_PROMPT_DISABLED=%q, want 1", c.Args, v)
		}
		if v, _ := pubEnvValue(c.Env, "PUBE2E_PASSTHROUGH"); v != "kept" {
			t.Errorf("gh %q did not receive cw's environment (PUBE2E_PASSTHROUGH=%q)", c.Args, v)
		}
	}
	return n
}

// cloneAndPush makes a commit on top of parent in a separate clone and
// pushes it to the remote branch; the commit is unknown to the repository.
func (e *pubEnv) cloneAndPush(parent, branch string) string {
	e.t.Helper()
	clone := filepath.Join(e.root, fmt.Sprintf("other clone %d", e.next()))
	e.gitIn(e.root, "clone", "-q", "--no-checkout", e.remote, clone)
	e.gitIn(clone, "checkout", "-q", "--detach", parent)
	pubWriteFile(e.t, filepath.Join(clone, "elsewhere.txt"), []byte("pushed elsewhere\n"), 0o644)
	e.gitIn(clone, "add", "elsewhere.txt")
	e.gitIn(clone, "commit", "-q", "-m", "elsewhere")
	e.gitIn(clone, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return strings.TrimSpace(e.gitIn(clone, "rev-parse", "HEAD"))
}

// pubGitCount counts a run's state-changing git commands; pushRefspec is
// the refspec of its push.
type pubGitCount struct {
	add, commit, push int
	pushRefspec       string
}

// pubFlagValue returns the last value of a flag in argv (`--f v` or `--f=v`).
func pubFlagValue(argv []string, name string) string {
	v := ""
	for i, a := range argv {
		if a == name && i+1 < len(argv) {
			v = argv[i+1]
		} else if after, ok := strings.CutPrefix(a, name+"="); ok {
			v = after
		}
	}
	return v
}

// pubGitValueOptions are git's global options that take their value as the
// next argument.
var pubGitValueOptions = []string{"-c", "-C", "--git-dir", "--work-tree", "--namespace", "--config-env", "--super-prefix", "--attr-source"}

// pubGitSub splits a git argv into its subcommand and arguments, skipping
// global options before the subcommand, with their separate values.
func pubGitSub(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if slices.Contains(pubGitValueOptions, a) {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a, args[i+1:]
	}
	return "", nil
}

// pubConfigForbidden returns why a `git config` argument list is not a plain
// read (a write, or a read as a boolean), or "" for a read.
func pubConfigForbidden(rest []string) string {
	valueOptions := []string{"--type", "--file", "-f", "--blob", "--default", "--comment", "--value"}
	read := false
	var positional []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--type" && i+1 < len(rest) && rest[i+1] == "bool", a == "--type=bool", a == "--bool", a == "--bool-or-int", a == "--type=bool-or-int":
			return "boolean type"
		case slices.Contains([]string{"--add", "--unset", "--unset-all", "--replace-all", "--remove-section", "--rename-section", "--edit", "-e"}, a):
			return "write option " + a
		case slices.Contains(valueOptions, a):
			i++
		case slices.Contains([]string{"--get", "--get-all", "--get-regexp", "--get-urlmatch", "--get-color", "--get-colorbool", "-l", "--list"}, a):
			read = true
		case strings.HasPrefix(a, "-"):
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) > 0 && !read {
		switch positional[0] {
		case "set", "unset", "rename-section", "remove-section", "edit":
			return "write subcommand " + positional[0]
		case "get", "list":
			return ""
		}
		if len(positional) >= 2 {
			return "write of " + positional[0]
		}
	}
	return ""
}

var pubRepoVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM"}

// checkGit checks every git command a publish run made through the git
// wrapper against section 8, section 9 (I3) and R17/R22: each state-changing
// command at most once, git add only with --intent-to-add, git commit
// without --amend or --no-verify, git push with exactly the argv of section 8
// and so no force, tags or deletion, the exact remote and ls-remote argv, no
// git config --type=bool or write, no command that rewrites refs; R22 for
// every git command of the publication steps (add, commit, push, ls-remote,
// remote, config) and for any other command that receives cw's environment
// (one that keeps GIT_CONFIG_GLOBAL; the source capture of O3 runs with a
// minimal environment): cw's environment passed through, the worktree as
// working directory, GIT_TERMINAL_PROMPT=0 and none of the
// repository-selecting variables; and R19: the run's started entry exists
// before each state-changing git command.
func (e *pubEnv) checkGit(r pubRun) pubGitCount {
	t := e.t
	t.Helper()
	remote, branch := pubFlagValue(r.args, "--push"), pubFlagValue(r.args, "--branch")
	var n pubGitCount
	for _, c := range pubCalls(t, r.gitLog) {
		sub, rest := pubGitSub(c.Args)
		step := slices.Contains([]string{"add", "commit", "push", "ls-remote", "remote", "config"}, sub)
		global, _ := pubEnvValue(c.Env, "GIT_CONFIG_GLOBAL")
		if step && global != e.gitconfig {
			t.Errorf("git %q ran without cw's environment (GIT_CONFIG_GLOBAL=%q, want %q) (R22)", c.Args, global, e.gitconfig)
		}
		if (sub == "add" || sub == "commit" || sub == "push") && r.pubBefore >= 0 && len(c.PubFiles) != r.pubBefore+1 {
			t.Errorf("git %q ran with publication files %q, want the run's started entry after the %d earlier files (R19)", c.Args, c.PubFiles, r.pubBefore)
		}
		if step || global == e.gitconfig {
			if !pubSamePath(c.Cwd, e.worktree) {
				t.Errorf("git %q ran in %q, want the worktree %q (R22)", c.Args, c.Cwd, e.worktree)
			}
			if v, _ := pubEnvValue(c.Env, "GIT_TERMINAL_PROMPT"); v != "0" {
				t.Errorf("git %q ran with GIT_TERMINAL_PROMPT=%q, want 0 (R22.b)", c.Args, v)
			}
			for _, k := range pubRepoVars {
				if v, ok := pubEnvValue(c.Env, k); ok {
					t.Errorf("git %q received %s=%q (R22.a)", c.Args, k, v)
				}
			}
		}
		switch sub {
		case "add":
			n.add++
			if !slices.Contains(rest, "--intent-to-add") && !slices.Contains(rest, "-N") {
				t.Errorf("git %q stages content; only git add --intent-to-add may run", c.Args)
			}
		case "commit":
			n.commit++
			for _, a := range rest {
				if a == "--amend" || a == "--no-verify" || a == "-n" {
					t.Errorf("git %q: %s is forbidden (section 8)", c.Args, a)
				}
			}
		case "push":
			n.push++
			if len(rest) == 4 {
				n.pushRefspec = rest[3]
			}
			ref, _, _ := strings.Cut(n.pushRefspec, ":")
			if len(rest) != 4 || rest[0] != "--no-follow-tags" || rest[1] != "--end-of-options" || rest[2] != remote ||
				!(pubHex40.MatchString(ref) || pubHex64.MatchString(ref)) || n.pushRefspec != ref+":refs/heads/"+branch {
				t.Errorf("git %q, want push --no-follow-tags --end-of-options %s C:refs/heads/%s (section 8)", c.Args, remote, branch)
			}
		case "config":
			if why := pubConfigForbidden(rest); why != "" {
				t.Errorf("git %q: %s; cw only reads configuration, never as a boolean (section 8)", c.Args, why)
			}
		case "remote":
			if len(rest) == 0 {
				break
			}
			switch rest[0] {
			case "get-url":
				if !slices.Equal(rest, []string{"get-url", "--all", "--end-of-options", remote}) && !slices.Equal(rest, []string{"get-url", "--push", "--all", "--end-of-options", remote}) {
					t.Errorf("git %q, want remote get-url [--push] --all --end-of-options %s (section 8)", c.Args, remote)
				}
			case "add", "remove", "rm", "rename", "set-url", "set-head", "set-branches", "prune", "update":
				t.Errorf("git %q changes a remote", c.Args)
			}
		case "ls-remote":
			if !slices.Equal(rest, []string{"--end-of-options", remote, "refs/heads/" + branch}) {
				t.Errorf("git %q, want ls-remote --end-of-options %s refs/heads/%s (section 8)", c.Args, remote, branch)
			}
		case "reset", "rebase", "update-ref", "stash", "restore", "checkout", "switch", "fetch", "pull", "merge", "cherry-pick",
			"revert", "am", "gc", "prune", "replace", "notes", "rm", "mv", "filter-branch", "reflog", "pack-refs":
			t.Errorf("git %q is not a command cw publish may run (sections 2, 8 and I3)", c.Args)
		}
	}
	if n.add > 1 || n.commit > 1 || n.push > 1 {
		t.Errorf("state-changing git commands ran more than once: %+v (R17)", n)
	}
	return n
}

// ---------------------------------------------------------------------------
// Test cases.

// pubTestHappyPath: R13.a (one commit that is this request's commit, hooks
// run), R13.e, R15.b (push done, pre-push runs, no tags with
// push.followTags), R2 (one stdout line, empty stderr), R4 (--pr none never
// runs gh although present), R5 (lock held during the run, released after),
// R19 (started entry before git commit), R20.d (none), R21.a, R21.c and R22
// (worktree as working directory, GIT_TERMINAL_PROMPT=0, pass-through).
func pubTestHappyPath(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.git("config", "push.followTags", "true")
	e.git("tag", "-a", "-m", "release v1", "v1", e.base)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	preCommit := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Probe: []string{filepath.Join(e.task, "publication-000001.json"), filepath.Join(e.task, ".publish-lock")}})
	commitMsg := e.hook(filepath.Join(e.gitDir, "hooks"), "commit-msg", pubHookConf{})
	prePush := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{})
	show := e.cwOK("task", "show", "--task", e.task)
	records := e.taskRecordFiles()

	r := e.publish()
	rep := pubWant(t, r, 0, "published", "")
	c := rep.Steps.Commit.Commit
	if !pubHex40.MatchString(c) || c == e.base {
		t.Fatalf("commit step commit %q, want a new commit", c)
	}
	if want := (pubGitCount{add: 1, commit: 1, push: 1, pushRefspec: c + ":refs/heads/" + pubBranch}); r.git != want {
		t.Errorf("state-changing git commands %+v, want %+v", r.git, want)
	}
	if got := e.rev("HEAD"); got != c {
		t.Errorf("HEAD = %s, want the published commit %s", got, c)
	}
	if sym := strings.TrimSpace(e.git("symbolic-ref", "HEAD")); sym != "refs/heads/"+pubBranch {
		t.Errorf("HEAD is %s, want refs/heads/%s", sym, pubBranch)
	}
	e.wantRequestCommit(c, e.base, pubMessage, want)
	if got := e.remoteRef(e.remote, pubBranch); got != c {
		t.Errorf("remote %s = %s, want %s", pubBranch, got, c)
	}
	if tags := e.gitIn(e.root, "--git-dir="+e.remote, "for-each-ref", "refs/tags"); tags != "" {
		t.Errorf("remote holds tags %q; cw must not push tags", tags)
	}
	pubWantSteps(t, rep, "done", "done", "not-requested")
	if rep.Steps.Push.RemoteRef != c {
		t.Errorf("push remote_ref %q, want %s", rep.Steps.Push.RemoteRef, c)
	}
	if pr := rep.Steps.PR; pr.Number != 0 || pr.URL != "" || pr.HeadRefOID != "" || pr.Base != "" || pr.State != "" {
		t.Errorf("pr step %+v, want zero values when not requested", pr)
	}
	if !pubSamePath(rep.Task, e.task) || rep.TaskID != e.taskID || !pubSamePath(rep.Worktree, e.repo) {
		t.Errorf("task %q id %q worktree %q, want %q %q %q", rep.Task, rep.TaskID, rep.Worktree, e.task, e.taskID, e.repo)
	}
	if !pubHex64.MatchString(rep.SourceSHA256) {
		t.Errorf("source_sha256 %q is not 64 lowercase hex", rep.SourceSHA256)
	}
	pubWantStrings(t, "paths", rep.Paths, pubStandardD)
	pubWantStrings(t, "committed_paths", rep.CommittedPaths, pubStandardD)
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, pubStandardNew)
	pubWantStrings(t, "locks_left", rep.LocksLeft, nil)
	if len(rep.OpenRecords) != 0 {
		t.Errorf("open_records %+v, want []", rep.OpenRecords)
	}
	if rep.ExpectHead != e.base || rep.Branch != pubBranch || rep.Remote != "origin" || rep.ExpectRemote != "" || rep.PreviousOutcome != "none" {
		t.Errorf("expect_head %q branch %q remote %q expect_remote %q previous_outcome %q", rep.ExpectHead, rep.Branch, rep.Remote, rep.ExpectRemote, rep.PreviousOutcome)
	}
	entries := e.entries()
	if len(entries) != 2 {
		t.Fatalf("%d publication entries, want 2", len(entries))
	}
	req := e.checkLastRun(entries, rep, 0, e.msg)
	if req.PR != (pubPublicationPR{Mode: "none"}) {
		t.Errorf("request.pr = %+v, want mode none and empty fields", req.PR)
	}
	if !strings.HasSuffix(rep.Record, "publication-000002.json") {
		t.Errorf("record %q, want publication-000002.json", rep.Record)
	}
	calls := pubCalls(t, preCommit)
	if len(calls) != 1 {
		t.Fatalf("pre-commit ran %d times, want 1", len(calls))
	}
	if !calls[0].Probe[filepath.Join(e.task, "publication-000001.json")] {
		t.Errorf("the started entry did not exist when git commit ran its pre-commit hook (R19)")
	}
	if !calls[0].Probe[filepath.Join(e.task, ".publish-lock")] {
		t.Errorf(".publish-lock did not exist during the run (R5)")
	}
	if v, _ := pubEnvValue(calls[0].Env, "GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("git ran with GIT_TERMINAL_PROMPT=%q, want 0 (R22.b)", v)
	}
	if v, _ := pubEnvValue(calls[0].Env, "PUBE2E_PASSTHROUGH"); v != "kept" {
		t.Errorf("git did not receive cw's environment (R22)")
	}
	if !pubSamePath(calls[0].Cwd, e.repo) {
		t.Errorf("git commit ran in %q, want the worktree %q", calls[0].Cwd, e.repo)
	}
	if n := len(pubCalls(t, commitMsg)); n != 1 {
		t.Errorf("commit-msg ran %d times, want 1", n)
	}
	if pp := pubCalls(t, prePush); len(pp) != 1 || len(pp[0].Args) == 0 || pp[0].Args[0] != "origin" {
		t.Errorf("pre-push calls %+v, want one for origin", pp)
	} else {
		// R22 for git push, seen from the hook it runs.
		if v, _ := pubEnvValue(pp[0].Env, "GIT_TERMINAL_PROMPT"); v != "0" {
			t.Errorf("git push ran with GIT_TERMINAL_PROMPT=%q, want 0 (R22.b)", v)
		}
		if v, _ := pubEnvValue(pp[0].Env, "PUBE2E_PASSTHROUGH"); v != "kept" {
			t.Errorf("git push did not receive cw's environment (R22)")
		}
		if v, _ := pubEnvValue(pp[0].Env, "GIT_CONFIG_GLOBAL"); v != e.gitconfig {
			t.Errorf("git push ran with GIT_CONFIG_GLOBAL=%q, want cw's %q (R22)", v, e.gitconfig)
		}
		if !pubSamePath(pp[0].Cwd, e.repo) {
			t.Errorf("git push ran its pre-push hook in %q, want the worktree %q", pp[0].Cwd, e.repo)
		}
	}
	if calls := pubCalls(t, e.ghLog); len(calls) != 0 {
		t.Errorf("gh ran %d times with --pr none, want never (R4)", len(calls))
	}
	// The lock file stays in the task directory; no process holds it.
	if lock, err := os.Open(filepath.Join(e.task, ".publish-lock")); err != nil {
		t.Errorf(".publish-lock after the run: %v", err)
	} else {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Errorf(".publish-lock still held after the run: %v", err)
		}
		lock.Close()
	}
	if st := e.git("status", "--porcelain"); st != "" {
		t.Errorf("worktree or index differ from the commit after publishing:\n%s", st)
	}
	if d := pubDiffMaps(records, e.taskRecordFiles()); len(d) > 0 {
		t.Errorf("task record files changed: %q (R21.a)", d)
	}
	if got := e.cwOK("task", "show", "--task", e.task); got != show {
		t.Errorf("cw task show changed after publishing (R21.a)")
	}
	e.cwOK("task", "abandon", "--task", e.task, "--reason", "published elsewhere") // R21.c
}

// pubTestMessageVerbatim: R13.a requires the commit message bytes to equal
// the message file's bytes, so blank-line runs, '#' lines and trailing
// spaces survive.
func pubTestMessageVerbatim(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.write("a.txt", "line one\nline two\nverbatim\n")
	e.verifyTask()
	msg := "Subject line\n\n\n# not a comment\ntrailing spaces   \n\n"
	pubWriteFile(t, e.msg, []byte(msg), 0o644)
	rep := pubWant(t, e.publish(), 0, "published", "")
	e.wantRequestCommit(rep.Steps.Commit.Commit, e.base, msg, map[string]*pubFile{"a.txt": {"100644", "line one\nline two\nverbatim\n"}})
}

// pubTestUnrelatedWork: R13.b keeps index entries and files outside D, R6.d
// staging after verification keeps the gate open, R8 without --path A is CS.
func pubTestUnrelatedWork(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.write("u.txt", "unrelated staged\n")
	e.git("add", "u.txt")
	e.write("w.txt", "worktree dirty\n")
	e.write("notes.txt", "untracked before start\n")
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	e.git("add", "a.txt", "new.txt") // R6.d: staging does not change the source digest
	stagedU := e.rev(":u.txt")
	rep := pubWant(t, e.publish(), 0, "published", "")
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, want)
	pubWantStrings(t, "paths", rep.Paths, pubStandardD)
	if got := e.rev(":u.txt"); got != stagedU {
		t.Errorf("staged u.txt blob %s, want %s kept staged", got, stagedU)
	}
	if got := e.rev(c + ":u.txt"); got != e.rev(e.base+":u.txt") {
		t.Errorf("the commit took the unrelated staged u.txt")
	}
	cached := pubSplitNUL(e.git("diff", "--cached", "--name-only", "-z"))
	pubWantStrings(t, "staged paths after publishing", cached, []string{"u.txt"})
	if got := e.rev(":w.txt"); got != e.rev(e.base+":w.txt") {
		t.Errorf("index entry of w.txt changed")
	}
	for name, content := range map[string]string{"w.txt": "worktree dirty\n", "notes.txt": "untracked before start\n", "u.txt": "unrelated staged\n"} {
		if data, err := os.ReadFile(filepath.Join(e.repo, name)); err != nil || string(data) != content {
			t.Errorf("%s = %q (%v), want %q untouched", name, data, err, content)
		}
	}
	if slices.Contains(e.lsFiles(), "notes.txt") {
		t.Errorf("untracked notes.txt was added to the index")
	}
	for _, p := range []string{"a.txt", "new.txt", "tool"} {
		if e.rev(":"+p) != e.rev(c+":"+p) {
			t.Errorf("index entry of %s differs from the commit's tree", p)
		}
	}
	if out := e.git("ls-files", "-s", "--", "gone.txt"); out != "" {
		t.Errorf("index still lists deleted gone.txt: %q", out)
	}
}

// pubTestPathSubset: R8 (A is the --path values), R13.a with a subset, and
// R4 (no gh on PATH is fine with --pr none).
func pubTestPathSubset(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.gh = false
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	rep := pubWant(t, e.publish("--path", "new.txt", "--path", "a.txt"), 0, "published", "")
	pubWantStrings(t, "paths", rep.Paths, []string{"a.txt", "new.txt"})
	pubWantStrings(t, "committed_paths", rep.CommittedPaths, []string{"a.txt", "new.txt"})
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, []string{"new.txt"})
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, map[string]*pubFile{"a.txt": {"100644", "line one\nline two\nline three\n"}, "new.txt": {"100644", "new file\n"}})
	if e.rev(c+":gone.txt") == "" {
		t.Errorf("gone.txt, outside A, was removed from the commit")
	}
	if _, err := os.Lstat(filepath.Join(e.repo, "tool")); err != nil || slices.Contains(e.lsFiles(), "tool") {
		t.Errorf("tool, outside A, must stay an untracked file (%v)", err)
	}
	if e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("remote not at %s", c)
	}
}

// pubTestNothingToPublish: R8, CS empty.
func pubTestNothingToPublish(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.verifyTask()
	e.refuse("empty change set", e.flags(), 4, "refused", "nothing-to-publish")
}

// pubTestPathNotVerified: R8, a --path outside CS (unchanged path, directory,
// glob, pathspec, other spelling) is refused; a path inside CS is accepted.
func pubTestPathNotVerified(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.write("sub/s.txt", "sub changed\n")
	e.verifyTask()
	for _, p := range []string{"u.txt", "sub", "sub/", "sub/*", "*.txt", "./a.txt", ":(top)a.txt", "A.TXT", "a..txt", "missing.txt"} {
		e.refuse("--path "+p, e.flags().add("--path", p), 4, "refused", "path-not-verified")
	}
	e.refuse("one path outside", e.flags().add("--path", "a.txt", "--path", "u.txt"), 4, "refused", "path-not-verified")
	rep := e.refuse("path inside CS", e.flags().add("--path", "sub/s.txt").set("--push", "nosuch"), 4, "refused", "remote-unknown")
	pubWantStrings(t, "paths", rep.Paths, []string{"sub/s.txt"})
}

// pubTestSourceChanged: R6.c, bytes, mode, added or removed eligible files
// after verification deny; an ignored file does not; restoring the bytes
// passes the gate again.
func pubTestSourceChanged(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	p := func(rel string) string { return filepath.Join(e.repo, rel) }
	steps := []struct {
		name         string
		change, undo func()
	}{
		{"bytes of a member of CS", func() { e.write("a.txt", "line one\nline two\nline three\nlater\n") }, func() { e.write("a.txt", "line one\nline two\nline three\n") }},
		{"mode of a member of CS", func() { _ = os.Chmod(p("a.txt"), 0o755) }, func() { _ = os.Chmod(p("a.txt"), 0o644) }},
		{"mode of a path outside CS", func() { _ = os.Chmod(p("b.txt"), 0o755) }, func() { _ = os.Chmod(p("b.txt"), 0o644) }},
		{"new untracked file", func() { e.write("extra.txt", "extra\n") }, func() { e.remove("extra.txt") }},
		{"removed new file", func() { e.remove("new.txt") }, func() { e.write("new.txt", "new file\n") }},
	}
	for _, s := range steps {
		s.change()
		e.refuse(s.name, e.flags(), 3, "denied", "source-changed")
		s.undo()
	}
	pubWriteFile(t, filepath.Join(e.gitDir, "info", "exclude"), []byte("*.log\n"), 0o644)
	e.write("debug.log", "ignored\n")
	e.refuse("ignored file", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	pubWant(t, e.publish(), 0, "published", "")
}

// pubTestCaptureFailed: R6.c, a capture that fails (an eligible file that
// cannot be read) exits 1 capture-failed.
func pubTestCaptureFailed(t *testing.T, tl *pubTools) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	b := filepath.Join(e.repo, "b.txt")
	if err := os.Chmod(b, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(b, 0o644) })
	e.refuse("unreadable b.txt", e.flags(), 1, "failed", "capture-failed")
}

// pubTestGateNotVerified: R6.b (no or incomplete verification) and R6.a
// (abandoned, which precedes task-not-verified even when verified).
func pubTestGateNotVerified(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.refuse("no verification", e.flags(), 3, "denied", "task-not-verified")
	if code, complete, _ := e.verifySummary(e.task); code == 0 || complete {
		t.Fatalf("verify before assessment: exit %d complete %v, want an incomplete verification", code, complete)
	}
	e.refuse("incomplete verification", e.flags(), 3, "denied", "task-not-verified")
	e.verifyTask()
	e.cwOK("task", "abandon", "--task", e.task, "--reason", "stopped")
	e.refuse("abandoned", e.flags(), 3, "denied", "task-abandoned")
	// R3/R6.a: an abandoned task that was never verified is task-abandoned,
	// not task-not-verified.
	unverified, _ := e.startOther(pubPlanSmall, e.repo, "")
	e.cwOK("task", "abandon", "--task", unverified, "--reason", "never verified")
	e.refuse("abandoned and unverified", e.flags().set("--task", unverified), 3, "denied", "task-abandoned")
}

// pubTestOpenRecords: R7.d (a small record and a record for another
// repository do not deny), R7.a (an unverified substantial record for the
// repository denies, listed in open_records), R7.b (abandoned no longer).
func pubTestOpenRecords(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.startOther(pubPlanOtherSmall, e.repo, "")
	e.refuse("small record", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	elsewhere := filepath.Join(e.root, "elsewhere repo")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	e.gitIn(elsewhere, "init", "-q")
	pubWriteFile(t, filepath.Join(elsewhere, "a.txt"), []byte("line one\nline two\n"), 0o644)
	e.gitIn(elsewhere, "add", "a.txt")
	e.gitIn(elsewhere, "commit", "-q", "-m", "elsewhere")
	e.startOther(pubPlanOther, elsewhere, "")
	e.refuse("record for another repository", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	otherDir, otherID := e.startOther(pubPlanOther, e.repo, "")
	rep := e.refuse("open substantial record", e.flags(), 3, "denied", "open-record")
	if len(rep.OpenRecords) != 1 {
		t.Fatalf("open_records %+v, want the one open record", rep.OpenRecords)
	}
	if o := rep.OpenRecords[0]; o.TaskID != otherID || o.Title != "Other open work" || !pubSamePath(o.Directory, otherDir) || o.Problem == "" {
		t.Errorf("open record %+v, want task %s title %q directory %s and a problem", o, otherID, "Other open work", otherDir)
	}
	e.cwOK("task", "abandon", "--task", otherDir, "--reason", "not needed")
	pubWant(t, e.publish(), 0, "published", "")
}

// pubToV2096Shape rewrites every revision file of the task at directory into
// the shape cw v2.0.96 writes (no packs, no test_rules, no verification
// source_sha256), chained again by SHA-256, and requires cw task show to
// refuse it as a record this cw cannot read.
func (e *pubEnv) pubToV2096Shape(directory string) {
	e.t.Helper()
	names, err := filepath.Glob(filepath.Join(directory, "record-*.json"))
	if err != nil || len(names) == 0 {
		e.t.Fatalf("no revision files in %s: %v", directory, err)
	}
	sort.Strings(names)
	previous := ""
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			e.t.Fatal(err)
		}
		members := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &members); err != nil {
			e.t.Fatal(err)
		}
		delete(members, "packs")
		delete(members, "test_rules")
		if raw, ok := members["verification"]; ok {
			verification := map[string]json.RawMessage{}
			if err := json.Unmarshal(raw, &verification); err != nil {
				e.t.Fatal(err)
			}
			delete(verification, "source_sha256")
			if members["verification"], err = json.Marshal(verification); err != nil {
				e.t.Fatal(err)
			}
		}
		if members["previous_sha256"], err = json.Marshal(previous); err != nil {
			e.t.Fatal(err)
		}
		if data, err = json.Marshal(members); err != nil {
			e.t.Fatal(err)
		}
		pubWriteFile(e.t, name, data, 0o600)
		sum := sha256.Sum256(data)
		previous = hex.EncodeToString(sum[:])
	}
	if r := e.cw(nil, "task", "show", "--task", directory); r.code == 0 || !strings.Contains(r.stderr, "start a new task") {
		e.t.Fatalf("cw task show of the v2.0.96-shaped record: exit %d, stderr %q; want the legacy-record refusal", r.code, r.stderr)
	}
}

// pubTestOpenLegacyRecord: R7 for a record cw v2.0.66 to v2.0.96 wrote. This
// cw refuses to load it, yet an open substantial one still denies the
// publication (exit 3, open-record), names its directory and the way out,
// and moving it out of the store lets the publication through.
func pubTestOpenLegacyRecord(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	otherDir, otherID := e.startOther(pubPlanOther, e.repo, "")
	e.pubToV2096Shape(otherDir)
	rep := e.refuse("open record from cw v2.0.96", e.flags(), 3, "denied", "open-record")
	if len(rep.OpenRecords) != 1 {
		t.Fatalf("open_records %+v, want the one legacy record", rep.OpenRecords)
	}
	if o := rep.OpenRecords[0]; o.TaskID != otherID || o.Title != "Other open work" || !pubSamePath(o.Directory, otherDir) ||
		!strings.Contains(o.Problem, "out of the task store, then start a new task with `cw task start`") {
		t.Errorf("open record %+v, want task %s title %q directory %s and the move-it-out problem", o, otherID, "Other open work", otherDir)
	}
	if err := os.Rename(otherDir, filepath.Join(e.root, "moved v2 task")); err != nil {
		t.Fatal(err)
	}
	pubWant(t, e.publish(), 0, "published", "")
}

// pubTestOpenUnreadableRecord: R7 for an open record this cw cannot read
// (D4). A garbage newest revision used to hide it, and the publication went
// through; now publish refuses (exit 3, open-record), naming its directory
// and the load error, and moving it out of the store lets it through.
func pubTestOpenUnreadableRecord(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	otherDir, otherID := e.startOther(pubPlanOther, e.repo, "")
	pubWriteFile(t, filepath.Join(otherDir, "record-000002.json"), []byte("{\n"), 0o600)
	rep := e.refuse("open record that cannot be read", e.flags(), 3, "denied", "open-record")
	if len(rep.OpenRecords) != 1 {
		t.Fatalf("open_records %+v, want the one unreadable record", rep.OpenRecords)
	}
	if o := rep.OpenRecords[0]; o.TaskID != otherID || !pubSamePath(o.Directory, otherDir) || !strings.Contains(o.Problem, "this cw cannot read its record: invalid task JSON") {
		t.Errorf("open record %+v, want task %s directory %s and the load error", o, otherID, otherDir)
	}
	if err := os.Rename(otherDir, filepath.Join(e.root, "moved damaged task")); err != nil {
		t.Fatal(err)
	}
	pubWant(t, e.publish(), 0, "published", "")
}

// pubTestOpenRecordStores: R7.c, the hook store ($CW_TASK_STORE) and the
// task's store are both read, deduplicated.
func pubTestOpenRecordStores(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	hookStore := filepath.Join(e.root, "hook store")
	otherDir, otherID := e.startOther(pubPlanOther, e.repo, hookStore)
	rep := e.refuse("record in CW_TASK_STORE", e.flags(), 3, "denied", "open-record", "CW_TASK_STORE="+hookStore)
	if len(rep.OpenRecords) != 1 || rep.OpenRecords[0].TaskID != otherID {
		t.Errorf("open_records %+v, want %s", rep.OpenRecords, otherID)
	}
	e.refuse("hook store not selected", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	e.cwOK("task", "abandon", "--task", otherDir, "--reason", "not needed")
	_, secondID := e.startOther(pubPlanOther, e.repo, "")
	rep = e.refuse("record in the task's store", e.flags(), 3, "denied", "open-record", "CW_TASK_STORE="+hookStore)
	if len(rep.OpenRecords) != 1 || rep.OpenRecords[0].TaskID != secondID {
		t.Errorf("open_records %+v, want %s from the task's own store", rep.OpenRecords, secondID)
	}
	rep = e.refuse("same store twice", e.flags(), 3, "denied", "open-record", "CW_TASK_STORE="+filepath.Dir(e.task))
	if len(rep.OpenRecords) != 1 {
		t.Errorf("open_records %+v, want the record once (stores deduplicated)", rep.OpenRecords)
	}
}

// pubTestStoreUnreadable: R7.e.
func pubTestStoreUnreadable(t *testing.T, tl *pubTools) {
	if os.Geteuid() == 0 {
		t.Skip("root reads directories whatever their mode")
	}
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	store := filepath.Join(e.root, "locked store")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o700) })
	e.refuse("unreadable store", e.flags(), 1, "failed", "read-failed", "CW_TASK_STORE="+store)
}

// pubTestLinkedWorktree: R7 (a record for the main worktree gates a linked
// worktree of the same repository) and the Git lock files of section 2 for a
// linked worktree (the main worktree's index.lock is not one of them).
func pubTestLinkedWorktree(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	wt := filepath.Join(e.root, "linked wt")
	e.git("worktree", "add", "-q", "-b", "feature2", wt, e.base)
	e.task, e.taskID = e.startOther(pubPlanSmall, wt, "")
	e.worktree = wt
	pubWriteFile(t, filepath.Join(wt, "a.txt"), []byte("line one\nline two\nlinked\n"), 0o644)
	e.verifyTaskAt(e.task)
	otherDir, otherID := e.startOther(pubPlanOther, e.repo, "")
	f := e.flags().set("--branch", "feature2")
	rep := pubWant(t, e.publishF(f), 3, "denied", "open-record")
	if len(rep.OpenRecords) != 1 || rep.OpenRecords[0].TaskID != otherID {
		t.Errorf("open_records %+v, want the main worktree's record %s", rep.OpenRecords, otherID)
	}
	e.cwOK("task", "abandon", "--task", otherDir, "--reason", "not needed")
	mainLock := filepath.Join(e.gitDir, "index.lock")
	pubWriteFile(t, mainLock, nil, 0o644)
	rep = pubWant(t, e.publishF(f), 0, "published", "")
	if !pubSamePath(rep.Worktree, wt) {
		t.Errorf("worktree %q, want %q", rep.Worktree, wt)
	}
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, map[string]*pubFile{"a.txt": {"100644", "line one\nline two\nlinked\n"}})
	if e.remoteRef(e.remote, "feature2") != c {
		t.Errorf("remote feature2 not at %s", c)
	}
	if _, err := os.Lstat(mainLock); err != nil {
		t.Errorf("the main worktree's index.lock was removed: %v", err)
	}
}

// pubTestNotARepository: R9.a.
func pubTestNotARepository(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	plain := filepath.Join(e.root, "plain folder")
	pubWriteFile(t, filepath.Join(plain, "a.txt"), []byte("line one\nline two\n"), 0o644)
	e.task, e.taskID = e.startOther(pubPlanSmall, plain, "")
	e.worktree = plain
	pubWriteFile(t, filepath.Join(plain, "a.txt"), []byte("line one\nline two\nplain\n"), 0o644)
	e.verifyTaskAt(e.task)
	e.refuse("plain folder", e.flags(), 4, "refused", "not-a-repository")
}

// pubTestBranchHead: R9.b (other branch, detached HEAD) and R9.c (unknown or
// other --expect-head, HEAD moved, HEAD a commit that is not this request's).
func pubTestBranchHead(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	e.git("branch", "other")
	e.refuse("--branch other", e.flags().set("--branch", "other"), 4, "refused", "branch-mismatch")
	e.git("update-ref", "--no-deref", "HEAD", e.base)
	e.refuse("detached HEAD", e.flags(), 4, "refused", "branch-mismatch")
	e.git("symbolic-ref", "HEAD", "refs/heads/"+pubBranch)
	e.refuse("unknown 40-hex", e.flags().set("--expect-head", strings.Repeat("ab", 20)), 4, "refused", "head-moved")
	e.refuse("unknown 64-hex", e.flags().set("--expect-head", strings.Repeat("cd", 32)), 4, "refused", "head-moved")
	orphan := strings.TrimSpace(e.git("commit-tree", e.base+"^{tree}", "-m", "orphan"))
	e.refuse("other commit", e.flags().set("--expect-head", orphan), 4, "refused", "head-moved")
	moved := strings.TrimSpace(e.git("commit-tree", e.base+"^{tree}", "-p", e.base, "-m", "moved"))
	e.git("update-ref", "refs/heads/"+pubBranch, moved)
	e.refuse("HEAD moved", e.flags(), 4, "refused", "head-moved")
	e.git("update-ref", "refs/heads/"+pubBranch, e.base)
	rep := pubWant(t, e.publish(), 0, "published", "")
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, want)
	other := filepath.Join(e.work, "other-message.txt")
	pubWriteFile(t, other, []byte("A different message\n"), 0o644)
	e.refuse("HEAD is not this request's commit", e.flags().set("--message-file", other), 4, "refused", "head-moved")
	rep = pubWant(t, e.publish(), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
	if rep.Steps.Commit.Commit != c {
		t.Errorf("commit %s, want %s", rep.Steps.Commit.Commit, c)
	}
}

// pubTestHunkScope: R10.a, partial, stale or unmerged index entries of D are
// refused; whole-file staging, intent-to-add and staged deletions are not.
func pubTestHunkScope(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	nosuch := e.flags().set("--push", "nosuch")
	e.write("a.txt", "line one\nstaged part\n")
	e.git("add", "a.txt")
	e.write("a.txt", "line one\nline two\nline three\n")
	e.refuse("partially staged a.txt", e.flags(), 4, "refused", "hunk-scope")
	e.git("add", "a.txt")
	e.refuse("fully staged a.txt", nosuch, 4, "refused", "remote-unknown")
	e.write("new.txt", "stale\n")
	e.git("add", "new.txt")
	e.write("new.txt", "new file\n")
	e.refuse("stale staged new.txt", e.flags(), 4, "refused", "hunk-scope")
	e.git("rm", "-q", "-f", "--cached", "new.txt") // the staged blob differs from both the file and HEAD
	e.refuse("unstaged new.txt", nosuch, 4, "refused", "remote-unknown")
	e.git("add", "-N", "new.txt")
	e.refuse("intent-to-add new.txt", nosuch, 4, "refused", "remote-unknown")
	e.git("rm", "-q", "--cached", "gone.txt")
	e.refuse("staged deletion", nosuch, 4, "refused", "remote-unknown")
	blob := e.rev(":a.txt")
	e.git("update-index", "--force-remove", "a.txt")
	if _, err := e.gitRun(e.repo, fmt.Sprintf("100644 %s 1\ta.txt\n100644 %s 2\ta.txt\n100644 %s 3\ta.txt\n", blob, blob, blob), "update-index", "--index-info"); err != nil {
		t.Fatal(err)
	}
	e.refuse("unmerged a.txt", e.flags(), 4, "refused", "hunk-scope")
	e.git("add", "a.txt")
	rep := pubWant(t, e.publish(), 0, "published", "")
	e.wantRequestCommit(rep.Steps.Commit.Commit, e.base, pubMessage, want)
}

// pubTestConversionCR: R10.b for a commit-set file with CR bytes under
// core.autocrlf (Git's boolean rules, effective value across scopes) and the
// text, eol, crlf, filter, ident and working-tree-encoding attributes; then
// R10 applies only while HEAD equals --expect-head.
func pubTestConversionCR(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.write("a.txt", "line one\nline two\nline three\n")
	e.write("crlf.txt", "one\r\ntwo\r\n")
	e.verifyTask()
	localPath := filepath.Join(e.gitDir, "config")
	localBase, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	attrs := filepath.Join(e.gitDir, "info", "attributes")
	set := func(global, local, attr string) {
		pubWriteFile(t, e.gitconfig, []byte(pubGitconfig+global), 0o644)
		pubWriteFile(t, localPath, append(slices.Clone(localBase), local...), 0o644)
		pubWriteFile(t, attrs, []byte(attr), 0o644)
	}
	ac := func(v string) string { return "[core]\n\tautocrlf = " + v + "\n" }
	cases := []struct {
		name, global, local, attr string
		refused                   bool
	}{
		{"unset", "", "", "", false},
		{"true", "", ac("true"), "", true},
		{"yes", "", ac("yes"), "", true},
		{"on", "", ac("on"), "", true},
		{"TRUE", "", ac("TRUE"), "", true},
		{"1", "", ac("1"), "", true},
		{"-1", "", ac("-1"), "", true},
		{"0x10", "", ac("0x10"), "", true},
		{"2k", "", ac("2k"), "", true},
		{"1G", "", ac("1G"), "", true},
		{"input", "", ac("input"), "", true},
		{"INPUT", "", ac("INPUT"), "", true},
		{"Input", "", ac("Input"), "", true},
		{"valueless", "", "[core]\n\tautocrlf\n", "", true},
		{"false", "", ac("false"), "", false},
		{"no", "", ac("no"), "", false},
		{"off", "", ac("off"), "", false},
		{"OFF", "", ac("OFF"), "", false},
		{"0", "", ac("0"), "", false},
		{"0x0", "", ac("0x0"), "", false},
		{"0k", "", ac("0k"), "", false},
		{"empty", "", "[core]\n\tautocrlf =\n", "", false},
		{"last local entry false", "", "[core]\n\tautocrlf = true\n\tautocrlf = false\n", "", false},
		{"last local entry input", "", "[core]\n\tautocrlf = false\n\tautocrlf = input\n", "", true},
		{"global input, local false", ac("input"), ac("false"), "", false},
		{"global false, local input", ac("false"), ac("input"), "", true},
		{"global input only", ac("input"), "", "", true},
		{"attr text", "", "", "crlf.txt text\n", true},
		{"attr text=auto", "", "", "crlf.txt text=auto\n", true},
		{"attr eol=crlf", "", "", "crlf.txt eol=crlf\n", true},
		{"attr crlf", "", "", "crlf.txt crlf\n", true},
		{"attr -text", "", "", "crlf.txt -text\n", false},
		{"attr !text", "", "", "crlf.txt !text\n", false},
		{"attr text on a file without CR", "", "", "a.txt text\n", false},
		{"attr filter", "", "", "a.txt filter=pubfilter\n", true},
		{"attr ident", "", "", "a.txt ident\n", true},
		{"attr working-tree-encoding", "", "", "a.txt working-tree-encoding=ISO-8859-1\n", true},
		{"attr filter outside D", "", "", "u.txt filter=pubfilter\n", false},
	}
	for _, c := range cases {
		set(c.global, c.local, c.attr)
		if c.refused {
			e.refuse(c.name, e.flags(), 4, "refused", "content-conversion")
		} else {
			e.refuse(c.name, e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
		}
	}
	set("", "", "")
	rep := pubWant(t, e.publish(), 0, "published", "")
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, map[string]*pubFile{"a.txt": {"100644", "line one\nline two\nline three\n"}, "crlf.txt": {"100644", "one\r\ntwo\r\n"}})
	set("", ac("true"), "")
	rep = pubWant(t, e.publish(), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
}

// pubTestConversionMode: R10.b with core.fileMode off, for a new 100755 file
// and a mode change; a bytes-only change is not refused.
func pubTestConversionMode(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.git("config", "--unset-all", "core.filemode")
	e.startTask(pubPlanSmall)
	e.write("a.txt", "line one\nline two\nline three\n")
	e.writeMode("tool", "tool\n", 0o755)
	if err := os.Chmod(filepath.Join(e.repo, "b.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.verifyTask()
	localPath := filepath.Join(e.gitDir, "config")
	localBase, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	setLocal := func(extra string) {
		pubWriteFile(t, localPath, append(slices.Clone(localBase), extra...), 0o644)
	}
	fm := func(v string) string { return "[core]\n\tfilemode = " + v + "\n" }
	nosuch := e.flags().set("--push", "nosuch")
	for _, v := range []string{"false", "off", "no", "0", "FALSE"} {
		setLocal(fm(v))
		e.refuse("filemode "+v, e.flags(), 4, "refused", "content-conversion")
		e.refuse("filemode "+v+" new executable", e.flags().add("--path", "tool"), 4, "refused", "content-conversion")
		e.refuse("filemode "+v+" mode change", e.flags().add("--path", "b.txt"), 4, "refused", "content-conversion")
		e.refuse("filemode "+v+" bytes only", nosuch.add("--path", "a.txt"), 4, "refused", "remote-unknown")
	}
	for _, extra := range []string{fm("true"), fm("yes"), "", "[core]\n\tautocrlf = true\n"} {
		setLocal(extra)
		e.refuse("not refused: "+extra, nosuch, 4, "refused", "remote-unknown")
	}
	setLocal("")
	rep := pubWant(t, e.publish(), 0, "published", "")
	e.wantRequestCommit(rep.Steps.Commit.Commit, e.base, pubMessage, map[string]*pubFile{
		"a.txt": {"100644", "line one\nline two\nline three\n"}, "tool": {"100755", "tool\n"}, "b.txt": {"100755", "bee\n"},
	})
}

// pubTestGitLocks: R10.c, the lock listing holds exactly the Git lock files
// of section 2, sorted, with symbolic links resolved; others are ignored;
// refusals leave the locks in place.
func pubTestGitLocks(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	gd, err := filepath.EvalSymlinks(e.gitDir)
	if err != nil {
		t.Fatal(err)
	}
	lock := func(rel string) string {
		p := filepath.Join(gd, rel)
		pubWriteFile(t, p, nil, 0o644)
		return p
	}
	idx := lock("index.lock")
	rep := e.refuse("index.lock", e.flags(), 4, "refused", "git-locked")
	pubWantStrings(t, "locks_left", rep.LocksLeft, []string{idx})
	reported := []string{idx, lock("HEAD.lock"), lock("config.lock"), lock("packed-refs.lock"), lock("refs/heads/feature.lock"),
		lock("refs/remotes/origin/feature.lock"), lock("reftable/tables.list.lock")}
	ignored := []string{lock("refs/heads/other.lock"), lock("refs/remotes/origin/other.lock"), lock("objects/pub.lock"), lock("logs/pub.lock")}
	sort.Strings(reported)
	rep = e.refuse("all lock kinds", e.flags(), 4, "refused", "git-locked")
	pubWantStrings(t, "locks_left", rep.LocksLeft, reported)
	for _, p := range append(slices.Clone(reported), ignored...) {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("lock %s removed by a refused run: %v", p, err)
		}
	}
	for _, p := range reported {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	e.refuse("only locks outside the set", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	for _, p := range ignored {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	pubWant(t, e.publish(), 0, "published", "")
}

// pubTestRemoteRefs: R11.a, R11.c, R11.d, R11.e with R11.f (refusals change
// nothing), then R15.b fast-forward from an existing remote branch.
func pubTestRemoteRefs(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.refuse("unknown remote", e.flags().set("--push", "nosuch"), 4, "refused", "remote-unknown")
	e.refuse("expected a commit, remote absent", e.flags().add("--expect-remote", e.base), 4, "refused", "remote-moved")
	e.git("push", "-q", "origin", e.base+":refs/heads/"+pubBranch)
	e.refuse("expected absent, remote at base", e.flags().add("--expect-remote", "absent"), 4, "refused", "remote-moved")
	e.refuse("expected another commit", e.flags().add("--expect-remote", strings.Repeat("ab", 20)), 4, "refused", "remote-moved")
	x := e.cloneAndPush(e.base, pubBranch)
	e.refuse("remote at an unknown commit", e.flags(), 4, "refused", "not-fast-forward")
	e.refuse("expected the unknown commit", e.flags().add("--expect-remote", x), 4, "refused", "not-fast-forward")
	e.refuse("expected base, remote moved on", e.flags().add("--expect-remote", e.base), 4, "refused", "remote-moved")
	e.git("fetch", "-q", "origin")
	e.refuse("expected a local non-ancestor", e.flags().add("--expect-remote", x), 4, "refused", "not-fast-forward")
	e.refuse("remote at a local non-ancestor", e.flags(), 4, "refused", "not-fast-forward")
	if e.remoteRef(e.remote, pubBranch) != x {
		t.Fatalf("remote moved by a refused run")
	}
	e.gitIn(e.root, "--git-dir="+e.remote, "update-ref", "refs/heads/"+pubBranch, e.base)
	rep := pubWant(t, e.publishF(e.flags().add("--expect-remote", e.base)), 0, "published", "")
	c := rep.Steps.Commit.Commit
	if rep.ExpectRemote != e.base || e.remoteRef(e.remote, pubBranch) != c || rep.Steps.Push.Status != "done" {
		t.Errorf("expect_remote %q remote %s push %q, want %s %s done", rep.ExpectRemote, e.remoteRef(e.remote, pubBranch), rep.Steps.Push.Status, e.base, c)
	}
	// R11.b, R20.a: the remote now holds C, so the original --expect-remote
	// no longer applies and the same command is a no-op.
	r := e.publishF(e.flags().add("--expect-remote", e.base))
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
	if r.git != (pubGitCount{}) || e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("re-run with the original --expect-remote ran %+v, remote %s", r.git, e.remoteRef(e.remote, pubBranch))
	}
	// R11.b: with R equal to C, R11.c to R11.e do not apply at all, so an
	// expectation that would fail R11.c and R11.d (the local non-ancestor x,
	// a commit that does not exist) still gives the R20.a no-op.
	for _, expect := range []string{x, strings.Repeat("ab", 20)} {
		r := e.publishF(e.flags().add("--expect-remote", expect))
		rep := pubWant(t, r, 0, "published", "")
		pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
		if rep.ExpectRemote != expect || rep.Steps.Commit.Commit != c || rep.Steps.Push.RemoteRef != c {
			t.Errorf("--expect-remote %s with the remote at C: expect_remote %q commit %q remote_ref %q, want %s and %s (R11.b)", expect, rep.ExpectRemote, rep.Steps.Commit.Commit, rep.Steps.Push.RemoteRef, expect, c)
		}
		if r.git != (pubGitCount{}) || e.remoteRef(e.remote, pubBranch) != c {
			t.Errorf("--expect-remote %s with the remote at C ran %+v, remote %s, want no state-changing git and the remote at %s", expect, r.git, e.remoteRef(e.remote, pubBranch), c)
		}
	}
}

// pubTestRemoteSplit: R11.g, fetch and push URLs (after insteadOf and
// pushInsteadOf) must be one each and name the same repository by the URL
// reduction of section 2.
func pubTestRemoteSplit(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	second := filepath.Join(e.root, "second.git")
	e.gitIn(e.root, "init", "-q", "--bare", second)
	localPath := filepath.Join(e.gitDir, "config")
	localBase, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	reset := func() { pubWriteFile(t, localPath, localBase, 0o644) }
	cfg := func(args ...string) func() { return func() { e.git(append([]string{"config"}, args...)...) } }
	bogus := strings.Repeat("ef", 20)
	cases := []struct {
		name  string
		set   []func()
		split bool
	}{
		{"pushurl names another repository", []func(){cfg("remote.origin.pushurl", second)}, true},
		{"two fetch urls", []func(){cfg("--add", "remote.origin.url", second)}, true},
		{"two push urls", []func(){cfg("--add", "remote.origin.pushurl", e.remote), cfg("--add", "remote.origin.pushurl", e.remote)}, true},
		{"pushInsteadOf to another repository", []func(){cfg("url."+second+".pushInsteadOf", e.remote)}, true},
		{"pushurl on another host", []func(){cfg("remote.origin.pushurl", "ssh://host.example"+e.remote)}, true},
		{"pushurl with a second .git", []func(){cfg("remote.origin.pushurl", e.remote+".git")}, true},
		{"pushurl file scheme", []func(){cfg("remote.origin.pushurl", "file://"+e.remote)}, false},
		// Git hands these to a remote helper, which picks the destination.
		{"pushurl through a remote helper", []func(){cfg("remote.origin.pushurl", "pubhelper::file://"+e.remote)}, true},
		{"pushurl with an unknown scheme", []func(){cfg("remote.origin.pushurl", "pubhelper://"+e.remote)}, true},
		{"pushurl trailing slash", []func(){cfg("remote.origin.pushurl", e.remote+"/")}, false},
		{"pushurl without .git", []func(){cfg("remote.origin.pushurl", strings.TrimSuffix(e.remote, ".git"))}, false},
		// The path is compared case-insensitively, and the one trailing .git
		// is removed in any case (revision 3b); each rule has its own input.
		{"pushurl path in other case", []func(){cfg("remote.origin.pushurl", strings.ToUpper(strings.TrimSuffix(e.remote, ".git"))+".git")}, false},
		{"pushurl .GIT suffix", []func(){cfg("remote.origin.pushurl", strings.TrimSuffix(e.remote, ".git")+".GIT")}, false},
		{"pushurl .GIT.git removes one suffix only", []func(){cfg("remote.origin.pushurl", strings.TrimSuffix(e.remote, ".git")+".GIT.git")}, true},
		{"pushurl relative to the worktree", []func(){cfg("remote.origin.pushurl", "../remote.git")}, false},
		{"insteadOf for both", []func(){cfg("remote.origin.url", "pubalias:remote.git"), cfg("url."+e.root+"/.insteadOf", "pubalias:")}, false},
	}
	secondRefs := e.refsOf(second)
	for _, c := range cases {
		reset()
		for _, s := range c.set {
			s()
		}
		if c.split {
			rep := e.refuse(c.name, e.flags(), 4, "refused", "remote-split")
			if c.name == "pushurl names another repository" && !strings.Contains(rep.Detail, second) {
				t.Errorf("detail %q does not name the push URL %s", rep.Detail, second)
			}
			if c.name == "pushurl through a remote helper" && !strings.Contains(rep.Detail, "pubhelper::file://"+e.remote+", which Git hands to a remote helper") {
				t.Errorf("detail %q does not name the helper URL", rep.Detail)
			}
		} else {
			e.refuse(c.name, e.flags().add("--expect-remote", bogus), 4, "refused", "remote-moved")
		}
		if got := e.refsOf(second); got != secondRefs {
			t.Errorf("%s: the second repository gained refs %q", c.name, got)
		}
	}
	reset()
	cfg("remote.origin.pushurl", "file://"+e.remote)()
	rep := pubWant(t, e.publish(), 0, "published", "")
	if e.remoteRef(e.remote, pubBranch) != rep.Steps.Commit.Commit || e.refsOf(second) != secondRefs {
		t.Errorf("push through the file:// push URL did not reach the remote only")
	}
}

// pubTestPRPreconditions: R12.a (repo-mismatch; url or sshUrl may match, in
// file:// form too), R12.b (cross-repository and closed PRs ignored,
// equivalence), R12.c (pr-exists) and R12.d (pr-missing, pr-ambiguous), each
// before any change; finally a create beside a cross-repository PR.
func pubTestPRPreconditions(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	equivalentBody := "Why: the widget needed it.\n\nTrailing spaces stay in stdin.\n\n\t "
	create, edit := e.prFlags("create"), e.prFlags("edit")
	e.setGH(func(s *pubGHState) { s.RepoURL, s.SSHURL = "https://github.example/octo/widget", pubSSHURL })
	e.refuse("repo-mismatch", create, 4, "refused", "repo-mismatch")
	e.setGH(func(s *pubGHState) {
		s.SSHURL = e.remote
		s.PRs = []pubPR{pubOpenPR(3, "Different title", pubBase, pubBody, false)}
	})
	e.refuse("sshUrl matches, title differs", create, 4, "refused", "pr-exists")
	e.setGH(func(s *pubGHState) { s.RepoURL, s.SSHURL = "file://"+e.remote, pubSSHURL })
	e.refuse("file url matches, title differs", create, 4, "refused", "pr-exists")
	e.setGH(func(s *pubGHState) { s.PRs = []pubPR{pubOpenPR(3, pubTitle, "develop", equivalentBody, false)} })
	e.refuse("base differs", create, 4, "refused", "pr-exists")
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(3, pubTitle, pubBase, "Why: the widget needed it.\n\nSomething else.", false)}
	})
	e.refuse("body differs", create, 4, "refused", "pr-exists")
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(3, pubTitle, pubBase, equivalentBody, false), pubOpenPR(4, pubTitle, pubBase, pubBody, false)}
	})
	e.refuse("two equivalent", create, 4, "refused", "pr-exists")
	e.setGH(func(s *pubGHState) { s.PRs = nil })
	e.refuse("edit with none", edit, 4, "refused", "pr-missing")
	e.setGH(func(s *pubGHState) { s.PRs = []pubPR{pubOpenPR(5, "Fork PR", pubBase, pubBody, true)} })
	e.refuse("edit with only a cross-repository PR", edit, 4, "refused", "pr-missing")
	closed := pubOpenPR(6, pubTitle, pubBase, pubBody, false)
	closed.State = "CLOSED"
	e.setGH(func(s *pubGHState) { s.PRs = []pubPR{closed} })
	e.refuse("edit with only a closed PR", edit, 4, "refused", "pr-missing")
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(7, "One", pubBase, pubBody, false), pubOpenPR(8, "Two", pubBase, pubBody, false)}
	})
	e.refuse("edit with two", edit, 4, "refused", "pr-ambiguous")
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 {
		t.Errorf("gh create %d edit %d during refusals, want 0", n.create, n.edit)
	}
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(5, "Fork PR", pubBase, pubBody, true)}
		s.NextNumber = 20
	})
	rep := pubWant(t, e.publishF(create), 0, "published", "")
	if rep.Steps.PR.Status != "done" || rep.Steps.PR.Number != 20 {
		t.Errorf("pr step %+v, want done with the created PR 20", rep.Steps.PR)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 {
		t.Errorf("gh pr create ran %d times, want 1", n.create)
	}
}

// pubTestPREquivalentExists: R12.c, exactly one equivalent PR (body equal
// after normalisation) gives PR step already-present with no create or edit.
func pubTestPREquivalentExists(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(3, pubTitle, pubBase, "Why: the widget needed it.\r\n\r\nTrailing spaces stay in stdin.\r\n\r\n", false)}
	})
	rep := pubWant(t, e.publishF(e.prFlags("create")), 0, "published", "")
	pubWantSteps(t, rep, "done", "done", "already-present")
	c := rep.Steps.Commit.Commit
	if pr := rep.Steps.PR; pr.Number != 3 || pr.HeadRefOID != c || pr.Base != pubBase || pr.State != "OPEN" || pr.URL != "https://github.example/octo/widget/pull/3" {
		t.Errorf("pr step %+v, want PR 3 at %s", pr, c)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 || n.view == 0 {
		t.Errorf("gh create %d edit %d view %d, want 0 0 and a readback", n.create, n.edit, n.view)
	}
}

// pubTestPRCreate: R16.a and R16.c (create once, locate, readback), R22
// (exact gh argv with --flag=value so a title starting with -- stays a
// value, body on stdin, worktree as working directory), R19 request.pr, R2
// --summary, and R20.a re-runs with a PR.
func pubTestPRCreate(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	// A title that a shell would expand or split, and that starts like an
	// option, must reach gh as one --title= value (R22, section 8).
	title := `--draft: publish $HOME and $(uname) 'quoted' "double" ; | *`
	f := e.prFlags("create").drop("--title").add("--title=" + title)
	rep := pubWant(t, e.publishF(f), 0, "published", "")
	c := rep.Steps.Commit.Commit
	pubWantSteps(t, rep, "done", "done", "done")
	if pr := rep.Steps.PR; pr.Number != 1 || pr.URL != "https://github.example/octo/widget/pull/1" || pr.HeadRefOID != c || pr.Base != pubBase || pr.State != "OPEN" {
		t.Errorf("pr step %+v, want PR 1 at %s on %s", pr, c, pubBase)
	}
	n := e.ghCheck(e.repo, title)
	if n.create != 1 || n.edit != 0 || n.repo == 0 || n.list == 0 || n.view == 0 {
		t.Errorf("gh calls %+v, want one create, no edit, and repo, list and view reads", n)
	}
	s := e.loadGH()
	if len(s.PRs) != 1 || s.PRs[0].Title != title || s.PRs[0].Body != pubBody || s.PRs[0].BaseRefName != pubBase || s.PRs[0].HeadRefName != pubBranch {
		t.Errorf("PR state %+v, want one PR with the title, body bytes, base and head", s.PRs)
	}
	entries := e.entries()
	req := e.checkLastRun(entries, rep, 0, e.msg)
	if want := (pubPublicationPR{Mode: "create", Repo: pubRepo, Base: pubBase, Title: title, BodySHA256: e.fileSHA(e.body)}); req.PR != want {
		t.Errorf("request.pr = %+v, want %+v", req.PR, want)
	}
	sum := pubWantSummary(t, e.publishF(f.add("--summary")))
	if sum.Status != "published" || sum.Reason != "" || sum.TaskID != e.taskID || sum.Commit != c || sum.RemoteRef != c || sum.PRNumber != 1 || sum.PRURL != "https://github.example/octo/widget/pull/1" || !strings.HasSuffix(sum.Record, "publication-000004.json") || !filepath.IsAbs(sum.Record) {
		t.Errorf("summary %+v", sum)
	}
	r := e.publishF(f)
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "already-present")
	if r.git != (pubGitCount{}) {
		t.Errorf("re-run ran state-changing git commands %+v (R20.a)", r.git)
	}
	if rep.PreviousOutcome != "published" || rep.Steps.PR.Number != 1 || e.rev("HEAD") != c || e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("re-run previous_outcome %q pr %+v HEAD %s remote %s", rep.PreviousOutcome, rep.Steps.PR, e.rev("HEAD"), e.remoteRef(e.remote, pubBranch))
	}
	if n := e.ghCheck(e.repo, title); n.create != 1 || n.edit != 0 {
		t.Errorf("re-runs ran gh create %d edit %d, want still 1 and 0", n.create, n.edit)
	}
	if got := len(e.entries()); got != 6 {
		t.Errorf("%d entries after three runs, want 6", got)
	}
}

// pubTestPREdit: R16.b, the one existing PR is edited once (base, title,
// body); a re-run finds it equivalent at C and edits nothing.
func pubTestPREdit(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.git("push", "-q", "origin", e.base+":refs/heads/"+pubBranch)
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(7, "Old title", "develop", "old body\n", false)}
		s.NextNumber = 8
	})
	rep := pubWant(t, e.publishF(e.prFlags("edit")), 0, "published", "")
	c := rep.Steps.Commit.Commit
	pubWantSteps(t, rep, "done", "done", "done")
	if pr := rep.Steps.PR; pr.Number != 7 || pr.HeadRefOID != c || pr.Base != pubBase || pr.State != "OPEN" {
		t.Errorf("pr step %+v, want PR 7 at %s", pr, c)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.edit != 1 || n.create != 0 {
		t.Errorf("gh edit %d create %d, want 1 and 0", n.edit, n.create)
	}
	if s := e.loadGH(); s.PRs[0].Title != pubTitle || s.PRs[0].Body != pubBody || s.PRs[0].BaseRefName != pubBase {
		t.Errorf("PR 7 after edit %+v", s.PRs[0])
	}
	r := e.publishF(e.prFlags("edit"))
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "already-present")
	if r.git != (pubGitCount{}) {
		t.Errorf("re-run ran state-changing git commands %+v (R20.a)", r.git)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.edit != 1 {
		t.Errorf("re-run edited again (%d edits), want none", n.edit)
	}
}

// pubTestPRUnknownAfterCreate: R16.d (create fails after creating: exit 6,
// PR step unknown with the number a read-only list found), R17 (no second
// create), R20.b (the same command continues and finds it).
func pubTestPRUnknownAfterCreate(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) { s.CreateMode = "fail-after" })
	f := e.prFlags("create")
	rep := pubWant(t, e.publishF(f), 6, "unknown", "pr-unknown")
	c := rep.Steps.Commit.Commit
	pubWantSteps(t, rep, "done", "done", "unknown")
	if rep.Steps.PR.Number != 1 {
		t.Errorf("pr step %+v, want the number 1 found by the reconciliation list", rep.Steps.PR)
	}
	if e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("push did not keep its effect")
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 || n.edit != 0 {
		t.Errorf("gh create %d edit %d after an unknown outcome, want 1 and 0 (R17)", n.create, n.edit)
	}
	e.checkLastRun(e.entries(), rep, 6, e.msg)
	e.setGH(func(s *pubGHState) { s.CreateMode = "" })
	r := e.publishF(f)
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "already-present")
	if r.git != (pubGitCount{}) {
		t.Errorf("continuation ran state-changing git commands %+v (R20.b)", r.git)
	}
	if rep.PreviousOutcome != "unknown" || rep.Steps.PR.Number != 1 || rep.Steps.PR.HeadRefOID != c {
		t.Errorf("continuation previous_outcome %q pr %+v", rep.PreviousOutcome, rep.Steps.PR)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 {
		t.Errorf("gh create ran %d times in all, want 1", n.create)
	}
}

// pubTestPRUnknownBeforeCreate: R16.d with nothing created (number 0), then
// R20.b creates it.
func pubTestPRUnknownBeforeCreate(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) { s.CreateMode = "fail-before" })
	f := e.prFlags("create")
	rep := pubWant(t, e.publishF(f), 6, "unknown", "pr-unknown")
	pubWantSteps(t, rep, "done", "done", "unknown")
	if rep.Steps.PR.Number != 0 {
		t.Errorf("pr step %+v, want number 0 when the list finds none", rep.Steps.PR)
	}
	e.setGH(func(s *pubGHState) { s.CreateMode = "" })
	rep = pubWant(t, e.publishF(f), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "done")
	if n := e.ghCheck(e.repo, pubTitle); n.create != 2 {
		t.Errorf("gh create ran %d times over two runs, want one per run", n.create)
	}
}

// prUnknown checks a run that must end in R16.d after the commit and push:
// exit 6 pr-unknown, PR step unknown with number the read-only list found,
// the push kept, and R17 (gh create and edit counts as given).
func (e *pubEnv) prUnknown(label string, r pubRun, number, creates, edits int) *pubReport {
	t := e.t
	t.Helper()
	rep := pubWant(t, r, 6, "unknown", "pr-unknown")
	pubWantSteps(t, rep, "done", "done", "unknown")
	if rep.Steps.PR.Number != number {
		t.Errorf("%s: pr step %+v, want number %d (R16.d)", label, rep.Steps.PR, number)
	}
	if got := e.remoteRef(e.remote, pubBranch); got != rep.Steps.Commit.Commit {
		t.Errorf("%s: remote %s, want the pushed %s kept", label, got, rep.Steps.Commit.Commit)
	}
	if r.git.add > 1 || r.git.commit != 1 || r.git.push != 1 {
		t.Errorf("%s: state-changing git commands %+v, want one commit and one push", label, r.git)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != creates || n.edit != edits {
		t.Errorf("%s: gh create %d edit %d, want %d and %d (R17)", label, n.create, n.edit, creates, edits)
	}
	e.checkLastRun(e.entries(), rep, 6, e.msg)
	return rep
}

// pubPREditEnv is a verified task whose branch is on the remote with one open
// PR 7 that differs from the request, so --pr edit edits it.
func pubPREditEnv(t *testing.T, tl *pubTools) *pubEnv {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.git("push", "-q", "origin", e.base+":refs/heads/"+pubBranch)
	e.setGH(func(s *pubGHState) {
		s.PRs = []pubPR{pubOpenPR(7, "Old title", "develop", "old body\n", false)}
		s.NextNumber = 8
	})
	return e
}

// pubTestPREditUnknown: R16.d for a gh pr edit that does not succeed, before
// and after it changed the PR (number 7 from the list), R17 (one edit), and
// R20.b: the same command continues; it edits again only when the PR is not
// yet equivalent at C (R16.b).
func pubTestPREditUnknown(t *testing.T, tl *pubTools) {
	for _, c := range []struct {
		mode       string
		continued  string
		totalEdits int
	}{
		{"fail-before", "done", 2},
		{"fail-after", "already-present", 1},
	} {
		t.Run(c.mode, func(t *testing.T) {
			e := pubPREditEnv(t, tl)
			e.setGH(func(s *pubGHState) { s.EditMode = c.mode })
			f := e.prFlags("edit")
			e.prUnknown("gh pr edit "+c.mode, e.publishF(f), 7, 0, 1)
			e.setGH(func(s *pubGHState) { s.EditMode = "" })
			r := e.publishF(f)
			rep := pubWant(t, r, 0, "published", "")
			pubWantSteps(t, rep, "already-present", "already-present", c.continued)
			if r.git != (pubGitCount{}) {
				t.Errorf("continuation ran state-changing git commands %+v (R20.b)", r.git)
			}
			if rep.PreviousOutcome != "unknown" || rep.Steps.PR.Number != 7 || rep.Steps.PR.HeadRefOID != rep.Steps.Commit.Commit {
				t.Errorf("continuation previous_outcome %q pr %+v", rep.PreviousOutcome, rep.Steps.PR)
			}
			if n := e.ghCheck(e.repo, pubTitle); n.edit != c.totalEdits || n.create != 0 {
				t.Errorf("gh edit %d create %d over both runs, want %d and 0", n.edit, n.create, c.totalEdits)
			}
		})
	}
}

// pubTestPRNotLocated: R16.a and R16.d, gh pr create succeeds but the list
// never shows the new PR: after five list reads the PR cannot be located,
// exit 6 pr-unknown with number 0 (the read-only list finds none); one
// create (R17). R20.b: once the list shows it, the same command finds the
// equivalent PR and creates nothing.
func pubTestPRNotLocated(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	const hidden = 100
	e.setGH(func(s *pubGHState) { s.HideNewLists = hidden })
	f := e.prFlags("create")
	e.prUnknown("PR never listed", e.publishF(f), 0, 1, 0)
	// Lists after the create: five locating reads, and at most one read-only
	// reconciliation list (R16.a, R16.d, R17).
	if after := hidden - e.loadGH().HideNewLists; after < 5 || after > 6 {
		t.Errorf("%d PR list reads after gh pr create, want five locating reads and at most one reconciliation read", after)
	}
	e.setGH(func(s *pubGHState) { s.HideNewLists, s.HiddenNumber = 0, 0 })
	r := e.publishF(f)
	rep := pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "already-present")
	if rep.Steps.PR.Number != 1 || rep.PreviousOutcome != "unknown" {
		t.Errorf("continuation pr %+v previous_outcome %q, want PR 1 after unknown", rep.Steps.PR, rep.PreviousOutcome)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 {
		t.Errorf("gh create ran %d times in all, want 1", n.create)
	}
}

// pubTestPRViewFails: R16.d, gh pr view fails after a create or an edit, so
// the PR cannot be read back: exit 6 pr-unknown with the number the list
// found (1 created, 7 edited); R20.b continues to already-present.
func pubTestPRViewFails(t *testing.T, tl *pubTools) {
	for _, c := range []struct {
		mode           string
		number         int
		creates, edits int
	}{
		{"create", 1, 1, 0},
		{"edit", 7, 0, 1},
	} {
		t.Run(c.mode, func(t *testing.T) {
			var e *pubEnv
			if c.mode == "edit" {
				e = pubPREditEnv(t, tl)
			} else {
				e = pubNewEnv(t, tl)
				e.startTask(pubPlanSmall)
				e.standardChange()
				e.verifyTask()
			}
			e.setGH(func(s *pubGHState) { s.ViewMode = "fail" })
			f := e.prFlags(c.mode)
			e.prUnknown("gh pr view fails after "+c.mode, e.publishF(f), c.number, c.creates, c.edits)
			if n := e.ghCheck(e.repo, pubTitle); n.view == 0 {
				t.Errorf("gh pr view never ran after the %s", c.mode)
			}
			e.setGH(func(s *pubGHState) { s.ViewMode = "" })
			rep := pubWant(t, e.publishF(f), 0, "published", "")
			pubWantSteps(t, rep, "already-present", "already-present", "already-present")
			if rep.Steps.PR.Number != c.number {
				t.Errorf("continuation pr %+v, want number %d", rep.Steps.PR, c.number)
			}
			if n := e.ghCheck(e.repo, pubTitle); n.create != c.creates || n.edit != c.edits {
				t.Errorf("gh create %d edit %d over both runs, want %d and %d", n.create, n.edit, c.creates, c.edits)
			}
		})
	}
}

// pubTestInterruptPR: R18.c and R16.d, SIGINT during gh pr create stops it
// and gives exit 6 pr-unknown (number 0: nothing was created); R17 (the
// create is not retried in the run); R20.b continues and creates the PR.
func pubTestInterruptPR(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("gh-pr-create")
	e.setGH(func(s *pubGHState) { s.CreateMode, s.Started, s.Release = "block", started, release })
	f := e.prFlags("create")
	p := e.startCW(nil, f.argv()...)
	pubWaitMarker(t, p, started)
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	// The stopped gh may not log its call; the started marker shows it ran.
	// No logged create is a second attempt in the run.
	e.prUnknown("interrupted gh pr create", e.waitRun(p), 0, 0, 0)
	e.setGH(func(s *pubGHState) { s.CreateMode, s.Started, s.Release = "", "", "" })
	rep := pubWant(t, e.publishF(f), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "done")
	if rep.Steps.PR.Number != 1 || rep.PreviousOutcome != "unknown" {
		t.Errorf("continuation pr %+v previous_outcome %q", rep.Steps.PR, rep.PreviousOutcome)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 {
		t.Errorf("gh create logged %d times, want the continuation's one", n.create)
	}
}

// pubTestPRMismatchTitle: R16.c, a readback whose title differs exits 7.
func pubTestPRMismatchTitle(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) { s.CreateTitleSuffix = " (edited by a bot)" })
	rep := pubWant(t, e.publishF(e.prFlags("create")), 7, "mismatch", "pr-mismatch")
	pubWantSteps(t, rep, "done", "done", "mismatch")
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 || n.edit != 0 {
		t.Errorf("gh create %d edit %d, want 1 and 0", n.create, n.edit)
	}
}

// pubTestPRReadbackRetries: R16.a (the list is read again until the new PR
// appears) and R16.c (views are read again while only headRefOid differs).
func pubTestPRReadbackRetries(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) { s.HideNewLists, s.StaleViews = 1, 2 })
	rep := pubWant(t, e.publishF(e.prFlags("create")), 0, "published", "")
	pubWantSteps(t, rep, "done", "done", "done")
	if rep.Steps.PR.HeadRefOID != rep.Steps.Commit.Commit {
		t.Errorf("head_ref_oid %q, want %s", rep.Steps.PR.HeadRefOID, rep.Steps.Commit.Commit)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 1 || n.view != 3 {
		t.Errorf("gh create %d view %d, want 1 create and 3 views (two stale)", n.create, n.view)
	}
}

// pubTestPRReadbackStale: R16.c, a headRefOid that never matches gives
// pr-mismatch after five views.
func pubTestPRReadbackStale(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.setGH(func(s *pubGHState) { s.StaleViews = 100 })
	rep := pubWant(t, e.publishF(e.prFlags("create")), 7, "mismatch", "pr-mismatch")
	pubWantSteps(t, rep, "done", "done", "mismatch")
	if n := e.ghCheck(e.repo, pubTitle); n.view != 5 || n.create != 1 {
		t.Errorf("gh view %d create %d, want 5 views (one and four more) and 1 create", n.view, n.create)
	}
}

// pubNormBody is R12.b's body normalisation, computed from the contract: CRLF
// to LF, then trailing spaces, tabs, CR and LF removed from the end only.
func pubNormBody(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\r\n")
}

// pubTestPRReadbackBodyState: R16.c two-sided for the body and the state,
// after gh pr create and after gh pr edit. The fake stores a body that
// differs from the body file (a bot's note appended, or a leading newline,
// which the normalisation does not remove because it trims the end only), or
// the PR is closed or merged between the list that located it and the view.
// Each gives exit 7 pr-mismatch with PR step mismatch after one view (cw reads
// again only while headRefOid alone differs), the commit and push kept, one
// create or edit (R17: no retry), and the started and finished entries
// (R19). A body that differs from the body file only by the normalisation of
// R12.b (CRLF turned into LF, other trailing whitespace) is accepted: exit 0,
// PR step done. Fails when the readback ignores the body or the state, or
// compares the body bytes without the normalisation.
func pubTestPRReadbackBodyState(t *testing.T, tl *pubTools) {
	const botNote = "\n\nEdited by a bot.\n"
	for _, c := range []struct {
		name, mode     string
		lf             bool
		prefix, suffix string
		state          string // ViewSetsState
		match          bool
	}{
		{name: "create_body_note_appended", mode: "create", suffix: botNote},
		{name: "create_body_leading_newline", mode: "create", prefix: "\n"},
		{name: "create_state_closed", mode: "create", state: "CLOSED"},
		{name: "create_state_merged", mode: "create", state: "MERGED"},
		{name: "create_body_normalised_accepted", mode: "create", lf: true, suffix: " \t\r\n\n", match: true},
		{name: "edit_body_note_appended", mode: "edit", suffix: botNote},
		{name: "edit_state_closed", mode: "edit", state: "CLOSED"},
		{name: "edit_body_normalised_accepted", mode: "edit", lf: true, suffix: "\r\n \n", match: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var e *pubEnv
			number, creates, edits := 1, 1, 0
			if c.mode == "edit" {
				e = pubPREditEnv(t, tl)
				number, creates, edits = 7, 0, 1
			} else {
				e = pubNewEnv(t, tl)
				e.startTask(pubPlanSmall)
				e.standardChange()
				e.verifyTask()
			}
			e.setGH(func(s *pubGHState) {
				s.BodyLF, s.BodyPrefix, s.BodySuffix, s.ViewSetsState = c.lf, c.prefix, c.suffix, c.state
			})
			r := e.publishF(e.prFlags(c.mode))
			var rep *pubReport
			if c.match {
				rep = pubWant(t, r, 0, "published", "")
				pubWantSteps(t, rep, "done", "done", "done")
				pr := rep.Steps.PR
				if pr.Number != number || pr.URL != fmt.Sprintf("https://github.example/%s/pull/%d", pubRepo, number) || pr.HeadRefOID != rep.Steps.Commit.Commit || pr.Base != pubBase || pr.State != "OPEN" {
					t.Errorf("pr step %+v, want PR %d at %s on %s, OPEN", pr, number, rep.Steps.Commit.Commit, pubBase)
				}
			} else {
				rep = pubWant(t, r, 7, "mismatch", "pr-mismatch")
				pubWantSteps(t, rep, "done", "done", "mismatch")
			}
			c0 := rep.Steps.Commit.Commit
			if c0 == "" || e.rev("HEAD") != c0 || e.remoteRef(e.remote, pubBranch) != c0 {
				t.Errorf("commit %q HEAD %s remote %s, want the commit and push kept", c0, e.rev("HEAD"), e.remoteRef(e.remote, pubBranch))
			}
			if r.git.commit != 1 || r.git.push != 1 {
				t.Errorf("state-changing git commands %+v, want one commit and one push", r.git)
			}
			if n := e.ghCheck(e.repo, pubTitle); n.create != creates || n.edit != edits || n.view != 1 {
				t.Errorf("gh create %d edit %d view %d, want %d, %d and one view (R16.c, R17)", n.create, n.edit, n.view, creates, edits)
			}
			// Fixture check: the stored PR differs from the request only as the
			// case intends.
			s := e.loadGH()
			i := slices.IndexFunc(s.PRs, func(pr pubPR) bool { return pr.Number == number })
			if i < 0 {
				t.Fatalf("PR %d missing from the fake's state %+v", number, s.PRs)
			}
			got := s.PRs[i]
			if got.Title != pubTitle || got.BaseRefName != pubBase || got.HeadRefName != pubBranch {
				t.Errorf("fixture: PR %+v differs in title, base or head", got)
			}
			wantState := "OPEN"
			if c.state != "" {
				wantState = c.state
			}
			if got.State != wantState {
				t.Errorf("fixture: PR state %q, want %q", got.State, wantState)
			}
			switch {
			case c.match:
				if got.Body == pubBody || pubNormBody(got.Body) != pubNormBody(pubBody) {
					t.Errorf("fixture: stored body %q must differ from the body file in bytes and equal it after normalisation", got.Body)
				}
			case c.state != "":
				if got.Body != pubBody {
					t.Errorf("fixture: stored body %q, want the body file's bytes", got.Body)
				}
			default:
				if pubNormBody(got.Body) == pubNormBody(pubBody) {
					t.Errorf("fixture: stored body %q must differ from the body file after normalisation", got.Body)
				}
			}
			entries := e.entries()
			if len(entries) != 2 {
				t.Errorf("%d publication entries, want a started and a finished entry", len(entries))
			}
			req := e.checkLastRun(entries, rep, r.code, e.msg)
			if want := (pubPublicationPR{Mode: c.mode, Repo: pubRepo, Base: pubBase, Title: pubTitle, BodySHA256: e.fileSHA(e.body)}); req.PR != want {
				t.Errorf("request.pr = %+v, want %+v", req.PR, want)
			}
		})
	}
}

// pubTestPRReRead: R16, the PR step's re-read refuses a PR that appeared
// after the preconditions, keeping the commit and push.
func pubTestPRReRead(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	later := e.loadGH()
	later.PRs = []pubPR{pubOpenPR(9, "Opened meanwhile", pubBase, "other\n", false)}
	prepared := filepath.Join(e.root, "gh-state-later.json")
	pubWriteJSON(t, prepared, later)
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{Copy: [][2]string{{prepared, e.ghState}}})
	rep := pubWant(t, e.publishF(e.prFlags("create")), 4, "refused", "pr-exists")
	pubWantSteps(t, rep, "done", "done", "refused")
	if e.remoteRef(e.remote, pubBranch) != rep.Steps.Commit.Commit {
		t.Errorf("push did not keep its effect")
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 {
		t.Errorf("gh create ran %d times, want 0", n.create)
	}
	e.checkLastRun(e.entries(), rep, 4, e.msg)
}

// pubTestPushRefused: R15.d (a remote's pre-receive refusal is unknown, 6,
// with remote_ref observed and no PR step), R17, R20.c (continuation).
func pubTestPushRefused(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.remote, "hooks"), "pre-receive", pubHookConf{Exit: 1})
	f := e.prFlags("create")
	rep := pubWant(t, e.publishF(f), 6, "unknown", "push-unknown")
	pubWantSteps(t, rep, "done", "unknown", "skipped")
	if rep.Steps.Push.RemoteRef != "absent" || e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("push remote_ref %q remote %s, want absent", rep.Steps.Push.RemoteRef, e.remoteRef(e.remote, pubBranch))
	}
	pubWantStrings(t, "locks_left", rep.LocksLeft, nil)
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 {
		t.Errorf("gh create %d edit %d after push-unknown, want none (R17)", n.create, n.edit)
	}
	if n := len(pubCalls(t, filepath.Join(e.logs, "pre-receive.jsonl"))); n != 1 {
		t.Errorf("push attempted %d times, want 1 (R17)", n)
	}
	e.hook(filepath.Join(e.remote, "hooks"), "pre-receive", pubHookConf{})
	r := e.publishF(f)
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "done", "done")
	if want := (pubGitCount{push: 1, pushRefspec: rep.Steps.Commit.Commit + ":refs/heads/" + pubBranch}); r.git != want {
		t.Errorf("continuation git commands %+v, want only the push %+v (R20.c)", r.git, want)
	}
	if rep.PreviousOutcome != "unknown" {
		t.Errorf("previous_outcome %q, want unknown", rep.PreviousOutcome)
	}
}

// pubTestPrePushFails: R15.d and R17, a failing pre-push hook makes the push
// unknown and is not retried.
func pubTestPrePushFails(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	log := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{Exit: 1})
	rep := pubWant(t, e.publish(), 6, "unknown", "push-unknown")
	pubWantSteps(t, rep, "done", "unknown", "not-requested")
	if n := len(pubCalls(t, log)); n != 1 {
		t.Errorf("pre-push ran %d times, want 1", n)
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote changed")
	}
}

// pubTestPushMismatch: R15.c, git push succeeds but R then reads as another
// commit (a post-receive hook moves it).
func pubTestPushMismatch(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.remote, "hooks"), "post-receive", pubHookConf{Run: [][]string{{tl.realGit, "--git-dir=" + e.remote, "update-ref", "refs/heads/" + pubBranch, e.base}}})
	rep := pubWant(t, e.publishF(e.prFlags("create")), 7, "mismatch", "push-mismatch")
	pubWantSteps(t, rep, "done", "mismatch", "skipped")
	if rep.Steps.Push.RemoteRef != e.base {
		t.Errorf("push remote_ref %q, want the observed %s", rep.Steps.Push.RemoteRef, e.base)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 {
		t.Errorf("gh create ran after push-mismatch")
	}
}

// pubTestRemoteMovedAtPush: R15 re-read, the remote moved during the commit
// step (--expect-remote absent) refuses the push and keeps the commit; the
// same command with the new expectation continues (commit already-present).
func pubTestRemoteMovedAtPush(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Run: [][]string{{tl.realGit, "--git-dir=" + e.remote, "update-ref", "refs/heads/" + pubBranch, e.base}}})
	rep := pubWant(t, e.publish("--expect-remote", "absent"), 4, "refused", "remote-moved")
	pubWantSteps(t, rep, "done", "refused", "not-requested")
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, want)
	if e.remoteRef(e.remote, pubBranch) != e.base {
		t.Errorf("remote changed by a refused push")
	}
	e.checkLastRun(e.entries(), rep, 4, e.msg)
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{})
	rep = pubWant(t, e.publish("--expect-remote", e.base), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "done", "not-requested")
	if e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("remote not at %s", c)
	}
}

// pubTestLockedAtPush: R15 re-read, a Git lock that appears after the commit
// refuses the push with locks_left.
func pubTestLockedAtPush(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	lock := filepath.Join(e.gitDir, "pube2e.lock")
	e.hook(filepath.Join(e.gitDir, "hooks"), "post-commit", pubHookConf{Create: []string{lock}})
	rep := pubWant(t, e.publish(), 4, "refused", "git-locked")
	pubWantSteps(t, rep, "done", "refused", "not-requested")
	pubWantStrings(t, "locks_left", rep.LocksLeft, []string{lock})
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote changed")
	}
}

// pubTestSplitAtPush: R15 re-read of R11.g after the commit.
func pubTestSplitAtPush(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	second := filepath.Join(e.root, "second.git")
	e.gitIn(e.root, "init", "-q", "--bare", second)
	e.hook(filepath.Join(e.gitDir, "hooks"), "post-commit", pubHookConf{Run: [][]string{{tl.realGit, "-C", e.repo, "config", "--add", "remote.origin.pushurl", second}}})
	rep := pubWant(t, e.publish(), 4, "refused", "remote-split")
	pubWantSteps(t, rep, "done", "refused", "not-requested")
	if e.remoteRef(e.remote, pubBranch) != "absent" || e.refsOf(second) != "" {
		t.Errorf("a repository gained a ref")
	}
}

// commitReRead runs a create whose precondition PR list runs commands (after
// every precondition passed, before the started entry) and returns the
// Report of the run that the commit step's re-read must stop.
func (e *pubEnv) commitReRead(commands [][]string, code int, status, reason string) *pubReport {
	t := e.t
	t.Helper()
	e.setGH(func(s *pubGHState) { s.OnList = commands })
	indexBefore := e.git("ls-files", "-s", "-z")
	r := e.publishF(e.prFlags("create"))
	rep := pubWant(t, r, code, status, reason)
	pubWantSteps(t, rep, "refused", "skipped", "skipped")
	if r.git != (pubGitCount{}) {
		t.Errorf("state-changing git commands ran after a refused re-read: %+v", r.git)
	}
	if e.rev("HEAD") != e.base {
		t.Errorf("HEAD moved to %s", e.rev("HEAD"))
	}
	if reason != "head-moved" {
		if got := e.git("ls-files", "-s", "-z"); got != indexBefore {
			t.Errorf("index changed (intent-to-add entries?) by a refused commit step")
		}
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote changed")
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 {
		t.Errorf("gh create ran")
	}
	e.checkLastRun(e.entries(), rep, code, e.msg)
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, nil)
	return rep
}

func pubTestCommitReReadSource(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.commitReRead([][]string{{filepath.Join(tl.utilDir, "pubappend"), filepath.Join(e.repo, "a.txt"), "late edit\n"}}, 3, "denied", "source-changed")
}

func pubTestCommitReReadHead(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	moved := strings.TrimSpace(e.git("commit-tree", e.base+"^{tree}", "-p", e.base, "-m", "moved meanwhile"))
	e.setGH(func(s *pubGHState) {
		s.OnList = [][]string{{tl.realGit, "-C", e.repo, "update-ref", "refs/heads/" + pubBranch, moved}}
	})
	rep := pubWant(t, e.publishF(e.prFlags("create")), 4, "refused", "head-moved")
	pubWantSteps(t, rep, "refused", "skipped", "skipped")
	if got := e.rev("HEAD"); got != moved {
		t.Errorf("HEAD %s, want the commit moved in by someone else %s", got, moved)
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote changed")
	}
	e.checkLastRun(e.entries(), rep, 4, e.msg)
}

func pubTestCommitReReadLock(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	lock := filepath.Join(e.gitDir, "index.lock")
	rep := e.commitReRead([][]string{{filepath.Join(tl.utilDir, "pubtouch"), lock}}, 4, "refused", "git-locked")
	pubWantStrings(t, "locks_left", rep.LocksLeft, []string{lock})
}

func pubTestCommitReReadAbandoned(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.commitReRead([][]string{{tl.cw, "task", "abandon", "--task", e.task, "--reason", "abandoned during publication"}}, 3, "denied", "task-abandoned")
}

// pubTestPushReReadAbandoned: R6.f at the push step's re-read. A post-commit
// hook abandons the task after git commit: exit 3 task-abandoned, push step
// refused, PR step skipped; the commit keeps its effect and nothing is
// pushed or created.
func pubTestPushReReadAbandoned(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "post-commit", pubHookConf{Run: [][]string{{tl.cw, "task", "abandon", "--task", e.task, "--reason", "abandoned after the commit"}}})
	r := e.publishF(e.prFlags("create"))
	rep := pubWant(t, r, 3, "denied", "task-abandoned")
	pubWantSteps(t, rep, "done", "refused", "skipped")
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, want)
	if r.git.commit != 1 || r.git.push != 0 {
		t.Errorf("state-changing git commands %+v, want the commit and no push", r.git)
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote changed after a denied push re-read")
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 {
		t.Errorf("gh create %d edit %d after a denied push re-read, want none", n.create, n.edit)
	}
	e.checkLastRun(e.entries(), rep, 3, e.msg)
}

// pubTestPRReReadAbandoned: R6.f at the PR step's re-read. A pre-push hook
// abandons the task during git push: exit 3 task-abandoned, commit and push
// done and kept, PR step refused with no gh pr create.
func pubTestPRReReadAbandoned(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{Run: [][]string{{tl.cw, "task", "abandon", "--task", e.task, "--reason", "abandoned during the push"}}})
	r := e.publishF(e.prFlags("create"))
	rep := pubWant(t, r, 3, "denied", "task-abandoned")
	pubWantSteps(t, rep, "done", "done", "refused")
	if got := e.remoteRef(e.remote, pubBranch); got != rep.Steps.Commit.Commit {
		t.Errorf("remote %s, want the pushed %s kept", got, rep.Steps.Commit.Commit)
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 {
		t.Errorf("gh create %d edit %d after a denied PR re-read, want none", n.create, n.edit)
	}
	if s := e.loadGH(); len(s.PRs) != 0 {
		t.Errorf("PRs %+v exist after a denied PR re-read", s.PRs)
	}
	e.checkLastRun(e.entries(), rep, 3, e.msg)
}

// pubTestDEmpty: R13.c, every authorized path already equals --expect-head:
// no commit (not-needed, C is --expect-head), the push still runs; R20.a
// re-run.
func pubTestDEmpty(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.write("a.txt", "dirty before start\nline two\n")
	e.startTask(pubPlanSmall)
	e.write("a.txt", pubBaseline["a.txt"])
	e.verifyTask()
	r := e.publish()
	rep := pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "not-needed", "done", "not-requested")
	if rep.Steps.Commit.Commit != e.base || e.rev("HEAD") != e.base || e.remoteRef(e.remote, pubBranch) != e.base {
		t.Errorf("commit %s HEAD %s remote %s, want all %s", rep.Steps.Commit.Commit, e.rev("HEAD"), e.remoteRef(e.remote, pubBranch), e.base)
	}
	if want := (pubGitCount{push: 1, pushRefspec: e.base + ":refs/heads/" + pubBranch}); r.git != want {
		t.Errorf("state-changing git commands %+v, want only the push %+v", r.git, want)
	}
	pubWantStrings(t, "paths", rep.Paths, []string{"a.txt"})
	pubWantStrings(t, "committed_paths", rep.CommittedPaths, nil)
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, nil)
	r = e.publish()
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "not-needed", "already-present", "not-requested")
	if r.git != (pubGitCount{}) {
		t.Errorf("re-run ran state-changing git commands %+v (R20.a)", r.git)
	}
}

// pubTestCommitUnknown: R14.a, a lock left after the failed commit is
// unknown (6) with locks_left; R17, nothing further runs.
func pubTestCommitUnknown(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	headLock := filepath.Join(e.gitDir, "HEAD.lock")
	log := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Create: []string{headLock}, Exit: 1})
	rep := pubWant(t, e.publishF(e.prFlags("create")), 6, "unknown", "commit-unknown")
	pubWantSteps(t, rep, "unknown", "skipped", "skipped")
	pubWantStrings(t, "locks_left", rep.LocksLeft, []string{headLock})
	if rep.Steps.Commit.Commit != e.base || e.rev("HEAD") != e.base {
		t.Errorf("commit step commit %q HEAD %s, want the observed HEAD %s", rep.Steps.Commit.Commit, e.rev("HEAD"), e.base)
	}
	if n := len(pubCalls(t, log)); n != 1 {
		t.Errorf("git commit ran %d times, want 1 (R17)", n)
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("pushed after commit-unknown")
	}
	if n := e.ghCheck(e.repo, pubTitle); n.create != 0 {
		t.Errorf("gh create after commit-unknown")
	}
	e.checkLastRun(e.entries(), rep, 6, e.msg)
}

// pubTestCommitFailed: R14.b (commit-failed, HEAD unchanged), R21.a and
// R21.b (a run that changed no repository state leaves verify's result).
func pubTestCommitFailed(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.write("a.txt", "line one\nline two\nline three\n")
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Exit: 1})
	_, complete, problems := e.verifySummary(e.task)
	show := e.cwOK("task", "show", "--task", e.task)
	records := e.taskRecordFiles()
	index := e.git("ls-files", "-s", "-z")
	rep := pubWant(t, e.publish(), 1, "failed", "commit-failed")
	pubWantSteps(t, rep, "failed", "skipped", "not-requested")
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, nil)
	if e.rev("HEAD") != e.base || e.git("ls-files", "-s", "-z") != index || e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("commit-failed changed HEAD, the index or the remote")
	}
	e.checkLastRun(e.entries(), rep, 1, e.msg)
	if d := pubDiffMaps(records, e.taskRecordFiles()); len(d) > 0 {
		t.Errorf("task record files changed: %q", d)
	}
	if got := e.cwOK("task", "show", "--task", e.task); got != show {
		t.Errorf("cw task show changed")
	}
	_, complete2, problems2 := e.verifySummary(e.task)
	if complete2 != complete || !slices.Equal(problems2, problems) {
		t.Errorf("verify after the run: complete %v problems %q, want %v %q", complete2, problems2, complete, problems)
	}
}

// pubTestCommitFailedNewPaths: R14.b and R13.e, intent_to_add lists the
// paths passed and every registered intent-to-add entry is one of them.
func pubTestCommitFailedNewPaths(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Exit: 1})
	before := e.lsFiles()
	rep := pubWant(t, e.publish(), 1, "failed", "commit-failed")
	pubWantSteps(t, rep, "failed", "skipped", "not-requested")
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, pubStandardNew)
	for _, p := range e.lsFiles() {
		if !slices.Contains(before, p) && !slices.Contains(rep.IntentToAdd, p) {
			t.Errorf("index gained %s, which is not in intent_to_add", p)
		}
	}
	if e.rev("HEAD") != e.base {
		t.Errorf("HEAD moved")
	}
}

// pubTestCommitMismatch: R14.d, a commit-msg hook changes the message: exit
// 6 with the observed HEAD, no push.
func pubTestCommitMismatch(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	e.hook(filepath.Join(e.gitDir, "hooks"), "commit-msg", pubHookConf{AppendArg1: "\nSigned-off-by: Hook <hook@example.invalid>\n"})
	prePush := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{})
	rep := pubWant(t, e.publish(), 7, "mismatch", "commit-mismatch")
	pubWantSteps(t, rep, "mismatch", "skipped", "not-requested")
	head := e.rev("HEAD")
	if rep.Steps.Commit.Commit != head || head == e.base {
		t.Errorf("commit step commit %q, want the observed new HEAD %s", rep.Steps.Commit.Commit, head)
	}
	if e.rev(head+"^") != e.base {
		t.Errorf("cw rewrote history")
	}
	if e.remoteRef(e.remote, pubBranch) != "absent" || len(pubCalls(t, prePush)) != 0 {
		t.Errorf("pushed after commit-mismatch")
	}
}

// pubTestCommitMismatchTree: R14.d for the committed tree, with the message
// file's bytes kept. A pre-commit hook re-stages one commit-set path in the
// index git commit is about to commit (the hook's GIT_INDEX_FILE), changing
// only its bytes or only its mode, removing a path verified as present
// (modified or new), or re-adding a path verified as deleted with its base
// bytes, so git commit succeeds with a commit that is not this request's
// commit (section 2: each path of D with its verified mode and bytes, absent
// when deleted). Fails when cw's readback ignores file bytes, file modes,
// presence or new paths, or compares against the index instead of the
// verified state.
// Expected (R14.d, section 6, R19): exit 7 commit-mismatch, commit step
// mismatch with the observed HEAD in steps.commit.commit, push and PR steps
// skipped, no git push and no pre-push hook, the remote's refs unchanged, no
// gh pr create, edit or view, no amend or reset (the branch's reflog holds
// the base and the one new commit, the worktree is untouched), and a started
// and a finished entry. A re-run of the same command is refused head-moved
// with nothing changed (R9.c: HEAD is neither --expect-head nor this
// request's commit), so the mismatching commit is never pushed as
// already-present.
func pubTestCommitMismatchTree(t *testing.T, tl *pubTools) {
	for _, c := range []struct {
		name, path, mode, data string
		remove                 bool // the hook removes path from the index instead
	}{
		{"bytes_of_modified_path", "a.txt", "100644", "line one\nline two\nline three, rewritten by a hook\n", false},
		{"bytes_of_new_path", "new.txt", "100644", "new file, rewritten by a hook\n", false},
		{"mode_of_modified_path_set_executable", "a.txt", "100755", "line one\nline two\nline three\n", false},
		{"mode_of_new_executable_path_cleared", "tool", "100644", "tool\n", false},
		{"presence_of_modified_path_removed", "a.txt", "", "", true},
		{"presence_of_new_path_removed", "new.txt", "", "", true},
		{"deleted_path_readded_with_base_bytes", "gone.txt", "100644", pubBaseline["gone.txt"], false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := pubNewEnv(t, tl)
			e.startTask(pubPlanSmall)
			want := e.standardChange()
			e.verifyTask()
			// The hook's entry differs from the verified state in exactly one
			// of bytes, mode and presence.
			v, inD := want[c.path]
			switch {
			case !inD:
				t.Fatalf("fixture: %s is not in the commit set", c.path)
			case c.remove:
				if v == nil {
					t.Fatalf("fixture: %s must be verified as present to be removed", c.path)
				}
			case v == nil:
				// A path verified as deleted, re-added by the hook.
			case (v.mode == c.mode) == (v.data == c.data):
				t.Fatalf("fixture: %s %s %q must differ from the verified %+v in bytes or mode only", c.path, c.mode, c.data, v)
			}
			var hookCmd []string
			if c.remove {
				hookCmd = []string{tl.realGit, "update-index", "--force-remove", "--", c.path}
			} else {
				// The blob is written to the object database before the run;
				// that changes neither the worktree nor the index.
				out, err := e.gitRun(e.repo, c.data, "hash-object", "-w", "--stdin")
				if err != nil {
					t.Fatal(err)
				}
				hookCmd = []string{tl.realGit, "update-index", "--cacheinfo", c.mode + "," + strings.TrimSpace(out) + "," + c.path}
				if v == nil {
					hookCmd = slices.Insert(hookCmd, 2, "--add")
				}
			}
			hooks := filepath.Join(e.gitDir, "hooks")
			preCommit := e.hook(hooks, "pre-commit", pubHookConf{RunHookEnv: [][]string{hookCmd}})
			prePush := e.hook(hooks, "pre-push", pubHookConf{})
			tree := pubTree(t, e.repo, true)
			remoteRefs := e.refsOf(e.remote)
			f := e.prFlags("create")

			r := e.publishF(f)
			rep := pubWant(t, r, 7, "mismatch", "commit-mismatch")
			pubWantSteps(t, rep, "mismatch", "skipped", "skipped")
			head := e.rev("HEAD")
			if head == e.base || rep.Steps.Commit.Commit != head {
				t.Errorf("steps.commit.commit = %q, HEAD %s; want the observed new HEAD (section 6)", rep.Steps.Commit.Commit, head)
			}
			// Fixture check: the hook's commit is this request's commit except
			// at c.path, so only the bytes, the mode or the presence there can
			// tell them apart. A path the hook leaves equal to --expect-head's
			// tree (a removed new path, a deleted path re-added with its base
			// bytes) is not among the paths the commit changes.
			hooked := maps.Clone(want)
			baseData, inBase := pubBaseline[c.path]
			switch {
			case c.remove && inBase:
				hooked[c.path] = nil
			case c.remove, inBase && c.mode == "100644" && c.data == baseData:
				delete(hooked, c.path)
			default:
				hooked[c.path] = &pubFile{c.mode, c.data}
			}
			e.wantRequestCommit(head, e.base, pubMessage, hooked)
			if n := len(pubCalls(t, preCommit)); n != 1 {
				t.Errorf("pre-commit ran %d times, want 1 (one git commit, R17)", n)
			}
			// No amend, reset or second commit by cw.
			reflog := strings.Fields(e.git("log", "-g", "--format=%H", "refs/heads/"+pubBranch))
			pubWantStrings(t, "reflog of "+pubBranch, reflog, []string{head, e.base})
			if d := pubDiffMaps(tree, pubTree(t, e.repo, true)); len(d) > 0 {
				t.Errorf("worktree paths %q changed after commit-mismatch (no reset)", d)
			}
			if r.git.commit != 1 || r.git.push != 0 {
				t.Errorf("state-changing git commands %+v, want one commit and no push", r.git)
			}
			pubWantStrings(t, "paths", rep.Paths, pubStandardD)
			pubWantStrings(t, "committed_paths", rep.CommittedPaths, pubStandardD)
			pubWantStrings(t, "intent_to_add", rep.IntentToAdd, pubStandardNew)
			// Nothing pushed, no PR command.
			if got := e.refsOf(e.remote); got != remoteRefs || len(pubCalls(t, prePush)) != 0 {
				t.Errorf("pushed after commit-mismatch: remote refs %q, want %q; pre-push calls %d", got, remoteRefs, len(pubCalls(t, prePush)))
			}
			if n := e.ghCheck(e.repo, pubTitle); n.create != 0 || n.edit != 0 || n.view != 0 {
				t.Errorf("gh create %d edit %d view %d after commit-mismatch, want none (no PR step)", n.create, n.edit, n.view)
			}
			if s := e.loadGH(); len(s.PRs) != 0 {
				t.Errorf("PRs %+v exist after commit-mismatch", s.PRs)
			}
			entries := e.entries()
			if len(entries) != 2 {
				t.Errorf("%d publication entries, want a started and a finished entry", len(entries))
			}
			e.checkLastRun(entries, rep, 7, e.msg)
			if rep.PreviousOutcome != "none" {
				t.Errorf("previous_outcome %q, want none", rep.PreviousOutcome)
			}

			again := e.refuse("re-run after commit-mismatch", f, 4, "refused", "head-moved")
			if again.PreviousOutcome != "mismatch" {
				t.Errorf("re-run previous_outcome %q, want mismatch (R20.d)", again.PreviousOutcome)
			}
			if e.rev("HEAD") != head || e.remoteRef(e.remote, pubBranch) != "absent" {
				t.Errorf("re-run moved HEAD to %s or pushed (remote %s)", e.rev("HEAD"), e.remoteRef(e.remote, pubBranch))
			}
		})
	}
}

// pubTestCommitTimeout: R14.e and R18.a, a commit that exceeds --timeout is
// stopped so git removes index.lock, and is commit-failed.
func pubTestCommitTimeout(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("pre-commit")
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Started: started, Release: release})
	// The bound is generous for every other command of the run; only the
	// pre-commit hook, which blocks until it is stopped, can reach it.
	rep := pubWant(t, e.publish("--timeout", pubBlockedTimeout), 1, "failed", "commit-failed")
	pubWantSteps(t, rep, "failed", "skipped", "not-requested")
	if _, err := os.Stat(started); err != nil {
		t.Errorf("the pre-commit hook never ran: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(e.gitDir, "index.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".git/index.lock exists after the stopped commit")
	}
	if e.rev("HEAD") != e.base {
		t.Errorf("HEAD moved")
	}
}

// pubTestHookBackgroundJob: a post-commit hook may start a background job on
// purpose, such as regenerating tags; git commit exits without waiting for it,
// and publish leaves it running, as a commit from a shell would. Oracle: the
// job holds the write end of a FIFO that the hook opened and wrote "held" to.
// After publish returns, the test reads "held" and then sees no end of file
// within 2 s, so a writer is still alive; the job would live 60 s. Had publish
// killed git's process group, the reader would reach end of file at once.
func pubTestHookBackgroundJob(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	dir := t.TempDir()
	fifo, pidFile := filepath.Join(dir, "held"), filepath.Join(dir, "job.pid")
	held, ended := pubWatchFIFO(t, fifo)
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	job := `exec 3>"$1"; echo held >&3; /bin/sleep 60 </dev/null >/dev/null 2>&1 & echo "$!" > "$2"`
	e.hook(filepath.Join(e.gitDir, "hooks"), "post-commit", pubHookConf{Run: [][]string{{"/bin/sh", "-c", job, "sh", fifo, pidFile}}})
	pubWant(t, e.publish(), 0, "published", "")
	select {
	case got := <-held:
		if got != "held\n" {
			t.Fatalf("the FIFO gave %q, want %q: the hook did not start its job", got, "held\n")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the hook never wrote to the FIFO")
	}
	select {
	case <-ended:
		t.Error("the hook's background job had exited when publish returned, want it still running")
	case <-time.After(2 * time.Second):
	}
}

// pubWatchFIFO makes a FIFO and reads it in the background with blocking
// calls, which work on every Unix (macOS cannot poll a FIFO, so it has no
// read deadline): the first five bytes arrive on held, and ended is closed at
// end of file, once every writer has exited. Cleanup releases a reader still
// waiting for a writer to open it.
func pubWatchFIFO(t *testing.T, path string) (<-chan string, <-chan struct{}) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	held, ended := make(chan string, 1), make(chan struct{})
	go func() {
		defer close(ended)
		file, err := os.Open(path)
		if err != nil {
			held <- err.Error()
			return
		}
		defer file.Close()
		first := make([]byte, 5)
		n, _ := io.ReadFull(file, first)
		held <- string(first[:n])
		_, _ = io.Copy(io.Discard, file)
	}()
	t.Cleanup(func() {
		if writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
	})
	return held, ended
}

// pubTestInterruptCommit: R18.c, SIGINT during git commit stops it; HEAD
// unchanged and no locks: 130 cancelled with commit step failed; R19 the
// finished entry is written; the same command then continues.
func pubTestInterruptCommit(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	started, release := e.blocker("pre-commit")
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Started: started, Release: release})
	p := e.startCW(nil, e.flags().argv()...)
	pubWaitMarker(t, p, started)
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	rep := pubWant(t, e.waitRun(p), 130, "cancelled", "cancelled")
	pubWantSteps(t, rep, "failed", "skipped", "not-requested")
	if _, err := os.Lstat(filepath.Join(e.gitDir, "index.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".git/index.lock exists after the interrupted commit")
	}
	if e.rev("HEAD") != e.base {
		t.Errorf("HEAD moved")
	}
	e.checkLastRun(e.entries(), rep, 130, e.msg)
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{})
	rep = pubWant(t, e.publish(), 0, "published", "")
	if rep.PreviousOutcome != "cancelled" {
		t.Errorf("previous_outcome %q, want cancelled", rep.PreviousOutcome)
	}
	e.wantRequestCommit(rep.Steps.Commit.Commit, e.base, pubMessage, want)
}

// pubTestInterruptPush: R18.c, SIGINT during git push gives 6 push-unknown;
// R20.c, with locks_left empty the same command continues.
func pubTestInterruptPush(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("pre-push")
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{Started: started, Release: release})
	p := e.startCW(nil, e.flags().argv()...)
	pubWaitMarker(t, p, started)
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	rep := pubWant(t, e.waitRun(p), 6, "unknown", "push-unknown")
	pubWantSteps(t, rep, "done", "unknown", "not-requested")
	if rep.Steps.Push.RemoteRef != "absent" || e.remoteRef(e.remote, pubBranch) != "absent" {
		t.Errorf("remote_ref %q remote %s, want absent", rep.Steps.Push.RemoteRef, e.remoteRef(e.remote, pubBranch))
	}
	e.checkLastRun(e.entries(), rep, 6, e.msg)
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{})
	if len(rep.LocksLeft) != 0 {
		t.Logf("locks_left %q after the stopped push; R20.c continuation not exercised", rep.LocksLeft)
		return
	}
	rep = pubWant(t, e.publish(), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "done", "not-requested")
}

// pubTestInterruptReadOnly: R18.b, SIGINT while only a read-only command
// runs (gh repo view) exits 130 with nothing changed and no record.
func pubTestInterruptReadOnly(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("gh-repo-view")
	e.setGH(func(s *pubGHState) { s.RepoMode, s.Started, s.Release = "block", started, release })
	before := e.snap()
	p := e.startCW(nil, e.prFlags("create").argv()...)
	pubWaitMarker(t, p, started)
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	rep := pubWant(t, e.waitRun(p), 130, "cancelled", "cancelled")
	pubWantSteps(t, rep, "skipped", "skipped", "skipped")
	if rep.Record != "" {
		t.Errorf("record %q, want none before the started entry", rep.Record)
	}
	e.mustUnchanged("interrupted precondition", before)
}

// pubTestReadOnlyTimeout: R18.a, a read-only command over --timeout is
// stopped and exits 1 read-failed with nothing changed.
func pubTestReadOnlyTimeout(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("gh-repo-view")
	e.setGH(func(s *pubGHState) { s.RepoMode, s.Started, s.Release = "block", started, release })
	rep := e.refuse("gh repo view over the timeout", e.prFlags("create").add("--timeout", pubBlockedTimeout), 1, "failed", "read-failed")
	pubWantSteps(t, rep, "skipped", "skipped", "skipped")
}

// pubTestKilledRun: R19 (a killed run leaves its started entry only), R5
// (the kernel releases the lock of a killed process, so the next run is not
// locked out), R20.d (unfinished).
func pubTestKilledRun(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	started, release := e.blocker("pre-commit")
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Started: started, Release: release, Exit: 1})
	p := e.startCW(nil, e.flags().argv()...)
	pubWaitMarker(t, p, started)
	if err := p.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.waitRun(p); r.code != -int(syscall.SIGKILL) {
		t.Fatalf("cw exit %d, want killed", r.code)
	}
	if !pubWaitGone(filepath.Join(e.gitDir, "index.lock"), pubDeadline) {
		t.Fatalf("git commit did not finish after the hook was released")
	}
	entries := e.entries()
	if len(entries) != 1 || entries[0].Kind != "started" {
		t.Fatalf("entries after a killed run: %d, want one started entry", len(entries))
	}
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{})
	rep := pubWant(t, e.publish(), 0, "published", "")
	if rep.PreviousOutcome != "unfinished" {
		t.Errorf("previous_outcome %q, want unfinished", rep.PreviousOutcome)
	}
	e.wantRequestCommit(rep.Steps.Commit.Commit, e.base, pubMessage, want)
	if got := e.entries(); len(got) != 3 {
		t.Errorf("%d entries, want 3 (started, started, finished)", len(got))
	} else {
		e.checkLastRun(got, rep, 0, e.msg)
	}
}

// pubTestConcurrentLocked: R5.a, a second run while the first holds
// .publish-lock exits 1 locked and changes nothing; the first completes.
func pubTestConcurrentLocked(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	started, release := e.blocker("pre-commit")
	e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{Started: started, Release: release})
	p := e.startCW(nil, e.flags().argv()...)
	pubWaitMarker(t, p, started)
	taskBefore := pubTree(t, e.task, false)
	rep := pubWant(t, e.publish(), 1, "failed", "locked")
	if rep.Record != "" {
		t.Errorf("locked run record %q", rep.Record)
	}
	if d := pubDiffMaps(taskBefore, pubTree(t, e.task, false)); len(d) > 0 {
		t.Errorf("locked run changed task entries %q", d)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rep1 := pubWant(t, e.waitRun(p), 0, "published", "")
	if got := e.entries(); len(got) != 2 {
		t.Errorf("%d entries, want the first run's 2", len(got))
	} else {
		e.checkLastRun(got, rep1, 0, e.msg)
	}
}

// pubTestRecordInvalid: R5.b, a publication log that violates section 7
// exits 1 record-invalid and changes nothing; restored, it is valid again.
func pubTestRecordInvalid(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	pubWant(t, e.publish(), 0, "published", "")
	f1, f2 := filepath.Join(e.task, "publication-000001.json"), filepath.Join(e.task, "publication-000002.json")
	orig1, err := os.ReadFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	orig2, err := os.ReadFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() {
		pubWriteFile(t, f1, orig1, 0o600)
		pubWriteFile(t, f2, orig2, 0o600)
		_ = os.Remove(filepath.Join(e.task, "publication-000003.json"))
	}
	edit := func(data []byte, fn func(map[string]any)) []byte {
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err != nil {
			t.Fatal(err)
		}
		fn(obj)
		out, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	cases := []struct {
		name   string
		break_ func()
	}{
		{"not private", func() { _ = os.Chmod(f2, 0o644) }},
		{"gap", func() { _ = os.Rename(f2, filepath.Join(e.task, "publication-000003.json")) }},
		{"broken chain", func() { pubWriteFile(t, f1, append(slices.Clone(orig1), ' '), 0o600) }},
		{"unknown member", func() { pubWriteFile(t, f2, edit(orig2, func(m map[string]any) { m["extra"] = true }), 0o600) }},
		{"unknown version", func() { pubWriteFile(t, f2, edit(orig2, func(m map[string]any) { m["version"] = 2 }), 0o600) }},
		{"wrong task", func() {
			pubWriteFile(t, f2, edit(orig2, func(m map[string]any) { m["task_id"] = strings.Repeat("0", 32) }), 0o600)
		}},
		{"wrong sequence", func() { pubWriteFile(t, f2, edit(orig2, func(m map[string]any) { m["sequence"] = 7 }), 0o600) }},
		{"stray entry", func() {
			pubWriteFile(t, filepath.Join(e.task, "publication-000003.json"), []byte("{}\n"), 0o600)
		}},
	}
	for _, c := range cases {
		c.break_()
		e.refuse(c.name, e.flags(), 1, "failed", "record-invalid")
		restore()
	}
	rep := pubWant(t, e.publish(), 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
}

// pubTestTaskUnreadable: R5, a task directory that cannot be opened.
func pubTestTaskUnreadable(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	missing := filepath.Join(e.root, "no such task")
	pubWant(t, e.publishF(e.flags().set("--task", missing)), 1, "failed", "task-unreadable")
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a task directory was created at %s", missing)
	}
	file := filepath.Join(e.root, "task-file")
	pubWriteFile(t, file, []byte("not a directory\n"), 0o600)
	pubWant(t, e.publishF(e.flags().set("--task", file)), 1, "failed", "task-unreadable")
}

// pubTestReruns: R20.a, the same command after exit 0 runs no state-changing
// command and appends two entries; after someone pushes on top of C, R11
// decides (not-fast-forward).
func pubTestReruns(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	// The same command keeps --expect-remote absent: once the remote holds C,
	// R11.b makes R11.c and R11.d not apply.
	rep := pubWant(t, e.publish("--expect-remote", "absent"), 0, "published", "")
	c := rep.Steps.Commit.Commit
	preCommit := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{})
	prePush := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{})
	refs := e.refsOf(e.remote)
	r := e.publish("--expect-remote", "absent")
	rep = pubWant(t, r, 0, "published", "")
	pubWantSteps(t, rep, "already-present", "already-present", "not-requested")
	if r.git != (pubGitCount{}) {
		t.Errorf("re-run ran state-changing git commands %+v (R20.a)", r.git)
	}
	if rep.Steps.Commit.Commit != c || rep.Steps.Push.RemoteRef != c || rep.PreviousOutcome != "published" {
		t.Errorf("re-run commit %q remote_ref %q previous %q", rep.Steps.Commit.Commit, rep.Steps.Push.RemoteRef, rep.PreviousOutcome)
	}
	pubWantStrings(t, "committed_paths", rep.CommittedPaths, pubStandardD)
	pubWantStrings(t, "intent_to_add", rep.IntentToAdd, nil)
	if e.rev("HEAD") != c || e.refsOf(e.remote) != refs || len(pubCalls(t, preCommit)) != 0 || len(pubCalls(t, prePush)) != 0 {
		t.Errorf("re-run changed state or ran git commit or push")
	}
	entries := e.entries()
	if len(entries) != 4 {
		t.Fatalf("%d entries, want 4", len(entries))
	}
	e.checkLastRun(entries, rep, 0, e.msg)
	e.cloneAndPush(c, pubBranch)
	e.refuse("pushed on top of C", e.flags(), 4, "refused", "not-fast-forward")
	e.refuse("pushed on top of C, original expectation", e.flags().add("--expect-remote", "absent"), 4, "refused", "remote-moved")
}

// pubTestLadder: R3, with many conditions holding at once cw reports the
// first in section 5's order; removing it reveals the next. Every refusal
// changes nothing and writes no record (I5).
func pubTestLadder(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.gh = false
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.write("crlf.txt", "one\r\ntwo\r\n")
	e.write("a.txt", "line one\npartial\n")
	e.git("add", "a.txt")
	e.write("a.txt", "line one\nline two\nline three\n")
	lockDir := filepath.Join(e.task, ".publish-lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := e.prFlags("create").add("--path", "u.txt").set("--expect-head", strings.Repeat("ab", 20)).set("--push", "nosuch").add("--expect-remote", strings.Repeat("cd", 20))
	e.refuse("locked before everything", f, 1, "failed", "locked")
	if err := os.Remove(lockDir); err != nil {
		t.Fatal(err)
	}
	e.refuse("unverified", f, 3, "denied", "task-not-verified")
	e.verifyTask()
	otherDir, _ := e.startOther(pubPlanOther, e.repo, "")
	e.git("branch", "other")
	e.git("symbolic-ref", "HEAD", "refs/heads/other")
	e.git("config", "core.autocrlf", "true")
	branchLock := filepath.Join(e.gitDir, "refs", "heads", pubBranch+".lock")
	pubWriteFile(t, branchLock, nil, 0o644)
	second := filepath.Join(e.root, "second.git")
	e.gitIn(e.root, "init", "-q", "--bare", second)
	e.git("config", "remote.origin.pushurl", second)
	e.setGH(func(s *pubGHState) {
		s.RepoURL, s.SSHURL = "https://github.example/someone/else", "git@github.example:someone/else.git"
		s.PRs = []pubPR{pubOpenPR(3, "Not equivalent", pubBase, pubBody, false)}
	})
	e.refuse("open record", f, 3, "denied", "open-record")
	e.cwOK("task", "abandon", "--task", otherDir, "--reason", "not needed")
	e.refuse("path outside CS", f, 4, "refused", "path-not-verified")
	f = f.drop("--path")
	e.refuse("other branch", f, 4, "refused", "branch-mismatch")
	e.git("symbolic-ref", "HEAD", "refs/heads/"+pubBranch)
	e.refuse("unknown expect-head", f, 4, "refused", "head-moved")
	f = f.set("--expect-head", e.base)
	e.refuse("partial staging", f, 4, "refused", "hunk-scope")
	e.git("add", "a.txt")
	e.refuse("autocrlf with CR bytes", f, 4, "refused", "content-conversion")
	e.git("config", "core.autocrlf", "false")
	rep := e.refuse("branch lock", f, 4, "refused", "git-locked")
	pubWantStrings(t, "locks_left", rep.LocksLeft, []string{branchLock})
	if err := os.Remove(branchLock); err != nil {
		t.Fatal(err)
	}
	e.refuse("unknown remote", f, 4, "refused", "remote-unknown")
	f = f.set("--push", "origin")
	e.refuse("split remote", f, 4, "refused", "remote-split")
	e.git("config", "--unset", "remote.origin.pushurl")
	e.refuse("remote moved", f, 4, "refused", "remote-moved")
	f = f.set("--expect-remote", "absent")
	e.refuse("gh missing", f, 4, "refused", "gh-missing")
	e.gh = true
	e.refuse("repo mismatch", f, 4, "refused", "repo-mismatch")
	e.setGH(func(s *pubGHState) { s.RepoURL = e.remote })
	e.refuse("pr exists", f, 4, "refused", "pr-exists")
	if calls := pubCalls(t, e.ghLog); slices.ContainsFunc(calls, func(c pubCall) bool { return len(c.Args) > 1 && (c.Args[1] == "create" || c.Args[1] == "edit") }) {
		t.Errorf("gh create or edit ran during refusals")
	}
	if names := pubEntryNames(t, e.task); len(names) != 0 {
		t.Errorf("refusals wrote publication files %q", names)
	}
	e.setGH(func(s *pubGHState) { s.PRs = nil })
	rep = pubWant(t, e.publishF(f), 0, "published", "")
	pubWantSteps(t, rep, "done", "done", "done")
	if e.refsOf(second) != "" {
		t.Errorf("second repository gained refs")
	}
}

// pubTestGitMissing: R4, git not on PATH exits 1 git-missing, after usage
// errors and before task-unreadable.
func pubTestGitMissing(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	noGit := "PATH=" + tl.ghDir
	before := e.snap()
	pubWant(t, e.publishF(e.flags(), noGit), 1, "failed", "git-missing")
	pubWant(t, e.publishF(e.flags().set("--task", filepath.Join(e.root, "no such task")), noGit), 1, "failed", "git-missing")
	pubWant(t, e.publishF(e.flags().set("--expect-head", "xyz"), noGit), 2, "", "")
	e.mustUnchanged("git missing", before)
	if n := len(pubCalls(t, e.ghLog)); n != 0 {
		t.Errorf("gh ran %d times", n)
	}
}

// pubTestEnvIsolation: R22.a, exported repository-selecting variables are
// removed for git and gh: the worktree's own repository, index and remote
// branch are used, the decoy repository and the named index file are
// untouched, no namespaced ref is pushed; R22.b for gh.
func pubTestEnvIsolation(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	want := e.standardChange()
	e.verifyTask()
	decoy := filepath.Join(e.root, "decoy")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatal(err)
	}
	e.gitIn(decoy, "init", "-q")
	pubWriteFile(t, filepath.Join(decoy, "d.txt"), []byte("decoy\n"), 0o644)
	e.gitIn(decoy, "add", "d.txt")
	e.gitIn(decoy, "commit", "-q", "-m", "decoy")
	decoyRefs := e.gitIn(decoy, "for-each-ref", "--format=%(refname) %(objectname)")
	decoyIndex := e.gitIn(decoy, "ls-files", "-s")
	strayIndex := filepath.Join(e.root, "stray.index")
	hookLog := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-commit", pubHookConf{})
	pushLog := e.hook(filepath.Join(e.gitDir, "hooks"), "pre-push", pubHookConf{})
	exported := []string{
		"GIT_DIR=" + filepath.Join(decoy, ".git"), "GIT_WORK_TREE=" + decoy, "GIT_INDEX_FILE=" + strayIndex,
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(e.root, "stray objects"), "GIT_ALTERNATE_OBJECT_DIRECTORIES=" + filepath.Join(e.root, "stray alternates"),
		"GIT_COMMON_DIR=" + filepath.Join(decoy, ".git"), "GIT_NAMESPACE=pubstray", "GIT_CEILING_DIRECTORIES=" + e.root,
		"GIT_DISCOVERY_ACROSS_FILESYSTEM=1",
	}
	e.extra = exported
	rep := pubWant(t, e.publishF(e.prFlags("create")), 0, "published", "")
	e.extra = nil
	c := rep.Steps.Commit.Commit
	e.wantRequestCommit(c, e.base, pubMessage, want)
	if e.rev("HEAD") != c || e.remoteRef(e.remote, pubBranch) != c {
		t.Errorf("HEAD %s remote %s, want %s in the worktree's own repository", e.rev("HEAD"), e.remoteRef(e.remote, pubBranch), c)
	}
	if refs := e.refsOf(e.remote); strings.Contains(refs, "refs/namespaces/") {
		t.Errorf("remote holds namespaced refs:\n%s", refs)
	}
	if got := e.gitIn(decoy, "for-each-ref", "--format=%(refname) %(objectname)"); got != decoyRefs {
		t.Errorf("decoy refs changed:\n%s", got)
	}
	if got := e.gitIn(decoy, "ls-files", "-s"); got != decoyIndex {
		t.Errorf("decoy index changed")
	}
	for _, p := range []string{strayIndex, filepath.Join(e.root, "stray objects"), filepath.Join(e.root, "stray alternates")} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was created", p)
		}
	}
	if st := e.git("status", "--porcelain"); st != "" {
		t.Errorf("the worktree's own index does not hold the commit:\n%s", st)
	}
	stripped := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM"}
	for _, call := range pubCalls(t, e.ghLog) {
		for _, k := range stripped {
			if v, ok := pubEnvValue(call.Env, k); ok {
				t.Errorf("gh %q received %s=%q", call.Args, k, v)
			}
		}
	}
	e.ghCheck(e.repo, pubTitle)
	// git sets some of these variables for its hooks itself; none may carry
	// the value exported to cw, for git commit (pre-commit) and git push
	// (pre-push) alike.
	for _, h := range []struct{ name, log string }{{"git commit", hookLog}, {"git push", pushLog}} {
		calls := pubCalls(t, h.log)
		if len(calls) != 1 {
			t.Fatalf("the hook of %s ran %d times, want 1", h.name, len(calls))
		}
		for _, kv := range exported {
			k, v, _ := strings.Cut(kv, "=")
			if got, ok := pubEnvValue(calls[0].Env, k); ok && (got == v || pubSamePath(got, v)) {
				t.Errorf("%s received the exported %s=%q (R22.a)", h.name, k, got)
			}
		}
		if v, _ := pubEnvValue(calls[0].Env, "GIT_TERMINAL_PROMPT"); v != "0" {
			t.Errorf("%s ran with GIT_TERMINAL_PROMPT=%q, want 0 (R22.b)", h.name, v)
		}
	}
}

// pubTestUsage: R1, every usage error exits 2 with empty stdout, one stderr
// line, no file in the task directory, no state change and gh never run;
// boundary values that are valid pass usage (reaching a later reason).
func pubTestUsage(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	w := func(name string, data []byte) string {
		p := filepath.Join(e.work, name)
		pubWriteFile(t, p, data, 0o644)
		return p
	}
	dir := filepath.Join(e.work, "a directory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bigMsg := w("big-message.txt", []byte("s\n"+strings.Repeat("x", 65536-2)+"\n")) // 65537 bytes
	exactMsg := w("exact-message.txt", []byte("s\n"+strings.Repeat("x", 65536-3)+"\n"))
	if fi, _ := os.Stat(exactMsg); fi.Size() != 65536 {
		t.Fatalf("exact message size %d", fi.Size())
	}
	if fi, _ := os.Stat(bigMsg); fi.Size() != 65537 {
		t.Fatalf("big message size %d", fi.Size())
	}
	badUTF8 := w("bad-utf8.txt", []byte("subject \xff\xfe\n"))
	nul := w("nul.txt", []byte("subject\x00\n"))
	noNL := w("no-newline.txt", []byte("subject"))
	blankFirst := w("blank-first.txt", []byte("\nbody\n"))
	bigBody := w("big-body.md", bytes.Repeat([]byte("b"), 65537))
	exactBody := w("exact-body.md", bytes.Repeat([]byte("b"), 65536))
	emptyBody := w("empty-body.md", nil)
	badBody := w("bad-body.md", []byte("body \xc3\x28\n"))
	base := e.flags()
	pr := e.prFlags("create")
	missing := filepath.Join(e.work, "missing.txt")
	cases := []struct {
		name string
		f    pubFlags
	}{
		{"no --task", base.drop("--task")},
		{"no --message-file", base.drop("--message-file")},
		{"no --expect-head", base.drop("--expect-head")},
		{"no --push", base.drop("--push")},
		{"no --branch", base.drop("--branch")},
		{"unknown flag", base.add("--bogus", "x")},
		{"positional argument", base.add("extra")},
		{"--dry-run", base.add("--dry-run")},
		{"create without --repo", pr.drop("--repo")},
		{"create without --base", pr.drop("--base")},
		{"create without --title", pr.drop("--title")},
		{"create without --body-file", pr.drop("--body-file")},
		{"edit without --repo", e.prFlags("edit").drop("--repo")},
		{"none with --repo", base.add("--pr", "none", "--repo", pubRepo)},
		{"default none with --title", base.add("--title", pubTitle)},
		{"default none with --base", base.add("--base", pubBase)},
		{"default none with --body-file", base.add("--body-file", e.body)},
		{"--pr bogus", pr.set("--pr", "draft")},
		{"expect-head short", base.set("--expect-head", "abc123")},
		{"expect-head 39", base.set("--expect-head", strings.Repeat("a", 39))},
		{"expect-head 41", base.set("--expect-head", strings.Repeat("a", 41))},
		{"expect-head 63", base.set("--expect-head", strings.Repeat("a", 63))},
		{"expect-head 65", base.set("--expect-head", strings.Repeat("a", 65))},
		{"expect-head uppercase", base.set("--expect-head", strings.ToUpper(e.base))},
		{"expect-head non-hex", base.set("--expect-head", strings.Repeat("g", 40))},
		{"expect-head empty", base.set("--expect-head", "")},
		{"expect-remote ABSENT", base.add("--expect-remote", "ABSENT")},
		{"expect-remote none", base.add("--expect-remote", "none")},
		{"expect-remote short", base.add("--expect-remote", "abc")},
		{"expect-remote uppercase", base.add("--expect-remote", strings.ToUpper(e.base))},
		{"branch leading dash", base.set("--branch", "-feature")},
		{"branch dotdot", base.set("--branch", "fea..ture")},
		{"branch space", base.set("--branch", "fea ture")},
		{"branch .lock", base.set("--branch", "feature.lock")},
		{"branch component .lock", base.set("--branch", "x.lock/y")},
		{"branch component dot", base.set("--branch", "feature/.x")},
		{"branch @", base.set("--branch", "@")},
		{"branch trailing slash", base.set("--branch", "feature/")},
		{"branch trailing dot", base.set("--branch", "feature.")},
		{"branch leading slash", base.set("--branch", "/feature")},
		{"branch double slash", base.set("--branch", "fea//ture")},
		{"branch @{", base.set("--branch", "fea@{ture")},
		{"branch tilde", base.set("--branch", "fea~1")},
		{"branch caret", base.set("--branch", "fea^")},
		{"branch colon", base.set("--branch", "fea:ture")},
		{"branch question", base.set("--branch", "fea?")},
		{"branch star", base.set("--branch", "fea*")},
		{"branch bracket", base.set("--branch", "fea[ture")},
		{"branch backslash", base.set("--branch", `fea\ture`)},
		{"branch control", base.set("--branch", "fea\x01ture")},
		{"branch DEL", base.set("--branch", "fea\x7fture")},
		{"branch tab", base.set("--branch", "fea\tture")},
		{"branch empty", base.set("--branch", "")},
		{"push leading dash", base.set("--push", "-origin")},
		{"push space", base.set("--push", "ori gin")},
		{"push dotdot", base.set("--push", "..")},
		{"base invalid", pr.set("--base", "ma..in")},
		{"base equals branch", pr.set("--base", pubBranch)},
		{"repo without slash", pr.set("--repo", "octo")},
		{"repo three parts", pr.set("--repo", "octo/widget/x")},
		{"repo owner dash", pr.set("--repo", "-octo/widget")},
		{"repo name dash", pr.set("--repo", "octo/-widget")},
		{"repo space", pr.set("--repo", "oc to/widget")},
		{"repo empty owner", pr.set("--repo", "/widget")},
		{"repo empty name", pr.set("--repo", "octo/")},
		{"repo colon", pr.set("--repo", "octo/wid:get")},
		{"title empty", pr.set("--title", "")},
		{"title leading space", pr.set("--title", " lead")},
		{"title trailing space", pr.set("--title", "trail ")},
		{"title trailing tab", pr.set("--title", "trail\t")},
		{"title newline", pr.set("--title", "two\nlines")},
		{"title CRLF", pr.set("--title", "two\r\nlines")},
		{"title 257", pr.set("--title", strings.Repeat("t", 257))},
		{"message missing", base.set("--message-file", missing)},
		{"message directory", base.set("--message-file", dir)},
		{"message 65537 bytes", base.set("--message-file", bigMsg)},
		{"message invalid UTF-8", base.set("--message-file", badUTF8)},
		{"message NUL", base.set("--message-file", nul)},
		{"message without newline", base.set("--message-file", noNL)},
		{"message blank first line", base.set("--message-file", blankFirst)},
		{"body missing", pr.set("--body-file", missing)},
		{"body directory", pr.set("--body-file", dir)},
		{"body 65537 bytes", pr.set("--body-file", bigBody)},
		{"body invalid UTF-8", pr.set("--body-file", badBody)},
		{"path empty", base.add("--path", "")},
		{"path absolute", base.add("--path", filepath.Join(e.repo, "a.txt"))},
		{"path dotdot segment", base.add("--path", "sub/../a.txt")},
		{"path dotdot", base.add("--path", "..")},
		{"path leading dotdot", base.add("--path", "../repo/a.txt")},
		{"path twice", base.add("--path", "a.txt", "--path", "a.txt")},
		{"timeout 0s", base.add("--timeout", "0s")},
		{"timeout 999ms", base.add("--timeout", "999ms")},
		{"timeout 2h0m1s", base.add("--timeout", "2h0m1s")},
		{"timeout 3h", base.add("--timeout", "3h")},
		{"timeout negative", base.add("--timeout", "-1s")},
		{"timeout no unit", base.add("--timeout", "10")},
		{"timeout word", base.add("--timeout", "soon")},
	}
	before := e.snap()
	for _, c := range cases {
		r := e.publishF(c.f)
		if r.code != 2 {
			t.Errorf("%s: exit %d, want 2\nstdout:\n%s\nstderr:\n%s", c.name, r.code, r.stdout, r.stderr)
			continue
		}
		pubWant(t, r, 2, "", "")
	}
	e.mustUnchanged("usage errors", before)
	if n := len(pubCalls(t, e.ghLog)); n != 0 {
		t.Errorf("gh ran %d times during usage errors", n)
	}
	// Valid boundary values pass usage and stop at remote-unknown (or the
	// named later reason) with nothing changed.
	nosuch := base.set("--push", "nosuch")
	prNosuch := pr.set("--push", "nosuch")
	accepted := []struct {
		name   string
		f      pubFlags
		reason string
	}{
		{"message 65536 bytes", nosuch.set("--message-file", exactMsg), "remote-unknown"},
		{"body empty", prNosuch.set("--body-file", emptyBody), "remote-unknown"},
		{"body 65536 bytes", prNosuch.set("--body-file", exactBody), "remote-unknown"},
		{"title 256", prNosuch.set("--title", strings.Repeat("t", 256)), "remote-unknown"},
		{"title starting with --", prNosuch.drop("--title").add("--title=--draft"), "remote-unknown"},
		{"title inner tab", prNosuch.set("--title", "a\tb"), "remote-unknown"},
		{"repo with dots and underscores", prNosuch.set("--repo", "o.c_t-o/w.i_d-g3t"), "remote-unknown"},
		{"edit mode", e.prFlags("edit").set("--push", "nosuch"), "remote-unknown"},
		{"explicit none", nosuch.add("--pr", "none"), "remote-unknown"},
		{"timeout 2h", nosuch.add("--timeout", "2h"), "remote-unknown"},
		{"timeout 1m30s", nosuch.add("--timeout", "1m30s"), "remote-unknown"},
		{"expect-head 64 hex", base.set("--expect-head", strings.Repeat("a", 64)), "head-moved"},
		{"expect-remote absent", nosuch.add("--expect-remote", "absent"), "remote-unknown"},
		{"expect-remote 64 hex", nosuch.add("--expect-remote", strings.Repeat("b", 64)), "remote-unknown"},
		{"path with dots that is not a segment", base.add("--path", "a..txt"), "path-not-verified"},
		{"branch with slash", base.set("--branch", "team/feature-1"), "branch-mismatch"},
		{"branch non-ASCII", base.set("--branch", "fé"), "branch-mismatch"},
		{"base with slash", prNosuch.set("--base", "release/1.x"), "remote-unknown"},
	}
	for _, c := range accepted {
		e.refuse(c.name, c.f, 4, "refused", c.reason)
	}
	// --timeout 1s is the lower bound and passes usage (R1). Under load a
	// read-only git command may exceed it (R18.a, read-failed), so only the
	// load-independent part is asserted: not exit 2, one Report line on
	// stdout, and nothing changed (both possible outcomes precede any change).
	before = e.snap()
	r := e.publishF(nosuch.add("--timeout", "1s"))
	if r.code == 2 {
		t.Errorf("timeout 1s: exit 2, want the value accepted\nstderr:\n%s", r.stderr)
	} else {
		pubDecodeReport(t, r.stdout)
	}
	e.mustUnchanged("timeout 1s", before)
	if n := len(pubCalls(t, e.ghLog)); n != 0 {
		t.Errorf("gh ran %d times before the gh preconditions", n)
	}
}

// pubNameValid is an independent model of the name rules of section 2.
func pubNameValid(s string) bool {
	if s == "" || s == "@" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, bad := range []string{"//", "..", "@{"} {
		if strings.Contains(s, bad) {
			return false
		}
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, comp := range strings.Split(s, "/") {
		if strings.HasPrefix(comp, ".") || strings.HasSuffix(comp, ".lock") {
			return false
		}
	}
	return true
}

// pubTestNameRules: R1.d as a property over generated names, judged by the
// independent model pubNameValid: a name that fails the rules is a usage
// error (exit 2) as --push, one that passes reaches remote-unknown (exit 4).
func pubTestNameRules(t *testing.T, tl *pubTools) {
	anchors := map[string]bool{
		"origin": true, "team/feature-1": true, "a.b": true, "x@y": true, "fé": true, "]": true,
		"a.lock": false, "a/.b": false, "a..b": false, "a//b": false, "@": false, "a@{b": false, "-a": false, "a/": false, "a.": false, "/a": false,
	}
	for name, want := range anchors {
		if pubNameValid(name) != want {
			t.Fatalf("model judges %q as %v, the contract says %v", name, !want, want)
		}
	}
	e := pubNewEnv(t, tl)
	e.startTask(pubPlanSmall)
	e.standardChange()
	e.verifyTask()
	tokens := []string{"a", "b", "Z", "0", "-", "_", ".", "/", "@", "{", "}", "~", "^", ":", "?", "*", "[", "]", "\\", " ", "\t", "\x01", "\x7f", "é", ".lock", "..", "//"}
	const seed1, seed2 = 20261108, 6508
	rng := rand.New(rand.NewPCG(seed1, seed2))
	seen := map[string]bool{"origin": true}
	var names []string
	for len(names) < 64 {
		var b strings.Builder
		for n := 1 + rng.IntN(5); n > 0; n-- {
			b.WriteString(tokens[rng.IntN(len(tokens))])
		}
		if s := b.String(); !seen[s] {
			seen[s] = true
			names = append(names, s)
		}
	}
	t.Logf("generated %d names from PCG(%d, %d)", len(names), seed1, seed2)
	before := e.snap()
	valid := 0
	for _, name := range names {
		f := e.flags().drop("--push").add("--push=" + name)
		r := e.publishF(f)
		if pubNameValid(name) {
			valid++
			if r.code != 4 || !strings.Contains(r.stderr, "(remote-unknown)") {
				t.Errorf("--push %q passes the name rules: exit %d stderr %q, want 4 remote-unknown", name, r.code, r.stderr)
			}
		} else if r.code != 2 || r.stdout != "" {
			t.Errorf("--push %q fails the name rules: exit %d stdout %q, want 2 and empty stdout", name, r.code, r.stdout)
		}
	}
	if valid == 0 || valid == len(names) {
		t.Errorf("generator produced %d valid of %d names; both classes must be exercised", valid, len(names))
	}
	e.mustUnchanged("name rule runs", before)
}

// pubTestCheckGuard: R23, cw task start refuses a check that runs
// `cw publish` (after global flags) and admits other cw commands.
func pubTestCheckGuard(t *testing.T, tl *pubTools) {
	e := pubNewEnv(t, tl)
	cases := []struct {
		argv    []string
		refused bool
	}{
		{[]string{"cw", "publish"}, true},
		{[]string{"cw", "--state-dir", "/s", "publish"}, true},
		{[]string{"cw", "--state-dir=/s", "publish"}, true},
		{[]string{"/usr/local/bin/CW", "publish"}, true},
		{[]string{"cw.exe", "publish"}, true},
		{[]string{"CW.EXE", "publish", "--task", "x"}, true},
		{[]string{"cw", "--dry-run", "publish"}, true},
		{[]string{"cw", "--dry-run=true", "publish"}, true},
		{[]string{"cw", "--home", "/h", "--config-dir", "/c", "--codex-home", "/x", "--claude-home", "/y", "publish"}, true},
		{[]string{"cw", "--home=/h", "--config-dir=/c", "--codex-home=/x", "--claude-home=/y", "publish"}, true},
		{[]string{"cw", "-v", "publish"}, true},
		{[]string{"cw", "--unknown", "publish"}, true},
		{[]string{"cw", "task", "show", "--field", "publish"}, false},
		{[]string{"cw", "--home", "publish", "doctor"}, false},
		{[]string{"cw", "--dry-run", "false", "publish"}, false},
		{[]string{"cw", "-x", "value", "publish"}, false},
		{[]string{"cwx", "publish"}, false},
		{[]string{"mycw", "publish"}, false},
		{[]string{"cw", "doctor", "publish"}, false},
		{[]string{"go", "run", "./cmd/cw", "publish"}, false},
	}
	for i, c := range cases {
		argv, err := json.Marshal(c.argv)
		if err != nil {
			t.Fatal(err)
		}
		plan := `{"title":"guard","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"],"allow_non_execution":false}],"checks":[{"id":"ok","argv":` + string(argv) + `,"timeout_seconds":30}]}`
		planPath := filepath.Join(e.work, fmt.Sprintf("guard-%d.json", i))
		pubWriteFile(t, planPath, []byte(plan), 0o644)
		store := filepath.Join(e.root, fmt.Sprintf("guard store %d", i))
		r := e.cw(nil, "task", "start", "--repo", e.repo, "--plan", planPath, "--store", store, "--summary")
		entries, _ := os.ReadDir(store)
		if c.refused {
			if r.code != 1 || !strings.Contains(r.stderr, "publication and deployment commands are outside task checks") {
				t.Errorf("check %q: exit %d stderr %q, want 1 and the refusal message", c.argv, r.code, r.stderr)
			}
			if len(entries) != 0 {
				t.Errorf("check %q: a task directory was created", c.argv)
			}
		} else if r.code != 0 || len(entries) != 1 {
			t.Errorf("check %q: exit %d with %d task directories, want admitted (0, 1)\nstderr:\n%s", c.argv, r.code, len(entries), r.stderr)
		}
	}
}
