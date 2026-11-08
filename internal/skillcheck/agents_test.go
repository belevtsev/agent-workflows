package skillcheck

// Tests for contract roster-tests, revision 1, section A (plugin agent
// roster). Each check is a function of a repository root so that it runs on
// the real checkout and on synthetic roots that must make it fail.

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// removedAgentNames are the agents A4 keeps removed.
var removedAgentNames = []string{"cw-reviewer", "cw-test-author", "cw-github-worker", "spec-reviewer"}

type agentFrontmatter struct {
	Name            string `yaml:"name"`
	Description     string `yaml:"description"`
	DisallowedTools any    `yaml:"disallowedTools"`
	Model           string `yaml:"model"`
	Effort          string `yaml:"effort"`
}

// parseAgentFrontmatter decodes the YAML between the leading --- lines.
func parseAgentFrontmatter(data []byte) (agentFrontmatter, error) {
	var fields agentFrontmatter
	head, ok := bytes.CutPrefix(data, []byte("---\n"))
	end := bytes.Index(head, []byte("\n---\n"))
	if !ok || end < 0 {
		return fields, errors.New("missing frontmatter")
	}
	if err := yaml.Unmarshal(head[:end], &fields); err != nil {
		return fields, fmt.Errorf("invalid frontmatter: %v", err)
	}
	return fields, nil
}

// pluginAgents returns the agents array of root/.claude-plugin/plugin.json.
func pluginAgents(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, err
	}
	var plugin struct {
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil {
		return nil, fmt.Errorf(".claude-plugin/plugin.json: %v", err)
	}
	return plugin.Agents, nil
}

// agentFiles returns the *.md files directly under root/agents as
// slash-separated repository paths such as "agents/reviewer.md".
func agentFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "agents"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") && !entry.IsDir() {
			files = append(files, "agents/"+entry.Name())
		}
	}
	return files, nil
}

// listedAgentPath maps a plugin.json agents entry to its repository path and
// reports whether that path names a file directly under agents/.
func listedAgentPath(entry string) (string, bool) {
	clean := path.Clean(entry)
	if entry == "" || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return clean, false
	}
	return clean, path.Dir(clean) == "agents" && path.Ext(clean) == ".md"
}

// agentListingProblems checks A1: every plugin.json agents entry resolves to
// an existing file directly under agents/, and every agents/*.md is listed.
func agentListingProblems(root string) []string {
	listed, err := pluginAgents(root)
	if err != nil {
		return []string{err.Error()}
	}
	files, err := agentFiles(root)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	resolved := map[string]bool{}
	for _, entry := range listed {
		file, ok := listedAgentPath(entry)
		if !ok {
			problems = append(problems, fmt.Sprintf("plugin.json agents entry %q is not a file directly under agents/", entry))
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil || !info.Mode().IsRegular() {
			problems = append(problems, fmt.Sprintf("plugin.json agents entry %q does not resolve to an existing file", entry))
			continue
		}
		resolved[file] = true
	}
	for _, file := range files {
		if !resolved[file] {
			problems = append(problems, file+": agent file is not listed in .claude-plugin/plugin.json agents")
		}
	}
	return problems
}

// agentFrontmatterProblems checks A2 for one listed agent file; each problem
// names the file and the field.
func agentFrontmatterProblems(file string, data []byte) []string {
	fields, err := parseAgentFrontmatter(data)
	if err != nil {
		return []string{file + ": " + err.Error()}
	}
	var problems []string
	if stem := strings.TrimSuffix(path.Base(file), ".md"); fields.Name != stem {
		problems = append(problems, fmt.Sprintf("%s: name %q differs from the file name %q", file, fields.Name, stem))
	}
	if strings.TrimSpace(fields.Description) == "" {
		problems = append(problems, file+": description is empty")
	}
	if tools, ok := fields.DisallowedTools.(string); !ok || tools != "Skill" {
		problems = append(problems, fmt.Sprintf("%s: disallowedTools = %#v, want Skill", file, fields.DisallowedTools))
	}
	if !slices.Contains([]string{"opus", "sonnet", "haiku"}, fields.Model) {
		problems = append(problems, fmt.Sprintf("%s: model %q is not opus, sonnet or haiku", file, fields.Model))
	}
	if !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, fields.Effort) {
		problems = append(problems, fmt.Sprintf("%s: effort %q is not low, medium, high, xhigh or max", file, fields.Effort))
	}
	return problems
}

// listedAgentFrontmatterProblems applies A2 to every listed agent file that
// resolves; unresolved entries are A1's concern.
func listedAgentFrontmatterProblems(root string) []string {
	listed, err := pluginAgents(root)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, entry := range listed {
		file, ok := listedAgentPath(entry)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		problems = append(problems, agentFrontmatterProblems(file, data)...)
	}
	return problems
}

// agentPinProblems checks A3's owner-fixed pins.
func agentPinProblems(root string) []string {
	listed, err := pluginAgents(root)
	if err != nil {
		return []string{err.Error()}
	}
	listedFiles := map[string]bool{}
	for _, entry := range listed {
		if file, ok := listedAgentPath(entry); ok {
			listedFiles[file] = true
		}
	}
	var problems []string
	for _, pin := range []struct{ file, model, effort string }{
		{"agents/reviewer.md", "opus", "high"},
		{"agents/github-worker.md", "sonnet", "medium"},
		{"agents/test-engineer.md", "", ""},
	} {
		if !listedFiles[pin.file] {
			problems = append(problems, pin.file+": not listed in .claude-plugin/plugin.json agents")
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pin.file)))
		if err != nil {
			problems = append(problems, pin.file+": does not exist")
			continue
		}
		if pin.model == "" {
			continue
		}
		fields, err := parseAgentFrontmatter(data)
		if err != nil {
			problems = append(problems, pin.file+": "+err.Error())
			continue
		}
		if fields.Model != pin.model {
			problems = append(problems, fmt.Sprintf("%s: model %q, want %q", pin.file, fields.Model, pin.model))
		}
		if fields.Effort != pin.effort {
			problems = append(problems, fmt.Sprintf("%s: effort %q, want %q", pin.file, fields.Effort, pin.effort))
		}
	}
	return problems
}

// removedAgentProblems checks A4: no listed agent or agents/*.md file is a
// removed agent by name or by file stem, and global/claude-agents is gone.
func removedAgentProblems(root string) []string {
	listed, err := pluginAgents(root)
	if err != nil {
		return []string{err.Error()}
	}
	files, err := agentFiles(root)
	if err != nil {
		return []string{err.Error()}
	}
	candidates := slices.Clone(files)
	for _, entry := range listed {
		candidates = append(candidates, path.Clean(entry))
	}
	slices.Sort(candidates)
	var problems []string
	for _, file := range slices.Compact(candidates) {
		if stem := strings.TrimSuffix(path.Base(file), ".md"); slices.Contains(removedAgentNames, stem) {
			problems = append(problems, fmt.Sprintf("%s: file stem %q is a removed agent", file, stem))
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		if fields, err := parseAgentFrontmatter(data); err == nil && slices.Contains(removedAgentNames, fields.Name) {
			problems = append(problems, fmt.Sprintf("%s: name %q is a removed agent", file, fields.Name))
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "global", "claude-agents")); !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, "global/claude-agents: directory still exists")
	}
	return problems
}

// --- synthetic roots ---------------------------------------------------------

func agentDoc(name, model, effort string) string {
	return "---\nname: " + name + "\ndescription: Synthetic " + name + " agent.\nmodel: " + model +
		"\neffort: " + effort + "\ndisallowedTools: Skill\n---\n\n# " + name + "\n"
}

// validAgentRoot builds a root whose roster satisfies A1 to A4.
func validAgentRoot(t *testing.T) (string, map[string]string) {
	t.Helper()
	files := map[string]string{
		"agents/reviewer.md":      agentDoc("reviewer", "opus", "high"),
		"agents/github-worker.md": agentDoc("github-worker", "sonnet", "medium"),
		"agents/test-engineer.md": agentDoc("test-engineer", "opus", "high"),
	}
	return writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md"}, files), files
}

func writeAgentRoot(t *testing.T, listed []string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	plugin, err := json.Marshal(map[string]any{"agents": listed})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, ".claude-plugin", "plugin.json"), plugin)
	for name, data := range files {
		writeFixture(t, filepath.Join(root, filepath.FromSlash(name)), []byte(data))
	}
	return root
}

// wantProblem fails unless some problem contains every fragment.
func wantProblem(t *testing.T, problems []string, fragments ...string) {
	t.Helper()
	for _, problem := range problems {
		matched := true
		for _, fragment := range fragments {
			matched = matched && strings.Contains(problem, fragment)
		}
		if matched {
			return
		}
	}
	t.Errorf("no problem contains all of %q; problems: %q", fragments, problems)
}

func TestAgentRosterSyntheticBaselineIsClean(t *testing.T) {
	root, _ := validAgentRoot(t)
	for name, check := range map[string]func(string) []string{
		"A1": agentListingProblems,
		"A2": listedAgentFrontmatterProblems,
		"A3": agentPinProblems,
		"A4": removedAgentProblems,
	} {
		if problems := check(root); len(problems) != 0 {
			t.Errorf("%s on a valid synthetic root: %q", name, problems)
		}
	}
}

// --- A1 ----------------------------------------------------------------------

// A1 fails when a plugin.json agents entry does not resolve to a file directly
// under agents/, or when an agents/*.md file is unlisted.
func TestA1_PluginAgentsMatchAgentFiles(t *testing.T) {
	listed, err := pluginAgents(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 {
		t.Fatal(".claude-plugin/plugin.json lists no agents")
	}
	for _, problem := range agentListingProblems(repoRoot) {
		t.Errorf("A1: %s", problem)
	}
}

func TestA1_SyntheticViolationsAreReported(t *testing.T) {
	t.Run("unlisted agent file", func(t *testing.T) {
		_, files := validAgentRoot(t)
		files["agents/stray.md"] = agentDoc("stray", "opus", "high")
		root := writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md"}, files)
		problems := agentListingProblems(root)
		wantProblem(t, problems, "agents/stray.md")
		if len(problems) != 1 {
			t.Errorf("problems = %q, want only the unlisted agents/stray.md", problems)
		}
	})
	t.Run("listed file missing", func(t *testing.T) {
		_, files := validAgentRoot(t)
		root := writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md", "./agents/ghost.md"}, files)
		wantProblem(t, agentListingProblems(root), "./agents/ghost.md")
	})
	t.Run("listed file outside agents", func(t *testing.T) {
		_, files := validAgentRoot(t)
		files["docs/reviewer.md"] = agentDoc("reviewer", "opus", "high")
		root := writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md", "./docs/reviewer.md"}, files)
		wantProblem(t, agentListingProblems(root), "./docs/reviewer.md")
	})
	t.Run("listed file in a subdirectory of agents", func(t *testing.T) {
		_, files := validAgentRoot(t)
		files["agents/nested/helper.md"] = agentDoc("helper", "opus", "high")
		root := writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md", "./agents/nested/helper.md"}, files)
		wantProblem(t, agentListingProblems(root), "./agents/nested/helper.md")
	})
	t.Run("listed path escapes the repository", func(t *testing.T) {
		_, files := validAgentRoot(t)
		root := writeAgentRoot(t, []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md", "./agents/../../outside.md"}, files)
		wantProblem(t, agentListingProblems(root), "./agents/../../outside.md")
	})
}

// --- A2 ----------------------------------------------------------------------

// A2 fails when a listed agent's frontmatter has a wrong name, an empty
// description, disallowedTools other than Skill, or a model or effort outside
// the allowed sets.
func TestA2_ListedAgentFrontmatter(t *testing.T) {
	listed, err := pluginAgents(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range listed {
		if file, ok := listedAgentPath(entry); ok {
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(file))); err == nil {
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no listed agent file was checked")
	}
	for _, problem := range listedAgentFrontmatterProblems(repoRoot) {
		t.Errorf("A2: %s", problem)
	}
}

func TestA2_SyntheticViolationsNameFileAndField(t *testing.T) {
	const file = "agents/worker.md"
	valid := map[string]string{
		"name":            "worker",
		"description":     "Synthetic worker.",
		"model":           "opus",
		"effort":          "high",
		"disallowedTools": "Skill",
	}
	doc := func(override map[string]string, drop string) []byte {
		var b strings.Builder
		b.WriteString("---\n")
		for _, key := range []string{"name", "description", "model", "effort", "disallowedTools"} {
			if key == drop {
				continue
			}
			value := valid[key]
			if v, ok := override[key]; ok {
				value = v
			}
			b.WriteString(key + ": " + value + "\n")
		}
		b.WriteString("---\n\nbody\n")
		return []byte(b.String())
	}
	if problems := agentFrontmatterProblems(file, doc(nil, "")); len(problems) != 0 {
		t.Fatalf("valid frontmatter rejected: %q", problems)
	}
	for _, model := range []string{"opus", "sonnet", "haiku"} {
		if problems := agentFrontmatterProblems(file, doc(map[string]string{"model": model}, "")); len(problems) != 0 {
			t.Errorf("model %s rejected: %q", model, problems)
		}
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		if problems := agentFrontmatterProblems(file, doc(map[string]string{"effort": effort}, "")); len(problems) != 0 {
			t.Errorf("effort %s rejected: %q", effort, problems)
		}
	}
	for _, test := range []struct {
		name     string
		override map[string]string
		drop     string
		field    string
	}{
		{"name differs from file", map[string]string{"name": "other-worker"}, "", "name"},
		{"name missing", nil, "name", "name"},
		{"description empty", map[string]string{"description": `""`}, "", "description"},
		{"description missing", nil, "description", "description"},
		{"disallowedTools other tool", map[string]string{"disallowedTools": "Bash"}, "", "disallowedTools"},
		{"disallowedTools missing", nil, "disallowedTools", "disallowedTools"},
		{"model unknown", map[string]string{"model": "gpt-5"}, "", "model"},
		{"model missing", nil, "model", "model"},
		{"effort unknown", map[string]string{"effort": "extreme"}, "", "effort"},
		{"effort missing", nil, "effort", "effort"},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems := agentFrontmatterProblems(file, doc(test.override, test.drop))
			wantProblem(t, problems, file, test.field)
		})
	}
	if problems := agentFrontmatterProblems(file, []byte("# no frontmatter\n")); len(problems) == 0 {
		t.Error("a file without frontmatter was accepted")
	}
}

// --- A3 ----------------------------------------------------------------------

// A3 fails when reviewer is not opus/high, github-worker is not sonnet/medium,
// or test-engineer is missing or unlisted.
func TestA3_OwnerFixedPins(t *testing.T) {
	for _, problem := range agentPinProblems(repoRoot) {
		t.Errorf("A3: %s", problem)
	}
}

func TestA3_SyntheticViolationsAreReported(t *testing.T) {
	allListed := []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md"}
	for _, test := range []struct {
		name      string
		listed    []string
		change    func(map[string]string)
		fragments []string
	}{
		{"reviewer model", allListed, func(f map[string]string) { f["agents/reviewer.md"] = agentDoc("reviewer", "sonnet", "high") }, []string{"agents/reviewer.md", "model"}},
		{"reviewer effort", allListed, func(f map[string]string) { f["agents/reviewer.md"] = agentDoc("reviewer", "opus", "xhigh") }, []string{"agents/reviewer.md", "effort"}},
		{"github-worker model", allListed, func(f map[string]string) { f["agents/github-worker.md"] = agentDoc("github-worker", "opus", "medium") }, []string{"agents/github-worker.md", "model"}},
		{"github-worker effort", allListed, func(f map[string]string) { f["agents/github-worker.md"] = agentDoc("github-worker", "sonnet", "high") }, []string{"agents/github-worker.md", "effort"}},
		{"test-engineer missing", allListed, func(f map[string]string) { delete(f, "agents/test-engineer.md") }, []string{"agents/test-engineer.md"}},
		{"test-engineer unlisted", allListed[:2], func(map[string]string) {}, []string{"agents/test-engineer.md", "not listed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, files := validAgentRoot(t)
			test.change(files)
			root := writeAgentRoot(t, test.listed, files)
			wantProblem(t, agentPinProblems(root), test.fragments...)
		})
	}
}

// --- A4 ----------------------------------------------------------------------

// A4 fails when a removed agent comes back by name or file stem, listed or
// not, or when global/claude-agents exists.
func TestA4_RemovedAgentsStayRemoved(t *testing.T) {
	for _, problem := range removedAgentProblems(repoRoot) {
		t.Errorf("A4: %s", problem)
	}
}

func TestA4_SyntheticViolationsAreReported(t *testing.T) {
	base := []string{"./agents/reviewer.md", "./agents/github-worker.md", "./agents/test-engineer.md"}
	for _, removed := range removedAgentNames {
		t.Run("file stem "+removed, func(t *testing.T) {
			_, files := validAgentRoot(t)
			files["agents/"+removed+".md"] = agentDoc("renamed", "opus", "high")
			root := writeAgentRoot(t, base, files)
			wantProblem(t, removedAgentProblems(root), "agents/"+removed+".md", removed)
		})
		t.Run("name "+removed, func(t *testing.T) {
			_, files := validAgentRoot(t)
			files["agents/innocent.md"] = agentDoc(removed, "opus", "high")
			root := writeAgentRoot(t, append(slices.Clone(base), "./agents/innocent.md"), files)
			wantProblem(t, removedAgentProblems(root), "agents/innocent.md", removed)
		})
		t.Run("listed only "+removed, func(t *testing.T) {
			_, files := validAgentRoot(t)
			root := writeAgentRoot(t, append(slices.Clone(base), "./agents/"+removed+".md"), files)
			wantProblem(t, removedAgentProblems(root), removed)
		})
	}
	t.Run("global/claude-agents directory", func(t *testing.T) {
		root, _ := validAgentRoot(t)
		if err := os.MkdirAll(filepath.Join(root, "global", "claude-agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantProblem(t, removedAgentProblems(root), "global/claude-agents")
	})
}
