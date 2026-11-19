package onboarding

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const workflowFixturePrompt = "Review this literal input.\n$() ' --argument"

func TestMain(m *testing.M) {
	if mode := os.Getenv("KITSU_WORKFLOW_TEST_FIXTURE"); mode != "" {
		if err := workflowFixture(mode, os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func TestWorkflowOrdinaryHosts(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		for _, effort := range []string{"high", ""} {
			t.Run(host+"/"+effort, func(t *testing.T) {
				worker := setupWorkflowFixture(t, host, host, effort)
				var observed string
				worker.OnSession = func(id string) error { observed = id; return nil }
				result, err := RunWorkflowWorker(t.Context(), worker)
				if err != nil || !result.Started || !result.Exited || !result.Terminal || result.Session != observed || observed != "fixture-session" ||
					string(result.Payload) != `{"note":"complete"}` || !result.Usage.Available || result.Usage.InputTokens != 12 || result.Usage.CachedInputTokens != 3 || result.Usage.OutputTokens != 5 || result.Elapsed <= 0 {
					t.Fatalf("result = %+v, %s, %v", result, result.Payload, err)
				}
			})
		}
	}
}

func TestWorkflowOrdinaryResultRequiresNativeCompletion(t *testing.T) {
	for _, tc := range []struct{ host, mode, diagnostic string }{
		{"codex", "codex-incomplete", "completed native turn"},
		{"codex", "codex-invalid-report", "bounded JSON object"},
		{"claude", "claude-mismatch", "match one native session"},
		{"claude", "claude-denied", "denied a tool call"},
		{"claude", "claude-substitution", "substituted the pinned model"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			worker := setupWorkflowFixture(t, tc.host, tc.mode, "high")
			result, err := RunWorkflowWorker(t.Context(), worker)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || !result.Started || !result.Exited || result.Terminal || result.Payload != nil {
				t.Fatalf("result = %+v, %v", result, err)
			}
		})
	}
}

func TestWorkflowStopsAfterCancelledSessionRegistration(t *testing.T) {
	worker := setupWorkflowFixture(t, "codex", "codex-stall", "high")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker.OnSession = func(string) error { cancel(); return nil }
	result, err := RunWorkflowWorker(ctx, worker)
	if !errors.Is(err, context.Canceled) || !result.Started || !result.Exited || result.Terminal || result.Session != "fixture-session" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestClaudeWorkflowNeedsExactTrustAndOutsideSession(t *testing.T) {
	worker := setupWorkflowFixture(t, "claude", "claude", "high")
	claudeWorkflowTrustFixture(t, filepath.Dir(worker.Root))
	result, err := RunWorkflowWorker(t.Context(), worker)
	if err == nil || !strings.Contains(err.Error(), "exact worktree") || result.Started || !result.Exited {
		t.Fatalf("ancestor trust: %+v, %v", result, err)
	}
	claudeWorkflowTrustFixture(t, worker.Root)
	t.Setenv("CLAUDECODE", "1")
	result, err = RunWorkflowWorker(t.Context(), worker)
	if err == nil || !strings.Contains(err.Error(), "outside Claude Code") || result.Started || !result.Exited {
		t.Fatalf("nested review: %+v, %v", result, err)
	}
	if os.Getenv("CLAUDECODE") != "1" {
		t.Fatal("workflow removed the host nesting marker")
	}
}

func TestWorkflowAdvisersCannotSuppressNativeObservations(t *testing.T) {
	worker := setupWorkflowFixture(t, "codex", "codex-review", "high")
	worker.ReadOnly, worker.Role = true, "reviewer"
	worker.ReviewPrompt = func(string) (string, error) { t.Error("review reserved from adviser context"); return "", nil }
	t.Setenv("KITSU_ADVISER_CALL", "1")
	result, err := RunWorkflowWorker(t.Context(), worker)
	if err == nil || !strings.Contains(err.Error(), "adviser cannot start") || result.Started || !result.Exited {
		t.Fatalf("adviser review: %+v, %v", result, err)
	}
}

func TestWorkflowNativeReviewUsesReservedParent(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		for _, effort := range []string{"max", ""} {
			t.Run(host+"/"+effort, func(t *testing.T) {
				worker := setupWorkflowFixture(t, host, host+"-review", effort)
				worker.ReadOnly, worker.Role = true, "reviewer"
				claim := filepath.Join(t.TempDir(), "review-owner")
				t.Setenv("KITSU_WORKFLOW_TEST_CLAIM", claim)
				worker.ReviewPrompt = func(parent string) (string, error) {
					return workflowFixturePrompt, os.WriteFile(claim, []byte(parent), 0o600)
				}
				worker.OnSession = func(session string) error { return confirmWorkflowTestClaim(session) }
				result, err := RunWorkflowWorker(t.Context(), worker)
				if err != nil || !result.Started || !result.Exited || !result.Terminal || result.Session == "" || result.Payload != nil {
					t.Fatalf("review: %+v, %v", result, err)
				}
				if err := confirmWorkflowTestClaim(result.Session); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCodexReviewCancellationUnblocksAnUndrainedRequest(t *testing.T) {
	worker := setupWorkflowFixture(t, "codex", "codex-review-stall", "max")
	worker.ReadOnly, worker.Role = true, "reviewer"
	claim := filepath.Join(t.TempDir(), "review-owner")
	t.Setenv("KITSU_WORKFLOW_TEST_CLAIM", claim)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	worker.ReviewPrompt = func(parent string) (string, error) {
		return strings.Repeat("x", workflowPromptLimit), os.WriteFile(claim, []byte(parent), 0o600)
	}
	result, err := RunWorkflowWorker(ctx, worker)
	if !errors.Is(err, context.DeadlineExceeded) || !result.Started || !result.Exited || result.Terminal || result.Session == "" {
		t.Fatalf("cancelled review: %+v, %v", result, err)
	}
}

func setupWorkflowFixture(t *testing.T, host, mode, effort string) WorkflowWorker {
	t.Helper()
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	name := host
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(bin, name)
	if err := os.Link(executable, destination); err != nil {
		data, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITSU_WORKFLOW_TEST_FIXTURE", mode)
	t.Setenv("KITSU_WORKFLOW_TEST_EFFORT", effort)
	t.Setenv("KITSU_ADVISER_CALL", "")
	if host == "claude" {
		t.Setenv("CLAUDECODE", "")
		t.Setenv("CLAUDE_CODE_CHILD_SESSION", "")
		t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "")
		t.Setenv("CLAUDE_CODE_SAFE_MODE", "")
		t.Setenv("CLAUDE_CODE_SIMPLE", "")
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
		claudeWorkflowTrustFixture(t, resolved)
	}
	return WorkflowWorker{Host: host, Root: resolved, Role: "architect", Prompt: workflowFixturePrompt, Model: "fixture-model", Effort: effort,
		Scratch: t.TempDir(), ReadOnly: true, Schema: json.RawMessage(`{"type":"object","properties":{"note":{"type":"string"}},"required":["note"],"additionalProperties":false}`)}
}

func claudeWorkflowTrustFixture(t *testing.T, root string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"projects": map[string]any{root: map[string]bool{"hasTrustDialogAccepted": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func workflowFixture(mode string, args []string) error {
	if strings.HasPrefix(mode, "codex-review") {
		return codexReviewWorkflowFixture(mode, args)
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	if string(prompt) != workflowFixturePrompt && !(mode == "claude-review" && strings.HasSuffix(string(prompt), workflowFixturePrompt)) {
		return errors.New("literal prompt was changed")
	}
	if strings.HasPrefix(mode, "codex") {
		return codexWorkflowFixture(mode, args)
	}
	return claudeWorkflowFixture(mode, args)
}

func codexWorkflowFixture(mode string, args []string) error {
	if len(args) < 2 || args[0] != "exec" || args[len(args)-1] != "-" || !slices.Contains(args, "--json") || fixtureArgument(args, "--sandbox") != "read-only" || fixtureArgument(args, "--model") != "fixture-model" {
		return errors.New("unexpected Codex arguments")
	}
	if !fixtureCodexEffort(args) {
		return errors.New("unexpected Codex effort setting")
	}
	schema, err := os.ReadFile(fixtureArgument(args, "--output-schema"))
	if err != nil || !json.Valid(schema) {
		return errors.New("missing Codex output schema")
	}
	if err := writeWorkflowEvent(`{"type":"thread.started","thread_id":"fixture-session"}`); err != nil {
		return err
	}
	if mode == "codex-stall" {
		time.Sleep(30 * time.Second)
		return errors.New("fixture was not stopped")
	}
	if err := writeWorkflowEvent(`{"type":"turn.started"}`); err != nil {
		return err
	}
	if mode == "codex-incomplete" {
		return nil
	}
	payload := []byte(`{"note":"complete"}`)
	if mode == "codex-invalid-report" {
		payload = []byte(`[]`)
	}
	if err := os.WriteFile(fixtureArgument(args, "--output-last-message"), payload, 0o600); err != nil {
		return err
	}
	return writeWorkflowEvent(`{"type":"turn.completed","usage":{"input_tokens":12,"cached_input_tokens":3,"output_tokens":5,"reasoning_output_tokens":2}}`)
}

func claudeWorkflowFixture(mode string, args []string) error {
	effort := os.Getenv("KITSU_WORKFLOW_TEST_EFFORT")
	if !slices.Contains(args, "--print") || fixtureArgument(args, "--permission-mode") != "plan" || fixtureArgument(args, "--permission-prompts") != "none" || fixtureArgument(args, "--model") != "fixture-model" ||
		fixtureArgument(args, "--effort") != effort || effort == "" && slices.Contains(args, "--effort") {
		return errors.New("unexpected Claude arguments")
	}
	session := "fixture-session"
	if mode == "claude-review" {
		session = fixtureArgument(args, "--session-id")
		if err := confirmWorkflowTestClaim(session); err != nil {
			return err
		}
		data, err := os.ReadFile(fixtureArgument(args, "--agents"))
		var roles map[string]map[string]any
		if err != nil || json.Unmarshal(data, &roles) != nil || roles[claudeWorkflowReviewRole]["model"] != "fixture-model" || !slices.Contains(args, "--include-hook-events") {
			return errors.New("missing Claude reviewer settings")
		}
		if value, exists := roles[claudeWorkflowReviewRole]["effort"]; exists != (effort != "") || exists && value != effort {
			return errors.New("incorrect Claude reviewer effort")
		}
	} else if !json.Valid([]byte(fixtureArgument(args, "--json-schema"))) {
		return errors.New("missing Claude output schema")
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(map[string]string{"type": "system", "subtype": "init", "session_id": session, "model": "claude-fixture"}); err != nil {
		return err
	}
	if mode == "claude-substitution" {
		if err := encoder.Encode(map[string]any{"type": "assistant", "session_id": session, "message": map[string]string{"model": "claude-alternate"}}); err != nil {
			return err
		}
	}
	result := map[string]any{"type": "result", "subtype": "success", "session_id": session,
		"structured_output": map[string]string{"note": "complete"},
		"usage":             map[string]int{"input_tokens": 7, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 2, "output_tokens": 5}}
	if mode == "claude-mismatch" {
		result["session_id"] = "another-session"
	}
	if mode == "claude-denied" {
		result["permission_denials"] = []any{map[string]string{"tool_name": "Bash"}}
	}
	return encoder.Encode(result)
}

func codexReviewWorkflowFixture(mode string, args []string) error {
	if len(args) == 0 || args[0] != "app-server" || !fixtureCodexEffort(args) {
		return errors.New("unexpected Codex review arguments")
	}
	var rolePath string
	for _, arg := range args {
		if raw, ok := strings.CutPrefix(arg, "agents."+codexWorkflowReviewRole+".config_file="); ok {
			rolePath, _ = strconv.Unquote(raw)
		}
	}
	definition, err := os.ReadFile(rolePath)
	effort := os.Getenv("KITSU_WORKFLOW_TEST_EFFORT")
	if err != nil || !strings.Contains(string(definition), `model = "fixture-model"`) ||
		strings.Contains(string(definition), "model_reasoning_effort") != (effort != "") {
		return errors.New("missing pinned Codex reviewer definition")
	}
	return codexReviewWorkflowWire(mode, effort)
}

func codexReviewWorkflowWire(mode, effort string) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), workflowPromptLimit*2)
	encoder := json.NewEncoder(os.Stdout)
	for index, method := range []string{"initialize", "initialized", "thread/start", "turn/start"} {
		var request struct {
			Method string
			Params map[string]json.RawMessage
		}
		if !scanner.Scan() || json.Unmarshal(scanner.Bytes(), &request) != nil || request.Method != method {
			return fmt.Errorf("missing native request %s", method)
		}
		switch index {
		case 0:
			if err := writeWorkflowEvent(`{"id":1,"result":{"userAgent":"fixture"}}`); err != nil {
				return err
			}
		case 2:
			if err := codexReviewFixtureThread(encoder, request.Params, effort); err != nil {
				return err
			}
			if mode == "codex-review-stall" {
				time.Sleep(30 * time.Second)
				return errors.New("Codex fixture read stall was not cancelled")
			}
		case 3:
			if err := confirmWorkflowTestClaim("review-parent"); err != nil {
				return err
			}
			if len(request.Params["effort"]) != 0 && string(request.Params["effort"]) != strconv.Quote(effort) || effort != "" && len(request.Params["effort"]) == 0 {
				return errors.New("incorrect effort on the Codex parent turn")
			}
			if !strings.Contains(string(request.Params["input"]), codexWorkflowReviewRole) {
				return errors.New("missing named Codex delegation")
			}
		}
	}
	for _, line := range []string{
		`{"id":3,"result":{"turn":{"id":"review-turn","status":"inProgress"}}}`,
		`{"method":"thread/tokenUsage/updated","params":{"threadId":"review-parent","turnId":"review-turn","tokenUsage":{"total":{"inputTokens":8,"cachedInputTokens":2,"outputTokens":3,"reasoningOutputTokens":1}}}}`,
		`{"method":"turn/completed","params":{"threadId":"review-child","turn":{"id":"child-turn","status":"completed"}}}`,
		`{"method":"turn/completed","params":{"threadId":"review-parent","turn":{"id":"review-turn","status":"completed"}}}`,
	} {
		if err := writeWorkflowEvent(line); err != nil {
			return err
		}
	}
	if scanner.Scan() {
		return errors.New("unexpected message after review completion")
	}
	return scanner.Err()
}

func codexReviewFixtureThread(encoder *json.Encoder, params map[string]json.RawMessage, effort string) error {
	var cwd, sandbox, approval string
	if json.Unmarshal(params["cwd"], &cwd) != nil || json.Unmarshal(params["sandbox"], &sandbox) != nil || sandbox != "read-only" || json.Unmarshal(params["approvalPolicy"], &approval) != nil || approval != "never" {
		return errors.New("missing read-only review permissions")
	}
	if effort == "" {
		effort = "medium"
	}
	return encoder.Encode(map[string]any{"id": 2, "result": map[string]any{
		"thread": map[string]string{"id": "review-parent", "sessionId": "review-parent"}, "cwd": cwd,
		"model": "fixture-model", "reasoningEffort": effort, "approvalPolicy": "never", "sandbox": map[string]string{"type": "readOnly"},
	}})
}

func fixtureCodexEffort(args []string) bool {
	effort := os.Getenv("KITSU_WORKFLOW_TEST_EFFORT")
	if effort == "" {
		return !slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "model_reasoning_effort=") })
	}
	return slices.Contains(args, "model_reasoning_effort="+strconv.Quote(effort))
}

func fixtureArgument(args []string, name string) string {
	index := slices.Index(args, name)
	if index < 0 || index+1 >= len(args) {
		return ""
	}
	return args[index+1]
}

func confirmWorkflowTestClaim(session string) error {
	data, err := os.ReadFile(os.Getenv("KITSU_WORKFLOW_TEST_CLAIM"))
	if err != nil || session == "" || string(data) != session {
		return errors.New("native parent does not match the pre-bound review owner")
	}
	return nil
}

func writeWorkflowEvent(line string) error {
	_, err := fmt.Fprintln(os.Stdout, line)
	return err
}
