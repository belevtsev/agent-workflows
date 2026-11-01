package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func claudeFixture(t *testing.T) *installerFixture {
	t.Helper()
	f := newInstallerFixture(t)
	f.paths.Claude = filepath.Join(f.paths.Home, ".claude")
	return f
}

func (f *installerFixture) hostInstaller(apply bool, hosts ...string) *Installer {
	i := f.installer(apply)
	i.Hosts = hosts
	return i
}

func seedClaudeFiles(t *testing.T, f *installerFixture) map[string]string {
	t.Helper()
	files := map[string]string{
		filepath.Join(f.paths.Claude, "CLAUDE.md"):     "# My instructions\n",
		filepath.Join(f.paths.Claude, "settings.json"): "{\n  \"theme\": \"dark\",\n  \"permissions\": {\"allow\": [\"Read\"]}\n}\n",
	}
	for path, contents := range files {
		installerWrite(t, path, contents, 0o640)
	}
	return files
}

func assertFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, want := range files {
		if got := installerRead(t, path); got != want {
			t.Fatalf("%s changed: got %q, want %q", path, got, want)
		}
	}
}

func TestClaudeHostInstallAndUninstallRestoreUserFilesExactly(t *testing.T) {
	f := claudeFixture(t)
	original := seedClaudeFiles(t, f)
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	h := i.host(hostClaude)
	settings := installerRead(t, h.settings)
	for _, want := range []string{`"theme": "dark"`, `"permissions": {"allow": ["Read"]}`, `"model": "claude-opus-5-5"`, `"effortLevel": "xhigh"`} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings lost %s: %s", want, settings)
		}
	}
	if !strings.HasPrefix(installerRead(t, h.instructions), "# My instructions\n") || !strings.Contains(installerRead(t, h.instructions), startMarker) {
		t.Fatal("instructions were not appended")
	}
	for name := range object(f.manifest["registrations"]) {
		target, err := os.Readlink(filepath.Join(h.skills, name))
		if err != nil || target != i.target(text(object(f.manifest["registrations"])[name])) {
			t.Fatalf("missing Claude link %s: %q, %v", name, target, err)
		}
	}
	if exists(i.agents) || exists(i.config) {
		t.Fatal("Claude-only install wrote Codex configuration")
	}
	if entries, err := os.ReadDir(i.skills); err == nil && len(entries) != 0 {
		t.Fatal("Claude-only install registered Codex skills")
	}
	report, err := i.Status()
	if err != nil {
		t.Fatal(err)
	}
	if object(report["hosts"])[hostCodex] != nil || object(report["hosts"])[hostClaude] == nil {
		t.Fatalf("wrong host report: %#v", report["hosts"])
	}
	if _, err := i.Uninstall(); err != nil {
		t.Fatal(err)
	}
	assertFiles(t, original)
	if exists(filepath.Join(h.skills, keys(object(f.manifest["registrations"]))[0])) {
		t.Fatal("uninstall retained a Claude link")
	}
}

func TestClaudeHostOnEmptyConfigRemovesWhatItCreated(t *testing.T) {
	f := claudeFixture(t)
	if err := os.MkdirAll(f.paths.Claude, 0o700); err != nil {
		t.Fatal(err)
	}
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Uninstall(); err != nil {
		t.Fatal(err)
	}
	h := i.host(hostClaude)
	if exists(h.settings) || exists(h.instructions) {
		t.Fatal("uninstall left files it created")
	}
}

func TestClaudeHostIgnoresUnrelatedCodexSkillRoots(t *testing.T) {
	f := claudeFixture(t)
	seedClaudeFiles(t, f)
	if err := os.MkdirAll(f.paths.Codex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(f.paths.Codex, "skills")); err != nil {
		t.Fatal(err)
	}
	origin := f.bareOrigin(t)
	if _, err := f.hostInstaller(false, hostClaude).Setup(true); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
		t.Fatalf("install: %v", err)
	}
	f.newRelease(t)
	f.git(t, "push", origin, "main")
	f.git(t, "reset", "--hard", f.first)
	if _, err := f.hostInstaller(true).Update(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.hostInstaller(true).Uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := f.hostInstaller(true, hostCodex).Setup(true); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("a selected Codex host must still refuse the symlink: %v", err)
	}
}

func TestHostsSharingASkillsDirectoryRefuseBeforeWrites(t *testing.T) {
	for _, test := range []struct {
		name  string
		roots func(*installerFixture)
	}{
		{"same home", func(f *installerFixture) { f.paths.Claude = f.paths.Codex }},
		{"Claude in the default Codex home", func(f *installerFixture) {
			f.paths.Codex = filepath.Join(f.base, "custom codex")
			f.paths.Claude = filepath.Join(f.paths.Home, ".codex")
		}},
		{"Claude in the personal skills root", func(f *installerFixture) { f.paths.Claude = filepath.Join(f.paths.Home, ".agents") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := claudeFixture(t)
			test.roots(f)
			before := installerSnapshot(t, f.base)
			for _, apply := range []bool{false, true} {
				if _, err := f.hostInstaller(apply, hostCodex, hostClaude).Setup(true); err == nil || !strings.Contains(err.Error(), "skills directory") {
					t.Fatalf("apply %t: %v", apply, err)
				}
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestEnrollingAHostRefusesASharedSkillsDirectory(t *testing.T) {
	for _, hosts := range [][2]string{{hostClaude, hostCodex}, {hostCodex, hostClaude}} {
		t.Run(hosts[0]+" first", func(t *testing.T) {
			f := claudeFixture(t)
			f.paths.Claude = f.paths.Codex
			if _, err := f.hostInstaller(true, hosts[0]).Setup(true); err != nil {
				t.Fatal(err)
			}
			before := installerSnapshot(t, f.base)
			if _, err := f.hostInstaller(true, hosts[1]).Setup(true); err == nil || !strings.Contains(err.Error(), "skills directory") {
				t.Fatalf("second host was not refused: %v", err)
			}
			installerUnchanged(t, f.base, before)
			if _, err := f.hostInstaller(true).Status(); err != nil {
				t.Fatalf("refusal broke the installation: %v", err)
			}
			if _, err := f.hostInstaller(true).Uninstall(); err != nil {
				t.Fatalf("refusal blocked uninstall: %v", err)
			}
		})
	}
}

func TestFreshInstallRefusesAmbiguousHostDetection(t *testing.T) {
	f := claudeFixture(t)
	seedClaudeFiles(t, f)
	before := installerSnapshot(t, f.base)
	if _, err := f.hostInstaller(true).Setup(true); err == nil || !strings.Contains(err.Error(), "--host") {
		t.Fatalf("both hosts detected: %v", err)
	}
	installerUnchanged(t, f.base, before)
	if err := os.RemoveAll(f.paths.Codex); err != nil {
		t.Fatal(err)
	}
	i := f.hostInstaller(true)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	state, err := i.state(true)
	if err != nil || hostRecord(state, hostClaude) == nil || hostRecord(state, hostCodex) != nil {
		t.Fatalf("detected host not selected: %#v, %v", state["hosts"], err)
	}
}

func TestAddClaudeThenRemoveCodexKeepsClaudeWorking(t *testing.T) {
	for _, forget := range []bool{false, true} {
		name := "uninstall"
		if forget {
			name = "forget"
		}
		t.Run(name, func(t *testing.T) {
			f := claudeFixture(t)
			codexFiles := f.seedUserFiles(t)
			claudeFiles := seedClaudeFiles(t, f)
			i := f.hostInstaller(true, hostCodex)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			state, err := i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			pointer, _ := os.Readlink(i.current)
			i = f.hostInstaller(true, hostClaude)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			added, err := i.state(true)
			if err != nil || hostRecord(added, hostClaude) == nil || hostRecord(added, hostCodex) == nil {
				t.Fatalf("host not added: %v", err)
			}
			if len(sequence(added["history"])) != 0 || added["release"] != state["release"] {
				t.Fatal("adding a host changed the release or its history")
			}
			if got, _ := os.Readlink(i.current); got != pointer {
				t.Fatal("adding a host moved the active pointer")
			}
			remove := f.hostInstaller(true, hostCodex)
			remove.Forget = forget
			if forget {
				if err := os.RemoveAll(f.paths.Codex); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Join(f.paths.Home, ".agents")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := remove.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if !forget {
				for path, want := range codexFiles {
					if filepath.Dir(path) == f.paths.Codex && installerRead(t, path) != want {
						t.Fatalf("Codex file not restored: %s", path)
					}
				}
			}
			kept := f.hostInstaller(true)
			report, err := kept.Status()
			if err != nil || report["installed"] != true || object(report["hosts"])[hostCodex] != nil {
				t.Fatalf("Claude installation stopped working: %#v, %v", report, err)
			}
			if !exists(kept.current) {
				t.Fatal("removing one host removed the shared release pointer")
			}
			if _, err := kept.Uninstall(); err != nil {
				t.Fatal(err)
			}
			assertFiles(t, claudeFiles)
		})
	}
}

func addHostFixture(t *testing.T) *installerFixture {
	t.Helper()
	f := historicalRegistrationFixture(t)
	f.paths.Claude = filepath.Join(f.paths.Home, ".claude")
	seedClaudeFiles(t, f)
	return f
}

func assertLinks(t *testing.T, h hostTarget, names []string, want bool) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(h.skills, name)); (err == nil) != want {
			t.Fatalf("%s link %s: want %t, got %v", h.id, name, want, err)
		}
	}
}

func TestAddingHostKeepsTheActiveRelease(t *testing.T) {
	for _, test := range []struct {
		name       string
		rolledBack bool
		hosts      []string
	}{
		{"checkout ahead", false, []string{hostClaude}},
		{"rolled back", true, []string{hostClaude}},
		{"rolled back naming every host", true, []string{hostCodex, hostClaude}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := addHostFixture(t)
			if _, err := f.hostInstaller(true, hostCodex).Setup(true); err != nil {
				t.Fatal(err)
			}
			second := addBackendFixtureRegistrations(t, f)
			if test.rolledBack {
				if _, err := f.hostInstaller(true).Setup(true); err != nil {
					t.Fatal(err)
				}
				if _, err := f.hostInstaller(true).Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			i := f.hostInstaller(true, test.hosts...)
			before, err := i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			pointer, _ := os.Readlink(i.current)
			snapshot := installerSnapshot(t, f.base)
			report, err := f.hostInstaller(false, test.hosts...).Setup(true)
			if err != nil || report["release"] != f.first {
				t.Fatalf("preview: %#v, %v", report, err)
			}
			installerUnchanged(t, f.base, snapshot)
			report, err = i.Setup(true)
			if err != nil || report["release"] != f.first || report["changed"] != true {
				t.Fatalf("add: %#v, %v", report, err)
			}
			after, err := i.state(true)
			if err != nil {
				t.Fatal(err)
			}
			if after["release"] != f.first || !equal(after["history"], before["history"]) || !equal(after["manifest_registrations"], before["manifest_registrations"]) {
				t.Fatalf("adding a host changed the release: %v, %v", after["release"], after["history"])
			}
			if got, _ := os.Readlink(i.current); got != pointer {
				t.Fatal("adding a host moved the active pointer")
			}
			claude := i.host(hostClaude)
			assertLinks(t, claude, keys(object(f.manifest["registrations"])), true)
			assertLinks(t, claude, backendFixtureNames, false)
			if _, err := f.hostInstaller(true, test.hosts...).Setup(true); err != nil {
				t.Fatal(err)
			}
			moved, err := i.state(true)
			if err != nil || moved["release"] != second {
				t.Fatalf("install of recorded hosts stayed behind HEAD: %v, %v", moved["release"], err)
			}
			assertLinks(t, claude, backendFixtureNames, true)
			assertLinks(t, i.host(hostCodex), backendFixtureNames, true)
		})
	}
}

func TestAddingHostIgnoresTheCheckout(t *testing.T) {
	f := addHostFixture(t)
	second := addBackendFixtureRegistrations(t, f)
	if _, err := f.hostInstaller(true, hostCodex).Setup(true); err != nil {
		t.Fatal(err)
	}
	f.git(t, "reset", "--hard", f.first)
	installerWrite(t, filepath.Join(f.paths.Source, "uncommitted.txt"), "work in progress\n", 0o644)
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatalf("a dirty checkout behind the release blocked the host: %v", err)
	}
	state, err := i.state(true)
	if err != nil || state["release"] != second {
		t.Fatalf("release moved: %v, %v", state["release"], err)
	}
	assertLinks(t, i.host(hostClaude), backendFixtureNames, true)
}

func TestAddingHostLeavesTheManagerAlone(t *testing.T) {
	f := addHostFixture(t)
	i := nativeFixtureInstaller(t, f, f.first, true)
	i.Hosts = []string{hostCodex}
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	before, err := i.state(true)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := os.Readlink(i.runtimeCurrent())
	newer := nativeFixtureInstaller(t, f, addBackendFixtureRegistrations(t, f), true)
	newer.Hosts = []string{hostClaude}
	if _, err := newer.Setup(true); err != nil {
		t.Fatal(err)
	}
	after, err := i.state(true)
	if err != nil {
		t.Fatal(err)
	}
	if after["release"] != before["release"] || !equal(after["manager"], before["manager"]) {
		t.Fatalf("adding a host changed the manager: %v, %v", after["release"], after["manager"])
	}
	if got, _ := os.Readlink(i.runtimeCurrent()); got != runtime {
		t.Fatalf("adding a host moved the runtime: %s", got)
	}
}

func TestClaudeSettingsDriftIsReportedAndLeftAlone(t *testing.T) {
	f := claudeFixture(t)
	original := seedClaudeFiles(t, f)
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	h := i.host(hostClaude)
	installerWrite(t, h.settings, strings.Replace(installerRead(t, h.settings), `"claude-opus-5-5"`, `"claude-fable-5-1"`, 1), 0o640)
	report, err := i.Status()
	if err != nil {
		t.Fatal(err)
	}
	drift := object(object(report["hosts"])[hostClaude])["settings_drift"]
	if !reflect.DeepEqual(drift, []string{"model"}) {
		t.Fatalf("wrong drift: %#v", drift)
	}
	f.newRelease(t)
	second := f.hostInstaller(true)
	if _, err := second.Setup(true); err != nil {
		t.Fatalf("update refused a user model change: %v", err)
	}
	if !strings.Contains(installerRead(t, h.settings), `"claude-fable-5-1"`) {
		t.Fatal("update overwrote the user's model")
	}
	if _, err := second.Rollback(); err != nil {
		t.Fatalf("rollback refused a user model change: %v", err)
	}
	if !strings.Contains(installerRead(t, h.settings), `"claude-fable-5-1"`) {
		t.Fatal("rollback overwrote the user's model")
	}
	if _, err := second.Uninstall(); err != nil {
		t.Fatal(err)
	}
	settings := installerRead(t, h.settings)
	if !strings.Contains(settings, `"claude-fable-5-1"`) || strings.Contains(settings, "effortLevel") {
		t.Fatalf("uninstall must keep the user's key and remove the managed one: %s", settings)
	}
	instructions := filepath.Join(f.paths.Claude, "CLAUDE.md")
	if installerRead(t, instructions) != original[instructions] {
		t.Fatal("uninstall did not restore instructions")
	}
}

func TestClaudeHostRecoversEveryBoundary(t *testing.T) {
	for _, command := range []string{"setup", "uninstall"} {
		for _, label := range []string{"after-pointer", "after-global:claude", "after-claude-settings", "after-state"} {
			t.Run(command+"/"+label, func(t *testing.T) {
				f := claudeFixture(t)
				seedClaudeFiles(t, f)
				if command == "uninstall" {
					if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
						t.Fatal(err)
					}
				}
				before := map[string]string{}
				for _, path := range []string{filepath.Join(f.paths.Claude, "CLAUDE.md"), filepath.Join(f.paths.Claude, "settings.json")} {
					before[path] = installerRead(t, path)
				}
				t.Setenv("AGENT_WORKFLOWS_FAULT", label)
				i := f.hostInstaller(true, hostClaude)
				if command == "uninstall" {
					i.Hosts = nil
				}
				_, err := i.Execute(command)
				t.Setenv("AGENT_WORKFLOWS_FAULT", "")
				if err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
					t.Fatalf("fault %s: %v", label, err)
				}
				if _, got := RecordedHomes(f.paths.State); got != i.Claude {
					t.Fatalf("journal lost the Claude root: %q", got)
				}
				if _, err = i.Recover(); err != nil {
					t.Fatal(err)
				}
				if !exists(i.statePath) && command == "uninstall" {
					t.Fatal("recovery did not restore the installation")
				}
				for _, path := range []string{filepath.Join(f.paths.Claude, "CLAUDE.md"), filepath.Join(f.paths.Claude, "settings.json")} {
					if got := installerRead(t, path); got != before[path] {
						t.Fatalf("recovery did not restore %s", path)
					}
				}
			})
		}
	}
}

func TestClaudeSettingsEditsPreserveEverythingElse(t *testing.T) {
	present := func(value string) Object { return Object{"present": true, "representation": value} }
	for _, test := range []struct{ name, input, key, want string }{
		{"replace in place", "{\n  \"model\":   \"old\",\n  \"z\": 1\n}\n", "model", "{\n  \"model\":   \"new\",\n  \"z\": 1\n}\n"},
		{"append with indent", "{\n    \"z\": [1, 2]\n}\n", "model", "{\n    \"z\": [1, 2],\n    \"model\": \"new\"\n}\n"},
		{"empty object", "{}\n", "model", "{\n  \"model\": \"new\"\n}\n"},
		{"empty file", "", "model", "{\n  \"model\": \"new\"\n}\n"},
		{"compact", `{"a":1}`, "model", `{"a":1,"model": "new"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := settingsApply([]byte(test.input), test.key, present(`"new"`))
			if err != nil || string(got) != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
	for _, test := range []struct{ name, input, want string }{
		{"first", "{\n  \"model\": \"m\",\n  \"z\": 1\n}\n", "{\n  \"z\": 1\n}\n"},
		{"middle", "{\n  \"a\": 1,\n  \"model\": \"m\",\n  \"z\": 1\n}\n", "{\n  \"a\": 1,\n  \"z\": 1\n}\n"},
		{"last", "{\n  \"a\": 1,\n  \"model\": \"m\"\n}\n", "{\n  \"a\": 1\n}\n"},
		{"only", "{\n  \"model\": \"m\"\n}\n", "{}\n"},
	} {
		t.Run("remove "+test.name, func(t *testing.T) {
			got, err := settingsApply([]byte(test.input), "model", Object{"present": false})
			if err != nil || string(got) != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
	for _, invalid := range []string{"[]", `{"a":1,"a":2}`, `{"a":1} trailing`, `{"a":`} {
		if _, err := settingsParse([]byte(invalid)); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestClaudeSettingsRejectsUnsafePaths(t *testing.T) {
	f := claudeFixture(t)
	seedClaudeFiles(t, f)
	i := f.hostInstaller(true, hostClaude)
	h := i.host(hostClaude)
	if err := os.Remove(h.settings); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.base, "elsewhere.json"), h.settings); err != nil {
		t.Fatal(err)
	}
	before := installerSnapshot(t, f.base)
	if _, err := i.Setup(true); err == nil {
		t.Fatal("setup followed a symlinked settings file")
	}
	installerUnchanged(t, f.base, before)
}
