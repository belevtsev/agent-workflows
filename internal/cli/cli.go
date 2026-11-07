// Package cli provides the command tree for the native workflow skill manager.
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

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/distribution"
	"github.com/belevtsev/agent-workflows/internal/manageruntime"
	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/spf13/cobra"
)

// Service is the skill ownership/transaction interface used by commands.
type Service interface {
	Execute(string) (workflow.Object, error)
	Status() (workflow.Object, error)
}

// Config defines build identity, streams and replaceable environment/services.
// A zero dependency selects its production implementation.
type Config struct {
	Version, Revision, BuiltAt string
	// ReleaseIdentity carries main.releaseIdentity. The hidden --release-identity
	// flag prints it, which keeps the CWRELEASE1 linker marker in release
	// binaries; release and update verification require that marker.
	ReleaseIdentity string
	In              io.Reader
	Out, Err        io.Writer
	HomeDir         func() (string, error)
	WorkingDir      func() (string, error)
	Executable      func() (string, error)
	Getenv          func(string) string
	NewService      func(workflow.Options) Service
	Resolve         func(string) (manageruntime.Locator, error)
	// Inspect identifies the running executable as a standalone runtime
	// candidate before install/setup or update activates it.
	Inspect      func(context.Context, string) (manageruntime.Candidate, error)
	Bundle       bundled.Provider
	Distribution *distribution.Client
	Terminal     func() bool
	Configure    workflow.ConfigurationSelector
}

// ManagerProtocol identifies the contract the bootstrap requires of a manager.
const ManagerProtocol = workflow.ManagerProtocol

type options struct {
	home, codex, claude, state, shell, host               string
	dryRun, apply, json, managerProtocol, releaseIdentity bool
	adoptPersonalSkills, forget                           bool
	configDir, preset, settings, handoff, verifyRelease   string
	nonInteractive, plain, bundleIdentity                 bool
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
	if config.NewService == nil {
		config.NewService = func(o workflow.Options) Service { return workflow.NewInstaller(o) }
	}
	if config.Resolve == nil {
		config.Resolve = manageruntime.Resolve
	}
	if config.Inspect == nil {
		config.Inspect = manageruntime.InspectStandalone
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

// NewCommand constructs the only supported product command surface.
func NewCommand(config Config) *cobra.Command {
	config = defaults(config)
	o := options{shell: "auto", host: "auto"}
	root := &cobra.Command{Use: "cw", Short: "Manage agent workflow skills", Long: "Install, update and recover the managed agent workflow skills. Mutations apply by default; --dry-run makes no downloads or persistent writes.", SilenceUsage: true, SilenceErrors: true, DisableSuggestions: true, Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usage(fmt.Errorf("unknown command: %s", args[0]))
		}
		return nil
	}}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(config.In)
	root.SetOut(config.Out)
	root.SetErr(config.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err) })
	flags := root.PersistentFlags()
	flags.StringVar(&o.configDir, "config-dir", "", "Private preferences and credential directory")
	flags.StringVar(&o.preset, "preset", "", "Replace saved overrides with recommended or balanced defaults")
	flags.StringVar(&o.settings, "settings", "", "Nonsecret preferences JSON file (exclusive with --preset)")
	flags.BoolVar(&o.nonInteractive, "non-interactive", false, "Use saved settings or explicit options without prompts")
	flags.BoolVar(&o.plain, "plain", false, "Use the plain terminal setup interface")
	flags.StringVar(&o.handoff, "prepared-handoff", "", "Private verified release handoff")
	flags.BoolVar(&o.bundleIdentity, "bundle-identity", false, "Report the embedded resource identity")
	_ = flags.MarkHidden("prepared-handoff")
	_ = flags.MarkHidden("bundle-identity")
	flags.StringVar(&o.verifyRelease, "verify-release", "", "Verify the embedded source against one pinned release tag")
	_ = flags.MarkHidden("verify-release")
	flags.StringVar(&o.home, "home", "", "Installation home directory")
	flags.StringVar(&o.codex, "codex-home", "", "Codex configuration directory")
	flags.StringVar(&o.claude, "claude-home", "", "Claude Code configuration directory")
	flags.StringVar(&o.host, "host", "auto", "Agent host for install or uninstall: auto, codex, claude or all")
	flags.BoolVar(&o.forget, "forget", false, "Detach an uninstalled host whose files are already gone")
	flags.StringVar(&o.state, "state-dir", "", "Ownership and runtime state directory")
	flags.StringVar(&o.shell, "shell", "auto", "Install PATH in auto, bash, zsh or none")
	flags.BoolVar(&o.adoptPersonalSkills, "adopt-personal-skills", false, "Back up and adopt verified standalone personal skills during install/setup")
	flags.BoolVar(&o.dryRun, "dry-run", false, "Check locally without writes or downloads")
	flags.BoolVar(&o.apply, "apply", false, "Apply changes (the default)")
	flags.BoolVar(&o.json, "json", false, "Emit JSON (the default)")
	flags.BoolVar(&o.managerProtocol, "manager-protocol", false, "Report native manager bootstrap protocol")
	// --release-identity is the only reference to main.releaseIdentity, so it
	// keeps the CWRELEASE1 marker in release binaries. Do not remove it.
	flags.BoolVar(&o.releaseIdentity, "release-identity", false, "Report native release build identity")
	_ = flags.MarkHidden("manager-protocol")
	_ = flags.MarkHidden("release-identity")
	run := func(cmd *cobra.Command, _ []string) error {
		if o.verifyRelease != "" {
			if config.Bundle == nil {
				return errNoPayload
			}
			bundle, err := config.Bundle(cmd.Context())
			if err != nil {
				return err
			}
			id := bundle.Identity()
			if id.Version != o.verifyRelease || id.Version != config.Version || id.Revision != config.Revision {
				return errors.New("pinned release and embedded binary identity differ")
			}
			client := distribution.Client{}
			if config.Distribution != nil {
				client = *config.Distribution
			}
			if err = client.VerifyTag(cmd.Context(), o.verifyRelease, id.Revision); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), workflow.Object{"release_tag_verified": true, "version": id.Version, "revision": id.Revision})
		}
		if o.bundleIdentity {
			if config.Bundle == nil {
				return errNoPayload
			}
			bundle, err := config.Bundle(cmd.Context())
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), bundle.Identity())
		}
		if o.releaseIdentity {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), config.ReleaseIdentity)
			return err
		}
		if o.managerProtocol {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), ManagerProtocol)
			return err
		}
		action := cmd.Name()
		if action == "cw" {
			action = "setup"
		} else if cmd.CalledAs() == "setup" {
			action = "setup"
		}
		return executeAction(cmd, config, o, action)
	}
	root.RunE = run
	definitions := []struct{ name, short string }{
		{"install", "Set up bundled workflows, model defaults and cw"},
		{"update", "Download, verify and activate the latest stable release"},
		{"status", "Verify installed ownership, skills, runtime and model defaults locally"},
		{"rollback", "Restore the previous validated installed release"},
		{"recover", "Reverse owned changes from an interrupted transaction"},
		{"uninstall", "Remove managed skills, instructions, settings and cw"},
	}
	for _, definition := range definitions {
		command := &cobra.Command{Use: definition.name, Short: definition.short, Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usage(fmt.Errorf("unexpected argument: %s", args[0]))
			}
			return nil
		}, RunE: run}
		if definition.name == "install" {
			command.Aliases = []string{"setup"}
		}
		root.AddCommand(command)
	}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show native manager build identity as JSON", Args: func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usage(errors.New("version accepts no arguments"))
		}
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		return writeJSON(cmd.OutOrStdout(), versionReport{Arch: runtime.GOARCH, BuiltAt: config.BuiltAt, OS: runtime.GOOS, Revision: config.Revision, Version: config.Version})
	}})
	addStatusSummaryFlag(root)
	addDoctorCommand(root, config, o)
	addTaskCommands(root, config, o)
	addMetricsCommand(root, config, o)
	addConsultCommand(root, config)
	addKeysCommand(root, config)
	addHookCommands(root, config)
	addGraphifyCommands(root, config)
	// Cobra's help path stays independent of state, payload and downloads.
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
	// Keep the alphabetical wire order that install.sh matches exactly.
	Arch     string `json:"arch"`
	BuiltAt  string `json:"built_at"`
	OS       string `json:"os"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
}
type environmentReport struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

// errNoPayload refuses lifecycle work in a binary built without the release
// payload, such as a plain go build of cmd/cw.
var errNoPayload = errors.New("this developer binary has no embedded workflow payload; build a release with cwdev release build")

// executeAction validates lifecycle options before any filesystem access, so a
// usage error exits 2 even when the binary carries no workflow payload.
func executeAction(cmd *cobra.Command, config Config, o options, action string) error {
	install := action == "install" || action == "setup"
	configure := install || action == "update"
	if o.dryRun && o.apply {
		return usage(errors.New("--apply and --dry-run are mutually exclusive"))
	}
	if o.shell != "auto" && o.shell != "bash" && o.shell != "zsh" && o.shell != "none" {
		return usage(errors.New("--shell must be auto, bash, zsh or none"))
	}
	if !install && (cmd.Flags().Changed("shell") || o.adoptPersonalSkills) {
		return usage(errors.New("shell and adoption options require install/setup"))
	}
	if o.preset != "" && o.settings != "" {
		return usage(errors.New("--preset and --settings are mutually exclusive"))
	}
	if !configure && (o.preset != "" || o.settings != "" || o.handoff != "") {
		return usage(errors.New("configuration selection requires install/setup or update"))
	}
	if o.preset != "" && o.preset != string(workflow.PresetRecommended) && o.preset != string(workflow.PresetBalanced) {
		return usage(errors.New("--preset must be recommended or balanced"))
	}
	hosts, err := selectedHosts(o.host)
	if err != nil {
		return usage(err)
	}
	if !install && action != "uninstall" && cmd.Flags().Changed("host") {
		return usage(errors.New("--host requires install/setup or uninstall"))
	}
	if o.forget && (action != "uninstall" || len(hosts) == 0) {
		return usage(errors.New("--forget requires uninstall with an explicit host"))
	}
	if config.Bundle == nil {
		return errNoPayload
	}
	return executeStandalone(cmd, config, o, action, hosts)
}

// hostRoots resolves each host's directory. Recorded roots win so a later shell's
// environment cannot detach a host from where it was enrolled.
func hostRoots(config Config, home, state, codex, claude string) (string, string) {
	recordedCodex, recordedClaude := workflow.RecordedHomes(state)
	return cmp.Or(codex, recordedCodex, config.Getenv("CODEX_HOME"), filepath.Join(home, ".codex")),
		cmp.Or(claude, recordedClaude, config.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude"))
}

// installRoots are the directories an installation lives in. Read-only commands
// resolve them the same way as install, so a custom root reaches every command.
type installRoots struct{ home, codex, claude, state string }

func resolveRoots(root *cobra.Command, config Config) (installRoots, error) {
	var roots installRoots
	flags := root.PersistentFlags()
	for flag, value := range map[string]*string{"home": &roots.home, "codex-home": &roots.codex, "claude-home": &roots.claude, "state-dir": &roots.state} {
		*value, _ = flags.GetString(flag)
	}
	// Resolve errors, including a legacy installation, leave the default roots.
	if executable, err := config.Executable(); err == nil {
		if locator, err := config.Resolve(executable); err == nil {
			for _, pair := range [][2]*string{{&roots.home, &locator.Home}, {&roots.state, &locator.State}} {
				if *pair[0] == "" {
					*pair[0] = *pair[1]
				}
			}
		}
	}
	if roots.home == "" {
		var err error
		if roots.home, err = config.HomeDir(); err != nil {
			return roots, errors.New("resolve home directory")
		}
	}
	if roots.state == "" {
		state := config.Getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(roots.home, ".local", "state")
		}
		roots.state = filepath.Join(state, "agent-workflows")
	}
	roots.codex, roots.claude = hostRoots(config, roots.home, roots.state, roots.codex, roots.claude)
	return roots, nil
}

func selectedHosts(value string) ([]string, error) {
	switch value {
	case "auto":
		return nil, nil
	case "codex", "claude":
		return []string{value}, nil
	case "all":
		return []string{"codex", "claude"}, nil
	}
	return nil, errors.New("--host must be auto, codex, claude or all")
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}
