package installers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type nativeClient struct{ name, program, installVerb string }

var nativeClients = [...]nativeClient{{name: "codex", installVerb: "add"}, {name: "claude", installVerb: "install"}}

func withNativeClients(t *testing.T, check func(*testing.T, nativeClient)) {
	t.Helper()
	for _, host := range nativeClients {
		t.Run("native-client/"+host.name, func(t *testing.T) { check(t, host) })
	}
}

func missingNativeCapability(t *testing.T, required bool, format string, args ...any) {
	t.Helper()
	if required {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func (host nativeClient) resolve(t *testing.T, required bool) nativeClient {
	t.Helper()
	client, err := exec.LookPath(host.name)
	if err != nil {
		missingNativeCapability(t, required, "%s client unavailable on %s/%s: %v", host.name, runtime.GOOS, runtime.GOARCH, err)
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(client), ".exe") {
		missingNativeCapability(t, required, "%s resolves to %s on Windows; Kitsu needs a native .exe for handoff", host.name, filepath.Base(client))
	}
	host.program = client
	return host
}

func checkNativeMarketplaces(t *testing.T, kitsu string, plugin []byte) {
	t.Helper()
	withNativeClients(t, func(t *testing.T, host nativeClient) {
		profile := newNativeProfile(t, host.resolve(t, false), kitsu)
		profile.requireMarketplaceCommands(profile.hostVersion(), false)
		profile.initRepository()
		marketplace := filepath.Join(profile.root, "kitsu-marketplace")
		extractMarketplace(t, plugin, marketplace)
		profile.hostCommand("plugin", "marketplace", "add", marketplace)
		t.Logf("%s plugin marketplace add local archive exit=0", host.name)
		profile.hostCommand("plugin", host.installVerb, "kitsu@kitsu")
		t.Logf("%s plugin %s kitsu@kitsu exit=0", host.name, host.installVerb)
		profile.checkHandoff()
	})
}

type nativeProfile struct {
	t                 *testing.T
	host              nativeClient
	root, repo, kitsu string
	env               []string
}

func newNativeProfile(t *testing.T, host nativeClient, kitsu string) nativeProfile {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	for _, dir := range []string{repo, filepath.Join(root, "tmp"), filepath.Join(home, ".codex"), filepath.Join(home, ".claude")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if kitsu == "" {
		kitsu = defaultInstallTarget(home)
	}
	return nativeProfile{t: t, host: host, root: root, repo: repo, kitsu: kitsu, env: nativeClientEnvironment(root, home, kitsu, host.program)}
}

func (p nativeProfile) command(program string, args ...string) []byte {
	p.t.Helper()
	return p.commandIn(p.t, p.repo, 0, program, args...)
}

func (p nativeProfile) commandIn(t *testing.T, dir string, wantExit int, program string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir, cmd.Env = dir, p.env
	return commandOutput(t, cmd, wantExit)
}

func (p nativeProfile) hostArgs(args ...string) []string {
	if p.host.name == "codex" {
		return append([]string{"-c", "analytics.enabled=false", "-c", "check_for_update_on_startup=false"}, args...)
	}
	return args
}

func (p nativeProfile) hostCommand(args ...string) []byte {
	p.t.Helper()
	return p.command(p.host.program, p.hostArgs(args...)...)
}

func (p nativeProfile) hostVersion() string {
	t := p.t
	t.Helper()
	version := strings.TrimSpace(string(p.hostCommand("--version")))
	if version == "" {
		t.Fatalf("%s --version returned an empty result", p.host.name)
	}
	t.Logf("%s/%s native client version %s", runtime.GOOS, runtime.GOARCH, version)
	return version
}

func (p nativeProfile) requireMarketplaceCommands(version string, required bool) {
	t := p.t
	t.Helper()
	if !advertisesCommand(p.hostCommand("--help"), "plugin") {
		missingNativeCapability(t, required, "%s lacks plugin commands", version)
	}
	pluginHelp := p.hostCommand("plugin", "--help")
	if !advertisesCommand(pluginHelp, "marketplace") || !advertisesCommand(pluginHelp, p.host.installVerb) ||
		!advertisesCommand(p.hostCommand("plugin", "marketplace", "--help"), "add") {
		missingNativeCapability(t, required, "%s lacks local marketplace installation commands", version)
	}
}

func (p nativeProfile) initRepository() string {
	t := p.t
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	p.command(git, "init", "-q", "--template=")
	return git
}

func (p nativeProfile) checkHandoff() (agentCheckReceipt, []byte, []byte) {
	t, host := p.t, p.host.name
	t.Helper()
	data := p.command(p.kitsu, "agent", "check", "--host", host, "--json")
	report := requireAgentChecks(t, data, host, true, map[string]string{
		"kitsu_cli": "pass", "host_cli": "pass", "plugin": "pass", "task_skill": "pass", "hooks": "pass", "hook_runtime": "warn",
	})
	selected, err := os.Stat(report.HostExecutable)
	if err != nil {
		t.Fatalf("agent check host executable: %v\n%s", err, data)
	}
	clientInfo, err := os.Stat(p.host.program)
	if err != nil || !os.SameFile(selected, clientInfo) || !strings.Contains(report.check("plugin").Detail, p.root) {
		t.Fatalf("agent check did not identify the tested client and disposable plugin:\n%s", data)
	}
	t.Logf("kitsu agent check --host %s exit=0 plugin=pass task_skill=pass hooks=pass hook runtime=unverified", host)
	if host == "codex" {
		t.Log("Codex hook trust requires manual review in /hooks")
	}

	const request = "Verify the existing response patch."
	var handoff struct {
		Host, Prompt string
		Ready        bool
		Argv         []string
	}
	preview := p.command(p.kitsu, "task", "start", request, "--host", host, "--until", "verify", "--json")
	decodeReceipt(t, preview, &handoff)
	if handoff.Host != host || !handoff.Ready || !strings.Contains(handoff.Prompt, request) ||
		len(handoff.Argv) != 2 || handoff.Argv[0] != report.HostExecutable || handoff.Argv[1] != handoff.Prompt {
		t.Fatalf("task preview did not identify the client, request and arguments:\n%s", preview)
	}
	if _, err := os.Stat(filepath.Join(p.repo, ".git", "kitsu")); !os.IsNotExist(err) {
		t.Fatalf("handoff preview should leave the Git directory without task records: %v", err)
	}
	t.Logf("kitsu task start --host %s --json exit=0, preview only, model requests=0", host)
	return report, data, preview
}

func advertisesCommand(help []byte, name string) bool {
	for line := range strings.Lines(string(help)) {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			command, _, _ := strings.Cut(fields[0], "|")
			if command == name {
				return true
			}
		}
	}
	return false
}

// Only runtime lookup variables are carried from the runner. The host, Git and
// platform configuration paths all point into this disposable profile.
func nativeClientEnvironment(root, home, kitsu, client string) []string {
	path := []string{filepath.Dir(kitsu), filepath.Dir(client)}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(dir) {
			path = append(path, dir)
		}
	}
	tmp := filepath.Join(root, "tmp")
	env := []string{
		"PATH=" + strings.Join(path, string(os.PathListSeparator)), "HOME=" + home, "USERPROFILE=" + home,
		"TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp,
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"CODEX_HOME=" + filepath.Join(home, ".codex"), "CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "OTEL_SDK_DISABLED=true", "TERM=dumb", "LANG=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Installer fixture", "GIT_COMMITTER_NAME=Installer fixture",
		"GIT_AUTHOR_EMAIL=installer@example.invalid", "GIT_COMMITTER_EMAIL=installer@example.invalid",
	}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT"} {
		if value, present := os.LookupEnv(key); present {
			env = append(env, key+"="+value)
		}
	}
	return env
}
