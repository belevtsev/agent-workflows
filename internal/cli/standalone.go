package cli

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/distribution"
	"github.com/belevtsev/agent-workflows/internal/manageruntime"
	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/spf13/cobra"
)

type preparedHandoff struct {
	Version                            int `json:"version"`
	Home, Codex, Claude, State, Config string
	Generation                         string
	Target                             bundled.Identity
}

func standaloneRoots(config Config, o options) (workflow.Paths, string, error) {
	var loc manageruntime.Locator
	executable, err := config.Executable()
	if err != nil {
		return workflow.Paths{}, "", err
	}
	// Resolve returns manageruntime.ErrLegacyInstallation for a runtime that an
	// older cw installed, here or for the installed command below, so every
	// lifecycle command refuses it before any write.
	loc, err = config.Resolve(executable)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return workflow.Paths{}, "", err
	}
	if o.home == "" {
		o.home = loc.Home
	}
	if o.home == "" {
		o.home, err = config.HomeDir()
		if err != nil {
			return workflow.Paths{}, "", err
		}
	}
	o.home = workflow.Normalize(o.home)
	// A downloaded bootstrap can discover the installed command at its normal
	// location. A random checkout or current directory never supplies roots.
	if loc.Home == "" {
		installed := filepath.Join(o.home, ".local", "bin", "cw")
		if _, e := os.Lstat(installed); e == nil {
			loc, err = config.Resolve(installed)
			if err != nil {
				return workflow.Paths{}, "", err
			}
		}
	}
	if loc.Home != "" && workflow.Normalize(loc.Home) != o.home {
		return workflow.Paths{}, "", errors.New("explicit home differs from installed command roots")
	}
	if o.state == "" {
		o.state = loc.State
	}
	if o.state == "" {
		base := config.Getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(o.home, ".local", "state")
		}
		o.state = filepath.Join(base, "agent-workflows")
	}
	o.state = workflow.Normalize(o.state)
	if loc.State != "" && workflow.Normalize(loc.State) != o.state {
		return workflow.Paths{}, "", errors.New("explicit state differs from installed command roots")
	}
	// Enrolled roots belong to state/journal. The immutable runtime locator's
	// host roots are initial hints and may predate a later host enrollment.
	o.codex, o.claude = hostRoots(config, o.home, o.state, o.codex, o.claude)
	return workflow.Paths{Home: o.home, State: o.state, Codex: workflow.Normalize(o.codex), Claude: workflow.Normalize(o.claude)}, loc.Config, nil
}

// executeStandalone runs a lifecycle action whose options executeAction has
// already validated.
func executeStandalone(cmd *cobra.Command, config Config, o options, action string, hosts []string) error {
	ctx := cmd.Context()
	install := action == "install" || action == "setup"
	configure := install || action == "update"
	paths, recordedConfig, err := standaloneRoots(config, o)
	if err != nil {
		return err
	}
	installation, err := workflow.RecordedInstallation(paths.State)
	if err != nil {
		return err
	}
	if installation.Pending && action != "recover" && action != "status" {
		return errors.New("an unfinished mutation requires explicit cw recover before setup or update")
	}
	if recordedConfig == "" {
		recordedConfig = installation.ConfigDir
	}
	configDir, err := workflow.ResolveConfigDir(paths.Home, o.configDir, recordedConfig, config.Getenv)
	if err != nil {
		return err
	}
	client := distribution.Client{}
	if config.Distribution != nil {
		client = *config.Distribution
	}
	apply := !o.dryRun
	var prepared *preparedHandoff
	if o.handoff != "" {
		h, e := readHandoff(o.handoff)
		if e != nil {
			return e
		}
		prepared = &h
		if h.Home != paths.Home || h.State != paths.State || h.Codex != paths.Codex || h.Claude != paths.Claude || h.Config != configDir {
			return errors.New("prepared release handoff roots differ")
		}
	}
	if action == "update" && apply && prepared == nil {
		release, e := client.Latest(ctx)
		if e != nil {
			return e
		}
		candidate, e := client.Prepare(ctx, release, paths.State)
		if e != nil {
			return e
		}
		if candidate.Bundle == nil {
			return errors.New("prepared release has no embedded identity")
		}
		h := preparedHandoff{Version: 1, Home: paths.Home, Codex: paths.Codex, Claude: paths.Claude, State: paths.State, Config: configDir, Generation: installation.Generation, Target: *candidate.Bundle}
		return handoffManager(ctx, cmd, config, o, candidate.Path, h)
	}
	options := workflow.Options{Paths: paths, ConfigDir: configDir, Bundle: config.Bundle, Context: ctx, Getenv: config.Getenv, Apply: apply, Shell: o.shell, Hosts: hosts, Forget: o.forget, AdoptPersonalSkills: o.adoptPersonalSkills}
	if prepared != nil {
		options.ExpectedGeneration = &prepared.Generation
		options.ExpectedBundle = &prepared.Target
	}
	if o.preset != "" {
		options.Selection = &workflow.ConfigurationSelection{Hosts: hosts, Preset: workflow.Preset(o.preset)}
	}
	if o.settings != "" {
		data, e := readBoundedFile(o.settings, 1<<20)
		if e != nil {
			return e
		}
		prefs, e := workflow.DecodePreferences(data)
		if e != nil {
			return e
		}
		options.Selection = &workflow.ConfigurationSelection{Hosts: hosts, Preferences: &prefs}
	}
	wizard := newSetupWizard(config, o)
	if configure && apply && !o.nonInteractive && !cmd.Flags().Changed("json") && terminalAvailable(config) {
		if options.Selection == nil {
			if config.Configure != nil {
				options.Configure = config.Configure
			} else {
				options.Configure = wizard.Select
			}
		}
	}
	if apply && configure && !(install && installation.Installed) {
		executable, e := config.Executable()
		if e != nil {
			return e
		}
		executable, e = filepath.EvalSymlinks(executable)
		if e != nil {
			return e
		}
		candidate, e := config.Inspect(ctx, executable)
		if e != nil {
			return e
		}
		options.RuntimeCandidate = candidate.Path
		options.RuntimeIdentity = workflow.RuntimeIdentity{Revision: candidate.Revision, Version: candidate.Version, OS: candidate.OS, Arch: candidate.Arch}
	}
	service := config.NewService(options)
	command := action
	if install {
		command = "setup"
	}
	report, executeErr := service.Execute(command)
	if executeErr != nil {
		return executeErr
	}
	credentialResult := workflow.Object{"status": "unchanged"}
	if report["configuration_committed"] == true && wizard.started {
		credentialResult, err = wizard.SaveCredentials(ctx)
		if err != nil {
			_ = writeJSON(cmd.OutOrStdout(), workflow.Object{"action": action, "configuration_committed": true, "credentials": credentialResult, "status": "partial"})
			return err
		}
	}
	var status workflow.Object
	var statusErr error
	if action == "recover" && !apply && report["pending"] == true {
		status = workflow.Object{"recovery_pending": true}
	} else {
		status, statusErr = service.Status()
	}
	if statusErr != nil {
		return statusErr
	}
	presence, e := (credentials.Resolver{Directory: configDir, Getenv: config.Getenv}).Presence()
	credentialStorage := "inspected"
	if e != nil {
		presence = nil
		credentialStorage = "unavailable"
	}
	if wizard.started {
		fmt.Fprintln(cmd.ErrOrStderr(), wizardSuccess(config, o, report))
	}
	return writeJSON(cmd.OutOrStdout(), workflow.Object{"action": action, "dry_run": !apply, "environment": environmentReport{"native_go", config.Version, config.Revision}, "result": report, "status": status, "credentials": credentialResult, "credential_sources": presence, "credential_storage_status": credentialStorage, "next_step": "Start a fresh agent session to use the selected settings"})
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("configuration input exceeds limit")
	}
	return data, nil
}

func readHandoff(path string) (preparedHandoff, error) {
	var h preparedHandoff
	info, err := os.Lstat(path)
	if err != nil {
		return h, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || int(st.Uid) != os.Getuid() || st.Nlink != 1 {
		return h, errors.New("unsafe prepared release handoff")
	}
	data, err := readBoundedFile(path, 64<<10)
	if err != nil {
		return h, err
	}
	if err = json.Unmarshal(data, &h, json.RejectUnknownMembers(true)); err != nil {
		return h, errors.New("invalid prepared release handoff")
	}
	if h.Version != 1 {
		return h, errors.New("unsupported prepared release handoff")
	}
	return h, nil
}

func handoffManager(ctx context.Context, cmd *cobra.Command, config Config, o options, path string, h preparedHandoff) error {
	f, err := os.CreateTemp(h.State, ".prepared-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	data, err := json.Marshal(h)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	args := []string{"update", "--prepared-handoff", f.Name(), "--home", h.Home, "--state-dir", h.State, "--codex-home", h.Codex, "--claude-home", h.Claude, "--config-dir", h.Config}
	if o.nonInteractive {
		args = append(args, "--non-interactive")
	}
	if o.plain {
		args = append(args, "--plain")
	}
	if o.json {
		args = append(args, "--json")
	}
	if o.preset != "" {
		args = append(args, "--preset", o.preset)
	}
	if o.settings != "" {
		absolute, e := filepath.Abs(o.settings)
		if e != nil {
			return e
		}
		args = append(args, "--settings", absolute)
	}
	child := exec.CommandContext(ctx, path, args...)
	child.Stdin = config.In
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = cmd.ErrOrStderr()
	if err = child.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return reportedExit(exit.ExitCode())
		}
		return errors.New("verified manager handoff could not start")
	}
	return nil
}
