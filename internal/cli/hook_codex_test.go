package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
)

// Tests for `cw hook pre-publish` run by Codex. testdata/codex-pre-tool-use.json
// is the PreToolUse input codex-cli 0.167.2 sent to the plugin hook on
// 2026-11-12, with its paths replaced by placeholders; that run showed Codex
// running the command after an ask (docs/hooks.md, Codex).
// testdata/claude-code-pre-tool-use.json has the members a review logged from
// Claude Code 2.1.311's PreToolUse input, with placeholder values.

// hkRecordedInput returns the recorded input in testdata/name with cwd set to
// repo and the members in extra added; every other member, the command
// included, is as recorded.
func hkRecordedInput(t *testing.T, name, repo string, extra map[string]any) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if input["cwd"] != "<repo>" {
		t.Fatalf("recorded input %s lacks the <repo> placeholder", data)
	}
	input["cwd"] = repo
	for key, value := range extra {
		input[key] = value
	}
	data, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// hkCodexInput is the recorded Codex input for repo.
func hkCodexInput(t *testing.T, repo string) []byte {
	t.Helper()
	return hkRecordedInput(t, "codex-pre-tool-use.json", repo, nil)
}

// hkCodexCommand is the command in both recorded inputs.
const hkCodexCommand = "git commit --allow-empty -m ask-probe"

// hkCodexOptions is the sentence every reason given to Codex ends with.
const hkCodexOptions = " Under Codex the plugin options are environment variables of the shell that starts Codex, " +
	"such as CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false or CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false."

// hkUnrecordedChange returns a repository with no record and 400 staged
// lines of code without a test, an unrecorded substantial change
// (docs/hooks.md, Asking instead of denying), and a harness whose store is
// empty.
func hkUnrecordedChange(t *testing.T) (string, *hkHarness) {
	t.Helper()
	repo := hkCommittedRepo(t, nil)
	hkWriteFile(t, repo, "x.go", hkLines(400))
	hkGit(t, repo, "add", "-A")
	return repo, hkNoRecordHarness(t)
}

func TestHookPrePublishDeniesOnCodexWhereClaudeCodeAsks(t *testing.T) {
	repo, h := hkUnrecordedChange(t)

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

func TestHookPrePublishAsksOnClaudeCodeEvenWithATurnID(t *testing.T) {
	repo, h := hkUnrecordedChange(t)

	// Oracle: the Claude Code input gets the ask, as does a later Claude
	// Code input that adds turn_id to PreToolUse (it already sends turn_id
	// for another event): without Codex's model member it is not Codex.
	hkExpectAsk(t, h.runPrePublish(t, hkRecordedInput(t, "claude-code-pre-tool-use.json", repo, nil)), hkCommitAskWording)
	withTurn := hkRecordedInput(t, "claude-code-pre-tool-use.json", repo, map[string]any{"turn_id": "0b7c3e52-9d14-4a6f-8e21-5f9a0c6d2b37"})
	hkExpectAsk(t, h.runPrePublish(t, withTurn), hkCommitAskWording)
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

func TestCodexDecisionLetsAMarkedAskProceed(t *testing.T) {
	// Oracle (OD-11): an ask marked ProceedWithoutAsk, such as one for a
	// task record that cannot be read, is not turned into a deny under
	// Codex: the command goes ahead, with no output, as Codex runs any ask.
	// An unmarked ask still becomes a deny.
	marked := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: a record cannot be read.", ProceedWithoutAsk: true}
	if got := codexDecision(marked); got != (gate.Decision{}) {
		t.Fatalf("codexDecision(%+v) = %+v, want Allow with no reason", marked, got)
	}
	unmarked := gate.Decision{Permission: gate.Ask, Reason: "agent-workflows: a record cannot be read."}
	if got := codexDecision(unmarked); got.Permission != gate.Deny {
		t.Fatalf("codexDecision(%+v) = %+v, want a deny", unmarked, got)
	}
}
