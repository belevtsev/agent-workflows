package cli

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// Claude Code plugin hooks. Both commands read the hook's JSON input from
// stdin and write the hook's JSON decision to stdout. They fail open: every
// condition that is not a positive gate decision, including any internal
// error, exits 0 without output, so a damaged record, an unreadable store or a
// missing git never blocks the user's own commands. The one exception is a
// task store that a relative XDG_STATE_HOME leaves unknown: then each git
// commit and git push the gate would judge asks, because allowing it would
// skip the gate without a word.
const (
	hookInputLimit = 1 << 20
	// hookPublishTimeout bounds the pre-publish hook's Git and source-capture
	// work, whose capture probes allow far longer on their own. A record
	// whose source identity cannot be captured in time is skipped, while
	// other records can still gate. It stays below the 30 second hook
	// timeout in hooks/hooks.json, so cw finishes before it is killed.
	hookPublishTimeout    = 20 * time.Second
	hookDoctorTimeout     = 5 * time.Second
	hookDoctorCacheAge    = time.Hour
	enforceVerifyOption   = "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"
	sessionDoctorOption   = "CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"
	smallTaskPromptOption = "CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT"
)

type hookInput struct {
	CWD       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

func addHookCommands(root *cobra.Command, config Config) {
	hook := &cobra.Command{Use: "hook", Short: "Claude Code plugin hooks: load conventions and gate git commit and push on cw task verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.AddCommand(hook)
	hook.AddCommand(&cobra.Command{
		Use:   "pre-publish",
		Short: "PreToolUse hook: deny git commit and push while any open substantial task record for the repository lacks a passing cw task verify on the source being published",
		Long: "Reads the PreToolUse JSON from stdin. When the Bash command runs git commit or git push inside a Git repository and the task store holds " +
			"substantial, unabandoned records for that repository written within 14 days, it prints a deny decision naming every one whose verification has not passed " +
			"on the source being published, and an ask decision for a push of a verified repository whose working tree has uncommitted tracked changes. " +
			"With no such record, it prints an ask decision when the change looks substantial (two of: more than 300 changed lines, more than one top-level directory, " +
			"code changed without tests). Every other case, including any internal error, exits 0 without output. " +
			"Set the plugin option enforce_verify to false (" + enforceVerifyOption + "=false) to disable the gate, " +
			"and small_task_prompt to false (" + smallTaskPromptOption + "=false) to disable the ask for unrecorded changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), cmp.Or(config.prePublishTimeout, hookPublishTimeout))
			defer cancel()
			decision, ok := prePublishDecision(ctx, root, config)
			if !ok {
				return nil
			}
			return writeJSON(cmd.OutOrStdout(), decision)
		},
	})
	var instructions, cache string
	sessionStart := &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: emit the instructions file, plus a cached cw doctor summary when the plugin option session_doctor is true, as additional context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if instructions == "" {
				return usage(errors.New("--instructions FILE is required"))
			}
			context, ok := sessionStartContext(cmd.Context(), root, config, instructions, cache)
			if !ok {
				return nil
			}
			return writeJSON(cmd.OutOrStdout(), hookOutput{hookSpecificOutput{HookEventName: "SessionStart", AdditionalContext: context}})
		},
	}
	sessionStart.Flags().StringVar(&instructions, "instructions", "", "Markdown file whose contents become the session's additional context")
	sessionStart.Flags().StringVar(&cache, "cache", "", "Directory for the one-hour doctor summary cache (no caching when empty)")
	hook.AddCommand(sessionStart)
}

// prePublishDecision returns the gate's deny or ask decision for the hook
// input on stdin, or false when the command may proceed silently. With both
// plugin options off it returns before reading stdin, which
// hookPublishTimeout does not bound.
func prePublishDecision(ctx context.Context, root *cobra.Command, config Config) (hookOutput, bool) {
	enforce := config.Getenv(enforceVerifyOption) != "false"
	prompt := config.Getenv(smallTaskPromptOption) != "false"
	if !enforce && !prompt {
		return hookOutput{}, false
	}
	input, err := readHookInput(config.In)
	if err != nil {
		return hookOutput{}, false
	}
	path, err := hookTaskStore(root, config)
	var store gate.Store = gate.TaskStore(path)
	var unlocated *unlocatedStore
	switch {
	case errors.Is(err, errRelativeStateHome):
		// The store cannot be located, but the gate still runs: it decides
		// which commands publish from a repository, and each of those asks.
		unlocated = &unlocatedStore{err: err}
		store = unlocated
	case err != nil:
		return hookOutput{}, false
	}
	decision := gate.Decide(ctx, gate.Input{Command: input.ToolInput.Command, CWD: input.CWD, Enforce: enforce, Prompt: prompt},
		gate.Deps{Git: gate.ExecGit, Now: time.Now, Store: store})
	if unlocated != nil && unlocated.consulted && decision.Permission == gate.Allow {
		decision = gate.Decision{Permission: gate.Ask, Reason: relativeStateHomeReason}
	}
	if decision.Permission == gate.Allow {
		return hookOutput{}, false
	}
	return preToolUseDecision(string(decision.Permission), decision.Reason), true
}

// relativeStateHomeReason is the ask for a publication when a relative
// XDG_STATE_HOME leaves the task store unknown.
const relativeStateHomeReason = "agent-workflows: XDG_STATE_HOME must be an absolute path; set it, or set CW_TASK_STORE. " +
	"Until then the commit and push gate cannot read the task records; confirm to run this command without it."

// unlocatedStore is the gate's store when the hook cannot locate the task
// store. Every lookup fails with err, so the gate itself allows; consulted
// reports that the gate reached a publication it would have judged against
// the records, which the hook then asks about.
type unlocatedStore struct {
	err       error
	consulted bool
}

func (s *unlocatedStore) FindOpen(context.Context, taskflow.Repository, time.Time) ([]taskflow.OpenRecord, error) {
	s.consulted = true
	return nil, s.err
}

func preToolUseDecision(decision, reason string) hookOutput {
	return hookOutput{hookSpecificOutput{HookEventName: "PreToolUse", PermissionDecision: decision, PermissionDecisionReason: reason}}
}

func readHookInput(reader io.Reader) (hookInput, error) {
	var input hookInput
	if reader == nil {
		return input, errors.New("no hook input")
	}
	data, err := io.ReadAll(io.LimitReader(reader, hookInputLimit+1))
	if err != nil {
		return input, err
	}
	if len(data) > hookInputLimit {
		return input, errors.New("hook input exceeds the size limit")
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return input, err
	}
	if input.CWD == "" {
		return input, errors.New("hook input has no cwd")
	}
	return input, nil
}

// hookTaskStore returns $CW_TASK_STORE, a hook-only override that
// `cw task start` does not read (it takes --store), else the store
// `cw task start` writes to: the state directory's workflow-tasks
// ($XDG_STATE_HOME/agent-workflows or ~/.local/state/agent-workflows).
func hookTaskStore(root *cobra.Command, config Config) (string, error) {
	if store := config.Getenv("CW_TASK_STORE"); store != "" {
		return store, nil
	}
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", err
	}
	state, err := roots.stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "workflow-tasks"), nil
}

// sessionStartContext returns the instructions file, followed by the doctor
// summary when the plugin option asks for it and it can be produced in time.
func sessionStartContext(ctx context.Context, root *cobra.Command, config Config, instructions, cache string) (string, bool) {
	data, err := os.ReadFile(instructions)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return "", false
	}
	text := strings.TrimRight(string(data), "\n")
	if config.Getenv(sessionDoctorOption) != "true" {
		return text, true
	}
	input, err := readHookInput(config.In)
	dir := input.CWD
	if err != nil {
		if dir, err = config.WorkingDir(); err != nil {
			return text, true
		}
	}
	if summary, ok := cachedDoctorSummary(ctx, root, config, dir, cache); ok {
		text += "\n\n" + summary
	}
	return text, true
}

// cachedDoctorSummary returns the one-line doctor summary for dir, served from the
// cache when it is younger than an hour. Any failure yields no summary.
func cachedDoctorSummary(ctx context.Context, root *cobra.Command, config Config, dir, cache string) (string, bool) {
	selected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	path := ""
	if cache != "" {
		key := sha256.Sum256([]byte(selected))
		path = filepath.Join(cache, "doctor", hex.EncodeToString(key[:])+".json")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) < hookDoctorCacheAge {
			if data, err := os.ReadFile(path); err == nil && jsontext.Value(data).IsValid() {
				return strings.TrimSpace(string(data)), true
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, hookDoctorTimeout)
	defer cancel()
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", false
	}
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return "", false
	}
	report, err := projectctx.Doctor(ctx, projectctx.DoctorOptions{Repo: selected, Home: roots.home, CodexHome: roots.codex, ClaudeHome: roots.claude, ConfigDir: directory, Timeout: hookDoctorTimeout, Getenv: config.Getenv})
	if err != nil {
		return "", false
	}
	data, err := json.Marshal(summarizeDoctor(report), json.Deterministic(true))
	if err != nil {
		return "", false
	}
	if path != "" {
		writeDoctorCache(path, data)
	}
	return string(data), true
}

// writeDoctorCache replaces the cache entry atomically; a failure only costs
// the next session a fresh doctor run.
func writeDoctorCache(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".doctor-*")
	if err != nil {
		return
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return
	}
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	_ = os.Rename(temporary.Name(), path)
}
