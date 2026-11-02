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
	"slices"
	"strings"
	"sync"
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
	env      *fixtureEnv
}

// fixtureEnv is one test's view of the variables the installer reads, so tests
// can change PATH or a fault label without touching the process environment.
type fixtureEnv struct {
	mu     sync.Mutex
	values map[string]string
}

func (e *fixtureEnv) Getenv(name string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if value, found := e.values[name]; found {
		return value
	}
	return os.Getenv(name)
}

func (e *fixtureEnv) Setenv(name, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.values[name] = value
}

func newInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "installer fixtures with spaces ' Ω")
	f := &installerFixture{base: base, env: &fixtureEnv{values: map[string]string{"AGENT_WORKFLOWS_FAULT": ""}}}
	f.paths = Paths{Source: filepath.Join(base, "source checkout"), Home: filepath.Join(base, "user home"),
		Codex: filepath.Join(base, "user home", ".codex"), State: filepath.Join(base, "private state")}
	installerCopyExact(t, installerTemplate, f.paths.Source)
	for _, directory := range []string{f.paths.Codex, filepath.Join(f.paths.Home, ".agents", "skills")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.first = installerTemplateHead
	var err error
	f.manifest, err = LoadManifest(f.paths.Source)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// installerBuildSource writes the maintained lifecycle inputs, with minimal
// skill metadata, without a testing.T so TestMain can build it once.
func installerBuildSource(maintained, destination string) error {
	manifest, err := LoadManifest(maintained)
	if err != nil {
		return err
	}
	// Lifecycle fixtures keep a stable registration subset as new personal
	// registrations are added to the maintained suite.
	manifest = clone(manifest)
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
	write := func(relative string, contents []byte, mode os.FileMode) error {
		target := filepath.Join(destination, filepath.FromSlash(relative))
		// Git archives represent tracked directories as 0755. Matching those
		// modes is material to exact legacy adoption inventory checks.
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, contents, mode); err != nil {
			return err
		}
		return os.Chmod(target, mode)
	}
	copyFile := func(relative string) error {
		source := filepath.Join(maintained, filepath.FromSlash(relative))
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		return write(relative, data, info.Mode().Perm())
	}
	for _, relative := range []string{
		".gitignore", "install.sh",
		text(manifest["global_instructions"]), text(manifest["model_policy"]), text(manifest["claude_model_policy"]),
		filepath.ToSlash(filepath.Join(filepath.Dir(text(manifest["model_policy"])), "references", "jev-questions.json")),
	} {
		if err := copyFile(relative); err != nil {
			return err
		}
	}
	for relative, contents := range map[string]string{
		"skills-manifest.json": string(legacyJSON(manifest)),
		personalCatalogName:    "{\"skills\":{},\"version\":1}",
		"LICENSE":              "Fixture permission notice.\n",
	} {
		if err := write(relative, []byte(contents), 0o644); err != nil {
			return err
		}
	}
	for _, relative := range sequence(manifest["required_licenses"]) {
		if err := copyFile(text(relative)); err != nil {
			return err
		}
	}
	for name, relative := range object(manifest["registrations"]) {
		metadata := "---\nname: " + name + "\ndescription: Isolated installer lifecycle fixture.\n---\n\n# Fixture skill\n"
		if err := write(filepath.ToSlash(filepath.Join(text(relative), "SKILL.md")), []byte(metadata), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// installerCopyExact copies a tree, .git included, with every entry's exact
// permission bits: inventories compare modes, so the umask must not decide them.
func installerCopyExact(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	directories := map[string]os.FileMode{}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
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
			directories[target] = info.Mode().Perm()
			return os.Mkdir(target, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fixture template contains nonregular file %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	// Restrictive directory modes would block writing their contents, so they
	// are applied once everything is copied.
	for directory, mode := range directories {
		if err := os.Chmod(directory, mode); err != nil {
			t.Fatal(err)
		}
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
	return NewInstaller(Options{Paths: f.paths, Apply: apply, Shell: "bash", Getenv: f.env.Getenv})
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
	f.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
	_, err := f.installer(true).Execute(command)
	f.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
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
	for _, conflict := range []string{"model", "global", "registration", "pointer", "release"} {
		t.Run(conflict, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.execute(t, "setup")
			i := f.installer(true)
			switch conflict {
			case "model":
				installerWrite(t, i.config, strings.ReplaceAll(installerRead(t, i.config), "gpt-6-astra", "user-edited"), 0o600)
			case "global":
				installerWrite(t, i.agents, strings.Replace(installerRead(t, i.agents), "# Working conventions", "# User edited managed conventions", 1), 0o600)
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
	labels = append(labels, "after-global", "after-model-config", "after-state")
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
	labels = append(labels, "after-global", "after-model-config", "after-pointer", "after-state")
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
	for _, target := range []string{"global", "model-config"} {
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
	for _, target := range []string{"global", "model", "pointer"} {
		t.Run(target, func(t *testing.T) {
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.execute(t, "setup")
			f.newRelease(t)
			f.failAt(t, "setup", "after-model-config")
			i := f.installer(true)
			path, old, replacement := i.agents, "New fixture instructions", "User changed owned block"
			switch target {
			case "model":
				path, old, replacement = i.config, "gpt-6-sol", "user-edited"
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
	for _, invalid := range []string{"checksum", "outside path", "full config write", "unowned model key", "pointer escape"} {
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
