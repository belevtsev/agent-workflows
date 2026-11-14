package skillcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// substantialHome is the one file that defines a substantial task. Every
// other file links to its "Substantial tasks" section.
const substantialHome = "global/AGENTS.md"

// substantialDefinition matches a statement of when a task is substantial:
// the defining clause, the size criterion that only a definition carries, the
// "independent workstreams" criterion of the older any-of rule, or a policy's
// activation block.
var substantialDefinition = regexp.MustCompile(`(?im)\bsubstantial when\b|\babout 300 changed lines\b|\bindependent[_ ]workstreams\b|^\s*activation:`)

// definitionRoots are the paths that tell an agent or a user when to run the
// staged flow: the always-on conventions, the skills, the agents, the hooks
// and the docs.
var definitionRoots = []string{"README.md", "docs/", "global/", "agents/", "hooks/", "skills/"}

// substantialClause opens the definition in substantialHome.
const substantialClause = "A task is substantial when"

// oracle: among the first-party Markdown, YAML and JSON files under
// definitionRoots, substantialDefinition matches only in global/AGENTS.md, and
// there substantialClause appears exactly once, so the activation rule cannot
// drift between copies (QR-38).
func TestSubstantialTaskIsDefinedOnce(t *testing.T) {
	found := map[string]int{}
	for _, file := range repoFiles(t) {
		if !underAny(file, definitionRoots) || strings.Contains(file, "third_party/") {
			continue
		}
		switch filepath.Ext(file) {
		case ".md", ".yaml", ".yml", ".json":
		default:
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(substantialDefinition.FindAllIndex(data, -1)); n > 0 {
			found[file] = n
		}
	}
	home, err := os.ReadFile(filepath.Join(repoRoot, substantialHome))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(home), substantialClause); n != 1 {
		t.Errorf("%s says %q %d times, want exactly 1", substantialHome, substantialClause, n)
	}
	for file, n := range found {
		if file != substantialHome {
			t.Errorf("%s states when a task is substantial (%d matches of %s): link to %s#substantial-tasks instead",
				file, n, substantialDefinition, substantialHome)
		}
	}
}

// underAny reports whether the slash path file is one of roots or under one
// that ends in a slash.
func underAny(file string, roots []string) bool {
	for _, root := range roots {
		if file == root || (strings.HasSuffix(root, "/") && strings.HasPrefix(file, root)) {
			return true
		}
	}
	return false
}
