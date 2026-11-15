package skills_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func TestSkillYAMLAndFingerprints(t *testing.T) {
	f := newInventory(t)
	path := f.write("repo/.agents/skills/quoted/SKILL.md", "---\nname: 'quoted-name'\ndescription: >-\n  Contains a colon: and a # character.\n  Fold this line.\nmetadata:\n  nested: [one, two]\n---\n!`touch should-never-run`\n")
	f.write("repo/.agents/skills/quoted/scripts/run.sh", "#!/bin/sh\ntouch should-never-run\n")
	catalog := f.discover("codex")
	entry := findSkill(t, catalog, "quoted-name")
	if entry.Description != "Contains a colon: and a # character. Fold this line." {
		t.Fatalf("folded description = %q", entry.Description)
	}
	if entry.Path != path || entry.SHA256 != fileHash(t, path) {
		t.Fatalf("source or hash changed: %+v", entry)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "should-never-run")); !os.IsNotExist(err) {
		t.Fatalf("skill code executed: %v", err)
	}
	f.write("repo/.agents/skills/quoted/SKILL.md", skillText("quoted-name", "Changed instruction bytes."))
	resolved, err := catalog.Resolve([]string{entry.ID})
	if err != nil || len(resolved) != 1 || resolved[0].SHA256 == entry.SHA256 || resolved[0].SHA256 != fileHash(t, path) {
		t.Fatalf("resolve must fingerprint current content: %+v, %v", resolved, err)
	}
	f.write("repo/.agents/skills/quoted/SKILL.md", "---\nname: [broken\n---\n")
	if _, err := catalog.Resolve([]string{entry.ID}); err == nil {
		t.Fatal("changed malformed skill remained selectable")
	}
}

func TestSkillInvocationSemantics(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.claude/skills/manual/SKILL.md", "---\nname: manual\ndescription: Manual workflow.\ndisable-model-invocation: yes\n---\nManual instructions.\n")
	f.write("repo/.claude/skills/background/SKILL.md", "---\nname: background\ndescription: Background reference.\nuser-invocable: false\n---\nReference.\n")
	catalog := f.discover("claude")
	manual := findSkill(t, catalog, "manual")
	if !manual.ManualOnly || manual.Disabled || !manual.UserInvocable {
		t.Fatalf("manual-only does not mean disabled: %+v", manual)
	}
	if _, err := catalog.Resolve([]string{manual.ID}); err == nil || !strings.Contains(err.Error(), "manual-only") {
		t.Fatalf("automatic worker preload accepted manual-only: %v", err)
	}
	background := findSkill(t, catalog, "background")
	if background.Disabled || background.UserInvocable || background.ManualOnly {
		t.Fatalf("background reference metadata: %+v", background)
	}
	if _, err := catalog.Resolve([]string{background.ID}); err != nil {
		t.Fatalf("background skill cannot be selected: %v", err)
	}
}

func TestSkillMalformedEntriesAreDiagnostic(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.agents/skills/good/SKILL.md", skillText("good", "Valid sibling."))
	invalid := map[string]string{
		"bad-yaml":      "---\nname: [broken\n---\n",
		"duplicate-key": "---\nname: first\nname: second\ndescription: Duplicate.\n---\n",
		"missing-field": "---\nname: missing-field\n---\n",
		"bad-type":      "---\nname: [one, two]\ndescription: Wrong type.\n---\n",
		"bad-boolean":   "---\nname: bad-boolean\ndescription: Wrong boolean.\nuser-invocable: maybe\n---\n",
		"unfinished":    "---\nname: unfinished\ndescription: Missing end.\n",
		"large":         strings.Repeat("x", (1<<20)+1),
	}
	for name, text := range invalid {
		f.write("repo/.agents/skills/"+name+"/SKILL.md", text)
	}
	catalog := f.discover("codex")
	if len(catalog.Skills) != len(invalid)+1 || len(catalog.Warnings) != len(invalid) {
		t.Fatalf("malformed entries hid valid inventory: %+v", catalog)
	}
	if _, err := catalog.Resolve([]string{"good"}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog.Skills {
		if entry.Name == "good" {
			continue
		}
		if !entry.Disabled || entry.Reason == "" {
			t.Errorf("invalid entry is selectable: %+v", entry)
		}
		if _, err := catalog.Resolve([]string{entry.ID}); err == nil {
			t.Errorf("invalid entry selected: %+v", entry)
		}
	}
}

func TestSkillExplicitPathsPreserveOriginalDirectory(t *testing.T) {
	f := newInventory(t)
	path := f.write("external/source skill/SKILL.md", skillText("external", "Original resources."))
	catalog := f.discover("codex")
	resolved, err := catalog.Resolve([]string{path, filepath.Dir(path)})
	if err != nil || len(resolved) != 1 {
		t.Fatalf("explicit source resolution: %+v, %v", resolved, err)
	}
	entry := resolved[0]
	if entry.Path != path || entry.Scope != "explicit" || strings.Contains(entry.ID, f.base) || entry.SHA256 != fileHash(t, path) {
		t.Fatalf("explicit source leaked or moved: %+v", entry)
	}
	if _, err := catalog.Resolve([]string{"../external/source skill"}); err == nil {
		t.Fatal("relative path accepted as an implicit source")
	}
	catalog, err = skills.DiscoverWithRoots(f.repo, "codex", []string{filepath.Dir(path)})
	if err != nil {
		t.Fatal(err)
	}
	if found := findSkill(t, catalog, "external"); found.ID != entry.ID || found.Path != path {
		t.Fatalf("explicit root and direct selectors disagree: %+v vs %+v", found, entry)
	}
}

func TestSkillClaudeOptionalMetadata(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.claude/skills/fallback/SKILL.md", "\n# First instruction line\n\nBody.\n")
	entry := findSkill(t, f.discover("claude"), "fallback")
	if entry.Description != "# First instruction line" || entry.Disabled {
		t.Fatalf("Claude fallback metadata = %+v", entry)
	}
	if _, err := skills.Discover(f.repo, "unsupported"); err == nil {
		t.Fatal("unsupported host accepted")
	}
}

type inventoryFixture struct {
	t                       *testing.T
	base, repo, home, codex string
	claude                  string
}

func newInventory(t *testing.T) inventoryFixture {
	t.Helper()
	f := inventoryFixture{t: t, base: t.TempDir()}
	f.repo, f.home = filepath.Join(f.base, "repo"), filepath.Join(f.base, "home")
	f.codex, f.claude = filepath.Join(f.base, "codex"), filepath.Join(f.base, "claude")
	for _, path := range []string{f.repo, f.home, f.codex, f.claude} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", f.home)
	t.Setenv("USERPROFILE", f.home)
	t.Setenv("CODEX_HOME", f.codex)
	t.Setenv("CLAUDE_CONFIG_DIR", f.claude)
	t.Chdir(f.repo)
	return f
}

func (f inventoryFixture) write(rel, text string) string {
	f.t.Helper()
	path := filepath.Join(f.base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f inventoryFixture) discover(host string) skills.Catalog {
	f.t.Helper()
	catalog, err := skills.Discover(f.repo, host)
	if err != nil {
		f.t.Fatal(err)
	}
	return catalog
}

func skillText(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\nInstructions.\n"
}

func findSkill(t *testing.T, catalog skills.Catalog, name string) skills.Entry {
	t.Helper()
	for _, entry := range catalog.Skills {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("skill %q missing from %+v", name, catalog)
	return skills.Entry{}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
