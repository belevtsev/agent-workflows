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

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
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
	// Environ lists the environment, as os.Environ does, for the variables
	// whose names Getenv cannot know in advance.
	Environ func() []string
	// Now is the clock: every time cw stores in a task record or a
	// publication entry, and the time open records age against.
	Now func() time.Time

	// prePublishTimeout replaces hookPublishTimeout when set, so tests can
	// reach the hook's deadline quickly.
	prePublishTimeout time.Duration
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
	if config.Environ == nil {
		config.Environ = os.Environ
	}
	if config.Now == nil {
		config.Now = time.Now
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

// taskEnv is the taskflow.Env the task commands run their use cases with:
// the production ports, with config's clock and cw consult's record validator.
func taskEnv(config Config) taskflow.Env {
	return taskflow.Env{Clock: config.Now, Consultation: consult.ValidateRecord}
}

// NewCommand constructs the cw command tree.
func NewCommand(config Config) *cobra.Command {
	config = defaults(config)
	root := &cobra.Command{
		Use:   "cw",
		Short: "Runtime for the agent-workflows skills",
		Long: "cw records and verifies substantial tasks and publishes the verified ones as a commit, push and pull request. " +
			"The skills that drive it ship as Claude Code and Codex plugins.",
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
	addPublishCommand(root, config)
	addMetricsCommand(root, config)
	addConsultCommand(root, config)
	addKeysCommand(root, config)
	addHookCommands(root, config)
	addGateCommands(root, config)
	addGraphifyCommands(root, config)
	addHostCommands(root, config)
	addPackCommands(root, config)
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

// errRelativeStateHome is the state root's error for a relative
// XDG_STATE_HOME. The pre-publish hook asks on it rather than allow.
var errRelativeStateHome = errors.New("XDG_STATE_HOME must be an absolute path")

// directories are the roots cw reads and writes, after flags and the
// environment are applied. Read the state root through stateDir.
type directories struct {
	home, codex, claude string
	state               string
	// stateErr is why there is no state root: a relative XDG_STATE_HOME.
	stateErr error
}

// stateDir returns the state root, or why there is none. Only commands that
// read or write state fail on it, as userconfig.Dir refuses a relative
// XDG_CONFIG_HOME only where the configuration directory is needed.
func (dirs directories) stateDir() (string, error) { return dirs.state, dirs.stateErr }

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
	dirs := directories{
		home:   home,
		codex:  cmp.Or(flag("codex-home"), config.Getenv("CODEX_HOME"), filepath.Join(home, ".codex")),
		claude: cmp.Or(flag("claude-home"), config.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude")),
		state:  flag("state-dir"),
	}
	if dirs.state == "" {
		// The XDG Base Directory specification makes a relative path
		// invalid. Refuse it rather than join it to the working directory,
		// which would move the task store, and the gate with it, on every cd.
		switch xdg := config.Getenv("XDG_STATE_HOME"); {
		case xdg == "":
			dirs.state = filepath.Join(home, ".local", "state", "agent-workflows")
		case filepath.IsAbs(xdg):
			dirs.state = filepath.Join(xdg, "agent-workflows")
		default:
			dirs.stateErr = errRelativeStateHome
		}
	}
	roots := []*string{&dirs.home, &dirs.codex, &dirs.claude}
	if dirs.stateErr == nil {
		roots = append(roots, &dirs.state)
	}
	for _, path := range roots {
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
