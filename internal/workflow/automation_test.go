package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomationRejectsFirstPartyScripts(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"scripts/setup.py", "skills/example/scripts/helper.py", "tests/smoke.sh", "tools/check.bash", "requirements-dev.txt", "run", "evals/example/case/helper.sh", "scaffold.sh"} {
		t.Run(file, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(file))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("#!/usr/bin/env python3\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := ValidateAutomation(root); err == nil {
				t.Fatalf("accepted first-party script %s", file)
			}
		})
	}
}

func TestAutomationPreservesSkillResourcesAndBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, file := range []string{"install.sh", "skills/example/scripts/helper.sh", "third_party/example/helper.py", "third_party/example/helper.sh", "cmd/cw/main.go", "evals/example/case/scaffold.sh"} {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("resource\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateAutomation(root); err != nil {
		t.Fatal(err)
	}
}
