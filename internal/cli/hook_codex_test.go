package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for `cw hook pre-publish` run by Codex. testdata/codex-pre-tool-use.json
// is the PreToolUse input codex-cli 0.167.2 sent to the plugin hook on
// 2026-11-12, with its paths replaced by placeholders; that run showed Codex
// running the command after an ask (docs/hooks.md, Codex).

// hkCodexInput returns the recorded Codex PreToolUse input with cwd set to
// repo; every other member, the command included, is as recorded.
func hkCodexInput(t *testing.T, repo string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "codex-pre-tool-use.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if turn, _ := input["turn_id"].(string); input["cwd"] != "<repo>" || turn == "" {
		t.Fatalf("recorded input %s lacks the <repo> placeholder or turn_id", data)
	}
	input["cwd"] = repo
	data, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// hkCodexCommand is the command in the recorded Codex input.
const hkCodexCommand = "git commit --allow-empty -m ask-probe"

// hkCodexOptions is the sentence every reason given to Codex ends with.
const hkCodexOptions = " Under Codex the plugin options are environment variables of the shell that starts Codex, " +
	"such as CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false or CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false."

func TestHookPrePublishDeniesOnCodexWhereClaudeCodeAsks(t *testing.T) {
	// A repository with no record and 400 staged lines of code without a
	// test: an unrecorded substantial change (docs/hooks.md, Asking instead
	// of denying).
	repo := hkCommittedRepo(t, nil)
	hkWriteFile(t, repo, "x.go", hkLines(400))
	hkGit(t, repo, "add", "-A")
	h := hkNoRecordHarness(t)

	// Oracle: Claude Code's input, which has no turn_id, still gets the ask.
	question := hkExpectAsk(t, h.prePublish(t, repo, hkCodexCommand), hkCommitAskWording)

	// Oracle: Codex runs a command it is asked about, so the recorded Codex
	// input for the same command gets a deny whose reason is the Codex
	// preamble, the whole question without its "agent-workflows: " prefix,
	// how the user goes ahead, and where the options are.
	decision, reason := hkDecision(t, h.runPrePublish(t, hkCodexInput(t, repo)))
	want := "agent-workflows: Codex cannot ask for confirmation, so this command is denied where Claude Code would ask: " +
		strings.TrimPrefix(question, "agent-workflows: ") +
		" Put the question to the user; to go ahead, the user runs the command outside Codex." + hkCodexOptions
	if decision != "deny" || reason != want {
		t.Fatalf("Codex decision %q with reason\n%q\nwant deny with reason\n%q", decision, reason, want)
	}
}

func TestHookPrePublishDenyOnCodexNamesTheOptionVariables(t *testing.T) {
	// Oracle: an open substantial record gates the commit for both hosts;
	// the deny Codex gets is Claude Code's, followed by where the options
	// are, because Codex has no plugin config.
	repo := hkRepo(t)
	h := newHKHarness(t)
	store := hkEmptyStore(t)
	h.env["CW_TASK_STORE"] = store
	record := hkSubstantialRecord(t, repo, store, "Codex gate")

	claude := hkExpectDeny(t, h.prePublish(t, repo, hkCodexCommand), record)
	codex := hkExpectDeny(t, h.runPrePublish(t, hkCodexInput(t, repo)), record)
	if codex != claude+hkCodexOptions {
		t.Fatalf("Codex deny reason\n%q\nwant Claude Code's\n%q\nfollowed by %q", codex, claude, hkCodexOptions)
	}
}
