package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const codexWorkflowReviewRole = "kitsu_workflow_reviewer"

func runCodexWorkflowReview(ctx context.Context, executable, scratch string, worker WorkflowWorker) (WorkflowResult, error) {
	role := filepath.Join(scratch, "reviewer.toml")
	definition := "model = " + strconv.Quote(worker.Model) +
		"\ndeveloper_instructions = \"Review the exact Kitsu candidate independently and read-only. Return the requested native_review_result as one JSON object.\"\n"
	if worker.Effort != "" {
		definition += "model_reasoning_effort = " + strconv.Quote(worker.Effort) + "\n"
	}
	if err := os.WriteFile(role, []byte(definition), 0o600); err != nil {
		return WorkflowResult{Exited: true}, fmt.Errorf("write the Codex reviewer definition: %w", err)
	}
	args := []string{"app-server",
		"-c", "agents." + codexWorkflowReviewRole + ".config_file=" + strconv.Quote(role),
		"-c", `agents.` + codexWorkflowReviewRole + `.description="Independent read-only reviewer for a Kitsu reservation"`}
	if worker.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(worker.Effort))
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	input, send, finish, err := workflowCommandInput(runCtx, cancel)
	if err != nil {
		return WorkflowResult{Exited: true}, fmt.Errorf("open Codex app-server input: %w", err)
	}
	server := &codexReviewServer{worker: worker, send: send}
	if err := server.request(1, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "kitsu", "version": "1"},
	}); err != nil {
		return WorkflowResult{Exited: true}, errors.Join(err, finish())
	}
	stream := &workflowStream{parse: server.parse, cancel: cancel}
	run := proc.Run(runCtx, proc.Spec{
		Path: executable, Args: args, Dir: worker.Root, Env: os.Environ(),
		Stdin: input, Stdout: stream, StderrLimit: 8 << 10,
	})
	inputErr := finish()
	server.result.Exited = workflowProcessExited(run)
	if err := stream.finish(); err != nil {
		return server.result, errors.Join(fmt.Errorf("Codex review protocol: %w", err), run.Cleanup)
	}
	if err := errors.Join(workflowProcessError(ctx, worker.Host, run), inputErr); err != nil {
		return server.result, err
	}
	if !server.complete() {
		return server.result, errors.New("Codex did not complete the reserved review parent's turn")
	}
	server.result.Terminal = true
	return server.result, nil
}

type codexReviewServer struct {
	worker              WorkflowWorker
	send                func([]byte) error
	result              WorkflowResult
	reply               int
	turn, completedTurn string
}

func (s *codexReviewServer) request(id int, method string, params any) error {
	message := struct {
		ID     int    `json:"id,omitempty"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.send(append(data, '\n'))
}

func (s *codexReviewServer) parse(data []byte) error {
	var event struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &event) != nil {
		return errors.New("invalid app-server JSON event")
	}
	if event.Error != nil {
		return workflowHostDiagnostic(event.Error.Message)
	}
	if event.Method != "" {
		if event.ID != nil {
			return errors.New("Codex review requested an interactive host action")
		}
		return s.notification(event.Method, event.Params)
	}
	if event.ID == nil || *event.ID != s.reply+1 || len(event.Result) == 0 {
		return errors.New("unexpected Codex app-server response")
	}
	s.reply = *event.ID
	return s.response(event.Result)
}

func (s *codexReviewServer) response(data json.RawMessage) error {
	switch s.reply {
	case 1:
		if err := s.request(0, "initialized", nil); err != nil {
			return err
		}
		return s.request(2, "thread/start", map[string]any{
			"cwd": s.worker.Root, "model": s.worker.Model, "sandbox": "read-only", "approvalPolicy": "never",
		})
	case 2:
		return s.startReview(data)
	case 3:
		var reply struct{ Turn codexReviewTurn }
		if json.Unmarshal(data, &reply) != nil || !workflowSessionID.MatchString(reply.Turn.ID) {
			return errors.New("Codex did not identify the review parent turn")
		}
		s.turn = reply.Turn.ID
		return s.finishTurn()
	default:
		return errors.New("unexpected Codex app-server response")
	}
}

func (s *codexReviewServer) startReview(data json.RawMessage) error {
	var reply struct {
		Thread struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		}
		Cwd, Model, ApprovalPolicy, ReasoningEffort string
		Sandbox                                     struct{ Type string }
	}
	if json.Unmarshal(data, &reply) != nil || reply.Thread.ID != reply.Thread.SessionID || !workflowSessionID.MatchString(reply.Thread.SessionID) {
		return errors.New("Codex did not provide one new root session for the review")
	}
	if filepath.Clean(reply.Cwd) != filepath.Clean(s.worker.Root) || reply.Sandbox.Type != "readOnly" || reply.ApprovalPolicy != "never" {
		return errors.New("Codex did not retain the reserved review worktree and permissions")
	}
	if reply.Model != s.worker.Model || s.worker.Effort != "" && reply.ReasoningEffort != s.worker.Effort {
		return errors.New("Codex did not apply the pinned review model and effort")
	}
	prompt, err := s.worker.ReviewPrompt(reply.Thread.SessionID)
	if err != nil {
		return err
	}
	if err := validateWorkflowPrompt(prompt); err != nil {
		return err
	}
	if err := workflowSession(s.worker, &s.result, reply.Thread.SessionID); err != nil {
		return err
	}
	params := map[string]any{
		"threadId": reply.Thread.ID,
		"input":    []map[string]string{{"type": "text", "text": reviewDelegation(codexWorkflowReviewRole, prompt)}},
	}
	if s.worker.Effort != "" {
		params["effort"] = s.worker.Effort
	}
	return s.request(3, "turn/start", params)
}

type codexReviewTurn struct {
	ID, Status string
	Error      *struct{ Message string }
}

func (s *codexReviewServer) notification(method string, data json.RawMessage) error {
	switch method {
	case "model/rerouted":
		return errors.New("Codex substituted the pinned review model")
	case "turn/completed":
		var event struct {
			ThreadID string `json:"threadId"`
			Turn     codexReviewTurn
		}
		if json.Unmarshal(data, &event) != nil {
			return errors.New("invalid Codex turn completion")
		}
		if event.ThreadID != s.result.Session {
			return nil
		}
		if event.Turn.Error != nil {
			return workflowHostDiagnostic(event.Turn.Error.Message)
		}
		if !workflowSessionID.MatchString(event.Turn.ID) || event.Turn.Status != "completed" || s.completedTurn != "" {
			return errors.New("Codex review parent did not complete one native turn")
		}
		s.completedTurn = event.Turn.ID
		return s.finishTurn()
	case "thread/tokenUsage/updated":
		var event codexReviewUsage
		if json.Unmarshal(data, &event) == nil && event.ThreadID == s.result.Session && (s.turn == "" || s.turn == event.TurnID) {
			s.result.Usage = event.usage()
		}
	}
	return nil
}

func (s *codexReviewServer) complete() bool {
	return s.turn != "" && s.turn == s.completedTurn
}

func (s *codexReviewServer) finishTurn() error {
	if s.turn != "" && s.completedTurn != "" {
		if !s.complete() {
			return errors.New("Codex review parent completed a different native turn")
		}
		return s.send(nil)
	}
	return nil
}

type codexReviewUsage struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId"`
	TokenUsage struct {
		Total *struct {
			InputTokens, CachedInputTokens, OutputTokens, ReasoningOutputTokens int64
		}
	}
}

func (e codexReviewUsage) usage() WorkflowUsage {
	if u := e.TokenUsage.Total; u != nil && u.InputTokens >= u.CachedInputTokens && u.CachedInputTokens >= 0 &&
		u.OutputTokens >= u.ReasoningOutputTokens && u.ReasoningOutputTokens >= 0 {
		return WorkflowUsage{Available: true, InputTokens: u.InputTokens, CachedInputTokens: u.CachedInputTokens,
			OutputTokens: u.OutputTokens, ReasoningOutputTokens: u.ReasoningOutputTokens}
	}
	return WorkflowUsage{}
}
