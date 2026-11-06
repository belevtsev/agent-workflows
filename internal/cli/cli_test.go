package cli

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/distribution"
	"github.com/belevtsev/agent-workflows/internal/manageruntime"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

const fixtureSHA = "0123456789012345678901234567890123456789"

// errFixtureBundle identifies the injected payload provider. Lifecycle tests
// fake the service, so the CLI only passes the provider through.
var errFixtureBundle = errors.New("fixture bundle provider: CLI tests decode no payload")

func fixtureBundle(context.Context) (*bundled.Bundle, error) { return nil, errFixtureBundle }

// refusedNetwork fails the test on any HTTP request, so a lifecycle path that
// reaches the release distribution never touches the network.
type refusedNetwork struct{ t *testing.T }

func (r refusedNetwork) RoundTrip(request *http.Request) (*http.Response, error) {
	r.t.Errorf("network request attempted: %s %s", request.Method, request.URL.Redacted())
	return nil, errors.New("network access is refused in CLI tests")
}

type fakeService struct {
	action      string
	pending     bool
	created     int
	statusCalls int
}

func (f *fakeService) Execute(action string) (workflow.Object, error) {
	f.action = action
	return workflow.Object{"command": action, "pending": f.pending}, nil
}
func (f *fakeService) Status() (workflow.Object, error) {
	f.statusCalls++
	return workflow.Object{"installed": true}, nil
}

// untouched reports that no service was built or used.
func (f *fakeService) untouched() bool {
	return f.created == 0 && f.action == "" && f.statusCalls == 0
}

// testConfig isolates every root in a fresh temporary home, injects a fake
// payload provider, a fake runtime inspection and a network-refusing
// distribution client, and records the options the service receives.
func testConfig(t *testing.T) (Config, *bytes.Buffer, *workflow.Options, *fakeService) {
	t.Helper()
	out := new(bytes.Buffer)
	options := new(workflow.Options)
	service := new(fakeService)
	home := t.TempDir()
	config := Config{
		Version: "v1.2.3", Revision: fixtureSHA, BuiltAt: "now", Out: out, Err: io.Discard,
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return "/fixture/cwd", nil },
		Executable: func() (string, error) { return os.Executable() },
		Getenv:     func(string) string { return "" },
		Resolve:    func(string) (manageruntime.Locator, error) { return manageruntime.Locator{}, os.ErrNotExist },
		Inspect: func(_ context.Context, path string) (manageruntime.Candidate, error) {
			return manageruntime.Candidate{Path: path, Revision: fixtureSHA, Version: "v1.2.3", OS: "linux", Arch: "amd64"}, nil
		},
		NewService:   func(o workflow.Options) Service { *options = o; service.created++; return service },
		Bundle:       fixtureBundle,
		Distribution: &distribution.Client{HTTP: &http.Client{Transport: refusedNetwork{t}}},
	}
	return config, out, options, service
}

func homeOf(t *testing.T, config Config) string {
	t.Helper()
	home, err := config.HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

// samePath compares directories that may be reached through a symlinked
// temporary root.
func samePath(got, want string) bool {
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	return filepath.Clean(got) == filepath.Clean(want)
}

func TestOfflineHelpAndVersionHaveNoEnvironmentDependencies(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "install"}, {"help", "task", "start"}, {"help", "metrics", "summarize"}, {"help", "consult"}, {"help", "graphify", "query"}, {"consult", "--help"}, {"graphify", "build", "--help"}, {"doctor", "--help"}, {"task", "verify", "--help"}, {"metrics", "schema"}, {"setup", "--help"}, {"version"}, {"--manager-protocol"}, {"--release-identity"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, out, _, _ := testConfig(t)
			config.ReleaseIdentity = "release-identity-fixture"
			config.Executable = func() (string, error) { t.Fatal("environment resolved for help/version"); return "", nil }
			config.HomeDir = config.Executable
			config.Getenv = func(string) string { t.Fatal("credentials queried for help/version/capabilities"); return "" }
			config.Bundle = func(context.Context) (*bundled.Bundle, error) {
				t.Fatal("help/version decoded the workflow payload")
				return nil, nil
			}
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if len(out.Bytes()) == 0 {
				t.Fatal("empty output")
			}
			// D2 / Revision 3: the manager protocol stays cw-manager-v10.
			if args[0] == "--manager-protocol" && out.String() != "cw-manager-v10\n" {
				t.Fatalf("wrong bootstrap protocol: %q", out.String())
			}
			if args[0] == "--release-identity" && out.String() != config.ReleaseIdentity+"\n" {
				t.Fatalf("wrong release identity: %q", out.String())
			}
			if args[0] == "version" {
				var report map[string]any
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if len(report) != 5 || report["revision"] != fixtureSHA {
					t.Fatalf("incompatible version protocol: %v", report)
				}
				expected := `{"arch":"` + runtime.GOARCH + `","built_at":"now","os":"` + runtime.GOOS + `","revision":"` + fixtureSHA + `","version":"v1.2.3"}` + "\n"
				if out.String() != expected {
					t.Fatalf("legacy bootstrap wire order changed: %q", out.String())
				}
			}
		})
	}
}

// R1.1: an installed manager takes its roots from the standalone locator,
// never from the working directory or the caller's home.
func TestInstalledCommandResolvesLocatorInsteadOfCWD(t *testing.T) {
	config, out, options, service := testConfig(t)
	owned := t.TempDir()
	home, state := filepath.Join(owned, "home"), filepath.Join(owned, "state")
	for _, directory := range []string{home, state} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	config.Resolve = func(string) (manageruntime.Locator, error) {
		return manageruntime.Locator{Version: 3, Home: home, State: state, CommandPath: filepath.Join(home, ".local", "bin", "cw")}, nil
	}
	config.WorkingDir = func() (string, error) { t.Fatal("installed manager used cwd"); return "", nil }
	if err := Execute(t.Context(), []string{"status"}, config); err != nil {
		t.Fatal(err)
	}
	if !samePath(options.Home, home) || !samePath(options.State, state) {
		t.Fatalf("locator roots not used: home %q state %q, want %q %q", options.Home, options.State, home, state)
	}
	if service.action != "status" && service.statusCalls == 0 {
		t.Fatal("status did not reach the service")
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if report["action"] != "status" {
		t.Fatalf("incompatible report: %+v", report)
	}
}

func TestHostRootsPreferFlagsThenRecordsThenEnvironment(t *testing.T) {
	config, _, _, _ := testConfig(t)
	config.Getenv = func(key string) string { return "/env/" + key }
	// Journal paths must already be normalized (macOS maps /var to /private/var).
	state := workflow.Normalize(t.TempDir())
	if codex, claude := hostRoots(config, "/home", state, "", ""); codex != "/env/CODEX_HOME" || claude != "/env/CLAUDE_CONFIG_DIR" {
		t.Fatalf("unrecorded hosts: %q, %q", codex, claude)
	}
	journal := `{"version":4,"command":"setup","home":"/home","codex_home":"/recorded/codex","claude_home":"/recorded/claude","state_dir":"` + state + `","config_dir":"/recorded/config","operations":[]}`
	if err := os.WriteFile(filepath.Join(state, "journal.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if codex, claude := hostRoots(config, "/home", state, "", ""); codex != "/recorded/codex" || claude != "/recorded/claude" {
		t.Fatalf("recorded hosts: %q, %q", codex, claude)
	}
	if codex, claude := hostRoots(config, "/home", state, "/flag/codex", "/flag/claude"); codex != "/flag/codex" || claude != "/flag/claude" {
		t.Fatalf("explicit roots: %q, %q", codex, claude)
	}
	// R2.4/R2.5: a version-3 journal is a legacy installation, which read-only
	// root resolution treats as no record at all.
	legacy := workflow.Normalize(t.TempDir())
	journal = `{"version":3,"command":"setup","home":"/home","codex_home":"/legacy/codex","claude_home":"/legacy/claude","state_dir":"` + legacy + `","operations":[]}`
	if err := os.WriteFile(filepath.Join(legacy, "journal.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if codex, claude := hostRoots(config, "/home", legacy, "", ""); codex != "/env/CODEX_HOME" || claude != "/env/CLAUDE_CONFIG_DIR" {
		t.Fatalf("legacy journal roots used: %q, %q", codex, claude)
	}
}

// R1.5: status reports the exact credential_sources for the environment
// without exposing keys and without the removed *_credential_present,
// integrations and skill_prerequisites keys.
func TestStatusReportsDecisionCredentialsWithoutExposingKeys(t *testing.T) {
	cases := []struct {
		name              string
		jev, subq, openai bool
	}{
		{"none", false, false, false},
		{"Jev only", true, false, false},
		{"SubQ only", false, true, false},
		{"OpenAI only", false, false, true},
		{"Jev and SubQ", true, true, false},
		{"Jev and OpenAI", true, false, true},
		{"SubQ and OpenAI", false, true, true},
		{"all", true, true, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config, out, _, _ := testConfig(t)
			service := new(registeredService)
			config.NewService = func(workflow.Options) Service { service.created++; return service }
			config.Getenv = func(name string) string {
				switch {
				case name == "TYPESAFE_API_KEY" && test.jev:
					return "fixture-secret-jev"
				case name == "SUBQ_API_KEY" && test.subq:
					return "fixture-secret-subq"
				case name == "OPENAI_API_KEY" && test.openai:
					return "fixture-secret-openai"
				default:
					return ""
				}
			}
			if err := Execute(t.Context(), []string{"status"}, config); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "fixture-secret") {
				t.Fatal("status exposed a decision credential")
			}
			var report map[string]any
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if report["action"] != "status" {
				t.Fatalf("not a lifecycle report: %+v", report)
			}
			if found := removedReportKeys(report); len(found) != 0 {
				t.Fatalf("status report kept removed keys %v: %s", found, out)
			}
			// R1.5 rev 3: the fresh home stores no key, so each source follows
			// the environment alone.
			if report["credential_storage_status"] != "inspected" {
				t.Fatalf("credential store not inspected: %v", report["credential_storage_status"])
			}
			want := map[string]any{
				"TYPESAFE_API_KEY": credentialSource(test.jev),
				"SUBQ_API_KEY":     credentialSource(test.subq),
				"OPENAI_API_KEY":   credentialSource(test.openai),
			}
			if got := report["credential_sources"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("credential_sources = %v, want %v", got, want)
			}
		})
	}
}

// credentialSource is the R1.5 rev 3 report entry for a key that is not
// stored and is or is not set in the environment.
func credentialSource(environment bool) map[string]any {
	source := "none"
	if environment {
		source = "environment"
	}
	return map[string]any{"environment": environment, "stored": false, "effective": environment, "source": source}
}

func TestInstallAliasAndDefaultEnroll(t *testing.T) {
	for _, args := range [][]string{{}, {"install"}, {"setup"}, {"--home", "{custom}", "install", "--shell", "none"}, {"install", "--apply"}, {"--apply", "setup"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _, options, service := testConfig(t)
			args = slices.Clone(args)
			if index := slices.Index(args, "{custom}"); index >= 0 {
				args[index] = t.TempDir()
			}
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if service.action != "setup" || !options.Apply {
				t.Fatalf("install must enroll by default: %s %+v", service.action, options)
			}
			// R1.1: the standalone manager receives the binary's payload provider.
			if options.Bundle == nil {
				t.Fatal("install ran without the embedded bundle")
			}
			if _, err := options.Bundle(t.Context()); !errors.Is(err, errFixtureBundle) {
				t.Fatalf("install received a different bundle provider: %v", err)
			}
		})
	}
}

func TestDryRunInstallDoesNotApply(t *testing.T) {
	config, _, options, service := testConfig(t)
	if err := Execute(t.Context(), []string{"install", "--dry-run"}, config); err != nil {
		t.Fatal(err)
	}
	if options.Apply || service.action != "setup" {
		t.Fatalf("wrong flags: %+v action=%s", options, service.action)
	}
}

func TestUsageFailuresHaveExitCodeTwo(t *testing.T) {
	for _, args := range [][]string{
		{"consult-jev"}, {"update", "--shell", "bash"}, {"install", "--shell", "fish"}, {"install", "--dry-run", "--apply"}, {"status", "extra"}, {"--missing"}, {"help", "unknown"},
		// R1.2: removed root flags and subcommands are unknown.
		{"--source", "/checkout"}, {"install", "--source", "/checkout"}, {"status", "--source", "/checkout"},
		{"--migrate-from", "/former"}, {"install", "--migrate-from", "/former"},
		{"install", "--no-checkout"}, {"--no-checkout"},
		{"install", "--bootstrap"}, {"--bootstrap"},
		{"update", "--prepare-only"}, {"--prepare-only"},
		{"validate"}, {"validate", "--json"},
		// D3 (Removed): the OpenAI participation flags are unknown.
		{"consult", "--request", "r", "--output", "o", "--with-openai"}, {"consult", "--request", "r", "--output", "o", "--without-openai"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _, _, service := testConfig(t)
			err := Execute(t.Context(), args, config)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("%v: %v code %d", args, err, ExitCode(err))
			}
			if !service.untouched() {
				t.Fatalf("%v mutated service", args)
			}
		})
	}
}

func TestPersonalAdoptionIsAnExplicitInstallOption(t *testing.T) {
	for _, action := range []string{"", "install", "setup"} {
		t.Run(action, func(t *testing.T) {
			config, _, options, service := testConfig(t)
			args := []string{"--adopt-personal-skills", "--dry-run"}
			if action != "" {
				args = append([]string{action}, args...)
			}
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if !options.AdoptPersonalSkills || options.Apply || service.action != "setup" {
				t.Fatalf("adoption option lost: %+v action=%s", options, service.action)
			}
		})
	}
	for _, action := range []string{"update", "status", "rollback", "recover", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			config, _, _, service := testConfig(t)
			config.Executable = func() (string, error) { t.Fatal("resolved environment for an invalid flag"); return "", nil }
			err := Execute(t.Context(), []string{action, "--adopt-personal-skills"}, config)
			if err == nil || ExitCode(err) != 2 || !service.untouched() {
				t.Fatalf("unsupported adoption flag reached service: %v action=%s", err, service.action)
			}
		})
	}
}
func TestCanceledContextReachesInstaller(t *testing.T) {
	config, _, options, _ := testConfig(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// The injected service deliberately succeeds: observe propagation directly.
	if err := Execute(ctx, []string{"status"}, config); err != nil {
		t.Fatal(err)
	}
	if options.Context == nil || !errors.Is(options.Context.Err(), context.Canceled) || ExitCode(context.Canceled) != 130 {
		t.Fatal("cancellation context lost")
	}
}

func TestOwnedLayoutMissingLocatorNeverGuessesDefaults(t *testing.T) {
	config, _, _, service := testConfig(t)
	config.Resolve = func(string) (manageruntime.Locator, error) {
		return manageruntime.Locator{}, manageruntime.ErrLocatorMissing
	}
	config.WorkingDir = func() (string, error) { t.Fatal("guessed cwd for installed manager"); return "", nil }
	config.HomeDir = func() (string, error) { t.Fatal("guessed home for installed manager"); return "", nil }
	if err := Execute(t.Context(), []string{"recover", "--home", "/explicit/home"}, config); !errors.Is(err, manageruntime.ErrLocatorMissing) {
		t.Fatalf("missing locator silently defaulted: %v", err)
	}
	if !service.untouched() {
		t.Fatal("service executed with guessed roots")
	}
}
