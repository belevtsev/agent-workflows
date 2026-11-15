package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func TestUXAccessibleEditorRespectsNoColor(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	for _, value := range []string{"1", "present", "0"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("NO_COLOR", value)
			var output bytes.Buffer
			host := ""
			form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Host").Options(huh.NewOption("Codex", "codex")).Value(&host)))
			cfg := Config{In: strings.NewReader("1\n"), Err: &output}
			if err := uxRunForm(t.Context(), cfg, form, true); err != nil || host != "codex" {
				t.Fatalf("accessible form did not complete: %q, %v", host, err)
			}
			if text := output.String(); strings.Contains(text, "38;") || strings.Contains(text, "48;") {
				t.Fatalf("NO_COLOR=%q leaked ANSI color sequences: %q", value, text)
			}
		})
	}
}

func TestUXEditorsRequireExplicitTerminalAndRejectJSON(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	for _, role := range []bool{false, true} {
		call := func(args []string) error {
			if role {
				return runUXRoleEdit(t.Context(), cfg, append([]string{"developer", "--host", "codex"}, args...))
			}
			return runUXConfigEdit(t.Context(), cfg, args)
		}
		if err := call(nil); ExitCode(err) != 2 || !strings.Contains(err.Error(), "terminal input and stderr") {
			t.Fatalf("headless editor did not fail before prompting: %v", err)
		}
		cfg.Editor = func(context.Context, *UXEditor) error { t.Fatal("JSON editor launched"); return nil }
		for _, json := range []string{"--json", "--json=false"} {
			if err := call([]string{json}); ExitCode(err) != 2 || !strings.Contains(err.Error(), "do not support --json") {
				t.Fatalf("JSON editor did not fail before launch: %v", err)
			}
		}
		cfg.Editor = nil
	}
	for _, path := range []string{checkout, personal, checkout + ".lock", personal + ".lock"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("headless or JSON editor wrote %s: %v", path, err)
		}
	}
}

func TestUXEditorCancellationWritesNothing(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		result error
		want   int
	}{
		{name: "explicit cancel", want: 0},
		{name: "control c", result: huh.ErrUserAborted, want: 130},
		{name: "cancelled context", result: context.Canceled, want: 130},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg.Editor = func(_ context.Context, draft *UXEditor) error {
				draft.Host, draft.Model, draft.Skills = "codex", "custom", []string{"missing"}
				// An interrupt cannot commit even if Save was selected.
				draft.Save = test.result != nil
				return test.result
			}
			if err := runUXConfigEdit(t.Context(), cfg, nil); ExitCode(err) != test.want {
				t.Fatalf("config editor exit %d, want %d: %v", ExitCode(err), test.want, err)
			}
			if err := runUXRoleEdit(t.Context(), cfg, []string{"developer", "--host", "codex"}); ExitCode(err) != test.want {
				t.Fatalf("role editor exit %d, want %d: %v", ExitCode(err), test.want, err)
			}
		})
	}
	for _, path := range []string{checkout, personal, filepath.Join(root, roles.FileName), checkout + ".lock", personal + ".lock", filepath.Join(root, ".git", "kitsu-roles.lock")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cancelled editor wrote %s: %v", path, err)
		}
	}
}

func TestUXConfigEditorSavesAndAbortsConcurrentChanges(t *testing.T) {
	cfg, checkout, _ := uxTestConfig(t)
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		if draft.Kind != "config" || !strings.Contains(draft.Preview(), checkout) {
			t.Fatalf("editor lacks scope preview: %+v", draft)
		}
		draft.Host, draft.Save = "codex", true
		return nil
	}
	if err := runUXConfigEdit(t.Context(), cfg, []string{"--accessible"}); err != nil {
		t.Fatal(err)
	}
	if host, _, err := uxReadPreference(checkout); err != nil || host != "codex" {
		t.Fatalf("confirmed editor did not save: %q, %v", host, err)
	}
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		uxTestWrite(t, checkout, "version = 1\nhost = 'claude'\n")
		draft.Host, draft.Save = "codex", true
		return nil
	}
	if err := runUXConfigEdit(t.Context(), cfg, nil); err == nil || !strings.Contains(err.Error(), "changed while editing") {
		t.Fatalf("editor overwrote concurrent preference: %v", err)
	}
	if host, _, err := uxReadPreference(checkout); err != nil || host != "claude" {
		t.Fatalf("concurrent preference was lost: %q, %v", host, err)
	}
}

func TestUXRoleEditorCustomValuesSkillValidationAndConcurrency(t *testing.T) {
	cfg, _, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, roles.FileName)
	const before = "# Role choices\nversion = 1\n[paths]\ntests = ['tests/**'] # keep\n[codex.developer]\nmodel = 'account-model'\neffort = 'high'\n[codex.reviewer]\nmodel = 'keep-reviewer'\n"
	uxTestWrite(t, path, before)
	skillPath := filepath.Join(root, ".agents", "skills", "review", "SKILL.md")
	writeSkillFixture(t, skillPath, "review")
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		if !slices.Contains(draft.Models, "account-model") || !slices.Contains(draft.Models, "gpt-6.1-terra") || !slices.Contains(draft.ReasoningChoices, "max") {
			t.Fatalf("editor lacks current/built-in suggestions: %+v", draft)
		}
		draft.Model, draft.Reasoning = "custom-account-model", "custom-effort"
		draft.Skills, draft.Save = []string{"review"}, true
		return nil
	}
	if err := runUXRoleEdit(t.Context(), cfg, []string{"developer", "--host", "codex"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := roles.Parse(data)
	if err != nil || parsed.Models["codex"]["developer"] != (roles.Model{Model: "custom-account-model", Effort: "custom-effort"}) || len(parsed.Skills["codex"]["developer"]) != 1 || parsed.Models["codex"]["reviewer"].Model != "keep-reviewer" || !strings.Contains(string(data), "tests = ['tests/**'] # keep") {
		t.Fatalf("editor changed unrelated settings or lost custom values: %s, %v", data, err)
	}
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		if err := os.Remove(skillPath); err != nil {
			t.Fatal(err)
		}
		draft.Model, draft.Save = "should-not-save", true
		return nil
	}
	if err := runUXRoleEdit(t.Context(), cfg, []string{"developer", "--host", "codex"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("source was not revalidated before save: %v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(data) {
		t.Fatal("failed source validation partially wrote the role config")
	}
	writeSkillFixture(t, skillPath, "review")
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		uxTestWrite(t, path, before)
		draft.Model, draft.Save = "should-not-save", true
		return nil
	}
	if err := runUXRoleEdit(t.Context(), cfg, []string{"developer", "--host", "codex"}); err == nil || !strings.Contains(err.Error(), "changed while editing") {
		t.Fatalf("concurrent role configuration was not protected: %v", err)
	}
}

func TestUXRoleEditorHostChoiceAndScope(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	visited := []string{}
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		visited = append(visited, draft.Kind)
		if draft.Kind == "host" {
			draft.Host = "codex"
		} else {
			draft.Model = "chosen-model"
		}
		draft.Save = true
		return nil
	}
	if err := runUXRoleEdit(t.Context(), cfg, []string{"developer"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(visited, []string{"host", "role"}) {
		t.Fatalf("missing host was not chosen inside explicit editor: %q", visited)
	}
	for _, path := range []string{checkout, personal} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("role host choice auto-saved preference %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, roles.FileName)); err != nil || !strings.Contains(string(data), "chosen-model") {
		t.Fatalf("chosen host role edit not saved: %s, %v", data, err)
	}
	for _, draft := range []*UXEditor{{Kind: "role", Global: true, Role: "developer"}, {Kind: "role", Role: roles.Coordinator}} {
		for _, field := range uxEditorFields(draft, false) {
			if field.GetKey() == "model" || field.GetKey() == "reasoning" {
				t.Fatal("global/coordinator editor exposes model controls")
			}
		}
	}
}

func TestUXRoleEditorAbortsChangedInheritedConfiguration(t *testing.T) {
	cfg, _, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, roles.FileName)
	const original = "version = 1\n[codex.developer]\nmodel = 'keep'\n"
	uxTestWrite(t, path, original)
	personal, err := roles.PersonalPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Editor = func(_ context.Context, draft *UXEditor) error {
		uxTestWrite(t, personal, "version = 1\n[codex.reviewer]\nskills = []\n")
		draft.Model, draft.Save = "should-not-save", true
		return nil
	}
	if err := runUXRoleEdit(t.Context(), cfg, []string{"developer", "--host", "codex"}); err == nil || !strings.Contains(err.Error(), "changed while editing") {
		t.Fatalf("editor ignored changed inherited configuration: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != original {
		t.Fatalf("stale inherited settings partially changed role file: %s, %v", data, err)
	}
}

func TestUXEditorContextHelpPreservesDraftAndUnavailableReasons(t *testing.T) {
	t.Parallel()
	value := "partially entered model"
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Key("model").Value(&value)))
	model := uxFormModel{form: form, width: 32}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	model = updated.(uxFormModel)
	if !model.help || !strings.Contains(strings.Join(strings.Fields(model.View().Content), " "), "custom model name") {
		t.Fatal("F1 did not open contextual field help")
	}
	for _, line := range strings.Split(model.View().Content, "\n") {
		if len(line) > 30 {
			t.Fatalf("help is truncated instead of wrapping to the terminal: %q", line)
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(uxFormModel)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(uxFormModel)
	if model.help || value != "partially entered model" || model.form != form {
		t.Fatal("help navigation changed or rebuilt the editor draft")
	}
	draft := &UXEditor{AvailableSkills: []skills.Entry{{ID: "codex:user:disabled", Name: "disabled", Disabled: true, Reason: "disabled by settings"}, {ID: "codex:user:manual", Name: "manual", ManualOnly: true}}}
	options, reasons := uxSkillOptions(draft)
	if len(options) != 2 || !strings.Contains(options[0].Key, "disabled by settings") || reasons["codex:user:manual"] == "" {
		t.Fatal("searchable skill options lost availability reasons")
	}
	if title := uxFieldTitle("model", "Model", true); !strings.Contains(title, uxEditorHelp["model"]) {
		t.Fatal("accessible field does not include full explanation")
	}
}
