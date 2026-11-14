package main_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// plainCW is the one cw binary this package's tests run. buildPlainCW builds
// it on first use and TestMain removes it after the last test. Its directory
// holds nothing else, so a test may put that directory on PATH.
var plainCW struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if plainCW.dir != "" {
		_ = os.RemoveAll(plainCW.dir)
	}
	os.Exit(code)
}

// goTool finds the go command of the running toolchain, then PATH.
func goTool(t *testing.T) string {
	t.Helper()
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if root := runtime.GOROOT(); root != "" { //nolint:staticcheck // SA1019: a test binary runs where go test built it, so this is the toolchain that built the test.
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

// buildPlainCW returns cmd/cw built with a plain go build, once per package
// run. Every test shares the file, so a caller only runs or copies it and
// never moves, changes or removes it.
func buildPlainCW(t *testing.T) string {
	t.Helper()
	tool := goTool(t)
	plainCW.once.Do(func() { plainCW.err = buildPlainCWOnce(tool) })
	if plainCW.err != nil {
		t.Fatal(plainCW.err)
	}
	return plainCW.path
}

func buildPlainCWOnce(tool string) error {
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "cw-e2e-bin-")
	if err != nil {
		return err
	}
	plainCW.dir = dir
	binary := filepath.Join(dir, "cw")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, tool, "build", "-o", binary, "github.com/belevtsev/agent-workflows/v3/cmd/cw")
	build.Dir = module
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build cmd/cw: %v\n%s", err, output)
	}
	plainCW.path = binary
	return nil
}
