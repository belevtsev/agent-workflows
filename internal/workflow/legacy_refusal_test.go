package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// R2.2: an installation written by a removed cw is refused, with one exact
// sentinel, before any write.

const legacyRefusalMessage = `this installation was made by an older cw that this release no longer supports; run "cw uninstall" with your existing cw, then install again`

// Commit-shaped release names, as the removed checkout installer recorded them.
const (
	legacyCommit       = "0123456789abcdef0123456789abcdef01234567"
	legacyParentCommit = "89abcdef0123456789abcdef0123456789abcdef"
)

// legacyInstallation is one state directory a removed cw could have left.
type legacyInstallation struct {
	name string
	// journal reports whether journal.json exists, which decides recovery.
	journal bool
	// codexHome reports whether RecordedHomes still reads the Codex home from
	// a valid version-4 state beside a legacy journal.
	codexHome bool
	// validJournal reports whether a valid version-4 journal records the
	// homes RecordedHomes returns: its envelope always names the Codex home
	// and the Claude root, which defaults to <Home>/.claude.
	validJournal bool
	build        func(*testing.T, *installerFixture)
}

func legacyRecord(t *testing.T, f *installerFixture, name string, record Object) {
	t.Helper()
	if err := os.MkdirAll(f.paths.State, 0o700); err != nil {
		t.Fatal(err)
	}
	installerWrite(t, filepath.Join(f.paths.State, name), string(legacyJSON(record)), 0o600)
}

// legacyStandaloneState installs with the current manager and rewrites the
// version-4 state as the removed migration would have left it.
func legacyStandaloneState(t *testing.T, f *installerFixture, mutate func(Object)) {
	t.Helper()
	f.execute(t, "setup")
	legacyMigratedState(t, f, mutate)
}

// legacyMigratedState rewrites the recorded version-4 state as the removed
// migration would have left it, resealed so only the legacy markers differ
// from a valid record.
func legacyMigratedState(t *testing.T, f *installerFixture, mutate func(Object)) {
	t.Helper()
	state := f.readState(t)
	if integer(state["version"]) != 4 {
		t.Fatalf("standalone state version %v", state["version"])
	}
	mutate(state)
	installerWrite(t, filepath.Join(f.paths.State, "state.json"), string(legacyJSON(seal(state))), 0o600)
}

// legacyVersion3State is the state.json the removed version-3 manager wrote.
func legacyVersion3State(f *installerFixture) Object {
	return Object{
		"version": 3, "source": filepath.Join(f.base, "legacy checkout"), "release": legacyCommit,
		"history": []any{Object{"release": legacyParentCommit}}, "hosts": Object{hostCodex: Object{"home": f.paths.Codex}},
		"integrity_sha256": strings.Repeat("0", 64),
	}
}

// interruptAfterJournal leaves a genuine version-4 journal: the standalone
// command stops right after writing it. An interrupted update also leaves the
// valid version-4 state it started from.
func interruptAfterJournal(t *testing.T, f *installerFixture, command string) {
	t.Helper()
	if command == "update" {
		f.execute(t, "setup")
		f.newRelease(t)
	}
	f.failAt(t, command, "after-journal")
	journal, err := readJSON(filepath.Join(f.paths.State, "journal.json"))
	if err != nil || integer(journal["version"]) != 4 {
		t.Fatalf("interrupted %s left journal %#v, %v", command, journal, err)
	}
}

func legacyInstallations() []legacyInstallation {
	unsealed := strings.Repeat("0", 64)
	return []legacyInstallation{
		{name: "state v1", build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "state.json", Object{"version": 1, "release": legacyCommit, "history": []any{}, "home": f.paths.Home, "registrations": Object{}, "integrity_sha256": unsealed})
		}},
		{name: "state v2", build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "state.json", Object{"version": 2, "release": legacyCommit, "history": []any{legacyParentCommit}, "home": f.paths.Home, "codex_home": f.paths.Codex, "registrations": Object{}, "integrity_sha256": unsealed})
		}},
		{name: "state v3", build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "state.json", legacyVersion3State(f))
		}},
		{name: "state v3 with only a version", build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "state.json", Object{"version": 3})
		}},
		{name: "journal v1", journal: true, build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "journal.json", Object{"version": 1, "command": "setup", "operations": []any{}, "integrity_sha256": unsealed})
		}},
		{name: "journal v2", journal: true, build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "journal.json", Object{"version": 2, "command": "update", "operations": []any{}, "integrity_sha256": unsealed})
		}},
		{name: "journal v3", journal: true, build: func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "journal.json", Object{"version": 3, "command": "update", "source": filepath.Join(f.base, "legacy checkout"), "operations": []any{}, "integrity_sha256": unsealed})
		}},
		{name: "journal v3 beside a version-4 state", journal: true, codexHome: true, build: func(t *testing.T, f *installerFixture) {
			f.execute(t, "setup")
			legacyRecord(t, f, "journal.json", Object{"version": 3, "command": "update", "operations": []any{}, "integrity_sha256": unsealed})
		}},
		{name: "version-4 state with a source", build: func(t *testing.T, f *installerFixture) {
			legacyStandaloneState(t, f, func(state Object) { state["source"] = filepath.Join(f.base, "legacy checkout") })
		}},
		{name: "version-4 state with a commit in its history", build: func(t *testing.T, f *installerFixture) {
			legacyStandaloneState(t, f, func(state Object) {
				state["history"] = append(sequence(state["history"]), Object{"release": legacyCommit, "base": state["base"]})
			})
		}},
		// Contract revision 3: a genuine version-4 journal beside a legacy
		// state. RecordedHomes reads both homes from the valid journal.
		{name: "valid journal from an interrupted setup beside state v3", journal: true, validJournal: true, build: func(t *testing.T, f *installerFixture) {
			interruptAfterJournal(t, f, "setup")
			legacyRecord(t, f, "state.json", legacyVersion3State(f))
		}},
		{name: "valid journal from an interrupted update beside state v3", journal: true, validJournal: true, build: func(t *testing.T, f *installerFixture) {
			interruptAfterJournal(t, f, "update")
			legacyRecord(t, f, "state.json", legacyVersion3State(f))
		}},
		{name: "valid journal beside a version-4 state with a source", journal: true, validJournal: true, build: func(t *testing.T, f *installerFixture) {
			interruptAfterJournal(t, f, "update")
			legacyMigratedState(t, f, func(state Object) { state["source"] = filepath.Join(f.base, "legacy checkout") })
		}},
		{name: "valid journal beside a version-4 state with a commit in its history", journal: true, validJournal: true, build: func(t *testing.T, f *installerFixture) {
			interruptAfterJournal(t, f, "update")
			legacyMigratedState(t, f, func(state Object) {
				state["history"] = append(sequence(state["history"]), Object{"release": legacyCommit, "base": state["base"]})
			})
		}},
	}
}

// standaloneTrees records every entry below Home, State and ConfigDir.
func standaloneTrees(t *testing.T, f *installerFixture) map[string]map[string]string {
	t.Helper()
	trees := map[string]map[string]string{}
	for _, root := range []string{f.paths.Home, f.paths.State, f.config} {
		trees[root] = fixtureTree(t, root)
	}
	return trees
}

func assertLegacyRefusal(t *testing.T, err error, operation string) {
	t.Helper()
	if !errors.Is(err, ErrLegacyInstallation) {
		t.Fatalf("%s: got %v, want ErrLegacyInstallation", operation, err)
	}
	if err.Error() != legacyRefusalMessage {
		t.Fatalf("%s: message %q, want %q", operation, err.Error(), legacyRefusalMessage)
	}
}

func TestLegacyInstallationsAreRefusedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	if ErrLegacyInstallation == nil || ErrLegacyInstallation.Error() != legacyRefusalMessage {
		t.Fatalf("sentinel message = %v", ErrLegacyInstallation)
	}
	for _, legacy := range legacyInstallations() {
		t.Run(legacy.name, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			legacy.build(t, f)
			before := standaloneTrees(t, f)
			for _, command := range []string{"setup", "install", "update", "status", "rollback", "uninstall"} {
				for _, apply := range []bool{false, true} {
					_, err := f.installer(apply).Execute(command)
					assertLegacyRefusal(t, err, command)
					assertTreesUnchanged(t, before)
				}
			}
			// The refusal precedes the missing-bundle error.
			for _, command := range []string{"setup", "install", "update"} {
				for _, apply := range []bool{false, true} {
					i := f.installer(apply)
					i.Bundle = nil
					_, err := i.Execute(command)
					assertLegacyRefusal(t, err, command+" without a bundle")
					assertTreesUnchanged(t, before)
				}
			}
			for _, apply := range []bool{false, true} {
				report, err := f.installer(apply).Execute("recover")
				if legacy.journal {
					assertLegacyRefusal(t, err, "recover")
				} else if err != nil || report["pending"] != false {
					t.Fatalf("recover without a journal: %#v, %v", report, err)
				}
				assertTreesUnchanged(t, before)
			}
			_, err := RecordedInstallation(f.paths.State)
			assertLegacyRefusal(t, err, "RecordedInstallation")
			codex, claude := RecordedHomes(f.paths.State)
			wantCodex, wantClaude := "", ""
			if legacy.codexHome || legacy.validJournal {
				wantCodex = f.paths.Codex
			}
			if legacy.validJournal {
				wantClaude = filepath.Join(f.paths.Home, ".claude")
			}
			if codex != wantCodex || claude != wantClaude {
				t.Fatalf("RecordedHomes = %q, %q; want %q, %q", codex, claude, wantCodex, wantClaude)
			}
			assertTreesUnchanged(t, before)
		})
	}
}

// The refusal is specific to legacy markers: a version-4 standalone state with
// an activation history of effective snapshots is an ordinary installation.
func TestStandaloneStateIsNotMistakenForLegacy(t *testing.T) {
	t.Parallel()
	f := newInstallerFixture(t)
	f.execute(t, "setup")
	second := f.newRelease(t)
	f.execute(t, "update")
	state := f.readState(t)
	if integer(state["version"]) != 4 || state["source"] != "" || len(sequence(state["history"])) == 0 {
		t.Fatalf("update left no version-4 history: %#v", state)
	}
	for _, raw := range sequence(state["history"]) {
		if commitSHA.MatchString(text(object(raw)["release"])) {
			t.Fatalf("standalone history names a commit: %#v", raw)
		}
	}
	installation, err := RecordedInstallation(f.paths.State)
	if err != nil || installation.StateVersion != 4 || !installation.Installed || installation.Pending {
		t.Fatalf("RecordedInstallation = %#v, %v", installation, err)
	}
	if codex, claude := RecordedHomes(f.paths.State); codex != f.paths.Codex || claude != "" {
		t.Fatalf("RecordedHomes = %q, %q", codex, claude)
	}
	for _, command := range []string{"status", "rollback", "update", "uninstall"} {
		if _, err := f.installer(true).Execute(command); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if command == "rollback" && f.activeRevision(t) != f.first {
			t.Fatal("rollback did not return to the first release")
		}
		if command == "update" && f.activeRevision(t) != second {
			t.Fatal("update did not return to the second release")
		}
	}
}

// Contract revision 3, R2.2: recovery refuses a genuine version-4 journal
// beside a legacy state and leaves the journal byte for byte. With the valid
// state put back, the same journal recovers, so the refusal comes from the
// legacy state alone.
func TestLegacyStateBesideAValidJournalRefusesRecovery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		legacy func(*testing.T, *installerFixture)
	}{
		{"state v3", func(t *testing.T, f *installerFixture) {
			legacyRecord(t, f, "state.json", legacyVersion3State(f))
		}},
		{"version-4 state with a source", func(t *testing.T, f *installerFixture) {
			legacyMigratedState(t, f, func(state Object) { state["source"] = filepath.Join(f.base, "legacy checkout") })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			interruptAfterJournal(t, f, "update")
			statePath := filepath.Join(f.paths.State, "state.json")
			journalPath := filepath.Join(f.paths.State, "journal.json")
			validState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			stateInfo, err := os.Stat(statePath)
			if err != nil {
				t.Fatal(err)
			}
			test.legacy(t, f)
			journal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			before := standaloneTrees(t, f)
			for _, apply := range []bool{false, true} {
				_, err := f.installer(apply).Execute("recover")
				assertLegacyRefusal(t, err, fmt.Sprintf("recover (apply %t)", apply))
				if got, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(got, journal) {
					t.Fatalf("recover (apply %t) changed the journal: %v", apply, err)
				}
				assertTreesUnchanged(t, before)
			}
			// Control: the same journal beside the valid state recovers.
			fixtureRewrite(t, statePath, validState)
			if err := os.Chmod(statePath, stateInfo.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if report, err := f.installer(false).Execute("recover"); err != nil || report["pending"] != true {
				t.Fatalf("recovery preview with the valid state: %#v, %v", report, err)
			}
			f.execute(t, "recover")
			if exists(journalPath) || f.activeRevision(t) != f.first || f.statusRevision(t) != f.first {
				t.Fatal("recovery with the valid state did not return to the first release")
			}
		})
	}
}

// D2: the new cw refuses an installation whose active release ships a policy
// older than version 3, with the #18 refusal, before any write.

// activeReleasePolicy returns the model policy inside the active immutable
// release, located the way tamperActiveRelease locates a skill file there.
func activeReleasePolicy(t *testing.T, f *installerFixture) string {
	t.Helper()
	registration := text(object(f.manifest["registrations"])["task-orchestration"])
	policy := text(f.manifest["model_policy"])
	relative, found := strings.CutPrefix(policy, registration+"/")
	if registration == "" || !found {
		t.Fatalf("model policy %q is not inside the task-orchestration registration %q", policy, registration)
	}
	path := filepath.Join(f.installer(false).target(registration), filepath.FromSlash(relative))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("active release policy: %v", err)
	}
	var recorded Object
	if err := yaml.Unmarshal(data, &recorded); err != nil || recorded["version"] != 3 {
		t.Fatalf("active release policy %s is not the version-3 policy: %v, %v", path, recorded["version"], err)
	}
	return path
}

// version2PolicyBytes is the version-2 policy the previous release shipped.
func version2PolicyBytes(t *testing.T) []byte {
	t.Helper()
	policy := v3Policy(t)
	version2Policy(policy)
	data, err := yaml.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// installVersion2PolicyRelease leaves an installation whose active release
// ships the version-2 policy and returns the policy path and its version-3
// bytes, so a control can restore them.
//
// SEAM (open question to the coordinator): a release written by the previous
// cw cannot be packed or installed by this cw's fixtures, so this rewrites
// the policy inside the active release in place. That matches a real old
// installation only if the refusal is decided before, or independently of,
// any release-integrity check. If that does not hold, replace this helper
// with a fixture that produces an integrity-consistent installation of a
// version-2 release.
func installVersion2PolicyRelease(t *testing.T, f *installerFixture, journal bool) (string, []byte) {
	t.Helper()
	if journal {
		interruptAfterJournal(t, f, "update")
	} else {
		f.execute(t, "setup")
	}
	path := activeReleasePolicy(t, f)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixtureRewrite(t, path, version2PolicyBytes(t))
	return path, current
}

func TestVersion2PolicyInstallationIsRefusedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	for _, journal := range []bool{false, true} {
		t.Run(fmt.Sprintf("journal %t", journal), func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			path, current := installVersion2PolicyRelease(t, f, journal)
			before := standaloneTrees(t, f)
			// The same lifecycle commands #18 refuses for a legacy
			// installation, previewed and applied.
			for _, command := range []string{"setup", "install", "update", "status", "rollback", "uninstall"} {
				for _, apply := range []bool{false, true} {
					_, err := f.installer(apply).Execute(command)
					assertLegacyRefusal(t, err, fmt.Sprintf("%s (apply %t)", command, apply))
					assertTreesUnchanged(t, before)
				}
			}
			// As in #18, the refusal precedes the missing-bundle error.
			for _, command := range []string{"setup", "install", "update"} {
				for _, apply := range []bool{false, true} {
					i := f.installer(apply)
					i.Bundle = nil
					_, err := i.Execute(command)
					assertLegacyRefusal(t, err, fmt.Sprintf("%s without a bundle (apply %t)", command, apply))
					assertTreesUnchanged(t, before)
				}
			}
			// Recovery must read the release it returns to, so a pending
			// journal is refused. Without a journal, #18 reports nothing
			// pending; either answer must leave the installation untouched.
			for _, apply := range []bool{false, true} {
				report, err := f.installer(apply).Execute("recover")
				switch {
				case journal:
					assertLegacyRefusal(t, err, fmt.Sprintf("recover (apply %t)", apply))
				case err != nil:
					assertLegacyRefusal(t, err, fmt.Sprintf("recover without a journal (apply %t)", apply))
				case report["pending"] != false:
					t.Fatalf("recover without a journal (apply %t): %#v", apply, report)
				}
				assertTreesUnchanged(t, before)
			}
			// Read-only: the recorded Codex home stays readable from the
			// valid version-4 state, as #18 keeps it for uninstalling with
			// the existing cw.
			if codex, _ := RecordedHomes(f.paths.State); codex != f.paths.Codex {
				t.Fatalf("RecordedHomes codex = %q; want %q", codex, f.paths.Codex)
			}
			assertTreesUnchanged(t, before)
			// Control: with the version-3 policy back, the same installation
			// is accepted, so the refusal came from the policy version alone.
			fixtureRewrite(t, path, current)
			if journal {
				f.execute(t, "recover")
			}
			if revision := f.statusRevision(t); revision != f.first {
				t.Fatalf("status after restoring the version-3 policy = %s; want %s", revision, f.first)
			}
		})
	}
}
