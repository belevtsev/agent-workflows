package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
	"github.com/spf13/cobra"
)

// Claude Code plugin hooks. Both commands read the hook's JSON input from
// stdin and write the hook's JSON decision to stdout. They fail open: every
// condition that is not a positive gate decision, including any internal
// error, exits 0 without output, so a damaged record, an unreadable store or a
// missing git never blocks the user's own commands.
const (
	hookInputLimit      = 1 << 20
	hookGitTimeout      = 5 * time.Second
	hookDoctorTimeout   = 5 * time.Second
	hookDoctorCacheAge  = time.Hour
	enforceVerifyOption = "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"
	sessionDoctorOption = "CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"
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
		Short: "PreToolUse hook: deny git commit and push while the newest substantial task record for the repository lacks a passing cw task verify on the current source",
		Long: "Reads the PreToolUse JSON from stdin. When the Bash command is a git commit or git push, the directory is inside a Git repository, " +
			"and the task store holds a substantial record for that repository started within 14 days whose verification has not passed on the current source, " +
			"it prints a deny decision. Every other case, including any internal error, exits 0 without output. " +
			"Set the plugin option enforce_verify to false (" + enforceVerifyOption + "=false) to disable the gate.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			decision, ok := prePublishDecision(cmd.Context(), root, config)
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

// prePublishDecision returns the deny decision for the hook input on stdin,
// or false when the command may proceed silently.
func prePublishDecision(ctx context.Context, root *cobra.Command, config Config) (hookOutput, bool) {
	if config.Getenv(enforceVerifyOption) == "false" {
		return hookOutput{}, false
	}
	input, err := readHookInput(config.In)
	if err != nil {
		return hookOutput{}, false
	}
	targets := gitPublishDirs(input.ToolInput.Command, input.CWD)
	if len(targets) == 0 {
		return hookOutput{}, false
	}
	store, err := hookTaskStore(root, config)
	if err != nil {
		return hookOutput{}, false
	}
	for _, target := range targets {
		roots, err := repositoryRoots(ctx, target)
		if err != nil {
			continue
		}
		open, err := taskflow.FindOpen(ctx, store, roots, time.Now(), taskflow.SourceDigest)
		if err != nil || open == nil {
			continue
		}
		reason := fmt.Sprintf("agent-workflows: task %s (%s) for this repository has no passing cw task verify on the current source; run `cw task verify --task %s --summary` and retry, or set enforce_verify=false in the plugin config", open.ID, open.Plan.Title, open.Directory)
		return hookOutput{hookSpecificOutput{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}, true
	}
	return hookOutput{}, false
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

// shellPrefixWords are words that may precede a simple command without being
// its program: shell keywords that open a compound command and the common
// command prefixes. Claude Code's own `Bash(git commit *)` rule decides
// whether the hook runs; the hook still has to find git inside such forms.
var shellPrefixWords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "{": true, "!": true, "time": true, "command": true, "exec": true, "env": true, "nohup": true, "builtin": true}

// gitPublishDirs returns the working directory of every git commit or push
// in a shell command line, resolved from cwd through any preceding `cd DIR`
// or `pushd DIR` and git's own -C DIR options, without duplicates and in
// command order. The split is deliberately simple: simple commands are
// separated by `;`, `|`, `&`, newlines, parentheses and backticks, and words
// by whitespace with surrounding quotes removed. A directory change inside
// parentheses or backticks ends with them, as in a subshell. After a
// directory change the hook cannot follow (`cd` with options, `cd -`, `~`,
// `$VAR`, quoted paths with spaces, `popd`), the directory is unknown and
// later git commands are not checked until an absolute `cd` or -C, since the
// hook fails open. Redirections on a cd (`cd DIR >/dev/null 2>&1`) are
// ignored, and `cd "$(git rev-parse --show-toplevel)"` stays in the same
// repository.
func gitPublishDirs(command, cwd string) []string {
	command = toplevelSubstitution.ReplaceAllString(command, ".")
	dir := cwd
	var scopes []string
	inBacktick := false
	var dirs []string
	visit := func(segment string) {
		words := commandWords(segment)
		if len(words) > 0 && (words[0] == "cd" || words[0] == "pushd" || words[0] == "popd") {
			words = withoutRedirections(words)
			if len(words) == 2 && words[0] != "popd" && followablePath(words[1]) {
				dir = resolveDir(dir, words[1])
			} else {
				dir = ""
			}
			return
		}
		target, subcommand := gitInvocation(words, dir)
		if target != "" && (subcommand == "commit" || subcommand == "push") && !containsString(dirs, target) {
			dirs = append(dirs, target)
		}
	}
	start := 0
	for i, r := range command {
		switch r {
		case ';', '|', '&', '\n', '(', ')', '`':
		default:
			continue
		}
		visit(command[start:i])
		start = i + 1
		opening := r == '(' || (r == '`' && !inBacktick)
		closing := r == ')' || (r == '`' && inBacktick)
		if r == '`' {
			inBacktick = !inBacktick
		}
		if opening {
			scopes = append(scopes, dir)
		}
		if closing && len(scopes) > 0 {
			dir = scopes[len(scopes)-1]
			scopes = scopes[:len(scopes)-1]
		}
	}
	visit(command[start:])
	return dirs
}

// toplevelSubstitution matches a command substitution that names the
// current repository's top level. A cd into it stays in the same repository,
// so it is rewritten to `.` before the command is split.
var toplevelSubstitution = regexp.MustCompile(`"?\$\(\s*git\s+rev-parse\s+--show-toplevel\s*\)"?`)

// withoutRedirections drops redirection words (`>FILE`, `2>FILE`, `>>FILE`,
// `<FILE`, or the operator alone followed by its target word) from a simple
// command. `&` is already a command separator, so `2>&1` and `&>FILE` arrive
// here split.
func withoutRedirections(words []string) []string {
	kept := words[:1:1]
	for i := 1; i < len(words); i++ {
		operator := strings.TrimLeft(words[i], "0123456789")
		if !strings.HasPrefix(operator, ">") && !strings.HasPrefix(operator, "<") {
			kept = append(kept, words[i])
			continue
		}
		if strings.Trim(operator, "<>") == "" {
			i++ // the operator stands alone; its target is the next word
		}
	}
	return kept
}

// followablePath reports whether a cd argument is a literal path the hook
// can resolve without a shell: not an option, not `-`, and free of `~`, `$`
// and glob characters.
func followablePath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "-") && !strings.ContainsAny(path, "~$*?[{")
}

// commandWords splits a simple command into words, drops leading NAME=value
// assignments and shell prefix words, and removes one pair of surrounding
// quotes from each word.
func commandWords(segment string) []string {
	var words []string
	for _, word := range strings.Fields(segment) {
		words = append(words, unquote(word))
	}
	for len(words) > 0 {
		word := words[0]
		if shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")) {
			words = words[1:]
			continue
		}
		break
	}
	return words
}

func unquote(word string) string {
	if len(word) >= 2 && (word[0] == '"' || word[0] == '\'') && word[len(word)-1] == word[0] {
		return word[1 : len(word)-1]
	}
	return word
}

// gitInvocation returns the directory git runs in and its subcommand when
// words invoke git, or "" when they do not. git's global options are skipped;
// the ones that take a separate argument consume it, and each -C DIR moves
// the directory like git does.
func gitInvocation(words []string, dir string) (string, string) {
	if len(words) == 0 || filepath.Base(words[0]) != "git" {
		return "", ""
	}
	for i := 1; i < len(words); i++ {
		word := words[i]
		if !strings.HasPrefix(word, "-") {
			return dir, word
		}
		switch word {
		case "-C":
			if i+1 < len(words) {
				dir = resolveDir(dir, words[i+1])
			}
			i++
		case "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--super-prefix", "--config-env":
			i++
		}
	}
	return "", ""
}

// resolveDir follows a cd-like path change from dir. A relative path from
// an unknown directory ("") stays unknown. Paths the hook cannot resolve
// lexically, such as $VAR after git -C, are left to git, which then fails
// and lets the command through.
func resolveDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, path)
}

// repositoryRoots returns the canonical worktree root of the repository that
// contains dir and, for a linked worktree, the main worktree's root as well,
// so a record started in either location gates both.
func repositoryRoots(ctx context.Context, dir string) ([]string, error) {
	toplevel, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	roots := []string{}
	add := func(path string) {
		if canonical, err := filepath.EvalSymlinks(path); err == nil && !containsString(roots, canonical) {
			roots = append(roots, canonical)
		}
	}
	add(toplevel)
	if len(roots) == 0 {
		return nil, errors.New("worktree root is unavailable")
	}
	common, err := gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		// Git before 2.31 has no --path-format; its plain form is relative to dir.
		common, err = gitOutput(ctx, dir, "rev-parse", "--git-common-dir")
	}
	if err == nil {
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		if filepath.Base(common) == ".git" {
			add(filepath.Dir(common))
		}
	}
	return roots, nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, hookGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimRight(string(output), "\r\n")
	if value == "" {
		return "", errors.New("git printed nothing")
	}
	return value, nil
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
	return filepath.Join(roots.state, "workflow-tasks"), nil
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
