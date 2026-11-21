package installers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const pluginArchiveName = "kitsu-plugin.zip"

func checkReleaseChecksum(t *testing.T, sums []byte, name string, data []byte) string {
	t.Helper()
	digest := strings.Fields(string(checksumLine(name, data)))[0]
	count := 0
	for line := range strings.Lines(string(sums)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			count++
			if fields[0] != digest {
				t.Fatalf("%s SHA-256 is %s, want %s from SHA256SUMS", name, digest, fields[0])
			}
		}
	}
	if count != 1 {
		t.Fatalf("found %d SHA256SUMS entries for %s, want one", count, name)
	}
	return digest
}

func TestMatchedPluginArtifact(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := os.Getenv("KITSU_INSTALL_ARCHIVES")
	if dir == "" {
		python, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("Python 3 or KITSU_INSTALL_ARCHIVES is needed to inspect the plugin archive")
		}
		dir = t.TempDir()
		cmd := exec.CommandContext(t.Context(), python, filepath.Join(root, ".github", "scripts", "package-plugin.py"), filepath.Join(dir, pluginArchiveName))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("package plugin: %v\n%s", err, out)
		}
	} else {
		checkReleaseChecksum(t, readFile(t, filepath.Join(dir, "SHA256SUMS")), pluginArchiveName, readFile(t, filepath.Join(dir, pluginArchiveName)))
	}

	files := []string{".agents/plugins/marketplace.json", ".claude-plugin/marketplace.json", ".claude-plugin/plugin.json",
		".codex-plugin/plugin.json", "hooks/hooks.json", "hooks/codex-hooks.json", "docs/brand/kitsu-mascot.png", "LICENSE",
		"plugins/kitsu-team/.claude-plugin/plugin.json", "plugins/kitsu-team/.codex-plugin/plugin.json"}
	for _, dir := range []string{"skills", "plugins/kitsu-team/agents", "plugins/kitsu-team/skills"} {
		err := fs.WalkDir(os.DirFS(root), dir, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	archive, err := zip.OpenReader(filepath.Join(dir, pluginArchiveName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	if len(archive.File) != len(files) {
		t.Fatalf("archive has %d entries, want %d", len(archive.File), len(files))
	}
	for _, name := range files {
		entry := "kitsu-marketplace/" + name
		member, err := fs.ReadFile(&archive.Reader, entry)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(member, readFile(t, filepath.Join(root, filepath.FromSlash(name)))) {
			t.Errorf("%s differs between marketplace and CLI source revision", name)
		}
		if strings.HasSuffix(name, ".md") {
			checkLocalLinks(t, &archive.Reader, entry, member)
		}
	}
}

func checkLocalLinks(t *testing.T, archive fs.FS, entry string, doc []byte) {
	t.Helper()
	for _, match := range regexp.MustCompile(`\]\(([^ )]+)\)`).FindAllSubmatch(doc, -1) {
		u, err := url.Parse(string(match[1]))
		if err != nil {
			t.Fatal(err)
		}
		if u.IsAbs() || u.Host != "" || u.Path == "" {
			continue
		}
		target := path.Join(path.Dir(entry), u.Path)
		if _, err := fs.Stat(archive, target); err != nil {
			t.Errorf("installed guide %s links to missing file %s", entry, target)
		}
	}
}

func extractMarketplace(t *testing.T, plugin []byte, dir string) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(plugin), int64(len(plugin)))
	if err != nil {
		t.Fatal(err)
	}
	marketplace, err := fs.Sub(archive, "kitsu-marketplace")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, marketplace); err != nil {
		t.Fatal(err)
	}
}

// Populate disposable host inventory from the verified marketplace archive.
// The installed CLI checks the configuration with host executables absent from PATH.
func checkInstalledMarketplace(t *testing.T, run *installRun, plugin []byte) {
	t.Helper()
	codex := filepath.Join(run.home, ".codex")
	claude := filepath.Join(run.home, ".claude")
	installed := filepath.Join(codex, "plugins", "cache", "kitsu", "kitsu", "release")
	extractMarketplace(t, plugin, installed)
	writeFile(t, filepath.Join(codex, "config.toml"), []byte("[plugins.\"kitsu@kitsu\"]\nenabled = true\n"))
	writeFile(t, filepath.Join(claude, "settings.json"), []byte(`{"enabledPlugins":{"kitsu@kitsu":true}}`))
	type registration struct {
		Scope       string `json:"scope"`
		InstallPath string `json:"installPath"`
	}
	inventory := struct {
		Version int                       `json:"version"`
		Plugins map[string][]registration `json:"plugins"`
	}{2, map[string][]registration{"kitsu@kitsu": {{Scope: "user", InstallPath: installed}}}}
	data, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(claude, "plugins", "installed_plugins.json"), data)
	run.env["CODEX_HOME"], run.env["CLAUDE_CONFIG_DIR"] = codex, claude
	previousPath := run.env["PATH"]
	run.env["PATH"] = filepath.Dir(run.target)
	defer func() { run.env["PATH"] = previousPath }()

	for _, host := range []string{"codex", "claude"} {
		checkPluginDiagnostic(t, run, host, installed, "pass", "")
	}
	skill := filepath.Join(installed, "skills", "task", "SKILL.md")
	original := readFile(t, skill)
	marker := regexp.MustCompile(`<!-- kitsu-task-harness: ([0-9]+) -->`).FindSubmatch(original)
	if marker == nil {
		t.Fatal("missing harness protocol declaration in the packaged task skill")
	}
	legacy := []byte("<!-- kitsu-task-harness: 1 -->")
	if bytes.Equal(marker[0], legacy) {
		legacy = nil
	}
	writeFile(t, skill, bytes.ReplaceAll(original, marker[0], legacy))
	for _, host := range []string{"codex", "claude"} {
		checkPluginDiagnostic(t, run, host, installed, "fail", string(marker[1]))
	}
	writeFile(t, skill, original)
}

type agentCheck struct{ ID, Status, Detail string }

type agentCheckReceipt struct {
	Host           string
	HostExecutable string `json:"host_executable"`
	Ready          bool
	Checks         []agentCheck
}

func (r agentCheckReceipt) check(id string) agentCheck {
	for _, check := range r.Checks {
		if check.ID == id {
			return check
		}
	}
	return agentCheck{}
}

func requireAgentChecks(t *testing.T, data []byte, host string, ready bool, statuses map[string]string) agentCheckReceipt {
	t.Helper()
	var report agentCheckReceipt
	decodeReceipt(t, data, &report)
	if report.Host != host || report.Ready != ready {
		t.Fatalf("%s agent check readiness should be %t\n%s", host, ready, data)
	}
	for id, want := range statuses {
		if report.check(id).Status != want {
			t.Fatalf("%s agent check %s: want %s\n%s", host, id, want, data)
		}
	}
	return report
}

func checkPluginDiagnostic(t *testing.T, run *installRun, host, installed, wantSkill, protocol string) {
	t.Helper()
	const blockedExit = 5 // Expected with the host executable absent from this fixture's PATH.
	data := run.command(t, run.dir, blockedExit, run.target, "agent", "check", "--host", host, "--json")
	report := requireAgentChecks(t, data, host, false, map[string]string{
		"kitsu_cli": "pass", "host_cli": "fail", "plugin": "pass", "task_skill": wantSkill, "hooks": "pass", "hook_runtime": "warn",
	})
	if !strings.Contains(report.check("plugin").Detail, installed) {
		t.Fatalf("%s diagnostic did not identify the scripted marketplace registration:\n%s", host, data)
	}
	if wantSkill == "fail" && !strings.Contains(report.check("task_skill").Detail, "does not declare harness protocol "+protocol+".") {
		t.Fatalf("%s diagnostic did not report a mismatched task skill:\n%s", host, data)
	}
	t.Logf("kitsu agent check --host %s exit=%d scripted registration: plugin=%s task_skill=%s hooks=%s native client=unexercised hook trust=unverified",
		host, blockedExit, report.check("plugin").Status, report.check("task_skill").Status, report.check("hooks").Status)
}
