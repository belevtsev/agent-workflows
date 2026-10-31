package workflow

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// Installer fixtures retain the maintained lifecycle inputs, with minimal skill
// metadata. Full vendored content is checked by TestValidateSourceTree and the
// exact-commit launcher smoke. Every source, home, origin, release, and recovery
// path belongs to the test directory.
type installerFixture struct {
	paths    Paths
	base     string
	first    string
	manifest Object
}

func newInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_OPTIONAL_LOCKS", "0")
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("ZDOTDIR", "")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "installer fixtures with spaces ' Ω")
	f := &installerFixture{base: base}
	f.paths = Paths{Source: filepath.Join(base, "source checkout"), Home: filepath.Join(base, "user home"),
		Codex: filepath.Join(base, "user home", ".codex"), State: filepath.Join(base, "private state")}
	installerBuildFixture(t, root, f.paths.Source)
	for _, directory := range []string{f.paths.Codex, filepath.Join(f.paths.Home, ".agents", "skills")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.git(t, "init", "-b", "main")
	f.git(t, "config", "user.name", "Installer Fixture")
	f.git(t, "config", "user.email", "fixture@example.invalid")
	f.git(t, "config", "gc.auto", "0")
	f.git(t, "config", "maintenance.auto", "false")
	f.git(t, "add", ".")
	f.git(t, "commit", "-m", "Maintained fixture suite")
	f.first = f.git(t, "rev-parse", "HEAD")
	f.manifest, err = LoadManifest(f.paths.Source)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func installerBuildFixture(t *testing.T, maintained, destination string) {
	t.Helper()
	manifest, err := LoadManifest(maintained)
	if err != nil {
		t.Fatal(err)
	}
	// Lifecycle fixtures preserve the deployed thirteen-registration v1 source
	// contract as new personal registrations are added to the maintained suite.
	manifest = clone(manifest)
	manifest["version"] = 1
	delete(manifest, "adoption_catalog")
	registrations := Object{}
	for _, name := range []string{"cc-skills-golang", "db-postgres", "go-principal-engineer", "backend-security-review", "protobuf-contracts", "go-pki-mtls", "software-architecture", "code-review", "test-strategy", "security-threat-model", "task-orchestration", "typesafe-ai"} {
		registrations[name] = object(manifest["registrations"])[name]
	}
	manifest["registrations"] = registrations
	var licenses []any
	for _, license := range sequence(manifest["required_licenses"]) {
		if !strings.HasPrefix(text(license), "third_party/archify/") && !strings.HasPrefix(text(license), "third_party/docker-skills/") {
			licenses = append(licenses, license)
		}
	}
	manifest["required_licenses"] = licenses
	write := func(relative, contents string, mode os.FileMode) {
		t.Helper()
		target := filepath.Join(destination, filepath.FromSlash(relative))
		// Git archives represent tracked directories as 0755. Matching those
		// modes is material to exact legacy adoption inventory checks.
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		installerWrite(t, target, contents, mode)
	}
	copyFile := func(relative string) {
		t.Helper()
		source := filepath.Join(maintained, filepath.FromSlash(relative))
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		write(relative, string(data), info.Mode().Perm())
	}
	for _, relative := range []string{
		".gitignore", "install.sh",
		text(manifest["global_instructions"]), text(manifest["model_policy"]),
		filepath.ToSlash(filepath.Join(filepath.Dir(text(manifest["model_policy"])), "references", "jev-questions.json")),
	} {
		copyFile(relative)
	}
	// These lifecycle fixtures describe managers predating the OpenAI adviser
	// extensions. Keep their policy independent of newly maintained defaults.
	editModelPolicy(t, filepath.Join(destination, text(manifest["model_policy"])), func(policy Object) {
		consultation := policy["consultation"].(Object)
		delete(consultation, "optional_providers")
		delete(object(consultation["providers"]), "openai_decisions")
	})
	write("skills-manifest.json", string(legacyJSON(manifest)), 0o644)
	write("LICENSE", "Fixture permission notice.\n", 0o644)
	for _, relative := range sequence(manifest["required_licenses"]) {
		copyFile(text(relative))
	}
	for name, relative := range object(manifest["registrations"]) {
		if name == "typesafe-ai" {
			// Adoption requires exactly these maintained files and modes. Retain
			// their contents so installation and restored backups match a real
			// legacy TypeSafe directory, including its linked reference.
			for _, file := range []string{"SKILL.md", "LICENSE", "references/development-consultations.md"} {
				copyFile(filepath.ToSlash(filepath.Join(text(relative), filepath.FromSlash(file))))
			}
			continue
		}
		metadata := "---\nname: " + name + "\ndescription: Isolated installer lifecycle fixture.\n---\n\n# Fixture skill\n"
		write(filepath.ToSlash(filepath.Join(text(relative), "SKILL.md")), metadata, 0o644)
	}
}

func installerCopyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != source && slices.Contains([]string{".git", ".venv", ".bin", "dist", "__pycache__", ".pytest_cache", ".cache"}, entry.Name()) {
			return filepath.SkipDir
		}
		if entry.Name() == ".DS_Store" || strings.HasSuffix(entry.Name(), ".pyc") {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fixture source contains nonregular file %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *installerFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	return installerGit(t, f.paths.Source, args...)
}

func installerGit(t *testing.T, source string, args ...string) string {
	t.Helper()
	args = append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", source}, args...)
	command := exec.CommandContext(t.Context(), "git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (f *installerFixture) installer(apply bool) *Installer {
	return NewInstaller(Options{Paths: f.paths, Apply: apply, Shell: "bash"})
}

func (f *installerFixture) execute(t *testing.T, command string) Object {
	t.Helper()
	report, err := f.installer(true).Execute(command)
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return report
}

func (f *installerFixture) failAt(t *testing.T, command, label string) {
	t.Helper()
	t.Setenv("AGENT_WORKFLOWS_FAULT", label)
	_, err := f.installer(true).Execute(command)
	t.Setenv("AGENT_WORKFLOWS_FAULT", "")
	if err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
		t.Fatalf("%s with %s: got %v", command, label, err)
	}
}

func installerWrite(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func installerRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func installerAppend(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(contents)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: %v; close: %v", writeErr, closeErr)
	}
}

func installerSnapshot(t *testing.T, root string) Object {
	t.Helper()
	if !exists(root) {
		return Object{}
	}
	value, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func installerUnchanged(t *testing.T, root string, before Object) {
	t.Helper()
	if after := installerSnapshot(t, root); !equal(before, after) {
		t.Fatalf("unexpected changes beneath %s", root)
	}
}

func (f *installerFixture) seedUserFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{
		filepath.Join(f.paths.Codex, "AGENTS.md"):   "Personal instructions without final newline",
		filepath.Join(f.paths.Codex, "config.toml"): "model = 'original' # user model\nmodel_reasoning_effort = 'high'\n[profiles.personal]\nmodel = 'profile'\n",
		filepath.Join(f.paths.Home, ".bashrc"):      "# private startup config\nexport USER_SECRET='fixture-secret'",
	}
	for path, data := range files {
		installerWrite(t, path, data, 0o640)
	}
	return files
}

func (f *installerFixture) newRelease(t *testing.T) string {
	t.Helper()
	installerAppend(t, filepath.Join(f.paths.Source, text(f.manifest["global_instructions"])), "\nNew fixture instructions\n")
	policy := filepath.Join(f.paths.Source, text(f.manifest["model_policy"]))
	editModelPolicy(t, policy, func(policy Object) {
		profile := policy["profiles"].(Object)["sol"].(Object)
		profile["model"], profile["reasoning_effort"] = "gpt-6-sol", "high"
		effort := policy["effort_policy"].(Object)
		effort["default_substantive"], effort["configured_max_profiles"] = "high", []any{}
	})
	f.git(t, "add", ".")
	f.git(t, "commit", "-m", "Alternate valid fixture release")
	return f.git(t, "rev-parse", "HEAD")
}

func (f *installerFixture) bareOrigin(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(f.base, "local origin.git")
	f.git(t, "clone", "--bare", f.paths.Source, origin)
	const official = "https://github.com/belevtsev/agent-workflows.git"
	f.git(t, "remote", "add", "origin", official)
	f.git(t, "config", "url."+origin+".insteadOf", official)
	return origin
}

func TestInstallerSetupDryRunRepeatAndSelectiveUninstall(t *testing.T) {
	f := newInstallerFixture(t)
	original := f.seedUserFiles(t)
	before := installerSnapshot(t, f.base)
	report, err := f.installer(false).Setup(true)
	if err != nil || report["dry_run"] != true || report["release"] != f.first {
		t.Fatalf("dry setup: %#v, %v", report, err)
	}
	installerUnchanged(t, f.base, before)
	f.execute(t, "setup")
	status := f.execute(t, "status")
	if status["installed"] != true || status["release"] != f.first || len(status["registrations"].([]string)) != len(object(f.manifest["registrations"])) {
		t.Fatalf("unexpected status: %#v", status)
	}
	for name := range object(f.manifest["registrations"]) {
		if info, err := os.Stat(filepath.Join(f.paths.Home, ".agents", "skills", name)); err != nil || !info.IsDir() {
			t.Fatalf("registration %s: %v", name, err)
		}
	}
	before = installerSnapshot(t, f.base)
	if report := f.execute(t, "setup"); report["changed"] != false {
		t.Fatalf("repeat changed installation: %#v", report)
	}
	installerUnchanged(t, f.base, before)
	for path := range original {
		installerAppend(t, path, "\n# Independent later edit\n")
		if err := os.Chmod(path, 0o604); err != nil {
			t.Fatal(err)
		}
	}
	f.execute(t, "uninstall")
	if f.execute(t, "status")["installed"] != false {
		t.Fatal("uninstall left an installed status")
	}
	for path, value := range original {
		if got := installerRead(t, path); got != value+"\n# Independent later edit\n" {
			t.Fatalf("uninstall changed unrelated bytes in %s: %q", path, got)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o604 {
			t.Fatalf("uninstall changed later mode in %s: %v", path, err)
		}
	}
}

func TestInstallerLocalOriginUpdateRollbackAndNoCheckout(t *testing.T) {
	for _, noCheckout := range []bool{false, true} {
		t.Run(fmt.Sprintf("no checkout %t", noCheckout), func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			origin := f.bareOrigin(t)
			f.execute(t, "setup")
			second := f.newRelease(t)
			f.git(t, "push", origin, "main")
			f.git(t, "reset", "--hard", f.first)
			before := installerSnapshot(t, f.base)
			if _, err := f.installer(false).Update(); err != nil {
				t.Fatal(err)
			}
			installerUnchanged(t, f.base, before)
			i := f.installer(true)
			i.NoCheckout = noCheckout
			if _, err := i.Update(); err != nil {
				t.Fatal(err)
			}
			wantHead := second
			if noCheckout {
				wantHead = f.first
			}
			if got := f.git(t, "rev-parse", "HEAD"); got != wantHead {
				t.Fatalf("source HEAD = %s, want %s", got, wantHead)
			}
			if got := f.execute(t, "status")["release"]; got != second {
				t.Fatalf("active release = %v, want %s", got, second)
			}
			sourceBefore := installerSnapshot(t, f.paths.Source)
			f.execute(t, "rollback")
			installerUnchanged(t, f.paths.Source, sourceBefore)
			if got := f.execute(t, "status")["release"]; got != f.first {
				t.Fatalf("rollback release = %v", got)
			}
			if strings.Contains(installerRead(t, filepath.Join(f.paths.Codex, "config.toml")), "gpt-6-sol") {
				t.Fatal("rollback retained new model defaults")
			}
		})
	}
}

func TestInstallerSetupLocalFastForwardNeverFetches(t *testing.T) {
	f := newInstallerFixture(t)
	f.execute(t, "setup")
	second := f.newRelease(t)
	f.git(t, "remote", "add", "origin", "https://invalid.invalid/no-network")
	before := installerSnapshot(t, f.paths.Source)
	if report := f.execute(t, "setup"); report["release"] != second {
		t.Fatalf("setup used wrong local release: %#v", report)
	}
	installerUnchanged(t, f.paths.Source, before)
}

func TestInstallerUpdateRejectsWrongOriginDirtyDivergenceAndInvalidRelease(t *testing.T) {
	for _, invalid := range []string{"origin", "dirty", "divergence", "invalid release"} {
		t.Run(invalid, func(t *testing.T) {
			f := newInstallerFixture(t)
			origin := f.bareOrigin(t)
			f.execute(t, "setup")
			second := f.newRelease(t)
			if invalid == "invalid release" {
				if err := os.Remove(filepath.Join(f.paths.Source, text(f.manifest["global_instructions"]))); err != nil {
					t.Fatal(err)
				}
				f.git(t, "add", ".")
				f.git(t, "commit", "-m", "Invalid remote suite")
				second = f.git(t, "rev-parse", "HEAD")
			}
			f.git(t, "push", origin, "main")
			f.git(t, "reset", "--hard", f.first)
			switch invalid {
			case "origin":
				f.git(t, "remote", "set-url", "origin", "https://invalid.invalid/other")
			case "dirty":
				installerAppend(t, filepath.Join(f.paths.Source, "LICENSE"), "uncommitted change\n")
			case "divergence":
				installerAppend(t, filepath.Join(f.paths.Source, "LICENSE"), "sibling release\n")
				f.git(t, "add", ".")
				f.git(t, "commit", "-m", "Divergent local release")
			}
			head := f.git(t, "rev-parse", "HEAD")
			before := installerSnapshot(t, f.paths.Home)
			if _, err := f.installer(true).Update(); err == nil {
				t.Fatalf("update accepted %s", invalid)
			}
			installerUnchanged(t, f.paths.Home, before)
			if got := f.git(t, "rev-parse", "HEAD"); got != head {
				t.Fatal("rejected update changed source HEAD")
			}
			if f.execute(t, "status")["release"] != f.first || exists(filepath.Join(f.paths.State, "journal.json")) {
				t.Fatal("rejected update changed installation ownership")
			}
			if invalid == "invalid release" && exists(filepath.Join(f.paths.State, "releases", second)) {
				t.Fatal("invalid suite was published as an immutable release")
			}
		})
	}
}

func TestInstallerOwnedConflictsRejectBeforeAnyWrites(t *testing.T) {
	for _, conflict := range []string{"model", "global", "alias", "registration", "pointer", "release"} {
		t.Run(conflict, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.execute(t, "setup")
			i := f.installer(true)
			switch conflict {
			case "model":
				installerWrite(t, i.config, strings.ReplaceAll(installerRead(t, i.config), "gpt-6.1-sol", "user-edited"), 0o600)
			case "global":
				installerWrite(t, i.agents, strings.Replace(installerRead(t, i.agents), "# Working conventions", "# User edited managed conventions", 1), 0o600)
			case "alias":
				path := filepath.Join(f.paths.Home, ".bashrc")
				installerWrite(t, path, strings.Replace(installerRead(t, path), `"$@"`, `"$1"`, 1), 0o600)
			case "registration", "pointer":
				path := i.current
				if conflict == "registration" {
					path = filepath.Join(i.skills, keys(object(f.manifest["registrations"]))[0])
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(f.base, path); err != nil {
					t.Fatal(err)
				}
			case "release":
				installerAppend(t, filepath.Join(f.paths.State, "releases", f.first, "LICENSE"), "Changed immutable release\n")
			}
			before := installerSnapshot(t, f.base)
			for _, command := range []string{"status", "setup", "update", "rollback", "uninstall"} {
				if _, err := f.installer(true).Execute(command); err == nil {
					t.Fatalf("%s accepted %s conflict", command, conflict)
				}
				installerUnchanged(t, f.base, before)
			}
		})
	}
}

func TestInstallerRewindAndDivergenceRejectBeforeActivation(t *testing.T) {
	for _, diverge := range []bool{false, true} {
		t.Run(fmt.Sprintf("divergent %t", diverge), func(t *testing.T) {
			f := newInstallerFixture(t)
			f.execute(t, "setup")
			f.newRelease(t)
			f.execute(t, "setup")
			f.git(t, "reset", "--hard", f.first)
			if diverge {
				installerAppend(t, filepath.Join(f.paths.Source, "LICENSE"), "Sibling local commit\n")
				f.git(t, "add", ".")
				f.git(t, "commit", "-m", "Divergent fixture")
			}
			before := installerSnapshot(t, f.base)
			if _, err := f.installer(true).Setup(true); err == nil || !strings.Contains(err.Error(), "rewinds") {
				t.Fatalf("accepted rewind/divergence: %v", err)
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestInstallerFreshSetupRecoversEveryActivationBoundary(t *testing.T) {
	f := newInstallerFixture(t)
	labels := []string{"after-journal", "after-pointer"}
	for _, name := range keys(object(f.manifest["registrations"])) {
		labels = append(labels, "after-registration:"+name)
	}
	labels = append(labels, "after-global", "after-model-config", "after-command-alias", "after-state")
	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			before := installerSnapshot(t, f.paths.Home)
			f.failAt(t, "setup", label)
			if _, err := f.installer(true).Status(); err == nil || !strings.Contains(err.Error(), "recover") {
				t.Fatalf("status hid interrupted setup: %v", err)
			}
			stateBefore := installerSnapshot(t, f.paths.State)
			if report, err := f.installer(false).Recover(); err != nil || report["pending"] != true {
				t.Fatalf("recovery preview: %#v, %v", report, err)
			}
			installerUnchanged(t, f.paths.State, stateBefore)
			f.execute(t, "recover")
			installerUnchanged(t, f.paths.Home, before)
			if f.execute(t, "status")["installed"] != false || exists(filepath.Join(f.paths.State, "journal.json")) {
				t.Fatal("recovery retained setup ownership")
			}
			f.execute(t, "setup")
		})
	}
}

func TestInstallerPublicationFailureCanResumeExactStage(t *testing.T) {
	f := newInstallerFixture(t)
	f.seedUserFiles(t)
	before := installerSnapshot(t, f.paths.Home)
	f.failAt(t, "setup", "after-release-publication")
	installerUnchanged(t, f.paths.Home, before)
	if exists(filepath.Join(f.paths.State, "journal.json")) || f.execute(t, "recover")["pending"] != false {
		t.Fatal("release publication fault falsely required rollback recovery")
	}
	f.execute(t, "setup")
	f.execute(t, "status")
}

func TestInstallerUninstallRecoversEveryMutationBoundary(t *testing.T) {
	f := newInstallerFixture(t)
	labels := []string{"after-journal"}
	for _, name := range keys(object(f.manifest["registrations"])) {
		labels = append(labels, "after-registration:"+name)
	}
	labels = append(labels, "after-global", "after-model-config", "after-command-alias", "after-pointer", "after-state")
	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.execute(t, "setup")
			before := installerSnapshot(t, f.paths.Home)
			f.failAt(t, "uninstall", label)
			f.execute(t, "recover")
			installerUnchanged(t, f.paths.Home, before)
			if f.execute(t, "status")["release"] != f.first {
				t.Fatal("uninstall recovery failed to restore active release")
			}
			f.execute(t, "uninstall")
		})
	}
}

func TestInstallerInterruptedRecoveryKeepsIndependentEdits(t *testing.T) {
	for _, target := range []string{"global", "model-config", "command-alias"} {
		t.Run(target, func(t *testing.T) {
			f := newInstallerFixture(t)
			original := f.seedUserFiles(t)
			f.execute(t, "setup")
			f.failAt(t, "uninstall", "after-state")
			for path := range original {
				installerAppend(t, path, "\n# edit after removal\n")
			}
			f.failAt(t, "recover", "after-recover-"+target)
			for path := range original {
				installerAppend(t, path, "# edit during recovery\n")
			}
			f.execute(t, "recover")
			f.execute(t, "status")
			f.execute(t, "uninstall")
			for path, data := range original {
				if got := installerRead(t, path); got != data+"\n# edit after removal\n# edit during recovery\n" {
					t.Fatalf("recovery lost unrelated bytes in %s: %q", path, got)
				}
			}
			before := installerSnapshot(t, f.base)
			if f.execute(t, "recover")["pending"] != false {
				t.Fatal("repeat recovery reported work")
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestInstallerRecoveryRejectsOwnedEditsBeforePartialReversal(t *testing.T) {
	for _, target := range []string{"global", "model", "alias", "pointer"} {
		t.Run(target, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			if target == "alias" {
				f.failAt(t, "setup", "after-command-alias")
			} else {
				f.execute(t, "setup")
				f.newRelease(t)
				f.failAt(t, "setup", "after-model-config")
			}
			i := f.installer(true)
			path, old, replacement := i.agents, "New fixture instructions", "User changed owned block"
			switch target {
			case "model":
				path, old, replacement = i.config, "gpt-6-sol", "user-edited"
			case "alias":
				path, old, replacement = filepath.Join(f.paths.Home, ".bashrc"), `"$@"`, `"$1"`
			case "pointer":
				if err := os.Remove(i.current); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(f.base, i.current); err != nil {
					t.Fatal(err)
				}
			}
			if target != "pointer" {
				installerWrite(t, path, strings.Replace(installerRead(t, path), old, replacement, 1), 0o600)
			}
			before := installerSnapshot(t, f.base)
			if _, err := i.Recover(); err == nil {
				t.Fatal("recovery accepted an owned edit")
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestInstallerJournalRejectsCorruptionAndPathsBeforeWrites(t *testing.T) {
	for _, invalid := range []string{"checksum", "outside path", "full config write", "unowned model key", "alias path", "full alias write", "embedded alias path", "pointer escape"} {
		t.Run(invalid, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.failAt(t, "setup", "after-journal")
			i := f.installer(true)
			j, err := readJSON(i.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range sequence(j["operations"]) {
				op := object(raw)
				switch {
				case invalid == "outside path" && op["kind"] == "global":
					op["path"] = filepath.Join(f.base, "foreign file")
				case invalid == "full config write" && op["kind"] == "model_config":
					op["kind"], op["before"], op["after"] = "path", Object{"kind": "absent"}, Object{"kind": "absent"}
				case invalid == "unowned model key" && op["kind"] == "model_config":
					object(op["after_keys"])["api_key"] = Object{"present": true, "value": "fixture-key"}
				case invalid == "alias path" && op["kind"] == "command_alias":
					op["path"] = filepath.Join(f.base, "foreign.rc")
				case invalid == "full alias write" && op["kind"] == "command_alias":
					op["kind"], op["before"], op["after"] = "path", Object{"kind": "absent"}, Object{"kind": "absent"}
				case invalid == "embedded alias path" && op["path"] == i.statePath:
					data, err := decode(object(op["after"])["data"])
					if err != nil {
						t.Fatal(err)
					}
					var state Object
					if err := json.Unmarshal(data, &state); err != nil {
						t.Fatal(err)
					}
					object(state["command_alias"])["path"] = filepath.Join(f.base, "foreign.rc")
					object(op["after"])["data"] = encode(legacyJSON(seal(state)))
				case invalid == "pointer escape" && op["path"] == i.current:
					object(op["after"])["target"] = f.base
				}
			}
			j = seal(j)
			if invalid == "checksum" {
				j["integrity_sha256"] = strings.Repeat("0", 64)
			}
			installerWrite(t, i.journalPath, string(legacyJSON(j)), 0o600)
			before := installerSnapshot(t, f.base)
			if _, err := i.Recover(); err == nil {
				t.Fatalf("recovery accepted %s journal", invalid)
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestInstallerMutationLockAndUnsafeLockRefuseWrites(t *testing.T) {
	f := newInstallerFixture(t)
	f.execute(t, "setup")
	path := filepath.Join(f.paths.State, "mutation.lock")
	lock, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	before := installerSnapshot(t, f.base)
	for _, command := range []string{"setup", "uninstall"} {
		if _, err := f.installer(true).Execute(command); err == nil || !strings.Contains(err.Error(), "holds the lock") {
			t.Fatalf("%s bypassed mutation lock: %v", command, err)
		}
		installerUnchanged(t, f.base, before)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(f.base, "unrelated lock")
	installerWrite(t, foreign, "keep", 0o600)
	if err := os.Symlink(foreign, path); err != nil {
		t.Fatal(err)
	}
	before = installerSnapshot(t, f.base)
	if _, err := f.installer(true).Uninstall(); err == nil || !strings.Contains(err.Error(), "unsafe mutation lock") {
		t.Fatalf("symlink lock accepted: %v", err)
	}
	installerUnchanged(t, f.base, before)
}

func TestInstallerMigrationRestoresRawLinksPrefixAndLegacyDirectory(t *testing.T) {
	f := newInstallerFixture(t)
	prior := filepath.Join(f.base, "prior suite")
	installerCopyTree(t, f.paths.Source, prior)
	originalLinks := map[string]string{}
	for name, relative := range object(f.manifest["registrations"]) {
		if name == "typesafe-ai" {
			continue
		}
		target := filepath.Join(prior, text(relative))
		path := filepath.Join(f.paths.Home, ".agents", "skills", name)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		originalLinks[path] = target
	}
	legacy := filepath.Join(f.paths.Home, ".codex", "skills", "typesafe-ai")
	installerCopyTree(t, filepath.Join(prior, text(object(f.manifest["registrations"])["typesafe-ai"])), legacy)
	legacyBefore := installerSnapshot(t, legacy)
	agents := filepath.Join(f.paths.Codex, "AGENTS.md")
	prefix := installerRead(t, filepath.Join(prior, text(f.manifest["global_instructions"])))
	installerWrite(t, agents, prefix+"\nKeep exact personal suffix\n", 0o640)
	i := f.installer(true)
	i.MigrateFrom = prior
	i.TypeSafeLegacy = new("")
	if _, err := i.Setup(false); err != nil {
		t.Fatal(err)
	}
	if exists(legacy) {
		t.Fatal("legacy skill directory was not adopted")
	}
	installerAppend(t, agents, "Further independent instructions\n")
	f.execute(t, "uninstall")
	installerUnchanged(t, legacy, legacyBefore)
	for path, want := range originalLinks {
		if got, err := os.Readlink(path); err != nil || got != want {
			t.Fatalf("legacy target = %q, want %q: %v", got, want, err)
		}
	}
	if got := installerRead(t, agents); got != prefix+"\nKeep exact personal suffix\nFurther independent instructions\n" {
		t.Fatal("migration uninstall changed original prefix or personal suffix")
	}
}

func TestInstallerLegacyAdoptionRecoveryRestoresDirectory(t *testing.T) {
	for _, label := range []string{"after-legacy", "after-global", "after-state"} {
		t.Run(label, func(t *testing.T) {
			f := newInstallerFixture(t)
			legacy := filepath.Join(f.paths.Home, ".codex", "skills", "typesafe-ai")
			installerCopyTree(t, filepath.Join(f.paths.Source, text(object(f.manifest["registrations"])["typesafe-ai"])), legacy)
			before := installerSnapshot(t, f.paths.Home)
			i := f.installer(true)
			i.TypeSafeLegacy = new("")
			t.Setenv("AGENT_WORKFLOWS_FAULT", label)
			_, err := i.Setup(false)
			t.Setenv("AGENT_WORKFLOWS_FAULT", "")
			if err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
				t.Fatalf("legacy setup at %s: %v", label, err)
			}
			f.execute(t, "recover")
			installerUnchanged(t, f.paths.Home, before)
			if f.execute(t, "status")["installed"] != false {
				t.Fatal("adoption recovery retained installation ownership")
			}
		})
	}
}

func TestInstallerNativeMacOSAliasMigration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires native macOS maintained /tmp alias")
	}
	t.Setenv("TMPDIR", "/tmp")
	if Normalize("/tmp") != "/private/tmp" {
		t.Fatal("native macOS /tmp alias is unavailable; migration must execute")
	}
	f := newInstallerFixture(t)
	prior := filepath.Join(f.base, "prior suite")
	installerCopyTree(t, f.paths.Source, prior)
	name := keys(object(f.manifest["registrations"]))[0]
	raw := filepath.Join(prior, text(object(f.manifest["registrations"])[name]))
	if Normalize(raw) == raw {
		t.Fatal("temporary fixture did not traverse the native macOS alias")
	}
	path := filepath.Join(f.paths.Home, ".agents", "skills", name)
	if err := os.Symlink(raw, path); err != nil {
		t.Fatal(err)
	}
	i := f.installer(true)
	i.MigrateFrom = prior
	if _, err := i.Setup(false); err != nil {
		t.Fatal(err)
	}
	f.execute(t, "uninstall")
	if got, err := os.Readlink(path); err != nil || got != raw {
		t.Fatalf("original macOS alias target = %q, want %q: %v", got, raw, err)
	}
}

func TestInstallerPythonV1SealGolden(t *testing.T) {
	// Generated independently with Python json.dumps(sort_keys=True, indent=2,
	// ensure_ascii=True) + newline and hashlib.sha256, without engine helpers.
	const golden = `{
  "escaped": "<script>&\nquote\"",
  "history": [],
  "home": "/tmp/home ' \u03a9",
  "mode": 384,
  "nullable": null,
  "registrations": {},
  "release": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "truth": true,
  "unicode": "\u4f60\u597d\ud83d\ude00",
  "version": 1
}
`
	var value Object
	if err := json.Unmarshal([]byte(golden), &value); err != nil {
		t.Fatal(err)
	}
	if got := legacyJSON(value); !bytes.Equal(got, []byte(golden)) {
		t.Fatalf("Go serialization differs from Python v1: %s", got)
	}
	value["integrity_sha256"] = "8c860410bc3e156720b01d70257dbb25be48289bced612783207076be1a2c497"
	if err := verifySeal(value); err != nil {
		t.Fatalf("Python v1 receipt rejected: %v", err)
	}
	if got := seal(value); !reflect.DeepEqual(got, value) {
		t.Fatal("resealing changed a deployed v1 receipt")
	}
	value["home"] = "/different"
	if err := verifySeal(value); err == nil {
		t.Fatal("tampered Python v1 receipt accepted")
	}
}
