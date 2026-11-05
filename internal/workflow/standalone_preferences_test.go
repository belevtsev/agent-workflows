package workflow_test

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/workflow"
)

func standalonePointer[Value any](value Value) *Value { return &value }
func standaloneSnapshot(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func standaloneShape(value workflow.Preferences) workflow.Preferences {
	result := workflow.Preferences{Version: value.Version}
	for hostID, host := range value.Hosts {
		shaped := workflow.HostPreferences{}
		for role, override := range host.Roles {
			if override.Model == nil && override.Effort == nil {
				continue
			}
			if shaped.Roles == nil {
				shaped.Roles = make(map[string]workflow.ModelOverride)
			}
			shaped.Roles[role] = override
		}
		if host.Fallback != nil && (host.Fallback.Model != nil || host.Fallback.Effort != nil) {
			shaped.Fallback = host.Fallback
		}
		if len(shaped.Roles) == 0 && shaped.Fallback == nil {
			continue
		}
		if result.Hosts == nil {
			result.Hosts = make(map[string]workflow.HostPreferences)
		}
		result.Hosts[hostID] = shaped
	}
	if value.Shared != nil && !reflect.DeepEqual(*value.Shared, workflow.SharedPreferences{}) {
		result.Shared = value.Shared
	}
	return result
}
func standalonePins(codexEffort, claudeEffort string) workflow.Preferences {
	preferences := workflow.Preferences{Version: 1, Hosts: make(map[string]workflow.HostPreferences)}
	for _, host := range []struct{ id, model, effort string }{{"codex", "gpt-6.1-sol", codexEffort}, {"claude", "claude-opus-5-5", claudeEffort}} {
		override := workflow.ModelOverride{Model: standalonePointer(host.model), Effort: standalonePointer(host.effort)}
		preferences.Hosts[host.id] = workflow.HostPreferences{Roles: map[string]workflow.ModelOverride{"coordinator": override, "testing": override}, Fallback: &override}
	}
	return preferences
}
func standaloneSaved() workflow.Preferences {
	preferences := standalonePins("max", "xhigh")
	for _, host := range preferences.Hosts {
		host.Roles["commit_execution"] = workflow.ModelOverride{Effort: standalonePointer("high")}
		host.Roles["lookup"] = workflow.ModelOverride{Effort: standalonePointer("high")}
	}
	preferences.Shared = &workflow.SharedPreferences{ContextWindow: standalonePointer(int64(128000)), Memories: standalonePointer(false), GenerateMemories: standalonePointer(false), UseMemories: standalonePointer(false), ExtractModel: standalonePointer("gpt-6.1-sol"), ConsolidationModel: standalonePointer("gpt-6.1-sol")}
	return preferences
}
func standaloneView(saved workflow.Preferences) workflow.ConfigurationView {
	defaults := workflow.Object{"model_context_window": int64(200000), "features.memories": true, "memories.generate_memories": true, "memories.use_memories": true, "memories.extract_model": "gpt-6.1-sol", "memories.consolidation_model": "gpt-6.1-sol"}
	view := workflow.ConfigurationView{Preferences: saved, SharedDefaults: defaults, SharedEffective: make(workflow.Object)}
	for key, value := range defaults {
		view.SharedEffective[key] = value
	}
	if shared := saved.Shared; shared != nil {
		view.SharedEffective = workflow.Object{"model_context_window": *shared.ContextWindow, "features.memories": *shared.Memories, "memories.generate_memories": *shared.GenerateMemories, "memories.use_memories": *shared.UseMemories, "memories.extract_model": *shared.ExtractModel, "memories.consolidation_model": *shared.ConsolidationModel}
	}
	for _, hostID := range []string{"codex", "claude"} {
		model, effort, efforts := "gpt-6.1-sol", "max", []string{"high", "xhigh", "max"}
		if hostID == "claude" {
			model, effort, efforts = "claude-opus-5-5", "xhigh", []string{"high", "xhigh"}
		}
		makeRole := func(name, scope string, override workflow.ModelOverride) workflow.RoleConfiguration {
			defaultSelection := workflow.ModelSelection{Model: model, Effort: effort}
			effective := defaultSelection
			if override.Model != nil {
				effective.Model = *override.Model
			}
			if override.Effort != nil {
				effective.Effort = *override.Effort
			}
			return workflow.RoleConfiguration{Role: name, Scope: scope, Default: defaultSelection, Effective: effective, Override: override, Choices: []workflow.ModelChoice{{Model: model, Efforts: append([]string(nil), efforts...)}}}
		}
		host := workflow.HostConfiguration{ID: hostID, Detected: true, Selected: true}
		for _, role := range []struct{ name, scope string }{{"coordinator", "substantive"}, {"testing", "substantive"}, {"commit_execution", "bounded_execution"}, {"lookup", "bounded_evidence"}} {
			host.Roles = append(host.Roles, makeRole(role.name, role.scope, saved.Hosts[hostID].Roles[role.name]))
		}
		fallback := workflow.ModelOverride{}
		if saved.Hosts[hostID].Fallback != nil {
			fallback = *saved.Hosts[hostID].Fallback
		}
		host.Fallback = makeRole("fallback", "substantive", fallback)
		view.Hosts = append(view.Hosts, host)
	}
	return view
}
func standaloneMutateReturned(value workflow.Preferences) {
	for _, host := range value.Hosts {
		overrides := make([]workflow.ModelOverride, 0, len(host.Roles)+1)
		for _, override := range host.Roles {
			overrides = append(overrides, override)
		}
		if host.Fallback != nil {
			overrides = append(overrides, *host.Fallback)
		}
		for _, override := range overrides {
			for _, field := range []*string{override.Model, override.Effort} {
				if field != nil {
					*field = "returned-object-mutation"
				}
			}
		}
		clear(host.Roles)
	}
	clear(value.Hosts)
	if shared := value.Shared; shared != nil {
		if shared.ContextWindow != nil {
			*shared.ContextWindow = 1
		}
		for _, field := range []*bool{shared.Memories, shared.GenerateMemories, shared.UseMemories} {
			if field != nil {
				*field = !*field
			}
		}
		for _, field := range []*string{shared.ExtractModel, shared.ConsolidationModel} {
			if field != nil {
				*field = "returned-object-mutation"
			}
		}
	}
}
func TestStandaloneDecodePreferencesContract(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	empty := workflow.Preferences{Version: 1}
	partial := workflow.Preferences{Version: 1, Hosts: map[string]workflow.HostPreferences{"codex": {Roles: map[string]workflow.ModelOverride{"testing": {Model: standalonePointer("gpt-6.1-sol")}}}, "claude": {Fallback: &workflow.ModelOverride{Effort: standalonePointer("high")}}}}
	future := workflow.Preferences{Version: 1, Hosts: map[string]workflow.HostPreferences{"codex": {Roles: map[string]workflow.ModelOverride{"future_role": {Model: standalonePointer("future-model"), Effort: standalonePointer("future-effort")}}}}}
	for _, test := range []struct {
		name, data string
		want       workflow.Preferences
	}{
		{"minimal", `{"version":1}`, empty},
		{"empty-overrides", `{"version":1,"hosts":{"codex":{"roles":{"testing":{}},"fallback":{}},"claude":{}},"shared":{}}`, empty},
		{"all-fields-and-explicit-false", standaloneSnapshot(t, standaloneSaved()), standaloneSaved()},
		{"per-field-inheritance", `{"version":1,"hosts":{"codex":{"roles":{"testing":{"model":"gpt-6.1-sol"}}},"claude":{"fallback":{"effort":"high"}}}}`, partial},
		{"release-dependent-role-model-effort", `{"version":1,"hosts":{"codex":{"roles":{"future_role":{"model":"future-model","effort":"future-effort"}}}}}`, future},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := workflow.DecodePreferences([]byte(test.data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(standaloneShape(got), standaloneShape(test.want)) {
				t.Fatalf("decoded %#v; want %#v", got, test.want)
			}
		})
	}
	role := func(value string) string {
		return `{"version":1,"hosts":{"codex":{"roles":{"testing":` + value + `}}}}`
	}
	shared := func(fields string) string { return `{"version":1,"shared":{` + fields + `}}` }
	for _, test := range []struct{ name, data string }{
		{"empty-input", ""}, {"malformed", `{"version":1,`}, {"trailing-document", `{"version":1}{"version":1}`},
		{"null-root", `null`}, {"array-root", `[]`}, {"missing-version", `{}`}, {"wrong-version", `{"version":2}`}, {"zero-version", `{"version":0}`},
		{"null-version", `{"version":null}`}, {"string-version", `{"version":"1"}`}, {"fraction-version", `{"version":1.5}`},
		{"secret-field", `{"version":1,"api_key":"not-a-preference"}`}, {"unknown-host", `{"version":1,"hosts":{"other":{}}}`},
		{"null-hosts", `{"version":1,"hosts":null}`}, {"array-hosts", `{"version":1,"hosts":[]}`}, {"null-host", `{"version":1,"hosts":{"codex":null}}`},
		{"unknown-host-field", `{"version":1,"hosts":{"codex":{"token":"secret"}}}`}, {"null-roles", `{"version":1,"hosts":{"codex":{"roles":null}}}`}, {"array-roles", `{"version":1,"hosts":{"codex":{"roles":[]}}}`},
		{"null-role", role(`null`)}, {"array-role", role(`[]`)}, {"unknown-override-field", role(`{"secret":"x"}`)},
		{"null-model", role(`{"model":null}`)}, {"number-model", role(`{"model":3}`)}, {"blank-model", role(`{"model":" \t"}`)},
		{"null-effort", role(`{"effort":null}`)}, {"bool-effort", role(`{"effort":false}`)}, {"blank-effort", role(`{"effort":""}`)},
		{"null-fallback", `{"version":1,"hosts":{"claude":{"fallback":null}}}`}, {"unknown-fallback-field", `{"version":1,"hosts":{"claude":{"fallback":{"token":"secret"}}}}`}, {"array-fallback", `{"version":1,"hosts":{"claude":{"fallback":[]}}}`},
		{"duplicate-version", `{"version":1,"version":1}`}, {"duplicate-host", `{"version":1,"hosts":{"codex":{},"codex":{}}}`}, {"duplicate-roles-field", `{"version":1,"hosts":{"codex":{"roles":{},"roles":{}}}}`},
		{"duplicate-role", `{"version":1,"hosts":{"codex":{"roles":{"testing":{},"testing":{}}}}`}, {"duplicate-fallback-field", `{"version":1,"hosts":{"claude":{"fallback":{},"fallback":{}}}}`}, {"duplicate-model", role(`{"model":"a","model":"b"}`)},
		{"duplicate-effort", role(`{"effort":"high","effort":"max"}`)}, {"escaped-duplicate-model", role(`{"model":"a","\u006dodel":"b"}`)},
		{"null-shared", `{"version":1,"shared":null}`}, {"array-shared", `{"version":1,"shared":[]}`}, {"unknown-shared", shared(`"other":true`)},
		{"duplicate-shared", shared(`"features.memories":true,"features.memories":false`)}, {"duplicate-shared-section", `{"version":1,"shared":{},"shared":{}}`},
		{"null-context", shared(`"model_context_window":null`)}, {"string-context", shared(`"model_context_window":"100"`)}, {"fraction-context", shared(`"model_context_window":1.5`)},
		{"zero-context", shared(`"model_context_window":0`)}, {"negative-context", shared(`"model_context_window":-1`)}, {"overflow-context", shared(`"model_context_window":9223372036854775808`)},
		{"null-memories", shared(`"features.memories":null`)}, {"string-memories", shared(`"features.memories":"false"`)},
		{"null-generate", shared(`"memories.generate_memories":null`)}, {"number-generate", shared(`"memories.generate_memories":0`)},
		{"null-use", shared(`"memories.use_memories":null`)}, {"object-use", shared(`"memories.use_memories":{}`)},
		{"null-extract", shared(`"memories.extract_model":null`)}, {"number-extract", shared(`"memories.extract_model":1`)}, {"blank-extract", shared(`"memories.extract_model":" \t"`)}, {"multiline-extract", shared(`"memories.extract_model":"a\nb"`)},
		{"null-consolidation", shared(`"memories.consolidation_model":null`)}, {"bool-consolidation", shared(`"memories.consolidation_model":true`)}, {"blank-consolidation", shared(`"memories.consolidation_model":""`)}, {"multiline-consolidation", shared(`"memories.consolidation_model":"a\rb"`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := workflow.DecodePreferences([]byte(test.data)); err == nil {
				t.Fatal("invalid preferences accepted")
			}
		})
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("decoder created files: %v, %v", entries, err)
	}
}
func TestStandalonePreferencesForSelectionContract(t *testing.T) {
	empty, explicit := workflow.Preferences{Version: 1}, standaloneSaved()
	partial := workflow.Preferences{Version: 1, Hosts: map[string]workflow.HostPreferences{"codex": {Roles: map[string]workflow.ModelOverride{"testing": {Model: standalonePointer("gpt-6.1-sol")}, "coordinator": {Effort: standalonePointer("max")}}}}}
	for _, test := range []struct {
		name       string
		selection  workflow.ConfigurationSelection
		want       workflow.Preferences
		emptySaved bool
	}{
		{"default-preserves-all-hosts-and-shared", workflow.ConfigurationSelection{}, standaloneSaved(), false},
		{"default-host-selection-preserves-other-host", workflow.ConfigurationSelection{Hosts: []string{"codex"}}, standaloneSaved(), false},
		{"recommended-resets-everything", workflow.ConfigurationSelection{Hosts: []string{"codex"}, Preset: workflow.PresetRecommended}, empty, false},
		{"balanced-pins-both-hosts-only-substantive", workflow.ConfigurationSelection{Hosts: []string{"codex"}, Preset: workflow.PresetBalanced}, standalonePins("xhigh", "high"), false},
		{"explicit-equal-defaults-and-false-stay-pinned", workflow.ConfigurationSelection{Preferences: &explicit}, standaloneSaved(), false},
		{"explicit-per-field-nil-keeps-inheritance", workflow.ConfigurationSelection{Preferences: &partial}, partial, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved := standaloneSaved()
			if test.emptySaved {
				saved = empty
			}
			view := standaloneView(saved)
			beforeView, beforeSelection := standaloneSnapshot(t, view), standaloneSnapshot(t, test.selection)
			got, err := workflow.PreferencesForSelection(view, test.selection)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(standaloneShape(got), standaloneShape(test.want)) {
				t.Fatalf("preferences %#v; want %#v", got, test.want)
			}
			if standaloneSnapshot(t, view) != beforeView || standaloneSnapshot(t, test.selection) != beforeSelection {
				t.Fatal("compiler mutated its inputs")
			}
			standaloneMutateReturned(got)
			if standaloneSnapshot(t, view) != beforeView || standaloneSnapshot(t, test.selection) != beforeSelection {
				t.Fatal("returned preferences alias caller-owned data")
			}
		})
	}
}
func TestStandaloneSelectionRejectsInvalidChoices(t *testing.T) {
	empty := workflow.Preferences{Version: 1}
	preferences := func(host, role string, override workflow.ModelOverride) *workflow.Preferences {
		return &workflow.Preferences{Version: 1, Hosts: map[string]workflow.HostPreferences{host: {Roles: map[string]workflow.ModelOverride{role: override}}}}
	}
	for _, test := range []struct {
		name      string
		selection workflow.ConfigurationSelection
	}{
		{"unknown-preset", workflow.ConfigurationSelection{Preset: workflow.Preset("unknown")}},
		{"recommended-explicit-conflict", workflow.ConfigurationSelection{Preset: workflow.PresetRecommended, Preferences: &empty}},
		{"balanced-explicit-conflict", workflow.ConfigurationSelection{Preset: workflow.PresetBalanced, Preferences: &empty}},
		{"unknown-selected-host", workflow.ConfigurationSelection{Hosts: []string{"other"}}},
		{"unknown-preference-host", workflow.ConfigurationSelection{Preferences: preferences("other", "testing", workflow.ModelOverride{Effort: standalonePointer("high")})}},
		{"unknown-role", workflow.ConfigurationSelection{Preferences: preferences("claude", "missing", workflow.ModelOverride{Effort: standalonePointer("high")})}},
		{"unsupported-model", workflow.ConfigurationSelection{Preferences: preferences("codex", "testing", workflow.ModelOverride{Model: standalonePointer("not-an-approved-model")})}},
		{"unsupported-effort", workflow.ConfigurationSelection{Preferences: preferences("codex", "testing", workflow.ModelOverride{Effort: standalonePointer("unsupported-effort")})}},
		{"unsupported-claude-effort", workflow.ConfigurationSelection{Preferences: preferences("claude", "testing", workflow.ModelOverride{Effort: standalonePointer("max")})}},
		{"unsupported-fallback", workflow.ConfigurationSelection{Preferences: &workflow.Preferences{Version: 1, Hosts: map[string]workflow.HostPreferences{"claude": {Fallback: &workflow.ModelOverride{Effort: standalonePointer("max")}}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := standaloneView(standaloneSaved())
			beforeView, beforeSelection := standaloneSnapshot(t, view), standaloneSnapshot(t, test.selection)
			if _, err := workflow.PreferencesForSelection(view, test.selection); err == nil {
				t.Fatal("invalid selection accepted")
			}
			if standaloneSnapshot(t, view) != beforeView || standaloneSnapshot(t, test.selection) != beforeSelection {
				t.Fatal("rejected selection mutated inputs")
			}
		})
	}
}
func TestStandaloneSharedOverridesRequireReleaseField(t *testing.T) {
	for _, test := range []struct {
		key    string
		shared workflow.SharedPreferences
	}{
		{"model_context_window", workflow.SharedPreferences{ContextWindow: standalonePointer(int64(100))}},
		{"features.memories", workflow.SharedPreferences{Memories: standalonePointer(false)}},
		{"memories.generate_memories", workflow.SharedPreferences{GenerateMemories: standalonePointer(false)}},
		{"memories.use_memories", workflow.SharedPreferences{UseMemories: standalonePointer(false)}},
		{"memories.extract_model", workflow.SharedPreferences{ExtractModel: standalonePointer("gpt-6.1-sol")}},
		{"memories.consolidation_model", workflow.SharedPreferences{ConsolidationModel: standalonePointer("gpt-6.1-sol")}},
	} {
		t.Run(test.key, func(t *testing.T) {
			view := standaloneView(workflow.Preferences{Version: 1})
			selection := workflow.ConfigurationSelection{Preferences: &workflow.Preferences{Version: 1, Shared: &test.shared}}
			if _, err := workflow.PreferencesForSelection(view, selection); err != nil {
				t.Fatalf("supported field rejected: %v", err)
			}
			delete(view.SharedDefaults, test.key)
			if _, err := workflow.PreferencesForSelection(view, selection); err == nil {
				t.Fatal("field absent from release accepted")
			}
		})
	}
}

func TestStandaloneResolveConfigDirContract(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, explicit, recorded, xdg := filepath.Join(root, "home"), filepath.Join(root, "explicit"), filepath.Join(root, "recorded"), filepath.Join(root, "xdg")
	for _, test := range []struct {
		name, explicit, recorded, xdg, want string
		reject                              bool
	}{
		{"recorded-beats-changed-xdg", "", recorded, xdg, recorded, false},
		{"recorded-ignores-malformed-xdg", "", recorded, "relative", recorded, false},
		{"explicit-matches-recorded", recorded, recorded, xdg, recorded, false},
		{"conflicting-explicit", explicit, recorded, xdg, "", true},
		{"initial-explicit", explicit, "", xdg, explicit, false},
		{"explicit-ignores-malformed-xdg", explicit, "", "relative", explicit, false},
		{"initial-xdg", "", "", xdg, filepath.Join(xdg, "agent-workflows"), false},
		{"initial-home-default", "", "", "", filepath.Join(home, ".config", "agent-workflows"), false},
		{"relative-xdg", "", "", "relative", "", true},
		{"malformed-xdg", "", "", xdg + "\x00bad", "", true},
		{"relative-explicit", "relative", "", xdg, "", true},
		{"malformed-explicit", explicit + "\x00bad", "", xdg, "", true},
		{"relative-recorded", "", "relative", xdg, "", true},
		{"malformed-recorded", "", recorded + "\x00bad", xdg, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key != "XDG_CONFIG_HOME" || test.explicit != "" || test.recorded != "" {
					t.Errorf("forbidden environment lookup: %q", key)
				}
				return test.xdg
			}
			got, err := workflow.ResolveConfigDir(home, test.explicit, test.recorded, getenv)
			if test.reject {
				if err == nil {
					t.Fatal("invalid root accepted")
				}
			} else if err != nil || got != test.want {
				t.Fatalf("root %q, %v; want %q", got, err, test.want)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("resolver created directories: %v, %v", entries, err)
			}
		})
	}
}
