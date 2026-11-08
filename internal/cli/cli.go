// Package cli provides the cw command tree: the task, consult, doctor, metrics,
// keys and graphify runtime, plus the thin Codex installer.
package cli

import (
	"cmp"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// Config defines build identity, streams and replaceable environment.
// A zero dependency selects its production implementation.
type Config struct {
	Version, Revision, BuiltAt string
	In                         io.Reader
	Out, Err                   io.Writer
	HomeDir                    func() (string, error)
	WorkingDir                 func() (string, error)
	Executable                 func() (string, error)
	Getenv                     func(string) string

	// prePublishTimeout replaces hookPublishTimeout when set, so tests can
	// reach the hook's deadline quickly.
	prePublishTimeout time.Duration
}

type usageError struct{ error }

func usage(err error) error { return usageError{err} }

// ExitCode maps runtime, usage and cancellation errors to Unix exit codes.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	if status, ok := errors.AsType[reportedExit](err); ok {
		return int(status)
	}
	if _, ok := errors.AsType[usageError](err); ok {
		return 2
	}
	return 1
}

// Execute runs a fresh command with explicit arguments and cancellation.
func Execute(ctx context.Context, args []string, config Config) error {
	command := NewCommand(config)
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

func defaults(config Config) Config {
	if config.HomeDir == nil {
		config.HomeDir = os.UserHomeDir
	}
	if config.WorkingDir == nil {
		config.WorkingDir = os.Getwd
	}
	if config.Executable == nil {
		config.Executable = os.Executable
	}
	if config.Getenv == nil {
		config.Getenv = os.Getenv
	}
	if config.In == nil {
		config.In = os.Stdin
	}
	if config.Out == nil {
		config.Out = os.Stdout
	}
	if config.Err == nil {
		config.Err = os.Stderr
	}
	return config
}

// NewCommand constructs the cw command tree.
func NewCommand(config Config) *cobra.Command {
	config = defaults(config)
	root := &cobra.Command{
		Use:   "cw",
		Short: "Runtime for the agent-workflows skills",
		Long: "cw records and verifies substantial tasks, publishes verified ones (commit, push and pull request, run by a coordinator that carries the user's authorization), consults configured advisers, inspects projects and installs the Codex instruction block. " +
			"The skills themselves ship as Claude Code and Codex plugins.",
		SilenceUsage: true, SilenceErrors: true, DisableSuggestions: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usage(fmt.Errorf("unknown command: %s", args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(config.In)
	root.SetOut(config.Out)
	root.SetErr(config.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err) })
	flags := root.PersistentFlags()
	flags.String("config-dir", "", "Private preferences and credential directory")
	flags.String("home", "", "Home directory")
	flags.String("codex-home", "", "Codex configuration directory")
	flags.String("claude-home", "", "Claude Code configuration directory")
	flags.String("state-dir", "", "State directory holding the task store")
	flags.Bool("dry-run", false, "Report without writing")
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show the build identity as JSON", Args: func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usage(errors.New("version accepts no arguments"))
		}
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		return writeJSON(cmd.OutOrStdout(), versionReport{Arch: runtime.GOARCH, BuiltAt: config.BuiltAt, OS: runtime.GOOS, Revision: config.Revision, Version: config.Version})
	}})
	addDoctorCommand(root, config)
	addTaskCommands(root, config)
	addTaskAbandonCommand(root)
	addTaskTimelineCommand(root)
	addPublishCommand(root, config)
	addMetricsCommand(root, config)
	addConsultCommand(root, config)
	addKeysCommand(root, config)
	addHookCommands(root, config)
	addGraphifyCommands(root, config)
	addHostCommands(root, config)
	// Cobra's help path stays independent of state and environment.
	root.SetHelpCommand(&cobra.Command{Use: "help [command ...]", Short: "Show help for a command", RunE: func(_ *cobra.Command, args []string) error {
		target := root
		if len(args) > 0 {
			found, remaining, err := root.Find(args)
			if err != nil || found == root || len(remaining) > 0 {
				return usage(fmt.Errorf("unknown help topic: %s", args[0]))
			}
			target = found
		}
		return target.Help()
	}})
	return root
}

type versionReport struct {
	Arch     string `json:"arch"`
	BuiltAt  string `json:"built_at"`
	OS       string `json:"os"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
}

// directories are the roots cw reads and writes, after flags and the
// environment are applied.
type directories struct{ home, codex, claude, state string }

// resolveRoots applies --home, --codex-home, --claude-home and --state-dir,
// then CODEX_HOME, CLAUDE_CONFIG_DIR and XDG_STATE_HOME, then the defaults
// under the home directory.
func resolveRoots(root *cobra.Command, config Config) (directories, error) {
	flags := root.PersistentFlags()
	flag := func(name string) string { value, _ := flags.GetString(name); return value }
	home := flag("home")
	if home == "" {
		var err error
		if home, err = config.HomeDir(); err != nil {
			return directories{}, errors.New("resolve home directory")
		}
	}
	state := flag("state-dir")
	if state == "" {
		base := cmp.Or(config.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))
		state = filepath.Join(base, "agent-workflows")
	}
	dirs := directories{
		home:   home,
		codex:  cmp.Or(flag("codex-home"), config.Getenv("CODEX_HOME"), filepath.Join(home, ".codex")),
		claude: cmp.Or(flag("claude-home"), config.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude")),
		state:  state,
	}
	for _, path := range []*string{&dirs.home, &dirs.codex, &dirs.claude, &dirs.state} {
		var err error
		if *path, err = absolute(config, *path); err != nil {
			return directories{}, err
		}
	}
	return dirs, nil
}

// absolute resolves a relative path against the working directory, so roots
// compare with the absolute targets of the links they own.
func absolute(config Config, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	wd, err := config.WorkingDir()
	if err != nil {
		return "", errors.New("resolve working directory")
	}
	return filepath.Join(wd, path), nil
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}
