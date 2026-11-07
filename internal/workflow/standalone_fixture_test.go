package workflow

import (
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// Installer fixtures drive the standalone manager the way a released cw does:
// every install, setup and update carries an embedded bundle and the manager
// executable that reports exactly that bundle's identity. The fixture keeps a
// small editable source tree only to pack new bundles; the installer never sees
// it (Paths.Source stays empty). Every home, state, config and candidate path
// belongs to the test directory.
type installerFixture struct {
	paths    Paths
	base     string
	source   string
	config   string
	first    string
	current  string
	manifest Object
	env      *fixtureEnv
	releases map[string]*fixtureRelease
	builds   int
}

// fixtureRelease is one packed bundle, its identity and a fake manager that
// answers the two calls the installer makes of a runtime candidate.
type fixtureRelease struct {
	data      []byte
	identity  bundled.Identity
	candidate string
}

func (r *fixtureRelease) provider() bundled.Provider {
	data := r.data
	return func(context.Context) (*bundled.Bundle, error) { return bundled.Open(data) }
}

func (r *fixtureRelease) runtimeIdentity() RuntimeIdentity {
	return RuntimeIdentity{Revision: r.identity.Revision, Version: r.identity.Version, OS: r.identity.OS, Arch: r.identity.Arch}
}

// fixtureEnv is one test's view of the variables the installer reads, so tests
// can change PATH or a fault label without touching the process environment.
// Unset names read as empty: the real PATH may hold a cw, and nothing else
// from the developer's environment belongs in a fixture.
type fixtureEnv struct {
	mu     sync.Mutex
	values map[string]string
}

func newFixtureEnv() *fixtureEnv {
	return &fixtureEnv{values: map[string]string{"AGENT_WORKFLOWS_FAULT": "", "PATH": "/usr/bin:/bin"}}
}

func (e *fixtureEnv) Getenv(name string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.values[name]
}

func (e *fixtureEnv) Setenv(name, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.values[name] = value
}

func newInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	base := filepath.Join(Normalize(t.TempDir()), "installer fixtures with spaces ' Ω")
	f := &installerFixture{
		base:     base,
		source:   filepath.Join(base, "release source"),
		config:   filepath.Join(base, "private config"),
		env:      newFixtureEnv(),
		releases: map[string]*fixtureRelease{},
	}
	f.paths = Paths{Home: filepath.Join(base, "user home"), Codex: filepath.Join(base, "user home", ".codex"), State: filepath.Join(base, "private state")}
	installerCopyExact(t, installerTemplate, f.source)
	// The command directory exists before install, as on a machine whose
	// startup files already name it; tests that need it missing remove it.
	for _, directory := range []string{f.paths.Codex, filepath.Join(f.paths.Home, ".agents", "skills"), filepath.Join(f.paths.Home, ".local", "bin")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.manifest, err = LoadManifest(f.source)
	if err != nil {
		t.Fatal(err)
	}
	f.first = f.addRelease(t, installerTemplateBundle)
	return f
}

// installerTemplateVersion and installerTemplateRevision identify the bundle
// TestMain packs once from the template source; every fixture starts with it.
const (
	installerTemplateVersion  = "v1.0.0"
	installerTemplateRevision = "5f0c3ad1e2b4c6d8e0f1a2b3c4d5e6f708192a3b"
)

// fixtureRevision gives every fixture release its own commit-shaped revision.
func fixtureRevision(base, version string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(base+"\x00"+version)))[:40]
}

// fixtureManagerScript answers the candidate calls the installer makes: the
// manager protocol and the exact bundle identity. Any other call is refused so
// an unexpected dependency on the fake shows up as a failure.
func fixtureManagerScript(t *testing.T, protocol string, identity bundled.Identity) string {
	t.Helper()
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	return "#!/bin/sh\ncase \"${1-}\" in\n" +
		"--manager-protocol) printf '%s\\n' '" + protocol + "' ;;\n" +
		"--bundle-identity) printf '%s\\n' '" + string(data) + "' ;;\n" +
		"*) printf 'fixture manager: unexpected call: %s\\n' \"$*\" >&2; exit 2 ;;\n" +
		"esac\n"
}

// fixtureShellQuote quotes a path for the fixture's /bin/sh scripts.
func fixtureShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func (f *installerFixture) addRelease(t *testing.T, data []byte) string {
	t.Helper()
	bundle, err := bundled.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	identity := bundle.Identity()
	candidate := filepath.Join(f.base, "manager candidates", identity.Version, "cw")
	installerWrite(t, candidate, fixtureManagerScript(t, ManagerProtocol, identity), 0o755)
	f.releases[identity.Revision] = &fixtureRelease{data: data, identity: identity, candidate: candidate}
	f.current = identity.Revision
	return identity.Revision
}

// tryCommit packs the source tree as the next, newer release. It replaces a
// Git commit of the old checkout fixture; later installers carry this bundle.
func (f *installerFixture) tryCommit(t *testing.T) (string, error) {
	t.Helper()
	f.builds++
	version := fmt.Sprintf("v1.0.%d", f.builds)
	data, err := bundled.Build(f.source, version, fixtureRevision(f.base, version))
	if err != nil {
		return "", err
	}
	return f.addRelease(t, data), nil
}

func (f *installerFixture) commit(t *testing.T) string {
	t.Helper()
	revision, err := f.tryCommit(t)
	if err != nil {
		t.Fatalf("pack fixture release: %v", err)
	}
	return revision
}

func (f *installerFixture) release(t *testing.T, revision string) *fixtureRelease {
	t.Helper()
	release := f.releases[revision]
	if release == nil {
		t.Fatalf("fixture has no release %q", revision)
	}
	return release
}

func (f *installerFixture) optionsFor(release *fixtureRelease, apply bool) Options {
	return Options{
		Paths: f.paths, Apply: apply, Shell: "bash", Getenv: f.env.Getenv, ConfigDir: f.config,
		Bundle: release.provider(), RuntimeCandidate: release.candidate, RuntimeIdentity: release.runtimeIdentity(),
	}
}

// installer carries the newest bundle, as the newest cw would.
func (f *installerFixture) installer(apply bool) *Installer {
	return NewInstaller(f.optionsFor(f.releases[f.current], apply))
}

func (f *installerFixture) installerFor(t *testing.T, revision string, apply bool) *Installer {
	t.Helper()
	return NewInstaller(f.optionsFor(f.release(t, revision), apply))
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

// readState reads the ownership record as written, without the installer's
// validation, so assertions can name the active base release.
func (f *installerFixture) readState(t *testing.T) Object {
	t.Helper()
	state, err := readJSON(filepath.Join(f.paths.State, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// stateRevision names the bundle revision of the base an ownership record
// activates. Standalone release names are effective-snapshot digests, so tests
// compare the base revision they packed rather than a predicted release name.
func stateRevision(state Object) string {
	return text(object(state["base"])["revision"])
}

func (f *installerFixture) activeRevision(t *testing.T) string {
	t.Helper()
	return stateRevision(f.readState(t))
}

// statusRevision runs status, checks it reports the release state records,
// and returns that release's base revision.
func (f *installerFixture) statusRevision(t *testing.T) string {
	t.Helper()
	status := f.execute(t, "status")
	state := f.readState(t)
	if status["release"] == nil || !reflect.DeepEqual(status["release"], state["release"]) {
		t.Fatalf("status release %#v differs from recorded release %#v", status["release"], state["release"])
	}
	return stateRevision(state)
}

// fixtureRuntimeDir is the sealed directory a release's manager was copied
// into: <State>/runtime/releases/<revision>-<digest>.
func fixtureRuntimeDir(t *testing.T, stateDir, revision string) string {
	t.Helper()
	root := filepath.Join(stateDir, "runtime", "releases")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var matches []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), revision+"-") {
			matches = append(matches, filepath.Join(root, entry.Name()))
		}
	}
	if len(matches) != 1 {
		t.Fatalf("runtime directories for %s: %q", revision, matches)
	}
	return matches[0]
}

// fixtureRewrite replaces a sealed file's bytes and keeps its mode, so a test
// changes exactly the contents it means to change.
func fixtureRewrite(t *testing.T, path string, data []byte) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
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
	for _, name := range []string{"cc-skills-golang", "codebase-design", "go-principal-engineer", "backend-security-review", "protobuf-contracts", "go-pki-mtls", "software-architecture", "code-review", "test-strategy", "security-threat-model", "task-orchestration", "typesafe-ai"} {
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
		// Released trees represent directories as 0755. Matching those modes
		// is material to exact adoption inventory checks.
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
		filepath.ToSlash(filepath.Join(filepath.Dir(text(manifest["model_policy"])), "references", "question-patterns.json")),
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
	// Claude agent definitions ship as released files; the fixture manifest
	// keeps the key, so every named definition must be present.
	for _, relative := range object(manifest["claude_agents"]) {
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

// installerCopyExact copies a tree with every entry's exact permission bits:
// inventories compare modes, so the umask must not decide them.
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

// fixtureTree records every entry below root, directories and links included,
// with its type, mode, contents digest or link target. Comparing two trees
// shows any created, removed or changed entry, which inventory comparisons of
// release content need not.
func fixtureTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		tree["."] = "absent"
		return tree
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			tree[relative] = "symlink " + target
		case info.IsDir():
			tree[relative] = "directory " + info.Mode().String()
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			tree[relative] = fmt.Sprintf("file %s %x", info.Mode().String(), sha256.Sum256(data))
		default:
			tree[relative] = "other " + info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// assertTreesUnchanged compares fixtureTree records and names each difference.
func assertTreesUnchanged(t *testing.T, before map[string]map[string]string) {
	t.Helper()
	for root, want := range before {
		got := fixtureTree(t, root)
		if reflect.DeepEqual(got, want) {
			continue
		}
		var differences []string
		for name, value := range got {
			if want[name] != value {
				differences = append(differences, fmt.Sprintf("%s: %q -> %q", name, want[name], value))
			}
		}
		for name, value := range want {
			if _, found := got[name]; !found {
				differences = append(differences, fmt.Sprintf("%s: %q -> removed", name, value))
			}
		}
		slices.Sort(differences)
		t.Fatalf("changes beneath %s:\n%s", root, strings.Join(differences, "\n"))
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

// newRelease packs an alternate valid release: new global instructions and a
// different Sol default.
func (f *installerFixture) newRelease(t *testing.T) string {
	t.Helper()
	installerAppend(t, filepath.Join(f.source, text(f.manifest["global_instructions"])), "\nNew fixture instructions\n")
	policy := filepath.Join(f.source, text(f.manifest["model_policy"]))
	editModelPolicy(t, policy, func(policy Object) {
		profile := policy["profiles"].(Object)["sol"].(Object)
		profile["model"], profile["reasoning_effort"] = "gpt-6-sol", "high"
		effort := policy["effort_policy"].(Object)
		effort["default_substantive"], effort["configured_max_profiles"] = "high", []any{}
	})
	return f.commit(t)
}
