package skills_test

import (
	"path/filepath"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func TestReadMetadataKeepsInvocationRulesWithoutApplyingHostSettings(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			f := newInventory(t)
			f.write("codex/config.toml", "[[skills.config]]\nname = 'task'\nenabled = false\n")
			f.write("claude/settings.json", `{"skillOverrides":{"task":"off"}}`)
			path := f.write("repo/task/SKILL.md", "---\nname: task\ndescription: Coordinate this task.\ndisable-model-invocation: true\nuser-invocable: false\n---\nDo not execute sibling scripts.\n")
			f.write("codex/config.toml", "[[skills.config]]\nname = 'task'\nenabled = false\n")
			f.write("claude/settings.json", `{"skillOverrides":{"task":"off"}}`)
			for _, source := range []string{path, filepath.Dir(path)} {
				entry, err := skills.ReadMetadata(source, host)
				if err != nil || entry.Name != "task" || entry.Path != path || entry.SHA256 != fileHash(t, path) || !entry.ManualOnly || entry.UserInvocable || entry.Disabled || entry.Scope != "" || entry.ID != "" {
					t.Fatalf("metadata-only read = %+v, %v", entry, err)
				}
			}
		})
	}
}

func TestReadMetadataRejectsInvalidSourceAndInvocationMetadata(t *testing.T) {
	f := newInventory(t)
	path := f.write("repo/task/SKILL.md", skillText("task", "Coordinate work."))
	if _, err := skills.ReadMetadata(path, "unknown"); err == nil {
		t.Fatal("unknown host accepted")
	}
	f.write("repo/task/agents/openai.yaml", "policy: [broken")
	if _, err := skills.ReadMetadata(path, "codex"); err == nil {
		t.Fatal("malformed native invocation policy accepted")
	}
	f.write("repo/task/SKILL.md", "---\nname: [broken\n---\n")
	if _, err := skills.ReadMetadata(path, "claude"); err == nil {
		t.Fatal("malformed skill accepted")
	}
}
