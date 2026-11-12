package gate_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// These tests drive Decide through its Deps alone: a fake Git that answers
// from a table, a fixed clock and a fake Store. No git process runs and no
// task record exists on disk.

// fixedNow is the clock every test passes as Deps.Now.
var fixedNow = time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)

// fakeGit answers git commands from a table keyed by "DIR: ARGS" and records
// every call in the same form. A command without an answer fails.
type fakeGit struct {
	answers map[string]string
	calls   []string
}

func (f *fakeGit) run(_ context.Context, dir string, args ...string) (string, error) {
	key := dir + ": " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if output, ok := f.answers[key]; ok {
		return output, nil
	}
	return "", errors.New("fake git has no answer for " + key)
}

// answer adds output for git args run in dir.
func (f *fakeGit) answer(dir, args, output string) {
	if f.answers == nil {
		f.answers = map[string]string{}
	}
	f.answers[dir+": "+args] = output
}

// repository makes the fake resolve dir as the top level of a main
// worktree.
func (f *fakeGit) repository(dir string) {
	f.answer(dir, "rev-parse --show-toplevel", dir+"\n")
	f.answer(dir, "rev-parse --path-format=absolute --git-common-dir", filepath.Join(dir, ".git")+"\n")
}

// storeCall is one FindOpen call the fake store saw.
type storeCall struct {
	repo taskflow.Repository
	now  time.Time
}

// fakeStore returns fixed open records per worktree, or err.
type fakeStore struct {
	open  map[string][]taskflow.OpenRecord
	err   error
	calls []storeCall
}

func (s *fakeStore) FindOpen(_ context.Context, repo taskflow.Repository, now time.Time) ([]taskflow.OpenRecord, error) {
	s.calls = append(s.calls, storeCall{repo: repo, now: now})
	if s.err != nil {
		return nil, s.err
	}
	return s.open[repo.Worktree], nil
}

// canonicalDir returns a fresh directory in canonical form; the repository
// resolver keeps only top levels that exist.
func canonicalDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// record is an open record; problem "" means its verification covers the
// source.
func record(id, title, problem string) taskflow.OpenRecord {
	return taskflow.OpenRecord{Record: taskflow.Record{ID: id, Directory: "/store/" + id, Plan: taskflow.Plan{Title: title}}, Problem: problem}
}

func decide(command, cwd string, enforce, prompt bool, git *fakeGit, store *fakeStore) gate.Decision {
	return decideIn(gate.Input{Command: command, CWD: cwd, Enforce: enforce, Prompt: prompt}, git, store)
}

func decideIn(in gate.Input, git *fakeGit, store *fakeStore) gate.Decision {
	return gate.Decide(context.Background(), in, gate.Deps{Git: git.run, Now: func() time.Time { return fixedNow }, Store: store})
}

func TestDecideDeniesWhileARecordGates(t *testing.T) {
	repo := canonicalDir(t)
	git := &fakeGit{}
	git.repository(repo)
	store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {record("task-1", "Add retries", "it has no passing cw task verify")}}}

	got := decide("git commit -m x && git push origin main", repo, true, true, git, store)

	// Oracle: the deny text cw hook pre-publish printed before the gate left
	// internal/cli, byte for byte, for one gating record.
	want := gate.Decision{Permission: gate.Deny, Reason: "agent-workflows: git commit and push stay blocked while a task for this repository is open: " +
		"each must pass cw task verify on the source being published, or be closed. " +
		"Task task-1 (Add retries): it has no passing cw task verify; run `cw task verify --task /store/task-1 --summary`, " +
		"or close it with `cw task abandon --task /store/task-1 --reason TEXT` if it is finished or was given up. " +
		"To turn the gate off, set enforce_verify=false in the plugin config."}
	if got != want {
		t.Fatalf("Decide = %+v\nwant %+v", got, want)
	}
	// Oracle: a commit and a push from one worktree read its records once,
	// for the resolved repository and at the time Deps.Now reports.
	if len(store.calls) != 1 {
		t.Fatalf("store calls %+v, want one", store.calls)
	}
	if call := store.calls[0]; call.repo.Worktree != repo || !slices.Equal(call.repo.Roots, []string{repo}) || !call.now.Equal(fixedNow) {
		t.Fatalf("store call %+v, want worktree and roots %s at %v", call, repo, fixedNow)
	}
}

func TestDecideAsksBeforePushingUncommittedChangesOverAVerifiedRecord(t *testing.T) {
	repo := canonicalDir(t)
	verified := map[string][]taskflow.OpenRecord{repo: {record("task-2", "Fix parser", "")}}
	status := "status --porcelain --untracked-files=no --ignore-submodules=none"
	// Oracle: the ask text cw hook pre-publish printed before the move.
	ask := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: task task-2 (Fix parser) passed cw task verify on the working tree, " +
		"but the working tree has uncommitted changes to tracked files, so the pushed commits may not hold the verified source. " +
		"Commit the changes and verify again, or confirm to push anyway."}
	for _, c := range []struct {
		name    string
		command string
		enforce bool
		status  string
		want    gate.Decision
	}{
		// Oracle for each row: only a push asks, only with enforce on, and
		// only when git status lists a tracked change.
		{"push of a dirty tree asks", "git push origin main", true, " M source.txt\n", ask},
		{"commit of a dirty tree is allowed", "git commit -m x", true, " M source.txt\n", gate.Decision{}},
		{"push with enforce off is allowed", "git push origin main", false, " M source.txt\n", gate.Decision{}},
		{"push of a clean tree is allowed", "git push origin main", true, "", gate.Decision{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			git := &fakeGit{}
			git.repository(repo)
			git.answer(repo, status, c.status)
			if got := decide(c.command, repo, c.enforce, true, git, &fakeStore{open: verified}); got != c.want {
				t.Fatalf("Decide = %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestDecideAsksForAnUnrecordedSubstantialChange(t *testing.T) {
	repo := canonicalDir(t)
	numstat := "diff --numstat -z --no-renames --no-ext-diff --no-textconv"
	// 320 changed lines under two top-level directories, code without tests:
	// all three signs.
	changes := "250\t60\tpkg/a.go\x0010\t0\tcmd/b.go\x00"
	signs := "320 changed lines (more than 300); changes in 2 top-level directories (cmd, pkg); code changed with no test file changed"
	advice := "Consider recording it with `cw task start --repo " + repo + " --plan PLAN.json --summary` and a passing cw task verify before publishing, or confirm to continue. " +
		"Set small_task_prompt=false in the plugin config to stop this prompt."

	t.Run("commit", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		git.answer(repo, numstat+" --cached", changes)
		got := decide("git commit -m x", repo, true, true, git, &fakeStore{})
		// Oracle: the commit wording of the ask text printed before the move.
		want := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: this commit looks like a substantial change with no cw task record for " + repo + ": " + signs + ". " + advice}
		if got != want {
			t.Fatalf("Decide = %+v\nwant %+v", got, want)
		}
	})

	t.Run("push", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		git.answer(repo, "rev-parse --verify --quiet @{upstream}", "0123abcd\n")
		git.answer(repo, numstat+" 0123abcd...HEAD", changes)
		got := decide("git push", repo, true, true, git, &fakeStore{})
		// Oracle: a push compares HEAD with the upstream git reports, its
		// trailing newline removed, and uses the push wording.
		want := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: the commits being pushed look like a substantial change with no cw task record for " + repo + ": " + signs + ". " + advice}
		if got != want {
			t.Fatalf("Decide = %+v\nwant %+v", got, want)
		}
	})

	t.Run("prompt off", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		git.answer(repo, numstat+" --cached", changes)
		// Oracle: small_task_prompt=false turns this ask off.
		if got := decide("git commit -m x", repo, true, false, git, &fakeStore{}); got != (gate.Decision{}) {
			t.Fatalf("Decide = %+v, want allow", got)
		}
	})
}

func TestDecideDenyWinsOverAsk(t *testing.T) {
	gated, unrecorded := canonicalDir(t), canonicalDir(t)
	git := &fakeGit{}
	git.repository(gated)
	git.repository(unrecorded)
	git.answer(unrecorded, "diff --numstat -z --no-renames --no-ext-diff --no-textconv --cached", "400\t0\tpkg/a.go\x001\t0\tcmd/b.go\x00")
	store := &fakeStore{open: map[string][]taskflow.OpenRecord{gated: {record("task-3", "Gated", "its source changed after the last passing cw task verify")}}}

	got := decide("git -C "+unrecorded+" commit -m y; git commit -m x", gated, true, true, git, store)

	// Oracle: a deny for one publication in the command wins over an ask for
	// another, and names only the gating record.
	if got.Permission != gate.Deny || !strings.Contains(got.Reason, "Task task-3 (Gated): its source changed after the last passing cw task verify;") || strings.Contains(got.Reason, "substantial change") {
		t.Fatalf("Decide = %+v, want a deny naming only task-3", got)
	}
}

func TestDecideAsksWhenARecordCannotBeRead(t *testing.T) {
	repo, unrecorded := canonicalDir(t), canonicalDir(t)
	damaged := taskflow.OpenRecord{Record: taskflow.Record{ID: "task-5", Directory: "/store/task-5"}, Unreadable: "invalid task JSON: unexpected EOF"}
	newer := taskflow.OpenRecord{Record: taskflow.Record{ID: "task-6", Directory: "/store/task-6"}, Newer: true,
		Unreadable: "task record version 5 was written by a newer cw; this cw reads versions 3 to 4; upgrade cw to read it"}
	// Oracle (D4, OD-11): an ask naming each directory and load error, with
	// the way out for a damaged record and the error's own advice for a newer
	// one, that a host which cannot ask lets run.
	ask := gate.Decision{Permission: gate.Ask, ProceedWithoutAsk: true, Reason: "agent-workflows: cw cannot read a task record for this repository, " +
		"so the commit and push gate cannot tell whether it allows this command. " +
		"Task directory /store/task-5: invalid task JSON: unexpected EOF; if the record is damaged, remove the directory or move it out of the task store, " +
		"then start a new task with `cw task start` if the work is still open. " +
		"Task directory /store/task-6: task record version 5 was written by a newer cw; this cw reads versions 3 to 4; upgrade cw to read it. " +
		"Confirm to run this command without the gate."}
	git := &fakeGit{}
	git.repository(repo)
	git.repository(unrecorded)
	git.answer(unrecorded, "diff --numstat -z --no-renames --no-ext-diff --no-textconv --cached", "400\t0\tpkg/a.go\x001\t0\tcmd/b.go\x00")
	gating := record("task-7", "Gated", "it has no passing cw task verify")

	if got := decide("git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {damaged, newer}}}); got != ask {
		t.Fatalf("Decide = %+v\nwant %+v", got, ask)
	}
	// Oracle: a readable gating record for the repository still denies, and
	// the deny names only that record.
	got := decide("git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {damaged, gating}}})
	if got.Permission != gate.Deny || !strings.Contains(got.Reason, "Task task-7 (Gated)") || strings.Contains(got.Reason, "task-5") {
		t.Fatalf("Decide = %+v, want a deny naming only task-7", got)
	}
	// Oracle: enforce_verify=false turns this ask off, as it does the deny.
	if got := decide("git commit -m x", repo, false, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {damaged}}}); got != (gate.Decision{}) {
		t.Fatalf("Decide with enforce off = %+v, want allow", got)
	}
	// Oracle: joined with an ask for an unrecorded change, which a host that
	// cannot ask must not run, the joined ask is not ProceedWithoutAsk.
	got = decide("git -C "+unrecorded+" commit -m y; git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {damaged}}})
	if got.Permission != gate.Ask || got.ProceedWithoutAsk || !strings.Contains(got.Reason, "substantial change") || !strings.Contains(got.Reason, "/store/task-5") {
		t.Fatalf("Decide = %+v, want an ask about both that is not ProceedWithoutAsk", got)
	}
	// Oracle: with enforce_verify=false an unreadable record counts as no
	// record, so the ask for an unrecorded substantial change still comes.
	got = decide("git commit -m y", unrecorded, false, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{unrecorded: {damaged}}})
	if got.Permission != gate.Ask || got.ProceedWithoutAsk || !strings.Contains(got.Reason, "substantial change with no readable cw task record") {
		t.Fatalf("Decide with enforce off = %+v, want the unrecorded-change ask", got)
	}
}

func TestDecideAsksAboutAnUnrecordedChangeWhenTheOnlyRecordCannotBeRead(t *testing.T) {
	repo := canonicalDir(t)
	git := &fakeGit{}
	git.repository(repo)
	git.answer(repo, "diff --numstat -z --no-renames --no-ext-diff --no-textconv --cached", "250\t60\tpkg/a.go\x0010\t0\tcmd/b.go\x00")
	damaged := taskflow.OpenRecord{Record: taskflow.Record{ID: "task-5", Directory: "/store/task-5"}, Unreadable: "invalid task JSON: unexpected EOF"}

	got := decide("git commit -m x", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {damaged}}})

	// Oracle: the unrecorded-change ask the commit raises when the damaged
	// record is left out, worded so it stays true ("no readable cw task
	// record"), joined with the ask for the damaged record; a host that
	// cannot ask must not run it, so it is not ProceedWithoutAsk.
	want := "agent-workflows: this commit looks like a substantial change with no readable cw task record for " + repo + ": " +
		"320 changed lines (more than 300); changes in 2 top-level directories (cmd, pkg); code changed with no test file changed. " +
		"Consider recording it with `cw task start --repo " + repo + " --plan PLAN.json --summary` and a passing cw task verify before publishing, or confirm to continue. " +
		"Set small_task_prompt=false in the plugin config to stop this prompt. " +
		"agent-workflows: cw cannot read a task record for this repository, so the commit and push gate cannot tell whether it allows this command. " +
		"Task directory /store/task-5: invalid task JSON: unexpected EOF; if the record is damaged, remove the directory or move it out of the task store, " +
		"then start a new task with `cw task start` if the work is still open. Confirm to run this command without the gate."
	if got != (gate.Decision{Permission: gate.Ask, Reason: want}) {
		t.Fatalf("Decide = %+v\nwant an ask with reason %q", got, want)
	}
}

func TestDecideAsksAboutAnUncommittedPushBesideAnUnreadableRecord(t *testing.T) {
	repo := canonicalDir(t)
	git := &fakeGit{}
	git.repository(repo)
	git.answer(repo, "status --porcelain --untracked-files=no --ignore-submodules=none", " M source.txt\n")
	damaged := taskflow.OpenRecord{Record: taskflow.Record{ID: "task-5", Directory: "/store/task-5"}, Unreadable: "invalid task JSON: unexpected EOF"}

	got := decide("git push origin main", repo, true, true, git, &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {record("task-2", "Fix parser", ""), damaged}}})

	// Oracle: the uncommitted-push ask for the verified record, as without
	// the damaged one, joined with the ask for the damaged one; a host that
	// cannot ask must not run it, so it is not ProceedWithoutAsk.
	want := "agent-workflows: task task-2 (Fix parser) passed cw task verify on the working tree, " +
		"but the working tree has uncommitted changes to tracked files, so the pushed commits may not hold the verified source. " +
		"Commit the changes and verify again, or confirm to push anyway. " +
		"agent-workflows: cw cannot read a task record for this repository, so the commit and push gate cannot tell whether it allows this command. " +
		"Task directory /store/task-5: invalid task JSON: unexpected EOF; if the record is damaged, remove the directory or move it out of the task store, " +
		"then start a new task with `cw task start` if the work is still open. Confirm to run this command without the gate."
	if got != (gate.Decision{Permission: gate.Ask, Reason: want}) {
		t.Fatalf("Decide = %+v\nwant an ask with reason %q", got, want)
	}
}

func TestDecideAsksAfterADirectoryItCannotResolve(t *testing.T) {
	repo, other := canonicalDir(t), canonicalDir(t)
	git := &fakeGit{}
	git.repository(repo)
	git.repository(other)
	damaged := taskflow.OpenRecord{Record: taskflow.Record{ID: "task-5", Directory: "/store/task-5"}, Unreadable: "invalid task JSON: unexpected EOF"}
	store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {record("task-8", "Open", "it has no passing cw task verify")}, other: {damaged}}}
	command := `cd "$OTHER" && git commit -m x`

	// Oracle: an ask naming the directory change as written and the
	// session's repository the gate checked instead, with each gating record
	// as the deny names it. It is not a record the host cannot read, so a
	// host that cannot ask must not run it: it is not ProceedWithoutAsk.
	want := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: the commit and push gate cannot tell which directory `cd \"$OTHER\"` leads to, " +
		"so it checked the session's repository " + repo + " instead, where a task is open: " +
		"Task task-8 (Open): it has no passing cw task verify; run `cw task verify --task /store/task-8 --summary`, " +
		"or close it with `cw task abandon --task /store/task-8 --reason TEXT` if it is finished or was given up. " +
		"Confirm only if the git commit or push does not run in " + repo + "."}
	if got := decide(command, repo, true, true, git, store); got != want {
		t.Fatalf("Decide = %+v\nwant %+v", got, want)
	}
	// Oracle: enforce_verify=false turns this ask off, as it does the deny.
	if got := decide(command, repo, false, true, git, store); got != (gate.Decision{}) {
		t.Fatalf("Decide with enforce off = %+v, want allow", got)
	}
	// Oracle: joined with the ask for a record that cannot be read, which a
	// host that cannot ask lets run, the joined ask is not ProceedWithoutAsk.
	got := decide(command+"; git -C "+other+" push", repo, true, true, git, store)
	if got.Permission != gate.Ask || got.ProceedWithoutAsk || !strings.Contains(got.Reason, `"$OTHER"`) || !strings.Contains(got.Reason, "/store/task-5") {
		t.Fatalf("Decide = %+v, want an ask about both that is not ProceedWithoutAsk", got)
	}
}

func TestDecideFailsOpen(t *testing.T) {
	repo := canonicalDir(t)
	gating := map[string][]taskflow.OpenRecord{repo: {record("task-4", "T", "it has no passing cw task verify")}}

	t.Run("git fails", func(t *testing.T) {
		git := &fakeGit{}
		store := &fakeStore{open: gating}
		// Oracle: a repository git cannot resolve is not checked.
		if got := decide("git commit -m x", repo, true, true, git, store); got != (gate.Decision{}) || len(store.calls) != 0 {
			t.Fatalf("Decide = %+v with %d store calls, want allow and none", got, len(store.calls))
		}
	})

	t.Run("store fails", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		// Oracle: a store that cannot be read is not checked; the hook
		// asks itself when it cannot locate the store.
		if got := decide("git commit -m x", repo, true, true, git, &fakeStore{err: errors.New("unreadable")}); got != (gate.Decision{}) {
			t.Fatalf("Decide = %+v, want allow", got)
		}
	})

	t.Run("both options off", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		// Oracle: enforce_verify=false and small_task_prompt=false switch the
		// gate off before it runs git.
		if got := decide("git commit -m x", repo, false, false, git, &fakeStore{open: gating}); got != (gate.Decision{}) || len(git.calls) != 0 {
			t.Fatalf("Decide = %+v after git calls %v, want allow and none", got, git.calls)
		}
	})

	t.Run("nothing published", func(t *testing.T) {
		git := &fakeGit{}
		git.repository(repo)
		// Oracle: a command with no git commit or push runs no git.
		if got := decide("git status && echo git commit", repo, true, true, git, &fakeStore{open: gating}); got != (gate.Decision{}) || len(git.calls) != 0 {
			t.Fatalf("Decide = %+v after git calls %v, want allow and none", got, git.calls)
		}
	})
}
