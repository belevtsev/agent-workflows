package roles_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// roleFile is the Claude Code file of a role, in the opt-in plugin. managedSkill
// holds the coordinator's instructions there.
func roleFile(id string) string { return "plugins/kitsu-team/agents/" + id + ".md" }

const managedSkill = "plugins/kitsu-team/skills/managed/SKILL.md"

// Requirement "Explicit start", scenario "No role can start a role".
func TestNoRoleCanStartARole(t *testing.T) {
	t.Parallel()
	for _, id := range roleIDs {
		fields := frontMatter(t, roleFile(id))
		denied := contains(toolList(fields["disallowedTools"]), "Agent")
		tools, listed := fields["tools"]
		allowed := !listed || contains(toolList(tools), "Agent")
		if allowed && !denied {
			t.Errorf("%s lets the role use Agent: tools %q, disallowedTools %q", roleFile(id), tools, fields["disallowedTools"])
		}
	}
}

// Requirement "Roles without a writing tool", scenario "The tools of the two
// reading roles on Claude Code".
func TestTheToolsOfTheTwoReadingRolesOnClaudeCode(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"lookup", "reviewer"} {
		fields := frontMatter(t, roleFile(id))
		tools, listed := fields["tools"]
		if !listed {
			t.Errorf("%s lists no tools, so the role has every tool", roleFile(id))
			continue
		}
		names := toolList(tools)
		forbidden := []string{"Edit", "Write", "NotebookEdit"}
		if id == "lookup" {
			forbidden = append(forbidden, "Bash")
		}
		for _, name := range forbidden {
			if contains(names, name) {
				t.Errorf("%s lists %s among its tools: %s", roleFile(id), name, tools)
			}
		}
	}
}

// Requirement "Role text names no language", scenario "A search of the
// texts".
func TestASearchOfTheTexts(t *testing.T) {
	t.Parallel()
	words := []string{"go test", "npm", "pytest", "cargo", "gradle", "maven"}
	search := func(t *testing.T, name, text string) {
		t.Helper()
		for _, word := range words {
			if strings.Contains(text, word) {
				t.Errorf("%s holds %q", name, word)
			}
		}
	}

	// The one text of each role and the coordinator's, as files and as
	// kitsu roles text prints them.
	t.Run("texts", func(t *testing.T) {
		t.Parallel()
		repo := demo(t)
		for _, id := range append(append([]string(nil), roleIDs...), "coordinator") {
			rel := "internal/roles/text/" + id + ".md"
			search(t, rel, readModuleFile(t, rel))
			result := repo.Kitsu("roles", "text", id)
			if result.Code != 0 {
				t.Errorf("kitsu roles text %s exited %d:\n%s", id, result.Code, result.Output())
			}
			search(t, "kitsu roles text "+id, result.Stdout)
		}
	})

	// What the host reads: the role files and the coordinator's instructions
	// of the opt-in plugin, and the skill for a daily task.
	t.Run("host files", func(t *testing.T) {
		t.Parallel()
		for _, id := range roleIDs {
			search(t, roleFile(id), readModuleFile(t, roleFile(id)))
		}
		for _, skill := range []string{"skills/task/SKILL.md", managedSkill} {
			search(t, skill, readModuleFile(t, skill))
		}
	})
}

// Requirement "One text per role", scenario "A host file is edited by hand".
func TestAHostFileIsEditedByHand(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs go test on a copy of the module")
	}
	root := copyModule(t)
	file := roleFile("architect")
	path := filepath.Join(root, filepath.FromSlash(file))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// One sentence of the role's body; the file may wrap it over two lines.
	sentence := regexp.MustCompile(`You\s+write\s+no\s+test\s+and\s+no\s+code\.`)
	if !sentence.Match(data) {
		t.Fatalf("%s does not hold the sentence %q to change", file, sentence)
	}
	edited := sentence.ReplaceAllString(string(data), "You write the tests and the code as well.")
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := goTestRoles(t, root)
	if code == 0 {
		t.Fatalf("go test ./internal/roles passed with %s edited by hand:\n%s", file, out)
	}
	if !strings.Contains(out, "--- FAIL") {
		t.Errorf("no test failed; go test exited %d for another reason:\n%s", code, out)
	}
	if !strings.Contains(out, file) {
		t.Errorf("the failure does not name %s:\n%s", file, out)
	}
}

// Requirement "Text budget", scenario "A text grows past its limit".
func TestATextGrowsPastItsLimit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs go test on a copy of the module")
	}
	root := copyModule(t)
	path := filepath.Join(root, "internal", "roles", "text", "developer.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	const limit = 8000
	have := utf8.RuneCountInString(text)
	if have >= limit {
		t.Fatalf("the text of developer already has %d characters", have)
	}
	// Plain words, so that only the size changes.
	padding := strings.Repeat("Keep the work small. ", (limit+1-have)/21+1)
	text += padding[:limit+1-have]
	if n := utf8.RuneCountInString(text); n != limit+1 {
		t.Fatalf("the grown text has %d characters, want %d", n, limit+1)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := goTestRoles(t, root)
	if code == 0 {
		t.Fatalf("go test ./internal/roles passed with a developer text of 8,001 characters:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL") {
		t.Errorf("no test failed; go test exited %d for another reason:\n%s", code, out)
	}
	limitWord := regexp.MustCompile(`(^|[^0-9,])(8000|8,000|8 000)($|[^0-9,])`)
	named := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "developer") && limitWord.MatchString(line) {
			named = true
		}
	}
	if !named {
		t.Errorf("no failure line names developer and the limit 8000:\n%s", out)
	}
}
