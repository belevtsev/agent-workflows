package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const (
	workflowPromptLimit = 256 << 10
	workflowResultLimit = 64 << 10
	workflowSchemaLimit = 8 << 10
	// Output handling may still be recording a native session when the host exits.
	workflowOutputDrain = 2 * time.Second
)

var workflowSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// WorkflowWorker describes one pinned assignment for a native host process.
type WorkflowWorker struct {
	Host, Root, Role, Prompt, Model, Effort, Scratch string
	ReadOnly                                         bool
	Schema                                           json.RawMessage
	// OnSession observes the native session while the process is active. A
	// callback failure stops the process. Its work should respect the run context.
	OnSession func(string) error
	// ReviewPrompt reserves the review with this native parent ID and supplies
	// its assignment. The review result is collected by the existing hooks.
	ReviewPrompt func(parent string) (string, error)
}

// WorkflowUsage reports counts that the native host returned for this process.
// InputTokens includes cached input and cache creation tokens.
type WorkflowUsage struct {
	Available                bool  `json:"available"`
	InputTokens              int64 `json:"input_tokens"`
	CachedInputTokens        int64 `json:"cached_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	ReasoningOutputTokens    int64 `json:"reasoning_output_tokens"`
}

// WorkflowResult distinguishes an attempt that may have run from a validated
// successful completion. Payload is empty for a reviewer parent.
type WorkflowResult struct {
	Session        string
	ObservedModel  string
	ObservedEffort string
	Payload        json.RawMessage
	Started        bool
	// Exited means the process never started or its group has stopped.
	Exited   bool
	Terminal bool
	Elapsed  time.Duration
	Usage    WorkflowUsage
}

// RunWorkflowWorker supervises a fresh native CLI process. The caller owns
// reservation recovery, candidate validation and acceptance of hook evidence.
func RunWorkflowWorker(ctx context.Context, w WorkflowWorker) (result WorkflowResult, err error) {
	result.Exited = true
	defer func() {
		if err != nil {
			result.Payload, result.Terminal = nil, false
		}
	}()
	if err := validateWorkflowWorker(w); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if os.Getenv("KITSU_ADVISER_CALL") != "" {
		return result, errors.New("a Kitsu adviser cannot start a native workflow worker")
	}
	root, err := filepath.EvalSymlinks(w.Root)
	if err != nil {
		return result, fmt.Errorf("resolve native workflow worktree: %w", err)
	}
	w.Root = root
	executable, native := HostExecutable(w.Host)
	if !native {
		return result, fmt.Errorf("%s workflow needs a native CLI on absolute PATH", w.Host)
	}
	if w.Host == "claude" {
		if err := checkClaudeWorkflowTrust(w.Root); err != nil {
			return result, err
		}
	}
	work, err := os.MkdirTemp(w.Scratch, "kitsu-host-")
	if err != nil {
		return result, fmt.Errorf("create private host scratch directory: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(work)) }()
	started := time.Now()
	defer func() { result.Elapsed = time.Since(started) }()
	switch {
	case w.Host == "claude":
		return runClaudeWorkflow(ctx, executable, work, w)
	case w.ReviewPrompt != nil:
		return runCodexWorkflowReview(ctx, executable, work, w)
	default:
		return runCodexWorkflow(ctx, executable, work, w)
	}
}

func validateWorkflowWorker(w WorkflowWorker) error {
	if w.Host != "claude" && w.Host != "codex" {
		return errors.New("workflow host must be codex or claude")
	}
	if !filepath.IsAbs(w.Root) || w.Scratch != "" && !filepath.IsAbs(w.Scratch) {
		return errors.New("workflow requires an absolute worktree and, when supplied, absolute scratch directory")
	}
	if strings.TrimSpace(w.Model) != w.Model || w.Model == "" || len(w.Model) > 512 || strings.ContainsRune(w.Model, 0) {
		return errors.New("workflow requires an explicit native model")
	}
	if w.Effort != "" && !validWorkflowEffort(w.Host, w.Effort) {
		return fmt.Errorf("%s workflow requires a supported effort or host default", w.Host)
	}
	if w.ReviewPrompt != nil {
		if !w.ReadOnly || w.Role != "reviewer" {
			return errors.New("native review requires a read-only reviewer assignment")
		}
		return nil
	}
	if err := validateWorkflowPrompt(w.Prompt); err != nil {
		return err
	}
	if !workflowJSONObject(w.Schema, workflowSchemaLimit) {
		return errors.New("workflow requires a bounded JSON output schema")
	}
	return nil
}

func validWorkflowEffort(host, value string) bool {
	return slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, value) ||
		host == "codex" && slices.Contains([]string{"none", "minimal", "ultra"}, value)
}

func validateWorkflowPrompt(prompt string) error {
	if strings.TrimSpace(prompt) == "" || len(prompt) > workflowPromptLimit || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0) {
		return errors.New("workflow prompt must be nonempty bounded UTF-8 without NUL bytes")
	}
	return nil
}

func workflowJSONObject(data []byte, limit int) bool {
	if len(data) > limit || !utf8.Valid(data) {
		return false
	}
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '{' && json.Valid(data)
}

func workflowSession(w WorkflowWorker, result *WorkflowResult, id string) error {
	if !workflowSessionID.MatchString(id) || result.Session != "" {
		return errors.New("host did not provide one valid native session identity")
	}
	result.Session, result.Started = id, true
	if w.OnSession != nil {
		return w.OnSession(id)
	}
	return nil
}

func workflowProcessCouldRun(result proc.Result) bool {
	var exit *exec.ExitError
	return result.Code >= 0 || errors.As(result.Err, &exit) || errors.Is(result.Err, exec.ErrWaitDelay) || result.Cleanup != nil
}

func workflowProcessExited(result proc.Result) bool {
	return result.Cleanup == nil && !errors.Is(result.Err, exec.ErrWaitDelay)
}

func workflowProcessError(ctx context.Context, host string, result proc.Result) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(err, result.Cleanup)
	}
	if result.Err != nil || result.Cleanup != nil || result.Stopped || result.Code != 0 {
		return fmt.Errorf("%s host did not exit cleanly (status %d): %w", host, result.Code,
			errors.Join(workflowHostDiagnostic(string(result.Stderr)), result.Err, result.Cleanup))
	}
	return nil
}

// Classify native diagnostics without echoing potentially private model or tool data.
func workflowHostDiagnostic(diagnostic string) error {
	detail := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(detail, "not logged in"), strings.Contains(detail, "authentication"), strings.Contains(detail, "unauthorized"), strings.Contains(detail, "please run /login"):
		return errors.New("host authentication failed")
	case strings.Contains(detail, "permission"), strings.Contains(detail, "sandbox"), strings.Contains(detail, "workspace not trusted"):
		return errors.New("host permissions blocked the assignment")
	case strings.Contains(detail, "model") && (strings.Contains(detail, "unavailable") || strings.Contains(detail, "not supported")):
		return errors.New("the pinned model or effort is unavailable")
	case strings.Contains(detail, "limit"), strings.Contains(detail, "quota"):
		return errors.New("host limit reached")
	default:
		return errors.New("host did not complete the assignment")
	}
}
