package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
)

func TestUXHelpAtEveryDepthIsIndependentOfRuntimeState(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("KITSU_HOST", "invalid-host")
	if err := os.MkdirAll(filepath.Join(dir, "kitsu"), 0o700); err != nil {
		t.Fatal(err)
	}
	badConfig := filepath.Join(dir, "kitsu", "config.toml")
	if err := os.WriteFile(badConfig, []byte("not = valid [ TOML"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range uxCommands() {
		path := strings.Fields(command.path)
		nestedHelp := append(append([]string{}, path[:len(path)-1]...), "help", path[len(path)-1])
		for _, args := range [][]string{
			append(append([]string{}, path...), "--help"),
			append(append([]string{}, path...), "-h"),
			append([]string{"help"}, path...),
			nestedHelp,
		} {
			var out, diagnostics bytes.Buffer
			config := Config{Out: &out, Err: &diagnostics, In: strings.NewReader(""),
				Editor: func(context.Context, *UXEditor) error { t.Fatal("help launched an editor"); return nil },
				NativeExecute: func(context.Context, *advisers.NativeCall) advisers.Execution {
					t.Fatal("help invoked a provider")
					return advisers.Execution{}
				},
			}
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatalf("help %q failed without runtime state: %v", args, err)
			}
			if !strings.Contains(out.String(), "usage: kitsu "+command.path) || !strings.Contains(out.String(), "Examples:") || diagnostics.Len() != 0 {
				t.Fatalf("help %q did not describe its leaf:\n%s\n%s", args, &out, &diagnostics)
			}
			for _, flag := range command.flags {
				if !strings.Contains(out.String(), "--"+flag.name) || !strings.Contains(strings.Join(strings.Fields(out.String()), " "), flag.description) {
					t.Fatalf("%s help omitted flag %s", command.path, flag.name)
				}
			}
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"help", "--all"}, {"role"}, {"skill"}, {"config"}, {"completion"}, {"roles", "set", "--help"}, {"advisers", "resolve", "--help"}, {"change", "status", "--help"}, {"tokens", "record", "--help"}} {
		var out, diagnostics bytes.Buffer
		if err := Execute(t.Context(), args, Config{Out: &out, Err: &diagnostics}); err != nil || out.Len() == 0 || diagnostics.Len() != 0 {
			t.Fatalf("help %q: %v; %s; %s", args, err, &out, &diagnostics)
		}
	}
	data, err := os.ReadFile(badConfig)
	if err != nil || string(data) != "not = valid [ TOML" {
		t.Fatalf("help rewrote malformed configuration: %q, %v", data, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("help created runtime files outside Git: %v; %v", entries, err)
	}
}

func TestUXLegacyNestedHelpKeepsActualArgumentRules(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path, usage, detail string
	}{
		{"roles set", "<role> --host <host>", "saved preferences apply to preferred commands"},
		{"skills resolve", "<role> --host <host>", "saved preferences apply to preferred commands"},
		{"advisers bind", "--task <task> --workflow <workflow>", "required task name supplied with --task"},
		{"advisers status", "--task <task>", "required task name supplied with --task"},
		{"advisers check", "--task <task>", "required task name supplied with --task"},
		{"advisers resolve", "<id> --task <task>", "--disposition accept --reason"},
		{"change status", "[<task>]", "list all full task folders"},
		{"tokens record", "[options]", "legacy default is claude"},
	} {
		var out, diagnostics bytes.Buffer
		args := append(strings.Fields(test.path), "--help")
		if err := Execute(t.Context(), args, Config{Out: &out, Err: &diagnostics}); err != nil {
			t.Fatal(err)
		}
		normalized := strings.Join(strings.Fields(out.String()), " ")
		if !strings.HasPrefix(out.String(), "usage: kitsu "+test.path+" "+test.usage) || !strings.Contains(normalized, test.detail) || strings.Contains(out.String(), "Host: --host >") {
			t.Fatalf("legacy help teaches another protocol for %s:\n%s", test.path, &out)
		}
	}
	for _, args := range [][]string{{"help", "__complete"}, {"__complete", "--help"}} {
		var out bytes.Buffer
		if err := Execute(t.Context(), args, Config{Out: &out}); err != nil || !strings.Contains(out.String(), "words-base64") {
			t.Fatalf("internal callback has no help: %v; %s", err, &out)
		}
	}
}

func TestUXUsageErrorsNameTheSpecificHelpAndNeverGuessExecution(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("KITSU_HOST", "")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"role", "ste"}, `Did you mean "role set"?`},
		{[]string{"rol"}, `Did you mean "role"?`},
		{[]string{"config", "set"}, "kitsu config set --help"},
		{[]string{"task", "show"}, "kitsu task show --help"},
		{[]string{"role", "set", "--host", "codex"}, "kitsu role set --help"},
		{[]string{"usage", "record"}, "kitsu usage record --help"},
	} {
		var out, diagnostics bytes.Buffer
		err := Execute(t.Context(), test.args, Config{Out: &out, Err: &diagnostics, In: strings.NewReader("")})
		if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), test.want) || out.Len() != 0 {
			t.Fatalf("%q: %v, output %q", test.args, err, &out)
		}
	}
}

func TestUXCanonicalMutationsAndAliasesReceiveGuardProtection(t *testing.T) {
	t.Parallel()
	_, root := guardRepo(t, "")
	for _, command := range []string{
		"kitsu role set developer --host codex --model custom",
		"kitsu role edit developer", "kitsu role reset developer --host codex",
		"kitsu skill attach go-backend --role developer --host codex",
		"kitsu skill detach missing --role developer --host codex",
		"kitsu skill clear --role developer --host codex", "kitsu skill reset --role developer --host codex",
		"kitsu config set host codex", "kitsu config reset host", "kitsu config edit",
		"kitsu check add unit -- go test ./...", "kitsu add unit -- go test ./...",
		"kitsu gate init --observe", "kitsu gate uninstall",
		`env FOO=bar bash -lc 'kitsu skill clear --role developer --host codex'`,
		`& 'C:\tools\kitsu.exe' role reset developer --host codex`,
	} {
		call := toolCall{Tool: "Bash", Dir: root}
		call.Input.Command = command
		if why, _ := guardReason(t.Context(), call); why == "" {
			t.Errorf("mutation bypassed guard: %s", command)
		}
	}
	for _, command := range []string{
		"kitsu role list", "kitsu role show developer", "kitsu skill list --host codex",
		"kitsu check list", "kitsu gate status", "kitsu gate init",
		"kitsu role set --help", "kitsu role edit --help", "kitsu config set --help",
		"kitsu gate uninstall --help", "kitsu uninstall --help", "kitsu init --observe --help",
		"echo kitsu skill attach foo --role developer",
	} {
		call := toolCall{Tool: "Bash", Dir: root}
		call.Input.Command = command
		if why, _ := guardReason(t.Context(), call); why != "" {
			t.Errorf("read-only request blocked: %s: %s", command, why)
		}
	}
}

func TestTaskScopeChoicesAreDiscoverableAndValidatedBeforeOpeningRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct{ verb, flag, choices string }{
		{"start", "until", "plan, implement, review, verify, deliver"},
		{"open", "until", "plan, implement, review, verify, deliver"},
		{"extend", "until", "plan, implement, review, verify, deliver"},
		{"open", "entry", "plan, implement, review, verify"},
	} {
		var out bytes.Buffer
		cfg := Config{Out: &out}
		if err := Execute(t.Context(), []string{"task", tc.verb, "--help"}, cfg); err != nil {
			t.Fatal(err)
		}
		help := strings.Join(strings.Fields(out.String()), " ")
		flagHelp := "--until <outcome> finish at "
		if tc.flag == "entry" {
			flagHelp = "--entry <stage> start at "
		}
		if !strings.Contains(help, flagHelp+tc.choices) || (tc.verb == "start" && !strings.Contains(help, "kitsu task start --resume")) {
			t.Errorf("task %s help lacks choices or resume example", tc.verb)
		}
		for _, value := range []string{"", "typo"} {
			args := []string{"task", tc.verb, "sample", "--" + tc.flag + "=" + value}
			if tc.verb == "start" {
				args = append(args, "--host", "codex", "--print")
			}
			err := Execute(t.Context(), args, cfg)
			if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "--"+tc.flag) || !strings.Contains(err.Error(), tc.choices) {
				t.Errorf("%q: got %v, want choices for --%s", args, err, tc.flag)
			}
		}
	}
	err := Execute(t.Context(), []string{"task", "open", "sample", "--request", "test"}, Config{})
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "requires --until") {
		t.Errorf("missing outcome: %v", err)
	}
}
