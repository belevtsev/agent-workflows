package workflow

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Contract adviser-switch, revision 3: A1-A4 and B1-B4.

// adviserAll is every known consultation provider ID in provider order.
var adviserAll = []string{"jev", "subq_snap", "openai_decisions"}

func adviserDisabled(p Preferences) []string {
	if p.Advisers == nil {
		return nil
	}
	return p.Advisers.Disabled
}

// adviserView is a two-host configuration view whose saved preferences are
// saved. It mirrors the approved standaloneView scaffolding, which lives in the
// external test package and so cannot be called from here.
func adviserView(saved Preferences) ConfigurationView {
	defaults := Object{"model_context_window": int64(200000), "features.memories": true, "memories.generate_memories": true, "memories.use_memories": true, "memories.extract_model": "gpt-6.1-sol", "memories.consolidation_model": "gpt-6.1-sol"}
	view := ConfigurationView{Preferences: saved, SharedDefaults: defaults, SharedEffective: make(Object)}
	for key, value := range defaults {
		view.SharedEffective[key] = value
	}
	for _, hostID := range []string{"codex", "claude"} {
		model, effort, efforts := "gpt-6.1-sol", "max", []string{"high", "xhigh", "max"}
		if hostID == "claude" {
			model, effort, efforts = "claude-opus-5-5", "xhigh", []string{"high", "xhigh"}
		}
		makeRole := func(name, scope string, override ModelOverride) RoleConfiguration {
			defaultSelection := ModelSelection{Model: model, Effort: effort}
			effective := defaultSelection
			if override.Model != nil {
				effective.Model = *override.Model
			}
			if override.Effort != nil {
				effective.Effort = *override.Effort
			}
			return RoleConfiguration{Role: name, Scope: scope, Default: defaultSelection, Effective: effective, Override: override, Choices: []ModelChoice{{Model: model, Efforts: append([]string(nil), efforts...)}}}
		}
		host := HostConfiguration{ID: hostID, Detected: true, Selected: true}
		for _, role := range []struct{ name, scope string }{{"coordinator", "substantive"}, {"testing", "substantive"}, {"commit_execution", "bounded_execution"}, {"lookup", "bounded_evidence"}} {
			host.Roles = append(host.Roles, makeRole(role.name, role.scope, saved.Hosts[hostID].Roles[role.name]))
		}
		fallback := ModelOverride{}
		if saved.Hosts[hostID].Fallback != nil {
			fallback = *saved.Hosts[hostID].Fallback
		}
		host.Fallback = makeRole("fallback", "substantive", fallback)
		view.Hosts = append(view.Hosts, host)
	}
	return view
}

// adviserSavedPreferences pins one Codex effort and disables OpenAI, so a
// preset visibly replaces the model pins while the adviser choice must stay.
func adviserSavedPreferences() Preferences {
	high := "high"
	return Preferences{
		Version:  1,
		Hosts:    map[string]HostPreferences{"codex": {Roles: map[string]ModelOverride{"coordinator": {Effort: &high}}}},
		Advisers: &AdviserPreferences{Disabled: []string{"openai_decisions"}},
	}
}

func adviserSnapshot(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatalf("snapshot %T: %v", value, err)
	}
	return string(data)
}

// A1: the JSON members are "advisers" and "disabled", both omitted when empty.
func TestAdviserPreferencesJSONMembers(t *testing.T) {
	data, err := json.Marshal(Preferences{Version: 1, Advisers: &AdviserPreferences{Disabled: []string{"subq_snap", "openai_decisions"}}})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"disabled": []any{"subq_snap", "openai_decisions"}}
	if !reflect.DeepEqual(document["advisers"], want) {
		t.Fatalf("advisers member of %s = %#v, want %#v", data, document["advisers"], want)
	}

	decoded, err := DecodePreferences(data)
	if err != nil {
		t.Fatalf("DecodePreferences(%s): %v", data, err)
	}
	if got := adviserDisabled(decoded); !slices.Equal(got, []string{"subq_snap", "openai_decisions"}) {
		t.Fatalf("round trip of %s disabled = %q", data, got)
	}

	data, err = json.Marshal(Preferences{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	document = nil
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if member, present := document["advisers"]; present {
		t.Fatalf("Preferences without advisers encodes %s with advisers %#v; want the member omitted", data, member)
	}

	data, err = json.Marshal(AdviserPreferences{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Fatalf("empty AdviserPreferences encodes %s, want {} (disabled omitted)", data)
	}
}

// A2: decode refuses unknown IDs, duplicates and unknown members, and
// normalises the list to provider order, or to nil when empty or absent.
func TestAdviserDecodePreferences(t *testing.T) {
	valid := []struct {
		name, document string
		want           []string
	}{
		{"absent", `{"version":1}`, nil},
		{"empty object", `{"version":1,"advisers":{}}`, nil},
		{"empty list", `{"version":1,"advisers":{"disabled":[]}}`, nil},
		{"one", `{"version":1,"advisers":{"disabled":["openai_decisions"]}}`, []string{"openai_decisions"}},
		{"jev alone", `{"version":1,"advisers":{"disabled":["jev"]}}`, []string{"jev"}},
		{"reversed pair", `{"version":1,"advisers":{"disabled":["openai_decisions","subq_snap"]}}`, []string{"subq_snap", "openai_decisions"}},
		{"shuffled all", `{"version":1,"advisers":{"disabled":["openai_decisions","jev","subq_snap"]}}`, []string{"jev", "subq_snap", "openai_decisions"}},
		{"reversed all", `{"version":1,"advisers":{"disabled":["openai_decisions","subq_snap","jev"]}}`, []string{"jev", "subq_snap", "openai_decisions"}},
		{"jev last", `{"version":1,"advisers":{"disabled":["subq_snap","jev"]}}`, []string{"jev", "subq_snap"}},
	}
	for _, c := range valid {
		t.Run("valid/"+c.name, func(t *testing.T) {
			got, err := DecodePreferences([]byte(c.document))
			if err != nil {
				t.Fatalf("DecodePreferences(%s): %v", c.document, err)
			}
			if c.want == nil {
				if got.Advisers != nil {
					t.Fatalf("DecodePreferences(%s).Advisers = %+v, want nil", c.document, *got.Advisers)
				}
				return
			}
			if got.Advisers == nil || !slices.Equal(got.Advisers.Disabled, c.want) {
				t.Fatalf("DecodePreferences(%s) disabled = %q, want %q", c.document, adviserDisabled(got), c.want)
			}
		})
	}

	invalid := []struct{ name, document string }{
		{"unknown id", `{"version":1,"advisers":{"disabled":["gemini_review"]}}`},
		{"unknown id beside known", `{"version":1,"advisers":{"disabled":["subq_snap","gemini_review"]}}`},
		{"empty id", `{"version":1,"advisers":{"disabled":[""]}}`},
		{"credential name", `{"version":1,"advisers":{"disabled":["OPENAI_API_KEY"]}}`},
		{"wrong case", `{"version":1,"advisers":{"disabled":["JEV"]}}`},
		{"duplicate", `{"version":1,"advisers":{"disabled":["jev","jev"]}}`},
		{"duplicate apart", `{"version":1,"advisers":{"disabled":["subq_snap","jev","subq_snap"]}}`},
		{"unknown member", `{"version":1,"advisers":{"enabled":["jev"]}}`},
		{"unknown member beside disabled", `{"version":1,"advisers":{"disabled":["jev"],"note":"off"}}`},
		{"disabled not a list", `{"version":1,"advisers":{"disabled":"jev"}}`},
	}
	for _, c := range invalid {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			got, err := DecodePreferences([]byte(c.document))
			if err == nil {
				t.Fatalf("DecodePreferences(%s) = %+v (disabled %q), want an error", c.document, got, adviserDisabled(got))
			}
		})
	}
}

// A3: saved advisers survive a nil selection, including named presets; a
// non-nil selection replaces them; an explicit settings document is whole.
func TestAdviserPreferencesForSelection(t *testing.T) {
	cases := []struct {
		name      string
		selection func() ConfigurationSelection
		want      []string
		resets    bool // the named preset clears every model pin
	}{
		{"keep without preset", func() ConfigurationSelection { return ConfigurationSelection{} }, []string{"openai_decisions"}, false},
		{"keep through recommended", func() ConfigurationSelection { return ConfigurationSelection{Preset: PresetRecommended} }, []string{"openai_decisions"}, true},
		{"keep through balanced", func() ConfigurationSelection { return ConfigurationSelection{Preset: PresetBalanced} }, []string{"openai_decisions"}, false},
		{"replace", func() ConfigurationSelection {
			return ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"subq_snap"}}}
		}, []string{"subq_snap"}, false},
		{"replace with recommended", func() ConfigurationSelection {
			return ConfigurationSelection{Preset: PresetRecommended, Advisers: &AdviserPreferences{Disabled: []string{"jev", "subq_snap"}}}
		}, []string{"jev", "subq_snap"}, true},
		{"replace with every provider", func() ConfigurationSelection {
			return ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"jev", "subq_snap", "openai_decisions"}}}
		}, []string{"jev", "subq_snap", "openai_decisions"}, false},
		{"empty nil list enables all", func() ConfigurationSelection {
			return ConfigurationSelection{Advisers: &AdviserPreferences{}}
		}, nil, false},
		{"empty list enables all", func() ConfigurationSelection {
			return ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{}}}
		}, nil, false},
		{"empty list with balanced", func() ConfigurationSelection {
			return ConfigurationSelection{Preset: PresetBalanced, Advisers: &AdviserPreferences{Disabled: []string{}}}
		}, nil, false},
		{"settings without advisers enables all", func() ConfigurationSelection {
			return ConfigurationSelection{Preferences: &Preferences{Version: 1}}
		}, nil, false},
		{"settings with advisers", func() ConfigurationSelection {
			return ConfigurationSelection{Preferences: &Preferences{Version: 1, Advisers: &AdviserPreferences{Disabled: []string{"subq_snap"}}}}
		}, []string{"subq_snap"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := adviserView(adviserSavedPreferences())
			selection := c.selection()
			viewBefore, selectionBefore := adviserSnapshot(t, view), adviserSnapshot(t, selection)

			got, err := PreferencesForSelection(view, selection)
			if err != nil {
				t.Fatalf("PreferencesForSelection(%s): %v", selectionBefore, err)
			}
			if c.want == nil {
				if got.Advisers != nil {
					t.Fatalf("Advisers = %+v, want nil", *got.Advisers)
				}
			} else if got.Advisers == nil || !slices.Equal(got.Advisers.Disabled, c.want) {
				t.Fatalf("disabled = %q, want %q", adviserDisabled(got), c.want)
			}
			if c.resets && (len(got.Hosts) != 0 || got.Shared != nil) {
				t.Fatalf("preset kept model preferences: hosts %+v shared %+v", got.Hosts, got.Shared)
			}

			// The compiler is pure: no input changes, and the result does not
			// share the adviser list with the view or the selection.
			if got.Advisers != nil && len(got.Advisers.Disabled) > 0 {
				got.Advisers.Disabled[0] = "mutated"
			}
			if after := adviserSnapshot(t, view); after != viewBefore {
				t.Fatalf("view changed:\nbefore %s\nafter  %s", viewBefore, after)
			}
			if after := adviserSnapshot(t, selection); after != selectionBefore {
				t.Fatalf("selection changed:\nbefore %s\nafter  %s", selectionBefore, after)
			}
		})
	}
}

// A3 (both set) and A4 (unknown IDs, named in the error).
func TestAdviserPreferencesForSelectionRefusals(t *testing.T) {
	cases := []struct {
		name      string
		selection ConfigurationSelection
		names     string // the error must name this ID, when set
	}{
		{"settings and advisers", ConfigurationSelection{
			Preferences: &Preferences{Version: 1},
			Advisers:    &AdviserPreferences{Disabled: []string{"subq_snap"}},
		}, ""},
		{"settings and empty advisers", ConfigurationSelection{
			Preferences: &Preferences{Version: 1},
			Advisers:    &AdviserPreferences{},
		}, ""},
		{"unknown adviser", ConfigurationSelection{
			Advisers: &AdviserPreferences{Disabled: []string{"bogus_adviser"}},
		}, "bogus_adviser"},
		{"unknown beside known with preset", ConfigurationSelection{
			Preset:   PresetRecommended,
			Advisers: &AdviserPreferences{Disabled: []string{"subq_snap", "mystery_panel"}},
		}, "mystery_panel"},
		{"unknown in settings", ConfigurationSelection{
			Preferences: &Preferences{Version: 1, Advisers: &AdviserPreferences{Disabled: []string{"rogue_oracle"}}},
		}, "rogue_oracle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := adviserView(adviserSavedPreferences())
			viewBefore, selectionBefore := adviserSnapshot(t, view), adviserSnapshot(t, c.selection)
			got, err := PreferencesForSelection(view, c.selection)
			if err == nil {
				t.Fatalf("PreferencesForSelection(%s) = %+v (disabled %q), want an error", selectionBefore, got, adviserDisabled(got))
			}
			if c.names != "" && !strings.Contains(err.Error(), c.names) {
				t.Fatalf("error %q does not name %q", err, c.names)
			}
			if after := adviserSnapshot(t, view); after != viewBefore {
				t.Fatalf("refusal changed the view:\nbefore %s\nafter  %s", viewBefore, after)
			}
			if after := adviserSnapshot(t, c.selection); after != selectionBefore {
				t.Fatalf("refusal changed the selection:\nbefore %s\nafter  %s", selectionBefore, after)
			}
		})
	}
	if known := ConsultationProviders(); !slices.Equal(known, adviserAll) {
		t.Fatalf("ConsultationProviders() = %q, want %q", known, adviserAll)
	}
}

// ---- B: the effective policy built by the installer ----

type adviserApplied struct {
	release, preferencesSHA, policy string
}

func adviserShippedPolicy(f *installerFixture) string {
	return filepath.Join(f.source, filepath.FromSlash(text(f.manifest["model_policy"])))
}

// adviserApply runs setup (enroll) or install on the fixture with selection and
// returns the effective release, its preferences hash and its policy path.
func adviserApply(t *testing.T, f *installerFixture, revision string, selection ConfigurationSelection, enroll bool) adviserApplied {
	t.Helper()
	installer := nativeFixtureInstaller(t, f, revision, true)
	installer.Selection = &selection
	report, err := installer.Setup(enroll)
	if err != nil {
		t.Fatalf("apply selection %s: %v", adviserSnapshot(t, selection), err)
	}
	state := f.readState(t)
	release := text(state["release"])
	if release == "" || text(report["release"]) != release {
		t.Fatalf("report release %q, state release %q", text(report["release"]), release)
	}
	root := filepath.Join(f.paths.State, "releases", release)
	return adviserApplied{
		release:        release,
		preferencesSHA: text(state["preferences_sha256"]),
		policy:         filepath.Join(root, filepath.FromSlash(text(f.manifest["model_policy"]))),
	}
}

// adviserProviders validates the policy and returns consultation.providers,
// which must be present as a mapping (possibly empty).
func adviserProviders(t *testing.T, policyPath string) Object {
	t.Helper()
	policy, err := ValidateModelPolicy(policyPath)
	if err != nil {
		t.Fatalf("ValidateModelPolicy(%s): %v", policyPath, err)
	}
	consultation := object(policy["consultation"])
	if consultation == nil {
		t.Fatalf("%s has no consultation mapping", policyPath)
	}
	raw, present := consultation["providers"]
	if !present {
		t.Fatalf("%s has no consultation.providers", policyPath)
	}
	providers := object(raw)
	if providers == nil {
		t.Fatalf("%s consultation.providers is %T (%v), want a mapping", policyPath, raw, raw)
	}
	return providers
}

func adviserWithout(shipped Object, disabled ...string) Object {
	want := make(Object, len(shipped))
	for id, model := range shipped {
		if !slices.Contains(disabled, id) {
			want[id] = model
		}
	}
	return want
}

func adviserRequireProviders(t *testing.T, policyPath string, want Object) {
	t.Helper()
	if got := adviserProviders(t, policyPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("effective consultation.providers = %v, want %v", got, want)
	}
}

// adviserShipped reads the fixture's shipped providers and requires the two
// that the contract says the release ships, so removals are observable.
func adviserShipped(t *testing.T, f *installerFixture) Object {
	t.Helper()
	shipped := adviserProviders(t, adviserShippedPolicy(f))
	for _, id := range []string{"subq_snap", "openai_decisions"} {
		if text(shipped[id]) == "" {
			t.Fatalf("precondition: shipped providers %v lack %s", shipped, id)
		}
	}
	return shipped
}

// B1 and B4 (in package): disabled providers leave the effective policy,
// other pins are unchanged, whichever hosts are selected.
func TestAdviserEffectivePolicyOmitsDisabledProviders(t *testing.T) {
	t.Run("both hosts through selection advisers", func(t *testing.T) {
		f := newInstallerFixture(t)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"openai_decisions"}}}, true)
		adviserRequireProviders(t, got.policy, adviserWithout(shipped, "openai_decisions"))

		data, err := os.ReadFile(filepath.Join(f.config, "preferences.json"))
		if err != nil {
			t.Fatal(err)
		}
		saved, err := DecodePreferences(data)
		if err != nil {
			t.Fatalf("saved preferences %s: %v", data, err)
		}
		if disabled := adviserDisabled(saved); !slices.Equal(disabled, []string{"openai_decisions"}) {
			t.Fatalf("saved preferences %s disable %q, want [openai_decisions]", data, disabled)
		}
	})

	t.Run("claude only through a settings document", func(t *testing.T) {
		f := claudeFixture(t)
		seedClaudeFiles(t, f)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{
			Hosts:       []string{"claude"},
			Preferences: &Preferences{Version: 1, Advisers: &AdviserPreferences{Disabled: []string{"subq_snap"}}},
		}, true)
		adviserRequireProviders(t, got.policy, adviserWithout(shipped, "subq_snap"))
	})

	t.Run("claude only through selection advisers", func(t *testing.T) {
		f := claudeFixture(t)
		seedClaudeFiles(t, f)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{
			Hosts:    []string{"claude"},
			Advisers: &AdviserPreferences{Disabled: []string{"openai_decisions"}},
		}, true)
		adviserRequireProviders(t, got.policy, adviserWithout(shipped, "openai_decisions"))
	})

	t.Run("every provider disabled leaves an empty mapping", func(t *testing.T) {
		f := newInstallerFixture(t)
		adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: slices.Clone(adviserAll)}}, true)
		if providers := adviserProviders(t, got.policy); len(providers) != 0 {
			t.Fatalf("effective consultation.providers = %v, want an empty mapping", providers)
		}
	})

	t.Run("an unshipped provider disabled keeps the shipped ones", func(t *testing.T) {
		f := newInstallerFixture(t)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"jev"}}}, true)
		adviserRequireProviders(t, got.policy, shipped)
	})

	t.Run("a release shipping jev keeps its pin", func(t *testing.T) {
		f := newInstallerFixture(t)
		revision := commitModelPolicy(t, f, func(policy Object) {
			object(policy["consultation"])["providers"] = Object{"jev": "jev-1.14.0", "openai_decisions": "gpt-6-luna"}
		}, "")
		got := adviserApply(t, f, revision, ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"openai_decisions"}}}, true)
		adviserRequireProviders(t, got.policy, Object{"jev": "jev-1.14.0"})
	})
}

// B2: with nothing disabled the providers equal the shipped ones, and a
// Claude-only install leaves the policy file as shipped.
func TestAdviserEffectivePolicyKeepsShippedProvidersWhenNoneDisabled(t *testing.T) {
	t.Run("claude only keeps the shipped file", func(t *testing.T) {
		f := claudeFixture(t)
		seedClaudeFiles(t, f)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{Hosts: []string{"claude"}, Advisers: &AdviserPreferences{Disabled: []string{}}}, true)
		adviserRequireProviders(t, got.policy, shipped)
		want, err := os.ReadFile(adviserShippedPolicy(f))
		if err != nil {
			t.Fatal(err)
		}
		have, err := os.ReadFile(got.policy)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, want) {
			t.Fatalf("Claude-only effective policy was rewritten with no adviser disabled")
		}
	})

	t.Run("both hosts with recommended", func(t *testing.T) {
		f := newInstallerFixture(t)
		shipped := adviserShipped(t, f)
		got := adviserApply(t, f, f.first, ConfigurationSelection{Preset: PresetRecommended}, true)
		adviserRequireProviders(t, got.policy, shipped)
	})
}

// B3: an adviser change alone makes a new effective release; unchanged
// preferences reuse it.
func TestAdviserSelectionChangeAloneBuildsNewRelease(t *testing.T) {
	f := newInstallerFixture(t)
	shipped := adviserShipped(t, f)
	base := adviserApply(t, f, f.first, ConfigurationSelection{Preset: PresetRecommended}, true)
	if base.preferencesSHA == "" {
		t.Fatalf("state.json records no preferences_sha256")
	}
	adviserRequireProviders(t, base.policy, shipped)

	off := ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: []string{"openai_decisions"}}}
	changed := adviserApply(t, f, f.first, off, false)
	if changed.release == base.release {
		t.Fatalf("disabling OpenAI reused release %s", base.release)
	}
	if changed.preferencesSHA == base.preferencesSHA {
		t.Fatalf("disabling OpenAI kept preferences hash %s", base.preferencesSHA)
	}
	adviserRequireProviders(t, changed.policy, adviserWithout(shipped, "openai_decisions"))

	again := adviserApply(t, f, f.first, off, false)
	if again.release != changed.release || again.preferencesSHA != changed.preferencesSHA {
		t.Fatalf("re-applying unchanged preferences: release %s hash %s, want %s and %s",
			again.release, again.preferencesSHA, changed.release, changed.preferencesSHA)
	}

	// A named preset with no adviser selection keeps the saved choice (A3),
	// so the preferences and the release stay the same.
	kept := adviserApply(t, f, f.first, ConfigurationSelection{Preset: PresetRecommended}, false)
	if kept.release != changed.release || kept.preferencesSHA != changed.preferencesSHA {
		t.Fatalf("recommended without an adviser selection: release %s hash %s, want %s and %s",
			kept.release, kept.preferencesSHA, changed.release, changed.preferencesSHA)
	}

	back := adviserApply(t, f, f.first, ConfigurationSelection{Advisers: &AdviserPreferences{}}, false)
	if back.release == changed.release || back.preferencesSHA == changed.preferencesSHA {
		t.Fatalf("re-enabling OpenAI reused release %s or hash %s", changed.release, changed.preferencesSHA)
	}
	adviserRequireProviders(t, back.policy, shipped)
}

// AdviserInstallForConsult installs the first fixture release with the given
// providers disabled and returns the effective and shipped model-policy paths.
// It is the bridge adviser_policy_ext_test.go uses to reach
// consult.ConfiguredAdvisers (B4) without an import cycle.
func AdviserInstallForConsult(t *testing.T, disabled ...string) (effective, shipped string) {
	t.Helper()
	f := newInstallerFixture(t)
	adviserShipped(t, f)
	applied := adviserApply(t, f, f.first, ConfigurationSelection{Advisers: &AdviserPreferences{Disabled: slices.Clone(disabled)}}, true)
	return applied.policy, adviserShippedPolicy(f)
}
