//go:build unix

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests for `cw hook git pre-commit`, `pre-push` and `pre-merge-commit`, driven
// through Execute with the hook's working directory set to the repository,
// as git runs its hooks. Each oracle is the exit status and stderr.

// hgOptions ends every deny and warning: where a git hook reads the options.
const hgOptions = " In a git hook the plugin options are environment variables of git, such as CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false " +
	"or CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false, and CW_GATE=off turns the gate off for git's hooks."

// gitHook runs `cw hook git NAME` in dir, requires empty stdout and returns
// the exit status and stderr.
func (h *hkHarness) gitHook(t *testing.T, name, dir string) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	h.config.Err = &stderr
	h.config.WorkingDir = func() (string, error) { return dir, nil }
	h.out.Reset()
	err := Execute(t.Context(), []string{"hook", "git", name}, h.config)
	if h.out.Len() != 0 {
		t.Fatalf("hook git %s wrote %q on stdout, want nothing", name, h.out.String())
	}
	return ExitCode(err), stderr.String()
}

// hgExpect requires that `cw hook git NAME` in dir exits with code and writes
// exactly stderr.
func (h *hkHarness) hgExpect(t *testing.T, name, dir string, code int, stderr string) {
	t.Helper()
	if gotCode, gotStderr := h.gitHook(t, name, dir); gotCode != code || gotStderr != stderr {
		t.Fatalf("hook git %s: exit %d stderr %q\nwant exit %d stderr %q", name, gotCode, gotStderr, code, stderr)
	}
}

func TestHookGitBlocksUntilTheTaskIsVerified(t *testing.T) {
	// Oracle (plan G4a acceptance): with an open unverified substantial task,
	// pre-commit and pre-push exit 3 (denied) with the deny reason cw hook pre-publish
	// gives, which names the task and its verify command, followed by where a
	// git hook reads its options. Once cw task verify passes on the source,
	// both exit 0 with nothing on stderr.
	repo := hkRepo(t)
	source := []byte("verified source\n")
	hkWriteFile(t, repo, "source.txt", string(source))
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	for name, command := range map[string]string{"pre-commit": "git commit -m x", "pre-push": "git push"} {
		reason := hkExpectDeny(t, h.prePublish(t, repo, command), record)
		h.hgExpect(t, name, repo, ExitDenied, reason+hgOptions+"\n")
	}
	h.assessAndVerify(t, record, source)
	for _, name := range []string{"pre-commit", "pre-push"} {
		h.hgExpect(t, name, repo, ExitOK, "")
	}
}

func TestHookGitWarnsWherePrePublishAsks(t *testing.T) {
	// Oracle: a git hook cannot ask, so each state in which cw hook
	// pre-publish asks exits 0 with that question on stderr, behind a line
	// that says the commit or push goes ahead.
	warning := func(command, question string) string {
		return "agent-workflows: warning: a git hook cannot ask for confirmation, so this " + command + " goes ahead where Claude Code would ask: " +
			strings.TrimPrefix(question, "agent-workflows: ") + hgOptions + "\n"
	}
	expectWarning := func(t *testing.T, h *hkHarness, repo, name, contains string) {
		t.Helper()
		command := strings.TrimPrefix(name, "pre-")
		question := hkExpectAsk(t, h.prePublish(t, repo, "git "+command), contains)
		h.hgExpect(t, name, repo, ExitOK, warning(command, question))
	}

	t.Run("record that cannot be read", func(t *testing.T) {
		h, repo, _, record := hkUnreadableStore(t)
		hkWriteFile(t, record.Directory, "record-000002.json", "{\n")
		expectWarning(t, h, repo, "pre-commit", "Task directory "+record.Directory+": invalid task JSON")
		expectWarning(t, h, repo, "pre-push", "Task directory "+record.Directory+": invalid task JSON")
	})
	t.Run("record from a newer cw", func(t *testing.T) {
		h, repo, _, record := hkUnreadableStore(t)
		data, err := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
		if err != nil {
			t.Fatal(err)
		}
		newer := strings.Replace(string(data), fmt.Sprintf(`"version":%d`, taskflow.Version), fmt.Sprintf(`"version":%d`, taskflow.Version+1), 1)
		hkWriteFile(t, record.Directory, "record-000001.json", newer)
		expectWarning(t, h, repo, "pre-commit", "upgrade cw")
	})
	t.Run("task store that cannot be read", func(t *testing.T) {
		repo := hkRepo(t)
		h := newHKHarness(t)
		file := filepath.Join(hkCanonicalTempDir(t), "store")
		hkWriteFile(t, filepath.Dir(file), "store", "not a directory\n")
		h.env["CW_TASK_STORE"] = file
		expectWarning(t, h, repo, "pre-commit", "cw cannot read the task store")
	})
	t.Run("task store that cannot be located", func(t *testing.T) {
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.config.HomeDir = func() (string, error) { return "", errors.New("$HOME is not defined") }
		expectWarning(t, h, repo, "pre-push", "cw cannot locate the task store")
	})
	t.Run("push with uncommitted changes over a verified task", func(t *testing.T) {
		repo, h := hkDirtyPushState(t)
		expectWarning(t, h, repo, "pre-push", "uncommitted changes to tracked files")
		// The working tree holds the verified bytes, so the commit goes ahead.
		h.hgExpect(t, "pre-commit", repo, ExitOK, "")
	})
	t.Run("substantial change with no task", func(t *testing.T) {
		repo, h := hkUnrecordedChange(t)
		expectWarning(t, h, repo, "pre-commit", hkCommitAskWording)
	})
}

func TestHookGitPreMergeCommitJudgesAMergeCommitAsACommit(t *testing.T) {
	// Oracle (F4): git runs pre-merge-commit, not pre-commit, before a merge
	// commit it makes without conflicts, and cw judges that commit as any
	// other, without the prompt about a substantial change: in each state,
	// cw hook git pre-merge-commit exits with the status and writes exactly
	// the stderr of cw hook git pre-commit, which the tests above pin, except
	// that it is silent about a substantial change with no task, which in a
	// merge commit is the incoming branch's (review finding 2). The states
	// cover a deny (an open unverified task), a warning (a record that cannot
	// be read), the substantial change, and silence; the last is a verified
	// task with uncommitted changes, where pre-push would warn, so
	// pre-merge-commit is not judged as a push. No GITHEAD_ variable names a
	// merge head here, so no merge is of remote-tracking commits.
	for _, row := range []struct {
		name  string
		state func(t *testing.T) (*hkHarness, string)
		code  int
		warns bool
		// mergeSilent: pre-merge-commit exits 0 without output, whatever
		// pre-commit does.
		mergeSilent bool
	}{
		{"open unverified task", func(t *testing.T) (*hkHarness, string) {
			repo := hkRepo(t)
			h := newHKHarness(t)
			h.start(t, repo, hkSubstantialPlan)
			return h, repo
		}, ExitDenied, true, false},
		{"record that cannot be read", func(t *testing.T) (*hkHarness, string) {
			h, repo, _, record := hkUnreadableStore(t)
			hkWriteFile(t, record.Directory, "record-000002.json", "{\n")
			return h, repo
		}, ExitOK, true, false},
		{"substantial change with no task", func(t *testing.T) (*hkHarness, string) {
			repo, h := hkUnrecordedChange(t)
			return h, repo
		}, ExitOK, true, true},
		{"verified task with uncommitted changes", func(t *testing.T) (*hkHarness, string) {
			repo, h := hkDirtyPushState(t)
			return h, repo
		}, ExitOK, false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			h, repo := row.state(t)
			code, stderr := h.gitHook(t, "pre-commit", repo)
			if code != row.code || (stderr != "") != row.warns {
				t.Fatalf("hook git pre-commit: exit %d stderr %q, want exit %d and stderr only if %v", code, stderr, row.code, row.warns)
			}
			if row.mergeSilent {
				code, stderr = ExitOK, ""
			}
			h.hgExpect(t, "pre-merge-commit", repo, code, stderr)
		})
	}
}

// hgRevParse returns the object name git resolves rev to in repo.
func hgRevParse(t *testing.T, repo, rev string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "--verify", rev).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(output))
}

func TestHookGitPreMergeCommitAllowsAMergeOfRemoteTrackingCommits(t *testing.T) {
	// Oracle (F4 review, design decision): with an open unverified task,
	// pre-merge-commit lets a merge commit through, exit 0 without output,
	// exactly when every merge head git names in a GITHEAD_<object name>
	// variable is reachable from a remote-tracking ref (refs/remotes/), such
	// as the base branch fetched from a remote; with any merge head only
	// local, or none named, it denies as pre-commit does, with the same
	// stderr. pre-commit ignores the variables: a merge with conflicts is
	// concluded by git commit, which is judged as any commit.
	repo := hkCommittedRepo(t, map[string]string{"base.txt": "base\n"})
	hkGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	base := hgRevParse(t, repo, "HEAD")
	hkWriteFile(t, repo, "base.txt", "upstream\n")
	hkGit(t, repo, "commit", "-q", "-am", "upstream")
	hkGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	upstream := hgRevParse(t, repo, "HEAD")
	hkWriteFile(t, repo, "side.txt", "side\n")
	hkGit(t, repo, "add", "side.txt")
	hkGit(t, repo, "commit", "-q", "-m", "local")
	local := hgRevParse(t, repo, "HEAD")
	h := newHKHarness(t)
	h.start(t, repo, hkSubstantialPlan)
	denied, deny := h.gitHook(t, "pre-commit", repo)
	if denied != ExitDenied || deny == "" {
		t.Fatalf("control: hook git pre-commit exit %d stderr %q, want a deny", denied, deny)
	}
	for _, row := range []struct {
		name  string
		heads map[string]string
		allow bool
	}{
		{"the remote-tracking branch's tip", map[string]string{upstream: "origin/main"}, true},
		{"an older commit of the remote-tracking branch, as git pull names it", map[string]string{base: base}, true},
		{"a local commit", map[string]string{local: "side"}, false},
		{"a remote-tracking commit and a local one", map[string]string{upstream: "origin/main", local: "side"}, false},
		{"no merge head named", nil, false},
		{"a merge head that is not an object name", map[string]string{"origin/main": "origin/main"}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			for head, name := range row.heads {
				h.env["GITHEAD_"+head] = name
			}
			t.Cleanup(func() {
				for head := range row.heads {
					delete(h.env, "GITHEAD_"+head)
				}
			})
			if row.allow {
				h.hgExpect(t, "pre-merge-commit", repo, ExitOK, "")
			} else {
				h.hgExpect(t, "pre-merge-commit", repo, ExitDenied, deny)
			}
			h.hgExpect(t, "pre-commit", repo, ExitDenied, deny)
		})
	}
}

func TestHookGitOffSwitchesAndFailOpen(t *testing.T) {
	// Oracle (docs/hooks.md, Git hooks): with an open unverified task,
	// CW_GATE=off and enforce_verify=false each exit 0 without output; any
	// other CW_GATE value keeps the gate. A directory outside any repository
	// exits 0 without output.
	repo := hkRepo(t)
	h := newHKHarness(t)
	h.start(t, repo, hkSubstantialPlan)
	h.env["CW_GATE"] = "off"
	h.hgExpect(t, "pre-commit", repo, ExitOK, "")
	h.env["CW_GATE"] = "on"
	if code, _ := h.gitHook(t, "pre-commit", repo); code != ExitDenied {
		t.Fatalf("CW_GATE=on: exit %d, want %d", code, ExitDenied)
	}
	delete(h.env, "CW_GATE")
	h.env[enforceVerifyOption] = "false"
	h.hgExpect(t, "pre-push", repo, ExitOK, "")
	delete(h.env, enforceVerifyOption)
	h.hgExpect(t, "pre-commit", hkCanonicalTempDir(t), ExitOK, "")
}

func TestHookGitCaptureDeadlineAllows(t *testing.T) {
	// Oracle: cw hook git has cw hook pre-publish's deadline. With a verified
	// task and every source capture probe hanging for minutes, pre-commit
	// exits 0 without output after reaching capture, no sooner than the
	// deadline, so a probe that ended early cannot pass, and well before the
	// probes end (hang guard: deadline plus 20 s).
	realGit, sleep, ps := hgLookPath(t, "git"), hgLookPath(t, "sleep"), hgLookPath(t, "ps")
	repo := hkCommittedRepo(t, map[string]string{"source.txt": "source\n"})
	h := newHKHarness(t)
	h.assessAndVerify(t, h.start(t, repo, hkSubstantialPlan), []byte("source\n"))
	marker := hdInstallHangingCaptureGit(t, realGit, sleep, ps, os.Getenv("PATH"))
	const deadline = 10 * time.Second
	h.config.prePublishTimeout = deadline
	start := time.Now()
	h.hgExpect(t, "pre-commit", repo, ExitOK, "")
	if elapsed := time.Since(start); elapsed < deadline || elapsed > deadline+20*time.Second {
		t.Fatalf("hook took %v, want from %v to %v", elapsed, deadline, deadline+20*time.Second)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the hook never reached source capture: %v", err)
	}
}

func hgLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is not on PATH: %v", name, err)
	}
	return path
}
