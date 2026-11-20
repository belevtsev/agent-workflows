package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/shellwords"
)

// toolCall is what an agent host tells a hook before it runs a tool. Claude
// Code and Codex use the same field names. The input holds a shell command,
// the path of a file edit, or a Codex patch as the command.
type toolCall struct {
	Tool    string `json:"tool_name"`
	Dir     string `json:"cwd"`
	Session string `json:"session_id"`
	Input   struct {
		Command     any    `json:"command"`
		Path        string `json:"file_path"`
		Model       string `json:"model"`
		Effort      string `json:"effort"`
		Reasoning   string `json:"reasoning_effort"`
		ForkContext *bool  `json:"fork_context"`
		ForkTurns   string `json:"fork_turns"`
	} `json:"tool_input"`
}

// runGuard responds to the host before a tool call. It checks approval for
// declared commands and warns about changes to the local workflow and gate.
func runGuard(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("guard")
	host := flags.String("host", "claude", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash || (*host != "claude" && *host != "codex") {
		return usagef("usage: kitsu guard [--host claude|codex], with the host's notice on standard input")
	}
	var call toolCall
	if json.NewDecoder(cfg.In).Decode(&call) != nil {
		return nil // a notice the guard cannot read is one it does not act on
	}
	reason, developerOnly := guardReason(ctx, call)
	if native := guardWorkflowSpawn(ctx, *host, call); native != "" {
		reason, developerOnly = native, true
	}
	if reason == "" {
		return nil
	}
	decision := "ask"
	if developerOnly {
		decision = "deny"
	} else if *host == "codex" {
		// Codex reads "ask" and does not support it yet, so the call would
		// go through unasked.
		decision = "deny"
		reason += " This host cannot ask from a hook, so the call is refused. Ask the user; they can run it themselves."
	}
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       decision,
		"permissionDecisionReason": reason,
	}})
	if err != nil {
		return failf("%v", err)
	}
	fmt.Fprintln(cfg.Out, string(out))
	return nil
}

// guardReason explains the decision and whether it requires the developer to
// act in a terminal instead of responding to a host permission prompt.
func guardReason(ctx context.Context, call toolCall) (string, bool) {
	dir := call.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	repo, err := gitx.Open(ctx, dir)
	if err != nil || repo.Root == "" {
		return "", false
	}
	// A host gives a command as one text; a list of words is read as one
	// line and, for a word that is itself a command line, word by word.
	command, _ := call.Input.Command.(string)
	if words, ok := call.Input.Command.([]any); ok {
		command = fmt.Sprintln(words...)
		for _, word := range words {
			command += fmt.Sprintln(word)
		}
	}
	var paths []string
	switch call.Tool {
	case "Bash", "PowerShell":
		if why := guardCheckTrust(ctx, repo, command); why != "" {
			return why, true
		}
		if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); err == nil {
			if how := uxChangesPolicy(command); how != "" && !uxRepositoryRoleCommand(repo.Root, command) {
				return "This command " + how + ".", false
			}
			if how := shellwords.SkipsHooks(command); how != "" {
				return "This command " + how + ", which skips the commit gate: the git hooks of kitsu would not deny the commit or the push.", false
			}
		}
		paths = patchPaths(command)
	case "Edit", "Write", "MultiEdit":
		paths = []string{call.Input.Path}
	case "apply_patch":
		paths = patchPaths(command)
	}
	reason := editReason(ctx, repo, dir, paths)
	return reason, reason == checkTrustEditReason
}

const checkTrustEditReason = "The developer manages local check approvals in their own terminal."

// editReason says why an edit of paths, relative to dir or absolute, needs
// the user. A path is compared as the file system sees it, with each folder
// above it, so another spelling of a name, a link or a file that does not
// exist yet is judged like the place it leads to.
func editReason(ctx context.Context, repo *gitx.Repo, dir string, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var guarded [][2]string
	if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); err == nil {
		hooks, _, _ := repo.HooksDir(ctx)
		const hook = "This edit changes a git hook of this clone, and the git hooks of kitsu judge each commit and push."
		guarded = [][2]string{
			{filepath.Join(repo.Root, decl.FileName), "This edit changes kitsu.toml, and with it the checks this repository requires before a commit or a push."},
			{filepath.Join(repo.Root, "kitsu-advisers.toml"), "This edit changes adviser commands, model selections, role access or required consultations."},
			{filepath.Join(repo.Root, "kitsu-roles.toml"), "This edit changes role models, reasoning effort or write limits."},
			{filepath.Join(repo.GitDir, uxCheckoutConfigFile), "This edit changes this checkout's CLI host preference."},
			{filepath.Join(repo.CommonDir, "hooks"), hook},
			{hooks, hook},
			{filepath.Join(repo.CommonDir, "config"), "This edit changes the git configuration of this clone, which names the hooks git runs."},
			{filepath.Join(repo.CommonDir, "kitsu"), "This edit changes the records of kitsu, which hold the passing runs that the commit gate reads."},
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		guarded = append(guarded, [2]string{filepath.Join(dir, "kitsu", "check-trust"), checkTrustEditReason})
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		for at, above := filepath.Clean(path), ""; at != above; at, above = filepath.Dir(at), at {
			info, err := os.Stat(at)
			for _, g := range guarded {
				// Protect the destination before a target inode exists.
				if g[0] != "" && at == filepath.Clean(g[0]) {
					return g[1]
				}
				if target, targetErr := os.Stat(g[0]); err == nil && targetErr == nil && os.SameFile(info, target) {
					return g[1]
				}
			}
		}
	}
	return ""
}

// patchPaths lists the files a Codex patch touches.
func patchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, head := range []string{"*** Update File:", "*** Add File:", "*** Delete File:", "*** Move to:"} {
			if rest, ok := strings.CutPrefix(line, head); ok {
				paths = append(paths, strings.TrimSpace(rest))
			}
		}
	}
	return paths
}
