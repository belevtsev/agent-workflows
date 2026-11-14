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
)

// toolCall is what an agent host tells a hook before it runs a tool. Claude
// Code and Codex use the same field names.
type toolCall struct {
	Tool  string          `json:"tool_name"`
	Dir   string          `json:"cwd"`
	Input json.RawMessage `json:"tool_input"`
}

// toolInput holds the two fields the guard reads. A shell tool gives the
// command; a file-editing tool gives the path; Codex gives a patch as the
// command of `apply_patch`.
type toolInput struct {
	Command string `json:"command"`
	Path    string `json:"file_path"`
}

// runGuard is the plugin's job before a tool call. It reads the host's
// notice from standard input and speaks up in two cases only: a shell
// command whose text holds --no-verify, and a file edit of cw.toml. It asks
// the user where the host can ask and denies with the reason where it
// cannot. It never judges a commit or a push: the git hooks do that.
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
	const edit = "This edit changes cw.toml, and with it the checks this repository requires before a commit or a push."
	switch call.Tool {
	case "Bash":
		if strings.Contains(input.Command, "--no-verify") {
			return "This command holds --no-verify, which skips the commit gate: the git hooks of cw would not judge the commit or the push."
		}
	case "Edit", "Write", "MultiEdit":
		if samePath(input.Path, declaration, dir) {
			return edit
		}
	case "apply_patch":
		for _, path := range patchPaths(input.Command) {
			if samePath(path, declaration, dir) {
				return edit
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

// samePath says whether path, which may be relative to dir, names the file
// target. Directories are resolved, because a host may give the working
// directory through a symbolic link.
func samePath(path, target, dir string) bool {
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return resolved(path) == resolved(target)
}

func resolved(path string) string {
	path = filepath.Clean(path)
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}
