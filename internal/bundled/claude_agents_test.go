package bundled_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// Contract claude-agents-tests, revision 1, requirement A13: Build packs every
// Claude agent definition the manifest names and refuses a definition outside
// global/claude-agents/<name>.md.

func bundledAgentDefinition(name string) []byte {
	return []byte("---\nname: " + name + "\ndescription: Synthetic agent " + name + ".\ndisallowedTools: Skill\n---\nSynthetic agent body for " + name + ".\n")
}

// bundledAgentFiles is the synthetic source plus claude_agents naming the
// given definition paths, each file present at its path.
func bundledAgentFiles(test *testing.T, agents map[string]string) map[string]bundledResource {
	test.Helper()
	files := bundledFixtureFiles(test, "skills/demo")
	manifest := bundledFixtureManifest("skills/demo")
	manifest["claude_agents"] = agents
	files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
	for name, relative := range agents {
		if strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, "../") {
			continue
		}
		files[relative] = bundledResource{bundledAgentDefinition(name), 0644}
	}
	return files
}

func TestA13_BuildIncludesClaudeAgentDefinitions(test *testing.T) {
	agents := map[string]string{
		"cw-x":       "global/claude-agents/cw-x.md",
		"cw-another": "global/claude-agents/cw-another.md",
	}
	files := bundledAgentFiles(test, agents)
	root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
	bundle := bundledOpen(test, bundledBuild(test, root))
	for name, relative := range agents {
		data, err := bundle.ReadFile(relative)
		if err != nil {
			test.Fatalf("bundle lacks definition %s at %s: %v", name, relative, err)
		}
		if !bytes.Equal(data, bundledAgentDefinition(name)) {
			test.Fatalf("bundled definition %s = %q, want %q", relative, data, bundledAgentDefinition(name))
		}
	}
	bundledRequireReads(test, bundle, bundledRequiredFiles(files))
}

func TestA13_BuildRejectsDefinitionsOutsideTheAgentDirectory(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		agents map[string]string
	}{
		{"elsewhere_in_global", map[string]string{"cw-x": "global/elsewhere.md"}},
		{"another_name_in_the_agent_directory", map[string]string{"cw-x": "global/claude-agents/cw-y.md"}},
		{"nested_below_the_agent_directory", map[string]string{"cw-x": "global/claude-agents/nested/cw-x.md"}},
		{"registration_root", map[string]string{"cw-x": "skills/demo/cw-x.md"}},
		{"escaping", map[string]string{"cw-x": "../cw-x.md"}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			files := bundledAgentFiles(test, scenario.agents)
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted claude_agents %v", scenario.agents)
			}
		})
	}
}

// A13b(1): a named definition is a required input. Every global/ file is
// packed when present, so only a missing file shows whether Build requires it.
func TestA13b_BuildRejectsMissingDefinitionFile(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		agents  map[string]string
		missing string
	}{
		{"only_definition", map[string]string{"cw-x": "global/claude-agents/cw-x.md"}, "global/claude-agents/cw-x.md"},
		{"one_of_two", map[string]string{"cw-x": "global/claude-agents/cw-x.md", "cw-y": "global/claude-agents/cw-y.md"}, "global/claude-agents/cw-y.md"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			files := bundledAgentFiles(test, scenario.agents)
			delete(files, scenario.missing)
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted claude_agents %v without %s", scenario.agents, scenario.missing)
			}
		})
	}
}

// A13b(2): claude_agents, when present, is a non-empty mapping.
func TestA13b_BuildRejectsNullAndEmptyAgentMappings(test *testing.T) {
	for _, scenario := range []struct {
		name  string
		value any
	}{
		{"null", nil},
		{"empty", map[string]string{}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			files := bundledFixtureFiles(test, "skills/demo")
			files["global/claude-agents/cw-x.md"] = bundledResource{bundledAgentDefinition("cw-x"), 0644}
			manifest := bundledFixtureManifest("skills/demo")
			manifest["claude_agents"] = scenario.value
			files["skills-manifest.json"] = bundledResource{bundledJSON(test, manifest), 0644}
			if !bytes.Contains(files["skills-manifest.json"].data, []byte(`"claude_agents"`)) {
				test.Fatal("synthetic manifest lost the claude_agents key")
			}
			root := bundledWriteSource(test, files, false, time.Unix(1000000000, 0))
			if _, err := bundled.Build(root, "v1.2.3", strings.Repeat("a", 40)); err == nil {
				test.Fatalf("Build accepted claude_agents %s", files["skills-manifest.json"].data)
			}
		})
	}
}
