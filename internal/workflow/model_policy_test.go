package workflow

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func editModelPolicy(t *testing.T, path string, edit func(Object)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy Object
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	edit(policy)
	data, err = yaml.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

// Keep this historical input explicit. Maintained fixtures copy the current
// policy; this applies the earlier profile and effort defaults (Ultra Sol and
// Luna execution Max) inside it. It changes only profiles, roles, fallbacks
// for GitHub, effort policy and guidance, so the policy keeps its version-3
// shape, consultation section and adviser fallback (D1).
func historicalModelPolicy(policy Object) {
	profiles := policy["profiles"].(Object)
	delete(profiles, "github")
	delete(policy["fallbacks"].(Object), "github_unavailable")
	for _, role := range policyExecutionRoles {
		policy["roles"].(Object)[role] = "luna_execution"
	}
	profiles["sol"].(Object)["model"] = "gpt-6.1-sol"
	profiles["sol"].(Object)["reasoning_effort"] = "ultra"
	profiles["sol"].(Object)["user_requested_efforts"] = []any{"max"}
	profiles["luna_execution"].(Object)["reasoning_effort"] = "max"
	effort := policy["effort_policy"].(Object)
	effort["default_substantive"] = "ultra"
	effort["max_for_sol"] = "explicit_user_request"
	effort["configured_max_profiles"] = []any{"luna_execution"}
	delete(policy["effort_guidance"].(Object), "xhigh")
}

func configuredModelPolicy(policy Object) {
	profiles := policy["profiles"].(Object)
	profiles["sol"].(Object)["model"] = "gpt-6-astra"
	profiles["sol"].(Object)["reasoning_effort"] = "max"
	profiles["sol"].(Object)["user_requested_efforts"] = []any{"xhigh", "ultra"}
	profiles["luna_execution"].(Object)["reasoning_effort"] = "xhigh"
	effort := policy["effort_policy"].(Object)
	effort["default_substantive"] = "max"
	effort["max_for_sol"] = "configured_or_explicit_user_request"
	effort["configured_max_profiles"] = []any{"sol"}
	policy["effort_guidance"].(Object)["xhigh"] = "Use for bounded execution."
}

func TestGitHubFallbackIsOptionalAndCannotEscalate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		present bool
		value   any
		valid   bool
	}{
		{"historical absent", false, nil, true},
		{"report blocker", true, "report_blocker", true},
		{"expensive profile", true, "sol", false},
		{"empty", true, "", false},
		{"null", true, nil, false},
		{"wrong type", true, []any{"report_blocker"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSuiteFixture(t)
			path := filepath.Join(fixture.root, text(fixture.manifest["model_policy"]))
			editModelPolicy(t, path, func(policy Object) {
				fallbacks := object(policy["fallbacks"])
				delete(fallbacks, "github_unavailable")
				if test.present {
					fallbacks["github_unavailable"] = test.value
				}
			})
			policy, err := ValidateModelPolicy(path)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateModelPolicy() error = %v; valid = %v", err, test.valid)
			}
			if err != nil {
				return
			}
			for _, role := range policyExecutionRoles {
				worker, err := ResolveWorker(policy, role, "")
				if err != nil || worker["model"] != "gpt-6-luna" || worker["reasoning_effort"] != "high" {
					t.Fatalf("fallback changed %s routing: %v, %v", role, worker, err)
				}
			}
		})
	}
}

func TestModelPolicyAcceptsHistoricalAndConfiguredDefaults(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, model, effort string
		edit                func(Object)
	}{
		{"maintained defaults", "gpt-6.1-sol", "max", func(Object) {}},
		{"configured historical Astra Max", "gpt-6-astra", "max", configuredModelPolicy},
		{"configured supported custom profile", "gpt-6-sol", "ultra", func(policy Object) {
			policy["profiles"].(Object)["custom"] = Object{"model": "gpt-6-sol", "reasoning_effort": "ultra", "user_requested_efforts": []any{"max"}, "scope": "substantive"}
			policy["roles"].(Object)["coordinator"] = "custom"
			policy["effort_policy"].(Object)["default_substantive"] = "ultra"
		}},
		{"historical defaults and explicit Max override", "gpt-6.1-sol", "ultra", historicalModelPolicy},
		{"historical previous Sol", "gpt-6-sol", "ultra", func(policy Object) {
			historicalModelPolicy(policy)
			policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6-sol"
		}},
		{"historical lower Sol effort", "gpt-6.1-sol", "high", func(policy Object) {
			historicalModelPolicy(policy)
			policy["profiles"].(Object)["sol"].(Object)["reasoning_effort"] = "high"
			policy["effort_policy"].(Object)["default_substantive"] = "high"
		}},
		{"legacy Sol restriction permits Astra Max", "gpt-6-astra", "max", func(policy Object) {
			historicalModelPolicy(policy)
			profile := policy["profiles"].(Object)["sol"].(Object)
			profile["model"], profile["reasoning_effort"] = "gpt-6-astra", "max"
			effort := policy["effort_policy"].(Object)
			effort["default_substantive"], effort["configured_max_profiles"] = "max", []any{"luna_execution", "sol"}
		}},
		{"configured previous Sol", "gpt-6-sol", "max", func(policy Object) {
			policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6-sol"
		}},
		{"configured without optional xhigh guidance", "gpt-6.1-sol", "max", func(policy Object) {
			delete(policy["effort_guidance"].(Object), "xhigh")
		}},
		{"supported Luna effort overrides", "gpt-6.1-sol", "max", func(policy Object) {
			policy["profiles"].(Object)["luna_execution"].(Object)["user_requested_efforts"] = []any{"high", "xhigh", "max"}
		}},
		{"configured Ultra override", "gpt-6.1-sol", "ultra", func(policy Object) {
			policy["profiles"].(Object)["sol"].(Object)["reasoning_effort"] = "ultra"
			policy["profiles"].(Object)["sol"].(Object)["user_requested_efforts"] = []any{"max", "xhigh"}
			policy["effort_policy"].(Object)["default_substantive"] = "ultra"
			policy["effort_policy"].(Object)["configured_max_profiles"] = []any{}
		}},
		{"unordered exact configured Max profiles", "gpt-6.1-sol", "max", func(policy Object) {
			policy["profiles"].(Object)["luna_execution"].(Object)["reasoning_effort"] = "max"
			policy["effort_policy"].(Object)["configured_max_profiles"] = []any{"sol", "luna_execution"}
		}},
		{"editable coordinator profile", "gpt-6-astra", "high", func(policy Object) {
			policy["profiles"].(Object)["expert"] = Object{"model": "gpt-6-astra", "reasoning_effort": "high", "user_requested_efforts": []any{"max", "ultra"}, "scope": "substantive"}
			policy["roles"].(Object)["coordinator"] = "expert"
			policy["effort_policy"].(Object)["default_substantive"] = "high"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSuiteFixture(t)
			editModelPolicy(t, filepath.Join(fixture.root, text(fixture.manifest["model_policy"])), test.edit)
			before := suiteSnapshot(t, fixture.root)
			if _, err := ValidateSuite(fixture.root); err != nil {
				t.Fatal(err)
			}
			defaults, err := ModelDefaults(fixture.root, fixture.manifest)
			want := map[string]string{"model": test.model, "model_reasoning_effort": test.effort}
			if err != nil || !reflect.DeepEqual(defaults, want) {
				t.Fatalf("defaults = %#v, %v; want %#v", defaults, err, want)
			}
			if !reflect.DeepEqual(suiteSnapshot(t, fixture.root), before) {
				t.Fatal("policy validation changed the source")
			}
		})
	}
}

func TestModelPolicyRejectsInvalidConfiguredDefaultsAndCapabilities(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, message string
		edit          func(Object)
	}{
		{"legacy previous Sol Max", "Sol max requires", func(policy Object) {
			policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6-sol"
			policy["effort_policy"].(Object)["max_for_sol"] = "explicit_user_request"
		}},
		{"legacy extra Sol Max profile", "Sol max requires", func(policy Object) {
			historicalModelPolicy(policy)
			policy["profiles"].(Object)["extra"] = Object{"model": "gpt-6.1-sol", "reasoning_effort": "max", "user_requested_efforts": []any{}, "scope": "substantive"}
			policy["effort_policy"].(Object)["configured_max_profiles"] = []any{"extra", "luna_execution"}
		}},
		{"unknown Sol Max rule", "invalid Sol max rule", func(policy Object) { policy["effort_policy"].(Object)["max_for_sol"] = "always" }},
		{"boolean Sol Max rule", "invalid Sol max rule", func(policy Object) { policy["effort_policy"].(Object)["max_for_sol"] = true }},
		{"unknown configured profile", "invalid configured max profiles", func(policy Object) { policy["effort_policy"].(Object)["configured_max_profiles"] = []any{"missing"} }},
		{"duplicate configured profile", "invalid configured max profiles", func(policy Object) { policy["effort_policy"].(Object)["configured_max_profiles"] = []any{"sol", "sol"} }},
		{"extra non-Max configured profile", "invalid configured max profiles", func(policy Object) {
			policy["effort_policy"].(Object)["configured_max_profiles"] = []any{"sol", "luna_execution"}
		}},
		{"omitted actual Max profile", "invalid configured max profiles", func(policy Object) { policy["effort_policy"].(Object)["configured_max_profiles"] = []any{} }},
		{"stale configured profile after effort override", "invalid configured max profiles", func(policy Object) { policy["profiles"].(Object)["sol"].(Object)["reasoning_effort"] = "xhigh" }},
		{"default does not match coordinator", "substantive default must match", func(policy Object) { policy["effort_policy"].(Object)["default_substantive"] = "ultra" }},
		{"unknown model", "unknown worker model", func(policy Object) { policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6-unknown" }},
		{"unsupported Sol default", "unsupported default reasoning effort", func(policy Object) { policy["profiles"].(Object)["sol"].(Object)["reasoning_effort"] = "extreme" }},
		{"unsupported Sol requested override", "unsupported or duplicate user-requested efforts", func(policy Object) {
			policy["profiles"].(Object)["sol"].(Object)["user_requested_efforts"] = []any{"extreme"}
		}},
		{"unsupported Luna requested Ultra", "unsupported or duplicate user-requested efforts", func(policy Object) {
			policy["profiles"].(Object)["luna_execution"].(Object)["user_requested_efforts"] = []any{"ultra"}
		}},
		{"empty optional guidance", "effort guidance must be nonempty text", func(policy Object) { policy["effort_guidance"].(Object)["xhigh"] = " " }},
		{"unsupported optional guidance", "effort_guidance has invalid fields", func(policy Object) { policy["effort_guidance"].(Object)["high"] = "Additional guidance." }},
		{"missing required legacy guidance", "effort_guidance has invalid fields", func(policy Object) { delete(policy["effort_guidance"].(Object), "ultra") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSuiteFixture(t)
			editModelPolicy(t, filepath.Join(fixture.root, text(fixture.manifest["model_policy"])), test.edit)
			fixture.invalid(t, test.message)
			if _, err := ModelDefaults(fixture.root, fixture.manifest); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("ModelDefaults error = %v; want %q", err, test.message)
			}
		})
	}
}

func policyProfiles(solModel, solEffort, executionEffort string) Object {
	return Object{
		"sol":            Object{"model": solModel, "reasoning_effort": solEffort, "scope": "substantive"},
		"luna_execution": Object{"model": "gpt-6-luna", "reasoning_effort": executionEffort, "scope": "bounded_execution"},
		"luna":           Object{"model": "gpt-6-luna", "reasoning_effort": "high", "scope": "bounded_evidence"},
	}
}

func assertInstalledModelPolicy(t *testing.T, installer *Installer, revision, solModel, solEffort, executionEffort string) {
	t.Helper()
	before := installerSnapshot(t, installer.State)
	report, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	// Standalone release names are effective snapshots; the state records
	// which bundle revision the active one was built from.
	state, err := readJSON(installer.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if report["installed"] != true || report["release"] == nil || report["release"] != state["release"] || stateRevision(state) != revision || !reflect.DeepEqual(report["model_profiles"], policyProfiles(solModel, solEffort, executionEffort)) {
		t.Fatalf("unexpected installed snapshot: %#v", report)
	}
	want := map[string]string{"model": solModel, "model_reasoning_effort": solEffort}
	if !reflect.DeepEqual(report["model_defaults"], want) || object(report["model_config"])["managed"] != true {
		t.Fatalf("unexpected model default ownership: %#v", report)
	}
	var config Object
	if err := toml.Unmarshal([]byte(installerRead(t, installer.config)), &config); err != nil {
		t.Fatal(err)
	}
	for key, value := range want {
		if config[key] != value {
			t.Fatalf("managed TOML %s = %v; want %s", key, config[key], value)
		}
	}
	data, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(data), "\"model_profiles\"") {
		t.Fatalf("status JSON omitted profiles: %s, %v", data, err)
	}
	installerUnchanged(t, installer.State, before)
}

// commitModelPolicy packs the fixture source with an edited model policy as
// the next release and returns its revision.
func commitModelPolicy(t *testing.T, fixture *installerFixture, edit func(Object), _ string) string {
	t.Helper()
	editModelPolicy(t, filepath.Join(fixture.source, text(fixture.manifest["model_policy"])), edit)
	return fixture.commit(t)
}

func nativePolicyInstaller(t *testing.T, fixture *installerFixture, revision, protocol string) *Installer {
	t.Helper()
	return nativeFixtureInstallerProtocol(t, fixture, revision, protocol, true)
}

func assertManagerProtocol(t *testing.T, installer *Installer, protocol string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), installer.commandPath(), "--manager-protocol").Output()
	if err != nil || string(output) != protocol+"\n" {
		t.Fatalf("enrolled manager protocol = %q, %v; want %s", output, err, protocol)
	}
}

func TestNativeModelPolicyUltraMaxUltraMaxActivationAndSelectiveUninstall(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	original := fixture.seedUserFiles(t)
	legacy := commitModelPolicy(t, fixture, historicalModelPolicy, "Historical Ultra policy")
	installer := nativePolicyInstaller(t, fixture, legacy, ManagerProtocol)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	assertInstalledModelPolicy(t, installer, legacy, "gpt-6.1-sol", "ultra", "max")
	initial, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	initialModelOrigin := object(hostRecord(initial, hostCodex)["model_config"])["original"]
	if initialModelOrigin == nil {
		t.Fatalf("setup recorded no original model ownership: %#v", hostRecord(initial, hostCodex)["model_config"])
	}
	maxRevision := commitModelPolicy(t, fixture, configuredModelPolicy, "Configured Max policy")
	installer = nativePolicyInstaller(t, fixture, maxRevision, ManagerProtocol)
	installerAppend(t, installer.config, "# independent model comment\n")
	if err := os.Chmod(installer.config, 0o604); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Update(); err != nil {
		t.Fatal(err)
	}
	assertInstalledModelPolicy(t, installer, maxRevision, "gpt-6-astra", "max", "xhigh")
	assertManagerProtocol(t, installer, ManagerProtocol)
	activated, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(object(hostRecord(activated, hostCodex)["model_config"])["original"], initialModelOrigin) {
		t.Fatal("model activation replaced the original TOML ownership")
	}
	// The installed snapshot, not the pack source, carries the policy.
	if err := os.Rename(fixture.source, fixture.source+" unavailable"); err != nil {
		t.Fatal(err)
	}
	assertInstalledModelPolicy(t, installer, maxRevision, "gpt-6-astra", "max", "xhigh")
	if _, err := installer.Rollback(); err != nil {
		t.Fatalf("policy rollback: %v", err)
	}
	assertInstalledModelPolicy(t, installer, legacy, "gpt-6.1-sol", "ultra", "max")
	assertManagerProtocol(t, installer, ManagerProtocol)
	rolledBack, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(rolledBack["manager"], activated["manager"]) || !equal(object(hostRecord(rolledBack, hostCodex)["model_config"])["original"], initialModelOrigin) {
		t.Fatal("skill rollback changed the manager or original model ownership")
	}
	if _, err := installer.Update(); err != nil {
		t.Fatal(err)
	}
	assertInstalledModelPolicy(t, installer, maxRevision, "gpt-6-astra", "max", "xhigh")
	assertManagerProtocol(t, installer, ManagerProtocol)
	if _, err := installer.Uninstall(); err != nil {
		t.Fatal(err)
	}
	for path, value := range original {
		mode := os.FileMode(0o640)
		if Normalize(path) == installer.config {
			value += "# independent model comment\n"
			mode = 0o604
		}
		if got := installerRead(t, path); got != value {
			t.Fatalf("uninstall changed original bytes in %s: %q", path, got)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("uninstall changed user mode in %s: %v", path, err)
		}
	}
}

func TestInstallerStatusProjectsAdditionalProfileFromImmutableSnapshot(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	revision := commitModelPolicy(t, fixture, func(policy Object) {
		policy["profiles"].(Object)["expert"] = Object{"model": "gpt-6-astra", "reasoning_effort": "ultra", "user_requested_efforts": []any{"max"}, "scope": "substantive"}
	}, "Additional expert profile")
	installer := nativePolicyInstaller(t, fixture, revision, ManagerProtocol)
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.source); err != nil {
		t.Fatal(err)
	}
	report, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	want := policyProfiles("gpt-6.1-sol", "max", "xhigh")
	want["github"] = Object{"model": "gpt-6-luna", "reasoning_effort": "high", "scope": "bounded_execution"}
	want["expert"] = Object{"model": "gpt-6-astra", "reasoning_effort": "ultra", "scope": "substantive"}
	if !reflect.DeepEqual(report["model_profiles"], want) {
		t.Fatalf("status projected profiles = %#v; want %#v", report["model_profiles"], want)
	}
}
