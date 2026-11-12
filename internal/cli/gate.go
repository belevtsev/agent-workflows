package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
	"github.com/belevtsev/agent-workflows/v3/internal/publish"
	"github.com/spf13/cobra"
)

// gateHookNames are the git hooks cw gate manages, in the order it reports
// them.
var gateHookNames = []string{"pre-commit", "pre-push"}

// gateHookMarker is the comment line of the scripts cw gate install writes.
// A hook is cw's own only when its bytes equal the script exactly.
const gateHookMarker = "# agent-workflows: written by cw gate install; cw gate uninstall deletes this file"

// The states of one hook file that cw gate status reports.
const (
	gateInstalled = "installed"
	gateAbsent    = "absent"
	gateForeign   = "foreign"
)

// gateGitTimeout bounds each git command cw gate runs.
const gateGitTimeout = 10 * time.Second

var (
	// gateAfterRead, when a test sets it, runs once cw gate has read a hook
	// it is about to write, replace or remove: where another program could
	// put a file of its own there.
	gateAfterRead func(path string)
	// gateLink puts a file in place without replacing one; a test replaces
	// it to stand for a file system without hard links.
	gateLink = os.Link
)

// gateHookScript is the hook cw gate install writes for the git hook name. It
// runs the first cw on git's PATH and, like the plugin hook, lets git go ahead
// when there is none.
func gateHookScript(name string) []byte {
	return []byte("#!/bin/sh\n" + gateHookMarker + "\ncommand -v cw >/dev/null 2>&1 || exit 0; exec cw hook git " + name + " \"$@\"\n")
}

// gateHookLine is the line a person adds to a hook cw gate did not write, so
// that hook runs the gate first and stops git when it blocks. It keeps the
// hook's standard input, where git lists the refs to push, for the rest of
// the hook.
func gateHookLine(name string) string {
	return "if command -v cw >/dev/null 2>&1; then cw hook git " + name + " \"$@\" </dev/null || exit $?; fi"
}

// gateHooks is the hooks directory git uses for a repository, with symbolic
// links resolved.
type gateHooks struct {
	dir string
	// gitDir is the repository's git directory, which its worktrees share.
	gitDir string
	// shared says why other repositories use the hooks directory too; empty
	// when they do not.
	shared string
}

type gateStatusReport struct {
	HooksPath string           `json:"hooks_path"`
	Shared    bool             `json:"shared"`
	Hooks     []gateHookStatus `json:"hooks"`
}

type gateHookStatus struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

func addGateCommands(root *cobra.Command, config Config) {
	gate := &cobra.Command{
		Use:   "gate",
		Short: "Install, remove or show the git pre-commit and pre-push hooks that run the commit gate",
		Long: "cw gate install writes git's pre-commit and pre-push hooks for one repository, so git itself runs cw hook git before each commit and push, " +
			"however it starts: an alias, a script or a terminal. The hooks go in the directory git runs them from (git rev-parse --git-path hooks), " +
			"which follows core.hooksPath and is shared by every worktree of the repository. cw gate never replaces or removes a hook it did not write, " +
			"and changes a hooks directory that other repositories use too only with --shared.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.AddCommand(gate)
	// define adds one subcommand with --repo, and --shared when it writes;
	// run gets the hooks directory of that repository.
	define := func(command *cobra.Command, writes bool, run func(cmd *cobra.Command, hooks gateHooks) error) *cobra.Command {
		var repo string
		var shared bool
		command.Args = noPositional
		command.Flags().StringVar(&repo, "repo", "", "Repository directory (default: the working directory)")
		if writes {
			command.Flags().BoolVar(&shared, "shared", false, "Change a hooks directory that other repositories use too")
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			if dryRun, _ := root.PersistentFlags().GetBool("dry-run"); dryRun && writes {
				return usage(fmt.Errorf("%s rejects --dry-run", cmd.CommandPath()))
			}
			dir, err := absolute(config, cmp.Or(repo, "."))
			if err != nil {
				return err
			}
			hooks, err := resolveGateHooks(cmd.Context(), dir)
			if err != nil {
				return err
			}
			if writes && hooks.shared != "" && !shared {
				hooks.report(cmd.OutOrStdout())
				fmt.Fprintf(cmd.ErrOrStderr(), "cw gate: %s can serve other repositories too, so cw changed nothing; run %s again with --shared to change it anyway.\n",
					hooks.dir, cmd.CommandPath())
				return reportedExit(ExitRefused)
			}
			return run(cmd, hooks)
		}
		gate.AddCommand(command)
		return command
	}
	define(&cobra.Command{
		Use:   "install [--repo DIR] [--shared]",
		Short: "Write git's pre-commit and pre-push hooks that run cw hook git",
		Long: "Write pre-commit and pre-push into the repository's hooks directory. Each runs the first cw on git's PATH, and exits 0 without checking when there is none. " +
			"A hook file cw gate did not write, such as one from a hook manager, is left unchanged: cw prints the line to add to it and exits 4 (refused). " +
			"It also exits 4 without writing when other repositories use the hooks directory (core.hooksPath from the global or system git configuration, " +
			"or a directory outside the repository), unless --shared is given, and when the hooks directory is missing outside the git directory.",
	}, true, func(cmd *cobra.Command, hooks gateHooks) error {
		hooks.report(cmd.OutOrStdout())
		if _, err := os.Lstat(hooks.dir); errors.Is(err, fs.ErrNotExist) {
			if !within(hooks.dir, hooks.gitDir) {
				fmt.Fprintf(cmd.ErrOrStderr(), "cw gate: the hooks directory %s does not exist, and cw creates one only inside the git directory %s; create it first.\n",
					hooks.dir, hooks.gitDir)
				return reportedExit(ExitRefused)
			}
			if err := os.MkdirAll(hooks.dir, 0o755); err != nil { //nolint:gosec // G301: git's hooks directory keeps git's usual 0755.
				return err
			}
		}
		refused := false
		for _, name := range gateHookNames {
			path := filepath.Join(hooks.dir, name)
			action, err := installGateHook(path, name)
			if err != nil {
				return fmt.Errorf("install %s: %w", path, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, action)
			if action == "refused" {
				refused = true
				fmt.Fprintf(cmd.ErrOrStderr(), "cw gate: %s was not written by cw gate install, so it is left unchanged. "+
					"To run the gate from it, add this line after its first line (with a hook manager, add it to the manager's %s hook instead):\n%s\n",
					path, name, gateHookLine(name))
			}
		}
		if refused {
			return reportedExit(ExitRefused)
		}
		return nil
	})
	define(&cobra.Command{
		Use:   "uninstall [--repo DIR] [--shared]",
		Short: "Remove the git hooks cw gate install wrote",
		Long: "Remove pre-commit and pre-push from the repository's hooks directory when they are exactly what cw gate install writes; any other file is left unchanged. " +
			"A hooks directory inside the git directory that this leaves empty is removed too. Other repositories' hooks directories need --shared, as for install.",
	}, true, func(cmd *cobra.Command, hooks gateHooks) error {
		hooks.report(cmd.OutOrStdout())
		removed := false
		for _, name := range gateHookNames {
			path := filepath.Join(hooks.dir, name)
			action, err := uninstallGateHook(path, name)
			if err != nil {
				return fmt.Errorf("uninstall %s: %w", path, err)
			}
			removed = removed || action == "removed"
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, action)
		}
		// Install may have created the directory; Remove leaves one that is
		// not empty.
		if removed && hooks.dir != hooks.gitDir && within(hooks.dir, hooks.gitDir) && os.Remove(hooks.dir) == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "removed the empty hooks directory")
		}
		return nil
	})
	var asJSON bool
	status := define(&cobra.Command{
		Use:   "status [--repo DIR] [--json]",
		Short: "Show whether each git hook is installed, absent or foreign",
		Long: "Print the repository's hooks directory, whether other repositories use it too, and, for pre-commit and pre-push, installed (exactly what cw gate install writes), " +
			"absent, or foreign (any other file). --json prints {hooks_path, shared, hooks: [{name, path, state}]}.",
	}, false, func(cmd *cobra.Command, hooks gateHooks) error {
		report, err := gateStatus(hooks)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		hooks.report(cmd.OutOrStdout())
		for _, hook := range report.Hooks {
			line := hook.Name + ": " + hook.State
			if hook.State == gateForeign {
				line += " (not written by cw gate install; cw gate install prints the line to add to it)"
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return nil
	})
	status.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
}

// gateStatus returns what cw gate status reports for the hooks directory.
func gateStatus(hooks gateHooks) (gateStatusReport, error) {
	report := gateStatusReport{HooksPath: hooks.dir, Shared: hooks.shared != "", Hooks: []gateHookStatus{}}
	for _, name := range gateHookNames {
		path := filepath.Join(hooks.dir, name)
		state, _, err := readGateHook(path, name)
		if err != nil {
			return gateStatusReport{}, fmt.Errorf("read %s: %w", path, err)
		}
		report.Hooks = append(report.Hooks, gateHookStatus{Name: name, Path: path, State: state})
	}
	return report, nil
}

// report writes the hooks directory and why other repositories use it, if
// they do.
func (hooks gateHooks) report(out io.Writer) {
	fmt.Fprintf(out, "git hooks directory: %s\n", hooks.dir)
	if hooks.shared != "" {
		fmt.Fprintf(out, "shared: %s, so other repositories that use it run these hooks too\n", hooks.shared)
	}
}

// resolveGateHooks returns the hooks directory git runs the hooks of the
// repository at requested from.
func resolveGateHooks(ctx context.Context, requested string) (gateHooks, error) {
	// Git reports paths relative to its working directory, whose symbolic
	// links the kernel has resolved, so they are joined to the resolved
	// directory; --path-format=absolute would need git 2.31.
	dir, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return gateHooks{}, fmt.Errorf("cannot locate the git hooks directory of %s: %w", requested, err)
	}
	out, err := gateGit(ctx, dir, "rev-parse", "--git-path", "hooks", "--git-common-dir")
	paths := strings.Split(out, "\n")
	if err == nil && len(paths) != 2 {
		err = fmt.Errorf("git rev-parse printed %q", out)
	}
	if err != nil {
		return gateHooks{}, fmt.Errorf("cannot locate the git hooks directory of %s: %w", requested, err)
	}
	resolve := func(path string) string {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		return canonicalPath(path)
	}
	hooks := gateHooks{dir: resolve(paths[0]), gitDir: resolve(paths[1])}
	areas := []string{hooks.gitDir}
	// A bare repository has no work tree.
	if top, err := gateGit(ctx, dir, "rev-parse", "--show-toplevel"); err == nil && top != "" {
		areas = append(areas, resolve(top))
	}
	// Git before 2.26 has no --show-scope; only the location then counts.
	scope := ""
	if setting, err := gateGit(ctx, dir, "config", "--show-scope", "--get", "core.hooksPath"); err == nil {
		scope, _, _ = strings.Cut(setting, "\t")
	}
	switch {
	case scope != "" && scope != "local" && scope != "worktree":
		hooks.shared = "core.hooksPath is set in the " + scope + " git configuration"
	case !slices.ContainsFunc(areas, func(area string) bool { return within(hooks.dir, area) }):
		hooks.shared = "it is outside this repository's git directory and work tree"
	}
	return hooks, nil
}

// canonicalPath resolves the symbolic links of the longest part of path that
// exists.
func canonicalPath(path string) string {
	rest := ""
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, rest)
		}
		if filepath.Dir(current) == current {
			return filepath.Clean(path)
		}
		rest = filepath.Join(filepath.Base(current), rest)
	}
}

// within reports whether path is dir or lies below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// gateGit runs git in dir with cw's environment, without the variables that
// would select another repository, and returns its output without the
// trailing line end.
func gateGit(ctx context.Context, dir string, args ...string) (string, error) {
	got := proc.Run(ctx, proc.Spec{Path: "git", Args: append([]string{"-C", dir}, args...), Env: publish.ToolEnvironment(os.Environ(), "GIT_TERMINAL_PROMPT=0"),
		Timeout: gateGitTimeout, StdoutLimit: 64 << 10, StderrLimit: 4 << 10})
	if got.Err != nil || got.Code != 0 {
		detail, _, _ := strings.Cut(strings.TrimSpace(string(got.Stderr)), "\n")
		if detail == "" && got.Err != nil {
			detail = got.Err.Error()
		}
		return "", errors.New(cmp.Or(detail, "git failed"))
	}
	return strings.TrimRight(string(got.Stdout), "\r\n"), nil
}

// readGateHook returns the state of the git hook name at path, and its mode.
// Only a regular file whose bytes equal the script cw gate install writes is
// installed; anything else there is foreign.
func readGateHook(path, name string) (string, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return gateAbsent, 0, nil
	} else if err != nil {
		return "", 0, err
	}
	script := gateHookScript(name)
	if !info.Mode().IsRegular() || info.Size() != int64(len(script)) {
		return gateForeign, info.Mode(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if !bytes.Equal(data, script) {
		return gateForeign, info.Mode(), nil
	}
	return gateInstalled, info.Mode(), nil
}

// installGateHook writes the git hook name at path unless a foreign file is
// there, and returns what it did: installed, updated (its mode), unchanged or
// refused. The hook is linked into place, so a file another program puts at
// path meanwhile is never replaced.
func installGateHook(path, name string) (string, error) {
	state, mode, err := readGateHook(path, name)
	switch {
	case err != nil:
		return "", err
	case state == gateForeign:
		return "refused", nil
	case state == gateInstalled && mode == 0o755:
		return "unchanged", nil
	}
	if gateAfterRead != nil {
		gateAfterRead(path)
	}
	temp, err := gateTemp(filepath.Dir(path), name)
	if err != nil {
		return "", err
	}
	defer os.Remove(temp)
	aside := ""
	if state == gateInstalled {
		if aside, err = takeGateHook(path, name); err != nil {
			return "", err
		} else if aside != "" {
			defer os.Remove(aside)
		}
	}
	if err := gateLink(temp, path); errors.Is(err, fs.ErrExist) {
		return "refused", nil
	} else if err != nil {
		if aside != "" {
			_ = os.Rename(aside, path)
		}
		return "", fmt.Errorf("cw gate links a hook into place, so that it never replaces a file another program put there, and this file system cannot: %w", err)
	}
	if state == gateInstalled {
		return "updated", nil
	}
	return "installed", nil
}

// uninstallGateHook removes the git hook name at path when it is cw's own,
// and returns what it did.
func uninstallGateHook(path, name string) (string, error) {
	state, _, err := readGateHook(path, name)
	switch {
	case err != nil:
		return "", err
	case state == gateForeign:
		return "left unchanged (not written by cw gate install)", nil
	case state == gateAbsent:
		return gateAbsent, nil
	}
	if gateAfterRead != nil {
		gateAfterRead(path)
	}
	aside, err := takeGateHook(path, name)
	if err != nil {
		return "", err
	}
	if aside == "" {
		return "left unchanged (it changed while cw gate was removing it)", nil
	}
	return "removed", os.Remove(aside)
}

// takeGateHook moves the hook at path, which was cw's own when read, to a
// new name beside it and returns that name, so no other program can replace
// it in the meantime. When the moved file is not cw's own after all, because
// another program put a file there since it was read, takeGateHook renames
// it back and returns "".
func takeGateHook(path, name string) (string, error) {
	placeholder, err := os.CreateTemp(filepath.Dir(path), "."+name+".cw-old-*")
	if err != nil {
		return "", err
	}
	aside := placeholder.Name()
	placeholder.Close()
	if err := os.Rename(path, aside); err != nil {
		os.Remove(aside)
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	state, _, err := readGateHook(aside, name)
	if err == nil && state == gateInstalled {
		return aside, nil
	}
	if restore := os.Rename(aside, path); restore != nil {
		return "", fmt.Errorf("%s changed while cw gate was moving it; the file is now %s: %w", path, aside, restore)
	}
	return "", err
}

// gateTemp writes the script for the git hook name to a new file in dir with
// mode 0755 and returns its name.
func gateTemp(dir, name string) (string, error) {
	temp, err := os.CreateTemp(dir, "."+name+".cw-new-*")
	if err != nil {
		return "", err
	}
	_, err = temp.Write(gateHookScript(name))
	if err == nil {
		err = temp.Chmod(0o755)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(temp.Name())
		return "", err
	}
	return temp.Name(), nil
}
