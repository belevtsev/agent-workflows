package workflow

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type consultationRuntimeFake struct {
	t             *testing.T
	path          string
	probe         consultationProbe
	pythonStatus  string
	pythonVersion string
	fail          string
	processError  error
	lookups       int
	venvs         int
	ensurepips    int
	pips          int
}

func newConsultationRuntimeFake(t *testing.T, path string) *consultationRuntimeFake {
	t.Helper()
	return &consultationRuntimeFake{t: t, path: path, probe: consultationProbe{Status: "pyyaml_missing", PythonVersion: "3.9.6", PipAvailable: true},
		pythonStatus: "ready", pythonVersion: "3.9.6"}
}

func (f *consultationRuntimeFake) runtime() consultationRuntime {
	return consultationRuntime{lookPath: func(name string) (string, error) {
		f.lookups++
		if name == "python3" {
			return filepath.Join(filepath.Dir(f.path), "selected python3"), nil
		}
		return "", os.ErrNotExist
	}, run: f.run}
}

func (f *consultationRuntimeFake) seed() {
	f.t.Helper()
	installerWrite(f.t, filepath.Join(f.path, "bin", "python"), "#!/bin/sh\nexit 99\n", 0o755)
}

func (f *consultationRuntimeFake) run(ctx context.Context, interpreter string, args ...string) ([]byte, error) {
	f.t.Helper()
	if _, present := ctx.Deadline(); !present {
		f.t.Fatal("consultation subprocess has no deadline")
	}
	if len(args) < 4 || args[0] != "-I" || args[1] != "-B" {
		f.t.Fatalf("unisolated Python invocation: %v", args)
	}
	if args[2] == "-c" {
		probe := f.probe
		if args[3] == consultationPythonProbe {
			probe = consultationProbe{Status: f.pythonStatus, PythonVersion: f.pythonVersion}
		} else if args[3] != consultationEnvironmentProbe {
			f.t.Fatal("unknown Python probe")
		}
		return json.Marshal(probe)
	}
	if args[2] != "-m" {
		f.t.Fatalf("unexpected Python arguments: %v", args)
	}
	stage := args[3]
	if stage == f.fail {
		return nil, f.processError
	}
	switch stage {
	case "venv":
		if len(args) != 5 || args[4] != f.path {
			f.t.Fatalf("unexpected venv target: %v", args)
		}
		f.venvs++
		f.seed()
	case "ensurepip":
		f.ensurepips++
		f.probe.PipAvailable = true
	case "pip":
		if interpreter != filepath.Join(f.path, "bin", "python") {
			f.t.Fatalf("pip used an external interpreter: %s", interpreter)
		}
		for _, required := range []string{"--isolated", "--disable-pip-version-check", "install", "https://pypi.org/simple", "PyYAML>=6,<7"} {
			if !slices.Contains(args, required) {
				f.t.Fatalf("pip omitted %s: %v", required, args)
			}
		}
		if f.probe.Status == "pyyaml_external" && !slices.Contains(args, "--ignore-installed") {
			f.t.Fatal("global PyYAML could satisfy the isolated installation")
		}
		f.pips++
		f.probe.Status, f.probe.PyYAMLVersion = "ready", "6.0.3"
	default:
		f.t.Fatalf("unexpected process stage: %s", stage)
	}
	return nil, nil
}

func TestConsultationRuntimeReusesWorkingEnvironmentOffline(t *testing.T) {
	path := Normalize(filepath.Join(t.TempDir(), "user home", ".venvs", "codex-decisions"))
	fake := newConsultationRuntimeFake(t, path)
	fake.probe.Status, fake.probe.PyYAMLVersion = "ready", "6.0.3"
	fake.seed()
	before := installerSnapshot(t, filepath.Dir(path))
	report, err := fake.runtime().prepare(t.Context(), path, true)
	if err != nil || report["status"] != "ready" || report["changed"] != false || report["python_version"] != "3.9.6" {
		t.Fatalf("reuse: %#v, %v", report, err)
	}
	if fake.lookups != 0 || fake.pips != 0 || fake.venvs != 0 {
		t.Fatal("working environment triggered interpreter discovery or installation")
	}
	installerUnchanged(t, filepath.Dir(path), before)
}

func TestConsultationRuntimeCreatesAndRepairsWithoutRealPip(t *testing.T) {
	for _, state := range []string{"absent", "missing_yaml", "missing_pip", "unsupported_yaml", "external_yaml"} {
		t.Run(state, func(t *testing.T) {
			path := Normalize(filepath.Join(t.TempDir(), "home with spaces", ".venvs", "codex-decisions"))
			fake := newConsultationRuntimeFake(t, path)
			if state != "absent" {
				fake.seed()
				installerWrite(t, filepath.Join(path, "personal-note"), "retain this\n", 0o640)
			}
			if state == "missing_pip" {
				fake.probe.PipAvailable = false
			}
			if state == "unsupported_yaml" {
				fake.probe.Status, fake.probe.PyYAMLVersion = "pyyaml_unsupported", "7.0.0"
			}
			if state == "external_yaml" {
				fake.probe.Status, fake.probe.PyYAMLVersion = "pyyaml_external", "6.0.3"
			}
			report, err := fake.runtime().prepare(t.Context(), path, true)
			if err != nil || report["status"] != "ready" || report["changed"] != true || fake.pips != 1 {
				t.Fatalf("provisioning: %#v, %v; fake=%#v", report, err, fake)
			}
			if state == "absent" {
				if fake.venvs != 1 {
					t.Fatal("missing environment did not invoke venv")
				}
			} else {
				if fake.lookups != 0 || fake.venvs != 0 || installerRead(t, filepath.Join(path, "personal-note")) != "retain this\n" {
					t.Fatal("repair replaced existing environment contents")
				}
			}
			if state == "missing_pip" && fake.ensurepips != 1 {
				t.Fatal("incomplete pip was not repaired with ensurepip")
			}
			before := installerSnapshot(t, filepath.Dir(path))
			if repeated, err := fake.runtime().prepare(t.Context(), path, true); err != nil || repeated["changed"] != false || fake.pips != 1 {
				t.Fatalf("repeat provision: %#v, %v", repeated, err)
			}
			installerUnchanged(t, filepath.Dir(path), before)
		})
	}
}

func TestConsultationRuntimeDryRunAndHistoricalSourcesNeverProvision(t *testing.T) {
	root := Normalize(t.TempDir())
	source, home := filepath.Join(root, "source"), filepath.Join(root, "home")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	i := NewInstaller(Options{Source: source, Home: home, Codex: filepath.Join(home, ".codex"), State: filepath.Join(root, "state"), Context: t.Context(), Apply: true})
	i.PrepareConsultationRuntime = func(context.Context, string, bool) (Object, error) {
		t.Fatal("historical or dry-run source invoked provisioning")
		return nil, nil
	}
	before := installerSnapshot(t, root)
	if report, err := i.prepareConsultationRuntime(); err != nil || report["status"] != "not_required" {
		t.Fatalf("historical source: %#v, %v", report, err)
	}
	installerUnchanged(t, root, before)
	installerWrite(t, filepath.Join(source, "global", "codex-defaults.toml"), "version = 1\n[features]\nmemories = true\n[memories]\ngenerate_memories = true\nuse_memories = true\nextract_model = \"gpt-6.1-sol\"\nconsolidation_model = \"gpt-6.1-sol\"\n", 0o644)
	i.Apply = false
	before = installerSnapshot(t, root)
	if report, err := i.prepareConsultationRuntime(); err != nil || report["status"] != "deferred" || report["python"] != filepath.Join(home, ".venvs", "codex-decisions", "bin", "python") {
		t.Fatalf("dry run: %#v, %v", report, err)
	}
	installerUnchanged(t, root, before)
}

func TestConsultationRuntimeRefusesSymlinkedRootsAndUnavailablePython(t *testing.T) {
	for _, state := range []string{"root_symlink", "parent_symlink", "python_too_old", "venv_missing"} {
		t.Run(state, func(t *testing.T) {
			root := Normalize(t.TempDir())
			path := filepath.Join(root, "home", ".venvs", "codex-decisions")
			fake := newConsultationRuntimeFake(t, path)
			if state == "root_symlink" || state == "parent_symlink" {
				link := path
				if state == "parent_symlink" {
					link = filepath.Dir(path)
				}
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}
			} else if state == "python_too_old" {
				fake.pythonStatus, fake.pythonVersion = "python_unsupported", "3.6.9"
			} else {
				fake.pythonStatus = "venv_unavailable"
			}
			before := installerSnapshot(t, root)
			if _, err := fake.runtime().prepare(t.Context(), path, true); err == nil {
				t.Fatal("unsafe or unavailable runtime accepted")
			}
			installerUnchanged(t, root, before)
		})
	}
}

func TestConsultationRuntimeFailuresAreSanitizedAndRepairable(t *testing.T) {
	for _, stage := range []string{"venv", "pip"} {
		t.Run(stage, func(t *testing.T) {
			path := Normalize(filepath.Join(t.TempDir(), "home", ".venvs", "codex-decisions"))
			fake := newConsultationRuntimeFake(t, path)
			fake.fail, fake.processError = stage, errors.New("https://user:secret@example.invalid/?credential=private")
			_, err := fake.runtime().prepare(t.Context(), path, true)
			if err == nil || !strings.Contains(err.Error(), stage) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "credential") {
				t.Fatalf("unsafe diagnostic: %v", err)
			}
			fake.fail = ""
			if report, err := fake.runtime().prepare(t.Context(), path, true); err != nil || report["status"] != "ready" {
				t.Fatalf("repair after failure: %#v, %v", report, err)
			}
		})
	}
	path := Normalize(filepath.Join(t.TempDir(), "home", ".venvs", "codex-decisions"))
	fake := newConsultationRuntimeFake(t, path)
	fake.fail, fake.processError = "pip", context.DeadlineExceeded
	if _, err := fake.runtime().prepare(t.Context(), path, true); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("deadline lost: %v", err)
	}
}

func TestConsultationRuntimeProcessBoundsAndSecretEnvironment(t *testing.T) {
	root := Normalize(t.TempDir())
	interpreter := filepath.Join(root, "fake python")
	t.Setenv("TYPESAFE_API_KEY", "do-not-pass-this-credential")
	t.Setenv("SUBQ_API_KEY", "do-not-pass-this-credential")
	hostileConfig := filepath.Join(root, "pip.conf")
	outside := filepath.Join(root, "outside target")
	installerWrite(t, hostileConfig, "[global]\ntarget = "+outside+"\nprefix = "+outside+"\n", 0o600)
	t.Setenv("PIP_CONFIG_FILE", hostileConfig)
	installerWrite(t, interpreter, "#!/bin/sh\nif [ -n \"${TYPESAFE_API_KEY-}${SUBQ_API_KEY-}\" ]; then exit 99; fi\nif [ \"${PIP_CONFIG_FILE-}\" != "+shellQuote(os.DevNull)+" ]; then exit 98; fi\nprintf '%s' ready\n", 0o755)
	if output, err := runConsultationProcess(t.Context(), interpreter, "-I", "-B", "-c", "probe"); err != nil || string(output) != "ready" {
		t.Fatalf("credential environment inherited: %q, %v", output, err)
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile pip destination was used: %v", err)
	}
	filtered := consultationProcessEnvironment([]string{"TYPESAFE_API_KEY=secret", "SUBQ_API_KEY=secret", "PIP_CONFIG_FILE=" + hostileConfig})
	if !slices.Equal(filtered, []string{"PIP_CONFIG_FILE=" + os.DevNull}) {
		t.Fatalf("empty allowlist inherited credentials or pip config: %v", filtered)
	}
	installerWrite(t, interpreter, "#!/bin/sh\nprintf '%040000d' 1\n", 0o755)
	if _, err := runConsultationProcess(t.Context(), interpreter); !errors.Is(err, errConsultationOutputLimit) {
		t.Fatalf("unbounded output accepted: %v", err)
	}
	childOutput := filepath.Join(root, "delayed child write")
	installerWrite(t, interpreter, "#!/bin/sh\n(/bin/sleep 0.4; printf unsafe > "+shellQuote(childOutput)+") &\n/bin/sleep 10\n", 0o755)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := runConsultationProcess(ctx, interpreter); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("process deadline lost: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("subprocess or its child escaped the deadline")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(childOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a child process survived cancellation and wrote after failure: %v", err)
	}
}

func TestConsultationRuntimeEnvironmentProbeRequiresLocalPyYAML(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python probe validation requires an existing python3; no packages are installed")
	}
	root := Normalize(t.TempDir())
	for _, test := range []struct {
		name, version, module, status string
	}{
		{name: "local supported", version: "6.0.3", module: filepath.Join(root, "venv", "lib", "yaml", "__init__.py"), status: "ready"},
		{name: "global supported", version: "6.0.3", module: filepath.Join(root, "global", "yaml", "__init__.py"), status: "pyyaml_external"},
		{name: "local unsupported", version: "7.0.0", module: filepath.Join(root, "venv", "lib", "yaml", "__init__.py"), status: "pyyaml_unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix, _ := json.Marshal(filepath.Join(root, "venv"))
			module, _ := json.Marshal(test.module)
			version, _ := json.Marshal(test.version)
			// Inject a module record rather than importing or installing PyYAML.
			setup := "import sys, types\nsys.prefix = " + string(prefix) + "\nsys.modules['yaml'] = types.SimpleNamespace(__version__=" + string(version) + ", __file__=" + string(module) + ")\n"
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			output, err := runConsultationProcess(ctx, python, "-I", "-B", "-c", setup+consultationEnvironmentProbe)
			if err != nil {
				t.Fatal(err)
			}
			var probe consultationProbe
			if err := json.Unmarshal(output, &probe); err != nil || probe.Status != test.status {
				t.Fatalf("probe did not enforce local PyYAML/version: %#v, %v", probe, err)
			}
		})
	}
}
