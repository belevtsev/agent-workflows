package cli

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

type wizardOption struct{ label, value string }
type setupWizard struct {
	config                    Config
	plain, started, checkKeys bool
	directory, generation     string
	changes                   map[string]*string
	reader                    cancelreader.CancelReader
	outputErr                 error
}

func (w *setupWizard) print(format string, args ...any) {
	if w.outputErr != nil {
		return
	}
	if _, err := fmt.Fprintf(w.config.Err, format, args...); err != nil {
		w.outputErr = errors.New("terminal output is unavailable")
	}
}

// Stop the cancellation watcher before closing its owned descriptors.
func watchInput(ctx context.Context, reader cancelreader.CancelReader) func() {
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			reader.Cancel()
		case <-stop:
		}
	}()
	return func() { close(stop); <-done; reader.Close() }
}

func terminalAvailable(config Config) bool {
	if config.Terminal != nil {
		return config.Terminal()
	}
	in, iok := config.In.(*os.File)
	out, ook := config.Out.(*os.File)
	err, eok := config.Err.(*os.File)
	return iok && ook && eok && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd())) && term.IsTerminal(int(err.Fd()))
}

func newSetupWizard(config Config, o options) *setupWizard {
	return &setupWizard{config: config, plain: o.plain || config.Getenv("TERM") == "dumb" || config.Getenv("NO_COLOR") != "", changes: map[string]*string{}}
}

func (w *setupWizard) Select(ctx context.Context, v workflow.ConfigurationView) (workflow.ConfigurationSelection, error) {
	w.started = true
	w.directory = v.ConfigDir
	reader, err := cancelreader.NewReader(w.config.In)
	if err != nil {
		return workflow.ConfigurationSelection{}, errors.New("terminal input is unavailable")
	}
	w.reader = reader
	stopInput := watchInput(ctx, reader)
	defer stopInput()

	w.header(v)
	var hosts []string
	for _, h := range v.Hosts {
		if h.Installed {
			hosts = append(hosts, h.ID)
			w.print("%s · enrolled at %s\n", hostName(h.ID), h.Root)
			continue
		}
		value := "no"
		if h.Selected {
			value = "yes"
		}
		if err := w.choose(ctx, "01 / Hosts", "Use "+hostName(h.ID)+"?", h.Root, []wizardOption{{"Use this host", "yes"}, {"Skip this host", "no"}}, &value); err != nil {
			return workflow.ConfigurationSelection{}, err
		}
		if value == "yes" {
			hosts = append(hosts, h.ID)
		}
	}
	if len(hosts) == 0 {
		return workflow.ConfigurationSelection{}, errors.New("select at least one installed agent host")
	}
	preset := "recommended"
	if v.Installed {
		preset = "keep"
	}
	choices := []wizardOption{}
	if v.Installed {
		choices = append(choices, wizardOption{"Keep saved choices  ·  recommended for updates", "keep"})
	}
	choices = append(choices, wizardOption{"Recommended  ·  Sol 6.1/max + Opus 5.5/xhigh", "recommended"}, wizardOption{"Balanced  ·  Sol 6.1/xhigh + Opus 5.5/high", "balanced"}, wizardOption{"Advanced  ·  choose each role and memory setting", "advanced"})
	if err := w.choose(ctx, "02 / Models", "Choose your workflow", "Named presets replace saved overrides, including bounded roles and memory/context settings. GitHub defaults stay Luna/high and Sonnet/medium.", choices, &preset); err != nil {
		return workflow.ConfigurationSelection{}, err
	}
	selection := workflow.ConfigurationSelection{Hosts: hosts}
	switch preset {
	case "recommended", "balanced":
		selection.Preset = workflow.Preset(preset)
	case "advanced":
		prefs, err := w.advanced(ctx, v)
		if err != nil {
			return selection, err
		}
		selection.Preferences = &prefs
	}
	prefs, err := workflow.PreferencesForSelection(v, selection)
	if err != nil {
		return selection, err
	}
	if err = w.credentials(ctx); err != nil {
		return selection, err
	}
	summary := configurationSummary(v, prefs, hosts, preset, w.changes)
	w.review(summary)
	apply := "yes"
	if err = w.choose(ctx, "04 / Review", "Apply these settings?", "Enter applies the reviewed changes. Esc or Ctrl-C cancels before installation changes.", []wizardOption{{"Apply settings", "yes"}, {"Cancel", "no"}}, &apply); err != nil {
		return selection, err
	}
	if apply != "yes" {
		return selection, context.Canceled
	}
	if err = ctx.Err(); err != nil {
		return selection, err
	}
	w.print("Applying verified workflows…\n")
	if w.outputErr != nil {
		return selection, w.outputErr
	}
	return selection, nil
}

func hostName(id string) string {
	if id == "codex" {
		return "Codex"
	}
	return "Claude Code"
}

func (w *setupWizard) advanced(ctx context.Context, v workflow.ConfigurationView) (workflow.Preferences, error) {
	data, err := json.Marshal(v.Preferences)
	if err != nil {
		return workflow.Preferences{}, err
	}
	var p workflow.Preferences
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	p.Version = 1
	if p.Hosts == nil {
		p.Hosts = map[string]workflow.HostPreferences{}
	}
	for {
		choices := []wizardOption{{"Review these choices", "done"}}
		for _, host := range v.Hosts {
			choices = append(choices, wizardOption{"Edit " + hostName(host.ID) + " roles", host.ID})
		}
		choices = append(choices, wizardOption{"Edit memory and context", "shared"})
		selected := "done"
		if err = w.choose(ctx, "02 / Advanced", "Choose what to customize", "Edit just the fields you need. Other fields keep their saved choices.", choices, &selected); err != nil {
			return p, err
		}
		if selected == "done" {
			return p, nil
		}
		if selected == "shared" {
			if err = w.shared(ctx, &p); err != nil {
				return p, err
			}
			continue
		}
		hostIndex := slices.IndexFunc(v.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == selected })
		if hostIndex < 0 {
			return p, errors.New("selected host is unavailable")
		}
		host := v.Hosts[hostIndex]
		for {
			roles := append(append([]workflow.RoleConfiguration{}, host.Roles...), host.Fallback)
			options := []wizardOption{{"Back to advanced choices", "back"}}
			for _, role := range roles {
				options = append(options, wizardOption{strings.ReplaceAll(role.Role, "_", " "), role.Role})
			}
			name := "back"
			if err = w.choose(ctx, "02 / Advanced", hostName(host.ID)+" roles", "Model and effort can each follow the release or stay pinned.", options, &name); err != nil {
				return p, err
			}
			if name == "back" {
				break
			}
			idx := slices.IndexFunc(roles, func(r workflow.RoleConfiguration) bool { return r.Role == name })
			if idx < 0 {
				return p, errors.New("selected role is unavailable")
			}
			role := roles[idx]
			hp := p.Hosts[host.ID]
			if hp.Roles == nil {
				hp.Roles = map[string]workflow.ModelOverride{}
			}
			role.Override = hp.Roles[name]
			if name == host.Fallback.Role && hp.Fallback != nil {
				role.Override = *hp.Fallback
			}
			override, e := w.role(ctx, hostName(host.ID)+" / "+strings.ReplaceAll(name, "_", " "), role)
			if e != nil {
				return p, e
			}
			if name == host.Fallback.Role {
				hp.Fallback = &override
			} else {
				hp.Roles[name] = override
			}
			p.Hosts[host.ID] = hp
		}
	}
}

func (w *setupWizard) shared(ctx context.Context, p *workflow.Preferences) error {
	if p.Shared == nil {
		p.Shared = &workflow.SharedPreferences{}
	}
	fields := []wizardOption{{"Back to advanced choices", "back"}, {"Context window", "window"}, {"Memory feature", "memories"}, {"Generate memories", "generate"}, {"Use memories", "use"}, {"Memory extraction model", "extract"}, {"Memory consolidation model", "consolidate"}}
	for {
		selected := "back"
		if err := w.choose(ctx, "02 / Advanced", "Memory and context", "Enter keeps the displayed choice; select a field to change it.", fields, &selected); err != nil {
			return err
		}
		if selected == "back" {
			return nil
		}
		if selected == "window" {
			value := ""
			if p.Shared.ContextWindow != nil {
				value = strconv.FormatInt(*p.Shared.ContextWindow, 10)
			}
			if err := w.input(ctx, "02 / Advanced", "Context window", "Enter keeps the current choice; type default to inherit, or enter a positive token count.", &value, false); err != nil {
				return err
			}
			p.Shared.ContextWindow = nil
			if value != "" {
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil || n <= 0 {
					return errors.New("context window must be a positive integer")
				}
				p.Shared.ContextWindow = &n
			}
			continue
		}
		if selected == "extract" || selected == "consolidate" {
			field := &p.Shared.ExtractModel
			if selected == "consolidate" {
				field = &p.Shared.ConsolidationModel
			}
			value := ""
			if *field != nil {
				value = **field
			}
			if err := w.input(ctx, "02 / Advanced", "Memory model", "Enter keeps the current choice; type default to inherit. An explicit model stays pinned.", &value, false); err != nil {
				return err
			}
			*field = nil
			if value != "" {
				*field = &value
			}
			continue
		}
		var field **bool
		switch selected {
		case "memories":
			field = &p.Shared.Memories
		case "generate":
			field = &p.Shared.GenerateMemories
		case "use":
			field = &p.Shared.UseMemories
		default:
			return errors.New("selected memory field is unavailable")
		}
		value := "inherit"
		if *field != nil {
			value = strconv.FormatBool(**field)
		}
		if err := w.choose(ctx, "02 / Advanced", "Memory setting", "Release defaults follow future updates; explicit values stay pinned.", []wizardOption{{"Use release default", "inherit"}, {"Enabled", "true"}, {"Disabled", "false"}}, &value); err != nil {
			return err
		}
		*field = nil
		if value != "inherit" {
			b := value == "true"
			*field = &b
		}
	}
}

func (w *setupWizard) role(ctx context.Context, title string, r workflow.RoleConfiguration) (workflow.ModelOverride, error) {
	ov := r.Override
	models := []wizardOption{{"Use release default  ·  " + r.Default.Model, "inherit"}}
	for _, m := range r.Choices {
		models = append(models, wizardOption{m.Model, m.Model})
	}
	model, effort := "inherit", "inherit"
	if ov.Model != nil {
		model = *ov.Model
	}
	if ov.Effort != nil {
		effort = *ov.Effort
	}
	desc := fmt.Sprintf("Current: %s / %s. Each explicit field stays pinned, including values equal to today's default.", r.Effective.Model, r.Effective.Effort)
	if r.Effective.Model == "" {
		desc = "New workflow role. Explicit fields stay pinned; release defaults follow future updates."
	}
	if err := w.choose(ctx, "02 / Advanced", title+" model", desc, models, &model); err != nil {
		return ov, err
	}
	selectedModel := model
	if selectedModel == "inherit" {
		selectedModel = r.Default.Model
	}
	var allowed []string
	for _, m := range r.Choices {
		if m.Model == selectedModel {
			allowed = m.Efforts
			break
		}
	}
	var efforts []wizardOption
	if slices.Contains(allowed, r.Default.Effort) {
		efforts = append(efforts, wizardOption{"Use release default  ·  " + r.Default.Effort, "inherit"})
	}
	for _, e := range allowed {
		efforts = append(efforts, wizardOption{e, e})
	}
	if len(efforts) == 0 {
		return ov, errors.New("selected role has no supported effort choices")
	}
	if effort == "inherit" && !slices.Contains(allowed, r.Default.Effort) || effort != "inherit" && !slices.Contains(allowed, effort) {
		effort = efforts[0].value
	}
	if err := w.choose(ctx, "02 / Advanced", title+" effort", "The selected model must support this effort; policy validation runs before applying.", efforts, &effort); err != nil {
		return ov, err
	}
	ov.Model = nil
	ov.Effort = nil
	if model != "inherit" {
		ov.Model = &model
	}
	if effort != "inherit" {
		ov.Effort = &effort
	}
	return ov, nil
}

func configurationSummary(v workflow.ConfigurationView, p workflow.Preferences, hosts []string, preset string, changes map[string]*string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release  %s\nHosts    %s\nChoice   %s\nPrivate settings  %s\n", v.Target.Version, strings.Join(hosts, ", "), preset, v.ConfigDir)
	for _, h := range v.Hosts {
		hp := p.Hosts[h.ID]
		type group struct {
			before, after, pin string
			roles              []string
		}
		var groups []group
		for _, r := range append(append([]workflow.RoleConfiguration{}, h.Roles...), h.Fallback) {
			ov := hp.Roles[r.Role]
			if r.Role == h.Fallback.Role && hp.Fallback != nil {
				ov = *hp.Fallback
			}
			after := r.Default
			if ov.Model != nil {
				after.Model = *ov.Model
			}
			if ov.Effort != nil {
				after.Effort = *ov.Effort
			}
			pin := "release defaults"
			if ov.Model != nil || ov.Effort != nil {
				pin = "explicit override"
			}
			before := r.Effective.Model + "/" + r.Effective.Effort
			if r.Effective.Model == "" {
				before = "not configured"
			}
			label := after.Model + "/" + after.Effort
			idx := slices.IndexFunc(groups, func(g group) bool { return g.before == before && g.after == label && g.pin == pin })
			if idx < 0 {
				groups = append(groups, group{before: before, after: label, pin: pin})
				idx = len(groups) - 1
			}
			groups[idx].roles = append(groups[idx].roles, r.Role)
		}
		fmt.Fprintf(&b, "\n\n%s workflow policy (current → selected)", hostName(h.ID))
		for _, g := range groups {
			fmt.Fprintf(&b, "\n%s\n  %s → %s (%s)", strings.Join(g.roles, ", "), g.before, g.after, g.pin)
		}
	}
	shared := map[string]any{}
	data, _ := json.Marshal(p.Shared)
	_ = json.Unmarshal(data, &shared)
	for _, name := range []string{"model_context_window", "features.memories", "memories.generate_memories", "memories.use_memories", "memories.extract_model", "memories.consolidation_model"} {
		after := v.SharedDefaults[name]
		if value, ok := shared[name]; ok {
			after = value
		}
		before := v.SharedEffective[name]
		if before == nil || !v.Installed {
			before = "unmanaged"
		}
		fmt.Fprintf(&b, "\n%s  %v → %v", name, before, after)
	}
	for _, name := range credentials.Names {
		if _, ok := changes[name]; ok {
			fmt.Fprintf(&b, "\n%s  replace stored key (hidden)", name)
		}
	}
	return b.String()
}
