package cli

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fixtureSHA = "0123456789012345678901234567890123456789"

// testConfig isolates every root in a fresh temporary home.
func testConfig(t *testing.T) (Config, *bytes.Buffer) {
	t.Helper()
	out := new(bytes.Buffer)
	home := t.TempDir()
	config := Config{
		Version: "v1.2.3", Revision: fixtureSHA, BuiltAt: "now", Out: out, Err: io.Discard,
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return "/fixture/cwd", nil },
		Executable: func() (string, error) { return os.Executable() },
		Getenv:     func(string) string { return "" },
	}
	return config, out
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
	for _, args := range [][]string{{"help"}, {"help", "install"}, {"help", "task", "start"}, {"help", "metrics", "summarize"}, {"help", "consult"}, {"help", "graphify", "query"}, {"consult", "--help"}, {"graphify", "build", "--help"}, {"doctor", "--help"}, {"task", "verify", "--help"}, {"metrics", "schema"}, {"install", "--help"}, {"version"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, out := testConfig(t)
			config.Executable = func() (string, error) { t.Fatal("environment resolved for help/version"); return "", nil }
			config.HomeDir = config.Executable
			config.Getenv = func(string) string { t.Fatal("credentials queried for help/version/capabilities"); return "" }
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if len(out.Bytes()) == 0 {
				t.Fatal("empty output")
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
					t.Fatalf("version wire order changed: %q", out.String())
				}
			}
		})
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
		// TC-RM-1 RM-1: the v2 migration command was removed and is unknown like the others.
		{"migrate-v2"}, {"migrate-v2", "--dry-run"},
		// D3 (Removed): the OpenAI participation flags are unknown.
		{"consult", "--request", "r", "--output", "o", "--with-openai"}, {"consult", "--request", "r", "--output", "o", "--without-openai"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _ := testConfig(t)
			err := Execute(t.Context(), args, config)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("%v: %v code %d", args, err, ExitCode(err))
			}
			home := homeOf(t, config)
			if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
				t.Fatalf("%v wrote under home: %v %v", args, entries, err)
			}
		})
	}
}
