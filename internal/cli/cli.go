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
	"os/exec"
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
	ReleaseIdentity            string
	In                         io.Reader
	Out, Err                   io.Writer
	HomeDir                    func() (string, error)
	WorkingDir                 func() (string, error)
	Executable                 func() (string, error)
	Getenv                     func(string) string
	NewService                 func(workflow.Options) Service
	Validate                   func(string) (workflow.Object, error)
	Resolve                    func(string) (manageruntime.Locator, error)
	Inspect                    func(context.Context, string) (manageruntime.Candidate, error)
	Prepare                    func(context.Context, string, string, string) (manageruntime.Candidate, error)
	Bundle                     bundled.Provider
	Distribution               *distribution.Client
	Terminal                   func() bool
	Configure                  workflow.ConfigurationSelector
}

// ManagerProtocol identifies the contract the bootstrap requires of a manager.
const ManagerProtocol = workflow.ManagerProtocol

type options struct {
	source, home, codex, claude, state, shell, host                          string
	dryRun, apply, noCheckout, json, bootstrap, prepareOnly, managerProtocol bool
	releaseIdentity                                                          bool
	adoptPersonalSkills, forget                                              bool
	configDir, preset, settings, migrateFrom, handoff, verifyRelease         string
	nonInteractive, plain, bundleIdentity                                    bool
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
	if config.Validate == nil {
		config.Validate = workflow.ValidateSuite
	}
	if config.Resolve == nil {
		config.Resolve = manageruntime.Resolve
	}
	if config.Inspect == nil {
		config.Inspect = manageruntime.Inspect
	}
	if config.Prepare == nil {
		config.Prepare = manageruntime.Prepare
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
	root := &cobra.Command{Use: "cw", Short: "Manage agent workflow skills", Long: "Install, update, validate and recover the managed agent workflow skills. Mutations apply by default; --dry-run makes no downloads or persistent writes.", SilenceUsage: true, SilenceErrors: true, DisableSuggestions: true, Args: func(cmd *cobra.Command, args []string) error {
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
	flags.StringVar(&o.source, "source", "", "Workflow source checkout")
	flags.StringVar(&o.configDir, "config-dir", "", "Private preferences and credential directory")
	flags.StringVar(&o.preset, "preset", "", "Replace saved overrides with recommended or balanced defaults")
	flags.StringVar(&o.settings, "settings", "", "Nonsecret preferences JSON file (exclusive with --preset)")
	flags.StringVar(&o.migrateFrom, "migrate-from", "", "Installed cw command whose verified roots should be migrated")
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
	flags.BoolVar(&o.noCheckout, "no-checkout", false, "Update without fast-forwarding source checkout")
	flags.BoolVar(&o.json, "json", false, "Emit JSON (the default)")
	flags.BoolVar(&o.bootstrap, "bootstrap", false, "Read-only default")
	flags.BoolVar(&o.prepareOnly, "prepare-only", false, "Prepare exact manager without activation")
	flags.BoolVar(&o.managerProtocol, "manager-protocol", false, "Report native manager bootstrap protocol")
	flags.BoolVar(&o.releaseIdentity, "release-identity", false, "Report native release build identity")
	_ = flags.MarkHidden("bootstrap")
	_ = flags.MarkHidden("prepare-only")
	_ = flags.MarkHidden("manager-protocol")
	_ = flags.MarkHidden("release-identity")
	run := func(cmd *cobra.Command, _ []string) error {
		if o.verifyRelease != "" {
			if config.Bundle == nil {
				return errors.New("this binary has no standalone payload")
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
				return errors.New("this developer binary has no embedded workflow payload")
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
		{"validate", "Validate the source skill suite without activating it"},
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
	addDoctorCommand(root, config, o)
	addTaskCommands(root, config, o)
	addMetricsCommand(root, config, o)
	addConsultCommand(root, config)
	addGraphifyCommands(root, config)
	// Cobra's help path stays independent of source, state, Git and acquisition.
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
	// Keep the legacy alphabetical wire order used by the shell bootstrap.
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
type resultReport struct {
	Action           string                      `json:"action"`
	DryRun           bool                        `json:"dry_run"`
	Validation       string                      `json:"validation"`
	Environment      environmentReport           `json:"environment"`
	Result           workflow.Object             `json:"result"`
	Status           workflow.Object             `json:"status"`
	Credential       bool                        `json:"jev_credential_present"`
	SubQCredential   bool                        `json:"subq_credential_present"`
	OpenAICredential bool                        `json:"openai_credential_present"`
	Integrations     workflow.Object             `json:"integrations"`
	NextStep         string                      `json:"next_step"`
	Prerequisites    map[string]toolPrerequisite `json:"skill_prerequisites"`
}

func executeAction(cmd *cobra.Command, config Config, o options, action string) error {
	if config.Bundle != nil && o.source == "" {
		return executeStandalone(cmd, config, o, action)
	}
	if o.configDir != "" || o.preset != "" || o.settings != "" || o.migrateFrom != "" || o.handoff != "" {
		return usage(errors.New("configuration and migration options require the standalone bundled manager"))
	}
	if o.dryRun && o.apply {
		return usage(errors.New("--apply and --dry-run are mutually exclusive"))
	}
	if o.shell != "auto" && o.shell != "bash" && o.shell != "zsh" && o.shell != "none" {
		return usage(errors.New("--shell must be auto, bash, zsh or none"))
	}
	install := action == "install" || action == "setup"
	if !install && cmd.Flags().Changed("shell") {
		return usage(errors.New("--shell is supported only for install/setup"))
	}
	if !install && cmd.Flags().Changed("adopt-personal-skills") {
		return usage(errors.New("--adopt-personal-skills is supported only for install/setup"))
	}
	if o.prepareOnly && !install {
		return usage(errors.New("--prepare-only is supported only for install/setup"))
	}
	hosts, err := selectedHosts(o.host)
	if err != nil {
		return usage(err)
	}
	if !install && action != "uninstall" && cmd.Flags().Changed("host") {
		return usage(errors.New("--host is supported only for install/setup and uninstall"))
	}
	if o.forget && (action != "uninstall" || len(hosts) == 0) {
		return usage(errors.New("--forget requires uninstall with an explicit --host"))
	}
	executable, err := config.Executable()
	if err != nil {
		return err
	}
	locator, locatorErr := config.Resolve(executable)
	if errors.Is(locatorErr, manageruntime.ErrLocatorMissing) && o.source != "" && o.home != "" && o.codex != "" && o.state != "" {
		// Explicit roots remain available for a runtime with no descriptor. An
		// owned layout must never select defaults from cwd.
		locatorErr = os.ErrNotExist
	}
	if locatorErr != nil && !errors.Is(locatorErr, os.ErrNotExist) {
		return locatorErr
	}
	if locatorErr == nil {
		if o.source == "" {
			o.source = locator.Source
		}
		if o.home == "" {
			o.home = locator.Home
		}
		if o.state == "" {
			o.state = locator.State
		}
	}
	if o.home == "" {
		o.home, err = config.HomeDir()
		if err != nil {
			return err
		}
	}
	if o.source == "" {
		// A source .bin manager binds to its own checkout as well. Cwd is used only
		// for an explicit developer binary outside an installed/bootstrap layout.
		real, resolveErr := filepath.EvalSymlinks(executable)
		if resolveErr == nil && filepath.Base(filepath.Dir(real)) == ".bin" {
			o.source = filepath.Dir(filepath.Dir(real))
		} else {
			o.source, err = config.WorkingDir()
			if err != nil {
				return err
			}
		}
	}
	if o.state == "" {
		state := config.Getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(o.home, ".local", "state")
		}
		o.state = filepath.Join(state, "agent-workflows")
	}
	apply := !o.dryRun
	if o.bootstrap {
		apply = o.apply && !o.dryRun
	}
	o.codex, o.claude = hostRoots(config, o.home, o.state, o.codex, o.claude)
	paths := workflow.Paths{Source: o.source, Home: o.home, Codex: o.codex, Claude: o.claude, State: o.state}
	options := workflow.Options{Paths: paths, Apply: apply, NoCheckout: o.noCheckout, Shell: o.shell, AdoptPersonalSkills: o.adoptPersonalSkills, Hosts: hosts, Forget: o.forget, Context: cmd.Context(), Getenv: config.Getenv}
	if action == "validate" {
		report, err := config.Validate(workflow.Normalize(o.source))
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), report)
	}
	real, resolveErr := filepath.EvalSymlinks(executable)
	var current manageruntime.Candidate
	if resolveErr == nil {
		current, err = config.Inspect(cmd.Context(), real)
	}
	if locatorErr == nil && err != nil {
		return err
	}
	if err == nil && current.Path != "" {
		options.RuntimeCandidate = current.Path
		options.RuntimeIdentity = workflow.RuntimeIdentity{Revision: current.Revision, Version: current.Version, OS: current.OS, Arch: current.Arch}
	}
	options.PrepareRuntime = func(ctx context.Context, source, sha, state string) (workflow.RuntimeCandidate, error) {
		candidate := current
		if candidate.Path == "" || candidate.Revision != sha {
			var err error
			candidate, err = config.Prepare(ctx, source, sha, state)
			if err != nil {
				return workflow.RuntimeCandidate{}, err
			}
		}
		return workflow.RuntimeCandidate{Path: candidate.Path, Identity: workflow.RuntimeIdentity{Revision: candidate.Revision, Version: candidate.Version, OS: candidate.OS, Arch: candidate.Arch}}, nil
	}
	service := config.NewService(options)
	if o.prepareOnly {
		if !apply {
			return writeJSON(cmd.OutOrStdout(), workflow.Object{"action": action, "dry_run": true, "environment": workflow.Object{"kind": "native_go", "status": "deferred"}, "validation": "deferred"})
		}
		head, err := manageruntime.SourceHead(cmd.Context(), workflow.Normalize(o.source))
		if err != nil {
			return err
		}
		candidate, err := config.Prepare(cmd.Context(), workflow.Normalize(o.source), head, workflow.Normalize(o.state))
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), candidate)
	}
	command := action
	if install {
		command = "setup"
	}
	report, err := service.Execute(command)
	if err != nil {
		return err
	}
	if o.bootstrap {
		return writeJSON(cmd.OutOrStdout(), report)
	}
	var status workflow.Object
	if action == "recover" && !apply && report["pending"] == true {
		status = workflow.Object{"recovery_pending": true}
	} else {
		status, err = service.Status()
		if err != nil {
			return err
		}
	}
	credential := config.Getenv("TYPESAFE_API_KEY") != ""
	subqCredential := config.Getenv("SUBQ_API_KEY") != ""
	openaiCredential := config.Getenv("OPENAI_API_KEY") != ""
	return writeJSON(cmd.OutOrStdout(), resultReport{
		Action: action, DryRun: !apply, Validation: "complete",
		Environment: environmentReport{"native_go", config.Version, config.Revision},
		Result:      report, Status: status, Credential: credential, SubQCredential: subqCredential, OpenAICredential: openaiCredential,
		Integrations: workflow.Object{
			"typesafe":         workflow.Object{"credential_available": credential},
			"subq":             workflow.Object{"credential_available": subqCredential},
			"openai_decisions": workflow.Object{"credential_available": openaiCredential},
		},
		NextStep:      "Reload your shell startup file for cw and start a fresh agent session",
		Prerequisites: skillPrerequisites(status, o.home, os.Stat, exec.LookPath),
	})
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
