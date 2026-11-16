package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/onboarding"
)

// The only host executed by these tests is a copy of this test binary.
func TestMain(m *testing.M) {
	if os.Getenv("KITSU_TEST_ONBOARDING_CHILD") == "1" {
		var input []byte
		if os.Getenv("KITSU_TEST_ONBOARDING_TTY") != "1" {
			var err error
			input, err = io.ReadAll(os.Stdin)
			if err != nil {
				os.Exit(81)
			}
		}
		data, err := json.Marshal(struct {
			Args  []string
			Input string
		}{os.Args[1:], string(input)})
		if err != nil || os.WriteFile(os.Getenv("KITSU_TEST_ONBOARDING_CAPTURE"), data, 0o600) != nil {
			os.Exit(82)
		}
		fmt.Fprint(os.Stdout, "fake host output")
		fmt.Fprint(os.Stderr, "fake host diagnostic")
		if os.Getenv("KITSU_TEST_ONBOARDING_EXIT") == "wait" {
			time.Sleep(time.Hour)
		}
		code, _ := strconv.Atoi(os.Getenv("KITSU_TEST_ONBOARDING_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func onboardingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(name, dir)
	}
	t.Setenv("KITSU_HOST", "")
	return dir
}

func onboardingFakeHost(t *testing.T) (program, capture string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "native host directory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	program, capture = filepath.Join(dir, name), filepath.Join(dir, "host-argv.json")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KITSU_TEST_ONBOARDING_CHILD", "1")
	t.Setenv("KITSU_TEST_ONBOARDING_CAPTURE", capture)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return program, capture
}

func onboardingReadyFixture(t *testing.T) (program, capture string) {
	t.Helper()
	dir := onboardingFixture(t)
	program, capture = onboardingFakeHost(t)
	name := "kitsu"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(program), name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(program)+string(os.PathListSeparator)+os.Getenv("PATH"))
	plugin := filepath.Join(dir, "plugins", "cache", "kitsu", "kitsu", "1")
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "config.toml"), "[plugins.\"kitsu@kitsu\"]\nenabled = true\n")
	write(filepath.Join(plugin, ".codex-plugin", "plugin.json"), `{"name":"kitsu","version":"1","skills":"./skills","hooks":"./hooks.json"}`)
	write(filepath.Join(plugin, "skills", "task", "SKILL.md"), "---\nname: task\ndescription: Coordinate a Kitsu task.\n---\nUse the host workflow.\n")
	events := map[string]any{}
	for _, event := range []string{"SessionStart", "PreToolUse", "SubagentStop", "SessionEnd"} {
		command := "tokens record"
		if event == "SessionStart" {
			command = "context"
		}
		if event == "PreToolUse" {
			command = "guard"
		}
		events[event] = []any{map[string]any{"matcher": "*", "hooks": []any{map[string]string{"type": "command", "command": "kitsu " + command + " --host codex"}}}}
	}
	hooks, err := json.Marshal(map[string]any{"hooks": events})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(plugin, "hooks.json"), string(hooks))
	return program, capture
}

func TestOnboardingReadySetupRequiresInteractiveTerminal(t *testing.T) {
	_, capture := onboardingReadyFixture(t)
	var out, diagnostics bytes.Buffer
	cfg := Config{In: strings.NewReader(""), Out: &out, Err: &diagnostics}
	if err := Execute(t.Context(), []string{"agent", "check", "--host", "codex", "--json"}, cfg); err != nil {
		t.Fatalf("fixture has local blockers: %v; %s", err, &out)
	}
	var report onboarding.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.Ready {
		t.Fatalf("fixture is not ready: %s, %v", &out, err)
	}
	out.Reset()
	err := Execute(t.Context(), []string{"task", "start", "fix the test", "--host", "codex"}, cfg)
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "--print or --json") || !strings.Contains(err.Error(), "kitsu task start --help") || out.Len() != 0 {
		t.Fatalf("nonterminal launch did not explain handoff: %v; %s; %s", err, &out, &diagnostics)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("nonterminal command executed host: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Execute(ctx, []string{"task", "start", "request", "--host", "codex"}, cfg); ExitCode(err) != 130 {
		t.Fatalf("cancelled command did not preserve interruption: %v", err)
	}
}

func TestOnboardingReadOnlyModesNeverStartHostOrCreateTask(t *testing.T) {
	dir := onboardingFixture(t)
	program, capture := onboardingFakeHost(t)
	t.Setenv("PATH", filepath.Dir(program)+string(os.PathListSeparator)+os.Getenv("PATH"))
	request := "Inspect $(touch marker); C:\\project\\file 'literally'"
	for _, test := range []struct {
		name string
		args []string
		code int
		json bool
	}{
		{name: "agent json", args: []string{"agent", "check", "--host", "codex", "--json"}, code: 5, json: true},
		{name: "agent text", args: []string{"agent", "check", "--host", "codex"}, code: 5},
		{name: "handoff json", args: []string{"task", "start", request, "--host", "codex", "--json"}, json: true},
		{name: "handoff text", args: []string{"task", "start", request, "--host", "codex", "--print"}},
		{name: "blocked start", args: []string{"task", "start", request, "--host", "codex"}, code: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			err := Execute(t.Context(), test.args, Config{Version: "test-version", In: strings.NewReader(""), Out: &out, Err: &diagnostics})
			if ExitCode(err) != test.code {
				t.Fatalf("wrong status: %v; %s; %s", err, &out, &diagnostics)
			}
			if test.json {
				var report taskHandoff
				if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Ready || report.Host != "codex" || report.HostExecutable != program || len(report.Checks) == 0 || diagnostics.Len() != 0 {
					t.Fatalf("JSON is incomplete or contaminated: %s; %s; %v", &out, &diagnostics, err)
				}
				if test.args[0] == "task" && !strings.HasSuffix(report.Prompt, request) {
					t.Fatalf("handoff omitted request: %s", &out)
				}
			} else if !strings.Contains(out.String()+diagnostics.String(), "Next:") {
				t.Fatalf("no remediation: %s; %s", &out, &diagnostics)
			}
		})
	}
	for _, args := range [][]string{{"agent", "check", "--help"}, {"task", "start", "--help"}, {"help", "task", "start"}, {"__complete", "--", "task", "start", "--host", "co"}} {
		var out bytes.Buffer
		if err := Execute(t.Context(), args, Config{Out: &out}); err != nil || out.Len() == 0 {
			t.Fatalf("help/completion %q: %v; %s", args, err, &out)
		}
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("read-only commands executed fake host: %v", err)
	}
	for _, path := range []string{"marker", "openspec", "kitsu.toml", ".git", "kitsu-cli.toml"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Errorf("read-only command created %s: %v", path, err)
		}
	}
}

func TestOnboardingHostChoiceUsesPreferences(t *testing.T) {
	onboardingFixture(t)
	for _, test := range []struct {
		name, preference string
		args             []string
		code             int
	}{
		{name: "missing selection", args: []string{"task", "start", "request", "--json"}, code: 2},
		{name: "saved selection", preference: "codex", args: []string{"task", "start", "request", "--json"}},
		{name: "explicit selection", preference: "invalid", args: []string{"task", "start", "request", "--host", "claude", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KITSU_HOST", test.preference)
			var out bytes.Buffer
			err := Execute(t.Context(), test.args, Config{Out: &out})
			if ExitCode(err) != test.code {
				t.Fatalf("host selection: %v; %s", err, &out)
			}
			if err == nil {
				var got taskHandoff
				if json.Unmarshal(out.Bytes(), &got) != nil || got.Host == "" || got.Host == "invalid" {
					t.Fatalf("host preference ignored: %s", &out)
				}
			}
		})
	}
}

func TestOnboardingParsesOneLiteralRequest(t *testing.T) {
	t.Parallel()
	request := "  Fix 'quotes', $HOME, `id`, $(touch marker); C:\\work\\file\nsecond line  "
	for _, test := range []struct {
		name  string
		args  []string
		start bool
		code  int
	}{
		{name: "literal request", args: []string{request, "--host", "codex", "--json"}, start: true},
		{name: "literal marker", args: []string{"--host", "claude", "--", request}, start: true},
		{name: "empty", args: []string{" ", "--host", "codex"}, start: true, code: 2},
		{name: "missing request", args: []string{"--host", "codex"}, start: true, code: 2},
		{name: "unquoted words", args: []string{"fix", "this", "--host", "codex"}, start: true, code: 2},
		{name: "nul", args: []string{"fix\x00this", "--host", "codex"}, start: true, code: 2},
		{name: "missing host", args: []string{request}, start: true, code: 2},
		{name: "invalid host", args: []string{request, "--host", "other"}, start: true, code: 2},
		{name: "repeated host", args: []string{request, "--host", "codex", "--host", "claude"}, start: true, code: 2},
		{name: "conflicting output", args: []string{request, "--host", "codex", "--print", "--json"}, start: true, code: 2},
		{name: "unknown flag", args: []string{request, "--host", "codex", "--auto"}, start: true, code: 2},
		{name: "extra host value", args: []string{request, "--host", "codex", "claude"}, start: true, code: 2},
		{name: "check", args: []string{"--host", "codex", "--json"}},
		{name: "check operand", args: []string{"unexpected", "--host", "codex"}, code: 2},
		{name: "check print", args: []string{"--host", "codex", "--print"}, code: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOnboardingArgs(test.args, test.start)
			if ExitCode(err) != test.code {
				t.Fatalf("code=%d, want %d: %v", ExitCode(err), test.code, err)
			}
			if err == nil && test.start && got.request != request {
				t.Fatalf("request changed: %q", got.request)
			}
		})
	}
}

func TestOnboardingHandoffKeepsTheHostAsOrchestrator(t *testing.T) {
	t.Parallel()
	request := "--dangerous 'literal' $HOME $(command)\nnext line"
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			report := onboarding.Report{Host: host, Ready: false, Checks: []onboarding.Check{}}
			handoff := prepareTaskHandoff(report, request)
			if len(handoff.Argv) != 2 || handoff.Argv[0] != host || handoff.Argv[1] != handoff.Prompt || !strings.HasSuffix(handoff.Prompt, request) {
				t.Fatalf("incomplete handoff: %+v", handoff)
			}
			if host == "claude" && handoff.Prompt != "/kitsu:task "+request || host == "codex" && !strings.HasPrefix(handoff.Prompt, "Use the task skill from the Kitsu plugin") {
				t.Fatalf("unsafe or ambiguous prompt: %q", handoff.Prompt)
			}
			data, err := json.Marshal(handoff)
			if err != nil || !bytes.Contains(data, []byte(`"ready":false`)) || bytes.Contains(data, []byte(`"stage"`)) {
				t.Fatalf("handoff invented task state: %s, %v", data, err)
			}
		})
	}
}

func TestOnboardingLaunchPreservesArgvStreamsAndExit(t *testing.T) {
	program, capture := onboardingFakeHost(t)
	request := "Change 'quotes' ; touch marker $(id) $HOME C:\\work\\file\nnext"
	for _, host := range []string{"codex", "claude"} {
		for _, code := range []int{0, 1, 2, 17, 130} {
			t.Run(fmt.Sprintf("%s exit %d", host, code), func(t *testing.T) {
				t.Setenv("KITSU_TEST_ONBOARDING_EXIT", strconv.Itoa(code))
				handoff := prepareTaskHandoff(onboarding.Report{Host: host, HostExecutable: program, Ready: true}, request)
				var out, diagnostics bytes.Buffer
				err := launchTaskHost(t.Context(), Config{In: strings.NewReader("terminal input"), Out: &out, Err: &diagnostics}, handoff.Argv)
				if ExitCode(err) != code || out.String() != "fake host output" || diagnostics.String() != "fake host diagnostic" {
					t.Fatalf("lost status or stdio: %v; %q; %q", err, &out, &diagnostics)
				}
				var got struct {
					Args  []string
					Input string
				}
				data, err := os.ReadFile(capture)
				if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got.Args, handoff.Argv[1:]) || got.Input != "terminal input" {
					t.Fatalf("host argv or input changed: %s, %v", data, err)
				}
			})
		}
	}
	if err := launchTaskHost(t.Context(), Config{}, []string{"codex", request}); ExitCode(err) != 1 {
		t.Fatalf("relative executable accepted: %v", err)
	}
	for _, extension := range []string{".cmd", ".CMD", ".bat"} {
		if err := launchTaskHost(t.Context(), Config{}, []string{program + extension, request}); ExitCode(err) != 1 || !strings.Contains(err.Error(), "--print") {
			t.Fatalf("shell wrapper accepted or lacks manual handoff: %v", err)
		}
	}
}

type onboardingReadyWriter struct {
	once  sync.Once
	ready chan struct{}
}

func (w *onboardingReadyWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(data), nil
}

func TestOnboardingLaunchHonorsCancellation(t *testing.T) {
	program, capture := onboardingFakeHost(t)
	t.Setenv("KITSU_TEST_ONBOARDING_EXIT", "wait")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := &onboardingReadyWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- launchTaskHost(ctx, Config{In: strings.NewReader(""), Out: ready, Err: io.Discard}, []string{program, "fixed task prompt"})
	}()
	select {
	case <-ready.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("fake host did not start")
	}
	cancel()
	select {
	case err := <-done:
		if ExitCode(err) != 130 {
			t.Fatalf("cancelled host code: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled host was not stopped")
	}
	if err := os.Remove(capture); err != nil {
		t.Fatal(err)
	}
	if err := launchTaskHost(ctx, Config{}, []string{program, "fixed prompt"}); ExitCode(err) != 130 {
		t.Fatalf("pre-cancelled launch: %v", err)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("pre-cancelled context executed host: %v", err)
	}
}
