package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// section is the part of a text under one "## " heading, with every run of
// white space read as one space.
func section(text, heading string) string {
	_, rest, found := strings.Cut(text, "\n## "+heading+"\n")
	if !found {
		return ""
	}
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return strings.Join(strings.Fields(rest), " ")
}

// A few rules of the flow are held here by the words that state them, so
// that an edit that drops one fails. The texts say more than this checks.
func TestTextsHoldTheRulesOfTheFlow(t *testing.T) {
	cases := []struct{ id, heading, words string }{
		{Coordinator, "After every role", "After `spec-writer` and `architect`, run `kitsu task check <task>`."},
		{Coordinator, "After every role", "A second write-limit violation by that role stops for the developer."},
		{Coordinator, "Reviews", "A stage has at most two review rounds."},
		{Coordinator, "Reviews", "Only the developer rejects a `blocker` or `major` finding"},
		{Coordinator, "The full path", "Test changes reported by `developer` go to `quality-engineer`"},
		{Coordinator, "The small path", "a small-path task cannot be continued from files"},
		{Coordinator, "Before the first role", "`git status --porcelain`"},
		{Coordinator, "Starting a role", "Start a fresh worker without this conversation or a fork"},
		{"quality-engineer", "Rules", "You are the only role that changes or removes an existing test."},
		{"developer", "Write", "You may not change or remove an existing one."},
		{"reviewer", "What to look for", "Check each test against its scenario."},
	}
	for _, c := range cases {
		text, err := Text(c.id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(section(text, c.heading), c.words) {
			t.Errorf("the text of %s does not say under %q: %s", c.id, c.heading, c.words)
		}
	}
}

// The two paths are defined in the same words by the coordinator's text and
// by the example of the managed skill.
func TestThePathsHaveOneDefinition(t *testing.T) {
	text, err := Text(Coordinator)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root(t), ManagedSkill, "references", "role-workflow.md"))
	if err != nil {
		t.Fatal(err)
	}
	example := strings.Join(strings.Fields(string(data)), " ")
	paths := section(text, "The two paths")
	for _, words := range []string{
		"**Full path**: the task needs a requirement written or changed: new behaviour, or a change to behaviour that is specified or relied on.",
		"**Small path**: the intended behaviour is already clear and does not change: a defect against stated behaviour, a refactoring, a change of tests only.",
		"Size does not choose the path.",
	} {
		if !strings.Contains(paths, words) {
			t.Errorf("the coordinator's text does not define under \"The two paths\": %s", words)
		}
		if !strings.Contains(example, words) {
			t.Errorf("%s/references/role-workflow.md does not hold: %s", ManagedSkill, words)
		}
	}
}
