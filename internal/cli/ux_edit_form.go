package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var uxEditorHelp = map[string]string{
	"host":      "Choose codex or claude. A role editor uses this host for this edit only; it does not save a host preference. In config edit, inherit/unset removes this scope's preference.",
	"model":     "Suggestions include current and built-in models. Type a custom model name if needed; Ctrl+E accepts a suggestion. Unchanged values keep existing overrides. Use role reset to restore defaults.",
	"reasoning": "Suggestions include current and built-in effort values. Custom host-supported values are allowed. Unchanged effort keeps its existing override; the coordinator uses host-session settings.",
	"skills":    "Press / to search names, IDs and descriptions; Space toggles a selection. Disabled or manual-only sources show a reason and cannot be saved. Clearing all selections writes an empty list; unchanged selections keep inheritance. Skills supplement native host skills and do not grant permissions.",
	"save":      "Review the destination and proposed settings, then choose Save or Cancel. Save revalidates sources and refuses configuration changed while the editor was open. Ctrl+C cancels without saving.",
}

func uxFieldTitle(key, title string, accessible bool) string {
	if accessible {
		return title + "\n" + uxEditorHelp[key]
	}
	return title
}

func uxRenderEditor(ctx context.Context, cfg Config, draft *UXEditor) error {
	accessible := draft.Accessible || os.Getenv("TERM") == "dumb"
	for {
		form := huh.NewForm(huh.NewGroup(uxEditorFields(draft, accessible)...))
		if err := uxRunForm(ctx, cfg, form, accessible); err != nil {
			return err
		}
		if draft.Kind == "host" {
			draft.Save = true
			return nil
		}
		// Keep the complete preview in terminal scrollback instead of clipping
		// a long role/skill selection inside a form field's viewport.
		fmt.Fprintln(cfg.Err, draft.Preview())
		action := "cancel"
		confirm := huh.NewSelect[string]().Key("save").Title(uxFieldTitle("save", "Save these settings?", accessible)).
			Description(uxEditorHelp["save"]).Options(huh.NewOption("Save", "save"), huh.NewOption("Back to editing", "back"), huh.NewOption("Cancel", "cancel")).Value(&action)
		if err := uxRunForm(ctx, cfg, huh.NewForm(huh.NewGroup(confirm)), accessible); err != nil {
			return err
		}
		if action != "back" {
			draft.Save = action == "save"
			return nil
		}
	}
}

func uxRunForm(ctx context.Context, cfg Config, form *huh.Form, accessible bool) error {
	form.WithInput(cfg.In).WithOutput(cfg.Err)
	if accessible {
		if os.Getenv("NO_COLOR") != "" {
			form.WithOutput(&colorprofile.Writer{Forward: cfg.Err, Profile: colorprofile.ASCII})
		}
		return form.WithAccessible(true).RunWithContext(ctx)
	}
	form.SubmitCmd, form.CancelCmd = tea.Quit, tea.Interrupt
	options := []tea.ProgramOption{tea.WithInput(cfg.In), tea.WithOutput(cfg.Err), tea.WithContext(ctx)}
	if os.Getenv("NO_COLOR") != "" {
		options = append(options, tea.WithColorProfile(colorprofile.ASCII))
	}
	_, err := tea.NewProgram(uxFormModel{form: form}, options...).Run()
	if form.State == huh.StateAborted || errors.Is(err, tea.ErrInterrupted) {
		return huh.ErrUserAborted
	}
	return err
}

func uxEditorFields(draft *UXEditor, accessible bool) []huh.Field {
	if draft.Kind != "role" {
		options := []huh.Option[string]{huh.NewOption("Codex", "codex"), huh.NewOption("Claude Code", "claude")}
		if draft.Kind == "config" {
			options = append([]huh.Option[string]{huh.NewOption("Inherit / unset", "")}, options...)
		}
		return []huh.Field{huh.NewSelect[string]().Key("host").Title(uxFieldTitle("host", "Host", accessible)).
			Description(uxEditorHelp["host"]).Options(options...).Value(&draft.Host)}
	}
	fields := []huh.Field{}
	if !draft.Global && draft.Role != roles.Coordinator {
		fields = append(fields,
			huh.NewInput().Key("model").Title(uxFieldTitle("model", "Model", accessible)).Description(uxEditorHelp["model"]).
				Suggestions(draft.Models).Value(&draft.Model).Validate(uxNonemptyInput),
			huh.NewInput().Key("reasoning").Title(uxFieldTitle("reasoning", "Reasoning", accessible)).Description(uxEditorHelp["reasoning"]).
				Suggestions(draft.ReasoningChoices).Value(&draft.Reasoning).Validate(uxNonemptyInput))
	}
	options, unavailable := uxSkillOptions(draft)
	if len(options) > 0 {
		field := huh.NewMultiSelect[string]().Key("skills").Title(uxFieldTitle("skills", "Required skill guidance", accessible)).
			Description(uxEditorHelp["skills"]).Options(options...).Value(&draft.Skills).Filterable(true).
			Validate(func(values []string) error {
				for _, value := range values {
					if reason := unavailable[value]; reason != "" {
						return fmt.Errorf("%s: %s", value, reason)
					}
				}
				return nil
			})
		fields = append(fields, field)
	} else {
		fields = append(fields, huh.NewNote().Title("No local skill selections").Description(uxEditorHelp["skills"]))
	}
	return fields
}

func uxNonemptyInput(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("enter a nonempty value")
	}
	return nil
}

func uxSkillOptions(draft *UXEditor) ([]huh.Option[string], map[string]string) {
	options, unavailable, values := []huh.Option[string]{}, map[string]string{}, []string{}
	for _, entry := range draft.AvailableSkills {
		value := uxSkillSelector(entry)
		label := entry.Name + " — " + entry.ID + "\n" + entry.Description
		reason := entry.Reason
		if entry.ManualOnly {
			if reason != "" {
				reason += "; "
			}
			reason += "manual-only; automatic role loading is unavailable"
		}
		if entry.Disabled || entry.ManualOnly {
			if reason == "" {
				reason = "source unavailable"
			}
			unavailable[value], label = reason, label+"\nUnavailable: "+reason
		}
		options, values = append(options, huh.NewOption(label, value)), append(values, value)
	}
	for _, value := range draft.Skills {
		if !slices.Contains(values, value) {
			unavailable[value] = "selected source is unavailable; remove it or restore the source"
			options = append(options, huh.NewOption(value+"\nUnavailable: "+unavailable[value], value))
		}
	}
	return options, unavailable
}

// uxFormModel adds a contextual help view without advancing or rebuilding the
// Huh form, so partially entered values and selections survive F1 and Escape.
type uxFormModel struct {
	form  *huh.Form
	help  bool
	width int
}

func (m uxFormModel) Init() tea.Cmd { return m.form.Init() }

func (m uxFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if key.String() == "f1" || m.help && key.String() == "esc" {
			m.help = !m.help
			return m, nil
		}
		if m.help && key.String() != "ctrl+c" {
			return m, nil
		}
	}
	_, cmd := m.form.Update(msg)
	return m, cmd
}

func (m uxFormModel) View() tea.View {
	if m.help {
		key := m.form.GetFocusedField().GetKey()
		if key == "" {
			key = "skills"
		}
		width := 78
		if m.width > 0 {
			width = max(1, m.width-2)
		}
		text := "Help: " + key + "\n\n" + uxEditorHelp[key] + "\n\nF1 / Escape: return to the editor. Ctrl+C: cancel."
		return tea.NewView(ansi.Wrap(text, width, ""))
	}
	return tea.NewView(m.form.View() + "\nF1: field help · Ctrl+C: cancel")
}
