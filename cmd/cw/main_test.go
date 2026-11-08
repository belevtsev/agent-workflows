package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// goTool finds the go command of the running toolchain, then PATH.
func goTool(t *testing.T) string {
	t.Helper()
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if root := runtime.GOROOT(); root != "" {
		candidate := filepath.Join(root, "bin", name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go tool not found: %v", err)
	}
	return path
}

// buildPlainCW builds cmd/cw with a plain go build.
func buildPlainCW(t *testing.T) string {
	t.Helper()
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "cw")
	build := exec.CommandContext(t.Context(), goTool(t), "build", "-o", binary, "github.com/belevtsev/agent-workflows/v3/cmd/cw")
	build.Dir = module
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/cw: %v\n%s", err, output)
	}
	return binary
}
