package plugin_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestWindowsSessionStartMissingCLIProvidesOnboarding(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	repo.Write("nested/README.md", "# no gate\n")
	var hooks hookFile
	if err := json.Unmarshal([]byte(readHookFile(t, codexHooks)), &hooks); err != nil {
		t.Fatal(err)
	}
	command := hooks.Hooks["SessionStart"][0].Hooks[0].CommandWindows
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	for name, dir := range map[string]*kitsutest.Repo{"plain": repo, "subfolder": repo.In("nested"), "outside Git": repo.In(t.TempDir())} {
		t.Run(name, func(t *testing.T) {
			dir.Setenv("PATH", t.TempDir())
			got := dir.Run("", cmd, "/d", "/c", command)
			text := strings.TrimSpace(got.Stdout)
			if got.Code != 0 || !strings.Contains(text, "kitsu is not installed") || !strings.Contains(text, "https://github.com/belevtsev/kitsu#install") || strings.ContainsAny(text, "\r\n") || strings.Contains(text, "denied") {
				t.Fatalf("missing CLI fallback = %d, %q; stderr %q", got.Code, text, got.Stderr)
			}
		})
	}
}
