package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookPrePublishAsksOnRelativeXDGStateHome: with a relative
// XDG_STATE_HOME and no CW_TASK_STORE the hook cannot locate the task store.
// It still runs the gate, and for a git commit or git push the gate would
// judge it asks, naming the variable, instead of allowing silently: on the
// parent of this change it allowed every such command, although the
// repository had an unrecorded substantial change (a) or an open record (b).
func TestHookPrePublishAsksOnRelativeXDGStateHome(t *testing.T) {
	const want = "XDG_STATE_HOME must be an absolute path; set it, or set CW_TASK_STORE"

	t.Run("unrecorded substantial change at the repository root", func(t *testing.T) {
		// Oracle: before the change this commit asked about an unrecorded
		// substantial change (400 staged lines of code, no test). The store
		// is now unknown, so the hook asks about the store instead.
		repo := hkCommittedRepo(t, nil)
		hkWriteFile(t, repo, "x.go", hkLines(400))
		hkGit(t, repo, "add", "-A")
		h := newHKHarness(t)
		h.config.WorkingDir = func() (string, error) { return repo, nil }
		h.env["XDG_STATE_HOME"] = "rel"
		hkExpectAsk(t, h.prePublish(t, repo, "git commit -m x"), want)
	})

	t.Run("open record in a store under the working directory", func(t *testing.T) {
		// Oracle: before the change cw task start, run from ws with
		// --repo proj, wrote this record to ws/rel/agent-workflows, and a hook
		// run in ws denied the commit. The store is now refused, so the hook
		// asks rather than allow the commit unchecked.
		ws := hkCanonicalTempDir(t)
		proj := filepath.Join(ws, "proj")
		if err := os.Mkdir(proj, 0o700); err != nil {
			t.Fatal(err)
		}
		hkGit(t, proj, "init", "-q")
		state := filepath.Join(ws, "rel", "agent-workflows")
		if err := os.MkdirAll(state, 0o700); err != nil {
			t.Fatal(err)
		}
		hkSubstantialRecord(t, proj, filepath.Join(state, "workflow-tasks"), "Relative state")
		h := newHKHarness(t)
		h.config.WorkingDir = func() (string, error) { return ws, nil }
		h.env["XDG_STATE_HOME"] = "rel"
		hkExpectAsk(t, h.prePublish(t, proj, "git commit -m x"), want)
		hkExpectAsk(t, h.prePublish(t, proj, "git push origin main"), want)
	})

	t.Run("an abandon beside a commit names the store too", func(t *testing.T) {
		// Oracle: the commit's check cannot read the store, which asks
		// whatever else the command does; the ask before cw task abandon
		// does not replace that ask but is joined with it.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.config.WorkingDir = func() (string, error) { return repo, nil }
		h.env["XDG_STATE_HOME"] = "rel"
		command := "cw task abandon --task /store/task-1 --reason x && git commit -m x"
		hkExpectAsk(t, h.prePublish(t, repo, command), "This command abandons the task in /store/task-1.", want)
		// Oracle: the store's ask alone would let Codex run the command, but
		// joined with the abandon's, which the user must answer, it is not
		// ProceedWithoutAsk, so Codex denies it.
		codex := hkRecordedInput(t, "codex-pre-tool-use.json", repo, map[string]any{"tool_input": map[string]any{"command": command}})
		if decision, reason := hkDecision(t, h.runCodexPrePublish(t, codex)); decision != "deny" || !strings.Contains(reason, "This command abandons the task in /store/task-1.") || !strings.Contains(reason, want) {
			t.Fatalf("Codex decision %q with reason %q, want a deny naming the abandon and the store", decision, reason)
		}
	})

	t.Run("commands the gate does not judge still pass", func(t *testing.T) {
		// Oracle: the fail-open rules for a command with no git commit or
		// git push, and for a directory outside any repository, do not
		// depend on the store, so they still write nothing.
		repo := hkRepo(t)
		outside := hkCanonicalTempDir(t)
		h := newHKHarness(t)
		h.config.WorkingDir = func() (string, error) { return repo, nil }
		h.env["XDG_STATE_HOME"] = "rel"
		hkExpectAllow(t, h.prePublish(t, repo, "git status"))
		hkExpectAllow(t, h.prePublish(t, outside, "git commit -m x"))
	})

	t.Run("CW_TASK_STORE is used and the variable is not read", func(t *testing.T) {
		// Oracle: CW_TASK_STORE comes before the state directory
		// (docs/hooks.md, Task store), so an empty store allows a plain
		// commit as it does without the variable.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.env["CW_TASK_STORE"] = hkEmptyStore(t)
		h.env["XDG_STATE_HOME"] = "rel"
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})
}
