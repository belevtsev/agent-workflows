package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/charmbracelet/x/term"
)

// UXEditor is an explicit editor's draft. Save is false until confirmed.
// Config.Editor may replace terminal rendering for embedding and tests.
type UXEditor struct {
	Kind, Host, Role, Path   string
	Global, Accessible, Save bool
	NativeEffort             bool
	Model, Reasoning         string
	Skills                   []string
	Models, ReasoningChoices []string
	AvailableSkills          []skills.Entry
	Before                   string
}

func (e *UXEditor) summary() string {
	if e.Kind == "config" {
		host := e.Host
		if host == "" {
			host = "inherit / unset"
		}
		return "Host preference: " + host
	}
	text := fmt.Sprintf("Role: %s\nHost: %s", e.Role, e.Host)
	if !e.Global && e.Role != roles.Coordinator {
		text += fmt.Sprintf("\nModel: %s\nReasoning: %s", e.Model, roleEffortText(e.Reasoning))
	}
	return text + "\nSkills: " + skillListText(e.Skills)
}

// Preview describes the current draft and its selected destination.
func (e *UXEditor) Preview() string {
	return "Destination: " + e.Path + "\n\nBefore:\n" + e.Before + "\n\nAfter:\n" + e.summary()
}

func uxEditorAllowed(cfg Config, json bool) error {
	if json {
		return usagef("interactive editors do not support --json; use explicit set/reset commands")
	}
	if cfg.Editor != nil {
		return nil
	}
	input, inOK := cfg.In.(interface{ Fd() uintptr })
	output, outOK := cfg.Err.(interface{ Fd() uintptr })
	if !inOK || !outOK || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return usagef("editor requires terminal input and stderr; use explicit set/reset commands for scripts")
	}
	return nil
}

func uxLaunchEditor(ctx context.Context, cfg Config, draft *UXEditor) (bool, error) {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return false, exit(130)
		}
		return false, err
	}
	render := cfg.Editor
	if render == nil {
		render = func(ctx context.Context, draft *UXEditor) error { return uxRenderEditor(ctx, cfg, draft) }
	}
	err := render(ctx, draft)
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		fmt.Fprintln(cfg.Out, "Interrupted; no changes saved.")
		return false, exit(130)
	}
	if err == nil && !draft.Save {
		fmt.Fprintln(cfg.Out, "Cancelled; no changes saved.")
		return false, nil
	}
	if err != nil {
		return false, failf("editor: %v", err)
	}
	return true, ctx.Err()
}

func runUXConfigEdit(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("config edit")
	global, accessible := flags.Bool("global", false, ""), flags.Bool("accessible", false, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash {
		return usagef("usage: kitsu config edit [--global] [--accessible]")
	}
	if err := uxEditorAllowed(cfg, uxFlagSet(flags, "json")); err != nil {
		return err
	}
	path, err := uxConfigTarget(ctx, *global)
	if err != nil {
		return err
	}
	host, before, err := uxReadPreference(path)
	if err != nil {
		return failf("%v", err)
	}
	draft := &UXEditor{Kind: "config", Host: host, Path: path, Global: *global, Accessible: *accessible}
	draft.Before = draft.summary()
	if save, err := uxLaunchEditor(ctx, cfg, draft); err != nil || !save {
		return err
	}
	if err := uxSavePreference(ctx, before, draft.Host); err != nil {
		return failf("%v", err)
	}
	return uxSavedPreference(cfg, path, draft.Host, false)
}

func uxFlagSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func runUXRoleEdit(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("role edit")
	host := flags.String("host", "", "")
	global, accessible := flags.Bool("global", false, ""), flags.Bool("accessible", false, "")
	flags.Bool("json", false, "")
	var roots stringList
	flags.Var(&roots, "root", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || p.dash || !roles.Known(p.pos[0]) && p.pos[0] != roles.Coordinator {
		return usagef("role edit requires one known role; see `kitsu role edit --help`")
	}
	if uxFlagSet(flags, "host") && !uxValidHost(*host) {
		return usagef("host must be codex or claude")
	}
	if err := uxEditorAllowed(cfg, uxFlagSet(flags, "json")); err != nil {
		return err
	}
	role := p.pos[0]
	if !*global && role != roles.Coordinator {
		config, err := rolesConfig(ctx)
		if err != nil {
			return err
		}
		if config.Version == roles.WorkflowConfigVersion {
			selected, err := config.SpecialistHost(*host, role)
			if err != nil {
				return usagef("%v; use `kitsu role set %s --host HOST` to assign a host", err, role)
			}
			return uxEditRole(ctx, cfg, role, selected, false, *accessible, roots)
		}
	}
	selected, err := uxEditorHost(ctx, cfg, *host, *accessible)
	if err != nil || selected == "" {
		return err
	}
	return uxEditRole(ctx, cfg, role, selected, *global, *accessible, roots)
}

func uxEditorHost(ctx context.Context, cfg Config, host string, accessible bool) (string, error) {
	if host == "" {
		var err error
		host, _, err = uxHostPreference(ctx)
		if err != nil {
			return "", err
		}
	}
	if host != "" {
		if !uxValidHost(host) {
			return "", usagef("host must be codex or claude")
		}
		return host, nil
	}
	draft := &UXEditor{Kind: "host", Accessible: accessible}
	if save, err := uxLaunchEditor(ctx, cfg, draft); err != nil || !save {
		return "", err
	}
	if !uxValidHost(draft.Host) {
		return "", usagef("host must be codex or claude")
	}
	return draft.Host, nil
}

func uxModelSuggestions(config *roles.Config, host string, effort bool) []string {
	values := []string{}
	for _, role := range append(roles.All(config), roles.All(nil)...) {
		model, exists := role.Models[host]
		if !exists {
			continue
		}
		value := model.Model
		if effort {
			value = model.Effort
		}
		if value != "" && !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	return values
}

func uxSkillSelector(entry skills.Entry) string {
	if entry.Scope == "explicit" {
		return filepath.Clean(entry.Path)
	}
	return entry.ID
}

func uxRoleEditModel(before, after *UXEditor) roles.Model {
	var model roles.Model
	if !after.Global && after.Role != roles.Coordinator {
		if after.Model != before.Model {
			model.Model = strings.TrimSpace(after.Model)
		}
		if after.Reasoning != before.Reasoning {
			model.Effort = strings.TrimSpace(after.Reasoning)
		}
	}
	return model
}
