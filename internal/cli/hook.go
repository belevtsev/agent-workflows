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

	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// Claude Code and Codex plugin hooks. Both commands read the hook's JSON input
// from stdin and write the hook's JSON decision to stdout. Under Codex, which
// does not support an ask, the pre-publish hook denies instead, except where
// the ask is about a record or store it cannot read. They fail open: every
// condition that is not a positive gate decision, including any internal
// error, exits 0 without output, so a missing git never blocks the user's own
// commands. Two exceptions ask, because allowing would skip the gate without
// a word: a governing task record that cannot be read, and a task store that
// cannot be located or read, for each git commit and git push the gate would
// judge.
const (
	hookInputLimit = 1 << 20
	// hookPublishTimeout bounds the pre-publish hook's Git and source-capture
	// work, whose capture probes allow far longer on their own. A record
	// whose source identity cannot be captured in time is skipped, while
	// other records can still gate. It stays below the 30 second hook
	// timeout in hooks/hooks.json, so cw finishes before it is killed.
	hookPublishTimeout    = 20 * time.Second
	hookDoctorCacheAge    = time.Hour
	enforceVerifyOption   = "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"
	sessionDoctorOption   = "CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"
	smallTaskPromptOption = "CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT"
)

type hookInput struct {
	CWD string `json:"cwd"`
	// TurnID and Model are Codex's documented extensions of the hook input;
	// Claude Code's PreToolUse input has neither, although it sends turn_id
	// for another event. testdata holds one recorded input from each host.
	TurnID string `json:"turn_id"`
	Model  string `json:"model"`
	// Source is how a session started, in SessionStart input only: startup,
	// resume, clear or compact (and fork under Codex).
	Source string `json:"source"`
	// SessionID and TranscriptPath name the session and its transcript, in
	// every hook input of both hosts; cw hook task-usage samples it.
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	ToolInput      struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// fromCodex reports whether Codex, not Claude Code, ran the hook: it needs
// both Codex members, so Claude Code adding one of them to PreToolUse does
// not turn its asks into denies.
func (input hookInput) fromCodex() bool {
	return input.TurnID != "" && input.Model != ""
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
	hook := &cobra.Command{Use: "hook", Short: "Claude Code plugin and git hooks: load conventions and gate git commit and push on cw task verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.AddCommand(hook)
	addGitHookCommand(root, hook, config)
	addTaskUsageHook(hook, config)
	hook.AddCommand(&cobra.Command{
		Use:   "pre-publish",
		Short: "PreToolUse hook: deny git commit and push while any open substantial task record for the repository lacks a passing cw task verify on the source being published",
		Long: "Reads the PreToolUse JSON from stdin. When the Bash command runs git commit or git push inside a Git repository and the task store holds " +
			"substantial, unabandoned records for that repository written within 14 days, it prints a deny decision naming every one whose verification has not passed " +
			"on the source being published, an ask decision when none gates but one cannot be read or the task store cannot be located or read, " +
			"and an ask decision for a push of a verified repository whose working tree has uncommitted tracked changes. " +
			"With no such record, it prints an ask decision when the change looks substantial (two of: more than 300 changed lines, more than one top-level directory, " +
			"code changed without tests). It also prints an ask decision, naming the task, for a cw task abandon, which needs the user's confirmation, " +
			"and one quoting the words of a cw --owner-approval, which only the user may give. " +
			"Under Codex, which cannot ask, an ask about an unreadable record or store lets the command run, and every other ask is a deny that carries the question. " +
			"Every other case, including any internal error, exits 0 without output. " +
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
		Short: "SessionStart hook: emit the instructions file, plus a line when the plugin and cw versions differ or Claude Code is too old, a line suggesting cw gate install once per open substantial task in a repository without git hooks, and a cached cw doctor summary when the plugin option session_doctor is true, as additional context; nothing when Codex resumes a thread",
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
	// When the store cannot be located or read, the gate still runs: it
	// decides which commands publish from a repository, and each of those
	// asks.
	path, err := hookTaskStore(root, config)
	store := &storeGuard{err: err}
	if err == nil {
		store.store = gate.TaskStore(path)
	}
	// The shell the agent runs commands in has the hook's HOME; without one,
	// the gate leaves `~` and `$HOME` unresolved.
	home, err := config.HomeDir()
	if err != nil {
		home = ""
	}
	decision := gate.Decide(ctx, gate.Input{Command: input.ToolInput.Command, CWD: input.CWD, Home: home, Enforce: enforce, Prompt: prompt},
		gate.Deps{Git: gate.ExecGit, Now: config.Now, Store: store})
	if store.failed {
		switch decision.Permission {
		case gate.Allow:
			// Like a record that cannot be read, a store that cannot be
			// located or read is nothing the user can fix from Codex, so
			// Codex lets it run.
			decision = gate.Decision{Permission: gate.Ask, Reason: storeFailureReason(store.err, store.store != nil), ProceedWithoutAsk: true}
		case gate.Ask:
			// Joined as the gate joins asks: the store's lets a host that
			// cannot ask proceed, so the joined ask does only when the other
			// one does. The ask before cw task abandon is such another one.
			decision.Reason += " " + storeFailureReason(store.err, store.store != nil)
		}
	}
	if input.fromCodex() {
		decision = codexDecision(decision)
	}
	if decision.Permission == gate.Allow {
		return hookOutput{}, false
	}
	return preToolUseDecision(string(decision.Permission), decision.Reason), true
}

// codexDecision adapts a decision to Codex, which parses an ask but runs the
// command without asking: an ask becomes a deny that carries the question,
// unless it is marked ProceedWithoutAsk, which lets the command run. Codex has
// no plugin config, so every remaining reason also says where its options are.
func codexDecision(decision gate.Decision) gate.Decision {
	switch {
	case decision.Permission == gate.Allow:
		return decision
	case decision.Permission == gate.Ask && decision.ProceedWithoutAsk:
		return gate.Decision{}
	case decision.Permission == gate.Ask:
		decision = gate.Decision{Permission: gate.Deny, Reason: "agent-workflows: Codex cannot ask for confirmation, so this command is denied where Claude Code would ask: " +
			strings.TrimPrefix(decision.Reason, "agent-workflows: ") +
			" Put the question to the user; to go ahead, the user runs the command outside Codex."}
	}
	decision.Reason += " Under Codex the plugin options are environment variables of the shell that starts Codex, such as " +
		enforceVerifyOption + "=false or " + smallTaskPromptOption + "=false."
	return decision
}

// storeFailureReason is the ask for a publication when err leaves the task
// records unknown. When the store was not located, err is a relative
// XDG_STATE_HOME or another failure to resolve the state directory, such as
// a home directory that cannot be resolved; when it was, err is why the
// store cannot be listed, such as its mode or a regular file in its place.
func storeFailureReason(err error, located bool) string {
	fix := "cw cannot locate the task store (" + err.Error() + "); set CW_TASK_STORE to it."
	switch {
	case located:
		fix = "cw cannot read the task store (" + err.Error() + "); make it a directory you can read, or set CW_TASK_STORE to one."
	case errors.Is(err, errRelativeStateHome):
		fix = "XDG_STATE_HOME must be an absolute path; set it, or set CW_TASK_STORE."
	}
	return "agent-workflows: " + fix + " Until then the commit and push gate cannot read the task records; confirm to run this command without it."
}

// storeGuard is the gate's store in the hook. It reads store, the task
// store, or, when the hook cannot locate one (store nil), fails every lookup
// with err. On a failed lookup the gate itself allows; failed reports that
// the gate reached a publication it would have judged against the records,
// which the hook then asks about, and err holds the first failure.
type storeGuard struct {
	store  gate.Store
	err    error
	failed bool
}

func (s *storeGuard) FindOpen(ctx context.Context, repo taskflow.Repository, now time.Time) ([]taskflow.OpenRecord, error) {
	if s.store == nil {
		s.failed = true
		return nil, s.err
	}
	open, err := s.store.FindOpen(ctx, repo, now)
	if err != nil && !s.failed {
		s.failed, s.err = true, err
	}
	return open, err
}

// Lookup reads a task directory for the ask before cw task abandon, which
// names its title. A failure only leaves the title out, so it does not count
// as the store failing. When the hook located no store it fails with err,
// and the ask names the directory alone.
func (s *storeGuard) Lookup(dir string) (taskflow.Record, error) {
	if s.store == nil {
		return taskflow.Record{}, s.err
	}
	return s.store.Lookup(dir)
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

// hookTaskStore returns $CW_TASK_STORE, an override that the hooks, cw
// consult and a task command given a task ID read, but that `cw task start`
// does not (it takes --store), else the store `cw task start` writes to: the
// state directory's workflow-tasks ($XDG_STATE_HOME/agent-workflows or
// ~/.local/state/agent-workflows).
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

// sessionStartContext returns the instructions file, followed by any version
// notes and the cw gate install line, and by the doctor summary when the
// plugin option asks for it and it can be produced in time.
//
// When Codex resumes a thread it returns "" and true, so the hook prints a
// SessionStart object without additionalContext. The thread already holds
// what earlier session starts added, and Codex adds the context again after
// a compaction, so a resume would only add a second copy. Printing nothing
// instead would make the hook command fall back to printing the file.
// Returning before gateInstallNote also keeps the resume from suggesting cw
// gate install beside the compact run Codex can start at the same moment.
func sessionStartContext(ctx context.Context, root *cobra.Command, config Config, instructions, cache string) (string, bool) {
	data, err := os.ReadFile(instructions)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return "", false
	}
	text := strings.TrimRight(string(data), "\n")
	// Only the cw gate install line, which the gate being off silences, and
	// the doctor summary need the session's directory; under Codex the
	// source decides whether there is anything to add.
	gate := config.Getenv(enforceVerifyOption) != "false"
	doctor := config.Getenv(sessionDoctorOption) == "true"
	codex := config.Getenv(codexPluginRoot) != ""
	dir, source := "", ""
	if gate || doctor || codex {
		dir, source = sessionInput(config)
	}
	if codex && source == "resume" {
		return "", true
	}
	var notes []string
	if note := versionNotes(config, instructions); note != "" {
		notes = append(notes, note)
	}
	if gate && dir != "" {
		if note := gateInstallNote(ctx, root, config, dir, cache); note != "" {
			notes = append(notes, note)
		}
	}
	if len(notes) > 0 {
		text += "\n\n" + strings.Join(notes, "\n")
	}
	if !doctor || dir == "" {
		return text, true
	}
	if summary, ok := cachedDoctorSummary(ctx, root, config, dir, cache); ok {
		text += "\n\n" + summary
	}
	return text, true
}

// The session-start deadlines. They are variables only so that tests can
// set them; cw never changes them.
var (
	// hookSessionInputTimeout bounds the session-start read of the hook
	// input, which the host has written by the time the hook runs.
	hookSessionInputTimeout = 2 * time.Second
	// hookDoctorTimeout bounds the session-start doctor summary.
	hookDoctorTimeout = 5 * time.Second
)

// sessionInput returns the session's directory and how the session started.
// The directory is the cwd of the hook input on stdin, else the working
// directory, which Claude Code and Codex both set to the session's directory;
// "" when neither is known. The source is the input's source, "" when the
// input cannot be read. A host that keeps stdin open would hold the read
// until it kills the hook, so the read gets hookSessionInputTimeout and is
// then abandoned. It runs in a goroutine with a timer rather than with a read
// deadline, which a pipe on darwin does not support.
func sessionInput(config Config) (dir, source string) {
	read := make(chan hookInput, 1)
	go func() {
		input, err := readHookInput(config.In)
		if err != nil {
			input = hookInput{}
		}
		read <- input
	}()
	timer := time.NewTimer(hookSessionInputTimeout)
	defer timer.Stop()
	select {
	case input := <-read:
		if input.CWD != "" {
			return input.CWD, input.Source
		}
	case <-timer.C:
	}
	dir, err := config.WorkingDir()
	if err != nil {
		return "", ""
	}
	return dir, ""
}

// cachedDoctorSummary returns the one-line doctor summary for dir, served from the
// cache when it is younger than an hour. Any failure yields no summary. It
// leaves out cw doctor's git_hooks check: the cw gate install line covers it.
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
	report, err := doctor.Doctor(ctx, doctor.DoctorOptions{Repo: selected, Home: roots.home, CodexHome: roots.codex, ClaudeHome: roots.claude, ConfigDir: directory, Timeout: hookDoctorTimeout, Getenv: config.Getenv})
	if err != nil {
		return "", false
	}
	data, err := json.Marshal(summarizeDoctor(report, false), json.Deterministic(true))
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
