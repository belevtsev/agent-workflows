package workflow

import (
	"path/filepath"
	"testing"
)

// Resealing separates schema rejection from checksum rejection. Each case
// starts after real owned changes were applied, so a no-op or late failure must
// also preserve the journal and every path already changed by the transaction.
func TestInstallerRejectsMalformedRecoveryOperationsBeforeAnyReversal(t *testing.T) {
	for name, value := range map[string]any{"false": false, "null": nil, "object": Object{}} {
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			fixture.failAt(t, "setup", "after-global")
			installer := fixture.installer(true)
			journal, err := readJSON(installer.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			journal["recovery_operations"] = value
			installerWrite(t, installer.journalPath, string(legacyJSON(seal(journal))), 0o600)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Recover(); err == nil {
				t.Fatal("recovery accepted a present nonarray recovery_operations field")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestInstallerRejectsMalformedUndoObservationsBeforeAnyReversal(t *testing.T) {
	cases := map[string]Object{
		"invalid base64":   {"kind": "file", "data": "not-base64", "mode": 0o600},
		"boolean mode":     {"kind": "file", "data": encode([]byte("original")), "mode": true},
		"negative mode":    {"kind": "file", "data": encode([]byte("original")), "mode": -1},
		"fractional mode":  {"kind": "file", "data": encode([]byte("original")), "mode": 384.5},
		"oversized mode":   {"kind": "file", "data": encode([]byte("original")), "mode": 0o10000},
		"unsupported kind": {"kind": "unsupported"},
		"empty symlink":    {"kind": "symlink", "target": ""},
		"NUL symlink":      {"kind": "symlink", "target": "original\x00target"},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			fixture.failAt(t, "setup", "after-global")
			installer := fixture.installer(true)
			journal, err := readJSON(installer.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			for _, raw := range sequence(journal["operations"]) {
				operation := object(raw)
				if operation["kind"] == "path" && filepath.Dir(text(operation["path"])) == installer.skills {
					operation["before"] = invalid
					changed = true
					break
				}
			}
			if !changed {
				t.Fatal("fixture contains no applied registration operation")
			}
			installerWrite(t, installer.journalPath, string(legacyJSON(seal(journal))), 0o600)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Recover(); err == nil {
				t.Fatal("recovery accepted a malformed undo observation")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestInstallerRejectsDuplicateRecoveryPathsBeforeAnyReversal(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		name := "original operations"
		if resumed {
			name = "resumed recovery operations"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			fixture.failAt(t, "setup", "after-global")
			if resumed {
				fixture.failAt(t, "recover", "after-recover-global")
			}
			installer := fixture.installer(true)
			journal, err := readJSON(installer.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			key := "operations"
			if resumed {
				key = "recovery_operations"
			}
			operations := sequence(journal[key])
			duplicated := false
			for _, raw := range operations {
				operation := object(raw)
				if operation["kind"] == "path" && filepath.Dir(text(operation["path"])) == installer.skills {
					journal[key] = append(operations, clone(operation))
					duplicated = true
					break
				}
			}
			if !duplicated {
				t.Fatal("fixture contains no pending registration reversal to duplicate")
			}
			installerWrite(t, installer.journalPath, string(legacyJSON(seal(journal))), 0o600)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Recover(); err == nil {
				t.Fatal("recovery accepted duplicate mutation paths")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}

func TestInstallerRejectsResealedMalformedStateBeforeAnyMutation(t *testing.T) {
	cases := map[string]func(Object){
		"null original": func(state Object) {
			for _, raw := range object(state["registrations"]) {
				object(raw)["original"] = nil
				break
			}
		},
		"unknown original kind": func(state Object) {
			for _, raw := range object(state["registrations"]) {
				object(raw)["original"] = Object{"kind": "unsupported"}
				break
			}
		},
		"empty original symlink": func(state Object) {
			for _, raw := range object(state["registrations"]) {
				object(raw)["original"] = Object{"kind": "symlink", "target": ""}
				break
			}
		},
		"unknown global origin": func(state Object) { state["global_origin"] = Object{"kind": "unsupported"} },
		"false model metadata":  func(state Object) { state["model_config"] = false },
		"null model metadata":   func(state Object) { state["model_config"] = nil },
		"nonarray history":      func(state Object) { state["history"] = Object{} },
		"malformed history":     func(state Object) { state["history"] = []any{false} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t)
			fixture.seedUserFiles(t)
			fixture.execute(t, "setup")
			installer := fixture.installer(true)
			state, err := readJSON(installer.statePath)
			if err != nil {
				t.Fatal(err)
			}
			mutate(state)
			installerWrite(t, installer.statePath, string(legacyJSON(seal(state))), 0o600)
			before := installerSnapshot(t, fixture.base)
			if _, err := installer.Status(); err == nil {
				t.Fatal("status accepted a malformed ownership state")
			}
			if _, err := installer.Uninstall(); err == nil {
				t.Fatal("uninstall accepted a malformed ownership state")
			}
			installerUnchanged(t, fixture.base, before)
		})
	}
}
