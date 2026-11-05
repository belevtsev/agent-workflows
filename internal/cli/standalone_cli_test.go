package cli

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/manageruntime"
	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

// payloadRefusal is the stable part of the no-payload refusal (R1.4).
const payloadRefusal = "has no embedded workflow payload"

// lifecycleArgs covers every lifecycle action, including bare cw (R1.1).
var lifecycleArgs = [][]string{
	{}, {"install"}, {"install", "--dry-run"}, {"setup"}, {"setup", "--dry-run"},
	{"update"}, {"update", "--dry-run"}, {"status"}, {"rollback"}, {"recover"}, {"uninstall"},
}

var fixtureRegistrations = []string{"archify", "graphify", "docker-agent-run"}

// registeredService reports enrolled registrations, the input from which the
// removed CLI derived skill_prerequisites.
type registeredService struct{ fakeService }

func (r *registeredService) Execute(action string) (workflow.Object, error) {
	r.action = action
	return workflow.Object{"command": action, "registrations": fixtureRegistrations}, nil
}

func (r *registeredService) Status() (workflow.Object, error) {
	r.statusCalls++
	return workflow.Object{"installed": true, "registrations": fixtureRegistrations}, nil
}

func argsName(args []string) string {
	if len(args) == 0 {
		return "bare"
	}
	return strings.Join(args, "_")
}

// removedReportKeys lists every member, at any depth, that R1.5 removed from
// the lifecycle report.
func removedReportKeys(report map[string]any) []string {
	removed := []string{"skill_prerequisites", "integrations", "migration", "legacy_admission"}
	var found []string
	var visit func(value any, path string)
	visit = func(value any, path string) {
		switch item := value.(type) {
		case map[string]any:
			for name, child := range item {
				if slices.Contains(removed, name) || strings.HasSuffix(name, "_credential_present") {
					found = append(found, path+name)
				}
				visit(child, path+name+".")
			}
		case []any:
			for index, child := range item {
				visit(child, path+strconv.Itoa(index)+".")
			}
		}
	}
	visit(report, "")
	slices.Sort(found)
	return found
}

// treeSnapshot records the type, mode and content of everything below root.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot[name] = "symlink " + target
		case info.IsDir():
			snapshot[name] = "directory " + info.Mode().String()
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot[name] = info.Mode().String() + " " + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func requireUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, root)
	if maps.Equal(before, after) {
		return
	}
	var changed []string
	for name, value := range before {
		if current, ok := after[name]; !ok || current != value {
			changed = append(changed, name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)
	t.Fatalf("refused command changed %s: %v", root, changed)
}

// R1.4: a binary without an embedded payload refuses every lifecycle action
// with a runtime error, after flag validation, and writes nothing.
func TestLifecycleWithoutPayloadFailsAfterFlagValidation(t *testing.T) {
	refused := append(slices.Clone(lifecycleArgs), []string{"status", "--json"}, []string{"uninstall", "--host", "codex", "--forget"}, []string{"--bundle-identity"}, []string{"--verify-release", "{release}"})
	for _, args := range refused {
		t.Run(argsName(args), func(t *testing.T) {
			config, _, _, service := testConfig(t)
			config.Bundle = nil
			home := homeOf(t, config)
			args = slices.Clone(args)
			if index := slices.Index(args, "{release}"); index >= 0 {
				release := filepath.Join(t.TempDir(), "cw")
				if err := os.WriteFile(release, []byte("not a release executable\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				args[index] = release
			}
			before := treeSnapshot(t, home)
			err := Execute(t.Context(), args, config)
			if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), payloadRefusal) {
				t.Fatalf("missing payload was not refused as a runtime error: %v (exit %d)", err, ExitCode(err))
			}
			if !service.untouched() {
				t.Fatalf("service ran without a payload: created=%d action=%q", service.created, service.action)
			}
			requireUnchanged(t, home, before)
		})
	}
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"install", "--dry-run", "--apply"}, "--apply and --dry-run are mutually exclusive"},
		{[]string{"install", "--shell", "fish"}, "--shell must be auto, bash, zsh or none"},
		{[]string{"update", "--shell", "bash"}, "shell and adoption options require install/setup"},
		{[]string{"status", "--adopt-personal-skills"}, "shell and adoption options require install/setup"},
		{[]string{"install", "--preset", "recommended", "--settings", "settings.json"}, "--preset and --settings are mutually exclusive"},
		{[]string{"status", "--preset", "recommended"}, "configuration selection requires install/setup or update"},
		{[]string{"rollback", "--settings", "settings.json"}, "configuration selection requires install/setup or update"},
		{[]string{"install", "--preset", "fastest"}, "--preset must be recommended or balanced"},
		{[]string{"update", "--dry-run", "--preset", "fastest"}, "--preset must be recommended or balanced"},
		{[]string{"install", "--host", "gemini"}, "--host must be auto, codex, claude or all"},
		{[]string{"status", "--host", "codex"}, "--host requires install/setup or uninstall"},
		{[]string{"uninstall", "--forget"}, "--forget requires uninstall with an explicit host"},
		{[]string{"status", "extra"}, ""},
		{[]string{"install", "--source", "/checkout"}, "unknown flag: --source"},
		{[]string{"validate"}, "validate"},
	} {
		t.Run("usage_"+argsName(test.args), func(t *testing.T) {
			config, _, _, service := testConfig(t)
			config.Bundle = nil
			config.Executable = func() (string, error) { t.Fatal("usage error resolved the running executable"); return "", nil }
			home := homeOf(t, config)
			before := treeSnapshot(t, home)
			err := Execute(t.Context(), test.args, config)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("usage error lost its exit code without a payload: %v (exit %d)", err, ExitCode(err))
			}
			if strings.Contains(err.Error(), payloadRefusal) {
				t.Fatalf("payload refusal preceded flag validation: %v", err)
			}
			if !strings.Contains(err.Error(), test.message) {
				t.Fatalf("usage message %q does not contain %q", err, test.message)
			}
			if !service.untouched() {
				t.Fatal("usage error reached the service")
			}
			requireUnchanged(t, home, before)
		})
	}
}

// R1.2: removed root flags and subcommands are ordinary unknown-flag and
// unknown-command usage errors, rejected before the environment is read.
func TestRemovedFlagsAndCommandsAreUnknown(t *testing.T) {
	for _, test := range []struct {
		args    []string
		mention string
	}{
		{[]string{"--source", "/checkout", "install"}, "unknown flag: --source"},
		{[]string{"update", "--source", "/checkout"}, "unknown flag: --source"},
		{[]string{"doctor", "--source", "/checkout"}, "unknown flag: --source"},
		{[]string{"--migrate-from", "/former"}, "unknown flag: --migrate-from"},
		{[]string{"install", "--migrate-from", "/former"}, "unknown flag: --migrate-from"},
		{[]string{"install", "--no-checkout"}, "unknown flag: --no-checkout"},
		{[]string{"install", "--bootstrap"}, "unknown flag: --bootstrap"},
		{[]string{"update", "--prepare-only"}, "unknown flag: --prepare-only"},
		{[]string{"--prepare-only"}, "unknown flag: --prepare-only"},
		{[]string{"validate"}, "validate"},
		{[]string{"validate", "--json"}, "validate"},
		{[]string{"consult", "jev"}, "jev"},
		{[]string{"consult", "jev", "--request", "request.json", "--output", "record.json", "--dry-run"}, "jev"},
	} {
		t.Run(argsName(test.args), func(t *testing.T) {
			config, _, _, service := testConfig(t)
			forbidden := func() (string, error) { t.Fatal("removed surface resolved the environment"); return "", nil }
			config.Executable, config.HomeDir, config.WorkingDir = forbidden, forbidden, forbidden
			config.Getenv = func(string) string { t.Fatal("removed surface read the environment"); return "" }
			err := Execute(t.Context(), test.args, config)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("removed surface accepted: %v (exit %d)", err, ExitCode(err))
			}
			if !strings.Contains(err.Error(), test.mention) {
				t.Fatalf("error %q does not name %q", err, test.mention)
			}
			if !service.untouched() {
				t.Fatal("removed surface reached the service")
			}
		})
	}
}

// R1.3: the kept root flags are still accepted.
func TestKeptLifecycleFlagsAreAccepted(t *testing.T) {
	t.Run("install_roots_and_selection", func(t *testing.T) {
		config, _, options, service := testConfig(t)
		roots := t.TempDir()
		codex, claude, configDir := filepath.Join(roots, "codex"), filepath.Join(roots, "claude"), filepath.Join(roots, "config")
		for _, directory := range []string{codex, claude, configDir} {
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		args := []string{"install", "--dry-run", "--json", "--non-interactive", "--preset", "recommended", "--host", "all", "--shell", "none", "--codex-home", codex, "--claude-home", claude, "--config-dir", configDir}
		if err := Execute(t.Context(), args, config); err != nil {
			t.Fatal(err)
		}
		if service.action != "setup" || options.Apply {
			t.Fatalf("kept install flags changed the action: %q %+v", service.action, options)
		}
	})
	t.Run("plain", func(t *testing.T) {
		config, _, _, service := testConfig(t)
		if err := Execute(t.Context(), []string{"--plain", "install", "--dry-run"}, config); err != nil {
			t.Fatal(err)
		}
		if service.action != "setup" {
			t.Fatalf("plain install did not run setup: %q", service.action)
		}
	})
	t.Run("home_and_state", func(t *testing.T) {
		config, _, options, service := testConfig(t)
		home, state := t.TempDir(), t.TempDir()
		if err := Execute(t.Context(), []string{"--home", home, "--state-dir", state, "status"}, config); err != nil {
			t.Fatal(err)
		}
		if !samePath(options.Home, home) || !samePath(options.State, state) || service.untouched() {
			t.Fatalf("explicit roots ignored: home %q state %q", options.Home, options.State)
		}
	})
	t.Run("update_preset", func(t *testing.T) {
		config, _, _, service := testConfig(t)
		if err := Execute(t.Context(), []string{"update", "--dry-run", "--preset", "balanced"}, config); err != nil {
			t.Fatal(err)
		}
		if service.untouched() {
			t.Fatal("update did not reach the service")
		}
	})
	t.Run("uninstall_forget", func(t *testing.T) {
		config, _, options, service := testConfig(t)
		if err := Execute(t.Context(), []string{"uninstall", "--host", "codex", "--forget"}, config); err != nil {
			t.Fatal(err)
		}
		if service.untouched() || !options.Forget {
			t.Fatalf("uninstall --forget lost: %+v action=%q", options, service.action)
		}
	})
	t.Run("bundle_identity", func(t *testing.T) {
		config, _, _, service := testConfig(t)
		err := Execute(t.Context(), []string{"--bundle-identity"}, config)
		// The fixture provider fails, so a kept flag is a runtime error, not a usage error.
		if err == nil || ExitCode(err) != 1 || strings.Contains(err.Error(), "unknown flag") || !service.untouched() {
			t.Fatalf("--bundle-identity: %v (exit %d)", err, ExitCode(err))
		}
	})
	t.Run("verify_release", func(t *testing.T) {
		config, _, _, service := testConfig(t)
		release := filepath.Join(t.TempDir(), "cw")
		if err := os.WriteFile(release, []byte("not a release executable\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := Execute(t.Context(), []string{"--verify-release", release}, config)
		if err == nil || ExitCode(err) == 2 || strings.Contains(err.Error(), "unknown flag") || !service.untouched() {
			t.Fatalf("--verify-release: %v (exit %d)", err, ExitCode(err))
		}
	})
}

// R1.5: no lifecycle report carries skill_prerequisites, integrations,
// *_credential_present, migration or legacy_admission.
func TestLifecycleReportsOmitRemovedKeys(t *testing.T) {
	for _, args := range [][]string{
		{"install", "--dry-run", "--json"}, {"setup", "--dry-run", "--json"}, {"update", "--dry-run", "--json"},
		{"status", "--json"}, {"rollback", "--json"}, {"recover", "--json"}, {"uninstall", "--json"},
	} {
		t.Run(argsName(args), func(t *testing.T) {
			config, out, _, _ := testConfig(t)
			service := new(registeredService)
			config.NewService = func(workflow.Options) Service { service.created++; return service }
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if service.untouched() {
				t.Fatal("lifecycle action did not reach the service")
			}
			var report map[string]any
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if action, _ := report["action"].(string); action == "" {
				t.Fatalf("not a lifecycle report: %s", out)
			}
			if found := removedReportKeys(report); len(found) != 0 {
				t.Fatalf("report kept removed keys %v: %s", found, out)
			}
		})
	}
}

// legacyLocator makes Resolve report a version-2 runtime locator, either for
// the running executable or for an older cw left at <home>/.local/bin/cw.
func legacyLocator(t *testing.T, config *Config, home, layout string) {
	t.Helper()
	command := filepath.Join(home, ".local", "bin", "cw")
	if layout == "home_command" {
		if err := os.MkdirAll(filepath.Dir(command), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 97\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	config.Resolve = func(path string) (manageruntime.Locator, error) {
		if layout == "executable" || samePath(path, command) {
			return manageruntime.Locator{}, manageruntime.ErrLegacyInstallation
		}
		return manageruntime.Locator{}, os.ErrNotExist
	}
}

// requireReadOnlyCommandsSucceed runs doctor, consult and task start with the
// given leading root flags. R2.4 requires each to treat a legacy installation
// as no recorded installation and fall back to the default roots.
func requireReadOnlyCommandsSucceed(t *testing.T, config Config, out *bytes.Buffer, flags []string) {
	t.Helper()
	home := homeOf(t, config)
	stderr := new(bytes.Buffer)
	config.Err = stderr
	doctorWrite(t, home, ".codex/config.toml", "[shell_environment_policy.set]\nOPENAI_API_KEY = 'secret-value'\n")
	repo := t.TempDir()
	doctorWrite(t, repo, "package.json", "{}")

	out.Reset()
	if err := Execute(t.Context(), append(slices.Clone(flags), "doctor", "--repo", repo), config); err != nil {
		t.Fatalf("doctor failed on a legacy installation: %v (stderr %q)", err, stderr)
	}
	var doctor projectctx.DoctorReport
	if err := json.Unmarshal(out.Bytes(), &doctor); err != nil {
		t.Fatalf("doctor report: %v: %s", err, out)
	}
	if !doctor.Credentials.CodexShell["OPENAI_API_KEY"] || strings.Contains(out.String(), "secret-value") {
		t.Fatalf("doctor did not use the default Codex home or leaked a credential: %s", out)
	}

	request, output := requestFile(t)
	policy := filepath.Join("..", "..", "skills", "task-orchestration", "model-policy.yaml")
	out.Reset()
	stderr.Reset()
	err := Execute(t.Context(), append(slices.Clone(flags), "consult", "--request", request, "--output", output, "--policy", policy), config)
	if err == nil || ExitCode(err) != 2 || err.Error() != "" || out.String() != "Decision consultation unavailable; Coordinator decision pending\n" {
		t.Fatalf("consult on a legacy installation: %v (exit %d) stdout %q stderr %q", err, ExitCode(err), out, stderr)
	}

	config.In = strings.NewReader(`{"title":"Inspect source beside a legacy installation","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`)
	config.WorkingDir = func() (string, error) { return repo, nil }
	out.Reset()
	stderr.Reset()
	if err := Execute(t.Context(), append(slices.Clone(flags), "task", "start", "--plan", "-"), config); err != nil {
		t.Fatalf("task start failed on a legacy installation: %v (stderr %q)", err, stderr)
	}
	var record taskflow.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Directory == "" {
		t.Fatalf("task start record: %v: %s", err, out)
	}
}

// R2.3/R2.4: a version-2 runtime locator refuses every lifecycle action with
// manageruntime.ErrLegacyInstallation before any write, while read-only
// commands fall back to the default roots.
func TestLegacyLocatorRefusesLifecycleBeforeAnyWrite(t *testing.T) {
	for _, layout := range []string{"executable", "home_command"} {
		t.Run(layout, func(t *testing.T) {
			for _, args := range lifecycleArgs {
				t.Run(argsName(args), func(t *testing.T) {
					config, _, _, service := testConfig(t)
					home := homeOf(t, config)
					legacyLocator(t, &config, home, layout)
					before := treeSnapshot(t, home)
					err := Execute(t.Context(), args, config)
					if !errors.Is(err, manageruntime.ErrLegacyInstallation) || ExitCode(err) != 1 {
						t.Fatalf("legacy locator not refused: %v (exit %d)", err, ExitCode(err))
					}
					if !service.untouched() {
						t.Fatalf("legacy installation reached the service: action=%q", service.action)
					}
					requireUnchanged(t, home, before)
				})
			}
			t.Run("read_only_commands", func(t *testing.T) {
				config, out, _, _ := testConfig(t)
				legacyLocator(t, &config, homeOf(t, config), layout)
				requireReadOnlyCommandsSucceed(t, config, out, nil)
			})
		})
	}
}

// legacyState writes a state.json of a removed version into the default or an
// explicit state directory below home, returning the root flags that select it.
func legacyState(t *testing.T, home string, version int, explicit bool) ([]string, string) {
	t.Helper()
	state := filepath.Join(home, ".local", "state", "agent-workflows")
	var flags []string
	if explicit {
		state = filepath.Join(home, "recorded-state")
		flags = []string{"--state-dir", state}
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"version": version, "home": home, "state_dir": state, "source": filepath.Join(home, "checkout"),
		"codex_home": filepath.Join(home, ".codex"), "claude_home": filepath.Join(home, ".claude"),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "state.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return flags, path
}

// R2.2/R2.4: a state directory written by an older cw refuses lifecycle
// commands with workflow.ErrLegacyInstallation and leaves every byte in place;
// doctor, consult and task commands treat it as no recorded installation.
// recover is omitted: it refuses only when a journal exists.
func TestLegacyStateRefusesLifecycleButNotReadOnlyCommands(t *testing.T) {
	refused := slices.DeleteFunc(slices.Clone(lifecycleArgs), func(args []string) bool { return slices.Contains(args, "recover") })
	for _, version := range []int{1, 2, 3} {
		for _, explicit := range []bool{false, true} {
			name := fmt.Sprintf("version_%d", version)
			if explicit {
				name += "_state_flag"
			}
			t.Run(name, func(t *testing.T) {
				for _, args := range refused {
					t.Run(argsName(args), func(t *testing.T) {
						config, _, _, service := testConfig(t)
						home := homeOf(t, config)
						flags, _ := legacyState(t, home, version, explicit)
						before := treeSnapshot(t, home)
						// Keep the argument list non-nil: bare cw must not fall back to os.Args.
						command := append(append([]string{}, flags...), args...)
						err := Execute(t.Context(), command, config)
						if !errors.Is(err, workflow.ErrLegacyInstallation) || ExitCode(err) != 1 {
							t.Fatalf("legacy state not refused: %v (exit %d)", err, ExitCode(err))
						}
						if !strings.Contains(err.Error(), `run "cw uninstall" with your existing cw, then install again`) {
							t.Fatalf("refusal lacks the uninstall guidance: %v", err)
						}
						if !service.untouched() {
							t.Fatalf("legacy state reached the service: action=%q", service.action)
						}
						requireUnchanged(t, home, before)
					})
				}
				t.Run("read_only_commands", func(t *testing.T) {
					config, out, _, _ := testConfig(t)
					flags, path := legacyState(t, homeOf(t, config), version, explicit)
					original, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					requireReadOnlyCommandsSucceed(t, config, out, flags)
					if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, original) {
						t.Fatalf("read-only command changed the legacy state: %v", err)
					}
				})
			})
		}
	}
}
