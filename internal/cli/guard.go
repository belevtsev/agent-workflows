package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// toolCall is what an agent host tells a hook before it runs a tool. Claude
// Code and Codex use the same field names.
type toolCall struct {
	Tool  string          `json:"tool_name"`
	Dir   string          `json:"cwd"`
	Input json.RawMessage `json:"tool_input"`
}

// toolInput holds a shell command, the path of a file edit, or a Codex patch.
type toolInput struct {
	Command string `json:"command"`
	Path    string `json:"file_path"`
}

// runGuard is the plugin's job before a tool call. It reads the host's notice
// from standard input and speaks up in two cases only: a git call that skips
// git's hooks, and an edit of cw.toml, of git's hooks or of cw's records. It
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
		return usagef("usage: cw guard [--host claude|codex], with the host's notice on standard input")
	}
	data, err := io.ReadAll(io.LimitReader(cfg.In, 16<<20))
	if err != nil {
		return nil
	}
	var call toolCall
	if json.Unmarshal(data, &call) != nil {
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
	var input toolInput
	if json.Unmarshal(call.Input, &input) != nil {
		return ""
	}
	dir := call.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	repo, err := gitx.Open(ctx, dir)
	if err != nil || repo.Root == "" {
		return ""
	}
	declaration := filepath.Join(repo.Root, decl.FileName)
	if _, err := os.Stat(declaration); err != nil {
		return "" // nothing is gated here
	}
	switch call.Tool {
	case "Bash":
		if how := shellwords.SkipsHooks(input.Command); how != "" {
			return "This command " + how + ", which skips the commit gate: the git hooks of cw would not judge the commit or the push."
		}
	case "Edit", "Write", "MultiEdit":
		return editReason([]string{input.Path}, dir, declaration, repo.CommonDir)
	case "apply_patch":
		return editReason(patchPaths(input.Command), dir, declaration, repo.CommonDir)
	}
	return ""
}

// editReason says why an edit of paths, relative to dir or absolute, needs the user. Directories
// are resolved as far as they exist, since a host may give dir through a link and an edit may create some.
func editReason(paths []string, dir, declaration, common string) string {
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		switch path = resolved(path); {
		case path == resolved(declaration):
			return "This edit changes cw.toml, and with it the checks this repository requires before a commit or a push."
		case strings.HasPrefix(path, resolved(filepath.Join(common, "hooks"))+string(filepath.Separator)):
			return "This edit changes a git hook of this clone, and the git hooks of cw judge each commit and push."
		case strings.HasPrefix(path, resolved(filepath.Join(common, "cw"))+string(filepath.Separator)):
			return "This edit changes the records of cw, which hold the passing runs that the commit gate reads."
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

func resolved(path string) string {
	path = filepath.Clean(path)
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	} else if filepath.Dir(path) == path {
		return path
	}
	return filepath.Join(resolved(filepath.Dir(path)), filepath.Base(path))
}
