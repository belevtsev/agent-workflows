package agentworkflows_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// catalogRow matches one table row of skills/README.md:
// | [name](name/SKILL.md) | automatic or on request | description |
var catalogRow = regexp.MustCompile(`^\| \[([a-z0-9-]+)\]\(([a-z0-9-]+)/SKILL\.md\) \| (automatic|on request) \| (.+) \|$`)

type catalogEntry struct {
	visibility  string
	description string
}

// TestSkillCatalogMatchesSkills keeps skills/README.md in step with every
// skills/*/SKILL.md: same set of skills, same visibility, same description.
func TestSkillCatalogMatchesSkills(t *testing.T) {
	want := map[string]catalogEntry{}
	paths, err := filepath.Glob(filepath.Join("skills", "*", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		entry, err := readSkillEntry(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		want[name] = entry
	}
	if len(want) == 0 {
		t.Fatal("no skills/*/SKILL.md found")
	}

	data, err := os.ReadFile(filepath.Join("skills", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]catalogEntry{}
	for _, line := range strings.Split(string(data), "\n") {
		m := catalogRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] != m[2] {
			t.Errorf("catalog row %q links to %s/SKILL.md", m[1], m[2])
		}
		if _, dup := got[m[1]]; dup {
			t.Errorf("catalog lists %q twice", m[1])
		}
		got[m[1]] = catalogEntry{visibility: m[3], description: m[4]}
	}

	for _, name := range sortedKeys(want) {
		g, ok := got[name]
		if !ok {
			t.Errorf("skills/README.md is missing %q", name)
			continue
		}
		if g != want[name] {
			t.Errorf("skills/README.md row for %q = %+v; SKILL.md says %+v", name, g, want[name])
		}
	}
	for _, name := range sortedKeys(got) {
		if _, ok := want[name]; !ok {
			t.Errorf("skills/README.md lists %q, which has no SKILL.md", name)
		}
	}
}

// readSkillEntry reads the description and visibility from a SKILL.md
// frontmatter block.
func readSkillEntry(path string) (catalogEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return catalogEntry{}, err
	}
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) < 3 {
		return catalogEntry{}, os.ErrInvalid
	}
	entry := catalogEntry{visibility: "automatic"}
	for _, line := range strings.Split(parts[1], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "description":
			entry.description = value
		case "disable-model-invocation":
			if value == "true" {
				entry.visibility = "on request"
			}
		}
	}
	if entry.description == "" {
		return catalogEntry{}, os.ErrInvalid
	}
	return entry, nil
}

func sortedKeys(m map[string]catalogEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
