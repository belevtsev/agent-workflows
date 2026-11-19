package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const codexWorkflowResult = "result.json"

type codexWorkflowOutput struct {
	worker        WorkflowWorker
	result        WorkflowResult
	turn, success bool
}

func runCodexWorkflow(ctx context.Context, executable, scratch string, worker WorkflowWorker) (WorkflowResult, error) {
	schema := filepath.Join(scratch, "schema.json")
	if err := os.WriteFile(schema, worker.Schema, 0o600); err != nil {
		return WorkflowResult{Exited: true}, fmt.Errorf("write the Codex output schema: %w", err)
	}
	sandbox := "workspace-write"
	if worker.ReadOnly {
		sandbox = "read-only"
	}
	args := []string{
		"exec", "--cd", worker.Root, "--sandbox", sandbox, "--json", "--color", "never",
		"--output-schema", schema, "--output-last-message", filepath.Join(scratch, codexWorkflowResult),
		"--model", worker.Model,
	}
	if worker.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(worker.Effort))
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &codexWorkflowOutput{worker: worker}
	stream := &workflowStream{parse: output.parse, cancel: cancel}
	run := proc.Run(runCtx, proc.Spec{
		Path: executable, Args: append(args, "-"), Dir: worker.Root, Env: os.Environ(),
		Stdin: strings.NewReader(worker.Prompt), Stdout: stream, StderrLimit: 8 << 10,
	})
	output.result.Started = output.result.Started || workflowProcessCouldRun(run)
	output.result.Exited = workflowProcessExited(run)
	if err := stream.finish(); err != nil {
		return output.result, errors.Join(fmt.Errorf("Codex protocol: %w", err), run.Cleanup)
	}
	if err := workflowProcessError(ctx, worker.Host, run); err != nil {
		return output.result, err
	}
	if !output.success {
		return output.result, errors.New("Codex did not report a completed native turn")
	}
	payload, err := readWorkflowPayload(scratch, codexWorkflowResult)
	if err != nil {
		return output.result, fmt.Errorf("read Codex workflow result: %w", err)
	}
	output.result.Payload, output.result.Terminal = payload, true
	return output.result, nil
}

func (o *codexWorkflowOutput) parse(data []byte) error {
	var event struct {
		Type    string `json:"type"`
		Session string `json:"thread_id"`
		Model   string `json:"model"`
		Effort  string `json:"reasoning_effort"`
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Item struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"item"`
		Usage *struct {
			Input     int64 `json:"input_tokens"`
			Cached    int64 `json:"cached_input_tokens"`
			Output    int64 `json:"output_tokens"`
			Reasoning int64 `json:"reasoning_output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &event) != nil || event.Type == "" {
		return errors.New("invalid Codex JSON event")
	}
	switch event.Type {
	case "thread.started":
		return o.observeThread(event.Session, event.Model, event.Effort)
	case "turn.started":
		if o.result.Session == "" || o.turn {
			return errors.New("unexpected Codex turn start")
		}
		o.turn = true
	case "turn.completed":
		if !o.turn || o.success {
			return errors.New("unexpected Codex turn completion")
		}
		o.success = true
		if u := event.Usage; u != nil && u.Input >= u.Cached && u.Cached >= 0 && u.Output >= u.Reasoning && u.Reasoning >= 0 {
			o.result.Usage = WorkflowUsage{Available: true, InputTokens: u.Input, CachedInputTokens: u.Cached, OutputTokens: u.Output, ReasoningOutputTokens: u.Reasoning}
		}
	case "turn.failed":
		return workflowHostDiagnostic(event.Error.Message)
	case "error":
		return workflowHostDiagnostic(event.Message)
	case "item.completed":
		if event.Item.Type == "error" && strings.HasPrefix(event.Item.Message, "model rerouted:") {
			return errors.New("Codex substituted the pinned model")
		}
	}
	return nil
}

func (o *codexWorkflowOutput) observeThread(session, model, effort string) error {
	o.result.ObservedModel, o.result.ObservedEffort = model, effort
	if err := verifyReportedCodexSettings(o.worker, model, effort); err != nil {
		return err
	}
	return workflowSession(o.worker, &o.result, session)
}

func verifyReportedCodexSettings(w WorkflowWorker, model, effort string) error {
	if model != "" && model != w.Model || effort != "" && w.Effort != "" && effort != w.Effort {
		return errors.New("Codex substituted a pinned model or effort")
	}
	return nil
}

func workflowReportedEffort(value json.RawMessage) string {
	var direct string
	if json.Unmarshal(value, &direct) == nil {
		return direct
	}
	var observed struct {
		Level string `json:"level"`
	}
	if json.Unmarshal(value, &observed) == nil {
		return observed.Level
	}
	return ""
}
