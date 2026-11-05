package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestMaintainedCodexSharedDefaults(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := SharedConfigDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := solDefaultsSharedDefaults("gpt-6.1-sol"); !reflect.DeepEqual(defaults, want) {
		t.Fatalf("maintained shared defaults = %#v; want exactly %#v", defaults, want)
	}
	resource, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sharedInstallerResourcePath)))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^[\t ]*[^#=\r\n]*effort[^#=\r\n]*=`).Match(resource) {
		t.Fatal("maintained shared resource adds an effort field outside its six defaults")
	}
}

func TestMaintainedCodexLegacySolExplicitMax(t *testing.T) {
	t.Parallel()
	fixture := newSuiteFixture(t)
	path := filepath.Join(fixture.root, text(fixture.manifest["model_policy"]))
	editModelPolicy(t, path, historicalModelPolicy)
	policy, err := ValidateModelPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ requested, want string }{{"", "ultra"}, {"max", "max"}} {
		worker, err := ResolveWorker(policy, "coding", test.requested)
		if err != nil || worker["model"] != "gpt-6.1-sol" || worker["reasoning_effort"] != test.want {
			t.Fatalf("legacy coding at %q = %#v, %v", test.requested, worker, err)
		}
	}
	editModelPolicy(t, path, func(policy Object) {
		policy["profiles"].(Object)["sol"].(Object)["reasoning_effort"] = "max"
		effort := policy["effort_policy"].(Object)
		effort["default_substantive"] = "max"
		effort["configured_max_profiles"] = []any{"luna_execution", "sol"}
	})
	if _, err := ValidateModelPolicy(path); err == nil {
		t.Fatal("legacy explicit-only Sol Max restriction accepted a configured Max default")
	}
}

func TestMaintainedCodexRejectsUnknownModel(t *testing.T) {
	t.Parallel()
	fixture := newSuiteFixture(t)
	path := filepath.Join(fixture.root, text(fixture.manifest["model_policy"]))
	editModelPolicy(t, path, func(policy Object) {
		policy["profiles"].(Object)["sol"].(Object)["model"] = "custom-model"
	})
	if _, err := ValidateModelPolicy(path); err == nil {
		t.Fatal("unsupported custom-model was accepted as a supported model")
	}
}

func TestSolDefaultsSetup(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fresh", "personal"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := sharedInstallerFixture(t)
			if name == "personal" {
				fixture.seedUserFiles(t)
			}
			dryRun := sharedLifecycleInstaller(t, fixture, false)
			before := installerSnapshot(t, fixture.base)
			if _, err := dryRun.Setup(true); err != nil {
				t.Fatal(err)
			}
			installerUnchanged(t, fixture.base, before)
			installer := sharedLifecycleInstaller(t, fixture, true)
			if _, err := installer.Setup(true); err != nil {
				t.Fatal(err)
			}
			config := solDefaultsAssertRoot(t, fixture, "gpt-6.1-sol", "max")
			sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
			report, err := installer.Status()
			if err != nil {
				t.Fatal(err)
			}
			solDefaultsAssertStatus(t, report, "gpt-6.1-sol")
			if name == "personal" && (!strings.Contains(config, "# user model") || !strings.Contains(config, "[profiles.personal]\nmodel = 'profile'\n")) {
				t.Fatal("setup changed personal comments or profile settings")
			}
			originals := solDefaultsOriginals(t, installer)
			repeated := sharedLifecycleInstaller(t, fixture, true)
			if _, err := repeated.Setup(true); err != nil {
				t.Fatal(err)
			}
			if got := installerRead(t, filepath.Join(fixture.paths.Codex, "config.toml")); got != config {
				t.Fatal("repeat setup changed the config")
			}
			if !reflect.DeepEqual(solDefaultsOriginals(t, repeated), originals) {
				t.Fatal("repeat setup replaced the original ownership values")
			}
		})
	}
}

// The standalone manager has no plain, non-enrolling install: install enrolls
// the root model like setup, keeps the personal comments and profiles beside
// it, and uninstall restores the personal root byte for byte.
func TestSolDefaultsPlainInstallPreservesPersonalRoot(t *testing.T) {
	t.Parallel()
	fixture := sharedInstallerFixture(t)
	originals := fixture.seedUserFiles(t)
	installer := sharedLifecycleInstaller(t, fixture, true)
	if _, err := installer.Execute("install"); err != nil {
		t.Fatal(err)
	}
	config := solDefaultsAssertRoot(t, fixture, "gpt-6.1-sol", "max")
	if !strings.Contains(config, "# user model") || !strings.Contains(config, "[profiles.personal]\nmodel = 'profile'\n") {
		t.Fatal("install changed personal comments or profile settings")
	}
	sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
	report, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	if object(report["model_config"])["managed"] != true {
		t.Fatalf("install did not enroll the root model: %#v", report["model_config"])
	}
	solDefaultsAssertStatus(t, report, "gpt-6.1-sol")
	if _, err := installer.Uninstall(); err != nil {
		t.Fatal(err)
	}
	solDefaultsAssertRestored(t, originals)
}

// The checkout and no-checkout variants were one standalone flow: update
// activates the newer bundle without any source checkout.
func TestAstraToSolLifecycle(t *testing.T) {
	t.Parallel()
	fixture := sharedInstallerFixture(t)
	originals := fixture.seedUserFiles(t)
	configPath := filepath.Join(fixture.paths.Codex, "config.toml")
	personalProfile := "\n[profiles.sol_defaults_personal]\nmodel = \"gpt-6-astra\"\nmodel_reasoning_effort = \"high\"\n"
	originals[configPath] = "# Sol defaults personal comment\n" + sharedInstallerDeviceConfig + personalProfile
	installerWrite(t, configPath, originals[configPath], 0o640)
	commitModelPolicy(t, fixture, func(policy Object) {
		policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6-astra"
	}, "Historical Astra Max with GitHub profile")
	fixture.first = sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6-astra", "gpt-6-astra"))
	astraRevision := fixture.first
	newInstaller := func() *Installer {
		return sharedLifecycleInstaller(t, fixture, true)
	}
	assertConfig := func(model string) string {
		config := solDefaultsAssertRoot(t, fixture, model, "max")
		sharedInstallerAssertDefaults(t, fixture, model, model)
		if !strings.Contains(config, "# Sol defaults personal comment") || !strings.Contains(config, personalProfile) {
			t.Fatal("lifecycle changed personal comments or profile settings")
		}
		info, err := os.Stat(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("config mode = %o; want 640", info.Mode().Perm())
		}
		return config
	}
	installer := newInstaller()
	if _, err := installer.Setup(true); err != nil {
		t.Fatal(err)
	}
	astraConfig := assertConfig("gpt-6-astra")
	ownership := solDefaultsOriginals(t, installer)
	if ownership["model"] == nil || ownership["shared"] == nil {
		t.Fatal("historical setup did not retain the personal root and shared originals")
	}
	astraStatus := solDefaultsStatusWithoutSource(t, fixture, installer, "gpt-6-astra")
	if got := fixture.activeRevision(t); got != astraRevision {
		t.Fatalf("initial release = %s; want Astra %s", got, astraRevision)
	}
	commitModelPolicy(t, fixture, func(policy Object) {
		policy["profiles"].(Object)["sol"].(Object)["model"] = "gpt-6.1-sol"
	}, "Maintained Sol Max with GitHub profile")
	solRevision := sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
	installer = newInstaller()
	if _, err := installer.Update(); err != nil {
		t.Fatal(err)
	}
	if got, want := assertConfig("gpt-6.1-sol"), solDefaultsReplaceManagedModels(t, astraConfig, "gpt-6-astra", "gpt-6.1-sol"); got != want {
		t.Fatal("update changed config bytes beyond the three managed model values")
	}
	if !reflect.DeepEqual(solDefaultsOriginals(t, installer), ownership) {
		t.Fatal("update replaced the original ownership values")
	}
	solStatus := solDefaultsStatusWithoutSource(t, fixture, installer, "gpt-6.1-sol")
	if got := fixture.activeRevision(t); got != solRevision {
		t.Fatalf("activated release = %s; want exact Sol %s", got, solRevision)
	}
	if reflect.DeepEqual(solStatus["release"], astraStatus["release"]) {
		t.Fatal("update did not select a new release")
	}
	installer = newInstaller()
	if _, err := installer.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := assertConfig("gpt-6-astra"); got != astraConfig {
		t.Fatal("rollback did not restore the Astra config bytes")
	}
	if !reflect.DeepEqual(solDefaultsOriginals(t, installer), ownership) {
		t.Fatal("rollback replaced the original ownership values")
	}
	rolledBack := solDefaultsStatusWithoutSource(t, fixture, installer, "gpt-6-astra")
	if got := fixture.activeRevision(t); got != astraRevision {
		t.Fatalf("rollback release = %s; want exact Astra %s", got, astraRevision)
	}
	if !reflect.DeepEqual(rolledBack["release"], astraStatus["release"]) {
		t.Fatal("rollback did not restore the historical release")
	}
	installer = newInstaller()
	if _, err := installer.Uninstall(); err != nil {
		t.Fatal(err)
	}
	solDefaultsAssertRestored(t, originals)
}

func solDefaultsSharedDefaults(model string) Object {
	return Object{
		"model_context_window":         int64(512000),
		"features.memories":            true,
		"memories.generate_memories":   true,
		"memories.use_memories":        true,
		"memories.extract_model":       model,
		"memories.consolidation_model": model,
	}
}

func solDefaultsOriginals(t *testing.T, installer *Installer) Object {
	t.Helper()
	state, err := installer.state(true)
	if err != nil {
		t.Fatal(err)
	}
	host := hostRecord(state, hostCodex)
	return Object{
		"model":  object(host["model_config"])["original"],
		"shared": object(host["shared_config"])["original"],
	}
}

func solDefaultsAssertStatus(t *testing.T, report Object, model string) {
	t.Helper()
	profiles := policyProfiles(model, "max", "xhigh")
	profiles["github"] = Object{"model": "gpt-6-luna", "reasoning_effort": "high", "scope": "bounded_execution"}
	if !reflect.DeepEqual(report["model_profiles"], profiles) {
		t.Fatalf("status profiles = %#v; want %#v", report["model_profiles"], profiles)
	}
	defaults := map[string]string{"model": model, "model_reasoning_effort": "max"}
	if !reflect.DeepEqual(report["model_defaults"], defaults) || object(report["model_config"])["managed"] != true {
		t.Fatalf("status root defaults or ownership = %#v", report)
	}
	shared := object(report["shared_config"])
	if shared["managed"] != true || !reflect.DeepEqual(shared["defaults"], solDefaultsSharedDefaults(model)) {
		t.Fatalf("status shared defaults or ownership = %#v", shared)
	}
}

// solDefaultsStatusWithoutSource shows status reads only the installed
// snapshot: hiding the source the fixture packs bundles from changes nothing.
func solDefaultsStatusWithoutSource(t *testing.T, fixture *installerFixture, installer *Installer, model string) Object {
	t.Helper()
	available, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	hidden := fixture.source + ".sol-defaults-unavailable"
	if err := os.Rename(fixture.source, hidden); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(hidden, fixture.source); err != nil {
			t.Errorf("restore isolated fixture source: %v", err)
		}
	}()
	before := installerSnapshot(t, fixture.base)
	report, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	installerUnchanged(t, fixture.base, before)
	solDefaultsAssertStatus(t, report, model)
	for _, key := range []string{"release", "model_profiles", "model_defaults", "model_config", "shared_config"} {
		if !reflect.DeepEqual(report[key], available[key]) {
			t.Fatalf("source unavailability changed status %s", key)
		}
	}
	return report
}

func solDefaultsStringField(t *testing.T, config, section, key string) (string, int, int) {
	t.Helper()
	offset := 0
	if section != "" {
		header := regexp.MustCompile(`(?m)^[\t ]*\[` + regexp.QuoteMeta(section) + `\][\t ]*(?:#[^\r\n]*)?\r?\n`)
		matches := header.FindAllStringIndex(config, -1)
		if len(matches) != 1 {
			t.Fatalf("expected one %s table; found %d", section, len(matches))
		}
		offset = matches[0][1]
	}
	body := config[offset:]
	if table := regexp.MustCompile(`(?m)^[\t ]*\[`).FindStringIndex(body); table != nil {
		body = body[:table[0]]
	}
	pattern := regexp.MustCompile(`(?m)^[\t ]*` + regexp.QuoteMeta(key) + `[\t ]*=[\t ]*("[^"\r\n]*"|'[^'\r\n]*')`)
	matches := pattern.FindAllStringSubmatchIndex(body, -1)
	if len(matches) != 1 {
		t.Fatalf("expected one %s.%s field; found %d", section, key, len(matches))
	}
	start, end := offset+matches[0][2], offset+matches[0][3]
	return config[start+1 : end-1], start, end
}

func solDefaultsReplaceRootModel(t *testing.T, config, oldModel, newModel string) string {
	t.Helper()
	actual, start, end := solDefaultsStringField(t, config, "", "model")
	if actual != oldModel {
		t.Fatalf("root model = %q; want %q before mutation", actual, oldModel)
	}
	return config[:start+1] + newModel + config[end-1:]
}

func solDefaultsReplaceManagedModels(t *testing.T, config, oldModel, newModel string) string {
	t.Helper()
	for _, field := range []struct{ section, key string }{{"", "model"}, {"memories", "extract_model"}, {"memories", "consolidation_model"}} {
		actual, start, end := solDefaultsStringField(t, config, field.section, field.key)
		if actual != oldModel {
			t.Fatalf("%s.%s = %q; want %q before mutation", field.section, field.key, actual, oldModel)
		}
		config = config[:start+1] + newModel + config[end-1:]
	}
	return config
}

func solDefaultsAssertRoot(t *testing.T, fixture *installerFixture, model, effort string) string {
	t.Helper()
	config := installerRead(t, filepath.Join(fixture.paths.Codex, "config.toml"))
	for key, want := range map[string]string{"model": model, "model_reasoning_effort": effort} {
		if got, _, _ := solDefaultsStringField(t, config, "", key); got != want {
			t.Fatalf("root %s = %q; want %q", key, got, want)
		}
	}
	return config
}

func solDefaultsAssertRestored(t *testing.T, originals map[string]string) {
	t.Helper()
	for path, want := range originals {
		if got := installerRead(t, path); got != want {
			t.Fatalf("uninstall did not restore %s byte-for-byte", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("restored %s mode = %o; want 640", path, info.Mode().Perm())
		}
	}
}
