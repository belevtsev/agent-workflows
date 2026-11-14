package evidence

import (
	"errors"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
)

// A pattern that names a folder of the tree matches no entry, so a check
// would be satisfied for ever by one pass. It is an invalid declaration, and
// the message gives the pattern to write (second challenge, finding G9).
func TestKeysRefuseAPatternThatNamesAFolder(t *testing.T) {
	e := newEnv(t)
	e.write(decl.FileName, "version = 1\n\n[[check]]\nid = \"unit\"\nrun = [\"sh\", \"unit.sh\"]\npaths = [\"unit.sh\", \"src\"]\n")
	tree := e.commit("paths names a folder")
	file, err := Declaration(t.Context(), e.repo, tree)
	if err != nil {
		t.Fatalf("Declaration: %v", err)
	}
	_, err = Keys(t.Context(), e.repo, tree, file.Checks)
	var invalid *decl.Error
	if !errors.As(err, &invalid) {
		t.Fatalf("Keys: error %v, want a *decl.Error", err)
	}
	for _, want := range []string{decl.FileName + ":3:", "`src`", "`unit`", "names a folder", "`src/**`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not hold %q", err.Error(), want)
		}
	}
	if _, err := Judge(t.Context(), e.repo, e.store, tree, now, nil); !errors.As(err, &invalid) {
		t.Errorf("Judge: error %v, want a *decl.Error", err)
	}

	// A pattern for a file, and one for everything below the folder, are
	// valid; so is a pattern that names a folder the tree does not hold.
	for _, paths := range []string{`["src/app.txt"]`, `["src/**"]`, `["docs"]`, `["sr"]`} {
		e.write(decl.FileName, "version = 1\n\n[[check]]\nid = \"unit\"\nrun = [\"sh\", \"unit.sh\"]\npaths = "+paths+"\n")
		tree := e.commit("paths " + paths)
		file, err := Declaration(t.Context(), e.repo, tree)
		if err != nil {
			t.Fatalf("Declaration: %v", err)
		}
		if _, err := Keys(t.Context(), e.repo, tree, file.Checks); err != nil {
			t.Errorf("paths = %s: Keys: %v", paths, err)
		}
	}
}

// A tree holds a submodule as one entry, which no pattern for a file inside
// it matches. Every submodule entry therefore counts in the key of every
// check with paths: a check may read the submodule's files (second
// challenge, finding G3).
func TestKeysCountEverySubmoduleEntry(t *testing.T) {
	e := newEnv(t)
	first := e.git("rev-parse", "HEAD")
	e.git("update-index", "--add", "--cacheinfo", "160000,"+first+",vendor/lib")
	e.git("commit", "-q", "-m", "add vendor/lib")
	before := e.keys(e.tree())

	second := e.git("commit-tree", "-m", "another", e.tree())
	e.git("update-index", "--cacheinfo", "160000,"+second+",vendor/lib")
	e.git("commit", "-q", "-m", "move vendor/lib")
	after := e.keys(e.tree())

	if before["unit"] == after["unit"] {
		t.Errorf("the key of unit, whose paths do not match vendor/lib, did not change when the submodule moved: %s", after["unit"])
	}
	if before["lint"] == after["lint"] {
		t.Errorf("the key of lint, which has no paths, did not change when the submodule moved")
	}

	// A change outside paths that is no submodule still leaves the key.
	e.write("README.md", "changed\n")
	e.git("add", "README.md")
	e.git("commit", "-q", "-m", "readme")
	if got := e.keys(e.tree()); got["unit"] != after["unit"] {
		t.Errorf("a change of README.md changed the key of unit: %s -> %s", after["unit"], got["unit"])
	}
}
