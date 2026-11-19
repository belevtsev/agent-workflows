package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type workflowNativeResult struct {
	Model    string          `json:"observed_model,omitempty"`
	Effort   string          `json:"observed_effort,omitempty"`
	Worker   string          `json:"worker"`
	Stopped  bool            `json:"stopped,omitempty"`
	Conflict bool            `json:"conflict,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type workflowNativeEnvelope struct {
	Task    string          `json:"task"`
	Attempt string          `json:"attempt"`
	Result  json.RawMessage `json:"result"`
}

func workflowNativeFile(attempt string) string { return "workflow-native-" + attempt + ".json" }

func observeWorkflowNative(l *stageLedger, host string, notice tokens.Notice) error {
	task, err := nativeWorkflowTask(l, host, notice.Session)
	if err != nil || task == nil {
		return err
	}
	a := task.Workflow.Active
	if !a.NativePrepared || a.Role == "reviewer" || workflowUsedSession(task, notice.AgentID) {
		return nil
	}
	name := workflowNativeFile(a.ID)
	var observed workflowNativeResult
	if err := l.read(name, &observed); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if notice.Event == "SubagentStart" {
		if observed.Worker == "" {
			observed.Worker = notice.AgentID
		} else if observed.Worker != notice.AgentID {
			observed.Conflict = true
		}
		return l.write(name, observed)
	}
	if notice.Event != "SubagentStop" || observed.Stopped || observed.Worker != notice.AgentID {
		return nil
	}
	observed.Stopped = true
	observed.Model, observed.Effort = workflowNoticeSettings(notice)
	var envelope workflowNativeEnvelope
	if decodeWorkflowJSON([]byte(notice.LastAssistant), &envelope) == nil && envelope.Task == task.ID && envelope.Attempt == a.ID {
		observed.Payload = envelope.Result
	}
	return l.write(name, observed)
}

func workflowNoticeSettings(notice tokens.Notice) (model, effort string) {
	if json.Unmarshal(notice.Effort, &effort) != nil {
		var value struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(notice.Effort, &value)
		effort = value.Level
	}
	return notice.Model, effort
}

func observedWorkflowSettingsMatch(settings roles.SpecialistSettings, model, effort string) bool {
	if effort != "" && settings.Model.Effort != "" && effort != settings.Model.Effort {
		return false
	}
	if model == "" || model == settings.Model.Model {
		return true
	}
	return settings.Host == "claude" && strings.HasPrefix(model, "claude-"+settings.Model.Model+"-")
}

func nativeWorkflowTask(l *stageLedger, host, parent string) (*harness.Task, error) {
	id, err := workflowActiveTask(l, "")
	if err != nil || id == "" {
		return nil, err
	}
	task, err := readWorkflow(l, id)
	if err != nil {
		return nil, err
	}
	a := task.Workflow.Active
	if a.Native && a.Host == host && a.Owner == parent {
		return task, nil
	}
	return nil, nil
}

func guardWorkflowSpawn(ctx context.Context, host string, call toolCall) string {
	if (call.Tool != "Agent" && call.Tool != "spawn_agent") || !nativeSessionID.MatchString(call.Session) {
		return ""
	}
	repo, err := gitx.Open(ctx, call.Dir)
	if err != nil || repo.Root == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(repo.GitDir, "kitsu-stages")); errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	l, err := openStageLedger(ctx, &session{repo: repo})
	if err != nil {
		return "Kitsu could not inspect the native assignment. Retry after the task ledger is available."
	}
	defer l.close()
	task, err := nativeWorkflowTask(l, host, call.Session)
	if err != nil {
		return "Kitsu could not inspect the configured worker assignment."
	}
	if task == nil {
		return ""
	}
	a := task.Workflow.Active
	if a.NativePrepared {
		return "This Kitsu assignment already authorized a native worker. Recover it before launching another."
	}
	settings := task.Workflow.Roles[a.Role].Model
	effort, fresh := nativeWorkflowSpawnSettings(host, call)
	if settings.Model != call.Input.Model || settings.Effort != effort || !fresh {
		return fmt.Sprintf("Use a fresh %s worker with the saved model %q and effort %q. If this host cannot apply them, reconcile the assignment and use kitsu task run --resume %s in a terminal.",
			a.Role, settings.Model, settings.Effort, task.ID)
	}
	a.NativePrepared = true
	if err := l.write(harnessFile(task.ID), task); err != nil {
		return "Kitsu could not record the native launch authorization."
	}
	return ""
}

func nativeWorkflowSpawnSettings(host string, call toolCall) (string, bool) {
	if host == "codex" {
		return call.Input.Reasoning, call.Input.ForkTurns == "none" || call.Input.ForkContext != nil && !*call.Input.ForkContext
	}
	return call.Input.Effort, call.Input.ForkContext == nil || !*call.Input.ForkContext
}
