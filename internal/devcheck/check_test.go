package devcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDeveloperPolicyPreservesIgnoredSkillEnvironment(t *testing.T) {
	root := t.TempDir()
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = physical
	writeFixture(t, filepath.Join(root, ".gitignore"), []byte(".venv/\n.bin/\ndist/\n"))
	for _, path := range []string{"install.sh", "skills/example/helper.sh", "third_party/example/helper.py", "third_party/example/tool.sh", ".venv/python", ".bin/private.py", "dist/private.sh"} {
		writeFixture(t, filepath.Join(root, path), []byte("fixture"))
	}
	command := exec.CommandContext(t.Context(), "git", "-C", root, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture git init: %v\n%s", err, output)
	}
	if err := CheckScripts(root); err != nil {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
