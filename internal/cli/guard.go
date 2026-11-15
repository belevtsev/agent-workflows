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
	Tool  string `json:"tool_name"`
	Dir   string `json:"cwd"`
	Input struct {
		Command any    `json:"command"`
		Path    string `json:"file_path"`
	} `json:"tool_input"`
}

// runGuard is the plugin's job before a tool call. It reads the host's notice
// from standard input and speaks up in two cases only: a git call that skips
// git's hooks, and an edit of kitsu.toml, of git's hooks or of kitsu's records. It
// asks where the host can ask and denies with the reason where it cannot. It
// never judges a commit or a push: the git hooks do that.
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
	reason := guardReason(ctx, call)
	if reason == "" {
		return nil
	}
	decision := "ask"
	if *host == "codex" {
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

// guardReason is why the user should be asked, or nothing.
func guardReason(ctx context.Context, call toolCall) string {
	dir := call.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	repo, err := gitx.Open(ctx, dir)
	if err != nil || repo.Root == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); err != nil {
		return "" // nothing is gated here
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
	switch call.Tool {
	case "Bash", "PowerShell":
		if how := shellwords.SkipsHooks(command); how != "" {
			return "This command " + how + ", which skips the commit gate: the git hooks of kitsu would not deny the commit or the push."
		}
		return editReason(ctx, repo, dir, patchPaths(command)) // a patch given to the shell
	case "Edit", "Write", "MultiEdit":
		return editReason(ctx, repo, dir, []string{call.Input.Path})
	case "apply_patch":
		return editReason(ctx, repo, dir, patchPaths(command))
	}
	return ""
}

// editReason says why an edit of paths, relative to dir or absolute, needs
// the user. A path is compared as the file system sees it, with each folder
// above it, so another spelling of a name, a link or a file that does not
// exist yet is judged like the place it leads to.
func editReason(ctx context.Context, repo *gitx.Repo, dir string, paths []string) string {
	hooks, _, _ := repo.HooksDir(ctx)
	const hook = "This edit changes a git hook of this clone, and the git hooks of kitsu judge each commit and push."
	guarded := [][2]string{
		{filepath.Join(repo.Root, decl.FileName), "This edit changes kitsu.toml, and with it the checks this repository requires before a commit or a push."},
		{filepath.Join(repo.Root, "kitsu-advisers.toml"), "This edit changes adviser commands, model selections, role access or required consultations."},
		{filepath.Join(repo.Root, "kitsu-roles.toml"), "This edit changes role models, reasoning effort or write limits."},
		{filepath.Join(repo.CommonDir, "hooks"), hook},
		{hooks, hook},
		{filepath.Join(repo.CommonDir, "config"), "This edit changes the git configuration of this clone, which names the hooks git runs."},
		{filepath.Join(repo.CommonDir, "kitsu"), "This edit changes the records of kitsu, which hold the passing runs that the commit gate reads."},
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		// Configuration creation must be guarded before a target inode exists.
		for _, g := range guarded[:3] {
			if filepath.Clean(path) == filepath.Clean(g[0]) {
				return g[1]
			}
		}
		for at, above := filepath.Clean(path), ""; at != above; at, above = filepath.Dir(at), at {
			info, err := os.Stat(at)
			for _, g := range guarded {
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
