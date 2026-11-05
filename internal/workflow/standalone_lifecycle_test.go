package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// executeWithoutPanic runs one lifecycle command and turns a panic into a
// test failure that names the command.
func executeWithoutPanic(t *testing.T, i *Installer, command string) (report Object, err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s panicked: %v", command, recovered)
		}
	}()
	return i.Execute(command)
}

// R1.4: without an embedded bundle, setup, install and update fail before any
// write; status, rollback, recover and uninstall do not need the bundle.
func TestStandaloneMissingBundleRefusesBeforeWrites(t *testing.T) {
	t.Parallel()
	const missing = "has no embedded workflow payload"
	t.Run("fresh", func(t *testing.T) {
		t.Parallel()
		f := newInstallerFixture(t)
		f.seedUserFiles(t)
		before := standaloneTrees(t, f)
		for _, command := range []string{"setup", "install", "update"} {
			for _, apply := range []bool{false, true} {
				i := f.installer(apply)
				i.Bundle = nil
				_, err := executeWithoutPanic(t, i, command)
				want := missing
				if command == "update" {
					// Update reports the missing installation first.
					want = "no installation state exists"
				}
				if err == nil || !strings.Contains(err.Error(), want) || errors.Is(err, ErrLegacyInstallation) {
					t.Fatalf("%s (apply %t) without a bundle: %v; want %q", command, apply, err, want)
				}
				assertTreesUnchanged(t, before)
			}
		}
		// The same fixture installs once the bundle is present.
		f.execute(t, "setup")
	})
	t.Run("installed", func(t *testing.T) {
		t.Parallel()
		f := newInstallerFixture(t)
		original := f.seedUserFiles(t)
		f.execute(t, "setup")
		second := f.newRelease(t)
		before := standaloneTrees(t, f)
		for _, command := range []string{"setup", "install", "update"} {
			for _, apply := range []bool{false, true} {
				i := f.installer(apply)
				i.Bundle = nil
				if _, err := executeWithoutPanic(t, i, command); err == nil || !strings.Contains(err.Error(), missing) || errors.Is(err, ErrLegacyInstallation) {
					t.Fatalf("%s (apply %t) without a bundle: %v", command, apply, err)
				}
				assertTreesUnchanged(t, before)
			}
		}
		if f.activeRevision(t) != f.first {
			t.Fatal("refused update changed the active release")
		}
		f.execute(t, "update")
		if f.activeRevision(t) != second {
			t.Fatal("update with the bundle did not activate the second release")
		}
		withoutBundle := func(apply bool) *Installer {
			i := f.installer(apply)
			i.Bundle = nil
			return i
		}
		if report, err := executeWithoutPanic(t, withoutBundle(true), "status"); err != nil || report["installed"] != true {
			t.Fatalf("status without a bundle: %#v, %v", report, err)
		}
		if _, err := executeWithoutPanic(t, withoutBundle(true), "rollback"); err != nil {
			t.Fatalf("rollback without a bundle: %v", err)
		}
		if f.activeRevision(t) != f.first {
			t.Fatal("rollback without a bundle did not return to the first release")
		}
		f.failAt(t, "uninstall", "after-state")
		if report, err := executeWithoutPanic(t, withoutBundle(false), "recover"); err != nil || report["pending"] != true {
			t.Fatalf("recovery preview without a bundle: %#v, %v", report, err)
		}
		if _, err := executeWithoutPanic(t, withoutBundle(true), "recover"); err != nil {
			t.Fatalf("recover without a bundle: %v", err)
		}
		if _, err := executeWithoutPanic(t, withoutBundle(true), "uninstall"); err != nil {
			t.Fatalf("uninstall without a bundle: %v", err)
		}
		for path, want := range original {
			if got := installerRead(t, path); got != want {
				t.Fatalf("uninstall without a bundle changed %s: %q", path, got)
			}
		}
	})
}

// R1.4: a source checkout is no longer an installation mode.
func TestStandaloneSourceCheckoutIsRefusedBeforeWrites(t *testing.T) {
	t.Parallel()
	const refused = "source checkout installations are no longer supported"
	withSource := func(f *installerFixture, apply bool) *Installer {
		options := f.optionsFor(f.releases[f.current], apply)
		options.Source = f.source
		return NewInstaller(options)
	}
	t.Run("fresh", func(t *testing.T) {
		t.Parallel()
		f := newInstallerFixture(t)
		f.seedUserFiles(t)
		before := standaloneTrees(t, f)
		for _, command := range []string{"setup", "install"} {
			for _, apply := range []bool{false, true} {
				if _, err := withSource(f, apply).Execute(command); err == nil || !strings.Contains(err.Error(), refused) {
					t.Fatalf("%s (apply %t) with a source: %v", command, apply, err)
				}
				assertTreesUnchanged(t, before)
			}
		}
		f.execute(t, "setup")
	})
	t.Run("installed", func(t *testing.T) {
		t.Parallel()
		f := newInstallerFixture(t)
		f.seedUserFiles(t)
		f.execute(t, "setup")
		second := f.newRelease(t)
		before := standaloneTrees(t, f)
		for _, command := range []string{"update", "rollback"} {
			for _, apply := range []bool{false, true} {
				if _, err := withSource(f, apply).Execute(command); err == nil || !strings.Contains(err.Error(), refused) {
					t.Fatalf("%s (apply %t) with a source: %v", command, apply, err)
				}
				assertTreesUnchanged(t, before)
			}
		}
		f.execute(t, "update")
		if f.activeRevision(t) != second {
			t.Fatal("update without a source did not activate the second release")
		}
		before = standaloneTrees(t, f)
		for _, apply := range []bool{false, true} {
			if _, err := withSource(f, apply).Execute("rollback"); err == nil || !strings.Contains(err.Error(), refused) {
				t.Fatalf("rollback (apply %t) with a source: %v", apply, err)
			}
			assertTreesUnchanged(t, before)
		}
		f.execute(t, "rollback")
		if f.activeRevision(t) != f.first {
			t.Fatal("rollback without a source did not return to the first release")
		}
	})
}

// R1.5: lifecycle reports carry the standalone keys and none of the removed
// migration keys.
func TestStandaloneReportsOmitRemovedKeys(t *testing.T) {
	t.Parallel()
	documented := []string{"command", "dry_run", "changed", "configuration_committed", "preferences_applied", "config_dir", "base_identity", "effective_digest", "release"}
	removed := []string{"migration", "legacy_admission", "skill_prerequisites"}
	f := newInstallerFixture(t)
	f.seedUserFiles(t)
	check := func(command string, apply bool) {
		t.Helper()
		report, err := f.installer(apply).Execute(command)
		if err != nil {
			t.Fatalf("%s (apply %t): %v", command, apply, err)
		}
		for _, key := range documented {
			if _, found := report[key]; !found {
				t.Fatalf("%s (apply %t) report lacks %s: %#v", command, apply, key, report)
			}
		}
		if report["dry_run"] != !apply {
			t.Fatalf("%s (apply %t) dry_run = %#v", command, apply, report["dry_run"])
		}
		if _, found := report["generation"]; apply && !found {
			t.Fatalf("%s apply report lacks generation: %#v", command, report)
		}
		for _, key := range removed {
			if _, found := report[key]; found {
				t.Fatalf("%s (apply %t) report still has %s: %#v", command, apply, key, report[key])
			}
		}
	}
	check("install", false)
	check("setup", false)
	check("setup", true)
	check("install", true)
	f.newRelease(t)
	check("update", false)
	check("update", true)
	view := reflect.TypeOf(ConfigurationView{})
	if _, found := view.FieldByName("Migrating"); found {
		t.Fatal("ConfigurationView still has Migrating")
	}
	for index := range view.NumField() {
		if tag := strings.Split(view.Field(index).Tag.Get("json"), ",")[0]; strings.Contains(strings.ToLower(tag), "migrat") {
			t.Fatalf("ConfigurationView still encodes %s", tag)
		}
	}
}

// R2.7: a host home at or below <skills root>/<registration> is refused before
// any write, for setup and install and for an update that adds the skill.
func TestStandaloneHostHomeOnSkillPathRefusesBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"setup", "install"} {
		t.Run(command+" codex home on a skill", func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			skills := filepath.Join(f.paths.Home, ".codex", "skills")
			f.paths.Codex = filepath.Join(skills, "code-review")
			want := fmt.Sprintf("home %s sits on the %s skill in %s; choose a home outside that path", f.paths.Codex, "code-review", skills)
			before := standaloneTrees(t, f)
			for _, apply := range []bool{false, true} {
				i := f.installer(apply)
				i.Hosts = []string{hostCodex}
				if _, err := i.Execute(command); err == nil || err.Error() != want {
					t.Fatalf("apply %t: %v; want %q", apply, err, want)
				}
				assertTreesUnchanged(t, before)
			}
		})
		t.Run(command+" undecided spelling", func(t *testing.T) {
			t.Parallel()
			f := claudeFixture(t)
			f.paths.Claude = filepath.Join(f.paths.Home, ".agents", "skills", "CODE-REVIEW")
			want := fmt.Sprintf("cannot tell whether home %s sits on the %s skill until it exists; create the home and run this again", f.paths.Claude, "code-review")
			before := standaloneTrees(t, f)
			for _, apply := range []bool{false, true} {
				i := f.installer(apply)
				i.Hosts = []string{hostCodex, hostClaude}
				if _, err := i.Execute(command); err == nil || err.Error() != want {
					t.Fatalf("apply %t: %v; want %q", apply, err, want)
				}
				assertTreesUnchanged(t, before)
			}
		})
		t.Run(command+" home clear of the skill", func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.paths.Codex = filepath.Join(f.paths.Home, ".codex", "skills", "codex-config")
			i := f.installer(true)
			i.Hosts = []string{hostCodex}
			if _, err := i.Execute(command); err != nil {
				t.Fatalf("a home beside the skills installs: %v", err)
			}
		})
	}
	t.Run("update adding the skill", func(t *testing.T) {
		t.Parallel()
		f := historicalRegistrationFixture(t)
		skills := filepath.Join(f.paths.Home, ".agents", "skills")
		name := backendFixtureNames[0]
		f.paths.Claude = filepath.Join(skills, name)
		i := f.installer(true)
		i.Hosts = []string{hostCodex, hostClaude}
		if _, err := i.Setup(true); err != nil {
			t.Fatalf("the home was clear of the installed skills: %v", err)
		}
		addBackendFixtureRegistrations(t, f)
		want := fmt.Sprintf("home %s sits on the %s skill in %s; choose a home outside that path", f.paths.Claude, name, skills)
		before := standaloneTrees(t, f)
		for _, apply := range []bool{false, true} {
			// Update acts on the recorded hosts; the new bundle's skill is
			// where the recorded Claude home sits.
			if _, err := f.installer(apply).Update(); err == nil || err.Error() != want {
				t.Fatalf("apply %t: %v; want %q", apply, err, want)
			}
			assertTreesUnchanged(t, before)
		}
		if f.activeRevision(t) != f.first {
			t.Fatal("refused update changed the active release")
		}
	})
}

// R2.6: a full standalone lifecycle on both hosts restores every original
// file byte for byte.
func TestStandaloneLifecycleRestoresOriginalFiles(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	f.seedUserFiles(t)
	seedClaudeFiles(t, f)
	files := func() map[string]string {
		entries := map[string]string{}
		for path, entry := range fixtureTree(t, f.paths.Home) {
			if strings.HasPrefix(entry, "file ") || strings.HasPrefix(entry, "symlink ") {
				entries[path] = entry
			}
		}
		return entries
	}
	original := files()
	i := f.installer(true)
	i.Hosts = []string{hostCodex, hostClaude}
	report, err := i.Setup(true)
	if err != nil {
		t.Fatal(err)
	}
	state := f.readState(t)
	if source, found := state["source"]; integer(state["version"]) != 4 || !found || source != "" {
		t.Fatalf("installed state is not version 4 without a source: %v, %#v", state["version"], state["source"])
	}
	if integer(object(state["manager"])["version"]) != 2 || stateRevision(state) != f.first {
		t.Fatalf("installed manager or base: %#v, %s", state["manager"], stateRevision(state))
	}
	locator, err := readJSON(i.locatorPath())
	if err != nil || integer(locator["version"]) != 3 {
		t.Fatalf("runtime locator: %#v, %v", locator, err)
	}
	receipt, err := readJSON(filepath.Join(fixtureRuntimeDir(t, f.paths.State, f.first), "receipt.json"))
	if err != nil || integer(receipt["version"]) != 3 || receipt["manager_protocol"] != ManagerProtocol {
		t.Fatalf("runtime receipt: %#v, %v", receipt, err)
	}
	installation, err := RecordedInstallation(f.paths.State)
	want := Paths{Home: f.paths.Home, Codex: f.paths.Codex, Claude: f.paths.Claude, State: f.paths.State}
	if err != nil || installation.Paths != want || installation.ConfigDir != f.config || installation.StateVersion != 4 || !installation.Installed || installation.Pending || installation.Generation == "" || installation.Generation != text(report["generation"]) {
		t.Fatalf("RecordedInstallation = %#v, %v; report generation %#v", installation, err, report["generation"])
	}
	second := f.newRelease(t)
	if _, err := f.installer(true).Update(); err != nil {
		t.Fatal(err)
	}
	state = f.readState(t)
	if stateRevision(state) != second || text(object(object(state["manager"])["runtime"])["revision"]) != second {
		t.Fatalf("update did not move the release and manager to %s: %#v", second, state["manager"])
	}
	if _, err := f.installer(true).Rollback(); err != nil {
		t.Fatal(err)
	}
	state = f.readState(t)
	if stateRevision(state) != f.first || text(object(object(state["manager"])["runtime"])["revision"]) != second {
		t.Fatalf("rollback must return the skills and keep the newer manager: %s, %#v", stateRevision(state), state["manager"])
	}
	if f.statusRevision(t) != f.first {
		t.Fatal("status disagrees with the rolled-back state")
	}
	if _, err := f.installer(true).Uninstall(); err != nil {
		t.Fatal(err)
	}
	if got := files(); !reflect.DeepEqual(got, original) {
		for path, entry := range got {
			if original[path] != entry {
				t.Errorf("%s: %q, originally %q", path, entry, original[path])
			}
		}
		for path := range original {
			if _, found := got[path]; !found {
				t.Errorf("%s was removed", path)
			}
		}
		t.Fatal("uninstall did not restore the original files")
	}
	for _, path := range []string{i.statePath, i.journalPath, i.current, i.commandPath(), i.locatorPath(), i.runtimeCurrent()} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
	if f.execute(t, "status")["installed"] != false {
		t.Fatal("status still reports an installation")
	}
}
