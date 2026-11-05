package workflow

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// statusRegistrations reads the registration names a status report lists.
func statusRegistrations(t *testing.T, status Object) []string {
	t.Helper()
	switch names := status["registrations"].(type) {
	case []string:
		return names
	case []any:
		var result []string
		for _, name := range names {
			result = append(result, text(name))
		}
		return result
	}
	t.Fatalf("status has no registration list: %#v", status)
	return nil
}

func TestInstallerSetupDryRunRepeatAndSelectiveUninstall(t *testing.T) {
	t.Parallel()
	f := newInstallerFixture(t)
	original := f.seedUserFiles(t)
	before := installerSnapshot(t, f.base)
	preview, err := f.installer(false).Setup(true)
	if err != nil || preview["dry_run"] != true || preview["release"] == nil {
		t.Fatalf("dry setup: %#v, %v", preview, err)
	}
	installerUnchanged(t, f.base, before)
	applied := f.execute(t, "setup")
	if applied["dry_run"] != false || !reflect.DeepEqual(applied["release"], preview["release"]) {
		t.Fatalf("setup activated %#v; the preview named %#v", applied["release"], preview["release"])
	}
	status := f.execute(t, "status")
	state := f.readState(t)
	if status["installed"] != true || !reflect.DeepEqual(status["release"], state["release"]) || stateRevision(state) != f.first || len(statusRegistrations(t, status)) != len(object(f.manifest["registrations"])) {
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

// tamperActiveRelease changes one file inside the active immutable release.
func tamperActiveRelease(t *testing.T, i *Installer, manifest Object) {
	t.Helper()
	path := filepath.Join(i.target(text(object(manifest["registrations"])["code-review"])), "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		if err := os.Chmod(path, info.Mode().Perm()|0o200); err != nil {
			t.Fatal(err)
		}
	}
	installerAppend(t, path, "Changed immutable release\n")
}

func TestInstallerOwnedConflictsRejectBeforeAnyWrites(t *testing.T) {
	t.Parallel()
	for _, conflict := range []string{"model", "global", "registration", "pointer", "release"} {
		t.Run(conflict, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.execute(t, "setup")
			i := f.installer(true)
			switch conflict {
			case "model":
				original := installerRead(t, i.config)
				info, err := os.Stat(i.config)
				if err != nil {
					t.Fatal(err)
				}
				drifted := solDefaultsReplaceRootModel(t, original, "gpt-6.1-sol", "user-edited")
				installerWrite(t, i.config, drifted, info.Mode().Perm())
				actual := installerRead(t, i.config)
				if actual == original || actual != drifted || solDefaultsReplaceRootModel(t, actual, "user-edited", "gpt-6.1-sol") != original {
					t.Fatal("model conflict did not change exactly the root model value")
				}
				changedInfo, err := os.Stat(i.config)
				if err != nil {
					t.Fatal(err)
				}
				if changedInfo.Mode().Perm() != info.Mode().Perm() {
					t.Fatal("model conflict changed the config mode")
				}
			case "global":
				current := installerRead(t, i.agents)
				edited := strings.Replace(current, "# Working conventions", "# User edited managed conventions", 1)
				if edited == current {
					t.Fatal("fixture global instructions lack the managed heading")
				}
				installerWrite(t, i.agents, edited, 0o600)
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
				tamperActiveRelease(t, i, f.manifest)
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

func TestInstallerFreshSetupRecoversEveryActivationBoundary(t *testing.T) {
	t.Parallel()
	f := newInstallerFixture(t)
	labels := []string{"after-journal", "after-pointer"}
	for _, name := range keys(object(f.manifest["registrations"])) {
		labels = append(labels, "after-registration:"+name)
	}
	labels = append(labels, "after-global", "after-model-config", "after-state")
	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
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

// The two publication labels stop after the immutable base or effective
// snapshot is published and before the journal exists, so nothing needs
// recovery and the next setup resumes from the published stage.
func TestInstallerPublicationFailureCanResumeExactStage(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"after-base-publication", "after-effective-publication"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			before := installerSnapshot(t, f.paths.Home)
			f.failAt(t, "setup", label)
			installerUnchanged(t, f.paths.Home, before)
			if exists(filepath.Join(f.paths.State, "journal.json")) || exists(filepath.Join(f.paths.State, "state.json")) {
				t.Fatal("publication fault left a journal or an ownership record")
			}
			if report := f.execute(t, "recover"); report["pending"] != false {
				t.Fatalf("publication fault falsely required rollback recovery: %#v", report)
			}
			if f.execute(t, "status")["installed"] != false {
				t.Fatal("publication fault reported an installation")
			}
			f.execute(t, "setup")
			if f.activeRevision(t) != f.first || f.statusRevision(t) != f.first {
				t.Fatal("resumed setup did not activate the published release")
			}
		})
	}
}

func TestInstallerUninstallRecoversEveryMutationBoundary(t *testing.T) {
	t.Parallel()
	f := newInstallerFixture(t)
	labels := []string{"after-journal"}
	for _, name := range keys(object(f.manifest["registrations"])) {
		labels = append(labels, "after-registration:"+name)
	}
	labels = append(labels, "after-global", "after-model-config", "after-pointer", "after-state")
	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.execute(t, "setup")
			before := installerSnapshot(t, f.paths.Home)
			f.failAt(t, "uninstall", label)
			f.execute(t, "recover")
			installerUnchanged(t, f.paths.Home, before)
			if f.statusRevision(t) != f.first {
				t.Fatal("uninstall recovery failed to restore active release")
			}
			f.execute(t, "uninstall")
		})
	}
}

func TestInstallerInterruptedRecoveryKeepsIndependentEdits(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"global", "model-config"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	for _, target := range []string{"global", "model", "pointer"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.execute(t, "setup")
			f.newRelease(t)
			// Only update activates a newer bundle on an existing installation.
			f.failAt(t, "update", "after-model-config")
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
				current := installerRead(t, path)
				edited := strings.Replace(current, old, replacement, 1)
				if edited == current {
					t.Fatalf("interrupted update had not applied %q to %s", old, path)
				}
				installerWrite(t, path, edited, 0o600)
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
	t.Parallel()
	for _, invalid := range []string{"checksum", "outside path", "full config write", "unowned model key", "pointer escape"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.failAt(t, "setup", "after-journal")
			i := f.installer(true)
			j, err := readJSON(i.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			corrupted := invalid == "checksum"
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
				default:
					continue
				}
				corrupted = true
			}
			if !corrupted {
				t.Fatalf("journal has no operation to corrupt for %s", invalid)
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
	t.Parallel()
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
	t.Parallel()
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
