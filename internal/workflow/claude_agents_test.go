package workflow

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Contract claude-agents-tests, revision 1. Every test names the requirement
// IDs (A1-A14) it covers.

// shippedAgentNames is the set of Claude agent definitions the maintained
// manifest ships (A14), sorted.
var shippedAgentNames = []string{"cw-github-worker", "cw-reviewer", "cw-test-author"}

const (
	agentUserFileName = "mine.md"
	agentUserFileData = "---\nname: mine\ndescription: The user's own agent.\n---\nMy own agent, not managed by cw.\n"
	agentUserFileMode = fs.FileMode(0o640)
)

// agentRelative is the only manifest value the contract allows for name.
func agentRelative(name string) string { return "global/claude-agents/" + name + ".md" }

// agentDefinitionText is a valid definition in the contract's format.
func agentDefinitionText(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\ndisallowedTools: Skill\n---\n" + body
}

// agentManifestDefinitions reads a manifest's claude_agents as name -> relative.
func agentManifestDefinitions(manifest Object) map[string]string {
	definitions := map[string]string{}
	for name, relative := range object(manifest["claude_agents"]) {
		definitions[name] = text(relative)
	}
	return definitions
}

// agentFixtureDefinitions reads the fixture's first-release definitions and
// refuses a fixture that lost them, so no test passes vacuously.
func agentFixtureDefinitions(t *testing.T, f *installerFixture) map[string]string {
	t.Helper()
	definitions := agentManifestDefinitions(f.manifest)
	if got := slices.Sorted(maps.Keys(definitions)); !slices.Equal(got, shippedAgentNames) {
		t.Fatalf("fixture release names agents %q; want %q", got, shippedAgentNames)
	}
	return definitions
}

// agentEditManifest rewrites a source tree's manifest through edit, keeping
// the file mode, the way the fixture template writes it.
func agentEditManifest(t *testing.T, root string, edit func(Object)) {
	t.Helper()
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("load manifest to edit: %v", err)
	}
	manifest = clone(manifest)
	edit(manifest)
	fixtureRewrite(t, filepath.Join(root, "skills-manifest.json"), legacyJSON(manifest))
}

// agentWriteDefinition writes a definition file below root at relative.
func agentWriteDefinition(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func agentDir(f *installerFixture) string { return filepath.Join(f.paths.Claude, "agents") }

func agentPath(f *installerFixture, name string) string {
	return filepath.Join(agentDir(f), name+".md")
}

func seedAgentUserFile(t *testing.T, f *installerFixture) string {
	t.Helper()
	path := filepath.Join(agentDir(f), agentUserFileName)
	installerWrite(t, path, agentUserFileData, agentUserFileMode)
	return path
}

// assertAgentUserFile checks A4: the user's own definition is untouched.
func assertAgentUserFile(t *testing.T, f *installerFixture, step string) {
	t.Helper()
	path := filepath.Join(agentDir(f), agentUserFileName)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != agentUserFileMode {
		t.Fatalf("after %s the user's %s is %v, %v; want a regular file with mode %v", step, path, info, err, agentUserFileMode)
	}
	if got := installerRead(t, path); got != agentUserFileData {
		t.Fatalf("after %s the user's %s changed: %q", step, path, got)
	}
}

// assertAgentLinks checks A1/A5/A6: exactly the definitions in want are
// linked, each through the current release pointer, and each reads as the
// matching source bytes. Regular files (user files) are ignored; any other
// symlink in the directory is an extra managed link.
func assertAgentLinks(t *testing.T, f *installerFixture, i *Installer, want map[string]string, contents map[string][]byte) {
	t.Helper()
	for name, relative := range want {
		path := agentPath(f, name)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("agent %s: %s is %v, %v; want a symlink", name, path, info, err)
		}
		target, err := os.Readlink(path)
		if err != nil || target != i.target(relative) {
			t.Fatalf("agent %s links to %q, %v; want %q", name, target, err, i.target(relative))
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, contents[name]) {
			t.Fatalf("agent %s reads %q, %v; want the release bytes %q", name, data, err, contents[name])
		}
	}
	entries, err := os.ReadDir(agentDir(f))
	if errors.Is(err, fs.ErrNotExist) && len(want) == 0 {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		name, found := strings.CutSuffix(entry.Name(), ".md")
		if _, wanted := want[name]; !found || !wanted {
			target, _ := os.Readlink(filepath.Join(agentDir(f), entry.Name()))
			t.Fatalf("unexpected agent link %s -> %s; want only %q", entry.Name(), target, slices.Sorted(maps.Keys(want)))
		}
	}
}

func assertNoAgentLinks(t *testing.T, f *installerFixture, names []string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Lstat(agentPath(f, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("agent %s still present: %v", name, err)
		}
	}
}

// assertAgentState checks A2: the Claude host record owns exactly want, each
// with the shared target and an absent original; an empty want means the
// record has no agents key at all.
func assertAgentState(t *testing.T, f *installerFixture, i *Installer, want map[string]string) {
	t.Helper()
	claude := object(object(f.readState(t)["hosts"])[hostClaude])
	if claude == nil {
		t.Fatal("state has no Claude host record")
	}
	if len(want) == 0 {
		if value, found := claude["agents"]; found {
			t.Fatalf("Claude host record has an agents key without definitions: %#v", value)
		}
		return
	}
	agents := object(claude["agents"])
	if got := keys(agents); !slices.Equal(got, slices.Sorted(maps.Keys(want))) {
		t.Fatalf("state owns agents %q; want %q", got, slices.Sorted(maps.Keys(want)))
	}
	for name, relative := range want {
		record := object(agents[name])
		original := object(record["original"])
		if !slices.Equal(keys(record), []string{"original", "target"}) || text(record["target"]) != i.target(relative) ||
			!slices.Equal(keys(original), []string{"kind"}) || text(original["kind"]) != "absent" {
			t.Fatalf("state record for %s = %#v; want target %q and original {kind: absent}", name, record, i.target(relative))
		}
	}
}

// agentSourceContents reads each definition's bytes from a source tree.
func agentSourceContents(t *testing.T, root string, definitions map[string]string) map[string][]byte {
	t.Helper()
	contents := map[string][]byte{}
	for name, relative := range definitions {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		contents[name] = data
	}
	return contents
}

// A1, A2, A4, A11: install links every definition, records ownership and
// reports it, creating the agents directory when missing.
func TestClaudeAgentsInstallLinksRecordsAndReports(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		command  string
		userFile bool
	}{
		{"setup into a home without an agents directory", "setup", false},
		{"install beside a user's own definition", "install", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := claudeFixture(t)
			definitions := agentFixtureDefinitions(t, f)
			contents := agentSourceContents(t, f.source, definitions)
			if test.userFile {
				seedAgentUserFile(t, f)
			} else if exists(agentDir(f)) {
				t.Fatal("fixture already has an agents directory")
			}
			i := f.hostInstaller(true, hostClaude)
			if got := i.host(hostClaude).agentDir; got != agentDir(f) {
				t.Fatalf("Claude agent directory = %q; want %q", got, agentDir(f))
			}
			var err error
			if test.command == "setup" {
				_, err = i.Setup(true)
			} else {
				_, err = i.Execute(test.command)
			}
			if err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(agentDir(f)); err != nil || !info.IsDir() {
				t.Fatalf("agents directory: %v, %v", info, err)
			}
			assertAgentLinks(t, f, i, definitions, contents)
			assertAgentState(t, f, i, definitions)
			if test.userFile {
				assertAgentUserFile(t, f, test.command)
			}
			report, err := f.hostInstaller(true).Status()
			if err != nil {
				t.Fatal(err)
			}
			claude := object(object(report["hosts"])[hostClaude])
			if claude == nil {
				t.Fatalf("status has no Claude host: %#v", report["hosts"])
			}
			if got := claude["agents_dir"]; got != agentDir(f) {
				t.Fatalf("status agents_dir = %#v; want %q", got, agentDir(f))
			}
			if got, ok := claude["agents"].([]string); !ok || !slices.Equal(got, shippedAgentNames) {
				t.Fatalf("status agents = %#v; want []string %q", claude["agents"], shippedAgentNames)
			}
		})
	}
}

// A3: install refuses an occupied definition path before recording anything.
func TestClaudeAgentsInstallRefusesOccupiedPaths(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"regular file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := claudeFixture(t)
			name := slices.Sorted(maps.Keys(agentFixtureDefinitions(t, f)))[0]
			path := agentPath(f, name)
			userTarget := filepath.Join(f.base, "user agent elsewhere.md")
			installerWrite(t, userTarget, "user agent elsewhere\n", 0o600)
			const userData = "---\nname: user\ndescription: The user's file.\n---\nUser body.\n"
			if kind == "regular file" {
				installerWrite(t, path, userData, 0o640)
			} else {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(userTarget, path); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.hostInstaller(true, hostClaude).Setup(true)
			if err == nil || !strings.Contains(err.Error(), "unrelated occupied agent definition") {
				t.Fatalf("setup over an occupied %s: %v", kind, err)
			}
			if kind == "regular file" {
				info, statErr := os.Lstat(path)
				if statErr != nil || !info.Mode().IsRegular() || installerRead(t, path) != userData {
					t.Fatalf("the user's file changed: %v, %v", info, statErr)
				}
			} else if target, err := os.Readlink(path); err != nil || target != userTarget {
				t.Fatalf("the user's symlink changed: %q, %v", target, err)
			}
			if installerRead(t, userTarget) != "user agent elsewhere\n" {
				t.Fatal("setup wrote through the user's symlink")
			}
			if exists(filepath.Join(f.paths.State, "state.json")) {
				t.Fatal("refused setup left state.json")
			}
		})
	}
}

// A4, A5, A6, A7: update follows the new manifest exactly, rollback restores
// the earlier release's links and record, and uninstall removes them; the
// user's own definition survives every step.
func TestClaudeAgentsFollowUpdateRollbackAndUninstall(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	first := agentFixtureDefinitions(t, f)
	firstContents := agentSourceContents(t, f.source, first)
	seedAgentUserFile(t, f)
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	assertAgentLinks(t, f, i, first, firstContents)
	keptTargets := map[string]string{}
	for name := range first {
		keptTargets[name], _ = os.Readlink(agentPath(f, name))
	}

	const dropped, added = "cw-github-worker", "cw-extra"
	agentWriteDefinition(t, f.source, agentRelative(added), agentDefinitionText(added, "Extra fixture agent.", "You are an extra fixture agent.\n"))
	agentEditManifest(t, f.source, func(manifest Object) {
		agents := Object{}
		for name, relative := range first {
			if name != dropped {
				agents[name] = relative
			}
		}
		agents[added] = agentRelative(added)
		manifest["claude_agents"] = agents
	})
	second := maps.Clone(first)
	delete(second, dropped)
	second[added] = agentRelative(added)
	secondContents := agentSourceContents(t, f.source, second)
	revision := f.commit(t)

	f.execute(t, "update")
	if got := f.activeRevision(t); got != revision {
		t.Fatalf("update activated %s, want %s", got, revision)
	}
	assertNoAgentLinks(t, f, []string{dropped})
	assertAgentLinks(t, f, i, second, secondContents)
	for name := range second {
		if name == added {
			continue
		}
		if target, _ := os.Readlink(agentPath(f, name)); target != keptTargets[name] {
			t.Fatalf("update changed kept agent %s: %q -> %q", name, keptTargets[name], target)
		}
	}
	assertAgentState(t, f, i, second)
	assertAgentUserFile(t, f, "update")

	f.execute(t, "rollback")
	if got := f.activeRevision(t); got != f.first {
		t.Fatalf("rollback activated %s, want %s", got, f.first)
	}
	assertNoAgentLinks(t, f, []string{added})
	assertAgentLinks(t, f, i, first, firstContents)
	assertAgentState(t, f, i, first)
	assertAgentUserFile(t, f, "rollback")

	if _, err := f.hostInstaller(true).Uninstall(); err != nil {
		t.Fatal(err)
	}
	assertNoAgentLinks(t, f, slices.Concat(slices.Collect(maps.Keys(first)), []string{added}))
	assertAgentLinks(t, f, i, nil, nil)
	assertAgentUserFile(t, f, "uninstall")
}

// A2, A4, A6: a release without claude_agents records no agents key; updating
// to one with definitions links them, and rolling back removes every link and
// the key.
func TestClaudeAgentsRollbackToAReleaseWithoutDefinitions(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	definitions := agentFixtureDefinitions(t, f)
	contents := agentSourceContents(t, f.source, definitions)
	agentEditManifest(t, f.source, func(manifest Object) { delete(manifest, "claude_agents") })
	without := f.commit(t)
	seedAgentUserFile(t, f)
	i := f.hostInstaller(true, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	if got := f.activeRevision(t); got != without {
		t.Fatalf("setup activated %s, want the release without definitions %s", got, without)
	}
	names := slices.Collect(maps.Keys(definitions))
	assertNoAgentLinks(t, f, names)
	assertAgentState(t, f, i, nil)

	agentEditManifest(t, f.source, func(manifest Object) {
		agents := Object{}
		for name, relative := range definitions {
			agents[name] = relative
		}
		manifest["claude_agents"] = agents
	})
	with := f.commit(t)
	f.execute(t, "update")
	if got := f.activeRevision(t); got != with {
		t.Fatalf("update activated %s, want %s", got, with)
	}
	assertAgentLinks(t, f, i, definitions, contents)
	assertAgentState(t, f, i, definitions)

	f.execute(t, "rollback")
	if got := f.activeRevision(t); got != without {
		t.Fatalf("rollback activated %s, want %s", got, without)
	}
	assertNoAgentLinks(t, f, names)
	assertAgentLinks(t, f, i, nil, nil)
	assertAgentState(t, f, i, nil)
	assertAgentUserFile(t, f, "rollback")
}

// A4, A7, A10: removing only the Claude host removes its agent links and
// leaves Codex installed; the Codex record never owns agents.
func TestClaudeAgentsUninstallingClaudeKeepsCodex(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	definitions := agentFixtureDefinitions(t, f)
	contents := agentSourceContents(t, f.source, definitions)
	seedAgentUserFile(t, f)
	i := f.hostInstaller(true, hostCodex, hostClaude)
	if _, err := i.Setup(true); err != nil {
		t.Fatal(err)
	}
	assertAgentLinks(t, f, i, definitions, contents)
	if value, found := object(object(f.readState(t)["hosts"])[hostCodex])["agents"]; found {
		t.Fatalf("Codex host record owns agents: %#v", value)
	}
	if _, err := f.hostInstaller(true, hostClaude).Uninstall(); err != nil {
		t.Fatal(err)
	}
	assertNoAgentLinks(t, f, slices.Collect(maps.Keys(definitions)))
	assertAgentLinks(t, f, i, nil, nil)
	assertAgentUserFile(t, f, "uninstalling Claude")
	report, err := f.hostInstaller(true).Status()
	if err != nil || report["installed"] != true || object(report["hosts"])[hostCodex] == nil || object(report["hosts"])[hostClaude] != nil {
		t.Fatalf("Codex stopped working after removing Claude: %#v, %v", report, err)
	}
	if _, err := f.hostInstaller(true).Uninstall(); err != nil {
		t.Fatal(err)
	}
	assertAgentUserFile(t, f, "uninstall")
}

// A8: a changed owned link stops status, update and uninstall, and stays as
// the user left it.
func TestClaudeAgentsChangedLinkIsRefused(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"deleted", "regular file", "symlink elsewhere"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := claudeFixture(t)
			name := slices.Sorted(maps.Keys(agentFixtureDefinitions(t, f)))[0]
			if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
				t.Fatal(err)
			}
			path := agentPath(f, name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			elsewhere := filepath.Join(f.base, "elsewhere agent.md")
			installerWrite(t, elsewhere, "elsewhere\n", 0o600)
			const replaced = "user replaced the managed agent\n"
			switch change {
			case "regular file":
				installerWrite(t, path, replaced, 0o640)
			case "symlink elsewhere":
				if err := os.Symlink(elsewhere, path); err != nil {
					t.Fatal(err)
				}
			}
			assertChanged := func(step string) {
				t.Helper()
				info, err := os.Lstat(path)
				switch change {
				case "deleted":
					if !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("after %s the deleted link is back: %v, %v", step, info, err)
					}
				case "regular file":
					if err != nil || !info.Mode().IsRegular() || installerRead(t, path) != replaced {
						t.Fatalf("after %s the user's file changed: %v, %v", step, info, err)
					}
				case "symlink elsewhere":
					if target, err := os.Readlink(path); err != nil || target != elsewhere {
						t.Fatalf("after %s the user's symlink changed: %q, %v", step, target, err)
					}
				}
				if installerRead(t, elsewhere) != "elsewhere\n" {
					t.Fatalf("after %s a file outside the agents directory changed", step)
				}
			}
			f.newRelease(t)
			for _, step := range []struct {
				name string
				run  func() (Object, error)
			}{
				{"status", func() (Object, error) { return f.hostInstaller(true).Status() }},
				{"update", func() (Object, error) { return f.hostInstaller(true).Execute("update") }},
				{"uninstall", func() (Object, error) { return f.hostInstaller(true).Uninstall() }},
			} {
				if _, err := step.run(); err == nil || !strings.Contains(err.Error(), "owned agent definition changed") {
					t.Fatalf("%s with a %s agent link: %v", step.name, change, err)
				}
				assertChanged(step.name)
			}
		})
	}
}

// A4, A9: an interrupted setup or uninstall at each agent boundary is
// recovered to the state before the command.
func TestClaudeAgentsRecoverEveryAgentBoundary(t *testing.T) {
	t.Parallel()
	names := shippedAgentNames
	for _, command := range []string{"setup", "uninstall"} {
		for _, name := range names {
			label := "after-agent:" + name + ":claude"
			t.Run(command+"/"+label, func(t *testing.T) {
				t.Parallel()
				f := claudeFixture(t)
				definitions := agentFixtureDefinitions(t, f)
				contents := agentSourceContents(t, f.source, definitions)
				seedClaudeFiles(t, f)
				seedAgentUserFile(t, f)
				if command == "uninstall" {
					if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
						t.Fatal(err)
					}
				}
				i := f.hostInstaller(true, hostClaude)
				if command == "uninstall" {
					i.Hosts = nil
				}
				f.env.Setenv("AGENT_WORKFLOWS_FAULT", label)
				_, err := i.Execute(command)
				f.env.Setenv("AGENT_WORKFLOWS_FAULT", "")
				if err == nil || !strings.Contains(err.Error(), "injected failure at "+label) {
					t.Fatalf("%s with %s: %v", command, label, err)
				}
				if _, err := i.Recover(); err != nil {
					t.Fatalf("recover: %v", err)
				}
				assertAgentUserFile(t, f, "recovery")
				if command == "setup" {
					assertNoAgentLinks(t, f, names)
					if exists(filepath.Join(f.paths.State, "state.json")) {
						t.Fatal("recovered setup left state.json")
					}
					return
				}
				if !exists(filepath.Join(f.paths.State, "state.json")) {
					t.Fatal("recovered uninstall did not restore state.json")
				}
				assertAgentLinks(t, f, i, definitions, contents)
				assertAgentState(t, f, i, definitions)
				if _, err := f.hostInstaller(true).Status(); err != nil {
					t.Fatalf("status after recovery: %v", err)
				}
			})
		}
	}
}

// A10: a Codex host never gets agent definitions or an agents record.
func TestClaudeAgentsNeverInstallForCodex(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		claude bool
	}{
		{"Claude home configured, Codex selected", true},
		{"plain Codex fixture", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var f *installerFixture
			var i *Installer
			if test.claude {
				f = claudeFixture(t)
				i = f.hostInstaller(true, hostCodex)
			} else {
				f = newInstallerFixture(t)
				i = f.installer(true)
			}
			agentFixtureDefinitions(t, f)
			if _, err := i.Setup(true); err != nil {
				t.Fatal(err)
			}
			directories := []string{filepath.Join(f.paths.Home, ".claude", "agents")}
			if f.paths.Claude != "" {
				directories = append(directories, agentDir(f))
			}
			for _, directory := range directories {
				if exists(directory) {
					t.Fatalf("a Codex install created %s", directory)
				}
			}
			hosts := object(f.readState(t)["hosts"])
			codex := object(hosts[hostCodex])
			if codex == nil || hosts[hostClaude] != nil {
				t.Fatalf("unexpected host records: %#v", hosts)
			}
			if value, found := codex["agents"]; found {
				t.Fatalf("Codex host record owns agents: %#v", value)
			}
		})
	}
}

// agentSuiteSource copies the fixture template: a full source tree that
// LoadManifest and ValidateSuite accept.
func agentSuiteSource(t *testing.T) string {
	t.Helper()
	root := filepath.Join(Normalize(t.TempDir()), "suite source")
	installerCopyExact(t, installerTemplate, root)
	return root
}

// A12: the manifest and every definition are validated by LoadManifest and
// ValidateSuite.
func TestClaudeAgentsManifestValidation(t *testing.T) {
	t.Parallel()
	t.Run("valid tree", func(t *testing.T) {
		t.Parallel()
		root := agentSuiteSource(t)
		manifest, err := LoadManifest(root)
		if err != nil {
			t.Fatalf("LoadManifest rejected the fixture tree: %v", err)
		}
		if got := slices.Sorted(maps.Keys(agentManifestDefinitions(manifest))); !slices.Equal(got, shippedAgentNames) {
			t.Fatalf("LoadManifest claude_agents = %q", got)
		}
		report, err := ValidateSuite(root)
		if err != nil {
			t.Fatalf("ValidateSuite rejected the fixture tree: %v", err)
		}
		if got, ok := report["claude_agents"].(int); !ok || got != len(shippedAgentNames) {
			t.Fatalf("ValidateSuite claude_agents = %#v; want int %d", report["claude_agents"], len(shippedAgentNames))
		}
	})
	t.Run("valid tree with an added definition", func(t *testing.T) {
		t.Parallel()
		root := agentSuiteSource(t)
		agentWriteDefinition(t, root, agentRelative("cw-extra"), agentDefinitionText("cw-extra", "Extra fixture agent.", "Extra body.\n"))
		agentEditManifest(t, root, func(manifest Object) {
			agents := clone(object(manifest["claude_agents"]))
			agents["cw-extra"] = agentRelative("cw-extra")
			manifest["claude_agents"] = agents
		})
		if _, err := LoadManifest(root); err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		report, err := ValidateSuite(root)
		if err != nil {
			t.Fatalf("ValidateSuite: %v", err)
		}
		if got, ok := report["claude_agents"].(int); !ok || got != len(shippedAgentNames)+1 {
			t.Fatalf("ValidateSuite claude_agents = %#v; want int %d", report["claude_agents"], len(shippedAgentNames)+1)
		}
	})
	t.Run("manifest without claude_agents", func(t *testing.T) {
		t.Parallel()
		root := agentSuiteSource(t)
		agentEditManifest(t, root, func(manifest Object) { delete(manifest, "claude_agents") })
		if _, err := LoadManifest(root); err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		if _, err := ValidateSuite(root); err != nil {
			t.Fatalf("ValidateSuite: %v", err)
		}
	})

	const reviewer = "cw-reviewer"
	definition := func(frontmatter, body string) string { return "---\n" + frontmatter + "---\n" + body }
	rewriteReviewer := func(contents string) func(*testing.T, string, Object) any {
		return func(t *testing.T, root string, agents Object) any {
			fixtureRewrite(t, filepath.Join(root, filepath.FromSlash(agentRelative(reviewer))), []byte(contents))
			return agents
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, root string, agents Object) any
	}{
		{"empty mapping", func(*testing.T, string, Object) any { return Object{} }},
		{"value names another definition", func(_ *testing.T, _ string, agents Object) any {
			agents[reviewer] = agentRelative("cw-test-author")
			return agents
		}},
		{"value outside the agents directory", func(t *testing.T, root string, agents Object) any {
			agentWriteDefinition(t, root, "global/"+reviewer+".md", agentDefinitionText(reviewer, "Moved reviewer.", "Body.\n"))
			agents[reviewer] = "global/" + reviewer + ".md"
			return agents
		}},
		{"value not in canonical form", func(_ *testing.T, _ string, agents Object) any {
			agents[reviewer] = "./" + agentRelative(reviewer)
			return agents
		}},
		{"absolute value", func(_ *testing.T, root string, agents Object) any {
			agents[reviewer] = filepath.Join(root, filepath.FromSlash(agentRelative(reviewer)))
			return agents
		}},
		{"uppercase name", func(t *testing.T, root string, agents Object) any {
			agentWriteDefinition(t, root, agentRelative("CW-Upper"), agentDefinitionText("CW-Upper", "Uppercase agent.", "Body.\n"))
			agents["CW-Upper"] = agentRelative("CW-Upper")
			return agents
		}},
		{"escaping name", func(t *testing.T, root string, agents Object) any {
			agentWriteDefinition(t, root, "global/x.md", agentDefinitionText("../x", "Escaping agent.", "Body.\n"))
			agents["../x"] = agentRelative("../x")
			return agents
		}},
		{"missing file", func(_ *testing.T, _ string, agents Object) any {
			agents["cw-missing"] = agentRelative("cw-missing")
			return agents
		}},
		{"frontmatter name differs from the key", rewriteReviewer(agentDefinitionText("cw-other", "Fixture reviewer.", "Body.\n"))},
		{"disallowedTools names another tool", rewriteReviewer(definition("name: cw-reviewer\ndescription: Fixture reviewer.\ndisallowedTools: Read\n", "Body.\n"))},
		{"disallowedTools as a flow list", rewriteReviewer(definition("name: cw-reviewer\ndescription: Fixture reviewer.\ndisallowedTools: [Skill]\n", "Body.\n"))},
		{"disallowedTools as a block list", rewriteReviewer(definition("name: cw-reviewer\ndescription: Fixture reviewer.\ndisallowedTools:\n  - Skill\n", "Body.\n"))},
		{"model key", rewriteReviewer(definition("name: cw-reviewer\ndescription: Fixture reviewer.\ndisallowedTools: Skill\nmodel: opus\n", "Body.\n"))},
		{"effort key", rewriteReviewer(definition("name: cw-reviewer\ndescription: Fixture reviewer.\ndisallowedTools: Skill\neffort: high\n", "Body.\n"))},
		{"empty quoted description", rewriteReviewer(definition("name: cw-reviewer\ndescription: \"\"\ndisallowedTools: Skill\n", "Body.\n"))},
		{"missing description value", rewriteReviewer(definition("name: cw-reviewer\ndescription:\ndisallowedTools: Skill\n", "Body.\n"))},
		{"empty body", rewriteReviewer(agentDefinitionText(reviewer, "Fixture reviewer.", ""))},
		{"no frontmatter", rewriteReviewer("You are a reviewer without frontmatter.\n")},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			t.Parallel()
			root := agentSuiteSource(t)
			baseline, err := LoadManifest(root)
			if err != nil {
				t.Fatalf("baseline fixture tree rejected: %v", err)
			}
			agents := Object{}
			for name, relative := range agentManifestDefinitions(baseline) {
				agents[name] = relative
			}
			value := test.mutate(t, root, agents)
			manifest := clone(baseline)
			manifest["claude_agents"] = value
			fixtureRewrite(t, filepath.Join(root, "skills-manifest.json"), legacyJSON(manifest))
			if _, err := LoadManifest(root); err == nil {
				t.Error("LoadManifest accepted the invalid definition")
			}
			if _, err := ValidateSuite(root); err == nil {
				t.Error("ValidateSuite accepted the invalid definition")
			}
		})
	}
}

// agentParseDefinition splits a definition into its frontmatter lines as
// key -> raw value and its body, independently of the implementation.
func agentParseDefinition(t *testing.T, path string) (map[string]string, string) {
	t.Helper()
	data := installerRead(t, path)
	rest, found := strings.CutPrefix(data, "---\n")
	if !found {
		t.Fatalf("%s has no frontmatter", path)
	}
	frontmatter, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		t.Fatalf("%s has no closing frontmatter delimiter", path)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.HasPrefix(line, " ") {
			t.Fatalf("%s: frontmatter line %q is not a plain key", path, line)
		}
		if _, duplicate := fields[key]; duplicate {
			t.Fatalf("%s: duplicate frontmatter key %q", path, key)
		}
		fields[key] = strings.TrimSpace(value)
	}
	return fields, body
}

// A14: the maintained manifest ships exactly the three definitions, each in
// the contract's format with no model or effort key.
func TestClaudeAgentsShippedDefinitions(t *testing.T) {
	t.Parallel()
	maintained, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(maintained)
	if err != nil {
		t.Fatalf("maintained manifest rejected: %v", err)
	}
	definitions := agentManifestDefinitions(manifest)
	if got := slices.Sorted(maps.Keys(definitions)); !slices.Equal(got, shippedAgentNames) {
		t.Fatalf("maintained claude_agents = %q; want %q", got, shippedAgentNames)
	}
	for _, name := range shippedAgentNames {
		if definitions[name] != agentRelative(name) {
			t.Fatalf("%s maps to %q; want %q", name, definitions[name], agentRelative(name))
		}
		path := filepath.Join(maintained, filepath.FromSlash(agentRelative(name)))
		if !strings.Contains(installerRead(t, path), "\ndisallowedTools: Skill\n") {
			t.Fatalf("%s lacks the line disallowedTools: Skill", path)
		}
		fields, body := agentParseDefinition(t, path)
		if got := slices.Sorted(maps.Keys(fields)); !reflect.DeepEqual(got, []string{"description", "disallowedTools", "name"}) {
			t.Fatalf("%s frontmatter keys = %q; want only name, description, disallowedTools (no model or effort)", path, got)
		}
		if fields["name"] != name || fields["description"] == "" || fields["disallowedTools"] != "Skill" || strings.TrimSpace(body) == "" {
			t.Fatalf("%s is not a valid definition: %q, body %q", path, fields, body)
		}
	}
}

// agentLinkDirectory replaces <Claude>/agents with a symlink to a real
// directory elsewhere that holds a user's file, and returns the directory and
// a record of the symlink and the directory's tree.
func agentLinkDirectory(t *testing.T, f *installerFixture) (string, map[string]map[string]string) {
	t.Helper()
	target := filepath.Join(f.base, "real agents elsewhere")
	installerWrite(t, filepath.Join(target, "theirs.md"), "---\nname: theirs\ndescription: Elsewhere.\n---\nTheirs.\n", 0o640)
	if err := os.MkdirAll(f.paths.Claude, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(agentDir(f)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, agentDir(f)); err != nil {
		t.Fatal(err)
	}
	return target, map[string]map[string]string{target: fixtureTree(t, target)}
}

func assertAgentDirectoryLink(t *testing.T, f *installerFixture, want string, before map[string]map[string]string) {
	t.Helper()
	if got, err := os.Readlink(agentDir(f)); err != nil || got != want {
		t.Fatalf("agents directory symlink = %q, %v; want %q", got, err, want)
	}
	assertTreesUnchanged(t, before)
}

// agentReleaseWithout packs the fixture source without claude_agents.
func agentReleaseWithout(t *testing.T, f *installerFixture) string {
	t.Helper()
	agentEditManifest(t, f.source, func(manifest Object) { delete(manifest, "claude_agents") })
	return f.commit(t)
}

// A16: a symlinked agents directory is left alone while no definitions are
// owned, and refused before writes once the target release names any.
func TestClaudeAgentsSymlinkedAgentsDirectory(t *testing.T) {
	t.Parallel()
	const refusal = "to be a real directory, not a symlink"
	t.Run("release without definitions", func(t *testing.T) {
		t.Parallel()
		f := claudeFixture(t)
		without := agentReleaseWithout(t, f)
		if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
			t.Fatal(err)
		}
		if got := f.activeRevision(t); got != without {
			t.Fatalf("setup activated %s, want %s", got, without)
		}
		target, before := agentLinkDirectory(t, f)
		i := f.hostInstaller(true)
		assertAgentState(t, f, i, nil)
		if _, err := f.hostInstaller(true).Status(); err != nil {
			t.Fatalf("status: %v", err)
		}
		assertAgentDirectoryLink(t, f, target, before)
		next := f.newRelease(t)
		if _, found := mustLoadAgentManifest(t, f.source)["claude_agents"]; found {
			t.Fatal("the next fixture release names definitions")
		}
		if _, err := f.hostInstaller(true).Execute("update"); err != nil {
			t.Fatalf("update to another release without definitions: %v", err)
		}
		if got := f.activeRevision(t); got != next {
			t.Fatalf("update activated %s, want %s", got, next)
		}
		assertAgentState(t, f, i, nil)
		assertAgentDirectoryLink(t, f, target, before)
		if _, err := f.hostInstaller(true).Uninstall(); err != nil {
			t.Fatalf("uninstall: %v", err)
		}
		assertAgentDirectoryLink(t, f, target, before)
	})
	t.Run("fresh install with definitions", func(t *testing.T) {
		t.Parallel()
		f := claudeFixture(t)
		agentFixtureDefinitions(t, f)
		target, before := agentLinkDirectory(t, f)
		_, err := f.hostInstaller(true, hostClaude).Setup(true)
		if err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("setup through a symlinked agents directory: %v", err)
		}
		assertAgentDirectoryLink(t, f, target, before)
		if exists(filepath.Join(f.paths.State, "state.json")) {
			t.Fatal("refused setup left state.json")
		}
	})
	t.Run("update to a release with definitions", func(t *testing.T) {
		t.Parallel()
		f := claudeFixture(t)
		definitions := agentFixtureDefinitions(t, f)
		without := agentReleaseWithout(t, f)
		if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
			t.Fatal(err)
		}
		target, before := agentLinkDirectory(t, f)
		agentEditManifest(t, f.source, func(manifest Object) {
			agents := Object{}
			for name, relative := range definitions {
				agents[name] = relative
			}
			manifest["claude_agents"] = agents
		})
		f.commit(t)
		_, err := f.hostInstaller(true).Execute("update")
		if err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("update through a symlinked agents directory: %v", err)
		}
		assertAgentDirectoryLink(t, f, target, before)
		if got := f.statusRevision(t); got != without {
			t.Fatalf("refused update left release %s active, want %s", got, without)
		}
		assertAgentState(t, f, f.hostInstaller(true), nil)
	})
}

func mustLoadAgentManifest(t *testing.T, root string) Object {
	t.Helper()
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// A17: each owned definition is read-only through its link; the repository's
// own definition files are not part of that rule and stay writable.
func TestClaudeAgentsInstalledDefinitionsAreReadOnly(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	definitions := agentFixtureDefinitions(t, f)
	if _, err := f.hostInstaller(true, hostClaude).Setup(true); err != nil {
		t.Fatal(err)
	}
	for name := range definitions {
		path := agentPath(f, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("agent %s is writable through its link: %v", name, info.Mode())
		}
		if os.Geteuid() == 0 {
			continue // root ignores permission bits
		}
		if file, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			_ = file.Close()
			t.Fatalf("agent %s opened for writing through its link", name)
		}
	}
	maintained, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range shippedAgentNames {
		info, err := os.Stat(filepath.Join(maintained, filepath.FromSlash(agentRelative(name))))
		if err != nil || info.Mode().Perm()&0o200 == 0 {
			t.Fatalf("repository definition %s lost its normal mode: %v, %v", name, info, err)
		}
	}
}
