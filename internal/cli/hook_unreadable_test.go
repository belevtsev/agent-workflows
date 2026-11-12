package cli

import (
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests that a governing task record this cw cannot read, and a task store it
// cannot locate, make cw hook pre-publish ask under Claude Code instead of
// allowing silently (D4), and let the command run under Codex, which cannot
// ask (OD-11). Each Codex run uses the recorded Codex input, whose command is
// hkCodexCommand.

// hkUnreadableStore returns a harness and a repository whose store holds one
// open substantial record, after requiring that it denies the commit.
func hkUnreadableStore(t *testing.T) (*hkHarness, string, string, taskflow.Record) {
	t.Helper()
	repo := hkRepo(t)
	h := newHKHarness(t)
	store := hkEmptyStore(t)
	h.env["CW_TASK_STORE"] = store
	record := hkSubstantialRecord(t, repo, store, "Damaged")
	hkExpectDeny(t, h.prePublish(t, repo, hkCodexCommand), record)
	return h, repo, store, record
}

func TestHookPrePublishAsksWhenARecordCannotBeRead(t *testing.T) {
	t.Run("garbage newest revision", func(t *testing.T) {
		// Oracle: the review's repro, echo '{' > <task>/record-000002.json,
		// turns the deny into an ask that names the task directory, the load
		// error and the way out, where it was an allow; Codex runs the
		// command (no output). A readable open record still denies.
		h, repo, store, record := hkUnreadableStore(t)
		hkWriteFile(t, record.Directory, "record-000002.json", "{\n")
		hkExpectAsk(t, h.prePublish(t, repo, hkCodexCommand), "Task directory "+record.Directory+": invalid task JSON",
			"remove the directory or move it out of the task store")
		hkExpectAllow(t, h.runPrePublish(t, hkCodexInput(t, repo)))
		readable := hkSubstantialRecord(t, repo, store, "Readable")
		hkExpectDeny(t, h.prePublish(t, repo, hkCodexCommand), readable)
	})

	t.Run("record from a newer cw", func(t *testing.T) {
		// Oracle: a revision with a version above this cw's asks, naming the
		// directory and saying to upgrade cw; Codex runs the command.
		h, repo, _, record := hkUnreadableStore(t)
		path := filepath.Join(record.Directory, "record-000001.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var members map[string]any
		if err := json.Unmarshal(data, &members); err != nil {
			t.Fatal(err)
		}
		members["version"] = taskflow.Version + 1
		if data, err = json.Marshal(members); err != nil {
			t.Fatal(err)
		}
		hkWriteFile(t, record.Directory, "record-000001.json", string(data))
		hkExpectAsk(t, h.prePublish(t, repo, hkCodexCommand), "Task directory "+record.Directory+": task record version", "upgrade cw")
		hkExpectAllow(t, h.runPrePublish(t, hkCodexInput(t, repo)))
	})
}

func TestHookPrePublishAsksWhenTheTaskStoreCannotBeLocated(t *testing.T) {
	// Oracle: without CW_TASK_STORE, a home directory that cannot be
	// resolved leaves the store unknown. The hook asked only for a relative
	// XDG_STATE_HOME and allowed this silently; it now asks, naming the
	// error and CW_TASK_STORE. Codex runs the command for both, where a
	// relative XDG_STATE_HOME was a deny.
	repo := hkRepo(t)
	h := newHKHarness(t)
	h.config.HomeDir = func() (string, error) { return "", errors.New("$HOME is not defined") }
	hkExpectAsk(t, h.prePublish(t, repo, hkCodexCommand), "agent-workflows: cw cannot locate the task store (resolve home directory); set CW_TASK_STORE to it.")
	hkExpectAllow(t, h.runPrePublish(t, hkCodexInput(t, repo)))

	h = newHKHarness(t)
	h.config.WorkingDir = func() (string, error) { return repo, nil }
	h.env["XDG_STATE_HOME"] = "rel"
	hkExpectAsk(t, h.prePublish(t, repo, hkCodexCommand), "XDG_STATE_HOME must be an absolute path")
	hkExpectAllow(t, h.runPrePublish(t, hkCodexInput(t, repo)))
}
