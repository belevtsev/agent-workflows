package tokens_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// moduleRoot is the directory of go.mod, found from the test's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal(errors.New("go.mod not found above the test directory"))
		}
		dir = parent
	}
}

// hookFile is a hook file of the plugin: for each event, groups of hooks.
type hookFile struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// Task 4.4: the plugin runs kitsu tokens record when a role (SubagentStop) or
// the session (SessionEnd) ends, on Claude Code and on Codex.
func TestPluginHooksRecordTokens(t *testing.T) {
	t.Parallel()
	for file, host := range map[string]string{"hooks/hooks.json": "claude", "hooks/codex-hooks.json": "codex"} {
		data, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		var hooks hookFile
		if err := json.Unmarshal(data, &hooks); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		recordCmd := regexp.MustCompile(`"\$c" tokens record --host ` + host + `\b`)
		for _, event := range []string{"SubagentStop", "SessionEnd"} {
			found := 0
			for _, group := range hooks.Hooks[event] {
				for _, h := range group.Hooks {
					if h.Type == "command" && recordCmd.MatchString(h.Command) {
						found++
					}
				}
			}
			if found != 1 {
				t.Errorf("%s: %d commands for %s run kitsu tokens record --host %s, want one", file, found, event, host)
			}
		}
	}
}
