package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const sharedInstallerResourcePath = "global/codex-defaults.toml"

const sharedInstallerDeviceConfig = `# Device configuration
model = 'local-model' # keep model note
model_reasoning_effort = 'high'
sandbox_mode = 'workspace-write'

[profiles.personal]
model = 'profile-model'

[features]
memories = false # keep feature note
device_feature = true

[memories]
generate_memories = false # keep generation note
use_memories = false
extract_model = 'local-extract'
consolidation_model = '''local-consolidation'''
device_retention = 17 # keep retention note

[mcp_servers.local]
command = '/device-only/helper'
args = ['--private']
`

func sharedInstallerResource(extractModel, consolidationModel string) string {
	return fmt.Sprintf(`version = 1
model_context_window = 512000

[features]
memories = true

[memories]
generate_memories = true
use_memories = true
extract_model = %q
consolidation_model = %q
`, extractModel, consolidationModel)
}

func sharedInstallerRelease(t *testing.T, fixture *installerFixture, resource string) string {
	t.Helper()
	installerWrite(t, filepath.Join(fixture.paths.Source, filepath.FromSlash(sharedInstallerResourcePath)), resource, 0o644)
	fixture.git(t, "add", sharedInstallerResourcePath)
	fixture.git(t, "commit", "-m", "Shared config fixture release")
	return fixture.git(t, "rev-parse", "HEAD")
}

func sharedInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	fixture := newInstallerFixture(t)
	fixture.first = sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
	return fixture
}

func sharedLifecycleInstaller(t *testing.T, fixture *installerFixture, apply bool) *Installer {
	t.Helper()
	revision := fixture.git(t, "rev-parse", "HEAD")
	installer := sharedConfigRuntimeFixtureFor(t, fixture, revision)
	installer.Apply = apply
	installer.PrepareRuntime = func(ctx context.Context, source, selectedRevision, state string) (RuntimeCandidate, error) {
		if ctx != installer.Context || source != installer.Source || state != installer.State {
			t.Fatal("shared config runtime preparation lost the installation context or roots")
		}
		candidate := sharedConfigRuntimeFixtureFor(t, fixture, selectedRevision)
		return RuntimeCandidate{Path: candidate.RuntimeCandidate, Identity: candidate.RuntimeIdentity}, nil
	}
	return installer
}

func sharedLifecycleExecute(t *testing.T, fixture *installerFixture, command string) Object {
	t.Helper()
	report, err := sharedLifecycleInstaller(t, fixture, true).Execute(command)
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return report
}

func sharedLifecycleFailAt(t *testing.T, fixture *installerFixture, command, label string) {
	t.Helper()
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
	_, err := sharedLifecycleInstaller(t, fixture, true).Execute(command)
	fixture.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
	if err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
		t.Fatalf("%s with %s: got %v", command, label, err)
	}
}

func sharedInstallerConfigPath(fixture *installerFixture) string {
	return filepath.Join(fixture.paths.Codex, "config.toml")
}

func sharedInstallerLookup(values Object, key string) (any, bool) {
	var current any = values
	for part := range strings.SplitSeq(key, ".") {
		parent, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = parent[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func sharedInstallerAssertDefaults(t *testing.T, fixture *installerFixture, extractModel, consolidationModel string) {
	t.Helper()
	var values Object
	if err := toml.Unmarshal([]byte(installerRead(t, sharedInstallerConfigPath(fixture))), &values); err != nil {
		t.Fatalf("installed config is not valid TOML: %v", err)
	}
	expected := Object{
		"features.memories": true, "memories.generate_memories": true, "memories.use_memories": true,
		"memories.extract_model": extractModel, "memories.consolidation_model": consolidationModel,
		"model_context_window": int64(512000),
	}
	for key, want := range expected {
		if got, found := sharedInstallerLookup(values, key); !found || !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func sharedInstallerAssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("config mode = %o, want %o", info.Mode().Perm(), want)
	}
}

func TestSharedConfigInstallerInstallAndSetupPreserveDeviceSettings(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"install", "setup"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			fixture := sharedInstallerFixture(t)
			fixture.seedUserFiles(t)
			path := sharedInstallerConfigPath(fixture)
			installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
			installer := sharedLifecycleInstaller(t, fixture, false)
			before := installerSnapshot(t, fixture.base)
			report, err := installer.Execute(command)
			if err != nil || report["dry_run"] != true {
				t.Fatalf("preview %s: %#v, %v", command, report, err)
			}
			installerUnchanged(t, fixture.base, before)
			sharedLifecycleExecute(t, fixture, command)
			sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
			installed := installerRead(t, path)
			for _, preserved := range []string{
				"# keep model note", "# keep feature note", "# keep generation note",
				"sandbox_mode = 'workspace-write'", "model = 'profile-model'", "device_feature = true",
				"device_retention = 17 # keep retention note", "command = '/device-only/helper'", "args = ['--private']",
			} {
				if !strings.Contains(installed, preserved) {
					t.Errorf("installation lost device bytes %q", preserved)
				}
			}
			state, err := fixture.installer(false).state(true)
			if err != nil {
				t.Fatal(err)
			}
			if hostRecord(state, hostCodex)["shared_config"] == nil || (hostRecord(state, hostCodex)["model_config"] != nil) != (command == "setup") {
				t.Fatalf("unexpected config ownership for %s: %#v", command, state)
			}
			if command == "install" && !strings.Contains(installed, "model = 'local-model'") {
				t.Fatal("install changed an unenrolled root model")
			}
			sharedInstallerAssertMode(t, path, 0o640)
			status := sharedLifecycleExecute(t, fixture, "status")
			sharedStatus := object(status["shared_config"])
			if sharedStatus["managed"] != true || object(sharedStatus["defaults"])["memories.extract_model"] != "gpt-6.1-sol" || object(sharedStatus["defaults"])["memories.consolidation_model"] != "gpt-6.1-sol" {
				t.Fatalf("status did not report managed shared defaults: %#v", sharedStatus)
			}
			if command == "setup" {
				before = installerSnapshot(t, fixture.paths.Home)
				if report := sharedLifecycleExecute(t, fixture, "setup"); report["changed"] != false {
					t.Fatalf("repeat setup changed installation: %#v", report)
				}
				installerUnchanged(t, fixture.paths.Home, before)
			}
			later := strings.Replace(installed, "device_retention = 17", "device_retention = 99", 1) + "\n# Later local edit\n"
			installerWrite(t, path, later, 0o604)
			sharedLifecycleExecute(t, fixture, "uninstall")
			want := strings.Replace(sharedInstallerDeviceConfig, "device_retention = 17", "device_retention = 99", 1) + "\n# Later local edit\n"
			if got := installerRead(t, path); got != want {
				t.Fatalf("uninstall lost original or later device bytes: got %q, want %q", got, want)
			}
			sharedInstallerAssertMode(t, path, 0o604)
		})
	}
}

func TestSharedConfigInstallerUpdateRollbackAndOriginalRestoration(t *testing.T) {
	t.Parallel()
	fixture := sharedInstallerFixture(t)
	fixture.seedUserFiles(t)
	path := sharedInstallerConfigPath(fixture)
	installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
	origin := fixture.bareOrigin(t)
	sharedLifecycleExecute(t, fixture, "setup")
	installerWrite(t, path, strings.Replace(installerRead(t, path), "device_retention = 17", "device_retention = 99", 1)+"\n# Later local edit\n", 0o604)
	second := sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6-sol", "gpt-6-luna"))
	fixture.git(t, "push", origin, "main")
	fixture.git(t, "reset", "--hard", fixture.first)
	preview := sharedLifecycleInstaller(t, fixture, false)
	before := installerSnapshot(t, fixture.base)
	if _, err := preview.Update(); err != nil {
		t.Fatal(err)
	}
	installerUnchanged(t, fixture.base, before)
	if report := sharedLifecycleExecute(t, fixture, "update"); report["release"] != second || report["changed"] != true {
		t.Fatalf("update did not activate shared defaults: %#v", report)
	}
	sharedInstallerAssertDefaults(t, fixture, "gpt-6-sol", "gpt-6-luna")
	sharedInstallerAssertMode(t, path, 0o604)
	before = installerSnapshot(t, fixture.paths.Home)
	if report := sharedLifecycleExecute(t, fixture, "update"); report["changed"] != false {
		t.Fatalf("repeat update changed installation: %#v", report)
	}
	installerUnchanged(t, fixture.paths.Home, before)
	sharedLifecycleExecute(t, fixture, "rollback")
	sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
	if !strings.Contains(installerRead(t, path), "device_retention = 99 # keep retention note") {
		t.Fatal("update or rollback lost device settings")
	}
	sharedLifecycleExecute(t, fixture, "uninstall")
	want := strings.Replace(sharedInstallerDeviceConfig, "device_retention = 17", "device_retention = 99", 1) + "\n# Later local edit\n"
	if got := installerRead(t, path); got != want {
		t.Fatalf("update replaced the saved originals: got %q, want %q", got, want)
	}
	sharedInstallerAssertMode(t, path, 0o604)
}

func TestSharedConfigInstallerRollbackToHistoricalResourceAbsence(t *testing.T) {
	t.Parallel()
	fixture := newInstallerFixture(t)
	fixture.seedUserFiles(t)
	path := sharedInstallerConfigPath(fixture)
	installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
	fixture.execute(t, "setup")
	beforeConfig := installerRead(t, path)
	sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
	sharedLifecycleExecute(t, fixture, "setup")
	sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
	installerAppend(t, path, "\n# Local edit before rollback\n")
	sharedLifecycleExecute(t, fixture, "rollback")
	state, err := fixture.installer(false).state(true)
	if err != nil {
		t.Fatal(err)
	}
	if hostRecord(state, hostCodex)["shared_config"] != nil || state["release"] != fixture.first {
		t.Fatalf("historical rollback retained shared ownership: %#v", state)
	}
	if got := installerRead(t, path); got != beforeConfig+"\n# Local edit before rollback\n" {
		t.Fatalf("historical rollback did not restore local shared values: %q", got)
	}
	sharedLifecycleExecute(t, fixture, "status")
	sharedLifecycleExecute(t, fixture, "uninstall")
	if got := installerRead(t, path); got != sharedInstallerDeviceConfig+"\n# Local edit before rollback\n" {
		t.Fatalf("historical rollback changed uninstall originals: %q", got)
	}
}

func TestSharedConfigInstallerAbsentFileOriginSurvivesOwnershipMigration(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"model first", "shared first"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			path := sharedInstallerConfigPath(fixture)
			if order == "model first" {
				fixture.execute(t, "setup")
				sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
				sharedLifecycleExecute(t, fixture, "setup")
			} else {
				origin := fixture.bareOrigin(t)
				fixture.execute(t, "install")
				sharedInstallerRelease(t, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
				fixture.git(t, "push", origin, "main")
				sharedLifecycleExecute(t, fixture, "update")
				state, err := fixture.installer(false).state(true)
				if err != nil || hostRecord(state, hostCodex)["model_config"] != nil {
					t.Fatalf("shared-only update enrolled root defaults: %#v, %v", state, err)
				}
				sharedLifecycleExecute(t, fixture, "setup")
			}
			sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
			sharedLifecycleExecute(t, fixture, "rollback")
			state, err := fixture.installer(false).state(true)
			if err != nil {
				t.Fatal(err)
			}
			if hostRecord(state, hostCodex)["shared_config"] != nil || hostRecord(state, hostCodex)["config_original_exists"] != false || hostRecord(state, hostCodex)["model_config"] == nil {
				t.Fatalf("rollback lost earliest config origin: %#v", state)
			}
			sharedLifecycleExecute(t, fixture, "uninstall")
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("originally absent empty config survived uninstall: %v", err)
			}
		})
	}
}

func TestSharedConfigInstallerOneRecoverableConfigOperation(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"install", "setup"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			fixture := sharedInstallerFixture(t)
			fixture.seedUserFiles(t)
			installerWrite(t, sharedInstallerConfigPath(fixture), sharedInstallerDeviceConfig, 0o640)
			before := installerSnapshot(t, fixture.paths.Home)
			sharedLifecycleFailAt(t, fixture, command, "after-journal")
			journal, err := readJSON(filepath.Join(fixture.paths.State, "journal.json"))
			if err != nil {
				t.Fatal(err)
			}
			if integer(journal["version"]) != 3 {
				t.Fatal("shared config was journaled without a capable v2 recovery runtime")
			}
			count := 0
			for _, raw := range sequence(journal["operations"]) {
				op := object(raw)
				if op["path"] != Normalize(sharedInstallerConfigPath(fixture)) {
					continue
				}
				count++
				wantKeys := 6
				if command == "setup" {
					wantKeys = 8
				}
				if op["kind"] != "codex_config" || len(object(op["before_keys"])) != wantKeys || len(object(op["after_keys"])) != wantKeys {
					t.Fatalf("incomplete combined config operation: %#v", op)
				}
			}
			if count != 1 {
				t.Fatalf("config has %d mutation operations, want one", count)
			}
			sharedLifecycleExecute(t, fixture, "recover")
			installerUnchanged(t, fixture.paths.Home, before)
		})
	}
}

func TestSharedConfigInstallerActivationRecoveryRestoresOriginalBytes(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"install", "setup"} {
		for _, boundary := range []string{"after-codex-config", "after-state"} {
			t.Run(command+"/"+boundary, func(t *testing.T) {
				t.Parallel()
				fixture := sharedInstallerFixture(t)
				original := fixture.seedUserFiles(t)
				path := sharedInstallerConfigPath(fixture)
				installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
				original[path] = sharedInstallerDeviceConfig
				sharedLifecycleFailAt(t, fixture, command, boundary)
				sharedLifecycleExecute(t, fixture, "recover")
				for path, want := range original {
					if got := installerRead(t, path); got != want {
						t.Fatalf("recovery changed original file %s: %q", path, got)
					}
					sharedInstallerAssertMode(t, path, 0o640)
				}
				installer := fixture.installer(false)
				for _, path := range []string{installer.current, installer.statePath, installer.journalPath, installer.commandPath(), installer.runtimeCurrent(), installer.locatorPath()} {
					if exists(path) {
						t.Fatalf("recovery retained owned path %s", path)
					}
				}
				if sharedLifecycleExecute(t, fixture, "status")["installed"] != false {
					t.Fatal("recovery retained an interrupted fresh installation")
				}
			})
		}
	}
}

func TestSharedConfigInstallerInterruptedRecoveryPreservesIndependentEdits(t *testing.T) {
	t.Parallel()
	fixture := sharedInstallerFixture(t)
	fixture.seedUserFiles(t)
	path := sharedInstallerConfigPath(fixture)
	installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
	sharedLifecycleExecute(t, fixture, "setup")
	sharedLifecycleFailAt(t, fixture, "uninstall", "after-state")
	installerWrite(t, path, strings.Replace(installerRead(t, path), "device_retention = 17", "device_retention = 99", 1)+"\n# Edit after removal\n", 0o604)
	sharedLifecycleFailAt(t, fixture, "recover", "after-recover-codex-config")
	installerAppend(t, path, "# Edit during recovery\n")
	sharedLifecycleExecute(t, fixture, "recover")
	sharedLifecycleExecute(t, fixture, "status")
	sharedInstallerAssertDefaults(t, fixture, "gpt-6.1-sol", "gpt-6.1-sol")
	sharedLifecycleExecute(t, fixture, "uninstall")
	want := strings.Replace(sharedInstallerDeviceConfig, "device_retention = 17", "device_retention = 99", 1) + "\n# Edit after removal\n# Edit during recovery\n"
	if got := installerRead(t, path); got != want {
		t.Fatalf("interrupted recovery lost local edits: got %q, want %q", got, want)
	}
	sharedInstallerAssertMode(t, path, 0o604)
	before := installerSnapshot(t, fixture.paths.Home)
	if sharedLifecycleExecute(t, fixture, "recover")["pending"] != false {
		t.Fatal("completed recovery reported pending work")
	}
	installerUnchanged(t, fixture.paths.Home, before)
}

func TestSharedConfigInstallerOwnedDriftRefusesMutation(t *testing.T) {
	t.Parallel()
	for _, mutation := range []struct {
		name, before, after string
	}{
		{"feature toggle", "memories = true # keep feature note", "memories = false # keep feature note"},
		{"generation toggle", "generate_memories = true", "generate_memories = false"},
		{"extract model", `extract_model = "gpt-6.1-sol"`, `extract_model = "local-change"`},
		{"consolidation model", `consolidation_model = "gpt-6.1-sol"`, `consolidation_model = "local-change"`},
		{"context window", "model_context_window = 512000", "model_context_window = 272000"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			fixture := sharedInstallerFixture(t)
			fixture.seedUserFiles(t)
			path := sharedInstallerConfigPath(fixture)
			installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
			fixture.bareOrigin(t)
			sharedLifecycleExecute(t, fixture, "setup")
			installed := installerRead(t, path)
			changed := strings.Replace(installed, mutation.before, mutation.after, 1)
			if changed == installed {
				t.Fatal("owned drift fixture did not change the selected key")
			}
			installerWrite(t, path, changed, 0o640)
			for _, command := range []string{"setup", "update", "uninstall"} {
				installer := sharedLifecycleInstaller(t, fixture, true)
				before := installerSnapshot(t, fixture.base)
				if _, err := installer.Execute(command); err == nil || !strings.Contains(strings.ToLower(err.Error()), "changed") {
					t.Fatalf("%s accepted owned shared drift: %v", command, err)
				}
				installerUnchanged(t, fixture.base, before)
			}
		})
	}
}

func TestSharedConfigInstallerRecoveryRejectsOwnedDriftBeforeReversal(t *testing.T) {
	t.Parallel()
	fixture := sharedInstallerFixture(t)
	fixture.seedUserFiles(t)
	path := sharedInstallerConfigPath(fixture)
	installerWrite(t, path, sharedInstallerDeviceConfig, 0o640)
	sharedLifecycleFailAt(t, fixture, "setup", "after-state")
	installerWrite(t, path, strings.Replace(installerRead(t, path), `extract_model = "gpt-6.1-sol"`, `extract_model = "local-change"`, 1), 0o640)
	installer := sharedLifecycleInstaller(t, fixture, true)
	before := installerSnapshot(t, fixture.base)
	if _, err := installer.Recover(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "changed") {
		t.Fatalf("recovery accepted owned shared drift: %v", err)
	}
	installerUnchanged(t, fixture.base, before)
}

func TestSharedConfigInstallerInvalidSourceRefusesMutation(t *testing.T) {
	t.Parallel()
	valid := sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol")
	t.Run("supported five-key preview", func(test *testing.T) {
		test.Parallel()
		fixture := newInstallerFixture(test)
		fixture.seedUserFiles(test)
		resource := strings.Replace(valid, "model_context_window = 512000\n", "", 1)
		sharedInstallerRelease(test, fixture, resource)
		installer := sharedLifecycleInstaller(test, fixture, false)
		before := installerSnapshot(test, fixture.base)
		if _, err := installer.Setup(false); err != nil {
			test.Fatalf("five-key shared source preview failed: %v", err)
		}
		installerUnchanged(test, fixture.base, before)
	})
	for name, resource := range map[string]string{
		"malformed":                   "version = [",
		"unsupported version":         strings.Replace(valid, "version = 1", "version = 2", 1),
		"unknown root key":            "device_path = '/private/device'\n" + valid,
		"wrong feature type":          strings.Replace(valid, "memories = true", "memories = 'true'", 1),
		"wrong model type":            strings.Replace(valid, `extract_model = "gpt-6.1-sol"`, "extract_model = true", 1),
		"missing consolidation model": strings.Replace(valid, "consolidation_model = \"gpt-6.1-sol\"\n", "", 1),
		"string context window":       strings.Replace(valid, "model_context_window = 512000", "model_context_window = '512000'", 1),
		"zero context window":         strings.Replace(valid, "model_context_window = 512000", "model_context_window = 0", 1),
		"unknown memory key":          valid + "device_retention = 99\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			sharedInstallerRelease(t, fixture, resource)
			installer := sharedLifecycleInstaller(t, fixture, true)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Setup(false); err == nil {
				t.Fatal("installation accepted invalid shared source defaults")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestSharedConfigInstallerInvalidDeviceConfigRefusesMutation(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]string{
		"malformed":            "other = [",
		"wrong feature type":   "[features]\nmemories = 'false'\n",
		"wrong model type":     "[memories]\nextract_model = true\n",
		"wrong window type":    "model_context_window = '512k'\n",
		"scalar memory parent": "memories = 'device value'\n",
		"inline features":      "features = { memories = false, device_feature = true }\n",
		"inline memories":      "memories = { extract_model = 'local-extract', device_retention = 17 }\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := sharedInstallerFixture(t)
			fixture.seedUserFiles(t)
			installerWrite(t, sharedInstallerConfigPath(fixture), config, 0o640)
			installer := sharedLifecycleInstaller(t, fixture, true)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Setup(false); err == nil {
				t.Fatal("installation accepted invalid or unsupported device config")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}
