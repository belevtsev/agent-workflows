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
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
	"github.com/spf13/cobra"
)

// gateHookNames are the git hooks cw gate manages, in the order it reports
// them.
var gateHookNames = []string{"pre-commit", "pre-push"}

// gateHookMarker is the line that marks a hook file cw gate install wrote.
// cw gate install replaces, and cw gate uninstall removes, only a file that
// carries it.
const gateHookMarker = "# agent-workflows: written by cw gate install; cw gate uninstall deletes this file"

// The states of one hook file that cw gate status reports.
const (
	gateInstalled = "installed"
	gateAbsent    = "absent"
	gateForeign   = "foreign"
)

// gateGitTimeout bounds each git command cw gate runs.
const gateGitTimeout = 10 * time.Second

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

// gateHooks is the hooks directory git uses for a repository.
type gateHooks struct {
	dir string
	// scope is where core.hooksPath is set, such as "global"; empty when it
	// is not set.
	scope string
}

type gateStatusReport struct {
	HooksPath string           `json:"hooks_path"`
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
			"which follows core.hooksPath and is shared by every worktree of the repository. cw gate never replaces or removes a hook it did not write.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.AddCommand(gate)
	// define adds one subcommand with its --repo flag; run gets the hooks
	// directory of that repository.
	define := func(command *cobra.Command, writes bool, run func(cmd *cobra.Command, hooks gateHooks) error) *cobra.Command {
		var repo string
		command.Args = noPositional
		command.Flags().StringVar(&repo, "repo", "", "Repository directory (default: the working directory)")
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
			return run(cmd, hooks)
		}
		gate.AddCommand(command)
		return command
	}
	define(&cobra.Command{
		Use:   "install [--repo DIR]",
		Short: "Write git's pre-commit and pre-push hooks that run cw hook git",
		Long: "Write pre-commit and pre-push into the repository's hooks directory, or update ones an earlier cw gate install wrote. " +
			"Each runs the first cw on git's PATH, and exits 0 without checking when there is none. " +
			"A hook file cw gate did not write, such as one from a hook manager, is left unchanged: cw prints the line to add to it and exits 4 (refused).",
	}, true, func(cmd *cobra.Command, hooks gateHooks) error {
		hooks.report(cmd.OutOrStdout())
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
		Use:   "uninstall [--repo DIR]",
		Short: "Remove the git hooks cw gate install wrote",
		Long:  "Remove pre-commit and pre-push from the repository's hooks directory when cw gate install wrote them. Any other hook file is left unchanged.",
	}, true, func(cmd *cobra.Command, hooks gateHooks) error {
		hooks.report(cmd.OutOrStdout())
		for _, name := range gateHookNames {
			path := filepath.Join(hooks.dir, name)
			file, err := readGateHook(path)
			action := file.state
			switch {
			case err != nil:
				return fmt.Errorf("read %s: %w", path, err)
			case file.state == gateInstalled:
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("remove %s: %w", path, err)
				}
				action = "removed"
			case file.state == gateForeign:
				action = "left unchanged (not written by cw gate install)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, action)
		}
		return nil
	})
	var asJSON bool
	status := define(&cobra.Command{
		Use:   "status [--repo DIR] [--json]",
		Short: "Show whether each git hook is installed, absent or foreign",
		Long: "Print the repository's hooks directory and, for pre-commit and pre-push, installed (written by cw gate install), absent, " +
			"or foreign (a hook file cw gate did not write). --json prints {hooks_path, hooks: [{name, path, state}]}.",
	}, false, func(cmd *cobra.Command, hooks gateHooks) error {
		report := gateStatusReport{HooksPath: hooks.dir, Hooks: []gateHookStatus{}}
		for _, name := range gateHookNames {
			path := filepath.Join(hooks.dir, name)
			file, err := readGateHook(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			report.Hooks = append(report.Hooks, gateHookStatus{Name: name, Path: path, State: file.state})
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

// report writes the hooks directory and, when core.hooksPath comes from
// outside the repository, that the hooks reach other repositories too.
func (hooks gateHooks) report(out io.Writer) {
	fmt.Fprintf(out, "git hooks directory: %s\n", hooks.dir)
	if hooks.scope != "" && hooks.scope != "local" && hooks.scope != "worktree" {
		fmt.Fprintf(out, "note: core.hooksPath is set in the %s git configuration, so these hooks run in every repository that uses this directory\n", hooks.scope)
	}
}

// resolveGateHooks returns the hooks directory git runs dir's hooks from.
func resolveGateHooks(ctx context.Context, dir string) (gateHooks, error) {
	// Git prints the path relative to dir unless it is elsewhere, as for a
	// linked worktree; --path-format=absolute would need git 2.31.
	path, err := gateGit(ctx, dir, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return gateHooks{}, fmt.Errorf("cannot locate the git hooks directory of %s: %w", dir, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	hooks := gateHooks{dir: filepath.Clean(path)}
	// Git before 2.26 has no --show-scope; the note is then left out.
	if setting, err := gateGit(ctx, dir, "config", "--show-scope", "--get", "core.hooksPath"); err == nil {
		hooks.scope, _, _ = strings.Cut(setting, "\t")
	}
	return hooks, nil
}

// gateGit runs git in dir with cw's environment, so it reads the user's git
// configuration, and returns its output without the trailing line end.
func gateGit(ctx context.Context, dir string, args ...string) (string, error) {
	got := proc.Run(ctx, proc.Spec{Path: "git", Args: append([]string{"-C", dir}, args...), Env: os.Environ(),
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

// gateHookFile is one hook file as cw gate found it.
type gateHookFile struct {
	state string
	data  []byte
	mode  fs.FileMode
}

// readGateHook returns the state of the hook file at path. Anything there
// but a regular file with the marker line is foreign.
func readGateHook(path string) (gateHookFile, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return gateHookFile{state: gateAbsent}, nil
	} else if err != nil {
		return gateHookFile{}, err
	}
	file := gateHookFile{state: gateForeign, mode: info.Mode()}
	if !info.Mode().IsRegular() {
		return file, nil
	}
	if file.data, err = os.ReadFile(path); err != nil {
		return gateHookFile{}, err
	}
	for line := range strings.Lines(string(file.data)) {
		if strings.TrimRight(line, "\r\n") == gateHookMarker {
			file.state = gateInstalled
			break
		}
	}
	return file, nil
}

// installGateHook writes the hook name at path unless a foreign file is
// there, and returns what it did: installed, updated, unchanged or refused.
func installGateHook(path, name string) (string, error) {
	file, err := readGateHook(path)
	if err != nil {
		return "", err
	}
	script := gateHookScript(name)
	switch file.state {
	case gateForeign:
		return "refused", nil
	case gateInstalled:
		if bytes.Equal(file.data, script) && file.mode == 0o755 {
			return "unchanged", nil
		}
		return "updated", writeGateHook(path, script, true)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: git's hooks directory keeps git's usual 0755.
		return "", err
	}
	if err := writeGateHook(path, script, false); errors.Is(err, fs.ErrExist) {
		// Another program wrote the hook since it was read.
		return "refused", nil
	} else if err != nil {
		return "", err
	}
	return "installed", nil
}

// writeGateHook writes data to path with mode 0755 through a temporary file
// in the same directory, so an interrupted install leaves either the old
// state or the whole hook. With replace false it links the file into place,
// which fails with fs.ErrExist if a file appeared at path meanwhile.
func writeGateHook(path string, data []byte, replace bool) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".cw-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Chmod(0o755)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return err
	case replace:
		return os.Rename(temp.Name(), path)
	}
	if err := os.Link(temp.Name(), path); err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	// A file system without hard links.
	return os.Rename(temp.Name(), path)
}
