package onboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/proc"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

const claudeWorkflowReviewRole = "kitsu-workflow-reviewer"

func checkClaudeWorkflowTrust(root string) error {
	if os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_CHILD_SESSION") != "" {
		return errors.New("Claude headless workflow must be resumed from a terminal outside Claude Code")
	}
	home, err := os.UserHomeDir()
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			return errors.New("CLAUDE_CONFIG_DIR must be absolute for a headless workflow")
		}
		home, err = dir, nil
	}
	if err != nil {
		return errors.New("Claude headless workflow cannot locate the user trust store")
	}
	var state struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	f, err := os.Open(filepath.Join(home, ".claude.json"))
	if err != nil {
		return fmt.Errorf("read Claude trust store: %w", err)
	}
	defer func() { _ = f.Close() }()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) > limit || json.Unmarshal(data, &state) != nil {
		return errors.New("Claude trust store cannot be read as JSON within 16 MiB")
	}
	if !state.Projects[root].Trusted && !state.Projects[filepath.ToSlash(root)].Trusted {
		return errors.New("Claude headless workflow requires explicit trust for this exact worktree in Claude Code")
	}
	return nil
}

func runClaudeWorkflow(ctx context.Context, executable, scratch string, worker WorkflowWorker) (WorkflowResult, error) {
	if forced := os.Getenv("CLAUDE_CODE_EFFORT_LEVEL"); worker.Effort != "" && forced != "" && forced != worker.Effort {
		return WorkflowResult{Exited: true}, errors.New("Claude's configured effort overrides the pinned workflow effort")
	}
	mode := "acceptEdits"
	if worker.ReadOnly {
		mode = "plan"
	}
	args := []string{
		"--print", "--input-format", "text", "--output-format", "stream-json", "--verbose",
		"--permission-prompts", "none", "--strict-mcp-config", "--no-chrome", "--permission-mode", mode,
		"--model", worker.Model, "--setting-sources", "user",
	}
	if worker.Effort != "" {
		args = append(args, "--effort", worker.Effort)
	}
	output := &claudeWorkflowOutput{worker: worker}
	environment := os.Environ()
	prompt, instruction := worker.Prompt, "Complete the Kitsu workflow assignment from standard input."
	if worker.ReviewPrompt != nil {
		var err error
		args, prompt, output.parent, err = prepareClaudeReview(args, scratch, worker)
		if err != nil {
			return WorkflowResult{Exited: true}, err
		}
		instruction = "Start the named Kitsu reviewer as one foreground native Agent with the assignment from standard input. Wait for it."
		environment = append(environment, "CLAUDE_CODE_FORK_SUBAGENT=0", "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1")
	} else {
		var schema bytes.Buffer
		if err := json.Compact(&schema, worker.Schema); err != nil {
			return WorkflowResult{Exited: true}, errors.New("invalid Claude output schema")
		}
		args = append(args, "--json-schema", schema.String())
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := &workflowStream{parse: output.parse, cancel: cancel}
	run := proc.Run(runCtx, proc.Spec{
		Path: executable, Args: append(args, "--", instruction), Dir: worker.Root, Env: environment,
		Stdin: strings.NewReader(prompt), Stdout: stream, StderrLimit: 8 << 10,
		WaitDelay: workflowOutputDrain,
	})
	output.result.Started = output.result.Started || workflowProcessCouldRun(run)
	output.result.Exited = workflowProcessExited(run)
	if err := stream.finish(); err != nil {
		return output.result, errors.Join(fmt.Errorf("Claude protocol: %w", err), run.Cleanup)
	}
	if err := workflowProcessError(ctx, worker.Host, run); err != nil {
		return output.result, err
	}
	if !output.success {
		return output.result, errors.New("Claude did not report a successful native result")
	}
	output.result.Terminal = true
	return output.result, nil
}

func prepareClaudeReview(args []string, scratch string, worker WorkflowWorker) ([]string, string, string, error) {
	if claudeWorkflowFlag("CLAUDE_CODE_SIMPLE") || claudeWorkflowFlag("CLAUDE_CODE_SAFE_MODE") {
		return nil, "", "", errors.New("Claude review requires a session with native review hooks enabled")
	}
	if claudeWorkflowFlag("CLAUDE_CODE_SUBAGENT_MODEL_FORCE") {
		return nil, "", "", errors.New("Claude's forced subagent model may override the pinned reviewer model")
	}
	parent := workflowUUID()
	path := filepath.Join(scratch, "agents.json")
	agent := map[string]any{
		"description":    "Review the exact Kitsu candidate independently and read-only.",
		"prompt":         "Review the assignment from the coordinator. Return the requested native_review_result as one JSON object, without Markdown or other text.",
		"model":          worker.Model,
		"permissionMode": "plan", "tools": []string{"Read", "Glob", "Grep", "Bash"},
	}
	if worker.Effort != "" {
		agent["effort"] = worker.Effort
	}
	definition, err := json.Marshal(map[string]any{claudeWorkflowReviewRole: agent})
	if err != nil {
		return nil, "", "", err
	}
	if err := os.WriteFile(path, definition, 0o600); err != nil {
		return nil, "", "", fmt.Errorf("write the Claude reviewer definition: %w", err)
	}
	prompt, err := worker.ReviewPrompt(parent)
	if err != nil {
		return nil, "", "", err
	}
	if err := validateWorkflowPrompt(prompt); err != nil {
		return nil, "", "", err
	}
	args = append(args, "--session-id", parent, "--agents", path, "--tools", "Agent,Read,Glob,Grep,Bash", "--include-hook-events", "--forward-subagent-text")
	return args, reviewDelegation(claudeWorkflowReviewRole, prompt), parent, nil
}

func claudeWorkflowFlag(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func workflowUUID() string {
	var id [16]byte
	_, _ = rand.Read(id[:]) // crypto/rand Read cannot fail.
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func reviewDelegation(role, prompt string) string {
	return "Start exactly one fresh foreground native subagent named " + role +
		" using its configured model and effort. Start it without inherited conversation history. " +
		"Give it the following assignment and wait for its final response. Do not review the code or write a verdict yourself.\n\n" + prompt
}

type claudeWorkflowOutput struct {
	worker        WorkflowWorker
	parent, model string
	result        WorkflowResult
	success       bool
}

func (o *claudeWorkflowOutput) parse(data []byte) error {
	var event struct {
		Type    string               `json:"type"`
		Subtype string               `json:"subtype"`
		Session string               `json:"session_id"`
		Model   string               `json:"model"`
		Effort  json.RawMessage      `json:"effort"`
		IsError bool                 `json:"is_error"`
		Result  string               `json:"result"`
		Errors  []string             `json:"errors"`
		Payload json.RawMessage      `json:"structured_output"`
		Denied  []json.RawMessage    `json:"permission_denials"`
		Usage   *claudeWorkflowUsage `json:"usage"`
		Message json.RawMessage      `json:"message"`
	}
	if json.Unmarshal(data, &event) != nil || event.Type == "" {
		return errors.New("invalid Claude JSON event")
	}
	if event.Type == "system" && event.Subtype == "init" {
		return o.observeInit(event.Session, event.Model, workflowReportedEffort(event.Effort))
	}
	if event.Type == "assistant" && event.Session == o.result.Session {
		var message struct{ Model string }
		if json.Unmarshal(event.Message, &message) == nil {
			return checkClaudeModel(o.model, message.Model)
		}
	}
	if event.Type != "result" {
		return nil
	}
	if event.IsError || event.Subtype != "success" {
		return workflowHostDiagnostic(event.Result + " " + strings.Join(event.Errors, " "))
	}
	if o.result.Session == "" || event.Session != o.result.Session || o.success {
		return errors.New("Claude result does not match one native session")
	}
	if len(event.Denied) != 0 {
		return errors.New("Claude denied a tool call during this assignment")
	}
	if o.parent == "" {
		if !workflowJSONObject(event.Payload, workflowResultLimit) {
			return errors.New("Claude result is missing a bounded structured JSON report")
		}
		o.result.Payload = event.Payload
	}
	o.result.Usage, o.success = event.Usage.counts(), true
	return nil
}

func (o *claudeWorkflowOutput) observeInit(session, model, effort string) error {
	if o.parent != "" && o.parent != session {
		return errors.New("Claude did not start the reserved review parent")
	}
	if err := workflowSession(o.worker, &o.result, session); err != nil {
		return err
	}
	o.model = model
	if o.parent == "" {
		o.result.ObservedModel, o.result.ObservedEffort = model, effort
		if o.worker.Effort != "" && effort != "" && o.worker.Effort != effort {
			return errors.New("Claude substituted the pinned effort")
		}
	}
	return checkClaudeModel(o.worker.Model, model)
}

func checkClaudeModel(requested, reported string) error {
	if !roles.ObservedModelMatches("claude", requested, reported) {
		return errors.New("Claude substituted the pinned model")
	}
	return nil
}

type claudeWorkflowUsage struct {
	Input   int64 `json:"input_tokens"`
	Output  int64 `json:"output_tokens"`
	Cached  int64 `json:"cache_read_input_tokens"`
	Created int64 `json:"cache_creation_input_tokens"`
}

func (u *claudeWorkflowUsage) counts() WorkflowUsage {
	if u == nil || u.Cached < 0 || u.Created < 0 || u.Input < 0 || u.Output < 0 ||
		u.Input > math.MaxInt64-u.Cached || u.Created > math.MaxInt64-u.Input-u.Cached {
		return WorkflowUsage{}
	}
	return WorkflowUsage{Available: true, InputTokens: u.Input + u.Cached + u.Created,
		CachedInputTokens: u.Cached, CacheCreationInputTokens: u.Created, OutputTokens: u.Output}
}
