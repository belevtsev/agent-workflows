package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMutationPathsPhysicalAncestry(t *testing.T) {
	t.Parallel()
	base := Normalize(t.TempDir())
	one, two := filepath.Join(base, "one"), filepath.Join(base, "two")
	for _, path := range []string{one, two, filepath.Join(base, "é-left"), filepath.Join(base, "é-right")} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"link-one", "link-two"} {
		if err := os.Symlink(one, filepath.Join(base, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name       string
		a, b       string
		overlap    bool
		unresolved bool
	}{
		{"same missing leaf", filepath.Join(one, "new"), filepath.Join(one, "new"), true, false},
		{"missing ancestor", filepath.Join(one, "new"), filepath.Join(one, "new", "child"), true, false},
		{"existing ancestor", one, filepath.Join(one, "new", "child"), true, false},
		{"separate ancestors", filepath.Join(one, "new"), filepath.Join(two, "new"), false, false},
		{"separate leaves", filepath.Join(one, "new"), filepath.Join(one, "other"), false, false},
		{"prefix only", filepath.Join(one, "new"), filepath.Join(one, "newer", "child"), false, false},
		{"distinct leaves below folded missing parents", filepath.Join(one, "new", "left"), filepath.Join(one, "NEW", "right"), false, false},
		{"missing case alias ancestor", filepath.Join(one, "new"), filepath.Join(one, "NEW", "child"), false, true},
		{"missing Unicode alias ancestor", filepath.Join(one, "codéx"), filepath.Join(one, "codéx", "child"), false, true},
		{"missing Unicode table alias", filepath.Join(one, "straße"), filepath.Join(one, "STRASSE", "child"), false, true},
		{"created separate Unicode directories", filepath.Join(base, "é-left", "new"), filepath.Join(base, "é-right", "new"), false, false},
		{"separate links to one target", filepath.Join(base, "link-one"), filepath.Join(base, "link-two"), false, false},
		{"link and target entry", filepath.Join(base, "link-one"), one, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, paths := range [][2]string{{test.a, test.b}, {test.b, test.a}} {
				overlap, err := pathsOverlap(paths[0], paths[1])
				if overlap != test.overlap || (err != nil) != test.unresolved {
					t.Fatalf("pathsOverlap(%q, %q) = %t, %v", paths[0], paths[1], overlap, err)
				}
			}
		})
	}
	for name, alias := range map[string]string{"case alias": filepath.Join(base, "ONE"), "data volume alias": onDataVolume(one)} {
		t.Run(name, func(t *testing.T) {
			requireOneDirectory(t, one, alias)
			for _, pair := range [][2]string{{one, filepath.Join(alias, "child")}, {filepath.Join(one, "new"), filepath.Join(alias, "new", "child")}} {
				for _, paths := range [][2]string{pair, {pair[1], pair[0]}} {
					overlap, err := pathsOverlap(paths[0], paths[1])
					if err != nil || !overlap {
						t.Fatalf("physical ancestor was missed: %q, %q: %t, %v", paths[0], paths[1], overlap, err)
					}
				}
			}
		})
	}
}

func TestSetupRefusesOverlappingNativeDestinationsBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		roots func(*testing.T, *installerFixture)
	}{
		{"same spelling", func(_ *testing.T, f *installerFixture) {
			f.paths.Claude = filepath.Join(f.paths.Home, ".local", "bin", "cw")
		}},
		{"missing folded parents", func(_ *testing.T, f *installerFixture) {
			f.paths.Claude = filepath.Join(f.paths.Home, ".local", "BIN", "CW")
		}},
		{"existing case alias parent", func(t *testing.T, f *installerFixture) {
			bin := filepath.Join(f.paths.Home, ".local", "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(f.paths.Home, ".local", "BIN")
			requireOneDirectory(t, bin, alias)
			f.paths.Claude = filepath.Join(alias, "CW")
		}},
		{"existing Unicode alias parent", func(t *testing.T, f *installerFixture) {
			f.paths.Home = filepath.Join(f.base, "homé")
			if err := os.MkdirAll(f.paths.Home, 0o700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(f.base, "homé")
			requireOneDirectory(t, f.paths.Home, alias)
			f.paths.Claude = filepath.Join(alias, ".local", "bin", "cw")
		}},
		{"data volume alias parent", func(t *testing.T, f *installerFixture) {
			requireOneDirectory(t, f.paths.Home, onDataVolume(f.paths.Home))
			f.paths.Claude = filepath.Join(onDataVolume(f.paths.Home), ".local", "bin", "cw")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, deferred := range []bool{false, true} {
				t.Run(map[bool]string{false: "ready runtime", true: "deferred runtime"}[deferred], func(t *testing.T) {
					t.Parallel()
					f := claudeFixture(t)
					test.roots(t, f)
					i := nativeFixtureInstaller(t, f, f.first, false)
					i.Hosts, i.Shell = []string{hostClaude}, "none"
					calls := 0
					if deferred {
						i.RuntimeCandidate = ""
						i.PrepareRuntime = func(context.Context, string, string, string) (RuntimeCandidate, error) {
							calls++
							return RuntimeCandidate{}, errors.New("runtime acquisition must follow destination preflight")
						}
					}
					before := installerSnapshot(t, f.base)
					var previewError string
					for _, apply := range []bool{false, true} {
						i.Apply = apply
						_, err := i.Setup(true)
						if err == nil || (!strings.Contains(err.Error(), "overlapping mutation paths") && !strings.Contains(err.Error(), "cannot tell whether mutation paths")) {
							t.Fatalf("apply %t accepted an overlapping command and host: %v", apply, err)
						}
						if !apply {
							previewError = err.Error()
						} else if err.Error() != previewError {
							t.Fatalf("preview and apply refused different layouts: %q / %q", previewError, err)
						}
						installerUnchanged(t, f.base, before)
					}
					if calls != 0 {
						t.Fatalf("runtime acquisition started before destination refusal: %d calls", calls)
					}
				})
			}
		})
	}
}

func TestMutationPathsExistingFoldedNamesFollowFilesystemIdentity(t *testing.T) {
	t.Parallel()
	base := Normalize(t.TempDir())
	for _, names := range [][2]string{{"created", "CREATED"}, {"codéx", "codéx"}} {
		left, right := filepath.Join(base, names[0]), filepath.Join(base, names[1])
		for _, path := range []string{left, right} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		leftInfo, err := os.Stat(left)
		if err != nil {
			t.Fatal(err)
		}
		rightInfo, err := os.Stat(right)
		if err != nil {
			t.Fatal(err)
		}
		overlap, err := pathsOverlap(filepath.Join(left, "new"), filepath.Join(right, "new", "child"))
		if err != nil || overlap != os.SameFile(leftInfo, rightInfo) {
			t.Fatalf("existing directory identity was ignored for %q / %q: %t, %v", left, right, overlap, err)
		}
	}
}

func TestNativeSetupAcceptsDistinctSiblingDestinations(t *testing.T) {
	t.Parallel()
	f := claudeFixture(t)
	f.paths.Claude = filepath.Join(f.paths.Home, ".local", "bin", "cw-host")
	f.paths.State = filepath.Join(f.paths.Home, ".local", "bin", "cw-state")
	i := nativeFixtureInstaller(t, f, f.first, false)
	i.Hosts, i.Shell = []string{hostClaude}, "none"
	before := installerSnapshot(t, f.base)
	if _, err := i.Setup(true); err != nil {
		t.Fatalf("separate sibling preview refused: %v", err)
	}
	installerUnchanged(t, f.base, before)
	i.Apply = true
	if _, err := i.Setup(true); err != nil {
		t.Fatalf("separate sibling setup refused: %v", err)
	}
	if _, err := i.Status(); err != nil {
		t.Fatalf("separate sibling ownership is invalid: %v", err)
	}
	if _, err := i.Uninstall(); err != nil {
		t.Fatalf("separate sibling uninstall refused: %v", err)
	}
}

func TestTransactionRefusesPhysicalOverlapForEverySinglePathKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"path", "global", "global_recovery", "manager_path", "command_profile", "model_config", sharedConfigOperationKind, claudeSettingsOperationKind} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			i := f.installer(true)
			parent := Normalize(filepath.Join(f.base, "destination"))
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			alias := onDataVolume(parent)
			// This test also runs on case-sensitive filesystems using the same
			// spelling; the alias-only cases above retain their physical check.
			if left, err := os.Stat(alias); err != nil {
				alias = parent
			} else if right, err := os.Stat(parent); err != nil || !os.SameFile(left, right) {
				alias = parent
			}
			first := pathOperation(filepath.Join(parent, "new"), Object{"kind": "absent"}, Object{"kind": "absent"}, "first")
			first["kind"] = kind
			second := pathOperation(filepath.Join(alias, "new", "child"), Object{"kind": "absent"}, Object{"kind": "absent"}, "second")
			before := installerSnapshot(t, f.base)
			if err := i.transact("setup", []Object{first, second}); err == nil || !strings.Contains(err.Error(), "overlapping mutation paths") {
				t.Fatalf("%s transaction overlap accepted: %v", kind, err)
			}
			installerUnchanged(t, f.base, before)
		})
	}
}

func TestRecoveryRefusesPhysicalAliasPathsBeforeAnyReversal(t *testing.T) {
	t.Parallel()
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "original journal", true: "resumed recovery"}[resumed], func(t *testing.T) {
			t.Parallel()
			f := newInstallerFixture(t)
			f.seedUserFiles(t)
			f.failAt(t, "setup", "after-global")
			if resumed {
				f.failAt(t, "recover", "after-recover-global")
			}
			i := f.installer(false)
			alias := filepath.Join(filepath.Dir(i.skills), "SKILLS")
			requireOneDirectory(t, i.skills, alias)
			journal, err := readJSON(i.journalPath)
			if err != nil {
				t.Fatal(err)
			}
			field := "operations"
			if resumed {
				field = "recovery_operations"
			}
			operations := sequence(journal[field])
			var duplicate Object
			for _, raw := range operations {
				op := object(raw)
				if op["kind"] == "path" && filepath.Dir(text(op["path"])) == i.skills {
					duplicate = clone(op)
					duplicate["path"] = filepath.Join(alias, filepath.Base(text(op["path"])))
					break
				}
			}
			if duplicate == nil {
				t.Fatal("fixture contains no registration operation")
			}
			journal[field] = append(operations, duplicate)
			installerWrite(t, i.journalPath, string(legacyJSON(seal(journal))), 0o600)
			before := installerSnapshot(t, f.base)
			for _, apply := range []bool{false, true} {
				i.Apply = apply
				if _, err := i.Recover(); err == nil || !strings.Contains(err.Error(), "overlapping mutation paths") {
					t.Fatalf("apply %t accepted physical aliases in %s: %v", apply, field, err)
				}
				installerUnchanged(t, f.base, before)
			}
		})
	}
}
