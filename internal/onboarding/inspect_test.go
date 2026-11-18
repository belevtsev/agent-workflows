package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/gate"
)

type fixture struct {
	t                                                           *testing.T
	base, home, dir, hostDir, pluginDir, system, host, bin, git string
}

func newFixture(t *testing.T, host string) fixture {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	f := fixture{t: t, base: base, home: filepath.Join(base, "home"), dir: filepath.Join(base, "project"), hostDir: filepath.Join(base, host), system: filepath.Join(base, "system"), host: host, bin: filepath.Join(base, "bin"), git: git}
	for _, dir := range []string{f.home, f.dir, f.hostDir, f.system, f.bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", f.home)
	t.Setenv("USERPROFILE", f.home)
	t.Setenv("CODEX_HOME", filepath.Join(base, "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(base, "claude"))
	t.Setenv("PATH", f.bin+string(os.PathListSeparator)+filepath.Dir(git))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"kitsu", "codex", "claude"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		f.write(filepath.Join(f.bin, name), "#!/bin/sh\nprintf executed > '"+filepath.ToSlash(filepath.Join(base, "executed"))+"'\n", 0o755)
	}
	f.pluginDir = filepath.Join(f.hostDir, "plugins", "cache", "kitsu", "kitsu", "1")
	f.install()
	return f
}

func (f fixture) write(path, body string, mode os.FileMode) {
	f.t.Helper()
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.base, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) json(path string, value any) {
	f.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	f.write(path, string(data), 0o644)
}

func (f fixture) install() {
	f.t.Helper()
	m := map[string]any{"name": "kitsu", "version": "fixture", "skills": "./skills"}
	if f.host == "codex" {
		m["hooks"] = "./hooks/codex-hooks.json"
		f.write(filepath.Join(f.hostDir, "config.toml"), "[plugins.\"kitsu@kitsu\"]\nenabled = true\n", 0o644)
	} else {
		f.write(filepath.Join(f.hostDir, "settings.json"), `{"enabledPlugins":{"kitsu@kitsu":true}}`, 0o644)
		f.inventory(2, "user", f.dir, f.pluginDir)
	}
	f.json(filepath.Join(f.pluginDir, "."+f.host+"-plugin", "plugin.json"), m)
	f.write(filepath.Join(f.pluginDir, "skills", "task", "SKILL.md"), "---\nname: task\ndescription: Handle a Kitsu task.\n---\n<!-- kitsu-task-harness: 3 -->\nCoordinate the task.\n", 0o644)
	f.hooks("", "", false)
}

func (f fixture) inventory(version int, scope, project, path string) {
	f.t.Helper()
	install := map[string]string{"scope": scope, "projectPath": project, "installPath": path}
	var record any = []any{install}
	if version == 1 {
		record = install
	}
	f.json(filepath.Join(f.hostDir, "plugins", "installed_plugins.json"), map[string]any{"version": version, "plugins": map[string]any{"kitsu@kitsu": record}})
}

func (f fixture) hooks(omit, matcher string, disabled bool) {
	f.t.Helper()
	events := map[string]any{}
	for _, event := range []string{"SessionStart", "PreToolUse", "SubagentStop", "SessionEnd"} {
		if event == omit {
			continue
		}
		command := "tokens record"
		if event == "SessionStart" {
			command = "context"
		}
		if event == "PreToolUse" {
			command = "guard"
		}
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "kitsu " + command + " --host " + f.host, "disabled": disabled}}}
		if event == "PreToolUse" {
			if matcher == "" {
				matcher = "Bash|apply_patch"
				if f.host == "claude" {
					matcher = "Bash|PowerShell|Edit|Write|MultiEdit"
				}
			}
			group["matcher"] = matcher
		}
		events[event] = []any{group}
	}
	file := "hooks.json"
	if f.host == "codex" {
		file = "codex-hooks.json"
	}
	f.json(filepath.Join(f.pluginDir, "hooks", file), map[string]any{"hooks": events})
}

func (f fixture) inspect() Report {
	f.t.Helper()
	report, err := inspect(context.Background(), Options{Host: f.host, Dir: f.dir, Version: "test"}, f.system)
	if err != nil {
		f.t.Fatal(err)
	}
	return report
}

func checkByID(t *testing.T, report Report, id string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing %s check in %+v", id, report)
	return Check{}
}

func TestInspectConfiguredHosts(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t, host)
			report := f.inspect()
			if !report.Ready || report.Host != host || !filepath.IsAbs(report.HostExecutable) {
				t.Fatalf("ready candidate: %+v", report)
			}
			for _, id := range []string{"host_cli", "plugin", "task_skill", "hooks"} {
				if check := checkByID(t, report, id); check.Status != "pass" {
					t.Errorf("%s = %+v", id, check)
				}
			}
			check := checkByID(t, report, "hook_runtime")
			if check.Status != "warn" || !strings.Contains(check.Detail, "not tested") {
				t.Fatalf("runtime uncertainty omitted: %+v", check)
			}
			if host == "codex" && !strings.Contains(check.Detail, "trust is unknown") {
				t.Fatalf("Codex trust asserted: %+v", check)
			}
			if check := checkByID(t, report, "git_gate"); check.Status != "warn" {
				t.Fatalf("gate should be optional outside Git: %+v", check)
			}
			if _, err := os.Stat(filepath.Join(f.base, "executed")); !os.IsNotExist(err) {
				t.Fatalf("host or Kitsu CLI executed: %v", err)
			}
		})
	}
}

func TestInspectPluginProblems(t *testing.T) {
	tests := []struct {
		name, host string
		change     func(fixture)
	}{
		{"Codex cache without installation setting", "codex", func(f fixture) { f.write(filepath.Join(f.hostDir, "config.toml"), "", 0o644) }},
		{"Codex disabled", "codex", func(f fixture) {
			f.write(filepath.Join(f.hostDir, "config.toml"), "[plugins.\"kitsu@kitsu\"]\nenabled=false\n", 0o644)
		}},
		{"Codex ambiguous cache versions", "codex", func(f fixture) {
			f.json(filepath.Join(filepath.Dir(f.pluginDir), "2", ".codex-plugin", "plugin.json"), map[string]string{"name": "kitsu"})
		}},
		{"Codex malformed manifest", "codex", func(f fixture) { f.write(filepath.Join(f.pluginDir, ".codex-plugin", "plugin.json"), "{broken", 0o644) }},
		{"Claude cache without inventory", "claude", func(f fixture) {
			if err := os.Remove(filepath.Join(f.hostDir, "plugins", "installed_plugins.json")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"Claude unsupported inventory", "claude", func(f fixture) { f.inventory(3, "user", f.dir, f.pluginDir) }},
		{"Claude malformed inventory", "claude", func(f fixture) {
			f.write(filepath.Join(f.hostDir, "plugins", "installed_plugins.json"), "broken", 0o644)
		}},
		{"Claude inventory wrong project", "claude", func(f fixture) { f.inventory(2, "project", filepath.Join(f.base, "other"), f.pluginDir) }},
		{"Claude inventory missing scope", "claude", func(f fixture) { f.inventory(2, "", f.dir, f.pluginDir) }},
		{"Claude disabled local scope", "claude", func(f fixture) {
			f.write(filepath.Join(f.dir, ".claude", "settings.local.json"), `{"enabledPlugins":{"kitsu@kitsu":false}}`, 0o644)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.host)
			tt.change(f)
			report := f.inspect()
			if report.Ready || checkByID(t, report, "plugin").Status != "fail" {
				t.Fatalf("unverified plugin passed: %+v", report)
			}
		})
	}
}

func TestInspectHookProblems(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		for _, problem := range []string{"missing event", "partial matcher", "disabled handler", "wrong host", "comment only", "missing file", "malformed file"} {
			t.Run(host+"/"+problem, func(t *testing.T) {
				f := newFixture(t, host)
				switch problem {
				case "missing event":
					f.hooks("SessionEnd", "", false)
				case "partial matcher":
					f.hooks("", "Bash", false)
				case "disabled handler":
					f.hooks("", "", true)
				default:
					file := filepath.Join(f.pluginDir, "hooks", "hooks.json")
					if host == "codex" {
						file = filepath.Join(f.pluginDir, "hooks", "codex-hooks.json")
					}
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					switch problem {
					case "wrong host":
						f.write(file, strings.ReplaceAll(string(data), "--host "+host, "--host other"), 0o644)
					case "comment only":
						f.write(file, strings.ReplaceAll(string(data), "kitsu ", "# kitsu "), 0o644)
					case "missing file":
						if err := os.Remove(file); err != nil {
							t.Fatal(err)
						}
					case "malformed file":
						f.write(file, "not JSON", 0o644)
					}
				}
				report := f.inspect()
				if report.Ready || checkByID(t, report, "hooks").Status != "fail" {
					t.Fatalf("broken hook bundle passed: %+v", report)
				}
			})
		}
	}
}

func TestInspectTaskSkillProblems(t *testing.T) {
	for _, problem := range []string{"missing", "older task protocol", "previous daily protocol", "malformed", "user invocation disabled", "Codex policy disabled", "standalone copy"} {
		t.Run(problem, func(t *testing.T) {
			f := newFixture(t, "codex")
			path := filepath.Join(f.pluginDir, "skills", "task", "SKILL.md")
			switch problem {
			case "malformed":
				f.write(path, "---\nname: task\ndescription: [broken\n---", 0o644)
			case "previous daily protocol":
				f.write(path, "---\nname: task\ndescription: Previous daily preview.\n---\n<!-- kitsu-task-harness: 2 -->\nCoordinate the task.", 0o644)
			case "older task protocol":
				f.write(path, "---\nname: task\ndescription: Valid older task skill.\n---\nOriginal managed workflow.", 0o644)
			case "user invocation disabled":
				f.write(path, "---\nname: task\ndescription: Hidden.\nuser-invocable: false\n---\nBody", 0o644)
			case "Codex policy disabled":
				f.write(filepath.Join(f.dir, ".codex", "config.toml"), "[[skills.config]]\nname='task'\nenabled=false\n", 0o644)
			default:
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if problem == "standalone copy" {
					f.write(filepath.Join(f.home, ".agents", "skills", "task", "SKILL.md"), "---\nname: task\ndescription: Uninstalled copy.\n---\nBody", 0o644)
				}
			}
			report := f.inspect()
			if report.Ready || checkByID(t, report, "task_skill").Status != "fail" {
				t.Fatalf("unusable installed task skill passed: %+v", report)
			}
		})
	}
}

func TestInspectSettingsPrecedence(t *testing.T) {
	tests := []struct {
		name, host, user, project, local, managed string
		ready                                     bool
	}{
		{"Claude project enables hooks", "claude", `{"enabledPlugins":{"kitsu@kitsu":true},"disableAllHooks":true}`, `{"disableAllHooks":false}`, "", "", true},
		{"Claude local disables hooks", "claude", `{"enabledPlugins":{"kitsu@kitsu":true}}`, `{"disableAllHooks":false}`, `{"disableAllHooks":true}`, "", false},
		{"Claude project enables plugin", "claude", `{"enabledPlugins":{"kitsu@kitsu":false}}`, `{"enabledPlugins":{"kitsu@kitsu":true}}`, "", "", true},
		{"Claude standalone skill overrides exclude plugins", "claude", `{"enabledPlugins":{"kitsu@kitsu":true},"skillOverrides":{"task":"off","kitsu:task":"off"}}`, "", "", "", true},
		{"Claude managed hooks restriction", "claude", `{"enabledPlugins":{"kitsu@kitsu":true}}`, "", "", `{"allowManagedHooksOnly":true}`, false},
		{"Claude managed forced plugin exception", "claude", `{"enabledPlugins":{"kitsu@kitsu":false}}`, "", "", `{"allowManagedHooksOnly":true,"enabledPlugins":{"kitsu@kitsu":true}}`, true},
		{"Codex feature disables hooks", "codex", "[plugins.'kitsu@kitsu']\nenabled=true\n[features]\nhooks=false\n", "", "", "", false},
		{"Codex legacy feature disables hooks", "codex", "[plugins.'kitsu@kitsu']\nenabled=true\n[features]\ncodex_hooks=false\n", "", "", "", false},
		{"Codex project enables hooks", "codex", "[plugins.'kitsu@kitsu']\nenabled=true\n[features]\nhooks=false\n", "[features]\nhooks=true", "", "", true},
		{"Codex managed restriction", "codex", "[plugins.'kitsu@kitsu']\nenabled=true\n", "", "", "allow_managed_hooks_only=true\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.host)
			file, managed := "settings.json", "managed-settings.json"
			if tt.host == "codex" {
				file, managed = "config.toml", "requirements.toml"
			}
			f.write(filepath.Join(f.hostDir, file), tt.user, 0o644)
			if tt.project != "" {
				f.write(filepath.Join(f.dir, "."+tt.host, file), tt.project, 0o644)
			}
			if tt.local != "" {
				f.write(filepath.Join(f.dir, ".claude", "settings.local.json"), tt.local, 0o644)
			}
			if tt.managed != "" {
				f.write(filepath.Join(f.system, managed), tt.managed, 0o644)
			}
			report := f.inspect()
			if report.Ready != tt.ready {
				t.Fatalf("ready=%t, want %t: %+v", report.Ready, tt.ready, report)
			}
		})
	}
}

func TestInspectCodexPluginFeature(t *testing.T) {
	for _, tt := range []struct {
		name, system, user, project, requirements string
		ready                                     bool
	}{
		{"user disables plugins", "", "plugins=false", "", "", false},
		{"system disables plugins", "plugins=false", "", "", "", false},
		{"user enables over system", "plugins=false", "plugins=true", "", "", true},
		{"project disables over user", "", "plugins=true", "plugins=false", "", false},
		{"project enables over user", "", "plugins=false", "plugins=true", "", true},
		{"requirements disable over project", "", "plugins=true", "plugins=true", "plugins=false", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, "codex")
			f.write(filepath.Join(f.hostDir, "config.toml"), "[plugins.'kitsu@kitsu']\nenabled=true\n[features]\n"+tt.user+"\n", 0o644)
			for path, feature := range map[string]string{
				filepath.Join(f.system, "config.toml"):        tt.system,
				filepath.Join(f.dir, ".codex", "config.toml"): tt.project,
				filepath.Join(f.system, "requirements.toml"):  tt.requirements,
			} {
				if feature != "" {
					f.write(path, "[features]\n"+feature+"\n", 0o644)
				}
			}
			state := readSettings(f.host, f.home, f.dir, f.dir, f.system)
			if state.disabled || !state.enabled["kitsu@kitsu"] {
				t.Fatalf("plugin feature conflated with hooks or individual entry: %+v", state)
			}
			report := f.inspect()
			if report.Ready != tt.ready {
				t.Fatalf("ready=%t, want %t: %+v", report.Ready, tt.ready, report)
			}
			if !tt.ready {
				check := checkByID(t, report, "plugin")
				if check.Status != "fail" || !strings.Contains(check.Summary, "plugins are disabled") {
					t.Fatalf("missing feature blocker: %+v", check)
				}
				for _, id := range []string{"task_skill", "hooks"} {
					if checkByID(t, report, id).Status == "pass" {
						t.Fatalf("disabled plugin component passed: %s", id)
					}
				}
			}
		})
	}
}

func TestInspectReadOnlyAndSanitized(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t, host)
			secret := "private-token-should-not-appear"
			file, content := "config.toml", "token='"+secret+"'\n[plugins\n"
			if host == "claude" {
				file, content = "settings.json", `{"token":"`+secret+`","enabledPlugins":`
			}
			f.write(filepath.Join(f.hostDir, file), content, 0o644)
			before := snapshot(t, f.base)
			report := f.inspect()
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if report.Ready || checkByID(t, report, "host_settings").Status != "fail" || strings.Contains(string(data), secret) {
				t.Fatalf("malformed settings or secret handling: %s", data)
			}
			if after := snapshot(t, f.base); after != before {
				t.Fatal("inspection changed fixture files")
			}
		})
	}
}

func TestInspectAbsolutePATH(t *testing.T) {
	f := newFixture(t, "codex")
	t.Setenv("PATH", "."+string(os.PathListSeparator)+"bin")
	report := f.inspect()
	for _, id := range []string{"kitsu_cli", "host_cli"} {
		if checkByID(t, report, id).Status != "fail" {
			t.Fatalf("relative PATH entry accepted: %+v", report)
		}
	}
	if report.Ready || report.HostExecutable != "" {
		t.Fatalf("unavailable executable reported ready: %+v", report)
	}
}

func TestInspectValidation(t *testing.T) {
	for _, tt := range []struct{ name, host, dir string }{{"missing host", "", "."}, {"unknown host", "other", "."}, {"missing directory", "codex", filepath.Join(t.TempDir(), "missing")}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Inspect(context.Background(), Options{Host: tt.host, Dir: tt.dir}); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, Options{Host: "codex"}); err != context.Canceled {
		t.Fatalf("canceled context: %v", err)
	}
}

func TestInspectInventoryVersionsAndScopes(t *testing.T) {
	for _, tt := range []struct {
		name, scope string
		version     int
	}{{"legacy implicit user", "", 1}, {"legacy user", "user", 1}, {"project", "project", 2}, {"local", "local", 2}} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, "claude")
			f.inventory(tt.version, tt.scope, f.dir, f.pluginDir)
			if report := f.inspect(); !report.Ready {
				t.Fatalf("valid scoped inventory rejected: %+v", report)
			}
		})
	}
	t.Run("same-rank versions are ambiguous", func(t *testing.T) {
		f := newFixture(t, "claude")
		f.json(filepath.Join(f.hostDir, "plugins", "installed_plugins.json"), map[string]any{"version": 2, "plugins": map[string]any{"kitsu@kitsu": []any{
			map[string]string{"scope": "user", "installPath": f.pluginDir},
			map[string]string{"scope": "user", "installPath": filepath.Join(f.hostDir, "other-version")},
		}}})
		if report := f.inspect(); report.Ready || checkByID(t, report, "plugin").Status != "fail" {
			t.Fatalf("ambiguous Claude installation selected: %+v", report)
		}
	})
}

func TestInspectMalformedAndBoundedSettings(t *testing.T) {
	for _, content := range []string{"null", `{"disableAllHooks":null}`, `{"enabledPlugins":null}`, `{"enabledPlugins":{"kitsu@kitsu":true},"disableAllHooks":"false"}`, strings.Repeat(" ", (1<<20)+1)} {
		t.Run(fmt.Sprintf("%d bytes %q", len(content), content[:min(len(content), 24)]), func(t *testing.T) {
			f := newFixture(t, "claude")
			f.write(filepath.Join(f.hostDir, "settings.json"), content, 0o644)
			report := f.inspect()
			if report.Ready || checkByID(t, report, "host_settings").Status != "fail" {
				t.Fatalf("invalid settings passed: %+v", report)
			}
		})
	}
}

func TestInspectEscapingComponentPaths(t *testing.T) {
	for _, component := range []string{"skills", "hooks"} {
		t.Run(component, func(t *testing.T) {
			f := newFixture(t, "codex")
			m := map[string]any{"name": "kitsu", "skills": "./skills", "hooks": "./hooks/codex-hooks.json"}
			m[component] = "./../../outside"
			f.json(filepath.Join(f.pluginDir, ".codex-plugin", "plugin.json"), m)
			report := f.inspect()
			id := "task_skill"
			if component == "hooks" {
				id = "hooks"
			}
			if report.Ready || checkByID(t, report, id).Status != "fail" {
				t.Fatalf("escaping component path followed: %+v", report)
			}
		})
	}
}

func TestHookCommandRecognition(t *testing.T) {
	for _, tt := range []struct {
		name, line string
		want       bool
	}{
		{"direct", "kitsu guard --host codex", true},
		{"shell", "sh -c 'kitsu guard --host codex'", true},
		{"wrong host", "kitsu guard --host claude", false},
		{"noop", "true", false},
		{"comment", "# kitsu guard --host codex", false},
		{"echo", "echo kitsu guard --host codex", false},
		{"shell echo", "sh -c 'echo kitsu guard --host codex'", false},
		{"broken quote", "sh -c 'kitsu guard --host codex", false},
		{"help", "kitsu guard --host codex --help", false},
		{"unbound variable", "$c guard --host codex", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := callsKitsu(tt.line, "guard", "codex"); got != tt.want {
				t.Errorf("recognition=%t, want %t", got, tt.want)
			}
		})
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "codex-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	for event, groups := range bundle.Hooks {
		command := "tokens record"
		if event == "SessionStart" {
			command = "context"
		}
		if event == "PreToolUse" {
			command = "guard"
		}
		if len(groups) == 0 || len(groups[0].Hooks) == 0 || !callsKitsu(groups[0].Hooks[0].Windows, command, "codex") {
			t.Errorf("Windows %s hook command was not recognized", event)
		}
	}
}

func TestInstalledHookBundles(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t, host)
			file := "hooks.json"
			if host == "codex" {
				file = "codex-hooks.json"
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "hooks", file))
			if err != nil {
				t.Fatal(err)
			}
			f.write(filepath.Join(f.pluginDir, "hooks", file), string(data), 0o644)
			report := f.inspect()
			if !report.Ready || checkByID(t, report, "hooks").Status != "pass" {
				t.Fatalf("repository's installed hook bundle rejected: %+v", report)
			}
		})
	}
}

func TestInspectOptionalGitGate(t *testing.T) {
	f := newFixture(t, "codex")
	cmd := exec.Command(f.git, "init", "--quiet", f.dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	if report := f.inspect(); !report.Ready || checkByID(t, report, "git_gate").Status != "warn" {
		t.Fatalf("missing optional gate blocked task: %+v", report)
	}
	f.write(filepath.Join(f.dir, "kitsu.toml"), "version = 1\n", 0o644)
	for _, hook := range gate.Hooks {
		f.write(filepath.Join(f.dir, ".git", "hooks", hook), gate.Script(hook), 0o755)
	}
	report := f.inspect()
	if !report.Ready || checkByID(t, report, "git_gate").Status != "pass" {
		t.Fatalf("installed optional gate: %+v", report)
	}
	f.write(filepath.Join(f.dir, ".git", "kitsu", "mode"), "observe\n", 0o644)
	if report := f.inspect(); !report.Ready || checkByID(t, report, "git_gate").Status != "warn" {
		t.Fatalf("observe gate blocked workflow or passed: %+v", report)
	}
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var result strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&result, "%s:%x\n", path, data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.String()
}
